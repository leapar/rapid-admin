// refresh 轮换的行为契约(W7)。
//
// 这些断言针对的是三个**具体**失效,不是泛泛的"能用就行"。
//
// # 1. 复用同一个 Claims 结构签两次(Claims.Fork 存在的原因)
//
// sign() 每次生成**新的随机 jti**,所以两张 token 的 JTI 其实**不同** ——
// 实测确认。坏的不是撞号,而是**内存里那份 claims 停留在第二张(下签的)
// token 上**:此后 claims.JTI() / claims.ExpiresAt 读到的都是**新 refresh**
// 的值,不是被消费掉的那张旧 token。
//
// 后果实测(把 Fork() 改回共享 claims,下面两条立刻转红):
//   - 第 5 步 Revoke(claims.JTI()) 吊掉的是**刚签出的新 refresh**,
//     被消费的旧 token 根本没被吊 → **轮换形同虚设,可无限重放**;
//   - 同时**新 refresh 一出生就是吊销态** → 用户这次续期成功、下次立刻被拒,
//     被踢回登录页;
//   - 吊销 TTL 也取错(算的是新 refresh 的 720h,不是旧 token 的剩余寿命)。
//
// # 2. 旧 refresh 轮换后仍可用 —— 轮换=单次使用,否则"轮换"只是多发了一堆 token。
//
// # 3. 签发失败被吞掉 —— `refresh, _ :=` 会返 200 + 空串。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// mintRefresh 给一个已存在的用户签一个 refresh token(模拟登录产物)
func mintRefresh(t *testing.T, multi *auth.Multi, userID, identityID uint64, roleCode string) string {
	t.Helper()
	tok, err := multi.RefreshByAudience(auth.AudienceAdmin, (&auth.Claims{
		UserID:     userID,
		IdentityID: identityID,
		Username:   "rot",
		RoleCode:   roleCode,
	}).Fork())
	require.NoError(t, err)
	return tok
}

// TestRefresh_RotationInvalidatesOldToken 是本文件的主断言。
func TestRefresh_RotationInvalidatesOldToken(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "rot", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	old := mintRefresh(t, buildTestJWTMulti(t), userID, identityID, "role_user_admin")

	tok, err := svc.RefreshToken(ctx, old)
	require.NoError(t, err, "首次续期应成功")
	require.NotEmpty(t, tok.RefreshToken)
	require.NotEmpty(t, tok.Token)

	// 旧的必须失效 —— 否则 refresh token 可以无限重放,轮换毫无意义
	_, err = svc.RefreshToken(ctx, old)
	assert.True(t, errIs(err, errcode.TokenInvalid),
		"轮换后旧 refresh 仍可用 —— 单次使用失效,泄露的 token 可以反复换发(实际 err=%v)", err)

	// 新的必须可用
	tok2, err := svc.RefreshToken(ctx, tok.RefreshToken)
	require.NoError(t, err, "新 refresh 应可用:轮换把链路掐断了")
	require.NotEmpty(t, tok2.Token)
}

// TestRefresh_AccessAndRefreshJTIDiffer 钉住"两张 token 各自可独立吊销"这条不变式。
//
// ⚠️ 诚实说明这条断言的**边界**:它**不是** Fork() 那个 bug 的复现。
// 实测(Fork() 改回共享 claims)这条**依然绿** —— 因为 sign() 每次生成新随机
// jti,两张 token 的 JTI 本来就不同。它挡住的是另一类改法:哪天有人把
// sign() 改成"调用方已给 jti 就不覆盖",那时复用同一个结构签两次才会撞号,
// 而撞号的后果是登出/轮换一个连带另一个。
//
// 真正复现 Fork() 缺陷的是上面那两条(旧 token 没被消费 / 新 token 一出生
// 就是吊销态),它们在共享 claims 下确实转红。
func TestRefresh_AccessAndRefreshJTIDiffer(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "rot2", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	multi := buildTestJWTMulti(t)
	old := mintRefresh(t, multi, userID, identityID, "role_user_admin")
	oldJTI := jtiOf(t, old) // 旧 token 的 JTI —— 注意不是 old 本身,那是 token 字符串

	tok, err := svc.RefreshToken(ctx, old)
	require.NoError(t, err)

	accessClaims, err := multi.MustGet(auth.AudienceAdmin).ParseAccess(tok.Token)
	require.NoError(t, err)
	refreshClaims, err := multi.MustGet(auth.AudienceAdmin).ParseRefresh(tok.RefreshToken)
	require.NoError(t, err)

	assert.NotEqual(t, accessClaims.JTI(), refreshClaims.JTI(),
		"access 与 refresh 共用一个 JTI —— 吊销一个会连带吊掉另一个")
	// ⚠️ 这里必须比 JTI,不能拿 refreshClaims.JTI() 去比 old(token 字符串):
	// 字符串不相等是废话,这条断言会永远绿。踩过一次。
	assert.NotEqual(t, oldJTI, refreshClaims.JTI(),
		"轮换没有产生新的 refresh:新 token 复用了被消费掉那个的 JTI")
}

// TestRefresh_NewTokenSurvivesItsOwnRotation 回归:上一条保证了 JTI 不同,
// 这一条证明"不同"真的落在了行为上 —— 续期拿到的新 access 不会被
// 这次续期自己的吊销动作带走。
func TestRefresh_NewTokenSurvivesItsOwnRotation(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "rot3", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	old := mintRefresh(t, buildTestJWTMulti(t), userID, identityID, "role_user_admin")
	tok, err := svc.RefreshToken(ctx, old)
	require.NoError(t, err)

	for _, jti := range []string{ // 新的一对里,两个都该是干净的
		jtiOf(t, tok.Token), jtiOf(t, tok.RefreshToken),
	} {
		revoked, err := auth.DefaultRevocation().IsRevoked(ctx, jti)
		require.NoError(t, err)
		assert.False(t, revoked,
			"刚签出来的 token 立刻处于吊销态 —— 用户续期成功却马上会被踢下线")
	}
}

func jtiOf(t *testing.T, token string) string {
	t.Helper()
	m := buildTestJWTMulti(t)
	c, err := m.MustGet(auth.AudienceAdmin).ParseAccess(token)
	if err != nil {
		c2, err2 := m.MustGet(auth.AudienceAdmin).ParseRefresh(token)
		require.NoError(t, err2)
		return c2.JTI()
	}
	return c.JTI()
}

// TestRefresh_RejectsAccessTokenAsRefresh 是 W3 typ 声明的 service 层守卫:
// access token 不能拿来续期(否则续期接口变成 token 提款机)。
func TestRefresh_RejectsAccessTokenAsRefresh(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "rot4", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	multi := buildTestJWTMulti(t)
	access, err := multi.SignByAudience(auth.AudienceAdmin, (&auth.Claims{
		UserID: userID, IdentityID: identityID, Username: "rot4",
	}).Fork())
	require.NoError(t, err)

	_, err = svc.RefreshToken(context.Background(), access)
	assert.True(t, errIs(err, errcode.TokenInvalid), "access token 不该能续期")
}

// TestRefresh_RejectsRevokedUpfront 吊销检查前置:即使一个 token 还没被
// 轮换消费过,只要它在别处被吊销了(登出/改密码),续期也必须拒。
func TestRefresh_RejectsRevokedUpfront(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "rot5", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	multi := buildTestJWTMulti(t)
	tok := mintRefresh(t, multi, userID, identityID, "role_user_admin")
	jti := jtiOf(t, tok)
	require.NoError(t, auth.DefaultRevocation().Revoke(ctx, jti, time.Hour))

	_, err := svc.RefreshToken(ctx, tok)
	assert.True(t, errIs(err, errcode.TokenInvalid), "已被吊销的 refresh 不该能续期")
}

// TestRefresh_RejectsInactiveUser 停用/离职的用户不该靠 refresh 续命。
func TestRefresh_RejectsInactiveUser(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "rot6", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	old := mintRefresh(t, buildTestJWTMulti(t), userID, identityID, "role_user_admin")
	require.NoError(t, db.Model(&model.SysUser{}).
		Where("id = ?", userID).Update("is_active", false).Error)

	_, err := svc.RefreshToken(ctx, old)
	assert.True(t, errIs(err, errcode.NoActiveIdentity), "已停用用户不该能续期")
}

// TestLogin_AccessAndRefreshJTIDiffer 把同一条不变式钉在登录路径上 ——
// 那是 token 唯一的常规来源,比续期更容易被"顺手改回去"。
func TestLogin_AccessAndRefreshJTIDiffer(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "loginjti", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	tok, err := svc.Login(context.Background(), LoginRequest{
		Username: "loginjti",
		Password: preHash("Pwd@1234567"),
		LoginIP:  "127.0.0.1",
	})
	require.NoError(t, err)

	multi := buildTestJWTMulti(t)
	ac, err := multi.MustGet(auth.AudienceAdmin).ParseAccess(tok.Token)
	require.NoError(t, err)
	rc, err := multi.MustGet(auth.AudienceAdmin).ParseRefresh(tok.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, ac.JTI(), rc.JTI(),
		"登录签发的 access / refresh 共用同一个 JTI —— 登出一个会连带吊掉另一个")
	_ = sonyflake.Next
}