package smslimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/pkg/errcode"
)

// ── 本文件的来历 ───────────────────────────────────────────────────────
//
// 这个包之前**一张测试都没有**。而它站在**每一个发短信的接口**前面
// (auth_impl.go 的 SendSms 入口第一行就是 smslimit.Allow)。
//
// 它有一条 P0 真 bug(2026-10-29 M112 修的):
//
//	"固定窗口"根本不是固定窗口 —— 每次计数都执行 `INCR` + 无条件 `EXPIRE`,
//	TTL 每次都被往后推 ⇒ 计数器**永不过期** ⇒
//	  - 第 11 条短信(per_day=10)被拒,而这次拒绝又把 24h 的 TTL 续上
//	    ⇒ **这个手机号在这个场景下被永久拉黑**(每次重试都续一次 TTL)
//	  - 1 分钟窗口同理:冷却期一旦短于 60s,计数再也回不到 0
//
// ── M112 可逆探针记录 ───────────────────────────────────────────────────
//
//	拆法                                     必须变红
//	───────────────────────────────────────  ─────────────────────────────────
//	P0 窗口 TTL 每次都重设(退回旧的 INCR+EXPIRE)  TestM112_WindowTTL_NotPushedForward
//	P0 窗口永不过期(计数不随时间归零)            TestM112_DayWindow_RecoversAfterExpiry
//	P1 冷却期不生效                              TestM112_Cooldown_BlocksSecondSend
//	P1 场景之间不隔离                            TestM112_Scene_Isolated
//	P1 1 天上限不生效                            TestM112_DayLimit_Rejects
//	P1 Redis 故障不放行                          TestM112_RedisDown_FailsOpen
// ───────────────────────────────────────────────────────────────────────

// newLimiter 造一个连着 miniredis 的 Limiter
//
// miniredis 是**进程内真 Redis**(真 RESP 协议),不是 mock —— 走的是
// go-redis 的真命令路径(含 Lua 脚本)。
func newLimiter(t *testing.T, perMinute, perDay, cooldownSec int) (*Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &Limiter{
		rdb:         rdb,
		perMinute:   perMinute,
		perDay:      perDay,
		cooldownSec: cooldownSec,
	}, mr
}

// errCodeOf 取 errcode.Error 的码
func errCodeOf(t *testing.T, err error) int {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *errcode.Error,实际 %T(%v)", err, err)
	}
	return e.Code
}

// ── 基础行为 ────────────────────────────────────────────────────────────

func TestM112_NotInitialized_Allows(t *testing.T) {
	// global == nil(没调 Init)→ 放行
	old := global
	global = nil
	t.Cleanup(func() { global = old })

	if err := Allow(context.Background(), "13800000001", "login"); err != nil {
		t.Fatalf("未初始化时应放行,实际 %v", err)
	}
}

func TestM112_NilRedis_Allows(t *testing.T) {
	old := global
	global = &Limiter{rdb: nil, perMinute: 1, perDay: 10, cooldownSec: 60}
	t.Cleanup(func() { global = old })

	if err := Allow(context.Background(), "13800000001", "login"); err != nil {
		t.Fatalf("rdb 为 nil 时应放行,实际 %v", err)
	}
}

func TestM112_FirstSend_Allowed(t *testing.T) {
	l, _ := newLimiter(t, 1, 10, 60)
	if err := l.Allow(context.Background(), "13800000001", "login"); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
}

// TestM112_Cooldown_BlocksSecondSend 冷却期内第二次要挡住
func TestM112_Cooldown_BlocksSecondSend(t *testing.T) {
	l, _ := newLimiter(t, 100, 100, 60)
	ctx := context.Background()

	if err := l.Allow(ctx, "13800000001", "login"); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
	err := l.Allow(ctx, "13800000001", "login")
	if err == nil {
		t.Fatal("冷却期内第二次应被拒")
	}
	if code := errCodeOf(t, err); code != errcode.SmsSendTooFrequent {
		t.Fatalf("错误码应为 SmsSendTooFrequent(%d),实际 %d", errcode.SmsSendTooFrequent, code)
	}
}

// TestM112_Cooldown_Expires 冷却期过了要能再发
func TestM112_Cooldown_Expires(t *testing.T) {
	l, mr := newLimiter(t, 100, 100, 60)
	ctx := context.Background()

	_ = l.Allow(ctx, "13800000001", "login")
	mr.FastForward(61 * time.Second)
	if err := l.Allow(ctx, "13800000001", "login"); err != nil {
		t.Fatalf("冷却期过后应放行,实际 %v", err)
	}
}

// TestM112_DayLimit_Rejects 1 天上限要生效
func TestM112_DayLimit_Rejects(t *testing.T) {
	l, mr := newLimiter(t, 100, 1, 10)
	ctx := context.Background()

	if err := l.Allow(ctx, "13800000001", "login"); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
	mr.FastForward(11 * time.Second)
	err := l.Allow(ctx, "13800000001", "login")
	if err == nil {
		t.Fatal("超过 1 天上限应被拒")
	}
	if code := errCodeOf(t, err); code != errcode.SmsTooFrequent {
		t.Fatalf("错误码应为 SmsTooFrequent(%d),实际 %d", errcode.SmsTooFrequent, code)
	}
}

// ── M112 核心:固定窗口 ─────────────────────────────────────────────────

// TestM112_WindowTTL_NotPushedForward 窗口 TTL 不能被后续请求推后
//
// 这是"固定窗口"与"滑动窗口"的分水岭:旧实现每次都 EXPIRE,
// TTL 会被不断往后推 ⇒ 计数器永不过期。
//
// ⚠️ **判别式是 TTL 的值,不是"能不能恢复"**:旧实现的窗口是**滑动**的
//
//	(每次尝试都把 TTL 往后推 ⇒ 窗口在"最后一次尝试"之后 60s/24h 才结束),
//	所以"过一会儿又能发了"这件事新旧都成立 —— 只有 TTL 本身能区分。
func TestM112_WindowTTL_NotPushedForward(t *testing.T) {
	l, mr := newLimiter(t, 100, 1, 10)
	ctx := context.Background()
	phone, scene := "13800000001", "login"
	minKey := prefixWindow1m + phone + ":" + scene
	dayKey := prefixWindow1d + phone + ":" + scene

	if err := l.Allow(ctx, phone, scene); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
	if ttl := mr.TTL(dayKey); ttl != 24*time.Hour {
		t.Fatalf("1 天窗口 TTL 应为 24h,实际 %v", ttl)
	}
	if ttl := mr.TTL(minKey); ttl != 60*time.Second {
		t.Fatalf("1 分钟窗口 TTL 应为 60s,实际 %v", ttl)
	}

	// 冷却期过后再打一次(会被 1 天上限拒掉)
	mr.FastForward(11 * time.Second)
	if err := l.Allow(ctx, phone, scene); err == nil {
		t.Fatal("超过 1 天上限应被拒")
	}

	ttl := mr.TTL(dayKey)
	if ttl >= 24*time.Hour {
		t.Fatalf("窗口 TTL 被后续请求推后了(%v):固定窗口的 TTL 只能在创建时设一次", ttl)
	}
	if want := 24*time.Hour - 11*time.Second; ttl != want {
		t.Fatalf("窗口 TTL 应为 %v(创建时刻起算),实际 %v", want, ttl)
	}
	// 1 分钟窗口同理
	if ttl := mr.TTL(minKey); ttl != 60*time.Second-11*time.Second {
		t.Fatalf("1 分钟窗口 TTL 应为 %v(创建时刻起算),实际 %v", 60*time.Second-11*time.Second, ttl)
	}
}

// TestM112_DayWindow_RecoversAfterExpiry 1 天窗口到期后必须能再发
//
// 这一条是**用户能感知到的那个 bug**:旧实现里被拒的请求会把 TTL 续上,
// 于是这个手机号在这个场景下被永久拉黑。
func TestM112_DayWindow_RecoversAfterExpiry(t *testing.T) {
	l, mr := newLimiter(t, 100, 1, 10)
	ctx := context.Background()
	phone, scene := "13800000001", "login"

	if err := l.Allow(ctx, phone, scene); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
	mr.FastForward(11 * time.Second)
	if err := l.Allow(ctx, phone, scene); err == nil {
		t.Fatal("超过 1 天上限应被拒")
	}

	// 1 天窗口走完(从第一次发短信起算 24h)
	mr.FastForward(24*time.Hour - 10*time.Second)
	if err := l.Allow(ctx, phone, scene); err != nil {
		t.Fatalf("1 天窗口过期后应恢复放行,实际 %v(说明计数被续期续活了)", err)
	}
}

// TestM112_MinuteWindow_RecoversAfterExpiry 1 分钟窗口到期后必须能再发
//
// ⚠️ **这条是特征测试,不是判别式**:旧实现(每次都续 TTL)的窗口是**滑动**的,
//
//	结束时间是"最后一次尝试 + 60s",所以新旧都能恢复 —— 真正区分新旧的是
//	TTL 的值(见 TestM112_WindowTTL_NotPushedForward)。
//	这条守的是"窗口终会结束"这个契约本身(别把窗口做成永不过期)。
func TestM112_MinuteWindow_RecoversAfterExpiry(t *testing.T) {
	l, mr := newLimiter(t, 1, 100, 1) // 冷却 1s < 窗口 60s
	ctx := context.Background()
	phone, scene := "13800000001", "login"

	if err := l.Allow(ctx, phone, scene); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
	// 冷却期(1s)过后再来 ⇒ 超过 1 分钟上限 ⇒ 拒
	mr.FastForward(2 * time.Second)
	if err := l.Allow(ctx, phone, scene); err == nil {
		t.Fatal("超过 1 分钟上限应被拒")
	}
	// 1 分钟窗口走完 ⇒ 必须能再发(多走 1s,避开 TTL 边界)
	mr.FastForward(61 * time.Second)
	if err := l.Allow(ctx, phone, scene); err != nil {
		t.Fatalf("1 分钟窗口过期后应恢复放行,实际 %v", err)
	}
}

// ── 维度隔离 ────────────────────────────────────────────────────────────

// TestM112_Scene_Isolated 不同 scene 的配额互不影响
func TestM112_Scene_Isolated(t *testing.T) {
	l, mr := newLimiter(t, 100, 1, 10)
	ctx := context.Background()
	phone := "13800000001"

	if err := l.Allow(ctx, phone, "login"); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
	mr.FastForward(11 * time.Second)
	if err := l.Allow(ctx, phone, "login"); err == nil {
		t.Fatal("同一 scene 超过上限应被拒")
	}
	// 换一个 scene ⇒ 另一套配额 ⇒ 应放行
	if err := l.Allow(ctx, phone, "reset_password"); err != nil {
		t.Fatalf("不同 scene 应互不影响,实际 %v", err)
	}
}

// TestM112_Phone_Isolated 不同手机号互不影响
func TestM112_Phone_Isolated(t *testing.T) {
	l, mr := newLimiter(t, 100, 1, 10)
	ctx := context.Background()

	if err := l.Allow(ctx, "13800000001", "login"); err != nil {
		t.Fatalf("首次应放行,实际 %v", err)
	}
	mr.FastForward(11 * time.Second)
	if err := l.Allow(ctx, "13800000001", "login"); err == nil {
		t.Fatal("同一手机号超过上限应被拒")
	}
	if err := l.Allow(ctx, "13800000002", "login"); err != nil {
		t.Fatalf("不同手机号应互不影响,实际 %v", err)
	}
}

// ── 降级 ────────────────────────────────────────────────────────────────

// TestM112_RedisDown_FailsOpen Redis 挂了要放行(主流程不阻塞)
func TestM112_RedisDown_FailsOpen(t *testing.T) {
	l, mr := newLimiter(t, 1, 1, 60)
	ctx := context.Background()

	mr.Close() // 端口直接断掉
	if err := l.Allow(ctx, "13800000001", "login"); err != nil {
		t.Fatalf("Redis 故障时应放行(DB 层还有 sys_sms_code 兜底),实际 %v", err)
	}
}

// ── Init ────────────────────────────────────────────────────────────────

func TestM112_Init_Defaults(t *testing.T) {
	old := global
	global = nil
	t.Cleanup(func() { global = old })

	Init(nil, Config{}) // 全 0 ⇒ 全部走默认值
	if global == nil {
		t.Fatal("Init 后 global 不该为 nil")
	}
	if global.perMinute != defaultPerMinute {
		t.Fatalf("perMinute 默认应为 %d,实际 %d", defaultPerMinute, global.perMinute)
	}
	if global.perDay != defaultPerDay {
		t.Fatalf("perDay 默认应为 %d,实际 %d", defaultPerDay, global.perDay)
	}
	if global.cooldownSec != defaultCooldownSecs {
		t.Fatalf("cooldownSec 默认应为 %d,实际 %d", defaultCooldownSecs, global.cooldownSec)
	}
}

// TestM112_Init_ZeroIsNotUnlimited 配置写 0 **不是**"不限"
//
// 旧注释写"0 = 不限",但代码把 0 当默认值 ⇒ 限流是安全阀,
// 配置漏写时必须落在"限住"这一侧。
func TestM112_Init_ZeroIsNotUnlimited(t *testing.T) {
	old := global
	global = nil
	t.Cleanup(func() { global = old })

	Init(nil, Config{PerMinute: 0, PerDay: 0, CooldownSecs: 0})
	if global.perMinute != defaultPerMinute || global.perDay != defaultPerDay {
		t.Fatalf("0 应取默认值而不是不限,实际 perMinute=%d perDay=%d",
			global.perMinute, global.perDay)
	}
}

// TestM112_Init_OnlyOnce Init 只生效一次
func TestM112_Init_OnlyOnce(t *testing.T) {
	old := global
	global = nil
	t.Cleanup(func() { global = old })

	Init(nil, Config{PerMinute: 5, PerDay: 50, CooldownSecs: 30})
	Init(nil, Config{PerMinute: 99, PerDay: 99, CooldownSecs: 99})
	if global.perMinute != 5 {
		t.Fatalf("第二次 Init 不该覆盖第一次,实际 perMinute=%d", global.perMinute)
	}
}

// ── 脚本本身 ────────────────────────────────────────────────────────────

// TestM112_WindowScript_SetsTTLOnlyOnCreate 脚本层:TTL 只在计数为 1 时设
func TestM112_WindowScript_SetsTTLOnlyOnCreate(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	key := "test:window"

	c1, err := windowScript.Run(ctx, rdb, []string{key}, 60).Int64()
	if err != nil || c1 != 1 {
		t.Fatalf("首次计数应为 1,实际 %d(err=%v)", c1, err)
	}
	if ttl := mr.TTL(key); ttl != 60*time.Second {
		t.Fatalf("首次应设 60s TTL,实际 %v", ttl)
	}

	mr.FastForward(10 * time.Second)
	c2, err := windowScript.Run(ctx, rdb, []string{key}, 60).Int64()
	if err != nil || c2 != 2 {
		t.Fatalf("第二次计数应为 2,实际 %d(err=%v)", c2, err)
	}
	if ttl := mr.TTL(key); ttl != 50*time.Second {
		t.Fatalf("TTL 不该被重设,实际 %v(应为 50s)", ttl)
	}
}
