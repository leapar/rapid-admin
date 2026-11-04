// 中断语义(Abort)的回归网 —— hertz 的坑,踩一次就够了
//
// 这几条不是测某个业务中间件,而是把 **hertz 引擎的中断语义**钉死:中间件想
// 拦下请求必须显式 `c.Abort()`,光 `return` 没用。
//
// 为什么值得单独一个文件:症状是**响应完全正常**(200 + 业务数据),只是 handler
// 多跑了一遍。之前 BodyLimit 和 Idempotency 都中过同一个招 ——
//   - BodyLimit:写完 413 直接 return,handler 跑完用 200 + body 覆盖 → 上传接口
//     实际上一次都没被拦过
//   - Idempotency:409 冲突 / 命中重放两条路都直接 return → 并发同 key 的请求
//     **真的执行了两遍**,正是幂等要防的那件事,而客户端看到的是"成功了两次"
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"

	"rapid-admin/internal/pkg/idempotentx"
)

// idempotentxM 取全局 manager,包一层只为测试可读
func idempotentxM() *idempotentx.Manager { return idempotentx.M() }

// runAbortCase 跑一次「中间件写一个状态码后要不要 Abort」的用例
func runAbortCase(t *testing.T, abort bool) (int, bool, string) {
	t.Helper()
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(func(ctx context.Context, c *app.RequestContext) {
		if abort {
			c.Abort()
		}
		c.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
		c.SetStatusCode(consts.StatusRequestEntityTooLarge)
		c.Response.SetBody([]byte(`{"code":41300}`))
		return
	})
	ran := false
	engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		ran = true
		c.String(http.StatusOK, "handler-ran")
	})

	w := ut.PerformRequest(engine, "POST", "/x", &ut.Body{})
	return w.Code, ran, w.Body.String()
}

// TestAbort_RequiredToStopHandlerChain 这条如果哪天变红,说明 hertz 换了语义
// (或者我们把 Abort 改成了别的东西)—— 那时本文件里所有中间件的判断都要重审。
func TestAbort_RequiredToStopHandlerChain(t *testing.T) {
	code, ran, body := runAbortCase(t, true)
	if ran || code != http.StatusRequestEntityTooLarge || body != `{"code":41300}` {
		t.Fatalf("Abort 后应拦下:code=%d ran=%v body=%q", code, ran, body)
	}

	// 反面对照:不 Abort 时 handler **照样跑**,而且它的 200 + body 会串到中间件
	// 写的 413 后面去,最终响应是"413 的 JSON 后面又跟了一段业务数据"。
	// 这就是修复前的真实行为 —— 所以这一条是"复现旧 bug",不是"期望行为"。
	//
	// ⚠️ 断言只能落在 **status 和 ran** 上,不能断言完整 body:body 的具体形态
	// 取决于 hertz Response 的缓冲实现,那是实现细节;而"handler 跑没跑"和
	// "状态码是谁的"才是这个中间件真正的语义。
	code, ran, _ = runAbortCase(t, false)
	if !ran || code != http.StatusOK {
		t.Fatalf("不 Abort 应复现旧行为(handler 跑完并改写状态码):code=%d ran=%v", code, ran)
	}
}

// TestIdempotency_NoManagerIsPassThrough 未 Init 时中间件必须完全放行。
//
// 这条同时是"Idempotency 中间件没有误 Abort"的守卫:一个装在全局链上的
// no-op 中间件,一旦不小心 Abort 掉,整个平台的所有写接口会静默返回空响应。
func TestIdempotency_NoManagerIsPassThrough(t *testing.T) {
	if idempotentxM() != nil {
		t.Skip("全局 idempotentx manager 已初始化,本用例只覆盖 nil 分支")
	}
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(Idempotency())
	ran := false
	engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		ran = true
		c.String(http.StatusOK, "ok")
	})

	w := ut.PerformRequest(engine, "POST", "/x", &ut.Body{},
		ut.Header{Key: "Idempotency-Key", Value: "k1"},
	)
	if !ran || w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("无 manager 时应完全放行:code=%d ran=%v body=%q", w.Code, ran, w.Body.String())
	}
}
