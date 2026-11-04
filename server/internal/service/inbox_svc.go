// 站内信 service
// 文档 §1.9 #11+#12 P1
//
// 设计:
//   - InboxService 是 handler 与 pkg/inbox.Manager 之间的薄壳
//   - 仅暴露 handler 需要的 4 个查询/写入方法
//   - 业务模块投递站内信:直接拿 pkg/inbox.Manager.Notifier(在 main.go 注入到 service 注册表)
package service

import (
	"context"

	"rapid-admin/internal/pkg/inbox"
)

// InboxService 站内信 service 接口(handler 用)
type InboxService interface {
	// List 拉某 user 的站内信列表
	List(ctx context.Context, f inbox.ListFilter) ([]inbox.MessageView, int64, error)
	// UnreadCount 未读数
	UnreadCount(ctx context.Context, userID uint64, msgType string) (int64, error)
	// MarkRead 标单条已读
	MarkRead(ctx context.Context, userID, msgID uint64) error
	// MarkAllRead 全部已读
	MarkAllRead(ctx context.Context, userID uint64, msgType string) (int64, error)
}

// inboxService 默认实现
type inboxService struct {
	mgr *inbox.Manager
}

// NewInboxService 构造
func NewInboxService(mgr *inbox.Manager) InboxService {
	return &inboxService{mgr: mgr}
}

func (s *inboxService) List(ctx context.Context, f inbox.ListFilter) ([]inbox.MessageView, int64, error) {
	return s.mgr.List(ctx, f)
}

func (s *inboxService) UnreadCount(ctx context.Context, userID uint64, msgType string) (int64, error) {
	return s.mgr.UnreadCount(ctx, userID, msgType)
}

func (s *inboxService) MarkRead(ctx context.Context, userID, msgID uint64) error {
	return s.mgr.MarkRead(ctx, userID, msgID)
}

func (s *inboxService) MarkAllRead(ctx context.Context, userID uint64, msgType string) (int64, error) {
	return s.mgr.MarkAllRead(ctx, userID, msgType)
}
