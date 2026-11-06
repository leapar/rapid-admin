// 音色克隆 + TTS handler
// 克隆:multipart 上传参考音频 → DashScope 注册 → 台账(sys_voice_clone)
// TTS:音色登记册 CRUD(sys_tts_voice)+ 文本合成(流式回音频 / 落 OSS)
package v1

import (
	"context"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/service"
)

// bindIDs 解析 {"ids":["1","2"]} 形态的批量 id 入参(与 user.BatchRemove 同款)
func bindIDs(h *handler.BaseHandler, c *app.RequestContext) ([]uint64, error) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(body.IDs))
	for _, s := range body.IDs {
		if s == "" {
			continue
		}
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, errcode.New(errcode.BadRequest, "ids 格式错误: "+s)
		}
		ids = append(ids, v)
	}
	if len(ids) == 0 {
		return nil, errcode.New(errcode.BadRequest, "ids 不能为空")
	}
	return ids, nil
}

// VoiceCloneHandler 音色克隆
type VoiceCloneHandler struct{ *handler.BaseHandler }

func NewVoiceCloneHandler(r *service.Registry) *VoiceCloneHandler {
	return &VoiceCloneHandler{BaseHandler: handler.New(r)}
}

// Create POST /rapid-admin/v1/biz/voice-clones (multipart/form-data)
//   - audio: 必填,参考音频(wav/mp3/m4a/pcm/aac/flac/ogg)
//   - name: 必填,显示名称
//   - targetModel: 选填,默认 cosyvoice-v3-plus(配置 voice.default_target_model)
//   - prefix: 选填,voice_id 前缀,默认 voice
//   - languageHints: 选填,逗号分隔,如 "zh,en"
//   - gender / sex / ttsType / sort / description / recommend: 选填
func (h *VoiceCloneHandler) Create(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	fileHeader, err := c.FormFile("audio")
	if err != nil {
		h.Fail(c, errcode.New(errcode.BadRequest, "请上传参考音频"))
		return
	}
	req := service.VoiceCloneCreateRequest{
		Name:        string(c.FormValue("name")),
		Gender:      string(c.FormValue("gender")),
		TtsType:     string(c.FormValue("ttsType")),
		TargetModel: string(c.FormValue("targetModel")),
		Prefix:      string(c.FormValue("prefix")),
		Description: string(c.FormValue("description")),
	}
	if v, err := strconv.Atoi(string(c.FormValue("sex"))); err == nil {
		req.Sex = v
	}
	if v, err := strconv.Atoi(string(c.FormValue("sort"))); err == nil {
		req.Sort = v
	}
	req.Recommend = string(c.FormValue("recommend")) == "true" || string(c.FormValue("recommend")) == "1"
	if lh := string(c.FormValue("languageHints")); lh != "" {
		req.LanguageHints = strings.Split(lh, ",")
	}
	m, err := h.Services.VoiceClone.CreateFromAudio(ctx, caller, req, fileHeader)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, m)
}

// List GET /rapid-admin/v1/biz/voice-clones?keyword=&status=&tts_type=&page=&size=
func (h *VoiceCloneHandler) List(ctx context.Context, c *app.RequestContext) {
	q := service.VoiceCloneListQuery{
		Keyword: c.Query("keyword"),
		Status:  c.Query("status"),
		TtsType: c.Query("tts_type"),
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.VoiceClone.List(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// Get GET /rapid-admin/v1/biz/voice-clones/:id
func (h *VoiceCloneHandler) Get(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.VoiceClone.Get(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// Update PATCH /rapid-admin/v1/biz/voice-clones/:id —— 只改展示信息,不重新训练
func (h *VoiceCloneHandler) Update(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body model.SysVoiceClone
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	req := service.VoiceCloneUpdateRequest{
		Name:        body.Name,
		Gender:      body.Gender,
		Sex:         body.Sex,
		TtsType:     body.TtsType,
		Sort:        body.Sort,
		Description: body.Description,
		Recommend:   body.Recommend,
	}
	if err := h.Services.VoiceClone.Update(ctx, caller, id, req); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Delete DELETE /rapid-admin/v1/biz/voice-clones/:id
// 尽力删远端音色 + 参考音频,失败不阻断本地删除(service 层只打日志)。
func (h *VoiceCloneHandler) Delete(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.VoiceClone.Delete(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// BatchDelete POST /rapid-admin/v1/biz/voice-clones/batch-delete  {"ids":["1","2"]}
func (h *VoiceCloneHandler) BatchDelete(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ids, err := bindIDs(h.BaseHandler, c)
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.VoiceClone.BatchDelete(ctx, caller, ids); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// Refresh POST /rapid-admin/v1/biz/voice-clones/:id/refresh
// 拉一次 DashScope query_voice 同步状态(DEPLOYING → OK / UNDEPLOYED)。
func (h *VoiceCloneHandler) Refresh(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.VoiceClone.RefreshStatus(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// RemoteList GET /rapid-admin/v1/biz/voice-clones/remote?prefix=&page=&size=
// 拉远端音色列表,不写本地库 —— 对账 / 排查"克隆成了但台账丢了"用。
func (h *VoiceCloneHandler) RemoteList(ctx context.Context, c *app.RequestContext) {
	page := int(h.MustUint(c, "page", 1))
	size := int(h.MustUint(c, "size", 50))
	list, err := h.Services.VoiceClone.ListFromRemote(ctx, c.Query("prefix"), page, size)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, list)
}

// TtsHandler TTS 音色登记册 + 文本合成
type TtsHandler struct{ *handler.BaseHandler }

func NewTtsHandler(r *service.Registry) *TtsHandler {
	return &TtsHandler{BaseHandler: handler.New(r)}
}

// Convert POST /rapid-admin/v1/biz/tts/convert —— **两套形态并存,按 stream 挑**
//   - stream=false(默认):**整段合成** —— 端点合成完一次性回完整 wav/mp3,
//     适合下载/存档;dir 非空时落 OSS 回 {"key":"..."}
//   - stream=true:**流式** —— 不用 WebSocket,单个 HTTP POST + chunked 响应,
//     前端 fetch + ReadableStream 边收边播(边合成边转发,首包即首音频帧)
func (h *TtsHandler) Convert(ctx context.Context, c *app.RequestContext) {
	var req service.TtsConvertRequest
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	if req.Stream && strings.TrimSpace(req.Dir) == "" {
		h.convertStream(ctx, c, req)
		return
	}
	// 整段形态:dir 为空回完整音频字节,非空落 OSS 回 key
	audio, key, err := h.Services.TtsVoice.Convert(ctx, req)
	if err != nil {
		h.Fail(c, err)
		return
	}
	if key != "" {
		h.OK(c, map[string]string{"key": key})
		return
	}
	// ⚠️ 音频字节不走统一响应体 —— 统一响应体是 JSON,把 wav/mp3 字节包进
	// JSON 只会得到一个巨大的 base64 字符串。直接写二进制 + audio Content-Type。
	ct := "audio/mpeg"
	if strings.Contains(strings.ToLower(req.Format), "wav") {
		ct = "audio/wav"
	}
	c.Header("Content-Type", ct)
	c.Status(200)
	_, _ = c.Write(audio)
}

// convertStream 流式转发:从合成端点读一块写一块。
// ⚠️ 音频流不走统一响应体 —— 统一响应体是 JSON,把 mp3 字节包进 JSON
// 只会得到一个巨大的 base64 字符串,流式也就没了意义。
// ⚠️ 一旦已经写了响应头,后续出错就只剩断流一条路 —— 所以**参数校验/错误
// 都发生在第一个字节写出去之前**(service 层先归一 voice、再建流)。
func (h *TtsHandler) convertStream(ctx context.Context, c *app.RequestContext, req service.TtsConvertRequest) {
	stream, err := h.Services.TtsVoice.ConvertStream(ctx, req)
	if err != nil {
		h.Fail(c, err)
		return
	}
	defer stream.Body.Close()
	ct := stream.ContentType
	if ct == "" {
		ct = "audio/mpeg"
	}
	c.Header("Content-Type", ct)
	c.Status(200)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := stream.Body.Read(buf)
		if n > 0 {
			if _, werr := c.Write(buf[:n]); werr != nil {
				// 客户端断开(关播放器)是最常见原因,降级为 debug 不打 error
				return
			}
		}
		if rerr != nil {
			return // io.EOF 或上游错误 —— 头已发出,只能断流
		}
	}
}

// VoiceList GET /rapid-admin/v1/biz/tts/voices?keyword=&tts_type=&page=&size=
func (h *TtsHandler) VoiceList(ctx context.Context, c *app.RequestContext) {
	q := service.TtsVoiceListQuery{
		Keyword: c.Query("keyword"),
		TtsType: c.Query("tts_type"),
	}
	q.Page, q.Size = h.GetPage(c)
	res, err := h.Services.TtsVoice.List(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.Page(c, res.List, res.Total)
}

// VoiceGet GET /rapid-admin/v1/biz/tts/voices/:id
func (h *TtsHandler) VoiceGet(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	v, err := h.Services.TtsVoice.Get(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, v)
}

// VoiceCreate POST /rapid-admin/v1/biz/tts/voices
// 创建/更新后异步合成一条试听音频(demo_key);失败只打日志,不影响主流程。
func (h *TtsHandler) VoiceCreate(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body model.SysTtsVoice
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.TtsVoice.Create(ctx, caller, &body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// VoiceUpdate PATCH /rapid-admin/v1/biz/tts/voices/:id
// ⚠️ voice_id / tts_type 不可改(换平台/换音色应删了重建,见 service 层说明)。
func (h *TtsHandler) VoiceUpdate(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body model.SysTtsVoice
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.TtsVoice.Update(ctx, caller, id, &body); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// VoiceDelete DELETE /rapid-admin/v1/biz/tts/voices/:id
func (h *TtsHandler) VoiceDelete(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.TtsVoice.Delete(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// VoiceBatchDelete POST /rapid-admin/v1/biz/tts/voices/batch-delete  {"ids":["1","2"]}
func (h *TtsHandler) VoiceBatchDelete(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ids, err := bindIDs(h.BaseHandler, c)
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.TtsVoice.BatchDelete(ctx, caller, ids); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}
