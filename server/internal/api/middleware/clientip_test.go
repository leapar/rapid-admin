// 客户端 IP 解析单测 —— 伪造 X-Forwarded-For 必须无效
//
// # 本文件存在的理由
//
// M105 审计 §1.3 时发现 Hertz **默认**的 client-IP 解析有两个致命默认值:
//
//  1. `defaultTrustedCIDRs = 0.0.0.0/0 + ::/0` —— **信任所有对端**,
//     于是 `X-Forwarded-For` 被无条件采信。
//  2. 在"信任所有"的前提下,`validateHeader` 从右往左扫、遇到第一个不可信
//     项就返回 —— 但因为每个 IP 都"可信",它会一路扫到**最左边**那一项。
//
// 两条叠起来的后果:
//
//   - **按 IP 的限流可以被一个请求头击穿**。客户端每换一个 XFF 就是换一个桶,
//     `RateLimit(scene, max, window, "ip")` 等于没挂 —— 而登录防爆破就建在这上面。
//   - **审计表里的 ip 列是攻击者可控的**。`sys_pii_access_log.ip`
//     (等保项)记的是"谁看了手机号",而这个"谁"由客户端自己填。
//
// M105 把可信代理网段改成"**只信任明确列出的网段**"。这些用例守的是:
//   - 对端不在可信网段 → XFF **完全忽略**
//   - 对端在可信网段 → 取**最右侧的不可信项**(标准口径)
//   - 网段配错 → 信任零个(**不是**退回"信任所有")
//
// 顺带钉住"对照组":Hertz 的默认解析器确实会把攻击者填的 IP 当成客户端 IP ——
// 那是这次改动的全部理由,不能只钉住"我们改成对的"。
package middleware

import (
	"net"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/network"
)

// fakeConn 只实现 RemoteAddr —— 客户端 IP 解析器只用这一个方法。
//
// 其余方法靠嵌入的 nil 接口(真被调用会 panic)。这是刻意的:
// 一旦解析器开始依赖连接上的别的东西,测试会立刻炸,而不是悄悄给出一个错的地址。
type fakeConn struct {
	network.Conn
	remote net.Addr
}

func (f *fakeConn) RemoteAddr() net.Addr { return f.remote }

// ctxWithRemote 构造一个"对端地址为 ip"的请求上下文
func ctxWithRemote(ip string) *app.RequestContext {
	c := app.NewContext(0)
	c.SetConn(&fakeConn{remote: &net.TCPAddr{IP: net.ParseIP(ip), Port: 12345}})
	return c
}

// hertzDefaultClientIP 是 Hertz 的默认解析器 —— 改动前的行为,留着当对照组
func hertzDefaultClientIP() app.ClientIP {
	_, all, _ := net.ParseCIDR("0.0.0.0/0")
	_, allV6, _ := net.ParseCIDR("::/0")
	return app.ClientIPWithOption(app.ClientIPOptions{
		RemoteIPHeaders: []string{"X-Forwarded-For", "X-Real-IP"},
		TrustedCIDRs:    []*net.IPNet{all, allV6},
	})
}

// TestClientIP_SpoofedForwardedForIsIgnored 是 M105 的核心一条
//
// 客户端直连(对端是公网地址,不在可信代理网段里)时发一个
// X-Forwarded-For,服务端**不能**把它当成客户端 IP。
func TestClientIP_SpoofedForwardedForIsIgnored(t *testing.T) {
	resolve := ClientIPFunc(nil) // 默认可信网段 = 回环 + RFC1918

	c := ctxWithRemote("203.0.113.9")
	c.Request.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := resolve(c)
	if got == "1.2.3.4" {
		t.Fatal("伪造的 X-Forwarded-For 被当成了客户端 IP —— 按 IP 的限流可以被一个请求头击穿")
	}
	if got != "203.0.113.9" {
		t.Fatalf("客户端 IP = %q, want 203.0.113.9(对端地址)", got)
	}
}

// TestClientIP_HertzDefaultAcceptsSpoofedHeader 是对照组
//
// 证明"改之前确实是坏的":Hertz 的默认解析器会把攻击者填的 IP 原样采信。
// 这条用例的价值在于:将来有人把 ClientIPFunc 换回 Hertz 默认,这里会红。
func TestClientIP_HertzDefaultAcceptsSpoofedHeader(t *testing.T) {
	c := ctxWithRemote("203.0.113.9")
	c.Request.Header.Set("X-Forwarded-For", "1.2.3.4")

	got := hertzDefaultClientIP()(c)
	if got != "1.2.3.4" {
		t.Fatalf("Hertz 默认解析器 = %q, want 1.2.3.4 —— Hertz 的行为变了,本用例失去意义", got)
	}
}

// TestClientIP_TrustedProxyUsesRightmostUntrustedEntry 可信代理场景下的正确口径
//
// 对端是 nginx(127.0.0.1,在默认可信网段里)→ 采信 XFF,
// 但取的是**最右侧的不可信项**,不是最左边那一项。
//
// 最左边那一项是**攻击者可以随便填的**(每个客户端自己往 XFF 前面塞)。
func TestClientIP_TrustedProxyUsesRightmostUntrustedEntry(t *testing.T) {
	resolve := ClientIPFunc(nil)

	cases := []struct {
		name string
		xff  string
		want string
	}{
		{"单条", "1.2.3.4", "1.2.3.4"},
		// 攻击者自己往左塞了一项 → 必须取最右侧那个
		{"带伪造前缀", "9.9.9.9, 1.2.3.4", "1.2.3.4"},
		// 多级代理:10.0.0.5 是内网代理(可信),跳过它继续往左
		{"多级代理", "1.1.1.1, 2.2.2.2, 10.0.0.5", "2.2.2.2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ctxWithRemote("127.0.0.1")
			c.Request.Header.Set("X-Forwarded-For", tc.xff)
			if got := resolve(c); got != tc.want {
				t.Fatalf("XFF %q → 客户端 IP %q, want %q", tc.xff, got, tc.want)
			}
		})
	}
}

// TestClientIP_TrustedProxyStillIgnoresForgedLeftmost 对照组(可信代理 + Hertz 默认)
//
// 同样是"9.9.9.9, 1.2.3.4",Hertz 的默认(信任所有)会取**最左边**的 9.9.9.9;
// 我们改成只信任明确网段之后取最右侧的 1.2.3.4。
func TestClientIP_TrustedProxyStillIgnoresForgedLeftmost(t *testing.T) {
	c := ctxWithRemote("127.0.0.1")
	c.Request.Header.Set("X-Forwarded-For", "9.9.9.9, 1.2.3.4")

	if got := hertzDefaultClientIP()(c); got != "9.9.9.9" {
		t.Fatalf("Hertz 默认解析器 = %q, want 9.9.9.9 —— Hertz 的行为变了,本用例失去意义", got)
	}
	if got := ClientIPFunc(nil)(c); got != "1.2.3.4" {
		t.Fatalf("M105 解析器 = %q, want 1.2.3.4", got)
	}
}

// TestClientIP_RealIPFallback 可信代理没给 XFF 时退到 X-Real-IP
func TestClientIP_RealIPFallback(t *testing.T) {
	resolve := ClientIPFunc(nil)

	c := ctxWithRemote("127.0.0.1")
	c.Request.Header.Set("X-Real-IP", "5.6.7.8")
	if got := resolve(c); got != "5.6.7.8" {
		t.Fatalf("客户端 IP = %q, want 5.6.7.8", got)
	}
}

// TestClientIP_MalformedForwardedForFallsBackToPeer XFF 里塞非 IP → 退回对端地址
func TestClientIP_MalformedForwardedForFallsBackToPeer(t *testing.T) {
	resolve := ClientIPFunc(nil)

	c := ctxWithRemote("127.0.0.1")
	c.Request.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := resolve(c); got != "127.0.0.1" {
		t.Fatalf("客户端 IP = %q, want 127.0.0.1", got)
	}
}

// TestClientIP_BrokenCIDRMeansTrustNobody 网段配错 → 信任零个,不是信任所有
//
// 这是最危险的降级方向:配错网段如果退回"信任所有",等于限流和审计一起废掉,
// 而且**看不出来**(请求照常成功)。
func TestClientIP_BrokenCIDRMeansTrustNobody(t *testing.T) {
	resolve := ClientIPFunc([]string{"这不是一个CIDR"})

	c := ctxWithRemote("127.0.0.1")
	c.Request.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := resolve(c); got == "1.2.3.4" {
		t.Fatal("网段配错之后仍然采信了 X-Forwarded-For —— 降级方向反了")
	}
	if got := resolve(c); got != "127.0.0.1" {
		t.Fatalf("客户端 IP = %q, want 127.0.0.1(退回对端地址)", got)
	}
}

// TestClientIP_ExplicitTrustAllIsAllowed 显式配置"信任所有"仍然允许
//
// 云 LB 直连应用的部署确实需要这个(见 clientip.go 的文件头)。
// 显式配置 = 运维知道自己在做什么,与"默认值信任所有"是两回事。
func TestClientIP_ExplicitTrustAllIsAllowed(t *testing.T) {
	resolve := ClientIPFunc([]string{"0.0.0.0/0"})

	c := ctxWithRemote("203.0.113.9")
	c.Request.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := resolve(c); got != "1.2.3.4" {
		t.Fatalf("显式配置 0.0.0.0/0 时客户端 IP = %q, want 1.2.3.4", got)
	}
}

// TestParseTrustedCIDRs 单测:空列表用默认 / 混合列表跳过坏的
func TestParseTrustedCIDRs(t *testing.T) {
	// 空 → 默认
	cidrs, bad := parseTrustedCIDRs(nil)
	if len(cidrs) != len(defaultTrustedCIDRs) {
		t.Fatalf("空列表 → %d 个网段, want %d(默认)", len(cidrs), len(defaultTrustedCIDRs))
	}
	if len(bad) != 0 {
		t.Fatalf("空列表不该有解析失败的项:%v", bad)
	}

	// 混合 → 好的留下,坏的报出来
	cidrs, bad = parseTrustedCIDRs([]string{"10.0.0.0/8", "乱写", "  ", "192.168.0.0/16"})
	if len(cidrs) != 2 {
		t.Fatalf("有效网段数 = %d, want 2", len(cidrs))
	}
	if len(bad) != 1 || bad[0] != "乱写" {
		t.Fatalf("解析失败的项 = %v, want [乱写]", bad)
	}
}

// TestCIDRsToStrings 打印用的网段列表不能为空(启动日志要能对账)
func TestCIDRsToStrings(t *testing.T) {
	cidrs, _ := parseTrustedCIDRs([]string{"10.0.0.0/8"})
	got := cidrsToStrings(cidrs)
	if len(got) != 1 || got[0] != "10.0.0.0/8" {
		t.Fatalf("cidrsToStrings = %v", got)
	}
}
