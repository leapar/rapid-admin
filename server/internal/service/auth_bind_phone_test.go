// 换绑手机号(C5)—— 2026-10-29
//
// # 这组用例守的是什么
//
//	个人中心的「安全设置 → 手机绑定 → 换绑手机」以前是一个**禁用按钮**
//	(tooltip 写"Phase 6 真实化")⇒ 这条链路**从来没有过**。
//
//	这条链路的语义和「自助短信」(M104)是**反方向**的:
//
//	  自助短信   → 码发给【旧号】(服务端从 sys_user 取),证明"用户还占着旧号"
//	  换绑手机号 → 码发给【新号】(前端传),证明"用户占着新号"
//
//	换绑要证明的恰恰是**新号** —— 旧号马上就要失效了,证明它没有意义。
//	所以"新号由前端传、服务端只管校验"是这个设计的核心,不是偷懒。
//
// # 三个最容易写错、这里逐条钉住的地方
//
//  1. **验证码必须绑在【新号】上** —— 不是旧号。
//     用错手机号的码(发给别人的号)必须被拒。
//
//  2. **phone 与 phone_hash 必须一起写**(M100 审计定的硬约束)。
//     只写 phone 的话,"按手机号短信登录 / 短信重置密码"会**直接失配** ——
//     那两条流程是按 phone_hash 查人的。这条有端到端断言:
//     换绑之后,旧号再也重置不了这个账号的密码。
//
//  3. **旧号上残留的验证码必须作废** —— 手机号是**会回收**的。
//     旧号上没用的码如果还在有效期内,等旧号被绑到别的账号上,
//     那条码就能登进**新主人**的账号。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	SendBindPhoneCode 去掉"新号 == 旧号就拒"  → TestSendBindPhoneCode_RejectsSamePhone 变红
//	BindPhone 去掉 phone_hash 那行写入        → TestBindPhone_WritesPhoneAndPhoneHash 变红
//	BindPhone 去掉 invalidateSmsCodes          → TestBindPhone_InvalidatesOldPhoneCodes 变红
//	BindPhone 去掉 ensurePhoneAvailable        → TestBindPhone_RejectsPhoneOwnedByAnotherUser 变红
//	consumeSmsCode 去掉 phone 条件             → TestBindPhone_RejectsCodeSentToAnotherPhone 变红
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/desensitize"
	"rapid-admin/internal/pkg/errcode"
)

// setUserPhoneWithHash 给用户写登记手机号。
//
//	明文 + 盲索引**一起写** —— 生产上也是这么写的(user_impl / auth_impl
//	两条写入路径都成对写)。只写一半的话,"按手机号找人"那两条流程就废了,
//	而 fixture 造出来的这种半截数据会让测试**假绿**。
func setUserPhoneWithHash(t *testing.T, db *gorm.DB, userID uint64, phone string) {
	t.Helper()
	require.NoError(t, db.Model(&model.SysUser{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"phone":      phone,
			"phone_hash": desensitize.PhoneHash(phone),
		}).Error)
}

// lastSmsCode 取某个(手机号, scene)上最近一条验证码的码值
func lastSmsCode(t *testing.T, db *gorm.DB, phone, scene string) string {
	t.Helper()
	var rec model.SysSmsCode
	require.NoError(t, db.Where("phone = ? AND scene = ?", phone, scene).
		Order("send_at DESC").First(&rec).Error)
	return rec.Code
}

// ===== 发码:必须落在【新号】上 =====

// TestSendBindPhoneCode_LandsOnNewPhone
//
//	换绑的验证码**发给新号**,旧号上不许出现任何码。
//
//	这是整条链路成立的前提:消费端(BindPhone)按新号查 sys_sms_code,
//	发到旧号上的码就永远配不上。
func TestSendBindPhoneCode_LandsOnNewPhone(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "bindnew", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	require.NoError(t, svc.SendBindPhoneCode(context.Background(), caller, testOtherPhone))

	code := lastSmsCode(t, db, testOtherPhone, model.SmsSceneChangePhone)
	assert.NotEmpty(t, code, "新号上应该有验证码")

	var oldCnt int64
	require.NoError(t, db.Model(&model.SysSmsCode{}).
		Where("phone = ?", testPhone).Count(&oldCnt).Error)
	assert.Zero(t, oldCnt, "换绑的码只能发给新号,旧号上不该有码")
}

// TestSendBindPhoneCode_RejectsSamePhone
//
//	新号 == 旧号 必须拒。
//
//	不拒的话,用户在"换绑"弹窗里发的那条码,**同时**也是"切高敏身份"
//	弹窗里能用的那条 —— 一个码两处用,事后说不清是谁在用。
//	而且"换绑成自己现在的号"本身也没有业务意义。
func TestSendBindPhoneCode_RejectsSamePhone(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "bindsame", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	err := svc.SendBindPhoneCode(context.Background(), caller, testPhone)
	assert.True(t, errIs(err, errcode.InvalidParam),
		"新号与旧号相同应该被拒,实际 err=%v", err)

	var cnt int64
	require.NoError(t, db.Model(&model.SysSmsCode{}).Count(&cnt).Error)
	assert.Zero(t, cnt, "被拒的请求不该留下验证码记录")
}

// TestSendBindPhoneCode_RejectsPhoneOwnedByAnotherUser
//
//	新号已经被别的账号绑着 → 拒(Conflict)。
//
//	phone_hash 是"按手机号找人"的唯一依据(短信登录 / 短信重置密码都靠它),
//	两个账号共用一个号 ⇒ 那条短信会被消费到**先写入**的那个账号头上。
func TestSendBindPhoneCode_RejectsPhoneOwnedByAnotherUser(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "binddup", "role_user_admin", "Pwd@1234567")
	otherID, _ := seedAuthUser(t, db, "bindowner", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, otherID, testOtherPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	err := svc.SendBindPhoneCode(context.Background(), caller, testOtherPhone)
	assert.True(t, errIs(err, errcode.Conflict),
		"新号已被别的账号绑定应该被拒,实际 err=%v", err)
}

// ===== 换绑:双写 + 校验 + 作废 =====

// TestBindPhone_WritesPhoneAndPhoneHash
//
//	phone(明文)和 phone_hash(盲索引)必须**一起**写。
//
//	少写 phone_hash 的后果不是"搜不到",而是**短信登录 / 短信重置密码直接失配**
//	—— 那两条按 phone_hash 查人(见 LoginBySms / ResetPasswordBySms)。
func TestBindPhone_WritesPhoneAndPhoneHash(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "bindwrite", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")
	ctx := context.Background()

	require.NoError(t, svc.SendBindPhoneCode(ctx, caller, testOtherPhone))
	code := lastSmsCode(t, db, testOtherPhone, model.SmsSceneChangePhone)

	view, err := svc.BindPhone(ctx, caller, testOtherPhone, code)
	require.NoError(t, err, "正确的验证码应该能换绑成功")

	var u model.SysUser
	require.NoError(t, db.First(&u, userID).Error)
	assert.Equal(t, testOtherPhone, u.Phone, "phone 应该写新号")
	assert.Equal(t, desensitize.PhoneHash(testOtherPhone), u.PhoneHash,
		"phone_hash 必须一起写 —— 否则短信登录/短信重置密码直接失配")

	// 返回的视图是脱敏后的新号(前端整份替换 store)
	assert.Contains(t, view.Phone, "****", "视图里的手机号应该是脱敏的,实际=%s", view.Phone)
	assert.NotEqual(t, "", view.Phone)
}

// TestBindPhone_RejectsCodeSentToAnotherPhone
//
//	发给**别的号**的验证码不能拿来换绑 —— 这是"验证码到底绑没绑定新号"的分界线。
func TestBindPhone_RejectsCodeSentToAnotherPhone(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "bindwrong", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")

	// 一条发给第三方号码的码
	seedSmsCode(t, db, "13700000000", model.SmsSceneChangePhone, "123456")

	_, err := svc.BindPhone(context.Background(), caller, testOtherPhone, "123456")
	assert.True(t, errIs(err, errcode.SmsCodeInvalid),
		"发给别的手机号的验证码不该能换绑,实际 err=%v", err)

	var u model.SysUser
	require.NoError(t, db.First(&u, userID).Error)
	assert.Equal(t, testPhone, u.Phone, "失败的换绑不该改手机号")
}

// TestBindPhone_RejectsPhoneOwnedByAnotherUser
//
//	新号已被别的账号占用 → 拒,而且**不该**把那个账号的手机号改掉。
func TestBindPhone_RejectsPhoneOwnedByAnotherUser(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "binddup2", "role_user_admin", "Pwd@1234567")
	otherID, _ := seedAuthUser(t, db, "bindowner2", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, otherID, testOtherPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")
	ctx := context.Background()

	// ⚠️ 这里**不能**走 SendBindPhoneCode 发码 —— 发码那一步就会拒(Conflict),
	//   根本轮不到换绑。所以手工摆一条码,单独验"换绑这一步自己也会拒"。
	//   (发码侧的同一道防线由 TestSendBindPhoneCode_RejectsPhoneOwnedByAnotherUser 守着。)
	seedSmsCode(t, db, testOtherPhone, model.SmsSceneChangePhone, "123456")

	_, err := svc.BindPhone(ctx, caller, testOtherPhone, "123456")
	assert.True(t, errIs(err, errcode.Conflict),
		"新号已被别的账号绑定应该被拒,实际 err=%v", err)

	var other model.SysUser
	require.NoError(t, db.First(&other, otherID).Error)
	assert.Equal(t, testOtherPhone, other.Phone, "别人的手机号不该被改掉")
}

// TestBindPhone_InvalidatesOldPhoneCodes
//
//	换绑成功后,旧号上**所有**未使用的验证码一律作废。
//
//	为什么:手机号是**会回收**的。旧号上残留的码(scene=login 之类)
//	如果还在有效期内,等旧号被绑到别的账号上,那条码就能登进**新主人**的账号。
//
//	断言覆盖两个 scene —— 不是只作废 change_phone。
func TestBindPhone_InvalidatesOldPhoneCodes(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "bindinv", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")
	ctx := context.Background()

	// 旧号上散着几条不同 scene 的未使用验证码
	seedSmsCode(t, db, testPhone, model.SmsSceneChangePhone, "111111")
	seedSmsCode(t, db, testPhone, model.SmsSceneLogin, "222222")
	seedSmsCode(t, db, testPhone, model.SmsSceneResetPassword, "333333")

	require.NoError(t, svc.SendBindPhoneCode(ctx, caller, testOtherPhone))
	code := lastSmsCode(t, db, testOtherPhone, model.SmsSceneChangePhone)
	_, err := svc.BindPhone(ctx, caller, testOtherPhone, code)
	require.NoError(t, err)

	var left int64
	require.NoError(t, db.Model(&model.SysSmsCode{}).
		Where("phone = ? AND used_at IS NULL", testPhone).Count(&left).Error)
	assert.Zero(t, left, "旧号上不该还有未使用的验证码")

	// 已经用掉的那条(本次换绑消费的)也不该被算成"未使用"
	var newLeft int64
	require.NoError(t, db.Model(&model.SysSmsCode{}).
		Where("phone = ? AND used_at IS NULL", testOtherPhone).Count(&newLeft).Error)
	assert.Zero(t, newLeft, "换绑用的那条码应该已被消费")
}

// TestBindPhone_CodeIsSingleUse
//
//	一条验证码只能用一次:用过之后 used_at 置位,拿它去换**别的**号会被拒。
//
//	⚠️ 为什么第二次不是拿同一个号再试:
//	  同号会被"新号必须与当前号不同"那条先拦掉(InvalidParam),
//	  那样断言的就不是"码被消费了"而是"号重复了"—— 两件事,别混成一条断言。
//	  这里改成"拿用过的码去换一个别的号",才是真正在验**码的一次性**。
func TestBindPhone_CodeIsSingleUse(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "bindonce", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")
	ctx := context.Background()

	require.NoError(t, svc.SendBindPhoneCode(ctx, caller, testOtherPhone))
	code := lastSmsCode(t, db, testOtherPhone, model.SmsSceneChangePhone)

	_, err := svc.BindPhone(ctx, caller, testOtherPhone, code)
	require.NoError(t, err)

	// 一次性语义的落点:那条码必须已经被标记成"用过"
	var rec model.SysSmsCode
	require.NoError(t, db.Where("phone = ? AND scene = ?", testOtherPhone, model.SmsSceneChangePhone).
		Order("send_at DESC").First(&rec).Error)
	require.NotNil(t, rec.UsedAt, "用过的验证码必须标记 used_at")
	assert.False(t, rec.UsedAt.IsZero(), "used_at 不该是零值")

	// 拿这条**已经用过**的码去换一个别的号 → 必须被拒
	_, err = svc.BindPhone(ctx, caller, "13711112222", code)
	assert.True(t, errIs(err, errcode.SmsCodeInvalid),
		"同一条验证码不该能用第二次,实际 err=%v", err)

	// 同一个号再来一次 → 同样被拒(但理由不同:号重复,不是码重复)
	_, err = svc.BindPhone(ctx, caller, testOtherPhone, code)
	assert.True(t, errIs(err, errcode.InvalidParam),
		"拿同一个号再换一次应该被拒,实际 err=%v", err)
}

// TestBindPhone_OldPhoneCanNoLongerResetPassword
//
//	**端到端**证明"双写"不是洁癖:换绑之后,旧号再也重置不了这个账号的密码。
//
//	这条链路是按 phone_hash 查人的(ResetPasswordBySms):
//	如果 BindPhone 只写 phone 不写 phone_hash,旧号**依然**能重置密码 ——
//	那意味着"我已经把手机号换走了",但攻击者拿着旧号还能把账号接管回去。
func TestBindPhone_OldPhoneCanNoLongerResetPassword(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "bindold", "role_user_admin", "Pwd@1234567")
	setUserPhoneWithHash(t, db, userID, testPhone)

	svc := newAuthSMSTestSvc(t, db)
	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")
	ctx := context.Background()

	// 换绑前:旧号能重置密码(对照组 —— 证明这条链路本身是通的)
	seedSmsCode(t, db, testPhone, model.SmsSceneResetPassword, "444444")
	require.NoError(t, svc.ResetPasswordBySms(ctx, testPhone, "444444", "NewPwd@12345"))

	// 换绑
	require.NoError(t, svc.SendBindPhoneCode(ctx, caller, testOtherPhone))
	code := lastSmsCode(t, db, testOtherPhone, model.SmsSceneChangePhone)
	_, err := svc.BindPhone(ctx, caller, testOtherPhone, code)
	require.NoError(t, err)

	// 换绑后:旧号再也重置不了
	seedSmsCode(t, db, testPhone, model.SmsSceneResetPassword, "555555")
	err = svc.ResetPasswordBySms(ctx, testPhone, "555555", "HackPwd@12345")
	assert.True(t, errIs(err, errcode.LoginFailed),
		"换绑后旧号不该还能重置密码,实际 err=%v", err)

	// 新号可以
	seedSmsCode(t, db, testOtherPhone, model.SmsSceneResetPassword, "666666")
	require.NoError(t, svc.ResetPasswordBySms(ctx, testOtherPhone, "666666", "NewPwd@12345"))
}
