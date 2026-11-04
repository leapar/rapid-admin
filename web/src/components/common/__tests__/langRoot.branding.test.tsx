/**
 * 浏览器标题 —— 平台名来自后端 branding 配置(2026-11-01 开源通用化)
 *
 * ## 这条网守的是什么
 *
 *   开源之后"平台名"是**服务端 yaml 的 branding.app_title**,
 *   前端语言包里的 `common.appTitle` 只是**兜底值**。
 *
 *   两条规则:
 *   1. 后端配了 → document.title 用后端那份(改 yaml 即换名字,不用改前端)
 *   2. 后端没配 / 拉失败 → 落回语言包(界面永远有一行字,不会空着)
 *
 *   ⚠️ **为什么这条要有网**:`document.title` 是**写副作用** ——
 *   它不在 DOM 里,断言只能读 `document.title`。
 *   而"配置异步到货后标题没跟着改"这种故障,肉眼在页面上很难发现
 *   (标签页标题就那么小一行),上线后基本没人会报。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { render } from '@testing-library/react'

import { LangRoot } from '@/components/common/LangRoot'
import { usePublicConfigStore } from '@/stores/publicConfig'

beforeEach(() => {
  usePublicConfigStore.setState({
    captchaLoginEnabled: null,
    captchaSmsSendEnabled: null,
    appName: '',
    appTitle: '',
    loading: false,
  })
  document.title = ''
})

describe('LangRoot · 浏览器标题', () => {
  it('后端配了 app_title → 用它(改 yaml 即换平台名)', () => {
    usePublicConfigStore.setState({ appTitle: 'Acme Console' })
    render(
      <LangRoot>
        <div />
      </LangRoot>,
    )
    expect(document.title).toBe('Acme Console')
  })

  it('后端没配 → 落回语言包(不是空标题)', () => {
    render(
      <LangRoot>
        <div />
      </LangRoot>,
    )
    expect(document.title).not.toBe('')
    expect(document.title).toBe('后台管理平台')
  })

  it('配置**异步**到货后标题要跟着改(依赖里必须带 appTitle)', () => {
    const { rerender } = render(
      <LangRoot>
        <div />
      </LangRoot>,
    )
    expect(document.title).toBe('后台管理平台')

    // 模拟 main.tsx 启动时的 fetch 回来
    usePublicConfigStore.setState({ appTitle: 'Acme Console' })
    rerender(
      <LangRoot>
        <div />
      </LangRoot>,
    )
    expect(document.title).toBe('Acme Console')
  })
})
