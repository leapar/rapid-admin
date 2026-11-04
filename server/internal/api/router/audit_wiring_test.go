// 审计服务**装配**的守卫 —— M117(2026-10-30)
//
// # 这个洞是怎么漏掉的
//
// `middleware.AuditSvc` 是个包级全局。2026-10-30 查出来:
//
//	全仓**只有一处**给它赋值 —— `main.go` 里 `SetGlobal(..., nil)`,传的就是 nil。
//	之后再没有第二处赋值。
//
// 于是 audit 中间件里 `if AuditSvc == nil { return }` **每次都提前返回** ⇒
// **sys_operation_log 一行都没被中间件写过。**
//
// 症状是"页面正常、功能正常,只是操作日志永远是空的",而且**零报错**:
// 请求照常成功,中间件照常跑,只是最后那一句写库被跳过了。
//
// 当时的 dev 库里只有 11 行操作日志,全是 service 层直写的
// (switch-identity / revoke-sessions),没有一行来自中间件 ——
// 中间件的 `module` 字段是**路由路径**,那 11 行的 module 全是 `auth`,
// 一行路由路径都没有。这是对得上的。
//
// # 为什么这条测试盯的是「装配」而不是「中间件逻辑」
//
// 中间件逻辑本身另有一条测试(`middleware/audit_db_test.go`)。
// 但那条测试**自己装配 AuditSvc** —— 它证明不了生产里那个 AuditSvc 非 nil。
// 两条测试缺一不可:
//
//	· 只有中间件那条 → 它自己装自己,生产照样是 nil ⇒ 假绿
//	· 只有本条       → 中间件逻辑坏了没人知道
//
// # 可逆探针(拆掉实现看会不会红)
//
//	拆法                                              必须变红
//	────────────────────────────────────────────  ──────────────────────────
//	删掉 router.go 里的 SetAuditService(svc.Audit)  TestRegister_WiresAuditService
//	把 svc.Audit 换成 nil                            同上
package router

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/service"
)

// TestRegister_WiresAuditService Register 必须把审计服务装进中间件
func TestRegister_WiresAuditService(t *testing.T) {
	old := middleware.AuditSvc
	t.Cleanup(func() { middleware.AuditSvc = old })

	h := server.Default()
	// ⚠️ registry 必须**非 nil** —— svc == nil 时 Register 会跳过装配
	//   (那是单测"不装配审计"的场景)。用 nil registry 测这条 = 恒绿。
	// scope 传 nil —— 本用例只验审计中间件有没有被装上,不做行级过滤
	reg := &service.Registry{Audit: service.NewAuditService(nil, nil)}

	Register(h, minimalCfg(), reg, nil, nil)

	require.NotNil(t, middleware.AuditSvc,
		"Register 之后 AuditSvc 仍是 nil —— 审计中间件的 DB 轨会一直提前返回,"+
			"sys_operation_log 一行都不会有,而且**没有任何报错**")
}

// TestRegister_NilRegistrySkipsAudit 单测场景:registry 为 nil 时不装配、也不崩
func TestRegister_NilRegistrySkipsAudit(t *testing.T) {
	old := middleware.AuditSvc
	middleware.AuditSvc = nil
	t.Cleanup(func() { middleware.AuditSvc = old })

	h := server.Default()
	require.NotPanics(t, func() {
		Register(h, minimalCfg(), nil, nil, nil)
	})
	require.Nil(t, middleware.AuditSvc)
}
