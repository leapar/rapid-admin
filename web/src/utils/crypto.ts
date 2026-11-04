/**
 * 前端加密工具
 *
 * 用途:
 * 1. 登录密码传输前摘要(配合后端 bcrypt 比对)
 *
 * ⚠️ **前后端契约(改一处就要改两处)**:
 *   前端:`preHashPassword(明文)` = `sha256("rapid-admin::" + 明文)` → 64 位小写 hex
 *   后端:库里存 `bcrypt(那个 hex)`,登录时拿前端发来的 hex **直接** bcrypt 比对,
 *         **不再二次 hash**(server/internal/service/auth_impl.go:143-144)。
 *   改 salt 或改算法 = 全员登不进去,而症状只有"账号或密码错误"——
 *   **看不出真因**(这个坑踩过:见 memory 的"超管密码的两个历史约定")。
 *
 * ⚠️ **为什么必须有纯 JS 兜底(2026-10-29 M110)**:
 *   `crypto.subtle` **只在安全上下文可用**(MDN:SubtleCrypto — Secure context:
 *   HTTPS / localhost / file://)。而本仓 vite dev server 配的是
 *   `server.host: '0.0.0.0'` ⇒ 同事从 `http://<局域网IP>:5173` 打开就是
 *   **非安全上下文** ⇒ `crypto.subtle` 是 `undefined`。
 *   此前这里"降级"到一个自制的 `fallbackHash()` —— **那不是 SHA-256**
 *   (一个 64 位级别的自造哈希,输出还不是 64 位 hex)。后端拿它去 bcrypt 比对
 *   **必然失败** ⇒ **登录永远报"账号或密码错误"**,而且错误信息完全指不到真因。
 *   现在降级路径也是**真 SHA-256**,与 WebCrypto **逐字节相同**
 *   (回归网 `utils/__tests__/crypto.test.ts`:NIST 标准向量 + 与 WebCrypto 逐输入对拍)。
 *
 * 注意:
 * - 前端任何"加密"对攻击者来说都不是绝对安全的
 * - 真正的安全靠 HTTPS + 后端校验
 * - 这里只做一层摘要,降低明文密码在网络/日志中暴露的风险
 * - ⚠️ 在**非安全上下文**里(纯 HTTP),密码本来就是在链路上明文传的 ——
 *   这一层摘要只是不让它出现在 nginx access log / 浏览器历史里,
 *   **不构成传输加密**。要防中间人必须上 HTTPS。
 */

/** SHA-256 摘要(浏览器 crypto.subtle;不可用时走纯 JS 实现,结果一致) */
export async function sha256(input: string): Promise<string> {
  if (typeof crypto === 'undefined' || !crypto.subtle) {
    // 非安全上下文(HTTP 局域网访问等)—— 降级到纯 JS,输出与 WebCrypto 相同
    return sha256Pure(input)
  }
  const enc = new TextEncoder().encode(input)
  const buf = await crypto.subtle.digest('SHA-256', enc)
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

/**
 * 登录密码预处理
 * 与后端约定:前端先 SHA-256(密码+salt),后端再 bcrypt
 * salt 取固定值 'rapid-admin'(与后端 seed 的自愈迁移用的是同一个值)
 */
export async function preHashPassword(password: string, salt = 'rapid-admin'): Promise<string> {
  return sha256(`${salt}::${password}`)
}

/* ================== 纯 JS SHA-256(非安全上下文的降级路径) ================== */

/** SHA-256 的 64 个轮常量 */
const SHA256_K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

/** 32 位循环右移 */
function rotr(x: number, n: number): number {
  return ((x >>> n) | (x << (32 - n))) >>> 0
}

/**
 * 纯 JS SHA-256 —— 输出与 `crypto.subtle.digest('SHA-256', …)` **逐字节相同**。
 *
 * ⚠️ 这不是"随便一个哈希" —— 后端 bcrypt 比对的就是这个值,
 *   差一位 = 全员登不进去。回归网用 NIST 标准向量 + 与 WebCrypto 对拍钉死。
 *
 * @param input UTF-8 字符串
 * @returns 64 位小写 hex
 */
export function sha256Pure(input: string): string {
  const bytes = new TextEncoder().encode(input)

  // 填充:0x80 → 补 0 到长度 ≡ 56 (mod 64) → 8 字节大端比特长度
  const blocks = Math.floor((bytes.length + 8) / 64) + 1
  const padded = new Uint8Array(blocks * 64)
  padded.set(bytes)
  padded[bytes.length] = 0x80
  const view = new DataView(padded.buffer)
  const bitLen = bytes.length * 8
  view.setUint32(padded.length - 8, Math.floor(bitLen / 0x100000000), false)
  view.setUint32(padded.length - 4, bitLen >>> 0, false)

  // 中间哈希值(H0..H7)
  let h0 = 0x6a09e667
  let h1 = 0xbb67ae85
  let h2 = 0x3c6ef372
  let h3 = 0xa54ff53a
  let h4 = 0x510e527f
  let h5 = 0x9b05688c
  let h6 = 0x1f83d9ab
  let h7 = 0x5be0cd19

  const w = new Uint32Array(64)

  for (let off = 0; off < padded.length; off += 64) {
    // 消息调度:W[0..15] 直接取字节, W[16..63] 递推
    for (let i = 0; i < 16; i++) w[i] = view.getUint32(off + i * 4, false)
    for (let i = 16; i < 64; i++) {
      const x = w[i - 15]!
      const y = w[i - 2]!
      const s0 = rotr(x, 7) ^ rotr(x, 18) ^ (x >>> 3)
      const s1 = rotr(y, 17) ^ rotr(y, 19) ^ (y >>> 10)
      w[i] = (w[i - 16]! + s0 + w[i - 7]! + s1) >>> 0
    }

    // 每块的工作变量从 H 重新开始
    let a = h0
    let b = h1
    let c = h2
    let d = h3
    let e = h4
    let f = h5
    let g = h6
    let h = h7

    for (let i = 0; i < 64; i++) {
      const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)
      const ch = (e & f) ^ (~e & g)
      const t1 = (h + S1 + ch + SHA256_K[i]! + w[i]!) >>> 0
      const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (S0 + maj) >>> 0
      h = g
      g = f
      f = e
      e = (d + t1) >>> 0
      d = c
      c = b
      b = a
      a = (t1 + t2) >>> 0
    }

    h0 = (h0 + a) >>> 0
    h1 = (h1 + b) >>> 0
    h2 = (h2 + c) >>> 0
    h3 = (h3 + d) >>> 0
    h4 = (h4 + e) >>> 0
    h5 = (h5 + f) >>> 0
    h6 = (h6 + g) >>> 0
    h7 = (h7 + h) >>> 0
  }

  return [h0, h1, h2, h3, h4, h5, h6, h7]
    .map((x) => (x >>> 0).toString(16).padStart(8, '0'))
    .join('')
}
