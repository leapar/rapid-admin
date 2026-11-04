// RoleSwitchAudit 身份/角色切换审计(框架能力:多身份切换的审计面)
//
// 历史口径:原定义在业务侧 model(owner.go),开源抽离时迁入框架,
// 表名从 biz_role_switch_audit 更名为 sys_role_switch_audit(新仓库无存量数据)。
package model

// RoleSwitchAudit 身份/角色切换审计
type RoleSwitchAudit struct {
	BaseModel
	UserID     uint64 `gorm:"index;not null" json:"user_id,string"`
	FromRole   string `gorm:"size:32" json:"from_role"`
	ToRole     string `gorm:"size:32;not null" json:"to_role"`
	FromDomain string `gorm:"size:64" json:"from_domain"`
	ToDomain   string `gorm:"size:64" json:"to_domain"`
	IP         string `gorm:"size:64" json:"ip"`
	UA         string `gorm:"size:255" json:"ua"`
	Reason     string `gorm:"size:255" json:"reason"`
}

func (RoleSwitchAudit) TableName() string { return "sys_role_switch_audit" }

// RoleSwitchFailPrefix 身份切换**失败**时 ToRole 的前缀
//
// ⚠️ handler 在切换失败时把 ToRole 写成 `FAIL:<原因>`(见 api/handler/v1/role_switch.go 的 writeRoleSwitchAudit),而 service 侧要靠它判"这条记录是失败还是成功"(status = 0 / 1)。
//
// 两处各写一个字面量就可能对不上 —— 一旦对不上,**失败的切换会被记成成功**,而失败的切换恰恰是审计最要看的那批(有人想切高敏身份但没切成)。
//
// ⇒ 单一真相源:两边都引用这个常量。
const RoleSwitchFailPrefix = "FAIL:"
