// 审计日志
// 文档 §9.5
package model

// SysOperationLog 操作日志
type SysOperationLog struct {
	BaseModel
	UserID          uint64 `gorm:"index" json:"user_id,string"`
	Username        string `gorm:"size:64;index" json:"username"`
	UserRole        string `gorm:"size:64" json:"user_role"`
	Module          string `gorm:"size:64;index" json:"module"` // 业务模块
	Action          string `gorm:"size:64;index" json:"action"` // login / list / create / update / ...
	Description     string `gorm:"size:500" json:"description"`
	RequestURL      string `gorm:"size:255" json:"request_url"`
	RequestMethod   string `gorm:"size:8" json:"request_method"`
	RequestBody     string `gorm:"type:text" json:"request_body"`   // 脱敏后
	RequestParams   string `gorm:"type:text" json:"request_params"` // 脱敏后 query/path
	ResponseSummary string `gorm:"type:text" json:"response_summary"`
	IP              string `gorm:"size:45;index" json:"ip"`
	Location        string `gorm:"size:64" json:"location"`
	UserAgent       string `gorm:"size:255" json:"user_agent"`
	Duration        int    `json:"duration"`             // ms,实际耗时
	CostMS          int64  `gorm:"index" json:"cost_ms"` // 冗余,便于聚合
	// Status 1 成功 / 0 失败
	//
	// ⚠️ **2026-10-30 去掉 `default:1`** —— 那是 GORM 的一个陷阱:
	//   带 `default` tag 且值为 Go 零值的字段会被 GORM **从 INSERT 里省略**,
	//   于是 DB 的列默认值(1)生效 ⇒ **记下来的失败变成了成功**。
	//   实测过:中间件写 status=0(失败),落库查出来是 1。
	//   失败的操作日志正是审计最要看的那批,这个 tag 必须去掉。
	//   (SysLoginLog.Status 本来就没有 default tag,是安全的。)
	Status    int    `gorm:"index" json:"status"`
	ErrorMsg  string `gorm:"size:1000" json:"error_msg"`
	TraceID   string `gorm:"size:64;index" json:"trace_id"`
	RequestID string `gorm:"size:64;index" json:"request_id"`
}

func (SysOperationLog) TableName() string { return "sys_operation_log" }

// SysDataChangeLog 数据变更日志
type SysDataChangeLog struct {
	BaseModel
	TableNameField string `gorm:"column:table_name;size:64;index" json:"table_name"`
	RecordID       uint64 `gorm:"index" json:"record_id,string"`
	// Operation 1 新增 / 2 修改 / 3 删除
	//
	// ⚠️ 同 SysOperationLog.Status:**不能带 `default` tag**。
	//   带 default 且值为零值 ⇒ GORM 省略该列 ⇒ DB 默认值(2 = 修改)生效,
	//   一个"忘了填操作类型"的记录会**静默变成一条修改记录**。
	//   宁可它是 0(前端不显示任何操作类型),也不要谎报成"修改"。
	Operation int    `gorm:"index" json:"operation"`
	Before    string `gorm:"type:text" json:"before"`
	After     string `gorm:"type:text" json:"after"`
	UserID    uint64 `gorm:"index" json:"user_id,string"`
	Username  string `gorm:"size:64" json:"username"`
}

func (SysDataChangeLog) TableName() string { return "sys_data_change_log" }

// SysLoginLog 登录日志
//
// # 本表同时是**会话登记簿**(session registry)
//
// 一次登录 = 一行 = 一条会话。靠 SessionID 关联到 token 里那个 sid claim
// (见 internal/pkg/auth/session.go)。
//
// 为什么把登记簿放在这张表里,而不是新建一张 `sys_session`:
//   - 需求 §2.2.1 #6 要的字段(设备 / IP / 浏览器 / 系统 / 登录时间)**这张表
//     全都有**,新建一张表等于把这些字段抄一遍
//   - 会话登记簿需要**跨实例一致**。吊销存储的内存实现是**进程内**的,
//     多实例部署时 A 实例不知道 B 实例上有哪些会话 —— 放 DB 就没有这个问题
//   - 登出/续期都要写这张表,已是热路径,合在一张表里省一次写
//
// # 三个时间字段的分工
//
//   - LoginAt      —— 这一次登录发生的时刻(不随续期变)
//   - LastActiveAt —— 最后一次**确认这条会话还活着**的时刻。
//     2026-10-30(M117-4)起:**有请求打在这条会话上就记一次**(登录本身也算
//     一次),节流写(≤5min 一次)。
//     此前**只在 refresh 时更新**,而续期每 8 小时才发生一次 ⇒ 会话建立后的
//     整整 8 小时里这一列是空的(审计中心「最后活动」对当天所有登录都空白)。
//   - LogoutAt     —— 被显式登出/强制下线的时刻
//
// 判"会话是否还活着"要用 `COALESCE(last_active_at, login_at) + refresh_ttl > now`,
// 不能只看 login_at:refresh token 每次轮换都重新起一个 720h 窗口,一条 40 天前
// 登录、昨天刚续过期的会话**还活着**,只看 login_at 会把它误判成死的。
type SysLoginLog struct {
	BaseModel
	UserID   uint64 `gorm:"index" json:"user_id,string"`
	Username string `gorm:"size:64;index" json:"username"`
	IP       string `gorm:"size:45;index" json:"ip"`
	Location string `gorm:"size:64" json:"location"`
	Device   string `gorm:"size:128" json:"device"`
	Browser  string `gorm:"size:64" json:"browser"`
	OS       string `gorm:"size:64" json:"os"`
	Status   int    `gorm:"index" json:"status"` // 1 成功 / 0 失败
	Message  string `gorm:"size:255" json:"message"`
	LoginAt  Time   `json:"login_at"`
	LogoutAt *Time  `json:"logout_at"`
	// SessionID 会话 id(sid)—— 一次登录一个,与 access / refresh 两张 token 共用
	//   (见 internal/pkg/auth/session.go)。
	//
	//   空串的两种情况:本轮上线前签发的 token(老会话)、登录失败的记录
	//   (失败时压根没有会话)。**空串的会话不可被点名吊销** —— 没有 sid 就
	//   没有吊销的句柄,这不是"暂时没实现",是物理上做不到。
	SessionID string `gorm:"size:64;index" json:"session_id"`
	// LastActiveAt 最后一次确认这条会话还活着的时刻。
	//   写入方:鉴权中间件的会话活动心跳(节流,≤5min 一次)—— 见
	//   internal/service/auth_touch.go;**成功登录时也写**(登录本身就是
	//   一次活动);续期也会写。
	//   nil = 登录失败的记录(压根没有会话),此时用 LoginAt 兜底。
	LastActiveAt *Time `json:"last_active_at"`
}

func (SysLoginLog) TableName() string { return "sys_login_log" }

// BizAuditFullInfoView 完整敏感信息查看审计
// 文档 §9.3.3
type BizAuditFullInfoView struct {
	BaseModel
	UserID     uint64 `gorm:"index;not null" json:"user_id,string"`
	TargetType string `gorm:"size:32;index" json:"target_type"` // user / owner
	TargetID   uint64 `gorm:"index" json:"target_id,string"`
	Field      string `gorm:"size:32" json:"field"`            // phone / id_card / email / bank_card
	SmsScene   string `gorm:"size:32" json:"sms_scene"`        // view_full_phone / etc.
	SmsCodeID  uint64 `gorm:"index" json:"sms_code_id,string"` // 短信批次 id(回溯)
	RequestID  string `gorm:"size:64;index" json:"request_id"`
	Verified   bool   `json:"verified"`
	IP         string `gorm:"size:45" json:"ip"`
	Reason     string `gorm:"size:500" json:"reason"`
}

func (BizAuditFullInfoView) TableName() string { return "biz_audit_full_info_view" }

// BizAuditDataExportLog 业务数据导出审计
// 文档 §10.4
type BizAuditDataExportLog struct {
	BaseModel
	UserID       uint64 `gorm:"index;not null" json:"user_id,string"`
	Username     string `gorm:"size:64" json:"username"`
	BusinessType string `gorm:"size:32;index" json:"business_type"` // owner / house / issue
	ExportType   string `gorm:"size:16" json:"export_type"`         // csv / xlsx
	RecordCount  int    `json:"record_count"`
	FileURL      string `gorm:"size:500" json:"file_url"`
	IP           string `gorm:"size:45" json:"ip"`
	UA           string `gorm:"size:255" json:"ua"`
	// Status 1 成功 0 失败
	//
	// ⚠️ 同 SysOperationLog.Status:**不能带 `default` tag**。
	//   (本表目前**零写入方**,但 tag 一旦留着,将来第一个写失败记录的人
	//   会踩中"失败被记成成功"的坑。)
	Status   int    `gorm:"index" json:"status"`
	ErrorMsg string `gorm:"size:500" json:"error_msg"`
}

func (BizAuditDataExportLog) TableName() string { return "biz_audit_data_export_log" }

// SysSystemLog 系统运行日志(2026-11-03)—— 后台自己写的运行时事件,不是人的操作
//
// # 收录哪些(写入口在哪)
//
//   - slow_sql   GORM 慢查询(>500ms,slowquery.OnSlow 回调,main.go 装配)
//   - slow_http  接口响应慢(>2s,AccessLog 中间件)
//   - http_error 接口 5xx(AccessLog 中间件)
//   - panic      panic 兜底(Recover 中间件)
//
// # 为什么落库而不只是打日志 / 推 OTel
//
//   - OTel(otelx)只导 trace,且 otel.enabled 打开才生效 —— dev 默认关
//   - zap 日志在文件里,后台页面看不见
//   - 落库后审计中心能查"什么时候 / 哪个接口 / 慢多少 / 什么错",
//     这是运维排查的第一入口;ARMS / 文件日志是第二三入口
//
// # 写入纪律
//
//   - RecordSystemLog **异步写**(goroutine + 独立超时),任何失败只打
//     warn 日志,绝不影响业务请求 —— 这张表是"观测",不是"业务"
//   - Detail(完整 SQL / 堆栈)写入前截断,防单条日志撑爆
type SysSystemLog struct {
	BaseModel
	// Level 级别 —— 字典 sys_log_level(info / warn / error)
	Level string `gorm:"size:16;index" json:"level"`
	// Source 来源 —— slow_sql / slow_http / http_error / panic
	Source string `gorm:"size:32;index" json:"source"`
	// Method HTTP 方法(slow_http / http_error 有;slow_sql / panic 可空)
	Method string `gorm:"size:8" json:"method"`
	// Path 接口路径(http_error / slow_http)或表名(slow_sql)
	Path string `gorm:"size:255;index" json:"path"`
	// Message 一句话摘要(列表页直接看的列)
	Message string `gorm:"size:255" json:"message"`
	// Detail 完整 SQL / 错误串 / 截断堆栈(详情抽屉看)
	Detail string `gorm:"type:text" json:"detail"`
	// TraceID 关联 OTel / 响应头的 trace id,串起同一次请求的所有记录
	TraceID string `gorm:"size:64;index" json:"trace_id"`
	// UserID 触发请求的登录人(0 = 系统自身 / 定时任务 / 未鉴权路径)
	UserID uint64 `gorm:"index" json:"user_id,string"`
	// DurationMs 耗时毫秒(slow_sql / slow_http 有;error / panic 记 0)
	DurationMs int64 `json:"duration_ms"`
}

func (SysSystemLog) TableName() string { return "sys_system_log" }
