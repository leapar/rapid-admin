// piiaudit —— PII 访问审计
//
// ## 这个包解决什么问题
//
// 等保 P0 项:"谁在什么时候看了业主的手机号"。
//
// 任何地方解了 PII(手机号 / 身份证 / 银行卡 / 住址)就调一行
// `piiaudit.Record(ctx, ...)`,剩下的框架负责。
//
// ## 关键点
//
//   - **异步攒批**:100 条 / 5s 落一次库,业务请求不阻塞
//   - **ctx 里的东西自动带上**:principal(谁)、request_id / trace_id(哪次请求)、
//     ip / user_agent(从哪台机器来)。业务代码不用自己拼
//   - **主键必须自己填**(雪花)。`BaseModel.ID` 的 tag 是
//     `primaryKey;autoIncrement:false` —— GORM 不会让库自增,零值 0 原样入库
//     ⇒ id 撞主键 ⇒ 第二批往后全部插不进去(重试 3 次后整批丢弃)
//     ⇒ **这张表永远只能存下 1 行** —— 而它是 P0 合规项:
//     "表在、行有,但查不出东西"
//   - **Action 为空默认 read**;列表/导出场景用 `RecordMany` ——
//     **每行算 1 条**(产品意图)
//   - **db 为 nil 时 Init 打一条 Warn**(不是静默关掉)
//   - **ctx 为 nil 不 panic**(审计路径不该把业务请求带崩)
//
// ## 字段名从哪来
//
// action / field 的取值是字符串常量(read / list / export / decrypt,
// phone / id_card / email / bank_card / address / name),
// 常量本身在 internal/model 里 —— 框架示例**不许 import 业务模型**,
// 所以这里写字面量。真实业务代码请用 model.PIIActionRead 这类常量。
//
// ## 表从哪来
//
// 生产上表由 GORM AutoMigrate 建。示例里手写 DDL,原因同上。
//
// ## 跑法
//
//	go run ./examples/piiaudit
package main

import (
	"context"
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/piiaudit"
)

// piiAuditDDL PII 访问审计表(生产上由 AutoMigrate 建)
const piiAuditDDL = `
CREATE TABLE IF NOT EXISTS sys_pii_access_log (
	id           INTEGER PRIMARY KEY,
	created_at   DATETIME,
	updated_at   DATETIME,
	deleted_at   DATETIME,
	user_id      INTEGER,
	identity_id  INTEGER,
	role_code    TEXT,
	action       TEXT,
	resource_type TEXT,
	resource_id  INTEGER,
	field        TEXT,
	ip           TEXT,
	user_agent   TEXT,
	request_id   TEXT,
	trace_id     TEXT,
	reason       TEXT
)`

func main() {
	ctx := context.Background()

	// ── 一、装配 ────────────────────────────────────────────────
	fmt.Println("=== 装配 ===")
	db, err := gorm.Open(sqlite.Open("file:piiaudit_demo?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		fmt.Println("开库失败:", err)
		return
	}
	if err := db.Exec(piiAuditDDL).Error; err != nil {
		fmt.Println("建表失败:", err)
		return
	}
	piiaudit.Init(db)
	defer piiaudit.Shutdown()
	fmt.Println("piiaudit 已启动(异步攒批:100 条 / 5s)")

	// ── 二、请求上下文 ──────────────────────────────────────────
	ctx = authz.WithPrincipal(ctx, &authz.Principal{
		UserID:     8801,
		IdentityID: 8801,
		RoleCode:   "ops_manager",
	})
	ctx = piiaudit.InjectRequestID(ctx, "req-20261101-0002")
	ctx = piiaudit.InjectTraceID(ctx, "trace-123456abcdef")
	ctx = piiaudit.InjectIP(ctx, "10.0.0.8")
	ctx = piiaudit.InjectUA(ctx, "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	fmt.Println("  principal / request_id / trace_id / ip / ua 都塞进 ctx 了")

	// ── 三、记录 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 记录 ===")
	// 单条查看
	piiaudit.Record(ctx, piiaudit.Access{
		Action:       "read",
		ResourceType: "owner",
		ResourceID:   20260301,
		Field:        "phone",
		Reason:       "业主来电查询",
	})
	fmt.Println("  记了一条:查看业主手机号")

	// 导出(批量)—— RecordMany 每行算 1 条
	piiaudit.RecordMany(ctx, []piiaudit.Access{
		{Action: "export", ResourceType: "owner", ResourceID: 20260302, Field: "id_card", Reason: "报建材料"},
		{Action: "export", ResourceType: "owner", ResourceID: 20260303, Field: "id_card", Reason: "报建材料"},
		{Action: "export", ResourceType: "owner", ResourceID: 20260304, Field: "id_card", Reason: "报建材料"},
	})
	fmt.Println("  批量记了 3 条:导出身份证")

	// Action 留空 ⇒ 默认 read
	piiaudit.Record(ctx, piiaudit.Access{
		ResourceType: "owner",
		ResourceID:   20260305,
		Field:        "email",
	})
	fmt.Println("  记了一条 Action 为空的(默认 read)")

	// ctx 为 nil 不该 panic
	piiaudit.Record(nil, piiaudit.Access{
		Action: "decrypt", ResourceType: "owner", ResourceID: 20260306, Field: "bank_card",
	})
	fmt.Println("  ctx 传 nil 也没炸(审计路径不该把业务请求带崩)")

	// ── 四、flush + 回查 ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 回查 ===")
	piiaudit.Shutdown() // flush 最后一批

	type row struct {
		ID           uint64
		UserID       uint64
		RoleCode     string
		Action       string
		ResourceType string
		ResourceID   uint64
		Field        string
		Reason       string
		RequestID    string
		TraceID      string
		IP           string
		UserAgent    string
	}
	var rows []row
	if err := db.Raw(`SELECT id, user_id, role_code, action, resource_type, resource_id,
		field, reason, request_id, trace_id, ip, user_agent
		FROM sys_pii_access_log ORDER BY id`).Scan(&rows).Error; err != nil {
		fmt.Println("查询失败:", err)
		return
	}
	fmt.Printf("  表里 %d 行\n", len(rows))
	for _, r := range rows {
		fmt.Printf("    %s %s/%d 字段=%s 事由=%q\n", r.Action, r.ResourceType, r.ResourceID, r.Field, r.Reason)
		fmt.Printf("      id=%d user=%d role=%s request_id=%s ip=%s\n",
			r.ID, r.UserID, r.RoleCode, r.RequestID, r.IP)
	}
	fmt.Println("  ⇒ 每行 id 都不同(应用侧雪花)—— 批量插入不会撞主键")
	fmt.Println("  ⇒ 「谁在什么时候看了手机号」= user_id + created_at + field 三列")
}
