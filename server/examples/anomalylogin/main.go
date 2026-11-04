// anomalylogin —— 异常登录检测
//
// ## 这个包解决什么问题
//
// 同一用户多端 / 异地同时登录时,识别可疑会话。
//
// 本包只做**检测 + 通知**,具体写库 / 告警由注入的 Notifier 决定 ——
// 与登录 service 完全解耦。
//
// ## 典型场景
//
//  1. 用户 A 在北京登录(sess1 仍 active)
//  2. 1 分钟后用户 A 在上海再次登录
//  3. Detector.Detect() 返 Anomaly{Type: ConcurrentDifferentIP, ...}
//  4. Notifier.Notify() 调 SMS / 邮件 / 系统通知 / 强制登出(由注入决定)
//
// ## 关键点
//
//   - Detector 是**纯函数、无状态**、线程安全:历史由 service 查库传入,
//     本包不读 DB
//   - 严重度分三级:Low / Medium / High
//   - 规则 3(不可能旅行)**不受 MaxConcurrent 门控** ——
//     它判时间差,不判数量
//   - 默认 Notifier 走 zap 的 WARN 级别:异常登录是**安全事件**,不是调试信息。
//     但它**不阻断登录** —— 拦住人的是别的机制
//
// ## 跑法
//
//	go run ./examples/anomalylogin
package main

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/anomalylogin"
)

func main() {
	ctx := context.Background()

	// ── 一、构造检测器 ──────────────────────────────────────────
	//
	// 默认:最多 3 个并发会话,不开"不可能旅行"。
	d := anomalylogin.New().
		WithMaxConcurrent(2).
		WithImpossibleTravelWindow(30 * time.Minute)

	// ── 二、构造历史会话 ────────────────────────────────────────
	now := time.Now()
	loggedOut := now.Add(-2 * time.Hour)
	history := []anomalylogin.LoginInfo{
		{
			SessionID: "sess-0", UserID: 1001,
			IP: "8.8.8.8", Device: "Chrome/Windows",
			LoginAt:  now.Add(-3 * time.Hour),
			LogoutAt: &loggedOut, // 已登出 ⇒ 不算活跃会话
		},
		{
			SessionID: "sess-1", UserID: 1001,
			IP: "1.2.3.4", Device: "Chrome/Windows",
			LoginAt: now.Add(-30 * time.Minute),
			// LogoutAt 为 nil = 仍活跃
		},
		{
			SessionID: "sess-2", UserID: 1001,
			IP: "1.2.3.4", Device: "Chrome/Windows",
			LoginAt: now.Add(-20 * time.Minute),
		},
		{
			SessionID: "sess-3", UserID: 1001,
			IP: "1.2.3.4", Device: "Chrome/Windows",
			LoginAt: now.Add(-10 * time.Minute),
		},
	}

	current := anomalylogin.LoginInfo{
		SessionID: "sess-4", UserID: 1001,
		IP: "5.6.7.8", Device: "微信/iOS", // 不同 IP、不同设备
		LoginAt: now,
	}

	// ── 三、检测 ────────────────────────────────────────────────
	anomalies := d.Detect(current, history)
	fmt.Printf("检出 %d 个异常\n", len(anomalies))
	for _, a := range anomalies {
		fmt.Printf("  类型=%-28s 严重度=%-6s %s\n", a.Type, a.Severity, a.Reason)
		fmt.Printf("    当前: %s @ %s\n", a.Current.IP, a.Current.Device)
		fmt.Printf("    冲突: %s @ %s\n", a.Existing.IP, a.Existing.Device)
	}

	// ── 四、通知 ────────────────────────────────────────────────
	//
	// 默认通知器:记一条 WARN 日志 + 调 Hook。
	// ⚠️ 业务可以在这里注入额外副作用(发短信 / 站内信 / 吊销旧 jti),
	// 但**日志与副作用都要** —— 别用 Hook 取代日志。
	fmt.Println()
	fmt.Println("=== 通知 ===")
	notifier := &anomalylogin.LogNotifier{
		Hook: func(ctx context.Context, a anomalylogin.Anomaly) {
			fmt.Printf("  [Hook] 用户 %d 触发 %s(严重度 %s)\n", a.UserID, a.Type, a.Severity)
		},
	}
	for _, a := range anomalies {
		notifier.Notify(ctx, a)
	}

	// ── 五、没有异常时什么都不做 ────────────────────────────────
	fmt.Println()
	normal := anomalylogin.LoginInfo{
		SessionID: "sess-5", UserID: 2002,
		IP: "9.9.9.9", Device: "Safari/macOS",
		LoginAt: now,
	}
	fmt.Printf("正常登录检出 %d 个异常\n", len(d.Detect(normal, nil)))
}
