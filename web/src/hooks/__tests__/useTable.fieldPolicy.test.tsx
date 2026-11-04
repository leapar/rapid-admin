/**
 * useTable · 字段可见性报告通道 —— M168(2026-11-02)
 *
 * useTable 从**响应信封**上读回报告,塞进 tableProps.fieldPolicy
 * (框架通道,外壳 ManagementTable / DataTable 摘掉后过滤列)。
 *
 * # 可逆探针(拆掉实现看测试会不会红)
 *
 *	拆法                                          必须变红
 *	────────────────────────────────────────────  ────────────────────────
 *	load() 里 setFieldPolicy(readFieldPolicyReport(resp)) 那行删掉  「信封上的报告进通道」
 *	改成从行上读(列表为空就没有报告了)              「空列表也带报告」
 *	tableProps 里不带 fieldPolicy 字段              「tableProps 类型上有这个字段」
 */
import { describe, it, expect, vi } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'
import { useTable } from '@/hooks/useTable'
import { stampFieldPolicyReport } from '@/utils/fieldPolicy'

const REPORT = { hidden: ['phone'], masked: ['real_name'] }

/** 模拟盖章方(api/client.ts 拦截器):信封和每一行都被盖 */
function stampedEnvelope(rows: Array<Record<string, unknown>>) {
  return stampFieldPolicyReport(
    { list: rows.map((r) => ({ ...r })), total: rows.length, page: 1, size: 20 },
    REPORT,
  )
}

describe('useTable · fieldPolicy 通道', () => {
  it('信封上的报告进 tableProps.fieldPolicy', async () => {
    const fetchFn = vi.fn().mockResolvedValue(
      stampedEnvelope([{ id: '1', phone: '138****1234' }]),
    )
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))

    await waitFor(() => expect(result.current.data).toHaveLength(1))
    expect(result.current.tableProps.fieldPolicy).toEqual(REPORT)
  })

  it('⚠️ 空列表也带报告(报告是"规则说了什么",不是"这次改了什么")', async () => {
    const fetchFn = vi.fn().mockResolvedValue(stampedEnvelope([]))
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))

    await waitFor(() => expect(fetchFn).toHaveBeenCalled())
    expect(result.current.data).toHaveLength(0)
    expect(result.current.tableProps.fieldPolicy).toEqual(REPORT)
  })

  it('没盖章的信封(大多数响应)⇒ fieldPolicy 为 null', async () => {
    const fetchFn = vi.fn().mockResolvedValue({ list: [{ id: '1' }], total: 1, page: 1, size: 20 })
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))

    await waitFor(() => expect(result.current.data).toHaveLength(1))
    expect(result.current.tableProps.fieldPolicy).toBeNull()
  })

  it('失败时旧数据留着,旧报告也留着(与新数据保持一致)', async () => {
    const fetchFn = vi
      .fn()
      .mockResolvedValueOnce(stampedEnvelope([{ id: '1' }]))
      .mockRejectedValueOnce(new Error('网络抖动'))
    const { result } = renderHook(() => useTable({ fetch: fetchFn }))
    await waitFor(() => expect(result.current.data).toHaveLength(1))

    act(() => {
      result.current.reload()
    })
    await waitFor(() => expect(result.current.error).toBeTruthy())
    // 旧数据 + 旧报告都还在(外壳据此继续藏那一列 —— 数据里本来就没有那个键)
    expect(result.current.data).toHaveLength(1)
    expect(result.current.tableProps.fieldPolicy).toEqual(REPORT)
  })
})
