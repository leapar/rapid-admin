// Package asrx —— 阿里云百炼(DashScope)实时语音识别(Paraformer)WebSocket 客户端
//
// 同一协议两个入口:
//
//   - **Stream**(实时识别):建连 + run-task,拿回一个会话 ——
//     调用方持续 Send 音频块、从 Results() 收逐句结果(中间结果 + 终稿),
//     说完 Finish。这是"边说边出字"的底座,WS 代理(handler 层)直接架在它上面。
//   - **Once**(一句话识别):整段音频流进去,聚合全部终稿句子回纯文本 ——
//     同步形态,短音频(<60s)一发一收。
//
// # 协议骨架(对齐官方 WebSocket 接入指南)
//
//	客户端 → 服务端:Text 帧 run-task →(等 task-started)→ Binary 帧音频 × N
//	              → Text 帧 finish-task
//	服务端 → 客户端:task-started / result-generated(sentence.end=false 中间
//	              结果、true 终稿;heartbeat=true 心跳帧直接滤掉)/
//	              task-finished / task-failed
//
// # 关键纪律
//
//   - **凭据缺一不可**:api_key 为空 New() 显式报错(与 voicex 同一套)——
//     半可用的客户端会把"没配"拖到第一次调用才炸;
//   - **必须在 task-started 之后才能发音频**:协议硬约束,提前发会被服务端
//     直接断连且没有可读的错误;
//   - **心跳帧不是结果**:sentence.heartbeat=true 的帧 text 可能为空,
//     透传给上层只会制造"识别结果闪空"的鬼影,包内直接滤掉;
//   - **两层超时防 hang**:握手超时 + 整次任务兜底强拆(读挂起 / 服务端
//     不回 task-finished 时,Once 不会永远卡着)。
package asrx

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// DefaultURL DashScope WebSocket 推理端点(Paraformer 仅华北2 北京)
	DefaultURL = "wss://dashscope.aliyuncs.com/api-ws/v1/inference"
	// DefaultModel 默认实时识别模型
	DefaultModel = "paraformer-realtime-v2"
	// DefaultTimeoutSeconds 整次任务兜底超时(秒)
	DefaultTimeoutSeconds = 300
	// ChunkSize 音频分块大小(字节)。16kHz 16bit mono 下 3200B = 100ms,
	// 与官方 SDK 的发送粒度同量级 —— 更大省系统调用,更小降首包延迟。
	ChunkSize = 3200
)

// Config 客户端配置
type Config struct {
	// APIKey 百炼 API Key(必填,空 = New 显式报错)
	APIKey string
	// WorkspaceID 业务空间 ID(选填,填了带 X-DashScope-WorkSpace 头)
	WorkspaceID string
	// URL 推理端点(空 = DefaultURL;业务空间专属域名指过来)
	URL string
	// Model 识别模型(空 = DefaultModel)
	Model string
	// TimeoutSeconds 整次任务兜底超时秒数(空/0 = DefaultTimeoutSeconds)
	TimeoutSeconds int
}

// Client 识别客户端(凭据只在这里,绝不落到调用方)
type Client struct {
	dialURL   string
	apiKey    string
	workspace string
	model     string
	timeout   time.Duration
	handshake time.Duration // 握手超时,固定 15s
}

// New 构造。api_key 为空显式报错 —— 克隆侧同款纪律。
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("api_key 未配置:实时语音识别需要百炼 API Key")
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = DefaultTimeoutSeconds
	}
	url := strings.TrimSpace(cfg.URL)
	if url == "" {
		url = DefaultURL
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		dialURL:   url,
		apiKey:    cfg.APIKey,
		workspace: strings.TrimSpace(cfg.WorkspaceID),
		model:     model,
		timeout:   time.Duration(timeout) * time.Second,
		handshake: 15 * time.Second,
	}, nil
}

// RecognizeParams 一次识别任务的音频描述
type RecognizeParams struct {
	// Format 音频格式:pcm / wav / mp3 / opus / speex / aac / amr。
	// ⚠️ opus/speex 必须 Ogg 封装;wav 必须是 PCM 编码;amr 仅 AMR-NB。空 = pcm
	Format string
	// SampleRate 采样率,空 = 16000
	SampleRate int
	// DisfluencyRemoval 去除口语赘词(啊/嗯),默认 false
	DisfluencyRemoval bool
	// LanguageHints 语言提示,如 ["zh","en"](仅 paraformer-realtime-v2 支持)
	LanguageHints []string
}

func (p RecognizeParams) format() string {
	if f := strings.TrimSpace(p.Format); f != "" {
		return strings.ToLower(f)
	}
	return "pcm"
}

func (p RecognizeParams) sampleRate() int {
	if p.SampleRate > 0 {
		return p.SampleRate
	}
	return 16000
}

// Sentence 一句识别结果
type Sentence struct {
	Text      string `json:"text"`
	BeginTime int64  `json:"begin_time"` // ms
	EndTime   int64  `json:"end_time"`   // ms
	End       bool   `json:"end"`        // sentence_end:true = 终稿,false = 中间结果
}

// ---- 协议帧 ----

type taskHeader struct {
	Action       string `json:"action,omitempty"`
	TaskID       string `json:"task_id"`
	Streaming    string `json:"streaming"`
	Event        string `json:"event,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type runTaskPayload struct {
	TaskGroup  string           `json:"task_group"`
	Task       string           `json:"task"`
	Function   string           `json:"function"`
	Model      string           `json:"model"`
	Parameters recognizeParams2 `json:"parameters"`
	Input      struct{}         `json:"input"`
}

// recognizeParams2 协议字段的 wire 形态(与 RecognizeParams 分开,
// 免得把 Go 零值序列化成 false/0 之外的东西——这里显式逐字段给)
type recognizeParams2 struct {
	Format                   string   `json:"format"`
	SampleRate               int      `json:"sample_rate"`
	DisfluencyRemovalEnabled bool     `json:"disfluency_removal_enabled"`
	LanguageHints            []string `json:"language_hints,omitempty"`
}

type taskFrame struct {
	Header  taskHeader     `json:"header"`
	Payload runTaskPayload `json:"payload"`
}

type finishTaskFrame struct {
	Header  taskHeader `json:"header"`
	Payload struct {
		Input struct{} `json:"input"`
	} `json:"payload"`
}

// serverFrame 服务端事件的解析形态(只取用得到的字段)
type serverFrame struct {
	Header struct {
		TaskID       string `json:"task_id"`
		Event        string `json:"event"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"header"`
	Payload struct {
		Output struct {
			Sentence struct {
				Text        string `json:"text"`
				BeginTime   int64  `json:"begin_time"`
				EndTime     int64  `json:"end_time"`
				Heartbeat   bool   `json:"heartbeat"`
				SentenceEnd bool   `json:"sentence_end"`
			} `json:"sentence"`
		} `json:"output"`
	} `json:"payload"`
}

// Session 一次实时识别会话(Stream 成功返回后处于"可发音频"状态:
// run-task 已发、task-started 已等到)
type Session struct {
	conn    *websocket.Conn
	taskID  string
	timeout time.Duration

	results   chan Sentence
	done      chan struct{} // 关闭 = 终态已到(task-finished / task-failed)
	closeOnce sync.Once

	mu  sync.Mutex // 串行化写帧
	err error      // task-failed 的错误(读挂在 Done 之后再取)
}

// Send 发一块音频(Binary 帧)。必须在 Stream 返回之后调用
// (那时 task-started 已经等到)。
func (s *Session) Send(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.WriteMessage(websocket.BinaryMessage, chunk)
}

// Results 识别结果流(心跳帧已滤)。终态后通道关闭。
func (s *Session) Results() <-chan Sentence { return s.results }

// Done 终态信号:task-finished / task-failed / 连接异常都会关闭它。
func (s *Session) Done() <-chan struct{} { return s.done }

// Err 终态为 task-failed 或连接异常时的错误(nil = 正常结束)。
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Finish 发 finish-task(音频发完了,等服务端把尾巴吐完)。
// 不关连接 —— 关连接交给 Close 或读协程在终态后自动做。
func (s *Session) Finish() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	frame := finishTaskFrame{}
	frame.Header.Action = "finish-task"
	frame.Header.TaskID = s.taskID
	frame.Header.Streaming = "duplex"
	b, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return s.conn.WriteMessage(websocket.TextMessage, b)
}

// Close 断开底层连接(幂等)。正常流程:Finish → 等 Done → Close。
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		// 终态已到或调用方放弃 —— 直接关;读协程的读错误会落在没人收的
		// results 上,不影响正确性。
		_ = s.conn.Close()
	})
	return nil
}

// Stream 开一次实时识别会话:建连 → run-task → 等 task-started。
// 返回的会话已处于可发音频状态;任何一步失败都返回错误(连接已代为关闭)。
func (c *Client) Stream(ctx context.Context, p RecognizeParams) (*Session, error) {
	taskID := newTaskID()
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}

	frame := taskFrame{}
	frame.Header = taskHeader{Action: "run-task", TaskID: taskID, Streaming: "duplex"}
	frame.Payload = runTaskPayload{
		TaskGroup: "audio",
		Task:      "asr",
		Function:  "recognition",
		Model:     c.model,
		Parameters: recognizeParams2{
			Format:                   p.format(),
			SampleRate:               p.sampleRate(),
			DisfluencyRemovalEnabled: p.DisfluencyRemoval,
			LanguageHints:            p.LanguageHints,
		},
	}
	b, err := json.Marshal(frame)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("marshal run-task: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("send run-task: %w", err)
	}

	s := &Session{
		conn:    conn,
		taskID:  taskID,
		timeout: c.timeout,
		results: make(chan Sentence, 64),
		done:    make(chan struct{}),
	}

	// 等 task-started(带兜底超时):协议硬约束,没等到不能发音频。
	started := make(chan error, 1)
	go func() { started <- s.readLoop(true) }()
	select {
	case err := <-started:
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
	case <-time.After(c.timeout):
		_ = conn.Close()
		return nil, fmt.Errorf("等待 task-started 超时(%s)", c.timeout)
	case <-ctx.Done():
		_ = conn.Close()
		return nil, ctx.Err()
	}

	// task-started 已到:读协程继续常驻,把 result-generated 推进 results,
	// 终态关闭 done。
	go func() { _ = s.readLoop(false) }()
	return s, nil
}

// readLoop 读服务端事件。waitStarted=true 时只服务"等 task-started"这一段
// (读到即返回 nil);false 时常驻读到终态,结果推进 results。
func (s *Session) readLoop(waitStarted bool) error {
	deadline := time.Now().Add(s.timeout)
	for {
		_ = s.conn.SetReadDeadline(deadline)
		mt, data, err := s.conn.ReadMessage()
		if err != nil {
			s.fail(fmt.Errorf("识别连接中断: %w", err))
			return err
		}
		if mt != websocket.TextMessage {
			continue // 心跳 / 未知的二进制帧,不认识就跳过
		}
		var f serverFrame
		if err := json.Unmarshal(data, &f); err != nil {
			continue // 非 JSON 的文本帧,跳过
		}
		switch f.Header.Event {
		case "task-started":
			if waitStarted {
				return nil
			}
		case "result-generated":
			sen := f.Payload.Output.Sentence
			if sen.Heartbeat {
				continue // 心跳帧滤掉,不给上层制造"结果闪空"
			}
			s.results <- Sentence{
				Text:      sen.Text,
				BeginTime: sen.BeginTime,
				EndTime:   sen.EndTime,
				End:       sen.SentenceEnd,
			}
		case "task-finished":
			close(s.done)
			return nil
		case "task-failed":
			msg := f.Header.ErrorMessage
			if msg == "" {
				msg = "unknown"
			}
			s.fail(fmt.Errorf("识别任务失败(%s): %s", f.Header.ErrorCode, msg))
			return s.Err()
		}
	}
}

func (s *Session) fail(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
	s.closeOnce.Do(func() { _ = s.conn.Close() })
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// Once 一句话识别:整段音频 → 聚合后的纯文本(只拼 sentence_end=true 的句子)。
//
// 读侧把音频按 ChunkSize 切块流进去,发完 Finish,然后等终态、聚合终稿。
// ⚠️ ctx 取消 / 兜底超时都会让这里立刻返回错误,不会 hang。
func (c *Client) Once(ctx context.Context, p RecognizeParams, audio io.Reader) (string, error) {
	sess, err := c.Stream(ctx, p)
	if err != nil {
		return "", err
	}
	defer sess.Close()

	// 发侧:读完音频 → Finish。错误经 sendErr 传回(连接被对端断开等)。
	sendErr := make(chan error, 1)
	go func() {
		buf := make([]byte, ChunkSize)
		for {
			n, rerr := audio.Read(buf)
			if n > 0 {
				if werr := sess.Send(buf[:n]); werr != nil {
					sendErr <- werr
					return
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					sendErr <- sess.Finish()
				} else {
					sendErr <- rerr
				}
				return
			}
		}
	}()

	// 收侧:聚合终稿句子。
	var b strings.Builder
	for {
		select {
		case err := <-sendErr:
			if err != nil {
				return "", err
			}
			sendErr = nil // 已正常收尾,不再读这个通道
		case sen, ok := <-sess.Results():
			if !ok {
				// results 在终态后不会自动关 —— 这里等 Done 统一收口
				continue
			}
			if sen.End && sen.Text != "" {
				b.WriteString(sen.Text)
			}
		case <-sess.Done():
			if err := sess.Err(); err != nil {
				return "", err
			}
			// 正常终态后可能还有缓存在管道里的终稿,排干再走
			for {
				select {
				case sen := <-sess.Results():
					if sen.End && sen.Text != "" {
						b.WriteString(sen.Text)
					}
					continue
				default:
				}
				break
			}
			return b.String(), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
		// 音频发完后 sendErr 置 nil 通道,后续 select 只等 Results / Done / ctx
	}
}

// dial 建 WebSocket 连接(鉴权在握手头)
func (c *Client) dial(ctx context.Context) (*websocket.Conn, error) {
	h := make(map[string][]string)
	h["Authorization"] = []string{"Bearer " + c.apiKey}
	if c.workspace != "" {
		h["X-DashScope-WorkSpace"] = []string{c.workspace}
	}
	dialer := websocket.Dialer{HandshakeTimeout: c.handshake}
	conn, _, err := dialer.DialContext(ctx, c.dialURL, h)
	if err != nil {
		return nil, fmt.Errorf("连接识别端点失败(%s): %w", c.dialURL, err)
	}
	return conn, nil
}

// newTaskID 客户端生成的任务 ID(UUID v4 形态,排查问题用)
func newTaskID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
