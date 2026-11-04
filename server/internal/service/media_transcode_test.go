// M145 媒体转码 —— service 层接线测试
//
// 覆盖的是**接线**(判出来播不了 → 派活 → 转码 → 落 key → 状态),
// 不是"ffmpeg 能不能转这个格式"(那是 internal/pkg/media 的事)。
//
// ⚠️ **不依赖本机装没装 ffmpeg** —— media.DefaultRunner 换成假的。
// 真 ffmpeg 的集成测试在 internal/pkg/media/media_integration_test.go。
//
// ⚠️ **转码是异步的** —— 所以这里的 DB 用"共享缓存 + 单连接"的 sqlite:
// 转码 goroutine 要写库,测试 goroutine 要读库,`:memory:`(不带 cache=shared)
// 下两个连接是**两个不同的库**,而多连接下共享缓存模式会抛
// "database table is locked"(见 approval_engine_test.go 的同一段说明)。
package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/oss"
)

// ====================== 探测样本 ======================

// unplayableVideoProbeJSON 一个 MKV 容器 + MPEG-4 Part 2 编码的视频 ——
// **ffmpeg 能解,浏览器不能播**。
const unplayableVideoProbeJSON = `{
  "streams": [
    {"codec_type": "video", "codec_name": "mpeg4", "width": 640, "height": 480},
    {"codec_type": "audio", "codec_name": "mp3"}
  ],
  "format": {"duration": "5.000000", "format_name": "matroska"}
}`

// playableVideoProbeJSON 一个 MP4 + H.264 + AAC 的视频 —— 浏览器能直接播。
const playableVideoProbeJSON = `{
  "streams": [
    {"codec_type": "video", "codec_name": "h264", "width": 640, "height": 480},
    {"codec_type": "audio", "codec_name": "aac"}
  ],
  "format": {"duration": "5.000000", "format_name": "mov,mp4,m4a,3gp,3g2,mj2"}
}`

// unplayableAudioProbeJSON 一个 WMA 音频 —— 浏览器没有解码器。
const unplayableAudioProbeJSON = `{
  "streams": [
    {"codec_type": "audio", "codec_name": "wmav2"}
  ],
  "format": {"duration": "7.000000", "format_name": "asf"}
}`

// ====================== 假 ffmpeg / ffprobe ======================

// transcodeRunner 假的 ffprobe / ffmpeg,能挡住转码那一步。
type transcodeRunner struct {
	// probeOut ffprobe 的 JSON 输出
	probeOut []byte
	// ffmpegErr 抽帧/转码直接失败
	ffmpegErr error
	// started 转码开始时被关掉(nil = 不记)
	started chan struct{}
	// release 放行转码(nil = 不挡)
	release chan struct{}
	once    sync.Once
}

// Run 实现 media.Runner
func (f *transcodeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if strings.Contains(name, "ffprobe") {
		return f.probeOut, nil
	}
	// ⚠️ **只有转码才挡** —— 抽帧的命令是
	// `-frames:v 1 -vf scale=640:-2`,转码的命令里有 `-c:v` / `-c:a`。
	// 两个都挡的话,测试会卡在抽帧那一步,压根到不了转码。
	if isTranscodeArgs(args) {
		if f.started != nil {
			f.once.Do(func() { close(f.started) })
		}
		if f.release != nil {
			select {
			case <-f.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	if f.ffmpegErr != nil {
		return nil, f.ffmpegErr
	}
	// ⚠️ **最后一个参数是输出路径** —— 真 ffmpeg 会把产物写在那里,
	// 而上传那侧要打开它、判大小、传给存储。假执行器不写文件的话,
	// 后面那几步会一路失败,测的就不是接线了。
	if len(args) > 0 {
		out := args[len(args)-1]
		if err := os.WriteFile(out, []byte("fake-media"), 0o644); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// isTranscodeArgs 这条命令是不是转码
func isTranscodeArgs(args []string) bool {
	for _, a := range args {
		if a == "-c:v" || a == "-c:a" {
			return true
		}
	}
	return false
}

// ====================== 夹具 ======================

// newTranscodeTestDB 共享缓存 + 单连接的内存库。
//
// ⚠️ **必须单连接** —— 转码 goroutine 和测试 goroutine 同时用库,
// 多连接下共享缓存模式会抛 "database table is locked"。
func newTranscodeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		// ⚠️ **静音 SQL 日志** —— 等转码状态的那段轮询会打出成堆
		//   "record not found",真正的失败反而被淹掉。
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysFolder{},
		&model.SysFile{},
		&model.SysUploadSession{},
	))
	return db
}

// transcodeFileOpts 装好媒体工具 + 开着转码的 FileOptions
func transcodeFileOpts(t *testing.T) FileOptions {
	t.Helper()
	_, opt := newFileTestProv(t)
	opt.FFmpegPath = "fake-ffmpeg"
	opt.FFprobePath = "fake-ffprobe"
	opt.MediaTmpDir = t.TempDir()
	opt.TranscodeWorkers = 1
	return opt
}

// waitTranscodeStatus 等转码状态到某个值。
func waitTranscodeStatus(t *testing.T, db *gorm.DB, fileID uint64, want model.TranscodeStatus) model.SysFile {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var row model.SysFile
	for time.Now().Before(deadline) {
		require.NoError(t, db.First(&row, fileID).Error)
		if row.TranscodeStatus == want {
			return row
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("转码状态没到 %d,现在是 %d(playback_key=%q)", want, row.TranscodeStatus, row.PlaybackKey)
	return row
}

// ====================== 用例 ======================

// 特征:**浏览器播不了的视频会被转一份能播的** ——
// 而且原文件保留、播放副本真的在存储里。
//
// ⚠️ **断言"播放副本对象真的在"** —— 只断言 playback_key 非空的话,
// "key 写了但对象没传上去"这种最典型的半成品会绿过去。
func TestReceive_UnplayableVideo_ProducesPlaybackCopy(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &transcodeRunner{probeOut: []byte(unplayableVideoProbeJSON)})

	content := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "movie.mkv", ContentType: "video/x-matroska",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	// ⚠️ **原始编码和"为什么播不了"要落库** —— 用户会问"为什么我的视频播不了",
	//   而前端不该自己拿编码去拼这句话(前端多一份判据表 = 两处会漂移)。
	var row model.SysFile
	require.NoError(t, db.First(&row, v.ID).Error)
	require.Equal(t, "mkv", row.MediaContainer)
	require.Equal(t, "mpeg4", row.MediaVideoCodec)
	require.Equal(t, "mp3", row.MediaAudioCodec)
	require.NotEmpty(t, row.MediaReason, "要告诉用户为什么播不了")

	row = waitTranscodeStatus(t, db, v.ID, model.TranscodeStatusDone)
	require.NotEmpty(t, row.PlaybackKey, "转码完成要有播放副本的 key")

	// ⚠️ **播放副本对象必须真的在存储里** —— key 有值但对象没传上去,
	//   前端就是"点开是 404"。
	_, err = p.Stat(context.Background(), row.PlaybackKey)
	require.NoError(t, err, "播放副本必须真的传上去了")

	// ⚠️ **原文件必须还在** —— 转码是**追加**一份,不是替换。
	//   用户下载到的还是他传上来的那个文件。
	_, err = p.Stat(context.Background(), row.FileKey)
	require.NoError(t, err, "原文件不能被转码顶掉")

	// ⚠️ **播放副本不进文件列表** —— 它不是用户文件(和封面一个道理):
	//   不进 sys_file、不计文件数、不占配额。
	var count int64
	require.NoError(t, db.Model(&model.SysFile{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "播放副本不该多出一行")

	// ⚠️ **列表/详情里要带可播副本的直链** —— 前端据此决定播哪一份。
	page, err := svc.List(context.Background(), mkFileCaller(uid), FileListQuery{Biz: "asset"})
	require.NoError(t, err)
	items, ok := page.List.([]FileView)
	require.True(t, ok, "列表元素类型必须是 []FileView")
	require.Len(t, items, 1)
	require.NotEmpty(t, items[0].PlaybackURL, "列表里要能直接拿到可播副本的直链")
	require.EqualValues(t, model.TranscodeStatusDone, items[0].TranscodeStatus)
}

// 特征:**浏览器本来就能播的视频不转码** —— 不白烧 CPU、不白占存储。
func TestReceive_PlayableVideo_NoTranscode(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &transcodeRunner{probeOut: []byte(playableVideoProbeJSON)})

	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "movie.mp4", ContentType: "video/mp4",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	var row model.SysFile
	require.NoError(t, db.First(&row, v.ID).Error)
	require.EqualValues(t, model.TranscodeStatusNone, row.TranscodeStatus)
	require.Empty(t, row.PlaybackKey)
	require.Empty(t, row.MediaReason, "能播的文件不该有播不了的原因")
	require.Equal(t, "mp4", row.MediaContainer)
	require.Equal(t, "h264", row.MediaVideoCodec)
	require.Equal(t, "aac", row.MediaAudioCodec)
}

// 特征:**浏览器播不了的音频也转** —— 而且产物是 .m4a。
//
// ⚠️ **音频和视频的产物后缀不一样** —— 视频 .mp4、音频 .m4a(都在 mp4 容器里,
// 但后缀不同,浏览器和播放器才认得出这是纯音频)。
func TestReceive_UnplayableAudio_ProducesM4A(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &transcodeRunner{probeOut: []byte(unplayableAudioProbeJSON)})

	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "song.wma", ContentType: "audio/x-ms-wma",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	row := waitTranscodeStatus(t, db, v.ID, model.TranscodeStatusDone)
	require.NotEmpty(t, row.PlaybackKey)
	require.True(t, strings.HasSuffix(row.PlaybackKey, ".m4a"),
		"音频产物后缀必须是 .m4a,实际: %s", row.PlaybackKey)
}

// 特征:**转码失败不影响文件本身** —— 原文件照样在,状态是"失败"。
//
// ⚠️ 这是整个功能最重要的一条边界:转码是锦上添花,
// ffmpeg 解不开某个文件时,用户至少还能把原文件下载走。
func TestReceive_TranscodeFailure_FileStillUploaded(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &transcodeRunner{
		probeOut:  []byte(unplayableVideoProbeJSON),
		ffmpegErr: errors.New("转码炸了"),
	})

	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "movie.mkv", ContentType: "video/x-matroska",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err, "转码失败不能让上传失败")

	row := waitTranscodeStatus(t, db, v.ID, model.TranscodeStatusFailed)
	require.Empty(t, row.PlaybackKey, "失败不该有播放副本")

	_, err = p.Stat(context.Background(), row.FileKey)
	require.NoError(t, err, "原文件必须还在")
}

// 特征:**转码功能关掉时,不转码,但"为什么播不了"照样记着**。
//
// ⚠️ 关掉转码 ≠ 假装没事发生 —— 用户还是需要知道"这个文件浏览器播不了"。
func TestReceive_TranscodeDisabled_KeepsReason(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	opt.TranscodeWorkers = 0 // 关
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &transcodeRunner{probeOut: []byte(unplayableVideoProbeJSON)})

	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "movie.mkv", ContentType: "video/x-matroska",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	var row model.SysFile
	require.NoError(t, db.First(&row, v.ID).Error)
	require.EqualValues(t, model.TranscodeStatusNone, row.TranscodeStatus)
	require.Empty(t, row.PlaybackKey)
	require.NotEmpty(t, row.MediaReason, "关了转码也要告诉用户为什么播不了")
}

// 特征:**抢单是乐观锁** —— 同一个文件只能被一个 worker 转一次。
//
// ⚠️ 不加这道锁的话,两个 worker 同时看到 status=pending 就会转两遍:
// 白烧 CPU,而且存储里会多出一份(后一个覆盖前一个,倒还不至于,
// 但 CPU 是实打实烧了两遍)。
func TestClaimTranscode_OnlyOneWorkerWins(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	p, _ := newFileTestProv(t)
	fs := NewFileService(db, p, opt).(*fileService)

	content := []byte{1, 2, 3, 4}
	v, err := fs.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "movie.mkv", ContentType: "video/x-matroska",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	// 把状态拨回 pending,模拟"刚派活还没人接"
	require.NoError(t, db.Model(&model.SysFile{}).Where("id = ?", v.ID).
		Update("transcode_status", model.TranscodeStatusPending).Error)

	require.True(t, fs.claimTranscode(context.Background(), v.ID), "第一个 worker 要抢到")
	require.False(t, fs.claimTranscode(context.Background(), v.ID), "第二个 worker 必须抢不到")

	var row model.SysFile
	require.NoError(t, db.First(&row, v.ID).Error)
	require.EqualValues(t, model.TranscodeStatusRunning, row.TranscodeStatus)
}

// 特征:**转码进行中,本地副本不能被上传流程删掉**。
//
// ⚠️ **这是最容易踩的坑** —— 上传流程那侧也有"用完就删副本"的逻辑,
// 而 worker 还要读它。两边都删的话,worker 拿到的是"文件不存在",
// ffmpeg 报 exit status 1,而真因是"文件被人删了"。
func TestReceive_TranscodeKeepsTempCopyWhileRunning(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	runner := &transcodeRunner{
		probeOut: []byte(unplayableVideoProbeJSON),
		started:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	swapMediaRunner(t, runner)

	content := []byte{1, 2, 3, 4}
	_, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "movie.mkv", ContentType: "video/x-matroska",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	// 等转码真的开始(这时候副本正被 worker 用着)
	select {
	case <-runner.started:
	case <-time.After(15 * time.Second):
		t.Fatal("转码没开始")
	}

	// ⚠️ **副本必须还在** —— 上传流程已经返回了,但它不能把副本删掉。
	require.NotEmpty(t, findMediaTempCopies(t, opt.MediaTmpDir),
		"转码期间本地副本必须还在(上传流程不能删它)")

	// 放行,让转码跑完
	close(runner.release)

	// 跑完之后副本必须被删掉 —— 临时盘不能攒垃圾
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if len(findMediaTempCopies(t, opt.MediaTmpDir)) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("转码结束后本地副本必须被删掉")
}

// findMediaTempCopies 找媒体临时副本目录
func findMediaTempCopies(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), mediaTempDirPrefix) {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	return out
}

// 特征:**删文件要连播放副本一起删** —— 它和封面一样不是 sys_file 里的行,
// 删主文件不会级联到它。
func TestDelete_RemovesPlaybackObject(t *testing.T) {
	db := newTranscodeTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := transcodeFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &transcodeRunner{probeOut: []byte(unplayableVideoProbeJSON)})

	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "movie.mkv", ContentType: "video/x-matroska",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	row := waitTranscodeStatus(t, db, v.ID, model.TranscodeStatusDone)
	require.NotEmpty(t, row.PlaybackKey)

	require.NoError(t, svc.Delete(context.Background(), mkFileCaller(uid), v.ID))

	_, err = p.Stat(context.Background(), row.PlaybackKey)
	require.Error(t, err, "播放副本必须被删掉,不能变成孤儿")
	require.True(t, oss.IsNotFound(err), "应该是对象不存在,实际: %v", err)
}
