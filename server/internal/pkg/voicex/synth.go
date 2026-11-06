package voicex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// readLimited 限量读 body —— 上游给的字节数不可信,不设闸就是给
// "一个响应把内存吃光" 留口子。克隆客户端与合成客户端共用。
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}

// SynthConfig 文本合成(TTS)端点配置
type SynthConfig struct {
	// Endpoint 合成端点(必填)。请求形态:POST JSON
	//   {"text":"...","voice":"...","tts":"...","volume":"0%","rate":"0%"}
	// 成功响应 = 二进制音频流;失败响应 = JSON(带 code/msg)。
	Endpoint string
	// TimeoutSeconds 单次合成超时,默认 60s
	TimeoutSeconds int
}

// Synthesizer 文本合成客户端
//
// ⚠️ **合成后端不在本包里实现** —— DashScope 的克隆端点只管"音色注册",
// 真正的合成走部署方自己的 TTS 网关(或后续接 DashScope 合成 API)。
// 端点在 voice.synth_url 配,不配 = 不装配,调用方拿到的是显式错误。
type Synthesizer struct {
	endpoint   string
	httpClient *http.Client
}

// NewSynthesizer 构造合成客户端。endpoint 为空显式报错 ——
// 与 Client.New 同一条纪律:"没配"必须在装配期暴露,不能拖到第一次调用。
func NewSynthesizer(cfg SynthConfig) (*Synthesizer, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("voicex: synth_url 未配置(文本合成端点)")
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	return &Synthesizer{
		endpoint: cfg.Endpoint,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}, nil
}

// SynthParams 合成请求参数。Volume / Rate 是百分比字符串("0%" = 不变),
// 沿用 TTS 网关的历史形态,传空则不下发这两个字段。
type SynthParams struct {
	Text   string `json:"text"`
	Voice  string `json:"voice"`
	TTS    string `json:"tts,omitempty"`
	Volume string `json:"volume,omitempty"`
	Rate   string `json:"rate,omitempty"`
	// Format 音频格式("wav"/"mp3"),原样透传;端点不认识就忽略(omitempty)
	Format string `json:"format,omitempty"`
}

// SynthStream 流式合成结果
type SynthStream struct {
	// Body 音频字节流(由调用方 Close)
	Body io.ReadCloser
	// ContentType 端点声明的媒体类型(空 = 未知,调用方自行兜底 audio/mpeg)
	ContentType string
}

// OnceStream 流式合成 —— **不用 WebSocket**:
// 单个 HTTP POST,音频以 chunked 流返回,调用方边读边转发
// (前端 fetch + ReadableStream 边收边播,首包延迟不再等于整段合成时长)。
//
// ⚠️ **"响应是 JSON" = 失败** —— 成功回音频流;部分网关失败时回
// 200 + {"code":"...","msg":"..."}。这里按两级嗅探:
//   1. Content-Type 是 JSON ⇒ 整体读出来解析错误;
//   2. Content-Type 缺失 ⇒ 探首字节,'{' 开头当 JSON 错误读干净,
//      否则把已读字节原样续回流(一字节都不丢)。
func (s *Synthesizer) OnceStream(p SynthParams) (*SynthStream, error) {
	if p.Text == "" {
		return nil, fmt.Errorf("voicex: text 不能为空")
	}
	if p.Voice == "" {
		return nil, fmt.Errorf("voicex: voice 不能为空")
	}
	resp, err := s.post(p)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := readLimited(resp.Body, 1<<20)
		resp.Body.Close()
		return nil, fmt.Errorf("voicex: 合成端点返回非 2xx: status=%d body=%s", resp.StatusCode, string(body))
	}
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	mt := mediaType(ct)
	if isJSONMediaType(mt) {
		body, _ := readLimited(resp.Body, 1<<20)
		resp.Body.Close()
		return nil, synthJSONError(body)
	}
	// Content-Type 缺失/不可信 → 探首字节(网关失败时可能不带 JSON 的 Content-Type)
	if mt == "" {
		head := make([]byte, 1)
		n, rerr := io.ReadFull(resp.Body, head)
		if rerr != nil && !(rerr == io.EOF && n > 0) {
			resp.Body.Close()
			if rerr == io.EOF {
				return nil, fmt.Errorf("voicex: 合成端点返回空响应")
			}
			return nil, fmt.Errorf("voicex: 读取合成响应失败: %w", rerr)
		}
		if n > 0 && head[0] == '{' {
			rest, _ := readLimited(resp.Body, 1<<20)
			resp.Body.Close()
			return nil, synthJSONError(append(head[:n], rest...))
		}
		return &SynthStream{
			Body: readCloserChain(io.MultiReader(bytes.NewReader(head[:n]), resp.Body), resp.Body),
		}, nil
	}
	return &SynthStream{Body: resp.Body, ContentType: ct}, nil
}

// Once 单次合成,返回完整音频字节(用于落 OSS / 存试听文件的场景)。
// 流式版本见 OnceStream —— 那是给"边合成边播"用的。
func (s *Synthesizer) Once(p SynthParams) ([]byte, error) {
	stream, err := s.OnceStream(p)
	if err != nil {
		return nil, err
	}
	defer stream.Body.Close()
	body, err := readLimited(stream.Body, 50<<20)
	if err != nil {
		return nil, fmt.Errorf("voicex: 读取合成响应失败: %w", err)
	}
	return body, nil
}

// post 组装并发送 POST(JSON body),不读 body —— 由调用方决定怎么消费
func (s *Synthesizer) post(p SynthParams) (*http.Response, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("voicex: 序列化合成请求失败: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("voicex: 构造合成请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voicex: 请求合成端点失败: %w", err)
	}
	return resp, nil
}

// synthJSONError 把"200 + JSON"形态的失败响应解析成显式错误。
// ⚠️ 必须**先嗅探再当音频用** —— 一段 JSON 存成 .mp3,播放器只会报"文件损坏"。
func synthJSONError(body []byte) error {
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err == nil {
		if code, has := probe["code"]; has {
			msg := "合成失败"
			if m, has := probe["msg"].(string); has && m != "" {
				msg = m
			}
			return fmt.Errorf("voicex: 合成端点业务错误: code=%v msg=%s", code, msg)
		}
	}
	return fmt.Errorf("voicex: 合成端点返回了 JSON 而不是音频: %s", truncate(string(body), 200))
}

func mediaType(ct string) string {
	if i := strings.IndexByte(ct, ';'); i > 0 {
		return strings.TrimSpace(ct[:i])
	}
	return ct
}

func isJSONMediaType(mt string) bool {
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// readCloserChain MultiReader 没有 Close,包一层把底层 body 一起关掉
func readCloserChain(r io.Reader, closer io.Closer) io.ReadCloser {
	return &chainedReadCloser{r: r, closer: closer}
}

type chainedReadCloser struct {
	r      io.Reader
	closer io.Closer
}

func (c *chainedReadCloser) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *chainedReadCloser) Close() error               { return c.closer.Close() }
