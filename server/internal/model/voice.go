// 语音业务模型 —— 音色克隆记录 + TTS 音色登记册
//
// ⚠️ **两张表是两个域,不要合并**:
//   - sys_voice_clone 是"动作台账" —— 每一行是一次克隆(或失败尝试),
//     带 DashScope 的状态机(DEPLOYING/OK/UNDEPLOYED/FAILED)
//   - sys_tts_voice 是"可用音色登记册" —— 既含克隆出来的,也含平台内置音色
//     (cosyvoice / qwen-tts / aliyun 各自的内置音色都登记在这),
//     TTS 合成接口查的是这张表
package model

// SysVoiceClone 音色克隆记录(表 sys_voice_clone)
//
// 字段:
//   - VoiceID: DashScope 返回的 voice_id;克隆失败时为空
//     (源项目用占位串 + 唯一索引,这里改成普通索引 —— 软删除 + 唯一索引
//     会让"删了重建"撞死在旧行上,不值得为占位串维持唯一约束)
//   - TargetModel: DashScope target_model(cosyvoice-v3-plus 等)
//   - SampleKey: 参考音频的对象 key(内容在 local 磁盘或 OSS 上,表里不存内容)
//   - Status: DEPLOYING(审核中) / OK(已发布) / UNDEPLOYED(审核拒绝) / FAILED(本地异常)
//   - SourceType: CLONE(从音频克隆) / DESIGN(从文本描述设计)
type SysVoiceClone struct {
	BaseModel

	// 审计
	CreatedBy uint64 `gorm:"index" json:"created_by,string"`
	UpdatedBy uint64 `json:"updated_by,string"`

	// DashScope 侧
	VoiceID     string `gorm:"size:128;index;collate:utf8mb4_bin" json:"voice_id"`
	Model       string `gorm:"size:64" json:"model"`         // voice-enrollment 等
	TargetModel string `gorm:"size:64" json:"target_model"`  // cosyvoice-v3-plus 等
	Prefix      string `gorm:"size:16" json:"prefix"`        // voice_id 前缀
	Status      string `gorm:"size:16;default:DEPLOYING;index" json:"status"`
	SourceType  string `gorm:"size:16;default:CLONE" json:"source_type"`
	ErrorMsg    string `gorm:"size:512" json:"error_msg"`

	// 展示信息
	Name        string `gorm:"size:64;not null;index;collate:utf8mb4_bin" json:"name"`
	Gender      string `gorm:"size:32" json:"gender"` // 性别类别(男声/女声/定制…)
	Sex         int    `gorm:"size:1" json:"sex"`     // 1 男 2 女
	TtsType     string `gorm:"size:32;index" json:"tts_type"` // 所属平台标识(aliyun-bailian 等)
	SampleKey   string `gorm:"size:256" json:"sample_key"`
	Language    string `gorm:"size:64" json:"language"` // 语言提示(逗号分隔,如 zh,en)
	Sort        int    `gorm:"default:0" json:"sort"`
	Description string `gorm:"size:512" json:"description"`
	Recommend   bool   `gorm:"default:false" json:"recommend"`
}

func (SysVoiceClone) TableName() string { return "sys_voice_clone" }

// VoiceSource 取值
const (
	VoiceSourceClone  = "CLONE"
	VoiceSourceDesign = "DESIGN"
)

// VoiceStatus 取值
const (
	VoiceStatusDeploying  = "DEPLOYING"
	VoiceStatusOK         = "OK"
	VoiceStatusUndeployed = "UNDEPLOYED"
	VoiceStatusFailed     = "FAILED"
)

// SysTtsVoice TTS 音色登记册(表 sys_tts_voice)
//
// ⚠️ **唯一性约束是复合的**(name+ttstype / voiceid+ttstype):
// 同一 voiceId 在 cosyvoice / qwen-tts / aliyun 等不同平台有各自的内置音色,
// 单列唯一索引会把"多平台同名音色"拦死。
// ⚠️ 同样出于软删除考虑**没有用数据库唯一索引** —— 用普通索引 + service 层
// 查重(排除软删行),删掉同名音色后可以重建。
type SysTtsVoice struct {
	BaseModel

	// 审计
	CreatedBy uint64 `gorm:"index" json:"created_by,string"`
	UpdatedBy uint64 `json:"updated_by,string"`

	Name        string `gorm:"size:64;not null;index;collate:utf8mb4_bin" json:"name"`
	VoiceID     string `gorm:"size:128;not null;index;collate:utf8mb4_bin" json:"voice_id"`
	Gender      string `gorm:"size:32" json:"gender"` // 男声/女声/童声/方言/定制
	Sex         int    `gorm:"size:1" json:"sex"`     // 1 男 2 女
	TtsType     string `gorm:"size:32;not null;index" json:"tts_type"` // 合成平台
	Sort        int    `gorm:"default:0" json:"sort"`  // 优先显示,推荐值为 0
	Description string `gorm:"size:512" json:"description"`
	Recommend   bool   `gorm:"default:false" json:"recommend"`
	// DemoKey 试听音频的对象 key(创建/更新后异步合成一份,"欢迎使用文本朗读…")
	DemoKey string `gorm:"size:256" json:"demo_key"`
}

func (SysTtsVoice) TableName() string { return "sys_tts_voice" }
