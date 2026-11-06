// 语音能力配置(voicex:音色克隆 + 文本合成)
//
// ⚠️ **api_key / workspace_id 未配 = 克隆相关接口显式报错**,不是静默降级 ——
// 音色克隆是"花钱的外呼"(调一次 DashScope 计一次费),悄悄失败和
// 悄悄成功一样不可接受,必须让调用方拿到"没配"这三个字。
//
// ⚠️ **synth_url 未配 = 文本合成不装配** —— 与 email.provider 空串同一纪律:
// 合成是额外交付,没配就是没配;但**一旦配了就按配置走**,不会兜底到任何
// 内置端点(内置不了 —— 合成后端是部署方自己的 TTS 网关)。
package config

// VoiceConfig 语音能力配置(mapstructure "voice")
type VoiceConfig struct {
	// APIKey 阿里云百炼 API Key(音色克隆必填)
	APIKey string `mapstructure:"api_key"`
	// WorkspaceID 百炼业务空间 ID(拼进端点域名 https://{workspace_id}.{region}.maas.aliyuncs.com)
	WorkspaceID string `mapstructure:"workspace_id"`
	// Region 默认 cn-beijing
	Region string `mapstructure:"region"`
	// DefaultModel 音色克隆默认 model,默认 voice-enrollment;
	// 配 qwen-voice-enrollment 时 CreateVoiceFromURL 实际走 Qwen-TTS 路径
	DefaultModel string `mapstructure:"default_model"`
	// DefaultTargetModel 克隆目标音色模型,默认 cosyvoice-v3-plus
	DefaultTargetModel string `mapstructure:"default_target_model"`
	// TimeoutSeconds 克隆接口单次 HTTP 超时秒数,默认 120(审核链路偏慢)
	TimeoutSeconds int `mapstructure:"timeout_seconds"`

	// SynthURL 文本合成(TTS)端点。空 = 不装配,合成接口显式报错。
	// 请求形态 POST JSON {text, voice, tts, volume, rate},成功回二进制音频。
	SynthURL string `mapstructure:"synth_url"`
	// SynthTimeoutSeconds 合成单次 HTTP 超时秒数,默认 60
	SynthTimeoutSeconds int `mapstructure:"synth_timeout_seconds"`
}
