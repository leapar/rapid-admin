/**
 * utils/crypto.ts 回归网(2026-10-29 M110)
 *
 * 这一层此前**一条网都没有**,而它是"登录能不能成功"的唯一入口。
 *
 * 锁住的:
 *   ① **纯 JS SHA-256 与 WebCrypto 逐字节相同** —— 降级路径不能是"另一个哈希"。
 *      回归:M110 P0 —— `crypto.subtle` 只在**安全上下文**可用,而 vite dev
 *      server 配的是 `host: '0.0.0.0'`,同事从 `http://<局域网IP>:5173` 打开
 *      就是非安全上下文 ⇒ `crypto.subtle` 是 undefined ⇒ 此前走的是自制
 *      `fallbackHash()`(**不是 SHA-256**)⇒ 后端 bcrypt 必然比对失败
 *      ⇒ **登录永远报"账号或密码错误"**,零提示。
 *   ② 填充边界(55 / 56 / 57 / 63 / 64 / 119 / 120 字节)—— SHA-256 补位最容易
 *      在这些长度上写错,而它们正好是"一个 block / 两个 block"的临界点。
 *   ③ **前后端契约**:`preHashPassword(明文)` = `sha256("rapid-admin::" + 明文)`,
 *      64 位小写 hex —— 后端拿它直接 bcrypt 比对
 *      (server/internal/service/auth_impl.go:143-144)。
 *      回归:seed 的自愈迁移用的是同一个 salt(见 server/internal/seed/seed.go)。
 *
 * 反向探针(见 scripts/probe_m110.mjs):把每一处修复拆掉,对应��例必须变红。
 */
import { describe, it, expect, afterEach } from 'vitest'
import { createHash } from 'node:crypto'
import { sha256, sha256Pure, preHashPassword } from '@/utils/crypto'

/**
 * 模拟**非安全上下文**(http 局域网访问)—— `crypto.subtle` 不可用。
 * Node 的 `crypto.subtle` 是原型上的 getter,在实例上定义一个自有属性即可遮蔽。
 */
function withoutSubtle<T>(fn: () => T): T {
  const c = globalThis.crypto as unknown as Record<string, unknown>
  const had = Object.prototype.hasOwnProperty.call(c, 'subtle')
  const original = Object.getOwnPropertyDescriptor(c, 'subtle')
  Object.defineProperty(c, 'subtle', { value: undefined, configurable: true, writable: true })
  try {
    return fn()
  } finally {
    if (had && original) Object.defineProperty(c, 'subtle', original)
    else delete c.subtle
  }
}

/** Node 的 SHA-256(独立实现,用来当"标准答案") */
function nodeSha256(input: string): string {
  return createHash('sha256').update(input, 'utf8').digest('hex')
}

afterEach(() => {
  // 兜底:万一某个用例中途抛错,别把 crypto.subtle 留在被遮蔽的状态
  const c = globalThis.crypto as unknown as Record<string, unknown>
  if (Object.prototype.hasOwnProperty.call(c, 'subtle')) {
    delete c.subtle
  }
})

describe('crypto · 纯 JS SHA-256', () => {
  it('NIST 标准向量', () => {
    // https://www.di-mgt.com.au/sha_testvectors.html
    expect(sha256Pure('')).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855')
    expect(sha256Pure('abc')).toBe(
      'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
    )
    expect(sha256Pure('abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq')).toBe(
      '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1',
    )
  })

  it('与 Node 的 SHA-256 逐输入相同(含中文 / emoji / 空格)', () => {
    const inputs = [
      '',
      'a',
      'rapid-admin::Admin@2026',
      '中文密码',
      'emoji 🔐🚀',
      ' leading and trailing ',
      'x'.repeat(1000),
    ]
    for (const input of inputs) {
      expect(sha256Pure(input), `input=${JSON.stringify(input.slice(0, 20))}`).toBe(
        nodeSha256(input),
      )
    }
  })

  it('填充边界:55 / 56 / 57 / 63 / 64 / 119 / 120 字节都对', () => {
    // 56 = 最后一个字节放 0x80 的位置;57 起就要多开一个 block
    for (const len of [55, 56, 57, 63, 64, 65, 119, 120, 121]) {
      const input = 'a'.repeat(len)
      expect(sha256Pure(input), `len=${len}`).toBe(nodeSha256(input))
    }
  })

  it('输出是 64 位小写 hex', () => {
    const out = sha256Pure('anything')
    expect(out).toHaveLength(64)
    expect(out).toMatch(/^[0-9a-f]{64}$/)
  })
})

describe('crypto · 非安全上下文的降级路径(M110 P0)', () => {
  it('crypto.subtle 不可用时,结果与 WebCrypto **完全相同**', async () => {
    const inputs = ['', 'abc', 'rapid-admin::Admin@2026', '中文密码', 'y'.repeat(200)]

    const withWebCrypto: string[] = []
    for (const i of inputs) withWebCrypto.push(await sha256(i))

    const withoutWebCrypto: string[] = []
    for (const i of inputs) {
      withoutWebCrypto.push(await withoutSubtle(() => sha256(i)))
    }

    expect(withoutWebCrypto).toEqual(withWebCrypto)
    // 再跟独立实现对一次
    expect(withoutWebCrypto).toEqual(inputs.map(nodeSha256))
  })

  it('降级路径的输出仍是 64 位 hex(旧 fallbackHash 不是)', async () => {
    const out = await withoutSubtle(() => sha256('rapid-admin::Admin@2026'))
    expect(out).toMatch(/^[0-9a-f]{64}$/)
    expect(out).toBe(nodeSha256('rapid-admin::Admin@2026'))
  })

  it('降级路径下 preHashPassword 也一样', async () => {
    const normal = await preHashPassword('Admin@2026')
    const degraded = await withoutSubtle(() => preHashPassword('Admin@2026'))
    expect(degraded).toBe(normal)
  })
})

describe('crypto · 前后端契约', () => {
  it('preHashPassword = sha256(salt + "::" + 明文)', async () => {
    const plain = 'Admin@2026'
    expect(await preHashPassword(plain)).toBe(nodeSha256(`rapid-admin::${plain}`))
  })

  it('默认 salt 是 rapid-admin(与后端 seed 自愈迁移同一个值)', async () => {
    // 后端 server/internal/seed/seed.go 的迁移算的是 sha256("rapid-admin::" + 明文)
    const plain = 'Admin@2026'
    const expected = createHash('sha256').update(`rapid-admin::${plain}`, 'utf8').digest('hex')
    expect(await preHashPassword(plain)).toBe(expected)
    // 换 salt 必须变 —— 说明 salt 真的参与了摘要
    expect(await preHashPassword(plain, 'other')).not.toBe(expected)
  })

  it('输出是 64 位小写 hex(后端只认这个长度)', async () => {
    const out = await preHashPassword('P@ssw0rd')
    expect(out).toHaveLength(64)
    expect(out).toMatch(/^[0-9a-f]{64}$/)
  })
})
