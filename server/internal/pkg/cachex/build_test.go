package cachex

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestBuild_ModeMatrix mode → 实际装配结果
//
// 这一层是"有没有 Redis / 有没有缓存"的唯一决策点。判错的后果不是报错而是
// "线上悄悄跑着另一个模式",所以必须逐格钉死。
func TestBuild_ModeMatrix(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"}) // 不会真连,这里只验装配

	cases := []struct {
		name    string
		cfg     Config
		rdb     *redis.Client
		wantNil bool
		wantL2  bool
		wantErr bool
	}{
		{"空 mode 视作 auto + 无 Redis", Config{}, nil, false, false, false},
		{"auto + 无 Redis → 纯 L1", Config{Mode: ModeAuto}, nil, false, false, false},
		{"auto + 有 Redis → L1+L2", Config{Mode: ModeAuto}, rdb, false, true, false},
		{"l1 + 有 Redis 也不连", Config{Mode: ModeL1}, rdb, false, false, false},
		{"off → 不启用", Config{Mode: ModeOff}, rdb, true, false, false},
		{"拼错的 mode 必须报错", Config{Mode: "redi"}, rdb, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Build(c.cfg, c.rdb)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错,got nil (装配=%v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if c.wantNil != (got == nil) {
				t.Fatalf("nil 期望 %v,实际 %v", c.wantNil, got == nil)
			}
			// ⚠️ 这里必须判 `safe == nil` 而不是 L2Available()。
			// L2Available 只看 rdb,`New(nil)` 会造出一个 **非 nil 但没有连接** 的
			// Safe —— L2Available 返 false,而 GetOrLoad 照样会走进去,
			// 在 `s.rdb.Get` 上 panic。用"看起来对"的断言把这个坑留了下来。
			if got != nil && (got.safe != nil) != c.wantL2 {
				t.Fatalf("L2 期望 %v,实际 safe=%v(L2Available=%v)",
					c.wantL2, got.safe != nil, got.safe.L2Available())
			}
		})
	}
}

// TestBuild_UnknownMode_NoSideEffect 报错时不能返回一个半成品
func TestBuild_UnknownMode_NoSideEffect(t *testing.T) {
	got, err := Build(Config{Mode: "memory"}, nil)
	if err == nil {
		t.Fatal("未知 mode 应报错")
	}
	if got != nil {
		t.Fatalf("报错时不该返回实例,got %v", got)
	}
}

// TestBuild_AppliesOverrides 前缀/抖动/nullTTL 真的传下去了
func TestBuild_AppliesOverrides(t *testing.T) {
	got, err := Build(Config{
		Mode:    ModeAuto,
		Prefix:  "app",
		NullTTL: 30 * time.Second,
		Jitter:  0.5,
	}, redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"}))
	if err != nil {
		t.Fatal(err)
	}
	if got.safe.keyPrefix != "app" {
		t.Fatalf("prefix 未生效: %q", got.safe.keyPrefix)
	}
	if got.safe.nullTTL != 30*time.Second {
		t.Fatalf("nullTTL 未生效: %v", got.safe.nullTTL)
	}
	if got.safe.jitterPct != 0.5 {
		t.Fatalf("jitter 未生效: %v", got.safe.jitterPct)
	}
	// L1 key 必须带上 L2 的前缀,否则两套系统共用 Redis 时 key 会撞
	if k := got.fkey("dict:1"); k != "l1:app:dict:1" {
		t.Fatalf("TwoLevel key 拼装 = %q", k)
	}
}

// TestBuild_L1Defaults 0 值走默认常量
func TestBuild_L1Defaults(t *testing.T) {
	got, err := Build(Config{Mode: ModeL1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.l1TTL != DefaultL1TTL {
		t.Fatalf("l1TTL 应取默认 %v,got %v", DefaultL1TTL, got.l1TTL)
	}
}

// TestDescribe_OffReportsDisabled off 模式要能被日志一眼看出来
func TestDescribe_OffReportsDisabled(t *testing.T) {
	fields := Describe(nil, ModeOff)
	var enabled, seen bool
	for _, f := range fields {
		if f.Key == "enabled" {
			enabled = f.Type == 25 && f.Integer == 0 // zap Bool false
			seen = true
		}
	}
	if !seen {
		t.Fatal("off 模式必须打出 enabled 字段")
	}
	if enabled {
		t.Fatal("off 模式 enabled 应为 false")
	}
	if len(Describe(NewTwoLevel(10, time.Second, nil), ModeL1)) < 2 {
		t.Fatal("开启状态至少要打 mode + enabled + l2_enabled")
	}
}

// ── Safe 的纯函数守卫 ──
//
// ⚠️ twolevel_test.go 顶上写着 "L2 行为已由 safe_test.go 覆盖",
//    但 **safe_test.go 根本不存在** —— 三防核心(Safe.GetOrLoad)是零测试的。
//    Redis 路径按本包约定靠 dev 端到端,但纯函数部分必须补上。

// TestSafe_jitteredTTL_Bounded 抖动必须落在 [base*(1-p), base*(1+p)]
func TestSafe_jitteredTTL_Bounded(t *testing.T) {
	s := &Safe{jitterPct: DefaultJitterPct}
	base := 60 * time.Second
	lo := time.Duration(float64(base) * 0.8)
	hi := time.Duration(float64(base) * 1.2)
	for i := 0; i < 2000; i++ {
		got := s.jitteredTTL(base)
		if got < lo || got > hi {
			t.Fatalf("第 %d 次抖动越界: %v 不在 [%v,%v]", i, got, lo, hi)
		}
	}
}

// TestSafe_jitteredTTL_ZeroIsIdentity 0 抖动 / 0 基值不动它
func TestSafe_jitteredTTL_ZeroIsIdentity(t *testing.T) {
	base := time.Minute
	if got := (&Safe{jitterPct: 0}).jitteredTTL(base); got != base {
		t.Fatalf("jitter=0 应原样返回,got %v", got)
	}
	if got := (&Safe{jitterPct: 0.2}).jitteredTTL(0); got != 0 {
		t.Fatalf("base=0 应返 0,got %v", got)
	}
}

// TestWithJitter_OutOfRangeFallsBack 越界的抖动比例要落到默认,不能裸传
func TestWithJitter_OutOfRangeFallsBack(t *testing.T) {
	for _, pct := range []float64{-1, 0.9} {
		s := &Safe{}
		WithJitter(pct)(s)
		if s.jitterPct != DefaultJitterPct {
			t.Fatalf("jitter=%v 越界应回落默认 %v,got %v", pct, DefaultJitterPct, s.jitterPct)
		}
	}
	s := &Safe{}
	WithJitter(0.3)(s)
	if s.jitterPct != 0.3 {
		t.Fatalf("合法值不该被改写,got %v", s.jitterPct)
	}
}

// TestSafe_fkey 空前缀时 key 原样返回
func TestSafe_fkey(t *testing.T) {
	if got := (&Safe{}).fkey("a:1"); got != "a:1" {
		t.Fatalf("空前缀应原样返回,got %q", got)
	}
	if got := (&Safe{keyPrefix: "app"}).fkey("a:1"); got != "app:a:1" {
		t.Fatalf("got %q", got)
	}
}

// TestSafe_L2Available_NilSafe nil receiver 不能炸 —— main 可能拿到 nil
func TestSafe_L2Available_NilSafe(t *testing.T) {
	var s *Safe
	if s.L2Available() {
		t.Fatal("nil Safe 不该报 L2 可用")
	}
	if err := s.PingL2(t.Context()); err == nil {
		t.Fatal("nil Safe 的 PingL2 应返回错误")
	}
	if (&Safe{}).L2Available() {
		t.Fatal("rdb=nil 时不该报 L2 可用")
	}
}
