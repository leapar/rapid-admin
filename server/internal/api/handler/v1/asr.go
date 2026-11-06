// 语音识别 handler(asrx 的接口面)
//
// 两条通道对应 asrx 的两个入口:
//
//   - **一句话识别** POST /biz/asr/recognize:multipart 上传短音频 → 同步回
//     识别文本。长录音不该走这里(一句话识别的语义边界)。
//   - **实时识别** GET /biz/asr/stream:WebSocket 代理 —— 浏览器 WS ←→
//     百炼 WS 双向转发。**凭据不出服务端**:浏览器永远见不到 api_key,
//     协议也被收窄成"发音频 / 收结果",run-task 的参数在握手 query 里定死。
package v1

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/hertz-contrib/websocket"
	"go.uber.org/zap"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/pkg/asrx"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/service"
)

// AsrHandler 语音识别
type AsrHandler struct {
	*handler.BaseHandler
	// corsOrigins WS 握手的 Origin 白名单(与 CORS 中间件同一份配置 ——
	// WebSocket 没过 CORS 中间件,hertz-contrib 升级器的默认 CheckOrigin
	// 只认"Origin host == 请求 Host",vite 代理 / 前后端分域部署全部 403)
	corsOrigins []string
	// upgrader WS 升级器,CheckOrigin 与 CORS 白名单同一套口径
	upgrader websocket.HertzUpgrader
}

func NewAsrHandler(r *service.Registry, corsOrigins []string) *AsrHandler {
	return &AsrHandler{
		BaseHandler: handler.New(r),
		corsOrigins: corsOrigins,
		// CheckOrigin 与 CORS 白名单同一套口径。⚠️ **不能省**:hertz-contrib
		// 的默认 CheckOrigin 只认"Origin host == 请求 Host",vite 代理
		// (changeOrigin 改写 Host 不改写 Origin)和前后端分域部署全部握手 403
		// —— 且错误只落到"连接失败",用户明明有权限,排障方向全错。
		upgrader: websocket.HertzUpgrader{
			CheckOrigin: func(c *app.RequestContext) bool {
				origin := string(c.GetHeader("Origin"))
				if origin == "" {
					return true // 非浏览器客户端(无 Origin 头)
				}
				if u, err := url.Parse(origin); err == nil && u.Host == string(c.Host()) {
					return true // 同源
				}
				for _, a := range corsOrigins {
					if a == "*" || a == origin {
						return true
					}
				}
				return false
			},
		},
	}
}

// RecognizeOnce POST /rapid-admin/v1/biz/asr/recognize (multipart/form-data)
//   - audio: 必填,短音频(pcm/wav/mp3/aac/amr/opus/speex,语义上 <60s)
//   - format: 选填,默认按扩展名猜,猜不出回 pcm
//   - sampleRate: 选填,默认 16000
//   - languageHints: 选填,逗号分隔,如 "zh,en"
func (h *AsrHandler) RecognizeOnce(ctx context.Context, c *app.RequestContext) {
	if h.RequireCaller(c) == nil {
		return
	}
	fileHeader, err := c.FormFile("audio")
	if err != nil {
		h.Fail(c, errcode.New(errcode.BadRequest, "请上传音频文件"))
		return
	}
	p := asrx.RecognizeParams{
		Format:     asrAudioFormat(string(c.FormValue("format")), fileHeader.Filename),
		SampleRate: asrFormUint(c, "sampleRate", 16000),
	}
	if lh := string(c.FormValue("languageHints")); lh != "" {
		p.LanguageHints = strings.Split(lh, ",")
	}
	f, err := fileHeader.Open()
	if err != nil {
		h.Fail(c, errcode.New(errcode.BadRequest, "读取音频失败").WithDetail(err.Error()))
		return
	}
	defer f.Close()

	text, err := h.Services.Asr.RecognizeOnce(ctx, p, f)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, map[string]string{"text": text})
}

// asrAudioFormat 格式归一:显式指定优先;否则按扩展名猜(大小写都认,
// Windows 导出的 Recording.WAV 很常见);猜不出 = pcm
func asrAudioFormat(explicit, filename string) string {
	if v := strings.ToLower(strings.TrimSpace(explicit)); v != "" {
		return v
	}
	filename = strings.ToLower(filename)
	switch {
	case strings.HasSuffix(filename, ".wav"):
		return "wav"
	case strings.HasSuffix(filename, ".mp3"):
		return "mp3"
	case strings.HasSuffix(filename, ".aac"):
		return "aac"
	case strings.HasSuffix(filename, ".amr"):
		return "amr"
	case strings.HasSuffix(filename, ".opus"):
		return "opus"
	case strings.HasSuffix(filename, ".speex"):
		return "speex"
	default:
		return "pcm"
	}
}

// asrFormUint multipart 表单字段取整数(multipart POST 里 c.Query 永远是空,
// 走 MustUint 会把客户端传的 sampleRate 静默吞成默认值)
func asrFormUint(c *app.RequestContext, key string, def uint64) int {
	v := strings.TrimSpace(string(c.FormValue(key)))
	if v == "" {
		return int(def)
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return int(def)
	}
	return int(n)
}

// ---- 实时识别(WS 代理)----

// asrStreamMsg 浏览器 → 服务端的一帧(音频是 Binary 帧,不走这里)
type asrStreamMsg struct {
	// Action: "finish"(音频发完,等服务端收尾)。将来加控制指令走这里。
	Action string `json:"action"`
}

// asrSentenceMsg 服务端 → 浏览器的一帧
type asrSentenceMsg struct {
	Event     string `json:"event"` // "sentence" / "finished" / "failed"
	Text      string `json:"text,omitempty"`
	BeginTime int64  `json:"begin_time,omitempty"`
	EndTime   int64  `json:"end_time,omitempty"`
	End       bool   `json:"end,omitempty"`
	// Failed 时的原因(凭据没配 / 上游 task-failed / 连接中断)
	Message string `json:"message,omitempty"`
}

// Stream GET /rapid-admin/v1/biz/asr/stream?token=...&format=pcm&sample_rate=16000&language_hints=zh,en
//
// 浏览器侧协议(收窄后的):
//
//	浏览器 → 服务端:Binary 帧 = 音频块;Text 帧 {"action":"finish"} = 说完了
//	服务端 → 浏览器:{"event":"sentence", text, begin_time, end_time, end} × N
//	              → {"event":"finished"} 或 {"event":"failed","message":"..."}
//
// ⚠️ **握手参数在 query 里定死**(format / sample_rate / language_hints),
// 会话中途不可换 —— 换参数 = 重开一次 WS。
//
// ⚠️ **装配期错误在升级前报**:凭据没配 / 参数非法走统一响应体(HTTP 状态 +
// 错误码),升级一旦完成就只能走 failed 帧了。
func (h *AsrHandler) Stream(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	// ⚠️ **权限闸在 handler 内**(豁免清单登记的替代鉴权)—— 浏览器 WebSocket
	// 握手带不了 Authorization 头,普通 Auth() 会先 401(同 /events 的教训),
	// 路由层只有 SSEAuth;rbac() 不挂(长连接不该占鉴权链),这里用与
	// dynGuard / SSE topic 判定同一套 decideAccess 判 asr:stream 码。
	// 识别是花钱的外呼,与克隆同档,码只绑超管线。
	principal, _ := authz.PrincipalFromContext(ctx)
	if !middleware.CanAccess(caller, principal, "asr", "stream", 0) {
		h.FailCode(c, errcode.Forbidden, "没有实时语音识别权限(asr:stream)")
		return
	}
	p := asrx.RecognizeParams{
		Format:     asrAudioFormat(c.Query("format"), ""),
		SampleRate: int(h.MustUint(c, "sample_rate", 16000)),
	}
	if lh := c.Query("language_hints"); lh != "" {
		p.LanguageHints = strings.Split(lh, ",")
	}
	// 先开会话再升级:百炼侧连不上(run-task 被拒 / 凭据没配)在这里显式报,
	// 不让浏览器拿到一个"升级成功但立刻 failed"的半截会话。
	sess, err := h.Services.Asr.Stream(ctx, p)
	if err != nil {
		h.Fail(c, err)
		return
	}

	// method / path 在升级前抓下来 —— 升级回调里 RequestContext 已被劫持,
	// 不保证还能读;系统日志条目要用。
	method, path := string(c.Method()), string(c.Path())
	if err := h.upgrader.Upgrade(c, func(conn *websocket.Conn) {
		runAsrRelay(ctx, conn, sess, method, path)
	}); err != nil {
		sess.Close()
	}
}

// runAsrRelay 双向转发:浏览器 WS ←→ 百炼识别会话。
//
// ⚠️ **读侧 / 写侧各一个 goroutine,退出条件独立**:
//   - 读侧(浏览器→上游):音频 Binary 帧 → sess.Send;{"action":"finish"} → sess.Finish
//   - 写侧(上游→浏览器):sess.Results() → sentence 帧;sess.Done() → finished/failed 帧
//
// 任一侧退出,defer sess.Close() 兜底断上游 —— 写侧 goroutine 随之收敛,
// 不会留下悬空的转发循环。
func runAsrRelay(ctx context.Context, conn *websocket.Conn, sess *asrx.Session, method, path string) {
	defer sess.Close()

	// 写侧 goroutine:上游结果 → 浏览器
	go func() {
		for {
			select {
			case sen, ok := <-sess.Results():
				if !ok {
					return
				}
				b, _ := json.Marshal(asrSentenceMsg{
					Event:     "sentence",
					Text:      sen.Text,
					BeginTime: sen.BeginTime,
					EndTime:   sen.EndTime,
					End:       sen.End,
				})
				if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
					return
				}
			case <-sess.Done():
				msg := asrSentenceMsg{Event: "finished"}
				if err := sess.Err(); err != nil {
					// ⚠️ WS 通道不走 HTTP 状态码,access_log 的 5xx 钩子抓不到
					// 升级后的会话失败 —— 上游 task-failed / 连接中断的原因
					// 只在这里落日志,不然排障时服务端零痕迹
					logger.L().Warn("asr: 实时识别会话失败", zap.Error(err))
					middleware.RecordBizSystemLog(ctx, method, path, "asr_error",
						"实时识别会话失败: "+err.Error(), "")
					msg = asrSentenceMsg{Event: "failed", Message: err.Error()}
				}
				b, _ := json.Marshal(msg)
				_ = conn.WriteMessage(websocket.TextMessage, b)
				// ⚠️ **终态帧之后客户端没有合法指令了** —— 服务端主动关连接:
				// 读侧 ReadMessage 随之报错退出,不留悬空的转发循环
				// (不发 close 握手的残废客户端也兜住)。
				_ = conn.Close()
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// 读侧(当前 goroutine):浏览器 → 上游
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return // 浏览器断开 —— defer sess.Close() 收尾
		}
		switch mt {
		case websocket.BinaryMessage:
			if err := sess.Send(data); err != nil {
				return
			}
		case websocket.TextMessage:
			var m asrStreamMsg
			if json.Unmarshal(data, &m) == nil && m.Action == "finish" {
				if err := sess.Finish(); err != nil {
					return
				}
			}
			// 其它指令不认识就忽略 —— 收窄的协议里没有别的合法指令
		}
	}
}
