/**
 * api/audit 的路径与参数传递(2026-10-30)
 *
 * ## 这条网守的是什么
 *
 *   三个审计接口读的 query 参数名**各不相同**,而且**读错名字不报错**:
 *
 *     · 操作日志   keyword / status / startAt / endAt
 *     · 数据变更   keyword / operation / startAt / endAt
 *     · 登录日志   keyword / **success** / startAt / endAt
 *
 *   名字对不上 ⇒ 后端 `c.Query("xxx")` 拿到空串 ⇒ **这个参数被完全忽略**:
 *   HTTP 200、结果不变、不报错、页面上的控件像坏了又像没坏。
 *
 *   ⚠️ 登录日志尤其阴险:后端 `LoginLogQuery.Success` 是 `*bool`,
 *      读的是 `success`;传 `status=0` **不是**"筛失败",而是**什么都不筛**。
 *
 *   另一头:路径必须带 group 前缀 —— `sys := api.Group("/system")`,
 *   所以完整路径是 `/rapid-admin/v1/system/audit/*`,
 *   写成 `/audit/login-logs` 会打到 `/rapid-admin/v1/audit/login-logs` ⇒ 404。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 loginLogs 的 `success` 改成 `status` → 「登录日志用 success」红;
 *   · 把路径的 `/system` 去掉 → 路径断言红;
 *   · 把 query 换成不透传(传 undefined)→ 参数断言红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

vi.mock('@/api/client', () => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn(),
  },
}))

import { http } from '@/api/client'
import { auditApi } from '@/api/audit'

const getMock = vi.mocked(http.get)

describe('api/audit(2026-10-30)', () => {
  beforeEach(() => {
    getMock.mockReset()
  })

  it('操作日志:GET /system/audit/operation-logs', () => {
    auditApi.operationLogs({ page: 1, size: 20 })

    expect(getMock).toHaveBeenCalledTimes(1)
    expect(getMock.mock.calls[0]![0]).toBe('/system/audit/operation-logs')
  })

  it('操作日志:keyword / status / 时间范围原样传出去(status=0 不能丢)', () => {
    auditApi.operationLogs({
      page: 1,
      size: 20,
      keyword: 'admin',
      status: 0,
      startAt: 100,
      endAt: 200,
    })

    // ⚠️ `status: 0` 是"只看失败",不是"不筛" —— 后端用 *int 区分,
    //   所以这一层**不能**把 0 当成空值丢掉。
    expect(getMock.mock.calls[0]![1]).toMatchObject({
      keyword: 'admin',
      status: 0,
      startAt: 100,
      endAt: 200,
    })
  })

  it('数据变更:GET /system/audit/data-change-logs', () => {
    auditApi.dataChangeLogs({ page: 1, size: 20 })

    expect(getMock.mock.calls[0]![0]).toBe('/system/audit/data-change-logs')
  })

  it('数据变更:operation 原样传出去(1/2/3)', () => {
    auditApi.dataChangeLogs({ page: 1, size: 20, operation: 2, tableName: 'biz_user' })

    expect(getMock.mock.calls[0]![1]).toMatchObject({
      operation: 2,
      tableName: 'biz_user',
    })
  })

  it('登录日志:GET /system/audit/login-logs', () => {
    auditApi.loginLogs({ page: 1, size: 20 })

    expect(getMock.mock.calls[0]![0]).toBe('/system/audit/login-logs')
  })

  it('登录日志:成功/失败筛选用 **success**,不是 status(后端只读 success)', () => {
    auditApi.loginLogs({ page: 1, size: 20, success: false })

    const params = getMock.mock.calls[0]![1] as Record<string, unknown>
    expect(params.success).toBe(false)
    // ⚠️ 传 `status` 后端**不读** —— 它只会让筛选"看着生效、其实没筛"
    expect(params).not.toHaveProperty('status')
  })

  it('登录日志:success=true 与不传是两回事(不传 = 不筛)', () => {
    auditApi.loginLogs({ page: 1, size: 20, success: true })
    expect(getMock.mock.calls[0]![1]).toMatchObject({ success: true })

    getMock.mockReset()
    auditApi.loginLogs({ page: 1, size: 20 })
    expect(getMock.mock.calls[0]![1]).not.toHaveProperty('success')
  })
})
