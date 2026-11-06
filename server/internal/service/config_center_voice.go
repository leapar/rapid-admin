// 配置中心 → 语音生效配置(2026-11-06)
//
// voice 组(百炼克隆 / 合成)此前只在 yaml 里:面板上看不见也改不了,
// 换个 api_key / 换个合成网关都得改配置文件重启。这一版接上配置中心:
// 配置文件打底,面板覆盖,消费方(voiceCloneService / ttsVoiceService)
// 在**每次调用时**解析生效配置 —— 面板改完即生效,不用重启。
//
// ⚠️ edge 通道不进面板:免凭据(空配置也能用),面板上没有可改的东西;
//    自建代理 wss_url 属于部署拓扑,留在配置文件。
package service

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
)

// voiceConfigKeys 配置中心里 voice.* 的键 → config.VoiceConfig 字段
//
// ⚠️ 键名必须与 seed.go 里 mustSysConfigs 的 Key **逐字一致** ——
// 这里写错一个字母,那一项就静默退回配置文件(不报错)。改 seed 的键名时必须同步这里。
var voiceConfigKeys = []struct {
	key   string
	apply func(*config.VoiceConfig, string)
}{
	{"voice.api_key", func(c *config.VoiceConfig, v string) { c.APIKey = v }},
	{"voice.workspace_id", func(c *config.VoiceConfig, v string) { c.WorkspaceID = v }},
	{"voice.region", func(c *config.VoiceConfig, v string) { c.Region = v }},
	{"voice.default_target_model", func(c *config.VoiceConfig, v string) { c.DefaultTargetModel = v }},
	{"voice.synth_url", func(c *config.VoiceConfig, v string) { c.SynthURL = v }},
}

// ResolveVoiceConfig 用配置中心的值覆盖配置文件里的语音配置
//
// 合并规则与 ResolveSMSConfig 完全一致(**为什么不是"面板一律优先"**,见
// config_center_sms.go 的注释):
//
//	面板值生效 ⇔ 该行非停用 且 值非空 且 **值 ≠ 这一行自带的默认值**
//
// ⚠️ **清空某一格 = 退回配置文件**(值为空 ⇒ 跳过)。这是唯一的"撤销面板覆盖"手段。
//
// 读配置中心失败时**退回配置文件并打 Warn**,不返回错误 ——
// 配置中心抖一下不该让合成 / 克隆整体不可用。
func (s *configCenterService) ResolveVoiceConfig(ctx context.Context, base config.VoiceConfig) (config.VoiceConfig, error) {
	out := base

	keys := make([]string, 0, len(voiceConfigKeys))
	for _, k := range voiceConfigKeys {
		keys = append(keys, k.key)
	}
	var rows []model.SysConfig
	if err := s.db.WithContext(ctx).Where("config_key IN ?", keys).Find(&rows).Error; err != nil {
		logger.L().Warn("voice: 读配置中心失败, 本次使用配置文件里的语音配置",
			zap.Error(err))
		return base, nil
	}

	byKey := make(map[string]model.SysConfig, len(rows))
	for _, r := range rows {
		byKey[r.ConfigKey] = r
	}
	for _, k := range voiceConfigKeys {
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
