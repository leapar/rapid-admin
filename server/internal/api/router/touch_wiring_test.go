// 会话活动心跳**装配**的守卫 —— M117-4(2026-10-30)
//
// # 这个洞是怎么漏掉的
//
// 心跳本身写对了、也测过了,但它要生效得先满足两个条件:
//
//  1. `middleware.AuthSvc` 非 nil(装配)
//  2. `router.Register` 真的调了 `SetAuthService(svc.Auth)`(接线)
//
// 这两件事**都不会编译报错**,也不会打日志 —— 少了任何一个,表现都是
// "审计中心「最后活动」那一列永远是空的",而且**零报错**。
//
// 与 `audit_wiring_test.go` 是同一类洞、同一类守卫:
// 中间件侧的单测**自己装配** AuthSvc(它证明不了生产里那个非 nil),
// 装配侧的单测证明不了中间件逻辑 —— 两条缺一不可。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	拆法                                             必须变红
//	─────────────────────────────────────────────  ──────────────────────────
//	删掉 router.go 里的 SetAuthService(svc.Auth)   TestRegister_WiresAuthService
//	把 svc.Auth 换成 nil                             同上
package router

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/service"
)

// TestRegister_WiresAuthService Register 必须把鉴权服务装进中间件
func TestRegister_WiresAuthService(t *testing.T) {
	old := middleware.AuthSvc
	t.Cleanup(func() { middleware.AuthSvc = old })

	h := server.Default()
	// ⚠️ registry 必须**非 nil** —— svc == nil 时 Register 会跳过装配。
	//   用 nil registry 测这条 = 恒绿(和 audit 那条同一个坑)。
	reg := &service.Registry{Auth: &fakeAuth{}}

	Register(h, minimalCfg(), reg, nil, nil)

	require.NotNil(t, middleware.AuthSvc,
		"Register 之后 AuthSvc 仍是 nil —— 会话活动心跳不会被触发,"+
			"last_active_at 只在续期时更新 ⇒ 会话建立后 8 小时内那列恒空,"+
			"而且**没有任何报错**")
}

// TestRegister_NilRegistrySkipsAuth 单测场景:registry 为 nil 时不装配、也不崩
func TestRegister_NilRegistrySkipsAuth(t *testing.T) {
	old := middleware.AuthSvc
	middleware.AuthSvc = nil
	t.Cleanup(func() { middleware.AuthSvc = old })

	h := server.Default()
	require.NotPanics(t, func() {
		Register(h, minimalCfg(), nil, nil, nil)
	})
	require.Nil(t, middleware.AuthSvc)
}

// fakeAuth 只实现 SessionToucher 需要的那一个方法
//
// Registry.Auth 的类型是 service.AuthService(二十来个方法),这里只给一个
// 最小实现 —— 因为本测试要验的只是"**装配有没有发生**",
// 不是 authService 的行为(行为另有 service 层的用例)。
type fakeAuth struct {
	service.AuthService
}

// TouchLoginSession 实现 service.AuthService 的心跳方法
func (f *fakeAuth) TouchLoginSession(_ context.Context, _ *service.Caller) error {
	return nil
}
