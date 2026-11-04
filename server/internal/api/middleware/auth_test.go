// Auth 中间件单测 —— 重点是**反向守卫**。
//
// # 本文件存在的理由
//
// 2026-10-21 之前的 JWT 里没有"用途"声明:SignAccess / SignRefresh 只差一个
// TTL,产出的 claim 完全一致。于是 **refresh token 能当 access token 用**,
// 而且有效期 720h。一个从日志 / 缓存 / URL 里泄露出去的 refresh token,
// 等价于 30 天的万能通行证。
//
// 这个洞在 service 层测不出来 —— RefreshToken 会拒绝 access token(那是 W3
// 之后新加的守卫),但反过来"access 接口收不收 refresh token"是中间件的职责。
// 所以这条守卫必须落在这里,而且它必须真的打一次 Auth()。
//
// 另一个理由:W3 之前 `if claims.JTI != ""` 与 `if bl != nil` 两层守卫让整条
// 吊销校验**从未执行过一次**。中间件这条路径此前零测试。
package middleware

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/service"
)

// setupAuthMW 注入真 JWT + 真内存吊销存储,并在用例结束后换一副新的
//
// 吊销存储和 JWTs 都是**进程级全局**,所以必须每例重置 —— 否则一个用例
// 吊销的 jti 会污染下一个。本包没有 t.Parallel(),顺序执行是安全的;
// 哪天有人加了 t.Parallel(),这里要改成 per-test 注入而不是全局。
func setupAuthMW(t *testing.T) *auth.Multi {
	t.Helper()
	multi := auth.NewMulti(
		"mw-test-admin-secret", "mw-test-owner-secret", "mw-test-supplier-secret",
		"mw-test-issuer",
		2*time.Hour, 720*time.Hour, 5*time.Minute,
	)
	JWTs = multi
	t.Cleanup(func() { JWTs = nil })

	auth.SetDefaultRevocation(auth.NewMemoryStore())
	t.Cleanup(func() { auth.SetDefaultRevocation(auth.NewMemoryStore()) })
	return multi
}

// ctxWithBearer 构造一个带 Authorization: Bearer <token> 的受保护路径上下文
func ctxWithBearer(token, path string) *app.RequestContext {
	c := &app.RequestContext{}
	c.Request.URI().SetPath(path) // c.Path() 读的就是 URI 里的这个
	if token != "" {
		c.Request.Header.Set("Authorization", "Bearer "+token)
	}
	return c
}

// allowed 中间件没有 Abort —— 即请求被放行到了下一个 handler
func allowed(c *app.RequestContext) bool { return !c.IsAborted() }

// jtiOf 取出 token 的 jti(access / refresh 都能取)
func jtiOf(t *testing.T, multi *auth.Multi, token string) string {
	t.Helper()
	if c, err := multi.ParseAnyAccess(token); err == nil {
		return c.JTI()
	}
	c, err := multi.ParseRefresh(token)
	require.NoError(t, err)
	return c.JTI()
}

// ================= 反向守卫:B 的回归 =================

// TestAuth_RejectsRefreshTokenOnProtectedRoute 是本文件最重要的一条。
//
// 拿一个**有效的**refresh token 打受保护路由,必须被拒。
// 前置断言特意先自证它确实是好的(能解析、用途是 refresh、未吊销)——
// 否则这条测试会因为"token 本来就是坏的"而绿,挡不住任何东西。
func TestAuth_RejectsRefreshTokenOnProtectedRoute(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	refresh, err := multi.RefreshByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	// 前置条件:这个 token 是好的 —— 用得了、没过期、没被吊销
	parsed, err := multi.ParseRefresh(refresh)
	require.NoError(t, err)
	revoked, err := auth.DefaultRevocation().IsRevoked(ctx, parsed.JTI())
	require.NoError(t, err)
	require.False(t, revoked, "前置条件:token 不该是吊销态")

	c := ctxWithBearer(refresh, "/rapid-admin/v1/system/users")
	Auth()(ctx, c)

	assert.False(t, allowed(c),
		"refresh token 打到了受保护路由并放行了 —— 那就是一张 720h 的万能通行证")
}

// TestAuth_AcceptsAccessTokenOnProtectedRoute 对照组。
//
// 上一条如果是因为"中间件坏了 / 路径不对 / 签名算法不对"而红,这里也会红。
// 两条一起才构成守卫:一个证明进不去,一个证明能进。
func TestAuth_AcceptsAccessTokenOnProtectedRoute(t *testing.T) {
	multi := setupAuthMW(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	c := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)

	require.True(t, allowed(c), "合法 access token 被拒了 —— 反向守卫的对照组不成立")

	// 光"没 abort"不够:caller 必须真的注入到 Hertz 上下文
	raw, ok := c.Get(service.CallerKey)
	require.True(t, ok, "caller 没注入")
	caller, ok := raw.(*service.Caller)
	require.True(t, ok, "caller 类型不对:%T", raw)
	assert.Equal(t, uint64(42), caller.UserID)
	assert.Equal(t, "role_user_admin", caller.RoleCode)
	assert.NotEmpty(t, caller.JTI, "caller.JTI 为空 —— 吊销链路将拿不到键")
}

// TestAuth_RejectsRevokedAccessToken 吊销校验这条路径此前从未执行过
// (两层守卫把它短路成了死代码)。这里先证未吊时放行,再吊,再打一次。
func TestAuth_RejectsRevokedAccessToken(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u",
	})
	require.NoError(t, err)

	// 先确认没吊时是放行的 —— 否则"吊了之后被拒"可能只是别的原因
	c0 := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(ctx, c0)
	require.True(t, allowed(c0), "前置条件:吊销前应放行")

	require.NoError(t, auth.DefaultRevocation().
		Revoke(ctx, jtiOf(t, multi, access), time.Hour))

	c1 := ctxWithBearer(access, "/rapid-admin/v1/system/users")
	Auth()(ctx, c1)
	assert.False(t, allowed(c1), "已吊销的 access token 仍然放行了")
}

// TestAuth_RejectsAccessTokenInQuery 是 M105 修的那条
//
// 原来 `ExtractToken` 有一句"兼容 query(仅供调试)":
//
//	if q := string(c.Query("access_token")); q != "" { return q }
//
// 它挂在**所有受保护路由**的鉴权路径上,不是"调试开关":
//   - token 进了 URL → 进 nginx access log、进浏览器历史、进 Referer
//     (跳到第三方站点时 Referer 会带上完整 URL);
//   - 本项目自己的 SSE 端点**根本不需要它** —— SSEAuth 有自己的三级取值
//     (Authorization → ?access_token= → cookie),见 sseauth.go。
//
// 文档 `docs/事件推送框架层设计方案V1.0.md` 也把 `?access_token=`
// 明确写成 **SSE 专属**的兜底手段,不是通用鉴权方式。
func TestAuth_RejectsAccessTokenInQuery(t *testing.T) {
	multi := setupAuthMW(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, Username: "u", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	c := &app.RequestContext{}
	c.Request.URI().SetPath("/rapid-admin/v1/system/users")
	c.Request.URI().SetQueryString("access_token=" + access)
	Auth()(context.Background(), c)

	assert.False(t, allowed(c),
		"?access_token= 被当成了通用鉴权方式 —— token 会进 access log / 浏览器历史 / Referer")
}

// TestAuth_PublicPathSkipsTokenCheck public 路径不该被 token 校验挡住
// (登录 / 续期接口自己就没有 access token)
func TestAuth_PublicPathSkipsTokenCheck(t *testing.T) {
	setupAuthMW(t)
	for _, p := range []string{
		"/rapid-admin/v1/public/auth/login",
		"/rapid-admin/v1/public/auth/refresh",
		"/healthz",
	} {
		c := ctxWithBearer("", p)
		Auth()(context.Background(), c)
		assert.True(t, allowed(c), "public 路径 %s 被 token 校验拦住了", p)
	}
}

// TestAuth_NoTokenRejected 受保护路径没带 token 必须拒
func TestAuth_NoTokenRejected(t *testing.T) {
	setupAuthMW(t)
	c := ctxWithBearer("", "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)
	assert.False(t, allowed(c), "无 token 访问受保护路由被放行了")
}

// TestAuth_RejectsTamperedAudience 篡改 token 里的 aud 但不改签名 → 验签必失败。
//
// aud 是"选哪个 secret"的依据,由签发方权威写入(W3 之前 aud 的 json tag
// 冲突让这个值压根到不了 wire,三个签发点只能各自手工补外层字段)。
func TestAuth_RejectsTamperedAudience(t *testing.T) {
	multi := setupAuthMW(t)

	owner, err := multi.SignByAudience(auth.AudienceOwner, &auth.Claims{
		UserID: 42, Username: "u",
	})
	require.NoError(t, err)

	// 把 payload 段解出来改掉 aud 再编回去 —— 签名段原样保留,所以验签必失败。
	tampered := tamperAudience(t, owner, "owner", "admin")
	require.NotEqual(t, owner, tampered, "没有改到 aud,本用例失去意义")

	c := ctxWithBearer(tampered, "/rapid-admin/v1/system/users")
	Auth()(context.Background(), c)
	assert.False(t, allowed(c), "篡改 aud 的 token 被放行了")
}

// tamperAudience 改 token 载荷里的 aud(签名不动)。
func tamperAudience(t *testing.T, token, from, to string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "不是三段式 JWT")

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err, "payload 段解不开")

	// aud 是 jwt.ClaimStrings,序列化成**数组**:{"aud":["owner"]}
	patched := strings.Replace(string(raw), `"aud":["`+from+`"]`, `"aud":["`+to+`"]`, 1)
	require.NotEqual(t, string(raw), patched, "没找到 aud=%q,载荷格式变了", from)

	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(patched))
	return strings.Join(parts, ".")
}
