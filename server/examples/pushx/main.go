// pushx —— App 推送(抽象 Provider)
//
// ## 这个包解决什么问题
//
// 统一 App Push 通道(通知 / 自定义消息),支持多端:
// Android / iOS / 鸿蒙 / Web。
//
// ## 关键点
//
//   - **Push 返回 nil = 推送平台已接受**,不代表用户已收到。
//     到达率由推送平台决定 —— 别把它当"送达成功"
//   - **多 target**:单 device / 单用户多个 device / 群发
//   - **通道重要**:iOS APNs / Android 厂商通道(华为/小米/OPPO/vivo)/
//     系统通道,到达率差别很大
//   - **静默推送(Silent)不校验 title / body** —— 静默消息只带 Data,
//     用来在后台同步数据,不该弹通知
//   - PushError 带 provider / code / message 三段,排障时能直接定位到
//     是平台拒了还是参数错了
//
// ## 跑法
//
//	go run ./examples/pushx
package main

import (
	"context"
	"fmt"

	"rapid-admin/internal/pkg/pushx"
)

func main() {
	ctx := context.Background()

	// ── 一、校验 ────────────────────────────────────────────────
	fmt.Println("=== 校验 ===")
	cases := []pushx.Notification{
		{
			Title: "新消息",
			Body:  "您有一条新的报修工单",
			Targets: []pushx.Target{
				{Type: "account", Value: "user_1001"},
			},
		},
		{
			// 缺 title
			Body:    "只有正文",
			Targets: []pushx.Target{{Type: "account", Value: "user_1001"}},
		},
		{
			// 缺 targets
			Title: "新消息",
			Body:  "有正文",
		},
		{
			// 静默推送:不校验 title / body,但仍然要 targets
			Silent: true,
			Targets: []pushx.Target{
				{Type: "device", Value: "dev_abc123"},
			},
			Data: map[string]string{"action": "sync"},
		},
	}
	for i, n := range cases {
		err := n.IsValid()
		if err != nil {
			fmt.Printf("  第 %d 例 → 不合法: %v\n", i+1, err)
			continue
		}
		fmt.Printf("  第 %d 例 → 合法\n", i+1)
	}

	// ── 二、Mock sender ─────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== Mock sender ===")
	mock := pushx.NewMock()
	fmt.Printf("provider = %s\n", mock.Provider())

	n := pushx.Notification{
		Title:    "新消息",
		Body:     "您有一条新的报修工单",
		SubTitle: "RapidAdmin",
		Badge:    3,
		Targets: []pushx.Target{
			{Type: "account", Value: "user_1001"},
			{Type: "device", Value: "dev_abc123"},
		},
		Data: map[string]string{"biz": "repair", "id": "8801"},
	}
	if err := mock.Push(ctx, n); err != nil {
		fmt.Println("推送失败:", err)
		return
	}
	fmt.Printf("已推送 %d 条\n", mock.Count())

	last, ok := mock.Last()
	if ok {
		fmt.Printf("最后一条: title=%q badge=%d targets=%d data=%v\n",
			last.Title, last.Badge, len(last.Targets), last.Data)
	}

	mock.Reset()
	fmt.Printf("Reset 后已推送 %d 条\n", mock.Count())

	// ── 三、Console sender ─────────────────────────────────────
	fmt.Println()
	fmt.Println("=== Console sender ===")
	cs := pushx.NewConsolePush()
	fmt.Printf("provider = %s\n", cs.Provider())
	if err := cs.Push(ctx, pushx.Notification{
		Title:   "开发环境推送",
		Body:    "这条只打到控制台,不真发",
		Targets: []pushx.Target{{Type: "account", Value: "user_1001"}},
	}); err != nil {
		fmt.Println("推送失败:", err)
		return
	}
	fmt.Printf("输出:\n%s\n", cs.Output())

	// ── 四、PushError ──────────────────────────────────────────
	fmt.Println("=== PushError ===")
	fmt.Printf("格式 = %q\n", (&pushx.PushError{
		Provider: "aliyun-emns",
		Code:     "isv.INVALID_PARAMETERS",
		Message:  "device token 不合法",
	}).Error())
	fmt.Println("⇒ 三段式:哪一家拒的 / 什么码 / 什么话,排障时不用去猜")
}
