// notifyx —— 多通道通知分发
//
// ## 这个包解决什么问题
//
// 把"要通知某人"这件事,和"用哪个通道发"拆开:
//
//	调用方只描述 **给谁、说什么**
//	分发器决定 **走哪几条通道**、以及 **某条通道挂了怎么办**
//
// ## 关键点
//
//   - **通知是旁路,永不阻断主流程**。审批单已经落库,不该因为
//     "邮件没发出去"让审批失败
//   - **单通道失败不牵连其它通道**。邮件挂了不影响站内信
//   - **通道的 panic 必须被隔离**。通知是主流程的旁支,
//     一条通知把整个进程带走是不可接受的失败方向
//   - **没有某个联系方式 = skipped,不是 failed**。后台账号常常没填邮箱,
//     那就不该发邮件 —— 但站内信照发
//   - ErrSkip 用 `errors.Is` 判,不要用 `==` —— 通道实现可能在它上面
//     再包一层(带上下文)
//
// ## 跑法
//
//	go run ./examples/notifyx
package main

import (
	"context"
	"errors"
	"fmt"

	"rapid-admin/internal/pkg/notifyx"
)

// logChannel 演示用的通道:把消息打到标准输出
type logChannel struct {
	name string
	fail bool
}

func (c *logChannel) Name() string { return c.name }

func (c *logChannel) Send(ctx context.Context, r notifyx.Recipient, m notifyx.Message) error {
	if c.fail {
		return errors.New("通道故障(演示)")
	}
	// 这个通道只认有邮箱的收件人 —— 没邮箱就跳过,不算失败
	if c.name == "email" && r.Email == "" {
		return fmt.Errorf("%w: 用户 %d 没填邮箱", notifyx.ErrSkip, r.UserID)
	}
	fmt.Printf("    [%s] → 用户 %d: %s\n", c.name, r.UserID, m.Title)
	return nil
}

// panicChannel 演示 panic 隔离:一条通道把整个进程带走是不可接受的
type panicChannel struct{}

func (panicChannel) Name() string { return "panic" }

func (panicChannel) Send(ctx context.Context, r notifyx.Recipient, m notifyx.Message) error {
	panic("通道内部 panic(演示)")
}

func main() {
	ctx := context.Background()

	// ── 一、装配分发器 ──────────────────────────────────────────
	fmt.Println("=== 装配 ===")
	d := notifyx.NewDispatcher(
		&logChannel{name: "email"},
		&logChannel{name: "sms"},
		&logChannel{name: "broken", fail: true},
		panicChannel{},
	)
	fmt.Printf("已注册通道: %v\n", d.Channels())

	// ── 二、正常投递 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 投递(有邮箱) ===")
	res := d.Dispatch(ctx, notifyx.Recipient{
		UserID: 1001,
		Email:  "zhangwei@example.com",
		Phone:  "13800001234",
	}, notifyx.Message{
		Title:   "您有一条新的审批待办",
		Content: "请在 24 小时内处理",
		Link:    "/approval/8801",
		RefType: "approval",
		RefID:   8801,
	})
	fmt.Printf("  成功通道 = %v\n", res.Delivered)
	fmt.Printf("  跳过通道 = %v\n", res.Skipped)
	fmt.Printf("  失败通道 = %v\n", keys(res.Failed))
	fmt.Printf("  OK() = %v\n", res.OK())

	// ── 三、失败隔离 ────────────────────────────────────────────
	//
	// ⚠️ broken 通道失败,**不影响** email / sms 成功。
	// 这就是"单通道失败不牵连其它通道"。
	fmt.Println()
	fmt.Println("=== 失败隔离 ===")
	fmt.Println("  (broken 通道失败 / panic 通道 panic,上面 email 和 sms 依然成功)")
	err := res.Err()
	fmt.Printf("  汇总错误 = %v\n", err)
	fmt.Println("  ⇒ 用 errors.Join 汇总,多个失败都要看得见,只留最后一个是丢信息")

	// ── 四、缺联系方式 = skipped 不是 failed ───────────────────
	fmt.Println()
	fmt.Println("=== 没填邮箱的后台账号 ===")
	res2 := d.Dispatch(ctx, notifyx.Recipient{
		UserID: 2002, // 后台账号,常常没邮箱
	}, notifyx.Message{Title: "系统通知"})
	fmt.Printf("  成功通道 = %v\n", res2.Delivered)
	fmt.Printf("  跳过通道 = %v\n", res2.Skipped)
	fmt.Printf("  失败通道 = %v\n", keys(res2.Failed))
	fmt.Println("  ⇒ skipped ≠ failed:站内信照发,只是这条通道没发")

	// ── 五、注册通道 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 运行时注册通道 ===")
	d.Register(&logChannel{name: "wechat"})
	fmt.Printf("  注册后通道 = %v\n", d.Channels())

	// ── 六、Result.OK 的口径 ───────────────────────────────────
	fmt.Println()
	fmt.Println("=== OK 的口径 ===")
	fmt.Printf("  有成功通道 → OK() = %v\n", res.OK())
	fmt.Printf("  只有 sms 成功 → OK() = %v\n", res2.OK())
	fmt.Println("  ⇒ OK() = 至少有一条通道投递成功")
}

// keys 取失败通道名(为了打印方便)
func keys(m map[string]error) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
