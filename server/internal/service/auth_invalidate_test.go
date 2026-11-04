// 强制下线的 service 层接线测试。
//
// # 这一组回答的问题
//
// 逐 jti 吊销只处理"眼前这一张"。于是三个改密码的入口里:
//   - ChangePassword / ResetPassword:只踢当前这张 token,同用户其它会话照旧有效
//   - ResetPasswordBySms:**一个令牌都不踢** —— 短信重置正是盗号后的自救路径,
//     攻击者重置了密码、原用户的会话却完好,两者在同一时间窗里并存
//
// 本文件逐条钉住"这三个入口都会推进作废线",外加两条**否定断言**:
// Logout 与 SwitchIdentity 刻意**不**推进(见下),那两条比正向断言更重要 ——
// 它们防的是一个看起来更安全、实际制造事故的"修复"。
//
// 真正的"旧 token 被拒"由 middleware 包验证(那里才能打一次 Auth());
// 本文件证明的是"线确实被推到了正确的**用户**身上" —— 后者 service 层独有,
// 中间件那条路径根本拿不到 targetUserID。
//
// # ClockSkewGrace 在这一层怎么验
//
// 作废线的判定是 `iat + skew < line`。这里绝大多数用例传 **skew = 0**,即
// "只承认签发时间早于线的 token",这样断言与真实签发时刻解耦,不受测试里
// 三次登录挤在同一秒的影响。
//
// 生产用的是 ClockSkewGrace(60s),它给"改完密码当场重新登录"留了窗口;
// 那一窗口的行为由 TestChangePassword_SessionsInsideClockSkewGraceSurviveByDesign
// 单独钉住,不散落在别的用例里。
//
// # 可逆探针(这一组当初是怎么验的)
//
// 每条用例都做过一次"把实现改回去,看它会不会红"。下面是当时的变异 → 必须变红的用例,
// 将来动了 auth_impl.go 里任何一处 invalidateUserSessions 调用,照着这张表复核一次。
//
//	变异                                              必须变红
//	ChangePassword 不调 invalidateUserSessions        TestChangePassword_PushesInvalidationLine
//	ResetPassword 退回 `if target == caller.UserID`   TestResetPassword_AdminResetPushesTargetUserLine
//	ResetPasswordBySms 不推线                         TestResetPasswordBySms_PushesInvalidationLine
//	RefreshToken 删掉 SessionInvalidated 闸           TestRefreshToken_RejectedAfterPasswordChange
//	Logout 顺手推线                                   TestLogout_DoesNotPushInvalidationLine
//	SwitchIdentity 顺手推线                           TestSwitchIdentity_DoesNotPushInvalidationLine
//
// 最后两条是**反向**探针:往实现里加一行本不该存在的代码,用例必须变红。
// 它们防的是"以后有人觉得登出/换身份也该踢掉所有设备"这种改法 ——
// 那会让用户在手机上点退出,电脑上的会话一起消失。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/desensitize"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// lineOf 取该用户的作废线(零值 = 从未作废过)
func lineOf(ctx context.Context, t *testing.T, userID uint64) time.Time {
	t.Helper()
	at, err := auth.DefaultRevocation().InvalidateBefore(ctx, userID)
	require.NoError(t, err)
	return at
}

// multi 返回与 newAuthTestSvc 里那个**同 secret** 的 JWT manager。
//
// 用它而不是走 service:这一组要验的正是"线上那道判定会看到什么",
// 不是"service 有没有暴露 parse 方法"。secret 一致(见 buildTestJWTMulti),
// 所以解析结果与线上完全一致。
func multi(t *testing.T) *auth.Multi { return buildTestJWTMulti(t) }

// tokenIssuedAt 解析一张 access token,取出它的 iat
func tokenIssuedAt(t *testing.T, token string) time.Time {
	t.Helper()
	claims, err := multi(t).ParseAnyAccess(token)
	require.NoError(t, err, "解析刚签出来的 access token 失败 —— 本用例失去意义")
	require.NotNil(t, claims.IssuedAt, "sign() 权威写 iat")
	return claims.IssuedAt.Time
}

// refreshJTI 取一张 refresh token 的 jti
func refreshJTI(t *testing.T, token string) string {
	t.Helper()
	claims, err := multi(t).ParseRefresh(token)
	require.NoError(t, err, "解析刚签出来的 refresh token 失败 —— 本用例失去意义")
	return claims.JTI()
}

// refreshIssuedAt 取一张 refresh token 的 iat
func refreshIssuedAt(t *testing.T, token string) time.Time {
	t.Helper()
	claims, err := multi(t).ParseRefresh(token)
	require.NoError(t, err, "解析刚签出来的 refresh token 失败 —— 本用例失去意义")
	require.NotNil(t, claims.IssuedAt, "sign() 权威写 iat")
	return claims.IssuedAt.Time
}

// isRevoked 查某个 jti 当前是否已被吊销(jtiOf 复用 auth_refresh_test.go 里的那个)
func isRevoked(t *testing.T, ctx context.Context, jti string) bool {
	t.Helper()
	rev, err := auth.DefaultRevocation().IsRevoked(ctx, jti)
	require.NoError(t, err)
	return rev
}

// loginTimes 在三台"设备"上分别登录,返回三张 access token
func loginTimes(t *testing.T, svc AuthService, username, pwd string) []string {
	t.Helper()
	ctx := context.Background()
	out := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		l, err := svc.Login(ctx, LoginRequest{Username: username, Password: preHash(pwd)})
		require.NoError(t, err, "第 %d 次登录失败", i)
		out = append(out, l.Token)
	}
	return out
}

// assertSessionsCovered 断言"这些会话都被作废线覆盖"。
//
// 传 skew=0:只要线被推到了这些会话**之后**,它们就全在作废范围内。
// 这正是逐 jti 吊销做不到的事 —— 服务端根本没有"这个用户还有哪些 jti"的索引。
func assertSessionsCovered(t *testing.T, tokens []string, line time.Time, what string) {
	t.Helper()
	for i, tok := range tokens {
		iat := tokenIssuedAt(t, tok)
		assert.True(t, auth.TokenInvalidated(iat, line, 0),
			"%s:第 %d 个会话(签发于 %s)的签发时间不早于作废线(%s) —— "+
				"改密码之前打开的会话没有被覆盖,盗号者不会被踢掉",
			what, i, iat.Format(time.RFC3339Nano), line.Format(time.RFC3339Nano))
	}
}

// ===== ChangePassword =====

// TestChangePassword_PushesInvalidationLine 改密码必须推进作废线。
func TestChangePassword_PushesInvalidationLine(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "cp", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	require.True(t, lineOf(ctx, t, userID).IsZero(), "前置条件:改密码之前不该有作废线")

	require.NoError(t, svc.ChangePassword(ctx,
		mkAdminCaller(userID, identityID, "role_user_admin"),
		preHash("OldPwd@12345"), preHash("NewPwd@12345")))

	assert.False(t, lineOf(ctx, t, userID).IsZero(),
		"改密码没有推进作废线 —— 改密码之前签发的 token 还能继续用,"+
			"用户发现被盗后做的第一件事就此形同虚设")
}

// TestChangePassword_KillsSessionsOnOtherDevices 核心用例:别的设备上的会话也死。
func TestChangePassword_KillsSessionsOnOtherDevices(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "cp2", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	sessions := loginTimes(t, svc, "cp2", "OldPwd@12345")

	require.NoError(t, svc.ChangePassword(context.Background(),
		mkAdminCaller(userID, identityID, "role_user_admin"),
		preHash("OldPwd@12345"), preHash("NewPwd@12345")))

	assertSessionsCovered(t, sessions, lineOf(context.Background(), t, userID), "改密码")
}

// TestChangePassword_SessionsInsideClockSkewGraceSurviveByDesign 钉住那 60 秒窗口。
//
// 生产判定是 `iat + 60s < line`,所以**改密码前 60 秒内**打开的会话仍然有效。
// 这是刻意的取舍,不是漏洞:
//
//   - 去掉宽限(skew=0)会怎样?A 实例的时钟比 B 慢 90s,用户在 B 上改完密码、
//     紧接着在 A 上重新登录,A 签出的新 token 的 iat 比线还早 90s → 判定失效
//     → 用户拿**刚换的新密码**登不进去,而且现场没有任何可查的报错线索。
//   - 保留宽限的代价:改密码前 60 秒内打开的会话会活下来。真实事故里盗号者的
//     会话是几小时前打开的,这个窗口碰不上他。
//
// 写死这条是为了防止将来有人"顺手把宽限去掉"——那会把一个静默的锁死带进生产。
func TestChangePassword_SessionsInsideClockSkewGraceSurviveByDesign(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "cp4", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	sessions := loginTimes(t, svc, "cp4", "OldPwd@12345")
	require.NoError(t, svc.ChangePassword(context.Background(),
		mkAdminCaller(userID, identityID, "role_user_admin"),
		preHash("OldPwd@12345"), preHash("NewPwd@12345")))

	line := lineOf(context.Background(), t, userID)
	for i, tok := range sessions {
		iat := tokenIssuedAt(t, tok)
		// 同一条判定,只换 skew:0 → 死,ClockSkewGrace → 活。
		// 两条一起才说明"活下来"是宽限给的,不是作废线没生效。
		require.True(t, auth.TokenInvalidated(iat, line, 0),
			"前置条件不成立:第 %d 个会话连 skew=0 都不作废,说明根本没被线覆盖", i)
		assert.False(t, auth.TokenInvalidated(iat, line, auth.ClockSkewGrace),
			"改密码前 %s 内打开的会话被判失效 —— "+
				"要么宽限被人去掉了(跨实例时钟偏差会让用户拿新密码也登不进去),"+
				"要么宽限被放大到了会话寿命量级", auth.ClockSkewGrace)
	}

	// 改完之后重新登录,新会话必须能用(反向守卫)
	l2, err := svc.Login(context.Background(), LoginRequest{Username: "cp4", Password: preHash("NewPwd@12345")})
	require.NoError(t, err)
	assert.False(t, auth.TokenInvalidated(tokenIssuedAt(t, l2.Token), line, auth.ClockSkewGrace),
		"改完密码重新登录拿到的 token 被判失效 —— 用户会卡在「改完密码再也登不进去」的死循环")
}

// TestChangePassword_WrongOldPwdDoesNotPushLine 失败路径不许推线。
//
// 推了就是"用户输错一次旧密码,自己所有设备全被踢" —— 一个认证失败不该有
// 这种副作用。
func TestChangePassword_WrongOldPwdDoesNotPushLine(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "cp3", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	err := svc.ChangePassword(context.Background(), mkAdminCaller(userID, identityID, "role_user_admin"),
		preHash("WrongPwd@12345"), preHash("NewPwd@12345"))
	require.True(t, errIs(err, errcode.LoginFailed))

	assert.True(t, lineOf(context.Background(), t, userID).IsZero(),
		"旧密码校验失败却推进了作废线 —— 输错一次密码就把所有设备踢下线")
}

// ===== ResetPassword(管理员代重置) =====

// TestResetPassword_AdminResetPushesTargetUserLine 本组的核心修复。
//
// 之前整段吊销包在 `if targetUserID == caller.UserID` 里,于是**管理员重置了
// 某个用户的密码,对那个用户的存量会话毫无影响** —— 密码变了,他所有设备上的
// 登录全部照旧有效,最长 720h。而代重置恰恰是最常走的一条路径。
func TestResetPassword_AdminResetPushesTargetUserLine(t *testing.T) {
	db := newAuthTestDB(t)
	adminID, adminIdentity := seedAuthUser(t, db, "admin1", "role_super_admin", "Pwd@1234567")
	victimID, _ := seedAuthUser(t, db, "victim1", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	sessions := loginTimes(t, svc, "victim1", "Pwd@1234567")

	require.NoError(t, svc.ResetPassword(context.Background(),
		mkAdminCaller(adminID, adminIdentity, "role_super_admin"), victimID, preHash("Reset@123456")))

	line := lineOf(context.Background(), t, victimID)
	require.False(t, line.IsZero(),
		"管理员代重置没有推进**目标用户**的作废线 —— 被重置的那个人的所有会话仍然有效")
	assertSessionsCovered(t, sessions, line, "管理员代重置")

	// 管理员自己不该被波及 —— 他没改自己的密码
	assert.True(t, lineOf(context.Background(), t, adminID).IsZero(),
		"管理员代重置把管理员自己也踢下线了")
}

// TestResetPassword_SelfResetPushesOwnLine 自助重置(改的是自己)也要推。
func TestResetPassword_SelfResetPushesOwnLine(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "selfreset", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	require.NoError(t, svc.ResetPassword(context.Background(),
		mkAdminCaller(userID, identityID, "role_user_admin"), userID, preHash("NewPwd@12345")))

	assert.False(t, lineOf(context.Background(), t, userID).IsZero(), "自助重置没有推进自己的作废线")
}

// ===== ResetPasswordBySms =====

// seedSmsUser 写一个带手机号盲索引的用户(ResetPasswordBySms 只按 phone_hash 找人)
func seedSmsUser(t *testing.T, db *gorm.DB, username, plain, phone string) uint64 {
	t.Helper()
	userID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel:    model.BaseModel{ID: userID},
		Username:     username,
		PasswordHash: hashForSeed(t, plain),
		PhoneHash:    desensitize.PhoneHash(phone),
		Nickname:     username,
		IsActive:     true,
	}).Error)
	return userID
}

// seedSmsCode 写一条未使用的验证码
func seedSmsCode(t *testing.T, db *gorm.DB, phone, scene, code string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysSmsCode{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Phone:     phone,
		Scene:     scene,
		Code:      code,
		SendAt:    model.Time{Time: time.Now()},
	}).Error)
}

// TestResetPasswordBySms_PushesInvalidationLine 短信重置必须推进作废线。
//
// 这是整条链路上此前最严重的一处空白:该方法**一个令牌都不吊销**。
// 而短信重置恰恰是盗号后的自救/接管路径 —— 攻击者拿到手机号重置了密码、
// 掌握了账号,而原用户设备上那个仍然有效的会话照旧畅通,两者在同一时间窗并存。
func TestResetPasswordBySms_PushesInvalidationLine(t *testing.T) {
	db := newAuthTestDB(t)
	const phone = "13800000001"
	userID := seedSmsUser(t, db, "smsuser", "OldPwd@12345", phone)
	seedSmsCode(t, db, phone, model.SmsSceneResetPassword, "123456")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	require.True(t, lineOf(ctx, t, userID).IsZero(), "前置条件:重置之前不该有作废线")

	require.NoError(t, svc.ResetPasswordBySms(ctx, phone, "123456", preHash("NewPwd@12345")))

	assert.False(t, lineOf(ctx, t, userID).IsZero(),
		"短信重置没有推进作废线 —— 攻击者重置密码后,原用户的会话照样有效,"+
			"他可以在被踢出门的同时把原用户的东西改个遍")
}

// TestResetPasswordBySms_WrongCodeDoesNotPushLine 验证码错不许推线。
//
// 这是本组最容易被"顺手补上"的一条:任何人都能拿一个错验证码把某个用户的
// 所有会话踢下线 —— 那是一个不需要任何凭据的 DoS。
func TestResetPasswordBySms_WrongCodeDoesNotPushLine(t *testing.T) {
	db := newAuthTestDB(t)
	const phone = "13800000002"
	userID := seedSmsUser(t, db, "smsuser2", "OldPwd@12345", phone)
	seedSmsCode(t, db, phone, model.SmsSceneResetPassword, "123456")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	err := svc.ResetPasswordBySms(ctx, phone, "999999", preHash("NewPwd@12345"))
	require.True(t, errIs(err, errcode.SmsCodeInvalid), "验证码错应被拒")

	assert.True(t, lineOf(ctx, t, userID).IsZero(),
		"验证码校验失败却推进了作废线 —— 任何人都能用一个错验证码踢掉这个用户的所有会话")
}

// ===== RefreshToken 的第二道闸 =====

// TestRefreshToken_RejectedAfterPasswordChange 改密码对 refresh 侧同样生效。
//
// refresh 走的是公开路由 /rapid-admin/v1/public/auth/refresh,**不经过中间件**。
// 所以中间件那道闸对 refresh 完全够不着 —— 少了 service 层这一查,改密码只踢掉
// 半个账号:access 死了,可攻击者手里那张 refresh 还能换出一张 iat 落在作废线
// 之后的**全新** access,一路畅通到天荒地老。中间件那一组永远测不到这一段,
// 因为中间件根本见不到 refresh token。
//
// 这条用例分成两段断言,分开的原因值得写下来:
//
//	a) 改密码把线推到了这张 refresh 的签发时间**之后**(skew=0 判定)——
//	   证明这道闸和改密码用的是**同一个用户**的线。
//	b) 把线再往前推过 ClockSkewGrace,RefreshToken 必须拒 —— 证明闸真在执行。
//
// 为什么 b) 要多推一把:测试里登录和改密码挤在同一秒,而 iat 的精度就是秒
// (sign() 写 jwt.NewNumericDate(now))。生产判定带 60s 宽限,所以"改密码前
// 60 秒内签的 refresh 仍然有效"是**设计如此**,不是缺陷(取舍见
// TestChangePassword_SessionsInsideClockSkewGraceSurviveByDesign)。
// 真实事故里盗号者的 refresh 是几小时前签的,早就在线前面了 —— b) 推的就是
// 那个真实形态。硬把 a) 写成"改完密码立刻就拒",等于要求去掉宽限,而去掉宽限
// 会让跨实例时钟偏差下的用户拿刚换的新密码登不进去。
//
// ⚠️ 这条用例曾经是**空跑**的:它先用同一张 refresh 试了一次"改密码前可用",
// 那次调用已经把 refresh 轮换消费掉了,后面必然撞上"已消费"分支 —— 与作废线
// 无关也会返错,断言照样绿。现在前置自证换用**另一张** refresh,并在失败后
// 再查一次 jti 没被吊销,证明拒它的确实是作废线而不是轮换。
func TestRefreshToken_RejectedAfterPasswordChange(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "refreshp", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 主样本:这张 refresh 从头到尾不会被消费
	login, err := svc.Login(ctx, LoginRequest{Username: "refreshp", Password: preHash("OldPwd@12345")})
	require.NoError(t, err)
	victimJTI := refreshJTI(t, login.RefreshToken)
	victimIAT := refreshIssuedAt(t, login.RefreshToken)

	// 前置自证用**另一张** session —— 同一张会被轮换消费掉,把两个失败原因搅在一起
	probe, err := svc.Login(ctx, LoginRequest{Username: "refreshp", Password: preHash("OldPwd@12345")})
	require.NoError(t, err)
	_, err = svc.RefreshToken(ctx, probe.RefreshToken)
	require.NoError(t, err, "前置条件:改密码之前 refresh 必须可用")
	require.False(t, isRevoked(t, ctx, victimJTI),
		"前置条件不成立:主样本 refresh 已被消费,后面的失败与作废线无关")

	// a) 用户改密码 —— 线推到此刻,必须落在主样本 refresh 的签发时间之后
	require.NoError(t, svc.ChangePassword(ctx,
		mkAdminCaller(userID, identityID, "role_user_admin"),
		preHash("OldPwd@12345"), preHash("NewPwd@12345")))

	line := lineOf(ctx, t, userID)
	require.False(t, line.IsZero(), "改密码没有推进作废线")
	assert.True(t, auth.TokenInvalidated(victimIAT, line, 0),
		"改密码之后,作废线没有落在 refresh 的签发时间之后 —— "+
			"这道闸查的根本不是这个用户的线")

	// b) 补上宽限窗口:把线推到"改密码发生在会话签发 1 分钟之后"这一真实形态
	require.NoError(t, auth.DefaultRevocation().
		InvalidateFrom(ctx, userID, line.Add(auth.ClockSkewGrace+time.Minute)))

	_, err = svc.RefreshToken(ctx, login.RefreshToken)
	require.NotNil(t, err, "作废线已经越过了这张 refresh 的签发时间,它却仍然能续命 —— "+
		"RefreshToken 里少了一次 SessionInvalidated,改密码只踢掉了半个账号:"+
		"access 死了,攻击者手里这张 refresh 还能换出全新 access")
	assert.True(t, errIs(err, errcode.TokenInvalid), "应返 TokenInvalid,got %v", err)

	// 关键:消费它的代码在作废线判定**之后**,所以它压根没走到那一步
	assert.False(t, isRevoked(t, ctx, victimJTI),
		"这张 refresh 被当作已消费而吊销了 —— 那说明本用例拒它的原因是轮换而不是作废线,"+
			"把 service 层这道闸整个删掉它也会绿")
}

// TestRefreshToken_StillWorksForFreshSession 对照组:换了新密码重新登录的会话能续期。
func TestRefreshToken_StillWorksForFreshSession(t *testing.T) {
	db := newAuthTestDB(t)
	_, identityID := seedAuthUser(t, db, "freshp", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	var u model.SysUser
	require.NoError(t, db.Where("username = ?", "freshp").First(&u).Error)

	require.NoError(t, svc.ChangePassword(ctx, mkAdminCaller(u.ID, identityID, "role_user_admin"),
		preHash("OldPwd@12345"), preHash("NewPwd@12345")))

	// 改完之后重新登录,拿到全新的 refresh
	login, err := svc.Login(ctx, LoginRequest{Username: "freshp", Password: preHash("NewPwd@12345")})
	require.NoError(t, err)

	refreshed, err := svc.RefreshToken(ctx, login.RefreshToken)
	require.NoError(t, err,
		"改完密码重新登录的会话续期失败 —— 用户改完密码就再也登不进去")
	assert.NotEmpty(t, refreshed.Token)
}

// ===== 否定断言:哪些动作刻意**不**推进作废线 =====

// TestLogout_DoesNotPushInvalidationLine 本组最容易被"顺手修掉"的一条。
//
// 作废线是**全有或全无**的:一推就是全部设备一起下线。而登出要的是"这台设备
// 退出",推线的后果是"用户在手机上点了退出,电脑上的会话也一起没了",而且
// **每次登出都这样** —— 一个看起来更安全、实际制造事故的"修复"。
//
// SRS §2.2.1 #6 要的"可强制下线其他设备"(保留本设备、只踢别的)形状完全相反,
// 需要会话登记簿才能做,不是把线推后。见 docs/框架层未完成项清单v1.md 缺口 A。
func TestLogout_DoesNotPushInvalidationLine(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "lo", "role_user_admin", "Pwd@1234567")
	seedLoginLog(t, db, userID)
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 两台设备各自登录
	l1, err := svc.Login(ctx, LoginRequest{Username: "lo", Password: preHash("Pwd@1234567")})
	require.NoError(t, err)
	l2, err := svc.Login(ctx, LoginRequest{Username: "lo", Password: preHash("Pwd@1234567")})
	require.NoError(t, err)
	j1, j2 := jtiOf(t, l1.Token), jtiOf(t, l2.Token)
	require.NotEqual(t, j1, j2, "两次登录必须签出不同的 jti")

	// 在设备 1 上登出
	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	caller.JTI = j1
	require.NoError(t, svc.Logout(ctx, caller))

	assert.True(t, lineOf(ctx, t, userID).IsZero(),
		"登出推进了作废线 —— 用户退出登录,结果所有设备的会话一起没了。"+
			"登出是 per-device 的动作,推线是 per-user 的,两者不能混用")
	assert.True(t, isRevoked(t, ctx, j1), "登出没有吊销本机的 jti —— 这台设备退出了但 token 还能用")
	assert.False(t, isRevoked(t, ctx, j2), "登出把别的设备也踢了")
}

// TestSwitchIdentity_DoesNotPushInvalidationLine 换身份同理:在手机上切换身份,
// 不该把电脑上的会话一起踢掉。
//
// 目标身份特意用**非高敏**角色(role_project_admin):切到高敏角色要短信验证码,
// 那样会在作废线断言之前就返回 20105,用例变成测 SMS 而不是测作废线。
func TestSwitchIdentity_DoesNotPushInvalidationLine(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryID := seedAuthUser(t, db, "sw", "role_user_admin", "Pwd@1234567")
	secondID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondID},
		UserID:    userID,
		RoleCode:  "role_project_admin",
		RoleName:  "项目管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	_, err := svc.SwitchIdentity(ctx,
		mkAdminCaller(userID, primaryID, "role_project_admin"), secondID, "")
	require.NoError(t, err)

	assert.True(t, lineOf(ctx, t, userID).IsZero(),
		"切换身份推进了作废线 —— 在一台设备上换身份,把所有设备一起踢下线了")
}
