package voicex

// Edge TTS 客户端 —— 微软 Edge 朗读(免费,无需注册)。
//
// ⚠️ **协议本身只有 WebSocket 一条通道**(speech.platform.bing.com /
// api.msedgeservices.com 的 cognitiveservices/websocket/v1),这里用 WS
// 是**对接上游的实现细节**;对外暴露的仍是 Once / OnceStream 两种形态,
// 与百炼 Synthesizer 完全同构 —— 前端拿到的依旧是 HTTP 音频流,
// 不感知任何 WS。
//
// 移植自 hertz_server/service/tts/edge(主备双握手 + Sec-MS-GEC 本地计算 +
// 两层超时防 hang),去掉了 zap 全局日志与远程 sec 服务,改成本地纯计算。

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// edgeTrustedClientToken 微软公共 TrustedClientToken(Edge 浏览器朗读功能内置,非私密)
	edgeTrustedClientToken = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"
	// edgePrimaryWssURL 主握手:Azure Cognitive Services 协议(需要 Token 配置)。
	// Bing 端点时不时被风控/不可达,主握手优先走这里,失败再兜底 Bing。
	edgePrimaryWssURL = "wss://api.msedgeservices.com/tts/cognitiveservices/websocket/v1"
	// edgeFallbackWssURL 备用握手:Bing speech.platform.bing.com + TrustedClientToken
	edgeFallbackWssURL = "wss://speech.platform.bing.com/consumer/speech/synthesize/readaloud/edge/v1"
	// edgeEdgeVersion Sec-MS-GEC-Version 里的 Edge 版本号(风控指纹的一部分)
	edgeEdgeVersion = "143.0.3650.75"
	// Edge 输出固定 mp3(24kHz 48kbit mono)—— 协议只回这一种
	edgeContentType = "audio/mpeg"
	// edgeMaxAudio 单次合成音频上限(与 Once 的 50MB 对齐)
	edgeMaxAudio = 50 << 20
)

// EdgeConfig Edge TTS 客户端配置。**全部可空** —— Token 不配就走
// Bing 公共 Token 的备用握手,零凭据可用;超时不配用默认值。
type EdgeConfig struct {
	// Token 主握手(api.msedgeservices.com)的 Ocp-Apim-Subscription-Key。
	// 空 = 跳过主握手,直接走 Bing 备用。
	Token string
	// WssURL 备用握手端点(空 = 内置 Bing 默认值)。自建代理时指过来。
	WssURL string
	// ReadTimeoutSeconds 单次 read 超时(Edge 端卡死检测),默认 30s
	ReadTimeoutSeconds int
	// TotalTimeoutSeconds 整次任务兜底(到点强拆连接),默认 180s
	TotalTimeoutSeconds int
}

// EdgeClient Edge TTS 客户端。并发安全(每次合成独立连接)。
type EdgeClient struct {
	cfg EdgeConfig

	// Sec-MS-GEC 风控基于时间戳,微软服务器时钟漂移会导致 403 ——
	// 拿响应 Date 头校出的偏差记在这里,下次计算时校正。
	mu       sync.Mutex
	clockSkew int64
}

// NewEdgeClient 构造。与 Synthesizer / Client 不同,**没有必填项** ——
// Edge 是免凭据服务,空配置也是合法配置。
func NewEdgeClient(cfg EdgeConfig) *EdgeClient {
	if cfg.ReadTimeoutSeconds <= 0 {
		cfg.ReadTimeoutSeconds = 30
	}
	if cfg.TotalTimeoutSeconds <= 0 {
		cfg.TotalTimeoutSeconds = 180
	}
	return &EdgeClient{cfg: cfg}
}

// EdgeSynthParams Edge 合成参数。
type EdgeSynthParams struct {
	Text   string // 待合成文本
	Voice  string // 形如 zh-CN-XiaoxiaoNeural(空 = 默认晓晓)
	Volume string // 百分比字符串 "+0%"
	Rate   string // 百分比字符串 "+0%"
	Pitch  string // 音高 "+0Hz"
}

// ---- Sec-MS-GEC 本地计算 ----
// 算法:Windows 文件时间(100ns)按 5 分钟取整,拼上 TrustedClientToken
// 做 SHA256,取大写十六进制。微软服务端用同样的算法校验,时间窗 ±5min。

const edgeWinEpoch int64 = 11644473600 // 1601-01-01 → 1970-01-01 的秒差

// edgeSecMSGEC 纯函数版(便于测试)。unixSec 为当前 unix 秒。
func edgeSecMSGEC(unixSec int64) string {
	ticks := (unixSec + edgeWinEpoch) * 10000000
	ticks -= ticks % 3000000000 // 5 分钟窗口(300s * 10^7 ticks)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%v%s", ticks, edgeTrustedClientToken)))
	return strings.ToUpper(fmt.Sprintf("%x", sum))
}

func (c *EdgeClient) secMSGEC() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	unix := time.Now().Unix() + c.clockSkew
	return edgeSecMSGEC(unix), "1-" + edgeEdgeVersion
}

// adjClockSkew 403 时用响应 Date 头校时(源项目同款自愈)
func (c *EdgeClient) adjClockSkew(resp *http.Response) {
	if resp == nil {
		return
	}
	if d := resp.Header.Get("Date"); d != "" {
		if t, err := http.ParseTime(d); err == nil {
			c.mu.Lock()
			c.clockSkew = t.Unix() - time.Now().Unix()
			c.mu.Unlock()
		}
	}
}

// ---- 参数归一 ----

var (
	edgeVoiceRe = regexp.MustCompile(`^([a-z]{2,})-([A-Z]{2,})-(.+Neural)$`)
	edgePctRe   = regexp.MustCompile(`^[+-]\d+%$`)
	edgePitchRe = regexp.MustCompile(`^[+-]\d+Hz$`)
)

// NormalizeEdgeVoice 短名 → 协议全名:
//   zh-CN-XiaoxiaoNeural → "Microsoft Server Speech Text to Speech Voice (zh-CN, XiaoxiaoNeural)"
// 已是全名 / 不认识的形态原样返回(校验交给服务端)。
func NormalizeEdgeVoice(voice string) string {
	m := edgeVoiceRe.FindStringSubmatch(strings.TrimSpace(voice))
	if m == nil {
		return voice
	}
	lang, region, name := m[1], m[2], m[3]
	if i := strings.Index(name, "-"); i != -1 { // zh-CN-liaoning-XiaobeiNeural 带地区变体
		region += "-" + name[:i]
		name = name[i+1:]
	}
	return fmt.Sprintf("Microsoft Server Speech Text to Speech Voice (%s-%s, %s)", lang, region, name)
}

// DefaultEdgeVoice 默认音色:晓晓(女,普通话)
const DefaultEdgeVoice = "Microsoft Server Speech Text to Speech Voice (zh-CN, XiaoxiaoNeural)"

func (p *EdgeSynthParams) fill() error {
	p.Text = strings.TrimSpace(p.Text)
	if p.Text == "" {
		return fmt.Errorf("voicex: text 不能为空")
	}
	if strings.TrimSpace(p.Voice) == "" {
		p.Voice = DefaultEdgeVoice
	} else {
		p.Voice = NormalizeEdgeVoice(p.Voice)
	}
	if p.Volume != "" && !edgePctRe.MatchString(p.Volume) {
		return fmt.Errorf("voicex: volume 非法(应为 +0%% / -20%% 形态): %s", p.Volume)
	}
	if p.Rate != "" && !edgePctRe.MatchString(p.Rate) {
		return fmt.Errorf("voicex: rate 非法(应为 +0%% / +10%% 形态): %s", p.Rate)
	}
	if p.Pitch != "" && !edgePitchRe.MatchString(p.Pitch) {
		return fmt.Errorf("voicex: pitch 非法(应为 +0Hz / -20Hz 形态): %s", p.Pitch)
	}
	return nil
}

// ---- 协议组装 ----

func edgeUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func edgeDate() string {
	return time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT-0700 (Coordinated Universal Time)")
}

func edgeSSML(text, voice, rate, volume, pitch string) string {
	return fmt.Sprintf("<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='zh-CN'><voice name='%s'><prosody pitch='%s' rate='%s' volume='%s'>%s</prosody></voice></speak>",
		voice, pitch, rate, volume, text)
}

func edgeSSMLFrame(requestID, timestamp, ssml string) string {
	return fmt.Sprintf("X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%sZ\r\nPath:ssml\r\n\r\n%s",
		requestID, timestamp, ssml)
}

// removeIncompatible 控制字符清洗(SSML 会被它们打断)
func removeIncompatible(text string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 0 && r <= 8) || (r >= 11 && r <= 12) || (r >= 14 && r <= 31) {
			return ' '
		}
		return r
	}, text)
}

// ---- 握手 ----

func (c *EdgeClient) dial() (*websocket.Conn, *http.Response, error) {
	gec, gecVer := c.secMSGEC()
	tail := fmt.Sprintf("&ConnectionId=%s&Sec-MS-GEC=%s&Sec-MS-GEC-Version=%s", edgeUUID(), gec, gecVer)

	headers := http.Header{}
	headers.Add("Pragma", "no-cache")
	headers.Add("Cache-Control", "no-cache")
	headers.Add("Origin", "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold")
	headers.Add("Accept-Encoding", "gzip, deflate, br")
	headers.Add("Accept-Language", "en-US,en;q=0.9")
	headers.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/"+edgeEdgeVersion)

	dialer := &websocket.Dialer{HandshakeTimeout: 15 * time.Second}

	fallback := c.cfg.WssURL
	if fallback == "" {
		fallback = edgeFallbackWssURL
	}
	// 主握手:有 Token 才走(api.msedgeservices.com 要订阅密钥)
	if c.cfg.Token != "" {
		url := fmt.Sprintf("%s?Ocp-Apim-Subscription-Key=%s%s", edgePrimaryWssURL, c.cfg.Token, tail)
		conn, resp, err := dialer.Dial(url, headers)
		if err == nil {
			return conn, resp, nil
		}
		c.adjClockSkew(resp)
	}
	// 备用握手:Bing + 公共 TrustedClientToken(只试一次,不再递归兜底)
	url := fmt.Sprintf("%s?TrustedClientToken=%s%s", fallback, edgeTrustedClientToken, tail)
	conn, resp, err := dialer.Dial(url, headers)
	if err != nil {
		c.adjClockSkew(resp)
		return nil, resp, fmt.Errorf("voicex: edge tts 握手失败: %w", err)
	}
	return conn, resp, nil
}

// ---- 合成 ----

// OnceStream 流式合成:WS 读循环把音频块写进 io.Pipe,
// 调用方拿到的仍是 SynthStream(Body/ContentType),与百炼同构。
//
// 错误语义:
//   - 握手失败 / 参数非法 → 返回 error,尚无字节产出;
//   - 读循环中途断 → CloseWithError,从 Body 读的时候才炸(与 chunked 上游断流同形态)。
func (c *EdgeClient) OnceStream(p EdgeSynthParams) (*SynthStream, error) {
	if err := p.fill(); err != nil {
		return nil, err
	}
	conn, resp, err := c.dial()
	if err != nil {
		return nil, err
	}
	_ = resp

	text := removeIncompatible(p.Text)
	ts := edgeDate()
	// speech.config:声明输出格式(Edge 只回这一种 mp3)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(
		"X-Timestamp:%s\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n{\"context\":{\"synthesis\":{\"audio\":{\"metadataoptions\":{\"sentenceBoundaryEnabled\":false,\"wordBoundaryEnabled\":true},\"outputFormat\":\"audio-24khz-48kbitrate-mono-mp3\"}}}}\r\n", ts)))
	_ = conn.WriteMessage(websocket.TextMessage, []byte(edgeSSMLFrame(edgeUUID(), ts, edgeSSML(text, p.Voice, p.Rate, p.Volume, p.Pitch))))

	pr, pw := io.Pipe()
	// 总兜底:到点强拆连接,ReadMessage 返回错误收场(防 Edge 端卡死拖死调用方)
	timer := time.AfterFunc(time.Duration(c.cfg.TotalTimeoutSeconds)*time.Second, func() { _ = conn.Close() })

	go func() {
		// 关闭顺序:先收 timer,再关 pipe,最后关连接 ——
		// pipe 关闭(带/不带错误)就是"流结束"的唯一信号。
		defer timer.Stop()
		defer pw.Close()
		defer conn.Close()

		downloadAudio := false
		readTimeout := time.Duration(c.cfg.ReadTimeoutSeconds) * time.Second
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				// 超时 = Edge 端卡死;其它 = 连接断。都按"流异常终止"处理,
				// 已产出的字节照常可读,后续读会拿到这个错误。
				pw.CloseWithError(fmt.Errorf("voicex: edge tts 读流终止: %w", err))
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
			switch mt {
			case websocket.TextMessage:
				headers, payload, err := edgeHeadersAndData(data)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				switch headers["Path"] {
				case "turn.start":
					downloadAudio = true
				case "turn.end":
					return // 正常收尾 → pw.Close()(defer)
				case "audio.metadata":
					// 字边界元数据,音频转发用不到,跳过
					_ = payload
				}
			case websocket.BinaryMessage:
				if !downloadAudio || len(data) < 2 {
					continue
				}
				hl := int(binary.BigEndian.Uint16(data[:2]))
				if len(data) < hl+2 {
					continue
				}
				if _, err := pw.Write(data[hl+2:]); err != nil {
					return // 调用方不读了,收摊
				}
			}
		}
	}()

	return &SynthStream{Body: pr, ContentType: edgeContentType}, nil
}

// Once 整段合成,返回完整 mp3 字节(落 OSS / 下载用)。
// ⚠️ 空 audio 必须当错误 —— 直接成功回 0 字节,播放器只会报"文件损坏"。
func (c *EdgeClient) Once(p EdgeSynthParams) ([]byte, error) {
	stream, err := c.OnceStream(p)
	if err != nil {
		return nil, err
	}
	defer stream.Body.Close()
	audio, err := readLimited(stream.Body, edgeMaxAudio)
	if err != nil {
		return nil, fmt.Errorf("voicex: 读取 edge 合成结果失败: %w", err)
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("voicex: edge tts 未返回音频(Sec-MS-GEC 风控 / Token 过期 / 出口 IP 被拉黑)")
	}
	return audio, nil
}

// edgeHeadersAndData 拆 WS 文本帧:头区(\r\n\r\n 前)与载荷
func edgeHeadersAndData(data []byte) (map[string]string, []byte, error) {
	i := strings.Index(string(data), "\r\n\r\n")
	if i < 0 {
		return nil, nil, fmt.Errorf("voicex: edge tts 文本帧缺头分隔")
	}
	headers := make(map[string]string)
	for _, line := range strings.Split(string(data[:i]), "\r\n") {
		kv := strings.SplitN(line, ":", 2)
		if len(kv) == 2 {
			headers[kv[0]] = strings.TrimSpace(kv[1])
		}
	}
	return headers, data[i+4:], nil
}
