// Gray release 中间件 —— 灰度 header 透传(§1.3 #21 P2)
//
// 目的:
//   - 入口读 X-Release (可配 header 名) → 注入 ctx
//   - 业务 handler 用 grayx.ReleaseTag(ctx) 读取
//   - 响应回写 X-Release(供客户端/网关观测)
//
// 用法(在 router.go):
//
//	h.Use(middleware.GrayRelease("X-Release"))  // 入口
//
// 缺失 header → ctx 不注入,grayx.ReleaseTag(ctx) 返 ""
package middleware

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/grayx"
)

// GrayRelease 灰度发布 tag 透传中间件
//   - headerName: 读哪个 header(默认 "X-Release",可改为 "X-Canary" / "X-Cohort")
//   - 多 header 兜底:除 headerName 外,还会读 X-Canary / X-Cohort,谁先到用谁
func GrayRelease(headerName ...string) app.HandlerFunc {
	primary := grayx.HeaderRelease
	if len(headerName) > 0 && headerName[0] != "" {
		primary = headerName[0]
	}
	return func(ctx context.Context, c *app.RequestContext) {
		tag := pickTag(c, primary)
		if tag != "" {
			// 注入 ctx,业务用 grayx.ReleaseTag(ctx) 读
			ctx = grayx.WithReleaseTag(ctx, tag)
			// 响应回写,供客户端/网关观测
			c.Header(grayx.HeaderRelease, tag)
		}
		c.Next(ctx)
	}
}

// pickTag 按优先级读 tag:primary header > X-Canary > X-Cohort
func pickTag(c *app.RequestContext, primary string) string {
	if v := c.Request.Header.Peek(primary); len(v) > 0 {
		return string(v)
	}
	if primary != grayx.HeaderCanary {
		if v := c.Request.Header.Peek(grayx.HeaderCanary); len(v) > 0 {
			return string(v)
		}
	}
	if primary != grayx.HeaderCohort {
		if v := c.Request.Header.Peek(grayx.HeaderCohort); len(v) > 0 {
			return string(v)
		}
	}
	return ""
}
