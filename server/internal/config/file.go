// 文件系统配置(M80,设计文档 §7)
//
// ⚠️ **这一块和 `OSSConfig` 是两回事,别合并**:
//   - OSSConfig 说的是"怎么连阿里云"(endpoint / ak / sk / bucket)
//   - FileConfig 说的是"文件放在哪、怎么放"(路径形态 / 配额闸 /
//     什么时候切分片)
//
// 两者正交:local 模式下 OSSConfig 整块不生效,aliyun 模式下 FileConfig 照样
// 生效(路径形态和配额闸跟后端是 local 还是 oss 无关)。合并成一个结构的话,
// 配 local 的部署会带着一堆用不上的 OSS 字段,读的人分不清哪些真的生效。
package config

import (
	"os"
	"strings"
)

// provider 取值。
const (
	// ProviderLocal 本地磁盘。
	ProviderLocal = "local"
	// ProviderAliyun 阿里云 OSS。
	ProviderAliyun = "aliyun"
)

// NormalizeProvider 把 provider 规整成小写、去空白,**空 = local**。
//
// ⚠️ **空 = local 是全局约定**,不止是这一处的实现细节:
//   - `buildFileProvider` 的 switch 按它兜底
//   - `staticFSTarget` 按它决定"要不要把本机磁盘树挂成静态路由"
// 两边各判一次 ⇒ 迟早对不齐(对不齐的症状是"OSS 模式下本机旧文件还能访问")。
// 所以只有这一个入口。
func NormalizeProvider(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "" {
		return ProviderLocal
	}
	return p
}

// IsLocalProvider 是不是本地磁盘 provider(空也算)。
func IsLocalProvider(p string) bool {
	return NormalizeProvider(p) == ProviderLocal
}

type FileConfig struct {
	// Provider local | aliyun。空 → local。
	//
	// ⚠️ **默认 local 是刻意的**:配错一个字符的代价是"文件传到别的地方去了",
	// 而 local 至少是"就存在本机磁盘上",能捞回来。
	Provider string `mapstructure:"provider"`

	// Root 存储根目录(local 时是磁盘路径)。
	//
	// ⚠️ **改它等于全量数据迁移** —— 已有文件不会跟着挪走,改完之后 sys_file
	// 里的 key 全指向不存在的路径,而报错是 404,不是"配置错了"。
	Root string `mapstructure:"root"`

	// PathRoot 路径根词(两种 provider 都生效),默认 `rapid-admin`。
	//
	// ⚠️ **这是 URL / key 里的第一段**,不是磁盘路径。local 的磁盘根是 Root,
	// key 根是 PathRoot —— 两个不同的东西,混了就会挂载点算错。
	//
	// ⚠️ **静态挂载点由它推导**(`/rapid-admin`),没有单独的 static_mount 配置项。
	// 以前挂载点配在 `static_mount`、前缀配在 `public_prefixes`,两个配置项
	// 对不齐时的症状是"请求命中了路由但永远 404"—— 静默的。现在只有一处
	// 真相:key 的第一段就是 URL 的第一段。
	PathRoot string `mapstructure:"path_root"`

	// AllowedBiz 业务段白名单。**留空 = 不限**(只做语法校验)。
	//
	// ⚠️ 框架不硬编码业务枚举(与本仓字典 / 配置单一来源的思路一致),哪个 biz
	// 合法由业务侧自己决定。填了就当白名单用。
	AllowedBiz []string `mapstructure:"allowed_biz"`

	// MaxFileSize 单文件字节上限(MB),默认 100。0 = 不限。
	MaxFileSizeMB int `mapstructure:"max_file_size_mb"`

	// MultipartThreshold 超过这个大小(MB)建议走分片,默认 100。
	MultipartThresholdMB int `mapstructure:"multipart_threshold_mb"`

	// MultipartPartSize 分片大小(MB),默认 8。低于 1 会被抬到 oss.MinPartSize。
	MultipartPartSizeMB int `mapstructure:"multipart_part_size_mb"`

	// SessionTTL 上传会话有效期,默认 24h。
	SessionTTL string `mapstructure:"session_ttl"`

	// DownloadURLTTL 下载临时 URL 默认有效期,默认 15m
	DownloadURLTTL string `mapstructure:"download_url_ttl"`

	// MaxDownloadTTL 下载 URL 有效期上限,默认 24h
	MaxDownloadTTL string `mapstructure:"max_download_ttl"`

	// FFmpegPath ffmpeg 可执行文件路径,默认 "ffmpeg"(走 PATH)。
	//
	// ⚠️ **可以被环境变量 FFMPEG_PATH 覆盖,而且环境变量优先** ——
	// ffmpeg 不是 Go 依赖,不会随仓库分发,每个部署环境装的位置都不一样
	// (Windows 上常常是某个绿色版目录)。yaml 是仓库里的文件,
	// 环境变量才是"这台机器上它到底在哪"的唯一真相。
	// 见 ResolveMediaTools。
	FFmpegPath string `mapstructure:"ffmpeg_path"`

	// FFprobePath ffprobe 可执行文件路径,默认 "ffprobe"(走 PATH)。
	//
	// ⚠️ **ffprobe 和 ffmpeg 是两个二进制** —— ffmpeg 发行包里通常两个都有,
	// 但也有只装了 ffmpeg 的精简包。分开配,免得为了时长把 ffmpeg 也拖上。
	FFprobePath string `mapstructure:"ffprobe_path"`

	// MediaMaxSizeMB 媒体探测的文件大小上限(MB),默认 2048。0 = 用默认值。
	//
	// ⚠️ **为什么要有上限** —— 探测媒体要把文件**完整落到本地磁盘**
	// (ffmpeg 必须读本地文件),所以"能传多大的文件"直接等于
	// "服务器临时盘要留多少空间"。不设闸的话,一个 10GB 的视频会把
	// 临时盘撑爆,而症状是"上传失败"或"服务器磁盘告警" —— 跟视频本身无关。
	MediaMaxSizeMB int `mapstructure:"media_max_size_mb"`

	// TranscodeEnabled 是否给"浏览器播不了"的音视频转一份可播副本(M145)。
	//
	// ⚠️ **零值是 false = 关** —— 所以 yaml 模板里必须显式写 `true`,
	//   否则"配了 ffmpeg 却不转码"这件事没有任何提示。
	//   (布尔值没法像 int 那样区分"没配"和"配成 0",这是这一项必须
	//   在模板里落一句 true 的原因。)
	//
	// ⚠️ **关掉它只是不转码,不影响别的** —— 时长、封面照旧,
	//   列表里会显示"浏览器不支持" + 原因(见 model.SysFile.MediaReason)。
	TranscodeEnabled bool `mapstructure:"transcode_enabled"`

	// TranscodeWorkers 转码并发数(M145),默认 2。
	//
	// ⚠️ **每个转码是一个 ffmpeg 进程,吃满一个核** —— 这个数就是
	//   "最多同时烧几个核"。2 是刻意的默认值:够一个上传高峰用,
	//   又不会把一台 2 核的小机器打满。
	TranscodeWorkers int `mapstructure:"transcode_workers"`
}

// MediaToolPaths 媒体工具的可执行文件路径
type MediaToolPaths struct {
	// FFmpeg ffmpeg 可执行文件路径
	FFmpeg string
	// FFprobe ffprobe 可执行文件路径
	FFprobe string
}

// ResolveMediaTools 解析媒体工具路径。
//
// ⚠️ **优先级:环境变量 > yaml > 默认值** ——
// 环境变量 `FFMPEG_PATH` / `FFPROBE_PATH` 是"这台机器上它在哪"的真相
// (ffmpeg 不随仓库分发,装的位置因机器而异);yaml 只是仓库内的默认值。
//
// ⚠️ **留空就落到 "ffmpeg" / "ffprobe"(走 PATH)** ——
// 不是留空不干活:多数 Linux 镜像和装了 ffmpeg 的 Windows 都能直接
// 从 PATH 找到,让用户为每个部署都写一遍 yaml 是没必要的负担。
func (c FileConfig) ResolveMediaTools() MediaToolPaths {
	out := MediaToolPaths{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}
	if v := strings.TrimSpace(os.Getenv("FFMPEG_PATH")); v != "" {
		out.FFmpeg = v
	} else if v := strings.TrimSpace(c.FFmpegPath); v != "" {
		out.FFmpeg = v
	}
	if v := strings.TrimSpace(os.Getenv("FFPROBE_PATH")); v != "" {
		out.FFprobe = v
	} else if v := strings.TrimSpace(c.FFprobePath); v != "" {
		out.FFprobe = v
	}
	return out
}
