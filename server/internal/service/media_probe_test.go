// M141 媒体探测 —— service 层接线测试
//
// 覆盖的是**接线**(什么时候探、结果存哪、失败了会怎样),不是"ffmpeg 能不能
// 解这个格式"(那是 internal/pkg/media 的事)。
//
// ⚠️ **不依赖本机装没装 ffmpeg** —— media.DefaultRunner 是一个包级变量,
// 这里换成假的。这样这条用例在任何机器上都能跑(开发机普遍没装 ffmpeg),
// 而"接线对不对"本来就跟装没装无关。
//
// 真 ffmpeg 的集成测试在 internal/pkg/media/media_integration_test.go,
// 那里找不到 ffmpeg 会显式 skip。
package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/media"
	"rapid-admin/internal/pkg/oss"
)

// ====================== 假 ffprobe / ffmpeg ======================

// fakeMediaRunner 假的 ffprobe / ffmpeg
type fakeMediaRunner struct {
	// probeOut ffprobe 的 JSON 输出
	probeOut []byte
	// probeErr ffprobe 直接失败
	probeErr error
	// ffmpegCalls 抽帧命令跑了几次
	ffmpegCalls int
	// ffmpegErr 抽帧失败
	ffmpegErr error
}

// Run 实现 media.Runner
func (f *fakeMediaRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	// ⚠️ **先判 ffprobe** —— "fake-ffprobe" 里没有 "ffmpeg",但反过来
	// "fake-ffmpeg" 里也没有 "ffprobe",顺序其实无所谓;写清楚是为了
	// 后来人改名字时不会静默走错分支。
	if strings.Contains(name, "ffprobe") {
		if f.probeErr != nil {
			return nil, f.probeErr
		}
		return f.probeOut, nil
	}
	f.ffmpegCalls++
	if f.ffmpegErr != nil {
		return nil, f.ffmpegErr
	}
	// ⚠️ **最后一个参数是输出路径** —— 真 ffmpeg 会把封面写在那里,
	//   而 extractVideoCover 随后要打开它、判大小、传给存储。
	//   假执行器不写文件的话,后面那几步会一路失败,测的就不是接线了。
	if len(args) > 0 {
		out := args[len(args)-1]
		if err := os.WriteFile(out, []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01}, 0o644); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// swapMediaRunner 换掉 media 包的默认执行器,测试结束自动还原。
//
// ⚠️ **本包没有 t.Parallel()**(见 auth_test.go 的说明),顺序执行,
// 换全局变量是安全的。哪天有人加了 t.Parallel,这里要改成 per-test 注入。
func swapMediaRunner(t *testing.T, r media.Runner) {
	t.Helper()
	old := media.DefaultRunner
	media.DefaultRunner = r
	t.Cleanup(func() { media.DefaultRunner = old })
}

// videoProbeJSON 一个 3 秒、320x240 的视频的 ffprobe 输出
const videoProbeJSON = `{"streams":[{"codec_type":"video","width":320,"height":240}],"format":{"duration":"3.000000"}}`

// audioProbeJSON 一个 12.5 秒的音频的 ffprobe 输出
const audioProbeJSON = `{"streams":[{"codec_type":"audio"}],"format":{"duration":"12.500000"}}`

// mediaFileOpts 装好媒体工具的 FileOptions
func mediaFileOpts(t *testing.T) FileOptions {
	t.Helper()
	_, opt := newFileTestProv(t)
	opt.FFmpegPath = "fake-ffmpeg"
	opt.FFprobePath = "fake-ffprobe"
	opt.MediaTmpDir = t.TempDir()
	// ⚠️ **转码关掉** —— 这一组用例只测"探测",不测"转码"。
	//   开着的话,哪天有人把探测样本改成"浏览器播不了"的格式,
	//   这里就会多跑一个后台转码 goroutine(用例会莫名其妙地时绿时红)。
	//   转码自己的用例在 media_transcode_test.go。
	opt.TranscodeWorkers = 0
	return opt
}

// ====================== 用例 ======================

// 特征:**探测是有闸的** —— 不是所有文件都去跑 ffmpeg。
//
// ⚠️ 这条闸的价值在"别把整个存储当媒体扫一遍":一个 5GB 的压缩包去探测,
// 意味着临时盘先要留 5GB。
func TestMediaProbeEligible(t *testing.T) {
	cases := []struct {
		name        string
		opt         FileOptions
		fileName    string
		contentType string
		size        int64
		want        bool
	}{
		{
			name: "没配工具就不探", opt: FileOptions{},
			fileName: "a.mp4", contentType: "video/mp4", size: 100, want: false,
		},
		{
			name:     "视频要探",
			opt:      FileOptions{FFmpegPath: "f", FFprobePath: "p"},
			fileName: "a.mp4", contentType: "video/mp4", size: 100, want: true,
		},
		{
			name:     "音频要探",
			opt:      FileOptions{FFmpegPath: "f", FFprobePath: "p"},
			fileName: "a.mp3", contentType: "audio/mpeg", size: 100, want: true,
		},
		{
			name:     "文本不探",
			opt:      FileOptions{FFmpegPath: "f", FFprobePath: "p"},
			fileName: "a.txt", contentType: "text/plain", size: 100, want: false,
		},
		{
			name:     "超上限不探",
			opt:      FileOptions{FFmpegPath: "f", FFprobePath: "p", MediaMaxSize: 10},
			fileName: "a.mp4", contentType: "video/mp4", size: 100, want: false,
		},
		{
			name:     "大小未知不拦(上传时可能没申报)",
			opt:      FileOptions{FFmpegPath: "f", FFprobePath: "p", MediaMaxSize: 10},
			fileName: "a.mp4", contentType: "video/mp4", size: 0, want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := &fileService{opt: c.opt.normalize()}
			got := fs.mediaProbeEligible(c.fileName, c.contentType, c.size)
			require.Equal(t, c.want, got)
		})
	}
}

// 特征:**视频上传完就有时长和封面**,而且封面是一个真实存在的对象。
//
// ⚠️ 断言的是"存的对象真在" —— 只断言 cover_key 非空的话,
// "key 写了但对象没传上去"这种最典型的半成品会绿过去。
func TestReceive_Video_SetsDurationAndCover(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	runner := &fakeMediaRunner{probeOut: []byte(videoProbeJSON)}
	swapMediaRunner(t, runner)

	caller := mkFileCaller(uid)
	content := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	v, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "movie.mp4", ContentType: "video/mp4",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)
	require.EqualValues(t, 3000, v.DurationMs, "3 秒视频应该探出 3000 毫秒")
	require.NotEmpty(t, v.CoverURL, "视频要有封面直链")

	var row model.SysFile
	require.NoError(t, db.First(&row, v.ID).Error)
	require.EqualValues(t, 3000, row.DurationMs)
	require.NotEmpty(t, row.CoverKey)

	// ⚠️ **封面对象必须真的在存储里** —— key 有值但对象没传上去,
	//   前端就是一片裂图。
	_, err = p.Stat(context.Background(), row.CoverKey)
	require.NoError(t, err, "封面对象必须真的传上去了")
}

// 特征:**音频只要时长,不要封面**。
//
// ⚠️ 音频的"封面"是图标(前端按扩展名画),抽出来的波形/垃圾帧
// 对一张唱片封面没有任何意义 —— 反而白占一份存储。
func TestReceive_Audio_DurationOnly_NoCover(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	runner := &fakeMediaRunner{probeOut: []byte(audioProbeJSON)}
	swapMediaRunner(t, runner)

	content := []byte{9, 8, 7, 6}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "song.mp3", ContentType: "audio/mpeg",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)
	require.EqualValues(t, 12500, v.DurationMs, "12.5 秒音频应该探出 12500 毫秒")
	require.Empty(t, v.CoverURL, "音频没有封面,前端摆音频图标")
	require.Equal(t, 0, runner.ffmpegCalls, "音频不该跑 ffmpeg 抽帧")

	var row model.SysFile
	require.NoError(t, db.First(&row, v.ID).Error)
	require.Empty(t, row.CoverKey)
}

// 特征:**探测失败绝不影响上传**。
//
// ⚠️ 这是整个功能最重要的一条边界:ffmpeg 不是 Go 依赖、不会随仓库分发,
// 开发机上大概率没有。用户上传的视频**必须照样传上去**,
// 只是列表里少一个时长而已。
func TestReceive_ProbeFailure_UploadStillSucceeds(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &fakeMediaRunner{probeErr: errors.New("moov atom not found")})

	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "broken.mp4", ContentType: "video/mp4",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err, "探测失败不能让上传失败")
	require.EqualValues(t, 0, v.DurationMs)
	require.NotZero(t, v.ID, "文件照样落库了")
	require.EqualValues(t, int64(len(content)), v.FileSize)

	// 文件本体必须真的在存储里(不能因为探测失败把字节也丢了)
	_, err = p.Stat(context.Background(), v.FileKey)
	require.NoError(t, err)
}

// 特征:**列表和详情都带封面直链和时长** —— 缩略图视图全靠这两个字段。
func TestListAndDetail_FillCoverURLAndDuration(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &fakeMediaRunner{probeOut: []byte(videoProbeJSON)})

	caller := mkFileCaller(uid)
	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "movie.mp4", ContentType: "video/mp4",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	page, err := svc.List(context.Background(), caller, FileListQuery{Biz: "asset"})
	require.NoError(t, err)
	items, ok := page.List.([]FileView)
	require.True(t, ok, "列表元素类型必须是 []FileView")
	require.Len(t, items, 1)
	item := items[0]
	require.NotEmpty(t, item.CoverURL, "列表页要能直接塞进 <img src>")
	require.EqualValues(t, 3000, item.DurationMs)

	d, err := svc.Detail(context.Background(), caller, v.ID)
	require.NoError(t, err)
	require.NotEmpty(t, d.CoverURL)
	require.EqualValues(t, 3000, d.DurationMs)
}

// 特征:**删文件要连封面对象一起删**。
//
// ⚠️ 封面**不是**用户文件(它不在 sys_file 里、不计配额),所以删主文件
// 不会级联到它。漏了这一步,封面就变成永远占着存储空间的孤儿对象,
// 而且没有任何东西会告诉你。
func TestDelete_RemovesCoverObject(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewFileService(db, p, opt)

	swapMediaRunner(t, &fakeMediaRunner{probeOut: []byte(videoProbeJSON)})

	caller := mkFileCaller(uid)
	content := []byte{1, 2, 3, 4}
	v, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: "movie.mp4", ContentType: "video/mp4",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)

	var row model.SysFile
	require.NoError(t, db.First(&row, v.ID).Error)
	require.NotEmpty(t, row.CoverKey)

	require.NoError(t, svc.Delete(context.Background(), caller, v.ID))

	_, err = p.Stat(context.Background(), row.CoverKey)
	require.Error(t, err, "封面对象必须被删掉,不能变成孤儿")
	require.True(t, oss.IsNotFound(err), "应该是对象不存在,实际: %v", err)
}

// ====================== 分片上传 ======================

// 特征:**分片上传也会探测** —— 而且用一份"边传边攒"的本地副本。
//
// ⚠️ 分片的字节**没有**经过服务端的一次性流,所以没有 tee 的机会:
// 必须自己按偏移把每一片写进本地副本。偏移错了不会报错,只会让
// 探出来的时长是错的 —— 所以这里断言副本内容逐字节等于原始内容。
func TestMultipart_Video_ProbesAndCleansTempCopy(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewUploadService(db, p, opt)

	swapMediaRunner(t, &fakeMediaRunner{probeOut: []byte(videoProbeJSON)})

	const partSize = int64(100 * 1024)
	const fileSize = partSize * 2
	full := make([]byte, fileSize)
	for i := range full {
		full[i] = byte(i % 251)
	}

	caller := mkFileCaller(uid)
	init, err := svc.Init(context.Background(), caller, MultipartInitReq{
		Biz: "asset", FileName: "movie.mp4", FileSize: fileSize,
		ContentType: "video/mp4", PartSize: partSize,
	})
	require.NoError(t, err)

	// ⚠️ **会话行里必须记着副本路径** —— 没有它,分片那条路就永远探不出来。
	var sess model.SysUploadSession
	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.NotEmpty(t, sess.MediaTmpPath, "会话要记着媒体副本的路径")
	require.FileExists(t, sess.MediaTmpPath)

	for i := 1; i <= 2; i++ {
		_, err := svc.UploadPart(context.Background(), caller, UploadPartReq{
			SessionID: init.SessionID, PartNumber: i,
			Body: full[int64(i-1)*init.PartSize : int64(i)*init.PartSize],
		})
		require.NoError(t, err, "part %d", i)
	}

	// ⚠️ **副本内容必须逐字节等于原始内容** —— 偏移算错的话这里会炸。
	tmpBytes, err := os.ReadFile(sess.MediaTmpPath)
	require.NoError(t, err)
	require.Equal(t, full, tmpBytes, "本地副本必须和原文件逐字节一致")

	v, err := svc.Complete(context.Background(), caller, CompleteReq{SessionID: init.SessionID})
	require.NoError(t, err)
	require.EqualValues(t, 3000, v.DurationMs, "分片合并后也要探出时长")
	require.NotEmpty(t, v.CoverURL)

	// ⚠️ **完成后副本要删掉** —— 留着就是纯占临时盘。
	_, err = os.Stat(sess.MediaTmpPath)
	require.True(t, os.IsNotExist(err), "完成后本地副本必须被删掉")

	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.Empty(t, sess.MediaTmpPath, "会话行里的副本路径要清空")
}

// 特征:**取消会话要把副本清掉** —— 否则临时盘上会攒一堆没人认领的半成品。
func TestMultipart_Cancel_CleansTempCopy(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewUploadService(db, p, opt)

	const fileSize = int64(100 * 1024)
	caller := mkFileCaller(uid)
	init, err := svc.Init(context.Background(), caller, MultipartInitReq{
		Biz: "asset", FileName: "movie.mp4", FileSize: fileSize,
		ContentType: "video/mp4", PartSize: fileSize,
	})
	require.NoError(t, err)

	var sess model.SysUploadSession
	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.NotEmpty(t, sess.MediaTmpPath)

	require.NoError(t, svc.Cancel(context.Background(), caller, init.SessionID))

	_, err = os.Stat(sess.MediaTmpPath)
	require.True(t, os.IsNotExist(err), "取消后本地副本必须被删掉")
	require.Equal(t, int64(0), usedBytes(t, db, uid), "取消要把预扣退回去")
}

// 特征:**不是媒体的文件不留副本** —— 建一个空临时文件只为"以防万一"
// 是纯浪费(每个非媒体上传都往临时盘塞一次 create+delete)。
func TestMultipart_NonMedia_NoTempCopy(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	opt := mediaFileOpts(t)
	p, _ := newFileTestProv(t)
	svc := NewUploadService(db, p, opt)

	const fileSize = int64(100 * 1024)
	init, err := svc.Init(context.Background(), mkFileCaller(uid), MultipartInitReq{
		Biz: "asset", FileName: "doc.pdf", FileSize: fileSize,
		ContentType: "application/pdf", PartSize: fileSize,
	})
	require.NoError(t, err)

	var sess model.SysUploadSession
	require.NoError(t, db.First(&sess, init.SessionID).Error)
	require.Empty(t, sess.MediaTmpPath, "不是媒体就不该建副本")
}

// 特征:**临时副本目录只删自己建的那种** —— 路径来自数据库,理论上可被篡改。
func TestCleanupMediaTemp_RefusesForeignPath(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "not-ours")
	require.NoError(t, os.MkdirAll(victim, 0o755))
	marker := filepath.Join(victim, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("x"), 0o644))

	cleanupMediaTemp(filepath.Join(victim, "media.mp4"))

	_, err := os.Stat(marker)
	require.NoError(t, err, "不是我们建的目录不能删")
}
