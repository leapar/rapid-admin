// 多 Core 装配 —— 控制台 + info/warn/error/audit/access 分文件
// 文档 §10.3 + §10.4
package logger

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config 日志配置
type Config struct {
	Level      string
	Format     string // json / console
	Dir        string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
	Env        string // dev / test / pre / prod
}

// New 创建多 Core zap logger
// 输出:
//
//	${dir}/${YYYY-MM-DD}/info.log    info/warn/error 全记录
//	${dir}/${YYYY-MM-DD}/warn.log    warn 及以上
//	${dir}/${YYYY-MM-DD}/error.log   error 及以上
//	${dir}/${YYYY-MM-DD}/audit.log   审计(通过 Audit() 命名 logger 单独写)
//	${dir}/${YYYY-MM-DD}/access.log  访问日志(通过 Access() 命名 logger 单独写)
//	+ stdout(开发环境)
func New(cfg Config) (*zap.Logger, error) {
	if cfg.Dir == "" {
		cfg.Dir = "logs"
	}
	if cfg.MaxSizeMB == 0 {
		cfg.MaxSizeMB = 100
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 30
	}
	if cfg.MaxAgeDays == 0 {
		cfg.MaxAgeDays = 90
	}
	if cfg.Compress == false && cfg.Env == "prod" {
		// 生产默认压缩
		cfg.Compress = true
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "ts"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.MessageKey = "msg"
	encoderCfg.LevelKey = "level"
	encoderCfg.CallerKey = "caller"
	encoderCfg.StacktraceKey = "stacktrace"

	// 每个 core 一份自己的 encoder
	//
	// encoder 内部持有 buffer(`With` 追加的字段就存在那儿),
	// 共享一个实例 = 共享可变状态 —— 分开最省心。
	newEncoder := func() zapcore.Encoder {
		if cfg.Format == "console" {
			return zapcore.NewConsoleEncoder(encoderCfg)
		}
		return zapcore.NewJSONEncoder(encoderCfg)
	}
	// audit / access 恒为 JSON —— 这两份是给机器读的(审计/访问日志检索)
	newJSONEncoder := func() zapcore.Encoder { return zapcore.NewJSONEncoder(encoderCfg) }

	maxSize := int64(cfg.MaxSizeMB) * 1024 * 1024

	// 1. 控制台(仅 dev 启用)
	var cores []zapcore.Core
	if cfg.Env == "dev" || cfg.Env == "test" {
		cores = append(cores, zapcore.NewCore(newEncoder(), zapcore.AddSync(os.Stdout), levelFromString(cfg.Level)))
	}

	// 2. info.log (level >= info)
	infoRotator := newTimeRotator(cfg.Dir, "info", maxSize, cfg.MaxBackups, cfg.MaxAgeDays, cfg.Compress)
	cores = append(cores, zapcore.NewCore(newEncoder(), zapcore.AddSync(infoRotator), zap.InfoLevel))

	// 3. warn.log (level >= warn)
	warnRotator := newTimeRotator(cfg.Dir, "warn", maxSize, cfg.MaxBackups, cfg.MaxAgeDays, cfg.Compress)
	cores = append(cores, zapcore.NewCore(newEncoder(), zapcore.AddSync(warnRotator), zap.WarnLevel))

	// 4. error.log (level >= error)
	errorRotator := newTimeRotator(cfg.Dir, "error", maxSize, cfg.MaxBackups, cfg.MaxAgeDays, cfg.Compress)
	cores = append(cores, zapcore.NewCore(newEncoder(), zapcore.AddSync(errorRotator), zap.ErrorLevel))

	// 5. audit.log + access.log 独立通道(**core 级路由**,不是 hook)
	//
	// ⚠️ M99(2026-10-26):两个 rotator 提到 New 里创建,原因有二 ——
	//   1) audit/access 的 encoder 之前用 `zap.NewProductionEncoderConfig()`,
	//      **没跟着上面那份自定义 encoderCfg 走** → 它们的 ts 是 epoch 秒,
	//      而 info/warn/error 的 ts 是 ISO8601,跨文件对不上时间。
	//   2) 独立通道的 core **不在 tee 里**,`log.Sync()` 刷不到它们 →
	//      必须由 SyncFlush() 单独兜(见 SyncFlush 注释)。
	//
	// ⚠️ M99(2026-10-26)第二处修正:此前用 `zap.Hooks(...)` 做路由,
	// 而 zap 的 hook **拿不到每次调用的结构化字段**
	// (`zapcore.hooked.Write(ent, _ []Field)` 直接把 fields 丢了)——
	// 于是 audit.log / access.log 里只剩 level/ts/logger/caller/msg,
	// path / method / status / user 这些**全没了**。
	// 改成 core 级路由(namedCore):见 route.go。
	auditRotator := newTimeRotator(cfg.Dir, "audit", maxSize, cfg.MaxBackups, cfg.MaxAgeDays, cfg.Compress)
	accessRotator := newTimeRotator(cfg.Dir, "access", maxSize, cfg.MaxBackups, cfg.MaxAgeDays, cfg.Compress)
	cores = append(cores,
		&namedCore{
			Core: zapcore.NewCore(newJSONEncoder(), zapcore.AddSync(auditRotator), zap.InfoLevel),
			name: LoggerNameAudit,
		},
		&namedCore{
			Core: zapcore.NewCore(newJSONEncoder(), zapcore.AddSync(accessRotator), zap.InfoLevel),
			name: LoggerNameAccess,
		},
	)

	logger := zap.New(zapcore.NewTee(cores...),
		zap.AddCaller(),
		zap.AddStacktrace(zap.ErrorLevel),
		zap.Fields(
			zap.String("app", "rapid-admin"),
			zap.String("env", cfg.Env),
		),
	)
	// 登记 rotator(SyncFlush 用);一次 New 一套,后建的覆盖先建的
	registerRotators(infoRotator, warnRotator, errorRotator, auditRotator, accessRotator)
	return logger, nil
}

// Audit 取得 named audit logger(独立写入 audit.log)
func Audit() *zap.Logger { return L().Named(LoggerNameAudit) }

// Access 取得 named access logger(独立写入 access.log)
func Access() *zap.Logger { return L().Named(LoggerNameAccess) }

func levelFromString(s string) zapcore.Level {
	switch s {
	case "debug":
		return zap.DebugLevel
	case "warn":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}

// rotator 登记表(SyncFlush 用)
var (
	rotatorMu sync.Mutex
	rotators  []*TimeRotator
)

// registerRotators 登记本次 New 创建的 rotator
//
// 一次 New 一套 —— 后建的覆盖先建的(测试里反复 New 不会攒出一堆僵尸)。
func registerRotators(rs ...*TimeRotator) {
	rotatorMu.Lock()
	defer rotatorMu.Unlock()
	rotators = rs
}

// currentRotators 取当前登记的 rotator 快照
func currentRotators() []*TimeRotator {
	rotatorMu.Lock()
	defer rotatorMu.Unlock()
	out := make([]*TimeRotator, len(rotators))
	copy(out, rotators)
	return out
}

// SyncFlush 退出前同步所有 rotator(把数据 fsync 到盘)
//
// # ⚠️ M99(2026-10-26):此前是 `return nil` 的空壳
//
// 名字承诺"同步所有 rotator",实现什么都不做 —— 典型的"注释与实现不符"。
//
// # 为什么 `log.Sync()` 还不够
//
// `zap.Logger.Sync()` 只刷 **tee 里的 core**。audit / access 两个 core 是
// 通过 `zap.Hooks(...)` 挂上去的,**不在 tee 里** → `log.Sync()` 碰不到它们。
//
// tee 里的三个(info/warn/error)能被刷到,是因为 `TimeRotator` 同时实现了
// `Write` 和 `Sync`,满足 `zapcore.WriteSyncer` —— `zapcore.AddSync` 见到
// WriteSyncer 会**原样返回**,不会包成 no-op 的 writerWrapper。
//
// 所以缺的就是 hook 那两个,由本函数兜。
func SyncFlush() error {
	for _, r := range currentRotators() {
		if r == nil {
			continue
		}
		if err := r.Sync(); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭所有 rotator 的文件句柄
//
// # 为什么需要它
//
//  1. **进程退出**:文件句柄不关,Windows 上日志目录删不掉、日志文件被锁;
//     Linux 上靠进程退出回收,但显式关闭更干净(尤其是"热重载"场景 ——
//     旧 logger 的句柄还开着,新 logger 又开一份,句柄泄漏)。
//  2. **测试收尾**:rotator 的文件是懒打开且**一直持有**,
//     `t.TempDir()` 的清理(RemoveAll)在 Windows 上会因为文件被占用而失败
//     → 测试报 "The process cannot access the file because it is being used
//     by another process"。
//
// 关闭后 rotator 是**可自愈**的:下一次 Write 发现 currentFile == nil 会
// 重新 rotate 打开(见 rotator.go 的 `|| r.currentFile == nil` 判据)。
func Close() error {
	var errs []error
	for _, r := range currentRotators() {
		if r == nil {
			continue
		}
		if err := r.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Must 启动失败 panic
func Must(l *zap.Logger, err error) *zap.Logger {
	if err != nil {
		panic(fmt.Errorf("init logger: %w", err))
	}
	return l
}

// Default 全局 logger(由 main.go 启动后注入)
var Default *zap.Logger

// L 简化访问(避免在 pkg 内部循环引用)
func L() *zap.Logger {
	if Default == nil {
		return zap.NewNop()
	}
	return Default
}
