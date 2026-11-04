// GuideService 交付指南
// 文档 §6.2.2 / §7.2.2
package service

import (
	"context"

	"rapid-admin/internal/model"
)

// GuideListQuery 指南列表查询
type GuideListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"`
	Category string `json:"category"`
	SubCategory string `json:"sub_category"`
	ProjectID uint64 `json:"project_id,string"`
	Status string `json:"status"`
	IsTop *bool `json:"is_top"`
	Tag string `json:"tag"`
}

// GuideService 指南
type GuideService interface {
	ListGuides(ctx context.Context, q GuideListQuery) (*model.PageResponse, error)
	GetGuide(ctx context.Context, id uint64) (*model.Guide, error)
	CreateGuide(ctx context.Context, caller *Caller, g *model.Guide) (uint64, error)
	UpdateGuide(ctx context.Context, caller *Caller, id uint64, g *model.Guide) error
	DeleteGuide(ctx context.Context, caller *Caller, id uint64) error
}
