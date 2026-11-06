/**
 * AsrService 测试 —— 路径 / 参数逐条对齐后端路由,并钉住:
 *   - 一句话识别走 multipart(FormData,Content-Type 不能手动写)
 *   - 实时会话的收窄协议(sentence / finished / failed 三种帧 + finish 指令)
 *   - 未登录开 WS 直接报错(token 是握手 query 的必选项)
 */
import { describe, expect, it, vi } from 'vitest'
import { AsrStreamSession, DynClient } from '../src'

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

function failEnvelope(message: string) {
  return new Response(JSON.stringify({ code: 50001, message, timestamp: 1 }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('asr.recognize(一句话识别)', () => {
  it('POST /biz/asr/recognize,multipart 字段逐个对齐(audio/format/sampleRate/languageHints)', async () => {
    const { client, calls } = makeClient(() => okResponse({ text: '你好世界' }))
    const audio = new Blob([new Uint8Array([1, 2, 3])], { type: 'audio/wav' })
    const text = await client.asr.recognize(audio, {
      format: 'wav',
      sampleRate: 16000,
      languageHints: ['zh', 'en'],
    })
    expect(text).toBe('你好世界')
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/biz/asr/recognize')
    expect(calls[0].init.method).toBe('POST')
    expect((calls[0].init.headers as Record<string, string>).Authorization).toBe('Bearer test-token')
    // ⚠️ Content-Type 不能手动设 —— boundary 要留给浏览器写
    expect((calls[0].init.headers as Record<string, string>)['Content-Type']).toBeUndefined()
    const form = calls[0].init.body as FormData
    expect(form).toBeInstanceOf(FormData)
    expect(form.get('format')).toBe('wav')
    expect(form.get('sampleRate')).toBe('16000')
    expect(form.get('languageHints')).toBe('zh,en')
    const file = form.get('audio') as File
    expect(file.name).toBe('audio.wav')
    expect(new Uint8Array(await file.arrayBuffer())).toEqual(new Uint8Array([1, 2, 3]))
  })

  it('选填参数不传就不进表单;失败信封原样抛', async () => {
    const { client, calls } = makeClient(() => okResponse({ text: '' }))
    await client.asr.recognize(new Blob([new Uint8Array([9])]))
    const form = calls[0].init.body as FormData
    expect(form.get('format')).toBeNull()
    expect(form.get('sampleRate')).toBeNull()
    expect(form.get('languageHints')).toBeNull()

    const { client: c2 } = makeClient(() => failEnvelope('识别失败'))
    await expect(c2.asr.recognize(new Blob([new Uint8Array([9])]))).rejects.toThrow(/识别失败/)
  })
})

describe('asr.streamURL / openStream(实时识别)', () => {
  it('URL:https→wss,token 走 access_token(浏览器 WS 带不了 Authorization 头)', () => {
    const { client } = makeClient(() => okResponse({}))
    expect(client.asr.streamURL({ format: 'pcm', sampleRate: 16000, languageHints: ['zh'] })).toBe(
      'wss://api.test/rapid-admin/v1/biz/asr/stream?access_token=test-token&format=pcm&sample_rate=16000&language_hints=zh',
    )
  })

  it('未登录直接抛错(静默拼出无 token 的 URL = 握手 401 排不了障)', () => {
    const client = new DynClient('https://api.test/rapid-admin/v1', {
      fetch: vi.fn() as any,
    })
    expect(() => client.asr.streamURL()).toThrow(/请先登录/)
  })

  it('会话:sentence 帧 → onSentence,终稿进 finalText,中间结果不混进去', () => {
    const { client } = makeClient(() => okResponse({}))
    const sentences: string[] = []
    const session = client.asr.openStream({
      wsFactory: () => new FakeWebSocket() as unknown as WebSocket,
    })
    session.onSentence = (s) => sentences.push(`${s.end ? '终' : '中'}:${s.text}`)
    const ws = (session as unknown as { ws: FakeWebSocket }).ws
    ws.emit('{"event":"sentence","text":"你","end":false}')
    ws.emit('{"event":"sentence","text":"你好","begin_time":0,"end_time":900,"end":true}')
    ws.emit('{"event":"sentence","text":"世界","end":false}')
    expect(sentences).toEqual(['中:你', '终:你好', '中:世界'])
    expect(session.fullText).toBe('你好世界') // 终稿 + 中间
    expect(session.finalText).toBe('你好')
  })

  it('会话:finished / failed 都算终态;断线未收尾报"连接中断"', () => {
    const { client } = makeClient(() => okResponse({}))
    const failed: string[] = []
    const finished: number[] = []
    const session = client.asr.openStream({
      wsFactory: () => new FakeWebSocket() as unknown as WebSocket,
    })
    session.onFinish = () => finished.push(1)
    session.onFailed = (m) => failed.push(m)
    const ws = (session as unknown as { ws: FakeWebSocket }).ws
    ws.onopen?.()
    ws.emit('{"event":"finished"}')
    expect(finished).toEqual([1])
    expect(session.done).toBe(true)
    // ⚠️ 终态即断 —— 客户端不关,服务端读侧 goroutine 就一直悬着
    expect(ws.readyState).toBe(FakeWebSocket.CLOSED)

    const session2 = client.asr.openStream({
      wsFactory: () => new FakeWebSocket() as unknown as WebSocket,
    })
    session2.onFailed = (m) => failed.push(m)
    const ws2 = (session2 as unknown as { ws: FakeWebSocket }).ws
    ws2.onopen?.()
    ws2.emit('{"event":"failed","message":"没配凭据"}')
    ws2.close()
    expect(failed).toEqual(['没配凭据']) // failed 已终态,close 不重复报

    const session3 = client.asr.openStream({
      wsFactory: () => new FakeWebSocket() as unknown as WebSocket,
    })
    session3.onFailed = (m) => failed.push(m)
    const ws3 = (session3 as unknown as { ws: FakeWebSocket }).ws
    ws3.onopen?.()
    ws3.close()
    expect(failed[1]).toContain('连接中断')
  })

  it('send 发 Binary、finish 发 {"action":"finish"}(协议收窄,没有别的合法指令)', () => {
    const { client } = makeClient(() => okResponse({}))
    const session = client.asr.openStream({
      wsFactory: () => new FakeWebSocket() as unknown as WebSocket,
    })
    const ws = (session as unknown as { ws: FakeWebSocket }).ws
    ws.readyState = FakeWebSocket.OPEN
    session.send(new Int16Array([1, 2, 3]))
    session.finish()
    expect(ws.sent).toEqual([JSON.stringify({ action: 'finish' })])
    expect(ws.sentBinary.length).toBe(1)
    // 关着的连接不抛,静默丢
    ws.readyState = FakeWebSocket.CLOSED
    expect(() => session.send(new Int16Array([9]))).not.toThrow()
  })
})

// ── 测试替身 ─────────────────────────────────────────────────────────

/** 最小 WebSocket 替身:事件靠 emit 手动触发,不做真连接 */
class FakeWebSocket {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  readyState = 0
  sent: string[] = []
  sentBinary: ArrayBufferView[] = []
  binaryType = ''
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null

  send(data: string | ArrayBuffer | ArrayBufferView) {
    if (typeof data === 'string') this.sent.push(data)
    else this.sentBinary.push(data as ArrayBufferView)
  }
  close() {
    this.readyState = 3
    this.onclose?.()
  }
  emit(json: string) {
    this.onmessage?.({ data: json })
  }
}
