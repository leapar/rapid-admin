// 交付指南(小区介绍 / 交房流程 / 验房须知 / 收房指引 / 常见问题)
// 文档 §6.2.2 / §7.2.2
package model

// Guide 交付指南条目
type Guide struct {
	BaseModel

	// 业务标识
	Title    string `gorm:"size:128;not null" json:"title"`
	Category string `gorm:"size:32;not null;index" json:"category"`  // handover_process / inspection_notice / receive_guide / faq
	SubCategory string `gorm:"size:64;index" json:"sub_category"`  // 二级分类

	// 内容
	Content     string `gorm:"type:text;not null" json:"content"`     // markdown/html
	Summary     string `gorm:"size:500" json:"summary"`               // 摘要
	CoverImage  string `gorm:"size:500" json:"cover_image"`            // 封面图 OSS
	Attachments string `gorm:"type:json" json:"attachments"`          // []string 附件 OSS keys

	// 关联
	ProjectID uint64 `gorm:"index" json:"project_id,string"` // 0=全平台可见

	// 状态
	Status      string `gorm:"size:16;default:'draft';index" json:"status"` // draft / published / archived
	IsActive    bool   `gorm:"default:true;index" json:"is_active"`
	PublishedAt *Time  `json:"published_at"`
	IsTop       bool   `gorm:"default:false;index" json:"is_top"`  // 置顶
	SortOrder   int    `gorm:"default:0;index" json:"sort_order"` // 排序(asc)

	// 浏览统计
	ViewCount  int `gorm:"default:0" json:"view_count"`
	LikeCount  int `gorm:"default:0" json:"like_count"`
	ShareCount int `gorm:"default:0" json:"share_count"`

	// 标签
	Tags string `gorm:"type:json" json:"tags"` // []string

	// 联系信息
	ContactName  string `gorm:"size:64" json:"contact_name"`
	ContactPhone string `gorm:"size:20" json:"contact_phone"`

	// 审计
	CreatedBy uint64 `gorm:"index" json:"created_by,string"`
	UpdatedBy uint64 `json:"updated_by,string"`
}

func (Guide) TableName() string { return "guide" }
