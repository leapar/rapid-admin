package smsaudit

import (
	"context"
	"testing"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/piiaudit"
)

// TestRecord_PicksUpMiddlewareCtxKeys 审计行必须带上**中间件注入的那一套** ctx
//
// # 缺陷:smsaudit 自定义了一套私有 ctx key,与全仓唯一注入方对不上
//
// 全仓只有一处注入 request_id / ip / user_agent:
// `internal/api/middleware/trace.go:92-94`,它用的是 **piiaudit** 的 key:
//
//	ctx = piiaudit.InjectRequestID(ctx, rid)
//	ctx = piiaudit.InjectIP(ctx, c.ClientIP())
//	ctx = piiaudit.InjectUA(ctx, string(c.UserAgent()))
//
// 而本包原来自己定义了 `ctxKeyRequestID{}` / `ctxKeyTraceID{}`
// —— **两套 key 谁也读不到谁** ⇒ `sys_sms_send_log.request_id` /
// `trace_id` 在生产里**恒为空**;ip / user_agent 更是**从来没读过**
// (表里有这两列,兄弟包 cryptaudit 也在读同一套 ctx key)。
//
// 后果:一条短信审计只剩"发了什么",没有"谁发的、哪次请求、从哪台机器来"
// —— 等保场景("谁在什么时间往哪个手机号发了验证码")缺了后两维。
func TestRecord_PicksUpMiddlewareCtxKeys(t *testing.T) {
	db := auditTestDB(t)

	ctx := authz.WithPrincipal(context.Background(), &authz.Principal{
		UserID:     1001,
		IdentityID: 2002,
		RoleCode:   "ops_manager",
	})
	// ⚠️ 刻意用 **piiaudit** 的注入器 —— 这才是中间件实际用的那一套。
	// 如果本包还在读自己的私有 key,这里注入的东西它一条都读不到。
	ctx = piiaudit.InjectRequestID(ctx, "req-ctx-0001")
	ctx = piiaudit.InjectTraceID(ctx, "trace-ctx-0001")
	ctx = piiaudit.InjectIP(ctx, "10.1.2.3")
	ctx = piiaudit.InjectUA(ctx, "Mozilla/5.0 (probe)")

	Record(ctx, SendLog{
		Phone:    "13700001111",
		Scene:    "ctxprobe",
		Provider: "console",
		Success:  true,
	})
	global.flush()

	var row struct {
		RequestID  string
		TraceID    string
		IP         string
		UserAgent  string
		UserID     uint64
		IdentityID uint64
	}
	if err := db.Raw(`SELECT request_id, trace_id, ip, user_agent, user_id, identity_id
		FROM sys_sms_send_log WHERE scene = 'ctxprobe'`).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}

	if row.RequestID != "req-ctx-0001" {
		t.Fatalf("request_id = %q, want %q —— 本包读的 ctx key 和中间件注入的不是一套",
			row.RequestID, "req-ctx-0001")
	}
	if row.TraceID != "trace-ctx-0001" {
		t.Fatalf("trace_id = %q, want %q", row.TraceID, "trace-ctx-0001")
	}
	if row.IP != "10.1.2.3" {
		t.Fatalf("ip = %q, want %q —— 本包从来没读过 ip 这个 ctx key", row.IP, "10.1.2.3")
	}
	if row.UserAgent != "Mozilla/5.0 (probe)" {
		t.Fatalf("user_agent = %q, want %q —— 本包从来没读过 ua 这个 ctx key",
			row.UserAgent, "Mozilla/5.0 (probe)")
	}
	if row.UserID != 1001 || row.IdentityID != 2002 {
		t.Fatalf("principal 没带上: user=%d identity=%d", row.UserID, row.IdentityID)
	}
}

// TestRecord_NilCtx_DoesNotPanic ctx 传 nil 不能把业务请求带崩
//
// 与 piiaudit 的 M100 同款:ctx 为 nil 时 ctx.Value 会 panic,
// 而审计路径是在**后台 goroutine / 旁路**里跑的,panic = 进程退出。
func TestRecord_NilCtx_DoesNotPanic(t *testing.T) {
	auditTestDB(t) // 确保 global 已就绪

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Record(nil, ...) panic 了: %v", r)
		}
	}()
	//nolint:staticcheck // 故意传 nil ctx
	Record(nil, SendLog{Phone: "13700002222", Scene: "nilctx", Provider: "console", Success: true})
	// 本包所有用例共用同一个全局 recorder 和同一个攒批缓冲 —— 本用例记的行
	// 必须自己 flush 掉,否则会占掉 batch 的位置,让后面按 100 条触发的
	// flush 边界整体前移(别的用例就会少落几行)。
	global.flush()
}
