// 请求体大小限制 — §1.3 #16 P1
//
// 设计目标:
//   - 拦住超大请求(防 DoS / 防误传大文件触发 OOM)
//   - 命中阈值 → 返 413 Payload Too Large(标准 HTTP 状态码)+ 业务码 41300
//   - 路径白名单:文件上传路由单独放行(走 OSS 直传前端的 STS 凭证签发,可以不限;走 server 中转的 API 才需要限)
//
// 阈值建议:
//   - 普通 API:        1 MB(常见业务表 JSON 不会超)
//   - 文件上传中转:   20 MB(走 server 中转才需要)
//   - OSS 直传凭证:   4 KB(只是签发一个 STS token,不应该大)
//
// 用法:
//
//	h.Use(middleware.BodyLimit(middleware.BodyLimitConfig{
//	    Default: 1 << 20, // 1 MB 普通业务
//	    PathLimits: map[string]int64{
//	        "/rapid-admin/v1/biz/uploads": 20 << 20, // 20 MB 中转上传
//	    },
//	}))
package middleware

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

// BodyLimitConfig 阈值配置
type BodyLimitConfig struct {
	// Default 全局默认上限(字节),0 = 不限
	Default int64
	// PathLimits 路径白名单(精确匹配 / 前缀匹配)
	//   精确: key 形如 "/rapid-admin/v1/biz/uploads" 必须完全相等
	//   前缀: key 形如 "/rapid-admin/v1/uploads/*" 以 * 结尾表示前缀匹配
	PathLimits map[string]int64
}

// BodyLimit 拦超大请求
// 命中后直接写 413 JSON 响应,不再 c.Next
func BodyLimit(cfg BodyLimitConfig) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// PathLimits 语义:**命中最具体(最长 pattern)的那一条直接覆盖 Default**,
		// 不管它比 Default 大还是小。
		//
		// ⚠️ 早期实现写的是 `if l > 0 && (limit == 0 || l < limit)`,于是
		// 「上传路由 20MB > 全局 1MB」永远匹配不上 —— 而**这正是 PathLimits
		// 存在的唯一理由**(给上传路由放宽)。症状是传 2MB 的图必 413,查中间件
		// 还会看到"配了 20MB 的呀",因为 map 迭代顺序还让结果不确定。
		limit := cfg.Default
		matchedLen := -1 // -1 = 还没命中任何一条,避免空 pattern 的零值歧义
		for path, l := range cfg.PathLimits {
			if l <= 0 || !matchPath(path, string(c.Request.URI().Path())) {
				continue
			}
			// 同长度(精确命中 vs 前缀命中同一长度不可能,保险起见取更小的上限)
			if len(path) > matchedLen || (len(path) == matchedLen && l < limit) {
				limit = l
				matchedLen = len(path)
			}
		}

		// 0 = 不限
		if limit <= 0 {
			c.Next(ctx)
			return
		}

		// Content-Length 头检查(快路径,大多数请求都有)
		if cl := int64(c.Request.Header.ContentLength()); cl > 0 && cl > limit {
			respondTooLarge(c, limit, cl)
			return
		}

		// 没 Content-Length(chunked / 流式)→ 暂时放行,handler 自行读 body 限制
		// 升级 TODO:用 http.MaxBytesReader 风格接管 io,读超限时拦截
		// 当前绝大多数业务都带 Content-Length,够用
		c.Next(ctx)
	}
}

// matchPath 精确 or 前缀匹配
//
// `/*` 前缀的语义是「**这条路径及其所有子路径**」,不是"从这条路径开始的任何
// 字符串"。
//
// ⚠️ 旧实现把前缀截成 `/a/b` 再做字符串前缀比较,于是 `/a/b/*` 会命中
// `/a/bc`、`/a/bXXXX` —— 白名单**多匹配**在 BodyLimit 上是安全方向的(更严),
// 但它让 pattern 变成"猜的":想放行 `/files/*` 的人完全不会想到它也管着
// `/filesomething`。保留尾斜杠比较,这个语义就唯一了。
//
// ⚠️ 裸路径也算命中。这是刻意宽松:`/rapid-admin/v1/biz/files/*` 同时覆盖
// `POST /rapid-admin/v1/biz/files`,省掉"配了 /* 忘了再配一条精确"这类静默漏配 ——
// 而 BodyLimit 漏配的症状是"上传莫名 413",是最难查的那一类。
func matchPath(pattern, actual string) bool {
	if pattern == actual {
		return true
	}
	// 前缀模式:以 /* 结尾
	if n := len(pattern); n >= 2 && pattern[n-2:] == "/*" {
		prefix := pattern[:n-1] // 只砍 `*` 不砍 `/`:"/a/b/*" → "/a/b/"
		if len(actual) >= len(prefix) && actual[:len(prefix)] == prefix {
			return true
		}
		if actual == prefix[:len(prefix)-1] { // 裸路径 "/a/b" 也算命中
			return true
		}
	}
	return false
}

// respondTooLarge 写 413 + 业务码,并**中断 handler 链**
//
// ⚠️ **`c.Abort()` 不能省**。hertz 的 `RequestContext.Next()`(app/context.go:830)
// 是 gin 那一套索引循环:
//
//	ctx.index++; for ctx.index < len(ctx.handlers) { run(handlers[index++]) }
//
// 而引擎只在入口调一次 `ctx.Next(c)`(route/engine.go:768)。所以中间件**直接
// return 并不等于拦下请求** —— 回到那个 for 循环,index 继续 +1,后面的 handler
// 照样跑,然后用 200 + 自己的 body 把刚才写的 413 整个覆盖掉。
//
// 症状极具迷惑性:响应**完全正常**(200 + 业务数据),只是多执行了一遍 handler ——
// 所以这个中间件从上线到现在**一次都没真正拦下过任何请求**,而单测只断言状态码
// 时又恰好能"通过"(handler 也返回 200)。
func respondTooLarge(c *app.RequestContext, limit, got int64) {
	c.Abort()
	c.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
	// 用业务码 41300 + 标准 HTTP 状态 413
	msg := fmt.Sprintf("request body too large (limit %d bytes, got %d)", limit, got)
	c.Response.SetStatusCode(consts.StatusRequestEntityTooLarge) // 413
	// 直接 SetBody 写标准 JSON(走 Gzip 中间件压缩)
	c.Response.SetBody([]byte(fmt.Sprintf(
		`{"code":%d,"message":%q,"data":null,"request_id":%q}`,
		errcode.PayloadTooLarge, msg, string(c.Response.Header.Peek("X-Request-Id")),
	)))
	_ = response.OK // 保留 import(供他处用)
}

