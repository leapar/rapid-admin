// ratelimitx —— 限流原语
//
// ## 这个包解决什么问题
//
// 三种限流算法,覆盖三类场景:
//
//   - TokenBucket:令牌桶 —— 允许**突发**(桶里攒着令牌就能一次放行一批)
//   - LeakyBucket:漏桶 —— 恒定速率出水,**不允许突发**
//   - KeyedBucket:按键分桶 —— 每个 key 一个桶(IP / 用户 / 接口)
//
// ## 关键点
//
//   - 三者都是**纯内存**、无外部依赖、线程安全
//   - KeyedBucket 的 key 数量有上界(maxKeys)+ TTL 清理,
//     不会因为攻击者换 IP 而无限增长
//   - 拒绝时返回 false,**不阻塞** —— 由调用方决定返 429 还是排队
//
// ## 跑法
//
//	go run ./examples/ratelimitx
package main

import (
	"fmt"
	"time"

	"rapid-admin/internal/pkg/ratelimitx"
)

func main() {
	// ── 一、令牌桶:允许突发 ────────────────────────────────────
	fmt.Println("=== 令牌桶(容量 5,每秒补 2)===")
	tb := ratelimitx.NewTokenBucket(5, 2)
	for i := 1; i <= 8; i++ {
		ok := tb.Allow()
		fmt.Printf("  第 %d 次请求 → %v (剩余令牌 %d)\n", i, ok, tb.Available())
	}
	fmt.Printf("  统计: %+v\n", tb.Stats())
	fmt.Println("  ⇒ 桶里攒的令牌可以一次放行一批(突发),之后按 2 个/秒匀速补")

	// AllowN:一次要多个令牌(比如批量导入按条数扣)
	fmt.Println()
	fmt.Println("=== AllowN(一次要 3 个)===")
	tb2 := ratelimitx.NewTokenBucket(5, 2)
	fmt.Printf("  要 3 个 → %v (剩 %d)\n", tb2.AllowN(3), tb2.Available())
	fmt.Printf("  再要 3 个 → %v (剩 %d)\n", tb2.AllowN(3), tb2.Available())

	// ── 二、漏桶:不允许突发 ────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 漏桶(容量 5,每秒漏 2)===")
	lb := ratelimitx.NewLeakyBucket(5, 2)
	for i := 1; i <= 7; i++ {
		ok := lb.Allow()
		fmt.Printf("  第 %d 次请求 → %v (水位 %d)\n", i, ok, lb.Level())
	}
	fmt.Printf("  统计: %+v\n", lb.Stats())
	fmt.Println("  ⇒ 水位满了就拒,没有\"攒令牌\"这回事 ⇒ 恒定速率")

	// ── 三、按 key 分桶 ────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 按 key 分桶(每桶容量 2,每秒补 1)===")
	kb := ratelimitx.NewKeyedBucket(2, 1, 1000, 10*time.Minute)

	for i := 1; i <= 3; i++ {
		fmt.Printf("  IP-A 第 %d 次 → %v\n", i, kb.Allow("IP-A"))
	}
	fmt.Printf("  IP-B 第 1 次 → %v(另一个 key,互不影响)\n", kb.Allow("IP-B"))
	fmt.Printf("  当前活跃 key 数 = %d\n", kb.Len())
	defer kb.Stop()

	// Get:拿到某个 key 的桶,可以直接看它的状态
	fmt.Printf("  IP-A 的桶: %+v\n", kb.Get("IP-A").Stats())
}
