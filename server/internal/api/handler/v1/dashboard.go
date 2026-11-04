// 平台看板 handler
// 文档 §10.5
//
// 4 个端点:
//
//	GET /platform/dashboard                 overview(关键指标)
//	GET /platform/dashboard/trend           trend(7/30 天)
//	GET /platform/dashboard/login-heatmap   登录热力
//	GET /platform/dashboard/server-metrics  服务器资源快照
//
// # ⚠️ 2026-11-01:看板内容按业务域权限过滤
//
//	路由上挂的是 rbac("dashboard", "read") —— 那是**能不能打开工作台**;
//	「打开之后看得到哪几块」在这里现算:每个业务域问一次执行层
//	(middleware.CanAccess),拿到的结果交给 service 层决定算不算。
//
//	⚠️ **判定必须走 middleware.CanAccess** —— 那是唯一一份判定实现
//	(super_admin 短路 / role+user 两路 / deny 一票否决 / grant 委托)。
//	在这里另写一套(比如直接查绑定矩阵)就会出现"看板显示了但接口 403"。
package v1

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/service"
)

type DashboardHandler struct{ *handler.BaseHandler }

func NewDashboardHandler(r *service.Registry) *DashboardHandler {
	return &DashboardHandler{BaseHandler: handler.New(r)}
}

// GetOverview 关键指标
func (h *DashboardHandler) GetOverview(ctx context.Context, c *app.RequestContext) {
	caller := h.CallerFromContext(ctx)
	if caller == nil {
		h.FailCode(c, 40100, "未登录")
		return
	}
	o, err := h.Services.Dashboard.GetOverview(ctx, h.dashboardVisibility(ctx, caller))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, o)
}

// GetTrend 趋势
func (h *DashboardHandler) GetTrend(ctx context.Context, c *app.RequestContext) {
	caller := h.CallerFromContext(ctx)
	if caller == nil {
		h.FailCode(c, 40100, "未登录")
		return
	}
	days, _ := strconv.Atoi(c.Query("days"))
	t, err := h.Services.Dashboard.GetTrend(ctx, h.dashboardVisibility(ctx, caller), days)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, t)
}

// GetLoginHeatmap 每日登录热力图
//
// ⚠️ **不判业务域** —— 纯计数(没有用户名 / IP / 设备),能打开工作台
// 且路由上挂的 rbac("dashboard","read") 就够,所以不传 vis。
func (h *DashboardHandler) GetLoginHeatmap(ctx context.Context, c *app.RequestContext) {
	days, _ := strconv.Atoi(c.Query("days"))
	points, err := h.Services.Dashboard.GetLoginHeatmap(ctx, days)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, points)
}

// GetServerMetrics 服务器资源快照(CPU / 内存 / 磁盘)
//
// ⚠️ **权限比其它看板接口窄一档**:路由挂的是 rbac("dashboard", "server"),
//
//	`dashboard:server` 只绑平台运营线 —— 服务器内部信息不广发。
//	前端卡片按 accessible 清单里有没有这个码决定显隐(显示层真相)。
func (h *DashboardHandler) GetServerMetrics(ctx context.Context, c *app.RequestContext) {
	snap, err := h.Services.Dashboard.GetServerMetrics(ctx)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, snap)
}

// dashboardVisibility 算出当前身份在看板上的可见域
//
// ⚠️ **判定走 middleware.CanAccessQuiet** —— 执行层(casbin)的权威答案。
//
//	为什么是执行层而不是绑定矩阵(sys_role_resource):
//	绑定矩阵是**显示层**的真相(前端菜单/按钮读它),casbin 才是**执行层**的真相。
//	M149 的规矩是「显示层不许自己算,必须读执行层那个权威字段」——
//	看板是显示层,所以读 casbin。
//
//	为什么是 Quiet(不记 WARN):
//	一次页面加载要问 6 个域,没权限的域每个都打一条 WARN
//	⇒ 日志被"权限不足"刷爆,真出事时反而看不见。
//
//	⚠️ **待办 / 消息不在这张表里** —— 它们是**自服务**内容:
//	  · 待办 = 派给我的问题 + 验房(按 caller.UserID 收窄,见 service 层)
//	  · 消息 = 已发布公告(广播)
//	两者都不泄漏别人的数据,所以不需要域权限。
func (h *DashboardHandler) dashboardVisibility(ctx context.Context, caller *service.Caller) service.DashboardVisibility {
	principal, _ := authz.PrincipalFromContext(ctx)
	domains := service.DashboardDomains()
	vis := make(service.DashboardVisibility, len(domains))
	for _, d := range domains {
		vis[d.Code] = middleware.CanAccessQuiet(caller, principal, d.Module, d.Action, 0)
	}
	return vis
}
