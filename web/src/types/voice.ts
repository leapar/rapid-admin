/**
 * 语音(音色登记册 + 克隆台账)—— 与后端 wire 格式对齐
 *
 * 后端: server/internal/model/voice.go(SysTtsVoice / SysVoiceClone)
 * 路由: /rapid-admin/v1/biz/tts/* 与 /rapid-admin/v1/biz/voice-clones/*
 */

/** sys_tts_voice 一行 —— 音色登记册 */
export interface TtsVoice {
  id: string
  /** 展示名(运营可改) */
  name: string
  /** 合成用的音色 ID(edge 行是 zh-CN-XiaoxiaoNeural 这类短名) */
  voice_id: string
  /** 男声 / 女声 / 童声 / 方言 / 定制 */
  gender: string
  /** 1 男 2 女 */
  sex: number
  /** 合成平台:bailian / edge */
  tts_type: string
  sort: number
  description: string
  recommend: boolean
  /** 试听音频的对象 key(空 = 试听还没合成出来;key 走 OSS,内容不落表) */
  demo_key: string
  created_at?: string
  updated_at?: string
}

/** 添加 / 编辑音色的表单值(编辑时 voice_id / tts_type 只读) */
export interface TtsVoiceForm {
  name: string
  voice_id: string
  tts_type: string
  gender: string
  sex: number
  sort: number
  description: string
  recommend: boolean
}

/** sys_voice_clone 一行 —— 克隆台账(每行是一次克隆或失败尝试) */
export interface VoiceClone {
  id: string
  /** DashScope 返回的 voice_id;克隆失败时为空 */
  voice_id: string
  model: string
  target_model: string
  prefix: string
  /** DEPLOYING(审核中)/ OK(已发布)/ UNDEPLOYED(审核拒绝)/ FAILED(本地异常) */
  status: string
  /** CLONE(从音频克隆)/ DESIGN(从文本描述设计) */
  source_type: string
  error_msg: string
  name: string
  gender: string
  sex: number
  tts_type: string
  sample_key: string
  /** 语言提示(逗号分隔,如 zh,en) */
  language: string
  sort: number
  description: string
  recommend: boolean
  created_at?: string
  updated_at?: string
}

/** 克隆音色的表单值(音频文件单独传,不进这个对象) */
export interface VoiceCloneForm {
  name: string
  gender: string
  sex: number
  ttsType: string
  targetModel: string
  prefix: string
  /** 逗号分隔,如 zh,en */
  languageHints: string
  description: string
}

/** 文本合成请求(对应后端 service.TtsConvertRequest) */
export interface TtsConvertParams {
  text: string
  /** 本地登记册 id(纯数字)或远端 voice_id */
  voice: string
  /** bailian(默认)/ edge */
  provider?: string
  /** wav / mp3(默认 mp3);edge 只出 mp3 */
  format?: string
  /** 音量,如 "+0%" / "-20%" */
  volume?: string
  /** 语速,如 "+0%" / "+50%" */
  rate?: string
}
