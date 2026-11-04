package jobs

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/logger"
)

// purgeHistory 清理 sys_job_run 执行记录。
//
// 导出而不是只在 RegisterAll 里写个闭包:单测要能直接调它验证边界
// (天数刚好卡在 cutoff 上、条数上限刚好等于总数),不去碰注册表。
//
// ⚠️ 两条规则是**双保险**,不是二选一:
//   - 按天数:防「低频任务长尾堆积」—— 一天一条的任务留一年也就 365 行,
//     但 5 年就是 1800 行,而且 3 年前那次失败再也查不到了。
//   - 按条数:防「高频任务一天几万条」—— 保留 30 天也可能是几百万行。
//     之所以要双保险,是因为**清理历史的任务本身也是定时任务**:它挂了没人知道,
//     兜底线必须不依赖它自己(见 config.JobConfig 的同款注释)。
//
// 两个参数都为 0 时**返回 error 而不是静默什么都不做**:
// 那多半是配置写错了(`history_retention_days: 0`),而一个每天都显示
// "执行成功"、实际什么都没清的任务,正是最难被发现的那种坏。
func purgeHistory(ctx context.Context, store cronx.Store, days, max int) error {
	if days <= 0 && max <= 0 {
		return errors.New("保留天数与条数上限都为 0,不做任何清理 —— 请检查 job.history_retention_days / job.history_retention_max")
	}

	n, err := store.PurgeHistory(ctx, days, max)
	if err != nil {
		// 上下文取消/超时不是"清理失败",是"清理没做完"。
		// 消息里带上 ctx 的状态,免得排障的人以为是 DB 挂了
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("执行记录清理被中断(已删除 %d 条):%w", n, ctxErr)
		}
		return fmt.Errorf("执行记录清理失败(已删除 %d 条):%w", n, err)
	}

	// 删了 0 条是常态(表还没到保留期),Info 即可,不该 warn 刷屏
	if n > 0 {
		logger.L().Info("framework: 定时任务执行记录清理完成",
			zap.Int64("deleted", n),
			zap.Int("retention_days", days),
			zap.Int("retention_max", max))
	} else {
		logger.L().Debug("framework: 定时任务执行记录清理完成,无过期记录",
			zap.Int("retention_days", days),
			zap.Int("retention_max", max))
	}
	return nil
}
