// context.go —— 数据范围在 ctx 里的传递 —— M155(2026-11-02)
//
// # 为什么业务域(module)也要走 ctx
//
// 数据范围规则是**按业务域**配的:同一个角色,在"用户"域可以配成"本部门",
// 在"角色"域可以是"全部"。
//
// 而业务域这个字符串,**路由层已经知道了** ——
// rbac("user", "list") 里的 "user" 就是它。
// 如果让每个 service 自己在 Apply 时写一遍 "user",那就是**两处拼同一个字符串**
// —— 迟早有一处改了另一处没改(rbac.go:230 那条注释说的就是这种病)。
//
// ⇒ 业务域由**判定权限的那一层**写进 ctx,service 只管读。
//   - 普通路由:RBAC 中间件在放行时写入
//   - 动态集合:数据面不挂 rbac()(dyn 的资源码带集合编码,注册期拼不出来),
//     由 handler 自己写入 —— 它同样知道集合编码

package scope

import "context"

type moduleKey struct{}

type scopeKey struct{}

// WithModule 把业务域写进 ctx
//
// ⚠️ **只在"已经判定过权限"之后写** ——
// 业务域的语义是"这次请求正在操作哪一块数据",
// 不是"这次请求想操作哪一块数据"(那是路由的事,路由已经定了)。
func WithModule(ctx context.Context, module string) context.Context {
	return context.WithValue(ctx, moduleKey{}, module)
}

// ModuleFromContext 从 ctx 取业务域
//
// ⚠️ **取不到时返回空串,false** ——
// 没有业务域 = 用角色的**默认规则**(module 为空的那一组)。
// 这不是错误:内部调用(定时任务、跨服务调用)没有路由,也就没有业务域。
func ModuleFromContext(ctx context.Context) (string, bool) {
	m, ok := ctx.Value(moduleKey{}).(string)
	return m, ok
}

// WithScope 把解析好的作用域写进 ctx
//
// ⚠️ **只有"已经解析出结果"才写** ——
// 写一个空的 Scope 进 ctx 与"没写"是**两件事**:
// 前者表示"查过了,没有限制",后者表示"没查过"。
// 下游据此决定要不要报警/记日志。
//
// ⚠️ **s 传指针,但存进 ctx 的是值** ——
// 存值 ⇒ 调用方之后改自己的 s 不会影响 ctx 里那一份(反之亦然)。
// 指针只是为了让这个 104 字节的结构不按值拷进函数参数(gocritic hugeParam)。
func WithScope(ctx context.Context, s *Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, *s)
}

// ScopeFromContext 从 ctx 取作用域
//
// ⚠️ **返值不返指针** —— ctx 里存的是值,这里返出去的也是值,
// 调用方拿到的副本改了不影响 ctx 里那一份。
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(scopeKey{}).(Scope)
	return s, ok
}
