// 媒体转码 —— 把浏览器播不了的音视频转一份能播的(M145)
//
// # 这个文件解决什么
//
// 用户 2026-10-31 的原话:
// 「音视频文件的编码,你知道哪些是浏览器不支持的吗?如果不支持的音视频编码方式,
// 文件上传以后估计要重新给他转码,否则浏览器播放不了。」
//
// ⇒ 上传时探测出原始编码(见 media_probe.go),判据在 pkg/media.BrowserPlayable。
// 判出来"播不了"就派一个转码任务,转成 H.264 + AAC / MP4,
// 存成一个**额外**的对象(playback_key),**原文件保留不动**。
//
// # ⚠️ **转码是异步的,上传请求不等它**
//
// 一个 100MB 的视频转码要几十秒 —— 同步做的话 HTTP 请求会超时,
// 客户端会重试,然后上传两遍(两份字节、两次配额)。
//
// ⚠️ **请求的 ctx 不能传给 worker** —— 请求结束 ctx 就被取消了,
// 传过去的话转码刚开始就被杀掉(症状:状态永远停在"转码中",
// 而日志里只有一句 "context canceled")。
//
// # ⚠️ **并发必须有上界**
//
// 每个转码是一个 ffmpeg 进程,吃满一个核。20 个用户同时传 wmv
// 就是 20 个 ffmpeg 一起跑 —— 那台机器就废了。
// 所以有一个信号量(`transcodeSlots`),排队的是**任务**不是进程。
//
// # ⚠️ **本地副本的清理权在 worker 手里**
//
// 探测那层拿到的是"上传时顺手落的那份本地副本"(ffmpeg 只能读本地文件)。
// 转码要用它,而上传流程那侧也会想删它 —— **两边都删的话,
// worker 转码时拿到的是"文件不存在"**,ffmpeg 报 exit status 1,
// 而真正的原因是"文件被人删了"。
//
// ⇒ 所以 probeAndApply 的返回值是"副本归谁了"。
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/media"
	"rapid-admin/internal/pkg/oss"
)

// playbackObjectPrefix 播放副本的对象名前缀
//
// ⚠️ **必须和用户文件的对象名区分开** —— 用户文件的对象名是
// `{时间戳}-{8位随机}-{清洗后的原名}`(见 oss.NewObjectName),
// 而这里用的是 `__play__{文件id}.mp4`。用户文件名里不可能出现
// `__play__` 开头的东西 —— 和 `__cover__` 一样,真撞上了也只是副本被覆盖,
// 不会破坏文件本体。
const playbackObjectPrefix = "__play__"

// transcodeTimeout 一次转码的时间上限
//
// ⚠️ **必须有上界** —— ffmpeg 转一个坏文件可能卡住(坏帧、畸形的时间戳),
// 没有超时就是一个能永久占住并发闸的僵尸任务。
// 30 分钟对"几 GB 以内的普通视频"绰绰有余。
const transcodeTimeout = 30 * time.Minute

// transcodeJob 一个待转码的任务
type transcodeJob struct {
	// fileID 文件 id(写回状态用)
	fileID uint64
	// userID / biz / folderID 拼播放副本的 key 用
	userID   uint64
	biz      string
	folderID uint64
	// kind 视频还是音频(决定转码参数和产物后缀)
	kind media.Kind
	// localPath 本地副本路径(worker 负责删)
	localPath string
}

// enqueueTranscode 派一个转码任务。
//
// ⚠️ **返回值 = "本地副本的清理权归 worker 了"** —— 调用方不要再删它。
// 返回 false 有两种含义:不需要转 / 转码没派出去 —— 两种情况下
// 调用方都要自己清理副本。
//
// ⚠️ **先落状态再派活** —— worker 靠 `status = pending` 抢单(乐观锁),
// 顺序反了的话 worker 一启动就看到 status 还是 0,抢不到单,
// 任务就永远悬着(status 停在 0,而文件其实需要转码)。
func (s *fileService) enqueueTranscode(ctx context.Context, row *model.SysFile, res mediaProbeResult, localPath string) bool {
	if row == nil || localPath == "" {
		return false
	}
	// ⚠️ **没配 ffmpeg 就不派** —— 否则 worker 必然失败,
	// 状态会从"待转码"跳到"失败",前端显示"转码失败",
	// 而真相是"这台机器没装 ffmpeg"(部署问题,不是文件问题)。
	if s.opt.FFmpegPath == "" {
		logger.L().Info("file: 浏览器播不了,但没配 ffmpeg,跳过转码",
			zap.Uint64("file_id", row.ID), zap.String("reason", res.Reason))
		return false
	}
	// ⚠️ **并发为 0 = 整个功能关着** —— 配置里显式关掉的。
	if s.opt.TranscodeWorkers <= 0 {
		logger.L().Info("file: 浏览器播不了,但转码功能已关闭,跳过转码",
			zap.Uint64("file_id", row.ID), zap.String("reason", res.Reason))
		return false
	}
	if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("id = ?", row.ID).
		Update("transcode_status", model.TranscodeStatusPending).Error; err != nil {
		logger.L().Warn("file: 标记待转码失败,跳过转码",
			zap.Uint64("file_id", row.ID), zap.Error(err))
		return false
	}
	logger.L().Info("file: 浏览器播不了,已派转码任务",
		zap.Uint64("file_id", row.ID),
		zap.String("container", res.Container),
		zap.String("video_codec", res.VideoCodec),
		zap.String("audio_codec", res.AudioCodec),
		zap.String("reason", res.Reason))
	// ⚠️ **抢并发闸必须在 goroutine 里** —— 在这里抢的话,
	// 并发满了会**把上传请求堵住**(闸满 = 阻塞)。
	// 用户传 20 个 wmv,第 3 个之后的请求会一直挂着直到有 worker 空出来。
	go func() {
		// 等一个空位(不阻塞上传请求 —— 这已经是在 goroutine 里了)
		s.transcodeSlots <- struct{}{}
		defer func() { <-s.transcodeSlots }()
		s.runTranscode(jobFrom(row, res, localPath))
	}()
	return true
}

// jobFrom 拼一个转码任务
func jobFrom(row *model.SysFile, res mediaProbeResult, localPath string) transcodeJob {
	return transcodeJob{
		fileID:    row.ID,
		userID:    row.UserID,
		biz:       row.Biz,
		folderID:  row.FolderID,
		kind:      res.Kind,
		localPath: localPath,
	}
}

// runTranscode 跑一个转码任务。
//
// ⚠️ **全程不 panic 出去** —— 它跑在上传请求的 goroutine 之外,
// 一个 panic 会**打死整个进程**(Go 里任何 goroutine 的 panic 都会
// 终止程序,除非 recover)。所以这里必须 recover。
func (s *fileService) runTranscode(job transcodeJob) {
	defer func() {
		if r := recover(); r != nil {
			logger.L().Error("file: 转码任务 panic(已恢复)",
				zap.Uint64("file_id", job.fileID), zap.Any("panic", r))
			s.markTranscodeFailed(context.Background(), job.fileID,
				errors.New("转码任务异常终止"))
		}
	}()
	// ⚠️ **Background 而不是请求的 ctx** —— 见文件头。
	//   请求一结束 ctx 就被取消,传过去的话转码刚开始就被杀掉。
	ctx, cancel := context.WithTimeout(context.Background(), transcodeTimeout)
	defer cancel()

	// ⚠️ **副本的清理权在 worker 手里** —— 探测那层已经把它交给我们了。
	// 忘了删 = 临时盘上一个孤儿目录(每个几 MB 到几百 MB)。
	defer cleanupMediaTemp(job.localPath)

	if !s.claimTranscode(ctx, job.fileID) {
		// ⚠️ **抢不到单不是错误** —— 两个 worker 同时看到 status=pending
		//   就会转两遍(白烧 CPU + 双份存储)。乐观锁让只有一个能抢到。
		logger.L().Info("file: 转码任务被跳过(已被别的 worker 接手)",
			zap.Uint64("file_id", job.fileID))
		return
	}

	outPath, err := s.transcodeToTemp(ctx, job)
	if err != nil {
		s.markTranscodeFailed(ctx, job.fileID, err)
		return
	}
	defer os.RemoveAll(filepath.Dir(outPath))

	playbackKey, err := s.uploadPlayback(ctx, job, outPath)
	if err != nil {
		s.markTranscodeFailed(ctx, job.fileID, err)
		return
	}
	s.markTranscodeDone(ctx, job.fileID, playbackKey)
}

// claimTranscode 抢下这个转码任务。
//
// ⚠️ **用乐观锁而不是"查一下再改"** —— 两个 worker 同时读到 status=pending
// 就会都去转。`UPDATE ... WHERE status = pending` 只有第一个拿到
// RowsAffected=1,另一个拿到 0 就直接放弃。
func (s *fileService) claimTranscode(ctx context.Context, fileID uint64) bool {
	res := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("id = ? AND transcode_status = ?", fileID, model.TranscodeStatusPending).
		Update("transcode_status", model.TranscodeStatusRunning)
	if res.Error != nil {
		logger.L().Warn("file: 抢转码任务失败",
			zap.Uint64("file_id", fileID), zap.Error(res.Error))
		return false
	}
	return res.RowsAffected == 1
}

// transcodeToTemp 转码到临时文件,返回产物路径。
//
// ⚠️ **产物的后缀决定容器** —— ffmpeg 靠输出文件名后缀选 muxer,
// 而 mp4 容器装不下某些编码(所以参数里显式 map + -sn -dn)。
func (s *fileService) transcodeToTemp(ctx context.Context, job transcodeJob) (string, error) {
	dir, err := os.MkdirTemp(s.opt.MediaTmpDir, "ha-play-")
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, "playback"+media.PlaybackExt(job.kind))
	if err := media.Transcode(ctx, s.opt.FFmpegPath, job.localPath, out, job.kind); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	st, err := os.Stat(out)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	// ⚠️ **空产物 = 转码没成功** —— ffmpeg 退出码 0 但写出 0 字节的情况
	// 真实存在(某些畸形输入)。上传一个空对象,前端会拿到一个
	// "点开是 0 字节"的播放链接,比没有还糟。
	if st.Size() == 0 {
		_ = os.RemoveAll(dir)
		return "", errors.New("转码产物是空文件")
	}
	return out, nil
}

// uploadPlayback 把产物传回存储,返回播放副本的 key。
func (s *fileService) uploadPlayback(ctx context.Context, job transcodeJob, outPath string) (string, error) {
	if s.prov == nil {
		return "", errors.New("存储未初始化")
	}
	prefix, err := oss.FolderPrefix(s.opt.Root, job.userID, job.biz, job.folderID)
	if err != nil {
		return "", err
	}
	key := prefix + playbackObjectPrefix + strconv.FormatUint(job.fileID, 10) + media.PlaybackExt(job.kind)
	f, err := os.Open(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// ⚠️ **MIME 要跟产物类型对上** —— 音频产物是 audio/mp4(.m4a),
	// 视频产物是 video/mp4(.mp4)。给错的话浏览器可能拿视频播放器去放音频。
	ct := "video/mp4"
	if job.kind == media.KindAudio {
		ct = "audio/mp4"
	}
	if _, err := s.prov.Put(ctx, key, f, ct); err != nil {
		return "", err
	}
	return key, nil
}

// markTranscodeDone 转码完成,写回 key。
func (s *fileService) markTranscodeDone(ctx context.Context, fileID uint64, playbackKey string) {
	res := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("id = ?", fileID).
		Updates(map[string]any{
			"transcode_status": model.TranscodeStatusDone,
			"playback_key":     playbackKey,
		})
	if res.Error != nil {
		logger.L().Warn("file: 转码完成但写回失败",
			zap.Uint64("file_id", fileID), zap.Error(res.Error))
		return
	}
	// ⚠️ **RowsAffected=0 = 文件在这期间被删了** —— 副本成了孤儿对象。
	//   不删的话存储里会留一份没人认领、也没人计费的对象。
	if res.RowsAffected == 0 {
		logger.L().Info("file: 转码完成时文件已被删除,清理孤儿播放副本",
			zap.Uint64("file_id", fileID))
		if err := s.prov.Delete(ctx, playbackKey); err != nil {
			logger.L().Warn("file: 删除孤儿播放副本失败",
				zap.String("playback_key", playbackKey), zap.Error(err))
		}
		return
	}
	logger.L().Info("file: 转码完成",
		zap.Uint64("file_id", fileID), zap.String("playback_key", playbackKey))
}

// markTranscodeFailed 转码失败,记状态。
//
// ⚠️ **失败不重试** —— 重试要处理"半成品对象"、"幂等 key"、"失败原因分类",
// 而这一版的失败绝大多数是"这个文件 ffmpeg 也解不开",重试一百次也一样。
// 用户看到"转码失败 + 原因"就够了,原文件还在,能下载。
func (s *fileService) markTranscodeFailed(ctx context.Context, fileID uint64, err error) {
	logger.L().Warn("file: 转码失败(不影响文件本身)",
		zap.Uint64("file_id", fileID), zap.Error(err))
	if dbErr := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Where("id = ?", fileID).
		Update("transcode_status", model.TranscodeStatusFailed).Error; dbErr != nil {
		logger.L().Warn("file: 转码失败状态写回失败",
			zap.Uint64("file_id", fileID), zap.Error(dbErr))
	}
}

// deletePlaybackObject 删掉播放副本(文件被删时)。
//
// ⚠️ **删失败只记日志** —— 主文件已经删了,
// 留一个孤儿播放副本只是浪费一点存储,不该让"删除文件"报失败。
func (s *fileService) deletePlaybackObject(ctx context.Context, playbackKey string) {
	if playbackKey == "" || s.prov == nil {
		return
	}
	if err := s.prov.Delete(ctx, playbackKey); err != nil {
		logger.L().Warn("file: 删除播放副本失败",
			zap.String("playback_key", playbackKey), zap.Error(err))
	}
}
