package model

// SysFieldPolicy 角色的字段级可见性规则 —— M162(2026-11-02)
//
// # 这张表回答的问题
//
// "这个角色在**这个业务域**上,能看哪些**字段**?"
//
// 它是 sys_data_scope(行级)的**姊妹表**,两根轴互相独立:
//
//	sys_data_scope     → 这个人能看哪些**行**
//	sys_field_policy   → 这个人能看一行里的哪些**字段**
//
// ⚠️ **行级放行 ≠ 字段级放行** ——
// "客服部能看这个业主"不等于"客服部能看到业主的手机号"。
//
// # 唯一键
//
// uk_fp(role_id, module, field) —— 同一角色同一业务域下,
// 同一个字段只能有一条规则(改模式就是改这一行)。
//
// # ⚠️ **module 与行级那张表是同一套业务域字符串**
//
// 见 internal/pkg/scope/module.go:业务域的权威定义是路由层
// `rbac(module, action)` 的第一个参数,不能从资源码反推
// (module 与 action 都可能含冒号)。
//
// ⇒ 两张表的 module 必须逐字相同,否则配的规则会静默失配。
//
// # ⚠️ **module 为空串 = 默认规则组**
//
// "这个角色在**所有**业务域上的默认字段规则"。
//
// ⚠️ **字段级的"就近覆盖"与行级不一样** ——
// 行级:某业务域单独配了规则,就完全用那一组,默认组不参与;
// 字段级:默认组的限制**不会**被某个业务域的其它规则悄悄顶掉
// (逐字段覆盖,见 internal/pkg/fieldpolicy 的 Overlay 注释)。
//
// # ⚠️ **这张表只做"收窄"**
//
// 它能把一个已经在响应里的字段打码或删掉;
// 它**不能**把一个本来就不在响应里的字段(如 `json:"-"` 的加密手机号)变出来。
// ⇒ 要让某个角色看到某个字段,得业务侧先把值放进响应。
type SysFieldPolicy struct {
	BaseModel
	// RoleID 角色 id
	RoleID uint64 `gorm:"uniqueIndex:uk_fp;not null" json:"role_id,string"`
	// Module 业务域(空串 = 默认规则组)
	Module string `gorm:"size:64;uniqueIndex:uk_fp;not null" json:"module"`
	// Field 字段名(响应 JSON 里的键名,不是数据库列名)
	//
	// ⚠️ **是 JSON 键名** —— 前端/接口看到的就是它。
	// 数据库列名与 JSON 键名在本仓多数情况下相同(snake_case 迁移),
	// 但**不保证**(见 pkg/jsonid 与各模型上的 `json:"-"`)。
	Field string `gorm:"size:64;uniqueIndex:uk_fp;not null" json:"field"`
	// Mode 可见性模式(visible / masked / hidden)
	Mode string `gorm:"size:16;not null" json:"mode"`
	// MaskType 打码方式(phone / id_card / name / email / bank_card /
	// contract_no / full);空串 = 按字段名自动推断
	//
	// ⚠️ **mode=masked 且自动推断不出来 ⇒ 配置期就拒** ——
	// 运行期兜底是"整串打码",那会让管理员看到一堆意料之外的星号。
	MaskType string `gorm:"size:24" json:"mask_type"`
	// Remark 备注(给管理员看的)
	Remark string `gorm:"size:255" json:"remark"`
}

// TableName 表名
func (SysFieldPolicy) TableName() string { return "sys_field_policy" }
