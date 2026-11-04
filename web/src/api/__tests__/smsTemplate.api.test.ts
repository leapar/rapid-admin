/**
 * api/smsTemplate 的参数传递(M115)
 *
 * ## 这条网守的是什么
 *
 *   后端的停用 / 恢复 handler 读的是 **query**(`c.Query("reason")`),
 *   **不是 body**。而 `RequestOptions` 里**没有** `params` 字段
 *   (POST 的 query 参数只能内联进 URL)——
 *   所以 reason 必须拼在 URL 上。
 *
 *   塞进 body 的后果是**完全静默**的:后端读不到 reason,返回 200,
 *   页面显示"已停用",而**流转历史里没有原因**。
 *   排障时那句"为什么停用"就这么没了 —— 而它恰恰是唯一有用的信息。
 *
 *   另一头:reason 是用户手写的中文,不编码会被浏览器/HTTP 栈拆坏
 *   (空格、`&`、`#` 都会改变 query 的语义)。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `reasonQuery(reason)` 改成往 body 里塞 → 「reason 必须在 query 里」红;
 *   · 把 `encodeURIComponent` 去掉 → 「中文 reason 必须编码」红;
 *   · 把 `reasonQuery` 改成永远返 `?reason=`(空也带)→ 「空 reason 不带 ?」红。
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
import { smsTemplateApi } from '@/api/smsTemplate'

const getMock = vi.mocked(http.get)
const postMock = vi.mocked(http.post)

describe('api/smsTemplate(M115)', () => {
  beforeEach(() => {
    getMock.mockReset()
    postMock.mockReset()
  })

  it('列表:GET /platform/sms-templates(不带 query)', () => {
    smsTemplateApi.list()

    expect(getMock).toHaveBeenCalledTimes(1)
    expect(getMock).toHaveBeenCalledWith('/platform/sms-templates')
  })

  it('详情:GET /platform/sms-templates/:scene', () => {
    smsTemplateApi.detail('login')

    expect(getMock).toHaveBeenCalledWith('/platform/sms-templates/login')
  })

  it('历史:GET /platform/sms-templates/:scene/history,条数走 params', () => {
    smsTemplateApi.history('login')

    expect(getMock).toHaveBeenCalledWith('/platform/sms-templates/login/history', {
      limit: 50,
    })
  })

  it('同步:POST /platform/sms-templates/sync', () => {
    smsTemplateApi.sync()

    expect(postMock).toHaveBeenCalledWith('/platform/sms-templates/sync')
  })

  it('停用:reason 必须在 query 里(后端读的是 c.Query("reason"),不是 body)', () => {
    smsTemplateApi.disable('login', '服务商驳回')

    const [url] = postMock.mock.calls[0]!
    expect(url).toContain('/platform/sms-templates/login/disable')
    expect(url).toContain('?reason=')
    // ⚠️ body 必须为空 —— 一旦塞进 body,后端根本不读,而且不报错
    expect(postMock.mock.calls[0]![1]).toBeUndefined()
  })

  it('恢复:reason 同样在 query 里', () => {
    smsTemplateApi.enable('login', '误停')

    const [url] = postMock.mock.calls[0]!
    expect(url).toContain('/platform/sms-templates/login/enable')
    expect(url).toContain('?reason=')
    expect(postMock.mock.calls[0]![1]).toBeUndefined()
  })

  it('空 reason 不带 ?(少一个字符少一处分歧)', () => {
    smsTemplateApi.disable('login')

    const [url] = postMock.mock.calls[0]!
    expect(url).toBe('/platform/sms-templates/login/disable')
    expect(url).not.toContain('?')
  })

  it('空白 reason 等同于没有(后端 c.Query("reason") 两种都返空串)', () => {
    smsTemplateApi.disable('login', '   ')

    const [url] = postMock.mock.calls[0]!
    expect(url).toBe('/platform/sms-templates/login/disable')
  })

  it('中文 reason 必须 URL 编码(不编码会被 & / # / 空格改掉 query 语义)', () => {
    smsTemplateApi.disable('login', '模板内容有误 & 需整改')

    const [url] = postMock.mock.calls[0]!
    expect(url).toContain('?reason=')
    // 编码后不含裸的 & (只有一个 ? 和一个 =)
    expect(url.split('?')[1]!.split('&').length).toBe(1)
    expect(url).not.toContain('模板')
    expect(decodeURIComponent(url.split('reason=')[1]!)).toBe('模板内容有误 & 需整改')
  })
})
