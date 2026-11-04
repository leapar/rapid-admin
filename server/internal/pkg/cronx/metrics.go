package cronx

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	jobRunsTotal   *prometheus.CounterVec
	jobDuration    *prometheus.HistogramVec
	jobLockSkipped *prometheus.CounterVec
	jobDegraded    *prometheus.CounterVec

	promMu     sync.Mutex
	promInited bool
)

// InitMetrics 注册 cronx 指标到给定 registry(与 cachex.InitMetrics 同构)
//
// 重复调用 panic:prometheus 不允许重名注册,让"初始化两次"在启动时炸,
// 而不是让第二个 registry 拿到空指标、排查时看到"指标时有时无"。
func InitMetrics(reg prometheus.Registerer) {
	promMu.Lock()
	defer promMu.Unlock()
	if promInited {
		panic("cronx: InitMetrics already called (prometheus does not allow re-register)")
	}
	jobRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "app", Subsystem: "job", Name: "runs_total",
		Help: "定时任务执行次数",
	}, []string{"job", "trigger", "status"})

	jobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "app", Subsystem: "job", Name: "duration_seconds",
		Help:    "定时任务执行耗时(秒)",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 12),
	}, []string{"job"})

	jobLockSkipped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "app", Subsystem: "job", Name: "lock_skipped_total",
		Help: "因去重(Layer 0/1)而跳过本次触发的次数 —— 多副本下这个数持续增长说明去重生效",
	}, []string{"job"})

	jobDegraded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "app", Subsystem: "job", Name: "degraded_total",
		Help: "在去重能力降级(锁不可用)状态下执行的次数 —— 大于 0 就该查 Redis 连接",
	}, []string{"job"})

	for _, c := range []prometheus.Collector{jobRunsTotal, jobDuration, jobLockSkipped, jobDegraded} {
		reg.MustRegister(c)
	}
	promInited = true
}

func resetForTest() {
	promMu.Lock()
	defer promMu.Unlock()
	jobRunsTotal, jobDuration, jobLockSkipped, jobDegraded = nil, nil, nil, nil
	promInited = false
}

// recordRun 记一次执行(全 nil-safe:没 Init 就静默跳过)
func recordRun(jobKey string, src TriggerType, status string, durMs int64, degraded bool) {
	if jobRunsTotal != nil {
		jobRunsTotal.WithLabelValues(jobKey, string(src), status).Inc()
	}
	if jobDuration != nil {
		jobDuration.WithLabelValues(jobKey).Observe(float64(durMs) / 1000)
	}
	if degraded && jobDegraded != nil {
		jobDegraded.WithLabelValues(jobKey).Inc()
	}
}

func recordLockSkipped(jobKey string) {
	if jobLockSkipped != nil {
		jobLockSkipped.WithLabelValues(jobKey).Inc()
	}
}

func recordDegraded(jobKey string) {
	if jobDegraded != nil {
		jobDegraded.WithLabelValues(jobKey).Inc()
	}
}
