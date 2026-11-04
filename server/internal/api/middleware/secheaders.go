// SecHeaders 中间件 —— 安全响应头(§1.3 补 P2 #22)
//
// 目的:
//   - 一处挂载,所有响应自动带安全头(MVP 防 XSS / clickjacking / MIME 嗅探 / 信息泄露)
//   - 业务可按需 disable 某些头(API 响应不应带 X-Frame-Options;SPA 应按需调 CSP)
//
// 默认头(对应 OWASP Secure Headers Project 推荐):
//   - X-Content-Type-Options: nosniff        防 MIME 嗅探
//   - X-Frame-Options: DENY                  防 clickjacking
//   - Referrer-Policy: strict-origin-when-cross-origin
//   - X-XSS-Protection: 0                    现代浏览器已内置 CSP,X-XSS-Protection 反成攻击面
//   - Strict-Transport-Security: max-age=63072000; includeSubDomains
//     (仅 HTTPS 时,默认开;HTTP 环境通过 WithHSTS(false) 关)
//   - Permissions-Policy: 关闭敏感 API(geolocation/microphone/camera/payment 等)
//
// 故意不默认开的:
//   - Content-Security-Policy: SPA / API 差异大,留 WithCSP 显式注入
//   - Server / X-Powered-By: 删除"信息泄露"头(避免攻击者识别 stack)
//
// 用法(挂在 router.go 最早位置,所有响应都带):
//
//	h.Use(middleware.SecHeaders())
//	// 或:
//	h.Use(middleware.SecHeaders(
//	    middleware.WithFrame(middleware.FrameSameOrigin),  // SPA 用 SAMEORIGIN
//	    middleware.WithCSP("default-src 'self'"),         // 显式加 CSP
//	    middleware.WithHSTS(false),                       // dev / 内网关
//	))
package middleware

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

/* ====================== 配置 ====================== */

// FramePolicy X-Frame-Options 值
type FramePolicy string

const (
	FrameDeny       FramePolicy = "DENY"        // 完全不允许 frame
	FrameSameOrigin FramePolicy = "SAMEORIGIN"  // 同源可 frame
	FrameNone       FramePolicy = ""            // 不发送该头(SPA 自己用 CSP frame-ancestors 控制)
)

// PermPolicyConfig Permissions-Policy 关闭的敏感 API
type PermPolicyConfig struct {
	DisableGeolocation  bool
	DisableMicrophone   bool
	DisableCamera       bool
	DisablePayment      bool
	DisableUSB           bool
	DisableAccelerometer bool
	DisableGyroscope     bool
	DisableMagnetometer  bool
}

type secHeadersCfg struct {
	frame  FramePolicy
	csp    string
	hsts   bool
	perms  PermPolicyConfig
	extras map[string]string // 业务自定义头
}

// WithFrame 设置 X-Frame-Options
//
//	FrameNone ("") 表示不发送
func WithFrame(p FramePolicy) func(*secHeadersCfg) { return func(c *secHeadersCfg) { c.frame = p } }

// WithCSP 注入 Content-Security-Policy(默认不发送,SPA 显式开)
func WithCSP(value string) func(*secHeadersCfg) { return func(c *secHeadersCfg) { c.csp = value } }

// WithHSTS 开关 Strict-Transport-Security(默认 true,生产 HTTPS)
func WithHSTS(enabled bool) func(*secHeadersCfg) { return func(c *secHeadersCfg) { c.hsts = enabled } }

// WithPermissionsPolicy 自定义要关闭的敏感 API
func WithPermissionsPolicy(p PermPolicyConfig) func(*secHeadersCfg) {
	return func(c *secHeadersCfg) { c.perms = p }
}

// WithExtraHeader 追加自定义头(允许覆盖)
func WithExtraHeader(key, value string) func(*secHeadersCfg) {
	return func(c *secHeadersCfg) {
		if c.extras == nil {
			c.extras = map[string]string{}
		}
		c.extras[key] = value
	}
}

// SecHeaders 安全响应头中间件
func SecHeaders(opts ...func(*secHeadersCfg)) app.HandlerFunc {
	cfg := &secHeadersCfg{
		frame: FrameDeny,
		hsts:  true,
		perms: PermPolicyConfig{
			DisableGeolocation:  true,
			DisableMicrophone:   true,
			DisableCamera:       true,
			DisablePayment:      true,
			DisableUSB:           true,
			DisableAccelerometer: true,
			DisableGyroscope:     true,
			DisableMagnetometer:  true,
		},
	}
	for _, fn := range opts {
		fn(cfg)
	}
	// 预拼 Permissions-Policy
	permsHeader := buildPermissionsPolicy(cfg.perms)
	hstsHeader := "max-age=63072000; includeSubDomains"
	if !cfg.hsts {
		hstsHeader = ""
	}
	return func(ctx context.Context, c *app.RequestContext) {
		c.Header("X-Content-Type-Options", "nosniff")
		if cfg.frame != FrameNone {
			c.Header("X-Frame-Options", string(cfg.frame))
		}
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("X-XSS-Protection", "0")
		if hstsHeader != "" {
			c.Header("Strict-Transport-Security", hstsHeader)
		}
		if permsHeader != "" {
			c.Header("Permissions-Policy", permsHeader)
		}
		if cfg.csp != "" {
			c.Header("Content-Security-Policy", cfg.csp)
		}
		// 删信息泄露头(hertz 默认可能带 Server;用空值覆盖)
		c.Header("Server", "")
		c.Header("X-Powered-By", "")
		// 业务自定义头
		for k, v := range cfg.extras {
			c.Header(k, v)
		}
		c.Next(ctx)
	}
}

/* ====================== helpers ====================== */

// buildPermissionsPolicy 拼 Permissions-Policy 头
//   例:geolocation=(), microphone=(), camera=()
func buildPermissionsPolicy(p PermPolicyConfig) string {
	parts := make([]string, 0, 8)
	if p.DisableGeolocation {
		parts = append(parts, "geolocation=()")
	}
	if p.DisableMicrophone {
		parts = append(parts, "microphone=()")
	}
	if p.DisableCamera {
		parts = append(parts, "camera=()")
	}
	if p.DisablePayment {
		parts = append(parts, "payment=()")
	}
	if p.DisableUSB {
		parts = append(parts, "usb=()")
	}
	if p.DisableAccelerometer {
		parts = append(parts, "accelerometer=()")
	}
	if p.DisableGyroscope {
		parts = append(parts, "gyroscope=()")
	}
	if p.DisableMagnetometer {
		parts = append(parts, "magnetometer=()")
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
