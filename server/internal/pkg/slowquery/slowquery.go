// 慢 SQL 报警 —— 包装 GORM logger.Interface
// 文档 §1.8 #14 P1
//
// 设计:
//   - 复用 GORM 内置的 logger.Trace(ctx, begin, fc, err) 钩子(每条 SQL 必走)
//   - 比对 time.Since(begin) > Threshold → 触发 OnSlow
//   - 比对 OnSlow 比 callback chain 简单可靠(避开 Statement 克隆 + 注册位置坑)
//
// 用法:
//   db, _ := gorm.Open(...)
//   slowquery.Install(db, slowquery.Config{Threshold: 500*time.Millisecond})
//
// 也可独立作为 gorm logger 使用:
//   gl := slowquery.NewLogger(origLogger, slowquery.Config{...})
//   db, _ := gorm.Open(dialector, &gorm.Config{Logger: gl})
package slowquery

import (
	"context"
	"errors"
	"math/rand/v2"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/pkg/metricsx"
)

const metricName = "app_slow_sql_total"

// Config 慢查询配置
type Config struct {
	// Threshold 单条 SQL 超过这个时间算"慢"。默认 500ms
	Threshold time.Duration
	// OnSlow 自定义慢查询回调(用于接 Sentry / 告警 / 推送);nil 用默认(warn log + 计数)
	OnSlow func(entry Entry)
	// LogRawSQL 是否打印完整 SQL(默认 false,生产期 SQL 长易爆日志)
	LogRawSQL bool
	// SampleRate 采样率 0.0~1.0(默认 1.0 全量)
	SampleRate float64
	// Logger 自定义 logger;nil 用 noop(只走 metrics,不打 log)
	Logger *zap.Logger
}

// Entry 单次慢查询
type Entry struct {
	SQL      string
	Table    string
	Op       string // query|create|update|delete|row
	Duration time.Duration
	Rows     int64
	Error    string
}

var (
	tableRegexp = regexp.MustCompile(`(?i)\b(from|into|update|join)\s+["` + "`" + `]?([a-zA-Z_][a-zA-Z0-9_]*)["` + "`" + `]?`)

	slowSQLTotal *prometheus.CounterVec
	initOnce     sync.Once
)

func initCounter() {
	initOnce.Do(func() {
		slowSQLTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricName,
				Help: "Total slow SQL queries, labeled by table/op/is_error",
			},
			[]string{"table", "op", "is_error"},
		)
		if metricsx.Registry() != nil {
			metricsx.MustRegister(slowSQLTotal)
		} else {
			prometheus.MustRegister(slowSQLTotal)
		}
	})
}

// Install 在已有 db 上包一层 logger(替换 db.Logger)
//   - 原 logger 仍跑(走 LogMode/Info/Warn/Error)
//   - 但 Trace 走我们的 wrapper,慢的进 OnSlow
//
// 注意:db.InstanceSet("slowquery:installed", true) 幂等
func Install(db *gorm.DB, cfg Config) error {
	if db == nil {
		return errors.New("slowquery: nil db")
	}
	if _, ok := db.InstanceGet("slowquery:installed"); ok {
		return nil
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = 500 * time.Millisecond
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 1.0
	}
	if cfg.OnSlow == nil {
		cfg.OnSlow = defaultOnSlow(cfg.Logger, cfg.LogRawSQL)
	}
	initCounter()
	orig := db.Logger
	db.Logger = newWrappedLogger(orig, cfg)
	db.InstanceSet("slowquery:installed", true)
	return nil
}

// NewLogger 直接构造一个 logger.Interface(适合 gorm.Config{Logger: ...})
func NewLogger(orig logger.Interface, cfg Config) logger.Interface {
	if cfg.Threshold <= 0 {
		cfg.Threshold = 500 * time.Millisecond
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 1.0
	}
	if cfg.OnSlow == nil {
		cfg.OnSlow = defaultOnSlow(cfg.Logger, cfg.LogRawSQL)
	}
	initCounter()
	return newWrappedLogger(orig, cfg)
}

/* ====================== 内部 wrapped logger ====================== */

func newWrappedLogger(orig logger.Interface, cfg Config) logger.Interface {
	w := &wrapped{
		orig:      orig,
		threshold: cfg.Threshold,
		sample:    cfg.SampleRate,
		onSlow:    cfg.OnSlow,
		log:       cfg.Logger,
	}
	if orig == nil {
		// 用 gorm 默认 logger,避免 nil deref
		w.orig = logger.Default
	}
	return w
}

type wrapped struct {
	orig      logger.Interface
	threshold time.Duration
	sample    float64
	onSlow    func(Entry)
	log       *zap.Logger // OnSlow panic 兜底留痕用;nil 回落 zap.L()
}

// LogMode 透传 + 同步设置 SlowThreshold
func (w *wrapped) LogMode(level logger.LogLevel) logger.Interface {
	orig := w.orig.LogMode(level)
	// gorm logger 的 Config 有 SlowThreshold 字段;不同 logger 实现可能没有
	// 这里用 type assertion,失败就跳过
	type slowThresholder interface {
		SetSlowThreshold(time.Duration) logger.Interface
	}
	if st, ok := orig.(slowThresholder); ok {
		orig = st.SetSlowThreshold(w.threshold)
	}
	return &wrapped{
		orig:      orig,
		threshold: w.threshold,
		sample:    w.sample,
		onSlow:    w.onSlow,
		log:       w.log,
	}
}

// Info / Warn / Error 透传
func (w *wrapped) Info(ctx context.Context, msg string, data ...interface{}) {
	w.orig.Info(ctx, msg, data...)
}
func (w *wrapped) Warn(ctx context.Context, msg string, data ...interface{}) {
	w.orig.Warn(ctx, msg, data...)
}
func (w *wrapped) Error(ctx context.Context, msg string, data ...interface{}) {
	w.orig.Error(ctx, msg, data...)
}

// Trace 核心:每条 SQL 必走这里
func (w *wrapped) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	// 先调原 logger(可能 log 完整 SQL)
	w.orig.Trace(ctx, begin, fc, err)
	sql, rows := fc()
	dur := time.Since(begin)
	if dur < w.threshold {
		return
	}
	if w.sample < 1.0 && !sampleOK(w.sample) {
		return
	}
	entry := Entry{
		SQL:      sql,
		Table:    extractTable(sql),
		Op:       opLabel(sql),
		Duration: dur,
		Rows:     rows,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	if slowSQLTotal != nil {
		slowSQLTotal.WithLabelValues(entry.Table, entry.Op, boolLabel(err != nil)).Inc()
	}
	// 触发 OnSlow,recover 避免业务被影响
	func() {
		defer func() {
			if r := recover(); r != nil {
				// ⚠️ OnSlow 是告警链路(接 Sentry / 告警 / 推送),panic 被完全
				// 静默吞掉 = 告警链路可以无声死亡。"不影响业务"的兜底语义保留,
				// 但必须留痕(recover 值 + 栈)。
				log := w.log
				if log == nil {
					log = zap.L()
				}
				log.Error("slowquery: OnSlow panicked",
					zap.Any("panic", r),
					zap.ByteString("stack", debug.Stack()))
			}
		}()
		w.onSlow(entry)
	}()
}

/* ====================== 工具 ====================== */

func defaultOnSlow(log *zap.Logger, logRaw bool) func(Entry) {
	return func(e Entry) {
		if log == nil {
			return
		}
		fields := []zap.Field{
			zap.String("table", e.Table),
			zap.String("op", e.Op),
			zap.Duration("duration", e.Duration),
			zap.Int64("rows", e.Rows),
		}
		if e.Error != "" {
			fields = append(fields, zap.String("error", e.Error))
		}
		if logRaw {
			fields = append(fields, zap.String("sql", truncate(e.SQL, 2000)))
		}
		log.Warn("slow sql detected", fields...)
	}
}

func opLabel(sql string) string {
	s := strings.ToLower(strings.TrimSpace(sql))
	switch {
	case strings.HasPrefix(s, "select"):
		return "query"
	case strings.HasPrefix(s, "insert"):
		return "create"
	case strings.HasPrefix(s, "update"):
		return "update"
	case strings.HasPrefix(s, "delete"):
		return "delete"
	}
	return "other"
}

func extractTable(sql string) string {
	m := tableRegexp.FindStringSubmatch(sql)
	if len(m) >= 3 {
		return strings.ToLower(m[2])
	}
	return "unknown"
}

func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func sampleOK(rate float64) bool {
	// ⚠️ 旧实现用 time.Now().UnixNano()%1000 当伪随机数:Windows 时钟粒度
	// (~15ms)让纳秒低位严重成簇,采样率被量化到 ~10% 一档(0.05 实际 ≈10%,
	// 0.15 只在 0/10 两档跳)。换真随机,采样率才按配置生效。
	return rand.Float64() < rate
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
