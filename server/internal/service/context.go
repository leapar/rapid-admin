// 业务服务层 —— 上下文
// 文档 §4
package service

import (
	"context"
	"errors"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/errcode"
)

// Errors
var (
	ErrInternal   = errcode.New(errcode.Internal)
	ErrNotFound   = errcode.New(errcode.NotFound)
	ErrForbidden  = errcode.New(errcode.Forbidden)
	ErrBadRequest = errcode.New(errcode.BadRequest)
)

// Caller 业务调用上下文(handler 注入)
type Caller struct {
	UserID     uint64
	IdentityID uint64
	RoleCode   string
	RoleName   string
	Audience   string   // admin / owner / supplier
	ProjectIDs []uint64 // 工作范围(空 = 全平台)
	Username   string
	IP         string
	UserAgent  string
	RequestID  string
	TraceID    string
	// JTI 当前 token 的 jti,用于 logout/改密时 revoke 单个 jti
	// (M1 限制:per-user 全量 jti revoke 需要 per-user jti 索引,
	//  本期只 revoke 当前 jti;其他 jti 靠 access TTL 8h 自然失效)
	JTI string
	// SessionID 当前会话 id(sid)。access 与 refresh 两张 token 共用同一个,
	// 登出时靠它把**整条会话**(access + refresh)一起结束 ——
	// 只 revoke JTI 的话,那张 refresh 还能续命 720h。
	//
	// 老 token 没有这个 claim,此时是空串,登出退回到"只吊销 access jti"。
	// 见 pkg/auth/session.go 的降级说明。
	SessionID string
}

type callerKey struct{}

// CallerKey Hertz RequestContext 上的 key(给 handler 取 caller 用)
const CallerKey = "service.Caller"

// CtxKey 本次请求的 context.Context 在 Hertz RequestContext 上的 key
//
// # ⚠️ **为什么还要单独存一份 ctx**
//
// `*app.RequestContext` **不是** `context.Context`
// (Hertz v0.9.6:RequestContext 只有 `Value` 方法,没有 Deadline / Done / Err),
// 而**响应出口**(BaseHandler.OK / Page)手上只有一个 RequestContext。
//
// 中间件链上派生出来的 ctx(scope.WithModule 之类)只沿着 `c.Next(ctx)` 往下传,
// 到了 handler 里就**再也取不回来** —— RequestContext 里没有任何字段存着它。
//
// ⇒ 想在响应出口查规则(字段级可见性,M163)就缺一个 context.Context:
// GORM 的 `WithContext` 与 cachex 的加载器都收它。
// ⇒ 由中间件提前存进 Hertz 的 KV,响应出口再取出来。
//
// # ⚠️ **取不到时的兜底是 context.Background()**
//
// 这是"请求作用域的值",不是"必须存在的东西"
// (未装配中间件 / 公开路由 / 单测直接构造 RequestContext)。
// 兜底只是丢掉"客户端断连时取消查询"这一层,不影响正确性。
const CtxKey = "service.Ctx"

// FromContext 从 ctx 取 Caller(唯一路径,与 Hertz 上下文解耦)
func FromContext(ctx context.Context) (*Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(*Caller)
	return c, ok
}

// WithCaller 把 Caller 注入 ctx
func WithCaller(ctx context.Context, c *Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// Is 是否为指定角色
func (c *Caller) Is(roleCode string) bool {
	return c != nil && c.RoleCode == roleCode
}

// HasRole 单一角色判断(同 Is,语义更清晰)
func (c *Caller) HasRole(roleCode string) bool { return c.Is(roleCode) }

// HasAnyRole 是否为指定角色之一
func (c *Caller) HasAnyRole(codes ...string) bool {
	if c == nil {
		return false
	}
	for _, r := range codes {
		if c.RoleCode == r {
			return true
		}
	}
	return false
}

// InProject 资源项目是否在工作范围内
// projectID = 0 表示请求无项目域约束,放行
// projectID > 0 时:ProjectIDs 空 = 全平台放行;否则必须命中
func (c *Caller) InProject(projectID uint64) bool {
	if c == nil {
		return false
	}
	if projectID == 0 {
		return true
	}
	if len(c.ProjectIDs) == 0 {
		// 未指定工作范围 → 默认全平台
		return true
	}
	for _, p := range c.ProjectIDs {
		if p == projectID || p == 0 {
			return true
		}
	}
	return false
}

// IsAdmin audience 判定
func (c *Caller) IsAdmin() bool { return c != nil && c.Audience == "admin" }

// MustNoEmpty 校验非空
func MustNoEmpty(field string, v string) error {
	if v == "" {
		return errcode.New(errcode.BadRequest, field+" 不能为空")
	}
	return nil
}

// IsErrCode 判断业务码
func IsErrCode(err error, code int) bool {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}

// toCallerSummary service.Caller → authz.CallerSummary(解耦,避免反向依赖)
//
// 原在 assertor_ref.go(业务参考实现)里,开源抽离时迁入此处的公共 helper。
func toCallerSummary(c *Caller) *authz.CallerSummary {
	if c == nil {
		return nil
	}
	return &authz.CallerSummary{UserID: c.UserID, RoleCode: c.RoleCode}
}
