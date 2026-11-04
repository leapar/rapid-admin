// module.go —— 业务域登记表 —— M155(2026-11-02)
//
// # 为什么要有这张表
//
// 业务域(module)的**权威定义**是 `rbac(module, action)` 的第一个参数 ——
// 那是路由里写死的字面量,判定权限的那一层拿到的就是它。
//
// 而 `sys_resource.code` 是 `module + ":" + action` 拼出来的,
// **module 本身可以含冒号,action 也可以含冒号**:
//
//	rbac("audit:operation", "list")        → code "audit:operation:list"
//	rbac("job", "run:read")                → code "job:run:read"
//	rbac("dyn", "design:collection:create") → code "dyn:design:collection:create"
//
// ⇒ **从资源码反推 module 是不确定的** ——
// "audit:operation:list" 的 module 是 "audit:operation" 还是 "audit"?
// "job:run:read" 的 module 是 "job" 还是 "job:run"?
// 两种切法都能自圆其说,而**切错的代价是静默的**:
//
//	规则配在 "audit" 上,运行时注入的是 "audit:operation"
//	⇒ 规则不命中 ⇒ 回落默认组 ⇒ **不过滤**
//	⇒ 该角色看到了本该看不到的行,日志里一条痕迹都没有。
//
// 这正是本模块要消灭的那类洞(见 pkg/scope 的包注释),
// 所以它自己不能留一个。
//
// ⇒ **module 只能由知道它的人提供**:路由层。
// 路由层注册,service 层读,两边共用同一份字面量。
//
// # 与 dyn 数据面的关系
//
// 动态业务集合的数据面模块是 `dyn:<集合编码>` ——
// 那个字符串由**请求路径参数**决定,注册期拼不出来(设计面 rbac("dyn", ...) 的
// module 是 "dyn",两者是不同的数据域)。
// ⇒ 数据面那一族不进这张表,由 dyn handler 在请求期写进 ctx,
//   候选列表由 service 从 `sys_dyn_collection` 现算。

package scope

import (
	"sort"
	"sync"
)

// ModuleKey 业务域在 **Hertz 请求上下文**(c.Keys)里的键名
//
// # ⚠️ **为什么 ctx 里那份不够,还要 KV 一份**
//
// `WithModule` 写的是**派生 ctx** —— 它只沿中间件链往下传:
//
//	middleware.RBAC → c.Next(scope.WithModule(ctx, module)) → handler(ctx, c)
//
// 而 `handler.BaseHandler.OK(c, data)` **只有 RequestContext**,拿不到那个派生 ctx:
// Hertz v0.9.6 的 `RequestContext` 里**没有任何字段存着中间件链上的 ctx**
// (它的 `Value(key)` 读的是自己的 KV `ctx.Keys`,不是中间件传下来的那个)
// ⇒ 从 c 里取业务域永远是空。
//
// ⇒ 业务域再写一份进 Hertz 的 KV(c.Set / c.Get),响应出口才拿得到。
//
// # 两个通道各司其职
//
//	ctx 里的那份:service 层按业务域取数据范围时用(scope.ModuleFromContext)
//	KV 里的那份:handler 响应出口做字段级可见性时用(c.Get)
//
// 两边写的是**同一个字符串**,而且都只在 RBAC 中间件放行时写一次
// (动态集合的数据面由 handler 自己写 —— 它同样知道集合编码)。
//
// # ⚠️ **键名带包前缀** ——
// Hertz 的 KV 是**每个请求一个 map**,不跨请求;但同一个请求里
// 不同包各写各的,键名撞了就会互相覆盖 ⇒ 键名必须带来源前缀。
const ModuleKey = "scope.module"

var (
	moduleMu  sync.RWMutex
	moduleSet = map[string]struct{}{}
)

// RegisterModule 登记一个业务域
//
// ⚠️ **幂等** —— 同一个 module 被登记多次只算一次
// (一条路由一个 rbac(),同一个 module 出现在几十条路由上很常见)。
//
// ⚠️ **空串不登记** —— 空串是"默认规则组"(没给业务域时用),
// 它不是一个业务域,由 service 层单独处理。
func RegisterModule(module string) {
	if module == "" {
		return
	}
	moduleMu.Lock()
	defer moduleMu.Unlock()
	moduleSet[module] = struct{}{}
}

// RegisteredModules 已登记的业务域(排序后的一份拷贝)
//
// ⚠️ **返拷贝不返内部 map** —— 调用方要是能改内部 map,
// "登记表"就变成了"任何人都能改的全局变量"。
//
// ⚠️ **并发安全** —— 注册发生在启动期(路由注册),
// 读取发生在运行期(后台页面开下拉框),两者会重叠。
func RegisteredModules() []string {
	moduleMu.RLock()
	defer moduleMu.RUnlock()
	out := make([]string, 0, len(moduleSet))
	for m := range moduleSet {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// ResetModules 清空登记表
//
// ⚠️ **只在"整批重新注册"之前调** —— 路由注册入口调它,
// 防止单测多次 Register 把上一轮的残留带进来
// (与 router 包的 resetRBACTracking 同一个理由)。
//
// ⚠️ **生产里不要随便调** —— 调了之后已登记的业务域全没了,
// 而规则是按业务域存的 ⇒ 那些规则会静默失配 ⇒ 退化成不过滤。
func ResetModules() {
	moduleMu.Lock()
	defer moduleMu.Unlock()
	moduleSet = map[string]struct{}{}
}
