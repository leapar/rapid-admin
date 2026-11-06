/**
 * VoiceService 测试 —— 路径 / 方法 / 请求体逐条对齐后端路由,
 * 并钉住"响应不是统一信封"的合成形态(裸音频字节,JSON = 失败)
 */
import { describe, expect, it, vi } from 'vitest'
import { DynClient } from '../src'

type FetchHandler = (url: string, init: RequestInit) => Response

function makeClient(handler: FetchHandler) {
  const calls: Array<{ url: string; init: RequestInit }> = []
  const fetchImpl = vi.fn(async (url: any, init: any) => {
    calls.push({ url: String(url), init })
    return handler(String(url), init)
  })
  const client = new DynClient('https://api.test/rapid-admin/v1', { fetch: fetchImpl as any })
  client.authStore.save('test-token')
  return { client, calls }
}

function okResponse(data: unknown) {
  return new Response(JSON.stringify({ code: 0, data, message: 'ok', timestamp: 1 }), {
    status: 200,
  })
}

function audioResponse(contentType: string, bytes: Uint8Array) {
  return new Response(bytes.buffer as ArrayBuffer, {
    status: 200,
    headers: { 'Content-Type': contentType },
  })
}

function failEnvelope(message: string) {
  return new Response(JSON.stringify({ code: 50001, message, timestamp: 1 }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('voices.list', () => {
  it('GET /biz/tts/voices,查询参数空值掐掉', async () => {
    const { client, calls } = makeClient(() => okResponse({ list: [], total: 0, page: 1, size: 20 }))
    await client.voices.list({ page: 2, size: 50, keyword: '晓晓', tts_type: 'edge' })
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/biz/tts/voices?page=2&size=50&keyword=%E6%99%93%E6%99%93&tts_type=edge')
    expect(calls[0].init.method).toBe('GET')
    expect((calls[0].init.headers as Record<string, string>).Authorization).toBe('Bearer test-token')
  })

  it('返回信封里的分页数据', async () => {
    const row = { id: '1', name: '晓晓', voice_id: 'zh-CN-XiaoxiaoNeural', tts_type: 'edge' }
    const { client } = makeClient(() => okResponse({ list: [row], total: 1, page: 1, size: 20 }))
    const res = await client.voices.list()
    expect(res.list[0].voice_id).toBe('zh-CN-XiaoxiaoNeural')
    expect(res.total).toBe(1)
  })
})

describe('voices.synthesize(整段)', () => {
  it('POST /biz/tts/convert,body 带 stream:false;音频字节回 Blob', async () => {
    const bytes = new Uint8Array([1, 2, 3, 4])
    const { client, calls } = makeClient(() => audioResponse('audio/wav', bytes))
    const blob = await client.voices.synthesize({
      text: '你好', voice: 'zh-CN-XiaoxiaoNeural', provider: 'edge', format: 'wav', volume: '+0%', rate: '-10%',
    })
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/biz/tts/convert')
    expect(calls[0].init.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init.body))).toEqual({
      text: '你好',
      voice: 'zh-CN-XiaoxiaoNeural',
      stream: false,
      provider: 'edge',
      format: 'wav',
      volume: '+0%',
      rate: '-10%',
    })
    expect(blob).toBeInstanceOf(Blob)
    expect(blob.type).toBe('audio/wav')
    expect(new Uint8Array(await blob.arrayBuffer())).toEqual(bytes)
  })

  it('可选参数不传就不进 body(默认通道由服务端决定)', async () => {
    const { client, calls } = makeClient(() => audioResponse('audio/mpeg', new Uint8Array([9])))
    await client.voices.synthesize({ text: 'hi', voice: 'longxiaochun_v3' })
    expect(JSON.parse(String(calls[0].init.body))).toEqual({ text: 'hi', voice: 'longxiaochun_v3', stream: false })
  })

  it('⚠️ 200 + JSON 信封 = 失败,不让 JSON 冒充音频', async () => {
    const { client } = makeClient(() => failEnvelope('合成端点未装配'))
    await expect(
      client.voices.synthesize({ text: 'hi', voice: 'x' }),
    ).rejects.toThrow(/合成端点未装配/)
  })
})

describe('voices.synthesizeStream(流式)', () => {
  it('body 带 stream:true,返回可读的音频流', async () => {
    const { client, calls } = makeClient(() => audioResponse('audio/mpeg', new Uint8Array([7, 8])))
    const stream = await client.voices.synthesizeStream({ text: 'hi', voice: 'v1' })
    expect(JSON.parse(String(calls[0].init.body))).toEqual({ text: 'hi', voice: 'v1', stream: true })
    const reader = stream.getReader()
    const first = await reader.read()
    expect([...first.value!]).toEqual([7, 8])
    reader.releaseLock()
  })

  it('200 + JSON = 失败', async () => {
    const { client } = makeClient(() => failEnvelope('参数非法'))
    await expect(
      client.voices.synthesizeStream({ text: 'hi', voice: 'x' }),
    ).rejects.toThrow(/参数非法/)
  })
})
