// audit 中间件单测 —— §1.8(M99)
//
// # M99 在这里实证出的 1 个真 bug
//
//	**audit.log 也没有写入方**
//
//	`pkg/logger` 从一开始就有 audit.log 这个独立通道(`logger.Audit()`),
//	但全仓**零调用方** —— 通道建好了没人往里写,
//	文件连创建都不会发生(rotator 是懒创建的)。
//
//	修法:中间件在写操作之后补上文件轨。
//
// # 设计要点:文件轨**不依赖** AuditSvc
//
//	DB 审计服务(sys_operation_log)没装配时,文件轨照样有 ——
//
// 否则"审计服务没挂"就等于"没有审计"。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                              必须变红
//	──────────────────────────────────────────  ───────────────────────────────────
//	P2 audit 写入挪到 AuditSvc == nil 之后    TestAudit_FileTrackIndependentOfAuditSvc
//	P2b audit 整段删掉                        TestAudit_WritesToAuditLog
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"rapid-admin/internal/service"
)

// obsWithCaller 装一个注入 Caller 的中间件
//
// Audit 中间件从 ctx 里取 caller(`service.FromContext`),
// 而 ctx 是上一个中间件传下来的 —— 所以注入必须排在 Audit **之前**。
func obsWithCaller(caller *service.Caller) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Next(service.WithCaller(ctx, caller))
	}
}

// TestAudit_WritesToAuditLog 写操作必须落 audit.log
//
// # M99 修的那个 bug
//
// 旧实现只有 DB 轨(sys_operation_log),文件轨没有写入方 →
// audit.log 永远是空的(甚至不存在)。
//
// ⚠️ 断言要点:必须查 **audit.log**,不能查 info.log ——
// 审计条目同时也会进 info.log(tee),查 info.log 对旧实现是绿的(假绿)。
func TestAudit_WritesToAuditLog(t *testing.T) {
	old := AuditSvc
	AuditSvc = nil
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{
		UserID:   42,
		Username: "alice",
		RoleCode: "admin",
	}))
	e.Use(Audit())
	e.POST("/m99/audit/create", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	w := ut.PerformRequest(e, "POST", "/m99/audit/create", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	entries := obsLogEntries(t, "audit")
	entry := obsFindEntryByField(entries, "module", "/m99/audit/create")
	if entry == nil {
		t.Fatalf("audit.log 里没有 /m99/audit/create 的条目:\n%s", obsReadLog(t, "audit"))
	}
	if entry["action"] != "POST" {
		t.Errorf("action = %v,want \"POST\"", entry["action"])
	}
	if entry["user_id"] != float64(42) {
		t.Errorf("user_id = %v,want 42", entry["user_id"])
	}
	if entry["username"] != "alice" {
		t.Errorf("username = %v,want \"alice\"", entry["username"])
	}
	if entry["role"] != "admin" {
		t.Errorf("role = %v,want \"admin\"", entry["role"])
	}
	if entry["status_code"] != float64(http.StatusOK) {
		t.Errorf("status_code = %v,want 200", entry["status_code"])
	}
	if entry["logger"] != "audit" {
		t.Errorf("logger 字段 = %v,want \"audit\"", entry["logger"])
	}
}

// TestAudit_SkipsReadMethods 读操作不该产生审计条目
//
// 审计只记写操作 —— GET/HEAD/OPTIONS 直接返回。
func TestAudit_SkipsReadMethods(t *testing.T) {
	old := AuditSvc
	AuditSvc = nil
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.GET("/m99/audit/read", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "GET", "/m99/audit/read", nil)

	for _, entry := range obsLogEntries(t, "audit") {
		if m, _ := entry["module"].(string); m == "/m99/audit/read" {
			t.Errorf("GET 不该产生审计条目,却有一条:\n%v", entry)
		}
	}
}

// TestAudit_FileTrackIndependentOfAuditSvc DB 审计没装配时文件轨照样写
//
// # 设计要点的回归守卫
//
// AuditSvc == nil(没装 DB 审计服务)时,旧实现的顺序是
// "先判 AuditSvc == nil 就 return" → 文件轨(如果放在后面)就永远轮不到。
//
// 现在文件轨写在 return **之前**,所以:
// "审计服务没挂" ≠ "没有审计"。
func TestAudit_FileTrackIndependentOfAuditSvc(t *testing.T) {
	old := AuditSvc
	AuditSvc = nil
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 7, Username: "bob", RoleCode: "ops"}))
	e.Use(Audit())
	e.DELETE("/m99/audit/remove", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "DELETE", "/m99/audit/remove", nil)

	entries := obsLogEntries(t, "audit")
	entry := obsFindEntryByField(entries, "module", "/m99/audit/remove")
	if entry == nil {
		t.Fatalf("AuditSvc 为 nil 时 audit.log 仍然没有条目 —— 文件轨被 DB 轨挡住了")
	}
	if entry["action"] != "DELETE" {
		t.Errorf("action = %v,want \"DELETE\"", entry["action"])
	}
}

// TestAudit_NoCaller_StillRecords 没有登录身份也要记
//
// 匿名请求(比如登录失败)同样要留痕,只是 user_id = 0。
func TestAudit_NoCaller_StillRecords(t *testing.T) {
	old := AuditSvc
	AuditSvc = nil
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(Audit())
	e.POST("/m99/audit/anon", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusUnauthorized, "unauthorized")
	})
	ut.PerformRequest(e, "POST", "/m99/audit/anon", nil)

	entries := obsLogEntries(t, "audit")
	entry := obsFindEntryByField(entries, "module", "/m99/audit/anon")
	if entry == nil {
		t.Fatalf("匿名写操作没有审计条目")
	}
	if entry["user_id"] != float64(0) {
		t.Errorf("user_id = %v,want 0", entry["user_id"])
	}
	if entry["status_code"] != float64(http.StatusUnauthorized) {
		t.Errorf("status_code = %v,want 401", entry["status_code"])
	}
}
