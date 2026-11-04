// 配置中心 → 短信生效配置(C6,2026-10-29)
//
// ⚠️ **此前 aliyun.sms.* 这 9 项只存不读**
//
// 平台配置中心(§6.6)里有一组 aliyun.sms.* 的配置项(AccessKey / 签名 / 模板 / provider),
// 面板上能看能改,但**短信 sender 一眼都不看**(sender 只读配置文件)⇒
// 面板改了没反应,**而且没有任何报错** —— 这正是"面板是摆设"的典型形态。
// 这一版把它接上:配置文件打底,配置中心覆盖。
package service

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
)

// smsConfigKeys 配置中心里 aliyun.sms.* 的键 → config.SMSConfig 字段
//
// ⚠️ 键名必须与 seed.go 里 mustSysConfigs 的 Key **逐字一致** ——
// 这里写错一个字母,那一项就静默退回配置文件(不报错)。改 seed 的键名时必须同步这里。
var smsConfigKeys = []struct {
	key   string
	apply func(*config.SMSConfig, string)
}{
	{"aliyun.sms.provider", func(c *config.SMSConfig, v string) { c.Provider = v }},
	{"aliyun.sms.access_key", func(c *config.SMSConfig, v string) { c.AccessKey = v }},
	{"aliyun.sms.access_secret", func(c *config.SMSConfig, v string) { c.AccessSecret = v }},
	{"aliyun.sms.sign_name", func(c *config.SMSConfig, v string) { c.SignName = v }},
	{"aliyun.sms.region", func(c *config.SMSConfig, v string) { c.Region = v }},
	{"aliyun.sms.template.login", func(c *config.SMSConfig, v string) { c.Templates.Login = v }},
	{"aliyun.sms.template.view_full_phone", func(c *config.SMSConfig, v string) { c.Templates.ViewFullPhone = v }},
	{"aliyun.sms.template.reset_password", func(c *config.SMSConfig, v string) { c.Templates.ResetPassword = v }},
	{"aliyun.sms.template.change_phone", func(c *config.SMSConfig, v string) { c.Templates.ChangePhone = v }},
}

// ResolveSMSConfig 用配置中心的值覆盖配置文件里的短信配置
//
// 合并规则(**为什么不是"面板一律优先"**):
//
//	面板值生效 ⇔ 该行非停用 且 值非空 且 **值 ≠ 这一行自带的默认值**
//
// 也就是"**运营改过的才算数**"。理由:
//
//  1. seedAllSysConfigs **跳过已存在的行**(不覆盖运营改过的值)——
//     所以存量库里那 9 行可能还是老模板号 / 老签名这种**假值**。
//     如果"面板一律优先",存量库会把刚配好的配置文件盖回假值 ⇒ 短信直接发不出去。
//  2. 用"改没改过"判断,存量库(值 == 自带默认值)自动让位给配置文件,
//     而运营在面板上真改过的值依然生效。
//  3. 判断基准是**该行自己的 default_value_encrypted**(入库时写死的),
//     不是当前 seed 里的默认值 —— 否则改一次 seed 就会把存量库的"没改过"判成"改过"。
//
// ⚠️ **清空某一格 = 退回配置文件**(值为空 ⇒ 跳过)。这是唯一的"撤销面板覆盖"手段。
//
// 读配置中心失败时**退回配置文件并打 Warn**,不返回错误 ——
// 配置中心抖一下不该让所有人收不到登录验证码(而且此时多半有兜底配置)。
func (s *configCenterService) ResolveSMSConfig(ctx context.Context, base config.SMSConfig) (config.SMSConfig, error) {
	out := base

	keys := make([]string, 0, len(smsConfigKeys))
	for _, k := range smsConfigKeys {
		keys = append(keys, k.key)
	}
	var rows []model.SysConfig
	if err := s.db.WithContext(ctx).Where("config_key IN ?", keys).Find(&rows).Error; err != nil {
		logger.L().Warn("sms: 读配置中心失败, 本次使用配置文件里的短信配置",
			zap.Error(err))
		return base, nil
	}

	byKey := make(map[string]model.SysConfig, len(rows))
	for _, r := range rows {
		byKey[r.ConfigKey] = r
	}
	for _, k := range smsConfigKeys {
		r, ok := byKey[k.key]
		if !ok || !r.IsActive {
			continue
		}
		v := strings.TrimSpace(r.ValueEncrypted)
		if v == "" {
			// 空 = 运营清空了 = 退回配置文件
			continue
		}
		if v == strings.TrimSpace(r.DefaultValueEncrypted) {
			// 还是 seed 的默认值 = 没人改过 = 让位给配置文件
			continue
		}
		k.apply(&out, v)
	}
	return out, nil
}
