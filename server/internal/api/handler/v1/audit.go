// 审计 handler
package v1

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

type AuditHandler struct{ *handler.BaseHandler }

func NewAuditHandler(r *service.Registry) *AuditHandler {
	return &AuditHandler{BaseHandler: handler.New(r)}
}

func (h *AuditHandler) rc(c *app.RequestContext) *service.Caller {
	return h.BaseHandler.RequireCaller(c)
}

// auditScopeUserID 按调用者身份收敛 userId 过滤
//
// ⚠️ **必须无条件收敛**(2026-10-30 修正)。
//
// 旧实现是 `if q.UserID > 0 && !特权 { q.UserID = caller.UserID }` ——
// 也就是**只有"指定了别人的 userId"才收敛**。于是"什么都不传"这条最常见的路径
// 反而**完全不筛**,拿到的是**全平台**的日志。
// 注释写的意图("其他角色只看自己")与实现不符:它挡住的只是"点名查别人",
// 挡不住"不点名就全看"。
//
// 现在的口径:非特权调用方**一律**只查自己的行,传不传 userId 都一样。
// 特权方(super_admin / 风控合规)传什么查什么,保持原样。
//
// ⚠️ `role_compliance` 目前**没有任何角色在用**(13 个角色里没有它),
//
//	留着是为了将来加"风控合规"角色时不用改这里 —— 现在它恒为 false。
func auditScopeUserID(caller *service.Caller, userID uint64) uint64 {
	if caller == nil {
		return userID
	}
	if caller.HasRole("role_super_admin") || caller.HasRole("role_compliance") {
		return userID
	}
	return caller.UserID
}

// auditQuery = c.Query 的函数化形态
//
// # 为什么要抽这一层
//
// 这一层此前**零测试**,而它出过的真 bug 恰恰是"前端发了、后端读没读":
// 前端发 `keyword` / `status` / `startAt`,handler 一个都没接进 query struct,
// 页面表现是"输了关键字没反应"—— HTTP 200,不报错,结果不变。
//
// 抽成纯函数后,能不能直接喂一个 map 就能单测"参数真的进了 struct",
// 不用起 HTTP 服务、也不用拼 RequestContext。
type auditQuery func(key string) string

// auditQueryUint 取无符号整数 query 参数(缺省 / 非法 → 0)
func auditQueryUint(q auditQuery, key string) uint64 {
	v := q(key)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// auditQueryInt64Ptr 只在参数**真的传了**时返回非 nil(缺省 / 非法 → nil)
func auditQueryInt64Ptr(q auditQuery, key string) *int64 {
	v := q(key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// queryOptionalInt 取可选 int query 参数(缺省 / 非法 → nil = 不筛)
//
// ⚠️ 用指针而不是 int:0 在"筛不筛"里必须表示"不筛",
//
//	而 status=0 / operation=0 在审计表里是有意义的取值边界 ——
//	用 int 就没法表达"只看失败"(0)了。
func queryOptionalInt(q auditQuery, key string) *int {
	v := q(key)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil
	}
	return &n
}

// queryOptionalBool 取可选 bool query 参数
// ("true"/"1" → true;"false"/"0" → false;其余 → nil = 不筛)
func queryOptionalBool(q auditQuery, key string) *bool {
	switch q(key) {
	case "true", "1":
		v := true
		return &v
	case "false", "0":
		v := false
		return &v
	}
	return nil
}

// buildOperationLogQuery 操作日志查询条件
func buildOperationLogQuery(q auditQuery, caller *service.Caller) service.OperationLogQuery {
	return service.OperationLogQuery{
		UserID:  auditScopeUserID(caller, auditQueryUint(q, "userId")),
		Module:  q("module"),
		Action:  q("action"),
		IP:      q("ip"),
		Keyword: q("keyword"),
		Status:  queryOptionalInt(q, "status"),
		StartAt: auditQueryInt64Ptr(q, "startAt"),
		EndAt:   auditQueryInt64Ptr(q, "endAt"),
	}
}

// buildDataChangeLogQuery 数据变更日志查询条件
func buildDataChangeLogQuery(q auditQuery, caller *service.Caller) service.DataChangeLogQuery {
	return service.DataChangeLogQuery{
		TableName: q("tableName"),
		RecordID:  auditQueryUint(q, "recordId"),
		UserID:    auditScopeUserID(caller, auditQueryUint(q, "userId")),
		Keyword:   q("keyword"),
		Operation: queryOptionalInt(q, "operation"),
		StartAt:   auditQueryInt64Ptr(q, "startAt"),
		EndAt:     auditQueryInt64Ptr(q, "endAt"),
	}
}

// buildLoginLogQuery 登录日志查询条件
func buildLoginLogQuery(q auditQuery, caller *service.Caller) service.LoginLogQuery {
	return service.LoginLogQuery{
		UserID:  auditScopeUserID(caller, auditQueryUint(q, "userId")),
		IP:      q("ip"),
		Keyword: q("keyword"),
		Success: queryOptionalBool(q, "success"),
		StartAt: auditQueryInt64Ptr(q, "startAt"),
		EndAt:   auditQueryInt64Ptr(q, "endAt"),
	}
}

// buildSystemLogQuery 系统运行日志查询条件(2026-11-03)
//
// 不接 userId 筛选 —— 系统日志的主视角是"什么时间 / 哪个接口 / 什么错",
// 操作人只是记录里的一个线索列,不走 auditScopeUserID 那套"只看自己"收窄。
func buildSystemLogQuery(q auditQuery) service.SystemLogQuery {
	return service.SystemLogQuery{
		Level:   q("level"),
		Source:  q("source"),
		Keyword: q("keyword"),
		StartAt: auditQueryInt64Ptr(q, "startAt"),
		EndAt:   auditQueryInt64Ptr(q, "endAt"),
	}
}

// ListOperationLogs GET /rapid-admin/v1/system/audit/operation-logs
//
// Phase 1.7 IDOR 修复(2026-10-17):
//   - 旧实现:从 query 取 userId 直接传 service,任何有 audit:operation 权限的人都可查任意人操作日志
//   - 新实现:仅 super_admin / 风控合规可指定 userId;其他角色强制 userId = caller.UserID(只看自己)
//   - 注:ListFullInfoViews 更严,需 super_admin(脱敏旁路审计,极敏感)
//
// ⚠️ **2026-10-30 补齐"前端发了但后端不读"的筛选参数**:
//
//	keyword / status / startAt / endAt —— service 层早就实现了这四个,
//	handler 却一直没往 query 里塞 ⇒ 页面上的"搜索"框和"成功/失败"筛选
//	**永远是摆设**(HTTP 200,结果不变)。现在接上了。
func (h *AuditHandler) ListOperationLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q := buildOperationLogQuery(c.Query, caller)
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListOperationLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListDataChangeLogs GET /rapid-admin/v1/system/audit/data-change-logs
//
// Phase 1.7 IDOR 修复:同 ListOperationLogs
func (h *AuditHandler) ListDataChangeLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q := buildDataChangeLogQuery(c.Query, caller)
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListDataChangeLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListLoginLogs GET /rapid-admin/v1/system/audit/login-logs
//
// Phase 1.7 IDOR 修复:同 ListOperationLogs
func (h *AuditHandler) ListLoginLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q := buildLoginLogQuery(c.Query, caller)
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListLoginLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListSystemLogs GET /rapid-admin/v1/system/audit/system-logs
//
// 系统运行日志(2026-11-03):慢 SQL / 慢接口 / 5xx / panic。
// 无 IDOR 面 —— 不按 userId 收窄,记录里只有操作人线索列;
// 权限由路由上的 rbac("audit:system", "list") 控制。
func (h *AuditHandler) ListSystemLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q := buildSystemLogQuery(c.Query)
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListSystemLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListFullInfoViews GET /rapid-admin/v1/system/audit/full-info-views
//   - 旧实现:接受任意 userId/ownerId,任何有 audit:full-info 权限的人都可查脱敏旁路记录
//   - 新实现:仅 super_admin / role_compliance 可调用;非特权角色直接 403
//   - 这是「看脱敏明文」审计,泄露 = 旁路整条脱敏防线,需更严格
func (h *AuditHandler) ListFullInfoViews(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	if !caller.HasRole("role_super_admin") && !caller.HasRole("role_compliance") {
		h.FailCode(c, 40300, "脱敏旁路审计仅超管/合规可查")
		return
	}
	q := service.FullInfoViewQuery{UserID: h.MustUint(c, "userId", 0), OwnerID: h.MustUint(c, "ownerId", 0), Field: c.Query("field")}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListFullInfoViews(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

func (h *AuditHandler) ExportOperationLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q := service.OperationLogQuery{Module: c.Query("module"), Action: c.Query("action")}
	key, err := h.Services.Audit.ExportOperationLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	_ = caller
	h.OK(c, map[string]any{"key": key})
}

// ListPIIAccessLogs GET /rapid-admin/v1/system/audit/pii-access-logs
//
// §1.5 #5 P0 —— 框架级 PII 字段解密访问日志
// 用途:谁在什么时间看了哪个手机号/身份证/银行卡
// 权限:仅 super_admin / role_compliance 可查(脱敏旁路审计极敏感,等同 ListFullInfoViews)
func (h *AuditHandler) ListPIIAccessLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	if !caller.HasRole("role_super_admin") && !caller.HasRole("role_compliance") {
		h.FailCode(c, 40300, "PII 访问审计仅超管/合规可查")
		return
	}
	q := service.PIIAccessLogQuery{
		UserID:       h.MustUint(c, "userId", 0),
		ResourceType: c.Query("resourceType"),
		ResourceID:   h.MustUint(c, "resourceId", 0),
		Field:        c.Query("field"),
		Action:       c.Query("action"),
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListPIIAccessLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListSmsSendLogs GET /rapid-admin/v1/system/audit/sms-send-logs
//
// §1.9 #7 P1 —— 短信发送审计(成功 + 失败)
// 用途:等保审计 / 故障排查(阿里云错误码 / 响应时长)
// 权限:仅 super_admin / role_compliance
func (h *AuditHandler) ListSmsSendLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	if !caller.HasRole("role_super_admin") && !caller.HasRole("role_compliance") {
		h.FailCode(c, 40300, "短信发送审计仅超管/合规可查")
		return
	}
	successStr := c.Query("success")
	var success *bool
	if successStr == "true" || successStr == "1" {
		v := true
		success = &v
	} else if successStr == "false" || successStr == "0" {
		v := false
		success = &v
	}
	q := service.SmsSendLogQuery{
		Phone:    c.Query("phone"),
		Scene:    c.Query("scene"),
		Provider: c.Query("provider"),
		Success:  success,
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListSmsSendLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListCryptoAuditLogs GET /rapid-admin/v1/system/audit/crypto-logs
//
// §1.5 #8 P1 —— 密钥使用审计
// 用途:谁在什么时间用哪个 key 加密/解密了什么资源
// 权限:仅 super_admin / role_compliance(密钥使用审计,等保要求)
func (h *AuditHandler) ListCryptoAuditLogs(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	if !caller.HasRole("role_super_admin") && !caller.HasRole("role_compliance") {
		h.FailCode(c, 40300, "密钥使用审计仅超管/合规可查")
		return
	}
	successStr := c.Query("success")
	var success *bool
	if successStr == "true" || successStr == "1" {
		v := true
		success = &v
	} else if successStr == "false" || successStr == "0" {
		v := false
		success = &v
	}
	q := service.CryptoAuditLogQuery{
		UserID:       h.MustUint(c, "userId", 0),
		KeyID:        c.Query("keyId"),
		Operation:    c.Query("operation"),
		Algorithm:    c.Query("algorithm"),
		ResourceType: c.Query("resourceType"),
		ResourceID:   h.MustUint(c, "resourceId", 0),
		Success:      success,
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListCryptoAuditLogs(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// ListDataExports GET /rapid-admin/v1/system/audit/data-exports
// 业务数据导出审计列表(userId / businessType / 时间范围)
//
// Phase 1.7 IDOR 修复(2026-10-17):仅 super_admin / role_compliance 可指定 userId;
// 其他角色强制 userId = caller.UserID(只能看自己的导出记录)
func (h *AuditHandler) ListDataExports(ctx context.Context, c *app.RequestContext) {
	caller := h.rc(c)
	if caller == nil {
		return
	}
	q := service.DataExportLogQuery{
		UserID:       h.MustUint(c, "userId", 0),
		BusinessType: c.Query("businessType"),
	}
	if q.UserID > 0 && !caller.HasRole("role_super_admin") && !caller.HasRole("role_compliance") {
		q.UserID = caller.UserID
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.Audit.ListDataExports(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}
