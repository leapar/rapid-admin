// 定时任务调度配置 —— M67
package config

import "time"

// JobConfig 定时任务(cronx)配置。
//
// ⚠️ **这里配的是"调度层"的行为,不是"哪些数据该定时"** —— 跟 cachex 不收 TTL
// 是同一个理由:哪些 key 缓存/缓存多久是每份数据自己的性质,配置中心改了要全网
// 立刻生效、字典一个月不动,同一个值对两者都是错的。
//
// 哪些任务、什么 cron 表达式是**代码注册**(见 pkg/cronx.Register),
// DB(sys_job)只能覆盖 cron 表达式和启停。
type JobConfig struct {
	// Enabled 定时任务总开关,关掉后所有任务既不注册也不触发
	Enabled bool `mapstructure:"enabled"`

	// TimeZone 调度时区
	//
	// ⚠️ **必须显式配,不能留空取 time.Local。** 容器里 TZ 没设时 time.Local == UTC,
	//    `0 3 * * *` 会在北京时间 11:00 跑。而 system.timezone 字段写着 Asia/Shanghai,
	//    两者不一致时**看起来配置生效了,实际没生效** —— 这类 bug 极难从日志发现,
	//    因为任务确实跑了,只是跑在了错误的时间。
	TimeZone string `mapstructure:"timezone"`

	// LockMode 去重锁实现:auto | redis | memory | off
	//
	//   auto   有 Redis 用 Redis,没有退进程内锁 + 降级告警(默认)
	//   redis  强制 Redis,没配 redis.addr 直接启动失败。
	//         **生产多副本必须用这个** —— 与 kv.mode / auth.revocation_mode 同一条规矩。
	//   memory 强制进程内锁,即使配了 Redis 也不连。仅单副本部署。
	//   off    完全不锁。**仅本地开发**;启动会打一条显式告警。
	LockMode string `mapstructure:"lock_mode"`

	// LockTTL 锁的 TTL
	//
	// 必须显著小于任务时长,否则锁在任务跑到一半时自然过期,第二个副本直接抢进来。
	// 长任务靠 lock_renew 的看门狗每 ttl/3 续期,所以这个值定的是**故障恢复时间**
	// (持锁副本崩溃后,其他副本最多等这么久才能接手),不是任务时长上限。
	LockTTL time.Duration `mapstructure:"lock_ttl"`

	// LockRenew 长任务看门狗续期开关
	LockRenew bool `mapstructure:"lock_renew"`

	// DefaultTimeout Job.Timeout 为 0 时用这个
	DefaultTimeout time.Duration `mapstructure:"default_timeout"`

	// ShutdownTimeout 优雅停机等在跑任务的上限,超时后强杀并记 failed
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`

	// HistoryRetentionDays 执行记录保留天数
	HistoryRetentionDays int `mapstructure:"history_retention_days"`

	// HistoryRetentionMax 执行记录保留条数上限
	//
	// 与保留天数**双保险**:天数防"低频任务长尾堆积",条数防"高频任务一天几万条"。
	// 之所以要双保险,是因为清理历史的任务**本身就是定时任务** —— 它挂了没人知道,
	// 兜底线必须不依赖它自己。
	HistoryRetentionMax int `mapstructure:"history_retention_max"`
}
