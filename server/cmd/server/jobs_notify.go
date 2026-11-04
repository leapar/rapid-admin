// 定时任务执行事件 → 事件推送通道(M73 装配层)
//
// 这个文件存在的唯一理由是**依赖方向**:cronx 不知道 pkg/push 的存在,
// push 也不知道 cronx。两边的对接点就是 cronx.Config.OnFinish 这个回调,
// 映射逻辑放在这里(装配层)而不是塞进任一包里。
//
// 同构参考:cronx.ResolveLocker 也是同一个思路 —— "要不要连 Redis"的决策
// 留在 main 包,pkg 层只暴露能力。
package main

import (
	"context"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/push"
)

// jobEventPublishTimeout 单次投递的兜底 deadline。
//
// ⚠️ push.Publish 本身永不返回 error(失败只记日志),但它内部要等 Redis
// 的网络往返 —— 万一 Redis 是"连得上但卡住"而不是"连不上",一个没有
// deadline 的 Publish 会把跑完的任务的收尾流程一起拖住。
//
// 2s 是这么定的:一次 Redis PUBLISH 在同机房是毫秒级,2s 已经宽松到能覆盖
// 一次 GC 停顿和网络抖动;再长就会让"任务执行记录"和"任务状态推送"产生
// 可感知的延迟差,而推送本来就是尽力而为的附加能力。
const jobEventPublishTimeout = 2 * time.Second

// jobEventPublisher 返回 cronx.Config.OnFinish 的实现。
//
// 发的是 **job.finished / job.failed 两个事件名**,而不是"一个 job 事件 +
// status 字段":前端按事件名订阅(`subscribe('job.failed', ...)`)比在
// 每个回调里解一遍 status 直观,而且错订阅(只订 finished 还想看失败)
// 会立刻在浏览器 console 里显形,而不是静默漏掉。
func jobEventPublisher(log *zap.Logger) func(context.Context, *model.SysJobRun) {
	return func(ctx context.Context, rec *model.SysJobRun) {
		if rec == nil {
			return
		}

		event := push.EventJobFinished
		if rec.Status == model.JobStatusFailed {
			event = push.EventJobFailed
		}

		// ⚠️ payload 是白名单,不是整个 rec:
		// 事件会广播给**所有**有 job:read 的人连接的浏览器。执行记录里还有
		// Error 正文(可能含业务数据 / SQL 片段)、TraceID、Operator ——
		// 这些不该被推给"只要能看到任务状态"的所有人。前端要详情自己
		// refetch /jobs/runs,那才是按权限过滤过的地方。
		pushCtx, cancel := context.WithTimeout(ctx, jobEventPublishTimeout)
		defer cancel()

		push.Publish(pushCtx, []string{push.TopicJob}, push.Event{
			Topic: push.TopicJob,
			Event: event,
			// At 不填:Publish 内部在 ev.At==0 时补时间戳,这里再算一遍
			// 只是多一个"时间戳该由谁产生"的歧义点
			Payload: push.JobPayload{
				JobKey:      rec.JobKey,
				Status:      string(rec.Status),
				DurationMs:  rec.DurationMs,
				TriggerType: string(rec.TriggerType),
			},
		})

		// 只在降级执行时打这条:正常任务的日志里"成功"信息已经由 cronx 自己
		// 打过一条了,这里重复一遍纯属噪音。
		if rec.Degraded {
			log.Warn("cronx: 去重能力降级下执行的任务已结束,推送状态",
				zap.String("job_key", rec.JobKey),
				zap.String("status", string(rec.Status)),
				zap.Int64("duration_ms", rec.DurationMs))
		}
	}
}
