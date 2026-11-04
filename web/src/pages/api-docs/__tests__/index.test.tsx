/**
 * API 文档入口页(2026-11-01 · 开源通用化 §8 G5)
 *
 * ## 这一组守的是什么
 *
 *   1. **按钮必须指向后端真正挂载的那个地址** —— Swagger UI 不是前端的一部分,
 *      它由后端挂在 `/swagger` 下(见 server/internal/api/router/router.go)。
 *      前端这边写错路径的症状是**静默**的:点了没反应,或者跳到 vite 的
 *      SPA fallback(登录页)。
 *
 *   2. **必须在新标签页打开** —— `/swagger` 默认带 BasicAuth。iframe 里遇到
 *      BasicAuth 挑战时浏览器**不会**弹用户名/密码框,用户看到的是一个
 *      空白框架。所以这里断言 `target="_blank"`,而不是靠"看起来对"。
 *
 *   3. **文案必须走 i18n** —— `t()` 查不到 key 时**原样返回 key**(永不空白、
 *      不报错),所以"漏接 i18n"在运行时完全静默:英文用户看到一句中文,
 *      没有日志、没有异常、CI 全绿。唯一的兜底就是**切到英文环境看渲染结果**。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把按钮的 href 改成 '/swagger'（少了 index.html） → 「指向 UI 地址」红
 *   · 把 `target="_blank"` 去掉 → 「新标签页打开」红
 *   · 把文案从 `t('apiDocs.open')` 改成写死的中文 → 「英文环境」红
 */
import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { App } from 'antd'
import { MemoryRouter } from 'react-router-dom'
import ApiDocs from '../index'
import { SWAGGER_SPEC_PATH, SWAGGER_UI_PATH } from '@/constants/swagger'
import { useUIStore } from '@/stores/ui'
import { ensureLocale } from '@/locales/registry'

function renderPage() {
  return render(
    <App>
      <MemoryRouter>
        <ApiDocs />
      </MemoryRouter>
    </App>,
  )
}

describe('API 文档入口页', () => {
  beforeEach(() => {
    useUIStore.setState({ lang: 'zh-CN' })
  })

  afterEach(() => {
    useUIStore.setState({ lang: 'zh-CN' })
  })

  it('按钮指向后端挂载的 Swagger UI 地址', () => {
    renderPage()
    const link = screen.getByRole('link', { name: /Swagger UI/ })
    expect(link).toHaveAttribute('href', SWAGGER_UI_PATH)
  })

  it('在新标签页打开(否则 BasicAuth 的弹框出不来)', () => {
    renderPage()
    const link = screen.getByRole('link', { name: /Swagger UI/ })
    expect(link).toHaveAttribute('target', '_blank')
    // rel=noopener —— 新标签页不该拿到 opener 引用
    expect(link.getAttribute('rel')).toContain('noopener')
  })

  it('页面上给出可复制的两个地址(UI 与 OpenAPI 描述文件)', () => {
    renderPage()
    expect(screen.getByText(SWAGGER_UI_PATH)).toBeInTheDocument()
    expect(screen.getByText(SWAGGER_SPEC_PATH)).toBeInTheDocument()
  })

  it('中文环境下按钮是中文文案', () => {
    renderPage()
    const link = screen.getByRole('link', { name: /Swagger UI/ })
    // ⚠️ antd 5 的 Button 在**纯中文**文案里会自动插空格("打 开")——
    //   匹配用 \s* 容忍,别写死。
    expect(link.textContent).toMatch(/打\s*开\s*Swagger UI/)
  })

  it('英文环境下按钮是英文文案(漏接 i18n 在运行时是静默的)', async () => {
    // ⚠️ M173(635cfea)把 en-US 拆成了**异步懒加载**语言包 —— 只 setState
    //   lang 不够,字典没到时 t() 会回落 zh-CN(正是本用例防的"静默回落")。
    //   测试里必须先 ensureLocale 把包拉进缓存,再渲染、再断言。
    await ensureLocale('en-US')
    useUIStore.setState({ lang: 'en-US' })
    renderPage()
    const link = screen.getByRole('link', { name: /Swagger UI/ })
    expect(link.textContent).toMatch(/Open Swagger UI/)
    expect(link.textContent).not.toMatch(/打\s*开/)
  })
})
