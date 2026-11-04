package redisint

import (
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/smslimit"
)

// smslimit 的 Lua 固定窗口(windowScript)在真 Redis 上的行为。
//
// ⚠️ 之前只有 miniredis 覆盖 —— 而 windowScript 是 **Lua**,
// miniredis 用 gopher-lua 解释,Redis 用自己的 Lua 解释器,
// 两者在 `tonumber` / 数字比较 / 返回值类型上有过差异。
//
// 要守的性质(M112 修的那个"固定窗口"bug):
//  1. **TTL 只在计数器刚创建时设一次** —— 反复请求不能把计数续命
//     (旧实现每次都 EXPIRE ⇒ 手机号被永久拉黑)
//  2. 计数超过上限 → 拒
func TestSmslimit_真Redis上的固定窗口(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	// cooldown 取 1s,好让冷却期先过去,窗口才轮得到说话
	lim := smslimit.New(rdb, smslimit.Config{
		PerMinute:    100,
		PerDay:       2,
		CooldownSecs: 1,
	})

	const phone = "13800000001"
	const scene = "login"

	// 第 1、2 条放行
	for i := 1; i <= 2; i++ {
		time.Sleep(1100 * time.Millisecond) // 等冷却期过去
		if err := lim.Allow(ctx, phone, scene); err != nil {
			t.Fatalf("第 %d 条应放行,got %v", i, err)
		}
	}

	// 1 天窗口的 TTL 必须是 24h 附近,**不能被后续请求推后**
	ttl, err := rdb.TTL(ctx, "sms:win:v2:1d:"+phone+":"+scene).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		t.Fatalf("1 天窗口的 TTL 应在 (0, 24h],got %v", ttl)
	}
	firstTTL := ttl

	// 第 3 条:1 天窗口超限 → 拒
	time.Sleep(1100 * time.Millisecond)
	err = lim.Allow(ctx, phone, scene)
	if err == nil {
		t.Fatal("1 天窗口超限后应被拒")
	}
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.SmsTooFrequent {
		t.Fatalf("应返 SmsTooFrequent,got %v", err)
	}

	// ⚠️ **被拒的这次也会 INCR**(计数在判超限之前就加了)——
	//    所以 TTL 仍然不能被推后(旧实现就是在这里把手机号永久拉黑的)。
	ttl2, err := rdb.TTL(ctx, "sms:win:v2:1d:"+phone+":"+scene).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl2 > firstTTL {
		t.Fatalf("TTL 被推后了 —— 计数窗口会被无限续长(手机号会被永久拉黑):"+
			"first=%v now=%v", firstTTL, ttl2)
	}
}

// 冷却期:同 phone+scene 在冷却期内只放行一次。
func TestSmslimit_冷却期(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	lim := smslimit.New(rdb, smslimit.Config{
		PerMinute:    100,
		PerDay:       100,
		CooldownSecs: 60,
	})

	const phone = "13800000002"
	const scene = "login"

	if err := lim.Allow(ctx, phone, scene); err != nil {
		t.Fatalf("第一次应放行,got %v", err)
	}
	err := lim.Allow(ctx, phone, scene)
	if err == nil {
		t.Fatal("冷却期内第二次应被拒")
	}
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.SmsSendTooFrequent {
		t.Fatalf("应返 SmsSendTooFrequent,got %v", err)
	}
	// 别的场景不受冷却影响
	if err := lim.Allow(ctx, phone, "reset"); err != nil {
		t.Fatalf("别的场景应放行,got %v", err)
	}
}

// Redis 故障 → 放行(主流程不阻塞,DB 层 sys_sms_code 兜底)。
//
// ⚠️ 这是"安全阀失效"的边界:限流是安全阀,Redis 挂了就等于闸门没了,
// 但**绝不能因此让整个发短信接口不可用**。
func TestSmslimit_Redis故障时放行(t *testing.T) {
	// 连一个没人听的端口
	dead := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
		ReadTimeout: 200 * time.Millisecond,
	})
	t.Cleanup(func() { _ = dead.Close() })

	lim := smslimit.New(dead, smslimit.Config{PerMinute: 1, PerDay: 1, CooldownSecs: 60})
	if err := lim.Allow(ctx5(t), "13800000003", "login"); err != nil {
		t.Fatalf("Redis 故障时应放行,got %v", err)
	}
}
