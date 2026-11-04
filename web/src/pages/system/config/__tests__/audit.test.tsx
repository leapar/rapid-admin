/**
 * 配置审计页 · group 过滤(2026-10-29)
 *
 * ## 这条网守的是什么
 *
 *   用户报:**「系统配置的审计日志里面 group 没有过滤效果。」**
 *
 *   根因是三层叠在一起:
 *
 *   1. 审计页把 URL 上的 group code 当成"config_key 前缀"塞进 `config_key` 发出去,
 *      而后端当时是**等值**匹配 ⇒ `aliyun.sms` 匹配不上 `aliyun.sms.access_key`;
 *   2. 前端发 `config_key`、后端 handler 读 `configKey`(Hertz 的 Query 大小写敏感)
 *      ⇒ **过滤条件从来没进过 SQL**;
 *   3. 后端压根没有"按组过滤"这个维度。
 *
 *   ⇒ 现在 group 走 `config_group`,关键字走 `config_key`,两者独立。
 *
 * ## 为什么断言"发出去的参数"而不是"表格里有没有行"
 *
 *   表格有没有行取决于后端返回什么 —— 那要起真后端。
 *   而**这一版 bug 的本质就是"请求里根本没带过滤条件"**,
 *   在请求层断言最直接,也最贴住故障点:
 *   请求里没有 `config_group` ⇒ 后端无论如何都过滤不了。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `q.config_group = configGroup` 删掉 → 第 1 条变红
 *   · 把 group code 又塞回 `config_key` → 第 1 条变红(且第 2 条也变红)
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, fireEvent } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import ConfigAudit from '../audit'
import { configApi } from '@/api/config'
import type { ConfigAuditQuery } from '@/types/config'

vi.mock('@/api/config', () => ({
  configApi: {
    auditLogs: vi.fn(),
  },
}))

const auditMock = vi.mocked(configApi.auditLogs)

function renderPage(url: string) {
  return render(
    <App>
      <MemoryRouter initialEntries={[url]}>
        <ConfigAudit />
      </MemoryRouter>
    </App>,
  )
}

/** 最后一次请求带的 query */
function lastQuery(): ConfigAuditQuery {
  const calls = auditMock.mock.calls
  expect(calls.length).toBeGreaterThan(0)
  return calls[calls.length - 1]![0]
}

/** 搜索框(antd Input.Search 渲染成 type="search") */
function searchInput(): HTMLInputElement {
  const el = document.querySelector('input[type="search"]')
  if (!el) throw new Error('页面上没有搜索框')
  return el as HTMLInputElement
}

describe('配置审计页 · group 过滤', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    auditMock.mockResolvedValue({ list: [], total: 0, page: 1, size: 20 } as never)
  })

  it('URL 带 configGroup 时,请求必须带 config_group(而不是塞进 config_key)', async () => {
    renderPage('/system/config/audit?configGroup=aliyun.sms')
    await waitFor(() => expect(auditMock).toHaveBeenCalled())

    const q = lastQuery()
    expect(q.config_group).toBe('aliyun.sms')
    expect(q.config_key).toBeUndefined()
  })

  it('URL 不带 configGroup 时,请求里两个过滤都不该有', async () => {
    renderPage('/system/config/audit')
    await waitFor(() => expect(auditMock).toHaveBeenCalled())

    const q = lastQuery()
    expect(q.config_group).toBeUndefined()
    expect(q.config_key).toBeUndefined()
  })

  it('group 过滤与关键字过滤可以同时存在(AND)', async () => {
    renderPage('/system/config/audit?configGroup=aliyun.sms')
    await waitFor(() => expect(auditMock).toHaveBeenCalled())

    fireEvent.change(searchInput(), { target: { value: 'access_key' } })
    fireEvent.keyDown(searchInput(), { key: 'Enter', code: 'Enter' })

    await waitFor(() => {
      const q = lastQuery()
      expect(q.config_key).toBe('access_key')
      expect(q.config_group).toBe('aliyun.sms')
    })
  })

  it('页面上要看得见"当前按哪个组过滤"', async () => {
    renderPage('/system/config/audit?configGroup=aliyun.sms')
    await waitFor(() => expect(auditMock).toHaveBeenCalled())

    // 分组标签 + 清除按钮
    expect(document.body.textContent).toContain('aliyun.sms')
  })
})
