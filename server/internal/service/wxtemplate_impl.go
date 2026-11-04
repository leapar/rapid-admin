// WxTemplateService 真实实现 —— 微信模板消息
// 文档 §6.6
package service

import (
	"context"
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// wxTemplateService 真实实现
type wxTemplateService struct {
	db *gorm.DB
}

// NewWxTemplateService 构造
func NewWxTemplateService(db *gorm.DB) WxTemplateService {
	return &wxTemplateService{db: db}
}

func (s *wxTemplateService) normPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	return page, size
}

// ===== 模板 CRUD =====

func (s *wxTemplateService) ListTemplates(ctx context.Context, q WxTemplateListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.WxTemplate{})
	if strings.TrimSpace(q.Keyword) != "" {
		kw := "%" + q.Keyword + "%"
		tx = tx.Where("code LIKE ? OR name LIKE ? OR title LIKE ?", kw, kw, kw)
	}
	if q.Channel != "" {
		tx = tx.Where("channel = ?", q.Channel)
	}
	if q.Scene != "" {
		tx = tx.Where("scene = ?", q.Scene)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	} else {
		tx = tx.Where("status <> ?", "deleted")
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询模板数失败").WithDetail(err.Error())
	}
	var rows []model.WxTemplate
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询模板失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *wxTemplateService) GetTemplate(ctx context.Context, id uint64) (*model.WxTemplate, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var t model.WxTemplate
	if err := s.db.WithContext(ctx).First(&t, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "模板不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询模板失败").WithDetail(err.Error())
	}
	return &t, nil
}

func (s *wxTemplateService) GetTemplateByCode(ctx context.Context, code string) (*model.WxTemplate, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errcode.New(errcode.BadRequest, "code 不能为空")
	}
	var t model.WxTemplate
	if err := s.db.WithContext(ctx).Where("code = ?", code).First(&t).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "模板不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询模板失败").WithDetail(err.Error())
	}
	return &t, nil
}

func (s *wxTemplateService) CreateTemplate(ctx context.Context, caller *Caller, t *model.WxTemplate) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if strings.TrimSpace(t.Code) == "" {
		return 0, errcode.New(errcode.BadRequest, "code 不能为空")
	}
	if strings.TrimSpace(t.Name) == "" {
		return 0, errcode.New(errcode.BadRequest, "name 不能为空")
	}
	if t.Channel == "" {
		t.Channel = "miniapp"
	}
	if t.Status == "" {
		t.Status = "active"
	}
	t.IsActive = true
	t.CreatedBy = caller.UserID
	t.UpdatedBy = caller.UserID
	t.ID = sonyflake.Next()

	// 防重
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.WxTemplate{}).Where("code = ?", t.Code).Count(&n).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "校验模板 code 失败").WithDetail(err.Error())
	}
	if n > 0 {
		return 0, errcode.New(errcode.Conflict, "模板 code 已存在")
	}

	if err := s.db.WithContext(ctx).Create(t).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建模板失败").WithDetail(err.Error())
	}
	return t.ID, nil
}

func (s *wxTemplateService) UpdateTemplate(ctx context.Context, caller *Caller, id uint64, t *model.WxTemplate) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var old model.WxTemplate
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "模板不存在")
		}
		return errcode.New(errcode.Internal, "查询模板失败").WithDetail(err.Error())
	}

	t.ID = id
	t.Code = old.Code // code 不可改
	t.UpdatedBy = caller.UserID

	updates := map[string]any{
		"name":               t.Name,
		"channel":            t.Channel,
		"scene":              t.Scene,
		"template_id":        t.TemplateID,
		"miniprogram_state":  t.MiniprogramState,
		"miniprogram_app_id": t.MiniprogramAppID,
		"title":              t.Title,
		"content":            t.Content,
		"keywords":           t.Keywords,
		"example":            t.Example,
		"url":                t.URL,
		"pagepath":           t.Pagepath,
		"status":             t.Status,
		"is_active":          t.IsActive,
		"remark":             t.Remark,
		"updated_by":         caller.UserID,
	}
	if err := s.db.WithContext(ctx).Model(&model.WxTemplate{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新模板失败").WithDetail(err.Error())
	}
	return nil
}

func (s *wxTemplateService) DeleteTemplate(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	// 软删除:改 status 即可
	if err := s.db.WithContext(ctx).Model(&model.WxTemplate{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": "deleted", "is_active": false, "updated_by": caller.UserID}).Error; err != nil {
		return errcode.New(errcode.Internal, "删除模板失败").WithDetail(err.Error())
	}
	return nil
}

// ===== 发送 =====

// Send 发送模板(本期:仅落 send_log,实际微信 API 调用留待 wx-mp-sdk 接入)
func (s *wxTemplateService) Send(ctx context.Context, caller *Caller, req WxTemplateSendRequest) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if strings.TrimSpace(req.Code) == "" {
		return 0, errcode.New(errcode.BadRequest, "code 不能为空")
	}
	if strings.TrimSpace(req.OpenID) == "" {
		return 0, errcode.New(errcode.BadRequest, "openId 不能为空")
	}
	tpl, err := s.GetTemplateByCode(ctx, req.Code)
	if err != nil {
		return 0, err
	}
	if tpl.Status != "active" || !tpl.IsActive {
		return 0, errcode.New(errcode.Forbidden, "模板已停用")
	}

	dataJSON, _ := json.Marshal(req.Data)
	log := &model.WxTemplateSendLog{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		TemplateID:   tpl.ID,
		Code:         tpl.Code,
		Channel:      tpl.Channel,
		OpenID:       req.OpenID,
		UserID:       req.UserID,
		ReceiverType: req.ReceiverType,
		DataJSON:     string(dataJSON),
		WxStatus:     "pending", // 实际调用微信 API 后回填
		CallerID:     caller.UserID,
		CallerName:   caller.Username,
	}
	if err := s.db.WithContext(ctx).Create(log).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "记录发送日志失败").WithDetail(err.Error())
	}
	// TODO: 实际调微信 API
	//   miniapp: subscribeMessage.send
	//   mp:      template/api_send
	//   sdk 接入后,在此调 sdk 拿到 wxMsgId / wxStatus,再 update log
	return log.ID, nil
}

// ListSendLogs 发送日志列表
func (s *wxTemplateService) ListSendLogs(ctx context.Context, q WxSendLogListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.WxTemplateSendLog{})
	if q.TemplateID > 0 {
		tx = tx.Where("template_id = ?", q.TemplateID)
	}
	if q.Code != "" {
		tx = tx.Where("code = ?", q.Code)
	}
	if q.OpenID != "" {
		tx = tx.Where("open_id = ?", q.OpenID)
	}
	if q.WxStatus != "" {
		tx = tx.Where("wx_status = ?", q.WxStatus)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询发送日志数失败").WithDetail(err.Error())
	}
	var rows []model.WxTemplateSendLog
	if err := tx.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询发送日志失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}
