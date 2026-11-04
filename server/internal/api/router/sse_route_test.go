// M119 —— GET /rapid-admin/v1/events 的中间件链顺序回归网
//
// # 用户报的现象
//
//	浏览器 EventSource 连 http://localhost:5173/rapid-admin/v1/events?access_token=eyJ…
//	→ 401「未登录」
//
// # 根因(不是 SSEAuth 写错了,是链序)
//
// 受保护组 `api` 的**组级**中间件是 Auth → Principal → CSRF → Audit,
// 而 SSEAuth 是**路由级**中间件 —— Hertz 里组级链排在路由级链**前面**。
//
//	请求 → Auth() → Principal() → CSRF() → Audit() → SSEAuth() → handler
//
// Auth() 的 ExtractToken **只认 Authorization header**(M105 刻意删掉了
// `?access_token=` 兜底)。EventSource 发不出自定义 header ⇒ Auth() 当场
// 401 + Abort ⇒ SSEAuth 根本没机会跑 ⇒ 它三级取值里的
// 「?access_token=」和「Cookie」两级是**死代码**。
//
// ⇒ 修法:把 /events 挪出受保护组,单开一个组,链序改成
//
//	  SSEAuth → Principal → Audit
//	(见 router.go 的 M119 注释块)
//
// # 本文件守的是什么
//
// 守的是**链序**,不是"某个中间件函数写对了没有"。SSEAuth 本身有单测
// (internal/api/middleware/sseauth_test.go),但那些用例是**直接把中间件
// 调一遍** —— 它们证明不了"它在真实路由链上排在 Auth 后面"。
//
// 而这恰恰是这次出问题的位置:中间件单测全绿,线上 SSE 恒 401。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
// 把 /events 挪回受保护组 `api`(即 `api.GET("/events", rbac(...), SSEAuth(), ...)`)
//
//	→ TestEventsRoute_QueryTokenIsRejectedBySSEAuth  RED(返「未登录」而不是「令牌无效」)
//	→ TestEventsRoute_NoTokenGetsSSESpecificHint    RED(没有 SSE 提示)
//	→ TestEventsRoute_ValidQueryTokenIsAccepted     RED(401)
package router

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/pkg/auth"
)

// setupSSEMW 注入真 JWT(带 typ/JTI 的真签发链),用例结束后还原。
//
// ⚠️ middleware.JWTs 是**进程级全局**,必须每例重置 —— 否则一个用例签发的
// token 会泄漏到下一个用例,让"这条测试到底测的是谁"变得说不清。
// 本包没有 t.Parallel(),顺序执行是安全的。
func setupSSEMW(t *testing.T) *auth.Multi {
	t.Helper()
	multi := auth.NewMulti(
		"router-test-admin-secret", "router-test-owner-secret", "router-test-supplier-secret",
		"router-test-issuer",
		2*time.Hour, 720*time.Hour, 5*time.Minute,
	)
	middleware.JWTs = multi
	t.Cleanup(func() { middleware.JWTs = nil })

	// 吊销存储也换成干净的内存实现 —— SSEAuth 会查 jti 吊销,
	// 用例之间不能互相污染。
	auth.SetDefaultRevocation(auth.NewMemoryStore())
	t.Cleanup(func() { auth.SetDefaultRevocation(auth.NewMemoryStore()) })
	return multi
}

// newSSEHertz 起一个只注册路由、不监听端口的 Hertz 实例。
//
// ⚠️ 用 server.WithHostPorts(":0"):注册路由本身不占端口,这个只是让
// server.Default 拿到一个合法配置(它会解析端口串)。
func newSSEHertz(t *testing.T) *server.Hertz {
	t.Helper()
	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, nil, nil)
	return h
}

// driveSSE 在**进程内**驱动一次请求,不走网络。
//
// ⚠️ 为什么不用 httptest + 真实端口:那是 e2e 的做法,起停端口慢、还会被
// 端口占用/CORS 之类的外部因素干扰。而这里要验的是**中间件链的顺序**,
// 用 engine.ServeHTTP 直接驱动路由树就够了 —— 链序完全一致。
//
// ⚠️ RequestContext.Request 是**值类型**(不是指针),所以下面这几个 setter
// 在零值 context 上直接可用:
//   - SetRequestURI:写请求行(Hertz 的 URI 解析从 Header.RequestURI 取)
//   - SetMethod:写方法(路由树按 method 分树)
//   - SetHost:写 Host(Engine.ServeHTTP 校验 HTTP/1.1 必须有 Host,
//     少了会直接 400 requiredHostBody)
func driveSSE(t *testing.T, h *server.Hertz, rawURL string) *app.RequestContext {
	t.Helper()
	c := &app.RequestContext{}
	c.Request.SetRequestURI(rawURL)
	c.Request.SetMethod("GET")
	c.Request.SetHost("localhost")
	h.ServeHTTP(context.Background(), c)
	return c
}

// TestEventsRoute_QueryTokenIsRejectedBySSEAuth 是本文件最重要的一条。
//
// 特征:**?access_token= 里的 token 会被 SSEAuth 读到并判定**。
//
// 修复前:请求先进组级 Auth(),它只认 Authorization header ⇒ 没有 ⇒
// 当场 401「未登录」+ Abort ⇒ SSEAuth 根本没跑。
//
// 修复后:SSEAuth 读到 query token,解析失败 ⇒ 「令牌无效」(40102)。
func TestEventsRoute_QueryTokenIsRejectedBySSEAuth(t *testing.T) {
	setupSSEMW(t)
	h := newSSEHertz(t)

	c := driveSSE(t, h, "/rapid-admin/v1/events?access_token=not-a-real-token")
	body := string(c.Response.Body())

	require.NotContains(t, body, "未登录",
		"报文是组级 Auth() 拒的 —— 说明 /events 还在受保护组里,SSEAuth 根本没轮到跑")
	require.Contains(t, body, "令牌无效",
		"应当由 SSEAuth 判定 query 里的 token 无效")
	assert.Equal(t, 401, c.Response.StatusCode(),
		"鉴权失败必须是 401(而不是 404/500)")
}

// TestEventsRoute_NoTokenGetsSSESpecificHint 守"没带 token"这一档。
//
// 修复前:普通 Auth() 的「未登录」。
// 修复后:SSEAuth 的「未登录(SSE 需 ?access_token= 或 Cookie)」——
// 它明确告诉调用方**这个端点怎么带 token**,这是 SSEAuth 存在的意义之一。
//
// ⚠️ 不能断言"不含未登录"—— SSEAuth 那条报文里也含"未登录"三个字。
// 所以这里断言 SSE 专属提示在。
func TestEventsRoute_NoTokenGetsSSESpecificHint(t *testing.T) {
	setupSSEMW(t)
	h := newSSEHertz(t)

	c := driveSSE(t, h, "/rapid-admin/v1/events")
	body := string(c.Response.Body())

	require.Contains(t, body, "SSE",
		"没带 token 时应当由 SSEAuth 给出带 SSE 提示的报文,而不是普通 Auth() 的裸「未登录」")
}

// TestEventsRoute_ValidQueryTokenIsAccepted 是对照组:证明"不是把所有请求都拒了"。
//
// 上一条如果是因为"路由压根没注册 / 中间件炸了 / 路径写错了"而红,这里也会红。
// 两条一起才构成守卫:一个证明 query token 这条路能走到 SSEAuth,一个证明
// 真的 token 能过鉴权。
func TestEventsRoute_ValidQueryTokenIsAccepted(t *testing.T) {
	multi := setupSSEMW(t)
	h := newSSEHertz(t)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, IdentityID: 7, Username: "u", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	c := driveSSE(t, h, "/rapid-admin/v1/events?access_token="+access)
	body := string(c.Response.Body())

	assert.NotEqual(t, 401, c.Response.StatusCode(),
		"有效 token 不该被 401 拒掉:body=%s", body)
	assert.NotContains(t, body, "令牌无效", "有效 token 不该被判无效")
	assert.NotContains(t, body, "未登录", "有效 token 不该被判未登录")
}
