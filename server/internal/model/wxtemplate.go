// 微信模板消息表
// 文档 §6.6 / 微信小程序订阅消息 + 服务号模板
package model

// WxTemplate 微信模板消息(订阅消息 / 服务号模板)
type WxTemplate struct {
	BaseModel

	// 业务标识
	Code        string `gorm:"size:64;uniqueIndex;not null" json:"code"`     // 业务代号:delivery_remind / audit_result ...
	Name        string `gorm:"size:64;not null" json:"name"`                 // 显示名
	Channel     string `gorm:"size:16;not null;index" json:"channel"`        // miniapp / mp / both
	Scene       string `gorm:"size:32;index" json:"scene"`                   // 业务场景:验房 / 收房 / 通知

	// 微信侧模板 ID
	TemplateID string `gorm:"size:128;index" json:"template_id"` // 微信分配的 template_id
	MiniprogramState string `gorm:"size:16" json:"miniprogram_state"` // developer / trial / formal
	MiniprogramAppID string `gorm:"size:64" json:"miniprogram_app_id"`

	// 模板内容(占位符 {{xxx.DATA}})
	Title       string `gorm:"size:128" json:"title"`
	Content     string `gorm:"type:text" json:"content"`     // 含 {{xxx.DATA}} 占位符的原文
	Keywords    string `gorm:"type:json" json:"keywords"`    // []string 占位符顺序,空值=无
	Example     string `gorm:"type:text" json:"example"`     // 示例数据 JSON

	// 跳转
	URL     string `gorm:"size:500" json:"url"`
	Pagepath string `gorm:"size:255" json:"pagepath"` // 小程序页面

	// 状态
	Status      string `gorm:"size:16;default:'active';index" json:"status"` // active / disabled / draft
	IsActive    bool   `gorm:"default:true;index" json:"is_active"`
	Remark      string `gorm:"size:500" json:"remark"`

	// 审计
	CreatedBy uint64 `gorm:"index" json:"created_by,string"`
	UpdatedBy uint64 `json:"updated_by,string"`
}

func (WxTemplate) TableName() string { return "wx_template" }

// WxTemplateSendLog 模板发送日志
type WxTemplateSendLog struct {
	BaseModel

	TemplateID uint64 `gorm:"index;not null" json:"template_id,string"`
	Code       string `gorm:"size:64;index" json:"code"`
	Channel    string `gorm:"size:16" json:"channel"`

	// 接收方
	OpenID       string `gorm:"size:64;index" json:"open_id"`
	UserID       uint64 `gorm:"index" json:"user_id,string"`
	ReceiverType string `gorm:"size:16" json:"receiver_type"` // owner / staff / supplier
	DataJSON     string `gorm:"type:text" json:"data_json"`

	// 微信回执
	WxMsgID  string `gorm:"size:64;index" json:"wx_msg_id"`
	WxStatus string `gorm:"size:16;index" json:"wx_status"` // success / failed / pending
	WxErrCode int    `json:"wx_err_code"`
	WxErrMsg  string `gorm:"size:255" json:"wx_err_msg"`

	// 调用方审计
	CallerID   uint64 `gorm:"index" json:"caller_id,string"`
	CallerName string `gorm:"size:64" json:"caller_name"`
	IP         string `gorm:"size:45" json:"ip"`
}

func (WxTemplateSendLog) TableName() string { return "wx_template_send_log" }
