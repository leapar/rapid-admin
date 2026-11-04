// Package smslimit 短信限流(per phone + per scene)—— §1.9 #8 P1
//
// 设计:Redis 固定窗口(简单可预测,2 维独立计数)
//
//   - sms:cooldown:{phone}:{scene}  -- 1 个 key,冷却期(默认 60s)内只 1 次
//   - sms:win:v2:1m:{phone}:{scene} -- 1 分钟窗口计数
//   - sms:win:v2:1d:{phone}:{scene} -- 1 天窗口计数
//
// ⚠️ **M112(2026-10-29)修正:以前的"固定窗口"根本不是固定窗口**
//
//	旧实现每次计数都执行 `INCR` + **无条件 `EXPIRE key ttl`**。
//	EXPIRE 每次都把 TTL 往后推 ⇒ 计数器**永不过期**(只要还在请求就续命),
//	而且**被拒的请求也会 INCR**(计数在判超限之前就加了)。
//
//	实际后果(默认 per_minute=1 / per_day=10):
//	  - 第 11 条短信被拒;这次拒绝又给 24h 的 key 续了一次 TTL
//	    ⇒ **这个手机号在这个场景下被永久拉黑**(每次重试都续一次 TTL)
//	  - 1 分钟窗口同理:冷却期一旦短于 60s(窗口是写死的 60s),
//	    第 2 条短信之后计数就再也回不到 0
//
//	真正的固定窗口 = TTL 只在**计数器刚创建**时设一次,窗口走完自然过期。
//	⇒ 现在用 Lua 把 INCR 与 EXPIRE 合成一个原子操作(见 windowScript)。
//
// 配置(cfg.SMS.RateLimit):
//   - PerMinute: 1 分钟内同 phone+scene 最多 N 条
//   - PerDay:    1 天内同 phone+scene 最多 N 条
//   - WindowSeconds: 冷却期秒数(默认 60s)
//
// ⚠️ **M112 修正:配置写 0 不等于"不限"**
//
//	旧注释写"0 = 不限",但代码是 `if cfg.PerMinute <= 0 { cfg.PerMinute = 1 }`
//	—— 0 被当成默认值,`checkWindow` 里的 `limit <= 0 → 放行` 分支永远走不到。
//	两个口径必须选一个,这里选**代码的行为**(限住),理由:
//	  - 限流是安全阀。配置漏写 / 写错时必须落在"限住"这一侧,而不是"放开"。
//	  - configs/config.yaml 是 gitignore 的(见 .gitignore),部署方很容易漏配,
//	    漏配 ⇒ per_minute=0 ⇒ 若解释成"不限"就是**短信接口裸奔**。
//	所以:**0 或负数 = 用默认值(1 条/分钟、10 条/天),不解释为"不限"**。
//
// 失败降级:
//   - Redis nil / 故障 → 放行(主流程不阻塞;DB 层 sys_sms_code 仍兜底,只是慢)
//
// 用法:
//
//	// main.go
//	smslimit.Init(repository.GetRedis())
//
//	// auth_impl.go:SendSms 入口
//	if err := smslimit.Allow(ctx, phone, scene); err != nil {
//	    return err  // errcode.SmsSendTooFrequent / SmsTooFrequent
//	}
package smslimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
)

const (
	// 默认值(可被配置覆盖)
	defaultPerMinute    = 1
	defaultPerDay       = 10
	defaultCooldownSecs = 60
	// Redis key 前缀
	prefixCooldown = "sms:cooldown:"
	// ⚠️ M112:窗口 key 加了版本段(v2)。
	//   旧 key("sms:window:1m:" / "sms:window:1d:")里的计数是被推爆的
	//   (每次请求都续 TTL,见文件头),TTL 最长 24h —— 不换命名空间的话,
	//   升级后这些手机号会带着旧计数继续被拒,最长 24h。
	//   旧 key 没人再碰,自然过期即可。
	prefixWindow1m = "sms:win:v2:1m:"
	prefixWindow1d = "sms:win:v2:1d:"
)

var (
	global *Limiter
)

// windowScript 固定窗口计数的原子实现。
//
// ⚠️ M112:这是修"固定窗口"的关键。旧实现是无条件 EXPIRE(每次都续 TTL),
// 计数永不过期。现在只在**计数器刚创建**(count == 1)时设一次 TTL。
//
// 用 Lua 而不是"INCR 之后在 Go 里判断 count == 1 再 EXPIRE"的原因:
// 后者是两次往返,中间进程挂掉/超时 ⇒ 计数器建好了却没 TTL ⇒ 又变成永不过期
// (而且这次是静默的,没有任何日志能看出来)。Lua 里是原子的。
var windowScript = redis.NewScript(`
local c = redis.call('INCR', KEYS[1])
if c == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return c
`)

// Limiter 短信限流器(Redis 实现)
type Limiter struct {
	rdb         *redis.Client
	perMinute   int
	perDay      int
	cooldownSec int
}

// Config 限流配置
type Config struct {
	PerMinute    int // 1 分钟窗口内上限(<=0 ⇒ 用默认 1,不是"不限")
	PerDay       int // 1 天窗口内上限(<=0 ⇒ 用默认 10,不是"不限")
	CooldownSecs int // 冷却期秒数(<=0 ⇒ 用默认 60)
}

// Init 启动全局 Limiter(只调一次)
//
// rdb=nil → 关闭限流(本地 dev 无 redis 时 no-op,所有 Allow 返 nil)
func Init(rdb *redis.Client, cfg Config) {
	if global != nil {
		return
	}
	global = New(rdb, cfg)
}

// New 构造一个限流器(**不碰全局**)。
//
// ⚠️ 以前只有 Init(全局单例)+ 包级 Allow,而 Limiter 的字段全是 unexported
//
//	⇒ **外部包根本无法构造它**。于是"Redis 故障时放行"这类分支没法在包外
//	被驱动(全局只认一个配置,而且 Init 第二次调用直接 return)。
//	现在导出构造函数,集成测试可以按需造实例(不同配置 / 不同 client)。
//
// 配置归一(0 与负数 ⇒ 默认值,**不是"不限"**)与 Init 完全一致 ——
// 两处必须同规则,理由见包注释。
func New(rdb *redis.Client, cfg Config) *Limiter {
	// ⚠️ M112:0 / 负数 ⇒ 用默认值,**不是"不限"**。理由见包注释。
	if cfg.PerMinute <= 0 {
		cfg.PerMinute = defaultPerMinute
	}
	if cfg.PerDay <= 0 {
		cfg.PerDay = defaultPerDay
	}
	if cfg.CooldownSecs <= 0 {
		cfg.CooldownSecs = defaultCooldownSecs
	}
	return &Limiter{
		rdb:         rdb,
		perMinute:   cfg.PerMinute,
		perDay:      cfg.PerDay,
		cooldownSec: cfg.CooldownSecs,
	}
}

// Allow 判定是否允许发送;不允许时返 *errcode.Error
//
// ⚠️ M112:拆成"包级函数 → 委托给实例方法",是为了让这个原语**能被单测直接驱动**
//
//	(全局变量版只能靠改 global 来测,用例之间会互相污染)。
func Allow(ctx context.Context, phone, scene string) error {
	if global == nil {
		return nil // 未初始化 → 放行
	}
	return global.Allow(ctx, phone, scene)
}

// Allow 判定是否允许发送;不允许时返 *errcode.Error
//
// 顺序:冷却期 → 1 分钟窗口 → 1 天窗口。
// 冷却期先判,是为了让"冷却期内重复发送"这条短窗口不消耗窗口配额。
// (代价:窗口判超限时冷却期已经被消耗掉了,用户会先看到冷却提示,
//
//	60s 后再看到真正的超限提示 —— 两个都是"太频繁",不影响正确性。)
func (l *Limiter) Allow(ctx context.Context, phone, scene string) error {
	if l == nil || l.rdb == nil {
		return nil // 未初始化 → 放行
	}
	// 1. 冷却期
	cdKey := prefixCooldown + phone + ":" + scene
	ok, err := l.rdb.SetNX(ctx, cdKey, 1, time.Duration(l.cooldownSec)*time.Second).Result()
	if err != nil {
		// Redis 故障 → 放行(主流程不阻塞,DB 兜底)
		logger.L().Warn("smslimit: redis SetNX failed, allow", zap.Error(err))
		return nil
	}
	if !ok {
		// 冷却期未到
		return errcode.New(errcode.SmsSendTooFrequent,
			fmt.Sprintf("同一手机号+%s 场景冷却期内重复发送,请 %ds 后再试", scene, l.cooldownSec))
	}
	// 2. 1 分钟窗口
	if err := l.checkWindow(ctx, prefixWindow1m+phone+":"+scene, 60*time.Second, l.perMinute, "1 分钟"); err != nil {
		return err
	}
	// 3. 1 天窗口
	if err := l.checkWindow(ctx, prefixWindow1d+phone+":"+scene, 24*time.Hour, l.perDay, "1 天"); err != nil {
		return err
	}
	return nil
}

// checkWindow 固定窗口计数(递增 + 过期)
//
// ⚠️ M112:TTL **只在计数器刚创建时设一次**(windowScript 里的 `if c == 1`)。
// 旧实现每次都 EXPIRE ⇒ 计数永不过期 ⇒ 手机号被永久拉黑(见包注释)。
//
// 超过 limit 返 *errcode.Error;Redis 故障放行
func (l *Limiter) checkWindow(ctx context.Context, key string, ttl time.Duration, limit int, label string) error {
	if limit <= 0 {
		return nil // 防御:Limiter 被外部直接构造时可能带 0
	}
	secs := int64(ttl / time.Second)
	if secs <= 0 {
		secs = 1
	}
	count, err := windowScript.Run(ctx, l.rdb, []string{key}, secs).Int64()
	if err != nil {
		// Redis 故障 → 放行(主流程不阻塞,DB 兜底)
		// ⚠️ 不打 key:key 里含手机号(明文 PII),日志里不能出现。
		logger.L().Warn("smslimit: redis 窗口计数失败,放行", zap.Error(err), zap.Int("limit", limit))
		return nil
	}
	if count > int64(limit) {
		return errcode.New(errcode.SmsTooFrequent,
			fmt.Sprintf("短信发送超过 %s 上限(%d 条)", label, limit))
	}
	return nil
}
