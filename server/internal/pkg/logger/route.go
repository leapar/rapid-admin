// 按 logger 名把日志路由到独立文件 —— audit.log / access.log
//
// 文档 §10.3(审计)/ §10.4(访问日志)
package logger

import (
	"go.uber.org/zap/zapcore"
)

const (
	// LoggerNameAudit 审计通道的 logger 名
	LoggerNameAudit = "audit"
	// LoggerNameAccess 访问日志通道的 logger 名
	LoggerNameAccess = "access"
)

// namedCore 只接受 LoggerName 匹配的条目
//
// # 为什么不是 zap.Hooks
//
// ⚠️ M99(2026-10-26)实证:zap 的 hook **拿不到每次调用的结构化字段**。
//
// hook 的签名是 `func(zapcore.Entry) error`,而每次调用的字段
// (`Audit().Info("audit", zap.String("module", p))`)走的是另一条路 ——
// `Core.Write(ent, fields)` 的第二个参数。zap 内部
// `zapcore.hooked.Write(ent Entry, _ []Field)` **直接把 fields 丢了**。
//
// 于是旧实现里 audit.log / access.log 只有 level/ts/logger/caller/msg,
// path / method / status / user 这些**全部消失** ——
// 一份没有内容的访问日志,等于没有访问日志。
//
// # 改成 core 级路由
//
// 条目在 `Check` 阶段按 LoggerName 选核心,`Write(ent, fields)` 时字段原样带过去。
//
// 路由规则不变:`LoggerName == "audit"` → audit.log,`"access"` → access.log,
// **空串(无名 logger)两条都不进**。
type namedCore struct {
	zapcore.Core
	name string
}

// Check 只有 logger 名对得上才登记自己
//
// 登记进 CheckedEntry 的是**内层** core(zapcore 的 AddCore 语义),
// 所以 `Write` 时拿到的就是原始 fields。
func (c *namedCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if ent.LoggerName != c.name {
		return ce
	}
	return c.Core.Check(ent, ce)
}

// With 必须保住 name
//
// ⚠️ 忘了的话 `With` 之后这个包装就没了,过滤失效 ——
// **所有日志都会进 audit.log**。
func (c *namedCore) With(fields []zapcore.Field) zapcore.Core {
	return &namedCore{Core: c.Core.With(fields), name: c.name}
}

// Write 同样守住名字
//
// 正常路径下 zap 注册进 CheckedEntry 的是内层 core,这个方法不会被调到;
// 但 Core 接口要求实现,而且直接调用时也不该越权写别的通道。
func (c *namedCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	if ent.LoggerName != c.name {
		return nil
	}
	return c.Core.Write(ent, fields)
}
