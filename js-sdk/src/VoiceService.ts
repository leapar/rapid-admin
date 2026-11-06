/**
 * 语音服务 —— 音色列表 + 文本合成(整段 / 流式)
 *
 * # 路径与后端一一对应(router.go,勿改拼写)
 *
 *	GET  /rapid-admin/v1/biz/tts/voices          音色登记册分页(tts_voice:list)
 *	POST /rapid-admin/v1/biz/tts/convert         文本合成(tts:convert)
 *
 * # ⚠️ **convert 的响应不是统一信封**
 *
 * 合成成功回的是**裸音频字节**(audio/wav 或 audio/mpeg),不包 JSON ——
 * 把 wav 包进统一信封只会得到一坨 base64。所以这里用 `sendRaw` 拿原始
 * Response 自行判别;失败时后端仍回 JSON 信封(200 + code ≠ 0),
 * 按 Content-Type 区分,不让 JSON 冒充音频。
 */
import type { DynClient } from './Client'
import { ClientResponseError } from './ClientResponseError'
import type { RecordList, SendOptions } from './types'

/** TTS 合成通道 —— bailian(默认,走配置的合成端点)/ edge(微软朗读,免凭据) */
export type TtsProvider = 'bailian' | 'edge'

/** 整段合成的输出格式(edge 通道只出 mp3,format=wav 会被服务端显式拒绝) */
export type TtsFormat = 'wav' | 'mp3' | (string & {})

/** sys_tts_voice 一行(字段名是 wire 格式,原样透传) */
export interface TtsVoiceModel {
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
  /** 试听音频的对象 key(空 = 试听还没合成出来) */
  demo_key: string
  created_at?: string
  updated_at?: string
  [key: string]: unknown
}

/** 音色列表查询参数 */
export interface VoiceListOptions {
  page?: number
  size?: number
  /** 模糊匹配 name / voice_id */
  keyword?: string
  /** 按平台筛:bailian / edge */
  tts_type?: string
  signal?: AbortSignal
}

/** 合成参数(text + voice 必填) */
export interface SynthesizeOptions {
  /** 要读的文本 */
  text: string
  /** 音色 ID(取 list() 返回的 voice_id) */
  voice: string
  /** 不传 = 服务端按音色行的 tts_type 走默认通道 */
  provider?: TtsProvider
  /** 整段合成的输出格式,默认 wav */
  format?: TtsFormat
  /** 音量,如 "+0%" / "-20%" */
  volume?: string
  /** 语速,如 "+0%" / "+50%" */
  rate?: string
  signal?: AbortSignal
}

/** 流式合成参数(同一组,忽略 format —— 流式形态由 Content-Type 决定) */
export type SynthesizeStreamOptions = Omit<SynthesizeOptions, 'format'>

export class VoiceService {
  constructor(private client: DynClient) {}

  /**
   * 音色登记册分页(含 seed 内置音色与克隆出来的)。
   * ⚠️ 合成请传行里的 **voice_id**,不是 name(运营改过名时 name 不再是 ID)。
   */
  async list(options: VoiceListOptions = {}): Promise<RecordList<TtsVoiceModel>> {
    const query: Record<string, string> = {}
    const set = (key: string, value: unknown) => {
      if (value !== undefined && value !== null && value !== '') {
        query[key] = String(value)
      }
    }
    set('page', options.page)
    set('size', options.size)
    set('keyword', options.keyword)
    set('tts_type', options.tts_type)
    return this.client.send('/biz/tts/voices', {
      method: 'GET',
      query,
      signal: options.signal,
    })
  }

  /**
   * 整段合成:等端点合成完,一次性回完整音频 Blob(下载 / 存档用)。
   *
   * ⚠️ **失败长得很像成功** —— 部分网关失败回 200 + JSON 信封,
   * 这里按 Content-Type 拦下:JSON 一律当错误抛,不把一段 JSON 存成 .mp3。
   */
  async synthesize(options: SynthesizeOptions): Promise<Blob> {
    const res = await this.convert({ ...options, stream: false })
    const contentType = res.headers.get('Content-Type') ?? ''
    if (isJSONContentType(contentType)) {
      throw await envelopeError(res, '合成失败(服务端返回了 JSON 而非音频)')
    }
    const buf = await res.arrayBuffer()
    return new Blob([buf], { type: contentType || 'audio/mpeg' })
  }

  /**
   * 流式合成:返回音频字节流(边合成边收,首包即首音频帧)。
   *
   * 用法:`const stream = await client.voices.synthesizeStream({...})`,
   * 把 stream 喂给 MediaSource / 逐块解 MP3 播放;只想拿完整文件的话
   * 用 `synthesize()` 更省事。
   */
  async synthesizeStream(options: SynthesizeStreamOptions): Promise<ReadableStream<Uint8Array>> {
    const res = await this.convert({ ...options, stream: true })
    const contentType = res.headers.get('Content-Type') ?? ''
    if (isJSONContentType(contentType)) {
      throw await envelopeError(res, '流式合成失败(服务端返回了 JSON 而非音频)')
    }
    if (!res.body) {
      throw new ClientResponseError(res.url, res.status, 0, '响应没有可读的音频流', null)
    }
    return res.body
  }

  /** convert 底层:POST /biz/tts/convert,裸响应交出去(信封解析在两个入口各自做) */
  private convert(
    options: SynthesizeOptions & { stream: boolean },
  ): Promise<Response> {
    const sendOptions: SendOptions = {
      method: 'POST',
      body: {
        text: options.text,
        voice: options.voice,
        stream: options.stream,
        ...(options.provider ? { provider: options.provider } : {}),
        ...(options.format ? { format: options.format } : {}),
        ...(options.volume ? { volume: options.volume } : {}),
        ...(options.rate ? { rate: options.rate } : {}),
      },
      signal: options.signal,
    }
    return this.client.sendRaw('/biz/tts/convert', sendOptions)
  }
}

function isJSONContentType(contentType: string): boolean {
  return contentType.toLowerCase().includes('application/json')
}

/** 从"假成功"的 JSON 响应里抠出信封错误信息 */
async function envelopeError(res: Response, fallback: string): Promise<ClientResponseError> {
  let body: unknown = null
  let message = fallback
  try {
    body = await res.json()
  } catch {
    // JSON 解析失败 —— 保 fallback 就够
  }
  if (typeof body === 'object' && body !== null && 'message' in body) {
    const m = (body as { message?: unknown }).message
    if (typeof m === 'string' && m !== '') {
      message = m
    }
  }
  return new ClientResponseError(res.url, res.status, -1, message, body)
}
