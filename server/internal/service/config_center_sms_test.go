// C6 · 配置中心 aliyun.sms.* → 短信生效配置的合并规则(2026-10-29)
//
// # 这一组守的是什么
//
//	平台配置中心里有一组 aliyun.sms.* 的配置项(AccessKey / 签名 / 模板 / provider)。
//	在 C6 之前,它们**只存不读** —— 面板上改了,短信 sender 一眼都不看,
//	**改了没反应,而且不报错**。
//
//	接上之后,合并规则只有一条,但这条规则不显然:
//
//	面板值生效 ⇔ 值非空 且 **值 ≠ 这一行自带的默认值**(即"运营改过的才算数")
//
//	为什么不是"面板一律优先"?
//
//	  - seedAllSysConfigs **跳过已存在的行** ⇒ 存量库里那 9 行可能还是
//	    老模板号 / 老签名这种**假值**;"面板一律优先"会把刚配好的配置文件
//	    盖回假值 ⇒ 短信直接发不出去。
//	  - 判断基准必须是**该行自己的 default_value_encrypted**(入库时写死的),
//	    不是当前 seed 里的默认值 —— 否则改一次 seed 就会把存量库的"没改过"
//	    判成"改过",反过来污染配置。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 `if v == strings.TrimSpace(r.DefaultValueEncrypted) { continue }` 删掉
//	  → TestC6_ResolveSMSConfig_FileValueWinsWhenUnchanged 变红
//	· 把 9 个键名里任意一个写错(比如 template.view_full_phone)
//	  → TestC6_ResolveSMSConfig_AllNineKeysAreWired 变红
//	· 把"值为空 ⇒ 跳过"删掉
//	  → TestC6_ResolveSMSConfig_ClearedValueFallsBackToFile 变红
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// newSmsConfigTestDB 只建配置中心那张表
func newSmsConfigTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysConfig{}))
	return db
}

// seedSmsRow 造一行 aliyun.sms.* 配置
//   - value = 面板上现在的值
//   - def   = 这一行入库时自带的默认值(seed 写死的那个)
//   - active= false 模拟"停用这一项"
//
// ⚠️ IsActive 带 `gorm:"default:true"`,**零值(false)在 Create 时会被 GORM 省略**,
//    让数据库默认值(true)生效 ⇒ 要造"停用"的行必须建完再显式 Update。
func seedSmsRow(t *testing.T, db *gorm.DB, key, value, def string, active bool) {
	t.Helper()
	row := &model.SysConfig{
		BaseModel:             model.BaseModel{ID: sonyflake.Next()},
		ConfigKey:             key,
		ConfigGroup:           "aliyun.sms",
		ConfigName:            key,
		ValueType:             model.ConfigTypeString,
		ValueEncrypted:        value,
		DefaultValueEncrypted: def,
		ValuePreview:          value,
		IsActive:              true,
	}
	require.NoError(t, db.Create(row).Error)
	if !active {
		require.NoError(t, db.Model(row).Update("is_active", false).Error)
	}
}

// baseSmsConfig 配置文件里那一份(打底)
func baseSmsConfig() config.SMSConfig {
	return config.SMSConfig{
		Provider:     "aliyun",
		AccessKey:    "AK-from-file",
		AccessSecret: "SK-from-file",
		SignName:     "签名-来自配置文件",
		Region:       "cn-hangzhou",
		Templates: config.SMSTemplate{
			Login:         "TPL-login-file",
			ViewFullPhone: "TPL-view-file",
			ResetPassword: "TPL-reset-file",
			ChangePhone:   "TPL-change-file",
		},
	}
}

// TestC6_ResolveSMSConfig_FileValueWinsWhenUnchanged 没人改过 ⇒ 配置文件说了算
//
// 这条是**存量库保护**:老库里那 9 行还是假值(值 == 自带默认值),
// 如果它们盖住配置文件,刚配好的短信参数会被假值顶掉。
func TestC6_ResolveSMSConfig_FileValueWinsWhenUnchanged(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	// 存量库的样子:值 == 自带默认值 = 没人改过
	seedSmsRow(t, db, "aliyun.sms.sign_name", "老签名", "老签名", true)
	seedSmsRow(t, db, "aliyun.sms.template.login", "SMS_STALE", "SMS_STALE", true)
	seedSmsRow(t, db, "aliyun.sms.access_key", "", "", true)

	got, err := svc.ResolveSMSConfig(context.Background(), baseSmsConfig())
	require.NoError(t, err)

	assert.Equal(t, "签名-来自配置文件", got.SignName, "没人改过的项 ⇒ 配置文件说了算")
	assert.Equal(t, "TPL-login-file", got.Templates.Login, "模板同理")
	assert.Equal(t, "AK-from-file", got.AccessKey, "空值 ⇒ 配置文件说了算")
}

// TestC6_ResolveSMSConfig_PanelValueWinsWhenChanged 运营改过 ⇒ 面板说了算
func TestC6_ResolveSMSConfig_PanelValueWinsWhenChanged(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	seedSmsRow(t, db, "aliyun.sms.sign_name", "新签名", "老签名", true)
	seedSmsRow(t, db, "aliyun.sms.access_key", "AK-from-panel", "", true)
	seedSmsRow(t, db, "aliyun.sms.provider", "console", "aliyun", true)

	got, err := svc.ResolveSMSConfig(context.Background(), baseSmsConfig())
	require.NoError(t, err)

	assert.Equal(t, "新签名", got.SignName, "运营改过 ⇒ 面板的值生效")
	assert.Equal(t, "AK-from-panel", got.AccessKey, "运营填了密钥 ⇒ 面板的值生效")
	assert.Equal(t, "console", got.Provider, "provider 也是可运行时改的")
}

// TestC6_ResolveSMSConfig_ClearedValueFallsBackToFile 清空 ⇒ 退回配置文件
//
// 这是"撤销面板覆盖"的唯一手段:清空这一格。
func TestC6_ResolveSMSConfig_ClearedValueFallsBackToFile(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	seedSmsRow(t, db, "aliyun.sms.sign_name", "", "老签名", true)

	got, err := svc.ResolveSMSConfig(context.Background(), baseSmsConfig())
	require.NoError(t, err)

	assert.Equal(t, "签名-来自配置文件", got.SignName, "清空 ⇒ 退回配置文件")
}

// TestC6_ResolveSMSConfig_InactiveRowIgnored 停用的项不参与覆盖
func TestC6_ResolveSMSConfig_InactiveRowIgnored(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	seedSmsRow(t, db, "aliyun.sms.sign_name", "停用但有值的签名", "老签名", false)

	got, err := svc.ResolveSMSConfig(context.Background(), baseSmsConfig())
	require.NoError(t, err)

	assert.Equal(t, "签名-来自配置文件", got.SignName, "停用的项不参与覆盖")
}

// TestC6_ResolveSMSConfig_AllNineKeysAreWired 九个键一个都不能漏/写错
//
// 键名写错(比如 template.view_full_phone 拼错)在这里会暴露:
// 面板上改了这一项,配置却不生效 ⇒ 静默失效。
func TestC6_ResolveSMSConfig_AllNineKeysAreWired(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	overrides := map[string]string{
		"aliyun.sms.provider":                "console",
		"aliyun.sms.access_key":               "AK-panel",
		"aliyun.sms.access_secret":            "SK-panel",
		"aliyun.sms.sign_name":                "签名-panel",
		"aliyun.sms.region":                   "cn-shanghai",
		"aliyun.sms.template.login":           "TPL-panel-login",
		"aliyun.sms.template.view_full_phone": "TPL-panel-view",
		"aliyun.sms.template.reset_password":  "TPL-panel-reset",
		"aliyun.sms.template.change_phone":    "TPL-panel-change",
	}
	for k, v := range overrides {
		seedSmsRow(t, db, k, v, "seed-default", true)
	}

	got, err := svc.ResolveSMSConfig(context.Background(), baseSmsConfig())
	require.NoError(t, err)

	assert.Equal(t, "console", got.Provider)
	assert.Equal(t, "AK-panel", got.AccessKey)
	assert.Equal(t, "SK-panel", got.AccessSecret)
	assert.Equal(t, "签名-panel", got.SignName)
	assert.Equal(t, "cn-shanghai", got.Region)
	assert.Equal(t, "TPL-panel-login", got.Templates.Login)
	assert.Equal(t, "TPL-panel-view", got.Templates.ViewFullPhone)
	assert.Equal(t, "TPL-panel-reset", got.Templates.ResetPassword)
	assert.Equal(t, "TPL-panel-change", got.Templates.ChangePhone)
}

// TestC6_ResolveSMSConfig_NoRowsKeepsFileConfig 一行都没有 ⇒ 原样返回配置文件
func TestC6_ResolveSMSConfig_NoRowsKeepsFileConfig(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	base := baseSmsConfig()
	got, err := svc.ResolveSMSConfig(context.Background(), base)
	require.NoError(t, err)
	assert.Equal(t, base, got, "没有配置行时原样返回配置文件")
}

// TestC6_ResolveSMSConfig_DBErrorFallsBackToFile 配置中心读挂了 ⇒ 退回配置文件
//
// 短信不能因为配置中心抖一下就发不出去(那会让所有人登不了录)。
func TestC6_ResolveSMSConfig_DBErrorFallsBackToFile(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)
	require.NoError(t, db.Migrator().DropTable(&model.SysConfig{}))

	base := baseSmsConfig()
	got, err := svc.ResolveSMSConfig(context.Background(), base)
	require.NoError(t, err, "读配置中心失败不该让发送失败")
	assert.Equal(t, base, got, "读失败 ⇒ 退回配置文件")
}
