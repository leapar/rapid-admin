// 客户端 IP 解析 —— 限流 / 审计 / PII 关联链的共同底座
//
// # ⚠️ M105(2026-10-28)为什么改这里
//
// Hertz **默认**的 client-IP 解析有两个致命默认值:
//
//  1. `defaultTrustedCIDRs = 0.0.0.0/0 + ::/0` —— **信任所有对端**。
//     于是 `X-Forwarded-For` 被无条件采信。
//  2. 在"信任所有"的前提下,`validateHeader` 从右往左扫,遇到第一个 IP 就停 ——
//     但因为每个 IP 都"可信",它会一直扫到 **最左边**那一项才返回。
//
// 两条叠起来的后果(全部实测确认,不是推测):
//
//   - **按 IP 的限流可以被一个请求头击穿**。客户端发
//     `X-Forwarded-For: 1.2.3.4` 就能让这次请求落进 1.2.3.4 的桶,
//     再发一个 `2.3.4.5` 就是另一个桶 —— 每次请求一个新桶,
//     `RateLimit(scene, max, window, "ip")` **等于没挂**。
//     本项目里 `POST /public/auth/login` 的防爆破限流就建在这上面。
//   - **审计表里的 ip 列是攻击者可控的**。`sys_pii_access_log.ip`
//     (等保项,§1.5 #5)记的是"谁看了手机号",而这个"谁"由客户端自己填。
//     事故复盘时拿到的 IP 没有证据价值。
//
// # 修法
//
// 换成"**只信任明确列出的代理网段**"的解析器:
//
//   - 对端(RemoteAddr)不在可信网段 → **完全忽略** X-Forwarded-For,
//     用对端地址(直连场景下这就是真实客户端 IP)。
//   - 对端在可信网段 → 采信 X-Forwarded-For,但取**最右侧的不可信项**
//     (标准口径:代理把自己的对端地址追加在右边,一路往左跳过可信代理,
//     第一个不可信的就是真实客户端)。
//
// Hertz 自带的 `app.ClientIPWithOption` 已经是这个算法,只是默认 CIDRs 是
// "信任所有"。所以这里**不重写算法**,只换掉那份默认 CIDR 列表 ——
// 自己重写一遍只会引入第二套口径。
//
// # 默认可信网段
//
// 回环 + RFC1918 私有段(见 `defaultTrustedCIDRs`)。覆盖两种最常见部署:
//
//   - nginx 与应用同机(对端 127.0.0.1)
//   - nginx 与应用同内网(对端 10.x / 172.16-31.x / 192.168.x)
//
// ⚠️ **如果反向代理挂在公网 IP 上**(云 LB 直连应用),对端不在默认网段里 →
// X-Forwarded-For 会被忽略 → 所有请求看起来都来自那台 LB →
// **按 IP 的限流会退化成全局限流**。这种情况把它的网段写进
// `client_ip.trusted_cidrs` 即可(见 configs/config.example.yaml)。
// 解析器在启动时会打一条 WARN 把实际生效的网段列出来,便于对账。
package middleware

import (
	"net"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"
)

// defaultTrustedCIDRs 默认可信代理网段:回环 + RFC1918 私有段。
//
// 为什么不是"信任所有":信任所有 = 客户端可自选 IP = 限流形同虚设 +
// 审计 IP 可伪造(见文件头)。
// 为什么不是"只信回环":同网段的 nginx(LAN 部署)会被判成不可信 →
// XFF 被忽略 → 同样退化成全局限流。
var defaultTrustedCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
}

// ClientIPFunc 构造客户端 IP 解析函数。
//
//   - trustedCIDRs 为空 → 用 defaultTrustedCIDRs(回环 + 私有段)
//   - 传一个不存在的网段(如 "0.0.0.0/32")→ 等价于**完全不信任** X-Forwarded-For
//
// 解析失败的 CIDR 会被跳过并打 WARN —— 配错网段的后果是"限流退化成全局",
// 属于必须让人看见的那类错。
func ClientIPFunc(trustedCIDRs []string) app.ClientIP {
	cidrs, bad := parseTrustedCIDRs(trustedCIDRs)
	if len(cidrs) == 0 {
		// 一个都没解析出来 → 宁可**什么都不信**(用对端地址),
		// 也不要退回"信任所有" —— 后者等于把限流和审计一起废掉。
		L().Warn("client_ip: 可信代理网段全部解析失败,本次完全不采信 X-Forwarded-For",
			zap.Strings("invalid", bad))
		cidrs = nil
	} else if len(bad) > 0 {
		L().Warn("client_ip: 部分可信代理网段解析失败,已跳过",
			zap.Strings("invalid", bad),
			zap.Strings("effective", cidrsToStrings(cidrs)))
	}
	return app.ClientIPWithOption(app.ClientIPOptions{
		RemoteIPHeaders: []string{"X-Forwarded-For", "X-Real-IP"},
		TrustedCIDRs:    cidrs,
	})
}

// parseTrustedCIDRs 解析 CIDR 列表,返回可用的网段与解析失败的原文。
func parseTrustedCIDRs(list []string) (cidrs []*net.IPNet, bad []string) {
	if len(list) == 0 {
		list = defaultTrustedCIDRs
	}
	cidrs = make([]*net.IPNet, 0, len(list))
	for _, raw := range list {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			bad = append(bad, s)
			continue
		}
		cidrs = append(cidrs, ipnet)
	}
	return cidrs, bad
}

func cidrsToStrings(list []*net.IPNet) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		out = append(out, n.String())
	}
	return out
}
