// voicex —— 阿里云百炼(DashScope)语音能力:音色克隆 + 文本合成
//
// ## 这个包解决什么问题
//
//   - **音色克隆**:把一段参考音频(公网 https URL)注册成可复用音色,
//     拿回 voice_id,后续 TTS 用这个 id 发声
//   - **文本合成**:POST JSON 到可配置的合成端点,拿回音频字节
//
// ## 关键点
//
//   - **凭据缺一不可**:api_key / workspace_id 任一为空,New() 直接报错 ——
//     半可用的客户端会把"没配"拖到第一次调用才炸
//   - **克隆要求 https 公网 URL**:DashScope 服务端会回头 GET 参考音频,
//     http:// 或内网地址在客户端这边就被拒掉(不用等远端 403)
//   - **合成端点不在包里**:voice.synth_url 指向部署方自己的 TTS 网关;
//     不配 = 不装配,NewSynthesizer 显式报错
//   - **响应是 JSON = 合成失败**:网关失败时可能回 200 + {"code","msg"},
//     Once() 先嗅探再当音频返回,不让一段 JSON 冒充 mp3 存下去
//
// ## 跑法
//
//	go run ./examples/voicex
package main

import (
	"fmt"
	"strings"

	"rapid-admin/internal/pkg/voicex"
)

func main() {
	// ── 一、装配期校验:没配就是没配,不当场兜底 ───────────────────
	fmt.Println("=== 装配期校验(缺凭据必须当场报错)===")
	_, err := voicex.New(voicex.Config{})
	fmt.Println("空配置 New:", err)

	_, err = voicex.New(voicex.Config{APIKey: "sk-xxx"})
	fmt.Println("缺 workspace_id New:", err)

	_, err = voicex.NewSynthesizer(voicex.SynthConfig{})
	fmt.Println("空 synth_url NewSynthesizer:", err)

	// ── 二、端点拼装:凭据齐了才给建 ───────────────────────────────
	fmt.Println()
	fmt.Println("=== 正常构造(演示用假凭据,不发起真实请求)===")
	// ⚠️ 示例里的 key 是占位 —— 真实凭据放 configs/config.yaml 的 voice 段,
	//    由业务层装配(voicex 自己不读全局配置,依赖注入)。
	client, err := voicex.New(voicex.Config{
		APIKey:      "sk-demo-000000000000000000000000",
		WorkspaceID: "my-workspace",
		Region:      "", // 空 = 默认 cn-beijing
	})
	if err != nil {
		fmt.Println("构造失败:", err)
		return
	}
	fmt.Println("client 构造成功(默认 region=cn-beijing, model=voice-enrollment)")

	// ── 三、https 闸门:克隆 URL 必须公网可访问 ────────────────────
	fmt.Println()
	fmt.Println("=== https 闸门(不发请求,参数校验先拦)===")
	_, err = client.CreateVoiceFromURL("cosyvoice-v3-plus", "voice", "http://intranet/a.wav", nil)
	fmt.Println("http:// 参考音频:", firstLine(err))

	_, err = client.CreateVoiceFromURL("cosyvoice-v3-plus", "voice", "/local/a.wav", nil)
	fmt.Println("相对路径参考音频:", firstLine(err))

	// ── 四、合成参数校验 ───────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 合成参数校验 ===")
	synth, err := voicex.NewSynthesizer(voicex.SynthConfig{Endpoint: "https://tts.example.com/ai/tts/once"})
	if err != nil {
		fmt.Println("构造失败:", err)
		return
	}
	_, err = synth.Once(voicex.SynthParams{Text: "你好", Voice: ""})
	fmt.Println("缺 voice:", firstLine(err))

	_, err = synth.Once(voicex.SynthParams{Text: "", Voice: "cosyvoice-v3-plus-xxx"})
	fmt.Println("缺 text:", firstLine(err))

	// ── 五、两种远端音色的 ID 归一 ─────────────────────────────────
	fmt.Println()
	fmt.Println("=== RemoteVoice.LocalID():两种返回形态归一 ===")
	fmt.Println("CosyVoice(voice_id 字段):",
		voicex.RemoteVoice{VoiceID: "cosyvoice-v3-plus-abc123"}.LocalID())
	fmt.Println("Qwen-TTS(voice 字段):",
		voicex.RemoteVoice{Voice: "qwen-tts-xyz"}.LocalID())
	fmt.Println("状态常量:", voicex.StatusDeploying, "/", voicex.StatusOK, "/", voicex.StatusUndeployed)
}

// firstLine 错误消息第一行(错误里可能带上游 body,示例输出保持单行)
func firstLine(err error) string {
	if err == nil {
		return "<nil>"
	}
	if i := strings.IndexByte(err.Error(), '\n'); i > 0 {
		return err.Error()[:i]
	}
	return err.Error()
}
