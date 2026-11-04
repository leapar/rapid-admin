// biz_field_domain.go —— 业务域 → 响应字段候选的登记表 —— M162(2026-11-02)
//
// # 这张表回答什么问题
//
// "这个业务域的响应里,**真实存在**哪些字段?"
//
// 字段级可见性配的是**响应 JSON 里的键名**,不是数据库列名 ——
// 前端/接口看到的就是它。
//
// ⇒ 候选必须**从真实的响应模型上反射出来**,不能手写:
// 手写一份清单 ⇒ 模型改字段名而清单没改 ⇒ 管理员配了一个不存在的字段
// ⇒ **规则静默不生效**,而且页面看起来一切正常。
//
// # ⚠️ **为什么用"模型"而不是"数据库列"**
//
// 两者的差别是 `json:"-"`:
//
//	model.Owner.Phone  gorm:"size:512" json:"-"   ← 列存在,但**永远不在响应里**
//
// 按列出候选 ⇒ 管理员会看到"手机号"这个候选,勾上"隐藏" ⇒
// **什么都不会发生**(那个键压根不在响应里),而他以为自己已经防住了。
//
// ⇒ 候选 = 模型的 **json tag 名**(排除 `-`)。
//
// # ⚠️ **登记的是"响应模型",不是"表"**
//
// 一个业务域可能返回不止一种结构(列表项 / 详情 / 子表)。
// 这里登记**这个域真实会返回的那些模型**,候选取它们的并集。
//
// # ⚠️ **与行级数据范围共用同一套业务域字符串**
//
// 见 internal/pkg/scope/module.go:业务域的权威定义是路由层
// `rbac(module, action)` 的第一个参数。两张表(module 一套)必须逐字相同。
//
// # ⚠️ **"主要模型"的表名必须与 bizScopeSpecs 里的一致**
//
// biz_field_domain_test.go 有一条测试核对这件事 ——
// 登记错模型 ⇒ 候选字段与真实响应不符 ⇒ 又是"配了不生效"。
//
// # ⚠️ **两张账本是"互为镜像"的,两个方向都得有网**
//
// 行级账本(bizScopeSpecs / bizScopeCtxWired)与字段层账本(bizFieldDomains)
// 说的是同一组业务域 —— 今天的 19 个域**逐字相同**。
//
// ⚠️ **只守一个方向是不够的** ——
//
//	只守「字段层 ⊆ 行级层」⇒ 行级层新增一个域而没登记进字段层
//	                        ⇒ 该域的响应完全不过脱敏,且管理员也配不出来
//	只守「行级层 ⊆ 字段层」⇒ 字段层登记了一个行级层没有的域
//	                        ⇒ 两边对"有哪些业务域"的理解已经分叉
//
// ⇒ 两个方向都有网:前者是 `TestBizFieldDomain_每个域都在行级账本里`,
// 后者是 `CheckFieldPolicyWiring()`(启动期 fail-fast)+ 同名单测。

package service

import (
	"sort"
	"strings"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// bizFieldDomain 一个业务域的字段候选登记
type bizFieldDomain struct {
	// module 业务域 —— **必须与路由层 `rbac(module, action)` 的第一个参数逐字相同**
	module string
	// models 这个域真实会返回的模型(候选取它们的字段并集)
	//
	// ⚠️ **第一个是"主要模型"** —— 它的表名必须与
	// bizScopeSpecs 里同一个业务域的 table 一致(有测试核对)。
	models []any
}

// bizFieldDomains 业务域 → 字段候选登记表 —— **字段级可见性这层的单一真相源**
//
// ⚠️ **顺序按业务域名字母排** —— 便于人工核对,不承载语义。
//
// ⚠️ **动态集合(dyn:<集合编码>)不在这里** ——
// 它的字段是**运行期建的**,候选从 `sys_dyn_field` 现算(见 field_policy_impl.go)。
var bizFieldDomains = []bizFieldDomain{
	{module: "guide", models: []any{&model.Guide{}}},
	// 走 ctx 业务域那条路的两个域(见 bizScopeCtxWired)。
	//
	// 开源口径:业务方接入自己的模块后,在这里登记自己的域 + 模型;
	// dyn:<集合编码> 的动态集合不在此处(运行期从 sys_dyn_field 现算)。
	{module: "user", models: []any{&model.SysUser{}}},
	{module: "audit:operation", models: []any{&model.SysOperationLog{}}},
}

// bizFieldDomainIndex 业务域 → 登记的索引
//
// ⚠️ **响应出口每个请求都要查一次** ——
// 线性扫 19 条虽然也不贵,但那是**每个 HTTP 请求**的一次扫描;
// 换成 map 之后是 O(1)。
//
// ⚠️ **包级变量初始化顺序** ——
// Go 按依赖顺序初始化包级变量:bizFieldDomains 先于 bizFieldDomainIndex。
var bizFieldDomainIndex = func() map[string]bizFieldDomain {
	m := make(map[string]bizFieldDomain, len(bizFieldDomains))
	for _, d := range bizFieldDomains {
		m[d.module] = d
	}
	return m
}()

// bizFieldDomainFor 按业务域取登记
func bizFieldDomainFor(module string) (bizFieldDomain, bool) {
	d, ok := bizFieldDomainIndex[module]
	return d, ok
}

// CheckFieldPolicyWiring 启动期校验:行级账本里的每个业务域都必须在字段层登记
//
// ⚠️ **这是"反方向"的那张网** —— biz_field_domain_test.go 只核对
// 「字段层登记的域 ⊆ 行级账本」;**反方向**曾经没有网 ⇒
//
//	行级账本里新增一个业务域,而没登记进 bizFieldDomains
//	⇒ ① 执行层早退(fieldPolicyDomainSupported)⇒ 该域的响应**完全不过脱敏**
//	⇒ ② 配置期拒「业务域不存在」(field_policy_impl.go)⇒ 管理员**也配不出来**
//	⇒ 配了不生效,而且**没有任何报错** —— 与 M161 撞出的 family 那条同类。
//
// ⚠️ **判据是"有没有业务行",不是"有没有敏感字段"** ——
// 一个域今天没有手机号,不等于它永远没有;模型加一个字段,没人会记得回来登记。
// ⇒ 只要这个域的业务行会进响应,就必须在字段层登记。
//
// ⚠️ **动态集合(dyn:<集合编码>)不在这个比较里** ——
// 它的字段是运行期建的,候选从 sys_dyn_field 现算(见 field_policy_impl.go),
// 静态账本里没有它,也不该有。
func CheckFieldPolicyWiring() error {
	return checkFieldPolicyWiring(bizFieldDomains, bizScopeSpecs, bizScopeCtxWired)
}

// checkFieldPolicyWiring 纯函数版本 —— 账本当参数传进来,单测可以直接喂假的
//
// ⚠️ **为什么不直接读包级变量** ——
// 直接读的话,单测就得临时改包级变量再改回来(容易漏恢复);
// 喂参数则可以随便构造"漏登记"的场景。
func checkFieldPolicyWiring(
	fields []bizFieldDomain, specs []bizScopeSpec, ctxWired map[string]bizScopeCtxSpec,
) error {
	registered := make(map[string]bool, len(fields))
	for _, d := range fields {
		registered[d.module] = true
	}
	var missing []string
	for _, sp := range specs {
		if !registered[sp.module] {
			missing = append(missing, sp.module)
		}
	}
	for m := range ctxWired {
		if !registered[m] {
			missing = append(missing, m)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	// ⚠️ **后果要写在 Message 里,不能只写在 Detail 里** ——
	// 启动期是 `log.Fatal("...", zap.Error(err))`,而 zap 只打 `err.Error()`
	// (= "[码] 消息"),Detail **根本不会出现在日志里**。
	// ⇒ 把"不登记会怎样"放进 Message,运维在日志第一眼就能看到严重性。
	return errcode.New(errcode.Internal,
		"有业务域接了行级数据范围却没登记字段级可见性: "+strings.Join(missing, "、")+
			" —— 不登记的后果:① 执行层早退(fieldPolicyDomainSupported)⇒ 该域的响应完全不过脱敏;"+
			"② 配置期拒「业务域不存在」⇒ 管理员也配不出来;⇒ 配了不生效,而且没有任何报错").
		WithDetail("行级账本(bizScopeSpecs / bizScopeCtxWired)里的每个业务域," +
			"都必须在 bizFieldDomains 里登记它返回的响应模型" +
			"(第一个是「主要模型」,它的表名要与行级账本里的 table 一致)。" +
			"⚠️ 判据是「有没有业务行」,不是「有没有敏感字段」—— " +
			"模型以后加一个手机号字段,没人会记得回来登记")
}
