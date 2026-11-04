// DashboardService 平台看板 — 概览 / 趋势 / 登录热力 / 服务器指标
//
// # ⚠️ 看板内容**按业务域权限过滤**
//
//	看板接口只有持 dashboard:read 的角色能打开;打开之后"看得到哪几块"
//	由域权限决定 —— 没权限的域**不出现在响应里**(指针 + omitempty
//	⇒ JSON 里压根没这个 key),前端"字段缺省 = 不显示这张卡片"。
//
//	判定在 handler 层用 middleware.CanAccess 做(执行层,唯一一份实现),
//	service 层只按判定结果决定算不算 —— 见 dashboard_visibility.go。
//
// # 开源口径
//
//	框架自带指标 = 文件系统统计 + 在线人数 + 上传趋势 + 登录热力 +
//	服务器资源快照。业务方接入自己的模块后,在 dashboard_visibility.go
//	登记自己的域、在 dashboardMetricPerms/dashboardSeriesPerms 加指标即可,
//	过滤机制原样复用。
package service

import (
	"context"
)

// DashboardService 平台看板
type DashboardService interface {
	// GetOverview 关键指标卡片
	//
	// ⚠️ vis 决定**哪些指标出现在响应里** —— 没权限的指标不出现在 JSON 里。
	GetOverview(ctx context.Context, vis DashboardVisibility) (*Overview, error)

	// GetTrend 7/30 天趋势(每日新增文件序列)
	GetTrend(ctx context.Context, vis DashboardVisibility, days int) (*Trend, error)

	// GetLoginHeatmap 每日成功登录次数(按东八区自然日,缺的天补 0)
	//
	// ⚠️ **不做业务域过滤** —— 它是纯计数(没有用户名 / IP / 设备),
	//     不泄漏任何人的数据,能打开工作台(dashboard:read)就能看;
	//     所以 handler 不传 vis,service 也不判。
	GetLoginHeatmap(ctx context.Context, days int) ([]TrendPoint, error)

	// GetServerMetrics 服务器资源快照(CPU / 内存 / 磁盘)+ Redis 概览
	//
	// ⚠️ **权限比在线人数/热力图窄一档** —— 暴露的是服务器内部信息,
	//     路由挂 dashboard:server(只绑平台运营线),不是 dashboard:read。
	//     详见 dashboard_metrics.go 的头注释。
	GetServerMetrics(ctx context.Context) (*ServerMetricsView, error)
}

// Overview 关键指标
//
// ⚠️ **全部是指针 + omitempty** —— 没权限的指标**不出现在响应里**
// (字段缺省 = 前端不显示这张卡片)。
//
// 为什么不用「数值 + 一个 visible 列表」:那两份表示**可能互相矛盾**
// (值在、列表里没有)⇒ 前端得决定信哪个,判错一次就是"显示了没权限的数"。
// 指针只有一种表示:**有就是有,没有就是没有**。
//
// ⚠️ 指针 + omitempty 的坑:**值为 0 依然会出现在 JSON 里**
// (omitempty 对指针只判 nil)—— 这正是我们要的:
// "看得见但这个月一个都没有" ≠ "看不见"。
type Overview struct {
	// 文件系统(M80)统计 —— 全平台口径,不按 caller 收窄;
	// GORM 软删作用域自动排除 deleted_at 非空的行。
	FileTotal        *int64 `json:"file_total,omitempty"`
	FileToday        *int64 `json:"file_today,omitempty"`
	FileStorageBytes *int64 `json:"file_storage_bytes,omitempty"`
	FolderTotal      *int64 `json:"folder_total,omitempty"`
	// 当前在线人数 —— **去重的用户数**,不是会话数
	// (一个人开两个浏览器算 1 个)。
	//
	// 口径:status=1(登录成功)+ 没有登出(logout_at IS NULL)+
	// 最近活动落在在线窗口内(见 dashboard_impl.go 的 userOnlineWindow)。
	// "最近活动"与会话登记簿同源:COALESCE(last_active_at, login_at)
	// —— 心跳节流 5min 一次(见 auth_touch.go),窗口必须 ≥ 2 倍节流间隔,
	// 否则正在用的人会被窗口边界抖出去。
	UserOnline *int64 `json:"user_online,omitempty"`
}

// TrendPoint 单日数据
type TrendPoint struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Value int64  `json:"value"`
}

// Trend 趋势图
//
// ⚠️ 同样 omitempty —— 没权限的序列**不出现在响应里**
// (序列是切片,空切片也会被 omitempty 吃掉;而**有权限**的序列
// 永远有 days 个点(缺的天补 0)⇒ 不会被误删)。
type Trend struct {
	// FileCreate 每日新增文件数(东八区自然日分桶)
	FileCreate []TrendPoint `json:"file_create,omitempty"`
}

// dashboardMetricPerms 概览指标(wire 字段名)→ 需要的权限码
//
// ⚠️ **必须覆盖 Overview 的每一个字段** —— 漏一个 ⇒ 那个指标**永远不出现**
//
//	(查表拿到空串 ⇒ vis.Allows("") 恒 false),而且**不报错**。
//	TestDashboard_MetricPermsCoverAllFields 钉住这条。
var dashboardMetricPerms = map[string]string{
	"file_total":         "file:list",
	"file_today":         "file:list",
	"file_storage_bytes": "file:list",
	"folder_total":       "folder:list",
	// 在线人数是**纯计数**(没有用户名 / IP / 设备,不泄漏任何人的数据),
	// 凡能打开工作台(dashboard:read,在 authBaseCodes 里,每个登录角色都有)
	// 的人都能看 —— 不挂业务域的码。
	"user_online": "dashboard:read",
}

// dashboardSeriesPerms 趋势序列(wire 字段名)→ 需要的权限码
//
// ⚠️ 同上 —— 必须覆盖 Trend 的每一个字段(TestDashboard_MetricPermsCoverAllFields 一起守)
var dashboardSeriesPerms = map[string]string{
	"file_create": "file:list",
}
