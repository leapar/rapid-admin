// WxTemplateService 微信模板消息
// 文档 §6.6
package service

import (
	"context"

	"rapid-admin/internal/model"
)

// WxTemplateListQuery 模板列表查询
type WxTemplateListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"`
	Channel string `json:"channel"` // miniapp / mp / both
	Scene string `json:"scene"`
	Status string `json:"status"`
}

// WxTemplateSendRequest 发送请求
type WxTemplateSendRequest struct {
	Code         string                 `json:"code"`         // 模板代号
	OpenID       string                 `json:"open_id"`       // 接收方 openid
	ReceiverType string                 `json:"receiver_type"` // owner / staff / supplier
	UserID       uint64                 `json:"user_id,string"`       // 可选绑定用户
	Data         map[string]any         `json:"data"`         // 模板占位符
	URL          string                 `json:"url"`          // 覆盖默认 url
	Pagepath     string                 `json:"pagepath"`     // 覆盖默认 pagepath
}

// WxTemplateService 微信模板
type WxTemplateService interface {
	// ===== 模板 CRUD =====
	ListTemplates(ctx context.Context, q WxTemplateListQuery) (*model.PageResponse, error)
	GetTemplate(ctx context.Context, id uint64) (*model.WxTemplate, error)
	GetTemplateByCode(ctx context.Context, code string) (*model.WxTemplate, error)
	CreateTemplate(ctx context.Context, caller *Caller, t *model.WxTemplate) (uint64, error)
	UpdateTemplate(ctx context.Context, caller *Caller, id uint64, t *model.WxTemplate) error
	DeleteTemplate(ctx context.Context, caller *Caller, id uint64) error

	// ===== 发送 =====
	Send(ctx context.Context, caller *Caller, req WxTemplateSendRequest) (uint64, error)

	// ===== 发送日志 =====
	ListSendLogs(ctx context.Context, q WxSendLogListQuery) (*model.PageResponse, error)
}

// WxSendLogListQuery 发送日志查询
type WxSendLogListQuery struct {
	model.PageRequest
	TemplateID uint64 `json:"template_id,string"`
	Code string `json:"code"`
	OpenID string `json:"open_id"`
	WxStatus string `json:"wx_status"`
}
