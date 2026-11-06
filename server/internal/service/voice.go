// VoiceCloneService / TtsVoiceService —— 音色克隆 + TTS 音色登记册
//
// 移植自业务项目的声音克隆栈(移植于 2026-11-06),两处刻意的不同:
//   - 凭据不再从远程配置中心拉,直接走 config.VoiceConfig(yaml voice 段)
//   - 存储统一走 oss.Provider(源项目按 OssType 分发拼 URL ——
//     本仓库 provider 已经把"对象 → 可访问 URL"抽象成 SignDownloadURL,
//     local / aliyun / 未来其他实现各自处理,service 层不感知)
//
// ⚠️ **DashScope 要求参考音频是 https:// 公网可访问 URL** ——
// SignDownloadURL 给出的链接必须是 DashScope 服务端 GET 得到的:
// aliyun 公共读桶直链没问题;local 实现给的是本仓库静态挂载直链,
// 部署在内网/无 https 时克隆会在 DashScope 侧失败 —— 错误会落进
// 记录的 error_msg(FAILED),不会静默。
package service

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"path"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/pkg/voicex"
)

// voiceSampleTTL 参考音频/试听音频直链的有效期。
// DashScope 在克隆请求里会回头 GET 参考音频 —— TTL 太短会出现
// "本地拿到 URL 还没发出去就过期"的竞态;1 小时是宽裕值。
const voiceSampleTTL = time.Hour

// demoTtsText 音色登记后的试听文本(与源项目一致)
const demoTtsText = "欢迎使用文本朗读，这是我的声音。"

// VoiceCloneCreateRequest 音频克隆请求(multipart 字段在 handler 解析)
type VoiceCloneCreateRequest struct {
	Name          string
	Gender        string
	Sex           int
	TtsType       string
	TargetModel   string
	Prefix        string
	LanguageHints []string
	Sort          int
	Description   string
	Recommend     bool
}

// VoiceCloneUpdateRequest 编辑请求 —— **只允许改展示信息**,不重新训练:
// voice_id / status / sample_key 是 DashScope 侧的事实,接口不允许覆盖。
type VoiceCloneUpdateRequest struct {
	Name        string
	Gender      string
	Sex         int
	TtsType     string
	Sort        int
	Description string
	Recommend   bool
}

// VoiceCloneListQuery 列表查询
type VoiceCloneListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"` // 按名称模糊
	Status  string `json:"status"`
	TtsType string `json:"tts_type"`
}

// VoiceCloneService 音色克隆
type VoiceCloneService interface {
	CreateFromAudio(ctx context.Context, caller *Caller, req VoiceCloneCreateRequest, audio *multipart.FileHeader) (*model.SysVoiceClone, error)
	Update(ctx context.Context, caller *Caller, id uint64, req VoiceCloneUpdateRequest) error
	Delete(ctx context.Context, caller *Caller, id uint64) error
	BatchDelete(ctx context.Context, caller *Caller, ids []uint64) error
	Get(ctx context.Context, id uint64) (*model.SysVoiceClone, error)
	List(ctx context.Context, q VoiceCloneListQuery) (*model.PageResponse, error)
	RefreshStatus(ctx context.Context, id uint64) (*model.SysVoiceClone, error)
	ListFromRemote(ctx context.Context, prefix string, page, size int) ([]voicex.RemoteVoice, error)
}

// TtsConvertRequest 文本合成请求
type TtsConvertRequest struct {
	Text   string `json:"text"`
	Voice  string `json:"voice"`  // 本地登记册 id(纯数字)或远端 voice_id
	TTS    string `json:"tts"`    // 合成平台;voice 是本地 id 时以登记册为准
	Volume string `json:"volume"` // 百分比字符串,"0%" = 不变
	Rate   string `json:"rate"`
	// Dir 非空 = 结果存 OSS(key 前缀 Dir),返回 key;空 = 直接回音频
	Dir string `json:"dir"`
	// Stream 合成形态开关(两套并存,按请求挑):
	//   false(默认)= **整段合成** —— 端点合成完一次性回完整 wav/mp3,
	//                 适合下载/存档;长文本要等全程才有响应
	//   true        = **流式** —— chunked 逐块转发,前端 fetch + ReadableStream
	//                 边收边播,首包即首音频帧
	Stream bool `json:"stream"`
	// Format 音频格式:"wav" / "mp3"(默认 mp3)。
	// ⚠️ 只有端点支持时才生效 —— 参数原样透传给 synth 网关
	// (JSON 字段 format,老网关不认识就忽略,回它默认的 mp3)。
	Format string `json:"format"`
	// Provider 合成后端:"bailian"(默认,走 voice.synth_url)或 "edge"
	// (微软 Edge 朗读,免凭据)。voice 是本地登记册 id 时以登记册的
	// tts_type 为准;Edge 仅输出 mp3,format=wav 会被显式拒绝。
	Provider string `json:"provider"`
	// Pitch 音高("+0Hz" = 不变)。仅 Edge 生效。
	Pitch string `json:"pitch"`
}

// TtsVoiceService TTS 音色登记册 + 文本合成
type TtsVoiceService interface {
	Create(ctx context.Context, caller *Caller, m *model.SysTtsVoice) (uint64, error)
	Update(ctx context.Context, caller *Caller, id uint64, m *model.SysTtsVoice) error
	Delete(ctx context.Context, caller *Caller, id uint64) error
	BatchDelete(ctx context.Context, caller *Caller, ids []uint64) error
	Get(ctx context.Context, id uint64) (*model.SysTtsVoice, error)
	List(ctx context.Context, q TtsVoiceListQuery) (*model.PageResponse, error)
	// Convert 文本合成。Dir 为空时返回音频字节(handler 直接流回),
	// 非空时上传 OSS 并返回对象 key。
	Convert(ctx context.Context, req TtsConvertRequest) (audio []byte, key string, err error)
	// ConvertStream 流式合成 —— **不用 WebSocket**:单个 HTTP POST,
	// 音频以 chunked 流返回,handler 边读边转发(前端 fetch + ReadableStream
	// 边收边播,首包延迟不再等于整段合成时长)。仅用于 Dir == "" 的直播形态。
	ConvertStream(ctx context.Context, req TtsConvertRequest) (*voicex.SynthStream, error)
}

// TtsVoiceListQuery 登记册列表查询
type TtsVoiceListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"` // 按名称模糊
	TtsType string `json:"tts_type"`
}

// ---- 音色克隆实现 ----

type voiceCloneService struct {
	db        *gorm.DB
	prov      oss.Provider
	cfg       config.VoiceConfig
	keyPrefix string // 对象 key 第一段必须是静态挂载点(path_root),由 main 装配时传入
}

// NewVoiceCloneService 构造。keyPrefix 形如 "<path_root>/voice" ——
// ⚠️ local provider 的直链规则是"剥掉 key 第一段拼到静态挂载点后面",
// 第一段不是挂载点 ⇒ 直链 404,克隆必然失败。
func NewVoiceCloneService(db *gorm.DB, prov oss.Provider, cfg config.VoiceConfig, keyPrefix string) VoiceCloneService {
	return &voiceCloneService{db: db, prov: prov, cfg: cfg, keyPrefix: keyPrefix}
}

func (s *voiceCloneService) client() (*voicex.Client, error) {
	return voicex.New(voicex.Config{
		APIKey:         s.cfg.APIKey,
		WorkspaceID:    s.cfg.WorkspaceID,
		Region:         s.cfg.Region,
		DefaultModel:   s.cfg.DefaultModel,
		TimeoutSeconds: s.cfg.TimeoutSeconds,
	})
}

func (s *voiceCloneService) targetModel() string {
	if m := strings.TrimSpace(s.cfg.DefaultTargetModel); m != "" {
		return m
	}
	return "cosyvoice-v3-plus"
}

func (s *voiceCloneService) normPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	return page, size
}

// audioExt 取音频后缀,不在白名单里的一律落 .wav
// (DashScope 支持的格式集,与源项目一致)
func audioExt(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".wav", ".mp3", ".m4a", ".pcm", ".aac", ".flac", ".ogg":
		return ext
	default:
		return ".wav"
	}
}

func audioContentType(ext string) string {
	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".aac":
		return "audio/aac"
	case ".pcm":
		return "application/octet-stream"
	default:
		return "audio/wav"
	}
}

func (s *voiceCloneService) CreateFromAudio(ctx context.Context, caller *Caller, req VoiceCloneCreateRequest, audio *multipart.FileHeader) (*model.SysVoiceClone, error) {
	if caller == nil || caller.UserID == 0 {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, errcode.New(errcode.BadRequest, "name 不能为空")
	}
	if audio == nil || audio.Size == 0 {
		return nil, errcode.New(errcode.BadRequest, "请上传参考音频")
	}
	if req.TargetModel == "" {
		req.TargetModel = s.targetModel()
	}
	if req.Prefix == "" {
		req.Prefix = "voice"
	}
	if req.TtsType == "" {
		req.TtsType = "aliyun-bailian"
	}
	if req.Gender == "" {
		req.Gender = "定制"
	}

	// 1. 参考音频进对象存储(内容不落表,表里只留 key)
	ext := audioExt(audio.Filename)
	key := fmt.Sprintf("%s/clone/%s%s", s.keyPrefix, sonyflake.NextString(), ext)
	f, err := audio.Open()
	if err != nil {
		return nil, errcode.New(errcode.BadRequest, "读取参考音频失败").WithDetail(err.Error())
	}
	defer f.Close()
	if _, err := s.prov.Put(ctx, key, f, audioContentType(ext)); err != nil {
		return nil, errcode.New(errcode.Internal, "上传参考音频失败").WithDetail(err.Error())
	}

	// 2. 直链(DashScope 服务端要回头 GET,必须公网可访问)
	audioURL, err := s.prov.SignDownloadURL(ctx, key, voiceSampleTTL)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "生成参考音频公网 URL 失败").WithDetail(err.Error())
	}

	// 3. 调 DashScope 克隆
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	voiceID, err := client.CreateVoiceFromURL(req.TargetModel, req.Prefix, audioURL, req.LanguageHints)

	m := &model.SysVoiceClone{
		Name:        req.Name,
		Gender:      req.Gender,
		Sex:         req.Sex,
		TtsType:     req.TtsType,
		Model:       string(voicex.ModelVoiceEnrollment),
		TargetModel: req.TargetModel,
		SampleKey:   key,
		Prefix:      req.Prefix,
		SourceType:  model.VoiceSourceClone,
		Language:    strings.Join(req.LanguageHints, ","),
		Sort:        req.Sort,
		Description: req.Description,
		Recommend:   req.Recommend,
		CreatedBy: caller.UserID,
		UpdatedBy: caller.UserID,
	}
	m.ID = sonyflake.Next()
	if err != nil {
		// ⚠️ 克隆失败也要落一条 FAILED 记录 —— 否则"音频传上去了、远端没成"
		// 的孤儿对象没人知道从哪来的。失败记录 voice_id 为空,排障从 error_msg 进。
		m.Status = model.VoiceStatusFailed
		m.ErrorMsg = err.Error()
		if createErr := s.db.WithContext(ctx).Create(m).Error; createErr != nil {
			logger.L().Warn("voice: 记录克隆失败原因失败(存在孤儿音频对象)",
				zap.String("key", key), zap.Error(createErr))
		}
		return nil, errcode.New(errcode.Internal, "克隆音色失败").WithDetail(err.Error())
	}
	m.Status = model.VoiceStatusDeploying
	m.VoiceID = voiceID
	if err := s.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "持久化克隆记录失败").WithDetail(err.Error())
	}
	return m, nil
}

func (s *voiceCloneService) Update(ctx context.Context, caller *Caller, id uint64, req VoiceCloneUpdateRequest) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	if strings.TrimSpace(req.Name) == "" {
		return errcode.New(errcode.BadRequest, "name 不能为空")
	}
	var old model.SysVoiceClone
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "克隆记录不存在")
		}
		return errcode.New(errcode.Internal, "查询克隆记录失败").WithDetail(err.Error())
	}
	updates := map[string]any{
		"name":        req.Name,
		"gender":      req.Gender,
		"sex":         req.Sex,
		"tts_type":    req.TtsType,
		"sort":        req.Sort,
		"description": req.Description,
		"recommend":   req.Recommend,
		"updated_by":  caller.UserID,
	}
	if err := s.db.WithContext(ctx).Model(&model.SysVoiceClone{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新克隆记录失败").WithDetail(err.Error())
	}
	return nil
}

func (s *voiceCloneService) Delete(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var m model.SysVoiceClone
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "克隆记录不存在")
		}
		return errcode.New(errcode.Internal, "查询克隆记录失败").WithDetail(err.Error())
	}
	// 远端音色 + 参考音频都尽力删,失败不阻断本地删除 ——
	// 本地记录是事实源,远端残留可以靠 ListFromRemote 对账发现。
	if m.VoiceID != "" {
		if client, err := s.client(); err == nil {
			if derr := client.DeleteVoice(m.VoiceID); derr != nil {
				logger.L().Warn("voice: 删除 DashScope 远端音色失败(本地继续删除)",
					zap.Uint64("id", id), zap.Error(derr))
			}
		}
	}
	if m.SampleKey != "" {
		if derr := s.prov.Delete(ctx, m.SampleKey); derr != nil {
			logger.L().Warn("voice: 删除参考音频失败(本地继续删除)",
				zap.String("key", m.SampleKey), zap.Error(derr))
		}
	}
	return s.db.WithContext(ctx).Delete(&m).Error
}

func (s *voiceCloneService) BatchDelete(ctx context.Context, caller *Caller, ids []uint64) error {
	for _, id := range ids {
		if err := s.Delete(ctx, caller, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *voiceCloneService) Get(ctx context.Context, id uint64) (*model.SysVoiceClone, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var m model.SysVoiceClone
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "克隆记录不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询克隆记录失败").WithDetail(err.Error())
	}
	return &m, nil
}

func (s *voiceCloneService) List(ctx context.Context, q VoiceCloneListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysVoiceClone{})
	if strings.TrimSpace(q.Keyword) != "" {
		tx = tx.Where("name LIKE ?", "%"+q.Keyword+"%")
	}
	if q.Status != "" {
		tx = tx.Where("status = ?", q.Status)
	}
	if q.TtsType != "" {
		tx = tx.Where("tts_type = ?", q.TtsType)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询克隆记录数失败").WithDetail(err.Error())
	}
	var rows []model.SysVoiceClone
	if err := tx.Order("sort ASC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询克隆记录失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// isTerminalStatus 终态:OK(已发布) / UNDEPLOYED(审核拒绝) / FAILED(本地异常)。
// ⚠️ 终态的 error_msg 不被网络错误覆盖 —— 审核被拒的原因比"这次刷新超时"重要得多。
func isTerminalVoiceStatus(s string) bool {
	switch s {
	case model.VoiceStatusOK, model.VoiceStatusUndeployed, model.VoiceStatusFailed:
		return true
	default:
		return false
	}
}

func (s *voiceCloneService) RefreshStatus(ctx context.Context, id uint64) (*model.SysVoiceClone, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var m model.SysVoiceClone
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "克隆记录不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询克隆记录失败").WithDetail(err.Error())
	}
	if m.VoiceID == "" {
		return nil, errcode.New(errcode.BadRequest, "voice_id 为空(克隆失败记录),无法刷新")
	}
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	info, err := client.QueryVoice(m.VoiceID)
	if err != nil {
		if !isTerminalVoiceStatus(m.Status) {
			if uerr := s.db.WithContext(ctx).Model(&model.SysVoiceClone{}).Where("id = ?", id).
				Updates(map[string]any{"error_msg": err.Error()}).Error; uerr != nil {
				logger.L().Warn("voice: RefreshStatus 写入错误信息失败", zap.Uint64("id", id), zap.Error(uerr))
			}
		}
		return nil, errcode.New(errcode.Internal, "查询 DashScope 状态失败").WithDetail(err.Error())
	}
	updates := map[string]any{
		"status":       string(info.Status),
		"target_model": info.TargetModel,
		"error_msg":    "",
	}
	if err := s.db.WithContext(ctx).Model(&model.SysVoiceClone{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "更新克隆记录失败").WithDetail(err.Error())
	}
	m.Status = string(info.Status)
	m.TargetModel = info.TargetModel
	m.ErrorMsg = ""
	return &m, nil
}

func (s *voiceCloneService) ListFromRemote(ctx context.Context, prefix string, page, size int) ([]voicex.RemoteVoice, error) {
	client, err := s.client()
	if err != nil {
		return nil, err
	}
	if page <= 0 {
		page = 1
	}
	return client.ListVoices(prefix, page-1, size)
}

// ---- TTS 音色登记册 + 合成实现 ----

type ttsVoiceService struct {
	db        *gorm.DB
	prov      oss.Provider
	cfg       config.VoiceConfig
	keyPrefix string
}

func NewTtsVoiceService(db *gorm.DB, prov oss.Provider, cfg config.VoiceConfig, keyPrefix string) TtsVoiceService {
	return &ttsVoiceService{db: db, prov: prov, cfg: cfg, keyPrefix: keyPrefix}
}

func (s *ttsVoiceService) synthesizer() (*voicex.Synthesizer, error) {
	return voicex.NewSynthesizer(voicex.SynthConfig{
		Endpoint:       s.cfg.SynthURL,
		TimeoutSeconds: s.cfg.SynthTimeoutSeconds,
	})
}

// edgeClient Edge 合成客户端(免凭据,构造恒成功)。
// 每次取新的?不 —— 内部只有时钟偏差一把锁,单例复用即可。
func (s *ttsVoiceService) edgeClient() *voicex.EdgeClient {
	e := s.cfg.Edge
	return voicex.NewEdgeClient(voicex.EdgeConfig{
		Token:               e.Token,
		WssURL:              e.WssURL,
		ReadTimeoutSeconds:  e.ReadTimeoutSeconds,
		TotalTimeoutSeconds: e.TotalTimeoutSeconds,
	})
}

// provider 归一:显式 provider > 登记册 tts_type > 默认 bailian。
// "edge" 之外的一律当 bailian(拼错平台名会落到百炼并得到 synth_url 的
// 显式错误,不会静默换通道)。
func ttsProvider(req, record string) string {
	v := strings.TrimSpace(req)
	if v == "" {
		v = strings.TrimSpace(record)
	}
	if strings.EqualFold(v, "edge") {
		return "edge"
	}
	return "bailian"
}

func (s *ttsVoiceService) normPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > 200 {
		size = 20
	}
	return page, size
}

// checkDup 查重(排除软删行 + 指定 id)。
// ⚠️ 复合唯一约束(name+tts_type / voice_id+tts_type)在 service 层手工执行,
// 没有下放数据库唯一索引 —— 软删除行会占住唯一键,"删了重建"就死锁在旧行上。
func (s *ttsVoiceService) checkDup(ctx context.Context, ttsType, name, voiceID string, excludeID uint64) error {
	tx := s.db.WithContext(ctx).Model(&model.SysTtsVoice{}).Where("tts_type = ?", ttsType)
	if excludeID > 0 {
		tx = tx.Where("id <> ?", excludeID)
	}
	if name != "" {
		var n int64
		if err := tx.Where("name = ?", name).Count(&n).Error; err != nil {
			return errcode.New(errcode.Internal, "查重失败").WithDetail(err.Error())
		}
		if n > 0 {
			return errcode.New(errcode.BadRequest, "同一平台下音色名称已存在: "+name)
		}
	}
	if voiceID != "" {
		var n int64
		if err := tx.Where("voice_id = ?", voiceID).Count(&n).Error; err != nil {
			return errcode.New(errcode.Internal, "查重失败").WithDetail(err.Error())
		}
		if n > 0 {
			return errcode.New(errcode.BadRequest, "同一平台下 voice_id 已存在: "+voiceID)
		}
	}
	return nil
}

func (s *ttsVoiceService) Create(ctx context.Context, caller *Caller, m *model.SysTtsVoice) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if strings.TrimSpace(m.Name) == "" {
		return 0, errcode.New(errcode.BadRequest, "name 不能为空")
	}
	if strings.TrimSpace(m.VoiceID) == "" {
		return 0, errcode.New(errcode.BadRequest, "voice_id 不能为空")
	}
	if strings.TrimSpace(m.TtsType) == "" {
		return 0, errcode.New(errcode.BadRequest, "tts_type 不能为空")
	}
	if err := s.checkDup(ctx, m.TtsType, m.Name, m.VoiceID, 0); err != nil {
		return 0, err
	}
	m.ID = sonyflake.Next()
	m.CreatedBy = caller.UserID
	m.UpdatedBy = caller.UserID
	if err := s.db.WithContext(ctx).Create(m).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建音色失败").WithDetail(err.Error())
	}
	// 试听音频异步合成,失败不影响创建(只少一个试听文件)
	go s.createDemo(m.ID, m.VoiceID, m.TtsType)
	return m.ID, nil
}

func (s *ttsVoiceService) Update(ctx context.Context, caller *Caller, id uint64, m *model.SysTtsVoice) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	if strings.TrimSpace(m.Name) == "" {
		return errcode.New(errcode.BadRequest, "name 不能为空")
	}
	var old model.SysTtsVoice
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "音色不存在")
		}
		return errcode.New(errcode.Internal, "查询音色失败").WithDetail(err.Error())
	}
	if err := s.checkDup(ctx, old.TtsType, m.Name, "", id); err != nil {
		return err
	}
	// ⚠️ voice_id / tts_type 不允许改 —— 换平台/换音色应该删了重建,
	// 就地改会让 demo_key 与新音色对不上(试听放的是别人的声音)。
	updates := map[string]any{
		"name":        m.Name,
		"gender":      m.Gender,
		"sex":         m.Sex,
		"sort":        m.Sort,
		"description": m.Description,
		"recommend":   m.Recommend,
		"updated_by":  caller.UserID,
	}
	if err := s.db.WithContext(ctx).Model(&model.SysTtsVoice{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新音色失败").WithDetail(err.Error())
	}
	go s.createDemo(id, old.VoiceID, old.TtsType)
	return nil
}

func (s *ttsVoiceService) Delete(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var m model.SysTtsVoice
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "音色不存在")
		}
		return errcode.New(errcode.Internal, "查询音色失败").WithDetail(err.Error())
	}
	if err := s.db.WithContext(ctx).Delete(&m).Error; err != nil {
		return errcode.New(errcode.Internal, "删除音色失败").WithDetail(err.Error())
	}
	if m.DemoKey != "" {
		if derr := s.prov.Delete(ctx, m.DemoKey); derr != nil {
			logger.L().Warn("voice: 删除试听音频失败(本地继续删除)", zap.String("key", m.DemoKey), zap.Error(derr))
		}
	}
	return nil
}

func (s *ttsVoiceService) BatchDelete(ctx context.Context, caller *Caller, ids []uint64) error {
	for _, id := range ids {
		if err := s.Delete(ctx, caller, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *ttsVoiceService) Get(ctx context.Context, id uint64) (*model.SysTtsVoice, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var m model.SysTtsVoice
	if err := s.db.WithContext(ctx).First(&m, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "音色不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询音色失败").WithDetail(err.Error())
	}
	return &m, nil
}

func (s *ttsVoiceService) List(ctx context.Context, q TtsVoiceListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysTtsVoice{})
	if strings.TrimSpace(q.Keyword) != "" {
		tx = tx.Where("name LIKE ?", "%"+q.Keyword+"%")
	}
	if q.TtsType != "" {
		tx = tx.Where("tts_type = ?", q.TtsType)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询音色数失败").WithDetail(err.Error())
	}
	var rows []model.SysTtsVoice
	if err := tx.Order("sort ASC, id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询音色失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// createDemo 异步合成试听音频并回写 demo_key。
// ⚠️ 跑在 goroutine 里 —— 失败只打日志:试听是锦上添花,
// 为它让创建/更新失败,是拿主流程给装饰品陪葬。
func (s *ttsVoiceService) createDemo(id uint64, voiceID, ttsType string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var (
		audio []byte
		err   error
	)
	if ttsProvider("", ttsType) == "edge" {
		audio, err = s.edgeClient().Once(voicex.EdgeSynthParams{Text: demoTtsText, Voice: voiceID})
	} else {
		var synth *voicex.Synthesizer
		if synth, err = s.synthesizer(); err == nil {
			audio, err = synth.Once(voicex.SynthParams{
				Text:  demoTtsText,
				Voice: voiceID,
				TTS:   ttsType,
			})
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "synth_url 未配置") {
			logger.L().Warn("voice: 试听音频未合成(synth_url 未配置)", zap.Uint64("id", id))
		} else {
			logger.L().Warn("voice: 试听音频合成失败", zap.Uint64("id", id), zap.Error(err))
		}
		return
	}
	key := fmt.Sprintf("%s/tts-demo/%s.mp3", s.keyPrefix, sonyflake.NextString())
	if _, err := s.prov.Put(ctx, key, bytes.NewReader(audio), "audio/mpeg"); err != nil {
		logger.L().Warn("voice: 试听音频上传失败", zap.Uint64("id", id), zap.Error(err))
		return
	}
	if err := s.db.WithContext(ctx).Model(&model.SysTtsVoice{}).Where("id = ?", id).
		Update("demo_key", key).Error; err != nil {
		logger.L().Warn("voice: 回写试听 key 失败", zap.Uint64("id", id), zap.Error(err))
	}
}

func (s *ttsVoiceService) Convert(ctx context.Context, req TtsConvertRequest) ([]byte, string, error) {
	if err := s.resolveVoice(ctx, &req); err != nil {
		return nil, "", err
	}
	if ttsProvider(req.Provider, req.TTS) == "edge" {
		audio, err := s.edgeClient().Once(voicex.EdgeSynthParams{
			Text:   req.Text,
			Voice:  req.Voice,
			Volume: req.Volume,
			Rate:   req.Rate,
			Pitch:  req.Pitch,
		})
		if err != nil {
			return nil, "", errcode.New(errcode.Internal, "文本合成失败").WithDetail(err.Error())
		}
		return s.putAudio(ctx, audio, req.Dir)
	}
	synth, err := s.synthesizer()
	if err != nil {
		return nil, "", errcode.New(errcode.Internal, "文本合成未配置").WithDetail(err.Error())
	}
	audio, err := synth.Once(voicex.SynthParams{
		Text:   req.Text,
		Voice:  req.Voice,
		TTS:    req.TTS,
		Volume: req.Volume,
		Rate:   req.Rate,
		Format: req.Format,
	})
	if err != nil {
		return nil, "", errcode.New(errcode.Internal, "文本合成失败").WithDetail(err.Error())
	}
	return s.putAudio(ctx, audio, req.Dir)
}

func (s *ttsVoiceService) ConvertStream(ctx context.Context, req TtsConvertRequest) (*voicex.SynthStream, error) {
	if strings.TrimSpace(req.Dir) != "" {
		return nil, errcode.New(errcode.BadRequest, "流式合成不支持 dir(落 OSS 走非流式的 /tts/convert)")
	}
	if err := s.resolveVoice(ctx, &req); err != nil {
		return nil, err
	}
	if ttsProvider(req.Provider, req.TTS) == "edge" {
		if strings.Contains(strings.ToLower(req.Format), "wav") {
			return nil, errcode.New(errcode.BadRequest, "edge tts 仅输出 mp3,不支持 format=wav")
		}
		return s.edgeClient().OnceStream(voicex.EdgeSynthParams{
			Text:   req.Text,
			Voice:  req.Voice,
			Volume: req.Volume,
			Rate:   req.Rate,
			Pitch:  req.Pitch,
		})
	}
	synth, err := s.synthesizer()
	if err != nil {
		return nil, errcode.New(errcode.Internal, "文本合成未配置").WithDetail(err.Error())
	}
	return synth.OnceStream(voicex.SynthParams{
		Text:   req.Text,
		Voice:  req.Voice,
		TTS:    req.TTS,
		Volume: req.Volume,
		Rate:   req.Rate,
		Format: req.Format,
	})
}

// putAudio Dir 非空 → 落 OSS 返回 key;空 → 音频原样返回(handler 直接回给前端)
func (s *ttsVoiceService) putAudio(ctx context.Context, audio []byte, dir string) ([]byte, string, error) {
	if strings.TrimSpace(dir) == "" {
		return audio, "", nil
	}
	key := fmt.Sprintf("%s/%s/%s.mp3", s.keyPrefix, strings.Trim(dir, "/"), sonyflake.NextString())
	if _, err := s.prov.Put(ctx, key, bytes.NewReader(audio), "audio/mpeg"); err != nil {
		return nil, "", errcode.New(errcode.Internal, "合成结果上传失败").WithDetail(err.Error())
	}
	return nil, key, nil
}

// resolveVoice 校验 text + voice 归一(本地登记册 id → 远端 voice_id),
// Convert / ConvertStream 共用
func (s *ttsVoiceService) resolveVoice(ctx context.Context, req *TtsConvertRequest) error {
	if strings.TrimSpace(req.Text) == "" {
		return errcode.New(errcode.BadRequest, "text 不能为空")
	}
	// voice 传本地登记册 id(纯数字)时以登记册为准;否则当远端 voice_id 直传
	if id, perr := strconv.ParseUint(strings.TrimSpace(req.Voice), 10, 64); perr == nil && id > 0 {
		m, err := s.Get(ctx, id)
		if err != nil {
			return err
		}
		req.Voice = m.VoiceID
		req.TTS = m.TtsType
	}
	return nil
}
