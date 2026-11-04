package cronx

import (
	"sort"
	"sync"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// registry 代码注册表。
//
// 只增不改:注册发生在包 init 或 main 装配期,运行期不允许增删任务
// (增删任务 = 发版,不是运行时操作)。运行期唯一会变的是「DB 覆盖」。
type registry struct {
	mu   sync.RWMutex
	jobs map[string]Job
	// order 保注册顺序,让管理端列表稳定(而不是 map 随机序)
	order []string
}

var globalRegistry = &registry{jobs: make(map[string]Job)}

// Register 注册一个任务(包 init 或 main 装配期调)
//
// 重复注册同一个 key 会 panic:这一定是复制粘贴错了,让它在启动时炸,
// 而不是让后一个悄悄覆盖前一个 —— 那类问题的现场是"任务怎么没跑"。
func Register(job Job) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	if _, dup := globalRegistry.jobs[job.Key]; dup {
		panic("cronx: 任务重复注册 " + job.Key)
	}
	globalRegistry.jobs[job.Key] = job
	globalRegistry.order = append(globalRegistry.order, job.Key)
}

// lookup 查一个任务(只读注册表,不解锁 overrides)
func lookup(key string) (Job, bool) {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	j, ok := globalRegistry.jobs[key]
	return j, ok
}

// registeredKeys 按注册顺序返回全部 key
func registeredKeys() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	out := make([]string, len(globalRegistry.order))
	copy(out, globalRegistry.order)
	return out
}

// merge 把代码定义与 DB 覆盖合成生效配置
func merge(job Job, ov *Override) Effective {
	e := Effective{Job: job, CronExpr: job.Cron, DBEnabled: true}
	if ov == nil {
		return e
	}
	e.DBEnabled = ov.Enabled && ov.IsActive
	if ov.CronExpr != "" {
		e.CronExpr = ov.CronExpr
		e.CronOverridden = true
	}
	if ov.Name != "" {
		e.Name = ov.Name
	}
	if ov.Description != "" {
		e.Description = ov.Description
	}
	return e
}

// reconcile 启动时比对 DB 覆盖行与代码注册表
//
// DB 里存在但代码里没有的 job_key → 那是从代码里删掉任务后残留的行。
// 置 is_active=false 并 WARN。**不是直接删**:删掉会丢掉运维当初设的
// cron 覆盖值和备注,万一任务是误删、想回滚还能捞回来。
func reconcile(existing map[string]Override, present func(key string) bool) (stale []string) {
	for key := range existing {
		if !present(key) {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		logger.L().Warn("cronx: sys_job 存在代码里没有的残留行,已置 is_active=false",
			zap.String("job_key", key))
	}
	return stale
}
