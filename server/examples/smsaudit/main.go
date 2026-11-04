// smsaudit —— 短信发送审计
//
// ## 这个包解决什么问题
//
// "这条验证码到底发出去了没有""用户说没收到短信,到底是服务商拒了还是
// 我们根本没发"—— 每次发送都要留痕:手机号、场景、验证码、provider、
// 成没成、耗时多少、阿里云那边的 request_id。
//
// ## 关键点
//
//   - **推荐用装饰器接入**:`smsaudit.Wrap(realSender)` 包一层,发送结果
//     自动落审计(耗时也是装饰器量的),业务代码一行不用改
//   - **装饰器不吞错误** —— 底层 Send 返什么错,装饰器原样往上抛,
//     审计只是旁路
//   - **异步攒批**:100 条 / 5s 落一次库,业务请求不阻塞;
//     写审计失败不影响主流程
//   - **db 为 nil 时 Init 打一条 Warn**(不是静默关掉)
//   - **ctx 里的 principal / request_id / trace_id / ip / ua 自动带上**
//
// ## 表从哪来
//
// 生产上表由 GORM AutoMigrate 建。示例里手写 DDL,是因为框架示例
// **不许 import internal/model**(那是业务模型,开源时不在范围内)。
//
// ## 跑法
//
//	go run ./examples/smsaudit
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/msn"
	"rapid-admin/internal/pkg/piiaudit"
	"rapid-admin/internal/pkg/smsaudit"
)

// smsAuditDDL 短信发送审计表(生产上由 AutoMigrate 建)
const smsAuditDDL = `
CREATE TABLE IF NOT EXISTS sys_sms_send_log (
	id               INTEGER PRIMARY KEY,
	created_at       DATETIME,
	updated_at       DATETIME,
	deleted_at       DATETIME,
	phone            TEXT,
	scene            TEXT,
	code             TEXT,
	provider         TEXT,
	success          BOOLEAN,
	error_msg        TEXT,
	cost_ms          INTEGER,
	upstream_req_id  TEXT,
	ip               TEXT,
	user_agent       TEXT,
	request_id       TEXT,
	trace_id         TEXT,
	user_id          INTEGER,
	identity_id      INTEGER
)`

func main() {
	ctx := context.Background()

	// ── 一、装配 ────────────────────────────────────────────────
	fmt.Println("=== 装配 ===")
	db, err := gorm.Open(sqlite.Open("file:smsaudit_demo?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		fmt.Println("开库失败:", err)
		return
	}
	if err := db.Exec(smsAuditDDL).Error; err != nil {
		fmt.Println("建表失败:", err)
		return
	}
	smsaudit.Init(db)
	defer smsaudit.Shutdown()
	fmt.Println("smsaudit 已启动(异步攒批:100 条 / 5s)")

	ctx = authz.WithPrincipal(ctx, &authz.Principal{
		UserID: 8801, IdentityID: 8801, RoleCode: "ops_manager",
	})
	ctx = piiaudit.InjectRequestID(ctx, "req-20261101-0003")
	ctx = piiaudit.InjectTraceID(ctx, "trace-654321fedcba")
	ctx = piiaudit.InjectIP(ctx, "10.0.0.8")
	ctx = piiaudit.InjectUA(ctx, "Mozilla/5.0")

	// ── 二、装饰器 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 装饰器接入 ===")
	//
	// 真实 sender 由 msn.NewDynamicSender / aliyun.NewSender 提供,
	// 这里用一个假的演示"成功 / 失败"两条路径。
	real := &fakeSender{provider: "aliyun", cost: 234}
	var sender msn.Sender = smsaudit.Wrap(real)
	fmt.Printf("  装饰后的 Provider() = %s\n", sender.Provider())
	fmt.Println("  ⇒ 装饰器把 sender 包了一层,业务侧还是只认 msn.Sender 接口")

	// ── 三、发一条 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 发送 ===")
	err = sender.Send(ctx, "13800001234", "123456", "login")
	fmt.Printf("  发送成功 → err = %v\n", err)

	// ── 四、发一条失败的 ────────────────────────────────────────
	real.fail = errors.New("isv.SMS_TEMPLATE_ILLEGAL")
	err = sender.Send(ctx, "13800001234", "123456", "reset_password")
	fmt.Printf("  发送失败 → err = %v\n", err)
	fmt.Println("  ⇒ 装饰器**不吞错误** —— 底层返什么错就往上抛什么错")
	fmt.Println("    审计只是旁路,不是替业务做决定的")
	real.fail = nil

	// ── 五、手工记一条 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 手工记录 ===")
	smsaudit.Record(ctx, smsaudit.SendLog{
		Phone:    "13900002222",
		Scene:    "change_phone",
		Code:     "654321",
		Provider: "console",
		Success:  true,
		CostMs:   12,
	})
	fmt.Println("  手工记了一条(不走装饰器时的用法)")

	// ── 六、flush + 回查 ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 回查 ===")
	smsaudit.Shutdown() // flush 最后一批

	type row struct {
		ID            uint64
		Phone         string
		Scene         string
		Code          string
		Provider      string
		Success       bool
		ErrorMsg      string
		CostMs        int
		UpstreamReqID string
		RequestID     string
		UserID        uint64
		IP            string
	}
	var rows []row
	if err := db.Raw(`SELECT id, phone, scene, code, provider, success, error_msg,
		cost_ms, upstream_req_id, request_id, user_id, ip
		FROM sys_sms_send_log ORDER BY id`).Scan(&rows).Error; err != nil {
		fmt.Println("查询失败:", err)
		return
	}
	fmt.Printf("  表里 %d 行\n", len(rows))
	for _, r := range rows {
		fmt.Printf("    %s scene=%s provider=%s ok=%v cost=%dms code=%q\n",
			maskPhone(r.Phone), r.Scene, r.Provider, r.Success, r.CostMs, r.Code)
		if r.ErrorMsg != "" {
			fmt.Printf("      error_msg = %q\n", r.ErrorMsg)
		}
		fmt.Printf("      request_id=%s user=%d ip=%s\n", r.RequestID, r.UserID, r.IP)
	}
	fmt.Println("  ⇒ 验证码(code)也落库 —— 用户报「收到的不是这个」时能反查")
	fmt.Println("  ⇒ user_id / request_id / ip 都是从 ctx 自动带上的")
}

// fakeSender 假的服务商 sender(真实的是 aliyun / console)
type fakeSender struct {
	provider string
	cost     int
	fail     error
}

func (f *fakeSender) Send(ctx context.Context, phone, code, scene string) error {
	time.Sleep(time.Duration(f.cost) * time.Millisecond)
	return f.fail
}

func (f *fakeSender) Provider() string { return f.provider }

// maskPhone 手机号打码(日志/示例里不该出现完整手机号)
func maskPhone(phone string) string {
	if len(phone) < 7 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}
