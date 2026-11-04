// CSRF 中间件单测 —— 通配符域名的**标签边界** / RequiredHeader / Bearer 豁免
//
// # 本文件存在的理由
//
// M105 审计 §1.3 时发现:CSRF 中间件**零覆盖**,而它判定"这个请求是不是
// 跨站伪造"的唯一硬防线是同源检查(AllowedOrigins)。
//
// 那段判定有个真实的洞:通配符 `*.example.com` 的实现是
//
//	strings.HasSuffix(origin, base)     // base = "example.com"
//
// —— **没有标签边界**。于是:
//
//	https://evil-example.com     ← 以 "example.com" 结尾 → 放行
//	https://notexample.com       ← 放行
//	https://example.com.evil.com ← 不以结尾 → 拒(这个反而是对的)
//
// 攻击者只要注册一个 `evil-example.com`(或 `attacker-example.com`),
// 就能让后台的所有写操作在自己名下执行 —— 后台是等保目标,这条不能留。
//
// 正确的口径是**域名标签边界**:origin 必须以 "." + base 结尾,
// 即 base 是一个完整的标签,前面还有一个 "." 分隔符。
//
// 另外两条:
//   - RequiredHeader 的注释原先描述了一个代码**从来没做过的**值校验(已订正)
//   - Bearer 请求被豁免(注释里说过),这条网守住它不被改坏
package middleware

import (
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
)

// runCSRF 跑一次带 CSRF 中间件的请求
func runCSRF(t *testing.T, cfg CSRFConfig, method string, headers ...ut.Header) *ut.ResponseRecorder {
	t.Helper()
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(CSRF(cfg))
	h := func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	}
	e.GET("/m105/csrf", h)
	e.HEAD("/m105/csrf", h)
	e.OPTIONS("/m105/csrf", h)
	e.POST("/m105/csrf", h)
	return ut.PerformRequest(e, method, "/m105/csrf", nil, headers...)
}

// TestOriginAllowed_WildcardRespectsLabelBoundary 是 M105 修的那条
//
// 通配符 `*.example.com` 只该覆盖**子域**,
// 不该覆盖 "evil-example.com" 这种"恰好同后缀"的不同域名。
func TestOriginAllowed_WildcardRespectsLabelBoundary(t *testing.T) {
	allow := []string{"*.example.com"}

	// 该放行的
	for _, origin := range []string{
		"https://app.example.com",
		"https://a.b.example.com",
		"http://app.example.com",
	} {
		if !originAllowed(origin, allow) {
			t.Fatalf("origin %q 被拒了 —— 通配符同主域应该放行", origin)
		}
	}

	// 该拒的 —— 这几条在修之前全部放行
	for _, origin := range []string{
		"https://evil-example.com",     // 同后缀但不是子域(← M105 修的就是这条)
		"https://notexample.com",       // 同后缀但不是子域
		"https://example.com.evil.com", // 前缀相同但域名归属不同
		"https://example.computer",     // 同后缀但不是子域
		"https://example.org",          // 完全不同的域
		"https://example.com.cn",       // 后缀被扩展
	} {
		if originAllowed(origin, allow) {
			t.Fatalf("origin %q 被放行了 —— 通配符匹配没有按域名标签边界判断", origin)
		}
	}
}

// TestOriginAllowed_ExactMatchAndStar 精确匹配与 "*" 不受影响
func TestOriginAllowed_ExactMatchAndStar(t *testing.T) {
	if !originAllowed("https://app.example.com", []string{"https://app.example.com"}) {
		t.Fatal("精确匹配的 origin 被拒了")
	}
	if !originAllowed("https://anything.example", []string{"*"}) {
		t.Fatal("配置成 * 时被拒了")
	}
	if originAllowed("", []string{"*"}) {
		t.Fatal("空 origin 被放行了 —— 没有 Origin 也没有 Referer 的请求不该算同源")
	}
	if originAllowed("https://x.com", []string{"https://y.com"}) {
		t.Fatal("不匹配的 origin 被放行了")
	}
}

// TestCSRF_RejectsSpoofedOriginThroughMiddleware 把上面的判定接到真实中间件上
//
// 单测只钉住 originAllowed 这个纯函数;这条钉住"中间件确实用它挡下了请求"。
func TestCSRF_RejectsSpoofedOriginThroughMiddleware(t *testing.T) {
	cfg := CSRFConfig{
		Enabled:         true,
		AllowedOrigins:  []string{"*.example.com"},
		RequiredHeader:  "",
		ExemptTokenAuth: true,
	}

	ok := runCSRF(t, cfg, http.MethodPost, ut.Header{Key: "Origin", Value: "https://app.example.com"})
	if ok.Code != http.StatusOK {
		t.Fatalf("同源子域请求 = %d, want 200", ok.Code)
	}

	bad := runCSRF(t, cfg, http.MethodPost, ut.Header{Key: "Origin", Value: "https://evil-example.com"})
	if bad.Code != http.StatusForbidden {
		t.Fatalf("伪造域名的请求 = %d, want 403 —— 攻击者域名被放行了", bad.Code)
	}
}

// TestCSRF_RequiredHeaderMustBePresent 写请求缺 RequiredHeader 必须 403
func TestCSRF_RequiredHeaderMustBePresent(t *testing.T) {
	cfg := CSRFConfig{
		Enabled:         true,
		RequiredHeader:  "X-Requested-With",
		ExemptTokenAuth: false,
	}

	missing := runCSRF(t, cfg, http.MethodPost)
	if missing.Code != http.StatusForbidden {
		t.Fatalf("缺 RequiredHeader = %d, want 403", missing.Code)
	}

	// 存在即可(只判存在不判值 —— 防线在 CORS 预检,不在值)
	present := runCSRF(t, cfg, http.MethodPost, ut.Header{Key: "X-Requested-With", Value: "XMLHttpRequest"})
	if present.Code != http.StatusOK {
		t.Fatalf("带 RequiredHeader = %d, want 200", present.Code)
	}
}

// TestCSRF_BearerRequestsAreExempt Bearer 模式天然防 CSRF,不该被拦
func TestCSRF_BearerRequestsAreExempt(t *testing.T) {
	cfg := CSRFConfig{
		Enabled:         true,
		RequiredHeader:  "X-Requested-With",
		ExemptTokenAuth: true,
	}

	w := runCSRF(t, cfg, http.MethodPost, ut.Header{Key: "Authorization", Value: "Bearer some-token"})
	if w.Code != http.StatusOK {
		t.Fatalf("Bearer 请求 = %d, want 200 —— token 模式的写请求被 CSRF 拦了", w.Code)
	}
}

// TestCSRF_SafeMethodsAreNotChecked GET / HEAD / OPTIONS 不做同源校验
func TestCSRF_SafeMethodsAreNotChecked(t *testing.T) {
	cfg := CSRFConfig{
		Enabled:         true,
		RequiredHeader:  "X-Requested-With",
		ExemptTokenAuth: false,
		AllowedOrigins:  []string{"https://only.example.com"},
	}

	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		w := runCSRF(t, cfg, m, ut.Header{Key: "Origin", Value: "https://evil-example.com"})
		if w.Code != http.StatusOK {
			t.Fatalf("%s 请求 = %d, want 200 —— 安全方法不该做 CSRF 校验", m, w.Code)
		}
	}
}

// TestCSRF_DisabledIsPassThrough 关掉之后全放行
func TestCSRF_DisabledIsPassThrough(t *testing.T) {
	cfg := CSRFConfig{Enabled: false, RequiredHeader: "X-Requested-With"}
	w := runCSRF(t, cfg, http.MethodPost, ut.Header{Key: "Origin", Value: "https://evil-example.com"})
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, want 200", w.Code)
	}
}
