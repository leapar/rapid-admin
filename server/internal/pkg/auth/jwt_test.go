// pkg/auth 的安全性质测试
//
// 背景:这个包是**唯一在跑**的 JWT 实现(HS256),却长期零测试;而全仓零引用的
// internal/pkg/rsajwt(RS256)反倒带一整套测试 —— 典型假信心。
//
// 2026-10-21 (W3) 修掉的三个缺陷,回归网都在下面,不要重新发明:
//
//  1. Claims 没有 token 类型声明。SignAccess / SignRefresh 只差一个 TTL,
//     两者产出的 claim 完全一致 → refresh token 能当 access token 用,
//     而它的有效期是 720h。
//     守卫:TestRefreshTokenIsNotUsableAsAccessToken 等。
//  2. sign() 从不写 JTI → 中间件的 `if claims.JTI != ""` 恒假 →
//     登出 / 改密码 / 重置密码 里的吊销调用全是 no-op,黑名单整条链路是死代码。
//     守卫:TestSign_AlwaysSetsJTI / TestSign_OverwritesCallerSuppliedJTI。
//  3. Claims 上自定义的 Audience / JTI 字段与 jwt.RegisteredClaims 的同名 tag
//     冲突,浅层胜出 → sign() 里写 RegisteredClaims.Audience 是死代码。
//     现已删掉这两个自定义字段,一律读标准 claim。
//     守卫:TestClaims_NoStandardTagCollision(反射,防复发)。
package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAdminSecret    = "test-admin-secret-at-least-32-bytes!!"
	testOwnerSecret    = "test-owner-secret-at-least-32-bytes!!"
	testSupplierSecret = "test-supplier-secret-at-least-32-byte!"
	testIssuer         = "rapid-admin-test"
)

func testTTLs() (access, refresh, short time.Duration) {
	return 2 * time.Hour, 720 * time.Hour, 5 * time.Minute
}

func newTestMulti() *Multi {
	a, r, s := testTTLs()
	return NewMulti(testAdminSecret, testOwnerSecret, testSupplierSecret, testIssuer, a, r, s)
}

// testClaims 只填业务 claim —— aud / jti / exp 全部由 Manager.sign 权威写入。
func testClaims(uid uint64) *Claims {
	return &Claims{UserID: uid, Username: "tester"}
}

// ---------------------------------------------------------------------------
// 结构性守卫:Claims 不得再声明与 jwt.RegisteredClaims 同名的 json tag
//
// encoding/json 在两个不同深度的字段上遇到同名 tag 时,浅层胜出。被遮蔽的那个
// 在 marshal / unmarshal 两边都彻底不可见 —— 于是"给它赋值"是死代码,而"从它
// 读值"永远读到零值。本文件历史上踩过两次(Audience / JTI),两次都是"看着能跑,
// 实际收不到"。用反射把这条规则变成一条常驻断言,比注释可靠。
// ---------------------------------------------------------------------------

func jsonTagNames(t reflect.Type) map[string]string {
	out := make(map[string]string)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out[name] = f.Name
	}
	return out
}

func TestClaims_NoStandardTagCollision(t *testing.T) {
	standard := jsonTagNames(reflect.TypeOf(jwt.RegisteredClaims{}))
	custom := jsonTagNames(reflect.TypeOf(Claims{}))
	if len(standard) == 0 {
		t.Fatal("jwt.RegisteredClaims 上一个 json tag 都没解析出来,测试本身有问题")
	}

	for name, field := range custom {
		if std, ok := standard[name]; ok {
			t.Errorf("Claims.%s 的 json tag %q 与 jwt.RegisteredClaims.%s 冲突 —— "+
				"Claims 在 depth 0、RegisteredClaims 在 depth 1,encoding/json 浅层胜出,"+
				"后者的值永远到不了 wire。标准 claim(aud/jti/exp/iat/nbf/iss)一律走 "+
				"RegisteredClaims,用 Claims.Audience() / Claims.JTI() 访问器读。",
				field, name, std)
		}
	}
}

// TestClaims_StandardClaimsAreReachable 反向验证:通过访问器能读到标准 claim,
// 说明自定义字段移除之后,aud / jti 确实还活着。
func TestClaims_StandardClaimsAreReachable(t *testing.T) {
	c := &Claims{
		UserID:    1,
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Audience: jwt.ClaimStrings{"admin"},
			ID:       "jti-abc",
		},
	}
	if got := c.Audience(); got != AudienceAdmin {
		t.Fatalf("Audience() = %q, want admin", got)
	}
	if got := c.JTI(); got != "jti-abc" {
		t.Fatalf("JTI() = %q, want jti-abc", got)
	}

	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// RegisteredClaims.Audience 是 ClaimStrings([]string),所以 wire 上是数组形式
	// ["admin"]。RFC 7519 允许 aud 是单值或数组,jwt v5 解析两种都吃。
	// 注意:移除自定义的 Audience 字段之后,aud 的线格式从裸串 "admin" 变成了数组
	// ["admin"] —— 老 token 本来就要被 W3 的 typ 检查全部拒收,所以这个变化不产生
	// 迁移负担,但它确实是 wire 格式变更,记在这里免得以后有人排查时对不上。
	aud, ok := m["aud"].([]any)
	if !ok || len(aud) != 1 || aud[0] != "admin" {
		t.Fatalf("wire 上 aud = %v, want [\"admin\"]", m["aud"])
	}
	if m["jti"] != "jti-abc" {
		t.Fatalf("wire 上 jti = %v, want jti-abc", m["jti"])
	}
	if m["typ"] != "access" {
		t.Fatalf("wire 上 typ = %v, want access", m["typ"])
	}
}

// TestSign_AudienceRoundTrip 端到端确认:签出来的 token 能被同一受众的 manager 解析回来。
func TestSign_AudienceRoundTrip(t *testing.T) {
	admin := newTestMulti().MustGet(AudienceAdmin)

	tok, err := admin.SignAccess(&Claims{UserID: 42, Username: "u42"})
	if err != nil {
		t.Fatalf("SignAccess: %v", err)
	}
	got, err := admin.ParseAccess(tok)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if got.Audience() != AudienceAdmin {
		t.Fatalf("Audience() = %q, want admin", got.Audience())
	}
	if got.UserID != 42 || got.Username != "u42" {
		t.Fatalf("业务 claim 丢失: uid=%d usr=%q", got.UserID, got.Username)
	}
	if got.Issuer != testIssuer {
		t.Fatalf("Issuer = %q, want %q", got.Issuer, testIssuer)
	}
	if got.TokenType != TokenTypeAccess {
		t.Fatalf("TokenType = %q, want access", got.TokenType)
	}
}

// TestParse_WrongAudienceManager 跨受众:admin token 拿到 owner 的 manager 上必须被拒。
// 这是三受众独立 secret 的核心保证。
func TestParse_WrongAudienceManager(t *testing.T) {
	m := newTestMulti()
	tok, err := m.MustGet(AudienceAdmin).SignAccess(testClaims(1))
	if err != nil {
		t.Fatalf("SignAccess: %v", err)
	}
	if _, err := m.MustGet(AudienceOwner).ParseAccess(tok); err == nil {
		t.Fatal("admin token 被 owner 的 manager 接受了 —— 三受众隔离失效")
	}
}

// TestMulti_ParseAnyAccess_CrossAudienceResolvesRightSecret 靠 token 里的 aud 选出
// 正确的 manager(即正确的 secret)。这条守的是"未验签嗅探 aud"这条安全关键路径。
func TestMulti_ParseAnyAccess_CrossAudienceResolvesRightSecret(t *testing.T) {
	m := newTestMulti()
	tok, err := m.MustGet(AudienceOwner).SignAccess(&Claims{UserID: 7})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := m.ParseAnyAccess(tok)
	if err != nil {
		t.Fatalf("ParseAnyAccess(owner token): %v", err)
	}
	if got.Audience() != AudienceOwner {
		t.Fatalf("audience = %q, want owner", got.Audience())
	}
	if got.UserID != 7 {
		t.Fatalf("UserID = %d, want 7", got.UserID)
	}
}

// TestMulti_Parse_RejectsUnknownAudience aud 指向不存在的受众 → 拒绝,不能落到
// 某个"默认" manager 上。
func TestMulti_Parse_RejectsUnknownAudience(t *testing.T) {
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": 1, "usr": "x", "iss": testIssuer, "typ": "access",
		"aud": "ghost",
	}).SignedString([]byte(testAdminSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := newTestMulti().ParseAnyAccess(tok); err == nil {
		t.Fatal("未知 audience 的 token 被接受了")
	}
}

// TestMulti_Parse_RejectsMissingAudience 没有 aud claim → 拒绝。
func TestMulti_Parse_RejectsMissingAudience(t *testing.T) {
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": 1, "usr": "x", "iss": testIssuer, "typ": "access",
	}).SignedString([]byte(testAdminSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := newTestMulti().ParseAnyAccess(tok); err == nil {
		t.Fatal("没有 aud claim 的 token 被接受了")
	}
}

// TestMulti_SignByAudience_UnknownAudienceFails 未知受众签发返错。
func TestMulti_SignByAudience_UnknownAudienceFails(t *testing.T) {
	m := newTestMulti()
	if _, err := m.SignByAudience(Audience("ghost"), testClaims(1)); err == nil {
		t.Fatal("未知 audience 竟然签发成功了")
	}
	if _, err := m.RefreshByAudience(Audience("ghost"), testClaims(1)); err == nil {
		t.Fatal("未知 audience 竟然签发 refresh 成功了")
	}
}

// TestMulti_MustGetPanics MustGet 是给 main 启动自检用的,拿不到就该 panic 而不是
// 返回 nil 让调用方在几百行之外炸掉。
func TestMulti_MustGetPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustGet(未知受众) 没有 panic")
		}
	}()
	newTestMulti().MustGet(Audience("ghost"))
}

// ---------------------------------------------------------------------------
// token 类型隔离(W3 修复的核心)
// ---------------------------------------------------------------------------

// TestRefreshTokenIsNotUsableAsAccessToken refresh token 打到受保护路由上必须被拒。
//
// 这条是本次修复的核心断言。修复前 Claims 里没有任何类型声明,Parse() 只看 aud,
// 于是 refresh token 是一个有效期 720h 的万能通行证 —— 日志、截图、浏览器历史里
// 泄露一个 refresh token,等同于泄露 30 天的登录态。
func TestRefreshTokenIsNotUsableAsAccessToken(t *testing.T) {
	m := newTestMulti()
	tok, err := m.MustGet(AudienceAdmin).SignRefresh(testClaims(1))
	if err != nil {
		t.Fatalf("SignRefresh: %v", err)
	}

	// 单受众 manager
	if _, err := m.MustGet(AudienceAdmin).ParseAccess(tok); err == nil {
		t.Fatal("refresh token 被 ParseAccess 接受了")
	} else if !errors.Is(err, ErrTokenTypeMismatch) {
		t.Fatalf("err = %v, want ErrTokenTypeMismatch", err)
	}

	// Multi(中间件走的路径)
	if _, err := m.ParseAnyAccess(tok); err == nil {
		t.Fatal("refresh token 被 ParseAnyAccess(中间件)接受了 —— 720h 的长效凭证泄漏")
	} else if !errors.Is(err, ErrTokenTypeMismatch) {
		t.Fatalf("err = %v, want ErrTokenTypeMismatch", err)
	}

	// 但它作为 refresh 是好使的 —— 不能矫枉过正
	if _, err := m.ParseRefresh(tok); err != nil {
		t.Fatalf("refresh token 竟然不能当 refresh 用: %v", err)
	}
}

// TestAccessTokenIsNotUsableForRefresh 反向隔离:access token 不能拿去续期,
// 否则续期接口就是个不需要任何凭证的公共令牌兑换机。
func TestAccessTokenIsNotUsableForRefresh(t *testing.T) {
	m := newTestMulti()
	tok, err := m.MustGet(AudienceAdmin).SignAccess(testClaims(1))
	if err != nil {
		t.Fatalf("SignAccess: %v", err)
	}
	if _, err := m.ParseRefresh(tok); !errors.Is(err, ErrTokenTypeMismatch) {
		t.Fatalf("access token 续期 err = %v, want ErrTokenTypeMismatch", err)
	}
}

// TestParse_RejectsLegacyTokenWithoutTyp 老 token(没有 typ claim)明确拒绝。
//
// 不做兼容解析是有意的:兼容分支等于让"refresh 可当 access 用"这个洞一直活着,
// 而 720h 的兼容窗口不是迁移,是永久双模解析。
func TestParse_RejectsLegacyTokenWithoutTyp(t *testing.T) {
	m := newTestMulti()
	// 手工造一个"修复前形状"的 token:有 aud / jti / exp,但没有 typ
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": 1, "usr": "legacy", "iss": testIssuer, "aud": "admin",
		"jti": "legacy-jti",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}).SignedString([]byte(testAdminSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := m.ParseAnyAccess(tok); !errors.Is(err, ErrLegacyToken) {
		t.Fatalf("老 token err = %v, want ErrLegacyToken", err)
	}
}

// TestShortTokenIsolation 短时 token 同样被隔离在 access / refresh 之外。
func TestShortTokenIsolation(t *testing.T) {
	m := newTestMulti()
	admin := m.MustGet(AudienceAdmin)
	tok, err := admin.SignShort(testClaims(1))
	if err != nil {
		t.Fatalf("SignShort: %v", err)
	}
	if _, err := m.ParseAnyAccess(tok); !errors.Is(err, ErrTokenTypeMismatch) {
		t.Fatalf("short token 当 access 用 err = %v, want ErrTokenTypeMismatch", err)
	}
	if _, err := admin.ParseShort(tok); err != nil {
		t.Fatalf("short token 应当能被 ParseShort 解析: %v", err)
	}
}

// ---------------------------------------------------------------------------
// JTI(W3 修复的第二条)
// ---------------------------------------------------------------------------

// TestSign_AlwaysSetsJTI 每次签发都带唯一 JTI。
//
// 没有 JTI 就没有吊销:登出 / 改密码 / 重置密码全部以 JTI 为键去 revoke,
// JTI 为空时中间件和 service 的判断都是恒假,整套黑名单是死代码。
func TestSign_AlwaysSetsJTI(t *testing.T) {
	m := newTestMulti()

	for _, tc := range []struct {
		name string
		sign func(*Claims) (string, error)
		typ  TokenType
	}{
		{"access", m.MustGet(AudienceAdmin).SignAccess, TokenTypeAccess},
		{"refresh", m.MustGet(AudienceAdmin).SignRefresh, TokenTypeRefresh},
		{"short", m.MustGet(AudienceAdmin).SignShort, TokenTypeShort},
	} {
		seen := make(map[string]struct{}, 1000)
		for i := 0; i < 1000; i++ {
			tok, err := tc.sign(testClaims(uint64(i)))
			if err != nil {
				t.Fatalf("%s sign: %v", tc.name, err)
			}
			c, err := m.MustGet(AudienceAdmin).Parse(tok, tc.typ)
			if err != nil {
				t.Fatalf("%s parse: %v", tc.name, err)
			}
			jti := c.JTI()
			if jti == "" {
				t.Fatalf("%s 第 %d 次签发没有 JTI", tc.name, i)
			}
			if _, dup := seen[jti]; dup {
				t.Fatalf("%s JTI 重复: %q", tc.name, jti)
			}
			seen[jti] = struct{}{}
		}
	}
}

// TestSign_OverwritesCallerSuppliedJTI sign() 覆盖调用方传入的 JTI。
//
// 为什么重要:service 层有"用同一个 Claims 先签 access 再签 refresh"的写法。
// 如果 sign() 信任调用方,JTI 会被复用 —— 两个 token 共享一个吊销键,
// 吊销 refresh 会连带把 access 也干掉(登出后自己把自己踢飞)。
// 让 sign() 无条件覆写,调用方就算写错也伤不到。
func TestSign_OverwritesCallerSuppliedJTI(t *testing.T) {
	m := newTestMulti()
	admin := m.MustGet(AudienceAdmin)

	// 调用方"自作主张"塞了个 JTI
	c := testClaims(1)
	c.RegisteredClaims.ID = "caller-supplied-jti"

	a1, err := admin.SignAccess(c)
	if err != nil {
		t.Fatalf("sign1: %v", err)
	}
	a2, err := admin.SignRefresh(c)
	if err != nil {
		t.Fatalf("sign2: %v", err)
	}
	ca, err := admin.ParseAccess(a1)
	if err != nil {
		t.Fatalf("parse1: %v", err)
	}
	cb, err := admin.ParseRefresh(a2)
	if err != nil {
		t.Fatalf("parse2: %v", err)
	}
	if ca.JTI() == "caller-supplied-jti" || cb.JTI() == "caller-supplied-jti" {
		t.Fatal("sign() 没有覆盖调用方传入的 JTI")
	}
	if ca.JTI() == cb.JTI() {
		t.Fatalf("access 与 refresh 共用同一个 JTI %q —— 吊销一个会连带吊掉另一个", ca.JTI())
	}
}

// TestSign_OverwritesAllStandardClaims 同理:调用方预设的 exp / iss / aud 一律无效,
// manager 为准。防止"用 admin 的 secret 签一个自己写的 aud=owner 的 token"。
func TestSign_OverwritesAllStandardClaims(t *testing.T) {
	m := newTestMulti()
	admin := m.MustGet(AudienceAdmin)

	c := testClaims(1)
	c.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    "someone-else",
		Audience:  jwt.ClaimStrings{"owner"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(100 * time.Hour)),
		ID:        "preset",
	}

	tok, err := admin.SignAccess(c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := admin.ParseAccess(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Issuer != testIssuer {
		t.Fatalf("Issuer = %q, want %q", got.Issuer, testIssuer)
	}
	if got.Audience() != AudienceAdmin {
		t.Fatalf("Audience() = %q, want admin —— sign() 必须以 manager 为准", got.Audience())
	}
	wantTTL, _, _ := testTTLs()
	if ttl := got.ExpiresAt.Sub(got.IssuedAt.Time); ttl != wantTTL {
		t.Fatalf("预设的 100h 有效期没被覆盖,实际 TTL = %v, want %v", ttl, wantTTL)
	}
}

// ---------------------------------------------------------------------------
// 签名算法防混淆
// ---------------------------------------------------------------------------

// TestParse_RejectsAlgNone alg=none 的 token 必须被拒(经典 JWT 绕过)。
func TestParse_RejectsAlgNone(t *testing.T) {
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, &Claims{
		UserID: 1, TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"admin"}},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Skipf("当前 jwt 库版本不允许构造 alg=none token(测试无法构造,非代码问题): %v", err)
	}
	if _, err := newTestMulti().ParseAnyAccess(tok); err == nil {
		t.Fatal("alg=none 的 token 被接受了 —— 认证绕过")
	}
}

// TestParse_RejectsRS256 keyfunc 只放行 *jwt.SigningMethodHMAC。
// 用真 RSA 签一个 RS256 token,必须被拒。
func TestParse_RejectsRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA key 失败: %v", err)
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, &Claims{
		UserID: 1, TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"admin"}},
	}).SignedString(key)
	if err != nil {
		t.Fatalf("sign RS256: %v", err)
	}
	if _, err := newTestMulti().ParseAnyAccess(tok); err == nil {
		t.Fatal("RS256 token 被接受了 —— 算法白名单失效")
	}
}

// TestParse_AllowsHMACFamily 白名单是"HMAC 家族"而非"仅 HS256"。
func TestParse_AllowsHMACFamily(t *testing.T) {
	for _, method := range []jwt.SigningMethod{
		jwt.SigningMethodHS256, jwt.SigningMethodHS384, jwt.SigningMethodHS512,
	} {
		tok, err := jwt.NewWithClaims(method, &Claims{
			UserID: 1, TokenType: TokenTypeAccess,
			RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"admin"}},
		}).SignedString([]byte(testAdminSecret))
		if err != nil {
			t.Fatalf("sign %v: %v", method.Alg(), err)
		}
		if _, err := newTestMulti().ParseAnyAccess(tok); err != nil {
			t.Fatalf("%v 应当被放行(HMAC 家族),got %v", method.Alg(), err)
		}
	}
}

// TestParse_RejectsWrongSecret 同受众但用别的受众的 secret 签 → 必须被拒。
func TestParse_RejectsWrongSecret(t *testing.T) {
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
		UserID: 1, TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"admin"}},
	}).SignedString([]byte(testOwnerSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := newTestMulti().ParseAnyAccess(tok); err == nil {
		t.Fatal("用 owner secret 签的 admin token 被接受了")
	}
}

// TestParse_RejectsTamperedPayload 改一个 payload 字节就应当验签失败。
func TestParse_RejectsTamperedPayload(t *testing.T) {
	tok, err := newTestMulti().MustGet(AudienceAdmin).SignAccess(testClaims(1))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	i := len(tok) / 3
	b := []byte(tok)
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	if _, err := newTestMulti().ParseAnyAccess(string(b)); err == nil {
		t.Fatal("被篡改的 token 被接受了")
	}
}

// ---------------------------------------------------------------------------
// 过期
// ---------------------------------------------------------------------------

// TestParse_ExpiredReturnsSentinel 过期必须能被 errors.Is(err, jwt.ErrTokenExpired) 识别。
//
// 为什么重要:middleware 的错误分类据此区分"过期"和"无效"。字符串匹配依赖 jwt 库
// 的错误文案,升级即可能失配,后果是过期 token 被报成"令牌无效",前端拿到
// TokenInvalid 而不是 TokenExpired,刷新流程走错分支。
func TestParse_ExpiredReturnsSentinel(t *testing.T) {
	_, r, s := testTTLs()
	// accessTTL 传 -1h,SignAccess 签出来就是过期的
	expired := NewManager(testAdminSecret, testIssuer, -time.Hour, r, s, AudienceAdmin)

	tok, err := expired.SignAccess(testClaims(1))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = expired.ParseAccess(tok)
	if err == nil {
		t.Fatal("过期 token 被接受了")
	}
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Fatalf("errors.Is(err, jwt.ErrTokenExpired) = false, err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// TTL 三态
// ---------------------------------------------------------------------------

// TestManager_TTLs 三种 token 的 TTL 各不相同,且和签出来的 exp 对得上。
func TestManager_TTLs(t *testing.T) {
	a, r, s := testTTLs()
	m := NewManager(testAdminSecret, testIssuer, a, r, s, AudienceAdmin)

	if m.AccessTTL() != a || m.RefreshTTL() != r || m.ShortTTL() != s {
		t.Fatalf("TTL getter 不一致: %v/%v/%v", m.AccessTTL(), m.RefreshTTL(), m.ShortTTL())
	}

	for _, tc := range []struct {
		name string
		sign func(*Claims) (string, error)
		typ  TokenType
		want time.Duration
	}{
		{"access", m.SignAccess, TokenTypeAccess, a},
		{"refresh", m.SignRefresh, TokenTypeRefresh, r},
		{"short", m.SignShort, TokenTypeShort, s},
	} {
		tok, err := tc.sign(testClaims(1))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		c, err := m.Parse(tok, tc.typ)
		if err != nil {
			t.Fatalf("%s parse: %v", tc.name, err)
		}
		if got := c.ExpiresAt.Sub(c.IssuedAt.Time); got != tc.want {
			t.Fatalf("%s token 的有效期 = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestAudienceValidation 三个受众常量 + 合法性判断。
func TestAudienceValidation(t *testing.T) {
	if len(AllAudiences()) != 3 {
		t.Fatalf("AllAudiences() = %v, want 3 项", AllAudiences())
	}
	for _, a := range AllAudiences() {
		if !IsValidAudience(a) {
			t.Fatalf("%q 竟然是非法受众", a)
		}
	}
	if IsValidAudience(Audience("ghost")) {
		t.Fatal("ghost 竟然被当成合法受众")
	}
	if IsValidAudience("") {
		t.Fatal("空字符串竟然被当成合法受众")
	}
}

// TestNewMulti_RegistersAllAudiences Multi 必须把 3 个受众都装上,
// 否则 owner/supplier 的登录链路在运行时才会炸。
func TestNewMulti_RegistersAllAudiences(t *testing.T) {
	m := newTestMulti()
	for _, a := range AllAudiences() {
		got, ok := m.Get(a)
		if !ok || got == nil {
			t.Fatalf("受众 %q 没注册进 Multi", a)
		}
		if got.Audience() != a {
			t.Fatalf("manager 的 Audience() = %q, want %q", got.Audience(), a)
		}
	}
	if len(m.All()) != 3 {
		t.Fatalf("All() 返回 %d 个, want 3", len(m.All()))
	}
}
