package response

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	"rapid-admin/internal/pkg/errcode"
)

// ── M98(2026-10-26)可逆探针记录 ───────────────────────────────────────
//
//   拆法                                          必须变红
//   ─────────────────────────────────────────  ──────────────────────────────────
//   P1 response.Fail 改用固定 200                TestFail_HTTPStatus
//   P2 response.Fail 改用固定 500                TestFail_HTTPStatus
//   P3 Fail 里的 50100 特判去掉(落回 500)        TestFail_HTTPStatus/NotImplemented
//   P4 FailWithData 改用固定 400                 TestFailWithData
//   P5 FailError 改用 errcode.Internal 写死      TestFailError_WrappedKeepsCode
//   P6 FailError 里的 CodeOf 换成"永远 Internal"  TestFailError_WrappedKeepsCode
//   P7 OKWithData 把 code 写成 1                  TestOK_BodyShape
//   P8 Page 的 data 换成 list 本身(不带分页壳)   TestPage_BodyShape
//
//   8/8 全部变红。
//
// ⚠️ **P1 与 P2 的区别**:两个都让"状态码不对",但拆法不同 ——
// P1 把映射换成常量 200,P2 换成常量 500。两条都换常量看着像重复,
// 其实只有都做才能区分"映射整体错位"和"整体偏保守"两种坏法。
//
// ⚠️ **P3 只能被 §1 那张表盯住**:50100 在 M98 之前是从 5xxxx 兜底桶
// 落 500 的,不是专门写死的 —— 拆法是"把 NotImplemented 那个 case 删掉",
// 它自然滑回 500 大桶。
// ───────────────────────────────────────────────────────────────────────

// newEngine 造一个只挂响应helper的引擎
func newEngine() *route.Engine {
	return route.NewEngine(config.NewOptions([]config.Option{}))
}

// bodyOf 解析响应 body
func bodyOf(t *testing.T, w *ut.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应不是合法 JSON: %v (body=%q)", err, w.Body.String())
	}
	return m
}

// ── 1. HTTP 状态码 ─────────────────────────────────────────────────────

// TestFail_HTTPStatus 业务码 → HTTP 状态码
//
// # 这是本包的**唯一真源测试**
//
// 响应包以前自己有一份 `httpStatus` 拷贝(私有),跟 `errcode.HTTPStatus`
// 各写各的。活的那份**没有 413、没有 501** —— 于是:
//   - 41300(请求体过大)→ 400(前端以为是参数错,不是"包太大")
//   - 50100(功能未实现)→ 500(前端弹"服务器内部错误",不看 body 里的业务码)
//
// 现在响应包直接调 errcode.HTTPStatus,这条测试锁住的是**契约**。
func TestFail_HTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"成功码", errcode.Success, http.StatusOK},
		{"请求参数错", errcode.BadRequest, http.StatusBadRequest},
		{"参数缺失", errcode.ParamMissing, http.StatusBadRequest},
		{"参数无效", errcode.InvalidParam, http.StatusBadRequest},
		{"未登录", errcode.Unauthorized, http.StatusUnauthorized},
		{"令牌过期", errcode.TokenExpired, http.StatusUnauthorized},
		{"令牌无效", errcode.TokenInvalid, http.StatusUnauthorized},
		{"没有权限", errcode.Forbidden, http.StatusForbidden},
		{"身份停用", errcode.NoActiveIdentity, http.StatusForbidden},
		{"越权", errcode.IdorAttempt, http.StatusForbidden},
		{"资源不存在", errcode.NotFound, http.StatusNotFound},
		{"数据冲突", errcode.Conflict, http.StatusConflict},
		// ⚠️ M98 修的第一条:以前返 400
		{"请求体过大", errcode.PayloadTooLarge, http.StatusRequestEntityTooLarge},
		{"限流", errcode.TooManyRequests, http.StatusTooManyRequests},
		{"内部错误", errcode.Internal, http.StatusInternalServerError},
		// ⚠️ M98 修的第二条:以前返 500
		{"功能未实现", errcode.NotImplemented, http.StatusNotImplemented},
		{"服务不可用", errcode.ServiceUnavailable, http.StatusServiceUnavailable},
		{"网关超时", errcode.GatewayTimeout, http.StatusGatewayTimeout},
		// 业务段:200
		{"登录失败", errcode.LoginFailed, http.StatusOK},
		{"审批无模板", errcode.ApprovalNoTemplate, http.StatusOK},
		{"配置不存在", errcode.ConfigNotFound, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEngine()
			e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
				Fail(c, tc.code, "msg")
			})
			w := ut.PerformRequest(e, "GET", "/x", &ut.Body{})
			if w.Code != tc.want {
				t.Fatalf("码 %d 应返 HTTP %d,got %d (body=%q)", tc.code, tc.want, w.Code, w.Body.String())
			}
			m := bodyOf(t, w)
			if got, _ := m["code"].(float64); int(got) != tc.code {
				t.Errorf("body.code = %v, want %d", m["code"], tc.code)
			}
		})
	}
}

// TestFail_NotImplementedIs501 「功能未实现」必须是 501 不是 500
//
// 单独拎出来一条,因为它是 M98 里**代价最具体**的一处:
//
// 501 = "这个 provider 没有这个能力"(该换条路走)
// 500 = "它炸了"(该重试 / 该告警)
//
// 前端 `api/client.ts` 的响应拦截器里:
//
//	case 500 → 只弹"服务器内部错误"的通用文案,**根本不看 body 里的业务码**
//	default → tError(业务码) → 查 i18n
//
// 50100 在 `web/src/locales/zh-CN.ts` 里是有译文的("该功能尚未实现"),
// 但只要 HTTP 状态是 500,那条译文就**永远显示不出来**。
func TestFail_NotImplementedIs501(t *testing.T) {
	e := newEngine()
	e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		Fail(c, errcode.NotImplemented, "功能未实现: platform.ops")
	})
	w := ut.PerformRequest(e, "GET", "/x", &ut.Body{})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("50100 应返 501,got %d", w.Code)
	}
	m := bodyOf(t, w)
	if got, _ := m["code"].(float64); int(got) != errcode.NotImplemented {
		t.Errorf("body.code = %v, want %d", m["code"], errcode.NotImplemented)
	}
}

// ── 2. 响应体形状 ──────────────────────────────────────────────────────

// TestFail_BodyShape 失败响应的 body 形状
func TestFail_BodyShape(t *testing.T) {
	e := newEngine()
	e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		Fail(c, errcode.Forbidden, "没有权限")
	})
	w := ut.PerformRequest(e, "GET", "/x", &ut.Body{})
	m := bodyOf(t, w)
	if m["message"] != "没有权限" {
		t.Errorf("body.message = %v, want %q", m["message"], "没有权限")
	}
	if _, ok := m["timestamp"].(float64); !ok {
		t.Errorf("body.timestamp 缺失或不是数字: %v", m["timestamp"])
	}
	// Fail 不带 data —— data 是 omitempty,不该出现空壳
	if _, ok := m["data"]; ok {
		t.Errorf("Fail 的 body 不该带 data 字段: %v", m["data"])
	}
}

// TestFailWithData 失败响应(带明细)
//
// ⚠️ **必须用一条状态码不是 400 的码**。
// 早先这条测试用的是 40003(本身就该返 400),于是把 FailWithData 写死成
// 400 的变异**照样绿** —— 测试没盯住那个改动(可逆探针 ⑩ 抓到的)。
// 现在补 403 / 501:写死 400 就一定会红。
func TestFailWithData(t *testing.T) {
	cases := []struct {
		name string
		code int
		want int
	}{
		{"参数不合法", errcode.InvalidParam, http.StatusBadRequest},
		{"没有权限", errcode.Forbidden, http.StatusForbidden},
		{"功能未实现", errcode.NotImplemented, http.StatusNotImplemented},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEngine()
			e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
				FailWithData(c, tc.code, "字段不合法", map[string]any{"field": "email"})
			})
			w := ut.PerformRequest(e, "GET", "/x", &ut.Body{})
			if w.Code != tc.want {
				t.Fatalf("码 %d 应返 HTTP %d,got %d", tc.code, tc.want, w.Code)
			}
			m := bodyOf(t, w)
			if got, _ := m["code"].(float64); int(got) != tc.code {
				t.Errorf("body.code = %v, want %d", m["code"], tc.code)
			}
			data, ok := m["data"].(map[string]any)
			if !ok {
				t.Fatalf("body.data 不是对象: %v", m["data"])
			}
			if data["field"] != "email" {
				t.Errorf("body.data.field = %v, want %q", data["field"], "email")
			}
		})
	}
}

// TestOK_BodyShape 成功响应的 body 形状
func TestOK_BodyShape(t *testing.T) {
	e := newEngine()
	e.GET("/empty", func(ctx context.Context, c *app.RequestContext) { OK(c) })
	e.GET("/data", func(ctx context.Context, c *app.RequestContext) {
		OKWithData(c, map[string]any{"id": "123"})
	})
	e.GET("/msg", func(ctx context.Context, c *app.RequestContext) {
		OKWithMessage(c, nil, "已保存")
	})

	w := ut.PerformRequest(e, "GET", "/empty", &ut.Body{})
	if w.Code != http.StatusOK {
		t.Fatalf("OK 应返 200,got %d", w.Code)
	}
	m := bodyOf(t, w)
	if got, _ := m["code"].(float64); got != 0 {
		t.Errorf("成功响应的 body.code = %v, want 0", m["code"])
	}
	if m["message"] != "ok" {
		t.Errorf("body.message = %v, want %q", m["message"], "ok")
	}

	w = ut.PerformRequest(e, "GET", "/data", &ut.Body{})
	m = bodyOf(t, w)
	// ⚠️ 三条路由**都要**断言 code == 0。早先只断言了 /empty 那条,
	// 于是把 OKWithData 的 code 改成 1 的变异照样绿(可逆探针 ⑫ 抓到的)。
	if got, _ := m["code"].(float64); got != 0 {
		t.Errorf("OKWithData 的 body.code = %v, want 0", m["code"])
	}
	data, ok := m["data"].(map[string]any)
	if !ok || data["id"] != "123" {
		t.Errorf("body.data = %v, want {id:123}", m["data"])
	}

	w = ut.PerformRequest(e, "GET", "/msg", &ut.Body{})
	m = bodyOf(t, w)
	if got, _ := m["code"].(float64); got != 0 {
		t.Errorf("OKWithMessage 的 body.code = %v, want 0", m["code"])
	}
	if m["message"] != "已保存" {
		t.Errorf("body.message = %v, want %q", m["message"], "已保存")
	}
}

// TestPage_BodyShape 分页响应的 body 形状
func TestPage_BodyShape(t *testing.T) {
	e := newEngine()
	e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		Page(c, []string{"a", "b"}, 42, 2, 10)
	})
	w := ut.PerformRequest(e, "GET", "/x", &ut.Body{})
	if w.Code != http.StatusOK {
		t.Fatalf("Page 应返 200,got %d", w.Code)
	}
	m := bodyOf(t, w)
	data, ok := m["data"].(map[string]any)
	if !ok {
		t.Fatalf("body.data 不是对象: %v", m["data"])
	}
	if got, _ := data["total"].(float64); int(got) != 42 {
		t.Errorf("data.total = %v, want 42", data["total"])
	}
	if got, _ := data["page"].(float64); int(got) != 2 {
		t.Errorf("data.page = %v, want 2", data["page"])
	}
	if got, _ := data["size"].(float64); int(got) != 10 {
		t.Errorf("data.size = %v, want 10", data["size"])
	}
	list, ok := data["list"].([]any)
	if !ok || len(list) != 2 {
		t.Errorf("data.list = %v, want 2 项", data["list"])
	}
}

// ── 3. FailError ───────────────────────────────────────────────────────

// TestFailError_PlainError 普通 error → Internal(500)
func TestFailError_PlainError(t *testing.T) {
	e := newEngine()
	e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		FailError(c, fmt.Errorf("boom"))
	})
	w := ut.PerformRequest(e, "GET", "/x", &ut.Body{})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("普通 error 应返 500,got %d", w.Code)
	}
	m := bodyOf(t, w)
	if got, _ := m["code"].(float64); int(got) != errcode.Internal {
		t.Errorf("body.code = %v, want %d", m["code"], errcode.Internal)
	}
}

// TestFailError_WrappedKeepsCode 被包装的业务错误要保住业务码与状态码
//
// # M98 修的第三点
//
// `errcode.CodeOf` 原来用裸类型断言 `err.(*Error)`,只认最外层。
// 服务层用 `fmt.Errorf("...: %w", err)` 包装错误是常态 —— 断言失败
// → CodeOf 返 Internal → 客户端拿到 500,真正的业务码在包装层被吃掉。
//
// 而且**同一个错误**走 `handler.BaseHandler.Fail`(用 errors.As)是 403,
// 走 `response.FailError`(用 CodeOf)是 500 —— 两条出口两种语义。
func TestFailError_WrappedKeepsCode(t *testing.T) {
	e := newEngine()
	e.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		inner := errcode.New(errcode.Forbidden, "没有权限")
		FailError(c, fmt.Errorf("查详情失败: %w", inner))
	})
	w := ut.PerformRequest(e, "GET", "/x", &ut.Body{})
	if w.Code != http.StatusForbidden {
		t.Fatalf("被包装的 403 应返 403,got %d (body=%q)", w.Code, w.Body.String())
	}
	m := bodyOf(t, w)
	if got, _ := m["code"].(float64); int(got) != errcode.Forbidden {
		t.Errorf("body.code = %v, want %d", m["code"], errcode.Forbidden)
	}
}
