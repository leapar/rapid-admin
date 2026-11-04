// idempotentx —— 幂等键
//
// ## 这个包解决什么问题
//
// 写操作(POST/PUT/PATCH/DELETE)的重复请求不重复执行业务逻辑。
// 客户端在 `Idempotency-Key` 头里带一个 key,同 key 在 TTL 内重放
// 直接返首次结果。
//
// ## 关键点
//
//   - **两把 key,不是一个状态机** —— `idem:res:<key>`(结果,TTL 24h,
//     业务跑完才有,内容是 {state,http_status,body})与 `idem:lock:<key>`
//     (锁,TTL 30s,有人在跑,跑完就删)。为什么要分两个:锁要短(30s,免得业务
//     卡住把 key 锁死),结果要长(24h,免得用户重试时业务又跑一遍)。
//     一个 key 做不到"两套 TTL"
//
//   - **"正在跑"这个状态活在锁 key 的存在性里**,结果 key 里永远只有
//     done / failed。看到有代码判断 `State == "running"` 就是错的
//
//   - **键空间必须是单射**:结果前缀 `idem:res:` 与锁前缀 `idem:lock:`
//     不同 ⇒ 任何客户端 key 都撞不上另一个 key 的锁
//
//   - **业务 handler 失败也缓存**(返业务错误码)。panic 不缓存,
//     且**锁会被释放**,让客户端重试能再跑一次
//
//   - **"业务压根没跑成"的响应不缓存**(401/403/429)——
//     写进来等于告诉客户端"这个 key 已经用掉了",重试会被重放成同一个 401
//
// ## 跑法
//
//	go run ./examples/idempotentx
package main

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/idempotentx"
)

func main() {
	ctx := context.Background()

	// ── 一、Manager 的构造 ──────────────────────────────────────
	fmt.Println("=== Manager ===")
	m := idempotentx.New(nil, 30*time.Second, 24*time.Hour)
	fmt.Printf("%s\n", m.String())
	fmt.Println("  ⇒ New(nil, ...) 表示不接 Redis")

	// 不接 Redis 时 AcquireOrGet **一律放行** ——
	// Redis 挂了不该把所有写操作都挡住(挡住比不幂等更糟)。
	fmt.Println()
	fmt.Println("=== 没接 Redis 时的行为 ===")
	acquired, result, err := m.AcquireOrGet(ctx, "demo-key-1")
	fmt.Printf("  acquired = %v, result = %v, err = %v\n", acquired, result, err)
	fmt.Println("  ⇒ 未 Init / Redis 不可用 → 放行,降级成不幂等而不是全站 409")

	// Save / Release 在没 Redis 时是 no-op
	m.Save(ctx, "demo-key-1", 200, []byte(`{"code":0}`))
	m.Release(ctx, "demo-key-1")
	fmt.Println("  ⇒ Save / Release 在没 Redis 时是 no-op,不会 panic")

	// ── 二、空 key ─────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 空 key ===")
	acquired, _, err = m.AcquireOrGet(ctx, "")
	fmt.Printf("  acquired = %v, err = %v\n", acquired, err)
	fmt.Println("  ⇒ 空 key 放行:否则所有人共用一把空锁,而 Save/Release 因为")
	fmt.Println("    idemKey==\"\" 直接 return,那把锁永远不会被释放")

	// ── 三、ErrInFlight ─────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== ErrInFlight ===")
	fmt.Printf("  = %v\n", idempotentx.ErrInFlight)
	fmt.Println("  ⇒ 拿到它说明「有另一个请求正在跑」,中间件返 409 给客户端")

	// ── 四、状态只有两种 ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 结果状态 ===")
	fmt.Printf("  StateDone   = %q\n", idempotentx.StateDone)
	fmt.Printf("  StateFailed = %q\n", idempotentx.StateFailed)
	fmt.Println("  ⇒ 只有这两种。「正在跑」活在锁 key 的存在性里,不是第三种状态")

	// ── 五、Result 的形状 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== Result ===")
	r := idempotentx.Result{
		State:      idempotentx.StateDone,
		HTTPStatus: 200,
		Body:       []byte(`{"code":0,"data":{"id":8801}}`),
		WrittenAt:  time.Now(),
	}
	fmt.Printf("  state       = %s\n", r.State)
	fmt.Printf("  http_status = %d\n", r.HTTPStatus)
	fmt.Printf("  body        = %s\n", r.Body)
	fmt.Println("  ⇒ body 里存的是**原始响应字节**(含 code/message/data/request_id),")
	fmt.Println("    所以重放时客户端拿到的和第一次完全一样")

	// ── 六、全局单例 ───────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 全局单例 ===")
	fmt.Printf("  M() = %v\n", idempotentx.M())
	fmt.Println("  ⇒ main 里 Init 一次装全局,中间件通过 M() 取;")

	// ── 七、幂等键的判据 ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 什么时候才生效 ===")
	fmt.Println("  仅 method ∈ {POST, PUT, PATCH, DELETE}")
	fmt.Println("  仅当请求带了 Idempotency-Key 头")
	fmt.Println("  ⇒ GET 不做幂等:同一个 GET 重复调用本来就该重复执行")
}
