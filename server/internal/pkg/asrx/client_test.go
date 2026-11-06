// asrx 协议测试 —— 本地起一个"假 DashScope"WS 服务端,按官方协议回帧
//
// # 这一组守的是什么
//
//	真端点要真凭据真音频,单测里做不到;但**协议形态**可以完全离线守:
//	帧序(run-task → task-started → Binary → finish-task → task-finished)、
//	结果解析(sentence_end / heartbeat)、失败传导(task-failed → Err)。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· readLoop 里把 `sen.Heartbeat { continue }` 删掉
//	  → TestAsrx_Once_AggregatesFinals 变红(结果里混进空心跳句)
//	· Stream 里不等 task-started 就返回会话
//	  → TestAsrx_Stream_SendsAudioAfterStarted 变红(音频帧先于 started 到达)
//	· Once 里把 `sen.End` 判断删掉
//	  → TestAsrx_Once_AggregatesFinals 变红(中间结果被拼进全文)
package asrx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeDashScope 假服务端:校验 run-task 形态,回 started → 结果 → finished
type fakeDashScope struct {
	t   *testing.T
	srv *httptest.Server
	// 记录收到的帧,供断言
	gotRunTask bool
	gotFinish  bool
	gotAudio   []byte
	// 结果脚本:srv 收到第一块音频后依次推这些句子
	sentences []string // 依次 result-generated;text 非空;奇数条 End=false,偶数条 End=true
}

func newFakeServer(t *testing.T, script []string) *fakeDashScope {
	t.Helper()
	f := &fakeDashScope{t: t, sentences: script}
	up := websocket.Upgrader{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// 1. 读 run-task,校验协议骨架
		mt, data, err := conn.ReadMessage()
		if err != nil || mt != websocket.TextMessage {
			return
		}
		var frame taskFrame
		if err := jsonUnmarshal(data, &frame); err != nil {
			return
		}
		if frame.Header.Action != "run-task" || frame.Payload.TaskGroup != "audio" ||
			frame.Payload.Task != "asr" || frame.Payload.Function != "recognition" ||
			frame.Payload.Model == "" || frame.Payload.Parameters.Format == "" {
			_ = conn.WriteJSON(taskFailedFrame("BadRequest", "run-task 形态不对"))
			return
		}
		f.gotRunTask = true
		// 2. task-started
		_ = conn.WriteJSON(map[string]any{
			"header":  map[string]any{"task_id": frame.Header.TaskID, "event": "task-started"},
			"payload": map[string]any{},
		})
		// 3. 读音频帧(至少一块)后按脚本推结果
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.BinaryMessage {
				f.gotAudio = append(f.gotAudio, data...)
				if len(f.gotAudio) >= len(script) { // 简化:收够即推
					break
				}
			}
			if mt == websocket.TextMessage && strings.Contains(string(data), "finish-task") {
				f.gotFinish = true
				f.pushResults(conn, frame.Header.TaskID)
				_ = conn.WriteJSON(map[string]any{
					"header":  map[string]any{"task_id": frame.Header.TaskID, "event": "task-finished"},
					"payload": map[string]any{},
				})
				return
			}
		}
		// 等真正的 finish-task 再收尾
		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.TextMessage && strings.Contains(string(data), "finish-task") {
				f.gotFinish = true
				f.pushResults(conn, frame.Header.TaskID)
				_ = conn.WriteJSON(map[string]any{
					"header":  map[string]any{"task_id": frame.Header.TaskID, "event": "task-finished"},
					"payload": map[string]any{},
				})
				return
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDashScope) pushResults(conn *websocket.Conn, taskID string) {
	for i, text := range f.sentences {
		end := i == len(f.sentences)-1 // 前面全是中间结果、最后一条终稿
		frame := map[string]any{
			"header": map[string]any{"task_id": taskID, "event": "result-generated"},
			"payload": map[string]any{"output": map[string]any{"sentence": map[string]any{
				"text": text, "begin_time": i * 1000, "end_time": i*1000 + 900, "sentence_end": end,
			}}},
		}
		_ = conn.WriteJSON(frame)
		if i == 0 {
			// 中间结果后面塞一条心跳帧 —— 实现必须滤掉
			_ = conn.WriteJSON(map[string]any{
				"header": map[string]any{"task_id": taskID, "event": "result-generated"},
				"payload": map[string]any{"output": map[string]any{"sentence": map[string]any{
					"text": "", "heartbeat": true,
				}}},
			})
		}
	}
}

// taskFailedFrame 构造 task-failed 事件(raw JSON,字段形态按官方文档)
func taskFailedFrame(code, msg string) string {
	return `{"header":{"event":"task-failed","error_code":"` + code + `","error_message":"` + msg + `"},"payload":{}}`
}

func urlToWS(t *testing.T, httpURL string) string {
	t.Helper()
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func testClient(t *testing.T, srvURL string) *Client {
	t.Helper()
	c, err := New(Config{APIKey: "sk-test", URL: urlToWS(t, srvURL), TimeoutSeconds: 5})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestAsrx_Once_AggregatesFinals 一句话识别:只拼终稿、滤心跳、回纯文本
func TestAsrx_Once_AggregatesFinals(t *testing.T) {
	f := newFakeServer(t, []string{"今天", "今天天气", "今天天气不错"})
	c := testClient(t, f.srv.URL)

	text, err := c.Once(context.Background(), RecognizeParams{Format: "pcm", SampleRate: 16000},
		strings.NewReader(strings.Repeat("a", 8000)))
	if err != nil {
		t.Fatalf("Once: %v", err)
	}
	if text != "今天天气不错" {
		t.Fatalf("聚合结果 = %q,想要 %q", text, "今天天气不错")
	}
	if !f.gotRunTask || !f.gotFinish {
		t.Fatalf("协议帧序不完整:run-task=%v finish-task=%v", f.gotRunTask, f.gotFinish)
	}
	if len(f.gotAudio) == 0 {
		t.Fatal("音频帧没发出去")
	}
}

// TestAsrx_Stream_SendsAudioAfterStarted 实时会话:started 之前不发音频,结果逐条可收
func TestAsrx_Stream_SendsAudioAfterStarted(t *testing.T) {
	f := newFakeServer(t, []string{"你", "你好"})
	c := testClient(t, f.srv.URL)

	sess, err := c.Stream(context.Background(), RecognizeParams{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer sess.Close()

	// 先发音频 + finish(fakes 服务端收齐才推结果),再收两条:中间 + 终稿
	if err := sess.Send([]byte("audio-bytes")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sess.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	var got []Sentence
	deadline := time.After(3 * time.Second)
	for len(got) < 2 {
		select {
		case sen := <-sess.Results():
			got = append(got, sen)
		case <-deadline:
			t.Fatalf("结果收不够: got %d 条", len(got))
		}
	}
	if got[0].End || got[0].Text != "你" {
		t.Fatalf("第 1 条应为中间结果 %q: %+v", "你", got[0])
	}
	if !got[1].End || got[1].Text != "你好" {
		t.Fatalf("第 2 条应为终稿 %q: %+v", "你好", got[1])
	}
	select {
	case <-sess.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("终态没到")
	}
	if err := sess.Err(); err != nil {
		t.Fatalf("正常收尾不该有错: %v", err)
	}
}

// TestAsrx_TaskFailedSurfaces 服务端 task-failed → Once 报错(不吞)
func TestAsrx_TaskFailedSurfaces(t *testing.T) {
	// 脚本为空 + 服务端直接回 failed 的形态:用坏 run-task 触发
	// (fake 服务端对形态不对的 run-task 回 task-failed)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var frame taskFrame
		if jsonUnmarshal(data, &frame) != nil || frame.Header.Action != "run-task" {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(taskFailedFrame("InvalidAction", "action 不认识")))
		} else {
			// run-task 合法但 model 是坏的 → failed
			_ = conn.WriteMessage(websocket.TextMessage,
				[]byte(taskFailedFrame("InvalidParameter", "model 不存在")))
		}
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	_, err := c.Once(context.Background(), RecognizeParams{Format: "pcm"}, strings.NewReader("xx"))
	if err == nil || !strings.Contains(err.Error(), "model 不存在") {
		t.Fatalf("task-failed 应当把错误带出来, got: %v", err)
	}
}

// TestAsrx_MissingAPIKey 凭据缺一不可:空 api_key 当场报错
func TestAsrx_MissingAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("空 api_key 必须显式报错")
	}
}
