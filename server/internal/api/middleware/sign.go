// Sign 中间件 —— 接口签名防重放(§1.3 #20 P2)
//
// 行为:
//   - 仅对写方法生效(POST/PUT/PATCH/DELETE),GET/HEAD/OPTIONS 透传
//   - 缺失 X-Signature 或格式错 → 401/403 errcode.SignatureMissing
//   - ts 超出 skew(默认 5 分钟) → errcode.SignatureExpired
//   - sig 不匹配 → errcode.SignatureInvalid
//   - body 读完后 c.Request.SetBodyStream 重置回原 body(下游仍能读)
//
// 客户端协议:
//
//	X-Signature: t=<unix_seconds>,sig=<hex(hmac-sha256(secret, METHOD\nPATH\nTS\nsha256(BODY)))>
//
// 路径不包含 query string(签名只覆盖 path)
//
// 用法(在 router.go):
//
//	h.Use(middleware.Sign([]byte(cfg.Sign.Secret)))  // 全局
//	h.Use(middleware.Sign(secret, signx.WithSkew(60), signx.WithSkip(...)))
package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/pkg/signx"
)

// Sign HMAC 接口签名校验中间件
func Sign(secret []byte, opts ...func(*signxCfg)) app.HandlerFunc {
	cfg := &signxCfg{
		skewSec: 300, // 5 min
		now:     func() int64 { return time.Now().Unix() },
	}
	for _, fn := range opts {
		fn(cfg)
	}
	return func(ctx context.Context, c *app.RequestContext) {
		method := string(c.Method())
		// 读方法透传
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next(ctx)
			return
		}
		// 读 header
		raw := string(c.Request.Header.Peek(signxHeader))
		ts, sig, ok := signx.ParseHeader(raw)
		if !ok {
			response.Fail(c, errcode.SignatureMissing, errcode.Msg(errcode.SignatureMissing))
			c.Abort()
			return
		}
		// ts 校验
		if !signx.ValidTS(ts, cfg.now(), cfg.skewSec) {
			response.Fail(c, errcode.SignatureExpired, errcode.Msg(errcode.SignatureExpired))
			c.Abort()
			return
		}
		// 读 body(必须恢复,否则下游拿不到)
		body := peekBody(c)
		// 路径(只取 Path,不含 query)
		path := string(c.Path())
		// 验签
		if !signx.Verify(secret, method, path, ts, body, sig) {
			response.Fail(c, errcode.SignatureInvalid, errcode.Msg(errcode.SignatureInvalid))
			c.Abort()
			return
		}
		// body 恢复
		restoreBody(c, body)
		c.Next(ctx)
	}
}

const signxHeader = "X-Signature"

type signxCfg struct {
	skewSec int64
	now     func() int64
}

// WithSkew 设置允许的时钟漂移(秒)
func WithSkew(n int64) func(*signxCfg) { return func(c *signxCfg) { c.skewSec = n } }

// WithClock 注入时钟(测试用)
func WithClock(now func() int64) func(*signxCfg) { return func(c *signxCfg) { c.now = now } }

/* ====================== body 处理 ====================== */

// peekBody 读出 body 字节(后续会恢复)
//
//	优先用 Request.Body()(缓冲好的字节);若为流式(大 body)再 fallback 到 BodyStream
//	必须恢复,否则下游 handler 拿不到 body 内容
func peekBody(c *app.RequestContext) []byte {
	// 1. 优先:已缓冲的 Body()(小 body 走这里)
	if b := c.Request.Body(); len(b) > 0 {
		return append([]byte(nil), b...)
	}
	// 2. fallback:流式 body
	bs := c.Request.BodyStream()
	if bs == nil {
		return nil
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, bs); err != nil {
		return nil
	}
	return buf.Bytes()
}

func restoreBody(c *app.RequestContext, body []byte) {
	if body == nil {
		return
	}
	// 用 SetBody 写回缓冲,下游 Body() 仍能拿到
	c.Request.SetBody(body)
}

// 防止 strconv 导入
var _ = strconv.Itoa
