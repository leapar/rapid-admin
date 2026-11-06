// rapid-admin 入口
// 文档 §4.2
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"go.uber.org/zap"

	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/api/router"
	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/auditx"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/auth/revocache"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/pkg/casbinx"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/cryptaudit"
	"rapid-admin/internal/pkg/crypto"
	"rapid-admin/internal/pkg/desensitize"
	"rapid-admin/internal/pkg/emailx"
	"rapid-admin/internal/pkg/eventx"
	"rapid-admin/internal/pkg/idempotentx"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/jobs"
	"rapid-admin/internal/pkg/kms"
	"rapid-admin/internal/pkg/kvstore"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/metricsx"
	"rapid-admin/internal/pkg/msn"
	"rapid-admin/internal/pkg/msn/aliyun"
	"rapid-admin/internal/pkg/msn/console"
	"rapid-admin/internal/pkg/notifyx"
	"rapid-admin/internal/pkg/otelx"
	"rapid-admin/internal/pkg/piiaudit"
	"rapid-admin/internal/pkg/push"
	"rapid-admin/internal/pkg/pushx"
	"rapid-admin/internal/pkg/slowquery"
	"rapid-admin/internal/pkg/smsaudit"
	"rapid-admin/internal/pkg/smslimit"
	"rapid-admin/internal/pkg/smstpl"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/pkg/validatorx"
	"rapid-admin/internal/repository"
	"rapid-admin/internal/service"
)

var cfgPath = flag.String("c", "configs/config.yaml", "config file path")

func main() {
	flag.Parse()

	// 1. 配置
	if err := config.Load(*cfgPath); err != nil {
		panic(fmt.Errorf("load config: %w", err))
	}
	cfg := config.Get()

	// 1.5 路径自检 —— 先于 logger 初始化直写 stdout。
	//     必须在 logger.New 之前:logger 的 stdout core 只在 env=dev/test 挂载,
	//     而"数据/日志究竟落在哪个绝对路径"正是出问题时最该第一眼看到的东西。
	//     同一份内容稍后由 logResolvedPaths 写进 info.log,控制台 + 日志双份留存。
	printResolvedPaths(cfg)

	// 1.6 Swagger 文档端点的鉴权结论 —— **先于 logger 初始化**,所以用 panic 报错。
	//
	// ⚠️ **为什么在这里就 fail-fast** —— mode=basic 却没配凭据,是"以为有鉴权
	//   其实没有"的典型形态:服务能起、能访问、文档也在,但谁都能看。
	//   这种洞必须在启动时就炸掉,而不是等到上线后被人翻到。
	//
	// ⚠️ **ResolveAuth 是幂等的** —— 真正解析出来的密码会在第 9 步
	//   router.Register 里被复用(那里再调一次不会生成新密码),
	//   所以下面第 9.2 步日志里印的密码就是生效的那个。
	if err := cfg.Swagger.ResolveAuth(); err != nil {
		panic(fmt.Errorf("swagger auth config: %w", err))
	}

	// 2. 日志(按天分目录 + info/error 分文件)
	log, err := logger.New(logger.Config{
		Level:      cfg.Logger.Level,
		Format:     cfg.Logger.Format,
		Dir:        cfg.Logger.Dir,
		MaxSizeMB:  cfg.Logger.MaxSizeMB,
		MaxBackups: cfg.Logger.MaxBackups,
		MaxAgeDays: cfg.Logger.MaxAgeDays,
		Compress:   cfg.Logger.Compress,
		Env:        cfg.Logger.Env,
	})
	if err != nil {
		panic(fmt.Errorf("init logger: %w", err))
	}
	// ⚠️ M99(2026-10-26):退出时要把日志**刷盘 + 关句柄**,三步缺一不可:
	//
	//  1. `logger.SyncFlush()` —— `log.Sync()` 只刷 **tee 里的 core**
	//     (info/warn/error);audit / access 两个 rotator 是挂在
	//     `zap.Hooks(...)` 上的,**不在 tee 里** → log.Sync() 碰不到它们
	//     → 进程退出时最后几条审计/访问日志可能丢。SyncFlush() 专门兜这两个。
	//  2. `log.Sync()` —— 刷 tee。
	//  3. `logger.Close()` —— 关掉 rotator 持有的文件句柄。
	//     不关的话 Windows 上日志文件被锁住,目录删不掉、也没法归档。
	//
	// 放在一个 defer 里按顺序执行(而不是三个 defer):LIFO 会把顺序搞反,
	// 而且分三个 defer 时"关句柄"很可能抢在"刷盘"前面跑。
	defer func() {
		if err := logger.SyncFlush(); err != nil {
			// 刷盘失败不能拦住退出流程,只记一条
			log.Warn("logger: SyncFlush 失败", zap.Error(err))
		}
		_ = log.Sync()
		if err := logger.Close(); err != nil {
			log.Warn("logger: Close 失败", zap.Error(err))
		}
	}()
	logger.Default = log
	zap.ReplaceGlobals(log)

	// 2.1 路径自检 —— 同一份内容写进 info.log,与上面控制台那份双份留存
	logResolvedPaths(log, cfg)

	// 2.2 验证码开关自检 —— 关掉是安全策略变更,必须让运维在日志里看得见,
	//     否则"忘了自己关过"会以一个没人注意的静默状态长期跑在生产上。
	//     双输出理由同 2.1:生产 env 下 logger 的 stdout core 不挂载,
	//     只写日志的话本地开发反而看不到。
	//     两个开关分别提示:它们的关闭后果完全不同,混成一条会让人误判风险。
	switch {
	case !cfg.Captcha.LoginEnabled && !cfg.Captcha.SmsSendEnabled:
		msg := "captcha: 验证码全部关闭 —— 4 个 public 写接口均不再校验," +
			"前端不显示验证码框。/public/auth/sms/send 的防刷量闸门已失效," +
			"仅剩该接口自身 1 次/60 秒/IP 限流。生产环境请确认这是有意为之。"
		log.Warn(msg)
		fmt.Fprintln(os.Stdout, "\n[警告] "+msg)
	case !cfg.Captcha.LoginEnabled:
		log.Info("captcha: 登录验证码已关闭(密码登录/短信登录/短信重置密码)," +
			"发短信闸门保持开启")
	case !cfg.Captcha.SmsSendEnabled:
		msg := "captcha: 发短信验证码已关闭 —— /public/auth/sms/send 的防刷量闸门失效," +
			"仅剩该接口自身 1 次/60 秒/IP 限流。登录验证码仍保持开启。生产环境请确认这是有意为之。"
		log.Warn(msg)
		fmt.Fprintln(os.Stdout, "\n[警告] "+msg)
	default:
		log.Info("captcha: 验证码全部开启(4 个 public 写接口全部校验)")
	}

	// 3. MySQL / SQLite(由 cfg.Database.Driver 决定)
	db, err := repository.NewDB(cfg.Database, cfg.MySQL)
	if err != nil {
		log.Fatal("init db failed", zap.Error(err))
	}

	// §1.8 #14 P1 慢 SQL 报警(GORM callback)
	//   - 装 5 个 op(query/create/update/delete/row)的 before/after 钩,统计耗时
	//   - 默认阈值 500ms,超阈打 warn log + 计数器 +1(app_slow_sql_total{table,op,is_error})
	//   - OnSlow 钩子留接口,业务可接 Sentry/告警 webhook
	//
	// 2026-11-03:OnSlow 同时落 sys_system_log(审计中心「系统日志」)。
	// ⚠️ **间接层**:Install 在 service registry 建出来**之前**跑,拿不到
	// svc.Audit —— 所以这里闭包引用一个占位变量,registry 建好后回填。
	// 慢 SQL 回调只在请求进来后才触发,那时回填早已完成,无竞态。
	var systemLogSink atomic.Value // 存 middleware.SystemLogWriter
	slowToSystemLog := func(e slowquery.Entry) {
		// ⚠️ 传了自定义 OnSlow 后 defaultOnSlow 不再跑,warn 日志在这里补
		//(zap 轨照旧,不能因为落库就丢了文件日志)
		fields := []zap.Field{
			zap.String("table", e.Table),
			zap.String("op", e.Op),
			zap.Duration("duration", e.Duration),
			zap.Int64("rows", e.Rows),
		}
		if e.Error != "" {
			fields = append(fields, zap.String("error", e.Error))
		}
		log.Warn("slow sql detected", fields...)
		if w, ok := systemLogSink.Load().(middleware.SystemLogWriter); ok && w != nil {
			level := "warn"
			if e.Error != "" {
				level = "error"
			}
			w.RecordSystemLog(&service.SystemLogEntry{
				Level:      level,
				Source:     "slow_sql",
				Path:       e.Table,
				Message:    fmt.Sprintf("慢 SQL: %s %s 耗时 %dms", e.Table, e.Op, e.Duration.Milliseconds()),
				Detail:     e.SQL,
				DurationMs: e.Duration.Milliseconds(),
			})
		}
	}
	if err := slowquery.Install(db, slowquery.Config{
		Threshold:  500 * time.Millisecond,
		Logger:     log,
		LogRawSQL:  false,
		SampleRate: 1.0,
		OnSlow:     slowToSystemLog,
	}); err != nil {
		log.Warn("slowquery install failed; 慢 SQL 监控未启用", zap.Error(err))
	}

	// 审计中心「数据变更」自动写入器(GORM callback,白名单制)
	//   - 业务主数据 + 权限主体的 create/update/delete 自动落 sys_data_change_log
	//   - 操作人从 service ctx 取(Auth 中间件 WithCaller 注入);后台写记 system
	//   - 与业务写同一事务:回滚时审计行一起消失(证据一致性)
	//   - ⚠️ 只装在**服务运行时**的 db 上;cmd/migrate 是独立 db 实例,建表灌种子不被审计
	if err := auditx.Install(db, auditx.Config{
		LookupCaller: func(ctx context.Context) (uint64, string) {
			if c, ok := service.FromContext(ctx); ok {
				return c.UserID, c.Username
			}
			return 0, ""
		},
		Logger: log,
	}); err != nil {
		log.Warn("auditx install failed; 数据变更审计未启用", zap.Error(err))
	}

	// 4. Redis(可选:redis.addr 为空时跳过,适用于本地零依赖 dev 模式)
	if cfg.Redis.Addr != "" {
		if _, err := repository.NewRedis(cfg.Redis); err != nil {
			log.Fatal("init redis failed", zap.Error(err))
		}
	} else {
		log.Info("redis skipped (empty addr)")
	}

	// 5. Casbin enforcer
	if err := casbinx.Init(db, cfg.Casbin.ModelPath); err != nil {
		log.Fatal("init casbin failed", zap.Error(err))
	}

	// 5.0 validator 框架级自定义规则注册(§1.11 #6 P1)
	//   - cn_phone / idcard 在 validatorx/register.go 内,Init 阶段调一次
	//   - 业务模块可在自己的 init() 里追加自定义规则
	//
	// ⚠️ M112:注册失败现在**返回 error**(以前是 `_ =` 直接丢掉)。
	//   注册失败 ⇒ 用到该规则的 DTO 会校验失败("Undefined validation function"),
	//   必须让人看见 —— 但**不 Fatal**:没用到这两个规则的接口不受影响。
	if err := validatorx.RegisterBuiltins(); err != nil {
		log.Error("validator 内置规则注册失败(用到 cn_phone / idcard 的接口会校验失败)", zap.Error(err))
	}

	// 5.0.1 分布式 ID 机号 + 盲索引 key 来源(打出来便于对账)
	//
	// ⚠️ M112:这两条以前**完全没有日志**,出问题时无从查起:
	//   - sonyflake 机号:库默认取"私网 IPv4 低 16 位" ⇒ 跨网段 / 同节点副本
	//     会撞号 ⇒ 主键重复。现在自己算(私网 IP ^ 主机名 ^ pid),并打出来。
	//   - 脱敏盲索引 key:以前固定用仓库里的公开占位值 ⇒ 生产上盲索引等于
	//     明文哈希。现在优先用 crypto.blind_key,打出来确认用的是哪个。
	log.Info("sonyflake 机号",
		zap.Uint64("machine_id", sonyflake.MachineID()),
		zap.String("machine_id_source", sonyflake.MachineIDSource()))
	if src := desensitize.BlindKeySource(); src != desensitize.BlindSourceConfig {
		log.Warn("手机号盲索引未使用配置密钥(crypto.blind_key):"+
			"当前来源为公开占位值/环境变量,盲索引可被反查 —— 生产必须配置 crypto.blind_key",
			zap.String("blind_key_source", src))
	} else {
		log.Info("手机号盲索引已使用配置密钥(crypto.blind_key)", zap.String("blind_key_source", src))
	}

	// 5.1 OpenTelemetry trace 导出(可选 — otel.enabled=false 时不初始化)
	//   enabled 关闭 → noopShutdown,defer 仍可统一调,不影响主流程
	var otelShutdown otelx.ShutdownFunc = otelx.InitNoop()
	if cfg.OTel.Enabled {
		otelShutdown, err = otelx.Init(context.Background(),
			cfg.OTel.ServiceName,
			cfg.OTel.Env,
			cfg.OTel.Protocol,
			cfg.OTel.Endpoint,
			cfg.OTel.Insecure,
			cfg.OTel.SampleRatio,
		)
		if err != nil {
			log.Fatal("init otel failed", zap.Error(err))
		}
	} else {
		log.Info("otel disabled (otel.enabled=false)")
	}

	// 5.2 Prometheus metrics(§1.8 #8 P0)
	//   enabled 关闭 → Init 内部空实现,/metrics 端点也不挂载
	//   Init 后必须传入 appName/version/env → 写到 app_info 标签,方便 PromQL 关联
	metricsx.Init(cfg.Metrics, cfg.System.AppName, cfg.System.AppVersion, cfg.OTel.Env)
	if cfg.Metrics.Enabled {
		log.Info("metrics enabled", zap.String("path", cfg.Metrics.Path))
		// 5.2.1 cachex 缓存指标(§1.7 #9 P2)
		//   4 个 collector 注册到 metricsx 全局 registry,/metrics 端点自动暴露
		//   app_cache_ops_total / app_cache_op_duration_seconds / app_cache_l1_size / app_cache_loader_total
		cachex.InitMetrics(metricsx.Registry())
		// 5.2.2 cronx 定时任务指标(M67)
		//   app_job_runs_total / app_job_duration_seconds /
		//   app_job_lock_skipped_total / app_job_degraded_total
		//   后两个是排障主力:lock_skipped 持续涨 = 去重生效(多副本下正常);
		//   degraded > 0 = Redis 掉了,多副本会重复执行。
		cronx.InitMetrics(metricsx.Registry())
	}

	// 5.3 Idempotency-Key 框架级处理(§1.3 #17 P1)
	//   锁 30s 防止同 key 并发执行,结果缓存 24h
	//   rdb 未配 → manager 内部判 nil,中间件 no-op
	idempotentx.Init(repository.GetRedis(), 30*time.Second, 24*time.Hour)
	log.Info("idempotency manager initialized")

	// 5.4 PII 解密审计(§1.5 #5 P0)
	//   异步批量写 sys_pii_access_log,业务代码调 cipher.DecryptStringAudited 自动记
	//   Init 内部 nil-safe:db 未连上时 no-op,部署形态支持关停 PII 审计
	piiaudit.Init(repository.GetDB())
	defer piiaudit.Shutdown()
	log.Info("piiaudit recorder initialized")

	// 5.5 短信发送审计(§1.9 #7 P1)
	//   Wrap 装饰器自动计 send 耗时 + 写 sys_sms_send_log
	//   Init 内部 nil-safe:db 未连上时 no-op
	smsaudit.Init(repository.GetDB())
	defer smsaudit.Shutdown()
	log.Info("smsaudit recorder initialized")

	// §1.5 #8 P1 密钥使用审计
	cryptaudit.Init(repository.GetDB())
	defer cryptaudit.Shutdown()
	log.Info("cryptaudit recorder initialized")

	// §1.5 #6+#7+#10 P1 密钥分级 + 轮转 + KMS 抽象
	//   - tier:PII(手机/邮箱/身份证)/Secret(系统密钥)/Normal(普通)
	//   - 每个 tier 可配多把 key(轮转期:1 active + N-1 旧 key 仅解)
	//   - KMS_PROVIDER 选型:local(env,默认)/ aliyun / tencent / vault
	//   - local 模式:env 格式 KEY_<TIER>_V<N>=hex32B + KEY_<TIER>_ACTIVE=V<N> + KEY_LEGACY
	//   - 切换 KMS 只改 KMS_PROVIDER 即可,业务层零改动
	kmsCfg := kms.Config{
		Provider:  cfg.KMS.Provider, // 待 cfg.KMS 加进来;先用空(local)
		Endpoint:  cfg.KMS.Endpoint,
		AccessKey: cfg.KMS.AccessKey,
		SecretKey: cfg.KMS.SecretKey,
		Region:    cfg.KMS.Region,
		KeyPrefix: cfg.KMS.KeyPrefix,
	}
	kmsMgr, err := kms.NewManager(context.Background(), kmsCfg)
	if err != nil {
		log.Warn("kms init failed; tiered encrypt/decrypt disabled", zap.Error(err))
	} else {
		defer kmsMgr.Close()
	}

	// §1.5 #1+#2+#3 字段级加密器 + 盲索引器(从配置装载)
	//   - crypto.field_key:AES-256-GCM 32 字节(base64 或 32 字节原文)
	//   - crypto.blind_key:HMAC-SHA256 盲索引 key(>=16 字节)
	//
	// ⚠️ M100(2026-10-26):**这段以前根本不存在**。
	//   配置里有 crypto.field_key / crypto.blind_key(生产由 K8s Secret 注入),
	//   config.CryptoConfig 也解出来了 —— 但**全仓没有任何代码读它们**:
	//   `crypto.Field()` / `crypto.Blind()` 从来没被初始化过,
	//   业务代码一旦调就是 panic("not initialized")。
	//   清单上 §1.5 #3「全局 cipher + 盲索引注入」标着 ✅,
	//   实际上**从来没注入过** —— 一个"写完了但从没跑过"的 ✅。
	//
	//   配错(长度不对 / base64 非法)→ 打 Error 且**不初始化**:
	//   字段加密保持"未启用",真用到时 crypto.Field() 会 panic,
	//   不会拿着错误的 key 把 PII 加密成解不开的东西。
	//   没配(空串)→ 不算错误,保持未启用(本期业务线还没接 PII 加密)。
	fieldCryptoErr := initFieldCrypto(cfg.Crypto)
	switch {
	case fieldCryptoErr != nil:
		log.Error("字段加密 / 盲索引未启用:密钥配置有误", zap.Error(fieldCryptoErr))
	case crypto.TryField() == nil && crypto.TryBlind() == nil:
		log.Warn("字段加密 / 盲索引未配置(crypto.field_key / crypto.blind_key 为空);PII 字段将以明文存取")
	default:
		log.Info("字段加密 / 盲索引已装载",
			zap.Bool("field_cipher", crypto.TryField() != nil),
			zap.Bool("blind_index", crypto.TryBlind() != nil))
	}

	// 5.6 SMS 限流(per phone + per scene)—— §1.9 #8 P1
	//   Redis 实现:冷却期 60s + 1 分钟窗口 + 1 天窗口
	//   rdb nil 时内部 no-op,放行(DB 层 sys_sms_code 兜底)
	smslimit.Init(repository.GetRedis(), smslimit.Config{
		PerMinute:    cfg.SMS.RateLimit.PerMinute,
		PerDay:       cfg.SMS.RateLimit.PerDay,
		CooldownSecs: cfg.SMS.RateLimit.WindowSeconds,
	})
	// ⚠️ 这三个值原先被打上 "permnotify redis subscriber ready" 的标签记在启动
	// 日志里(复制粘贴时串了行),删掉那条日志时一并搬回正确的位置。
	log.Info("smslimit ready",
		zap.Int("per_minute", cfg.SMS.RateLimit.PerMinute),
		zap.Int("per_day", cfg.SMS.RateLimit.PerDay),
		zap.Int("cooldown", cfg.SMS.RateLimit.WindowSeconds))

	// 5.7 缓存层装配(§1.7 #5/#7/#8)
	//   cachex.Build 是"要不要连 Redis"的**唯一决策点**,与 kvstore.Resolve、
	//   auth.revocation_mode 同构。装配结果登记成全局单例,业务侧直接
	//   cachex.Default() 取,不做逐个 service 的 DI 改造。
	//
	//   之前这里只有一句 WarmAll(..., nil, ...):框架接好了但 warmers 传空,
	//   全仓 NewTwoLevel / GetOrLoad 零调用 —— 线上跑着一套没人用的缓存而没人知道。
	//   现在把"到底有没有缓存"打成一行启动日志。
	cachePool, cacheErr := cachex.Build(cachex.Config{
		Mode:    cfg.Cache.Mode,
		Prefix:  cfg.Cache.Prefix,
		L1Cap:   cfg.Cache.L1Cap,
		L1TTL:   cfg.Cache.L1TTL,
		NullTTL: cfg.Cache.NullTTL,
		Jitter:  cfg.Cache.Jitter,
	}, repository.GetRedis())
	if cacheErr != nil {
		// 不 fail-fast:缓存是加速器不是依赖,配错顶多没缓存,不该拦住启动。
		// 但必须 loud —— "我配了 X,系统跑的是 Y"是最难查的一类配置问题。
		log.Warn("cachex: 装配失败,已降级为 auto", zap.Error(cacheErr))
		cachePool, _ = cachex.Build(cachex.Config{Mode: cachex.ModeAuto}, repository.GetRedis())
	}
	cachex.SetDefault(cachePool)
	log.Info("cachex ready", cachex.Describe(cachePool, cfg.Cache.Mode)...)
	cachex.Verify(context.Background(), cachePool)

	// 5.7.1 缓存预热
	//   服务起来后并发灌热点数据,让命中率从 t=0 就是有效值。
	//   warmers 由业务模块注册;目前为 nil(框架就绪,不预热不报错)。
	warmCtx, warmCancel := context.WithTimeout(context.Background(), 60*time.Second)
	warmStats := cachex.WarmAll(warmCtx, cachex.New(repository.GetRedis()), nil,
		cachex.WithWarmConcurrency(10),
		cachex.WithWarmTimeout(30*time.Second),
	)
	warmCancel()
	log.Info("cachex warm done (no warmers registered yet — framework ready)",
		zap.Int("total", warmStats.Total),
		zap.Int("success", warmStats.Success),
		zap.Int("errored", warmStats.Errored),
		zap.Duration("duration", warmStats.Duration),
	)

	// 5.79 短信模板审核流运行期装配(M115)
	//
	// ⚠️ **必须早于 5.8(cronx)** —— 模板审核状态同步任务要在
	//	cronx.New / Start **之前**注册(cronx 的注册表是启动时一次性快照,
	//	晚注册的任务永远没有 entry,NextRunAt 是空的,而且**没有任何报错**)。
	//
	// ⚠️ **配置中心 service 提到这里建** —— 它原来在 6.5 才建,
	//	而模板登记册要读"生效的"模板号(配置中心那份)。提早建是为了让
	//	配置解析器在装配序列里只有**一份**来源 —— 三处各写一遍解析规则,
	//	迟早漂移(后台页面按 A 判、发送链路按 B 判,运营以为自己停了模板,
	//	结果短信照发)。
	ccSvc := service.NewConfigCenterService(db)
	smsTplRuntime := smstpl.NewRuntime(smstpl.RuntimeOptions{
		Store: smstpl.NewSQLStore(db),
		Resolve: func(ctx context.Context) (config.SMSConfig, error) {
			return ccSvc.ResolveSMSConfig(ctx, cfg.SMS)
		},
		NewFetcher: buildSmsTemplateFetcher,
		Policy:     smstpl.ParsePolicy(cfg.SMS.TemplateGuard),
	})
	// ⚠️ **启动时必须装载登记册** —— 不装的话,进程重启后登记册是空的,
	//	人工停用的模板会被下一轮同步"复活"(服务商说通过 → approved),
	//	硬规则 2(人工停用的,同步不能自动复活)在重启后就失效了。
	//
	// ⚠️ **装载失败不退出进程** —— 但要大声记一笔:
	//	登记册空了 ⇒ 守卫放行一切 ⇒ 人工停用形同虚设,
	//	这是"静默失去一个安全开关",必须在日志里看得见。
	if err := smsTplRuntime.Load(context.Background()); err != nil {
		log.Error("smstpl: 装载模板登记册失败,人工停用状态可能在下次同步时被覆盖",
			zap.Error(err))
	}

	// 5.8 定时任务调度(M67)+ 内置任务注册(M70)
	//   任务只在代码里 cronx.Register,DB(sys_job)只存启停 + cron 覆盖值。
	//   cronx.ResolveLocker 是"要不要连 Redis"的**唯一决策点**,与 cachex.Build
	//   / kvstore.Resolve / auth.revocation_mode 同构。
	//
	//   两个错误分级不同,别写成一样:
	//   - 锁模式配错(如 lock_mode=redis 却没配 Redis)→ Fatal。
	//     这是"多副本下会重复执行"的静默事故,不能带着问题上线。
	//   - 加载/注册失败 → Warn 后继续。定时任务停摆比频率没生效更难发现。
	//
	//   ⚠️ RegisterAll 必须在 cronx.New / Start **之前**调,而且**只调一次**:
	//   cron.Register 撞 key 会 panic,而 Start() 才是把注册表写进 robfig 的时机 ——
	//   晚注册的任务永远没有 entry,NextRunAt 是空的,而且没有任何报错。
	//   放在 New 之前是为了让"注册表为空导致 0 个任务"在启动日志里一眼可见。
	jobs.RegisterAll(repository.GetDB(), cfg.Job)

	// M88 审批超时巡检(§9.4)
	//
	// ⚠️ **必须在 cronx.New / Start 之前注册** —— cronx 的注册表是"启动时
	//   一次性快照"(scheduler.go:227 的 registeredKeys()),晚注册的任务
	//   永远没有 entry,NextRunAt 是空的,而且**没有任何报错**。
	//
	// ⚠️ **这里自己 new 一个 inbox.Manager,不用下面 6.6 的 inboxMgr** ——
	//   6.6 在装配序列的更后面,而巡检必须先注册。`inbox.Manager` 只有一个
	//   *gorm.DB 字段,是无状态的,建两个没有区别(不是"两套消息中心")。
	//
	// ⚠️ **只告警,绝不自动改单** —— 见 approval_scan.go 的包注释。
	service.RegisterApprovalScanJob(db, inbox.New(db))

	// M115 短信模板审核状态同步(每 10 分钟)
	//
	// ⚠️ **必须在 cronx.New / Start 之前注册** —— 同上,cronx 的注册表是
	//   "启动时一次性快照",晚注册的任务永远没有 entry,NextRunAt 是空的,
	//   而且**没有任何报错**。
	//
	// ⚠️ **无条件注册,不按 provider 判断** —— provider 是运行时可切的
	//   (配置中心面板上把 aliyun 切成 console 立刻生效),
	//   按当前 provider 决定注册与否的话,切换的那一刻任务就没了。
	//
	// ⚠️ **守卫依赖这个任务的时效性** —— 登记册里的状态如果永远停在
	//   "上次同步时的样子",模板被服务商驳回之后平台会一直按旧状态放行。
	service.RegisterSmsTemplateSyncJob(smsTplRuntime)

	jobLocker, lockerErr := cronx.ResolveLocker(cfg.Job.LockMode, repository.GetRedis(), cronx.DefaultLockKeyPrefix)
	if lockerErr != nil {
		log.Fatal("cronx: 锁模式装配失败,拒绝启动", zap.String("lock_mode", cfg.Job.LockMode), zap.Error(lockerErr))
	}
	jobSched, schedErr := cronx.New(cronx.Config{
		Enabled:         cfg.Job.Enabled,
		TimeZone:        cfg.Job.TimeZone,
		LockMode:        cfg.Job.LockMode,
		LockTTL:         cfg.Job.LockTTL,
		LockRenew:       cfg.Job.LockRenew,
		DefaultTimeout:  cfg.Job.DefaultTimeout,
		ShutdownTimeout: cfg.Job.ShutdownTimeout,
		// M73:任务跑完推事件(映射见 jobs_notify.go)。传 nil 时 cronx 只多一次
		// nil 判断,不会有人收到通知 —— 不接推送的部署照常工作。
		OnFinish: jobEventPublisher(log),
		// 刻意**不**在这里传 HistoryRetention*:清理执行记录是
		// framework.job_history_purge 任务的职责(它上面已注册),不是调度器的内建行为
	}, cronx.NewStore(repository.GetDB()), jobLocker)
	if schedErr != nil {
		log.Fatal("cronx: 调度器装配失败,拒绝启动", zap.Error(schedErr))
	}
	cronx.SetDefault(jobSched)
	if err := jobSched.Start(context.Background()); err != nil {
		// 启动失败不退出进程:此时 HTTP 已经起来了(装配在路由注册之前),
		// 让运维能进后台看到"定时任务没起来"这条横幅,比直接崩溃好排查。
		log.Error("cronx: 启动失败,定时任务未运行", zap.Error(err))
	}

	// ⚠️ **确认短信模板同步任务真的挂上了** —— cronx 的注册表是"启动时
	// 一次性快照",注册晚于 cronx.New 的任务永远没有 entry(NextRunAt 是零值),
	// 而且**没有任何报错**。
	//
	// 这个任务不跑的后果比别的任务严重:它是**守卫的数据源** ——
	// 登记册里的审核状态停在"上次同步时的样子",模板被服务商驳回之后
	// 平台会一直按旧状态放行 ⇒ 静默失去一个安全开关。
	//
	// ⚠️ **只在"整体开关 + 这个任务自己都开着"时才查** ——
	// 人为停用的任务没有 entry 是正常的,不该喊。
	if cfg.Job.Enabled {
		if eff, err := jobSched.Get(service.KeySmsTemplateSync); err == nil &&
			eff.EnabledEffective() && jobSched.NextRunAt(service.KeySmsTemplateSync).IsZero() {
			log.Error("cronx: 短信模板审核状态同步任务没有挂上调度器"+
				"(注册必须早于 cronx.New),模板审核状态将不再更新,"+
				"守卫会一直按过期状态放行", zap.String("job_key", service.KeySmsTemplateSync))
		}
	}

	// §2.2 #9 P1 —— 事件推送通道(Redis pub/sub)
	//   必须早于任何一次 Publish 调用装配好 —— permnotify / inbox / 未来的
	//   cronx 都经 pkg/push 投递。
	//   Redis 为 nil 时 push.Available() 返 false,发布端静默丢弃,
	//   /events 连接会发 degraded 让前端切轮询(**不静默**)。
	push.InitRedis(repository.GetRedis())
	if push.Available() {
		log.Info("push 事件推送通道已就绪(Redis pub/sub)")
	} else {
		log.Warn("push 事件推送通道不可用(Redis 未连接),SSE 将降级为轮询")
	}

	// M173 领域事件 outbox 投递器
	//
	//   事件先落 sys_event_outbox(publisher 落表,commit 即发布成功),
	//   这里是"落表 → 推给 push 通道"的那一环:轮询到期行、失败退避重试、
	//   耗尽进死信(见 internal/pkg/eventx 的包注释)。
	//
	//   ⚠️ **deliver 函数里自己补 At 时间戳** —— 这里直调 transport
	//   (拿得到 error,重试才判得出来),不经过 push.Publish 的顶层封装,
	//   那个"At==0 补时间戳"的兜底就不在链上了。
	//
	//   ⚠️ **transport 用 NewRedisTransport 单独建,不复用 push 包内部那个**:
	//   push.InitRedis 注入的实例拿不出来,而投递器需要 Publish 的返回值 ——
	//   redisTransport 无状态,建第二个实例没有成本,也不是"两套通道"
	//   (同一个 Redis、同一组频道,只是调用点不同)。
	//
	//   ⚠️ **Redis 没连不挡启动** —— deliver 返回 ErrTransportUnavailable,
	//   投递器对它退避重试但不烧尝试次数;Redis 恢复后积压事件自动补投。
	outboxTransport := push.NewRedisTransport(repository.GetRedis())
	outboxDeliverer := eventx.NewDeliverer(db, func(ctx context.Context, m eventx.Message) error {
		if !outboxTransport.Available() {
			return eventx.ErrTransportUnavailable
		}
		return outboxTransport.Publish(ctx, []string{m.Topic}, push.Event{
			Topic:   m.Topic,
			Event:   m.EventName,
			Payload: json.RawMessage(m.Payload),
			At:      time.Now().UnixMilli(),
		})
	}, eventx.DelivererConfig{})
	deliverCtx, deliverCancel := context.WithCancel(context.Background())
	defer deliverCancel()
	go func() {
		outboxDeliverer.Run(deliverCtx)
		log.Info("eventx outbox 投递器已停止")
	}()
	log.Info("eventx outbox 投递器已启动", zap.Duration("interval", time.Second))
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			log.Warn("otel shutdown error", zap.Error(err))
		}
	}()

	// 6. JWT(三受众)
	jwtMulti := auth.NewMulti(
		cfg.JWT.AdminSecret, cfg.JWT.OwnerSecret, cfg.JWT.SupplierSecret,
		cfg.JWT.Issuer, cfg.JWT.AccessTTL, cfg.JWT.RefreshTTL, cfg.JWT.ShortTTL,
	)
	auth.SetDefault(jwtMulti)

	// §1.2 #10 P1 —— token 吊销存储
	//   - mode=redis:RedisStore + revocache 跨实例广播(IsRevoked 本地 O(1) 优先)
	//   - mode=auto 且无 Redis:退 MemoryStore,**仍然能吊销**
	//   - mode=memory:显式选内存(仅调试)
	//
	// 以前这里是 if rdb != nil { ... } else { log.Info("blacklist skipped") },
	// 而四处调用点又各写了一层 if bl != nil —— 两层短路之后,登出/改密码/
	// 切换身份的吊销从来没有执行过,而且不报错。现在要么有存储,要么启动失败。
	revocation, err := auth.ResolveRevocation(cfg.Auth.RevocationMode, repository.GetRedis())
	if err != nil {
		// 配置错误不该带着"降级"的样子跑起来:多副本下内存吊销等于没有吊销
		log.Fatal("revocation store 初始化失败", zap.Error(err))
	}
	auth.SetDefaultRevocation(revocation)

	// revocache 只在 Redis 模式下有意义(跨实例广播),内存模式没有"别的实例"
	if rs, ok := revocation.(*auth.RedisStore); ok {
		if rdb := repository.GetRedis(); rdb != nil {
			rc := revocache.New(rdb)
			rc.Start(context.Background())
			defer rc.Stop()
			rs.SetRevocache(rc)
			log.Info("revocation: redis + revocache ready",
				zap.String("mode", cfg.Auth.RevocationMode),
				zap.String("channel", revocache.ChannelName))
		}
	}
	if _, degraded := revocation.(*auth.MemoryStore); degraded {
		// 结构化降级告警:部署方 grep 这个 key 就能确认自己跑在内存吊销上
		log.Warn("revocation: 内存模式,吊销仅在本实例有效;多副本部署请配 auth.revocation_mode=redis",
			zap.String("revocation_mode", "memory"),
			zap.Bool("auth_revocation_degraded", true))
	}

	// 限流 / 登录锁定的存储 —— 与吊销同一套思路,但**不同开关**。
	//
	// 分开的理由是后果等级不同:吊销降级 = 安全事故,漏配该启动失败;
	// 限流降级 = 阈值不准,本地/预发用内存完全合理。合成一个开关的话,
	// 本地想用内存限流调试就必须连 Redis,或者为了调试把吊销一起降级 ——
	// 后者不可接受。
	//
	// 但"有没有 Redis"只在 Resolve 里判断这一次:ratelimit / loginlock
	// 之后只问 kvstore.DefaultStore(),不再各自写 `if repository.RDB == nil`。
	kv, err := kvstore.Resolve(cfg.KV.Mode, repository.GetRedis())
	if err != nil {
		log.Fatal("kv store 初始化失败", zap.Error(err))
	}
	kvstore.SetDefault(kv)
	log.Info("kvstore ready", zap.String("mode", cfg.KV.Mode), zap.String("backend", kv.Backend))
	if kv.Backend == "memory" {
		// 内存限流**不是没有限流**:阈值照样生效,只是多副本各数各的
		// (实际阈值 = 配置值 × 副本数),且重启后计数归零。
		log.Warn("kvstore: 内存模式,限流/登录锁定仅在本实例有效;多副本部署请配 kv.mode=redis",
			zap.String("kv_mode", "memory"),
			zap.Bool("kv_degraded", true))
	}

	// 6.5 SMS sender(aliyun / console fallback)
	//   provider 选型放在 main 而非 msn 包内, 避免 msn -> aliyun -> msn 的 import cycle
	//   外面再套 smsaudit.Wrap:每次 send 耗时 + 写 sys_sms_send_log(§1.9 #7)
	//
	// ⚠️ **配置中心的 aliyun.sms.* 组是这个 sender 的运行时来源**(C6,2026-10-29)
	//   配置文件打底 + 配置中心覆盖,合并规则见 service.ResolveSMSConfig。
	//   ⇒ 面板上换密钥/换签名/换模板/切 provider,改完即生效,不用重启。
	//   (此前这 9 项**只存不读** —— 面板改了没反应,而且不报错。)
	//
	// ⚠️ **ccSvc 在 5.79 就建好了** —— 模板审核流的登记册也要读
	//	"生效的"模板号,同一个配置中心实例,不能建两份。
	//
	// ⚠️ **守卫接在 sender 内部(不是外面再包一层)** ——
	//	守卫判据是"这次要发的模板号",它必须和真正下发的那次用**同一份配置**;
	//	在外面包一层就要多解析一次配置,中间配置一变就会"按 A 号的状态放行 B 号"。
	//
	// ⚠️ **守卫在 smsaudit 里面** —— 被拦下的这次发送同样要落 sys_sms_send_log
	//	(带错误原因),否则"发不出去"在审计表里查不到。
	rawSmsSender := buildSmsSender(cfg.SMS, ccSvc.ResolveSMSConfig, smsTplRuntime.Guard())
	smsSender := smsaudit.Wrap(rawSmsSender)

	// 6.6 §1.9 #11+#12 P1 —— 站内信 + 通知中心 Manager
	//   - 未读推送经 pkg/push 走 Redis pub/sub;Redis 不可用时推送静默丢弃,
	//     前端轮询 GET /inbox/unread-count 兜底
	//   - 业务模块调 registry.InboxNotifier.Notify(ctx, msg) 投递
	inboxMgr := inbox.New(db)
	log.Info("inbox manager ready")

	// 6.65 §1.9 #9+#10 P2 —— 邮件 / 推送 通道装配
	//
	// ⚠️ **provider 为空 = 这条通道不存在**(不是"配好了但关着")——
	//   理由见 internal/config/email.go 的注释:邮件是额外交付,
	//   没配就是没配,悄悄落到 console 会让运维以为发出去了。
	//
	// ⚠️ **emailx / pushx 在此之前零生产调用方** —— 包写完了、单测也有了,
	//   但没有任何地方构造过它们。清单里那两行"✅ P2"说的是"代码完备",
	//   不是"能力已通"。这里就是那个缺口。
	emailSender := buildEmailSender(cfg.Email)
	pushSender := buildPushSender(cfg.Push)

	// 6.66 通知分发器:站内信(记录) + 额外交付通道(邮件)
	//
	// ⚠️ **分发器里目前只有邮件一条通道** —— 推送通道**故意没挂**:
	//   推送要的是**设备令牌**(APNs device token / 厂商 registration id),
	//   全仓没有这张登记册,挂了也是每条都 skip。
	//   pushSender 仍然装配进 Registry(业务侧拿到 App 端令牌登记之后
	//   直接用),但**不要**为了"看起来接上了"就把它注册成分发通道。
	notifier := notifyx.NewFanout(
		inboxMgr,
		buildNotifierDispatcher(emailSender, cfg.Email),
		notifyx.NewDBResolver(db),
	)

	// 6.7 文件系统(M80):provider 选型 + 配置映射
	//   ⚠️ **先 checkFileConfig 再建 provider** —— 静态挂载点和公开前缀对不齐时
	//   要在"建了半个存储"之前就停下来,否则会留下一个能写文件但读不出来的现场。
	checkFileConfig(cfg.File)
	fileProv := buildFileProvider(cfg.File, cfg.OSS)
	fileOpt := buildFileOptions(cfg.File)
	// ⚠️ **FileService 先建出来,再进 registry** —— 用户 service 要用它传头像
	// (M124),而 registry 字面量里的字段是**顺序无关**的,没法在字面量里
	// 引用还没声明的变量。
	//
	// ⚠️ **必须是同一个实例**:文件系统的 key 规则 / 配额记账 / 懒建文件夹
	// 都挂在实例上,造第二个会让两处各记各的账。
	fileSvc := service.NewFileService(db, fileProv, fileOpt)

	// 6.75 语音能力(voicex):音色克隆 + 文本合成
	//
	// ⚠️ **共用 fileProv 而不是另建一个 provider** —— 参考音频/试听音频
	//   就是普通对象,key 第一段必须是静态挂载点(path_root),否则
	//   local 直链 404、克隆必挂。voiceKeyPrefix 的 "/voice" 子树只是
	//   命名空间,不是另一套存储。
	//
	// ⚠️ **没配 voice.api_key 不拦启动** —— 克隆接口调用时才显式报错
	//   (与 email provider 空串同一纪律:额外交付,没配就是没配;
	//   但报错必须把"没配"三个字带到调用方)。
	voiceKeyPrefix := strings.Trim(cfg.File.PathRoot, "/") + "/voice"
	// ⚠️ **配置中心覆盖**(voice.* 组)—— 克隆 / 合成每次调用时解析生效配置,
	//   面板上改 api_key / synth_url 改完即生效,不用重启
	//   (合并规则见 service.ResolveVoiceConfig,与 aliyun.sms 同一套纪律)。
	voiceCfgResolve := func(ctx context.Context) (config.VoiceConfig, error) {
		return ccSvc.ResolveVoiceConfig(ctx, cfg.Voice)
	}
	voiceCloneSvc := service.NewVoiceCloneService(db, fileProv, cfg.Voice, voiceKeyPrefix, voiceCfgResolve)
	ttsVoiceSvc := service.NewTtsVoiceService(db, fileProv, cfg.Voice, voiceKeyPrefix, voiceCfgResolve)
	// 识别(asrx)与克隆/合成共用同一份生效配置 —— 面板改 api_key 同样即生效
	asrSvc := service.NewAsrService(cfg.Voice, voiceCfgResolve)

	// 6.8 文件系统自愈(M140)
	//
	// ⚠️ **两件事都不拦启动** —— 头像文件夹叫什么名字、info.json 在不在,
	//   都不影响业务读写(info.json 是**生成物**,DB 才是唯一真相源,
	//   见设计文档 §2.3 D3)。为一个锦上添花的修复拒绝启动是本末倒置。
	//
	// ⚠️ **改名同步做、补 info.json 放后台** —— 改名是一条 UPDATE(快),
	//   而补 info.json 要逐个问存储"在不在"(每个文件夹一次 HEAD),
	//   放同步路径上会把启动拖慢(OSS 下一个文件夹一次往返)。
	//
	// ⚠️ **失败只告警** —— 启动期自愈失败 = 下次启动还会试(幂等),
	//   而把进程卡死在这儿换不来任何东西。
	if n, err := service.NormalizeAvatarFolders(context.Background(), db); err != nil {
		log.Warn("file: 头像文件夹改名失败(不影响业务,下次启动会重试)", zap.Error(err))
	} else if n > 0 {
		log.Info("file: 头像文件夹已改成专属名字", zap.Int("count", n))
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		written, err := service.BackfillFolderInfo(ctx, db, fileProv, fileOpt, 0)
		if err != nil {
			log.Warn("file: 补齐缺失的 info.json 失败(不影响业务,下次启动会重试)", zap.Error(err))
			return
		}
		if written > 0 {
			log.Info("file: 已补齐缺失的 info.json", zap.Int("count", written))
		}
	}()


	// 7.1 通用审批系统(M86)
	//   - resolver 解析审批人(M85),引擎、模板服务共用同一个实例
	//   - handlers 是业务侧 SPI 的注册表(M86-2 装 material 那条)
	//   - publisher 把引擎发的事件投到事件通道(M88)
	//
	// ⚠️ **assertor 用 GenericAssertor** —— 它是纯内存判定(超管直通 /
	// 资源所有人直通 / deny 覆盖 / grant 命中),不碰 DB。
	// 审批详情可见性 = assertor 先判 + 审批域 isParty 后兜底(§8.3),
	// **顺序不能反** —— 反了委托路径就被绕过了。
	approvalResolver := service.NewApprovalResolver(db)
	// ⚠️ **业务侧 SPI 注册表** —— 当前只有 `digital_material` 一条(§14 Q7:
	// 「M86 强制替换 material 路径就是那唯一的验证案例」)。
	// **没注册的 biz_type 建不了草稿**(引擎直接拒),这是有意的:
	// 放过去等于"审批通过了但业务状态永远不变"(R11)。
	approvalHandlers := map[string]service.BizHandler{
		// 动态业务集合(2026-11-03):集合绑定审批模板后记录可送审。
		// biz_type 恒为 "dyn"(不按集合拆,理由见 dyn_approval.go 文件头 ①);
		// handler 四个钩子 no-op —— dyn 记录的数据在表里,审批状态以审批单为准。
		service.DynBizType: service.NewDynApprovalHandler(),
	}
	// ⚠️ **publisher 必须给真实现**(M88)—— 传 nil 的话引擎一条事件都不发,
	//   前端的待办角标永远是 0,而且**没有任何报错**(引擎里那处 nil 判断
	//   就是"不发事件"的意思,不是"发失败")。
	//
	//   它内部用 `context.WithoutCancel` + goroutine + recover(§9.2):
	//   HTTP 请求超时后 ctx 已经死了,拿它写 Redis 直接失败。
	//   Redis 没连时 push.Publish 记日志后丢弃,审批主流程不受影响。
	//
	// ⚠️ **外面再套一层站内信装饰器**(M3)—— 事件照发(Redis 通道完全不受影响),
	//   顺带投一条站内信:新待办投给审批人,结果投给发起人。
	//   **引擎对站内信一无所知**(它只认 ApprovalEvent),这条支路是加在
	//   publisher 上的,不是加在引擎里的。
	//
	//   ⚠️ **inboxMgr 必须在上面建好**(6.6 节)—— 顺序反了这里拿到的是 nil,
	//   站内信会静默地一条都投不出去(而且**不报错**)。
	//
	//   ⚠️ **这里塞的是 notifier(Fanout)而不是 inboxMgr** —— Fanout 的
	//   inner 就是 inboxMgr,站内信照旧落库,只是**顺带多一条邮件通道**。
	//   审批链因此成为 emailx 的**第一个真实生产调用方**(之前它只有单测)。
	approvalEngine := service.NewApprovalEngine(
		db, approvalResolver, approvalHandlers,
		service.NewInboxApprovalPublisher(
			// M173:审批事件走 outbox(先落表后投递,Redis 抖动不再丢事件)
			service.NewApprovalPublisherWithOutbox(db), notifier, db,
		),
	)
	approvalFlowSvc := service.NewApprovalFlowService(db, approvalResolver)
	approvalSvc := service.NewApprovalService(db, approvalEngine, authz.NewGenericAssertor())

	// ⚠️ **数据范围服务只建一个**(M155)——
	//   规则缓存(读路径)在所有使用者之间共享,
	//   建两个就是两套缓存键 ⇒ "改了规则立刻失效"退化成"最多 5 分钟后生效"。
	//
	//   **刻意只取 L2(`cachePool.Safe()`),不取全局池本身** ——
	//   全局池带 L1 本地 LRU,写路径的失效只能删掉本实例的 L1,
	//   别的副本仍持有旧值 ⇒ "改完立刻生效"退化成"最多 10 秒后生效"。
	scopeSvc := service.NewScopeService(db, cachePool.Safe())

	// ⚠️ **字段级可见性(列级权限)规则服务(M162)** ——
	//
	// **刻意只取 L2(`cachePool.Safe()`),不取全局池本身** ——
	// 与 scopeSvc 同一个理由:全局池带 L1 本地 LRU,写路径的失效只能删掉本实例的 L1,
	// 别的副本仍持有旧值 ⇒ "改完立刻生效"退化成"最多 10 秒后生效"。
	fieldPolicySvc := service.NewFieldPolicyService(db, cachePool.Safe())

	registry := &service.Registry{
		// ⚠️ **Auth 拿着异常登录检测配置 + 站内信通道**(M128)——
		//   两者都是**必填参数**:漏传是编译错误,而不是"静默不检测"。
		//
		//   cfg.Auth.Anomaly:开关 + 阈值(允许几端同时在线 / 多久算"还活跃")。
		//   notifier(Fanout):异常登录时给用户发站内信 —— Fanout 的 inner
		//   就是 inboxMgr,所以站内信照旧落库,顺带多一条邮件通道。
		//
		//   ⚠️ **notifier 必须在上面建好**(见上方装配段)—— 顺序反了这里拿到的是 nil,
		//   站内信会静默地一条都投不出去(而且**不报错**)。
		Auth: service.NewAuthService(db, jwtMulti, smsSender, cfg.SMS, cfg.Auth.Anomaly, notifier),
		// ⚠️ **User 拿着文件系统**(M124 头像上传)—— 头像是传上去的文件,
		// 落点在自己的 id 底下(见 service/user_avatar.go)。
		User: service.NewUserService(db, fileSvc, scopeSvc),
		// ⚠️ **Role 拿着字典事件生产者**(M94)—— 字典一改就广播,
		// 前端 `stores/dict.ts` 收到后把该组缓存作废。
		// 传 nil 的话"改了不生效"是默认行为,而且**没有任何报错**。
		//
		// ⚠️ **Role 还拿着字典缓存**(M95)—— 字典读路径走 L2 缓存,
		// 写路径显式失效。**刻意只取 L2(`cachePool.Safe()`),不取全局池本身**:
		// 全局池带 L1 本地 LRU,写路径的失效只能删掉本实例的 L1,
		// 别的副本仍持有旧值 —— "改完立刻生效"会退化成"最多 10 秒后生效"。
		// 与 `authz.CachedLoader` 同一判断。
		Role: service.NewRoleService(db, service.NewDictPublisherWithOutbox(db), service.NewDictCache(cachePool.Safe())),
		// ⚠️ **Scope 是上面那个 scopeSvc** —— 刻意复用同一个实例,
		// 另造一个就是两套缓存键 ⇒ "改了规则立刻生效"退化成"最多 5 分钟后生效"。
		// 数据范围规则每个请求都要读一次(M155),读路径走 L2 缓存,写路径显式失效。
		Scope: scopeSvc,
		// ⚠️ **FieldPolicy 是上面那个 fieldPolicySvc** —— 刻意复用同一个实例,
		// 另造一个就是两套缓存键 ⇒ "改了规则立刻失效"退化成"最多 5 分钟后生效"。
		FieldPolicy: fieldPolicySvc,
		// ⚠️ **AssetService 拿着审批引擎**(M86-2)—— 资料的提交与审核
		// 都走引擎:提交时同事务建审批单(D8),审核时调 `engine.Act`。
		Audit:    service.NewAuditService(db, scopeSvc),
		Config:   ccSvc,
		// 文件系统(M80)。⚠️ **三个 service 共用同一个 provider 实例和同一份
		// FileOptions** —— 分开构造会让 key 规则和配额记账各走各的,而它们本来
		// 就该是同一套。
		//
		// ⚠️ `fileSvc` 是**上面 §6 建好的那一个**(不是这里新建)——
		// 用户 service 传头像用的是同一个实例(M124)。
		File:       fileSvc,
		Folder:     service.NewFolderService(db, fileProv, fileOpt),
		Upload:     service.NewUploadService(db, fileProv, fileOpt),
		Dashboard:  service.NewDashboardService(db),
		WxTemplate: service.NewWxTemplateService(db),
		Guide:      service.NewGuideService(db, scopeSvc),
		Captcha:    service.NewCaptchaService(),
		// §1.9 #11+#12 P1 —— 站内信 + 通知中心
		Inbox: service.NewInboxService(inboxMgr),
		// ⚠️ **InboxNotifier 塞的是 notifier(Fanout),不是 inboxMgr** ——
		//   Fanout.inner 就是 inboxMgr,站内信语义完全不变,
		//   但业务侧**任何一次**站内信投递都会顺带走一遍额外交付通道(邮件)。
		//   这就是"通知中心(站内信 + 邮件)"(§4 P1 #10)在本层的落点:
		//   通道装在**投递口**上,而不是让每个业务模块自己记得发邮件。
		InboxNotifier: notifier,
		// §1.9 #9+#10 P2 —— 邮件 / 推送 通道本体。
		// ⚠️ **可能是 nil**(provider 为空串 / 未知值)—— 业务侧拿到 nil
		//   应当**降级为不投递**,不要 panic。
		EmailSender: emailSender,
		PushSender:  pushSender,
		// M68 定时任务管理。**必须在 5.8 SetDefault 之后构造**才有意义 ——
		// service 靠 cronx.Default() 拿调度器,而 jobService 每次调用时才取,
		// 所以构造顺序无关紧要,但 SetDefault 必须早于 router.Start()。
		Job: service.NewJobService(db),
		// M86 通用审批系统
		Approval:       approvalSvc,
		ApprovalFlow:   approvalFlowSvc,
		ApprovalEngine: approvalEngine,
		// M115 短信模板审核状态。
		//
		// ⚠️ **它背后是 pkg/smstpl 的运行期装配(Runtime)** ——
		//	登记册 / 守卫 / 同步器都在那儿,后台页面、同步任务、发送链路
		//	共用同一份。这里只做"把人工动作翻译成状态机认可的流转"。
		//
		// ⚠️ **与发送链路共用同一个 Runtime 实例** —— 页面点"停用"之后,
		//	下一次发送必须立刻被拦住。如果这里另建一个 Runtime,
		//	页面改的是 A 登记册,发送看的是 B 登记册 ⇒ 停用形同虚设。
		SmsTemplate: service.NewSmsTemplateService(smsTplRuntime),
		// M150 动态业务集合(业务表单设计器)
		//
		// ⚠️ **只需要 db** —— 它自己管建表/加列(DDL),
		// 不依赖 OSS / 审批 / 任何业务 service。
		//
		// ⚠️ **scopeSvc 是 M160 加的** —— 动态集合的数据面要受
		// 「数据范围(行级权限)」管(角色页配的 `dyn:<集合编码>` 规则)。
		//
		// ⚠️ **fileSvc 是附件字段加的** —— 字段值里只有文件 key,
		// 原文件名与直链不在列里 ⇒ 读记录时要找文件服务解析(视图里的 `_files`),
		// 写记录时要找它校验"这个 key 真的存在"。
		Dyn: service.NewDynService(db, scopeSvc, fileSvc, approvalEngine),
		// 语音能力(voicex)—— 音色克隆台账 + TTS 音色登记册/合成
		VoiceClone: voiceCloneSvc,
		TtsVoice:   ttsVoiceSvc,
		Asr:        asrSvc,
	}

	// 7.2 异常登录检测启动自检(M128)
	//
	//   ⚠️ **必须打出来** —— 检测是"静默"的:它不报错、不弹窗,
	//   只在真的发现异常时才产生一行 WARN 日志。
	//   ⇒ "检测没跑"和"跑了但没发现异常"在日志里**长得一模一样**。
	//   把开关与阈值打在启动日志里,运维 grep 一行就知道这个部署到底跑没跑检测。
	//
	//   顺带把"配置为零值"的两种危险形态点出来:
	//     - enabled=false ⇒ 检测**整个不跑**(连查库都不查);
	//     - max_concurrent=0 ⇒ 被 maxConcurrent() 兜底成 1
	//       ⇒ 等于"只允许 1 个会话在线",多端登录的用户每次登录都报异常。
	anomalyFields := []zap.Field{
		zap.Bool("enabled", cfg.Auth.Anomaly.Enabled),
		zap.Int("max_concurrent", cfg.Auth.Anomaly.MaxConcurrent),
		zap.Duration("active_window", cfg.Auth.Anomaly.ActiveWindow),
		zap.Duration("impossible_travel_window", cfg.Auth.Anomaly.ImpossibleTravelWindow),
	}
	if !cfg.Auth.Anomaly.Enabled {
		log.Warn("anomalylogin: 异常登录检测**已关闭** —— 账号异地/多端登录将没有任何提醒", anomalyFields...)
	} else {
		log.Info("anomalylogin ready", anomalyFields...)
		if cfg.Auth.Anomaly.MaxConcurrent <= 1 {
			// ≤1 时兜底成 1(见 anomalylogin.maxConcurrent),等于"只允许 1 个会话"。
			// 这几乎不可能是部署方的本意 —— 多端登录是常态。
			log.Warn("anomalylogin: max_concurrent <= 1,等价于只允许单端在线,正常多端登录会持续报异常",
				zap.Int("max_concurrent", cfg.Auth.Anomaly.MaxConcurrent))
		}
		if cfg.Auth.Anomaly.ActiveWindow <= 0 {
			log.Warn("anomalylogin: active_window <= 0,等价于不判「同时在线」,并发类异常将永远不会触发")
		}
	}

	// 8. Hertz
	h := server.Default(
		server.WithHostPorts(fmt.Sprintf(":%d", cfg.System.Addr)),
		server.WithReadTimeout(cfg.System.ReadTimeout),
		server.WithWriteTimeout(cfg.System.WriteTimeout),
		server.WithMaxRequestBodySize(int(cfg.System.MaxBodySize)),
	)
	h.Engine.Name = cfg.System.AppName

	// 8.1 客户端 IP 解析(M105)—— 只信任**显式列出的**反向代理网段。
	//
	// Hertz 默认信任所有对端(TrustedCIDRs = 0.0.0.0/0),并且在"全信任"
	// 前提下取 X-Forwarded-For 的最左项 —— 那一项是客户端自己填的。
	// 后果:按 IP 的限流可以被一个请求头击穿,审计表里的 ip 也是攻击者填的。
	// 详见 internal/api/middleware/clientip.go 的文件头。
	h.Engine.SetClientIPFunc(middleware.ClientIPFunc(cfg.ClientIP.TrustedCIDRs))

	// 9. 中间件全局依赖注入
	//
	// ⚠️ **audit 不在这里注入了** —— 它由 `router.Register` 装配
	//   (Register 手里有 registry,单一装配点)。
	//   以前这里是第 4 个参数 `nil`,而全仓再没有第二处赋值 ⇒
	//   操作日志(sys_operation_log)的中间件写侧**一行都没写过**。
	middleware.SetGlobal(cfg, jwtMulti, casbinx.Enforcer())

	// 9.0 熔断器配置(per-route 仍可单独挂)
	middleware.InitBreaker(cfg.Breaker)

	// 9.1 L0.5 Principal loader
	//   有 Redis:CachedLoader(Redis 缓存 + singleflight)
	//   无 Redis:DBLoader(每次直查,dev 模式可接受)
	//
	//   principal_cache_ttl = 权限撤销的延迟上界(grant/revoke 会主动 Invalidate,
	//   所以正常路径 0 延迟;漏掉 Invalidate 的改权限路径会挂满这个 TTL)。
	dbLoader := authz.NewDBLoader(db)
	var finalLoader authz.Loader = dbLoader
	if cfg.Redis.Addr != "" {
		rdb := repository.GetRedis()
		if rdb != nil {
			ttl := cfg.Auth.PrincipalCacheTTL
			if ttl <= 0 {
				ttl = 5 * time.Minute // NewCachedLoader 内部也有兜底,这里写出来是为了日志能打出真实值
			}
			// 缓存后端走 cachex(2026-10-25 M90):三防(单飞 / null 占位 / TTL 抖动)
			// 只有一份实现,而且 principal 缓存的命中率进了 app_cache_ops_total。
			//
			// ⚠️ **刻意不用全局 cachex 池**(cachex.Default()):那个池带 L1 本地 LRU,
			// 而 L1 只能被本实例的 Invalidate 清掉 —— 多副本下"改完权限立刻生效"
			// 会退化成"最多 10 秒后生效"。这里只要 L2。
			cached := authz.NewCachedLoader(dbLoader, cachex.New(rdb), ttl)
			finalLoader = cached
			log.Info("principal loader ready",
				zap.String("backend", "redis-cached"),
				zap.Duration("ttl", ttl))
		} else {
			log.Warn("principal loader ready (DB only, redis not initialized)",
				zap.String("backend", "db-only"))
		}
	} else {
		log.Info("principal loader ready (DB only, dev mode)",
			zap.String("backend", "db-only"))
	}
	middleware.SetPrincipalLoader(finalLoader)

	// 9. 路由 + 中间件
	//   db / redis 传给 Register 仅用于 /readyz 真实探活
	router.Register(h, cfg, registry, db, repository.GetRedis())
	// 系统日志 sink 回填(2026-11-03)—— slowquery 的 OnSlow 闭包在 registry
	// 建好之前就装了,这里把真正的写入方递过去。此点之后慢 SQL 才可能触发
	//(请求还没开始服务),无竞态窗口。
	systemLogSink.Store(registry.Audit)

	// 9.1 启动检查:rbac() 引用的 code 必须存在于 sys_resource;受保护路由数 == rbac 调用数
	//   失败 fail-fast,避免带病上线(漏挂 RBAC → 线上 403;typo → 同样 403)
	if err := router.CheckResourceConsistency(h, db); err != nil {
		log.Fatal("resource consistency check failed", zap.Error(err))
	}

	// 9.1.0 内置业务集合播种(Phase 2a,2026-11-03)
	//
	// 房源 / 公告两个内置业务改由设计器承载(方案 §11.2 2a):
	// 启动期幂等播种 dyn_house / dyn_notice 两个集合(建表+字段+资源+绑业务角色)。
	// ⚠️ 只补缺,不覆盖 —— 运营改过的集合一律不动;失败 fail-fast(半截播种 = 死接口)。
	{
		bcCtx, bcCancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := registry.Dyn.EnsureBuiltinCollections(bcCtx)
		bcCancel()
		if err != nil {
			log.Fatal("dyn builtin collections seed failed", zap.Error(err))
		}
	}

	// 9.1.1 启动检查:路由层登记的每个业务域都必须有数据范围接线
	//
	// ⚠️ **这是清单 §1.13 #2 要的那句「不声明 ⇒ 启动失败」** ——
	// 路由层新加了一个业务域(比如 rbac("news", "list"))却忘了在业务查询里
	// 接数据范围 ⇒ 后台给 "news" 配的规则**永远不生效**,而且没有任何报错。
	//
	// ⇒ 三张账本(bizScopeSpecs / bizScopeCtxWired / bizScopeUnwired)之外的业务域
	// 一律视为「漏声明」⇒ 启动失败。
	if err := service.CheckScopeWiring(); err != nil {
		log.Fatal("scope wiring check failed", zap.Error(err))
	}

	// 9.1.2 启动检查:接了行级数据范围的业务域都必须在字段级可见性层登记
	//
	// ⚠️ **这是"反方向"的那张网** —— 行级账本新增一个业务域而没在 bizFieldDomains
	// 登记 ⇒ ① 执行层早退(fieldPolicyDomainSupported)⇒ 该域的响应**完全不过脱敏**;
	// ② 配置期拒「业务域不存在」⇒ 管理员**也配不出来**
	// ⇒ 配了不生效,而且没有任何报错(与 M161 撞出的 family 那条同类)。
	if err := service.CheckFieldPolicyWiring(); err != nil {
		log.Fatal("field policy wiring check failed", zap.Error(err))
	}

	// 9.2 Swagger 文档端点的鉴权状态 —— 必须让人看见。
	//
	// ⚠️ **两种情况都要出声**:
	//   - 随机密码:用户得知道密码才能打开文档(否则只看到浏览器反复弹框)
	//   - 无鉴权:这是一条**公开的**接口清单,生产部署必须改
	if cfg.Swagger.Enabled {
		switch {
		case cfg.Swagger.Auth.Generated:
			log.Warn("swagger 文档端点已启用 BasicAuth(本次启动随机生成密码,重启会变)",
				zap.String("url", "/swagger/index.html"),
				zap.String("username", cfg.Swagger.Auth.Username),
				zap.String("password", cfg.Swagger.Auth.Password),
				zap.String("hint", "生产环境请配置 swagger.auth.username / swagger.auth.password(或环境变量 SWAGGER_AUTH_USERNAME / SWAGGER_AUTH_PASSWORD)"))
		case !cfg.Swagger.Auth.Enabled:
			log.Warn("swagger 文档端点**未鉴权**(/swagger 全公开,含全量接口清单)",
				zap.String("url", "/swagger/index.html"),
				zap.String("hint", "生产环境请把 swagger.auth.mode 改成 basic 并配置凭据"))
		default:
			log.Info("swagger 文档端点已启用 BasicAuth",
				zap.String("url", "/swagger/index.html"),
				zap.String("username", cfg.Swagger.Auth.Username))
		}
	}

	// 10. 启动 + 优雅退出
	go func() {
		log.Info("rapid-admin starting",
			zap.Int("port", cfg.System.Addr),
			zap.String("mode", cfg.System.Mode),
			zap.String("version", cfg.System.AppVersion))
		h.Spin()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("rapid-admin shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		log.Error("forced shutdown", zap.Error(err))
	}

	// 10.1 定时任务优雅停机
	//   顺序是刻意的:**先停 HTTP、再停调度器**。
	//   反过来的话,在 HTTP 关闭到调度器停止这段窗口里,手动触发接口还在接收请求,
	//   而 cronx 已经不收新任务了 —— 表现是「点了手动触发,返回成功,但没有执行记录」。
	//   两段各自有预算:HTTP 30s(上面),cronx 用 job.shutdown_timeout。
	jobStopCtx, jobStopCancel := context.WithTimeout(
		context.Background(), cfg.Job.ShutdownTimeout)
	defer jobStopCancel()
	jobSched.Shutdown(jobStopCtx)
	log.Info("rapid-admin exited")
}

// buildSmsSender 按"生效的" provider 选 SMS 下发实现
//   - aliyun  → 真实下发(需要 access_key / sign_name / templates)
//   - console → 仅日志打印(本地 dev / 未配凭证时 fallback)
//   - "" / 其他 → console + 一次性 warn
//
// ⚠️ **provider 现在是运行时决定的**(C6,2026-10-29):
//
//	启动时先解析一次(配置文件 + 配置中心),之后**每次发送都重新解析**
//	⇒ 面板上把 provider 切成 console(临时停发短信)立刻生效,不用重启。
//
//	启动时那次解析也是 fail-fast 点:provider=aliyun 却没配凭证 ⇒ 直接 Fatal,
//	而不是等第一个用户收不到验证码才发现。
//
// ⚠️ **guard 是发送前的模板守卫**(M115):模板确定发不出去时,在下发之前就拦下,
//
//	并明确报出"是哪张模板、为什么"。策略档位见 sms.template_guard
//	(off / warn / block,默认 block —— 写错也落到 block,失效比误拦危险)。
func buildSmsSender(base config.SMSConfig, resolve msn.Resolver, guard msn.TemplateGuard) msn.Sender {
	eff, err := resolve(context.Background(), base)
	if err != nil {
		logger.L().Fatal("resolve sms config failed", zap.Error(err))
	}
	switch eff.Provider {
	case "aliyun":
		s, err := aliyun.New(eff)
		if err != nil {
			logger.L().Fatal("init aliyun sms sender failed", zap.Error(err))
		}
		logger.L().Info("sms sender ready", zap.String("provider", "aliyun"))
		return msn.NewDynamicSender(base, resolve, s, console.New(), guard)
	case "console":
		logger.L().Info("sms sender ready", zap.String("provider", "console"))
		// 宽松构造:面板随时可能切到 aliyun,那时才校验凭证
		return msn.NewDynamicSender(base, resolve, aliyun.NewDeferred(eff), console.New(), guard)
	case "":
		logger.L().Warn("sms.provider 未配置, fallback 到 console (验证码只打到日志)")
		return msn.NewDynamicSender(base, resolve, aliyun.NewDeferred(eff), console.New(), guard)
	default:
		logger.L().Warn("未知的 sms.provider, fallback 到 console",
			zap.String("provider", eff.Provider))
		return msn.NewDynamicSender(base, resolve, aliyun.NewDeferred(eff), console.New(), guard)
	}
}

// buildSmsTemplateFetcher 造"服务商侧模板审核状态"查询器(M115)
//
// ⚠️ **每轮重新造** —— 配置中心换过凭证之后,抱着旧查询器去查,
//
//	查的是旧账号的模板(而且会一直查错下去)。
//
// ⚠️ **只有 aliyun 有这个能力** —— console 通道压根没有模板,
//
//	返回 (nil, nil) 表示"这个通道不查审核",**不是故障**
//	(调用方据此返回"当前通道不支持查询",而不是"同步失败")。
//
// ⚠️ **用宽松构造(不校验凭证)** —— provider 是运行时决定的:
//
//	启动时可能是 console(根本没配 AK),运营随时可能在面板上切成 aliyun。
//	如果这里因为"没凭证"而报错,面板就永远切不过去。
//	凭证缺失会在真正查询时暴露(同步任务记 Failed,状态保持原样)。
func buildSmsTemplateFetcher(ctx context.Context, eff config.SMSConfig) (smstpl.Fetcher, error) {
	if eff.Provider != "aliyun" {
		return nil, nil
	}
	return aliyun.NewTemplateStatusFetcher(aliyun.NewDeferred(eff), eff), nil
}

// buildEmailSender 按 cfg.Email.Provider 选邮件下发实现(§1.9 #9)
//
//   - smtp    → 通用 SMTP(465 TLS / 587 STARTTLS)
//   - aliyun  → 阿里云 DirectMail(HTTP REST)
//   - console → 仅日志打印(dev 用)
//   - ""      → **返回 nil,这条通道不存在**
//   - 其它    → **返回 nil + Warn**,不落 console
//
// 🔴 **未知的 provider 不落 console** —— 与 buildSmsSender 刻意不同。
//
//	短信验证码**必须**有去处,落 console 是安全方向;
//	邮件是额外交付,"配错了但看起来在工作"才是最坏的失败方向:
//	运维看着日志里一封封 "[email/console]" 会以为邮件通了,
//	实际上收件人一封都没收到 —— 而"没发出去"根本不会报错。
//
//	所以这里宁可**什么都不装配**(启动日志里明说"邮件通道未配置"),
//	也不要装一个假的。
func buildEmailSender(cfg config.EmailConfig) emailx.Sender {
	switch cfg.Provider {
	case "smtp":
		timeout := emailTimeout(cfg.Timeout)
		s, err := emailx.NewSMTP(emailx.SMTPConfig{
			Host:        cfg.SMTP.Host,
			Port:        cfg.SMTP.Port,
			Username:    cfg.SMTP.Username,
			Password:    cfg.SMTP.Password,
			From:        emailFrom(cfg),
			UseTLS:      cfg.SMTP.UseTLS,
			UseSTARTTLS: cfg.SMTP.UseSTARTTLS,
			Timeout:     timeout,
		})
		if err != nil {
			// ⚠️ **配置错了就该启动失败** —— 邮件通道配了但连不上,
			//   静默降级成"不发邮件"的话,运维要等到有人投诉才发现。
			logger.L().Fatal("init smtp email sender failed", zap.Error(err))
		}
		logger.L().Info("email sender ready", zap.String("provider", "smtp"))
		return s
	case "aliyun":
		s, err := emailx.NewAliyun(emailx.AliyunConfig{
			AccessKey:    cfg.Aliyun.AccessKey,
			AccessSecret: cfg.Aliyun.AccessSecret,
			AccountName:  cfg.Aliyun.AccountName,
			FromAlias:    firstNonEmpty(cfg.Aliyun.FromAlias, cfg.FromName),
			Region:       cfg.Aliyun.Region,
			Timeout:      emailTimeout(cfg.Timeout),
		})
		if err != nil {
			logger.L().Fatal("init aliyun email sender failed", zap.Error(err))
		}
		logger.L().Info("email sender ready", zap.String("provider", "aliyun"))
		return s
	case "console":
		logger.L().Info("email sender ready", zap.String("provider", "console"))
		return emailx.NewConsole()
	case "":
		logger.L().Info("email 通道未配置(站内信仍然正常投递)")
		return nil
	default:
		logger.L().Warn("未知的 email.provider,邮件通道不装配(站内信仍然正常投递)",
			zap.String("provider", cfg.Provider))
		return nil
	}
}

// buildPushSender 按 cfg.Push.Provider 选 App 推送实现(§1.9 #10)
//
//   - aliyun-emns → 阿里云 EMNS(iOS APNs / Android 厂商 / 鸿蒙)
//   - console     → 仅日志打印(dev 用)
//   - "" / 其它   → 返回 nil(通道不存在)
//
// ⚠️ **语义与 buildEmailSender 一致**:配错就装配不了,不落 console 假装在工作。
func buildPushSender(cfg config.PushConfig) pushx.Sender {
	switch cfg.Provider {
	case "aliyun-emns":
		s, err := pushx.NewAliyunEMNS(pushx.AliyunConfig{
			AccessKey:      cfg.AliyunEMNS.AccessKey,
			AccessSecret:   cfg.AliyunEMNS.AccessSecret,
			AppKey:         cfg.AliyunEMNS.AppKey,
			Region:         cfg.AliyunEMNS.Region,
			DefaultAPNSEnv: cfg.AliyunEMNS.DefaultAPNSEnv,
			Timeout:        pushTimeout(cfg.Timeout),
			TargetMax:      cfg.TargetMax,
		})
		if err != nil {
			logger.L().Fatal("init aliyun emns push sender failed", zap.Error(err))
		}
		logger.L().Info("push sender ready", zap.String("provider", "aliyun-emns"))
		return s
	case "console":
		logger.L().Info("push sender ready", zap.String("provider", "console"))
		return pushx.NewConsolePush()
	case "":
		logger.L().Info("push 通道未配置")
		return nil
	default:
		logger.L().Warn("未知的 push.provider,push 通道不装配",
			zap.String("provider", cfg.Provider))
		return nil
	}
}

// buildNotifierDispatcher 组装通知分发器的通道列表
//
// ⚠️ **只挂邮件** —— 推送通道要设备令牌,全仓没有那张登记册,
//
//	挂上去每条通知都会 skip,看着像"接上了"其实没发。
//	pushSender 走 Registry 交给业务侧(登记册落地之后再接)。
func buildNotifierDispatcher(emailSender emailx.Sender, cfg config.EmailConfig) *notifyx.Dispatcher {
	d := notifyx.NewDispatcher()
	if emailSender != nil {
		d.Register(notifyx.NewEmailChannel(
			emailSender,
			emailFrom(cfg),
			cfg.FromName,
			cfg.BaseURL,
		))
	}
	return d
}

// emailFrom 取发件地址
//
// ⚠️ **优先级:email.from > provider 自带的** ——
//
//	显式配的 from 覆盖一切;没配时 smtp 取 username、aliyun 取 account_name。
func emailFrom(cfg config.EmailConfig) string {
	if v := strings.TrimSpace(cfg.From); v != "" {
		return v
	}
	switch cfg.Provider {
	case "smtp":
		return cfg.SMTP.Username
	case "aliyun":
		return cfg.Aliyun.AccountName
	}
	return ""
}

// emailTimeout 配置里的秒 → time.Duration(0 = 用 provider 默认 30s)
func emailTimeout(sec int) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// pushTimeout 同上
func pushTimeout(sec int) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// firstNonEmpty 返回第一个非空串
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// initFieldCrypto 装载字段级加密器 + 盲索引器(§1.5 #1+#2+#3)
//
// 语义:
//   - **没配**(空串)→ 不是错误,什么都不做(保持"未启用")
//   - **配了但配错** → 返 error(调用方打 Error),**不初始化**
//
// ⚠️ M100(2026-10-26):这段以前根本不存在 —— 配置里的
// crypto.field_key / crypto.blind_key 没人读,`crypto.Field()` /
// `crypto.Blind()` 从来没被初始化过(详见调用点的说明)。
func initFieldCrypto(cfg config.CryptoConfig) error {
	var errs []error

	if s := strings.TrimSpace(cfg.FieldKey); s != "" {
		key, err := decodeFieldKey(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("crypto.field_key(env FIELD_AES_KEY): %w", err))
		} else if err := crypto.InitFieldCipherWithMeta(key, crypto.KeyMeta{
			KeyID:      "crypto.field",
			KeyVersion: 1,
			Algorithm:  "aes-256-gcm",
		}); err != nil {
			errs = append(errs, fmt.Errorf("crypto.field_key(env FIELD_AES_KEY): %w", err))
		}
	}

	if s := strings.TrimSpace(cfg.BlindKey); s != "" {
		if err := crypto.InitBlindIndexWithMeta([]byte(s), crypto.KeyMeta{
			KeyID:      "crypto.blind",
			KeyVersion: 1,
			Algorithm:  "hmac-sha256",
		}); err != nil {
			errs = append(errs, fmt.Errorf("crypto.blind_key(env FIELD_BLIND_KEY): %w", err))
		}
	}

	return errors.Join(errs...)
}

// decodeFieldKey 解析字段密钥:base64(标准 / 无 padding)或 32 字节原文
//
// 生产环境由 K8s Secret 注入 base64;本地开发可能直接写 32 字节原文 ——
// 两种都认,省得运维为了"必须是 base64"再写一层转换。
func decodeFieldKey(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(s) == 32 {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("需要 32 字节(base64 或原文),实际 %d 字节", len(s))
}

// §1.5 #6+#7+#10 P1 密钥装载已迁至 pkg/kms.Manager
//   - 旧 initCryptoKeys / loadTierKeys / itoa 删除
//   - 入口:cmd/server/main.go 调 kms.NewManager(ctx, cfg.KMS)
//   - local provider 走 env;切 aliyun/tencent/vault 只改 cfg.KMS.Provider

// ============================================================
// 启动路径自检
//
// 配置里 logger.dir / casbin.model_path / database.sqlite.path 原本都是相对路径,
// 而 viper/os 都相对【进程 cwd】解析 —— 从不同目录启动(mkdir -p data && go run /
// VSCode F5 / systemd WorkingDirectory),日志和 SQLite 就会落到不同地方。
// config.resolvePaths() 已把它们统一钉成绝对路径(基准 = APP_ROOT > cwd),
// 这里把结果同时输出到控制台和日志文件,出问题第一眼就能定位。
// ============================================================

// resolvedPath 一行路径信息
type resolvedPath struct {
	label string
	value string
}

// collectResolvedPaths 汇总所有需要自检的绝对路径
func collectResolvedPaths(cfg *config.Server) []resolvedPath {
	dbDesc := cfg.Database.SQLite.Path
	if cfg.Database.Driver != config.DriverSQLite {
		// MySQL 模式下 sqlite.path 不生效,照原样打印会误导排查
		dbDesc = fmt.Sprintf("(未启用 — database.driver=%s)", cfg.Database.Driver)
	}
	return []resolvedPath{
		{"根目录", config.Root()},
		{"配置文件", config.ConfigFile()},
		{"日志目录", cfg.Logger.Dir},
		{"SQLite 数据库", dbDesc},
		{"Casbin 模型", cfg.Casbin.ModelPath},
	}
}

// printResolvedPaths 直写 stdout,**不经 zap**
//
// 为什么不能只靠 logger:logger.New 仅在 env=dev/test 时挂 stdout core
// (internal/pkg/logger/logger.go:70),env=prod 时控制台会完全没有这一段,
// 而这恰恰是最需要它的时候。
func printResolvedPaths(cfg *config.Server) {
	const bar = "================================================================"
	fmt.Fprintln(os.Stdout, bar)
	fmt.Fprintln(os.Stdout, " rapid-admin 启动路径自检(以下均为绝对路径)")
	fmt.Fprintln(os.Stdout, "----------------------------------------------------------------")
	for _, p := range collectResolvedPaths(cfg) {
		fmt.Fprintf(os.Stdout, " %s %s\n", padDisplay(p.label, 16), p.value)
	}
	fmt.Fprintln(os.Stdout, bar)
}

// logResolvedPaths 同一份内容写进 info.log,与控制台那份双份留存
func logResolvedPaths(log *zap.Logger, cfg *config.Server) {
	fields := make([]zap.Field, 0, 5)
	for _, p := range collectResolvedPaths(cfg) {
		fields = append(fields, zap.String(p.label, p.value))
	}
	log.Info("resolved absolute paths", fields...)
}

// padDisplay 按【终端显示宽度】左对齐补空格
//
// 不能直接用 fmt 的 %-16s:中文是全角字符,占 2 列但 rune 数为 1,
// 那样排版会参差不齐(Go 标准库 fmt 不处理东亚宽度)。
func padDisplay(s string, width int) string {
	pad := width - dispWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// dispWidth 计算字符串的终端显示宽度(CJK 全角按 2 列)
func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r >= 0x1100 && r <= 0x115F, // 韩文字母
			r >= 0x2E80 && r <= 0x303E, // CJK 部首 + 中日韩符号标点
			r >= 0x3041 && r <= 0x33FF, // 假名 + CJK 兼容
			r >= 0x3400 && r <= 0x4DBF, // CJK 扩展 A
			r >= 0x4E00 && r <= 0x9FFF, // CJK 统一表意
			r >= 0xA000 && r <= 0xA4CF, // 彝文
			r >= 0xAC00 && r <= 0xD7A3, // 韩文音节
			r >= 0xF900 && r <= 0xFAFF, // CJK 兼容表意
			r >= 0xFE30 && r <= 0xFE6F, // CJK 兼容形式
			r >= 0xFF00 && r <= 0xFF60, // 全角 ASCII
			r >= 0xFFE0 && r <= 0xFFE6: // 全角符号
			w += 2
		default:
			w++
		}
	}
	return w
}
