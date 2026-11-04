// 短信模板审核状态同步(cronx 定时任务,M115)
//
// # 这个任务解决什么
//
// 模板在服务商那边过没过审,**平台只有主动去问才知道**。
// 而"什么时候问"这件事有两个不能省的理由:
//
//  1. **守卫依赖状态的时效性** —— 发送前的守卫(pkg/smstpl.Guard)判的是
//     "登记册里这张模板确定发不出去"。登记册里的状态如果永远停在
//     "上次同步时的样子",那模板被服务商驳回之后,平台会**一直**按
//     旧状态放行,守卫形同虚设。
//  2. **人工停用是"止血",但它不是"永久"** —— 运营改了模板内容重新提交审核后,
//     服务商那边会通过;没人去拉,登记册就永远停在 disabled。
//
// ⇒ 每 10 分钟拉一次。慢于这个间隔,守卫的"提前拦截"就没有意义了。
//
// # ⚠️ 与后台「立即同步」是同一个入口
//
// 后台管理页的手动同步(service/sms_template.go 的 Sync)与本任务都调
// `smstpl.Runtime.SyncOnce` —— **不是各写一遍同步逻辑**。
// 两处各写一遍的话,迟早漂移:后台页面按 A 判、任务按 B 判,
// 运营在页面上看到的和实际生效的不是一回事。
//
// ⚠️ **两者天然串行** —— Runtime 内部有一把锁(见 runtime.go 的包注释),
// 同步与人工流转改的是同一本登记册,不许并发。
//
// # ⚠️ 四种"不成功"的处置各不相同
//
//	通道不支持查询(ErrNoFetcher) → **跳过**,不是失败
//	                              console provider 压根没有模板可审,
//	                              这是"这个通道没这个能力",不是"同步坏了"。
//	                              标红会让运维以为出故障。
//	单个场景拉不到 / 状态机拒绝流转 → **不失败任务**,状态保持原样,下一轮重试
//	                              把它们算作任务失败 = 一次服务商抖动
//	                              就把整个同步标成坏的。
//	**所有场景都没定下来** → **任务记 failed**
//	                              这不是"个别场景抖了一下",是"这一轮什么也没干成"
//	                              (凭证错了 / 服务商挂了 / 解析出 bug)。
//	                              旧口径把它记成 success ⇒ 运维看着一条绿色的
//	                              "成功",页面上却是一片"未同步",且哪里都没写为什么。
//	                              ⇒ 失败原因要跟着进 sys_job_run.error。
//	系统性失败(配置读不到 / 落库失败) → 抛出去,任务记 failed
//
// # ⚠️ 注册在 main 装配期调一次,不是 init()
//
// 依赖(Runtime)进闭包,不用包级变量事后注入 —— 那是全局可变状态 + 竞态面。
// 代价:必须在 `cronx.New / Start` **之前**调,否则任务永远没有 entry
// (NextRunAt 是空的,而且没有任何报错)。
//
// ⚠️ **cron 表达式是 5 段,不是 6 段** —— cronx 的解析器是
// `cron.NewParser(cron.Minute|Hour|Dom|Month|Dow|Descriptor)`,**没有 Seconds**。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/smstpl"
)

// KeySmsTemplateSync 同步任务的 key
//
// ⚠️ **前缀必须是 `framework.*`** —— 短信模板审核流是框架层能力
// (不是某个业务模块的私事),用 `biz.*` 会让运维按业务任务分类,排查时找不到。
const KeySmsTemplateSync = "framework.sms_template.sync"

// smsTemplateSyncCron 同步频率:每 10 分钟一次
//
// ⚠️ **5 段** —— 分钟 / 小时 / 日 / 月 / 周,没有秒位(见包注释)。
//
// ⚠️ **分钟数错开整点** —— 7/17/27/37/47/57,全仓约定(见 pkg/jobs 的包注释
// 「错开整点」):所有副本都按绝对墙钟触发,整点那一刻大家一起醒来,
// 去重锁的那一瞬间最拥挤。
//
// ⚠️ **10 分钟是"上限",不是"随便定的"** —— 守卫依赖登记册的时效性:
// 模板被服务商驳回后,最多 10 分钟状态就更新了(守卫随即开始拦)。
// 再慢,"提前拦截"这件事就不成立了。
const smsTemplateSyncCron = "7,17,27,37,47,57 * * * *"

// RegisterSmsTemplateSyncJob 注册短信模板审核状态同步任务(main 装配期调**一次**)
//
// ⚠️ **必须在 `cronx.New / Start` 之前调** —— cronx 的注册表是"启动时
// 一次性快照"(scheduler.go 的 registeredKeys()),晚注册的任务永远没有
// entry,NextRunAt 是空的,而且**没有任何报错**。
//
// ⚠️ **无条件注册,不按 provider 判断** —— provider 是**运行时可切**的
// (配置中心面板上把 aliyun 切成 console,立刻生效)。
// 按"当前 provider 是不是 aliyun"来决定注册与否的话,切换的那一刻
// 任务就没了,切回来也不会自己回来。
//
// ⚠️ **重复注册同一个 key 会 panic**(cronx/registry.go)—— 这是故意的。
// 写测试时**不要**调这个函数(同一个进程里调两次照样炸)—— 直接调
// `syncSmsTemplateStatus` 测逻辑。
func RegisterSmsTemplateSyncJob(rt *smstpl.Runtime) {
	cronx.Register(cronx.Job{
		Key:  KeySmsTemplateSync,
		Name: "短信模板审核状态同步",
		Description: "每 10 分钟从短信服务商拉一次模板审核状态,写回登记册。" +
			"人工停用的模板不会被自动恢复;单个场景拉取失败不影响其它场景," +
			"状态保持原样,下一轮自动重试;" +
			"本轮所有场景都没能确定状态时,任务记失败并写明每个场景的原因。",
		Cron:    smsTemplateSyncCron,
		Enabled: true,
		// ⚠️ **Timeout 给 2 分钟,不给默认的 30 分钟** ——
		// 一轮最多 4 个场景 × 1 次 HTTP 调用,2 分钟绰绰有余;
		// 而默认的 30 分钟会让"服务商卡住"的任务占着去重锁不放
		// (锁 TTL 比它短,第二个副本会抢进来并发跑同一批场景)。
		Timeout: 2 * time.Minute,
		// ⚠️ **NonIdempotent 留零值(false)** —— 同步是幂等的:跑两遍
		// 得到同样的状态。填成 true 反而危险:非幂等任务在锁降级
		// (Redis 不可用)时会被**整个跳过** ⇒ "服务商把模板驳回了"
		// 这件事永远同步不回来 ⇒ 守卫拿着过期的状态放行。
		NonIdempotent: false,
		Handler: func(ctx context.Context) error {
			return syncSmsTemplateStatus(ctx, rt)
		},
	})
}

// syncSmsTemplateStatus 跑一轮同步(定时任务与后台「立即同步」共用同一份逻辑)
//
// ⚠️ **入参而不是包级变量** —— 依赖进参数,测试就能直接调它,
// 不用装配全局状态。
func syncSmsTemplateStatus(ctx context.Context, rt *smstpl.Runtime) error {
	if rt == nil {
		return errors.New("smstpl: 同步任务缺少运行期装配")
	}

	res, err := rt.SyncOnce(ctx)
	if err != nil {
		// ⚠️ **"这个通道没有审核状态"不是故障** ——
		// console provider 只把验证码打到日志,压根没有模板可审。
		// 记一条 Info 就走,**别把任务标红** —— 标红会让运维以为
		// 同步坏了,实际上只是当前通道没这个能力。
		if errors.Is(err, smstpl.ErrNoFetcher) {
			logger.L().Info("短信模板:当前短信通道不支持查询模板审核状态,跳过本轮同步")
			return nil
		}
		// 其余是**系统性失败**(生效配置读不到 / 落库失败)⇒ 抛出去,
		// 让任务记 failed(连续几轮失败要在 sys_job_run 里看得见)。
		return err
	}

	// ⚠️ **这一轮一个状态都没定下来 ⇒ 任务必须记 failed** ——
	//
	//	旧口径「单个场景失败不影响整个任务」是对的,但它漏了一种情况:
	//	**所有场景都失败**。那不是"个别场景抖了一下",
	//	那是**这一轮什么也没干成**(凭证错了 / 服务商挂了 / 解析出 bug)。
	//
	//	旧口径下它记 success ⇒ 运维在定时任务里看到一条绿色的"成功",
	//	而页面上四个场景还是"未同步",**且哪里都没有写为什么**
	//	—— 这正是用户 2026-10-30 报的现象("定时器记录显示成功,
	//	列表还是显示未同步")。
	//
	//	⇒ 判据:没定下来的场景数 == 尝试数 ⇒ 抛错,任务记 failed,
	//	  错误信息里带上每个场景的原因(运维点开执行记录就能看到)。
	if res.AllFailed() {
		return fmt.Errorf("短信模板:本轮 %d 个场景都没能确定状态(%s)",
			res.Attempted, describeSmsSyncFailures(res.Failures))
	}

	// ⚠️ **单个场景失败不失败整个任务** ——
	// 状态保持原样(拉不到 ≠ 被驳回),下一轮自动重试。
	// 把它们算作"任务失败"会让运维以为整个同步挂了。
	//
	// ⚠️ **文案不能说"失败"** —— Failed 里混着两种东西:
	// 真拉不到(服务商抖了),和状态机按规则拒绝流转
	// (人工停用的模板,服务商说通过 —— 硬规则 2 在起作用,不是故障)。
	// 一律说"失败"的话,每个被人工停用的模板都会每 10 分钟报一次警。
	if len(res.Failed) > 0 {
		logger.L().Warn("短信模板:部分场景本轮没能确定状态"+
			"(服务商查询失败,或状态机按规则拒绝流转),状态保持原样,下一轮自动重试",
			zap.Strings("scenes", res.Failed),
			zap.String("reasons", describeSmsSyncFailures(res.Failures)))
	}

	// ⚠️ **只有"有变化"才记 Info** —— 这一轮什么都不变是常态
	// (每 10 分钟一次 × 全天 = 144 次)。每次都记 Info 会把真正的
	// 变化淹没在噪声里。"任务还活着"这件事由 sys_job_run 的执行记录负责。
	if len(res.Registered) > 0 || len(res.Updated) > 0 {
		logger.L().Info("短信模板:审核状态同步完成",
			zap.Strings("registered", res.Registered),
			zap.Strings("updated", res.Updated),
			zap.Int("unchanged", res.Unchanged))
	} else {
		logger.L().Debug("短信模板:审核状态同步完成,状态无变化",
			zap.Int("unchanged", res.Unchanged))
	}
	return nil
}

// describeSmsSyncFailures 把失败原因压成一句人话
//
// ⚠️ **要能进 sys_job_run.error** —— 运维点开执行记录就能看到
//
//	"为什么没同步上",不用去翻日志。
func describeSmsSyncFailures(failures []smstpl.SceneFailure) string {
	if len(failures) == 0 {
		return "无明细"
	}
	parts := make([]string, 0, len(failures))
	for _, f := range failures {
		reason := f.Reason
		if reason == "" {
			reason = "原因未记录"
		}
		parts = append(parts, f.Scene+": "+reason)
	}
	return strings.Join(parts, "; ")
}
