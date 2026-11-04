package cronx

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
)

// Store 执行记录的持久化边界。
//
// 抽成接口有两个理由:
//  1. **单测不需要 DB** —— 与本仓 cachex 测试不连真 Redis 是同一条纪律。
//  2. 去重逻辑(HasRunning)是本模块最关键的代码,值得能用内存 fake 直接测。
type Store interface {
	// HasRunning Layer 0:上一次还在跑吗
	HasRunning(ctx context.Context, key string) (bool, error)
	// CreateRun 写一条 running 记录
	CreateRun(ctx context.Context, r *model.SysJobRun) error
	// FinishRun 更新终态
	FinishRun(ctx context.Context, r *model.SysJobRun) error
	// LoadOverrides 读全部覆盖行(含 is_active=false 的,用于 reconcile 提示)
	LoadOverrides(ctx context.Context) (map[string]Override, error)
	// DeactivateStale 把代码里已不存在的残留行置 is_active=false
	DeactivateStale(ctx context.Context, keys []string) error
	// RecoverStaleRuns 回收崩溃遗留的 running 行,返回回收条数
	RecoverStaleRuns(ctx context.Context, before time.Time) (int64, error)
	// PurgeHistory 按保留期 + 条数上限清理执行记录,返回删除条数
	PurgeHistory(ctx context.Context, days, max int) (int64, error)
}

type gormStore struct{ db *gorm.DB }

// NewStore 构造 gorm 实现(db 为 nil 时 cronx 退化为"无记录、无 Layer 0")
func NewStore(db *gorm.DB) Store { return &gormStore{db: db} }

func (s *gormStore) HasRunning(ctx context.Context, key string) (bool, error) {
	if s.db == nil {
		return false, nil
	}
	var n int64
	err := s.db.WithContext(ctx).Model(&model.SysJobRun{}).
		Where("job_key = ? AND status = ?", key, model.JobStatusRunning).
		Limit(1).Count(&n).Error
	return n > 0, err
}

func (s *gormStore) CreateRun(ctx context.Context, r *model.SysJobRun) error {
	if s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Create(r).Error
}

func (s *gormStore) FinishRun(ctx context.Context, r *model.SysJobRun) error {
	if s.db == nil {
		return nil
	}
	return s.db.WithContext(ctx).Model(&model.SysJobRun{}).Where("id = ?", r.ID).
		Updates(map[string]any{
			"status":      r.Status,
			"finished_at": r.FinishedAt,
			"duration_ms": r.DurationMs,
			"error":       r.Error,
			"degraded":    r.Degraded,
			"updated_at":  r.UpdatedAt,
		}).Error
}

func (s *gormStore) LoadOverrides(ctx context.Context) (map[string]Override, error) {
	out := make(map[string]Override)
	if s.db == nil {
		return out, nil
	}
	var rows []model.SysJob
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		// ⚠️ Description 必须在这里读出来。漏掉的话:运营在后台写备注,
		// 当次生效(Reload 拿到的是完整 ov),重启后 LoadOverrides 读回来的
		// Description 是空 → merge 判 `ov.Description != ""` 不成立 →
		// 悄悄回退到代码默认值。症状是「备注自己消失了」,没有任何报错。
		out[r.JobKey] = Override{
			JobKey: r.JobKey, Name: r.Name, Description: r.Description,
			CronExpr: r.CronExpr, Enabled: r.Enabled, Sort: r.Sort,
			IsActive: r.IsActive,
		}
	}
	return out, nil
}

func (s *gormStore) DeactivateStale(ctx context.Context, keys []string) error {
	if s.db == nil || len(keys) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&model.SysJob{}).
		Where("job_key IN ?", keys).
		Updates(map[string]any{"is_active": false, "updated_at": time.Now()}).Error
}

// RecoverStaleRuns 回收崩溃遗留的 running 行。
//
// 判据是**时间**而不是"实例是否存活",所以不需要心跳表和心跳 goroutine:
// 真在跑的任务,started_at 到现在也不会超过它自己的 Timeout —— 到了 Timeout
// 它自己就会被 ctx 取消并写成 failed。留在这里的,只能是进程死了。
func (s *gormStore) RecoverStaleRuns(ctx context.Context, before time.Time) (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&model.SysJobRun{}).
		Where("status = ? AND started_at < ?", model.JobStatusRunning, before).
		Updates(map[string]any{
			"status":      model.JobStatusFailed,
			"finished_at": &now,
			"error":       "进程异常退出,启动时回收",
			"updated_at":  now,
		})
	return res.RowsAffected, res.Error
}

// purgeBatch 清理批大小:按 id 分批删,避免长事务 + 跨驱动兼容
// (MySQL 的 DELETE ... LIMIT 与 SQLite 语法不同,IN (...) 两边都对)
const purgeBatch = 1000

func (s *gormStore) PurgeHistory(ctx context.Context, days, max int) (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	var total int64

	// 1) 按保留天数
	if days > 0 {
		cutoff := time.Now().AddDate(0, 0, -days)
		n, err := s.purgeWhere(ctx, "started_at < ?", cutoff)
		if err != nil {
			return total, err
		}
		total += n
	}

	// 2) 按条数上限:防"一天几万次"的高频任务把表撑爆
	if max > 0 {
		var kept int64
		if err := s.db.WithContext(ctx).Model(&model.SysJobRun{}).Count(&kept).Error; err != nil {
			return total, err
		}
		if over := kept - int64(max); over > 0 {
			// 保留最新的 max 条,删掉比它更旧的所有行
			n, err := s.purgeWhere(ctx, "id NOT IN (?)",
				s.db.Model(&model.SysJobRun{}).
					Select("id").Order("started_at DESC, id DESC").Limit(max))
			if err != nil {
				return total, err
			}
			total += n
		}
	}
	return total, nil
}

// purgeWhere 分批删除。sub 可以是 *gorm.DB(子查询),nil 时走纯条件。
func (s *gormStore) purgeWhere(ctx context.Context, where string, args ...any) (int64, error) {
	var deleted int64
	for {
		var ids []uint64
		q := s.db.WithContext(ctx).Model(&model.SysJobRun{}).Select("id").
			Where(where, args...).Limit(purgeBatch)
		if err := q.Find(&ids).Error; err != nil {
			return deleted, err
		}
		if len(ids) == 0 {
			return deleted, nil
		}
		// ⚠️ **必须 Unscoped —— 这是物理删。**
		//
		// model.BaseModel 带 DeletedAt gorm.DeletedAt,所以默认的 Delete() 是软删:
		// 行还在表里,只是 deleted_at 被盖了时间戳。而 PurgeHistory 的存在意义就是
		// **让表变小** —— 软删之后表只会越涨越大,任务跑得再勤也一行都不少,
		// 而且没有任何报错。探针实测:删 5 条后 Count() 返 0(看不见了),
		// Unscoped().Count() 仍返 5(物理行还在)—— 典型的假绿灯。
		//
		// 软删对本表没有任何价值:执行记录是纯日志,没有"要查已删除的历史"这种需求,
		// 留着的唯一后果就是磁盘和索引一起涨。
		res := s.db.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&model.SysJobRun{})
		if res.Error != nil {
			return deleted, res.Error
		}
		deleted += res.RowsAffected
		if len(ids) < purgeBatch {
			return deleted, nil
		}
	}
}

// nopStore 无 DB 模式(单测 / 极简部署):记录全部丢弃,Layer 0 恒为"没在跑"
type nopStore struct{}

func (nopStore) HasRunning(context.Context, string) (bool, error)  { return false, nil }
func (nopStore) CreateRun(context.Context, *model.SysJobRun) error { return nil }
func (nopStore) FinishRun(context.Context, *model.SysJobRun) error { return nil }
func (nopStore) LoadOverrides(context.Context) (map[string]Override, error) {
	return map[string]Override{}, nil
}
func (nopStore) DeactivateStale(context.Context, []string) error            { return nil }
func (nopStore) RecoverStaleRuns(context.Context, time.Time) (int64, error) { return 0, nil }
func (nopStore) PurgeHistory(context.Context, int, int) (int64, error)      { return 0, nil }

// remember StaleGrace 崩溃行回收的宽限:至少要比最长任务 Timeout 更长,
// 否则一个正在跑 29 分钟的任务会被误判为"进程死了"
const StaleGrace = 10 * time.Minute

// ErrNoDB 装配时传了需要 DB 的模式但没有 DB
var ErrNoDB = errors.New("cronx: 需要 DB 支持")

func logStaleRecovered(n int64, err error) {
	if err != nil {
		logger.L().Warn("cronx: 崩溃遗留 running 行回收失败", zap.Error(err))
		return
	}
	if n > 0 {
		logger.L().Warn("cronx: 已回收进程异常退出遗留的 running 执行记录",
			zap.Int64("count", n))
	}
}
