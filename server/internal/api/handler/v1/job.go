// 定时任务管理 handler —— M68
//
// handler 层刻意极薄:只做参数搬运 + 错误码转换,所有判定在 service。
// 唯一在 handler 层做的加工是 query 参数的 camelCase → snake_case 翻译,
// 因为前端 axios 传的是 camelCase,而 DTO 的 json tag 是 snake_case(全仓统一)。
//
// 路由形状刻意避开「静态段 + :param 同级」,统一用 :job_key:
//   GET /jobs                列表
//   GET /jobs/runtime        运行态
//   GET /jobs/cron/preview   表达式预览
//   GET /jobs/runs           执行记录(静态段)
//   GET /jobs/:job_key       详情(参数段)
// 两条都挂在 /jobs 下,靠段数区分,不靠前缀区分 ——
// Hertz 的 radix 树对「同级静态段与 :param 共存」的行为本仓已实测安全
// (9 条路由 9 条命中),但既然实测通过就没必要依赖它,靠段数更不容易踩未来的坑。

package v1

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

type JobHandler struct{ *handler.BaseHandler }

func NewJobHandler(r *service.Registry) *JobHandler {
	return &JobHandler{BaseHandler: handler.New(r)}
}

func (h *JobHandler) rc(c *app.RequestContext) *service.Caller { return h.BaseHandler.RequireCaller(c) }

// queryBool 把 "true"/"1" 当真。其它任何值(包括空串)都是假 ——
// 反过来写成 "只要不是 false 就当真"的话,URL 里带个 `?enabled_only=` 就会反转语义。
func queryBool(c *app.RequestContext, key string) bool {
	v := c.Query(key)
	return v == "true" || v == "1"
}

// queryIntPtr 只在参数**真的传了**时返回非 nil。
// 返回 *int64 是因为 start_at/end_at 的零值是合法时间戳(1970),
// 用值类型就没法区分"没传"和"传了 0"。
func queryIntPtr(c *app.RequestContext, key string) *int64 {
	v := c.Query(key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// ListJobs GET /jobs
func (h *JobHandler) ListJobs(ctx context.Context, c *app.RequestContext) {
	q := service.JobListQuery{
		Keyword:     c.Query("keyword"),
		EnabledOnly: queryBool(c, "enabledOnly"),
	}
	list, err := h.Services.Job.ListJobs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, list)
}

// GetJob GET /jobs/:jobKey
func (h *JobHandler) GetJob(ctx context.Context, c *app.RequestContext) {
	v, err := h.Services.Job.GetJob(ctx, c.Param("jobKey"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Runtime GET /jobs/runtime
//
// 放在静态路由而不是挂在列表里当一个字段:锁可用性是**进程级**状态,
// 同一进程内所有任务共享,做成 per-job 字段会让人误以为某个任务有自己的锁状态。
func (h *JobHandler) Runtime(ctx context.Context, c *app.RequestContext) {
	v, err := h.Services.Job.Runtime(ctx)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// ListJobRuns GET /jobs/runs
func (h *JobHandler) ListJobRuns(ctx context.Context, c *app.RequestContext) {
	q := service.JobRunListQuery{
		JobKey:       c.Query("jobKey"),
		Status:       c.Query("status"),
		TriggerType:  c.Query("triggerType"),
		DegradedOnly: queryBool(c, "degradedOnly"),
		StartAt:      queryIntPtr(c, "startAt"),
		EndAt:        queryIntPtr(c, "endAt"),
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Job.ListJobRuns(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// CronPreview GET /jobs/cron/preview?expr=...&n=5
func (h *JobHandler) CronPreview(ctx context.Context, c *app.RequestContext) {
	n := 5
	if v := c.Query("n"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			n = parsed
		}
	}
	list, err := h.Services.Job.CronPreview(ctx, c.Query("expr"), n)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, list)
}

// UpdateJob PATCH /jobs/:jobKey
//
// resetToDefault 是**显式布尔字段**,不用「cronExpr 传空串」表达恢复默认 ——
// 空串在前端表单里天然是"用户把输入框清空了",把它解释成"恢复默认"的话,
// 用户一次误删键就会永久丢掉代码里的默认表达式,而且没有任何提示。
func (h *JobHandler) UpdateJob(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	var body struct {
		Name           *string `json:"name"`
		Description    *string `json:"description"`
		CronExpr       *string `json:"cron_expr"`
		Enabled        *bool   `json:"enabled"`
		Sort           *int    `json:"sort"`
		ResetToDefault bool    `json:"reset_to_default"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	pu := service.JobOverridePatch{
		Name:        body.Name,
		Description: body.Description,
		CronExpr:    body.CronExpr,
		Enabled:     body.Enabled,
		Sort:        body.Sort,
	}
	v, err := h.Services.Job.UpdateOverride(ctx, caller, c.Param("jobKey"), pu, body.ResetToDefault)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// TriggerJob POST /jobs/:jobKey/trigger
//
// 同步返回本次执行结果(200),不是 202。理由见 JobService.TriggerNow 的注释。
// ⚠️ 同步意味着请求会挂到任务跑完,超时由中间件兜;
// 内置任务全部有 job.default_timeout(默认 30m)封顶,不会真的挂死。
func (h *JobHandler) TriggerJob(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	v, err := h.Services.Job.TriggerNow(ctx, caller, c.Param("jobKey"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}
