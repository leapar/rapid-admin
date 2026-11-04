// Sign 中间件单测(§1.3 #20 P2)
package middleware

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	"rapid-admin/internal/pkg/signx"
)

const signTestSecret = "test-secret-for-signing"

func newSignEngine(opts ...func(*signxCfg)) *route.Engine {
	// 默认冻结时间在 frozenNow(避免与实际时间漂移 > 300s)
	opts = append([]func(*signxCfg){WithClock(func() int64 { return frozenNow })}, opts...)
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(Sign([]byte(signTestSecret), opts...))
	engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		// 下游 handler 仍能拿到 body(读 Body(),中间件已 SetBody 写回)
		c.String(http.StatusOK, "got:"+string(c.Request.Body()))
	})
	engine.GET("/x", func(ctx context.Context, c *app.RequestContext) { c.String(http.StatusOK, "get-ok") })
	return engine
}

// makeSigned 用同一份 secret 签当前 ts+body
func makeSigned(method, path, ts string, body []byte) string {
	return signx.Sign([]byte(signTestSecret), method, path, ts, body)
}

func TestSign_GET_PassesThrough(t *testing.T) {
	engine := newSignEngine()
	w := ut.PerformRequest(engine, "GET", "/x", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (GET 不签)", w.Code)
	}
}

func TestSign_POST_MissingHeader_Rejected(t *testing.T) {
	engine := newSignEngine()
	w := ut.PerformRequest(engine, "POST", "/x", &ut.Body{Body: bytes.NewReader([]byte(`{}`))})
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (missing header)", w.Code)
	}
	if !containsStr(w.Body.String(), "缺少接口签名") {
		t.Fatalf("body = %q, want containing '缺少接口签名'", w.Body.String())
	}
}

func TestSign_POST_ValidSig_OK(t *testing.T) {
	engine := newSignEngine()
	body := []byte(`{"k":"v"}`)
	ts := strconv.FormatInt(frozenNow, 10)
	sig := makeSigned("POST", "/x", ts, body)
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Signature", Value: "t=" + ts + ",sig=" + sig},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), `{"k":"v"}`) {
		t.Fatalf("handler should see original body, got: %s", w.Body.String())
	}
}

func TestSign_POST_ExpiredTS_Rejected(t *testing.T) {
	engine := newSignEngine(WithSkew(60))
	body := []byte(`{}`)
	oldTS := strconv.FormatInt(frozenNow-3600, 10) // 1h 前
	sig := makeSigned("POST", "/x", oldTS, body)
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Signature", Value: "t=" + oldTS + ",sig=" + sig},
	)
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (expired)", w.Code)
	}
	if !containsStr(w.Body.String(), "已过期") {
		t.Fatalf("body = %q, want containing '已过期'", w.Body.String())
	}
}

func TestSign_POST_BadSig_Rejected(t *testing.T) {
	engine := newSignEngine()
	body := []byte(`{}`)
	ts := strconv.FormatInt(frozenNow, 10)
	// 用另一份 body 算 sig(与实际请求 body 不一致)
	sig := makeSigned("POST", "/x", ts, []byte(`{"tampered":1}`))
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Signature", Value: "t=" + ts + ",sig=" + sig},
	)
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (bad sig)", w.Code)
	}
	if !containsStr(w.Body.String(), "校验失败") {
		t.Fatalf("body = %q, want containing '校验失败'", w.Body.String())
	}
}

func TestSign_POST_WrongSecret_Rejected(t *testing.T) {
	engine := newSignEngine()
	body := []byte(`{}`)
	ts := strconv.FormatInt(frozenNow, 10)
	// 客户端用错 secret 签
	sig := signx.Sign([]byte("wrong-secret"), "POST", "/x", ts, body)
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Signature", Value: "t=" + ts + ",sig=" + sig},
	)
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (wrong secret)", w.Code)
	}
}

func TestSign_HEAD_PassesThrough(t *testing.T) {
	engine := newSignEngine()
	engine.HEAD("/x", func(ctx context.Context, c *app.RequestContext) { c.String(http.StatusOK, "ok") })
	w := ut.PerformRequest(engine, "HEAD", "/x", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (HEAD 不签)", w.Code)
	}
}

// 冻结时间(用 WithClock) + future skew 测试
func TestSign_ClockInjection(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(Sign([]byte(signTestSecret), WithClock(func() int64 { return frozenNow + 1000 })))
	engine.POST("/x", func(ctx context.Context, c *app.RequestContext) { c.String(http.StatusOK, "ok") })
	body := []byte(`{}`)
	ts := strconv.FormatInt(frozenNow, 10) // 比 fakeNow 早 1000s
	sig := makeSigned("POST", "/x", ts, body)
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader(body), Len: len(body)},
		ut.Header{Key: "X-Signature", Value: "t=" + ts + ",sig=" + sig},
	)
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (clock skewed > 300)", w.Code)
	}
}

// 防止 frozenNow 未定义报错
const frozenNow = 1700000000
