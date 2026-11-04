// Package permnotify 权限失效广播(§2.2 #9 P1)
//
// 设计:
//   - 角色/资源绑定变化时 → PublishInvalidateAll() 通知所有在线前端重新拉权限
//   - 前端通过 SSE 订阅(GET /auth/perm-stream),收到消息就 re-fetch accessible
//   - 不做精细化"按 userID 推":角色一变,所有持该角色的用户都要刷
//     (精细化要做"按 role 找 userID 列表"再 publish,本期不必要)
//
// 频道:
//   - push.TopicPerm = "perm:invalidate:all" —— 频道名**沿用本包自己原来那个**
//     所以本包迁到 pkg/push 之后,发布端与订阅端之间没有任何迁移窗口。
//
// ⚠️ M71 变更:本包**不再自己持有 redis.Client**,发布改走 pkg/push。
// 原先的 Init / InitRedis / Subscribe / rdb / ChannelAll 全部删除:
//   - Init 从来没人调用(一个空的 sync.Once)
//   - Subscribe 的唯一调用方是 StreamPermInvalidate handler,它同时被
//     pkg/push 接管(见 /events),所以订阅逻辑跟着 handler 一起搬走了
package permnotify

import (
	"context"

	"rapid-admin/internal/pkg/push"
)

// Message 广播内容(作为 push 事件的 payload)。
//
// ⚠️ 不含 At —— 时间戳由 push.Event.At 承载。两处各放一个时间戳只会让
// "前端该用哪个"变成一个需要回答的问题。
type Message struct {
	Reason  string `json:"reason"`          // "role_resource_changed" / "user_role_changed" / "manual"
	ActorID uint64 `json:"actor_id,string"` // 谁触发的(审计用)。string 化是因为 JS 的安全整数上限
}

// PublishInvalidateAll 广播"权限失效,所有前端重拉"。
//
// **永不返回错误**:推送是旁路,权限改成功了但推送失败不能变成"改失败"。
// push.Publish 内部已经把投递失败记成 Warn 日志,这里不必也不该重复处理。
func PublishInvalidateAll(ctx context.Context, actorID uint64, reason string) {
	push.Publish(ctx, []string{push.TopicPerm}, push.Event{
		Topic:   push.TopicPerm,
		Event:   push.EventPermChanged,
		Payload: Message{Reason: reason, ActorID: actorID},
	})
}
