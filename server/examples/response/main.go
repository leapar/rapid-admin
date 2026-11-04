// response —— 统一响应体
//
// ## 这个包解决什么问题
//
// 后端所有接口返同一种形状,前端只写一套解析:
//
//	{
//	  "code": 0,          // 0 = 成功;非 0 = 业务错误码(见 errcode 包)
//	  "data": {...},      // 业务数据
//	  "message": "ok",
//	  "timestamp": 1730000000,
//	  "trace_id": "abc123" // 出问题时拿它去搜日志
//	}
//
// ## 关键点
//
//   - HTTP 状态码与 body 里的 code **是两回事**:HTTP 说"请求成不成功",
//     code 说"为什么"。网关/监控看前者,前端分支看后者
//   - FailError 从 error 里抽业务码(errcode.CodeOf),而不是让调用方手填
//   - TraceID 随响应回传 ⇒ 用户截图里的一个 id 就能定位到那条日志
//
// ## 跑法
//
//	go run ./examples/response
package main

import (
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

func main() {
	// app.NewContext(0) 造一个空的 Hertz 上下文 —— 不用起 HTTP 服务就能看输出。
	// 每次都要新的:同一个 ctx 里的响应体是累加的。
	show := func(label string, fn func(c *app.RequestContext)) {
		c := app.NewContext(0)
		fn(c)
		fmt.Printf("%-14s %s\n", label, string(c.Response.Body()))
	}

	fmt.Println("=== 各种响应 ===")
	show("成功", func(c *app.RequestContext) {
		response.OK(c)
	})
	show("带数据", func(c *app.RequestContext) {
		response.OKWithData(c, map[string]any{"id": 8801, "name": "RapidAdmin"})
	})
	show("带提示文案", func(c *app.RequestContext) {
		response.OKWithMessage(c, map[string]any{"count": 3}, "共 3 条")
	})
	show("失败", func(c *app.RequestContext) {
		response.Fail(c, errcode.InvalidParam, "手机号不合法")
	})
	show("失败带明细", func(c *app.RequestContext) {
		response.FailWithData(c, errcode.InvalidParam, "手机号不合法",
			map[string]any{"field": "phone", "value": "1380000"})
	})
	show("从 error 抽码", func(c *app.RequestContext) {
		// ⚠️ 包装过的 error 也要能抽出业务码 —— service 层普遍用 %w 包一层
		wrapped := fmt.Errorf("查用户失败: %w", errcode.New(errcode.UserLocked, "账号已被锁定"))
		response.FailError(c, wrapped)
	})
	show("分页", func(c *app.RequestContext) {
		response.Page(c, []string{"第一条", "第二条"}, 137, 2, 20)
	})
}
