// cachex —— 两级缓存(L1 本地 LRU + L2 Redis)
//
// ## 这个包解决什么问题
//
// 字典、配置、资源树这些"读多写少、不能每次都打 DB"的数据,
// 需要一层缓存。cachex 提供:
//
//   - L1:进程内 LRU(微秒级,不跨实例)
//   - L2:Redis(跨实例共享)
//   - cache-aside:GetOrLoad —— 未命中才回源
//
// ## 关键点
//
//   - **GetOrLoadTyped 是主力入口**。Safe.GetOrLoad 返 any,
//     内部把 JSON 反序列化进了 any ⇒ 业务拿到的是 *map[string]any,
//     要拿回结构体还得自己再 Marshal 一次。这个摩擦正是"框架能力写完却零调用"
//     的原因 —— 框架提供的接口不能比手写 map 缓存更难用。
//   - **fail-open**:缓存里的值反序列化不回来(发版改了结构、老 key 还在)
//     时**不当错误**,记 warn 后直接回源。慢一点总比把错数据返给业务好。
//   - **L2 不可用时自动降级到 L1**,不报错。
//   - 前缀 + DeletePrefix:按业务前缀批量失效(dict: / cfg: / resource:)。
//
// ## 跑法
//
//	go run ./examples/cachex
//
// 本示例用内存 backend(不需要 Redis),换成 RedisBackend 只需一行。
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/cachex"
)

// dict 缓存里的结构体 —— 用泛型入口就不会退化成 map
type dict struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

func main() {
	ctx := context.Background()

	// ── 一、装配 ────────────────────────────────────────────────
	//
	// 生产用 cachex.New(rdb) 或 cachex.NewRedisBackend(rdb);
	// 这里用内存 backend,示例不需要外部依赖。
	backend := cachex.NewMemoryBackend()
	safe := cachex.NewWithBackend(backend,
		cachex.WithPrefix("demo:"),
		cachex.WithJitter(0.2),            // TTL 加 ±20% 抖动,避免缓存雪崩
		cachex.WithNullTTL(2*time.Minute), // 空值缓存 2 分钟,防缓存穿透
	)
	tl := cachex.NewTwoLevel(5000, 10*time.Second, safe)
	cachex.SetDefault(tl)

	fmt.Println("=== 装配 ===")
	fmt.Printf("L2 可用 = %v\n", safe.L2Available())
	fmt.Printf("统计 = %+v\n", safe.Stats())

	// ── 二、Set / Get ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 读写 ===")
	if err := tl.Set(ctx, "demo:dict:gongqu", map[string]string{"label": "公元"}, 5*time.Minute); err != nil {
		fmt.Println("写失败:", err)
		return
	}
	v, hit := tl.Get("demo:dict:gongqu")
	fmt.Printf("读回来: %v (hit=%v)\n", v, hit)

	// ── 三、GetOrLoadTyped:cache-aside 的主力入口 ───────────────
	fmt.Println()
	fmt.Println("=== cache-aside(泛型)===")
	loads := 0
	loader := func(ctx context.Context) (*dict, error) {
		loads++
		return &dict{Code: "gongqu", Label: "公元"}, nil
	}
	d, hit, err := cachex.GetOrLoadTyped(ctx, tl, "demo:dict:cacheaside", 5*time.Minute, loader)
	fmt.Printf("第一次: %+v hit=%v err=%v(回源次数=%d)\n", d, hit, err, loads)

	d, hit, err = cachex.GetOrLoadTyped(ctx, tl, "demo:dict:cacheaside", 5*time.Minute, loader)
	fmt.Printf("第二次: %+v hit=%v err=%v(回源次数=%d ⇒ 命中缓存)\n", d, hit, err, loads)

	// ── 四、fail-open:回源失败时把错误原样抛出 ─────────────────
	fmt.Println()
	fmt.Println("=== 回源失败 ===")
	boom := errors.New("db 连不上")
	_, _, err = cachex.GetOrLoadTyped(ctx, tl, "demo:dict:missing", time.Minute,
		func(ctx context.Context) (*dict, error) { return nil, boom })
	fmt.Printf("回源出错时 err = %v(原样抛出,不当成缓存命中)\n", err)

	// ── 五、按前缀批量失效 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 批量失效 ===")
	_ = tl.Set(ctx, "demo:dict:a", "A", time.Minute)
	_ = tl.Set(ctx, "demo:dict:b", "B", time.Minute)
	_ = tl.Set(ctx, "demo:other:c", "C", time.Minute)
	n, err := tl.DeletePrefix(ctx, "demo:dict:")
	fmt.Printf("删掉 demo:dict: 前缀的 %d 个 key (err=%v)\n", n, err)
	_, hit = tl.Get("demo:dict:a")
	fmt.Printf("再读 demo:dict:a → hit=%v\n", hit)
}
