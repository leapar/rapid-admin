package cronx

import "sync"

var (
	defaultMu       sync.RWMutex
	defaultInstance *Scheduler
)

// SetDefault 登记全局单例(装配期调一次)
//
// 与 cachex.SetDefault / auth.SetDefault 同一条纪律:让"调度器从哪来"只有一个答案。
// 做法是包级单例而不是把 *Scheduler 一路传进每个 service —— 后者会把构造参数
// 从 3 个涨到 4 个,并且逼着每一层都知道 cronx 的存在。
func SetDefault(s *Scheduler) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultInstance = s
}

// Default 返回全局单例;未装配时返 nil。
//
// 返 nil 而不是 panic:API 层要能在 job.enabled=false 时也把页面正常渲染出来
// (告诉运维"调度器没开"),而不是整个后台 500。
func Default() *Scheduler {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultInstance
}

// ClearDefault 摘掉单例(单测用:每个用例注册的任务互相独立)
func ClearDefault() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultInstance = nil
}

// resetRegistry 清空注册表 —— 只给单测用。
//
// 生产代码**永远不能**调用它:注册表是全局唯一的,清掉等于把所有任务删了,
// 而且任务只能靠重启进程重新注册(没有 Register 之外的运行期注册入口)。
func resetRegistry() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.jobs = make(map[string]Job)
	globalRegistry.order = nil
}
