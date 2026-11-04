// Package grayx 灰度发布 header 透传(§1.3 #21 P2)
//
// 目的:
//   - 让 canary / beta / production 等灰度分组信息在请求处理链中可读
//   - 业务代码可基于 ReleaseTag 走不同分支(新功能灰度、A/B 实验)
//   - access_log / 业务日志可选附带 tag,方便按灰度组筛日志
//
// 关键设计:
//   - tag 跟随 context 流转,不是 goroutine local(可跨 RPC / 异步任务)
//   - 单一 source of truth:从入口 middleware 读 header,下游 ctx 取用
//   - 缺失 tag → 返 ""(不强制要求,业务判空即可)
//
// 用法:
//
//	// router.go
//	h.Use(middleware.GrayRelease("X-Release"))  // 入口读 header → ctx
//
//	// 业务 handler(ctx 是 Hertz 第一个参数)
//	tag := grayx.ReleaseTag(ctx) // 从 ctx 取
//	if tag == "canary" { /* 走新逻辑 */ }
//
//	// 业务日志
//	logger.L().Info("order paid",
//	    zap.String("release", grayx.ReleaseTag(ctx)),
//	    zap.Int64("order_id", id),
//	)
//
// 跨服务 / 异步任务:
//   - grayx.WithReleaseTag(parentCtx, tag) 派生新 ctx,塞给 goroutine 或 RPC client
//   - 下游服务自己也有 GrayRelease middleware,会覆盖(防止 header 伪造)
package grayx

import (
	"context"
)

// ctxKey 避免与其他包的 ctx key 冲突(用类型)
type ctxKey struct{}

// 常用 header 名(常量,业务可直接用)
const (
	// HeaderRelease 主推荐 header(可改,默认是 "X-Release")
	HeaderRelease = "X-Release"

	// HeaderCanary 备选,可读性更强(部分团队用这个)
	HeaderCanary = "X-Canary"

	// HeaderCohort 备选,基于用户的灰度(beta / canary / production)
	HeaderCohort = "X-Cohort"

	// DefaultRelease 缺失时使用的默认 tag(空 → 当作 production 处理)
	DefaultRelease = ""
)

// WithReleaseTag 把 tag 塞到 ctx(给跨服务/异步任务用)
func WithReleaseTag(parent context.Context, tag string) context.Context {
	return context.WithValue(parent, ctxKey{}, tag)
}

// ReleaseTag 从 ctx 读 tag;缺失返 ""
func ReleaseTag(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}
