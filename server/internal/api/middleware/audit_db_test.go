// audit 中间件的 **DB 轨**单测 —— M117(2026-10-30)
//
// # 这条测试盯的是什么
//
// `audit_test.go` 里的四条用例**全部把 `AuditSvc` 设成 nil** ——
// 它们盯的是**文件轨**(audit.log)。于是有一个洞没人盯:
//
//	**DB 轨(sys_operation_log)从来没有被测过。**
//
// 而 2026-10-30 查出来的正是这个洞的**本体**:
//
//	`middleware.AuditSvc` 全仓再没有第二处赋值,永远是 nil ⇒
//	中间件里 `if AuditSvc == nil { return }` 每次提前返回 ⇒
//	**sys_operation_log 一行都没被中间件写过。**
//
// dev 库里那 11 行全是 service 层直写的(switch-identity / revoke-sessions),
// 没有一行来自中间件 —— 这跟"中间件的 module 字段 = 路由路径"对得上:
//
//	11 行的 module 全是 `auth`,没有一行是路由路径。
//
// # 为什么这条测试**不能**只测中间件逻辑
//
// 光测"中间件在 AuditSvc 非 nil 时会写一行"是**不够的** ——
// 它在自己构造的 AuditSvc 上当然是绿的,而生产里那个 AuditSvc 恰恰是 nil。
//
// 所以守卫分成两半,缺一不可:
//
//	① 本文件:中间件逻辑本身(有服务 → 真的落一行,字段对不对)
//	② router/audit_wiring_test.go:装配(Register 之后 AuditSvc 非 nil)
//
// ① 单独存在 ⇒ 假绿(它自己装配了自己)
// ② 单独存在 ⇒ 中间件逻辑坏了没人知道
//
// # 可逆探针(拆掉实现看会不会红)
//
//	拆法                                                必须变红
//	──────────────────────────────────────────────  ─────────────────────────
//	把 Audit() 里的 RecordOperation 整段删掉        TestAudit_WritesToOperationLog
//	把 RecordOperation 挪到 AuditSvc == nil 之后     同上
//	把中间件的 Username 改成写死的 "x"               同上(字段断言)
//	把 router 的 SetAuditService(svc.Audit) 删掉     TestRegister_WiresAuditService
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// m117AuditDB 建一个只含 sys_operation_log 的内存库
func m117AuditDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysOperationLog{}))
	return db
}

// TestAudit_WritesToOperationLog DB 轨必须真的落一行,且字段对得上
func TestAudit_WritesToOperationLog(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	// ⚠️ **必须挂 Trace()** —— 生产里 request_id 是它写进**请求头**的
	//   (middleware/trace.go 的 c.Request.Header.Set),
	//   审计中间件读的就是那个请求头。不挂 Trace 的话这一列恒空,
	//   断言它"有值"就是拿一个假前提在测。
	e.Use(Trace())
	e.Use(obsWithCaller(&service.Caller{
		UserID:   42,
		Username: "alice",
		RoleCode: "admin",
	}))
	e.Use(Audit())
	e.POST("/m117/audit/create", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})

	w := ut.PerformRequest(e, "POST", "/m117/audit/create", nil)
	require.Equal(t, http.StatusOK, w.Code)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1, "写操作必须在 sys_operation_log 落一行")

	got := rows[0]
	require.Equal(t, uint64(42), got.UserID)
	require.Equal(t, "alice", got.Username, "用户名列必须写进去 —— 审计中心那一列靠它")
	require.Equal(t, "admin", got.UserRole)
	require.Equal(t, "/m117/audit/create", got.Module)
	require.Equal(t, "POST", got.Action)
	require.Equal(t, 1, got.Status, "2xx 记成功")
	// request_id 要跟响应头里那个是同一个 —— 这是"拿一条审计记录去查日志"的接线
	require.Equal(t, w.Header().Get(response.HeaderRequestID), got.RequestID)
	require.NotEmpty(t, got.RequestID)
}

// TestAudit_FailedRequestRecordedAsFailure 4xx/5xx 要记成失败
func TestAudit_FailedRequestRecordedAsFailure(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 7, Username: "bob", RoleCode: "ops"}))
	e.Use(Audit())
	e.DELETE("/m117/audit/remove", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusInternalServerError, "boom")
	})

	w := ut.PerformRequest(e, "DELETE", "/m117/audit/remove", nil)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, 0, rows[0].Status, "5xx 必须记成失败")
}

// TestAudit_ReadMethodsNotInOperationLog 读操作不进操作日志
//
// ⚠️ 跟文件轨那条(`TestAudit_SkipsReadMethods`)是一回事,但落在 DB 轨上 ——
//
//	只测文件轨的话,DB 轨多记/少记没人知道。
func TestAudit_ReadMethodsNotInOperationLog(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 7, Username: "bob", RoleCode: "ops"}))
	e.Use(Audit())
	e.GET("/m117/audit/read", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "GET", "/m117/audit/read", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Empty(t, rows, "读操作不该进操作日志")
}

// TestAudit_WritesRequestURL request_url 必须记**完整请求 URI**
//
// # 这一列跟 module 的区别是什么
//
//	module      = `c.Path()`      —— 请求路径,**不含 query**
//	request_url = `c.URI()` 完整  —— 路径 **+ query**
//
//	差的就是 query 那一段:`?userId=123` / `?force=1` 这种
//	"这次到底动了哪个对象"的信息**只在 query 里**。
//
// 这一列此前**恒空**(2026-10-30 补)。
func TestAudit_WritesRequestURL(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.PUT("/m117/audit/items/:id", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "PUT", "/m117/audit/items/9527?force=1", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	got := rows[0]

	require.Equal(t, "/m117/audit/items/9527", got.Module, "module 记的是请求路径(不含 query)")
	require.Contains(t, got.RequestURL, "/m117/audit/items/9527")
	require.Contains(t, got.RequestURL, "force=1", "query 也要带上 —— 这是 module 没有的那部分")
	require.Equal(t, "/m117/audit/items/9527?force=1", got.RequestURL)
}

// TestAudit_RequestMethodLeftEmpty request_method 故意不写
//
// 它跟 `action` 是同一个值(HTTP 方法),写一遍就是冗余副本。
// 这条测试把这个"故意空着"钉住 —— 免得以后有人"顺手补上"然后
// 两列开始各说各话(改了一处忘了另一处)。
func TestAudit_RequestMethodLeftEmpty(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.POST("/m117/audit/one", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "POST", "/m117/audit/one", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "POST", rows[0].Action, "HTTP 方法记在 action 上")
	require.Equal(t, "", rows[0].RequestMethod, "request_method 故意不写(与 action 完全同值)")
}

// TestAudit_WritesErrorMsg 失败请求要记下错误文案
//
// 这一列此前**恒空**(2026-10-30 补)—— 抽屉里「错误信息」那一行永远是空的。
func TestAudit_WritesErrorMsg(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.POST("/m117/audit/boom", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(http.StatusBadRequest, response.Response{
			Code:    40000,
			Message: "缺少必填参数 name",
		})
	})
	ut.PerformRequest(e, "POST", "/m117/audit/boom", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, 0, rows[0].Status, "4xx 记失败")
	require.Equal(t, "缺少必填参数 name", rows[0].ErrorMsg)
}

// TestAudit_ErrorMsgEmptyOnSuccess 成功请求不该有错误文案
func TestAudit_ErrorMsgEmptyOnSuccess(t *testing.T) {
	db := m117AuditDB(t)

	old := AuditSvc
	AuditSvc = service.NewAuditService(db, nil)
	t.Cleanup(func() { AuditSvc = old })

	e := obsNewEngine()
	e.Use(obsWithCaller(&service.Caller{UserID: 42, Username: "alice", RoleCode: "admin"}))
	e.Use(Audit())
	e.POST("/m117/audit/ok", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	ut.PerformRequest(e, "POST", "/m117/audit/ok", nil)

	var rows []model.SysOperationLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "", rows[0].ErrorMsg, "成功请求不该有错误文案")
	require.Equal(t, 1, rows[0].Status)
}
