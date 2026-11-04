// Package anomalylogin 异常登录检测原语(§1.1 #11 P2)
//
// 目的:
//   - 同一 user 多端/异地同时登录时,识别可疑会话
//   - 与登录 service 解耦:本包只做"检测 + 通知",具体写库/告警由 notifier 注入
//
// 典型场景:
//  1. 用户 A 在北京 IP 登录(sess1 仍 active,logout_at=nil)
//  2. 1 分钟后用户 A 在上海 IP 再次登录
//  3. Detector.Detect() → 返 Anomaly{Type: "ConcurrentDifferentIP", ...}
//  4. Notifier.Notify() 调 SMS / 邮件 / 系统通知 / 强制登出(由注入决定)
//
// 设计原则:
//   - 纯函数 Detector,无状态(线程安全)
//   - Notifier interface 注入(默认 logger.Notify,业务可换 SMS/邮件/强制登出)
//   - 异常按"严重度"分级:Low / Medium / High
package anomalylogin

import (
	"context"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// Severity 异常严重度
type Severity int

const (
	Low Severity = iota
	Medium
	High
)

func (s Severity) String() string {
	switch s {
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	default:
		return "unknown"
	}
}

// AnomalyType 异常类型
type AnomalyType string

const (
	ConcurrentDifferentIP     AnomalyType = "concurrent_different_ip"     // 同时活跃 + IP 不同
	ConcurrentDifferentDevice AnomalyType = "concurrent_different_device" // 同时活跃 + Device 不同
	ImpossibleTravel          AnomalyType = "impossible_travel"           // 短时间不同 IP(可选,需要 IP 地理库)
)

// LoginInfo 一次登录的最少信息(由 service 层填,Detector 不读 DB)
type LoginInfo struct {
	SessionID string // 唯一会话 id(可用 jti)
	UserID    uint64
	IP        string
	Device    string // User-Agent 截断
	LoginAt   time.Time
	LogoutAt  *time.Time // nil = 仍活跃
}

// Anomaly 一次检测出的异常
type Anomaly struct {
	Type       AnomalyType
	Severity   Severity
	UserID     uint64
	Current    LoginInfo // 本次登录(新发生)
	Existing   LoginInfo // 已存在的活跃登录(冲突源)
	Reason     string    // 给人看的解释
	DetectedAt time.Time
}

// Detector 检测器配置
type Detector struct {
	// MaxConcurrent 同一 user 允许的同时活跃会话数(> 触发异常)
	//   默认 3(超过 3 端同时登录视为异常)
	MaxConcurrent int
	// ImpossibleTravelWindow 两个 IP 时间差小于该值 → 触发 ImpossibleTravel
	//   0 表示不检测
	ImpossibleTravelWindow time.Duration
}

// New 默认检测器(MaxConcurrent=3, 不开 ImpossibleTravel)
func New() *Detector {
	return &Detector{MaxConcurrent: 3}
}

// WithMaxConcurrent 设置最大并发数
func (d *Detector) WithMaxConcurrent(n int) *Detector {
	d.MaxConcurrent = n
	return d
}

// WithImpossibleTravelWindow 开启不可能旅行检测(同一 user 短时间不同 IP)
func (d *Detector) WithImpossibleTravelWindow(w time.Duration) *Detector {
	d.ImpossibleTravelWindow = w
	return d
}

// maxConcurrent 生效的并发阈值。
//
// ⚠️ ≤0 一律当成 1,不是 0。
//
//	MaxConcurrent=0 若按字面当"不允许任何并发",那么"只有当前这一个会话"
//	(active 长度 = 1)也会超限 ⇒ **登录一次报一次异常**。
//	Detector 的零值是 `&Detector{}`(MaxConcurrent=0),而 `Detect` 挂在
//	登录链路上 —— 一个零值 Detector 会让每次登录都产生一条 High 级异常。
//	所以这里兜底成 1(至少允许当前会话自己)。
func (d *Detector) maxConcurrent() int {
	if d.MaxConcurrent <= 0 {
		return 1
	}
	return d.MaxConcurrent
}

// Detect 对 (current, history) 跑所有检测,返回发现的异常列表
//
//	current: 本次登录
//	history: 该 user 历史登录(由 service 查库传入,Detector 不读 DB)
//
// 规则:
//  1. ConcurrentDifferentIP:   仍活跃的会话数 **超过 MaxConcurrent** + 其中有 IP 不同的 → High
//  2. ConcurrentDifferentDevice: 同上,Device 不同 → Medium
//  3. ImpossibleTravel:        history 仍活跃 + 距 current < window → High
//     (规则 3 不受 MaxConcurrent 门控 —— 它判时间差,不判数量)
func (d *Detector) Detect(current LoginInfo, history []LoginInfo) []Anomaly {
	var out []Anomaly

	// 统计当前 user 仍活跃的会话(含 current)
	active := make([]LoginInfo, 0, len(history)+1)
	if current.LogoutAt == nil {
		active = append(active, current)
	}
	for _, h := range history {
		if h.LogoutAt == nil {
			active = append(active, h)
		}
	}

	// 规则 1 & 2: 并发活跃 + IP/Device 不同
	//
	// ⚠️ **M128(2026-10-31):这里原来硬编码 `len(active) > 1`** ——
	//
	//   `MaxConcurrent` 字段(`New()` 的默认 3、`WithMaxConcurrent` 那个 setter)
	//   **从来没有被任何一行代码读过**。于是:
	//     - 配 `MaxConcurrent = 10`(允许 10 端同时在线)→ 实际上**第 2 端就报**
	//       "High 级异常";
	//     - 清单 §1.1 #11 里写的"MaxConcurrent=3 默认"是**假的** ——
	//       清单描述的是这个字段的注释,不是它的行为。
	//
	//   这就是「清单上标 ✅ ≠ 那行被读过」那条教训的又一个实例:
	//   一个包有 172 行单测、有完整注释、清单标 P2 ✅,
	//   但**旋钮是死的**,而且没有任何一条用例碰过它。
	//
	//   现在真的按阈值判:**超过允许的同时活跃会话数**才报并发类异常。
	//   `active` 里含 current,所以 MaxConcurrent=3 ⇒ 本次 + 2 个别人 = 3 允许,
	//   第 4 个才报(与字段注释"超过 N 端同时登录视为异常"一致)。
	//
	//   ⚠️ 规则 3(ImpossibleTravel)**不受这个门控** —— 它判的是"时间差",
	//     不是"数量",两者正交。
	if len(active) > d.maxConcurrent() {
		for _, a := range active {
			if a.SessionID == current.SessionID {
				continue // 跳过自己
			}
			if a.IP != "" && a.IP != current.IP {
				out = append(out, Anomaly{
					Type:       ConcurrentDifferentIP,
					Severity:   High,
					UserID:     current.UserID,
					Current:    current,
					Existing:   a,
					Reason:     "同一账号在两个不同 IP 同时活跃",
					DetectedAt: time.Now(),
				})
			}
			if a.Device != "" && a.Device != current.Device {
				out = append(out, Anomaly{
					Type:       ConcurrentDifferentDevice,
					Severity:   Medium,
					UserID:     current.UserID,
					Current:    current,
					Existing:   a,
					Reason:     "同一账号在两个不同设备同时活跃",
					DetectedAt: time.Now(),
				})
			}
		}
	}

	// 规则 3: 短时间内不同 IP(开启时才查)
	if d.ImpossibleTravelWindow > 0 {
		for _, a := range active {
			if a.SessionID == current.SessionID {
				continue
			}
			if a.IP == "" || a.IP == current.IP {
				continue
			}
			gap := current.LoginAt.Sub(a.LoginAt)
			if gap < 0 {
				gap = -gap
			}
			if gap < d.ImpossibleTravelWindow {
				out = append(out, Anomaly{
					Type:       ImpossibleTravel,
					Severity:   High,
					UserID:     current.UserID,
					Current:    current,
					Existing:   a,
					Reason:     "短时间内从两个不同 IP 登录(疑似账号共享/盗号)",
					DetectedAt: time.Now(),
				})
			}
		}
	}

	return out
}

// Notifier 通知器注入(由调用方实现:日志 / SMS / 邮件 / 强制登出 / 系统通知)
type Notifier interface {
	// Notify 单条异常通知(可同步或异步,实现自行决定)
	Notify(ctx context.Context, a Anomaly)
}

// LogNotifier 默认 Notifier:走 zap logger(用 stderr 不依赖具体 logger 路径)
//
//	业务可换 SMSNotifier/EmailNotifier
type LogNotifier struct {
	// Hook 业务可注入额外副作用(如发送 SMS、revoke 旧 jti)
	Hook func(ctx context.Context, a Anomaly)
}

// Notify 记一条 WARN 日志 + 调 Hook
//
// ⚠️ **M128(2026-10-31):原来这个函数体只有 `if n.Hook != nil { n.Hook(...) }`** ——
//
//	注释写着"落 stderr",实现里**一行日志都没有**。
//	于是"用默认通知器"= 什么都不发生:检测跑完了,结果被丢进虚空,
//	而且**没有任何迹象**表明发生过什么。
//
//	日志级别选 WARN:异常登录是**安全事件**,不是调试信息。
//	但它**不阻断登录** —— 记下来,不拦住人(拦住人的是别的机制)。
//
//	Hook 保留:业务可以在这里注入额外副作用(发短信 / 站内信 / 吊销旧 jti),
//	日志与副作用**都要**,别用 Hook 取代日志。
func (n *LogNotifier) Notify(ctx context.Context, a Anomaly) {
	logger.L().Warn("anomalylogin: 检测到异常登录",
		zap.Uint64("user_id", a.UserID),
		zap.String("type", string(a.Type)),
		zap.String("severity", a.Severity.String()),
		zap.String("reason", a.Reason),
		zap.String("current_ip", a.Current.IP),
		zap.String("current_session", a.Current.SessionID),
		zap.String("existing_ip", a.Existing.IP),
		zap.String("existing_device", a.Existing.Device),
		zap.String("existing_session", a.Existing.SessionID),
		zap.Time("detected_at", a.DetectedAt),
	)
	if n.Hook != nil {
		n.Hook(ctx, a)
	}
}
