/**
 * 字段级可见性报告 · 拦截器接线 —— M168(2026-11-02)
 *
 * client.ts 的成功拦截器是报告的**盖章点**:
 *   读 X-Field-Policy 头 → 解析 → 盖到 resp.data.data 上 → 按页面归档。
 *
 * 这里锁的是**端到端的那条链**(不是单函数):
 *   mock adapter 返回带头 / 不带头的响应 → http.get 拿回的数据上读不读得到报告。
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *	拆法                                            必须变红
 *	──────────────────────────────────────────────  ──────────────────────────
 *	拦截器里 applyFieldPolicyReport(resp) 那行删掉    「带头响应的数据被盖章」
 *	  (或整个函数体清空)
 *	stampFieldPolicyReport(payload, ...) 改成不递归   「行对象也被盖上」
 *	setLatestFieldPolicyReport 那行删掉               「报告按页面归档」
 */
import { describe, it, expect, afterEach } from 'vitest'
import { http, instance } from '@/api/client'
import {
  getLatestFieldPolicyReport,
  readFieldPolicyReport,
  currentPagePath,
  FIELD_POLICY_HEADER,
} from '@/utils/fieldPolicy'

const originalAdapter = instance.defaults.adapter

/** 让 axios 走一个本地假 adapter:返回给定的响应头 + body */
function mockAdapter(headers: Record<string, string>, data: unknown): void {
  instance.defaults.adapter = async (config) => ({
    data,
    status: 200,
    statusText: 'OK',
    headers,
    config,
  })
}

afterEach(() => {
  instance.defaults.adapter = originalAdapter
})

describe('client 拦截器 · 字段可见性报告盖章', () => {
  it('带头响应 → data 本体和每一行都被盖章', async () => {
    mockAdapter(
      { [FIELD_POLICY_HEADER]: 'hidden=phone;masked=real_name' },
      {
        code: 0,
        data: {
          list: [
            { id: '1', phone: '138****1234', real_name: '张*' },
            { id: '2', phone: '139****5678', real_name: '李*' },
          ],
          total: 2,
          page: 1,
          size: 20,
        },
        message: 'ok',
      },
    )

    const envelope = await http.get<{ list: Array<Record<string, unknown>>; total: number }>(
      '/system/users',
    )

    const report = { hidden: ['phone'], masked: ['real_name'] }
    // 信封上有
    expect(readFieldPolicyReport(envelope)).toEqual(report)
    // ⚠️ 每一行也有 —— 表单 / 详情拿到的是行,不是信封
    for (const row of envelope.list) {
      expect(readFieldPolicyReport(row)).toEqual(report)
    }
  })

  it('没带头 → 不盖章(绝大多数响应的零开销快路径)', async () => {
    mockAdapter(
      {},
      { code: 0, data: { list: [{ id: '1' }], total: 1, page: 1, size: 20 }, message: 'ok' },
    )

    const envelope = await http.get<{ list: Array<Record<string, unknown>> }>('/system/users')
    expect(readFieldPolicyReport(envelope)).toBeNull()
    expect(readFieldPolicyReport(envelope.list[0])).toBeNull()
  })

  it('垃圾头 → 不盖章(解析不出就不猜)', async () => {
    mockAdapter(
      { [FIELD_POLICY_HEADER]: 'nonsense' },
      { code: 0, data: { list: [{ id: '1' }] }, message: 'ok' },
    )

    const envelope = await http.get<{ list: Array<Record<string, unknown>> }>('/system/users')
    expect(readFieldPolicyReport(envelope)).toBeNull()
  })

  it('业务码非 0 → 不盖章(那条路走错误分支,没有数据可盖)', async () => {
    mockAdapter({ [FIELD_POLICY_HEADER]: 'hidden=phone' }, { code: 40300, message: '无权限' })

    await expect(http.get('/system/users')).rejects.toMatchObject({ code: 40300 })
  })

  it('⚠️ 报告按请求发起时的页面归档(兜底:页面重拼了行对象时用)', async () => {
    mockAdapter(
      { [FIELD_POLICY_HEADER]: 'hidden=phone' },
      { code: 0, data: { list: [{ id: '1' }] }, message: 'ok' },
    )

    await http.get('/system/users')

    // 测试环境的"当前页面"是 /
    const path = currentPagePath()
    expect(getLatestFieldPolicyReport(path)).toEqual({ hidden: ['phone'], masked: [] })
    // 别的页面拿不到这份归档 —— 不串页
    expect(getLatestFieldPolicyReport('/some/other/page')).toBeNull()
  })
})
