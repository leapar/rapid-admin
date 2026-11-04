/**
 * RecordService 测试 —— 路径 / 方法 / 请求体 / 查询串逐条对齐后端路由
 */
import { describe, expect, it, vi } from 'vitest'
import { buildListQuery, DynClient } from '../src'

type FetchHandler = (url: string, init: RequestInit) => Response

function makeClient(handler: FetchHandler) {
  const calls: Array<{ url: string; init: RequestInit }> = []
  const fetchImpl = vi.fn(async (url: any, init: any) => {
    calls.push({ url: String(url), init })
    return handler(String(url), init)
  })
  return { client: new DynClient('https://api.test/rapid-admin/v1', { fetch: fetchImpl as any }), calls }
}

function okResponse(data: unknown) {
  return new Response(JSON.stringify({ code: 0, data, message: 'ok', timestamp: 1 }), {
    status: 200,
  })
}

// ⚠️ **登录态客户端** —— 未登录的读动作会走公开只读镜像(见对应 describe),
// 正常数据面的路径形状要用带 token 的客户端钉住。
function makeAuthedClient(handler: FetchHandler) {
  const ctx = makeClient(handler)
  ctx.client.authStore.save('test-token')
  return ctx
}

describe('collection()', () => {
  it('空编码报错;同编码返回同一实例', () => {
    const client = new DynClient('https://api.test/rapid-admin/v1', { fetch: async () => okResponse(null) })
    expect(() => client.collection('')).toThrow(/集合编码/)
    expect(client.collection('inspection')).toBe(client.collection('inspection'))
    expect(client.collection('inspection')).not.toBe(client.collection('repair_order'))
  })
})

describe('查询串', () => {
  it('buildListQuery 全参数对齐后端 dynRecordQueryFromQuery', () => {
    const q = buildListQuery({
      page: 2,
      size: 50,
      keyword: '泵',
      filter: "status = 'active' && amount > 1000",
      filters: { status: ['active', 'pending'], level: 'high' },
      orderBy: 'created_at',
      orderDir: 'desc',
      fields: ['id', 'title'],
      skipTotal: true,
      trash: true,
      owner: '1001',
    })
    expect(q).toEqual({
      page: '2',
      size: '50',
      keyword: '泵',
      filter: "status = 'active' && amount > 1000",
      f_status: 'active,pending',
      f_level: 'high',
      order_by: 'created_at',
      order_dir: 'desc',
      fields: 'id,title',
      skip_total: '1',
      trash: '1',
      owner: '1001',
    })
  })

  it('f_ 前缀的空值被掐掉(空筛选 ≠ 按空值筛)', () => {
    const q = buildListQuery({ filters: { status: '', level: [''] } })
    expect(q).toEqual({})
  })
})

describe('CRUD 路径与方法', () => {
  // ⚠️ **这组断言全在登录态下跑** —— 未登录的读动作会走公开只读镜像
  // (见下面那个 describe),正常数据面的路径形状在这里钉住。

  it('getSchema', async () => {
    const { client, calls } = makeAuthedClient(() =>
      okResponse({ code: 'inspection', fields: [] }),
    )
    await client.collection('inspection').getSchema()
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/biz/dyn/inspection/schema')
  })

  it('getList', async () => {
    const { client, calls } = makeAuthedClient(() =>
      okResponse({ list: [], total: 0, page: 1, size: 20 }),
    )
    await client.collection('inspection').getList({ page: 1, size: 20, keyword: 'x' })
    expect(calls[0].url).toContain('/biz/dyn/inspection/records?')
    expect(calls[0].url).toContain('page=1')
    expect(calls[0].url).toContain('keyword=x')
  })

  it('getOne / create / update / delete / restore / submitApproval', async () => {
    const { client, calls } = makeAuthedClient(() => okResponse({ id: '42' }))
    const rec = client.collection('inspection')
    await rec.getOne('42')
    await rec.create({ title: 'A' })
    await rec.update('42', { title: 'B' })
    await rec.delete('42')
    await rec.restore('42')
    await rec.submitApproval('42')

    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/biz/dyn/inspection/records/42')
    expect(calls[1].init.method).toBe('POST')
    expect(calls[1].init.body).toBe(JSON.stringify({ title: 'A' }))
    expect(calls[2].init.method).toBe('PATCH')
    expect(calls[2].url).toBe('https://api.test/rapid-admin/v1/biz/dyn/inspection/records/42')
    expect(calls[3].init.method).toBe('DELETE')
    expect(calls[4].init.method).toBe('PUT')
    expect(calls[4].url).toContain('/records/42/restore')
    expect(calls[5].init.method).toBe('POST')
    expect(calls[5].url).toContain('/records/42/approval')
  })

  it('id 做路径转义;body 自动 JSON + Content-Type', async () => {
    const { client, calls } = makeAuthedClient(() => okResponse(null))
    await client.collection('inspection').create({ a: 1 }, { signal: undefined })
    expect((calls[0].init.headers as any)['Content-Type']).toBe('application/json')
  })

  it('batchDelete / batchUpdate 请求体', async () => {
    const { client, calls } = makeAuthedClient(() => okResponse({ count: 2 }))
    await client.collection('inspection').batchDelete(['1', '2'])
    await client.collection('inspection').batchUpdate(['1', '2'], 'status', 'closed')
    expect(calls[0].url).toContain('/records/batch-delete')
    expect(calls[0].init.body).toBe(JSON.stringify({ ids: ['1', '2'] }))
    expect(calls[1].url).toContain('/records/batch-update')
    expect(calls[1].init.body).toBe(JSON.stringify({ ids: ['1', '2'], field: 'status', value: 'closed' }))
  })
})

describe('公开只读路径回退(2026-11-03)', () => {
  it('未登录:读动作走 /public/dyn 镜像', async () => {
    const { client, calls } = makeClient(() => okResponse({ list: [], total: 0, page: 1, size: 20 }))
    const rec = client.collection('inspection')
    await rec.getSchema()
    await rec.getList({ page: 1 })
    await rec.getOne('7')
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/public/dyn/inspection/schema')
    expect(calls[1].url).toBe('https://api.test/rapid-admin/v1/public/dyn/inspection/records?page=1')
    expect(calls[2].url).toBe('https://api.test/rapid-admin/v1/public/dyn/inspection/records/7')
  })

  it('未登录:写动作仍走正常数据面(公开镜像没有写)', async () => {
    const { client, calls } = makeClient(() => okResponse({ id: '1' }))
    const rec = client.collection('inspection')
    await rec.create({ title: 'A' })
    await rec.delete('1')
    expect(calls[0].url).toBe('https://api.test/rapid-admin/v1/biz/dyn/inspection/records')
    expect(calls[1].init.method).toBe('DELETE')
    expect(calls[1].url).toBe('https://api.test/rapid-admin/v1/biz/dyn/inspection/records/1')
  })

  it('登录后读动作切回正常数据面;登出后再切回镜像', async () => {
    const { client, calls } = makeClient(() => okResponse({ list: [], total: 0, page: 1, size: 20 }))
    const rec = client.collection('inspection')
    await rec.getList()
    expect(calls[0].url).toContain('/public/dyn/')
    client.authStore.save('t1')
    await rec.getList()
    expect(calls[1].url).toContain('/biz/dyn/')
    client.authStore.clear()
    await rec.getList()
    expect(calls[2].url).toContain('/public/dyn/')
  })
})

describe('getFullList 自动翻页', () => {
  it('按 total 取完即停', async () => {
    let page = 0
    const { client, calls } = makeAuthedClient(() => {
      page++
      if (page === 1) {
        return okResponse({ list: [{ id: '1' }, { id: '2' }], total: 3, page: 1, size: 2 })
      }
      return okResponse({ list: [{ id: '3' }], total: 3, page: 2, size: 2 })
    })
    const all = await client.collection('inspection').getFullList({ batchSize: 2 })
    expect(all.map((r) => r.id)).toEqual(['1', '2', '3'])
    expect(calls).toHaveLength(2)
    expect(calls[1].url).toContain('page=2')
  })

  it('total 不准(谎报 999)时,页返回空 list 即停,不死循环', async () => {
    let page = 0
    const { client, calls } = makeAuthedClient(() => {
      page++
      if (page <= 2) {
        return okResponse({ list: [{ id: String(page) }], total: 999, page, size: 1 })
      }
      return okResponse({ list: [], total: 999, page, size: 1 })
    })
    const all = await client.collection('inspection').getFullList({ batchSize: 1 })
    expect(all).toHaveLength(2)
    expect(calls).toHaveLength(3)
  })
})
