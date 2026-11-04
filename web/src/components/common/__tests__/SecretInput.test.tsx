/**
 * SecretInput 掩码行为(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   本组件的输入框**不用 type="password"**(掩码态也是 type="text")。
 *   原因:页面里只要有一个 type="password",Chrome 就把整页当登录页,
 *   把页面上唯一的文本框当"用户名框",自动填它存着的账号 ——
 *   系统配置页的搜索框会凭空出现 "admin"(框里显示的是浏览器画的预览,
 *   DOM 的 value 是空的,所以列表还是全量)。
 *
 *   掩码不能因此失效,所以这里锁住两件事:
 *     · 藏起来的时候框里是 '********'(DOM 里没有明文);
 *     · 点眼睛能还原成明文。
 *
 * ## 为什么单独测组件
 *
 *   配置页把敏感项的初始值清空(留空 = 不修改),所以那页根本走不到
 *   "有值时怎么掩码"这条路径 —— 只有直接测组件才守得住。
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { SecretInput } from '@/components/common/SecretInput'

describe('SecretInput', () => {
  it('藏起来的时候框里是 **********,DOM 里没有明文', () => {
    render(<SecretInput value="super-secret-value" />)

    const input = document.querySelector('input') as HTMLInputElement
    expect(input.value).toBe('********')
    expect(input.value).not.toContain('super-secret')
  })

  it('点眼睛还原明文,再点一次收回', () => {
    render(<SecretInput value="super-secret-value" />)

    const input = document.querySelector('input') as HTMLInputElement
    const toggle = screen.getByRole('button')

    fireEvent.click(toggle)
    expect(input.value).toBe('super-secret-value')

    fireEvent.click(toggle)
    expect(input.value).toBe('********')
  })

  it('输入框不是 type="password"(Chrome 会因此把整页当登录页)', () => {
    render(<SecretInput value="" />)

    const input = document.querySelector('input') as HTMLInputElement
    expect(input.getAttribute('type')).toBe('text')
    expect(document.querySelectorAll('input[type="password"]').length).toBe(0)
  })

  it('打字会回调新值(受控用法)', () => {
    const onChange = vi.fn()
    render(<SecretInput value="" onChange={onChange} />)

    const input = document.querySelector('input') as HTMLInputElement
    fireEvent.change(input, { target: { value: 'abc' } })

    expect(onChange).toHaveBeenCalledWith('abc')
  })
})
