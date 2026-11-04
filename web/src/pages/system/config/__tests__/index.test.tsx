/**
 * 系统配置页 · 搜索框(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   搜索框里会凭空出现一个 "admin",而下面的列表还是全量 ——
 *   框里的字和过滤状态对不上。查明的原因(浏览器侧,实测):
 *
 *   1. Chrome 存着 http://localhost:5173/login 的账号 "admin";
 *   2. 这一页有 type="password" 的敏感配置框(SecretInput)→ Chrome 判定
 *      "这是个登录页",把页面上唯一的文本框(就是这个搜索框)当"用户名框";
 *   3. Chrome 把那个账号**画**在框里 —— 是浏览器渲染的自动填充预览,
 *      DOM 的 value 其实是空的(所以列表没被过滤)。
 *
 *   根因在 SecretInput(它已不用 type="password",见它的注释);这一页这边
 *   的两条防线是:搜索框 autoComplete="off" + 受控(value + onChange,
 *   框里显示什么永远等于 keyword,过滤改成边打边筛)。
 *
 * ## 为什么用 DOM 断言
 *
 *   "受控"在源码里就是有没有 value / onChange 两个属性,但要守的是**行为**:
 *   打完字列表必须立刻跟着变、清空后必须立刻恢复全量 —— 这两件事在
 *   旧实现(只有 onSearch)上都不成立,只有从 DOM 上看得出差别。
 *   另有一条:页面上不能出现 type="password" 的框(根因),这条最容易被
 *   悄悄改回去,所以单独锁住。
 *
 * ## ⚠️ **等的是"渲染出来",不是"请求发出去了"**(2026-10-31 M127)
 *
 *   下面 3 条断言原本都长这样:
 *
 *   ```ts
 *   await waitFor(() => expect(listMock).toHaveBeenCalled())   // ← 等的是"请求发出去了"
 *   expect(visibleGroups()).toEqual(['aliyun.oss', 'aliyun.sms'])  // ← 却立刻断言"组标签已经渲染出来了"
 *   ```
 *
 *   `listMock` 是 `vi.fn()` —— `toHaveBeenCalled()` 在**调用发生的那一刻**
 *   就为真;而组名标签(`<Tag color="blue">{g.group}</Tag>`,在 Collapse.Panel
 *   的 header 里)是跟着 `configApi.groups()` 的**响应**一起渲染出来的,
 *   中间隔着 `await listMock()` → setState → React 重渲染。
 *
 *   ⇒ **满载时**(全量跑 104 个文件)这个间隔会被拉长到超过同步断言的时机 ⇒
 *     `visibleGroups()` 拿到空数组,报
 *     "expected [] to deeply equal [ 'aliyun.oss', 'aliyun.sms' ]"。
 *     而**单独跑这个文件是绿的** —— 这就是 commit `73ad299` 记过的"满载假红"。
 *
 *   ⚠️ 这条**已经红过**(2026-10-31 全量跑 `--pool=threads` 实测),
 *   所以这不是"可能发生",是"已经在发生"。
 *
 *   ⇒ 正确写法是**等渲染结果**:见下面的 `waitForGroups()`。
 *
 *   ⚠️ **别把 `config/__tests__/audit.test.tsx` 也一起改了** ——
 *   那一条断言的 `aliyun.sms` 来自 **URL 参数**(`params.get('configGroup')`,
 *   首次渲染就在),不是响应 ⇒ 不受这个影响。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, waitFor, fireEvent, screen } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import ConfigList from '../index'
import { configApi } from '@/api/config'

vi.mock('@/api/config', () => ({
  configApi: {
    groups: vi.fn(),
    list: vi.fn(),
    update: vi.fn(),
  },
}))

/**
 * 字典 hook 打桩 —— 真实实现会去后端拉 sys_dict,
 * 在这个测试里只会打出一堆无意义的网络错误(而且 label 拿不到)。
 */
vi.mock('@/hooks/useDict', () => ({
  useDict: () => ({
    options: [],
    raw: [],
    loading: false,
    getLabel: (v: string | number) => String(v),
    getColor: () => 'default',
  }),
}))

const groupsMock = vi.mocked(configApi.groups)
const listMock = vi.mocked(configApi.list)

const GROUPS = [
  { group: 'aliyun.oss', count: 2 },
  { group: 'aliyun.sms', count: 1 },
]

const ITEMS = [
  {
    id: '1',
    config_group: 'aliyun.oss',
    config_key: 'aliyun.oss.provider',
    config_name: 'OSS 服务提供方',
    value: 'aliyun',
    value_type: 'string',
    is_sensitive: false,
    is_active: true,
    version: 1,
    last_changed_at: '2026-10-01T08:00:00Z',
  },
  {
    id: '2',
    config_group: 'aliyun.oss',
    config_key: 'aliyun.oss.access_key',
    config_name: 'OSS AccessKey ID',
    value: '',
    value_type: 'secret',
    is_sensitive: true,
    is_active: true,
    version: 1,
    last_changed_at: '',
  },
  {
    id: '3',
    config_group: 'aliyun.sms',
    config_key: 'aliyun.sms.access_key',
    config_name: '短信 AccessKey ID',
    value: '',
    value_type: 'secret',
    is_sensitive: true,
    is_active: true,
    version: 1,
    last_changed_at: '',
  },
]

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <ConfigList />
      </MemoryRouter>
    </App>,
  )
}

/**
 * 搜索框 —— 用 type="search" 定位,不用 placeholder 文案:
 * 文案跟着语言走(zh/en),类型不会。
 */
function searchInput(): HTMLInputElement {
  const el = document.querySelector('input[type="search"]')
  if (!el) throw new Error('页面上没有搜索框')
  return el as HTMLInputElement
}

/**
 * 当前露出来的配置组名
 *
 * 组名渲染在 Collapse.Panel 的 header 里(一个 <Tag>),而配置项卡片上的
 * Tag 是"敏感/已启用"这类文案 —— 所以按 "aliyun." 前缀筛,不会认错。
 * 用组名而不是面板 class:组名是数据(跟语言无关),class 是 antd 内部实现。
 */
function visibleGroups(): string[] {
  return Array.from(document.querySelectorAll('.ant-tag'))
    .map((el) => (el.textContent ?? '').trim())
    .filter((t) => t.startsWith('aliyun.'))
}

/**
 * 等配置组标签**渲染出来**
 *
 * ⚠️ **不能等"接口被调用过"** ——
 *   `await waitFor(() => expect(listMock).toHaveBeenCalled())` 只能证明
 *   **请求发出去了**,证明不了**响应回来了、React 重渲染完了**。
 *   而组名标签是跟着 `configApi.groups()` 的响应走的,
 *   中间隔着一整轮 microtask + 一次 React 调度,
 *   满载时那段空窗足够长 —— 于是 `visibleGroups()` 拿到空数组。
 *   (2026-10-31 M127 全量跑 `--pool=threads` 实测:满载时这条红,单独跑绿。)
 *
 *   ⇒ 等的必须是**要断言的那个东西本身**。
 */
async function waitForGroups(expected: string[]): Promise<void> {
  await waitFor(() => expect(visibleGroups()).toEqual(expected))
}

describe('系统配置页 · 搜索框', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    groupsMock.mockResolvedValue(GROUPS as never)
    listMock.mockResolvedValue({ list: ITEMS, total: 3, page: 1, size: 1000 } as never)
  })

  it('首屏:搜索框是空的,两个配置组都在', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    expect(searchInput().value).toBe('')
    // ⚠️ 等**组标签渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitForGroups(['aliyun.oss', 'aliyun.sms'])
  })

  it('搜索框带 autocomplete="off",别让浏览器把它当用户名框', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    expect(searchInput().getAttribute('autocomplete')).toBe('off')
  })

  it('页面里没有 type="password" 的框,否则 Chrome 会把整页当登录页', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())
    // 敏感配置项(SecretInput)要渲染出来,这条断言才成立
    await waitFor(() => expect(screen.getByText('aliyun.oss.access_key')).toBeInTheDocument())

    // 根因:页面上只要有一个 password 框,Chrome 就把整页当登录页,
    // 自动把存着的账号填进这个搜索框(框里凭空多一个 admin)。
    expect(document.querySelectorAll('input[type="password"]').length).toBe(0)
  })

  it('打字就过滤:框里的字和列表始终对得上', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    fireEvent.change(searchInput(), { target: { value: 'sms' } })

    expect(searchInput().value).toBe('sms')
    // ⚠️ 等**过滤结果渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitForGroups(['aliyun.sms'])
  })

  it('清空搜索框,列表立刻恢复全量', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    const input = searchInput()
    fireEvent.change(input, { target: { value: 'sms' } })
    // ⚠️ 等**过滤结果渲染出来**,不是等"请求发出去了" —— 见文件头
    await waitForGroups(['aliyun.sms'])

    // 清空 —— 走真实的"叉"按钮(antd 的 allowClear)
    fireEvent.click(document.querySelector('.ant-input-clear-icon') as HTMLElement)

    expect(input.value).toBe('')
    // ⚠️ 同样等渲染 —— 清空之后要等**全量**重新渲染出来
    await waitForGroups(['aliyun.oss', 'aliyun.sms'])
  })
})
