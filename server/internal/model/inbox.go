// 站内信数据模型
// 文档 §1.9 #11 P1
package model

// InboxMessage 站内信一条
//   - UserID: 收件人
//   - MsgType: system(系统) / biz(业务) / notice(公告) — 用于分类
//   - Title / Content: 标题 + 正文(纯文本,前端自渲染)
//   - Link: 跳转链接(可空,前端展示"查看详情"按钮)
//   - RefType + RefID: 关联业务资源(如 asset/material/issue)— 框架只存,业务可读
//   - SenderID: 发送者(0 = 系统)
//   - ReadAt: 已读时间(nil = 未读)
//   - ExpiredAt: 过期时间(可空;过期后定期清理)
type InboxMessage struct {
	BaseModel
	UserID    uint64 `gorm:"index;not null" json:"user_id,string"`
	MsgType   string `gorm:"size:32;index" json:"msg_type"`
	Title     string `gorm:"size:256;not null" json:"title"`
	Content   string `gorm:"type:text" json:"content"`
	Link      string `gorm:"size:500" json:"link"`
	RefType   string `gorm:"size:32;index" json:"ref_type"`
	RefID     uint64 `gorm:"index" json:"ref_id,string"`
	SenderID  uint64 `gorm:"index" json:"sender_id,string"`
	ReadAt    *Time  `gorm:"index" json:"read_at"`
	ExpiredAt *Time  `gorm:"index" json:"expired_at"`
}

func (InboxMessage) TableName() string { return "sys_inbox_message" }

// InboxUserPrefs 站内信用户偏好(框架占位,本期先建表,业务可后续启用)
//   - UserID: 主键
//   - MuteTypes: JSON 数组,["system","biz"] = 不再弹通知
//   - DigestMode: off / realtime / daily
type InboxUserPrefs struct {
	UserID     uint64 `gorm:"primaryKey" json:"user_id,string"`
	MuteTypes  string `gorm:"type:text" json:"mute_types"`
	DigestMode string `gorm:"size:16;default:off" json:"digest_mode"`
	UpdatedAt  Time   `json:"updated_at"`
}

func (InboxUserPrefs) TableName() string { return "sys_inbox_user_prefs" }
