/**
 * 系统配置页 · 配置项名称 / 表单 label 的 i18n(2026-10-29)
 *
 * ## 这条网守的是什么
 *
 *   用户报:**「系统配置界面:当前值、加速域名、服务提供方 这些没有 i8n。」**
 *
 *   两个来源:
 *
 *   1. 「当前值」= 表单 label,走字典 `config_ui_label` ——
 *      字典里存的是**运营可改的中文**,英文环境下当然还是中文;
 *   2. 「CDN 加速域名」「OSS 服务提供方」= 卡片标题,直接渲染 DB 里的
 *      `sys_config.config_name` —— 也是中文。
 *
 *   ⇒ 修法:**i18n 优先,DB / 字典兜底**(词条见 locales/*.ts 的
 *   `page.config.item.*` / `page.config.dict.*`)。
 *
 * ## 为什么用"英文环境下渲染出英文"来断言
 *
 *   `t()` 查不到 key 时**原样返回 key**(永不空白),所以"漏接 i18n"在运行时
 *   完全静默 —— 英文用户看到一句中文,没有报错、没有日志、CI 全绿。
 *   唯一能验的就是**切到英文环境看渲染结果**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 `itemLabel(it)` 改回 `{it.config_name}` → 英文断言变红
 *   · 把 `fieldLabel('config.value')` 改回 `getLabel('config.value')` → label 断言变红
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, waitFor, screen } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import ConfigList from '../index'
import { configApi } from '@/api/config'
import { useUIStore } from '@/stores/ui'

vi.mock('@/api/config', () => ({
  configApi: {
    groups: vi.fn(),
    list: vi.fn(),
    update: vi.fn(),
  },
}))

/**
 * 字典 hook 打桩 —— 真实实现会去后端拉 sys_dict。
 *
 * ⚠️ 这里**故意**让 getLabel 返回原始 code(不是中文 label):
 *   兜底路径拿到的是字典里的中文,而打桩返回 code ⇒
 *   "没接 i18n"会渲染成 'config.value' 而不是 '当前值',
 *   断言能明确区分"走了 i18n"还是"走了兜底"。
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
    config_key: 'aliyun.oss.cdn_domain',
    config_name: 'CDN 加速域名',
    value: 'cdn.example.com',
    value_type: 'string',
    is_sensitive: false,
    is_active: true,
    version: 1,
    last_changed_at: '',
  },
  {
    id: '3',
    config_group: 'aliyun.sms',
    config_key: 'aliyun.sms.provider',
    config_name: '短信服务提供方',
    value: 'aliyun',
    value_type: 'string',
    is_sensitive: false,
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

describe('系统配置页 · i18n', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    groupsMock.mockResolvedValue(GROUPS as never)
    listMock.mockResolvedValue({ list: ITEMS, total: 3, page: 1, size: 1000 } as never)
    useUIStore.setState({ lang: 'zh-CN' })
  })

  afterEach(() => {
    useUIStore.setState({ lang: 'zh-CN' })
  })

  it('中文环境下显示中文名和中文 label', async () => {
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    await waitFor(() => expect(screen.getByText('OSS 服务提供方')).toBeInTheDocument())
    expect(screen.getByText('CDN 加速域名')).toBeInTheDocument()
    expect(screen.getAllByText('当前值').length).toBeGreaterThan(0)
  })

  it('英文环境下配置项名称跟 label 都要是英文(不能是 DB / 字典里的中文)', async () => {
    // ⚠️ 走 setLang action 而不是裸 setState —— M173 起 en-US 是异步 chunk,
    //   只有 setLang 会触发 ensureLocale;裸 setState 永远停在中文兜底。
    useUIStore.getState().setLang('en-US')
    renderPage()
    await waitFor(() => expect(listMock).toHaveBeenCalled())

    await waitFor(() => expect(screen.getByText('OSS Provider')).toBeInTheDocument())
    expect(screen.getByText('CDN Domain')).toBeInTheDocument()
    expect(screen.getAllByText('Current Value').length).toBeGreaterThan(0)

    // 一句中文都不许漏出来
    expect(document.body.textContent).not.toContain('服务提供方')
    expect(document.body.textContent).not.toContain('加速域名')
    expect(document.body.textContent).not.toContain('当前值')
  })
})
