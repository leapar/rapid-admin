// cryptaudit —— 密钥使用审计
//
// ## 这个包解决什么问题
//
// "这个字段是用哪把密钥加密的""上周那次解密失败到底是哪把 key"——
// 合规审计要能回答这类问题。业务代码解密/加密成功后调一行 `Record(...)`,
// 剩下的(攒批、落库、重试)框架负责。
//
// ## 关键点
//
//   - **异步攒批**:100 条 / 5s 落一次库,业务请求不阻塞
//   - **失败也落库**。`Success=false` + `ErrorMsg` 是排查"哪个 key 解不开"
//     的直接线索(直接 grep error_msg)
//   - **主键必须自己填**(雪花)。`BaseModel.ID` 的 tag 是
//     `primaryKey;autoIncrement:false` —— GORM 不会让库自增,零值 0 会
//     原样入库 ⇒ id 撞主键 ⇒ 第二批往后全部插不进去(重试 3 次后丢弃)
//     ⇒ **这张表永远只有 1 行**
//   - **error_msg 按 rune 截断,不是按字节**。按字节切会把一个中文错误信息
//     从中间劈开,落库就是"半个汉字 + 乱码尾巴",排障时第一反应会变成
//     "程序写错了"
//   - **ctx 里的 principal / request_id / trace_id / ip / ua 自动带上** ——
//     这些是中间件注入的,审计记录不用业务代码自己拼
//   - **db 为 nil 时 Init 打一条 Warn**(不是静默关掉)。密钥使用审计是
//     合规项,"静默关掉"必须留痕
//
// ## 表从哪来
//
// 生产上表由 GORM AutoMigrate 建。示例里手写 DDL,是因为框架示例
// **不许 import internal/model**(那是业务模型,开源时不在范围内)。
//
// ## 跑法
//
//	go run ./examples/cryptaudit
package main

import (
	"context"
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/cryptaudit"
	"rapid-admin/internal/pkg/piiaudit"
)

// cryptoAuditDDL 密钥使用审计表(生产上由 AutoMigrate 建)
const cryptoAuditDDL = `
CREATE TABLE IF NOT EXISTS sys_crypto_audit_log (
	id            INTEGER PRIMARY KEY,
	created_at    DATETIME,
	updated_at    DATETIME,
	deleted_at    DATETIME,
	key_id        TEXT,
	key_version   INTEGER,
	algorithm     TEXT,
	operation     TEXT,
	resource_type TEXT,
	resource_id   INTEGER,
	byte_count    INTEGER,
	success       BOOLEAN,
	error_msg     TEXT,
	ip            TEXT,
	user_agent    TEXT,
	request_id    TEXT,
	trace_id      TEXT,
	user_id       INTEGER,
	identity_id   INTEGER,
	role_code     TEXT
)`

func main() {
	ctx := context.Background()

	// ── 一、装配 ────────────────────────────────────────────────
	fmt.Println("=== 装配 ===")
	db, err := gorm.Open(sqlite.Open("file:cryptaudit_demo?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		fmt.Println("开库失败:", err)
		return
	}
	if err := db.Exec(cryptoAuditDDL).Error; err != nil {
		fmt.Println("建表失败:", err)
		return
	}
	cryptaudit.Init(db)
	// 退出前必须 Shutdown —— 它会 flush 最后一批(攒批默认 5s 一轮)
	defer cryptaudit.Shutdown()
	fmt.Println("cryptaudit 已启动(异步攒批:100 条 / 5s)")

	// ── 二、准备一个"像样的"请求上下文 ──────────────────────────
	//
	// 这些值在真实服务里由中间件注入:
	//   principal ← principal middleware
	//   request_id / trace_id / ip / ua ← access_log / otel middleware
	fmt.Println()
	fmt.Println("=== 装配请求上下文 ===")
	ctx = authz.WithPrincipal(ctx, &authz.Principal{
		UserID:     8801,
		IdentityID: 8801,
		RoleCode:   "super_admin",
	})
	ctx = piiaudit.InjectRequestID(ctx, "req-20261101-0001")
	ctx = piiaudit.InjectTraceID(ctx, "trace-abcdef123456")
	ctx = piiaudit.InjectIP(ctx, "10.0.0.8")
	ctx = piiaudit.InjectUA(ctx, "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	fmt.Println("  principal / request_id / trace_id / ip / ua 都塞进 ctx 了")
	fmt.Println("  ⇒ 审计记录不用业务代码自己拼这些字段 —— 框架从 ctx 里提")

	// ── 三、记录 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 记录 ===")
	cryptaudit.Record(ctx, cryptaudit.Event{
		KeyID:        "KEY_PII_V1",
		KeyVersion:   1,
		Algorithm:    "aes-256-gcm",
		Operation:    "decrypt",
		ResourceType: "owner",
		ResourceID:   20260301,
		ByteCount:    11,
		Success:      true,
	})
	fmt.Println("  记了一条成功的解密")

	cryptaudit.Record(ctx, cryptaudit.Event{
		KeyID:        "KEY_PII_V1",
		KeyVersion:   1,
		Algorithm:    "aes-256-gcm",
		Operation:    "decrypt",
		ResourceType: "owner",
		ResourceID:   20260302,
		ByteCount:    0,
		Success:      false,
		ErrorMsg:     "cipher: message authentication failed",
	})
	fmt.Println("  记了一条失败的解密")
	fmt.Println("  ⇒ 失败也要落库 —— 「哪个 key 解不开」的直接线索就是 error_msg")

	// ── 四、flush + 回查 ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 回查 ===")
	// Shutdown 会 flush 最后一批;这里手动等一下 flush 周期
	cryptaudit.Shutdown()

	type row struct {
		KeyID        string
		KeyVersion   int
		Algorithm    string
		Operation    string
		ResourceType string
		ResourceID   uint64
		ByteCount    int
		Success      bool
		ErrorMsg     string
		RequestID    string
		TraceID      string
		UserID       uint64
		RoleCode     string
		IP           string
		UserAgent    string
	}
	var rows []row
	if err := db.Raw(`SELECT key_id, key_version, algorithm, operation, resource_type,
		resource_id, byte_count, success, error_msg, request_id, trace_id,
		user_id, role_code, ip, user_agent
		FROM sys_crypto_audit_log ORDER BY id`).Scan(&rows).Error; err != nil {
		fmt.Println("查询失败:", err)
		return
	}
	fmt.Printf("  表里 %d 行\n", len(rows))
	for _, r := range rows {
		fmt.Printf("    %s v%d %s %s %s/%d ok=%v bytes=%d\n",
			r.KeyID, r.KeyVersion, r.Algorithm, r.Operation, r.ResourceType, r.ResourceID, r.Success, r.ByteCount)
		if r.ErrorMsg != "" {
			fmt.Printf("      error_msg = %q\n", r.ErrorMsg)
		}
		fmt.Printf("      ctx 带出来的:user=%d role=%s request_id=%s trace_id=%s ip=%s\n",
			r.UserID, r.RoleCode, r.RequestID, r.TraceID, r.IP)
		fmt.Printf("      ua = %q\n", r.UserAgent)
	}
	fmt.Println("  ⇒ 主键是应用侧雪花 —— 每行的 id 都不一样,批量插入不会撞主键")
}
