// Phase 5 M1 — AuthService 真实单测
// 文档:后台权限模型设计V1.0.md §17.18
//
// 覆盖 Logout / ChangePassword / ResetPassword / SwitchIdentity / UpdateProfile
// 端到端关键路径(在已有 AuthService.Login 单测之上新增)
// 使用 sqlite 内存库 + 真实 GORM 迁移
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// newAuthTestDB 内存 sqlite + 全量 auth 相关表 autoMigrate
func newAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysUserIdentity{},
		&model.SysLoginLog{},
		&model.SysSmsCode{},
		// 强制下线要写操作留痕(需求 §1397「操作留痕」)
		&model.SysOperationLog{},
	))
	return db
}

// hashForSeed 与 cmd/seed/main.go 保持一致:
//
//	bcrypt(SHA-256("rapid-admin::" + 明文))
//
// 供测试创建种子账号
func hashForSeed(t *testing.T, plain string) string {
	t.Helper()
	h := sha256.Sum256([]byte("rapid-admin::" + plain))
	b, err := auth.HashPassword(hex.EncodeToString(h[:]))
	require.NoError(t, err)
	return b
}

// preHash 模拟前端 preHashPassword: SHA-256("rapid-admin::" + 明文) hex
//
//	业务 service 期望 caller 传进来的是 pre-hash(与 Login 流程一致)
//	ChangePassword / ResetPassword 的 oldPwd / newPwd 都要先 pre-hash
func preHash(plain string) string {
	h := sha256.Sum256([]byte("rapid-admin::" + plain))
	return hex.EncodeToString(h[:])
}

// seedAuthUser 写一个 user + 1 个 primary identity,返回 userID / identityID
func seedAuthUser(t *testing.T, db *gorm.DB, username, roleCode, plainPwd string) (userID, identityID uint64) {
	t.Helper()
	userID = sonyflake.Next()
	identityID = sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel:     model.BaseModel{ID: userID},
		Username:      username,
		PasswordHash:  hashForSeed(t, plainPwd),
		Nickname:      username,
		IsActive:      true,
		MustChangePwd: false,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: identityID},
		UserID:    userID,
		RoleCode:  roleCode,
		RoleName:  roleCode,
		IsActive:  true,
		IsPrimary: true,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)
	return
}

// seedLoginLog 写一条未登出的 login log(供 Logout 测试验证 logout_at 写入)
func seedLoginLog(t *testing.T, db *gorm.DB, userID uint64) uint64 {
	t.Helper()
	id := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysLoginLog{
		BaseModel: model.BaseModel{ID: id},
		UserID:    userID,
		Username:  "test",
		IP:        "127.0.0.1",
		Device:    "test",
		Status:    1,
		Message:   "ok",
		LoginAt:   model.Time{Time: time.Now().Add(-1 * time.Minute)},
	}).Error)
	return id
}

// mkAdminCaller 构造一个 admin 身份 caller
func mkAdminCaller(userID, identityID uint64, roleCode string) *Caller {
	return &Caller{
		UserID:     userID,
		IdentityID: identityID,
		RoleCode:   roleCode,
		Audience:   "admin",
		Username:   "test",
		IP:         "127.0.0.1",
		JTI:        "test-jti-001",
	}
}

// errIs 判断 err 是否为 errcode code
func errIs(err error, code int) bool {
	var e *errcode.Error
	return errors.As(err, &e) && e.Code == code
}

// ===== Logout =====

// TestAuthService_Logout_RevokesAndWritesLogoutAt 登出的**两件事**都要发生:
//  1. 当前 jti 进吊销存储(之后这个 token 不能再用)
//  2. login log.LogoutAt 被写入
//
// 2026-10-21 W5 之前,这条用例的注释自认"本地无 Redis → 静默 skip jti revoke",
// 只断言了第 2 件事 —— 于是"登出到底有没有真的吊销"在整个测试套件里
// **从来没人验过**,而那段代码也确实一次都没执行过。现在两件事都断言。
func TestAuthService_Logout_RevokesAndWritesLogoutAt(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "u1", "role_user_admin", "Pwd@1234567")
	logID := seedLoginLog(t, db, userID)
	svc := newAuthTestSvc(t, db)

	ctx := context.Background()
	caller := mkAdminCaller(userID, identityID, "role_user_admin")

	// 先确认:登出**前**这个 jti 是干净的
	revoked, err := auth.DefaultRevocation().IsRevoked(ctx, caller.JTI)
	require.NoError(t, err)
	require.False(t, revoked, "用例开始时 jti 就已是吊销态 —— 测试之间串味了")

	require.NoError(t, svc.Logout(ctx, caller))

	// 1. jti 必须进吊销存储
	revoked, err = auth.DefaultRevocation().IsRevoked(ctx, caller.JTI)
	require.NoError(t, err)
	assert.True(t, revoked,
		"Logout 没吊销当前 jti —— 登出之后这个 access token 还能继续用")

	// 2. login log.LogoutAt 应被填上
	var rec model.SysLoginLog
	require.NoError(t, db.First(&rec, logID).Error)
	assert.NotNil(t, rec.LogoutAt, "LogoutAt 应被写入")
}

// TestAuthService_Logout_RevokesOnlyOwnJTI 钉死吊销的作用域:
// 登出只该吊销**这一个** token,不该顺手把同用户的其他会话一起踢掉。
// (per-user 全量吊销需要 per-user jti 索引,本期不做 —— 那正是这条断言
// 存在的意义:让"没做"这件事在测试里是显式的,而不是靠注释提醒。)
func TestAuthService_Logout_RevokesOnlyOwnJTI(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "u1", "role_user_admin", "Pwd@1234567")
	seedLoginLog(t, db, userID)
	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	// 另一个标签页 / 另一台设备:签发了但没被吊销
	otherJTI := "other-tab-jti"

	require.NoError(t, svc.Logout(ctx, mkAdminCaller(userID, identityID, "role_user_admin")))

	revoked, err := auth.DefaultRevocation().IsRevoked(ctx, otherJTI)
	require.NoError(t, err)
	assert.False(t, revoked,
		"登出把别的会话也踢了 —— 本期只吊销当前 token,"+
			"per-user 全量吊销需要 per-user jti 索引(范围外,别假装已做)")
}

// TestAuthService_Logout_NilCaller
func TestAuthService_Logout_NilCaller(t *testing.T) {
	db := newAuthTestDB(t)
	svc := newAuthTestSvc(t, db)
	err := svc.Logout(context.Background(), nil)
	assert.True(t, errIs(err, errcode.Unauthorized))
}

// ===== ChangePassword =====

// TestAuthService_ChangePassword_Happy
func TestAuthService_ChangePassword_Happy(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "u2", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	// 前端契约:传 pre-hash(SHA-256 hex)给 service
	err := svc.ChangePassword(context.Background(), caller, preHash("OldPwd@12345"), preHash("NewPwd@12345"))
	require.NoError(t, err)

	// 校验新密码可用
	var u model.SysUser
	require.NoError(t, db.First(&u, userID).Error)
	assert.True(t, auth.CheckPassword(u.PasswordHash, preHash("NewPwd@12345")), "新密码(pre-hash)应能登录")
	assert.False(t, auth.CheckPassword(u.PasswordHash, preHash("OldPwd@12345")), "旧密码已失效")
}

// TestAuthService_ChangePassword_WrongOldPwd
func TestAuthService_ChangePassword_WrongOldPwd(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "u3", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	err := svc.ChangePassword(context.Background(), caller, preHash("WrongPwd@12345"), preHash("NewPwd@12345"))
	assert.True(t, errIs(err, errcode.LoginFailed), "旧密码错应返 LoginFailed")
}

// TestAuthService_ChangePassword_FormatError
//
//	service 拒绝非 64 字符 hex(SHA-256 hex 长度),前端必须 pre-hash
func TestAuthService_ChangePassword_FormatError(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "u4", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	// 短
	err := svc.ChangePassword(context.Background(), caller, preHash("OldPwd@12345"), "abc")
	assert.True(t, errIs(err, errcode.InvalidParam), "非 64 字符应返 InvalidParam")

	// 与旧密码相同
	err = svc.ChangePassword(context.Background(), caller, preHash("OldPwd@12345"), preHash("OldPwd@12345"))
	assert.True(t, errIs(err, errcode.InvalidParam), "新=旧应拒绝")
}

// ===== ResetPassword =====

// TestAuthService_ResetPassword_SelfIsOK
func TestAuthService_ResetPassword_SelfIsOK(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "u5", "role_user_admin", "OldPwd@12345")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	err := svc.ResetPassword(context.Background(), caller, userID, preHash("NewPwd@12345"))
	require.NoError(t, err)

	var u model.SysUser
	require.NoError(t, db.First(&u, userID).Error)
	assert.True(t, auth.CheckPassword(u.PasswordHash, preHash("NewPwd@12345")))
	assert.True(t, u.MustChangePwd, "自助改密后 must_change_pwd 应被置 true")
}

// TestAuthService_ResetPassword_NonSuperAdminOtherUser_DepthDefense
//
//	非超管不能重置他人密码(即使 handler IDOR 拦截被绕过,service 也应拒)
func TestAuthService_ResetPassword_NonSuperAdminOtherUser_DepthDefense(t *testing.T) {
	db := newAuthTestDB(t)
	adminID, adminIdentID := seedAuthUser(t, db, "admin", "role_user_admin", "AdminPwd@12345")
	otherID, _ := seedAuthUser(t, db, "other", "role_user_admin", "OtherPwd@12345")
	// 拿住 other 改密前的 hash,后面比对
	var otherBefore model.SysUser
	require.NoError(t, db.First(&otherBefore, otherID).Error)
	originalHash := otherBefore.PasswordHash

	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(adminID, adminIdentID, "role_user_admin")
	err := svc.ResetPassword(context.Background(), caller, otherID, preHash("HackedPwd@12345"))
	assert.True(t, errIs(err, errcode.IdorAttempt), "非超管重置他人应返 IdorAttempt")

	// 校验 other 密码 hash 没被改
	var otherAfter model.SysUser
	require.NoError(t, db.First(&otherAfter, otherID).Error)
	assert.Equal(t, originalHash, otherAfter.PasswordHash, "other 密码 hash 应保持原样")
}

// TestAuthService_ResetPassword_SuperAdmin_OtherUser_OK
func TestAuthService_ResetPassword_SuperAdmin_OtherUser_OK(t *testing.T) {
	db := newAuthTestDB(t)
	superID, superIdentID := seedAuthUser(t, db, "super", "role_super_admin", "SuperPwd@12345")
	otherID, _ := seedAuthUser(t, db, "other", "role_user_admin", "OtherPwd@12345")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(superID, superIdentID, "role_super_admin")
	err := svc.ResetPassword(context.Background(), caller, otherID, preHash("ResetPwd@12345"))
	require.NoError(t, err)

	var u model.SysUser
	require.NoError(t, db.First(&u, otherID).Error)
	assert.True(t, auth.CheckPassword(u.PasswordHash, preHash("ResetPwd@12345")), "超管重置后应生效")
	assert.True(t, u.MustChangePwd, "代重置后 must_change_pwd=true")
}

// ===== SwitchIdentity =====

// TestAuthService_SwitchIdentity_Happy
//  1. 用户有 2 个身份(super + user_admin),切到 user_admin
//  2. 期望返回新 AuthTokens, Token 非空
func TestAuthService_SwitchIdentity_Happy(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "dual", "role_super_admin", "Pwd@1234567")
	// 加第 2 个 identity
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

	caller := mkAdminCaller(userID, primaryIdentID, "role_super_admin")
	res, err := svc.SwitchIdentity(context.Background(), caller, secondIdentID, "")
	require.NoError(t, err)
	assert.NotEmpty(t, res.Token, "新 token 应非空")
	assert.NotEmpty(t, res.RefreshToken, "新 refresh 应非空")
	assert.Equal(t, userID, res.User.ID, "返回 user.ID 应等于原 userID")
	// 切换身份后必须告诉前端"当前是哪个身份" —— 前端不再靠 identities[0] 猜,
	// 猜错的表现是:token 里已经是 user_admin,界面还高亮 super_admin。
	assert.Equal(t, secondIdentID, res.User.CurrentIdentityID, "current_identity_id 应是刚切过去的身份")
	assert.Positive(t, res.RefreshExpiresIn, "必须带 refresh_expires_in,前端据此算 TTL")
}

// TestAuthService_SwitchIdentity_HighRiskRoleRequiresSms
//
//	切到 super_admin 的高敏校验口径(2026-11-03 拍板,commit 9a3f840):
//	· **未登记手机号 → 免验直接切**(否则没配短信通道的部署永远切不进高敏身份)
//	· **登记了手机号 → 必须验证码** —— 那半边由 TestSwitchIdentity_SmsCodeMustMatchRegisteredPhone 钉
//
//	⚠️ 本用例的 seed 用户没有手机号,所以断言是"成功",不是 SmsCodeInvalid ——
//	改回旧断言之前先看 9a3f840 的拍板说明。
func TestAuthService_SwitchIdentity_HighRiskRoleRequiresSms(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryIdentID := seedAuthUser(t, db, "normal", "role_user_admin", "Pwd@1234567")
	// 加 super 身份
	superIdentID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: superIdentID},
		UserID:    userID,
		RoleCode:  "role_super_admin",
		RoleName:  "超管",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, primaryIdentID, "role_user_admin")
	_, err := svc.SwitchIdentity(context.Background(), caller, superIdentID, "")
	assert.NoError(t, err, "未登记手机号的账号切高敏角色应免验直接切(9a3f840)")
}

// TestAuthService_SwitchIdentity_NotMine
//
//	切他人身份 → IdorAttempt
func TestAuthService_SwitchIdentity_NotMine(t *testing.T) {
	db := newAuthTestDB(t)
	u1ID, u1IdentID := seedAuthUser(t, db, "u1", "role_user_admin", "Pwd@1234567")
	u2ID, u2IdentID := seedAuthUser(t, db, "u2", "role_user_admin", "Pwd@1234567")

	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(u1ID, u1IdentID, "role_user_admin")
	_, err := svc.SwitchIdentity(context.Background(), caller, u2IdentID, "")
	assert.True(t, errIs(err, errcode.IdorAttempt), "切他人身份应返 IdorAttempt")

	_ = u2ID // suppress unused
}

// ===== UpdateProfile =====

// TestAuthService_UpdateProfile_Happy
func TestAuthService_UpdateProfile_Happy(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "profile1", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	newNick := "新昵称"
	u, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{
		Nickname: &newNick,
	})
	require.NoError(t, err)
	assert.Equal(t, "新昵称", u.Nickname)

	// 校验 DB 落地
	var reloaded model.SysUser
	require.NoError(t, db.First(&reloaded, userID).Error)
	assert.Equal(t, "新昵称", reloaded.Nickname)
}

// TestAuthService_UpdateProfile_IgnoresAvatarField —— **"贴外链头像"这条路必须堵死**
//
// M124 之前 `UpdateProfileDTO` 有 `Avatar *string`,前端可以直接往
// PATCH /auth/profile 传一个**任意 URL**。而头像会渲染在**别人的浏览器里**
// (管理端用户列表 / 顶栏)⇒ 那等于一个跟踪像素。
//
// ⚠️ **这条用例验的是"请求体里带了 avatar 也不会写库"**。
// 光断言"DTO 里没这个字段"是不够的 —— 那是编译期的事,用例不会红;
// 而 Go 的 json.Unmarshal **不认未知字段**,所以真正被守住的是
// "DTO 里没有 avatar ⇒ 绑定阶段就把它丢了"。
// 用例直接查库,才真的能挡住"有人又把它加回来"。
func TestAuthService_UpdateProfile_IgnoresAvatarField(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "profile3", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	// 先给一个"旧头像"
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", userID).
		Update("avatar", "/res/1/avatar/2/old.png").Error)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	// ⚠️ 走 JSON 往返:map → struct 时未知的 avatar 字段会被丢弃。
	// 这正是我们要的行为。
	raw := []byte(`{"avatar":"https://evil.example.com/track.png","nickname":"新昵称"}`)
	var dto UpdateProfileDTO
	require.NoError(t, json.Unmarshal(raw, &dto))

	_, err := svc.UpdateProfile(context.Background(), caller, dto)
	require.NoError(t, err)

	var reloaded model.SysUser
	require.NoError(t, db.First(&reloaded, userID).Error)
	assert.Equal(t, "/res/1/avatar/2/old.png", reloaded.Avatar,
		"头像不能被 PATCH /auth/profile 改成外部 URL(那是跟踪像素)")
}

// TestAuthService_UpdateProfile_EmptyNicknameRejected
func TestAuthService_UpdateProfile_EmptyNicknameRejected(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "profile2", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	empty := ""
	_, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{
		Nickname: &empty,
	})
	assert.True(t, errIs(err, errcode.InvalidParam), "昵称为空应拒绝")
}

// TestAuthService_UpdateProfile_InvalidEmailRejected
func TestAuthService_UpdateProfile_InvalidEmailRejected(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "profile3", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)

	caller := mkAdminCaller(userID, identityID, "role_user_admin")
	badEmail := "not-an-email"
	_, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{
		Email: &badEmail,
	})
	assert.True(t, errIs(err, errcode.InvalidParam), "邮箱格式错应拒绝")
}

// ===== 测试 helpers =====

// defaultSMSCfg 返回一个 sms nil-safe 的 config(测试无需真实发送)
func defaultSMSCfg() config.SMSConfig {
	return config.SMSConfig{
		Provider:  "",
		RateLimit: config.SMSRateLimit{WindowSeconds: 60, CodeTTL: 300},
	}
}

// buildTestJWTMulti 构造一个仅用于测试的 jwtMulti
//   - 三个受众 secret 不同(不与生产冲突)
//   - 短 TTL 即可
func buildTestJWTMulti(t *testing.T) *auth.Multi {
	t.Helper()
	return auth.NewMulti(
		"test-admin-secret-not-for-prod", "test-owner-secret", "test-supplier-secret",
		"test-issuer",
		2*time.Hour, 24*time.Hour, 5*time.Minute,
	)
}

// newAuthTestSvc 构造一个**装满了真东西**的 AuthService:真的 JWT Multi +
// 真的内存吊销存储。本文件所有用例都应该走它。
//
// # 为什么不能传 nil(2026-10-21,W5 实测踩出来的)
//
// 之前 16 处构造里有 11 处传 `nil *auth.Multi`。而 Logout 里有一行:
//
//	ttl := s.jwt.MustGet(auth.Audience(caller.Audience)).AccessTTL()
//
// 它之所以**没有**当场 panic,是因为同一阶段的另一处代码把吊销包在两层守卫里:
//
//	if caller.JTI != "" {                        // ← 恒真(W3 起 sign() 无条件写 JTI)
//	    if bl := DefaultBlacklist(); bl != nil { // ← 恒假(没配 Redis → 返回 nil)
//	        bl.Revoke(...)                        // ← 从未执行
//
// 守卫短路,**`s.jwt` 那行根本没跑到**。也就是说这些用例是
// **因为吊销功能是死的才绿的** —— 它们验证的"登出成功"里,
// 真正的登出动作一步都没发生。W5 把这两层守卫删掉(本来就该删),
// 这行第一次真的执行,`TestAuthService_Logout_RevokesAndWritesLogoutAt`
// 立刻 panic 在 auth_impl.go:270。
//
// 这正是 W9 那条结论的实证:传 nil 的用例"什么都没证明"。
// 断言本身(返回 token 非空之类)仍然有价值,但它们此前**顺带掩盖了一个
// 从未执行过的安全分支** —— 这个代价由 helper 一次性付清。
//
// # 隔离
//
// 吊销存储是**进程级全局**,所以每个用例都换一副新的:既保证用例之间
// 不互相污染(一个用例吊销了 jti,不影响下一个),cleanup 再换一副新的
// 交还给后续的测试。本包内没有 t.Parallel(),顺序执行是安全的 ——
// 如果哪天有人加了 t.Parallel(),这里要改成 per-test 注入而不是全局。
func newAuthTestSvc(t *testing.T, db *gorm.DB) AuthService {
	t.Helper()
	auth.SetDefaultRevocation(auth.NewMemoryStore())
	t.Cleanup(func() { auth.SetDefaultRevocation(auth.NewMemoryStore()) })
	// ⚠️ **异常登录检测默认开着**(M128)—— 让本包**每一个**登录用例
	//   都真的跑一遍检测,证明"接了检测不会把登录搞挂"。
	//   检测命中 / 检测失败都**不影响登录结果**,这条性质就该由所有用例共同守着,
	//   而不是只有一条专门的用例守着。
	return NewAuthService(db, buildTestJWTMulti(t), nil, defaultSMSCfg(), defaultAnomalyCfg(), nil)
}

// ===== W4:登录/续期/切换身份 三个端点的响应契约统一 =====
//
// 这一组测试锁的是"三个端点必须返同一个形状"。2026-10-21 之前的实际形状是:
// Login        → handler 拼 map,user 走 toCurrentUserView(带 identities)
// SwitchIdentity → 同上
// RefreshToken → service 直接吐 *model.SysUser(根本没有 identities 字段)
//
// 用户续期之后身份列表静默消失 —— 表现为"切换身份入口突然没了",
// 而且不报错、不 500,纯靠人眼才能发现。

// marshalTokens 把 AuthTokens 序列化成前端真正看到的字节。
// 直接断言 struct 字段是没用的:真正会骗人的是 json tag。
func marshalTokens(t *testing.T, tok *AuthTokens) map[string]any {
	t.Helper()
	b, err := json.Marshal(tok)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestAuthTokens_JSONShapeIsIdenticalAcrossEndpoints(t *testing.T) {
	db := newAuthTestDB(t)
	userID, primaryID := seedAuthUser(t, db, "shape", "role_super_admin", "Pwd@1234567")
	secondID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: secondID},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "用户管理员",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}).Error)

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	login, err := svc.Login(ctx, LoginRequest{
		Username: "shape", Password: preHash("Pwd@1234567"),
	})
	require.NoError(t, err)

	refreshed, err := svc.RefreshToken(ctx, login.RefreshToken)
	require.NoError(t, err)

	sw, err := svc.SwitchIdentity(ctx,
		mkAdminCaller(userID, primaryID, "role_super_admin"), secondID, "")
	require.NoError(t, err)

	loginM := marshalTokens(t, login)
	refreshM := marshalTokens(t, refreshed)
	switchM := marshalTokens(t, sw)

	// 顶层 key 集合必须一模一样 —— refresh 少任何一个都是契约漂移
	for _, tc := range []struct {
		name string
		m    map[string]any
	}{{"login", loginM}, {"refresh", refreshM}, {"switch", switchM}} {
		for _, key := range []string{"token", "refresh_token", "expires_in", "refresh_expires_in", "user"} {
			assert.Contains(t, tc.m, key, "%s 响应缺字段 %s", tc.name, key)
		}
		assert.Len(t, tc.m, 5, "%s 响应多出了未知字段(前端不需要,说明契约又分叉了)", tc.name)
	}

	// ★ 核心断言:三个端点的 user.identities 都必须存在且非空
	for _, tc := range []struct {
		name string
		m    map[string]any
	}{{"login", loginM}, {"refresh", refreshM}, {"switch", switchM}} {
		user, ok := tc.m["user"].(map[string]any)
		require.True(t, ok, "%s 的 user 必须是对象", tc.name)
		ids, ok := user["identities"].([]any)
		require.True(t, ok, "%s 的 user.identities 必须是数组 —— 缺它就是本次要修的 bug", tc.name)
		assert.Len(t, ids, 2, "%s 应返 2 个身份", tc.name)
	}

	// refresh_expires_in 必须有值,前端 TTL 全靠它(此前前端硬编码 30d/1d)
	for _, tc := range []struct {
		name string
		m    map[string]any
	}{{"login", loginM}, {"refresh", refreshM}, {"switch", switchM}} {
		assert.Positive(t, tc.m["refresh_expires_in"], "%s 的 refresh_expires_in 必须为正", tc.name)
		assert.Positive(t, tc.m["expires_in"], "%s 的 expires_in 必须为正", tc.name)
	}

	// access token 的 wire 名字是 token,不是 access_token —— 前端曾经读错
	// 这个名字,导致自动续期拿到 undefined、发 Bearer undefined 被踢下线。
	// 改名会波及 login / switch / 技术方案 §7.7.3,对齐方向永远是前端改。
	assert.NotEmpty(t, loginM["token"])
	assert.NotEmpty(t, refreshM["token"])
	assert.NotEmpty(t, switchM["token"])
}

// TestRefreshToken_UserViewCarriesFlags 单独锁 is_platform_super_admin /
// is_platform_ops —— 这两个字段前端当开关用(isSuper 决定系统配置页能不能写),
// 静默变 false 的表现是"权限莫名没了",不会报错。
func TestRefreshToken_UserViewCarriesFlags(t *testing.T) {
	db := newAuthTestDB(t)
	userID, _ := seedAuthUser(t, db, "flags", "role_super_admin", "Pwd@1234567")

	svc := newAuthTestSvc(t, db)
	ctx := context.Background()

	login, err := svc.Login(ctx, LoginRequest{
		Username: "flags", Password: preHash("Pwd@1234567"),
	})
	require.NoError(t, err)

	b, err := json.Marshal(login.User)
	require.NoError(t, err)
	var user map[string]any
	require.NoError(t, json.Unmarshal(b, &user))

	assert.Equal(t, true, user["is_platform_super_admin"], "续期后必须仍是超管")
	// 超管 ≠ 运维:isOps 只认 role_platform_ops / role_platform_ops_lead
	// (与 GetCurrentUser 的派生逻辑一致)。这条断言是防止有人"顺手"把
	// super 也算进 ops —— 那个 tag 在 profile 上是"平台运维",不是"超管"。
	assert.Equal(t, false, user["is_platform_ops"], "只有 role_platform_ops(_lead) 才算运维")
	// ID 在 wire 上必须是字符串:sonyflake 是 19 位数,超出 JS 的 2^53 安全整数,
	// JSON.parse 会静默截断(身份切换 404 就是这么来的)。
	assert.Equal(t, strconv.FormatUint(userID, 10), user["id"],
		"user.id 必须是字符串,裸整数会被 JS 截断")
	assert.Contains(t, user, "current_identity_id", "必须带当前身份,前端不再靠 identities[0] 猜")
	assert.IsType(t, "", user["current_identity_id"], "current_identity_id 同样必须是字符串")

	// refresh 响应里同一组标志也必须在
	res, err := svc.RefreshToken(ctx, login.RefreshToken)
	require.NoError(t, err)
	assert.True(t, res.User.IsPlatformSuperAdmin, "续期响应里 is_platform_super_admin 不能丢")
	assert.Equal(t, login.User.CurrentIdentityID, res.User.CurrentIdentityID,
		"续期必须延续原身份,不能重置成 0")
}

// TestRoleFlags_DerivedFromCurrentIdentityNotAssumed
// 两个标志都必须严格从**当前身份**的角色码派生,超管 / 运维互不蕴含。
// 这组断言是为了锁住"平台标记"的语义 —— 它们取代了 handler 里原先那份
// 内联循环,复制了一份派生逻辑。
//
// ⚠️ **M149(2026-11-01):参数从「身份列表」改成「当前身份的 role_code」**
//
//	「身份列表」是**账号属性**("这个账号有没有超管身份"),
//	而这两个标志描述的是"**我现在以什么身份行事**"。
//	多身份用户(超管 + 普通身份)切到普通身份后,按列表判的话标志仍是超管
//	⇒ 界面显示的身份与实际权限对不上。
//
// ⚠️ **M149-b:断言对象从 hasSuperRole/hasOpsRole 改成**视图上的两个字段** ——
// 那两个薄壳已删(零生产调用方):现在标志由 NewCurrentUserView 按角色码
// 一次性派生(唯一口径 platformFlags),而薄壳留着就等于同一个判定做了两遍。
func TestRoleFlags_DerivedFromCurrentIdentityNotAssumed(t *testing.T) {
	for _, tc := range []struct {
		role      string
		wantSuper bool
		wantOps   bool
	}{
		{role: "role_super_admin", wantSuper: true, wantOps: false},
		{role: "role_platform_ops", wantSuper: false, wantOps: true},
		{role: "role_user_admin", wantSuper: false, wantOps: false},
		{role: "role_finance", wantSuper: false, wantOps: false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			view := NewCurrentUserView(&model.SysUser{}, nil, tc.role, 1)
			assert.Equal(t, tc.wantSuper, view.IsPlatformSuperAdmin, "isSuper 派生错")
			assert.Equal(t, tc.wantOps, view.IsPlatformOps, "isOps 派生错")
			assert.Equal(t, tc.role, view.CurrentRoleCode, "当前角色码必须原样下发")
		})
	}

	// 空角色 = 两个标志都 false,不能 panic
	empty := NewCurrentUserView(&model.SysUser{}, nil, "", 0)
	assert.False(t, empty.IsPlatformSuperAdmin)
	assert.False(t, empty.IsPlatformOps)
	assert.Equal(t, "", empty.CurrentRoleCode)
}

// TestIdentityView_ZeroValidFromBecomesEmptyString
// 零值时间此前会被格式化成 "0001-01-01T00:00:00Z" 发给前端。
// 前端拿到这串会当成"有效期从公元 1 年开始",显示成一个假的日期。
func TestIdentityView_ZeroValidFromBecomesEmptyString(t *testing.T) {
	assert.Empty(t, formatIdentityTime(time.Time{}),
		"零值时间应序列化成空串,而不是 0001-01-01")

	u := &model.SysUser{BaseModel: model.BaseModel{ID: 1}, Username: "x"}
	when := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	v := NewCurrentUserView(u, []model.SysUserIdentity{{
		BaseModel: model.BaseModel{ID: 9},
		RoleCode:  "role_super_admin", RoleName: "超管",
		ValidFrom: model.Time{Time: when},
	}}, "role_super_admin", 9)

	require.Len(t, v.Identities, 1)
	assert.Equal(t, when.Format(time.RFC3339), v.Identities[0].ValidFrom)
	assert.Equal(t, uint64(9), v.Identities[0].ID)
	assert.Equal(t, uint64(9), v.CurrentIdentityID)
	assert.Equal(t, true, v.IsPlatformSuperAdmin)
	assert.NotNil(t, v.Identities[0].Permissions, "permissions 必须是 [] 而不是 null —— null 会让前端 .map() 崩")
}

// TestNewCurrentUserView_EmptyIdentitiesIsEmptyArrayNotNull
// identities 为空时 Go 会把 nil slice 序列化成 null,前端 store 里
// `.map()` 直接 TypeError。这里显式保证是 []。
func TestNewCurrentUserView_EmptyIdentitiesIsEmptyArrayNotNull(t *testing.T) {
	v := NewCurrentUserView(&model.SysUser{BaseModel: model.BaseModel{ID: 1}}, nil, "", 0)
	b, err := json.Marshal(v)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	raw, ok := m["identities"].([]any)
	require.True(t, ok, "identities 序列化成 null 会让前端 .map() 崩,got %v", m["identities"])
	assert.Len(t, raw, 0)
}
