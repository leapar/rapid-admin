// 数据模型 —— GORM 通用基类
// 文档 §6.2
package model

import (
	"time"

	"gorm.io/gorm"
)

// BaseModel 通用模型基类
//   - 主键 BIGINT UNSIGNED(sonyflake)
//   - 软删除 deleted_at
//   - 自动维护 created_at / updated_at
type BaseModel struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement:false" json:"id,string"`
	CreatedAt time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// 业务表共有的字段
type AuditFields struct {
	CreatedBy uint64    `gorm:"index" json:"created_by,string"`
	UpdatedBy uint64    `json:"updated_by,string"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// IsActive 通用"是否启用"
type IsActive struct {
	IsActive bool `gorm:"default:true;index" json:"is_active"`
}
