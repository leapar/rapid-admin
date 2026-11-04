// CSRF token 校验中间件
// 文档 §9.12
// 管理后台纯 token 鉴权(Authorization header),CSRF 主要风险在 cookie 场景
// 此处对 state-changing 请求(POST/PUT/PATCH/DELETE)校验同源/自定义 header
package middleware

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

// CSRFConfig CSRF 配置
type CSRFConfig struct {
	// 启用
	Enabled bool
	// 允许的 Origin(同源检查,空表示任意)
	AllowedOrigins []string
	// 自定义 header 校验(带这个头即视为合法 ajax 请求)
	//
	// ⚠️ M105(2026-10-28)订正注释:原注释写的是
	// "只要带 X-Requested-By: XMLHttpRequest 即视为合法 ajax",
	// 但代码**只判存在、不判值** —— 注释与实现不符。
	//
	// 保留"只判存在"是有意的:CSRF 的防线是"这个非简单头必须经过 CORS
	// 预检",跨域页面**根本发不出**这个头;值是什么并不影响这个性质。
	// 所以改注释而不是加值判断(加值判断只会把"头在但值不对"的合法
	// 客户端挡在门外,换不来任何安全性)。
	//
	// ⚠️ 另一条要注意的事实:本项目的后台前端**不发** X-Requested-With
	// (axios 1.7 默认不带),后台 API 一律走 `Authorization: Bearer` ——
	// 而 Bearer 请求被 ExemptTokenAuth 豁免掉了整套 CSRF 校验。
	// 所以 RequiredHeader 这条检查在当前流量下**从不触发**;
	// 一旦有人把 ExemptTokenAuth 关掉,所有写请求会集体 403。
	RequiredHeader string
	// 是否豁免 API token 鉴权(默认 true —— token 模式天然防 CSRF)
	ExemptTokenAuth bool
}

// DefaultCSRFConfig 默认配置
func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		Enabled:         true,
		RequiredHeader:  "X-Requested-With",
		ExemptTokenAuth: true,
	}
}

// CSRF 校验 state-changing 请求是否带防 CSRF header / 同源
func CSRF(cfg CSRFConfig) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !cfg.Enabled {
			c.Next(ctx)
			return
		}
		method := string(c.Method())
		// GET/HEAD/OPTIONS 不需要
		if method == consts.MethodGet || method == consts.MethodHead || method == consts.MethodOptions {
			c.Next(ctx)
			return
		}
		// ExemptTokenAuth:Authorization header 模式天然防 CSRF(攻击者读不到 token)
		if cfg.ExemptTokenAuth {
			authH := string(c.GetHeader("Authorization"))
			if strings.HasPrefix(authH, "Bearer ") {
				c.Next(ctx)
				return
			}
		}
		// 校验 RequiredHeader
		if cfg.RequiredHeader != "" {
			if v := string(c.GetHeader(cfg.RequiredHeader)); v == "" {
				response.Fail(c, errcode.Forbidden, "CSRF 校验失败:缺少 "+cfg.RequiredHeader)
				c.Abort()
				return
			}
		}
		// 同源检查(Origin / Referer)
		if len(cfg.AllowedOrigins) > 0 {
			origin := string(c.GetHeader("Origin"))
			if origin == "" {
				origin = string(c.GetHeader("Referer"))
			}
			if !originAllowed(origin, cfg.AllowedOrigins) {
				response.Fail(c, errcode.Forbidden, "CSRF 校验失败:来源不合法")
				c.Abort()
				return
			}
		}
		c.Next(ctx)
	}
}

func originAllowed(origin string, allow []string) bool {
	if origin == "" {
		return false
	}
	for _, a := range allow {
		if a == "*" || a == origin {
			return true
		}
		// 通配符(同主域):"*.example.com"
		//
		// ⚠️ M105(2026-10-28)修:原来判的是 `HasSuffix(origin, base)`,
		// base = "example.com" → **"https://evil-example.com" 也以
		// "example.com" 结尾** → 放行。同理 "https://notexample.com"、
		// "https://example.com.evil.com" 之类只要尾巴对得上就过。
		//
		// 正确的口径是**域名标签边界**:origin 必须以 "." + base 结尾
		// (即 base 是一个完整的标签,前面还有 "." 分隔)。
		// "https://evil-example.com" 的结尾是 "-example.com",不含 "." 边界 → 拒 ✓
		if strings.HasPrefix(a, "*.") {
			base := strings.TrimPrefix(a, "*.")
			if base != "" && strings.HasSuffix(origin, "."+base) {
				return true
			}
		}
	}
	return false
}
