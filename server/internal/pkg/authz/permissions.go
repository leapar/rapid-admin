// 权限码匹配工具 — 简化版 keyMatch2
// 文档:后台权限模型设计V1.0.md §8.2
//
// 用法:RBAC 中间件(grant 路径)+ ResourceAssertor(grant 路径)共享同一匹配规则
// 规则:
//   - "*" 匹配所有
//   - "<prefix>:*" 匹配 "<prefix>:任何"
//   - 其余精确匹配
package authz

import "strings"

// MatchPermissionCode 公开 API(rbac.go 也用这个,避免包内重复)
// pattern 可能是 "*" / "house:*" / "house:view" / "user:delete"
// target  是实际请求的权限码,通常是 "<module>:<action>"
func MatchPermissionCode(pattern, target string) bool {
	if pattern == "*" || pattern == target {
		return true
	}
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*")
		return strings.HasPrefix(target, prefix+":")
	}
	return false
}

// matchPermissionCode 包内别名(供 assertor 内部使用,保持向后兼容)
func matchPermissionCode(pattern, target string) bool {
	return MatchPermissionCode(pattern, target)
}
