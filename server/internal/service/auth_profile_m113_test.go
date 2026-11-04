// M113 个人资料链路回归网(2026-10-29)
//
// # 这一组守的是什么
//
//	用户在个人中心改一次资料,界面上发生了三件事(全都真实发生过):
//
//	 1. **真实姓名字段永远是空的** —— `CurrentUserView` 压根没有 real_name,
//	     前端只能读 `(userInfo as any)?.real_name` ⇒ 恒 undefined。
//	     而且更糟:表单回填成空串后提交,后端 `dto.RealName != nil` 成立
//	     ⇒ `updates["real_name"] = ""` ⇒ **把真实姓名抹成了空**。
//
//	 2. **邮箱被写成了掩码串** —— `CurrentUserView.Email` 之前是
//	     `desensitize.Email(...)` ⇒ "zhangsan@qq.com" 变成 "z******@qq.com"。
//	     而这个掩码值被**回填进可编辑的输入框** ⇒ 用户改个昵称点保存,
//	     后端收到的 email 就是 "z******@qq.com" ⇒ **真实邮箱被掩码串覆盖**。
//
//	 3. **改完资料整个人的信息塌了** —— UpdateProfile 之前返**裸 model.SysUser**,
//	     而它的 phone/email 是 `json:"-"`(不返)、identities 压根不存在
//	     ⇒ 前端 `setUserInfo(updated)` 一执行,身份列表 / 超管标记 /
//	     当前身份 / 手机号全被洗空。
//
//	根因是同一个:**"用户资料"这个概念在四个地方各有一个形状**。
//	现在收口成 `CurrentUserView` 一个 —— 类型保证,不是约定。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 `NewCurrentUserView` 里的 `Email: decryptPII(u.Email)` 改回
//	  `desensitize.Email(decryptPII(u.Email))` → TestM113_View_EmailNotMasked 变红
//	· 把 `CurrentUserView` 的 `RealName` 字段删掉 → 编译不过(这本身就是网)
//	· 把 `UpdateProfile` 的返回值改回 `*model.SysUser` → 编译不过
//	· 把 `currentUserView` 里的 `caller.RoleCode` 改成 `""`
//	  → TestM113_UpdateProfile_ViewCarriesIdentities 变红
package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
)

// ===== 1. 视图本身的字段口径 =====

// TestM113_View_EmailNotMasked 视图里的邮箱是**真邮箱**,不是掩码串
//
// 这是"改一次昵称把邮箱写成 z******@qq.com"那条 bug 的守门。
func TestM113_View_EmailNotMasked(t *testing.T) {
	u := &model.SysUser{
		BaseModel: model.BaseModel{ID: 1},
		Username:  "u1",
		Nickname:  "昵称",
		Email:     "zhangsan@qq.com",
	}
	view := NewCurrentUserView(u, nil, "", 0)

	assert.Equal(t, "zhangsan@qq.com", view.Email,
		"自己的邮箱不能脱敏 —— 掩码值会被表单回填再写回库,把真邮箱覆盖掉")
	assert.NotContains(t, view.Email, "*", "邮箱里不该有星号")
}

// TestM113_View_RealNamePresent 视图带真实姓名
func TestM113_View_RealNamePresent(t *testing.T) {
	u := &model.SysUser{
		BaseModel: model.BaseModel{ID: 1},
		Username:  "u1",
		RealName:  "张三",
	}
	view := NewCurrentUserView(u, nil, "", 0)
	assert.Equal(t, "张三", view.RealName)
}

// TestM113_View_PhoneStillMasked 手机号**仍然脱敏**
//
// 邮箱解禁不等于手机号解禁 —— 手机号要发短信、也会出现在管理员列表里,
// 口径跟邮箱不同。这条锁住"别把手机号一起放开了"。
func TestM113_View_PhoneStillMasked(t *testing.T) {
	u := &model.SysUser{
		BaseModel: model.BaseModel{ID: 1},
		Username:  "u1",
		Phone:     "13812345678",
	}
	view := NewCurrentUserView(u, nil, "", 0)
	assert.NotContains(t, view.Phone, "13812345678", "手机号必须是脱敏的")
	assert.Contains(t, view.Phone, "*")
}

// TestM113_View_WireShape 视图上线的 JSON 形状
//
// 前端 `UserInfo` 靠这几个 key;少一个 / 改名 = 前端读到 undefined。
func TestM113_View_WireShape(t *testing.T) {
	u := &model.SysUser{
		BaseModel: model.BaseModel{ID: 94883951113533126},
		Username:  "u1",
		Nickname:  "昵称",
		RealName:  "张三",
		Phone:     "13812345678",
		Email:     "zhangsan@qq.com",
		Avatar:    "https://cdn.example.com/a.png",
	}
	view := NewCurrentUserView(u, []model.SysUserIdentity{
		{BaseModel: model.BaseModel{ID: 7}, RoleCode: "role_user_admin", RoleName: "用户管理员", IsActive: true, IsPrimary: true},
	}, "role_user_admin", 7)

	raw, err := json.Marshal(view)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))

	for _, key := range []string{
		"id", "username", "nickname", "real_name", "phone", "email", "avatar",
		"identities", "current_identity_id", "is_platform_super_admin", "is_platform_ops",
	} {
		assert.Contains(t, m, key, "线上形状缺字段 %q —— 前端会读到 undefined", key)
	}
	assert.Equal(t, "zhangsan@qq.com", m["email"])
	assert.Equal(t, "张三", m["real_name"])
	assert.Len(t, m["identities"], 1)
}

// ===== 2. UpdateProfile 返回视图 =====

// TestM113_UpdateProfile_ViewCarriesIdentities 改完资料返回的视图带身份和平台标记
//
// 旧实现返裸 SysUser ⇒ identities / is_platform_super_admin / current_identity_id
// 全部丢失 ⇒ 前端一 setUserInfo 就把整个人的上下文洗空。
func TestM113_UpdateProfile_ViewCarriesIdentities(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "m113_u1", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	caller := mkAdminCaller(userID, identityID, "role_user_admin")

	nick := "新昵称"
	view, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{Nickname: &nick})
	require.NoError(t, err)
	require.NotNil(t, view)

	assert.Equal(t, "新昵称", view.Nickname)
	assert.Equal(t, userID, view.ID)
	assert.Equal(t, identityID, view.CurrentIdentityID)
	require.Len(t, view.Identities, 1, "身份列表必须回来 —— 少了前端「我的身份」页签就是空的")
	assert.Equal(t, "role_user_admin", view.Identities[0].RoleCode)
}

// TestM113_UpdateProfile_ViewCarriesSuperAdminFlag 超管标记跟着资料更新一起回来
//
// 这条专治"改一次昵称,右上角的超管标签就没了"。
func TestM113_UpdateProfile_ViewCarriesSuperAdminFlag(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "m113_super", "role_super_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	caller := mkAdminCaller(userID, identityID, "role_super_admin")

	nick := "还是超管"
	view, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{Nickname: &nick})
	require.NoError(t, err)
	assert.True(t, view.IsPlatformSuperAdmin, "超管身份改完资料不该掉标记")
}

// TestM113_UpdateProfile_ViewCarriesPlatformOpsFlag 平台运维标记同理
func TestM113_UpdateProfile_ViewCarriesPlatformOpsFlag(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "m113_ops", "role_platform_ops", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	caller := mkAdminCaller(userID, identityID, "role_platform_ops")

	nick := "还是运维"
	view, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{Nickname: &nick})
	require.NoError(t, err)
	assert.True(t, view.IsPlatformOps)
	assert.False(t, view.IsPlatformSuperAdmin)
}

// TestM113_UpdateProfile_EmailRoundTrip 邮箱改完能原样回来(不被掩码)
//
// 断言的是**回环**:改 email → 视图里的 email == 新值。
// 旧行为下视图里的 email 是掩码串,前端一保存就把掩码写回库。
func TestM113_UpdateProfile_EmailRoundTrip(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "m113_mail", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	caller := mkAdminCaller(userID, identityID, "role_user_admin")

	mail := "newmail@example.com"
	view, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{Email: &mail})
	require.NoError(t, err)
	assert.Equal(t, "newmail@example.com", view.Email)

	// DB 里也必须是明文新值(没有被掩码串污染)
	var reloaded model.SysUser
	require.NoError(t, db.First(&reloaded, userID).Error)
	assert.Equal(t, "newmail@example.com", reloaded.Email)
	assert.False(t, strings.Contains(reloaded.Email, "*"))
}

// TestM113_UpdateProfile_RealNameRoundTrip 真实姓名改完能回来
func TestM113_UpdateProfile_RealNameRoundTrip(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "m113_rn", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	caller := mkAdminCaller(userID, identityID, "role_user_admin")

	real := "李四"
	view, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{RealName: &real})
	require.NoError(t, err)
	assert.Equal(t, "李四", view.RealName)

	var reloaded model.SysUser
	require.NoError(t, db.First(&reloaded, userID).Error)
	assert.Equal(t, "李四", reloaded.RealName)
}

// TestM113_UpdateProfile_RealNameEmptyIsAllowed 真实姓名可以清空
//
// DTO 用指针区分"不改"(nil)与"改空串"(""),这条锁住"清空"这条路走得通 ——
// 用户有权把填错的真实姓名擦掉。
func TestM113_UpdateProfile_RealNameEmptyIsAllowed(t *testing.T) {
	db := newAuthTestDB(t)
	userID, identityID := seedAuthUser(t, db, "m113_rn_clear", "role_user_admin", "Pwd@1234567")
	svc := newAuthTestSvc(t, db)
	caller := mkAdminCaller(userID, identityID, "role_user_admin")

	empty := ""
	view, err := svc.UpdateProfile(context.Background(), caller, UpdateProfileDTO{RealName: &empty})
	require.NoError(t, err)
	assert.Empty(t, view.RealName)
}

// ===== 3. platformFlags 口径唯一 =====

// TestM113_PlatformFlags 平台标记的判定口径
//
// ⚠️ **M149(2026-11-01):参数从「身份列表」改成「当前身份的 role_code」**
//
//	以前判的是"这个用户**所有身份**里有没有超管" —— 那是**账号属性**。
//	多身份用户(超管 + 普通身份)切到普通身份之后,标志仍是超管 ⇒
//	界面显示的身份与实际权限对不上(执行层早就按当前身份判了)。
//	⇒ 现在只认当前身份,与执行层(RBAC 中间件的 caller.RoleCode)同一口径。
//
//	"超管+运维"那条原来断言的是**列表**里两个角色同时存在 ⇒ 两个标志都 true。
//	现在没有"列表"这个概念了 —— 一个身份只有一个角色,不可能既是超管又是运维;
//	这条断言由 TestRoleFlags_DerivedFromCurrentIdentityNotAssumed 覆盖。
func TestM113_PlatformFlags(t *testing.T) {
	cases := []struct {
		name      string
		role      string
		wantSuper bool
		wantIsOps bool
	}{
		{"普通用户", "role_user_admin", false, false},
		{"超管", "role_super_admin", true, false},
		{"运维员", "role_platform_ops", false, true},
		{"空", "", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			super, isOps := platformFlags(c.role)
			assert.Equal(t, c.wantSuper, super)
			assert.Equal(t, c.wantIsOps, isOps)
		})
	}
}
