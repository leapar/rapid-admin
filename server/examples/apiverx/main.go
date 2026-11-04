// apiverx —— API 版本协商
//
// ## 这个包解决什么问题
//
// 把 HTTP header `X-API-Version` 解析成 int 挂到 ctx 上,
// handler 用 `apiverx.Major(ctx)` 读,做版本分支。
//
// ## 关键点
//
//   - 默认版本 1:没有 header → 视为 1 ⇒ 老客户端不被拒
//   - 接受 "v2" / "2" / "V2" 各种写法
//   - 解析失败**不报错**(中间件层容错);要不要拒绝由 `WithMaxMajor` 决定
//
// ## 装配
//
//	h.Use(middleware.APIVersion())                        // 默认仅注入
//	h.Use(middleware.APIVersion(apiverx.WithMaxMajor(3))) // 超过 3 返 422
//
// ## 跑法
//
//	go run ./examples/apiverx
package main

import (
	"context"
	"fmt"

	"rapid-admin/internal/pkg/apiverx"
)

func main() {
	// 一、header 解析:容错的
	fmt.Println("解析 X-API-Version:")
	for _, raw := range []string{"", "1", "2", "v3", "V4", "1.2", "abc", "0"} {
		v, ok := apiverx.Parse(raw)
		label := raw
		if label == "" {
			label = "(空)"
		}
		fmt.Printf("  %-8q → 版本 %d, 合法=%v\n", label, v, ok)
	}

	// 二、ctx 取值:没有就是默认版本 1
	ctx := context.Background()
	fmt.Println()
	fmt.Printf("空 ctx 的版本 = %d(默认版本)\n", apiverx.Major(ctx))

	// 三、中间件注入后,handler 读到的就是那个版本
	ctx = apiverx.WithVersion(ctx, 2)
	fmt.Printf("注入 v2 后  = %d\n", apiverx.Major(ctx))

	// 四、handler 里做版本分支
	if apiverx.Major(ctx) >= 2 {
		fmt.Println("→ v2+ 的新字段结构")
	} else {
		fmt.Println("→ v1 的老字段结构")
	}

	// 五、最大版本限制:中间件据此决定返 422 还是放行
	fmt.Println()
	opt := &apiverx.Opt{}
	apiverx.WithMaxMajor(3)(opt)
	fmt.Printf("最大支持版本 = %d\n", opt.MaxMajor)
	fmt.Printf("请求版本 %d 是否超限 = %v\n", apiverx.Major(ctx), apiverx.Major(ctx) > opt.MaxMajor)
}
