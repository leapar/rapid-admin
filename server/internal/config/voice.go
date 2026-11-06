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
	// DefaultModel 音色克隆默认 model,默认 voice-enrollment(CosyVoice 专用,
	// 本框架刻意只接 CosyVoice,不接 Qwen-TTS/Qwen-Audio)
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

	// AsrModel 实时语音识别模型(asrx),空 = paraformer-realtime-v2。
	// 识别与克隆/合成共用同一把 api_key / workspace_id(百炼同一账号体系)。
	AsrModel string `mapstructure:"asr_model"`
	// AsrTimeoutSeconds 识别整次任务兜底超时秒数,空/0 = 300
	AsrTimeoutSeconds int `mapstructure:"asr_timeout_seconds"`

	// Edge 微软 Edge 朗读(免凭据)合成客户端配置。全部可空。
	//
	// ⚠️ **与 synth_url 的纪律相反** —— Edge 无需凭据,空配置也装配;
	// 只有请求里 provider=edge 时才会真正外呼微软。
	Edge VoiceEdgeConfig `mapstructure:"edge"`
}

// VoiceEdgeConfig Edge TTS 配置(全部可空)
type VoiceEdgeConfig struct {
	// Token 主握手(api.msedgeservices.com)的 Ocp-Apim-Subscription-Key。
	// 空 = 跳过主握手,直接走 Bing 公共 TrustedClientToken 备用通道。
	Token string `mapstructure:"token"`
	// WssURL 备用握手端点(空 = 内置 Bing 默认)。自建 edge 代理时指过来。
	WssURL string `mapstructure:"wss_url"`
	// ReadTimeoutSeconds 单次 read 超时秒数(Edge 端卡死检测),默认 30
	ReadTimeoutSeconds int `mapstructure:"read_timeout_seconds"`
	// TotalTimeoutSeconds 整次合成兜底超时秒数,默认 180
	TotalTimeoutSeconds int `mapstructure:"total_timeout_seconds"`
}
