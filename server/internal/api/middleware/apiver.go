// APIVersion 中间件 —— 解析 X-API-Version header 注入 ctx(§1.3 #19 P2)
//
// 行为:
//   - 无 header / 解析失败 → 用 apiverx.DefaultVersion(=1),不报错(向后兼容)
//   - 解析成功 → 注入 ctx(handler 用 apiverx.Major(ctx) 读)
//   - 响应回写 X-API-Version(供客户端/网关观测实际生效版本)
//   - 选配 apiverx.WithMaxMajor(n) → 超出 n 的版本返 422 + errcode.VersionNotSupported
//
// 用法(在 router.go):
//
//	h.Use(middleware.APIVersion())                         // 仅解析 + 注入
//	h.Use(middleware.APIVersion(apiverx.WithMaxMajor(3)))  // 强校验
package middleware

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/apiverx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

// APIVersion 解析 X-API-Version header
func APIVersion(opts ...func(*apiverx.Opt)) app.HandlerFunc {
	o := &apiverx.Opt{}
	for _, fn := range opts {
		fn(o)
	}
	return func(ctx context.Context, c *app.RequestContext) {
		raw := string(c.Request.Header.Peek(apiverx.HeaderName))
		v, ok := apiverx.Parse(raw)
		if !ok {
			// 解析失败:用默认版本,不阻断请求
			v = apiverx.DefaultVersion
		}
		// 选配:超过 MaxMajor → 422
		if o.MaxMajor > 0 && v > o.MaxMajor {
			response.Fail(c, errcode.VersionNotSupported, errcode.Msg(errcode.VersionNotSupported))
			c.Abort()
			return
		}
		// 注入 ctx + 响应回写
		ctx = apiverx.WithVersion(ctx, v)
		c.Header(apiverx.HeaderName, intToString(v))
		c.Next(ctx)
	}
}

// intToString 避免 strconv 导入
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
