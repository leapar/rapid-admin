/**
 * SHA-256(登录密码前置摘要用)
 *
 * # 前后端契约(改一处就要改两处,见 web/src/utils/crypto.ts 同一条)
 *
 *	SDK:preHashPassword(明文) = sha256("rapid-admin::" + 明文) → 64 位小写 hex
 *	后端:库里存 bcrypt(那个 hex),登录时拿 SDK 发来的 hex 直接 bcrypt 比对,
 *	      **不再二次 hash**
 *
 * 改 salt 或改算法 = 全员登不进去,症状只有"账号或密码错误",看不出真因。
 *
 * # 为什么要纯 JS 兜底
 *
 * crypto.subtle 只在安全上下文可用(HTTPS / localhost)——集成方从
 * http://内网IP 打开页面时 subtle 是 undefined。web 端踩过这个坑
 * (M110):当时的"降级"不是 SHA-256,后端比对必然失败且零提示。
 * ⇒ subtle 在就优先用,不在就走纯 JS 实现,两边都被测试钉死。
 */

/** SHA-256 的 64 个轮常量 */
const K = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
])

/** 纯 JS SHA-256(无 subtle 时的兜底;输出 64 位小写 hex) */
export function sha256Pure(input: string): string {
  const msg = new TextEncoder().encode(input)
  const l = msg.length
  const bitLen = l * 8
  const k = (56 - ((l + 1) % 64) + 64) % 64
  const total = l + 1 + k + 8
  const m = new Uint8Array(total)
  m.set(msg)
  m[l] = 0x80
  const hi = Math.floor(bitLen / 0x100000000)
  const lo = bitLen >>> 0
  m[total - 8] = (hi >>> 24) & 0xff
  m[total - 7] = (hi >>> 16) & 0xff
  m[total - 6] = (hi >>> 8) & 0xff
  m[total - 5] = hi & 0xff
  m[total - 4] = (lo >>> 24) & 0xff
  m[total - 3] = (lo >>> 16) & 0xff
  m[total - 2] = (lo >>> 8) & 0xff
  m[total - 1] = lo & 0xff

  const w = new Uint32Array(64)
  const rotr = (x: number, n: number) => (x >>> n) | (x << (32 - n))
  let h0 = 0x6a09e667, h1 = 0xbb67ae85, h2 = 0x3c6ef372, h3 = 0xa54ff53a
  let h4 = 0x510e527f, h5 = 0x9b05688c, h6 = 0x1f83d9ab, h7 = 0x5be0cd19

  for (let i = 0; i < total; i += 64) {
    for (let j = 0; j < 16; j++) {
      w[j] = ((m[i + 4 * j] << 24) | (m[i + 4 * j + 1] << 16) | (m[i + 4 * j + 2] << 8) | m[i + 4 * j + 3]) >>> 0
    }
    for (let j = 16; j < 64; j++) {
      const s0 = rotr(w[j - 15], 7) ^ rotr(w[j - 15], 18) ^ (w[j - 15] >>> 3)
      const s1 = rotr(w[j - 2], 17) ^ rotr(w[j - 2], 19) ^ (w[j - 2] >>> 10)
      w[j] = (w[j - 16] + s0 + w[j - 7] + s1) >>> 0
    }
    let a = h0, b = h1, c = h2, d = h3, e = h4, f = h5, g = h6, h = h7
    for (let j = 0; j < 64; j++) {
      const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)
      const ch = (e & f) ^ (~e & g)
      const t1 = (h + S1 + ch + K[j] + w[j]) >>> 0
      const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (S0 + maj) >>> 0
      h = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0
    }
    h0 = (h0 + a) >>> 0; h1 = (h1 + b) >>> 0; h2 = (h2 + c) >>> 0; h3 = (h3 + d) >>> 0
    h4 = (h4 + e) >>> 0; h5 = (h5 + f) >>> 0; h6 = (h6 + g) >>> 0; h7 = (h7 + h) >>> 0
  }
  return [h0, h1, h2, h3, h4, h5, h6, h7].map((x) => x.toString(16).padStart(8, '0')).join('')
}

/** SHA-256 → 64 位小写 hex(subtle 优先,非安全上下文走纯 JS) */
export async function sha256Hex(input: string): Promise<string> {
  const subtle = globalThis.crypto?.subtle
  if (subtle) {
    try {
      const digest = await subtle.digest('SHA-256', new TextEncoder().encode(input))
      return Array.from(new Uint8Array(digest))
        .map((b) => b.toString(16).padStart(2, '0'))
        .join('')
    } catch {
      // subtle 存在但调用失败(个别 WebView)→ 走纯 JS
    }
  }
  return sha256Pure(input)
}

/** 登录密码前置摘要:salt 固定 'rapid-admin',与 web 前端 / 后端 seed 三方一致 */
export async function preHashPassword(password: string, salt = 'rapid-admin'): Promise<string> {
  return sha256Hex(`${salt}::${password}`)
}
