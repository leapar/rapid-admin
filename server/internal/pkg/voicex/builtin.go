package voicex

// 内置 TTS 音色目录 —— seed 首启时批量登记进 sys_tts_voice。
//
// 数据来源:
//   - bailian:百炼 CosyVoice 官方内置音色(voiceId → model)
//     https://help.aliyun.com/zh/model-studio/cosyvoice-voice-list
//     ⚠️ 只收 CosyVoice —— Qwen-TTS/Qwen-Audio 产品上不用,刻意不收录
//   - edge:微软 Edge 朗读常用 Neural 音色(短名,合成时 NormalizeEdgeVoice
//     会展开成协议全名)
//
// 维护原则:百炼改版才动 cosyvoice 表;Name 先用 voiceId 占位,运营可在
// 登记册里改名/改排序 —— seed 只补缺(按 tts_type+voice_id 判存在),不覆盖。

// TtsType 登记册 tts_type 取值(与 service 的 provider 判定共用同一组字符串)
const (
	TtsTypeBailian = "bailian"
	TtsTypeEdge    = "edge"
)

// BuiltinVoice 内置音色(登记册种子行)
type BuiltinVoice struct {
	VoiceID     string // 合成时传的 voice 参数
	Name        string // 展示名
	TtsType     string // bailian / edge
	Model       string // bailian:承载该音色的 cosyvoice model;edge 空
	Gender      string // 男声/女声(bailian 内置官方未标注,留空)
	Sex         int    // 1 男 2 女(0 未知)
	Description string
	Recommend   bool
}

// builtinCosyVoices CosyVoice 内置音色 → model。
// 命名规则(按官方 docs):"_v3" 后缀 → cosyvoice-v3-flash;"_v2" → cosyvoice-v2;无后缀 → cosyvoice-v2/v3-plus。
var builtinCosyVoices = []BuiltinVoice{
	// ---- cosyvoice-v3-flash ----
	{VoiceID: "longanyang", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Recommend: true, Description: "CosyVoice 内置 · 播报"},
	{VoiceID: "longanhuan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Recommend: true, Description: "CosyVoice 内置 · 多方言"},
	{VoiceID: "longhuhu_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置"},
	{VoiceID: "longpaopao_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置"},
	{VoiceID: "longjielidou_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置"},
	{VoiceID: "longxian_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置"},
	{VoiceID: "longling_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 童声"},
	{VoiceID: "longshanshan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 童声"},
	{VoiceID: "longniuniu_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 童声"},
	{VoiceID: "longjiaxin_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 粤语"},
	{VoiceID: "longjiayi_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 粤语"},
	{VoiceID: "longanyue_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 粤语"},
	{VoiceID: "longlaotie_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 东北"},
	{VoiceID: "longshange_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 陕西"},
	{VoiceID: "longanmin_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 闽南"},
	{VoiceID: "loongkyong_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 韩语"},
	{VoiceID: "loongriko_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongtomoka_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongabby_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongandy_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongannie_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongava_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongbeth_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongbetty_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongcally_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongcindy_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongdavid_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongdonna_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(美)"},
	{VoiceID: "loongemily_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(英)"},
	{VoiceID: "loongeric_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(英)"},
	{VoiceID: "loongluna_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(英)"},
	{VoiceID: "loongluca_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 英语(英)"},
	{VoiceID: "loongtomoya_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongyuuna_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongyuuma_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongjihun_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 韩语"},
	{VoiceID: "loongindah_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 印尼语"},
	{VoiceID: "longfei_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 诗歌"},
	{VoiceID: "longyingxiao_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 营销"},
	{VoiceID: "longyingxun_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 客服"},
	{VoiceID: "longyingjing_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 客服"},
	{VoiceID: "longyingling_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 客服"},
	{VoiceID: "longyingtao_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 客服"},
	{VoiceID: "longxiaochun_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Recommend: true, Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longxiaoxia_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Recommend: true, Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longyumi_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longanyun_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longanwen_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longanli_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longanlang_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longyingmu_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longantai_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 语音助手"},
	{VoiceID: "longhua_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longcheng_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longze_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longzhe_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longyan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longxing_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longtian_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longwan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Recommend: true, Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longqiang_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longfeifei_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longhao_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longanrou_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longhan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longanzhi_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longanling_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longanya_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longanqin_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 社交"},
	{VoiceID: "longmiao_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Recommend: true, Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longsanshu_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longyuan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longyue_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longxiu_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longnan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longwanjun_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longyichen_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书"},
	{VoiceID: "longlaobo_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书(老年男)"},
	{VoiceID: "longlaoyi_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 有声书(老年女)"},
	{VoiceID: "longjiqi_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 短视频"},
	{VoiceID: "longhouge_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 短视频"},
	{VoiceID: "longdaiyu_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 短视频"},
	{VoiceID: "longanran_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 直播"},
	{VoiceID: "longanxuan_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 直播"},
	{VoiceID: "longshuo_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 新闻"},
	{VoiceID: "longshu_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 新闻"},
	{VoiceID: "loongbella_v3", TtsType: TtsTypeBailian, Model: "cosyvoice-v3-flash", Description: "CosyVoice 内置 · 新闻"},

	// ---- cosyvoice-v2 ----
	{VoiceID: "longyingxiao", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 营销"},
	{VoiceID: "longjiqi", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 短视频"},
	{VoiceID: "longhouge", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 短视频"},
	{VoiceID: "longjixin", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanyue", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 粤语"},
	{VoiceID: "longshange", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 陕西"},
	{VoiceID: "longanmin", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 闽南"},
	{VoiceID: "longdaiyu", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 短视频"},
	{VoiceID: "longgaoseng", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanli", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanlang", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanwen", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanyun", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyumi_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longxiaochun_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longxiaoxia_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyichen", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longwanjun", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longlaobo", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longlaoyi", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longbaizhi", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longsanshu", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longxiu_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longmiao_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyue_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longnan_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyuan_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanqin", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanya", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanshuo", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanling", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanzhi", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanrou", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longqiang_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longhan_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longxing_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longhua_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longwan_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longcheng_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longfeifei_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longxiaocheng_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longzhe_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyan_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longtian_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longze_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longshao_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longhao_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "kabuleshen_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longhuhu", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanpei", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longwangwang", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longpaopao", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longshanshan", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longniuniu", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingmu", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingxun", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingcui", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingda", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingyan", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingtian", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingbing", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longyingling", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanran", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanxuan", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanchong", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longanping", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longjielidou_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longling_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longke_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longxian_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longlaotie_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longjiayi_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longtao_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longfei_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "libai_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 诗歌"},
	{VoiceID: "longjin_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longshu_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "loongbella_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longshuo_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longxiaobai_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "longjing_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "loongstella_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置"},
	{VoiceID: "loongyuuna_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongyuuma_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongjihun_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 韩语"},
	{VoiceID: "loongeva_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongbrian_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongluna_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongluca_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongemily_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongeric_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongabby_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongannie_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongandy_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongava_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongbeth_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongbetty_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongcindy_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongcally_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongdavid_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongdonna_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 英语"},
	{VoiceID: "loongkyong_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 韩语"},
	{VoiceID: "loongtomoka_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 日语"},
	{VoiceID: "loongtomoya_v2", TtsType: TtsTypeBailian, Model: "cosyvoice-v2", Description: "CosyVoice 内置 · 日语"},
}

// builtinEdgeVoices Edge 朗读常用音色(短名;合成时 NormalizeEdgeVoice 归一)。
// 不穷举官方全量(500+),挑中文优先 + 常用外语的稳定子集,够用为主。
var builtinEdgeVoices = []BuiltinVoice{
	{VoiceID: "zh-CN-XiaoxiaoNeural", Name: "晓晓", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Recommend: true, Description: "Edge 朗读 · 温暖女声(默认)"},
	{VoiceID: "zh-CN-XiaoyiNeural", Name: "晓伊", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Description: "Edge 朗读 · 活泼女声"},
	{VoiceID: "zh-CN-YunxiNeural", Name: "云希", TtsType: TtsTypeEdge, Gender: "男声", Sex: 1, Recommend: true, Description: "Edge 朗读 · 年轻男声"},
	{VoiceID: "zh-CN-YunyangNeural", Name: "云扬", TtsType: TtsTypeEdge, Gender: "男声", Sex: 1, Description: "Edge 朗读 · 新闻男声"},
	{VoiceID: "zh-CN-YunjianNeural", Name: "云健", TtsType: TtsTypeEdge, Gender: "男声", Sex: 1, Description: "Edge 朗读 · 磁性男声"},
	{VoiceID: "zh-CN-YunxiaNeural", Name: "云夏", TtsType: TtsTypeEdge, Gender: "童声", Sex: 1, Description: "Edge 朗读 · 少年音"},
	{VoiceID: "zh-CN-liaoning-XiaobeiNeural", Name: "小贝", TtsType: TtsTypeEdge, Gender: "方言", Sex: 2, Description: "Edge 朗读 · 东北话"},
	{VoiceID: "zh-CN-shaanxi-XiaoniNeural", Name: "小妮", TtsType: TtsTypeEdge, Gender: "方言", Sex: 2, Description: "Edge 朗读 · 陕西话"},
	{VoiceID: "zh-HK-HiuMaanNeural", Name: "曉曼", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Description: "Edge 朗读 · 粤语"},
	{VoiceID: "zh-HK-WanLungNeural", Name: "雲龍", TtsType: TtsTypeEdge, Gender: "男声", Sex: 1, Description: "Edge 朗读 · 粤语"},
	{VoiceID: "zh-TW-HsiaoChenNeural", Name: "曉臻", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Description: "Edge 朗读 · 台湾普通话"},
	{VoiceID: "zh-TW-YunJheNeural", Name: "雲哲", TtsType: TtsTypeEdge, Gender: "男声", Sex: 1, Description: "Edge 朗读 · 台湾普通话"},
	{VoiceID: "en-US-JennyNeural", Name: "Jenny", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Description: "Edge 朗读 · 英语(美)"},
	{VoiceID: "en-US-AriaNeural", Name: "Aria", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Description: "Edge 朗读 · 英语(美)"},
	{VoiceID: "en-US-GuyNeural", Name: "Guy", TtsType: TtsTypeEdge, Gender: "男声", Sex: 1, Description: "Edge 朗读 · 英语(美)"},
	{VoiceID: "ja-JP-NanamiNeural", Name: "Nanami", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Description: "Edge 朗读 · 日语"},
	{VoiceID: "ko-KR-SunHiNeural", Name: "SunHi", TtsType: TtsTypeEdge, Gender: "女声", Sex: 2, Description: "Edge 朗读 · 韩语"},
}

// BuiltinTtsVoices 全量内置音色(bailian CosyVoice + edge),seed 用。
// bailian 的 Name 未在官方文档给出中文名,用 voiceId 占位,运营可改。
func BuiltinTtsVoices() []BuiltinVoice {
	out := make([]BuiltinVoice, 0, len(builtinCosyVoices)+len(builtinEdgeVoices))
	for _, v := range builtinCosyVoices {
		if v.Name == "" {
			v.Name = v.VoiceID
		}
		out = append(out, v)
	}
	out = append(out, builtinEdgeVoices...)
	return out
}
