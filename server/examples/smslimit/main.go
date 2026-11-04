// smslimit —— 短信发送限流
//
// ## 这个包解决什么问题
//
// 防止有人拿短信接口刷验证码(短信是要花钱的,而且会被服务商风控)。
// 三道闸:冷却期 → 1 分钟窗口 → 1 天窗口。
//
// ## 关键点
//
//   - **0 / 负数 ⇒ 用默认值,不是"不限"**。M112 之前填 0 会被当成
//     "不限"⇒ 限流整个失效。默认:1 分钟 1 条、1 天 10 条、冷却 60 秒
//   - **Redis 故障 ⇒ 放行**,不阻塞主流程(短信发不出去有 DB 兜底,
//     但把用户卡在"发送失败"上更糟)
//   - **Redis 未配置(rdb=nil)⇒ 放行**(本地 dev 没有 redis 是常态)
//   - **窗口 TTL 只在计数器刚创建时设一次**(windowScript 里 `if c == 1`)。
//     旧实现每次都 EXPIRE ⇒ 计数永不过期 ⇒ 手机号被**永久拉黑**
//   - **日志里不打 key** —— key 里含手机号(明文 PII)
//   - **判定顺序:冷却期 → 1 分钟 → 1 天**。冷却期先判,是为了让
//     "冷却期内重复发送"这条短窗口不消耗窗口配额
//   - **被拒的请求不计数**。窗口计数在判超限之前就加了 —— 所以
//     "第 3 次开始被拒"之后计数还会继续涨(不影响判定:已经超了就一直超)
//
// ## 跑法
//
//	go run ./examples/smslimit
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/pkg/smslimit"
)

func main() {
	ctx := context.Background()

	// ── 一、装配(用内存 Redis,不用真连)─────────────────────────
	fmt.Println("=== 装配 ===")
	mr, err := miniredis.Run()
	if err != nil {
		fmt.Println("启动内存 Redis 失败:", err)
		return
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	smslimit.Init(rdb, smslimit.Config{
		PerMinute:    2, // 1 分钟内最多 2 条
		PerDay:       5, // 1 天内最多 5 条
		CooldownSecs: 2, // 冷却 2 秒(示例里调短,好让窗口那条闸露出来)
	})
	fmt.Println("限流已开:1 分钟 2 条 / 1 天 5 条 / 冷却 2 秒")

	// ── 二、冷却期 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 冷却期 ===")
	fmt.Printf("  第 1 次 → %s\n", verdict(smslimit.Allow(ctx, "13800001234", "login")))
	fmt.Printf("  第 2 次(冷却期内)→ %s\n", verdict(smslimit.Allow(ctx, "13800001234", "login")))
	fmt.Println("  ⇒ 冷却期先判,这条短窗口不消耗窗口配额")

	// 换个手机号 —— 各算各的
	fmt.Printf("  换个手机号第 1 次 → %s\n", verdict(smslimit.Allow(ctx, "13900002222", "login")))

	// 换个场景 —— 冷却期也是按「手机号 + 场景」算的
	fmt.Printf("  换个场景第 1 次 → %s\n", verdict(smslimit.Allow(ctx, "13800001234", "reset_password")))

	// ── 三、1 分钟窗口 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 1 分钟窗口 ===")
	// 每一次调用前把内存 Redis 的时间往前推 3 秒 ⇒ 冷却期(2s)一定已经过期,
	// 于是这一段里真正在拦人的只有 1 分钟窗口
	for i := 1; i <= 3; i++ {
		mr.FastForward(3 * time.Second)
		fmt.Printf("  第 %d 次 → %s\n", i, verdict(smslimit.Allow(ctx, "13700003333", "login")))
	}
	fmt.Println("  ⇒ 上限 2 条 ⇒ 第 3 次开始被窗口拦下")

	// ── 四、1 天窗口 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 1 天窗口 ===")
	// 每次调用前推进 61 秒 ⇒ 1 分钟窗口每次都过期(计数归 1,永远不拦人),
	// 于是只剩 1 天窗口在拦 —— 上限 5 条,第 6 次被拦
	for i := 1; i <= 6; i++ {
		mr.FastForward(61 * time.Second)
		fmt.Printf("  第 %d 次 → %s\n", i, verdict(smslimit.Allow(ctx, "13600004444", "login")))
	}
	fmt.Println("  ⇒ 上限 5 条 ⇒ 第 6 次被 1 天窗口拦下")

	// ── 五、Redis 故障 ⇒ 放行 ──────────────────────────────────
	fmt.Println()
	fmt.Println("=== Redis 故障 ===")
	mr.Close()
	fmt.Printf("  Redis 挂了 → %s\n", verdict(smslimit.Allow(ctx, "13500005555", "login")))
	fmt.Println("  ⇒ 放行,不阻塞主流程 —— 短信发不出去有 DB 兜底,")
	fmt.Println("    但把用户卡在「发送失败」上更糟")
}

// verdict 把 err 渲染成一行人话
func verdict(err error) string {
	if err == nil {
		return "放行"
	}
	return "拦截:" + err.Error()
}
