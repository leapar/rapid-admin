// biz_scope.go —— 业务查询接上「数据范围(行级权限)」 —— M161(2026-11-02)
//
// # 这层解决什么
//
// §1.13 #2 记的那条:**业务查询的项目过滤至今是"调用方自己传"的** ——
//
//	if q.ProjectID > 0 { tx.Where("project_id = ?", q.ProjectID) }
//
// 而 q.ProjectID 是从 HTTP query 参数直接读出来的(`h.MustUint(c, "projectId", 0)`)
// ⇒ **不传 = 不过滤**,**传了 = 过滤到"你指定的"那个项目**(而且不校验你是不是那个项目的人)。
//
// ⇒ 今天的项目隔离不是鉴权,是前端便利。
//
// M155-M160 把"规则层"做出来了(配得到、算得出、落得下),
// 但**没有一个业务查询真的调用它** ⇒ 规则配了也不生效。
// 这个文件就是那个"真的调用它"的地方。
//
// # ⚠️ **核心决定:接线是"声明式"的,不是"顺手加一行"**
//
// 每一张业务表都要在下面那张表里**登记**:自己受哪个业务域管、哪几个维度表达得出来。
//
// 为什么必须登记,而不是在每个 service 的 List 里随手写一行 `scope.Apply`:
//
//  1. **"忘了接"是这种机制最常见的失败模式** ——
//     新加一个业务列表,忘了接数据范围 ⇒ 它**看起来正常**(不过滤),
//     而**管理员在后台配的规则对它不生效** —— 一行报错都没有。
//     这跟 §1.13 记的"不声明 ⇒ 启动失败"是同一件事。
//  2. **业务域字符串会漂移** —— 路由层写 `rbac("house", "list")`,
//     service 层写 `applyBizScope(..., "house")`,两处各写一个字面量。
//     改了一处忘了另一处 ⇒ 规则配在 "house" 上、运行时按 "houses" 查
//     ⇒ **回落默认规则组 ⇒ 静默失配**。
//     ⇒ 业务域由**登记表**说了算,service 只报"我要查哪个业务域"。
//  3. **维度能力因表而异** —— 有的表有 project_id,有的没有;
//     有的表有"谁建的",有的只有"谁处理的"。
//     不登记就没法告诉管理员"这一维在这张表上落不下去"。
//
// # ⚠️ **维度落不下去 ⇒ 响亮报错,不静默跳过**
//
// 见文件头 dyn_scope.go 里同一套理由:丢掉"或"组里的最后一个分支
// ⇒ 整段过滤变成"不过滤" ⇒ **看得见全部行** ⇒ 数据越权。
//
// ⇒ 所以 `checkDims` 在**值解析出来之后**检查:
// 这一维有值、而这张表没有对应的列 ⇒ 报错,消息里点名业务域 + 表 + 维度。
//
// ⚠️ **判据是"解析出来的取值",不是"规则里写了什么"** ——
// 「本部门」规则 + 调用方**没有部门** ⇒ 解析出来的 DeptIDs 是空的
// ⇒ 这一维不产生子句 ⇒ 匹配不到任何行(**安全方向**)。
// 而调用方**有部门** ⇒ 表达不出来 ⇒ 报错。
//
// # ⚠️ **业务域现算,不从 ctx 读**
//
// ctx 里那个业务域是 **RBAC 的业务域**(由 `rbac(module, action)` 写进去的),
// 用来判"这个人能不能调这个接口"。
// 而数据范围配的是**这张业务表** —— 两者多数时候同名,但不是同一件事。
//
// ⇒ 这里用 service 自己知道的业务域(它在调用处写死),
// 而不是 ctx —— 与 dyn 侧(M160)同一个理由:
// ctx 里有没有、是不是同一个业务域,取决于调用方走没走 handler;
// 直接调 service 的场景(内部任务 / 单测)ctx 里没有 ⇒ 读到空串
// ⇒ 回落默认规则组 ⇒ **该表的规则整组不生效,且没有任何报错**。
//
// # ⚠️ **"本部门"与"仅本人"落在"行归属人"那一列上**
//
// 业务表通常**没有 dept_id 列** —— 部门的归属关系在 `sys_user_department` 里。
// ⇒ 本部门 = "行归属人在本部门" ⇒ 子查询:
//
//	<归属人列> IN (SELECT user_id FROM sys_user_department WHERE dept_id IN ?)
//
// ⚠️ **归属人列不一定是"创建人"** —— 有的表根本没有创建人列,
// 只能落在"当前归属人"上(处理人 / 派单对象 / 提交人 / 发布人)。
// 逐张表的取舍写在下面那张表的注释里。
//
// # ⚠️ **本文件是"哪些业务域接了行级范围"的唯一真相源**
//
// 清单 §1.13 #2 要求:"让每个业务查询**必须声明**自己受哪个业务域管
// (不声明 ⇒ 启动失败),而不是'忘了接就当没这回事'"。
//
// ⇒ 三张账本:
//
//   - `bizScopeSpecs` —— 业务表(走 `applyBizScope`)
//   - `bizScopeCtxWired` —— 走 ctx 业务域那条路的(user / 操作日志)
//   - `bizScopeUnwired` —— **明确没有**行级范围的业务域 + 原因
//
// 路由层新加一个业务域却没在三张账本里登记 ⇒ `CheckScopeWiring()` 报错
// ⇒ 启动期 fail-fast(cmd/server/main.go 调用)。
//
// ⚠️ **`bizScopeUnwired` 是账本,不是逃逸名单** ——
// 每一项都必须写清"为什么它没有行级范围",
// 而且 `biz_scope_test.go` 里有一条测试扫源码核对这张表没有多出没登记的业务域。

package service

import (
	"context"
	"sort"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/scope"
)

// bizScopeSpec 一张业务表在数据范围这层的接线声明
//
// ⚠️ **三个维度模板都是 SQL 模板,各带恰好一个 `?`** ——
// 与 pkg/scope 的 Columns 约定一致(占位符收的是**值列表**)。
//
// ⚠️ **空串 = 这一维在这张表上表达不出来** ——
// 不是"不过滤",是"落不下去" ⇒ 配了这种维度的规则会在查询时**报错**。
type bizScopeSpec struct {
	// module 业务域 —— **必须与路由层 `rbac(module, action)` 的第一个参数逐字相同**
	module string
	// table 物理表名(错误信息里给人看的)
	table string
	// project 本项目维度的列模板
	project string
	// dept 本部门维度的列模板
	dept string
	// self 仅本人维度的列模板
	self string
}

// columns 翻译成 pkg/scope 的列模板
func (sp bizScopeSpec) columns() scope.Columns {
	return scope.Columns{Project: sp.project, Dept: sp.dept, Self: sp.self}
}

// supportedScopeTypes 这张表能表达的数据范围类型
//
// ⚠️ **"自定义"永远在列表里** ——
// 自定义范围的列名是**管理员填的**,框架事先不知道 ⇒ 没法预判。
// 它能不能用,取决于那个列在表上存不存在(不存在 ⇒ 数据库报错,也是响亮的)。
//
// ⚠️ **"全部数据"永远在列表里** —— 它不施加任何行级限制,任何表都支持。
func (sp bizScopeSpec) supportedScopeTypes() []string {
	out := []string{string(scope.TypeAll), string(scope.TypeCustom)}
	if sp.project != "" {
		out = append(out, string(scope.TypeProject))
	}
	if sp.dept != "" {
		out = append(out, string(scope.TypeDept), string(scope.TypeDeptAndChild))
	}
	if sp.self != "" {
		out = append(out, string(scope.TypeSelf))
	}
	return out
}

// checkDims 检查这条作用域用到的维度在这张表上表达得出来
//
// ⚠️ **表达不出来要响亮报错,不能静默跳过那一条** —— 见文件头的说明。
//
// # ⚠️ **判据是"解析出来的取值",不是"规则里写了什么"**
//
// 「本部门」规则 + 调用方**没有部门** ⇒ 解析出来的 DeptIDs 是空的
// ⇒ 这一维不产生任何子句 ⇒ 最终匹配不到任何行(**安全方向**)。
// 而「本部门」规则 + 调用方**有部门** ⇒ 这一维表达不出来 ⇒ 报错。
//
// ⇒ 同一条规则,不同的人看到的结果不同(一个报错、一个空列表)。
// 这是刻意的:报错的那些人是"配置真的坏了",空列表的那些人是"他本来就没有可见数据"。
func (sp bizScopeSpec) checkDims(sc *scope.Scope) error {
	if sc == nil {
		return nil
	}
	var missing []string
	if len(sc.ProjectIDs) > 0 && sp.project == "" {
		missing = append(missing, string(scope.TypeProject))
	}
	if len(sc.DeptIDs) > 0 && sp.dept == "" {
		missing = append(missing, string(scope.TypeDept))
	}
	if len(sc.SelfUserIDs) > 0 && sp.self == "" {
		missing = append(missing, string(scope.TypeSelf))
	}
	if len(missing) == 0 {
		return nil
	}
	return errcode.New(errcode.Internal,
		"业务域 "+sp.module+" 的「"+strings.Join(missing, "、")+"」数据范围落不到表 "+sp.table+" 上").
		WithDetail("这张表能表达的数据范围:" + strings.Join(sp.supportedScopeTypes(), "、") +
			";表里没有对应的归属列。⚠️ 静默跳过这一维会让可见集变窄(管理员配的规则与实际生效的不一致)")
}

// bizScopeSpecs æ¥çº¿ç»è®°è¡¨ ââ **æ°æ®èå´è¿å±çåä¸çç¸æº**
//
// â ï¸ **ä¸å¡åå­ç¬¦ä¸²å¿é¡»ä¸è·¯ç±å± `rbac(module, action)` éå­ç¸å** ââ
// å·®ä¸ä¸ªå­ç¬¦ â è§åéé»å¤±é â åè½é»è®¤è§åç» â ä¸è¿æ»¤ã
// è·¯ç±å±é£ä¸ä¾§ç± `internal/api/router/scope_wiring_test.go` å¨**è¿è¡æ**æ ¸å¯¹
// (`scope.RegisteredModules()` vs ä¸å¼ è´¦æ¬);
// æ¬æä»¶è¿ä¸ä¾§ç± `biz_scope_wiring_test.go` **æ«æºç **æ ¸å¯¹ã
//
// â ï¸ **é¡ºåºæä¸å¡ååå­æ¯æ** ââ ä¾¿äºäººå·¥æ ¸å¯¹,ä¸æ¿è½½è¯­ä¹ã
//
// # å¼æºå£å¾
//
//   - æ¡æ¶èªå¸¦å(guide)å¨æ­¤ç»è®°;
//   - ä¸å¡æ¹æ¥å¥èªå·±çæ¨¡åæ¶,ç§ guide çæ ·ä¾æ³¨å:
//     project = "é¡¹ç®åå" ç IN æ¨¡æ¿ / dept = é¨é¨å­æ¥è¯¢ / self = å½å±äººåç IN æ¨¡æ¿ã
var bizScopeSpecs = []bizScopeSpec{
	{
		module: "guide", table: "guide",
		project: "project_id IN ?",
		dept:    "created_by IN (SELECT user_id FROM sys_user_department WHERE dept_id IN ?)",
		self:    "created_by IN ?",
	},
}

// bizScopeCtxSpec 走「ctx 业务域」那条路接上的业务域
type bizScopeCtxSpec struct {
	// columnsVar service 包里那个列模板变量的名字
	//
	// ⚠️ **为什么记变量名而不是只记业务域** ——
	// 只记业务域的话,"登记了但那个列模板早就被改名/删了"没人发现
	// ⇒ 数据范围静默失效(跟 §1.13 记的"配了不生效"是同一类病)。
	// 记了变量名,守门网就能扫源码核对它还在、且真被 applyScope 用上。
	columnsVar string
	// types 这张表能表达的数据范围类型
	types []string
}

// bizScopeCtxWired 走「ctx 业务域」那条路接上的业务域
//
// ⚠️ **它们不在 bizScopeSpecs 里** ——
// 因为它们的列模板在各自的 service 里(userScopeColumns / opLogScopeColumns),
// 走的是 `scopeService.Apply(ctx, tx, cols)` 那条路(业务域从 ctx 读)。
//
// ⚠️ **但它们同样受数据范围管** —— 所以后台的候选列表里必须有它们,
// 否则管理员给"用户列表"配的规则根本配不上。
//
// ⚠️ **这两张表三个维度都表达得出来** ——
// sys_user 的部门归属在 sys_user_identity / sys_user_department(子查询),
// 操作日志的归属人是 user_id。
var bizScopeCtxWired = map[string]bizScopeCtxSpec{
	"user": {
		columnsVar: "userScopeColumns",
		types: []string{
			string(scope.TypeAll), string(scope.TypeProject), string(scope.TypeDept),
			string(scope.TypeDeptAndChild), string(scope.TypeSelf), string(scope.TypeCustom),
		},
	},
	"audit:operation": {
		columnsVar: "opLogScopeColumns",
		types: []string{
			string(scope.TypeAll), string(scope.TypeProject), string(scope.TypeDept),
			string(scope.TypeDeptAndChild), string(scope.TypeSelf), string(scope.TypeCustom),
		},
	},
}

// bizScopeUnwired 明确**没有**行级数据范围的业务域 + 原因
//
// ⚠️ **这张表是账本,不是逃逸名单** ——
// 每一项都必须写清"为什么它没有行级范围"。
// 路由层新增一个业务域却没在这三张账本里登记 ⇒ `CheckScopeWiring()` 报错 ⇒ 启动失败。
//
// ⚠️ **理由不能是"忘了"** —— 那不是理由,那是缺口。
// 缺口要么写清"这里没有业务行"(事实),要么写清"这里有业务行但还没接"(欠账)。
var bizScopeUnwired = map[string]string{
	"approval":         "审批单据 —— 单据可见性由审批流的节点/处理人决定,还没落成行级数据范围(欠账)",
	"audit:crypto":     "审计日志(加密操作)—— 只有「操作日志」那一张按操作人接了行级范围",
	"audit:data":       "审计日志(数据变更)—— 同上,还没接",
	"audit:export":     "审计日志(导出)—— 导出的是操作日志的子集,行级范围由操作日志那条链兜着",
	"audit:full-info":  "审计日志(全量信息)—— 同上,还没接",
	"audit:login":      "审计日志(登录)—— 登录记录没有业务归属列,还没接",
	"audit:pii":        "审计日志(PII 访问)—— 同上,还没接",
	"audit:sms":        "审计日志(短信发送)—— 同上,还没接",
	"audit:system":     "审计日志(系统运行日志)—— 记录的是机器事件(慢 SQL/慢接口/5xx/panic),没有业务归属列",
	"auth":             "登录/会话 —— 没有业务行",
	"config":           "系统配置 —— 没有业务行",
	"dashboard":        "首页统计 —— 聚合视图,按业务域过滤才有意义(还没做)",
	"dept":             "部门 —— 部门是数据范围的**维度来源**,不是被过滤的对象",
	"dict":             "字典 —— 没有业务行",
	"dyn":              "动态集合的**设计面**(集合/字段定义)—— 配置数据不是业务行;数据面在 dyn:<集合编码> 上单独配",
	"file":             "文件 —— 私密性靠 key 不可猜(单桶单树模型,M136),不靠行级范围",
	"flow":             "审批流模板 —— 流程定义,不是业务行",
	"folder":           "文件夹 —— 同 file",
	"inbox":            "站内信 —— 收件箱按 user_id 过滤,不是数据范围",
	"job":              "定时任务 —— 任务实例没有业务归属列",
	"resource":         "资源 —— 没有业务行",
	"role":             "角色 —— 没有业务行",
	"sms_template":     "短信模板 —— 没有业务行",
	"wxtemplate":       "微信模板 —— 没有业务行",
	"voice_clone":      "音色克隆台账 —— 平台级资产,按 API 码 all-or-nothing 管(同 wxtemplate),不做行级范围",
	"tts":              "文本合成 —— 即时调用,没有业务行",
	"tts_voice":        "TTS 音色登记册 —— 平台级资产(同 voice_clone),不做行级范围",
}

// bizScopeSpecFor 按业务域取接线声明
func bizScopeSpecFor(module string) (bizScopeSpec, bool) {
	for _, sp := range bizScopeSpecs {
		if sp.module == module {
			return sp, true
		}
	}
	return bizScopeSpec{}, false
}

// bizScopeSupportedTypes 某个业务域支持的数据范围类型
//
// ⚠️ **没登记的业务域返回 nil** —— 调用方据此知道"这一域没有业务数据"。
func bizScopeSupportedTypes(module string) []string {
	sp, ok := bizScopeSpecFor(module)
	if !ok {
		return nil
	}
	return sp.supportedScopeTypes()
}

// scopeModuleSupported 某个业务域支持哪些数据范围类型 + 它到底接没接
//
// ⚠️ **dyn:<集合编码> 是动态的** —— 集合是用户自己建的,
// 而它的物理表只有 owner_id 一维 ⇒ 只支持「全部数据」「仅本人」「自定义」。
//
// ⚠️ **第二个返回值是"接没接"** ——
// `bizScopeUnwired` 里登记的业务域返回 false(它们**接了但不支持任何维度**是另一回事)。
func scopeModuleSupported(module string) ([]string, bool) {
	if sp, ok := bizScopeSpecFor(module); ok {
		return sp.supportedScopeTypes(), true
	}
	if sp, ok := bizScopeCtxWired[module]; ok {
		return sp.types, true
	}
	if strings.HasPrefix(module, "dyn:") {
		// dyn 物理表只有 owner_id 一维(见 dyn_scope.go)
		return []string{string(scope.TypeAll), string(scope.TypeSelf), string(scope.TypeCustom)}, true
	}
	return nil, false
}

// scopeModuleSupportsType 某个业务域支不支持这种数据范围类型
func scopeModuleSupportsType(module, scopeType string) bool {
	types, ok := scopeModuleSupported(module)
	if !ok {
		return false
	}
	for _, t := range types {
		if t == scopeType {
			return true
		}
	}
	return false
}

// scopeModuleTypeError 生成"这一维在这个业务域上落不下去"的错误
//
// ⚠️ **配规则时就要说清楚,不能等到用规则的人查询时才炸** ——
// 那时候管理员已经走了,而报错的是**受害者**(用这条规则的人)。
func scopeModuleTypeError(module string, typ scope.Type) error {
	types, _ := scopeModuleSupported(module)
	msg := "业务域 " + module + " 不支持「" + string(typ) + "」数据范围"
	detail := "这个业务域能用的数据范围:" + strings.Join(types, "、")
	if sp, ok := bizScopeSpecFor(module); ok {
		msg = "业务域 " + module + "(表 " + sp.table + ")不支持「" + string(typ) + "」数据范围"
		detail += ";这张表没有对应的归属列。" +
			"⚠️ 配了也不会生效(查询时会报同样的错,只是那时候已经晚了)"
	}
	return errcode.New(errcode.BadRequest, msg).WithDetail(detail)
}

// ScopeWiredModules 列出所有接了行级数据范围的业务域
//
// ⚠️ **不含 dyn:<集合编码>** —— 那些是运行时按集合生成的,
// 而且它们只支持「仅本人」这一维(见 scopeModuleSupported)。
func ScopeWiredModules() []string {
	out := make([]string, 0, len(bizScopeSpecs)+len(bizScopeCtxWired))
	for _, sp := range bizScopeSpecs {
		out = append(out, sp.module)
	}
	for m := range bizScopeCtxWired {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// CheckScopeWiring 启动期校验:路由层登记的每个业务域都必须有归属
//
// ⚠️ **这是清单 §1.13 #2 要的那句"不声明 ⇒ 启动失败"** ——
// 路由层新加了一个 `rbac("news", "list")`,却忘了在业务查询里接数据范围,
// 那么后台给 "news" 配的规则**永远不生效**,而且没有任何报错。
//
// ⇒ 三张账本(bizScopeSpecs / bizScopeCtxWired / bizScopeUnwired)之外的业务域,
// 一律视为"漏声明" ⇒ 启动失败。
//
// ⚠️ **bizScopeUnwired 里的项不算漏声明** ——
// 它们是**明确登记过**"这里没有行级范围"的业务域,附原因。
func CheckScopeWiring() error {
	var missing []string
	for _, m := range scope.RegisteredModules() {
		if _, ok := scopeModuleSupported(m); ok {
			continue
		}
		if _, ok := bizScopeUnwired[m]; ok {
			continue
		}
		missing = append(missing, m)
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return errcode.New(errcode.Internal,
		"有业务域没有登记数据范围接线: "+strings.Join(missing, "、")).
		WithDetail("每个业务域必须三选一:① 在 bizScopeSpecs 里登记它受哪张业务表管;" +
			"② 在 bizScopeCtxWired 里登记它走 ctx 业务域那条路;" +
			"③ 在 bizScopeUnwired 里登记它为什么没有行级范围。" +
			"⚠️ 不登记的后果:后台配的规则对它永远不生效,而且没有任何报错")
}

// applyBizScope 把某个业务域的数据范围落到查询上
//
// ⚠️ **scopeSvc 为 nil ⇒ 不过滤** —— 与 scope_impl.go 的 applyScope helper 同一套兜底
// (单测 / 精简装配场景)。
//
// ⚠️ **ctx 里没有调用者 ⇒ 不过滤** ——
// 内部调用(定时任务 / 跨服务)没有"这个人能看哪些行"这件事 ⇒ 不过滤。
// **这不是放行漏洞**:内部调用不走用户身份。
//
// ⚠️ **业务域在参数里,不从 ctx 读** —— 见文件头的说明。
//
// # ⚠️ **列表与单条都要调它**
//
// 只过滤列表的话,用户仍然可以**按 id 打开**一条他看不见的记录
// ⇒ 那等于行级权限只挡了"翻页",没挡"直取"。
// (与 dyn 侧 M160 的 `recordWhere` + `recordScope` 同一个道理。)
func applyBizScope(
	ctx context.Context, scopeSvc ScopeService, tx *gorm.DB, module string,
) (*gorm.DB, error) {
	if scopeSvc == nil || tx == nil {
		return tx, nil
	}
	sp, ok := bizScopeSpecFor(module)
	if !ok {
		// ⚠️ **业务域没登记 = 编程错误** ——
		// 要么 service 里写错了业务域字符串,要么新加了一张业务表却忘了登记。
		// 两种情况都不能"安静地不过滤" —— 那正是这个机制要治的病。
		return nil, errcode.New(errcode.Internal,
			"业务域 "+module+" 没有登记数据范围接线").
			WithDetail("每张业务表都必须在 bizScopeSpecs 里登记自己受哪个业务域管、哪几维表达得出来")
	}
	caller, ok := FromContext(ctx)
	if !ok || caller == nil {
		return tx, nil
	}
	sc, err := scopeSvc.EffectiveScope(ctx, caller, sp.module)
	if err != nil {
		return nil, err
	}
	if err := sp.checkDims(&sc); err != nil {
		return nil, err
	}
	return scope.Apply(tx, &sc, sp.columns())
}
