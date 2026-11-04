package model

// SysDataScope 角色的数据范围规则 —— M155(2026-11-02)
//
// # 这张表回答的问题
//
// "这个角色在**这个业务域**上能看到哪些数据?"
//
// ⚠️ **它回答不了"能不能调这个接口"** ——
// 那是 sys_role_resource + casbin 的事(能不能调)。
// 两件事必须分开:
//   - 能调接口 ≠ 能看到全部数据
//   - 看不到数据 ≠ 不能调接口(可能只是列表页看不到,但详情能按 id 打开)
//
// # 唯一键
//
// uk_ds(role_id, module, scope_type) —— 同一角色同一业务域下,
// 同一种范围类型只能有一条(自定义范围的取值合并在那一行里)。
//
// ⚠️ **多条不同类型的行之间是"或"** ——
// 一个角色同时配了"本部门"和"仅本人",那它能看本部门的行 + 自己建的行。
//
// # module 是什么
//
// module = RBAC 的模块段,也就是资源码 `module:action` 里冒号前面那截:
// "user" / "role" / "house" / "dyn:合同表" ...
// 空串 = **默认规则**(该角色没在这个业务域上单独配过时用它)。
//
// ⚠️ **module 必须与路由层写进 ctx 的那个字符串完全一致** ——
// 配错一个字符,规则就静默不生效(退化成"全部")。
// ⇒ 后台页面配规则时下拉框的候选值由**资源码与动态集合**推导,不许手打。
type SysDataScope struct {
	BaseModel
	// RoleID 角色 id
	RoleID uint64 `gorm:"uniqueIndex:uk_ds;not null" json:"role_id,string"`
	// Module 业务域(空串 = 默认规则)
	Module string `gorm:"size:64;uniqueIndex:uk_ds;not null" json:"module"`
	// ScopeType 范围类型(all / project / dept / dept_and_child / self / custom)
	ScopeType string `gorm:"size:32;uniqueIndex:uk_ds;not null" json:"scope_type"`
	// CustomColumn 自定义范围的列名(仅 scope_type=custom)
	CustomColumn string `gorm:"size:64" json:"custom_column"`
	// CustomValues 自定义范围的允许取值 —— **JSON 数组字符串**
	//
	// ⚠️ **用 JSON 不用逗号分隔** —— 取值本身可能带逗号
	// (比如"杭州,南京"这种标签),逗号分隔会被切错。
	CustomValues string `gorm:"size:1024" json:"custom_values"`
	// Remark 备注(给管理员看的)
	Remark string `gorm:"size:255" json:"remark"`
}

// TableName 表名
func (SysDataScope) TableName() string { return "sys_data_scope" }
