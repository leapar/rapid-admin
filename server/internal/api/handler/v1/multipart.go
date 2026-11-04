// 分片上传 handler(设计文档 §8.2)
//
// ⚠️ **分片内容走 header 而不是 multipart**:`POST /files/mp/part` 的 body 就是
// 裸的分片字节,不套 multipart/form-data。理由是这一条路由每个文件要打几十上百
// 次,每次多编一层 multipart 只是白烧 CPU 和内存;代价是拿不到
// `multipart.FileHeader.Size`,所以长度闸放在 service 层(见 upload_impl.go
// UploadPart 里那段 ⚠️)。
package v1

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/service"
)

// MultipartHandler 分片上传
type MultipartHandler struct{ *handler.BaseHandler }

func NewMultipartHandler(r *service.Registry) *MultipartHandler {
	return &MultipartHandler{BaseHandler: handler.New(r)}
}

func (h *MultipartHandler) rc(c *app.RequestContext) *service.Caller {
	return h.BaseHandler.RequireCaller(c)
}

// Init 初始化分片上传
func (h *MultipartHandler) Init(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	var body service.MultipartInitReq
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	resp, err := h.Services.Upload.Init(ctx, caller, body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, resp)
}

// UploadPart 上传一个分片。session_id / part_number 走 header
func (h *MultipartHandler) UploadPart(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	sid, err := h.headerUint(c, "X-Session-Id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	pn, err := h.headerUint(c, "X-Part-Number")
	if err != nil {
		h.Fail(c, err)
		return
	}
	body := c.Request.Body()
	if len(body) == 0 {
		h.FailCode(c, errcode.ParamMissing, "分片内容为空")
		return
	}
	// ⚠️ body 不复制一份。hertz 的 Request.Body() 返回的是底层缓冲区的切片,
	// 而这个请求的生命周期到这里就结束了 —— provider 必须在本次调用内读完它。
	// 本仓三家 provider(Put / UploadPart)都是同步读完的,这条约束成立。
	info, err := h.Services.Upload.UploadPart(ctx, caller, service.UploadPartReq{
		SessionID:  sid,
		PartNumber: int(pn),
		Body:       body,
	})
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, info)
}

// Parts 查已传分片(断点续传依据)
func (h *MultipartHandler) Parts(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	sid, err := h.headerUint(c, "X-Session-Id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	parts, err := h.Services.Upload.Parts(ctx, caller, sid)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, parts)
}

// Complete 合并分片
func (h *MultipartHandler) Complete(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	var body service.CompleteReq
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.Upload.Complete(ctx, caller, body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Cancel 取消会话
func (h *MultipartHandler) Cancel(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "sid")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Upload.Cancel(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// headerUint 取 uint64 header
func (h *MultipartHandler) headerUint(c *app.RequestContext, key string) (uint64, error) {
	raw := string(c.GetHeader(key))
	if raw == "" {
		return 0, errcode.New(errcode.ParamMissing, "缺少 header "+key)
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, errcode.New(errcode.InvalidParam, "header "+key+" 必须是数字")
	}
	return n, nil
}
