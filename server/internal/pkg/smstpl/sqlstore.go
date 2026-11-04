// GORM 持久化实现 —— M115
//
// Store 接口的 DB 版(接口见 store.go)。
//
// ⚠️ **为什么放在 pkg 里而不是 service 里** ——
//
//	框架层自己持久化自己的状态,service 只负责编排。
//	反过来(service 直接写表)的话,框架包就再也测不了 ——
//	单测里用内存实现测状态机,DB 版只能另开一处写,两处会长歪。
//
// ⚠️ **状态值写坏时的处置:按 unknown 处理 + 大声记一笔** ——
//
//	DB 里手工改坏的状态(unknown → "approvd")有三种处置:
//	  1. 报错,整个装载失败 → 登记册空 ⇒ 所有场景"未登记" ⇒ 守卫全放行
//	     ⇒ **人工停用的模板会重新变成可发**(停用这个止血手段失效)
//	  2. 当成 rejected → 一个坏值就把登录验证码通道锁死(本设计最怕的事)
//	  3. 当成 unknown + 把原文写进 reason + 记 WARN 日志 → 放行(与"同步没跑"同口径)
//	     且下一轮同步会用服务商的真实状态覆盖掉这个坏值(自愈)
//	⇒ 选 3。它跟守卫的"不确定一律放行"是同一条口径。
package smstpl

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/pkg/timeutil"
)

// sqlStore GORM 版 Store
type sqlStore struct{ db *gorm.DB }

// NewSQLStore 构造 GORM 实现
//
// ⚠️ **db 为 nil 时全部退化成 no-op** —— 与 cronx 的 gormStore 同一条纪律:
//
//	极简部署(没挂 DB)不该让整个进程起不来。
func NewSQLStore(db *gorm.DB) Store { return &sqlStore{db: db} }

// Load 读出全部登记
//
// ⚠️ **加载不覆盖登记册里已有的行?** 不是 —— Load 只返回数据,
//
//	由调用方决定怎么装载(Registry.Load 是"灌进来",不"清空")。
func (s *sqlStore) Load(ctx context.Context) ([]Template, error) {
	if s.db == nil {
		return nil, nil
	}
	var rows []model.SysSmsTemplate
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("smstpl: 读模板登记失败: %w", err)
	}
	out := make([]Template, 0, len(rows))
	for _, r := range rows {
		out = append(out, templateFromModel(r))
	}
	return out, nil
}

// SaveTemplates 覆盖式保存登记(按 scene upsert)
//
// ⚠️ **只 upsert 传进来的行,不删没传的行** ——
//
//	运营把某个场景的模板号清空 = 停止发送,不是"忘掉这张模板"。
//	真要删,得是另一个显式动作。
func (s *sqlStore) SaveTemplates(ctx context.Context, items []Template) error {
	if s.db == nil {
		return nil
	}
	for _, it := range items {
		if it.Scene == "" {
			continue
		}
		row := templateToModel(it)
		if err := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "scene"}},
				// ⚠️ **只列要覆盖的列** —— created_at / id / deleted_at 不在里面,
				// 所以 upsert 不会把创建时间和主键改掉。
				DoUpdates: clause.AssignmentColumns([]string{
					"template_code", "status", "reason", "source", "reviewed_at", "updated_at",
				}),
			}).
			Create(&row).Error; err != nil {
			return fmt.Errorf("smstpl: 保存模板登记失败(scene=%s): %w", it.Scene, err)
		}
	}
	return nil
}

// AppendLogs 追加流转历史
func (s *sqlStore) AppendLogs(ctx context.Context, logs []Transition) error {
	if s.db == nil || len(logs) == 0 {
		return nil
	}
	rows := make([]model.SysSmsTemplateLog, 0, len(logs))
	for _, l := range logs {
		rows = append(rows, logToModel(l))
	}
	if err := s.db.WithContext(ctx).Create(&rows).Error; err != nil {
		return fmt.Errorf("smstpl: 写模板流转历史失败: %w", err)
	}
	return nil
}

// ListLogs 读流转历史(最新在前)
func (s *sqlStore) ListLogs(ctx context.Context, scene string, limit int) ([]Transition, error) {
	if s.db == nil {
		return nil, nil
	}
	q := s.db.WithContext(ctx).Model(&model.SysSmsTemplateLog{})
	if scene != "" {
		q = q.Where("scene = ?", scene)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []model.SysSmsTemplateLog
	if err := q.Order("id DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("smstpl: 读模板流转历史失败: %w", err)
	}
	out := make([]Transition, 0, len(rows))
	for _, r := range rows {
		out = append(out, logFromModel(r))
	}
	return out, nil
}

// ---- 映射 ----

// templateToModel 框架结构 → DB 行
func templateToModel(t Template) model.SysSmsTemplate {
	row := model.SysSmsTemplate{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		Scene:        t.Scene,
		TemplateCode: t.Code,
		Status:       string(t.Status),
		Reason:       t.Reason,
		Source:       string(t.Source),
	}
	if !t.ReviewedAt.IsZero() {
		row.ReviewedAt = &model.Time{Time: t.ReviewedAt.In(timeutil.AsiaShanghai)}
	}
	return row
}

// templateFromModel DB 行 → 框架结构
//
// ⚠️ **状态值解析不了时不报错**,按 unknown 处理并把原文写进 reason(见文件头注释)。
func templateFromModel(r model.SysSmsTemplate) Template {
	t := Template{
		Scene:  r.Scene,
		Code:   r.TemplateCode,
		Reason: r.Reason,
		Source: Source(r.Source),
	}
	st, err := ParseStatus(r.Status)
	if err != nil {
		logger.L().Warn("smstpl: 模板状态值无法解析,已按 unknown 处理(下次同步会覆盖)",
			zap.String("scene", r.Scene),
			zap.String("status", r.Status),
			zap.Error(err))
		t.Status = StatusUnknown
		t.Reason = fmt.Sprintf("状态值无法解析(原文:%q),已按未知处理,下次同步会自动覆盖", r.Status)
	} else {
		t.Status = st
	}
	if r.ReviewedAt != nil {
		t.ReviewedAt = r.ReviewedAt.Time
	}
	// ⚠️ UpdatedAt 用行上的 updated_at —— 它是"这一行最后被写的时间",
	// 比 reviewed_at 新(人工停用不更新 reviewed_at,但会更新 updated_at)。
	t.UpdatedAt = r.UpdatedAt
	return t
}

// logToModel 流转 → DB 行
func logToModel(t Transition) model.SysSmsTemplateLog {
	return model.SysSmsTemplateLog{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		Scene:        t.Scene,
		TemplateCode: t.Code,
		FromStatus:   string(t.From),
		ToStatus:     string(t.To),
		Source:       string(t.Source),
		Operator:     t.Operator,
		Reason:       t.Reason,
		OccurredAt:   model.Time{Time: t.At.In(timeutil.AsiaShanghai)},
	}
}

// logFromModel DB 行 → 流转
func logFromModel(r model.SysSmsTemplateLog) Transition {
	return Transition{
		Scene:    r.Scene,
		Code:     r.TemplateCode,
		From:     Status(r.FromStatus),
		To:       Status(r.ToStatus),
		Source:   Source(r.Source),
		Reason:   r.Reason,
		Operator: r.Operator,
		At:       r.OccurredAt.Time,
	}
}
