package handler

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/errcode"
)

// ── 本文件的来历 ───────────────────────────────────────────────────────
//
// `internal/api/handler` 之前**一张测试都没有** —— 而它是**每个 HTTP 接口
// 的第一道门**(BindAndValidate 在 100+ 个 handler 的第一行被调用)。
//
// 它有一条 P1 真 bug(2026-10-29 M112 修的):
// `BindAndValidate` 里无条件拼 `field + ": " + msg` ——
// 而 `validatorx.Explain` 在**认不出字段**时返回的是空 field
// (err 不是 validator.ValidationErrors,例如 DTO 根本不是 struct)
// ⇒ 消息变成 `": 参数校验失败"` ⇒ 前端弹一个带孤零零冒号的提示。
//
// ── M112 可逆探针记录 ───────────────────────────────────────────────────
//
//	拆法                              必须变红
//	──────────────────────────────  ─────────────────────────────────────
//	无条件拼 ": "(退回老实现)          TestM112_BindAndValidate_NoFieldNoColon
//	字段名丢失                        TestM112_BindAndValidate_KeepsFieldName
//	错误码不是 InvalidParam            TestM112_BindAndValidate_ErrorCode
// ───────────────────────────────────────────────────────────────────────

type bindDTO struct {
	Username string `json:"username" validate:"required,min=3"`
}

// ctxWithJSON 造一个带 JSON body 的请求上下文
func ctxWithJSON(body string) *app.RequestContext {
	c := app.NewContext(0)
	c.Request.Header.SetContentTypeBytes([]byte("application/json"))
	c.Request.SetBody([]byte(body))
	return c
}

// TestM112_BindAndValidate_KeepsFieldName 校验失败要带字段名
func TestM112_BindAndValidate_KeepsFieldName(t *testing.T) {
	h := &BaseHandler{}
	err := h.BindAndValidate(ctxWithJSON(`{"username":"ab"}`), &bindDTO{})
	if err == nil {
		t.Fatal("非法 DTO 应该报错")
	}
	if !strings.Contains(err.Error(), "username") {
		t.Fatalf("错误消息里应带字段名 username,实际 %q", err.Error())
	}
}

// TestM112_BindAndValidate_NoFieldNoColon ⚠️ 核心回归:认不出字段时不拼冒号
//
// DTO 不是 struct ⇒ validator 返 InvalidValidationError(不是 ValidationErrors)
// ⇒ Explain 认不出字段 ⇒ field 为空 ⇒ 老实现会拼出 ": 参数校验失败"。
func TestM112_BindAndValidate_NoFieldNoColon(t *testing.T) {
	h := &BaseHandler{}
	// body 是合法的 JSON 字符串,能绑进 *string;但 *string 不是 struct ⇒
	// validator 返 InvalidValidationError ⇒ Explain 的 field 为空。
	err := h.BindAndValidate(ctxWithJSON(`"hello"`), new(string))
	if err == nil {
		t.Fatal("非 struct DTO 应该报错")
	}
	msg := err.Error()
	if strings.Contains(msg, ": ") {
		t.Fatalf("认不出字段时不该拼 \": \",实际 %q", msg)
	}
	if !strings.Contains(msg, "参数校验失败") {
		t.Fatalf("消息应是「参数校验失败」,实际 %q", msg)
	}
	if strings.HasPrefix(msg, ":") {
		t.Fatalf("消息不该以冒号开头,实际 %q", msg)
	}
}

// TestM112_BindAndValidate_ErrorCode 校验失败的错误码必须是 InvalidParam
func TestM112_BindAndValidate_ErrorCode(t *testing.T) {
	h := &BaseHandler{}
	err := h.BindAndValidate(ctxWithJSON(`{"username":"ab"}`), &bindDTO{})
	if err == nil {
		t.Fatal("非法 DTO 应该报错")
	}
	var ec *errcode.Error
	if !errors.As(err, &ec) {
		t.Fatalf("错误应是 *errcode.Error,实际 %T", err)
	}
	if ec.Code != errcode.InvalidParam {
		t.Fatalf("错误码应为 InvalidParam(%d),实际 %d", errcode.InvalidParam, ec.Code)
	}
	if ec.Detail == nil {
		t.Fatal("错误应带 Detail(前端要靠它定位字段)")
	}
}

// TestM112_BindAndValidate_BadJSON body 坏了返 BadRequest
func TestM112_BindAndValidate_BadJSON(t *testing.T) {
	h := &BaseHandler{}
	err := h.BindAndValidate(ctxWithJSON(`{not json`), &bindDTO{})
	if err == nil {
		t.Fatal("坏 JSON 应该报错")
	}
	if !strings.Contains(err.Error(), "请求体格式错误") {
		t.Fatalf("消息应是「请求体格式错误」,实际 %q", err.Error())
	}
}

// TestM112_BindAndValidate_Pass 合法 DTO 不报错
func TestM112_BindAndValidate_Pass(t *testing.T) {
	h := &BaseHandler{}
	if err := h.BindAndValidate(ctxWithJSON(`{"username":"zhangsan"}`), &bindDTO{}); err != nil {
		t.Fatalf("合法 DTO 不该报错,实际 %v", err)
	}
}
