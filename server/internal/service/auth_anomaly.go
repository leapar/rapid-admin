// 异常登录检测接线(M128)
//
// # 为什么这个文件现在才出现
//
// `pkg/anomalylogin` 从 M45(2026-10-19)就在仓里了:172 行单测、
// 完整的字段注释、清单 §1.1 #11 标着 "P2 ✅"。
// 但它**从来没有被任何一行生产代码调用过** ——
// `grep -rn 'internal/pkg/anomalylogin'` 全仓零命中。
//
// ⇒ "用户被盗号时无法感知"这条痛点(M45 写在清单的价值列里)
// **一次都没有被真正解决过**:检测器写好了,但没人叫它。
//
// M128 同一个模块里还修了包本身的两处「看起来做了其实没做」
// (并发阈值旋钮是死的 / 默认通知器是空壳),见 anomalylogin 包的注释。
//
// # 「同时在线」的口径 —— 这一条决定了这个功能会不会误报到没法用
//
// 仓里有两个"会话还活着"的口径,它们回答的是**不同的问题**:
//
//  1. `sessionExpired`(auth_sessions.go):`COALESCE(last_active_at, login_at)
//     + refresh_ttl <= now` —— 答的是"**这条会话还算不算数**"。
//     refresh_ttl 默认 720h(30 天)⇒ 一个月前登录、没登出、token 还能
//     续期的会话,在它眼里**全都还活着**。
//
//  2. 本文件的"同时在线":`COALESCE(last_active_at, login_at)` 落在
//     **活动窗口**(active_window,默认 30m)之内 —— 答的是
//     "**这个人现在在不在用**"。
//
// ⚠️ **必须用 2,不能用 1**。
// 用 1 的话:一个 30 天里在 3 台设备上登录过的人,第 4 次登录时
// "同时活跃会话数"就是 4 > 阈值 3 ⇒ **每次登录都报异常**。
// 阈值 3 形同虚设,功能上线第一天就会被关掉。
//
// # 检测失败绝不影响登录
//
// 查库失败、通知失败、站内信投递失败 —— 全部只记日志。
// 登录链路已经过了密码校验与 token 签发,不该被一个"安全增强"拖垮。
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/anomalylogin"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/uainfo"
)

// anomalyInboxMsgType 站内信类型
//
// 用 "system" 而不是自造类型:站内信中心按 msgType 分 tab
// (见 inbox.ListFilter),自造类型会在界面上落进一个**没人认识**的分组。
const anomalyInboxMsgType = "system"

// detectLoginAnomaly 登录成功后跑一次异常登录检测。
//
// ⚠️ **本方法不返回错误** —— 它挂在登录链路上,任何失败都只记日志。
// 调用方(Login)不因为它失败而失败。
func (s *authService) detectLoginAnomaly(ctx context.Context, userID uint64, sessionID string, req LoginRequest, now time.Time) {
	if !s.anomalyCfg.Enabled {
		return
	}

	detector := anomalylogin.New().
		WithMaxConcurrent(s.anomalyCfg.MaxConcurrent).
		WithImpossibleTravelWindow(s.anomalyCfg.ImpossibleTravelWindow)

	current := anomalylogin.LoginInfo{
		SessionID: sessionID,
		UserID:    userID,
		IP:        req.LoginIP,
		// ⚠️ Device 装的是 UA 原文(与 sys_login_log.device 同源),
		//   必须按同样规则截断 —— 两边长度不一致会让"设备相同"的判断失真。
		Device:   uainfo.Clip(req.UserAgent, 128),
		LoginAt:  now,
		LogoutAt: nil,
	}

	history, err := s.concurrentSessions(ctx, userID, sessionID, now)
	if err != nil {
		logger.L().Warn("anomalylogin: 查询并发会话失败,跳过本次检测",
			zap.Uint64("user_id", userID), zap.Error(err))
		return
	}

	anomalies := detector.Detect(current, history)
	if len(anomalies) == 0 {
		return
	}

	// 日志:全部严重度都记。日志是给运维看的,多几行不构成骚扰。
	notifier := &anomalylogin.LogNotifier{}
	for _, a := range anomalies {
		notifier.Notify(ctx, a)
	}

	// 站内信:只发 High。Medium(只是设备不同)太常见 ——
	// 一个"手机 + 电脑"的正常人每次登录都会命中一次。
	// 发站内信 ⇒ 用户被反复骚扰 ⇒ 关掉通知 ⇒ 真异常也没人知道。
	s.notifyAnomalyInbox(ctx, userID, anomalies, len(history)+1)
}

// concurrentSessions 取该用户"最近还活跃"的其他会话(不含本次登录)。
//
// "最近还活跃" = 没有登出(logout_at IS NULL)且最近活动落在活动窗口内。
// 口径说明见文件头。
func (s *authService) concurrentSessions(ctx context.Context, userID uint64, currentSID string, now time.Time) ([]anomalylogin.LoginInfo, error) {
	window := s.anomalyCfg.ActiveWindow
	if window <= 0 {
		// 窗口为 0 = 不判"同时在线" ⇒ 没有历史可查,直接空。
		// (而不是"窗口 0 ⇒ 全部都算活跃" —— 那会把 30 天前的会话全算进来。)
		return nil, nil
	}
	cutoff := now.Add(-window)

	var rows []model.SysLoginLog
	if err := s.db.WithContext(ctx).
		Where("user_id = ? AND status = ? AND session_id != '' AND session_id != ? AND logout_at IS NULL",
			userID, 1, currentSID).
		Order("login_at DESC").
		// 上限:并发会话数超过这个量级时,阈值早就超了,再往下翻也没有增量信息。
		Limit(50).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]anomalylogin.LoginInfo, 0, len(rows))
	for _, r := range rows {
		// 活动时刻与 sessionExpired 同源:COALESCE(last_active_at, login_at)
		base := r.LoginAt.Time
		if r.LastActiveAt != nil && !r.LastActiveAt.Time.IsZero() {
			base = r.LastActiveAt.Time
		}
		if base.Before(cutoff) {
			continue // 太久没活动 ⇒ 不算"同时在线"
		}
		out = append(out, anomalylogin.LoginInfo{
			SessionID: r.SessionID,
			UserID:    r.UserID,
			IP:        r.IP,
			Device:    r.Device,
			LoginAt:   r.LoginAt.Time,
			// 查询已过滤 logout_at IS NULL ⇒ 这里恒为"仍活跃"
			LogoutAt: nil,
		})
	}
	return out, nil
}

// notifyAnomalyInbox 给用户发一条站内信(只有 High 级别才发)。
//
// activeCount 是"本次 + 其他仍活跃的会话数",写进正文让用户知道量级。
func (s *authService) notifyAnomalyInbox(ctx context.Context, userID uint64, anomalies []anomalylogin.Anomaly, activeCount int) {
	if s.inbox == nil {
		// 没装配站内信通道(单测 / 精简部署)⇒ 只留日志。
		// **不报错**:通知通道缺失不该让登录链路有意见。
		return
	}
	var highs []anomalylogin.Anomaly
	for _, a := range anomalies {
		if a.Severity == anomalylogin.High {
			highs = append(highs, a)
		}
	}
	if len(highs) == 0 {
		return
	}

	var lines []string
	for _, a := range highs {
		lines = append(lines, fmt.Sprintf("- %s(IP %s ↔ %s)", a.Reason, a.Current.IP, a.Existing.IP))
	}
	content := fmt.Sprintf(
		"检测到你的账号当前有 %d 个活跃会话,并出现以下异常:\n%s\n\n"+
			"如果这些登录不是你本人的操作,请立即修改密码并联系管理员。",
		activeCount, strings.Join(lines, "\n"))

	msg := inbox.Message{
		UserID:  userID,
		MsgType: anomalyInboxMsgType,
		Title:   "【安全提醒】检测到你的账号在多处同时登录",
		Content: content,
		// ⚠️ Link / RefType / RefID 留空:这条消息**没有对应的详情页**,
		//   硬塞一个链接过去只会点到一个 404。
	}
	if err := s.inbox.Notify(ctx, msg); err != nil {
		logger.L().Warn("anomalylogin: 站内信投递失败",
			zap.Uint64("user_id", userID), zap.Error(err))
	}
}
