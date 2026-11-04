// 会话活动心跳 —— 让 sys_login_log.last_active_at 记"最后一次活动"而不是"最后一次续期"
//
// # 为什么要有这个
//
// 这一列的文档定义是"最后一次**确认这条会话还活着**的时刻"
// (见 model.SysLoginLog 与技术方案 §7.7.4.17)。但它此前**只在续期时更新** ——
// 而续期每 8 小时才发生一次(access token 的寿命)。
// 于是有两个后果:
//
//  1. 会话建立后的整整 8 小时里,这一列是**空的**。审计中心「最后活动」那列对
//     当天所有的登录都是空白 —— 看上去像"登录之后就没动过",实际上人一直在用。
//  2. 会话登记簿判活用的 `COALESCE(last_active_at, login_at) + refresh_ttl > now`
//     拿到的也是一个最多 8 小时陈旧的值。
//
// ⇒ 改成:**有请求打在这条会话上,就记一次活动**,但必须**节流** ——
// 每个请求一次 UPDATE 的话,SQLite 下写锁串行(MySQL 下多一次往返)。
//
// # 两层节流
//
//  1. 进程内闸(touchGate):按 sid 记"下次允许写库的时刻"。
//     热路径上只是一次 map 查,绝大多数请求在这里就返回了 —— 根本不碰 DB。
//  2. DB 条件更新:`WHERE ... AND (last_active_at IS NULL OR last_active_at < ?)`。
//     真正拦写的是这一层,所以**多副本部署时也不会写爆**
//     (B 副本不知道 A 副本刚写过,但 DB 知道)。
//
// 只做第 1 层会在多副本下退化成"每个副本各写一次";只做第 2 层则每个请求
// 都要打一次 DB。两层都要。
//
// # 不影响任何安全判定
//
// 这一列是**展示 / 判活**用的:
//
//   - sessionExpired(活跃设备列表)只决定"还列不列出来",不决定放不放行;
//   - 真正的放行判定是吊销存储的三道闸(jti / sid / 作废线),与这一列无关。
//
// 所以写得更频繁**不会**放松任何一道闸 —— 这一点是本改动的前提,不是结论。
package service

import (
	"context"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"rapid-admin/internal/model"
)

const (
	// sessionTouchInterval 会话活动写库的节流间隔
	//
	// 取 5min 的理由:前端空闲超时是 30min(见 web/src/utils/idleTimer.ts),
	// 一条"活着"的会话最多 30 分钟没有活动。5 分钟的精度足够回答
	// "这条会话刚才还在用吗",又不会把写库的量放大到不可接受
	// (每个会话每 5 分钟一次,100 个并发会话 = 1200 次/小时)。
	sessionTouchInterval = 5 * time.Minute

	// touchGateSize 进程内闸的容量上限(按会话数)
	//
	// 超了会 LRU 淘汰 —— 淘汰的只是"节流记忆",后果是多写一次库(DB 那层会拦),
	// 不是正确性问题。
	touchGateSize = 10000
)

// touchGate 会话活动写库的进程内闸
//
// 零值可用(内部惰性建 LRU),所以 authService 不必在构造函数里额外初始化它 ——
// 而"忘了初始化 ⇒ 闸门永远开着 ⇒ 每个请求一次 UPDATE"这种坑就不存在了。
type touchGate struct {
	mu    sync.Mutex
	cache *lru.Cache[string, time.Time]
}

// allow 判断这次是否允许写库
//
// 允许时顺手把"下次允许的时刻"记进去 —— **即使后面 DB 那层没写成也要记**:
// 不然的话每个请求都会去撞一次 DB(那层明明会把它挡掉)。
func (g *touchGate) allow(sid string, now time.Time) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cache == nil {
		c, err := lru.New[string, time.Time](touchGateSize)
		if err != nil {
			// lru.New 只在 size <= 0 时报错,这里不可能 —— 兜底成"每次都放行"
			return true
		}
		g.cache = c
	}
	if next, ok := g.cache.Get(sid); ok && now.Before(next) {
		return false
	}
	g.cache.Add(sid, now.Add(sessionTouchInterval))
	return true
}

// TouchLoginSession 记一次"这条会话刚刚有活动"
//
// 入口在 Auth 中间件(每次已认证的请求)。**纯记账**:写失败只影响
// "活跃设备列表"的显示精度,不影响任何放行判定。
func (s *authService) TouchLoginSession(ctx context.Context, caller *Caller) error {
	if s == nil || caller == nil {
		return nil
	}
	_, err := s.touchSession(ctx, caller.UserID, caller.SessionID)
	return err
}

// touchSession 更新 sys_login_log.last_active_at
//
// 返回 (是否真的写了, error)。
//
// ⚠️ **不要用"SELECT 出来再判断"来做节流** —— 那是两次往返。
// 条件更新是一次往返就把判断和写入一起做完:
//
//	UPDATE sys_login_log SET last_active_at = ?
//	WHERE user_id = ? AND session_id = ?
//	  AND (last_active_at IS NULL OR last_active_at < ?)
//
// 第二个条件是**跨副本**的那道闸:本进程不知道别的副本刚写过,但 DB 知道。
func (s *authService) touchSession(ctx context.Context, userID uint64, sid string) (bool, error) {
	// sid 为空 = 这条 token 是本轮上线前签发的老会话(没有 sid)，
	// 或者压根不是一次成功的登录 —— 没有可定位的行,不写。
	if s == nil || s.db == nil || userID == 0 || sid == "" {
		return false, nil
	}
	now := model.Now()
	// 第一层:进程内闸。绝大多数请求在这里就返回了,不碰 DB。
	if !s.touch.allow(sid, now.Time) {
		return false, nil
	}
	// 第二层:DB 条件更新。
	cutoff := model.Time{Time: now.Add(-sessionTouchInterval)}
	res := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
		Where("user_id = ? AND session_id = ? AND (last_active_at IS NULL OR last_active_at < ?)",
			userID, sid, cutoff).
		Update("last_active_at", &now)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
