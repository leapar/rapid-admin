// 短信模板审核状态 handler —— M115
//
// handler 层刻意极薄:只做参数搬运 + 错误码转换,所有判定在 service。
//
// 路由形状(挂在 /rapid-admin/v1/platform 下):
//
//	GET  /sms-templates              列表(四个场景,固定顺序)
//	GET  /sms-templates/:scene       单个场景详情
//	GET  /sms-templates/:scene/history  流转历史
//	POST /sms-templates/:scene/disable  人工停用
//	POST /sms-templates/:scene/enable   人工恢复
//	POST /sms-templates/sync            手动同步一轮
//
// ⚠️ **sync 排在 :scene 之前** —— 它与 :scene 同深度但方法不同(POST vs GET),
// 靠方法区分已经够了;这里仍然把它写在前面,是为了不依赖"radix 树里静态段
// 与参数段同级时谁优先"这个行为(job.go 里有同样的约定)。
package v1

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

type SmsTemplateHandler struct{ *handler.BaseHandler }

func NewSmsTemplateHandler(r *service.Registry) *SmsTemplateHandler {
	return &SmsTemplateHandler{BaseHandler: handler.New(r)}
}

// List GET /sms-templates
func (h *SmsTemplateHandler) List(ctx context.Context, c *app.RequestContext) {
	v, err := h.Services.SmsTemplate.List(ctx)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Get GET /sms-templates/:scene
func (h *SmsTemplateHandler) Get(ctx context.Context, c *app.RequestContext) {
	v, err := h.Services.SmsTemplate.Get(ctx, c.Param("scene"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// History GET /sms-templates/:scene/history?limit=50
func (h *SmsTemplateHandler) History(ctx context.Context, c *app.RequestContext) {
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	list, err := h.Services.SmsTemplate.History(ctx, c.Param("scene"), limit)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, list)
}

// Disable POST /sms-templates/:scene/disable
//
// ⚠️ **没有 body 也能提交** —— 前端允许不填原因,所以后端不能因为
// body 为空就报错。reason 走 query 参数而不是 body,就是为了避开
// "空 body 解析失败"这个坑(见 service 层的 trimReason)。
func (h *SmsTemplateHandler) Disable(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	v, err := h.Services.SmsTemplate.Disable(ctx, caller, c.Param("scene"), c.Query("reason"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Enable POST /sms-templates/:scene/enable
//
// ⚠️ **恢复的落点是 pending 而不是 approved** —— 人工无权判定通过
// (smstpl 包的硬规则 1)。详见 SmsTemplateService.Enable 的注释。
func (h *SmsTemplateHandler) Enable(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	v, err := h.Services.SmsTemplate.Enable(ctx, caller, c.Param("scene"), c.Query("reason"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Sync POST /sms-templates/sync
//
// 手动跑一轮同步(与定时任务同一个入口)。同步是**幂等**的:
// 状态没变就不记历史,重复点不会刷出一堆流水。
func (h *SmsTemplateHandler) Sync(ctx context.Context, c *app.RequestContext) {
	v, err := h.Services.SmsTemplate.Sync(ctx)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}
