/**
 * 请求 ID 生成 —— 与后端 §10.4 协议一致
 * 后端会用这个 X-Request-Id 串到日志,前端出错时给后端可以秒查日志
 *
 * ⚠️ **与后端的契约(server/internal/api/middleware/trace.go 的 sanitizeRequestID)**:
 *   后端会**消毒**客户端自带的 X-Request-Id / X-Trace-Id:
 *   长度必须 1..64(64 = 审计表列宽),字符集必须 `[A-Za-z0-9._:-]`。
 *   **不合规就整个丢弃、改用后端自己生成的** ⇒ 串日志的链路直接断掉。
 *   所以这里生成的 ID 必须**天然合规**(回归网里直接照抄后端那套规则对拍)。
 */

/**
 * 生成 UUID v4
 *
 * ⚠️ **安全上下文限制**(MDN:Crypto.randomUUID —— Secure context: Yes):
 *   `crypto.randomUUID` 只在 **HTTPS / localhost** 下存在。而本仓 vite dev
 *   server 配的是 `server.host: '0.0.0.0'` ⇒ 同事从
 *   `http://<局域网IP>:5173` 打开就是**非安全上下文** ⇒ 这条路走不通。
 *
 *   `crypto.getRandomValues` **不受安全上下文限制**(所有环境都有)⇒ 降级用它,
 *   而不是 `Math.random` —— 后者不是密码学安全的随机源,而这个 ID 要当
 *   **幂等键**用(后端按它去重,见 server/internal/api/middleware/idempotency.go)。
 *
 *   顺带的好处:**不管从哪个地址打开,生成的 ID 形状一致**(都是 UUID v4),
 *   日志里不会出现"两种格式的 request_id"。
 */
export function genRequestId(): string {
  // 1) 标准 UUID v4(需要安全上下文)
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  // 2) getRandomValues —— 非安全上下文也有,自己拼一个 v4
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    bytes[6] = (bytes[6]! & 0x0f) | 0x40 // version = 4
    bytes[8] = (bytes[8]! & 0x3f) | 0x80 // variant = 10xx
    const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  }
  // 3) 极老环境兜底:时间戳 + Math.random(字符集仍然合规)
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 14)}`
}

/** 生成 Idempotency-Key(写操作用,防重复提交) */
export function genIdempotencyKey(): string {
  return genRequestId()
}
