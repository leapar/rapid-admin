// Package dynx —— 单选 / 多选的「选项来源」注册表(关联系统表)
//
// # 这张表解决什么问题
//
// 单选 / 多选的选项以前只有一种来源:**设计器里手工逐条填**。
// 于是"所属部门""负责人"这类**本来就存在系统表里**的选项,
// 只能被一份一份地抄进设计器 —— 抄一遍就多一份不同步的真相。
//
// ⇒ 这里给第二种来源:**关联系统表**。
// 选项在**读表单结构的时候现查** ⇒ 系统表里新增一行,表单里立刻多一个选项。
//
// # ⚠️ **白名单,不是"任意表"**
//
// 让用户填表名(或 SQL)等于把 SQL 的表名交给用户拼 ⇒ 那是注入面。
// ⇒ 能关联哪张表写死在这里;要加表 = 这里加一行。
//
// # ⚠️ **权限是这张表的一部分**
//
// 每个来源都带一个**资源码**(模块 + 动作)——
// 读那张表要哪个权限,就在这里写哪个。
// ⇒ 读选项之前先判这个权限,判不过就降级(选项为空 + 标记),
// **绝不让整个表单结构失败**。
//
// 判定本身走 `middleware.CanAccess`(**执行层的唯一一份实现**),
// 在 handler 层做 —— `internal/api/middleware` 依赖 `internal/service`,
// service 反过来 import 就是循环依赖。
package dynx

import "strings"

// RelationSource 一个可作为选项来源的系统表
type RelationSource struct {
	// Key 来源标识(存进 options.source.key)
	Key string `json:"key"`
	// Label 中文名(前端下拉框用)
	Label string `json:"label"`
	// Table 物理表名
	Table string `json:"table"`
	// ValueColumn 取值列(存进记录的就是这一列的值)
	ValueColumn string `json:"value_column"`
	// LabelSQL 显示名的 SQL 表达式(读的时候拼进 SELECT,别名固定是 t)
	LabelSQL string `json:"label_sql"`
	// WhereSQL 附加过滤(留空 = 不过滤)
	WhereSQL string `json:"where_sql,omitempty"`
	// Module 权限模块名(判定用 module:action)
	Module string `json:"module"`
	// Action 权限动作
	Action string `json:"action"`
	// ResourceCode 完整的资源码(前端提示"缺哪个权限"时用)
	ResourceCode string `json:"resource_code"`
}

// relationSources 可关联的系统表(顺序即前端下拉框顺序)
//
// ⚠️ **显示名的口径与全仓一致** ——
// 真实姓名 → 昵称 → 账号,第一个非空的胜出
// (同 config_center_impl.go 的 operatorNameSQL)。
//
// ⚠️ **NULLIF 是必须的** ——
// real_name 是空串而不是 NULL ⇒ 不用 NULLIF 时 COALESCE 会选中空串,
// 于是这一列永远是空白(后台账号的 real_name 长期是空的)。
//
// ⚠️ **软删要自己过滤** ——
// 这里走的是裸 SQL(不是 GORM 的 Model 查询)⇒ GORM 不会自动补
// `deleted_at IS NULL` ⇒ 不过滤的话,已删的用户/角色/部门会出现在选项里。
var relationSources = []RelationSource{
	{
		Key:          "user",
		Label:        "系统用户",
		Table:        "sys_user",
		ValueColumn:  "id",
		LabelSQL:     "COALESCE(NULLIF(t.real_name, ''), NULLIF(t.nickname, ''), t.username)",
		WhereSQL:     "t.is_active = 1 AND t.deleted_at IS NULL",
		Module:       "user",
		Action:       "list",
		ResourceCode: "user:list",
	},
	{
		Key:          "role",
		Label:        "系统角色",
		Table:        "sys_role",
		ValueColumn:  "id",
		LabelSQL:     "t.name",
		WhereSQL:     "t.is_active = 1 AND t.deleted_at IS NULL",
		Module:       "role",
		Action:       "list",
		ResourceCode: "role:list",
	},
	{
		Key:          "dept",
		Label:        "部门",
		Table:        "sys_department",
		ValueColumn:  "id",
		LabelSQL:     "t.name",
		WhereSQL:     "t.is_active = 1 AND t.deleted_at IS NULL",
		Module:       "dept",
		Action:       "list",
		ResourceCode: "dept:list",
	},
}

// LookupRelationSource 查一个来源
func LookupRelationSource(key string) (RelationSource, bool) {
	k := strings.TrimSpace(key)
	// ⚠️ **按索引取,不用 `for _, s := range relationSources`** ——
	//   RelationSource 一行 144 字节,值拷贝会被 gocritic 的 rangeValCopy 报警。
	for i := range relationSources {
		if relationSources[i].Key == k {
			return relationSources[i], true
		}
	}
	return RelationSource{}, false
}

// RelationSources 列出全部来源(顺序固定)
func RelationSources() []RelationSource {
	out := make([]RelationSource, len(relationSources))
	copy(out, relationSources)
	return out
}

// RelationSourceKeys 列出全部来源的 key
func RelationSourceKeys() []string {
	out := make([]string, 0, len(relationSources))
	for i := range relationSources {
		out = append(out, relationSources[i].Key)
	}
	return out
}
