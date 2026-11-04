package config

// ClientIPConfig 客户端 IP 解析配置(2026-10-28 M105 新增)
//
// # 为什么需要这个开关
//
// Hertz 默认**信任所有反向代理**(TrustedCIDRs = 0.0.0.0/0),并且在"全信任"
// 前提下取 X-Forwarded-For 的**最左**项 —— 那一项是客户端自己填的。
// 于是:
//
//   - 按 IP 的限流(`RateLimit(..., "ip")`)可以被一个请求头击穿:
//     每次换一个 X-Forwarded-For 就是一个新桶;
//   - 审计表里的 ip 列(等保项)记的是攻击者自己填的值。
//
// 本配置把"哪些网段算可信反向代理"显式化。详见
// `internal/api/middleware/clientip.go` 的文件头。
type ClientIPConfig struct {
	// TrustedCIDRs 可信反向代理的网段(CIDR 列表)。
	//
	//   - 留空 → 用内置默认:回环 + RFC1918 私有段
	//     (127.0.0.0/8、::1/128、10.0.0.0/8、172.16.0.0/12、192.168.0.0/16)
	//   - 只有当对端(RemoteAddr)落在这些网段里,才采信 X-Forwarded-For;
	//     否则一律用对端地址。
	//
	// ⚠️ 反向代理挂在**公网 IP** 上时(云 LB 直连应用),对端不在默认网段 →
	// X-Forwarded-For 被忽略 → 所有请求看起来都来自那台 LB →
	// 按 IP 的限流退化成全局限流。此时把它的网段写进来。
	//
	// 想**完全不信任** X-Forwarded-For,写一个匹配不到任何地址的网段
	// (例如 "0.0.0.0/32")即可。
	TrustedCIDRs []string `mapstructure:"trusted_cidrs"`
}
