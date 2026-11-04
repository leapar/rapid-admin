// 会话绑定(sid)在 service 层的行为契约 —— 关闭"登出只挡住一半"那个洞。
//
// # 洞是什么
//
// `Logout` 只 revoke 了 **access token 的 jti**。同一会话里那张 **refresh
// token** 纹丝不动 —— 用户点了"退出登录",那张 refresh 还能继续换发新 access,
// 最长活 720h(30 天)。
//
// 表现得像"登出没生效"或"登出后又自己登回来了",但根因是登出只做了一半。
//
// # 怎么关的
//
// 一次登录生成一个会话 id(sid),access 与 refresh 两张 token 共用它。
// 登出时吊销 sid → 两张一起死;refresh 续期时查 sid 吊销态 → 已被登出的会话
// 换不动了。中间件那道闸查的是 jti(不认 sid),而 refresh 走公开路由不经过
// 中间件,所以**只有 RefreshToken 这一处**会查会话态 —— 少了它就白改。
//
// # 这组测试最要紧的一条
//
// TestLogout_KillsRefreshToken —— 它是真的会红的(把 RevokeSession 那行删掉
// 立刻转红),不是"断言了一个恒真的东西"。
//
// ⚠️ 它曾经不是。改这一组测试时踩过:最早的写法在登出前先续期了一次,
// 而续期是"单次使用"(消费掉一张 refresh 的同时吊销它的 jti),于是登出后
// 那张 refresh 失败的原因是"它早就被消费了",跟"登出杀了它"无关 ——
// 删掉 RevokeSession 那行,它照样绿。已修正,细节见那条测试的注释。
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

// loginForSession 走完整 Login 流程,返回 token 对 + 该会话的 sid
func loginForSession(t *testing.T, svc AuthService, username, pwd string) (*AuthTokens, string) {
	t.Helper()
	tok, err := svc.Login(context.Background(), LoginRequest{
		Username: username,
		Password: preHash(pwd),
	})
	require.NoError(t, err, "登录失败")
	require.NotEmpty(t, tok.Token)
	require.NotEmpty(t, tok.RefreshToken)
	return tok, sessionIDOf(t, tok.Token)
}

// sessionIDOf 解析 access token 取出它的 sid
func sessionIDOf(t *testing.T, accessToken string) string {
	t.Helper()
	multi := buildTestJWTMulti(t)
	c, err := multi.MustGet(auth.AudienceAdmin).ParseAccess(accessToken)
	require.NoError(t, err)
	return c.SessionID
}

// callerFromTokens 由 access token 造一个 Caller(模拟中间件注入的那份)
func callerFromTokens(t *testing.T, accessToken string) *Caller {
	t.Helper()
	multi := buildTestJWTMulti(t)
	c, err := multi.MustGet(auth.AudienceAdmin).ParseAccess(accessToken)
	require.NoError(t, err)
	return &Caller{
		UserID:     c.UserID,
		IdentityID: c.IdentityID,
		RoleCode:   c.RoleCode,
		Username:   c.Username,
		Audience:   string(c.Audience()),
		IP:         "127.0.0.1",
		JTI:        c.JTI(),
		SessionID:  c.SessionID,
	}
}

// TestRefresh_WorksBeforeLogout 是本文件的**对照组**。
//
// 它存在的唯一理由:给下面那些"登出后 refresh 必须死"的断言一个"活着时
// refresh 确实是好的"的基准。没有它,那些断言可能是因为"refresh 本身一直
// 是坏的"而变绿 —— 那种绿不证明任何事。
func TestRefresh_WorksBeforeLogout(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "sess0", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "sess0", "Pwd@1234567")

	_, err := svc.RefreshToken(ctx, tok.RefreshToken)
	require.NoError(t, err, "登出前 refresh 必须能用 —— 不然后面的断言没有对照")
}

// TestLogout_KillsRefreshToken 是本文件的主断言。
//
// 登出之后,那张 refresh 必须换不动新 token —— 否则"退出登录"只退出了一半。
//
// ⚠️ 这条断言**不能**在登出前先续期一次。
//
// 续期是"单次使用":RefreshToken 消费掉一张 refresh 的同时会吊销它的 jti
// (见 auth_impl.go 续期流程第 5 步)。于是"登出前续期过一次"再登出,
// 登出后那张 refresh 失败的原因是**它早就被消费了**,而不是"登出杀了它" ——
// 把 RevokeSession 那行删掉,这条断言照样是绿的。它是一句假话。
//
// 轮换过的场景由 TestLogout_KillsRefreshToken_EvenAfterRotation 单独覆盖
// (那条是真的会红的)。
func TestLogout_KillsRefreshToken(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "sess1", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, sid := loginForSession(t, svc, "sess1", "Pwd@1234567")
	require.NotEmpty(t, sid, "登录必须给会话 id —— 没有它登出就只吊销一半")

	// 登出
	require.NoError(t, svc.Logout(ctx, callerFromTokens(t, tok.Token)))

	// 登出后:那张 refresh 必须死
	_, err := svc.RefreshToken(ctx, tok.RefreshToken)
	assert.True(t, errIs(err, errcode.TokenInvalid),
		"登出后 refresh 仍能续期 —— 登出只做了一半,那张 refresh 还能换发新 access(实际 err=%v)", err)
}

// TestLogout_KillsRefreshToken_EvenAfterRotation 轮换过的会话也一样。
//
// 轮换会换掉 access 的 jti,但**不换 sid**。如果登出是按"当前 access 的 jti"
// 去找 refresh 的,那用户在 A 标签页登出、B 标签页(拿着轮换后的 access)
// 继续续期,就绕过去了。sid 是会话级的,所以绕不过去。
func TestLogout_KillsRefreshToken_EvenAfterRotation(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "sess2", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, sid := loginForSession(t, svc, "sess2", "Pwd@1234567")

	// 轮换一次:access / refresh 的 jti 都换了,sid 不换
	rotated, err := svc.RefreshToken(ctx, tok.RefreshToken)
	require.NoError(t, err)
	require.Equal(t, sid, sessionIDOf(t, rotated.Token),
		"轮换不能换 sid —— 换了会话绑定就断了")
	require.NotEqual(t, sid, "", "sanity")

	// 用轮换后的 access 登出
	require.NoError(t, svc.Logout(ctx, callerFromTokens(t, rotated.Token)))

	// 轮换后拿到的那张 refresh 必须死
	_, err = svc.RefreshToken(ctx, rotated.RefreshToken)
	assert.True(t, errIs(err, errcode.TokenInvalid),
		"登出后轮换出来的 refresh 仍能续期 —— 会话绑定在轮换处断了(实际 err=%v)", err)
}

// TestLogout_DoesNotKillOtherSessions 登出只结束**这一条**会话。
//
// 这是 sid 相对"推进作废线"的核心优势:作废线是全有或全无的
// (用户在手机上退出,电脑上的会话也一起没了),sid 是 per-session 的。
func TestLogout_DoesNotKillOtherSessions(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "sess3", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tokA, sidA := loginForSession(t, svc, "sess3", "Pwd@1234567")
	tokB, sidB := loginForSession(t, svc, "sess3", "Pwd@1234567")
	require.NotEqual(t, sidA, sidB, "两次登录必须是两条不同会话")
	require.NotEqual(t, tokA.RefreshToken, tokB.RefreshToken)

	// 登出 A
	require.NoError(t, svc.Logout(ctx, callerFromTokens(t, tokA.Token)))

	// A 的 refresh 死了
	_, err := svc.RefreshToken(ctx, tokA.RefreshToken)
	assert.True(t, errIs(err, errcode.TokenInvalid), "A 的 refresh 应失效(实际 err=%v)", err)

	// B 的 refresh 还活着 —— 登出是 per-device 的
	_, err = svc.RefreshToken(ctx, tokB.RefreshToken)
	assert.NoError(t, err, "B 的会话不该被 A 的登出牵连 —— 作废线那种全有或全无的行为不能出现在这里")
}

// TestRefresh_BindsSessionIDWhenAbsent 老 refresh(没有 sid)续期后要拿到 sid。
//
// 本轮上线前签发的 token 没有 sid claim。那种会话在第一次续期时被绑定,
// 从此登出能一次结束它。绑定之前的那张 refresh 会被轮换掉(单次使用),
// 所以残留窗口只覆盖"上线前签发、且一次都没被用过"的那批 token。
func TestRefresh_BindsSessionIDWhenAbsent(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "sess4", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 手工签一张**没有 sid** 的 refresh(模拟上线前签发的 token)
	multi := buildTestJWTMulti(t)
	old := mintRefresh(t, multi, userID, identityID, "role_user_admin")
	oldClaims, err := multi.MustGet(auth.AudienceAdmin).ParseRefresh(old)
	require.NoError(t, err)
	require.Empty(t, oldClaims.SessionID, "前置条件:这张 token 就没有 sid")

	tok, err := svc.RefreshToken(ctx, old)
	require.NoError(t, err)

	newSID := sessionIDOf(t, tok.Token)
	require.NotEmpty(t, newSID, "续期后必须给会话补上 sid —— 否则这条会话永远绑不上")
	// 两张新 token 的 sid 必须一致
	multi2 := buildTestJWTMulti(t)
	rc, err := multi2.MustGet(auth.AudienceAdmin).ParseRefresh(tok.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, newSID, rc.SessionID, "access 与 refresh 的 sid 必须一致")
}

// TestSwitchIdentity_RotatesSessionID 切换身份**换** sid,并吊销旧 sid。
//
// ⚠️ **M148(2026-11-01)推翻了"切换身份不换 sid"这条**
// (本文件里原来那条 TestSwitchIdentity_KeepsSessionID 锁的正是旧行为)。
//
// 换之前的洞:切换时服务端只拿得到 **access** 的 jti,拿不到切换**之前**
// 那张 **refresh** 的 jti ⇒ 吊销不了它 ⇒ 它还能换发一份**旧身份**的 access
// (jti 没吊 / sid 没变 / 作废线没推 —— 三道闸全过)。
// ⇒ 危害:用户从高危身份切到低危身份(超管 → 普通用户)之后,手里那张旧
// refresh **还能继续拿超管**,最长 720h(30 天)。
// "切身份"这个动作在安全边界上等于什么也没做。
//
// 现在:换 sid + 吊旧 sid ⇒ 旧会话的 access + refresh 一起死,
// **不管中间轮换过几轮**(轮换只换 jti,sid 不变)。
func TestSwitchIdentity_RotatesSessionID(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "sess5", "role_user_admin", "Pwd@1234567")
	// 加第 2 个身份
	secondIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdentID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "用户管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, sid := loginForSession(t, svc, "sess5", "Pwd@1234567")
	require.NotEmpty(t, sid)

	res, err := svc.SwitchIdentity(ctx, callerFromTokens(t, tok.Token), secondIdentID, "")
	require.NoError(t, err)

	newSID := sessionIDOf(t, res.Token)
	require.NotEmpty(t, newSID, "切换后的 token 必须带 sid")
	assert.NotEqual(t, sid, newSID,
		"切换身份必须换 sid —— 不换的话切换前那张 refresh 还能换发旧身份的 access")

	// 旧 sid 必须已被吊销(否则旧 refresh 换不动,那才是真修上了)
	revoked, err := auth.IsSessionRevoked(ctx, sid)
	require.NoError(t, err)
	assert.True(t, revoked, "旧 sid 必须被吊销 —— 否则切换前那张 refresh 还能续期")
}

// TestSwitchIdentity_KillsPreSwitchRefresh 切换身份之后,**切换之前**那张
// refresh 必须立刻死 —— 不用等登出。
//
// 这是 M148 的**行为级**断言(上面那条断言的是"换了 sid",这条断言的是
// "旧 refresh 真的换不动了")。
//
// ⚠️ **这条以前是红的**:切换只吊销了 access 的 jti,旧 refresh 的 jti
// 服务端根本拿不到 ⇒ 它还能续期,而且续出来的是**旧身份**的 access。
func TestSwitchIdentity_KillsPreSwitchRefresh(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "sess8", "role_user_admin", "Pwd@1234567")
	secondIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdentID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "用户管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "sess8", "Pwd@1234567")
	_, err := svc.SwitchIdentity(ctx, callerFromTokens(t, tok.Token), secondIdentID, "")
	require.NoError(t, err)

	// 切换**之前**那张 refresh 必须换不动了
	_, err = svc.RefreshToken(ctx, tok.RefreshToken)
	assert.True(t, errIs(err, errcode.TokenInvalid),
		"切换身份后,切换前那张 refresh 仍能续期 —— 它换发的是**旧身份**的 access(实际 err=%v)", err)
}

// TestSwitchIdentity_NewRefreshCarriesNewIdentity 切换之后拿到的那张 refresh
// 必须换发出**新身份**的 access —— 换 sid 不能把身份换丢,也不能把用户自己
// 踢下线(换 sid 是安全动作,不是"把会话作废")。
func TestSwitchIdentity_NewRefreshCarriesNewIdentity(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "sess9", "role_user_admin", "Pwd@1234567")
	secondIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdentID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "用户管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "sess9", "Pwd@1234567")
	res, err := svc.SwitchIdentity(ctx, callerFromTokens(t, tok.Token), secondIdentID, "")
	require.NoError(t, err)

	// 切换后那张 refresh 必须还能续期
	res2, err := svc.RefreshToken(ctx, res.RefreshToken)
	require.NoError(t, err, "切换后拿到的那张 refresh 必须还能续期")

	claims, err := buildTestJWTMulti(t).MustGet(auth.AudienceAdmin).ParseAccess(res2.Token)
	require.NoError(t, err)
	assert.Equal(t, secondIdentID, claims.IdentityID, "续期出来的必须是**新身份**")
	assert.NotEmpty(t, claims.SessionID, "续期出来的 token 必须还带 sid(会话绑定不能断)")
}

// TestSwitchIdentity_UpdatesLoginLogSessionID 登记簿(sys_login_log)那一行的
// session_id 必须跟着换。
//
// ⚠️ **为什么这条不能省**:一次登录 = 一行 = 一条会话,靠 session_id 关联 sid。
// 不换的话三处一起失灵:
//   - touchSession 找不到行 ⇒ last_active_at 永远不更新
//   - Logout 按 sid 定位写 logout_at ⇒ 找不到行 ⇒ 列表里那条永远"未登出"
//   - 「强制下线」按 sid 吊销 ⇒ 吊的是已经作废的旧 sid ⇒ 报"会话不存在"
func TestSwitchIdentity_UpdatesLoginLogSessionID(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "sess10", "role_user_admin", "Pwd@1234567")
	secondIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdentID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "用户管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, oldSID := loginForSession(t, svc, "sess10", "Pwd@1234567")
	require.NotEmpty(t, oldSID)

	res, err := svc.SwitchIdentity(ctx, callerFromTokens(t, tok.Token), secondIdentID, "")
	require.NoError(t, err)
	newSID := sessionIDOf(t, res.Token)

	var row model.SysLoginLog
	require.NoError(t, db.Where("user_id = ? AND status = 1", userID).First(&row).Error)
	assert.Equal(t, newSID, row.SessionID,
		"登记簿的 session_id 必须跟着换 —— 不换的话活跃设备列表 / 强制下线 / 登出写 logout_at 全部失灵")
}

// TestSwitchIdentity_UpdatesCallerRole 切换之后 caller 必须变成"切换后的身份"。
//
// ⚠️ **M148:以前 service 层什么都不改**,而 handler 那行注释写着
// "SwitchIdentity 内部已改 caller.RoleCode" ⇒ to_role 恒等于 from_role
// ⇒ biz_role_switch_audit 那张表永远记不出"角色变了"
// (而它存在的意义就是记这个 —— 查"谁把谁提成了超管"时看到的 from == to,
// 等于没有记录)。
//
// ⚠️ 用的是**非高敏**角色:切到高敏角色要短信二次验证(那是另一条用例,
// 见 auth_sms_switch_test.go)。
func TestSwitchIdentity_UpdatesCallerRole(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "sess11", "role_platform_ops", "Pwd@1234567")
	secondIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdentID},
		UserID:    userID,
		RoleCode:  model.RoleReadOnly,
		RoleName:  "只读查看员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "sess11", "Pwd@1234567")
	caller := callerFromTokens(t, tok.Token)
	require.Equal(t, model.RolePlatformOps, caller.RoleCode, "前置条件:切换前 caller 是平台运营员")

	_, err := svc.SwitchIdentity(ctx, caller, secondIdentID, "")
	require.NoError(t, err)

	assert.Equal(t, model.RoleReadOnly, caller.RoleCode,
		"切换后 caller.RoleCode 必须变成目标角色 —— 审计的 to_role 从这儿取")
	assert.Equal(t, secondIdentID, caller.IdentityID, "切换后 caller.IdentityID 必须变成目标身份")
}

// TestSwitchIdentity_ThenLogout_KillsPreSwitchRefresh 切换身份后登出,
// **切换之前**那张 refresh 也要死。
//
// ⚠️ **M148(2026-11-01):这条的理由变了。**
//
// 以前它守的是"切换身份不换 sid"那条设计:切换时换了 sid,那两张 refresh
// 就分属两条会话,登出只能结束其中一条 —— 切换前拿到的那张会活下来。
// ⇒ 旧设计靠"不换 sid"来让登出一次结束两张。
//
// 现在换 sid 了(M148),但**旧 sid 在切换那一步就被吊销**,所以切换前那张
// refresh 在切换之后**立刻**就死了,压根不用等登出
// (见 TestSwitchIdentity_KillsPreSwitchRefresh)。
// 这条保留下来做**端到端**口径:切换 → 登出 → 旧 refresh 换不动。
func TestSwitchIdentity_ThenLogout_KillsPreSwitchRefresh(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "sess7", "role_user_admin", "Pwd@1234567")
	secondIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdentID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "用户管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "sess7", "Pwd@1234567")
	res, err := svc.SwitchIdentity(ctx, callerFromTokens(t, tok.Token), secondIdentID, "")
	require.NoError(t, err)

	// 用切换后那张 access 登出
	require.NoError(t, svc.Logout(ctx, callerFromTokens(t, res.Token)))

	// 切换**之前**那张 refresh 必须死
	_, err = svc.RefreshToken(ctx, tok.RefreshToken)
	assert.True(t, errIs(err, errcode.TokenInvalid),
		"切换身份后登出,切换前拿的 refresh 仍能续期 —— 会话绑定在切换身份处断了(实际 err=%v)", err)
}

// TestSwitchIdentity_ThenLogout_KillsNewRefresh 切换身份后登出,新拿的 refresh 也要死。
//
// 这是上一条的**后果**:切换身份换 sid(M148),用户在切换后登出,
// 登出吊的是**新** sid —— 切换后拿到的那张 refresh 必须跟着死,
// 而它正是切换流程亲手发的那张。
func TestSwitchIdentity_ThenLogout_KillsNewRefresh(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "sess6", "role_user_admin", "Pwd@1234567")
	secondIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondIdentID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "用户管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "sess6", "Pwd@1234567")
	res, err := svc.SwitchIdentity(ctx, callerFromTokens(t, tok.Token), secondIdentID, "")
	require.NoError(t, err)

	// 用切换后那张 access 登出
	require.NoError(t, svc.Logout(ctx, callerFromTokens(t, res.Token)))

	// 切换后拿到的那张 refresh 必须死
	_, err = svc.RefreshToken(ctx, res.RefreshToken)
	assert.True(t, errIs(err, errcode.TokenInvalid),
		"切换身份后登出,新拿的 refresh 仍能续期 —— 会话绑定在切换身份处断了(实际 err=%v)", err)
}
