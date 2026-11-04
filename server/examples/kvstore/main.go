// kvstore —— 计数 + 滑动窗口(Redis / 内存两种实现)
//
// ## 这个包解决什么问题
//
// 两类"需要跨实例共享计数"的原语:
//
//   - Counter:计数 + TTL —— 登录失败次数用这个
//   - Window:滑动窗口限流 —— 1 分钟内最多发 N 条短信用这个
//
// 两种后端(Redis / 内存)实现同一个接口,业务代码不用改。
//
// ## 关键点
//
//   - Counter.Get:key 不存在返 **0,err 为 nil** ——
//     调用方不该靠 err 区分"不存在"
//   - Window.Allow 语义:**本次请求计入窗口** ——
//     所以"窗口内第 max 次"放行、第 max+1 次拦
//   - Resolve(mode, rdb):mode=auto 时按"有没有配 Redis"启发式选;
//     显式 redis 但没配地址 ⇒ **直接报错**,不静默降级
//
// ## 跑法
//
//	go run ./examples/kvstore
package main

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/kvstore"
)

func main() {
	ctx := context.Background()

	// ── 一、内存实现(本示例用这个)─────────────────────────────
	//
	// 生产用 kvstore.Resolve("redis", rdb);没配 Redis 时用 "memory"。
	counter := kvstore.NewMemoryCounter()
	window := kvstore.NewMemoryWindow()
	store := &kvstore.Store{Counter: counter, Window: window, Backend: "memory"}
	kvstore.SetDefault(store)

	fmt.Println("=== 计数 ===")
	for i := 1; i <= 3; i++ {
		n, err := counter.Incr(ctx, "login_fail:zhangwei", 10*time.Minute)
		fmt.Printf("  第 %d 次失败 → 累计 %d (err=%v)\n", i, n, err)
	}
	cur, _ := counter.Get(ctx, "login_fail:zhangwei")
	fmt.Printf("  读当前值 = %d\n", cur)

	missing, err := counter.Get(ctx, "不存在的key")
	fmt.Printf("  读不存在的 key = %d (err=%v ⇒ 靠 err 区分不存在是错的)\n", missing, err)

	// 登录成功 ⇒ 清零
	if err := counter.Del(ctx, "login_fail:zhangwei"); err != nil {
		fmt.Println("  清零失败:", err)
	}
	cur, _ = counter.Get(ctx, "login_fail:zhangwei")
	fmt.Printf("  清零后 = %d\n", cur)

	// ── 二、滑动窗口限流 ────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 滑动窗口(1 分钟内最多 3 次)===")
	for i := 1; i <= 5; i++ {
		ok, err := window.Allow(ctx, "sms:13800001234:login", 3, time.Minute)
		verdict := "放行"
		if !ok {
			verdict = "拦截"
		}
		fmt.Printf("  第 %d 次 → %s (err=%v)\n", i, verdict, err)
	}
	fmt.Println("  ⇒ 语义是「本次计入窗口」:第 3 次放行,第 4 次拦")

	// 不同 key 互不影响
	ok, _ := window.Allow(ctx, "sms:13900002222:login", 3, time.Minute)
	fmt.Printf("  另一个手机号第 1 次 → %v\n", ok)

	// ── 三、Resolve:显式 redis 但没配 ⇒ 报错 ────────────────────
	fmt.Println()
	fmt.Println("=== 后端选择 ===")
	s, err := kvstore.Resolve("memory", nil)
	fmt.Printf("  Resolve(\"memory\", nil) → backend=%s err=%v\n", s.Backend, err)

	_, err = kvstore.Resolve("redis", nil)
	fmt.Printf("  Resolve(\"redis\", nil) → err=%v\n", err)
	fmt.Println("  ⇒ 显式配 redis 却没给连接 ⇒ 直接报错,不静默降级成内存")
}
