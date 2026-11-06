// 配置中心 voice.* 组 → 语音生效配置的合并规则(2026-11-06)
//
// # 这一组守的是什么
//
//	平台配置中心里有一组 voice.*(百炼克隆 / 合成)的配置项。接上之前,
//	它们**只在 yaml 里**:面板看不见、改不了,换个 api_key / 换个合成网关
//	都得改配置文件重启。
//
//	接上之后合并规则只有一条(与 aliyun.sms 同一套,推导见
//	config_center_sms_test.go 的头注)
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 `if v == strings.TrimSpace(r.DefaultValueEncrypted) { continue }` 删掉
//	  → TestVoiceCfg_FileValueWinsWhenUnchanged 变红
//	· 把 5 个键名里任意一个写错(比如 voice.synth_url 拼错)
//	  → TestVoiceCfg_AllFiveKeysAreWired 变红
//	· 把"值为空 ⇒ 跳过"删掉
//	  → TestVoiceCfg_ClearedValueFallsBackToFile 变红
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
)

func baseVoiceConfig() config.VoiceConfig {
	return config.VoiceConfig{
		APIKey:         "sk-from-file",
		WorkspaceID:    "ws-from-file",
		Region:         "cn-beijing",
		DefaultModel:   "voice-enrollment",
		DefaultTargetModel: "cosyvoice-v3-plus",
		SynthURL:       "https://tts-file.example.com/once",
	}
}

// TestVoiceCfg_FileValueWinsWhenUnchanged 没人改过 ⇒ 配置文件说了算
func TestVoiceCfg_FileValueWinsWhenUnchanged(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	seedSmsRow(t, db, "voice.region", "cn-beijing", "cn-beijing", true)
	seedSmsRow(t, db, "voice.default_target_model", "cosyvoice-v3-plus", "cosyvoice-v3-plus", true)

	got, err := svc.ResolveVoiceConfig(context.Background(), baseVoiceConfig())
	require.NoError(t, err)

	assert.Equal(t, "cn-beijing", got.Region, "没人改过的项 ⇒ 配置文件说了算")
	assert.Equal(t, "cosyvoice-v3-plus", got.DefaultTargetModel, "目标模型同理")
	assert.Equal(t, "sk-from-file", got.APIKey, "空值 ⇒ 配置文件说了算")
}

// TestVoiceCfg_PanelValueWinsWhenChanged 运营改过 ⇒ 面板说了算
func TestVoiceCfg_PanelValueWinsWhenChanged(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	seedSmsRow(t, db, "voice.api_key", "sk-panel", "", true)
	seedSmsRow(t, db, "voice.workspace_id", "ws-panel", "", true)
	seedSmsRow(t, db, "voice.synth_url", "https://tts-panel.example.com/once", "", true)

	got, err := svc.ResolveVoiceConfig(context.Background(), baseVoiceConfig())
	require.NoError(t, err)

	assert.Equal(t, "sk-panel", got.APIKey, "运营填了密钥 ⇒ 面板的值生效")
	assert.Equal(t, "ws-panel", got.WorkspaceID)
	assert.Equal(t, "https://tts-panel.example.com/once", got.SynthURL, "合成端点可运行时换网关")
}

// TestVoiceCfg_ClearedValueFallsBackToFile 清空 ⇒ 退回配置文件
func TestVoiceCfg_ClearedValueFallsBackToFile(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	seedSmsRow(t, db, "voice.synth_url", "", "https://tts-file.example.com/once", true)

	got, err := svc.ResolveVoiceConfig(context.Background(), baseVoiceConfig())
	require.NoError(t, err)

	assert.Equal(t, "https://tts-file.example.com/once", got.SynthURL, "清空 ⇒ 退回配置文件")
}

// TestVoiceCfg_InactiveRowIgnored 停用的项不参与覆盖
func TestVoiceCfg_InactiveRowIgnored(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	seedSmsRow(t, db, "voice.api_key", "sk-stale", "sk-stale", false)

	got, err := svc.ResolveVoiceConfig(context.Background(), baseVoiceConfig())
	require.NoError(t, err)

	assert.Equal(t, "sk-from-file", got.APIKey, "停用的项不参与覆盖")
}

// TestVoiceCfg_AllFiveKeysAreWired 五个键一个都不能漏/写错
//
// 键名写错(比如 voice.synth_url 拼错)在这里会暴露:面板上改了这一项,
// 配置却不生效 ⇒ 静默失效。
func TestVoiceCfg_AllFiveKeysAreWired(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	overrides := map[string]string{
		"voice.api_key":               "sk-panel",
		"voice.workspace_id":          "ws-panel",
		"voice.region":                "cn-shanghai",
		"voice.default_target_model":  "cosyvoice-v3-flash",
		"voice.synth_url":             "https://tts-panel.example.com/once",
	}
	for k, v := range overrides {
		seedSmsRow(t, db, k, v, "seed-default", true)
	}

	got, err := svc.ResolveVoiceConfig(context.Background(), baseVoiceConfig())
	require.NoError(t, err)

	assert.Equal(t, "sk-panel", got.APIKey)
	assert.Equal(t, "ws-panel", got.WorkspaceID)
	assert.Equal(t, "cn-shanghai", got.Region)
	assert.Equal(t, "cosyvoice-v3-flash", got.DefaultTargetModel)
	assert.Equal(t, "https://tts-panel.example.com/once", got.SynthURL)
}

// TestVoiceCfg_NoRowsKeepsFileConfig 一行都没有 ⇒ 原样返回配置文件
func TestVoiceCfg_NoRowsKeepsFileConfig(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)

	base := baseVoiceConfig()
	got, err := svc.ResolveVoiceConfig(context.Background(), base)
	require.NoError(t, err)
	assert.Equal(t, base, got, "没有配置行时原样返回配置文件")
}

// TestVoiceCfg_DBErrorFallsBackToFile 配置中心读挂了 ⇒ 退回配置文件
func TestVoiceCfg_DBErrorFallsBackToFile(t *testing.T) {
	db := newSmsConfigTestDB(t)
	svc := NewConfigCenterService(db)
	require.NoError(t, db.Migrator().DropTable(&model.SysConfig{}))

	base := baseVoiceConfig()
	got, err := svc.ResolveVoiceConfig(context.Background(), base)
	require.NoError(t, err, "读配置中心失败不该让合成失败")
	assert.Equal(t, base, got, "读失败 ⇒ 退回配置文件")
}
