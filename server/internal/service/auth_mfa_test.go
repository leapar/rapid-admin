// 登录二次验证 MFA(M150,2026-11-03)
//
// # 这组用例守的是什么
//
//	MFA 的安全边界全在 service 层,handler 只是壳。四条设计决策各有一条
//	反向用例钉着,谁改坏了立刻红:
//
//	  1. 码的 scene 独立(mfa ≠ login)—— LoginBySms 消费不到 MFA 的码,
//	     否则"短信登录"就成了 MFA 的总后门(TestLoginBySms_MfaCodeNotUsable)。
//	  2. 开/关 MFA 都要验码 —— 偷会话的人不能顺手把 MFA 关了
//	     (TestDisableMfa_WrongCodeRejected)。
//	  3. MFA 挑战不是登录失败 —— 不带码的第一次登录返 20109 但**不计数**;
//	     带错码才按登录失败计(TestLogin_MFAChallenge_NotCounted vs
//	     TestLogin_MFA_WrongCode_CountsAsLoginFail)。
//	  4. 开 MFA 的前提 = 账号已绑手机 —— 没绑手机的账号开不了
//	     (TestEnableMfa_NoPhoneRejected),开了之后号被清的走管理员恢复
//	     (TestLogin_MFA_NoPhone_FailClosed)。
//
//	与 auth_sms_switch_test.go 同一套夹具(console sender + 真 JWT multi)。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

const mfaTestPwd = "Pwd@1234567"

func TestEnableMfa_NoPhoneRejected(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identID := seedAuthUser(t, db, "mfanophone", "role_user_admin", mfaTestPwd)
	// 故意不绑手机号

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, identID, "role_user_admin")

	_, err := svc.EnableMfa(context.Background(), caller, "123456")
	require.Error(t, err)
	// accountPhone 的口径:没登记手机号 → InvalidParam
	assert.True(t, errIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
}

func TestEnableMfa_SuccessAndIdempotentRepeatRejected(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identID := seedAuthUser(t, db, "mfaenable", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, identID, "role_user_admin")
	ctx := context.Background()

	// 发码 + 开启
	require.NoError(t, svc.SendAccountSms(ctx, caller, model.SmsSceneMFA))
	code := lastSmsCode(t, db, testPhone, model.SmsSceneMFA)
	view, err := svc.EnableMfa(ctx, caller, code)
	require.NoError(t, err)
	assert.True(t, view.MfaEnabled, "开启后视图必须是 true")

	// 同一个码用第二次:状态检查(enable == u.MfaEnabled)在消费码**之前**,
	// 所以这里先撞上"已开启"的 StateInvalid,而不是码相关的错 ——
	// 无论哪条,重复开启都不静默成功
	_, err = svc.EnableMfa(ctx, caller, code)
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.StateInvalid), "want StateInvalid, got %v", err)

	// 换一条新码(手工种,避开发码冷却)依然是"已开启" —— 幂等重复按状态错报
	seedSmsCode(t, db, testPhone, model.SmsSceneMFA, "246810")
	_, err = svc.EnableMfa(ctx, caller, "246810")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.StateInvalid), "want StateInvalid, got %v", err)
}

func TestDisableMfa_RequiresCode(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identID := seedAuthUser(t, db, "mfadisable", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, identID, "role_user_admin")
	ctx := context.Background()

	// 先开起来
	require.NoError(t, svc.SendAccountSms(ctx, caller, model.SmsSceneMFA))
	view, err := svc.EnableMfa(ctx, caller, lastSmsCode(t, db, testPhone, model.SmsSceneMFA))
	require.NoError(t, err)

	// 关 MFA 不带有效码 = 拒:偷到会话的人不能顺手把 MFA 关掉
	// (种一条真码,但故意用错的去试 —— 证明校验是真的在比对)
	seedSmsCode(t, db, testPhone, model.SmsSceneMFA, "111222")
	_, err = svc.DisableMfa(ctx, caller, "000000")
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.SmsCodeInvalid), "want SmsCodeInvalid, got %v", err)

	// 带对码 → 关掉,视图翻 false
	view, err = svc.DisableMfa(ctx, caller, "111222")
	require.NoError(t, err)
	assert.False(t, view.MfaEnabled)
}

// TestLogin_MFAChallenge_NotCounted 第一次登录(不带码):自动发码 + 返 20109,
// 且**不进登录失败计数** —— 20109 不是失败,是"请继续"。
func TestLogin_MFAChallenge_NotCounted(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "mfachallenge", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)

	svc := newAuthSMSTestSvc(t, db)
	ctx := context.Background()

	res, err := svc.Login(ctx, LoginRequest{Username: "mfachallenge", Password: preHash(mfaTestPwd), LoginIP: "127.0.0.1"})
	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errIs(err, errcode.MfaRequired), "want MfaRequired(20109), got %v", err)

	// 自动发了一条 scene=mfa 的码
	code := lastSmsCode(t, db, testPhone, model.SmsSceneMFA)
	assert.NotEmpty(t, code)

	// 失败计数没动
	var u model.SysUser
	require.NoError(t, db.First(&u, userID).Error)
	assert.Zero(t, u.FailedCount, "MFA 挑战不许计入登录失败")
}

// TestLogin_MFA_CorrectCode 走完两步:挑战 → 带码重登 → 拿到 token,
// 且返回的 user 视图里 MfaEnabled=true(前端靠它渲染个人中心卡片)。
func TestLogin_MFA_CorrectCode(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "mfaok", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)

	svc := newAuthSMSTestSvc(t, db)
	ctx := context.Background()

	// 第一步:挑战,自动发码
	_, err := svc.Login(ctx, LoginRequest{Username: "mfaok", Password: preHash(mfaTestPwd), LoginIP: "127.0.0.1"})
	require.True(t, errIs(err, errcode.MfaRequired), "want MfaRequired, got %v", err)
	code := lastSmsCode(t, db, testPhone, model.SmsSceneMFA)

	// 第二步:带码 + 密码重发(挑战无状态)
	res, err := svc.Login(ctx, LoginRequest{Username: "mfaok", Password: preHash(mfaTestPwd), MfaCode: code, LoginIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token)
	assert.True(t, res.User.MfaEnabled)
}

// TestLogin_MFA_WrongCode_CountsAsLoginFail 对 MFA 步骤暴力试码 = 对密码暴力
// 试码,同样要计数(+ 达阈值锁定由 Login 既有逻辑兜着,这里只验计数)。
func TestLogin_MFA_WrongCode_CountsAsLoginFail(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "mfawrong", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)

	svc := newAuthSMSTestSvc(t, db)
	ctx := context.Background()

	_, err := svc.Login(ctx, LoginRequest{Username: "mfawrong", Password: preHash(mfaTestPwd), MfaCode: "999999", LoginIP: "127.0.0.1"})
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.SmsCodeInvalid), "want SmsCodeInvalid, got %v", err)

	var u model.SysUser
	require.NoError(t, db.First(&u, userID).Error)
	assert.Equal(t, 1, u.FailedCount, "错码必须计一次登录失败")
}

// TestLogin_MFA_CooldownTolerantRechallenge 用户在 MFA 步骤刷新/重试时,
// 自动发码撞上冷却(SmsSendTooFrequent)**不算错** —— 上一条码大概率还在,
// 照样返 20109 让用户把那条码填上。误报"发送太频繁"会把人卡死在登录页。
func TestLogin_MFA_CooldownTolerantRechallenge(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "mfacool", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)

	svc := newAuthSMSTestSvc(t, db)
	ctx := context.Background()

	// 第一次挑战:发码成功
	_, err := svc.Login(ctx, LoginRequest{Username: "mfacool", Password: preHash(mfaTestPwd), LoginIP: "127.0.0.1"})
	require.True(t, errIs(err, errcode.MfaRequired))
	first := lastSmsCode(t, db, testPhone, model.SmsSceneMFA)

	// 第二次挑战(冷却期内):仍返 20109,不是 SmsSendTooFrequent
	_, err = svc.Login(ctx, LoginRequest{Username: "mfacool", Password: preHash(mfaTestPwd), LoginIP: "127.0.0.1"})
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.MfaRequired), "want MfaRequired, got %v", err)
	assert.False(t, errIs(err, errcode.SmsSendTooFrequent))
	assert.Equal(t, first, lastSmsCode(t, db, testPhone, model.SmsSceneMFA), "冷却期内不该换码")
}

// TestLogin_MFA_NoPhone_FailClosed 开着 MFA 却没绑手机 → 不放行。
// 报错仍是 20109(前端按 MFA 弹层引导),但消息点名管理员恢复路径。
func TestLogin_MFA_NoPhone_FailClosed(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "mfaclosed", "role_user_admin", mfaTestPwd)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)

	svc := newAuthSMSTestSvc(t, db)
	res, err := svc.Login(context.Background(), LoginRequest{Username: "mfaclosed", Password: preHash(mfaTestPwd), LoginIP: "127.0.0.1"})
	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errIs(err, errcode.MfaRequired))
}

// TestSendMfaLoginCode_Gates 重发端点的三道门:
// 密码错 → LoginFailed(与登录同口径,不泄露"MFA 开没开");
// 密码对但没开 MFA → StateInvalid(重发无意义);
// 密码对且开着 → 发码。
func TestSendMfaLoginCode_Gates(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "mfaresend", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	ctx := context.Background()

	// 密码错
	err := svc.SendMfaLoginCode(ctx, "mfaresend", preHash("Wrong@1234567"), "127.0.0.1")
	assert.True(t, errIs(err, errcode.LoginFailed), "want LoginFailed, got %v", err)

	// 密码对,没开 MFA
	err = svc.SendMfaLoginCode(ctx, "mfaresend", preHash(mfaTestPwd), "127.0.0.1")
	assert.True(t, errIs(err, errcode.StateInvalid), "want StateInvalid, got %v", err)

	// 开了 MFA → 发码成功
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)
	require.NoError(t, svc.SendMfaLoginCode(ctx, "mfaresend", preHash(mfaTestPwd), "127.0.0.1"))
	assert.NotEmpty(t, lastSmsCode(t, db, testPhone, model.SmsSceneMFA))
}

// TestLoginBySms_MfaCodeNotUsable MFA 的码不能被短信登录消费 ——
// 这是 scene 独立(mfa ≠ login)存在的全部理由。
func TestLoginBySms_MfaCodeNotUsable(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "mfabypass", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)

	svc := newAuthSMSTestSvc(t, db)
	ctx := context.Background()

	// 手工种一条 scene=mfa 的码,试图拿它走短信登录
	seedSmsCode(t, db, testPhone, model.SmsSceneMFA, "654321")
	_, err := svc.LoginBySms(ctx, testPhone, "654321", "127.0.0.1", "test")
	require.Error(t, err, "MFA 的码不许被 LoginBySms 消费")

	// 对照组:scene=login 的码正常走通 —— MFA 开着不额外加一步
	// (短信登录本身就证明了持机,MFA 的因子已经用掉了)
	require.NoError(t, svc.SendSms(ctx, testPhone, model.SmsSceneLogin))
	res, err := svc.LoginBySms(ctx, testPhone, lastSmsCode(t, db, testPhone, model.SmsSceneLogin), "127.0.0.1", "test")
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token)
}

// TestDisableUserMfa_ByAdmin 管理员恢复路径:关掉后立即可登录(不再要求 MFA);
// 没开 MFA 的账号关第二次 → StateInvalid。
func TestDisableUserMfa_ByAdmin(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identID := seedAuthUser(t, db, "mfarecover", "role_user_admin", mfaTestPwd)
	setUserPhoneWithHash(t, db, userID, testPhone)
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("mfa_enabled", true).Error)

	svc := newAuthSMSTestSvc(t, db)
	users := NewUserService(db, nil, nil)
	ctx := context.Background()

	// 管理员关掉
	admin := mkAdminCaller(userID, identID, "role_super_admin")
	require.NoError(t, users.DisableUserMfa(ctx, admin, userID))

	// 关掉后密码登录直接通过,不再有 MFA 挑战
	res, err := svc.Login(ctx, LoginRequest{Username: "mfarecover", Password: preHash(mfaTestPwd), LoginIP: "127.0.0.1"})
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token)

	// 没开的账号再关 → StateInvalid
	err = users.DisableUserMfa(ctx, admin, userID)
	require.Error(t, err)
	assert.True(t, errIs(err, errcode.StateInvalid), "want StateInvalid, got %v", err)
}
