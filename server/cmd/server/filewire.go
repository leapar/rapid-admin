// 文件系统的 provider 选型 + 配置映射(M80 接线部分)
// 设计见 docs/文件系统模块设计方案V1.0.md §7
package main

import (
	"strings"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/oss/aliyun"
	"rapid-admin/internal/pkg/oss/local"
	"rapid-admin/internal/service"
)

// buildFileProvider 按 file.provider 选存储实现。
//
// ⚠️ **provider 为空 / 未知一律落到 local 并打 Warn,不报错**。理由和
// buildSmsSender 不同:短信落到 console 只是验证码打不出来,而存储落到一个
// "看不懂的 provider" 会让整个进程起不来 —— 文件系统是每个页面都要用的,
// 它挂了等于全站白屏。可接受的降级方向是"存到本机磁盘",不可接受的是"起不来"。
//
// ⚠️ **local 的 Root 为空时兜底成 `<absPath("")>/data/files`** 而不是直接
// 报错:一个刚 clone 下来、还没写配置文件的人应该能直接 `go run` 起来看到页面,
// 而不是先学会配 yaml。
func buildFileProvider(cfg config.FileConfig, ossCfg config.OSSConfig) oss.Provider {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "aliyun":
		p, err := aliyun.New(aliyun.Config{
			Endpoint:        ossCfg.Endpoint,
			EndpointPrivate: ossCfg.EndpointPrivate,
			AccessKey:       ossCfg.AccessKey,
			AccessSecret:    ossCfg.AccessSecret,
			Bucket:          ossCfg.Bucket,
			CDNDomain:       ossCfg.CDNDomain,
			UseHTTPS:        true,
		})
		if err != nil {
			// ⚠️ 这里 Fatal 而不是 Warn:配了 aliyun 却连不上,继续用 local 存
			// 会让文件"看起来传上去了"但其实躺在本机磁盘上,前端拿到的直链
			// 指向一个不存在的公网地址 —— 那种故障比启动失败难查一百倍。
			logger.L().Fatal("init aliyun oss provider failed",
				zap.String("file.root", cfg.PathRoot), zap.Error(err))
		}
		// ⚠️ **桶必须是公共读** —— 直链是拼出来的、不带签名的稳定 URL。
		// 私有桶上它会 403,而症状是"文件传上去了、列表里点得开、打开是裂图",
		// 报错信息还指向别处。这里喊一声,让人知道去改桶的 ACL。
		logger.L().Info("file provider ready(直链要求桶为公共读 public-read)",
			zap.String("provider", "aliyun"), zap.String("bucket", ossCfg.Bucket))
		return p
	case "local", "":
		root := cfg.Root
		if strings.TrimSpace(root) == "" {
			root = "data/files"
			logger.L().Warn("file.root 未配置,兜底到 data/files(相对 server/ 根)")
		}
		p, err := local.New(local.Config{
			Root: root,
			// ⚠️ **URLBase 必须等于静态挂载点**,而挂载点由 path_root 推导 ——
			// 两边都从同一个值算出来,就不可能对不齐(以前它们各配各的,
			// 对不齐的症状是"URL 永远 404")。
			URLBase: staticMountPoint(cfg.PathRoot),
		})
		if err != nil {
			logger.L().Fatal("init local file provider failed",
				zap.String("root", root), zap.Error(err))
		}
		logger.L().Info("file provider ready",
			zap.String("provider", "local"), zap.String("root", root))
		return p
	default:
		logger.L().Warn("未知的 file.provider, fallback 到 local",
			zap.String("provider", cfg.Provider))
		return buildFileProvider(config.FileConfig{
			Provider: "local", Root: cfg.Root, PathRoot: cfg.PathRoot,
		}, ossCfg)
	}
}

// staticMountPoint 由 path_root 推出静态挂载点:"rapid-admin" → "/rapid-admin"。
//
// ⚠️ **单桶单树模型下,挂载点就是 key 的第一段** —— 磁盘上是
// `<root>/rapid-admin/...`,URL 是 `/rapid-admin/...`。以前挂载点另配一个
// `static_mount`,两处对不齐时的症状是"请求命中了路由但永远 404",静默。
func staticMountPoint(pathRoot string) string {
	p := strings.Trim(strings.TrimSpace(pathRoot), "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

// buildFileOptions 把配置映射成 service 层的 FileOptions。
//
// ⚠️ **时长解析失败一律落回默认值并打 Warn,不返回错误**。这里的每一个值都
// 有安全的默认值(它们本来就写在 DefaultFileOptions 里),而配置写错一个
// "30"("30"没有单位,viper 的 GetDuration 不认)就拒绝启动,是拿一个可恢复的
// 笔误换一个全站不可用。
func buildFileOptions(cfg config.FileConfig) service.FileOptions {
	opt := service.DefaultFileOptions()

	opt.Root = cfg.PathRoot
	// ⚠️ **白名单必须真的传下去**。file.allowed_biz 的注释写着"填了就当白名单
	// 用",而这一行之前是漏的 —— 配置里写了不生效,而且没有任何启动日志提示。
	// 留空 = 不限,是 M80 落地时的行为,所以这一项对现有部署是无变更的。
	opt.AllowedBiz = trimBizList(cfg.AllowedBiz)

	if cfg.MaxFileSizeMB > 0 {
		opt.MaxFileSize = int64(cfg.MaxFileSizeMB) << 20
	}
	if cfg.MultipartThresholdMB > 0 {
		opt.MultipartThreshold = int64(cfg.MultipartThresholdMB) << 20
	}
	if cfg.MultipartPartSizeMB > 0 {
		opt.MultipartPartSize = int64(cfg.MultipartPartSizeMB) << 20
	}
	opt.SessionTTL = durOr(cfg.SessionTTL, opt.SessionTTL, "file.session_ttl")
	opt.DownloadURLTTL = durOr(cfg.DownloadURLTTL, opt.DownloadURLTTL, "file.download_url_ttl")
	opt.MaxDownloadTTL = durOr(cfg.MaxDownloadTTL, opt.MaxDownloadTTL, "file.max_download_ttl")

	// ⚠️ **媒体工具路径必须走 ResolveMediaTools** —— 环境变量 FFMPEG_PATH /
	//   FFPROBE_PATH 优先于 yaml。ffmpeg 不随仓库分发,每台机器装的位置都
	//   不一样(Windows 上常常是某个绿色版目录),"这台机器上它到底在哪"只有
	//   环境变量说了算。
	tools := cfg.ResolveMediaTools()
	opt.FFmpegPath = tools.FFmpeg
	opt.FFprobePath = tools.FFprobe

	// ⚠️ **媒体探测的大小闸** —— 探测要把文件完整落到本地磁盘,
	//   "能传多大的文件"直接等于"临时盘要留多少空间"。
	if cfg.MediaMaxSizeMB > 0 {
		opt.MediaMaxSize = int64(cfg.MediaMaxSizeMB) << 20
	}
	// ⚠️ **转码(M145)** —— 只有显式开了才做。
	//   0 = 关:service 层拿它当"不派活",于是浏览器播不了的那些文件
	//   只是多一条"为什么播不了"的说明,不会有转码副本。
	opt.TranscodeWorkers = 0
	if cfg.TranscodeEnabled {
		opt.TranscodeWorkers = cfg.TranscodeWorkers
		if opt.TranscodeWorkers <= 0 {
			opt.TranscodeWorkers = 2
		}
	}
	// ⚠️ **启动时喊一声工具路径** —— "视频没有时长/封面"最常见的原因就是
	//   路径指错了(或者压根没装)。日志里有这一行,排查时不用去猜。
	logger.L().Info("file: 媒体探测(视频时长/封面)",
		zap.String("ffmpeg", opt.FFmpegPath),
		zap.String("ffprobe", opt.FFprobePath),
		zap.Int64("max_size_bytes", opt.MediaMaxSize),
		zap.Int("transcode_workers", opt.TranscodeWorkers))

	// ⚠️ 上限不能小于默认值 —— 否则用户把 ttl 配成 1m 会让每次下载都更早
	// 过期,越用越容易 403。service 的 normalize() 也守这条,但这里先兜一层,
	// 让"配错了"在启动日志里就露出来。
	if opt.MaxDownloadTTL < opt.DownloadURLTTL {
		logger.L().Warn("file.max_download_ttl 小于 file.download_url_ttl,已抬到后者",
			zap.Duration("max", opt.MaxDownloadTTL), zap.Duration("ttl", opt.DownloadURLTTL))
		opt.MaxDownloadTTL = opt.DownloadURLTTL
	}
	return opt
}

// durOr 解析时长,失败落回 def
func durOr(raw string, def time.Duration, key string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		logger.L().Warn(key+" 不是合法时长,已落回默认值",
			zap.String("raw", raw), zap.Duration("fallback", def), zap.Error(err))
		return def
	}
	return d
}

// trimBizList 洗白名单:去空白、丢空项。
//
// ⚠️ **不 trim 小写、不做语法校验**。biz 的语法规则在 oss 包里(和 key 构造共用
// 一份),这里再写一遍就是第二份规则,而分叉出去的那份没人记得测。
// 真配错了会在建文件夹时被 validateBiz 拒掉,错得响亮、不静默。
func trimBizList(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// checkFileConfig 启动自检:path_root 必须能推出一个合法的挂载点。
//
// ⚠️ **path_root 为空时 Fatal**,因为挂载点会变成空串,静态路由挂不上,
// 而直链会变成 "/123/asset/2/a.pdf"(少了第一段)—— 症状是"每个文件
// 的链接都 404",而且没有任何日志提示它跟 path_root 有关。
func checkFileConfig(cfg config.FileConfig) {
	if strings.TrimSpace(cfg.PathRoot) == "" {
		logger.L().Fatal("file.path_root 不能为空:它同时是 key 的第一段和静态挂载点")
	}
}
