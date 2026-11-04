// 媒体探测接线 —— 视频时长 + 封面帧(M141)
//
// # 这一层负责什么
//
// `internal/pkg/media` 只管跑 ffprobe/ffmpeg、解析输出。这一层管**什么时候跑、
// 结果存哪、失败了怎么办**:
//
//  1. 判断这个文件是不是音视频(扩展名优先,理由见 media.Classify);
//  2. 把字节**落到本地一份** —— ffmpeg 只能读本地文件;
//  3. 跑 ffprobe 拿时长;
//  4. 视频的话再抽一帧,传回存储,拿一个 key;
//  5. 把时长和封面 key 写回 sys_file。
//
// # ⚠️ **探测失败绝不影响上传**
//
// 用户 2026-10-31 的原话是「上传文件时候如果是视频文件,都调用 ffmpeg 提取
// 视频时长,截取封面」—— 这是**锦上添花**,不是上传的前置条件。
// 没装 ffmpeg、文件解不开、ffmpeg 超时,任何一种都只影响"列表里少一个时长",
// 不影响"文件传上来了"。
//
// ⇒ 所以这里的每个错误都只记日志,**没有一个会往上抛**。
//
// # ⚠️ **本地副本是有代价的**
//
// 探测要求文件完整落一次本地磁盘 —— 一个 2GB 的视频就是 2GB 的临时文件。
// 所以有 `file.media_max_size_mb` 这道闸:超过就不探测。
// 副本用完立刻删(见 mediaTemp.Cleanup)。
//
// # ⚠️ **封面帧不是用户文件**
//
// 它不进 sys_file、不计文件数、不占用户配额(见 model.SysFile.CoverKey 的注释)。
// 它只是"存储里的一个对象 + 库里一个 key"。
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/media"
	"rapid-admin/internal/pkg/oss"
)

// coverObjectPrefix 封面对象名的前缀
//
// ⚠️ **必须和用户文件的对象名区分开** —— 用户文件的对象名是
// `{时间戳}-{8位随机}-{清洗后的原名}`(见 oss.NewObjectName),
// 而这里用的是 `__cover__{文件id}.jpg`。用户文件名里不可能出现
// `__cover__` 开头的东西(清洗规则会把下划线保留,但要求整段以它开头
// 且后面跟纯数字 + .jpg —— 撞上的概率为零,而且真撞了也只是封面被覆盖,
// 不会破坏文件本体)。
const coverObjectPrefix = "__cover__"

// mediaTempDirPrefix 媒体临时副本的目录名前缀
//
// ⚠️ **cleanupMediaTemp 靠它确认"这个目录是我们建的"** —— 路径来自数据库,
// 而数据库里的东西理论上可被篡改。目录名对不上就不删,免得 RemoveAll
// 误删用户目录下的正经文件夹。
const mediaTempDirPrefix = "ha-media-"

// mediaProbeTimeout 一次媒体探测的时间预算
//
// ⚠️ **必须有上界** —— ffprobe 探一个坏文件可能卡住(网络盘上的文件、
// 索引损坏的大文件),没有超时就是一个能把上传请求挂死的线程。
// 60 秒对"本地磁盘上的普通视频"绰绰有余。
const mediaProbeTimeout = 60 * time.Second

// mediaTemp 媒体探测用的本地副本
type mediaTemp struct {
	// path 本地文件路径
	path string
	// dir 临时目录(删文件时连目录一起删)
	dir string
	// f 打开的文件(分片那条路要按偏移写)
	f *os.File
	// limit 字节上限。0 = 不限
	limit int64
	// written 已写字节
	written int64
	// overflow 超过上限了(内容不完整,不能拿去探测)
	overflow bool
	// invalid 内容不完整/对不上(分片那条路:某一片长度不对)
	invalid bool
}

// newMediaTemp 建一个临时副本文件。
//
// ⚠️ **扩展名要跟原文件一致** —— ffmpeg 靠它选解码器,
// 而"名字对不上"的后果是 ffprobe 直接报"unknown format"。
func newMediaTemp(dir, fileName string, limit int64) (*mediaTemp, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	d, err := os.MkdirTemp(dir, mediaTempDirPrefix)
	if err != nil {
		return nil, err
	}
	ext := filepath.Ext(fileName)
	if len(ext) > 16 {
		ext = ""
	}
	p := filepath.Join(d, "media"+ext)
	f, err := os.Create(p)
	if err != nil {
		_ = os.RemoveAll(d)
		return nil, err
	}
	return &mediaTemp{path: p, dir: d, f: f, limit: limit}, nil
}

// Write 实现 io.Writer —— 给 io.TeeReader 用。
//
// ⚠️ **超限之后"吞掉"而不是报错** —— 上传本身跟探测无关,
// 不能因为"这个文件太大不适合探测"就让上传失败。
func (t *mediaTemp) Write(p []byte) (int, error) {
	if t.invalid || t.overflow {
		return len(p), nil
	}
	if t.limit > 0 && t.written+int64(len(p)) > t.limit {
		t.overflow = true
		return len(p), nil
	}
	n, err := t.f.Write(p)
	t.written += int64(n)
	return n, err
}

// Close 关掉文件句柄(不删目录)。
//
// ⚠️ **可重复调用** —— 上传流程会在"字节落完"和"函数退出"两个地方各关一次。
//
// ⚠️ **nil 安全** —— 调用方手上可能压根没有副本(不是媒体文件)。
func (t *mediaTemp) Close() {
	if t == nil || t.f == nil {
		return
	}
	_ = t.f.Close()
	t.f = nil
}

// WriteAt 按偏移写(分片那条路用)。
//
// ⚠️ **偏移是 (partNumber-1) * partSize** —— 和存储侧合并分片的偏移一致,
// 所以本地副本和最终对象是同一份字节。写错偏移不会报错,只会让探测出来的
// 时长是错的(用户看到"3 秒"而视频其实 30 秒),所以偏移只能由 partSize 算。
func (t *mediaTemp) WriteAt(p []byte, off int64) (int, error) {
	if t.invalid || t.overflow {
		return len(p), nil
	}
	if t.limit > 0 && off+int64(len(p)) > t.limit {
		t.overflow = true
		return len(p), nil
	}
	n, err := t.f.WriteAt(p, off)
	return n, err
}

// Size 当前文件大小(字节)
func (t *mediaTemp) Size() int64 {
	if t == nil || t.f == nil {
		return 0
	}
	st, err := t.f.Stat()
	if err != nil {
		return 0
	}
	return st.Size()
}

// Cleanup 删掉临时副本(文件 + 目录)
func (t *mediaTemp) Cleanup() {
	if t == nil {
		return
	}
	t.Close()
	if t.dir != "" {
		_ = os.RemoveAll(t.dir)
	}
}

// Usable 这份副本能不能拿去探测
func (t *mediaTemp) Usable() bool {
	return t != nil && !t.invalid && !t.overflow && t.written > 0
}

// ============== 临时副本的跨请求操作 ==============

// writeMediaPart 把一个分片写进本地副本的指定偏移(分片上传那条路)。
//
// ⚠️ **用 O_WRONLY 而不是 O_CREATE** —— 副本是 Init 时建好的,这里打不开
// 说明它没了(机器重启、临时盘被清、别的副本合并过了)。这时候**不要重建**:
// 重建出来的是一份中间全是 0 的假文件,探出来的时长是编的,比没有还糟。
// 返回错误让上层记日志跳过探测,上传本身不受影响。
func writeMediaPart(path string, body []byte, offset int64) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("没有媒体临时副本")
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(body, offset)
	return err
}

// mediaTempUsable 这份本地副本能不能拿去探测。
//
// ⚠️ **大小必须对得上** —— 副本是"尽力而为"攒出来的:超了上限会被截断,
// 分片没传全会留缺口。这两种情况下文件都**存在**、**非空**,
// ffprobe 也能跑出一个数 —— 但那个数是假的(时长偏短,抽出来的帧是黑的)。
// 所以这里比大小,对不上就整个跳过。
func mediaTempUsable(path string, expectSize int64) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	if st.Size() == 0 {
		return false
	}
	if expectSize > 0 && st.Size() != expectSize {
		return false
	}
	return true
}

// cleanupMediaTemp 删掉媒体临时副本(连目录一起)。
//
// ⚠️ **只删"我们自己建的那种目录"** —— 路径来自数据库,而数据库里的路径
// 理论上可被篡改。目录名前缀对不上就不动,免得误删别的目录。
func cleanupMediaTemp(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	dir := filepath.Dir(path)
	if !strings.HasPrefix(filepath.Base(dir), mediaTempDirPrefix) {
		logger.L().Warn("媒体临时副本路径异常,未清理", zap.String("path", path))
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		logger.L().Warn("清理媒体临时副本失败", zap.String("path", dir), zap.Error(err))
	}
}

// mediaProbeEligible 这个文件要不要探测。
//
// ⚠️ **大小闸在这里** —— 探测要把文件完整落到本地磁盘,
// "能传多大的文件"直接等于"临时盘要留多少空间"。
func (s *fileService) mediaProbeEligible(fileName, contentType string, size int64) bool {
	if s.opt.FFmpegPath == "" && s.opt.FFprobePath == "" {
		return false
	}
	if media.Classify(fileName, contentType) == media.KindUnknown {
		return false
	}
	if size > 0 && s.opt.MediaMaxSize > 0 && size > s.opt.MediaMaxSize {
		return false
	}
	return true
}

// mediaProbeResult 一次探测的结果。
//
// ⚠️ **所有字段都可能"没有"** —— 时长 0 = 探测不到;
// 封面 key 空 = 没有封面(音频本来就没有,或者抽帧失败);
// Reason 空 = 浏览器能直接播(不需要转码)。
type mediaProbeResult struct {
	DurationMs int64
	CoverKey   string
	Container  string
	VideoCodec string
	AudioCodec string
	// Kind 视频还是音频(转码参数和产物后缀靠它)
	Kind media.Kind
	// Reason 为什么浏览器播不了(一句人话)。空 = 能播。
	Reason string
}

// probeMedia 探测媒体:返回时长、封面 key、原始编码、以及"浏览器能不能播"。
//
// ⚠️ **两个返回值都可能"没有"** —— 时长 0 = 探测不到;
// 封面 key 空 = 没有封面(音频本来就没有,或者抽帧失败)。
//
// ⚠️ **音频不抽帧** —— 音频的"封面"是图标(前端按扩展名画),
// 抽出来的波形图对一张唱片封面没有任何意义。
//
// ⚠️ **抽帧失败不影响时长** —— 时长已经拿到了,封面是附加品。
func (s *fileService) probeMedia(ctx context.Context, row *model.SysFile, localPath string) mediaProbeResult {
	if row == nil || localPath == "" {
		return mediaProbeResult{}
	}
	// ⚠️ **ffprobe 没配就整个不探** —— 不是"跑一次看看报什么错"。
	//   只配了 ffmpeg 没配 ffprobe 是常见形态(精简包),那时错误信息是
	//   "exec: no command",看的人完全猜不到是配置缺了。
	if s.opt.FFprobePath == "" {
		return mediaProbeResult{}
	}
	cctx, cancel := context.WithTimeout(ctx, mediaProbeTimeout)
	defer cancel()

	info, err := media.Probe(cctx, s.opt.FFprobePath, localPath)
	if err != nil {
		if errors.Is(err, media.ErrUnavailable) {
			// ⚠️ **"没装 ffmpeg"不是错误** —— 是部署状态。
			//   记 Info 而不是 Warn:每个媒体文件上传都会打一条 Warn,
			//   而用户看不出任何区别(前端就是少个时长)—— 那是噪音。
			logger.L().Info("file: 跳过媒体探测(ffprobe 不可用)",
				zap.String("ffprobe", s.opt.FFprobePath),
				zap.String("file_key", row.FileKey))
			return mediaProbeResult{}
		}
		logger.L().Warn("file: 媒体探测失败(不影响上传)",
			zap.String("file_key", row.FileKey), zap.Error(err))
		return mediaProbeResult{}
	}
	res := mediaProbeResult{
		DurationMs: info.DurationMs,
		Container:  info.Container,
		VideoCodec: info.VideoCodec,
		AudioCodec: info.AudioCodec,
		Kind:       info.Kind,
		// ⚠️ **判据只有一处** —— pkg/media.BrowserPlayable。
		//   service 层自己再判一遍的话,两处判据迟早漂移,
		//   而"前端说能播、后端转了码"这种不一致极难查。
		Reason: media.UnplayableReason(info),
	}
	if info.Kind != media.KindVideo {
		// 音频:只要时长
		return res
	}

	coverKey, err := s.extractVideoCover(cctx, row, localPath, res.DurationMs)
	if err != nil {
		// ⚠️ **封面失败不影响时长** —— 已经在上面拿到了。
		logger.L().Warn("file: 视频封面抽取失败(不影响上传)",
			zap.String("file_key", row.FileKey), zap.Error(err))
		return res
	}
	res.CoverKey = coverKey
	return res
}

// extractVideoCover 抽一帧并传回存储,返回封面 key。
func (s *fileService) extractVideoCover(ctx context.Context, row *model.SysFile, localPath string, durationMs int64) (string, error) {
	if s.prov == nil {
		return "", errcode.New(errcode.Internal, "存储未初始化")
	}
	// ⚠️ **ffmpeg 没配就不抽帧** —— 只配了 ffprobe 的部署拿得到时长但拿不到
	//   封面,这是"能显示什么"的分级,不是一个故障。
	if s.opt.FFmpegPath == "" {
		return "", errors.New("ffmpeg 未配置,跳过抽帧")
	}
	prefix, err := oss.FolderPrefix(s.opt.Root, row.UserID, row.Biz, row.FolderID)
	if err != nil {
		return "", err
	}
	coverKey := prefix + coverObjectPrefix + strconv.FormatUint(row.ID, 10) + ".jpg"

	// 临时 jpg —— 抽完就删
	tmpDir, err := os.MkdirTemp(s.opt.MediaTmpDir, "ha-cover-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	outPath := filepath.Join(tmpDir, "cover.jpg")

	at := media.FramePosition(durationMs)
	if err := media.ExtractFrame(ctx, s.opt.FFmpegPath, localPath, outPath, at); err != nil {
		// ⚠️ **落点抽不到就退回第一帧** —— 有些视频在声明的时长之前
		//   就有问题(moov 缺失、时间戳乱),按中点抽会失败,
		//   而第 0 帧通常还在。
		if at == 0 {
			return "", err
		}
		if err2 := media.ExtractFrame(ctx, s.opt.FFmpegPath, localPath, outPath, 0); err2 != nil {
			return "", fmt.Errorf("抽帧失败(落点 %dms): %w;退回第 0 帧也失败: %v", at, err, err2)
		}
	}
	f, err := os.Open(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if st.Size() == 0 {
		return "", errors.New("抽出来的封面是空文件")
	}
	if _, err := s.prov.Put(ctx, coverKey, f, "image/jpeg"); err != nil {
		return "", err
	}
	return coverKey, nil
}

// probeStoredMedia 从存储里读一份到本地再探测(直传登记那条路)。
//
// ⚠️ **只有实现了 oss.ContentProvider 的 provider 能走这条路** —— aliyun
// 刻意不实现它(见 oss.ContentProvider 的注释:"中转大文件与直传的意义
// 相反")。所以**用 OSS 直传登记的视频没有时长和封面** —— 这是有意的取舍:
// 为了一个列表缩略图把整个视频从 OSS 拉回应用服务器,带宽代价远大于收益。
//
// ⚠️ **全程不返错** —— 同 probeAndApply:探测失败只影响"少一个时长"。
func (s *fileService) probeStoredMedia(ctx context.Context, row *model.SysFile, key string) {
	if row == nil || key == "" || s.prov == nil {
		return
	}
	cp, ok := s.prov.(oss.ContentProvider)
	if !ok {
		return
	}
	if !s.mediaProbeEligible(row.FileName, row.ContentType, row.FileSize) {
		return
	}
	rc, _, err := cp.Open(ctx, key)
	if err != nil {
		logger.L().Warn("file: 读取存储对象失败,跳过媒体探测(不影响上传)",
			zap.String("file_key", key), zap.Error(err))
		return
	}
	defer rc.Close()

	tmp, err := newMediaTemp(s.opt.MediaTmpDir, row.FileName, s.opt.MediaMaxSize)
	if err != nil {
		logger.L().Warn("file: 建媒体临时副本失败(不影响上传)",
			zap.String("file_key", key), zap.Error(err))
		return
	}
	// ⚠️ **副本的清理权可能被交给转码 worker**(M145)——
	//   交出去了就不能自己删,否则 worker 转码时拿到的是"文件不存在",
	//   ffmpeg 报 exit status 1,而真因是"文件被人删了"。
	cleanup := true
	defer func() {
		if cleanup {
			tmp.Cleanup()
		}
	}()

	// ⚠️ **超限的部分被 mediaTemp.Write 吞掉** —— 那是故意的:
	//   副本不完整就跳过探测(mediaTempUsable 会发现大小对不上),
	//   而把整个大文件拉下来只为算一个时长不划算。
	if _, err := io.Copy(tmp, rc); err != nil {
		logger.L().Warn("file: 复制存储对象到临时文件失败,跳过媒体探测(不影响上传)",
			zap.String("file_key", key), zap.Error(err))
		return
	}
	// ⚠️ **字节拷完就关句柄** —— 副本是给 ffprobe/ffmpeg 按路径读的,
	//   它们自己开自己的句柄,我们的句柄留着没用。
	//
	//   而"句柄开着"有个具体的坏后果:worker 转码完要 os.RemoveAll 这个目录,
	//   Windows 上文件还开着句柄 ⇒ RemoveAll 直接失败
	//   ("The process cannot access the file because it is being used by
	//   another process")⇒ 临时目录永远删不掉;Linux 上虽然能删,
	//   句柄却泄漏了(每次上传泄一个 fd)。
	tmp.Close()
	if s.probeAndApply(ctx, row, tmp.path, row.FileSize) {
		cleanup = false
	}
}

// probeAndApply 探测本地副本,并把结果写回 sys_file。
//
// ⚠️ **这是唯一对外的探测入口** —— 单次中转(Receive)、直传登记(Register)、
// 分片合并(Complete)三条路都走它,免得每处各写一遍"探测 + 写回"。
//
// ⚠️ **全程不返错** —— 探测是锦上添花,任何一步失败都只记日志(见文件头)。
//
// ⚠️ **expectSize 是"副本必须和它一样大"的意思** —— 副本是尽力而为攒出来的
// (超上限被截断 / 分片没传全),大小对不上就不能拿去探测(见 mediaTempUsable)。
//
// ⚠️ **返回值是"副本的清理权归谁"** —— true = 已经派了转码任务,
// **本地副本交给转码 worker 收了**,调用方不要再删它;
// false = 调用方自己删(见 mediaTemp.Cleanup 的调用点)。
//
// 弄反的后果很具体:调用方先删了副本,worker 转码时拿到的是
// "文件不存在",转码永远失败,而日志里只有一句 ffmpeg 的 exit status 1。
func (s *fileService) probeAndApply(ctx context.Context, row *model.SysFile, localPath string, expectSize int64) bool {
	if row == nil || localPath == "" {
		return false
	}
	if !mediaTempUsable(localPath, expectSize) {
		logger.L().Info("file: 跳过媒体探测(本地副本不可用)",
			zap.String("file_key", row.FileKey),
			zap.String("local_path", localPath),
			zap.Int64("expect_size", expectSize))
		return false
	}
	res := s.probeMedia(ctx, row, localPath)
	s.applyMediaResult(ctx, row.ID, res)
	// ⚠️ **只有"判出来浏览器播不了"才派转码** —— 能播的文件不产生副本。
	//   而且转码是**异步**的:这里只是把任务丢进队列,上传请求不等它。
	if res.Reason == "" {
		return false
	}
	return s.enqueueTranscode(ctx, row, res, localPath)
}

// applyMediaResult 把探测结果写回 sys_file。
//
// ⚠️ **写回失败只记日志** —— 文件已经传上去了,
// 时长/封面写不进去只是列表里少显示一个时长。
func (s *fileService) applyMediaResult(ctx context.Context, fileID uint64, res mediaProbeResult) {
	if fileID == 0 {
		return
	}
	updates := map[string]any{}
	if res.DurationMs > 0 {
		updates["duration_ms"] = res.DurationMs
	}
	if res.CoverKey != "" {
		updates["cover_key"] = res.CoverKey
	}
	if res.Container != "" {
		updates["media_container"] = res.Container
	}
	if res.VideoCodec != "" {
		updates["media_video_codec"] = res.VideoCodec
	}
	if res.AudioCodec != "" {
		updates["media_audio_codec"] = res.AudioCodec
	}
	if res.Reason != "" {
		updates["media_reason"] = res.Reason
	}
	if len(updates) == 0 {
		return
	}
	if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("id = ?", fileID).Updates(updates).Error; err != nil {
		logger.L().Warn("file: 写回媒体信息失败",
			zap.Uint64("file_id", fileID), zap.Error(err))
	}
}

// deleteCoverObject 删掉封面对象(文件被删时)。
//
// ⚠️ **删失败只记日志** —— 主文件已经删了,
// 留一个孤儿封面只是浪费一点存储,不该让"删除文件"报失败。
func (s *fileService) deleteCoverObject(ctx context.Context, coverKey string) {
	if coverKey == "" || s.prov == nil {
		return
	}
	if err := s.prov.Delete(ctx, coverKey); err != nil {
		logger.L().Warn("file: 删除视频封面对象失败",
			zap.String("cover_key", coverKey), zap.Error(err))
	}
}
