// 短信二次验证链路(切高敏身份 + 自助短信)—— M104(2026-10-28)
//
// # 这组用例守的是什么
//
//	2026-10-28 之前,"切到超管身份要短信二次验证"这条**从来没真正生效过**。
//	四个洞叠在一起:
//
//	  1. 前端发短信用的 scene 是 `login-sms` / `forgot-password` / 空串,
//	     后端 validScene() 只认 4 个(login / reset_password / change_phone /
//	     view_full_phone)→ **短信一条都没发出去过**。
//	  2. 前端没有明文手机号(/auth/me 只给脱敏号)→ 就算 scene 对了也发不出去。
//	  3. 前端切身份时传的是 `sms_code: undefined` → 后端直接拒
//	     「切换到高敏角色需要短信验证码」。
//
//	更要命的是第 4 条 —— 它当时**还没被发现**:
//
//	  4. 后端校验短信码时只按 `code + scene` 查,**不绑手机号**。
//	     而 scene=change_phone 是**公开可发**的:任何人可以走
//	     POST /public/auth/sms/send 给自己的手机号发一条 change_phone 的码,
//	     拿到码之后拿来切任意账号的高敏身份 —— 二次验证形同虚设。
//
//	这组用例把 3 / 4 两条钉死:验证码**必须**是发给"账号自己登记的手机号"的那一条。
//
// # 为什么用 console sender
//
//	auth_test.go 里的 newAuthTestSvc 传的 SMS sender 是 **nil** ——
//	那些用例从来不碰发短信,所以没事。真发短信会 nil 解引用。
//	这里用 console sender(只打日志,不真发)。
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
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/msn/console"
	"rapid-admin/internal/pkg/sonyflake"
)

const (
	testPhone      = "13812345678"
	testOtherPhone = "13999999999"
)

// newAuthSMSTestSvc 构造带**真短信 sender** 的 AuthService
func newAuthSMSTestSvc(t *testing.T, db *gorm.DB) AuthService {
	t.Helper()
	auth.SetDefaultRevocation(auth.NewMemoryStore())
	t.Cleanup(func() { auth.SetDefaultRevocation(auth.NewMemoryStore()) })
	return NewAuthService(db, buildTestJWTMulti(t), console.New(), defaultSMSCfg(), defaultAnomalyCfg(), nil)
}

// setUserPhone 给已 seed 的用户补一个登记手机号
func setUserPhone(t *testing.T, db *gorm.DB, userID uint64, phone string) {
	t.Helper()
	require.NoError(t, db.Model(&model.SysUser{}).
		Where("id = ?", userID).
		Update("phone", phone).Error)
}

// addIdentity 给用户加一个额外身份
func addIdentity(t *testing.T, db *gorm.DB, userID uint64, roleCode string) uint64 {
	t.Helper()
	id := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: id},
		UserID:    userID,
		RoleCode:  roleCode,
		RoleName:  roleCode,
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)
	return id
}

// seedSmsCode 复用 auth_invalidate_test.go 里那个(同包,签名完全一致)——
// 手工写一条 sys_sms_code,模拟"某条短信已经发出去"。
// 不重写一份:两份一模一样的 helper 迟早会各自漂移。

// ===== 验证码必须绑在账号登记的手机号上 =====

// TestSwitchIdentity_SmsCodeMustMatchRegisteredPhone
//
//	同一条短信码,发给**别人的手机** → 拒;发给**账号自己的手机** → 过。
//
//	这一条就是"二次验证到底有没有在验身份"的分界线:
//	以前只按 code+scene 查,自己给自己发一条就能切任意账号的高敏身份。
func TestSwitchIdentity_SmsCodeMustMatchRegisteredPhone(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "smsbind", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)
	superIdentID := addIdentity(t, db, userID, "role_super_admin")

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	// 1. 码发给了**别人**的手机 → 必须拒
	seedSmsCode(t, db, testOtherPhone, model.SmsSceneChangePhone, "123456")
	_, err := svc.SwitchIdentity(context.Background(), caller, superIdentID, "123456")
	assert.True(t, errIs(err, errcode.SmsCodeInvalid),
		"发给别的手机号的验证码不该能切高敏身份,实际 err=%v", err)

	// 2. 码发给了**账号自己**的手机 → 过
	require.NoError(t, db.Model(&model.SysSmsCode{}).
		Where("phone = ?", testOtherPhone).
		Update("phone", testPhone).Error)
	res, err := svc.SwitchIdentity(context.Background(), caller, superIdentID, "123456")
	require.NoError(t, err, "发给本人手机号的验证码应该能通过")
	assert.Equal(t, superIdentID, res.User.CurrentIdentityID)
}

// TestSwitchIdentity_NoRegisteredPhoneSwitchesDirectly
//
//	2026-11-03 用户拍板(推翻 M104 时期的硬拒):账号没登记手机号 →
//	免验证**直接切**。M104 那条"没手机号 = 免验切超管 = 洞"的口径作废:
//	角色授权本来就写在 sys_user_identity 里,能进来 = 已有合法登录会话;
//	而硬拒 + 绑号又依赖短信可用,对没配短信通道的部署是一条死路。
//
//	登记了手机号的账号仍然要验证码(见
//	TestSwitchIdentity_SmsCodeMustMatchRegisteredPhone,口径没变)。
func TestSwitchIdentity_NoRegisteredPhoneSwitchesDirectly(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "nophone", "role_user_admin", "Pwd@1234567")
	superIdentID := addIdentity(t, db, userID, "role_super_admin")
	// 故意摆一条码在库里:证明放行靠的是"没手机号就不验",不是"码恰好有效"
	seedSmsCode(t, db, testPhone, model.SmsSceneChangePhone, "123456")

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	res, err := svc.SwitchIdentity(context.Background(), caller, superIdentID, "")
	require.NoError(t, err, "没登记手机号的账号应该免验直接切,实际 err=%v", err)
	assert.Equal(t, superIdentID, res.User.CurrentIdentityID)
}

// ===== 服务端代发短信(手机号由服务端从 sys_user 取)=====

// TestSendAccountSms_LandsOnRegisteredPhone
//
//	服务端代发 → 验证码行的 phone **就是账号登记的手机号**。
//	这正是上面那条绑定校验能成立的前提。
func TestSendAccountSms_LandsOnRegisteredPhone(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "sendme", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	require.NoError(t, svc.SendAccountSms(context.Background(), caller, model.SmsSceneChangePhone))

	var rec model.SysSmsCode
	require.NoError(t, db.Where("scene = ?", model.SmsSceneChangePhone).
		Order("send_at DESC").First(&rec).Error)
	assert.Equal(t, testPhone, rec.Phone, "验证码必须落在账号登记的手机号上")
	assert.Len(t, rec.Code, 6, "验证码是 6 位数字")
	assert.Nil(t, rec.UsedAt, "刚发出去的验证码不该是已用状态")
}

// TestSendAccountSms_RejectsSceneNotInAllowlist
//
//	自助发短信的 scene 必须走白名单。`login` 尤其不能放行 ——
//	它是"手机号还没绑到这个账号"时用的,自助发给自己登记的号没有意义,
//	放开了只会让人误以为"我已经用短信登录过了"。
func TestSendAccountSms_RejectsSceneNotInAllowlist(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "badscene", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	for _, scene := range []string{model.SmsSceneLogin, "", "switch_identity"} {
		err := svc.SendAccountSms(context.Background(), caller, scene)
		assert.True(t, errIs(err, errcode.InvalidParam),
			"scene=%q 不该能自助发短信,实际 err=%v", scene, err)
	}

	// 一条都不能落库
	var n int64
	require.NoError(t, db.Model(&model.SysSmsCode{}).Count(&n).Error)
	assert.Zero(t, n, "被拒的 scene 不该留下任何验证码记录")
}

// TestSendAccountSms_NoRegisteredPhone
func TestSendAccountSms_NoRegisteredPhone(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "nophone2", "role_user_admin", "Pwd@1234567")

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	err := svc.SendAccountSms(context.Background(), caller, model.SmsSceneChangePhone)
	assert.True(t, errIs(err, errcode.InvalidParam),
		"没有登记手机号时应明确报错,实际 err=%v", err)
}

// TestSendAccountSms_RequiresLogin
func TestSendAccountSms_RequiresLogin(t *testing.T) {
	db := newAuthTestDB(t)
	svc := newAuthSMSTestSvc(t, db)

	err := svc.SendAccountSms(context.Background(), nil, model.SmsSceneChangePhone)
	assert.True(t, errIs(err, errcode.Unauthorized), "未登录不该能发短信,实际 err=%v", err)
}

// ===== 自助校验(手机号同样由服务端取)=====

// TestVerifyAccountSms_ConsumesCode
//
//	校验通过即消费 —— 同一条码不能验第二次。
func TestVerifyAccountSms_ConsumesCode(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "verifyonce", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)
	seedSmsCode(t, db, testPhone, model.SmsSceneResetPassword, "123456")

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	ok, err := svc.VerifyAccountSms(context.Background(), caller, model.SmsSceneResetPassword, "123456")
	require.NoError(t, err)
	assert.True(t, ok, "发给本人手机号的码应该验得过")

	// 第二次 → 已消费
	ok, err = svc.VerifyAccountSms(context.Background(), caller, model.SmsSceneResetPassword, "123456")
	assert.True(t, errIs(err, errcode.SmsCodeInvalid),
		"同一条码不该能验第二次,实际 err=%v", err)
	assert.False(t, ok)
}

// TestVerifyAccountSms_RejectsCodeSentToOtherPhone
//
//	跟 SwitchIdentity 那条一样的分界线:
//	码必须是发给**这个账号登记的手机号**的那一条。
func TestVerifyAccountSms_RejectsCodeSentToOtherPhone(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "verifybind", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)
	seedSmsCode(t, db, testOtherPhone, model.SmsSceneResetPassword, "123456")

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	ok, err := svc.VerifyAccountSms(context.Background(), caller, model.SmsSceneResetPassword, "123456")
	assert.True(t, errIs(err, errcode.SmsCodeInvalid),
		"发给别的手机号的码不该验得过,实际 err=%v", err)
	assert.False(t, ok)
}

// TestVerifyAccountSms_RejectsSceneMismatch
//
//	同一条码换一个 scene 就不认 —— scene 是验证码的一部分。
func TestVerifyAccountSms_RejectsSceneMismatch(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "verifyscene", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)
	seedSmsCode(t, db, testPhone, model.SmsSceneResetPassword, "123456")

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	ok, err := svc.VerifyAccountSms(context.Background(), caller, model.SmsSceneChangePhone, "123456")
	assert.True(t, errIs(err, errcode.SmsCodeInvalid),
		"scene 对不上的码不该验得过,实际 err=%v", err)
	assert.False(t, ok)
}

// TestVerifyAccountSms_RejectsSceneNotInAllowlist
func TestVerifyAccountSms_RejectsSceneNotInAllowlist(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "verifybadscene", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)
	seedSmsCode(t, db, testPhone, model.SmsSceneLogin, "123456")

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	ok, err := svc.VerifyAccountSms(context.Background(), caller, model.SmsSceneLogin, "123456")
	assert.True(t, errIs(err, errcode.InvalidParam),
		"白名单外的 scene 不该能自助验码,实际 err=%v", err)
	assert.False(t, ok)
}

// TestVerifyAccountSms_RequiresLogin
func TestVerifyAccountSms_RequiresLogin(t *testing.T) {
	db := newAuthTestDB(t)
	svc := newAuthSMSTestSvc(t, db)

	ok, err := svc.VerifyAccountSms(context.Background(), nil, model.SmsSceneResetPassword, "123456")
	assert.True(t, errIs(err, errcode.Unauthorized), "未登录不该能验码,实际 err=%v", err)
	assert.False(t, ok)
}

// ===== /auth/me 的 phone 不能再是空串 =====

// TestCurrentUserView_PhoneIsMaskedButNotEmpty
//
//	decryptPII 原来恒返回 "" → /auth/me 的 phone 恒空 → 前端拿不到手机号,
//	整条短信二次验证链路断掉。这里锁住"脱敏但非空"。
func TestCurrentUserView_PhoneIsMaskedButNotEmpty(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "viewphone", "role_user_admin", "Pwd@1234567")
	setUserPhone(t, db, userID, testPhone)

	var u model.SysUser
	require.NoError(t, db.First(&u, userID).Error)

	view := NewCurrentUserView(&u, []model.SysUserIdentity{{
		BaseModel: model.BaseModel{ID: identityID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		IsActive:  true,
	}}, "role_user_admin", identityID)

	assert.NotEmpty(t, view.Phone, "/auth/me 的 phone 不能是空串")
	assert.NotContains(t, view.Phone, testPhone, "wire 上必须是脱敏值,不能是明文")
	assert.Contains(t, view.Phone, "****", "应该是脱敏形态")
}
