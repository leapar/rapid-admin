// Package voicex —— 阿里云百炼 (DashScope) 语音能力:音色克隆 + 文本合成(TTS)。
//
// ## 这个包解决什么问题
//
// 两件事,共用一份凭据(api_key / workspace_id):
//
//   - **音色克隆**(voice-enrollment):把一段参考音频注册成可复用的音色,
//     拿回 voice_id,后续 TTS 用这个 id 发声。单端点 + model/action 切换:
//     POST https://{WorkspaceId}.{region}.maas.aliyuncs.com/api/v1/services/audio/tts/customization
//   - **文本合成**(synth):把文本转成音频字节。合成**不在 DashScope 克隆端点里**,
//     而是交给一个可配置的合成端点(部署方自己的 TTS 网关,请求形态
//     `{text, voice, tts, volume, rate}`,成功回二进制音频,失败回 JSON)。
//     这样换合成后端不用改代码 —— 换 `voice.synth_url` 一个配置就行。
//
// ## 关键点
//
//   - **凭据从 yaml 来**(voice.api_key / voice.workspace_id),不内置任何
//     "远程读配置"的逻辑 —— 那是部署方的事,框架只认配置结构体。
//   - **api_key 为空 = 客户端能建出来但一调就报错**(显式错误,不静默)。
//   - 克隆要求参考音频是 **https:// 公网可访问** 的 URL —— DashScope 服务端
//     会回头 GET 它;http:// 或内网地址都会被远端拒掉。
//   - Qwen-TTS 与 CosyVoice/Qwen-Audio-TTS 的 action / model / 返回字段
//     都不一样(voice vs voice_id),RemoteVoice.LocalID() 把两种归一。
//
// ## 跑法
//
//	go run ./examples/voicex
package voicex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Action DashScope voice customization action
type Action string

const (
	ActionCreateVoice Action = "create_voice" // 创建音色 (Qwen-Audio-TTS / CosyVoice)
	ActionCreate      Action = "create"       // 创建音色 (Qwen-TTS)
	ActionListVoice   Action = "list_voice"   // 列出音色 (Qwen-Audio-TTS / CosyVoice)
	ActionList        Action = "list"         // 列出音色 (Qwen-TTS)
	ActionQueryVoice  Action = "query_voice"  // 查询单个音色 (Qwen-Audio-TTS / CosyVoice)
	ActionUpdateVoice Action = "update_voice" // 更新音色 (Qwen-Audio-TTS / CosyVoice)
	ActionDeleteVoice Action = "delete_voice" // 删除音色 (Qwen-Audio-TTS / CosyVoice)
	ActionDelete      Action = "delete"       // 删除音色 (Qwen-TTS)
)

// Model DashScope voice model identifier
type Model string

const (
	ModelVoiceEnrollment     Model = "voice-enrollment"      // Qwen-Audio-TTS / CosyVoice
	ModelQwenVoiceEnrollment Model = "qwen-voice-enrollment" // Qwen-TTS 克隆
	ModelQwenVoiceDesign     Model = "qwen-voice-design"     // Qwen-TTS 设计
)

// VoiceStatus DashScope voice 状态 (Qwen-Audio-TTS / CosyVoice 才有, Qwen-TTS 不可用即生效)
type VoiceStatus string

const (
	StatusDeploying  VoiceStatus = "DEPLOYING"  // 审核中
	StatusOK         VoiceStatus = "OK"         // 已发布
	StatusUndeployed VoiceStatus = "UNDEPLOYED" // 审核拒绝
)

// RemoteVoice 远端音色列表项
type RemoteVoice struct {
	VoiceID        string      `json:"voice_id,omitempty"`    // Qwen-Audio-TTS / CosyVoice
	Voice          string      `json:"voice,omitempty"`       // Qwen-TTS
	TargetModel    string      `json:"target_model,omitempty"`
	GmtCreate      string      `json:"gmt_create,omitempty"`
	GmtModified    string      `json:"gmt_modified,omitempty"`
	Status         VoiceStatus `json:"status,omitempty"`
	ResourceLink   string      `json:"resource_link,omitempty"`
	FallbackMode   *bool       `json:"fallback_mode,omitempty"`
	FallbackReason string      `json:"fallback_reason,omitempty"`
}

// LocalID 返回统一的本地引用 ID(Qwen-TTS 走 voice 字段,其余走 voice_id)
func (v RemoteVoice) LocalID() string {
	if v.VoiceID != "" {
		return v.VoiceID
	}
	return v.Voice
}

// Config 客户端配置(api_key 必填;其余有默认值)
type Config struct {
	// APIKey 百炼 API Key(必填;为空时客户端可构造但每次调用显式报错)
	APIKey string
	// WorkspaceID 百炼业务空间 ID(必填,拼进端点域名)
	WorkspaceID string
	// Region 默认 cn-beijing
	Region string
	// DefaultModel 默认 voice-enrollment;
	// 配 qwen-voice-enrollment 时 CreateVoiceFromURL 实际走 Qwen-TTS 路径
	DefaultModel string
	// TimeoutSeconds 单次 HTTP 超时,默认 120s(克隆审核链路偏慢)
	TimeoutSeconds int
}

// Client DashScope 语音克隆客户端
type Client struct {
	apiKey     string
	endpoint   string
	model      Model
	httpClient *http.Client
}

// New 构造客户端。apiKey / workspaceId 任一为空只记在错误信息里吗 —— 不,
// 两者**都必须非空**,缺了直接报错:半可用的客户端只会把"没配"拖到第一次
// 调用时才炸,排障的人要从调用栈反推回配置。
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("voicex: api_key 未配置")
	}
	if cfg.WorkspaceID == "" {
		return nil, fmt.Errorf("voicex: workspace_id 未配置")
	}
	region := cfg.Region
	if region == "" {
		region = "cn-beijing"
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}
	model := cfg.DefaultModel
	if model == "" {
		model = string(ModelVoiceEnrollment)
	}
	return &Client{
		apiKey:   cfg.APIKey,
		endpoint: fmt.Sprintf("https://%s.%s.maas.aliyuncs.com/api/v1/services/audio/tts/customization", cfg.WorkspaceID, region),
		model:    Model(model),
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}, nil
}

// createVoiceInput Qwen-Audio-TTS / CosyVoice create_voice
type createVoiceInput struct {
	Action                    string   `json:"action"`
	TargetModel               string   `json:"target_model"`
	Prefix                    string   `json:"prefix,omitempty"`
	URL                       string   `json:"url,omitempty"`
	VoiceID                   string   `json:"voice_id,omitempty"`
	LanguageHints             []string `json:"language_hints,omitempty"`
	MaxPromptAudioLength      float64  `json:"max_prompt_audio_length,omitempty"`
	EnablePreprocess          *bool    `json:"enable_preprocess,omitempty"`
	EnableVolumeNormalization string   `json:"enable_volume_normalization,omitempty"`
}

// createInput Qwen-TTS create
type createInput struct {
	Action        string      `json:"action"`
	TargetModel   string      `json:"target_model"`
	PreferredName string      `json:"preferred_name,omitempty"`
	Voice         string      `json:"voice,omitempty"`
	Audio         *audioBlock `json:"audio,omitempty"`
	Text          string      `json:"text,omitempty"`
	Language      string      `json:"language,omitempty"`
}

type audioBlock struct {
	Data string `json:"data"`
}

// listInput Qwen-Audio-TTS / CosyVoice list_voice
type listInput struct {
	Action    string `json:"action"`
	Prefix    string `json:"prefix,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
	PageIndex int    `json:"page_index,omitempty"`
}

// listInputQwenTTS Qwen-TTS list
type listInputQwenTTS struct {
	Action    string `json:"action"`
	Prefix    string `json:"prefix,omitempty"`
	Voice     string `json:"voice,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
	PageIndex int    `json:"page_index,omitempty"`
}

// queryVoiceInput Qwen-Audio-TTS / CosyVoice query_voice
type queryVoiceInput struct {
	Action  string `json:"action"`
	VoiceID string `json:"voice_id"`
}

// updateVoiceInput Qwen-Audio-TTS / CosyVoice update_voice
type updateVoiceInput struct {
	Action  string `json:"action"`
	VoiceID string `json:"voice_id"`
	URL     string `json:"url"`
}

// deleteVoiceInput Qwen-Audio-TTS / CosyVoice delete_voice
type deleteVoiceInput struct {
	Action  string `json:"action"`
	VoiceID string `json:"voice_id"`
}

// deleteInputQwenTTS Qwen-TTS delete
type deleteInputQwenTTS struct {
	Action string `json:"action"`
	Voice  string `json:"voice"`
}

// requestBody 通用请求体
type requestBody struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

// responseBody 通用响应体
type responseBody struct {
	Output    json.RawMessage `json:"output"`
	Usage     json.RawMessage `json:"usage"`
	RequestID string          `json:"request_id"`
	Code      string          `json:"code,omitempty"`
	Message   string          `json:"message,omitempty"`
}

// voiceListOutput list_voice 响应
type voiceListOutput struct {
	VoiceList  []RemoteVoice `json:"voice_list"`
	PageIndex  int           `json:"page_index,omitempty"`
	PageSize   int           `json:"page_size,omitempty"`
	TotalCount int           `json:"total_count,omitempty"`
}

// createOutput create_voice/create 响应 (Qwen-Audio-TTS / CosyVoice)
type createOutput struct {
	VoiceID string `json:"voice_id"`
}

// createOutputQwenTTS create 响应 (Qwen-TTS)
type createOutputQwenTTS struct {
	Voice       string `json:"voice"`
	TargetModel string `json:"target_model"`
}

// queryVoiceOutput query_voice 响应
type queryVoiceOutput struct {
	ResourceLink string      `json:"resource_link"`
	TargetModel  string      `json:"target_model"`
	GmtCreate    string      `json:"gmt_create"`
	GmtModified  string      `json:"gmt_modified"`
	Status       VoiceStatus `json:"status"`
}

// CreateVoiceFromURL 通过公网可访问音频 URL 创建音色 (Qwen-Audio-TTS / CosyVoice)
// 返回 DashScope 返回的 voice_id
func (c *Client) CreateVoiceFromURL(targetModel, prefix, audioURL string, languageHints []string) (string, error) {
	if !strings.HasPrefix(audioURL, "https://") {
		return "", fmt.Errorf("voicex: audioURL 必须为 https:// 公网可访问地址(DashScope 服务端会回头 GET 它),拿到的是 %q", audioURL)
	}
	input := createVoiceInput{
		Action:        string(ActionCreateVoice),
		TargetModel:   targetModel,
		Prefix:        prefix,
		URL:           audioURL,
		LanguageHints: languageHints,
	}
	// 使用 Client.model 而非硬编码 ModelVoiceEnrollment,
	// 这样配置 voice.default_model=qwen-voice-enrollment 时实际走 Qwen-TTS 路径
	body := requestBody{Model: string(c.model), Input: input}
	var out createOutput
	if err := c.call(body, &out); err != nil {
		return "", err
	}
	if out.VoiceID == "" {
		return "", fmt.Errorf("voicex: DashScope 返回 voice_id 为空")
	}
	return out.VoiceID, nil
}

// CreateVoiceFromBase64 通过 Base64 data URL 创建音色 (Qwen-TTS)
// audioDataURL 形如 "data:audio/mpeg;base64,xxxxx"
func (c *Client) CreateVoiceFromBase64(targetModel, preferredName, audioDataURL, text, language string) (string, error) {
	input := createInput{
		Action:        string(ActionCreate),
		TargetModel:   targetModel,
		PreferredName: preferredName,
		Audio:         &audioBlock{Data: audioDataURL},
		Text:          text,
		Language:      language,
	}
	body := requestBody{Model: string(ModelQwenVoiceEnrollment), Input: input}
	var out createOutputQwenTTS
	if err := c.call(body, &out); err != nil {
		return "", err
	}
	if out.Voice == "" {
		return "", fmt.Errorf("voicex: DashScope 返回 voice 为空")
	}
	return out.Voice, nil
}

// ListVoices 列出所有音色 (Qwen-Audio-TTS / CosyVoice)
func (c *Client) ListVoices(prefix string, pageIndex, pageSize int) ([]RemoteVoice, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	input := listInput{
		Action:    string(ActionListVoice),
		Prefix:    prefix,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}
	body := requestBody{Model: string(ModelVoiceEnrollment), Input: input}
	var out voiceListOutput
	if err := c.call(body, &out); err != nil {
		return nil, err
	}
	return out.VoiceList, nil
}

// ListQwenTTSVoices 列出所有音色 (Qwen-TTS)
func (c *Client) ListQwenTTSVoices(prefix string, pageIndex, pageSize int) ([]RemoteVoice, int, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	input := listInputQwenTTS{
		Action:    string(ActionList),
		Prefix:    prefix,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}
	body := requestBody{Model: string(ModelQwenVoiceEnrollment), Input: input}
	var out voiceListOutput
	if err := c.call(body, &out); err != nil {
		return nil, 0, err
	}
	return out.VoiceList, out.TotalCount, nil
}

// QueryVoice 查询单个音色 (Qwen-Audio-TTS / CosyVoice)
func (c *Client) QueryVoice(voiceID string) (*queryVoiceOutput, error) {
	input := queryVoiceInput{Action: string(ActionQueryVoice), VoiceID: voiceID}
	body := requestBody{Model: string(ModelVoiceEnrollment), Input: input}
	var out queryVoiceOutput
	if err := c.call(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateVoice 更新音色参考音频 (Qwen-Audio-TTS / CosyVoice)
func (c *Client) UpdateVoice(voiceID, audioURL string) error {
	input := updateVoiceInput{Action: string(ActionUpdateVoice), VoiceID: voiceID, URL: audioURL}
	body := requestBody{Model: string(ModelVoiceEnrollment), Input: input}
	return c.call(body, nil)
}

// DeleteVoice 删除音色 (Qwen-Audio-TTS / CosyVoice)
func (c *Client) DeleteVoice(voiceID string) error {
	input := deleteVoiceInput{Action: string(ActionDeleteVoice), VoiceID: voiceID}
	body := requestBody{Model: string(ModelVoiceEnrollment), Input: input}
	return c.call(body, nil)
}

// DeleteQwenTTSVoice 删除音色 (Qwen-TTS)
func (c *Client) DeleteQwenTTSVoice(voice string) error {
	input := deleteInputQwenTTS{Action: string(ActionDelete), Voice: voice}
	body := requestBody{Model: string(ModelQwenVoiceEnrollment), Input: input}
	return c.call(body, nil)
}

// call 通用 POST 调用
func (c *Client) call(body requestBody, out any) error {
	if c == nil {
		return fmt.Errorf("voicex: Client 未初始化")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("voicex: 序列化请求失败: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("voicex: 构造请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("voicex: 请求 DashScope 失败: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := readLimited(resp.Body, 10<<20)
	if err != nil {
		return fmt.Errorf("voicex: 读取响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("voicex: DashScope 返回非 2xx: status=%d body=%s", resp.StatusCode, string(respBody))
	}
	var rb responseBody
	if err := json.Unmarshal(respBody, &rb); err != nil {
		return fmt.Errorf("voicex: 解析响应失败: %w", err)
	}
	if rb.Code != "" {
		return fmt.Errorf("voicex: DashScope 业务错误: code=%s message=%s", rb.Code, rb.Message)
	}
	if len(rb.Output) == 0 {
		return fmt.Errorf("voicex: DashScope 返回 output 为空")
	}
	if out != nil {
		if err := json.Unmarshal(rb.Output, out); err != nil {
			return fmt.Errorf("voicex: 解析 output 失败: %w", err)
		}
	}
	return nil
}
