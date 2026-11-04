/**
 * 登录链路测试
 *
 * ⚠️ **最要害的一条:发出去的 password 必须是 preHashPassword(明文)的 hex** ——
 *   后端拿它**直接** bcrypt 比对(不二次 hash),SDK 发明文 / 双重摘要,
 *   症状都只有"账号或密码错误",零提示(见 web crypto 契约注释)。
 *   ⇒ 用 node:crypto 独立算期望值,不 import SDK 的 sha256(同源不算对照)。
 */
import { describe, expect, it, vi } from 'vitest'
import { createHash } from 'node:crypto'
import { DynClient, preHashPassword, sha256Hex, sha256Pure } from '../src'

type FetchHandler = (url: string, init: RequestInit) => Response

function makeClient(handler: FetchHandler) {
  const calls: Array<{ url: string; init: RequestInit }> = []
  const fetchImpl = vi.fn(async (url: any, init: any) => {
    calls.push({ url: String(url), init })
    return handler(String(url), init)
  })
  return { client: new DynClient('https://api.test/rapid-admin/v1', { fetch: fetchImpl as any }), calls }
}

function tokensResponse() {
  return new Response(
    JSON.stringify({
      code: 0,
      message: 'ok',
      timestamp: 1,
      data: {
        token: 'tk-access',
        refresh_token: 'tk-refresh',
        expires_in: 3600,
        refresh_expires_in: 86400,
        user: { id: '1001', username: 'ops' },
      },
    }),
    { status: 200 },
  )
}

const expectedHash = (plain: string) =>
  createHash('sha256').update(`rapid-admin::${plain}`).digest('hex')

describe('sha256 实现(与 node:crypto 对照)', () => {
  // ⚠️ 补位边界长度必测:55/56/57/63/64/119/120 正好卡在
  // "一个 block / 两个 block"的临界点(SHA-256 补位最容易在这些长度写错)
  const lengths = [0, 1, 3, 55, 56, 57, 63, 64, 65, 119, 120, 500]
  for (const len of lengths) {
    it(`长度 ${len} 的输入与 node:crypto 一致`, async () => {
      const input = 'x'.repeat(len) + (len > 0 ? 'λ中' : '')
      const expected = createHash('sha256').update(input, 'utf8').digest('hex')
      expect(sha256Pure(input)).toBe(expected)
      expect(await sha256Hex(input)).toBe(expected)
    })
  }

  it('preHashPassword = sha256("rapid-admin::" + 明文)', async () => {
    expect(await preHashPassword('s3cret-密码')).toBe(expectedHash('s3cret-密码'))
  })
})

describe('authWithPassword', () => {
  it('发前置摘要而不是明文;token 自动入 authStore', async () => {
    const { client, calls } = makeClient(() => tokensResponse())
    const tokens = await client.authWithPassword('ops', 'plain-pwd')

    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/public/auth/login')
    const body = JSON.parse(String(calls[0].init.body))
    expect(body.username).toBe('ops')
    expect(body.password).toBe(expectedHash('plain-pwd'))
    expect(body.password).not.toBe('plain-pwd')
    expect(body.password).toMatch(/^[0-9a-f]{64}$/)
    expect(client.authStore.token).toBe('tk-access')
    expect(tokens.refresh_token).toBe('tk-refresh')
    expect(tokens.user).toEqual({ id: '1001', username: 'ops' })
  })

  it('验证码参数透传成 captcha_id / captcha_code', async () => {
    const { client, calls } = makeClient(() => tokensResponse())
    await client.authWithPassword('ops', 'p', { captchaId: 'cap-1', captchaCode: 'ab12' })
    const body = JSON.parse(String(calls[0].init.body))
    expect(body.captcha_id).toBe('cap-1')
    expect(body.captcha_code).toBe('ab12')
  })

  it('不带验证码时发空串(开关关闭场景)', async () => {
    const { client, calls } = makeClient(() => tokensResponse())
    await client.authWithPassword('ops', 'p')
    const body = JSON.parse(String(calls[0].init.body))
    expect(body.captcha_id).toBe('')
    expect(body.captcha_code).toBe('')
  })

  it('空用户名 / 空密码当场报错,不发请求', async () => {
    const { client, calls } = makeClient(() => tokensResponse())
    await expect(client.authWithPassword('', 'p')).rejects.toThrow(/用户名和密码/)
    await expect(client.authWithPassword('ops', '')).rejects.toThrow(/用户名和密码/)
    expect(calls).toHaveLength(0)
  })

  it('登录失败(验证码错)抛 ClientResponseError 且不污染 authStore', async () => {
    const { client, calls } = makeClient(
      () =>
        new Response(
          JSON.stringify({ code: 40201, message: '图形验证码错误或已过期', timestamp: 1 }),
          { status: 200 },
        ),
    )
    client.authStore.save('stale')
    const err = await client.authWithPassword('ops', 'p').catch((e) => e)
    expect(err.code).toBe(40201)
    // ⚠️ 旧 token 保持原样 —— 登录失败不该把调用方原有的会话清掉
    expect(client.authStore.token).toBe('stale')
    expect(calls).toHaveLength(1)
  })
})

describe('getCaptcha / refreshAuth / logout', () => {
  it('getCaptcha 打公开接口并透传响应', async () => {
    const { client, calls } = makeClient(
      () =>
        new Response(
          JSON.stringify({
            code: 0,
            data: { id: 'cap-1', image: 'data:image/png;base64,xxx', length: 4, expires_in: 120 },
            timestamp: 1,
          }),
          { status: 200 },
        ),
    )
    const info = await client.getCaptcha()
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/public/captcha')
    expect(info.id).toBe('cap-1')
    expect(info.image).toContain('data:image/png')
  })

  it('refreshAuth 换新 token 并自动入 authStore', async () => {
    const { client, calls } = makeClient(() => tokensResponse())
    await client.refreshAuth('old-refresh')
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/public/auth/refresh')
    expect(JSON.parse(String(calls[0].init.body))).toEqual({ refresh_token: 'old-refresh' })
    expect(client.authStore.token).toBe('tk-access')
  })

  it('logout 调服务端吊销 + 清本地;服务端 401 也照样清', async () => {
    const { client, calls } = makeClient(() => new Response(JSON.stringify({ code: 0 }), { status: 200 }))
    client.authStore.save('tk')
    await client.logout()
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/auth/logout')
    expect((calls[0].init.headers as any).Authorization).toBe('Bearer tk')
    expect(client.authStore.token).toBe('')
  })

  it('服务端拒绝(token 已过期)也不卡住登出', async () => {
    const { client, calls } = makeClient(
      () =>
        new Response(
          JSON.stringify({ code: 40101, message: 'token 已过期', timestamp: 1 }),
          { status: 401 },
        ),
    )
    client.authStore.save('expired')
    await client.logout()
    expect(calls).toHaveLength(1)
    expect(client.authStore.token).toBe('')
  })

  it('未登录时 logout 只清本地,不发请求', async () => {
    const { client, calls } = makeClient(() => new Response('{}', { status: 200 }))
    await client.logout()
    expect(calls).toHaveLength(0)
    expect(client.authStore.isValid).toBe(false)
  })
})
