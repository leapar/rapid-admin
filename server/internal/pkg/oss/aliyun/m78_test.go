// M78 单测 —— X2(防盗链密钥)、X3(公开前缀分流)、PostObject key 精确匹配
package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	alioss "github.com/aliyun/aliyun-oss-go-sdk/oss"

	"rapid-admin/internal/pkg/oss"
)

// newTestProvider 造一个**不联网**的 Provider。
//
// ⚠️ 直接 &Provider{} 而不是 New():New 会真去 alioss.New(...) 建连接。
// 这里测的全是纯函数(前缀判定 / policy 构造 / HMAC),不需要真 bucket。
func newTestProvider(ak, secret string) *Provider {
	return &Provider{
		cfg: alioss.Config{
			Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
			AccessKeyID:     ak,
			AccessKeySecret: secret,
		},
		bucket:     &alioss.Bucket{},
		bucketName: "only-bucket",
		accessKey:  ak,
		// ⚠️ **publicHost 必须填** —— 直链是"host + key"拼出来的,
		// 漏了它拼出来的是 `https://only-bucket./xxx`(host 为空)。
		// 这个字段以前不存在(拼的是 cfg.Endpoint,那个带 scheme 会拼出
		// `https://only-bucket.https://oss-...` ⇒ M137 改成先规整再拼)。
		publicHost: "oss-cn-hangzhou.aliyuncs.com",
	}
}

// ── 单桶模型 ───────────────────────────────────────────────────────────

// TestSingleBucket_NoPrefixDispatch **存储侧只有一个桶,没有按前缀分流**。
//
// ⚠️ 这条盯的是模型本身。以前是双桶(admin 私有 + public 公共读),每个
// provider 操作都要先判"这个 key 属于哪个桶",于是 SignDownloadURL /
// Delete / List / DeletePrefix 各写一遍分流,前缀一改就分叉。
//
// 现在"公开/私有"不是访问控制 —— 私密只意味着外部用户猜不到 key。所以
// **只有一个桶**,任何 key 都落它。
func TestSingleBucket_NoPrefixDispatch(t *testing.T) {
	p := newTestProvider("ak", "sk")

	keys := []string{
		"res/public/1/asset/2/avatar.png",
		"res/1/asset/2/report.pdf",
		"res/1/asset/2/cover.jpg",
		"res/public-secret/1/x.pdf",
		"res/publicx/a",
		"res/publi",
	}
	for _, k := range keys {
		if got := p.pickBucket(k); got != p.bucket {
			t.Errorf("pickBucket(%q) 选错了桶 —— 单桶模型下所有 key 都落同一个", k)
		}
	}
}

// TestBucketsForPrefix 列举范围:单桶下恒为一个。
//
// ⚠️ 以前是"浅前缀要扫两个 bucket",只扫一个会表现为"文件夹里少了几个文件",
// 而且**没有任何报错**。现在桶只有一个,这条退化成了"不许返回 0 个"。
func TestBucketsForPrefix(t *testing.T) {
	p := newTestProvider("ak", "sk")

	for _, prefix := range []string{"", "res", "res/p", "res/1", "res/public/1"} {
		got := p.bucketsForPrefix(prefix)
		if len(got) != 1 || got[0] != p.bucket {
			t.Errorf("bucketsForPrefix(%q) = %v, want [唯一的桶]", prefix, got)
		}
	}
}

// ── 直链 ───────────────────────────────────────────────────────────────

// TestSignDownloadURL_StableDirectURL 直链**不签名、不带过期时间**。
//
// ⚠️ 为什么:前端把这个 URL 塞进 <img src>,签名 URL 一过期页面上的图就裂 ——
// 而文件列表页可能开一整天。直链稳定是刻意的。
//
// ⚠️ 代价:桶必须是公共读,私有桶上这个 URL 会 403。这是"单桶 + 全树公共读"
// 模型的必然结果,写在这里是为了让运维知道配桶 ACL 时该配成什么。
func TestSignDownloadURL_StableDirectURL(t *testing.T) {
	p := newTestProvider("ak", "sk")
	const key = "res/1/asset/2/户型图.pdf"

	u, err := p.SignDownloadURL(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// ⚠️ **断言要落在"解码后等于 key"上,不能是"字符串以 key 结尾"** ——
	//   key 里有中文,直链里它是 percent-encode 过的
	//   (`%E6%88%B7%E5%9E%8B%E5%9B%BE.png`),所以"以原文结尾"是**假断言**:
	//   它只对纯 ASCII 的 key 成立 ⇒ 换成中文文件名就红,于是有人会把
	//   编码去掉来"修"它 ⇒ 直链里的中文又变回裸的。
	//
	//   真正的判据是:**URL 路径解码之后 == key**。
	if got := decodePathSuffix(t, u); got != key {
		t.Fatalf("URL = %q,解码后的路径 = %q,want %q", u, got, key)
	}
	// 稳定直链**不能带过期参数** —— 带了就说明还在签临时 URL
	if strings.Contains(u, "Expires=") || strings.Contains(u, "Signature=") {
		t.Fatalf("URL = %q,直链不该带签名/过期参数", u)
	}
}

// decodePathSuffix 取 URL 的 path 部分并 percent-decode。
func decodePathSuffix(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	dec, err := url.PathUnescape(u.Path)
	if err != nil {
		t.Fatalf("unescape %q: %v", u.Path, err)
	}
	return strings.TrimPrefix(dec, "/")
}

// TestSignDownloadURL_CDNDomain 配了 CDN 域名就用 CDN。
func TestSignDownloadURL_CDNDomain(t *testing.T) {
	p := newTestProvider("ak", "sk")
	p.cdnPrefix = normalizeCDN("cdn.example.com")
	u, err := p.SignDownloadURL(context.Background(), "res/1/a/2/b.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "https://cdn.example.com/") {
		t.Fatalf("URL = %q,want CDN 前缀", u)
	}
}

// TestSignDownloadURL_CDNDomainWithScheme **配置里带 scheme 的 CDN 域名**
//
// ⚠️ **M137 修的真 bug**:配置里常见写法是 `https://f.3dman.cn`(带 scheme),
// 而代码写死 `fmt.Sprintf("https://%s/%s", cdnDomain, key)` ⇒ 拼出
// `https://https://f.3dman.cn/...` ⇒ **每一条 OSS 直链都是废的**。
// 而且报错是"域名解析失败",指向不了"拼串拼重了"这个真因。
func TestSignDownloadURL_CDNDomainWithScheme(t *testing.T) {
	p := newTestProvider("ak", "sk")
	p.cdnPrefix = normalizeCDN("https://f.3dman.cn")
	u, err := p.SignDownloadURL(context.Background(), "res/1/a/2/b.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://f.3dman.cn/res/1/a/2/b.pdf"
	if u != want {
		t.Fatalf("URL = %q,want %q", u, want)
	}
	if strings.Contains(u, "https://https://") {
		t.Fatalf("URL = %q,scheme 拼重了", u)
	}
}

// TestSignDownloadURL_CDNDomainKeepsHTTP **配了 http 就别偷偷改成 https**
//
// ⚠️ normalizeCDN 跟 normalizeEndpoint 的差别就在这:endpoint 的 scheme
// 会被剥掉(它只用来拼 host),而 CDN 前缀是要**原样出现在 URL 里**的。
// 把用户配的 http 改成 https = 悄悄改掉他配的行为。
func TestSignDownloadURL_CDNDomainKeepsHTTP(t *testing.T) {
	p := newTestProvider("ak", "sk")
	p.cdnPrefix = normalizeCDN("http://cdn.example.com")
	u, err := p.SignDownloadURL(context.Background(), "res/1/a/2/b.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, "http://cdn.example.com/") {
		t.Fatalf("URL = %q,want http 前缀原样保留", u)
	}
}

// TestSignDownloadURL_EndpointWithScheme **endpoint 带 scheme 也不能拼出废 URL**
//
// ⚠️ 同 M137 的第二个真 bug:配置里 `endpoint: "https://oss-cn-shenzhen.aliyuncs.com"`
// (带 scheme)⇒ 拼出 `https://bucket.https://oss-cn-shenzhen.aliyuncs.com`。
func TestSignDownloadURL_EndpointWithScheme(t *testing.T) {
	p := newTestProvider("ak", "sk")
	p.publicHost = normalizeEndpoint("https://oss-cn-shenzhen.aliyuncs.com")
	u, err := p.SignDownloadURL(context.Background(), "res/1/a/2/b.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://only-bucket.oss-cn-shenzhen.aliyuncs.com/res/1/a/2/b.pdf"
	if u != want {
		t.Fatalf("URL = %q,want %q", u, want)
	}
}

// TestSignDownloadURL_EscapesChineseAndSpace **key 里的中文/空格要 percent-encode**
//
// ⚠️ key 里可能有中文和空格(oss.SafeKey 允许 UTF-8)。不编码的话:
//   · 中文直链"复制到聊天工具"会被截断
//   · 空格会让 URL 解析提前结束(后面的段全丢)
// 而 local provider 一直是逐段编码的 ⇒ 两个 provider 的直链形状必须一致,
// 不然"同一个 key 在 local 下的链接能用、切到 OSS 就不能用"。
func TestSignDownloadURL_EscapesChineseAndSpace(t *testing.T) {
	p := newTestProvider("ak", "sk")
	p.cdnPrefix = normalizeCDN("https://f.3dman.cn")
	key := "res/1/asset/2/户型图 v2.pdf"
	u, err := p.SignDownloadURL(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(u, " ") {
		t.Fatalf("URL = %q,空格必须编码", u)
	}
	if !strings.Contains(u, "%E6%88%B7") {
		t.Fatalf("URL = %q,中文必须 percent-encode", u)
	}
	// 斜杠不能被编码 —— 编码了这条路径在 OSS 上根本不存在
	if strings.Contains(u, "%2F") {
		t.Fatalf("URL = %q,分隔符不能被编码", u)
	}
}

// TestSignDownloadURL_RejectsUnsafeKey 穿越 key 不能拿到直链。
func TestSignDownloadURL_RejectsUnsafeKey(t *testing.T) {
	p := newTestProvider("ak", "sk")
	for _, bad := range []string{"", "res/../x", "res//1/a", `res\1\a`} {
		if _, err := p.SignDownloadURL(context.Background(), bad, time.Minute); err == nil {
			t.Errorf("SignDownloadURL(%q) 应该报错,却过了", bad)
		}
	}
}

// TestSignUploadToken_SingleBucket 直传凭证的 Host / Bucket 必须是那**一个**桶。
func TestSignUploadToken_SingleBucket(t *testing.T) {
	p := newTestProvider("ak", "sk")
	tok, err := p.SignUploadToken(context.Background(), "res/1/a/2/b.pdf", time.Minute, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Bucket != "only-bucket" {
		t.Errorf("Bucket = %q,want only-bucket", tok.Bucket)
	}
	if !strings.Contains(tok.Host, "only-bucket") {
		t.Errorf("Host = %q,应含 bucket 名", tok.Host)
	}
}

// ── X2:防盗链密钥 ──────────────────────────────────────────────────────

// TestHotlink_X2_RejectsAKIDSignedToken 是 X2 的回归守卫。
//
// 老实现拿 AccessKeyID 当 HMAC 密钥,而 AccessKeyID 是**公开**的(就在每个
// 直传凭证里下发给前端)。这条测试站在攻击者视角:只知道 AK ID,能不能伪造
// 一个通过校验的 token?能,就说明防盗链等于没设。
//
// ⚠️ 老实现在这里必然红 —— 这正是它存在的意义。
func TestHotlink_X2_RejectsAKIDSignedToken(t *testing.T) {
	const akID = "LTAI5tPublicAccessKeyID"
	p := newTestProvider(akID, "super-secret-that-is-not-public")
	key := "res/1/asset/2/report.pdf"

	// 攻击者只知道 AK ID,按老实现的算法伪造
	payload, _ := json.Marshal(hotlinkToken{Key: key, Expire: time.Now().Add(time.Hour).Unix()})
	mac := hmac.New(sha1.New, []byte(akID+"hotlink")) // ⚠️ 老密钥
	mac.Write(payload)
	forged := base64.RawURLEncoding.EncodeToString(payload) + "." + hex.EncodeToString(mac.Sum(nil))

	if err := p.Verify(context.Background(), "", key, forged); err == nil {
		t.Fatal("用公开的 AccessKeyID 签的 token 被接受了 —— 防盗链密钥仍然是 AK ID(X2 没修)")
	}

	// 对照:正规签的 token 要能过
	good, err := p.SignToken(context.Background(), key, time.Hour)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	if err := p.Verify(context.Background(), "", key, good); err != nil {
		t.Fatalf("正规 token 被拒: %v", err)
	}
}

// TestHotlink_SecretChangesSignature 密钥换了,签名必须跟着变。
func TestHotlink_SecretChangesSignature(t *testing.T) {
	key := "res/1/a/1/x.pdf"
	p1 := newTestProvider("ak", "secret-one")
	p2 := newTestProvider("ak", "secret-two")

	t1, _ := p1.SignToken(context.Background(), key, time.Hour)
	t2, _ := p2.SignToken(context.Background(), key, time.Hour)
	if t1 == t2 {
		t.Fatal("不同 secret 签出了同一个 token —— 签名没真的用 secret")
	}
	// p1 的 token 不能被 p2 验过
	if err := p2.Verify(context.Background(), "", key, t1); err == nil {
		t.Fatal("secret-two 验过了 secret-one 签的 token")
	}
}

// TestHotlink_DomainSeparation 域分隔后缀让防盗链签名不能当 policy 签名用。
func TestHotlink_DomainSeparation(t *testing.T) {
	p := newTestProvider("ak", "sk")
	if got := string(p.hotlinkSecret()); got != "skhotlink" {
		t.Fatalf("hotlinkSecret = %q, want \"skhotlink\" —— 域分隔后缀丢了", got)
	}
}

// ── PostObject key 精确匹配 ────────────────────────────────────────────

// decodePolicy 把 SignUploadToken 发出的 policy 解回结构体。
func decodePolicy(t *testing.T, pol string) []any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(pol)
	if err != nil {
		t.Fatalf("policy 不是合法 base64: %v", err)
	}
	var parsed struct {
		Conditions []any `json:"conditions"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("policy 不是合法 JSON: %v", err)
	}
	return parsed.Conditions
}

// TestSignUploadToken_KeyIsEq M78 的重点:key 条件必须是 eq,不能是 starts-with。
//
// starts-with 挡不住 `res/1/a/2/report.pdf` 后面接后缀 —— 客户端能往同一个
// 文件夹里塞任意文件。这条断言的是"条件里的匹配方式字面是 eq"。
func TestSignUploadToken_KeyIsEq(t *testing.T) {
	p := newTestProvider("ak", "sk")
	const key = "res/1/asset/2/1234-abcd-户型图.pdf"

	tok, err := p.SignUploadToken(context.Background(), key, 15*time.Minute, 10<<20)
	if err != nil {
		t.Fatalf("SignUploadToken: %v", err)
	}

	conds := decodePolicy(t, tok.Policy)
	var gotEq, gotSize bool
	for _, c := range conds {
		arr, ok := c.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		op, _ := arr[0].(string)
		switch op {
		case "eq":
			if len(arr) == 3 && arr[1] == "$key" {
				gotEq = true
				if arr[2] != key {
					t.Errorf("eq $key 的值 = %v, want 完整 key %q", arr[2], key)
				}
			}
		case "starts-with":
			// 出现即失败:starts-with 挡不住 key 接后缀
			t.Errorf("policy 里还有 starts-with 条件: %v —— key 不是精确匹配的", arr)
		case "content-length-range":
			gotSize = true
		}
	}
	if !gotEq {
		t.Error("policy 里没有 eq $key 条件 —— 直传凭证能往任意 key 写")
	}
	if !gotSize {
		t.Error("policy 里没有 content-length-range —— 单文件大小闸门没了")
	}
}

// TestSignUploadToken_KeepsSizeLimit eq 和 content-length-range 必须共存。
//
// 这是"别换成预签名 PUT"的理由:预签名 URL 没有大小限制,换过去等于把闸门扔了。
func TestSignUploadToken_KeepsSizeLimit(t *testing.T) {
	p := newTestProvider("ak", "sk")
	tok, err := p.SignUploadToken(context.Background(), "res/1/a/2/b.pdf", time.Minute, 5<<20)
	if err != nil {
		t.Fatalf("SignUploadToken: %v", err)
	}
	for _, c := range decodePolicy(t, tok.Policy) {
		arr, ok := c.([]any)
		if !ok || len(arr) != 3 || arr[0] != "content-length-range" {
			continue
		}
		if arr[2] != "5242880" { // 5<<20
			t.Errorf("content-length-range 上限 = %v, want 5242880", arr[2])
		}
		return
	}
	t.Error("找不到 content-length-range 条件")
}

// TestSignUploadToken_SignatureUsesSecret 签名密钥必须是 secret,不是 AK ID。
func TestSignUploadToken_SignatureUsesSecret(t *testing.T) {
	const akID, secret = "LTAI5tAKID", "the-real-secret"
	p := newTestProvider(akID, secret)
	key := "res/1/a/2/b.pdf"

	tok, err := p.SignUploadToken(context.Background(), key, time.Minute, 1<<20)
	if err != nil {
		t.Fatalf("SignUploadToken: %v", err)
	}

	// 用 secret 重算,必须跟下发的签名一致
	h := hmac.New(sha1.New, []byte(secret))
	h.Write([]byte(tok.Policy))
	want := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if tok.Signature != want {
		t.Errorf("签名不是用 secret 算的: got %q, want %q", tok.Signature, want)
	}

	// 用 AK ID 重算,必须**不**一致 —— 一致就说明又在用公开的 ID 当密钥
	h2 := hmac.New(sha1.New, []byte(akID))
	h2.Write([]byte(tok.Policy))
	if tok.Signature == base64.StdEncoding.EncodeToString(h2.Sum(nil)) {
		t.Error("签名竟然能用公开的 AccessKeyID 算出来 —— 密钥选错了")
	}
}

// TestSignUploadToken_RejectsUnsafeKey 穿越 key 不能拿到凭证。
func TestSignUploadToken_RejectsUnsafeKey(t *testing.T) {
	p := newTestProvider("ak", "sk")
	for _, bad := range []string{"", "res/../x", "../x", "res//1/a", `res\1\a`} {
		if _, err := p.SignUploadToken(context.Background(), bad, time.Minute, 0); err == nil {
			t.Errorf("SignUploadToken(%q) 应该报错,却过了", bad)
		}
	}
}

// TestTrimETagQuotes OSS 的 ETag 带引号,统一剥掉。
func TestTrimETagQuotes(t *testing.T) {
	cases := map[string]string{
		`"abc123"`: "abc123",
		`abc123`:   "abc123",
		`""`:       "",
		``:         ``,
	}
	for in, want := range cases {
		if got := trimETagQuotes(in); got != want {
			t.Errorf("trimETagQuotes(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseHelpers header 解析的边界。
func TestParseHelpers(t *testing.T) {
	if got := parseContentLength(""); got != 0 {
		t.Errorf("空 Content-Length = %d, want 0", got)
	}
	if got := parseContentLength("不是数字"); got != 0 {
		t.Errorf("非法 Content-Length = %d, want 0(不能 panic 也不能报错)", got)
	}
	if got := parseContentLength("1234"); got != 1234 {
		t.Errorf("parseContentLength = %d, want 1234", got)
	}
	if got := parseHTTPTime("garbage"); !got.IsZero() {
		t.Errorf("非法 Last-Modified = %v, want 零值", got)
	}
	want := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
	// ⚠️ 用 http.TimeFormat 而不是 time.RFC1123:后者在 UTC 布局下渲染出
	// "Thu, 22 Oct 2026 08:30:00 **UTC**",而 HTTP 规范规定 Last-Modified 的
	// 时区段**只能是 GMT**,http.ParseTime 按规范拒绝 UTC。OSS 发回来的也一定是
	// GMT。这里曾经用 RFC1123,测出个"ParseTime 不认合法时间"的假 bug。
	if got := parseHTTPTime(want.Format(http.TimeFormat)); !got.Equal(want) {
		t.Errorf("parseHTTPTime = %v, want %v", got, want)
	}
}

// TestStatRejectsUnsafeKey Stat 也要挡穿越 key —— 它内部会 pickBucket + 拼 URL。
func TestStatRejectsUnsafeKey(t *testing.T) {
	p := newTestProvider("ak", "sk")
	for _, bad := range []string{"res/../x", "../x"} {
		_, err := p.Stat(context.Background(), bad)
		if err == nil {
			t.Errorf("Stat(%q) 应该报错,却过了", bad)
			continue
		}
		var ei *oss.ErrInvalidKey
		if !errors.As(err, &ei) {
			t.Errorf("Stat(%q) 错误类型 = %T,应该是 *oss.ErrInvalidKey", bad, err)
		}
	}
}

// ── 可逆探针记录(2026-10-22)────────────────────────────────────────────
//
// 三条修复各打一个探针,把实现改回缺陷状态看用例会不会红。
// 方法同 `local_test.go` 末尾的记录表:RED 必须见输出里字面 `--- FAIL:`,
// 打完无条件还原。探针是手工打的(只有 3 条,不值得再写脚本)。
//
// ┌────┬──────────────────────────────────────┬─────┬──────────────────────┐
// │ 编号│ 把实现改回什么                         │ 结果│ 实际红了的用例         │
// ├────┼──────────────────────────────────────┼─────┼──────────────────────┤
// │ M78-A│ hotlinkSecret 用回 p.accessKey      │ RED │ X2_RejectsAKIDSigned  │
// │      │(X2 未修状态)                        │     │ + 另 2 条连带红       │
// │ M78-B│ pickBucket 写死 HasPrefix("public/")│ RED │ PickBucket_X3         │
// │      │(X3 未修状态)                        │     │                       │
// │ M78-C│ policy 条件换回 starts-with          │ RED │ KeyIsEq               │
// │      │(key 不精确匹配状态)                  │     │                       │
// └────┴──────────────────────────────────────┴─────┴──────────────────────┘
//
// ── 本轮抓到并修掉的两个"测试自己骗自己"──────────────────────────────
//
// **1. TestPickBucket 测的是它自己。** 原循环体是
// `isPub := len(c.key) >= 7 && c.key[:7] == "public/"` —— 它自己重新实现了一遍
// 前缀判定,**从头到尾没调用 pickBucket**。后果:把 `pickBucket` 整个函数删掉,
// 这条测试照样绿;而 X3 的真实缺陷(`public/` 匹配不上 `res/public/`)恰好就在
// 它测不到的缝里。已删除,换成真的调用 `pickBucket` 的 `TestPickBucket_X3`。
//
// **2. 防盗链两条测试修完 X2 之后变成假绿。** 它们构造
// `&Provider{accessKey: "test-key"}`,没设 `cfg.AccessKeySecret`。修 X2 之后
// 密钥变成空 secret,SignToken 和 Verify 双方都用同一个空串 —— 往返照样成功,
// 测试照样全绿,但它对"密钥选对了没有"一个字都证明不了。
// 已改用 `newTestProvider(ak, secret)` 带真 secret,并补 A 号探针证明它真能红。
//
// ── 首轮跑出来的 4 个失败:**全是测试写错了,不是实现有 bug** ───────────
//
// 记在这里是因为"测试红了→改实现"是个很容易走顺手的错误反射。
//
// · `isUnderPublic("res/p")`:我原本期望 true,实现给 false,而**实现是对的** ——
//   `res/public` 的路径段是 ["res", "res/public"],`res/p` 一个都不是。放行的话
//   就是拿裸 HasPrefix 当目录判据,`res/private` 会被 `res/p` 误伤。
// · `pickBucket("res/public")`:我原本期望 false(想"前缀自身不该命中"),
//   实现给 true。实现的 `key == pre` 分支是有意的全函数写法,而且这个 key 在
//   本项目造不出来(§3 的构造器永远至少多出 {userId})。改的是测试。
// · `parseHTTPTime`:我拿 `time.RFC1123` 格式化,而它在 UTC 布局下渲染出
//   "...UTC",`http.ParseTime` 按 HTTP 规范**只认 GMT**,理直气壮地拒绝。
//   测出个"不认识合法时间"的假 bug。OSS 发回来的 Last-Modified 一定是 GMT。
//
// ── 没写的测试,以及为什么 ───────────────────────────────────────────
//
// **Put / Stat / List / DeletePrefix 的联网行为没测。** 这四个方法全部直接调
// alioss SDK,给它们做真测试要么连真 OSS(测试环境有凭证?没有),要么对 SDK
// 做 interface 打桩(`*alioss.Bucket` 是具体 struct,SDK 没给接口,打不了)。
// 当前的可信度来自:纯逻辑部分(前缀判定 / policy 构造 / 翻页游标 / header 解析)
// 有单测,网络部分靠 M81 接线时的真机验证。**这一段是已知盲区,不是"测过了"。**
//
// **List 的跨 bucket 合并没测。** 同上 —— 需要两个真 bucket 才能触发。
// 合并逻辑本身(排序 / NextMarker / IsTruncated 的传播)在 local 那侧有对应用例。
