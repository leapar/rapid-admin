/**
 * 语音 API —— 音色登记册 + 克隆台账 + 文本合成
 * 2026-11-06 新建
 *
 * 路径: 后端 /rapid-admin/v1/biz/tts/*(登记册 + 合成)与
 *       /rapid-admin/v1/biz/voice-clones/*(克隆台账),router.go 的 biz 组。
 *
 * ⚠️ **URL 一律内联在 http.<verb>() 的第一个实参里** ——
 *   `web/src/api/__tests__/api-route-parity.test.ts` 靠扫 `http.<verb>(...)`
 *   的**第一个实参是不是字面量/模板串**来核对路径打不打得通后端路由。
 *
 * ⚠️ **convert 的成功响应不是统一信封** —— 后端直接回裸音频字节
 *   (audio/wav 或 audio/mpeg),把 wav 包进 JSON 信封只会得到一坨 base64。
 *   所以这里用 `instance`(responseType: 'blob')自己接;失败时后端仍是
 *   JSON 信封(blob.type = application/json),抠出 message 抛掉,
 *   **不让一段 JSON 冒充音频存下去**。
 */
import { http, instance } from './client'
import { getBaseURL } from './baseURL'
import type { ApiResponse, PageResponse } from '@/types/api'
import type { TtsVoice, TtsVoiceForm, VoiceClone, VoiceCloneForm, TtsConvertParams } from '@/types/voice'

/** 登记册 / 台账列表查询(与后端 handler 的 c.Query 对齐) */
export interface VoiceListQuery {
  page?: number
  size?: number
  keyword?: string
  tts_type?: string
  status?: string
  [key: string]: string | number | undefined
}

/** 从"假成功"的 JSON blob 里抠出信封错误并抛掉 */
async function assertAudioBlob(blob: Blob): Promise<Blob> {
  if (!blob.type.toLowerCase().includes('application/json')) {
    return blob
  }
  let message = '合成失败'
  try {
    const env = JSON.parse(await blob.text()) as { message?: unknown }
    if (typeof env.message === 'string' && env.message !== '') {
      message = env.message
    }
  } catch {
    // JSON 解析失败 —— 保兜底文案就够
  }
  throw new Error(message)
}

/** 整段合成 → 完整音频 Blob(试听 / 下载) */
export async function convertOnce(params: TtsConvertParams): Promise<Blob> {
  const resp = await instance.post<Blob>('/biz/tts/convert', { ...params, stream: false }, {
    responseType: 'blob',
  })
  return assertAudioBlob(resp.data)
}

/**
 * 流式合成 → 逐块回调,收完返回完整 Blob。
 *
 * ⚠️ **流式的意义在"边合成边收"** —— 这里用 fetch + ReadableStream
 *   逐块喂给 onChunk(调用方可以拿字节计数画进度、喂播放器);
   * 想自己接管流(真·边收边播)就直接用 convertStream,别用这个。
 */
export async function convertStream(
  params: TtsConvertParams,
  onChunk?: (bytes: Uint8Array, received: number) => void,
): Promise<Blob> {
  const resp = await convertStreamRaw(params)
  const contentType = resp.headers.get('Content-Type') ?? ''
  if (contentType.toLowerCase().includes('application/json')) {
    const env = (await resp.json().catch(() => null)) as { message?: string } | null
    throw new Error(env?.message || '流式合成失败')
  }
  if (!resp.body) {
    throw new Error('流式合成失败:响应没有可读的音频流')
  }
  const reader = resp.body.getReader()
  const chunks: BlobPart[] = []
  let received = 0
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    if (value) {
      chunks.push(value.slice().buffer as ArrayBuffer)
      received += value.byteLength
      onChunk?.(value, received)
    }
  }
  return new Blob(chunks, { type: contentType || 'audio/mpeg' })
}

/** 流式合成的原始 Response(给想接管流的调用方) */
export async function convertStreamRaw(params: TtsConvertParams): Promise<Response> {
  const { getToken } = await import('@/stores/user')
  const headers: Record<string, string> = { 'Content-Type': 'application/json' }
  const token = getToken()
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }
  // ⚠️ fetch 不走 axios 实例,baseURL 得自己拼(getBaseURL 与 client.ts 同源)
  return fetch(`${getBaseURL()}/biz/tts/convert`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ ...params, stream: true }),
  })
}

export const voiceApi = {
  /** 音色登记册分页 · GET /biz/tts/voices */
  listVoices: (params?: VoiceListQuery) => http.get<PageResponse<TtsVoice>>('/biz/tts/voices', params),

  /** 单条 · GET /biz/tts/voices/:id */
  getVoice: (id: string) => http.get<TtsVoice>(`/biz/tts/voices/${id}`),

  /**
   * 添加音色 · POST /biz/tts/voices
   * 创建成功后后端**异步**合成试听音频(demo_key)—— 失败只少一个试听文件
   */
  createVoice: (body: TtsVoiceForm) => http.post<{ id: string }>('/biz/tts/voices', body),

  /**
   * 编辑音色 · PATCH /biz/tts/voices/:id
   * ⚠️ voice_id / tts_type **不可改**(换平台/换音色应删了重建 —— 就地改会让
   * 试听与新音色对不上),编辑表单里这两项只读
   */
  updateVoice: (id: string, body: TtsVoiceForm) => http.patch(`/biz/tts/voices/${id}`, body),

  /** 删除 · DELETE /biz/tts/voices/:id */
  deleteVoice: (id: string) => http.delete(`/biz/tts/voices/${id}`),

  /** 批量删除 · POST /biz/tts/voices/batch-delete */
  batchDeleteVoices: (ids: string[]) => http.post('/biz/tts/voices/batch-delete', { ids }),

  /** 克隆台账分页 · GET /biz/voice-clones */
  listClones: (params?: VoiceListQuery) => http.get<PageResponse<VoiceClone>>('/biz/voice-clones', params),

  /** 克隆音色(multipart)· POST /biz/voice-clones */
  cloneVoice: async (form: VoiceCloneForm, audio: File): Promise<VoiceClone> => {
    const form_ = new FormData()
    form_.append('audio', audio)
    form_.append('name', form.name)
    form_.append('gender', form.gender)
    form_.append('sex', String(form.sex))
    form_.append('ttsType', form.ttsType)
    if (form.targetModel.trim()) form_.append('targetModel', form.targetModel.trim())
    if (form.prefix.trim()) form_.append('prefix', form.prefix.trim())
    if (form.languageHints.trim()) form_.append('languageHints', form.languageHints.trim())
    form_.append('description', form.description)
    const resp = await instance.post<ApiResponse<VoiceClone>>('/biz/voice-clones', form_)
    return resp.data.data
  },

  /** 刷新克隆状态(DEPLOYING → OK / UNDEPLOYED)· POST /biz/voice-clones/:id/refresh */
  refreshClone: (id: string) => http.post<VoiceClone>(`/biz/voice-clones/${id}/refresh`),

  /** 删除克隆记录(远端音色 + 参考音频尽力删,失败不阻断)· DELETE /biz/voice-clones/:id */
  deleteClone: (id: string) => http.delete(`/biz/voice-clones/${id}`),
}
