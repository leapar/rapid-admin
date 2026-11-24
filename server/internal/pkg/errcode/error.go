// 业务错误对象
package errcode

import (
	"errors"
	"fmt"
)

// Error 带业务码的错误
type Error struct {
	Code    int
	Message string
	// Detail 附加数据(例如校验错误的对象,详见 errcode.go 的码表)
	Detail interface{}
	// st 创建点调用栈(New/Newf 在调用瞬间捕获),只进日志不进 wire。
	// 直接字面量构造 &Error{} 的地方没有栈,Stack() 诚实地不渲染帧。
	st Stack
}

func (e *Error) Error() string { return fmt.Sprintf("[%d] %s", e.Code, e.Message) }

// New 创建业务错误(捕获创建点调用栈)
func New(code int, msg ...string) *Error {
	m := Msg(code)
	if len(msg) > 0 && msg[0] != "" {
		m = msg[0]
	}
	return &Error{Code: code, Message: m, st: capture()}
}

// WithDetail 附加数据
func (e *Error) WithDetail(d interface{}) *Error {
	e.Detail = d
	return e
}

// WithMsg 覆盖默认文案
func (e *Error) WithMsg(msg string) *Error {
	e.Message = msg
	return e
}

// Newf 用 format 创建业务错误(捕获创建点调用栈)
func Newf(code int, format string, args ...interface{}) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), st: capture()}
}

// CodeOf 从 error 提取业务码(非 *Error 返回 Internal)
//
// # ⚠️ 必须用 errors.As,不能用类型断言
//
// 裸类型断言 `err.(*Error)` **只认最外层**。而服务层普遍用
// `fmt.Errorf("解析第 %d 关审批人失败: %w", n, err)` 包装错误 ——
// 断言失败 → 一律返回 Internal(50000)→ 客户端拿到 500,
// 真正的业务码(比如 20601 "审批流程配置有误")**在包装层就被吃掉了**。
//
// `handler.BaseHandler.Fail` 一直用的是 `errors.As`,两边语义本来就不一致:
// 同一个错误,走 Fail 是 403,走 CodeOf 是 500。M98 统一成 errors.As。
func CodeOf(err error) int {
	if err == nil {
		return Success
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Internal
}
