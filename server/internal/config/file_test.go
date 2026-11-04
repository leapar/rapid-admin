// 配置加载的特征测试
//
// 存在的理由很具体:**yaml 里写错一个键名是零症状的**。`path_roots`(多了个 s)
// 解析出来落到 SetDefault 的 "rapid-admin",测试全绿、启动也不报错,而线上是
// "文件链接 404",一路查到 yaml 才知道。
//
// 所以这里断言的**不是解析后的值,而是"这个键必须真的写在文件里"**
// (viper.InConfig 只认配置文件本身,不看 SetDefault)。值的那一遍也留着,
// 但它是第二道 —— 第一道是键在不在。
package config

import (
	"os"
	"path/filepath"
	"testing"

	"rapid-admin/internal/pkg/oss"
)

// loadDev 加载真实的开发配置
func loadDev(t *testing.T) *Server {
	t.Helper()
	// 测试进程的 cwd = 包目录 internal/config,所以要退两级才是 server/
	p := filepath.Join("..", "..", "configs", "config.dev.yaml")
	if _, err := os.Stat(p); err != nil {
		// config.dev.yaml 是本机文件(在 .gitignore 里),clone 后没有 ——
		// 拷贝 config.dev.yaml.example 生成;模板那份已由 configFiles 的用例覆盖
		t.Skipf("configs/config.dev.yaml 不存在(它不进版本控制)—— cp configs/config.dev.yaml.example configs/config.dev.yaml 后再跑")
	}
	if err := Load(p); err != nil {
		t.Fatalf("load %s: %v", p, err)
	}
	return AppConfig
}

// requiredFileKeys file: 块里**必须显式出现在 yaml 里**的键。
//
// 为什么必须显式而不是靠 SetDefault:默认值的存在是为了"漏配也能跑",
// 但它同时把"写错了键名"变成一件查不出来的事。写错时的症状不是报错,
// 而是行为悄悄按默认值走(比如 path_root 错了 → 静态挂载指向一个不存在的目录)。
var requiredFileKeys = []string{
	"file.provider",
	"file.root",
	"file.path_root",
	"file.max_file_size_mb",
	"file.multipart_threshold_mb",
	"file.multipart_part_size_mb",
	"file.session_ttl",
	"file.download_url_ttl",
	"file.max_download_ttl",
	"file.ffmpeg_path",
	"file.ffprobe_path",
	"file.media_max_size_mb",
}

// configFiles 所有"会被真的加载"的配置文件。
//
// ⚠️ **`config.dev.yaml.example` 必须在里面** —— 它是**进版本控制**的那份
// 模板,新同事就是拿它拷成 config.dev.yaml 的。它漏改的话:本机 dev 全绿
// (本机用的是 config.dev.yaml,不是模板)、模板里留着已经作废的键
// (bucket_admin / bucket_public)、新人拷过去才发现 —— 而"配置键名写错"
// 是零症状的。所以模板和实际配置一起盯,别只盯实际配置。
var configFiles = []string{"config.dev.yaml", "config.dev.yaml.example", "config.example.yaml"}

// mustLoadOne 加载 configFiles 里的一个;config.dev.yaml 是本机文件
// (在 .gitignore 里),clone 后不存在 ⇒ 跳过该子用例(模板两份照测)。
func mustLoadOne(t *testing.T, name string) {
	t.Helper()
	p := filepath.Join("..", "..", "configs", name)
	if name == "config.dev.yaml" {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("configs/config.dev.yaml 不存在(它不进版本控制)—— 模板两份已单独覆盖")
		}
	}
	if err := Load(p); err != nil {
		t.Fatalf("load %s: %v", p, err)
	}
}

// removedFileKeys **已经不该再出现**的键。
//
// ⚠️ 留着它们不会有任何症状 —— viper 不认的键直接被丢掉,启动照常,
// 但读配置的人会以为"还在生效"。所以这里显式盯着它们不在。
func TestLoadDev_RemovedFileKeysAreGone(t *testing.T) {
	for _, name := range configFiles {
		t.Run(name, func(t *testing.T) {
			mustLoadOne(t, name)
			for _, k := range []string{
				"file.static_mount",
				"file.public_prefixes",
				"oss.bucket_admin",
				"oss.bucket_public",
				// ⚠️ **M137 删的**:`oss.provider` 从来没有任何代码读它 ——
				// 真正决定"文件存哪"的是 `file.provider`。两个键讲同一件事、
				// 只有一个有人读 ⇒ 配置里写什么都零症状,而读配置的人会以为
				// "oss.provider: mock" 管着什么(实际上 OSS 早就能真连)。
				"oss.provider",
			} {
				if AppViper.InConfig(k) {
					t.Errorf("configs/%s 里还有 %s —— 单桶单树模型下它已经不生效了,留着只会误导", name, k)
				}
			}
		})
	}
}

func TestLoadDev_FileKeysAreDeclaredInYaml(t *testing.T) {
	loadDev(t)
	for _, k := range requiredFileKeys {
		if !AppViper.InConfig(k) {
			t.Errorf("configs/config.dev.yaml 里没有 %s —— 键名写错会静默落回默认值", k)
		}
	}
}

// TestLoadDev_FileBlockIsFullyParsed 对齐 config.example.yaml(生产口径)。
// 两个文件都过一遍,免得"改了一个忘了另一个"。
func TestLoadDev_FileBlockIsFullyParsed(t *testing.T) {
	for _, name := range configFiles {
		t.Run(name, func(t *testing.T) {
			mustLoadOne(t, name)
			f := AppConfig.File

			if f.PathRoot != "rapid-admin" {
				t.Errorf("path_root=%q, want rapid-admin", f.PathRoot)
			}
			if f.MaxFileSizeMB != 100 || f.MultipartPartSizeMB != 8 {
				t.Errorf("max_file_size_mb=%d multipart_part_size_mb=%d, want 100 / 8",
					f.MaxFileSizeMB, f.MultipartPartSizeMB)
			}
			if f.SessionTTL != "24h" || f.DownloadURLTTL != "15m" || f.MaxDownloadTTL != "24h" {
				t.Errorf("ttl = %q/%q/%q, want 24h/15m/24h",
					f.SessionTTL, f.DownloadURLTTL, f.MaxDownloadTTL)
			}
			// 留空 = 不限,不是"漏配"
			if len(f.AllowedBiz) != 0 {
				t.Errorf("file.allowed_biz = %v, want 空(不限)", f.AllowedBiz)
			}
		})
	}
}

// TestLoadDev_SingleOSSBucket 桶只有一个 —— 老的 bucket_admin / bucket_public
// 必须都不在了(见 TestLoadDev_RemovedFileKeysAreGone)。
func TestLoadDev_SingleOSSBucket(t *testing.T) {
	for _, name := range configFiles {
		t.Run(name, func(t *testing.T) {
			mustLoadOne(t, name)
			o := AppConfig.OSS
			if o.Bucket == "" {
				t.Error("oss.bucket 为空 —— aliyun 模式下会起不来")
			}
		})
	}
}

// TestLoadDev_OSSEndpointPrivateParsed **内网 endpoint 那个键必须真的解析出来**
//
// ⚠️ 键名写错(`endpoint-private` / `endpointInternal` / `internal_endpoint`)
// 在 yaml 里是**零症状**的:viper 不认的键直接被丢掉 ⇒ 启动照常、日志照常,
// 只是"内网加速"从来没生效 —— 而没人会注意到"本来也没更快"。
//
// 所以这条断言的是"键在文件里 **且** 解析到了结构体字段上"。
func TestLoadDev_OSSEndpointPrivateParsed(t *testing.T) {
	for _, name := range configFiles {
		t.Run(name, func(t *testing.T) {
			mustLoadOne(t, name)
			if !AppViper.InConfig("oss.endpoint_private") {
				t.Errorf("configs/%s 里没有 oss.endpoint_private —— 键名写错会静默丢配置", name)
			}
			// 值可以为空(模板就是这么留的),但**字段得在**:
			// 断言"文件里的原样字符串 == 解析出来的值",空值也算过。
			raw := AppViper.GetString("oss.endpoint_private")
			if got := AppConfig.OSS.EndpointPrivate; got != raw {
				t.Errorf("oss.endpoint_private 解析成 %q,文件里是 %q", got, raw)
			}
		})
	}
}

// TestLoadDev_FileRootIsAbsolute resolvePaths 必须把 file.root 转绝对 ——
// 不然"从哪个目录启动"会决定文件存哪,而报错是 404 不是"配置错了"。
func TestLoadDev_FileRootIsAbsolute(t *testing.T) {
	f := loadDev(t).File
	if f.Root == "" {
		t.Fatal("file.root 为空")
	}
	if !filepath.IsAbs(f.Root) {
		t.Errorf("file.root = %q, want 绝对路径", f.Root)
	}
}

// TestPathRootDefault_MatchesOssDefault 两个默认值不许漂移。
//
// ⚠️ **path_root 的默认值在两处各写了一份字面量**:
//
//   - `config.go` 的 `SetDefault("file.path_root", ...)` —— 配置层的兜底
//   - `oss.DefaultRoot` —— oss 层的兜底(BuildKey / ParseKey 空 root 时用)
//
// 而 `file.path_root` **不在每个部署的 yaml 里都必须写** —— yaml 漏写时,
// 走的是 SetDefault;代码里直接用 oss.BuildKey 且不传 Root 时,走的是
// oss.DefaultRoot。两份漂移 ⇒ **两种路径长成两棵树**,
// 而症状是"有的文件能打开、有的打不开",直链还不经后端、日志里看不到。
//
// ⚠️ **这条测试故意不写 `file.path_root`** —— 写了就测的是 yaml 而不是默认值。
func TestPathRootDefault_MatchesOssDefault(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "no-path-root.yaml")
	// 最小 yaml:只留 system 块,其余全靠 SetDefault
	if err := os.WriteFile(p, []byte("system:\n  name: minimal\n"), 0o600); err != nil {
		t.Fatalf("写临时配置失败: %v", err)
	}
	// 恢复全局配置 —— 这条测试动了 AppConfig/AppViper,不能留给后面的用例。
	t.Cleanup(func() {
		_ = Load(filepath.Join("..", "..", "configs", "config.dev.yaml"))
	})

	if err := Load(p); err != nil {
		t.Fatalf("load %s: %v", p, err)
	}
	if got := AppConfig.File.PathRoot; got != oss.DefaultRoot {
		t.Errorf("file.path_root 的默认值 = %q,oss.DefaultRoot = %q —— 两者必须一致,"+
			"否则 yaml 漏写 path_root 的机器会和直接用 oss.BuildKey 的代码长出两棵树",
			got, oss.DefaultRoot)
	}
}

// TestLoadDev_MediaToolKeysAreDeclaredInYaml 媒体探测的三个键必须在**每个**
// 配置文件里显式出现。
//
// ⚠️ 理由和 requiredFileKeys 一样:**键名写错是零症状的** —— viper 不认的键
// 直接被丢掉,于是 ffmpeg_path 永远落回默认值,用户配的路径从来没生效过,
// 而症状只是"视频没有时长"。
func TestLoadDev_MediaToolKeysAreDeclaredInYaml(t *testing.T) {
	for _, name := range configFiles {
		t.Run(name, func(t *testing.T) {
			mustLoadOne(t, name)
			for _, k := range []string{"file.ffmpeg_path", "file.ffprobe_path", "file.media_max_size_mb"} {
				if !AppViper.InConfig(k) {
					t.Errorf("%s 里没有 %s", name, k)
				}
			}
			f := AppConfig.File
			if f.FFmpegPath == "" || f.FFprobePath == "" {
				t.Errorf("媒体工具路径为空:%q / %q", f.FFmpegPath, f.FFprobePath)
			}
			if f.MediaMaxSizeMB != 2048 {
				t.Errorf("media_max_size_mb=%d, want 2048", f.MediaMaxSizeMB)
			}
		})
	}
}

// TestResolveMediaTools_EnvBeatsYaml **环境变量优先于 yaml**。
//
// ⚠️ ffmpeg 不随仓库分发,每台机器装的位置都不一样(Windows 上常常是某个
// 绿色版目录)。"这台机器上它到底在哪"的唯一真相是环境变量 ——
// yaml 只是仓库内的默认值。
func TestResolveMediaTools_EnvBeatsYaml(t *testing.T) {
	t.Setenv("FFMPEG_PATH", "/opt/ffmpeg/bin/ffmpeg")
	t.Setenv("FFPROBE_PATH", "/opt/ffmpeg/bin/ffprobe")
	c := FileConfig{FFmpegPath: "yaml-ffmpeg", FFprobePath: "yaml-ffprobe"}
	got := c.ResolveMediaTools()
	if got.FFmpeg != "/opt/ffmpeg/bin/ffmpeg" {
		t.Errorf("FFmpeg=%q, want 环境变量优先", got.FFmpeg)
	}
	if got.FFprobe != "/opt/ffmpeg/bin/ffprobe" {
		t.Errorf("FFprobe=%q, want 环境变量优先", got.FFprobe)
	}
}

// TestResolveMediaTools_FallsBackToPathLookup 都没配时走 PATH,不是留空。
//
// ⚠️ **留空 = 不探测**(service 层的语义),而绝大多数装了 ffmpeg 的环境
// 都能直接从 PATH 找到 —— 让每个部署都写一遍 yaml 是没必要的负担。
func TestResolveMediaTools_FallsBackToPathLookup(t *testing.T) {
	t.Setenv("FFMPEG_PATH", "")
	t.Setenv("FFPROBE_PATH", "")
	got := FileConfig{}.ResolveMediaTools()
	if got.FFmpeg != "ffmpeg" || got.FFprobe != "ffprobe" {
		t.Errorf("兜底 = %q / %q, want ffmpeg / ffprobe", got.FFmpeg, got.FFprobe)
	}
	// yaml 有值时用 yaml
	got2 := FileConfig{FFmpegPath: "yaml-ffmpeg", FFprobePath: "yaml-ffprobe"}.ResolveMediaTools()
	if got2.FFmpeg != "yaml-ffmpeg" || got2.FFprobe != "yaml-ffprobe" {
		t.Errorf("yaml 兜底 = %q / %q, want yaml-ffmpeg / yaml-ffprobe", got2.FFmpeg, got2.FFprobe)
	}
}
