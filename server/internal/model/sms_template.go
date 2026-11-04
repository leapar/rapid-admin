// 短信模板审核状态 —— M115
//
// 两张表:
//
//	SysSmsTemplate     当前状态(一个场景一行)—— 登记册的快照
//	SysSmsTemplateLog  状态流转历史(只追加)
//
// ⚠️ **模板号的唯一真源不在这里** ——
//
//	模板号的唯一真源是配置中心(aliyun.sms.template.*),运营改的是那里。
//	本表的 TemplateCode 只是"上一次同步时用的号"的快照,
//	用来发现"配置改了但同步还没跑"(此时登记册的结论不适用于新号)。
//	两边对不上时**一律以配置中心为准**。
//
// ⚠️ **状态值是字符串,不是 int** ——
//
//	这个值要进 DB、要进 JSON、要给人看,还要跟 smstpl.Status 的字面量对得上。
//	写成 int 的话,枚举插一个值就全体错位,而且页面上看不出错。
package model

// SysSmsTemplate 短信模板审核状态(一个场景一行)
type SysSmsTemplate struct {
	BaseModel

	// Scene 业务场景(login / reset_password / change_phone / view_full_phone)
	Scene string `gorm:"size:32;uniqueIndex;not null" json:"scene"`
	// TemplateCode 上次同步时的模板号(快照,不是唯一真源)
	TemplateCode string `gorm:"size:64;index" json:"template_code"`
	// Status 审核状态(unknown / pending / reviewing / approved / rejected / disabled)
	Status string `gorm:"size:16;not null;index" json:"status"`
	// Reason 服务商给的理由(驳回原因)或人工给的原因
	Reason string `gorm:"size:500" json:"reason"`
	// Source 当前状态是谁给的(manual / provider)
	Source string `gorm:"size:16" json:"source"`
	// ReviewedAt 服务商最后一次确认这个状态的时间
	//
	// ⚠️ **它是"服务商确认于何时",不是"状态变成于何时"** ——
	//	状态变成的时间在 sys_sms_template_log 里(OccurredAt)。
	//	这两个时间问的是不同的问题,页面要分开显示。
	ReviewedAt *Time `gorm:"index" json:"reviewed_at"`
}

// TableName
func (SysSmsTemplate) TableName() string { return "sys_sms_template" }

// SysSmsTemplateLog 模板状态流转历史(只追加)
//
// ⚠️ **只追加,不更新不删除** ——
//
//	它的用途是回答"它是什么时候变成这样的"。一旦允许改写历史,
//	"什么时候驳回的"这个问题就没有答案了。
//	软删(BaseModel.DeletedAt)在这里没有价值,但跟全站保持一致,免得以后
//	有人写 `Unscoped()` 时踩空。
type SysSmsTemplateLog struct {
	BaseModel

	// Scene 业务场景
	Scene string `gorm:"size:32;not null;index" json:"scene"`
	// TemplateCode 这条流转说的是哪个模板号
	//
	// ⚠️ **必填** —— 审核的是**模板号**,不是场景。同一个场景换过模板号之后,
	//	历史里全是同一个 scene,不记 code 就分不清"哪一次流转说的是哪个号"。
	TemplateCode string `gorm:"size:64;index" json:"template_code"`
	// FromStatus 流转前的状态
	FromStatus string `gorm:"size:16" json:"from_status"`
	// ToStatus 流转后的状态
	ToStatus string `gorm:"size:16;not null;index" json:"to_status"`
	// Source 谁触发的这次流转(manual / provider)
	Source string `gorm:"size:16" json:"source"`
	// Operator 操作人(人工流转时 = 运维的登录名;服务商流转时为空)
	//
	// ⚠️ **它和 Source 是一对,缺一不可** ——
	//
	//	Source 只说"是人点的还是服务商给的",不说"是谁点的"。
	//	没有这一列,历史里那行"人工停用"就是无主的 ——
	//	审计链断在"什么时候、为什么"上,唯独缺了"谁"。
	Operator string `gorm:"size:64" json:"operator"`
	// Reason 原因 / 备注
	Reason string `gorm:"size:500" json:"reason"`
	// OccurredAt 这次流转发生的时间
	OccurredAt Time `gorm:"index" json:"occurred_at"`
}

// TableName
func (SysSmsTemplateLog) TableName() string { return "sys_sms_template_log" }
