// access_log 中间件单测 —— §1.8(M99)
//
// # M99 在这里实证出的 1 个真 bug
//
//	**access.log 这个文件从来没被创建过**
//
//	`pkg/logger` 从一开始就有 access.log 这个独立通道(`logger.Access()`),
//	路由靠的是 `zapcore.Entry.LoggerName`,而 LoggerName 只有
//	`Named(...)` 派生的 logger 才非空。
//
//	中间件此前写的是 `L().Info("access", ...)` —— **无名** logger
//	→ LoggerName == "" → 路由直接跳过它
//	→ rotator 是懒创建的 → **access.log 连文件都不会被创建**。
//
//	后果:运维想查访问日志,那个文件永远是空的;
//	info.log 里混着全部业务日志,没法当访问日志用。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                                必须变红
//	────────────────────────────────────────────  ─────────────────────────────────────
//	P1 AccessLog 改回无名 logger(L().Info)   TestAccessLog_WritesToAccessLog
package middleware

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

// TestAccessLog_WritesToAccessLog 访问日志必须真的落到 access.log
//
// # M99 修的那个 bug
//
// 旧实现写的是无名 logger → LoggerName 为空 → hook 不路由 → 文件都不存在。
//
// ⚠️ 断言要点:不能只看"info.log 里有这条" —— 无名 logger **也会**进 info.log,
// 那样断言对旧实现是绿的(假绿)。必须断言 **access.log 里有**。
func TestAccessLog_WritesToAccessLog(t *testing.T) {
	e := obsNewEngine()
	e.Use(AccessLog())
	e.GET("/m99/access/ping", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	w := ut.PerformRequest(e, "GET", "/m99/access/ping", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	entries := obsLogEntries(t, "access")
	entry := obsFindEntry(entries, "/m99/access/ping")
	if entry == nil {
		t.Fatalf("access.log 里没有 /m99/access/ping 的条目:\n%s", obsReadLog(t, "access"))
	}
	if entry["method"] != "GET" {
		t.Errorf("method = %v,want \"GET\"", entry["method"])
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v,want 200", entry["status"])
	}
	// logger 字段(= zapcore.Entry.LoggerName)必须叫 access
	// —— 这正是"按名路由到独立文件"的依据
	if entry["logger"] != "access" {
		t.Errorf("logger 字段 = %v,want \"access\"(无名 logger 不会被路由到 access.log)", entry["logger"])
	}
	if entry["msg"] != "access" {
		t.Errorf("msg = %v,want \"access\"", entry["msg"])
	}
}

// TestAccessLog_NotLeakIntoAuditLog access 条目不许串进 audit.log
func TestAccessLog_NotLeakIntoAuditLog(t *testing.T) {
	e := obsNewEngine()
	e.Use(AccessLog())
	e.GET("/m99/access/separate", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "GET", "/m99/access/separate", nil)

	if audit := obsReadLog(t, "audit"); strings.Contains(audit, "/m99/access/separate") {
		t.Errorf("access 条目串进了 audit.log:\n%s", audit)
	}
}

// TestAccessLog_RecordsErrorStatus 出错的请求也要记
func TestAccessLog_RecordsErrorStatus(t *testing.T) {
	e := obsNewEngine()
	e.Use(AccessLog())
	e.GET("/m99/access/boom", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusInternalServerError, "boom")
	})
	ut.PerformRequest(e, "GET", "/m99/access/boom", nil)

	entries := obsLogEntries(t, "access")
	entry := obsFindEntry(entries, "/m99/access/boom")
	if entry == nil {
		t.Fatalf("access.log 里没有 /m99/access/boom 的条目")
	}
	if entry["status"] != float64(http.StatusInternalServerError) {
		t.Errorf("status = %v,want 500", entry["status"])
	}
}
