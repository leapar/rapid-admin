// 配置加载 —— 顶层 Server 结构 + viper 加载
// 文档 §4.3
package config

import (
	"fmt"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Server 全部配置
type Server struct {
	System    SystemConfig    `mapstructure:"system"`
	Logger    LoggerConfig    `mapstructure:"logger"`
	MySQL     MySQLConfig     `mapstructure:"mysql"`    // 兼容:database.mysql 为空时自动 fallback 到此块
	Database  DatabaseConfig  `mapstructure:"database"` // 新:driver + mysql + sqlite
	Redis     RedisConfig     `mapstructure:"redis"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	Auth      AuthConfig      `mapstructure:"auth"`  // 吊销存储模式(与 JWT 分开:一个管 token 形态,一个管失效判定)
	KV        KVConfig        `mapstructure:"kv"`    // 限流 / 登录锁定的存储模式
	Cache     CacheConfig     `mapstructure:"cache"` // cachex 两级缓存(L1 本地 + L2 Redis)
	Job       JobConfig       `mapstructure:"job"`   // cronx 定时任务调度
	Casbin    CasbinConfig    `mapstructure:"casbin"`
	Cors      CorsConfig      `mapstructure:"cors"`
	SMS       SMSConfig       `mapstructure:"sms"`
	Email     EmailConfig     `mapstructure:"email"` // 额外交付通道(站内信之外):provider 为空串 = 不装配
	Push      PushConfig      `mapstructure:"push"`  // 同上;与 SMS 的差别见 email.go / push.go(失败方向不一样)
	OSS       OSSConfig       `mapstructure:"oss"`
	File      FileConfig      `mapstructure:"file"`
	// Voice 语音能力(音色克隆 + 文本合成)—— 见 internal/config/voice.go。
	Voice VoiceConfig `mapstructure:"voice"`
	Crypto    CryptoConfig    `mapstructure:"crypto"`
	KMS       KMSConfig       `mapstructure:"kms"`
	Captcha   CaptchaConfig   `mapstructure:"captcha"`
	RateLimit RateLimitConfig `mapstructure:"ratelimit"`
	Audit     AuditConfig     `mapstructure:"audit"`
	OTel      OTelConfig      `mapstructure:"otel"`
	Breaker   BreakerConfig   `mapstructure:"breaker"`
	Timeout   TimeoutConfig   `mapstructure:"timeout"`
	Metrics   MetricsConfig   `mapstructure:"metrics"`
	// ClientIP 客户端 IP 解析(可信反向代理网段)—— M105 新增。
	// 见 internal/config/clientip.go 与 middleware/clientip.go。
	ClientIP ClientIPConfig `mapstructure:"client_ip"`
	// Branding 品牌展示信息(平台名 / 浏览器标题)—— 开源通用化新增。
	// 见 internal/config/branding.go。
	Branding BrandingConfig `mapstructure:"branding"`
	// Swagger 文档端点(OpenAPI 3.0 + Swagger UI)—— 开源通用化新增。
	// 见 internal/config/swagger.go。
	Swagger SwaggerConfig `mapstructure:"swagger"`
}

var (
	AppViper  *viper.Viper
	AppConfig *Server
)

// Load 加载配置 + 注册热重载
//
// path 允许传相对路径,**按 rootDir(APP_ROOT > cwd)解析**,与其余相对路径同基准;
// 内部一律转绝对并记入 configFile,调用方可用 ConfigFile() 打印"到底读的是哪个文件"。
// APP_ROOT 未设时 rootDir == cwd,行为与改造前完全一致。
func Load(path string) error {
	// 0. 先定根目录 —— 后面所有相对路径都以它为基准(APP_ROOT > cwd)
	if err := initRoot(); err != nil {
		return fmt.Errorf("init root: %w", err)
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("config path is empty")
	}
	configFile = absPath(path)

	v := viper.New()
	v.SetConfigFile(configFile)
	v.SetConfigType("yaml")
	v.AutomaticEnv()
	// ENV 替换:支持 ${MY_VAR} 形式
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("read config %s: %w", configFile, err)
	}

	// 安全项默认开:yaml 漏写 captcha 块时 bool 零值是 false,会让验证码校验静默消失 ——
	// 把"配置遗漏"当成"安全策略"是最坏的失败方向。见 captcha.go 类型注释。
	v.SetDefault("captcha.login_enabled", true)
	v.SetDefault("captcha.sms_send_enabled", true)
	// 下面两个不是"安全策略",是"默认降级方向",但同样要显式写出:
	// 留空串时 Resolve 会落到 case "" 分支,效果**恰好等于 auto**,
	// 于是"yaml 里写了这个键却是空的"和"没写这个键"变成同一件事 ——
	// 前者通常意味着有人想关掉它却没关成功。
	v.SetDefault("auth.revocation_mode", "auto")
	// ---- 异常登录检测(M128)----
	//
	// ⚠️ enabled 的零值是 false —— yaml 里漏写 auth.anomaly 块时,
	//   检测会**静默不跑**,而症状是"日志里一条异常都没有" ——
	//   与"跑了但没发现异常"**完全无法区分**。
	//   与 job.enabled / captcha.* 同一条纪律:漏配必须等于"照常跑"。
	v.SetDefault("auth.anomaly.enabled", true)
	// ⚠️ max_concurrent 的零值也是 0 —— 而 0 会被 maxConcurrent() 兜底成 1,
	//   也就是"只允许 1 个会话在线",正常人多端登录全变异常。
	//   显式给 3,别让它靠兜底。
	v.SetDefault("auth.anomaly.max_concurrent", 3)
	// active_window 同理:0 会被当成"不判同时在线"(等于检测永远不报)。
	v.SetDefault("auth.anomaly.active_window", "30m")
	// impossible_travel_window 默认 0 = 关。**这是刻意的**,理由见
	// config.AnomalyConfig 的注释(没有 IP 地理库,开着必然误报)。
	v.SetDefault("auth.anomaly.impossible_travel_window", "0s")
	v.SetDefault("kv.mode", "auto")
	// cache.mode 默认 auto 走的是"有 Redis 用 Redis,没有退纯 L1",漏配不会有任何
	// 症状(只是没缓存),所以这里给默认值而不是像 captcha 那样给安全默认值 ——
	// 缓存**失效**只会变慢,不会出安全事故。
	v.SetDefault("cache.mode", "auto")
	// cronx.lock_mode 同理默认 auto。定时任务**漏配不会有症状**(只是没去重),
	// 但"多副本跑重复任务"是数据正确性问题,所以这里同时把降级告警做成响亮的。
	v.SetDefault("job.lock_mode", "auto")
	v.SetDefault("job.timezone", "Asia/Shanghai")
	// ⚠️ 下面两个默认值都是 Go 的"零值陷阱",不显式给就等于静默降级:
	//   enabled 的零值是 false —— 没配 job 块的部署会**一个定时任务都不跑**,
	//     而且没有任何症状(页面能开、接口能通,就是什么都不发生)。
	//   lock_renew 的零值也是 false —— 没配就是没有看门狗,长任务跑到一半
	//     锁自然过期,第二个副本直接抢进来并发写同一批数据。
	// 显式写成 true,是让「漏配」等于「照常跑」而不是「静默变弱」。
	v.SetDefault("job.enabled", true)
	v.SetDefault("job.lock_renew", true)
	v.SetDefault("job.lock_ttl", "30s")
	v.SetDefault("job.default_timeout", "30m")
	v.SetDefault("job.shutdown_timeout", "30s")
	v.SetDefault("job.history_retention_days", 30)
	v.SetDefault("job.history_retention_max", 50000)

	// ---- 文件系统(M80)----
	//
	// ⚠️ **path_root 一处决定两件事**:key 的第一段 + 静态挂载点(`/rapid-admin`)。
	// 以前挂载点另配一个 `static_mount`,两者对不齐时的症状是"请求命中了
	// 路由但永远 404"—— 静默的,而且要顺着 yaml → 路由 → provider 查一遍
	// 才找得到。现在只有一处真相。
	//
	// ⚠️ **这个默认值必须跟 `oss.DefaultRoot` 一致** —— 一个是配置层的兜底,
	// 一个是 oss 层的兜底,两者漂移就会长出两棵树(而症状是全站 404)。
	// `internal/config` 的测试盯着这件事。
	v.SetDefault("file.provider", "local")
	v.SetDefault("file.path_root", "rapid-admin")
	v.SetDefault("file.max_file_size_mb", 100)
	v.SetDefault("file.multipart_threshold_mb", 100)
	v.SetDefault("file.multipart_part_size_mb", 8)
	v.SetDefault("file.session_ttl", "24h")
	v.SetDefault("file.download_url_ttl", "15m")
	v.SetDefault("file.max_download_ttl", "24h")
	// ⚠️ **ffmpeg/ffprobe 的默认是"走 PATH"** —— 不是留空。
	//   留空会被当成"没有工具",于是连"PATH 里明明有 ffmpeg"的机器也不探测;
	//   而绝大多数装了 ffmpeg 的环境都能从 PATH 直接找到。
	//   要指到具体位置用环境变量 FFMPEG_PATH / FFPROBE_PATH(优先于 yaml)。
	v.SetDefault("file.ffmpeg_path", "ffmpeg")
	v.SetDefault("file.ffprobe_path", "ffprobe")
	// ---- Swagger 文档端点(开源通用化)----
	//
	// ⚠️ enabled 的零值是 false —— yaml 里漏写 swagger 块时文档端点会**静默消失**,
	//   而症状是"访问 /swagger 404",没人会想到是配置漏了。
	//   跟 job.enabled / captcha.* 同一条纪律:漏配必须等于"照常跑"。
	v.SetDefault("swagger.enabled", true)
	// ⚠️ mode 必须是 "auto" 而不是空串 —— 空串会让 ResolveAuth 走"没配凭据"的分支,
	//   而 auto 的语义是"没配凭据就本次启动随机生成一个"(**不是裸奔**)。
	//   两者的差别就是"有没有那层 BasicAuth",必须显式钉死。
	v.SetDefault("swagger.auth.mode", "auto")
	// ⚠️ **媒体探测有大小闸** —— 探测要把文件完整落到本地磁盘,
	//   "能传多大的文件"直接等于"临时盘要留多少空间"(见 FileConfig.MediaMaxSizeMB)。
	v.SetDefault("file.media_max_size_mb", 2048)

	cfg := &Server{}
	if err := v.Unmarshal(cfg); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	// 相对路径 → 绝对路径。必须在 logger.New / NewDB / casbinx.Init 之前,
	// 这些消费方拿到的应该是绝对路径而不是随手拼出来的相对路径。
	cfg.resolvePaths()

	AppViper = v
	AppConfig = cfg

	// 热重载(平台配置中心 §14.5)
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		zap.L().Info("config file changed", zap.String("file", e.Name))
		if err := v.Unmarshal(AppConfig); err != nil {
			zap.L().Error("re-unmarshal config failed", zap.Error(err))
			return
		}
		// 必须重新 resolve:Unmarshal 是往同一个指针里覆写,
		// yaml 里的相对值会把上一轮转好的绝对路径打回去。
		AppConfig.resolvePaths()
	})

	return nil
}

// Get 取当前配置
func Get() *Server { return AppConfig }
