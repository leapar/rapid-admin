// Package apiverx API 版本协商工具(§1.3 #19 P2)
//
// 目的:
//   - 把 HTTP header `X-API-Version` 解析成 int,挂到 ctx 上
//   - handler 用 apiverx.Major(ctx) 读,做版本分支
//   - 与 middleware/APIVersion 配合:中间件只解析 + 注入,不强制校验
//
// 设计要点:
//   - 默认版本 1(无 header → 视为 1,保持向后兼容)
//   - 支持 "v2" / "2" / "V2" 等大小写/前缀写法
//   - 解析失败 → 视为默认版本,不报错(中间件层容错;严格校验由 Opt 注入)
//
// 用法(在 router.go):
//
//	h.Use(middleware.APIVersion())            // 默认仅注入
//	h.Use(middleware.APIVersion(apiverx.WithMaxMajor(3))) // 超 3 返 422
//
// handler:
//
//	if apiverx.Major(ctx) >= 2 { /* 新逻辑 */ } else { /* 老逻辑 */ }
package apiverx

import "context"

// DefaultVersion 无 X-API-Version header 时使用的版本
const DefaultVersion = 1

// HeaderName 标准 header 名(也支持小写;HTTP header 不分大小写)
const HeaderName = "X-API-Version"

type ctxKey struct{}

// WithVersion 注入版本到 ctx(handler 一般不调,中间件用)
func WithVersion(parent context.Context, v int) context.Context {
	return context.WithValue(parent, ctxKey{}, v)
}

// Major 从 ctx 读 major 版本(缺失 → DefaultVersion)
//
//	apiverx.Major(ctx)  // 1
//	apiverx.Major(ctx)  // 2
func Major(ctx context.Context) int {
	if v, ok := ctx.Value(ctxKey{}).(int); ok {
		return v
	}
	return DefaultVersion
}

// Parse 从 raw header 解析 major version
//   - "" → DefaultVersion, ok
//   - "1" / "2" / ... → int, ok
//   - "v1" / "V2" / ... → 去前缀解析
//   - "1.2" / "abc" → DefaultVersion, !ok(让调用方决定是否拒绝)
func Parse(raw string) (int, bool) {
	if raw == "" {
		return DefaultVersion, true
	}
	// 去前缀 'v' / 'V'
	s := raw
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	if s == "" {
		return DefaultVersion, false
	}
	// 简单 ASCII 数字解析(避免 strconv 错误信息噪音)
	//   - 遇 '.' 立即停止(major 段足够,"1.2" → 1)
	//   - 遇非数字返 false
	n := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '.' {
			break
		}
		if ch < '0' || ch > '9' {
			return DefaultVersion, false
		}
		n = n*10 + int(ch-'0')
		if n > 1<<20 { // 防爆
			return DefaultVersion, false
		}
	}
	if n < 1 {
		return DefaultVersion, false
	}
	return n, true
}

// Opt 中间件配置
type Opt struct {
	// MaxMajor 支持的最大 major 版本;> 该值 → 中间件直接返 422
	//   0 表示不校验
	MaxMajor int
}

// WithMaxMajor 设置最大支持版本(> 该值的请求 → 422 拒绝)
func WithMaxMajor(n int) func(*Opt) {
	return func(o *Opt) { o.MaxMajor = n }
}
