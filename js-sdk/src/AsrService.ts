/**
 * 语音识别服务(ASR)—— 一句话识别 + 实时识别(WS)+ 麦克风采集链
 *
 * # 路径与后端一一对应(router.go,勿改拼写)
 *
 *	POST /rapid-admin/v1/biz/asr/recognize  一句话识别(multipart,asr:recognize)
 *	GET  /rapid-admin/v1/biz/asr/stream     实时识别 WS 代理(asr:stream,handler 内判)
 *
 * # ⚠️ **实时识别的协议是收窄过的**(服务端把百炼 WS 包了一层)
 *
 *	浏览器 → 服务端:Binary 帧 = 音频块(pcm 16bit 小端);Text 帧 {"action":"finish"} = 说完了
 *	服务端 → 浏览器:{"event":"sentence", text, begin_time, end_time, end} × N
 *	              → {"event":"finished"} 或 {"event":"failed","message":"..."}
 *
 * api_key 永远不出服务端;参数(format / sample_rate / language_hints)在
 * WS 握手 query 里定死,会话中途不可换 —— 换参数 = 重开会话。
 *
 * # 麦克风采集链(自 ai-im 的 Asr.js / AudioStreamRecognizer.js 移植)
 *
 * getUserMedia → AudioWorklet(2048 采样缓冲)→ float32 → int16 PCM →
 * 线性插值重采样到 16k → Binary 帧发给会话。集成方只需 `startMic()` /
 * `mic.stop()`,不用碰任何音频 API:
 *
 * ```ts
 * const mic = await client.asr.startMic({
 *   onSentence: s => console.log(s.end ? '终稿' : '中间', s.text),
 * })
 * const text = await mic.stop()   // 停采集 + finish + 等终稿,返回全文
 * ```
 */
import type { DynClient } from './Client'

/** 一句识别结果(end=false 是中间结果(灰字),true 是终稿) */
export interface AsrSentence {
  text: string
  /** 毫秒,相对音频起点 */
  begin_time: number
  end_time: number
  end: boolean
}

/** 一句话识别选项 */
export interface AsrRecognizeOptions {
  /** pcm / wav / mp3 / aac / amr / opus / speex;不传 = 服务端按文件名扩展猜,猜不出当 pcm */
  format?: string
  /** 默认 16000 */
  sampleRate?: number
  /** 语言提示,如 ['zh', 'en'] */
  languageHints?: string[]
  /** 文件名(影响服务端的扩展名嗅探;File 自带 name,不传也行) */
  filename?: string
  signal?: AbortSignal
}

/** 实时会话选项(参数在握手 query 里定死,会话中途不可换) */
export interface AsrStreamOptions {
  /** 默认 pcm(麦克风采集链固定发 pcm) */
  format?: string
  /** 默认 16000 */
  sampleRate?: number
  languageHints?: string[]
  /**
   * WebSocket 工厂(测试注入用;默认 new WebSocket(url))。
   * ⚠️ 传了它就完全接管连接,URL 仍然会先拼好给你。
   */
  wsFactory?: (url: string) => WebSocket
}

/** 麦克风识别选项 = 会话选项 + 采集配置与回调 */
export interface AsrMicOptions extends AsrStreamOptions {
  /** getUserMedia 的音频约束;默认开回声消除 + 噪声抑制 */
  audio?: MediaTrackConstraints
  /** stop() 等终稿的超时毫秒,默认 5000 */
  finalTimeoutMs?: number
  onSentence?: (sentence: AsrSentence) => void
  onFinish?: () => void
  onFailed?: (message: string) => void
}

/** 实时会话 —— 包着一条 WebSocket,协议收窄成"发音频 / 收句子" */
export class AsrStreamSession {
  onSentence?: (sentence: AsrSentence) => void
  onFinish?: () => void
  onFailed?: (message: string) => void
  onClose?: () => void

  private _finalText = ''
  private _interim = ''
  private _done = false
  private _opened = false
  private _closedByUser = false

  constructor(private readonly ws: WebSocket) {
    ws.binaryType = 'arraybuffer'
    ws.onopen = () => {
      this._opened = true
    }
    ws.onmessage = (ev: MessageEvent) => {
      // 服务端只发 Text 帧;Binary 是实现细节,不该出现
      if (typeof ev.data !== 'string') return
      let msg: { event?: string; text?: string; begin_time?: number; end_time?: number; end?: boolean; message?: string }
      try {
        msg = JSON.parse(ev.data)
      } catch {
        return
      }
      if (msg.event === 'sentence') {
        const text = msg.text ?? ''
        if (msg.end) {
          this._finalText += text
          this._interim = ''
        } else {
          this._interim = text
        }
        this.onSentence?.({
          text,
          begin_time: msg.begin_time ?? 0,
          end_time: msg.end_time ?? 0,
          end: !!msg.end,
        })
      } else if (msg.event === 'finished') {
        this._done = true
        this.onFinish?.()
      } else if (msg.event === 'failed') {
        this._done = true
        this.onFailed?.(msg.message ?? '识别失败')
      }
    }
    ws.onclose = () => {
      if (!this._done && !this._closedByUser) {
        this._done = true
        // 升级前被拒(没权限 / token 过期)和说一半断线,报错不同
        this.onFailed?.(
          this._opened ? '连接中断,识别未正常收尾' : '连接失败(可能没有 asr:stream 权限,或 token 已过期)',
        )
      }
      this.onClose?.()
    }
  }

  /** 终稿聚合 + 当前中间结果(实时展示用) */
  get fullText(): string {
    return this._finalText + this._interim
  }

  /** 只有终稿(整段识别文本) */
  get finalText(): string {
    return this._finalText
  }

  /** 终态已到(finished / failed / 连接断了) */
  get done(): boolean {
    return this._done
  }

  get connected(): boolean {
    return this.ws.readyState === WebSocket.OPEN
  }

  /** 发一块音频(Binary 帧)—— pcm 16bit 小端,采样率在握手时定死 */
  send(chunk: BufferSource): void {
    if (this.ws.readyState !== WebSocket.OPEN) {
      return // 断线后静默丢帧 —— 终态回调里已经报过错,这里再抛就是噪音
    }
    this.ws.send(chunk)
  }

  /** 音频发完了,等服务端收尾(服务端会先吐完剩余句子再回 finished) */
  finish(): void {
    if (this.ws.readyState !== WebSocket.OPEN) return
    this.ws.send(JSON.stringify({ action: 'finish' }))
  }

  close(): void {
    this._closedByUser = true
    try {
      this.ws.close()
    } catch {
      // 已断的 socket close 再抛错没有意义
    }
  }
}

/** 麦克风识别会话 —— 采集链 + 一条实时会话,stop() 一步到位 */
export class AsrMicSession {
  private ctx?: AudioContext
  private awNode?: AudioWorkletNode
  private srcNode?: MediaStreamAudioSourceNode
  private track?: MediaStreamTrack
  private resampleOffset = 0
  private stopped = false

  /** 底下的实时会话(高级用法直接操作它) */
  readonly session: AsrStreamSession

  constructor(session: AsrStreamSession) {
    this.session = session
  }

  /** 接上麦克风轨道开始采集(由 AsrService.startMic 调,集成方不用碰) */
  async start(track: MediaStreamTrack): Promise<void> {
    this.track = track
    const AC: typeof AudioContext | undefined =
      globalThis.AudioContext ?? (globalThis as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
    if (!AC) throw new Error('AsrMicSession: 当前环境没有 AudioContext')
    if (!globalThis.AudioWorkletNode) throw new Error('AsrMicSession: 当前环境不支持 AudioWorklet')

    const ctx = new AC()
    this.ctx = ctx
    if (ctx.state === 'suspended') {
      await ctx.resume() // 浏览器自动播放策略:用户手势外的 context 是挂起的
    }
    await ctx.audioWorklet.addModule(micWorkletURL())

    this.awNode = new AudioWorkletNode(ctx, MIC_PROCESSOR_NAME)
    this.srcNode = ctx.createMediaStreamSource(new MediaStream([track]))
    this.srcNode.connect(this.awNode)
    // ⚠️ worklet 必须连进渲染图 process() 才会被调;但 RecProc 这类采集器
    // 不往输出口写任何数据(输出静音),连 destination 不会回声
    this.awNode.connect(ctx.destination)
    this.awNode.port.onmessage = (msg: MessageEvent) => {
      this.onAudio(msg.data.val as Float32Array)
    }
  }

  private onAudio(f32: Float32Array): void {
    if (this.stopped || !this.ctx) return
    const pcm = float32ToInt16(f32)
    // 采集率高于 16k(一般 48k)→ 重采样到 16k;本身 ≤16k → 原样发,
    // 握手时的 sample_rate 与之对齐(见 startMic 里的 target 计算)
    const rate = this.ctx.sampleRate
    const target = this.sessionSampleRate
    let data: number[]
    if (rate > target) {
      const r = resample(pcm, rate, target, this.resampleOffset)
      this.resampleOffset = r.offset
      data = r.data
    } else {
      data = Array.from(pcm)
    }
    if (data.length > 0) {
      this.session.send(new Int16Array(data))
    }
  }

  /** 握手用的采样率(startMic 里算好塞进 sessionOptions) */
  sessionSampleRate = 16000

  /**
   * 停止采集 + finish + 等终稿,返回全文。
   *
   * ⚠️ 服务端在 finished 之前会把剩余句子吐完 —— 所以这里**等终态**再返回,
   * 超时(finalTimeoutMs,默认 5s)就用手头已有的文本兜底,不挂死。
   */
  async stop(finalTimeoutMs = 5000): Promise<string> {
    if (this.stopped) return this.session.finalText
    this.stopped = true

    this.track?.stop()
    this.srcNode?.disconnect()
    if (this.awNode) {
      try {
        this.awNode.port.close()
      } catch {
        // context 先关时 port 已死
      }
      this.awNode.disconnect()
    }
    await this.ctx?.close().catch(() => {})

    this.session.finish()
    const deadline = Date.now() + finalTimeoutMs
    while (!this.session.done && Date.now() < deadline) {
      await sleep(50)
    }
    return this.session.finalText
  }

  /** 立即断开,不等终稿(用户取消场景) */
  abort(): void {
    this.stopped = true
    this.track?.stop()
    this.session.close()
  }
}

export class AsrService {
  constructor(private client: DynClient) {}

  /**
   * 一句话识别:整段短音频(语义上 <60s)→ 纯文本。
   * 长录音 / 边说边出字用 `openStream()` / `startMic()`。
   */
  async recognize(audio: Blob | File | ArrayBuffer | Uint8Array, options: AsrRecognizeOptions = {}): Promise<string> {
    const form = new FormData()
    const blob = toBlob(audio)
    const name =
      options.filename ?? (isFile(audio) ? audio.name : `audio.${options.format ?? 'pcm'}`)
    form.append('audio', blob, name)
    if (options.format) form.append('format', options.format)
    if (options.sampleRate) form.append('sampleRate', String(options.sampleRate))
    if (options.languageHints?.length) form.append('languageHints', options.languageHints.join(','))

    const data = await this.client.send<{ text: string }>('/biz/asr/recognize', {
      method: 'POST',
      body: form,
      signal: options.signal,
    })
    return data?.text ?? ''
  }

  /**
   * 实时会话的 WS 地址(高级用法 —— 自带连接层时直接拿它连):
   * `wss://host/rapid-admin/v1/biz/asr/stream?access_token=...&format=...`
   *
   * ⚠️ 浏览器 WebSocket 握手带不了 Authorization 头,token 走 query;
   * 未登录直接抛错。
   */
  streamURL(options: AsrStreamOptions = {}): string {
    const token = this.client.authStore.token
    if (!token) {
      throw new Error('AsrService.streamURL: 请先登录(先调 authWithPassword,token 为空连不上)')
    }
    const url = new URL(`${this.client.baseURL.replace(/^http/, 'ws')}/biz/asr/stream`)
    url.searchParams.set('access_token', token)
    if (options.format) url.searchParams.set('format', options.format)
    if (options.sampleRate) url.searchParams.set('sample_rate', String(options.sampleRate))
    if (options.languageHints?.length) url.searchParams.set('language_hints', options.languageHints.join(','))
    return url.toString()
  }

  /** 开一条实时识别会话(连接异步建立,结果走 session 回调) */
  openStream(options: AsrStreamOptions = {}): AsrStreamSession {
    const url = this.streamURL(options)
    const ws = options.wsFactory ? options.wsFactory(url) : new WebSocket(url)
    return new AsrStreamSession(ws)
  }

  /**
   * 麦克风识别:getUserMedia → 采集 → 16k PCM → 实时会话,一条龙。
   *
   * ```ts
   * const mic = await client.asr.startMic({ onSentence: s => render(s) })
   * const text = await mic.stop()   // 按钮松开时调
   * ```
   *
   * ⚠️ 必须在用户手势里调(浏览器自动播放策略会挂起 AudioContext);
   * 失败会自动收掉轨道和会话,不留下悬空的麦克风指示灯。
   */
  async startMic(options: AsrMicOptions = {}): Promise<AsrMicSession> {
    const md = (globalThis as { navigator?: Navigator }).navigator?.mediaDevices
    if (!md?.getUserMedia) {
      throw new Error('AsrService.startMic: 当前环境不支持麦克风采集(navigator.mediaDevices 缺失;非 https 部署也没有它)')
    }
    const stream = await md.getUserMedia({
      audio: options.audio ?? { echoCancellation: true, noiseSuppression: true },
    })
    const session = this.openStream(options)
    const mic = new AsrMicSession(session)
    if (options.onSentence) session.onSentence = options.onSentence
    if (options.onFinish) session.onFinish = options.onFinish
    if (options.onFailed) session.onFailed = options.onFailed
    try {
      // 采样率握手前定死:采集 context ≤16k 就按它的来,否则统一 16k
      const probe = new AudioContext()
      const target = options.sampleRate ?? (probe.sampleRate <= 16000 ? probe.sampleRate : 16000)
      await probe.close()
      mic.sessionSampleRate = target
      await mic.start(stream.getAudioTracks()[0])
    } catch (err) {
      stream.getTracks().forEach((t) => t.stop())
      session.close()
      throw err
    }
    return mic
  }
}

// ── 内部工具 ──────────────────────────────────────────────────────────

const MIC_PROCESSOR_NAME = 'rapid-admin-asr-capture'

/**
 * AudioWorklet 采集器(内联 base64 模块,免掉独立 .js 文件与构建配置):
 * 2048 采样一攒满就 postMessage 出去,余数进下一轮 ——
 * 移植自 ai-im AudioStreamRecognizer.clazzUrl 的 RecProc,只改了处理器名。
 */
function micWorkletURL(): string {
  const code = `
    class ${MIC_PROCESSOR_NAME} extends AudioWorkletProcessor {
      constructor() {
        super();
        this.size = 2048;
        this.buffer = new Float32Array(this.size * 2);
        this.pos = 0;
        const scope = this;
        this.port.onmessage = function (e) {
          if (e.data.kill) scope.kill = true;
        };
      }
      process(input) {
        let buffer = this.buffer;
        let pos = this.pos;
        input = (input[0] || [])[0] || [];
        if (input.length) {
          buffer.set(input, pos);
          pos += input.length;
          const len = ~~(pos / this.size) * this.size;
          if (len) {
            this.port.postMessage({ val: buffer.slice(0, len) });
            const more = buffer.subarray(len, pos);
            buffer = new Float32Array(this.size * 2);
            buffer.set(more);
            pos = more.length;
            this.buffer = buffer;
          }
          this.pos = pos;
        }
        return !this.kill;
      }
    }
    try { registerProcessor('${MIC_PROCESSOR_NAME}', ${MIC_PROCESSOR_NAME}); } catch (e) {}
  `
  return 'data:text/javascript;base64,' + btoa(code)
}

/** float32 [-1,1] → int16 PCM(小端,直接 Int16Array 就是小端) */
function float32ToInt16(input: Float32Array): Int16Array {
  const out = new Int16Array(input.length)
  for (let i = 0; i < input.length; i++) {
    const s = Math.max(-1, Math.min(1, input[i]))
    out[i] = s < 0 ? s * 0x8000 : s * 0x7fff
  }
  return out
}

/**
 * 线性插值重采样(fromRate → toRate,toRate < fromRate 才真的采)。
 * 跨块连续:offset 是上一块没消费完的位置(0..<step),接着采不留缝。
 * (移植自 ai-im AudioStreamRecognizer.reSampleData;frameSize 对齐那套
 * 是给 mp3 编码用的,PCM 直发用不上,删了。)
 */
function resample(
  pcm: Int16Array,
  fromRate: number,
  toRate: number,
  offset: number,
): { data: number[]; offset: number } {
  let step = fromRate / toRate
  if (step <= 1) step = 1
  const out: number[] = []
  let i = offset
  while (i < pcm.length) {
    const before = Math.floor(i)
    const after = Math.ceil(i)
    const at = i - before
    const b = pcm[before]
    const a = after < pcm.length ? pcm[after] : b
    out.push(b + (a - b) * at)
    i += step
  }
  return { data: out, offset: i - pcm.length }
}

function toBlob(audio: Blob | ArrayBuffer | Uint8Array): Blob {
  if (typeof Blob !== 'undefined' && audio instanceof Blob) return audio
  return new Blob([audio as ArrayBuffer])
}

function isFile(audio: unknown): audio is File {
  return typeof File !== 'undefined' && audio instanceof File
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
