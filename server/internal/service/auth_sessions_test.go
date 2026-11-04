// 会话与设备管理(SRS §2.2.1 #6)—— 活跃设备列表 + 强制下线其他设备。
//
// # 这组测试在钉什么
//
// 1. **登记簿里必须带 sid**。没有它,sys_login_log 就只是"登录历史",
//    不是会话登记簿 —— 「强制下线」无从下手(没有句柄可吊销)。
// 2. **登出只能盖自己那一行**。旧实现取"最近一条 logout_at IS NULL 的记录",
//    两台设备时盖的是**另一台**的行 —— 登记簿一错,活跃设备列表就是错的。
// 3. **归属校验是硬要求**。sid 是能直接递给吊销存储的句柄,不校验归属
//    就等于"任意用户可踢任意用户下线"。
// 4. **被踢的 refresh 必须死**。这是"强制下线"这个功能的**行为**断言,
//    不是"调了某个函数"的断言。
//
// # 与 auth_session_test.go 的分工
//
//   - auth_session_test.go:登出(自己的那条会话)能不能一次结束
//   - 本文件:点名踢**别的**会话能不能踢掉
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// ===== 1. 登记簿必须带 sid =====

// TestLogin_WritesSessionIDIntoLoginLog 登录成功那一行必须带 sid。
//
// 这是整条链的**源头**:没有 sid,后面所有事(按 sid 定位行、按 sid 吊销)
// 都无从谈起。而且这个洞是**静默**的 —— 少写一个字段,接口照样返 200。
func TestLogin_WritesSessionIDIntoLoginLog(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "reg1", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	_, sid := loginForSession(t, svc, "reg1", "Pwd@1234567")
	require.NotEmpty(t, sid, "登录必须给会话 id")

	var row model.SysLoginLog
	require.NoError(t, db.Where("user_id = ?", userID).Order("id DESC").First(&row).Error)
	assert.Equal(t, sid, row.SessionID,
		"登记簿这一行没带 sid —— sys_login_log 就只是登录历史,不是会话登记簿,强制下线无从下手")
	assert.Equal(t, 1, row.Status, "成功登录的记录 status 必须是 1")
	assert.Nil(t, row.LogoutAt, "刚登录的记录不该有登出时间")
}

// TestLogin_FailureRowHasNoSessionID 登录失败那一行**没有** sid。
//
// 失败时压根没有会话。凭空造一个 sid 会让"活跃设备"列表里出现从未存在过的
// 设备 —— 而那台设备是能被"强制下线"的,吊的是一条不存在的会话。
func TestLogin_FailureRowHasNoSessionID(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "reg2", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	_, err := svc.Login(context.Background(), LoginRequest{
		Username: "reg2",
		Password: preHash("wrong-password"),
	})
	require.Error(t, err, "密码错必须登录失败")

	var row model.SysLoginLog
	require.NoError(t, db.Where("user_id = ?", userID).Order("id DESC").First(&row).Error)
	assert.Equal(t, 0, row.Status, "失败记录 status 必须是 0")
	assert.Empty(t, row.SessionID,
		"失败记录不能带 sid —— 失败时没有会话,凭空造一个会让活跃设备列表出现幽灵设备")
}

// ===== 2. 登出只能盖自己那一行 =====

// TestLogout_MarksOnlyItsOwnSessionRow 登出只盖**本会话**那一行。
//
// 这是多设备场景下登记簿正确性的地基。旧实现取"该用户最近一条
// logout_at IS NULL 的记录"—— 两台设备时,后登录那台(B)的行更新,
// 于是 A 的会话永远显示"未登出"、B 的会话明明活着却显示"已登出"。
// 活跃设备列表一错,"强制下线"就点不准人。
func TestLogout_MarksOnlyItsOwnSessionRow(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "reg3", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tokA, sidA := loginForSession(t, svc, "reg3", "Pwd@1234567")
	_, sidB := loginForSession(t, svc, "reg3", "Pwd@1234567")
	require.NotEqual(t, sidA, sidB)

	// 登出 A
	require.NoError(t, svc.Logout(ctx, callerFromTokens(t, tokA.Token)))

	var rowA, rowB model.SysLoginLog
	require.NoError(t, db.Where("session_id = ?", sidA).First(&rowA).Error)
	require.NoError(t, db.Where("session_id = ?", sidB).First(&rowB).Error)

	require.NotNil(t, rowA.LogoutAt, "A 登出了,A 那一行必须有登出时间")
	assert.Nil(t, rowB.LogoutAt,
		"登出 A 却把 B 的行盖上了 —— 登出按 sid 定位,不能按「最近一条未登出的记录」猜")
	assert.Equal(t, userID, rowA.UserID)
}

// TestLogout_IgnoresFailedLoginRow 登出不能盖住"登录失败"那一行。
//
// 旧实现取"最近一条 logout_at IS NULL 的记录" —— 而**失败记录永远没有
// logout_at**,于是"最近一条"很可能就是那条失败记录,登出时间盖到它头上,
// 一条失败记录被标成"已登出"。
func TestLogout_IgnoresFailedLoginRow(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "reg4", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 先来一次失败登录(它的行没有 logout_at,永远不会有)
	_, err := svc.Login(ctx, LoginRequest{Username: "reg4", Password: preHash("wrong-password")})
	require.Error(t, err)

	// 再成功登录
	tok, _ := loginForSession(t, svc, "reg4", "Pwd@1234567")
	require.NoError(t, svc.Logout(ctx, callerFromTokens(t, tok.Token)))

	var failRow model.SysLoginLog
	require.NoError(t, db.Where("user_id = ? AND status = 0", userID).First(&failRow).Error)
	assert.Nil(t, failRow.LogoutAt,
		"登出把登录失败那一行盖上了 —— 失败记录不是会话,不该有登出时间")
}

// TestLogout_LegacyTokenSkipsFailedRow 老 token(没有 sid)登出时,
// 不能盖住"登录失败"那一行。
//
// # 为什么要单独测这条
//
// 新 session 走 sid 定位,失败记录本来就不会被选中(sid 对不上)。
// 但**老 token 没有 sid**,走的是降级分支「最近一条没有 sid 的成功登录行」——
// 而失败记录既没有 sid 也永远没有 logout_at,于是它就是"最近一条"。
//
// 这个降级分支的窗口 = 上线前签发的 token 的最长寿命(720h)。窗口内都可能有
// 老 token 在流通,所以它不是"理论上不可达",是要**保证正确**的。
func TestLogout_LegacyTokenSkipsFailedRow(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "reg4b", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 一条**老样式**的成功登录行(没有 sid)
	legacyID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysLoginLog{
		BaseModel: model.BaseModel{ID: legacyID},
		UserID:    userID,
		Username:  "reg4b",
		Status:    1,
		Message:   "ok",
		LoginAt:   model.Time{Time: time.Now().Add(-2 * time.Minute)},
	}).Error)

	// 之后再来一次失败登录(更新的行,没有 sid,永远不会有 logout_at)
	_, err := svc.Login(ctx, LoginRequest{Username: "reg4b", Password: preHash("wrong-password")})
	require.Error(t, err)

	// 老 token 的 caller:没有 sid
	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	require.Empty(t, caller.SessionID, "前置条件:这是一个没有 sid 的老 caller")

	require.NoError(t, svc.Logout(ctx, caller))

	var failRow model.SysLoginLog
	require.NoError(t, db.Where("user_id = ? AND status = 0", userID).First(&failRow).Error)
	assert.Nil(t, failRow.LogoutAt,
		"老 token 登出把登录失败那一行盖上了 —— 降级分支必须限 status=1")

	var legacyRow model.SysLoginLog
	require.NoError(t, db.Where("id = ?", legacyID).First(&legacyRow).Error)
	assert.NotNil(t, legacyRow.LogoutAt, "老 token 登出该盖住**自己**那条老登录行")
}

// ===== 3. 活跃会话列表 =====

// TestListSessions_ReturnsOwnActiveSessions 只能看到自己的活跃会话。
func TestListSessions_ReturnsOwnActiveSessions(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg5", "role_user_admin", "Pwd@1234567")
	seedAuthUser(t, db, "reg6", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tokA, sidA := loginForSession(t, svc, "reg5", "Pwd@1234567")
	tokB, _ := loginForSession(t, svc, "reg6", "Pwd@1234567")

	ov, err := svc.ListSessions(ctx, callerFromTokens(t, tokA.Token), 1, 20)
	require.NoError(t, err)
	require.Len(t, ov.Active, 1, "A 只该看到自己那一条会话")
	assert.Equal(t, sidA, ov.Active[0].SessionID)

	ovB, err := svc.ListSessions(ctx, callerFromTokens(t, tokB.Token), 1, 20)
	require.NoError(t, err)
	require.Len(t, ovB.Active, 1, "B 只该看到自己那一条会话")
	assert.NotEqual(t, sidA, ovB.Active[0].SessionID, "B 不该看到 A 的会话")
}

// TestListSessions_MarksCurrentSession 当前这条要标出来。
//
// 前端靠 is_current 标"本机"并把"下线其他设备"的按钮排除掉这一条。
func TestListSessions_MarksCurrentSession(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg7", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, sid := loginForSession(t, svc, "reg7", "Pwd@1234567")

	ov, err := svc.ListSessions(ctx, callerFromTokens(t, tok.Token), 1, 20)
	require.NoError(t, err)
	require.Len(t, ov.Active, 1)
	assert.True(t, ov.Active[0].IsCurrent, "当前这条会话必须标出来")
	assert.Equal(t, sid, ov.Active[0].SessionID)
}

// TestListSessions_ExcludesExpiredSessions 过期会话不进活跃列表。
//
// 判据是 `COALESCE(last_active_at, login_at) + refresh_ttl <= now`。
// 测试里的 refresh_ttl 是 24h(buildTestJWTMulti),所以 48 小时前登录、
// 从没续期过的那条已经死了。
func TestListSessions_ExcludesExpiredSessions(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "reg8", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "reg8", "Pwd@1234567")

	// 手工插一条 48 小时前登录、从未续期的会话
	require.NoError(t, db.Create(&model.SysLoginLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID,
		Username:  "reg8",
		IP:        "10.0.0.9",
		Device:    "old-device",
		Status:    1,
		Message:   "ok",
		LoginAt:   model.Time{Time: time.Now().Add(-48 * time.Hour)},
		SessionID: "sid-already-expired-0001",
	}).Error)

	ov, err := svc.ListSessions(ctx, callerFromTokens(t, tok.Token), 1, 20)
	require.NoError(t, err)
	require.Len(t, ov.Active, 1, "过期会话不该出现在活跃设备里")
	assert.NotEqual(t, "sid-already-expired-0001", ov.Active[0].SessionID)
}

// TestListSessions_HistoryIncludesFailedLogins 历史记录要含失败登录。
//
// "历史登录记录"的价值有一半在失败的那些 —— 密码被反复试错是账号被盗的信号。
func TestListSessions_HistoryIncludesFailedLogins(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg9", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	_, err := svc.Login(ctx, LoginRequest{Username: "reg9", Password: preHash("wrong-password")})
	require.Error(t, err)
	tok, _ := loginForSession(t, svc, "reg9", "Pwd@1234567")

	ov, err := svc.ListSessions(ctx, callerFromTokens(t, tok.Token), 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(2), ov.History.Total, "历史记录应含 1 次失败 + 1 次成功")

	var sawFail bool
	for _, h := range ov.History.List {
		if h.Status == 0 {
			sawFail = true
		}
	}
	assert.True(t, sawFail, "历史记录里必须有失败的那次 —— 反复试错是盗号信号")
}

// ===== 4. 强制下线 =====

// TestRevokeSessions_KillsOtherSession 强制下线的**行为**断言:
// 被踢的那条会话,它的 refresh 必须换不动新 token。
//
// 这条是本文件的主断言。删掉 service 层那行 RevokeSession,它立刻转红。
func TestRevokeSessions_KillsOtherSession(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg10", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tokA, _ := loginForSession(t, svc, "reg10", "Pwd@1234567")
	tokB, sidB := loginForSession(t, svc, "reg10", "Pwd@1234567")

	n, err := svc.RevokeSessions(ctx, callerFromTokens(t, tokA.Token), []string{sidB})
	require.NoError(t, err)
	assert.Equal(t, 1, n, "应该真的下线了 1 条")

	// B 的 refresh 必须死
	_, err = svc.RefreshToken(ctx, tokB.RefreshToken)
	assert.True(t, errIs(err, errcode.TokenInvalid),
		"被强制下线的会话仍能续期 —— 强制下线没生效(实际 err=%v)", err)
}

// TestRevokeSessions_KeepsCurrentSessionAlive 强制下线**别的**设备,
// 不能连带把自己踢了(那是"推进作废线"的行为,形状相反)。
func TestRevokeSessions_KeepsCurrentSessionAlive(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg11", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tokA, _ := loginForSession(t, svc, "reg11", "Pwd@1234567")
	_, sidB := loginForSession(t, svc, "reg11", "Pwd@1234567")

	_, err := svc.RevokeSessions(ctx, callerFromTokens(t, tokA.Token), []string{sidB})
	require.NoError(t, err)

	// A 自己还活着
	_, err = svc.RefreshToken(ctx, tokA.RefreshToken)
	assert.NoError(t, err, "强制下线别的设备不该把本机也踢了 —— 那是作废线那种全有或全无的行为")
}

// TestRevokeSessions_CannotRevokeOwnCurrentSession 不能通过这个接口踢自己当前这条。
//
// 自己这条要走 /auth/logout(它连带把 access 的 jti 也吊了,语义完整)。
// 在这里吊掉自己这条,结果是"access 还活着、refresh 已死"——一种更难解释的
// 中间状态:用户眼前还好好地登着,却已经不能续期了。
func TestRevokeSessions_CannotRevokeOwnCurrentSession(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg12", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, sid := loginForSession(t, svc, "reg12", "Pwd@1234567")

	n, err := svc.RevokeSessions(ctx, callerFromTokens(t, tok.Token), []string{sid})
	require.NoError(t, err)
	assert.Equal(t, 0, n, "不该吊销自己当前这条会话")

	// 自己还活着
	_, err = svc.RefreshToken(ctx, tok.RefreshToken)
	assert.NoError(t, err, "自己的会话被这个接口踢了 —— 走错出口了")
}

// TestRevokeSessions_RejectsOtherUsersSession **归属校验**。
//
// session_id 是能直接递给吊销存储的句柄。不校验归属的话,任何登录用户都能
// 构造别人的 sid 把它吊掉 —— 变成"任意用户可踢任意用户下线"的 DoS。
//
// 这条测试是**安全断言**:B 传 A 的 sid,A 的会话必须毫发无损。
func TestRevokeSessions_RejectsOtherUsersSession(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg13", "role_user_admin", "Pwd@1234567")
	seedAuthUser(t, db, "reg14", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tokA, sidA := loginForSession(t, svc, "reg13", "Pwd@1234567")
	tokB, _ := loginForSession(t, svc, "reg14", "Pwd@1234567")
	require.NotEmpty(t, sidA, "这里要的是 **A 的** sid —— 拿到 B 自己的 sid 就测不到归属校验了")

	// B 拿着 A 的 sid 来踢
	_, err := svc.RevokeSessions(ctx, callerFromTokens(t, tokB.Token), []string{sidA})
	require.Error(t, err, "吊销别人的会话必须被拒")
	assert.True(t, errIs(err, errcode.BadRequest), "应该是参数错误(实际 err=%v)", err)

	// A 必须毫发无损
	_, err = svc.RefreshToken(ctx, tokA.RefreshToken)
	assert.NoError(t, err, "A 的会话被 B 踢掉了 —— 归属校验漏了,这是任意用户踢任意用户的洞")
}

// TestRevokeSessions_WritesLogoutAt 吊销后登记簿那一行要标成已下线。
//
// 不标的话,"活跃设备"列表会一直显示一台已经被踢掉的设备 —— 用户反复点
// "下线",系统每次都说成功,但那台设备其实早就没登录了。
func TestRevokeSessions_WritesLogoutAt(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg15", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tokA, _ := loginForSession(t, svc, "reg15", "Pwd@1234567")
	_, sidB := loginForSession(t, svc, "reg15", "Pwd@1234567")

	_, err := svc.RevokeSessions(ctx, callerFromTokens(t, tokA.Token), []string{sidB})
	require.NoError(t, err)

	var row model.SysLoginLog
	require.NoError(t, db.Where("session_id = ?", sidB).First(&row).Error)
	require.NotNil(t, row.LogoutAt, "被下线的会话在登记簿里要标出登出时间")

	// 活跃列表里不该再有它
	ov, err := svc.ListSessions(ctx, callerFromTokens(t, tokA.Token), 1, 20)
	require.NoError(t, err)
	for _, a := range ov.Active {
		assert.NotEqual(t, sidB, a.SessionID, "被下线的会话还在活跃列表里")
	}
}

// TestRevokeSessions_EmptyListIsNoOp 空列表 = 什么都不做,不是错误。
//
// "下线其他所有设备"在只有本机时就会传空数组(前端也会把按钮置灰),
// 接口不该因此报错。
func TestRevokeSessions_EmptyListIsNoOp(t *testing.T) {
	db := newAuthTestDB(t)
	seedAuthUser(t, db, "reg16", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, _ := loginForSession(t, svc, "reg16", "Pwd@1234567")

	n, err := svc.RevokeSessions(ctx, callerFromTokens(t, tok.Token), nil)
	require.NoError(t, err, "空列表不该报错")
	assert.Equal(t, 0, n)
}

// ===== 5. 续期心跳 =====

// TestRefresh_UpdatesLastActiveAt 续期要更新 last_active_at。
//
// 这一列是判"会话是否还活着"的依据:refresh token 每次轮换都重新起一个
// 720h 窗口,只看 login_at 会把"40 天前登录、昨天刚续期"的会话误判成死的
// —— 于是活跃设备列表里少了一台真在用的设备。
//
// ⚠️ M117-4:**登录本身就是一次活动**,所以刚登录完这一列**已经**有值了
// (等于登录时刻)。于是这条用例不能只看"续期后非空"(登录时就成立),
// 必须看"续期后**被推进了**" —— 而且得先把那一行改旧:
// 否则节流(DB 那道闸)认为"值还新鲜"而挡住写入,续期就成了空动作,
// 用例会变成"绿,但什么都没证明"。
func TestRefresh_UpdatesLastActiveAt(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "reg17", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	tok, sid := loginForSession(t, svc, "reg17", "Pwd@1234567")

	var before model.SysLoginLog
	require.NoError(t, db.Where("session_id = ?", sid).First(&before).Error)
	require.NotNil(t, before.LastActiveAt,
		"刚登录完 last_active_at 就该有值 —— 登录本身就是一次活动")

	// 把它改旧:模拟"这条会话 1 小时没动静了"
	old := model.Time{Time: time.Now().Add(-time.Hour)}
	require.NoError(t, db.Model(&model.SysLoginLog{}).
		Where("session_id = ?", sid).
		Update("last_active_at", &old).Error)

	_, err := svc.RefreshToken(ctx, tok.RefreshToken)
	require.NoError(t, err)

	var after model.SysLoginLog
	require.NoError(t, db.Where("session_id = ?", sid).First(&after).Error)
	require.NotNil(t, after.LastActiveAt,
		"续期没更新 last_active_at —— 活跃设备列表会把这台真在用的设备当成死的")
	assert.True(t, old.Before(after.LastActiveAt.Time),
		"续期后 last_active_at 应该被推进 —— 值停在旧时刻说明续期没写")
	assert.Equal(t, userID, after.UserID)
}
