// 用户管理 handler
// 文档 §7.3
package v1

import (
	"bytes"
	"context"
	"io"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/service"
)

type UserHandler struct {
	*handler.BaseHandler
}

func NewUserHandler(r *service.Registry) *UserHandler {
	return &UserHandler{BaseHandler: handler.New(r)}
}

func (h *UserHandler) requireCaller(c *app.RequestContext) *service.Caller {
	return h.BaseHandler.RequireCaller(c)
}

// List GET /rapid-admin/v1/users
func (h *UserHandler) List(ctx context.Context, c *app.RequestContext) {
	q := service.UserListQuery{
		Keyword:      c.Query("keyword"),
		DepartmentID: h.MustUint(c, "departmentId", 0),
		RoleCode:     c.Query("roleCode"),
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.User.List(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// Get GET /rapid-admin/v1/users/:id
func (h *UserHandler) Get(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	u, err := h.Services.User.Get(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, u)
}

// Create POST /rapid-admin/v1/users
func (h *UserHandler) Create(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var req service.CreateUserRequest
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.User.Create(ctx, caller, req)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// Update PATCH /rapid-admin/v1/users/:id
func (h *UserHandler) Update(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var req service.UpdateUserRequest
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.User.Update(ctx, caller, id, req); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Delete DELETE /rapid-admin/v1/users/:id
func (h *UserHandler) Delete(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.User.Delete(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// BatchRemove POST /rapid-admin/v1/users/batch-delete
// 2026-10-19 M63a 框架层批量删除,前端表格勾选多行 → 一次性提交
//   - body: { ids: ["1","2",...] } 字符串数组(与单删 GetID 风格一致)
//   - 单事务原子,任一失败整体回滚
func (h *UserHandler) BatchRemove(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	ids := make([]uint64, 0, len(body.IDs))
	for _, s := range body.IDs {
		if s == "" {
			continue
		}
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			h.Fail(c, errcode.New(errcode.BadRequest, "ids 格式错误: "+s))
			return
		}
		ids = append(ids, v)
	}
	if err := h.Services.User.BatchRemove(ctx, caller, ids); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Unlock POST /rapid-admin/v1/users/:id/unlock
func (h *UserHandler) Unlock(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.User.Unlock(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// DisableUserMfa POST /rapid-admin/v1/users/:id/mfa/disable (M150)
//
// 管理员关闭某用户的登录二次验证 —— 用户手机丢了收不到码、本人又没法
// 自助关闭(自助关也要验证码)时的唯一恢复路径。关闭后该用户的所有会话
// 立即作废(service 层 invalidateUserSessions),重新登录即不再要求 MFA。
func (h *UserHandler) DisableUserMfa(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.User.DisableUserMfa(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// ToggleActive POST /rapid-admin/v1/users/:id/toggle
func (h *UserHandler) ToggleActive(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body struct {
		Active bool `json:"active"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.User.ToggleActive(ctx, caller, id, body.Active); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// SetAvatar POST /rapid-admin/v1/auth/avatar —— 上传并设置**当前用户自己**的头像
//
// ⚠️ **multipart/form-data**,字段名固定 `file`(与 /biz/files 一致)。
//
// ⚠️ **路由挂在 /auth 下,但实现落在 user service** ——
// 因为它写的是 `sys_user.avatar`,那是**用户域**的字段;
// 而 /auth 是"自助能力"那一组(改自己的资料 / 切身份 / 看自己的设备)。
// 两边各取一头:路径按"谁来用"分,实现按"改哪张表"分。
//
// ⚠️ **不接 userId 参数** —— 头像只能改自己的,不给越权的入口。
// 管理员替别人改头像是另一个产品决策,本轮不做。
func (h *UserHandler) SetAvatar(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		h.FailCode(c, errcode.ParamMissing, "缺少文件字段 file")
		return
	}
	f, err := fh.Open()
	if err != nil {
		h.FailCode(c, errcode.BadRequest, "读取上传内容失败")
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		h.FailCode(c, errcode.BadRequest, "读取上传内容失败")
		return
	}
	view, err := h.Services.User.SetAvatar(ctx, caller, service.AvatarUploadReq{
		FileName:    fh.Filename,
		ContentType: fh.Header.Get("Content-Type"),
		// ⚠️ 申报大小用 `fh.Size`(multipart 解析出的这一段字节数),
		// 与 /biz/files 那条保持同一口径。
		Size: fh.Size,
		Body: bytes.NewReader(body),
	})
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, view)
}
