// 站内信 handler
// 文档 §1.9 #11+#12 P1
package v1

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/pkg/inbox"
	"rapid-admin/internal/service"
)

type InboxHandler struct{ *handler.BaseHandler }

func NewInboxHandler(r *service.Registry) *InboxHandler {
	return &InboxHandler{BaseHandler: handler.New(r)}
}

// List GET /rapid-admin/v1/biz/inbox/messages?msg_type=&only_unread=&page=&size=
//   - 当前登录用户的站内信(不可看他人 — 防 IDOR)
//
// ⚠️ 查询参数是 **snake_case**。M74 之前这里是 msgType / onlyUnread(驼峰),
// 与全仓其它接口不一致。是在前端接上之前改的 —— 没有前端在用旧名,迁移
// 成本为零;等列表页落地之后再改,就得两套名字并行一段时间了。
func (h *InboxHandler) List(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	f := inbox.ListFilter{
		UserID:     caller.UserID,
		MsgType:    c.Query("msg_type"),
		OnlyUnread: c.Query("only_unread") == "true" || c.Query("only_unread") == "1",
		Page:       0, // GetPage 兜底
		Size:       0,
	}
	if v := c.Query("page"); v != "" {
		f.Page, _ = strconv.Atoi(v)
	}
	if v := c.Query("size"); v != "" {
		f.Size, _ = strconv.Atoi(v)
	}
	list, total, err := h.Services.Inbox.List(ctx, f)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, list, total)
}

// UnreadCount GET /rapid-admin/v1/biz/inbox/unread-count?msg_type=
func (h *InboxHandler) UnreadCount(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	n, err := h.Services.Inbox.UnreadCount(ctx, caller.UserID, c.Query("msg_type"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, map[string]any{"unread": n})
}

// MarkRead POST /rapid-admin/v1/biz/inbox/messages/:id/read
func (h *InboxHandler) MarkRead(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Inbox.MarkRead(ctx, caller.UserID, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// MarkAllRead POST /rapid-admin/v1/biz/inbox/mark-all-read?msg_type=
func (h *InboxHandler) MarkAllRead(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	n, err := h.Services.Inbox.MarkAllRead(ctx, caller.UserID, c.Query("msg_type"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, map[string]any{"affected": n})
}
