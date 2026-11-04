// 部门 / 字典 / Casbin 持久化
package model

// SysDepartment 部门
type SysDepartment struct {
	BaseModel
	ParentID uint64 `gorm:"index" json:"parent_id,string"`
	Name     string `gorm:"size:64;not null" json:"name"`
	Code     string `gorm:"size:64;uniqueIndex" json:"code"`
	LeaderID uint64 `gorm:"index" json:"leader_id,string"`
	Sort     int    `gorm:"default:0" json:"sort"`
	IsActive bool   `gorm:"default:true;index" json:"is_active"`
	// 2026-10-18:Path 不入 DB,由 service 层按 parent 链动态计算
	//   格式:"rootID,midID,leafID," (逗号分隔 id 链,根部门为空)
	//   前端 dept 页详情 '部门路径' 字段就是这里
	//   gorm:"-" 防止 GORM 试图往 sys_department 表写 path 列(表里没这列)
	Path string `gorm:"-" json:"path,omitempty"`
	// 2026-10-18:LeaderName 不入 DB,由 service 层 join sys_user.nickname 动态算
	//   前端 dept 页详情 '负责人' 字段直接展示这个,不用再二次查 user 表
	LeaderName string `gorm:"-" json:"leader_name,omitempty"`
	// 2026-10-18:Children 不在 DB 存,由 service.DepartmentTree 临时组装后返回
	//   gorm:"-" 告诉 GORM 跳过,避免误认为关联表
	//   json:"children" 让前端 Antd Tree 直接消费
	Children []SysDepartment `gorm:"-" json:"children,omitempty"`
	// 2026-10-18:MemberCount 不在 DB 存,由 service 层按 sys_user_department 动态 COUNT
	//   前端 dept 页 ViewDrawer '成员数' 字段就是这里;gorm:"-" 防止 GORM 误识为列
	//   不能加 omitempty:0 是合法值(没有成员的部门),omitempty 会把 0 字段从 JSON 里删掉
	//   前端拿不到 key,展示 '-' 而不是 '0' — 跟 path/leader_name 不一样,那两个是 string
	MemberCount int `gorm:"-" json:"member_count"`
}

func (SysDepartment) TableName() string { return "sys_department" }

// SysUserDepartment 用户-部门多对多关联
type SysUserDepartment struct {
	BaseModel
	UserID     uint64 `gorm:"uniqueIndex:uk_ud;not null" json:"user_id,string"`
	DeptID     uint64 `gorm:"uniqueIndex:uk_ud;not null" json:"dept_id,string"`
	IsPrimary  bool   `gorm:"default:false" json:"is_primary"`
	IsLeader   bool   `gorm:"default:false" json:"is_leader"`
}

func (SysUserDepartment) TableName() string { return "sys_user_department" }

// SysDict 字典
type SysDict struct {
	BaseModel
	GroupCode   string `gorm:"size:64;index;not null" json:"group_code"`
	ItemKey     string `gorm:"size:128;index;not null" json:"item_key"`
	ItemValue   string `gorm:"size:255;not null" json:"item_value"`
	Label       string `gorm:"size:64;not null" json:"label"`
	Sort        int    `gorm:"default:0" json:"sort"`
	IsActive    bool   `gorm:"default:true;index" json:"is_active"`
	Description string `gorm:"size:500" json:"description"`
}

func (SysDict) TableName() string { return "sys_dict" }

// SysCasbinRule Casbin gorm-adapter 持久化策略
// 文档 §5.2 — rbac_with_domains 模型
type SysCasbinRule struct {
	ID    uint64 `gorm:"primaryKey;autoIncrement" json:"id,string"`
	PType string `gorm:"size:16;index" json:"p_type"`
	V0    string `gorm:"size:64;index" json:"v0"` // subject(user/role)
	V1    string `gorm:"size:64;index" json:"v1"` // domain(project)
	V2    string `gorm:"size:64;index" json:"v2"` // object(resource)
	V3    string `gorm:"size:64" json:"v3"`         // action
	V4    string `gorm:"size:64" json:"v4"`
	V5    string `gorm:"size:64" json:"v5"`
}

func (SysCasbinRule) TableName() string { return "sys_casbin_rule" }
