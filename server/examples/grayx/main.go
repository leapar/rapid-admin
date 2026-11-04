// grayx —— 灰度发布分组透传
//
// ## 这个包解决什么问题
//
// 让 canary / beta / production 这些分组信息在请求处理链中可读,
// 业务代码据此走不同分支(新功能灰度、A/B 实验)。
//
// ## 关键点
//
//   - tag 跟随 **context** 流转,不是 goroutine local
//     ⇒ 跨 RPC / 异步任务也能带过去
//   - 单一 source of truth:入口 middleware 读 header,下游 ctx 取用
//   - 缺失 tag → 返 ""(不强制要求,业务判空即可)
//
// ## 跑法
//
//	go run ./examples/grayx
package main

import (
	"context"
	"fmt"

	"rapid-admin/internal/pkg/grayx"
)

func main() {
	// 一、入口 middleware 从 header 取 tag 塞进 ctx
	//
	// 真实装配:h.Use(middleware.GrayRelease(grayx.HeaderRelease))
	ctx := grayx.WithReleaseTag(context.Background(), "canary")
	fmt.Println("当前分组 =", grayx.ReleaseTag(ctx))

	// 二、下游任意位置直接读
	if grayx.ReleaseTag(ctx) == "canary" {
		fmt.Println("→ 走新逻辑(新功能灰度分支)")
	} else {
		fmt.Println("→ 走老逻辑")
	}

	// 三、缺失就是空串 —— 业务判空即可,不用判 nil
	empty := grayx.ReleaseTag(context.Background())
	fmt.Println()
	fmt.Printf("没有 tag 时 = %q(空串,不是 nil)\n", empty)

	// 四、跨服务 / 异步任务:派生带 tag 的新 ctx
	//
	// ⚠️ 下游服务自己也有 GrayRelease middleware,会覆盖 ——
	// 防止客户端伪造 header 直接进灰度分组。
	fmt.Println()
	derived := grayx.WithReleaseTag(ctx, "beta")
	fmt.Println("派生 ctx 的分组 =", grayx.ReleaseTag(derived))
	fmt.Println("原 ctx 的分组   =", grayx.ReleaseTag(ctx), "(派生不改父 ctx)")

	// 五、常用 header 名
	fmt.Println()
	fmt.Println("header 常量:")
	fmt.Println("  分组   =", grayx.HeaderRelease)
	fmt.Println("  金丝雀 =", grayx.HeaderCanary)
	fmt.Println("  队列   =", grayx.HeaderCohort)
}
