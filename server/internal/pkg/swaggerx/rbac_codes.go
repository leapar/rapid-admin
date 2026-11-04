// swaggerx —— 共享 RBAC code 状态
//
// router 包在 rbac() 闭包里记账,我们需要把这些 code 共享给 swaggerx 来生成
// x-rbac-code 扩展字段。采用函数注入模式,避免 swaggerx 反向 import router(import cycle)。
package swaggerx

var rbacCodesFn = func() map[string]bool { return nil }

// SetRBACCodesProvider 让 router 包在 Register 时注入,签名极简
//   - provider 必须是幂等的(thread-safe 可选,内部只在 Register 阶段读一次)
func SetRBACCodesProvider(fn func() map[string]bool) {
	if fn == nil {
		return
	}
	rbacCodesFn = fn
}

// GetRBACCodes swaggerx 内部取当前已注册的 RBAC code
func GetRBACCodes() map[string]bool {
	return rbacCodesFn()
}
