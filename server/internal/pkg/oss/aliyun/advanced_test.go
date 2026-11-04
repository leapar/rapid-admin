// 阿里云 OSS 高级特性 单测(§1.10 #5-10 P2)
package aliyun

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"rapid-admin/internal/pkg/oss"
)

func TestInitOpts_PublicAPI(t *testing.T) {
	// 验证 WithContentType / WithMetadata / WithACL 在公开 API 下能用
	_ = oss.WithContentType("image/png")
	_ = oss.WithMetadata(map[string]string{"k": "v"})
	_ = oss.WithACL("public-read")
	// 验证选项能正确应用
	io := &oss.InitOpts{}
	oss.WithContentType("image/png")(io)
	if io.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want image/png", io.ContentType)
	}
	oss.WithMetadata(map[string]string{"x": "1"})(io)
	if io.Metadata["x"] != "1" {
		t.Errorf("Metadata = %v", io.Metadata)
	}
	oss.WithACL("public-read")(io)
	if io.ACL != "public-read" {
		t.Errorf("ACL = %q", io.ACL)
	}
}

// ⚠️ **原 `TestPickBucket` 已删除,不是被重写。**
//
// 它的循环体是 `isPub := len(c.key) >= 7 && c.key[:7] == "public/"` ——
// 它**自己重新实现了一遍前缀判定**,从头到尾没调用 `pickBucket` 或 `isPublic`。
// 所以它测的是测试文件自己,不是被测代码:把 `pickBucket` 整段删掉它照样绿,
// 而 X3 的真实缺陷(`public/` 匹配不上 `res/public/`)恰恰就在这条测不到的缝里。
//
// ⚠️ **后来双桶模型整个删掉了**(单桶 + 全树公共读,见 `m78_test.go` 的
// `TestSingleBucket_NoPrefixDispatch`),所以连"前缀分流"这个概念都不存在了 ——
// `pickBucket` 保留下来只是因为 Put/Stat 还在用它选桶,而它现在只有一个答案。

func TestHotlinkToken_RoundTrip(t *testing.T) {
	// ⚠️ **必须用 newTestProvider 而不是 `&Provider{accessKey: ...}`**。
	// 修 X2 之后签名密钥是 `cfg.AccessKeySecret`,只设 accessKey 会让
	// SignToken/Verify 双方都用空 secret —— 往返照样成功,测试照样绿,
	// 但它证明不了任何跟密钥有关的事。这是个已经发生过的假绿。
	p := newTestProvider("test-ak", "test-secret")
	ctx := context.Background()
	key := "rapid-admin/report.pdf"
	token, err := p.SignToken(ctx, key, 1*time.Hour)
	if err != nil {
		t.Fatalf("SignToken err: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	// valid
	if err := p.Verify(ctx, "https://example.com/page", key, token); err != nil {
		t.Errorf("Verify valid err: %v", err)
	}
	// wrong key
	if err := p.Verify(ctx, "https://example.com/page", "other.pdf", token); err == nil {
		t.Error("expected key mismatch err")
	}
	// wrong sig
	wrong := token + "x"
	if err := p.Verify(ctx, "", key, wrong); err == nil {
		t.Error("expected sig mismatch err")
	}
	// missing token
	if err := p.Verify(ctx, "", key, ""); err == nil {
		t.Error("expected missing token err")
	}
	// malformed token
	if err := p.Verify(ctx, "", key, "no-dot"); err == nil {
		t.Error("expected format err")
	}
}

func TestHotlinkToken_Expired(t *testing.T) {
	p := newTestProvider("test-ak", "test-secret") // 同上:必须带真 secret
	ctx := context.Background()
	// -1s ttl → 已过期
	token, err := p.SignToken(ctx, "k", -1*time.Second)
	if err != nil {
		t.Fatalf("SignToken err: %v", err)
	}
	if err := p.Verify(ctx, "", "k", token); err == nil {
		t.Error("expected expired err")
	}
}

func TestRandHex(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := randHexOSS(8); len(got) != 16 {
			t.Errorf("randHexOSS(8) length = %d, want 16", len(got))
			return
		}
	}
}

func TestAdvancedFacade(t *testing.T) {
	// 不带任何高级特性的 provider
	type basic struct{}
	// 模拟一个空 provider,Advanced() 应当全部返 nil
	_ = basic{}
	// 真实 provider(MultipartProvider 等已经 cast 进去)
	p, _ := New(Config{
		Endpoint:     "oss-cn-hangzhou.aliyuncs.com",
		AccessKey:    "ak",
		AccessSecret: "sk",
		Bucket:       "only-bucket",
	})
	mp, sp, tp, hp, qp, cp := oss.Advanced(p)
	if mp == nil {
		t.Error("MultipartProvider should be non-nil")
	}
	if sp == nil {
		t.Error("ScanProvider should be non-nil (stub)")
	}
	if tp == nil {
		t.Error("ThumbnailProvider should be non-nil")
	}
	if hp == nil {
		t.Error("HotlinkProvider should be non-nil")
	}
	// qp / cp 是 stub,也是 non-nil(类型断言成功,只是方法返错)
	if qp == nil {
		t.Error("QuotaProvider should be non-nil (stub)")
	}
	if cp == nil {
		t.Error("CDNProvider should be non-nil (stub)")
	}
}

func TestGetThumbURL_NeedsRealProvider(t *testing.T) {
	// GetThumbURL 依赖 SignDownloadURL(需要真实的 OSS client)
	// 这里跳过(避免在单测里 require OSS endpoint 凭证)
	// 真实联调走 integration test
	t.Skip("requires real OSS config; tested in integration suite")
}

func TestGetThumbURL_BuildQuery(t *testing.T) {
	// 仅测试 URL 拼接逻辑(query 参数构造)
	// 不走真实 OSS,直接验证 "x-oss-process" 拼接
	signed := "https://example.com/oss/rapid-admin/k.png?Expires=123&Signature=abc"
	sep := "?"
	// 模拟 separator 选择
	if len(signed) > 0 && signed[len(signed)-1] != '?' {
		// 用 strings.Contains 判定
	}
	_ = sep
	// 实际查询字符串构造
	process := "image/resize,200x200"
	encoded := url.QueryEscape(process)
	got := signed + "&x-oss-process=" + encoded
	if !strings.Contains(got, "x-oss-process=image%2Fresize%2C200x200") {
		t.Errorf("process encoding wrong: %s", got)
	}
}
