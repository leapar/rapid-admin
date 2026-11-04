// M137 —— 真实链路回归:请求体里发出去的东西必须是对的
//
// 这份文件补的是 m78_test.go 末尾自己承认的那个盲区:
// "Put / Stat / List / DeletePrefix 的联网行为没测"。
//
// 为什么现在能测了:alioss SDK 的 endpoint 是可以指向**任意 http 地址**的
// (urlMaker 对 IP 走 path-style),所以可以用 httptest 起一个假 OSS 端点,
// 把 SDK 真正发出去的请求体抓下来断言。
//
// ⚠️ 这类测试的价值在于**它测的是"发出去的那一行"**,不是"我们调用了什么":
// 老实现的 Delete 里写的是 `url.PathEscape(k)`,而 OSS 的 DeleteObjects
// 把 key 放在 **XML 请求体**里(不是 URL 路径)⇒ OSS 收到的是一个不存在的
// key ⇒ "删不存在的 key"是幂等成功 ⇒ **返回 nil,对象还在桶里**。
// 这个 bug 在任何"只看返回值"的测试下都是绿的 —— 必须抓请求体才看得见。
package aliyun

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOSS 起一个假的 OSS 端点,把收到的请求记下来。
//
// ⚠️ **加锁不是多余的**:handler 跑在 httptest 的 goroutine 上,而断言在测试
// goroutine 上,不加锁 `-race` 会报数据竞争(而且是偶发的 —— 那种 flaky 的
// 红比稳定的红难查十倍)。
type fakeOSS struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
	urls   []string
}

func newFakeOSS(t *testing.T) *fakeOSS {
	t.Helper()
	f := &fakeOSS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(b))
		f.urls = append(f.urls, r.URL.String())
		f.mu.Unlock()
		// 根元素必须是 <DeleteResult> —— SDK 把它反序列化进
		// DeleteObjectVersionsResult(XMLName 是 `xml:"DeleteResult"`),
		// 根元素名字对不上会直接反序列化失败。
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult></DeleteResult>`))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeOSS) lastBody() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return ""
	}
	return f.bodies[len(f.bodies)-1]
}

func (f *fakeOSS) lastURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.urls) == 0 {
		return ""
	}
	return f.urls[len(f.urls)-1]
}

// newProviderAgainst 把 provider 指向假端点。
func newProviderAgainst(t *testing.T, ep string) *Provider {
	t.Helper()
	p, err := New(Config{
		Endpoint:     ep,
		AccessKey:    "ak",
		AccessSecret: "sk",
		Bucket:       "test-bucket",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// TestDelete_SendsRawKeyInXMLBody **Delete 必须把 key 原样发出去**
//
// ⚠️ **这是 M137 真机跑出来的第一个 bug**,而且是最阴的一个:
//
//	老实现:  DeleteObjects(url.PathEscape(k))
//	实际发出: <Key>rapid-admin%2F1%2Fasset%2F2%2Fa.pdf</Key>
//	OSS 看到: 一个**不存在的 key**
//	OSS 行为: 删不存在的 key = 幂等成功
//	我们看到:  err == nil(删成功了!)
//	真相:      **对象还在桶里,继续占存储费**
//
// 症状是"DB 里软删了、列表里看不见了、连 DeletePrefix 也能把整棵删干净",
// 唯独"删单个"悄悄不删 —— 两个删除路径行为不一致就是这条线索。
func TestDelete_SendsRawKeyInXMLBody(t *testing.T) {
	f := newFakeOSS(t)
	p := newProviderAgainst(t, f.URL)

	const key = "rapid-admin/1/asset/2/户型图.pdf"
	if err := p.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	body := f.lastBody()
	if body == "" {
		t.Fatal("没有收到任何请求 —— Delete 根本没发出去")
	}
	if !strings.Contains(body, "<Key>"+key+"</Key>") {
		t.Fatalf("请求体里没有原样的 key:\n%s", body)
	}
	// ⚠️ 分隔符被编码 ⇒ OSS 收到的是另一个 key ⇒ 删了个寂寞
	if strings.Contains(body, "%2F") {
		t.Fatalf("key 被 percent-encode 了,OSS 会当成另一个 key:\n%s", body)
	}
	// 也不能是"只有 ASCII 才对"的那种假断言:中文必须原样在(SDK 只做 XML 转义)
	if !strings.Contains(body, "户型图") {
		t.Fatalf("请求体里中文丢了:\n%s", body)
	}
	// 请求走的是 POST + ?delete(批量删除接口)
	if !strings.Contains(f.lastURL(), "delete") {
		t.Fatalf("请求 URL = %q,want 带 delete 参数", f.lastURL())
	}
}

// TestDelete_MultipleKeysRaw 一次删多个,每个 key 都得原样。
func TestDelete_MultipleKeysRaw(t *testing.T) {
	f := newFakeOSS(t)
	p := newProviderAgainst(t, f.URL)

	keys := []string{
		"rapid-admin/1/asset/2/a.pdf",
		"rapid-admin/1/asset/3/b.jpg",
		"rapid-admin/2/house/4/c.png",
	}
	if err := p.Delete(context.Background(), keys...); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	body := f.lastBody()
	for _, k := range keys {
		if !strings.Contains(body, "<Key>"+k+"</Key>") {
			t.Fatalf("请求体里少了 %q:\n%s", k, body)
		}
	}
}

// TestDelete_NoKeysNoRequest 没有 key 就别发请求(省一次往返 + 一次签名)。
func TestDelete_NoKeysNoRequest(t *testing.T) {
	f := newFakeOSS(t)
	p := newProviderAgainst(t, f.URL)

	if err := p.Delete(context.Background()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	f.mu.Lock()
	n := len(f.bodies)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("发了 %d 个请求,want 0", n)
	}
}

// ── endpoint 选择(内网 / 外网)─────────────────────────────────────────

// TestPickClientEndpoint 内网 endpoint 的选择规则。
//
// ⚠️ **两条硬约束**:
//  1. 内网优先生效,但**前提是这台机器连得上它** —— `-internal` 域名在公网上
//     **也能解析**(指向 VPC 内部的 100.64.0.0/10),但路由不通。
//     所以判据是**连通性**,不是"DNS 有没有解析出来"。
//  2. **返回值必须带 scheme** —— SDK 在 endpoint 没写 scheme 时默认按 http
//     处理(conn.go:InitExt),于是"配置没写 https"等于"悄悄降级成明文"。
func TestPickClientEndpoint(t *testing.T) {
	reachable := func(string) bool { return true }
	unreachable := func(string) bool { return false }

	cases := []struct {
		name      string
		pub, priv string
		probe     func(string) bool
		want      string
		wantPriv  bool
	}{
		{
			name: "内网连得上 → 用内网",
			pub:  "oss-cn-shenzhen.aliyuncs.com", priv: "oss-cn-shenzhen-internal.aliyuncs.com",
			probe: reachable, want: "https://oss-cn-shenzhen-internal.aliyuncs.com", wantPriv: true,
		},
		{
			name: "内网连不上 → 回落外网",
			pub:  "oss-cn-shenzhen.aliyuncs.com", priv: "oss-cn-shenzhen-internal.aliyuncs.com",
			probe: unreachable, want: "https://oss-cn-shenzhen.aliyuncs.com", wantPriv: false,
		},
		{
			name: "没配内网 → 外网",
			pub:  "oss-cn-shenzhen.aliyuncs.com", priv: "",
			probe: reachable, want: "https://oss-cn-shenzhen.aliyuncs.com", wantPriv: false,
		},
		{
			name: "外网没写 scheme → 补 https",
			pub:  "oss-cn-shenzhen.aliyuncs.com", priv: "",
			probe: reachable, want: "https://oss-cn-shenzhen.aliyuncs.com", wantPriv: false,
		},
		{
			name: "外网写了 http → 尊重用户",
			pub:  "http://oss-cn-shenzhen.aliyuncs.com", priv: "",
			probe: reachable, want: "http://oss-cn-shenzhen.aliyuncs.com", wantPriv: false,
		},
		{
			name: "内网写了 scheme → 不动它",
			pub:  "oss-cn-shenzhen.aliyuncs.com", priv: "http://oss-cn-shenzhen-internal.aliyuncs.com",
			probe: reachable, want: "http://oss-cn-shenzhen-internal.aliyuncs.com", wantPriv: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, priv := pickClientEndpoint(c.pub, c.priv, c.probe)
			if got != c.want {
				t.Errorf("endpoint = %q, want %q", got, c.want)
			}
			if priv != c.wantPriv {
				t.Errorf("usedPrivate = %v, want %v", priv, c.wantPriv)
			}
		})
	}
}

// TestProbeEndpointReachable_RejectsGarbage 探不通的地址不许被当成"内网可用"。
func TestProbeEndpointReachable_RejectsGarbage(t *testing.T) {
	for _, ep := range []string{"", "   ", "://"} {
		if probeEndpointReachable(ep) {
			t.Errorf("probeEndpointReachable(%q) = true,want false", ep)
		}
	}
}

// TestProbeEndpointReachable_BoundedBudget 探测**不许把启动拖慢**(M138)。
//
// ⚠️ **这条守的是"启动路径上的固定延迟"** —— 真机实测(开发机不在 VPC):
//
//	16:23:23.260  push 通道未配置                 ← 主流程到这里
//	16:23:24.796  内网 endpoint 连不上,已回落外网   ← 中间 1.536 秒
//
// `-internal` 域名在公网上**也能解析**(指向 VPC 内部的 100.64.0.0/10),
// 但路由不通 ⇒ TCP 握手一直等到超时。**每一次启动都白等一秒半**。
//
// ⚠️ 断言的是**耗时**,不是"返不返 false" —— 只断言返回值的话,
//
//	把预算从 300ms 调回 1500ms 这条用例照样绿(它只多等了 1.2 秒)。
func TestProbeEndpointReachable_BoundedBudget(t *testing.T) {
	// 192.0.2.0/24 是 RFC 5737 保留的文档用网段,公网不可路由 ⇒ 拨号要么被拒
	// (立刻失败)要么进黑洞(等到预算耗尽)。**两种都该判 false**。
	const unroutable = "192.0.2.1:443"

	start := time.Now()
	got := probeEndpointReachable(unroutable)
	elapsed := time.Since(start)

	if got {
		t.Fatalf("probeEndpointReachable(%q) = true,want false", unroutable)
	}
	// ⚠️ 留 3 倍余量:CI / 满载机器上调度抖动是常态,卡的是"秒级"而不是"毫秒级"。
	if elapsed > 3*endpointProbeTimeout {
		t.Errorf("探测耗时 %v,超过预算 %v 的 3 倍 —— 启动路径上不能这么等", elapsed, endpointProbeTimeout)
	}
	// ⚠️ **这条才是真正的守卫**:上一条是**相对**预算的 —— 把常量从 300ms
	//   调回 1500ms,上一条照样绿(1500 < 3×1500)。所以这里再钉一条**绝对**上限:
	//   **整个探测不许超过 1 秒**,超了就是"每次启动都被它拖住"。
	if elapsed > time.Second {
		t.Errorf("探测耗时 %v,超过 1 秒的绝对上限 —— 启动路径上不能被它拖住", elapsed)
	}
}

// TestProbeEndpointReachable_LocalListenerIsReachable 预算**不能砍到"真连得上也探不到"**。
//
// ⚠️ 这是上一条的反向守卫:预算砍太狠(比如 1ms)会让探测在 VPC 里也失败,
//
//	后果是**永远走外网** —— 不报错、不丢文件,只是**一直慢**,没人会发现。
//	所以必须有一条"确实连得上 ⇒ 判 true"的用例。
func TestProbeEndpointReachable_LocalListenerIsReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	// ⚠️ Accept 要有人在跑,不然探测的 dial 会撞 backlog(有时也能连上,但不稳)。
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	ep := ln.Addr().String() // "127.0.0.1:port"
	if !probeEndpointReachable(ep) {
		t.Fatalf("probeEndpointReachable(%q) = false,want true(本机监听器一定连得上)", ep)
	}
}

// TestEnsureScheme 补 scheme 的边界。
func TestEnsureScheme(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"oss-cn-hangzhou.aliyuncs.com":         "https://oss-cn-hangzhou.aliyuncs.com",
		"https://oss-cn-hangzhou.aliyuncs.com": "https://oss-cn-hangzhou.aliyuncs.com",
		"http://oss-cn-hangzhou.aliyuncs.com":  "http://oss-cn-hangzhou.aliyuncs.com",
		"  oss-cn-hangzhou.aliyuncs.com/ ":     "https://oss-cn-hangzhou.aliyuncs.com",
	}
	for in, want := range cases {
		if got := ensureScheme(in); got != want {
			t.Errorf("ensureScheme(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── 直链用外网,SDK 走内网 ────────────────────────────────────────────

// TestDirectLinkUsesPublicEndpoint **配了内网 endpoint,直链仍必须是外网**
//
// ⚠️ 直链是给**浏览器**看的,而浏览器在公网 —— 给它 `-internal` 地址它
// 根本访问不到(而症状是"上传成功、列表里点得开、打开是裂图 / 连不上")。
//
// ⚠️ 这条断言**不能依赖 DNS 能不能解析内网域名** —— 测试机在阿里云里
// 解析得了、在开发机上解析不了,两种环境都必须过。所以断言落在
// "直链的 host 是外网那个"上,而不是"客户端用了哪个"。
func TestDirectLinkUsesPublicEndpoint(t *testing.T) {
	p, err := New(Config{
		Endpoint:        "https://oss-cn-shenzhen.aliyuncs.com",
		EndpointPrivate: "oss-cn-shenzhen-internal.aliyuncs.com",
		AccessKey:       "ak",
		AccessSecret:    "sk",
		Bucket:          "test-bucket",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// 直链(下载)
	u, err := p.SignDownloadURL(context.Background(), "rapid-admin/1/a/2/b.pdf", time.Minute)
	if err != nil {
		t.Fatalf("SignDownloadURL: %v", err)
	}
	if strings.Contains(u, "-internal") {
		t.Fatalf("直链用了内网地址 %q —— 浏览器在公网,访问不到", u)
	}
	want := "https://test-bucket.oss-cn-shenzhen.aliyuncs.com/rapid-admin/1/a/2/b.pdf"
	if u != want {
		t.Fatalf("直链 = %q, want %q", u, want)
	}

	// 直传凭证的 Host 也是给浏览器用的
	tok, err := p.SignUploadToken(context.Background(), "rapid-admin/1/a/2/b.pdf", time.Minute, 1<<20)
	if err != nil {
		t.Fatalf("SignUploadToken: %v", err)
	}
	if strings.Contains(tok.Host, "-internal") {
		t.Fatalf("直传 Host 用了内网地址 %q", tok.Host)
	}
	if tok.Host != "https://test-bucket.oss-cn-shenzhen.aliyuncs.com" {
		t.Fatalf("直传 Host = %q", tok.Host)
	}
}
