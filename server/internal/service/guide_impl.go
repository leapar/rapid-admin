// GuideService 真实实现 —— 交付指南
// 文档 §6.2.2 / §7.2.2
package service

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// guideService 真实实现
type guideService struct {
	db *gorm.DB
	// scope 数据范围(行级权限)服务 —— M161
	//
	// ⚠️ **允许为 nil** —— 没装配 scope 服务时不过滤
	// (单测 / 精简装配),与 scope_impl.go 的 applyScope helper 同一套兜底。
	scope ScopeService
}

// NewGuideService 构造
func NewGuideService(db *gorm.DB, scopeSvc ScopeService) GuideService {
	return &guideService{db: db, scope: scopeSvc}
}

func (s *guideService) normPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	return page, size
}

func (s *guideService) ListGuides(ctx context.Context, q GuideListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.Guide{})
	if strings.TrimSpace(q.Keyword) != "" {
		kw := "%" + q.Keyword + "%"
		tx = tx.Where("title LIKE ? OR summary LIKE ? OR content LIKE ?", kw, kw, kw)
	}
	if q.Category != "" {
		tx = tx.Where("category = ?", q.Category)
	}
	if q.SubCategory != "" {
		tx = tx.Where("sub_category = ?", q.SubCategory)
	}
	if q.ProjectID > 0 {
		tx = tx.Where("project_id = ? OR project_id = 0", q.ProjectID)
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.IsTop != nil {
		tx = tx.Where("is_top = ?", *q.IsTop)
	}
	// 数据范围(行级权限,M161)—— 规则从**身份**推导,不由调用方传
	//
	// ⚠️ **与上面那个 q.ProjectID 过滤不是一回事** ——
	// 上面是「调用方自己传了 projectId 就按它收窄」(查询条件),
	// 这里是「这个角色被配了只能看哪些行」(权限规则)。
	// 后者**不依赖调用方传什么** —— 不传就不过滤,那正是要治的病。
	//
	// ⚠️ 指南表有 created_by ⇒ 本部门/仅本人落得住。
	tx, err := applyBizScope(ctx, s.scope, tx, "guide")
	if err != nil {
		return nil, err
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询指南数失败").WithDetail(err.Error())
	}
	var rows []model.Guide
	// 排序:置顶 > sort_order asc > id desc
	if err := tx.Order("is_top DESC, sort_order ASC, id DESC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询指南失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *guideService) GetGuide(ctx context.Context, id uint64) (*model.Guide, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	// 数据范围(行级权限,M161)—— **单条也要受管**
	//
	// ⚠️ **只过滤列表的话,用户可以按 id 打开一条他看不见的记录** ——
	// 那等于行级权限只挡了「翻页」,没挡「直取」。
	var g model.Guide
	tx, err := applyBizScope(ctx, s.scope, s.db.WithContext(ctx), "guide")
	if err != nil {
		return nil, err
	}
	if err := tx.First(&g, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "指南不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询指南失败").WithDetail(err.Error())
	}
	// 浏览数 +1
	_ = s.db.WithContext(ctx).Model(&model.Guide{}).Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
	g.ViewCount++
	return &g, nil
}

func (s *guideService) CreateGuide(ctx context.Context, caller *Caller, g *model.Guide) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if strings.TrimSpace(g.Title) == "" {
		return 0, errcode.New(errcode.BadRequest, "title 不能为空")
	}
	if strings.TrimSpace(g.Content) == "" {
		return 0, errcode.New(errcode.BadRequest, "content 不能为空")
	}
	if g.Category == "" {
		g.Category = "handover_process"
	}
	if g.Status == "" {
		g.Status = "draft"
	}
	g.IsActive = true
	g.CreatedBy = caller.UserID
	g.UpdatedBy = caller.UserID
	g.ID = sonyflake.Next()

	if err := s.db.WithContext(ctx).Create(g).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建指南失败").WithDetail(err.Error())
	}
	return g.ID, nil
}

func (s *guideService) UpdateGuide(ctx context.Context, caller *Caller, id uint64, g *model.Guide) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var old model.Guide
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "指南不存在")
		}
		return errcode.New(errcode.Internal, "查询指南失败").WithDetail(err.Error())
	}

	updates := map[string]any{
		"title":         g.Title,
		"category":      g.Category,
		"sub_category":  g.SubCategory,
		"content":       g.Content,
		"summary":       g.Summary,
		"cover_image":   g.CoverImage,
		"attachments":   g.Attachments,
		"project_id":    g.ProjectID,
		"status":        g.Status,
		"is_active":     g.IsActive,
		"is_top":        g.IsTop,
		"sort_order":    g.SortOrder,
		"tags":          g.Tags,
		"contact_name":  g.ContactName,
		"contact_phone": g.ContactPhone,
		"updated_by":    caller.UserID,
	}
	// 发布时自动设 published_at
	if g.Status == "published" && old.Status != "published" {
		updates["published_at"] = gorm.Expr("CURRENT_TIMESTAMP")
	}
	if err := s.db.WithContext(ctx).Model(&model.Guide{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新指南失败").WithDetail(err.Error())
	}
	return nil
}

func (s *guideService) DeleteGuide(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	if err := s.db.WithContext(ctx).Model(&model.Guide{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": "archived", "is_active": false, "updated_by": caller.UserID}).Error; err != nil {
		return errcode.New(errcode.Internal, "删除指南失败").WithDetail(err.Error())
	}
	return nil
}
