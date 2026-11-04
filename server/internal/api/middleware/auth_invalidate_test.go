// Auth 中间件的作废线校验 —— 强制下线的**实际生效点**。
//
// # 为什么这一组必须在中间件测,service 层测不了
//
// service 层能证明"改密码把作废线推进了",证明不了"下一次带旧 token 的请求
// 会被拒"。中间件这条路径此前连 jti 吊销都没被执行过一次(见 auth_test.go 的
// 头注释),所以两道闸都必须在这里真的打一次 Auth()。
//
// # 与 auth_test.go 那组的分工
//
//   - auth_test.go:token **本身**对不对(用途 / 签名 / 吊销)
//   - 本文件:这张**有效的** token 在"用户改过密码"之后还该不该被认
//
// # 可逆探针(这一组当初是怎么验的)
//
//	变异                                          必须变红
//	Auth() 里 SessionInvalidated 闸整个删掉        TestAuth_RejectsAccessTokenIssuedBeforeInvalidationLine
//	SessionInvalidated 恒返回 false               同上
//	TokenInvalidated 判定方向写成 After(...)      同上
//	SessionInvalidated 查 userID=0 的线            同上
//
// 前三条验"闸在执行",第四条验"查的是这个用户的线"—— 后者是最阴险的一种错:
// 闸看起来在、判定也在跑,只是取错了用户的作废线,于是所有人都不受强制下线影响,
// 而日志里什么都看不到。
package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/auth"
)

// pushLineAfter 把作废线推到"宽限窗口之外" —— 用来模拟**存量**令牌:
// 存量令牌的 iat 在很久以前,只有把线推得足够远,才不会被 ClockSkewGrace 放过。
// 用它来模拟"重新登录后拿到的新 token"会得到相反的结果,别用错。
func pushLineAfter(ctx context.Context, t *testing.T, userID uint64) {
	t.Helper()
	require.NoError(t, auth.DefaultRevocation().
		InvalidateFrom(ctx, userID, time.Now().Add(auth.ClockSkewGrace+time.Minute)))
}

// pushLineNow 把作废线推到此刻 —— 用来模拟**此刻**刚改完密码。
// 随后签发的 token 的 iat 落在线的同一秒内(iat 精度是秒),靠 ClockSkewGrace
// 兜住这不足一秒的差 —— 这正是那个宽限存在的理由,这条用例把它钉住。
func pushLineNow(ctx context.Context, t *testing.T, userID uint64) {
	t.Helper()
	require.NoError(t, auth.DefaultRevocation().InvalidateFrom(ctx, userID, time.Now()))
}

// TestAuth_RejectsAccessTokenIssuedBeforeInvalidationLine 本组的核心。
//
// 模拟事故现场:用户手机被盗,另一台设备上的会话还在。他改完密码,
// 攻击者手里那张**完全有效**的 access token 打到受保护路由 —— 必须被拒。
func TestAuth_RejectsAccessTokenIssuedBeforeInvalidationLine(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	// 攻击者那张 token
	stolen, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, Username: "victim", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	// 前置自证:这张 token 在"改密码之前"是好的
	c0 := ctxWithBearer(stolen, "/rapid-admin/v1/system/users")
	Auth()(ctx, c0)
	require.True(t, allowed(c0),
		"前置条件不成立:改密码前这张 token 就进不去,后面的断言没有意义")

	// 用户发现被盗,改密码
	pushLineAfter(ctx, t, 42)

	c1 := ctxWithBearer(stolen, "/rapid-admin/v1/system/users")
	Auth()(ctx, c1)
	assert.False(t, allowed(c1),
		"改密码之后,改密码之前签发的 token 仍然畅通 —— "+
			"盗号者的会话在事故响应后依然有效,改密码这个动作形同虚设")
}

// TestAuth_AcceptsAccessTokenIssuedAfterInvalidationLine 反向守卫。
//
// 上一条如果是因为"中间件坏了 / 路径不对"而绿,这条会红。两条一起才算守卫。
func TestAuth_AcceptsAccessTokenIssuedAfterInvalidationLine(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	pushLineNow(ctx, t, 42)

	// 改密码**之后**重新登录拿到的 token
	fresh, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 42, Username: "victim", RoleCode: "role_user_admin",
	})
	require.NoError(t, err)

	c := ctxWithBearer(fresh, "/rapid-admin/v1/system/users")
	Auth()(ctx, c)
	assert.True(t, allowed(c),
		"改密码之后签发的 token 被拒 —— 用户改完密码当场登不进去,"+
			"而且是拿新密码也登不进去的死循环")
}

// TestAuth_InvalidationLineIsPerUser 改密码只能踢**这个用户**。
func TestAuth_InvalidationLineIsPerUser(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	// 42 号用户改密码
	pushLineAfter(ctx, t, 42)

	other, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{
		UserID: 43, Username: "bystander",
	})
	require.NoError(t, err)

	c := ctxWithBearer(other, "/rapid-admin/v1/system/users")
	Auth()(ctx, c)
	assert.True(t, allowed(c), "42 号用户改密码,把 43 号用户踢下线了")
}

// TestAuth_InvalidationLineDoesNotBlockPublicPaths public 路径不受作废线影响。
//
// 登录 / 续期接口自己就没有 access token,它们各自在 service 层判作废线。
// 若这里把它们拦下来,用户改完密码连登录页都进不去 —— 只能靠"清缓存再试"。
func TestAuth_InvalidationLineDoesNotBlockPublicPaths(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()
	pushLineAfter(ctx, t, 42)

	access, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{UserID: 42})
	require.NoError(t, err)

	for _, p := range []string{
		"/rapid-admin/v1/public/auth/login",
		"/rapid-admin/v1/public/auth/refresh",
		"/rapid-admin/v1/public/auth/reset-password-sms",
	} {
		c := ctxWithBearer(access, p)
		Auth()(ctx, c)
		assert.True(t, allowed(c), "public 路径 %s 被作废线拦住了", p)
	}
}

// TestAuth_InvalidationLineAndJTIRevocationAreIndependent 两道闸各管一段,
// 不能因为其中一道过了就放行另一道管的范围。
func TestAuth_InvalidationLineAndJTIRevocationAreIndependent(t *testing.T) {
	multi := setupAuthMW(t)
	ctx := context.Background()

	// 只推进作废线,不吊销 jti → 必须被拒(证明不是被 jti 那道闸拒的)
	onlyLine, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{UserID: 42})
	require.NoError(t, err)
	pushLineAfter(ctx, t, 42)

	c := ctxWithBearer(onlyLine, "/rapid-admin/v1/system/users")
	Auth()(ctx, c)
	assert.False(t, allowed(c), "只推进作废线(未吊销 jti)时竟然放行了")

	// 反向:另一个用户只吊销 jti、不推作废线 → 也必须被拒
	revokedOnly, err := multi.SignByAudience(auth.AudienceAdmin, &auth.Claims{UserID: 44})
	require.NoError(t, err)
	require.NoError(t, auth.DefaultRevocation().
		Revoke(ctx, jtiOf(t, multi, revokedOnly), time.Hour))

	c2 := ctxWithBearer(revokedOnly, "/rapid-admin/v1/system/users")
	Auth()(ctx, c2)
	assert.False(t, allowed(c2), "只吊销 jti(未推进作废线)时竟然放行了")
}
