/**
 * SensitiveField 回归网(2026-10-29 M111)
 *
 * 敏感字段展示是"用户信息脱敏"这条安全要求的落点 —— 但此前**零用例**。
 *
 * 锁住的:
 *   · 四种显式类型的脱敏形态(phone / idCard / email / name)
 *   · `auto` 的自动识别
 *   · `canView=false` ⇒ 只给脱敏值,**连眼睛按钮都没有**
 *   · `canView=true` ⇒ 点眼睛看明文,再点回到脱敏
 *   · ⚠️ **auto 识别里"18 位邮箱"以前被当成身份证脱敏**(M111 修正)
 *   · ⚠️ **眼睛按钮以前没有可访问名**(M111 修正)
 */
import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { SensitiveField } from '@/components/common/SensitiveField'

describe('SensitiveField · 基本形态', () => {
  it('没有值 → 显示占位符,不给眼睛按钮', () => {
    render(<SensitiveField value={undefined} type="phone" canView />)

    expect(screen.getByText('-')).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('自定义占位符', () => {
    render(<SensitiveField value="" type="phone" placeholder="未填写" />)

    expect(screen.getByText('未填写')).toBeInTheDocument()
  })

  it('canView=false → 只有脱敏值,没有眼睛按钮', () => {
    render(<SensitiveField value="13800000001" type="phone" />)

    expect(screen.getByText('138****0001')).toBeInTheDocument()
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('canView=true → 脱敏值 + 眼睛按钮;点一下看明文,再点回到脱敏', () => {
    render(<SensitiveField value="13800000001" type="phone" canView />)

    expect(screen.getByText('138****0001')).toBeInTheDocument()
    const eye = screen.getByRole('button')
    expect(eye).toBeInTheDocument()

    fireEvent.click(eye)
    expect(screen.getByText('13800000001')).toBeInTheDocument()

    // 明文态下换成"隐藏"按钮,点回去
    const hide = screen.getByRole('button')
    fireEvent.click(hide)
    expect(screen.getByText('138****0001')).toBeInTheDocument()
  })

  it('defaultVisible=true + canView → 直接显示明文', () => {
    render(<SensitiveField value="13800000001" type="phone" canView defaultVisible />)

    expect(screen.getByText('13800000001')).toBeInTheDocument()
  })

  it('defaultVisible=true 但 canView=false → 仍然只给脱敏值(权限优先)', () => {
    render(<SensitiveField value="13800000001" type="phone" defaultVisible />)

    expect(screen.getByText('138****0001')).toBeInTheDocument()
    expect(screen.queryByText('13800000001')).toBeNull()
  })
})

describe('SensitiveField · 脱敏形态', () => {
  it('type=phone', () => {
    render(<SensitiveField value="13800000001" type="phone" />)
    expect(screen.getByText('138****0001')).toBeInTheDocument()
  })

  it('type=idCard', () => {
    render(<SensitiveField value="110101199001011234" type="idCard" />)
    expect(screen.getByText('110101********1234')).toBeInTheDocument()
  })

  it('type=email', () => {
    render(<SensitiveField value="zhangsan@example.com" type="email" />)
    expect(screen.getByText('z***@example.com')).toBeInTheDocument()
  })

  it('type=name', () => {
    render(<SensitiveField value="张三丰" type="name" />)
    expect(screen.getByText('张**')).toBeInTheDocument()
  })
})

describe('SensitiveField · auto 自动识别', () => {
  it('11 位手机号 → 手机脱敏', () => {
    render(<SensitiveField value="13800000001" type="auto" />)
    expect(screen.getByText('138****0001')).toBeInTheDocument()
  })

  it('18 位身份证 → 身份证脱敏', () => {
    render(<SensitiveField value="110101199001011234" type="auto" />)
    expect(screen.getByText('110101********1234')).toBeInTheDocument()
  })

  it('邮箱 → 邮箱脱敏', () => {
    render(<SensitiveField value="zhangsan@example.com" type="auto" />)
    expect(screen.getByText('z***@example.com')).toBeInTheDocument()
  })

  it('⚠️ 恰好 18 个字符的邮箱 → 按邮箱脱敏(以前被当成身份证脱敏)', () => {
    // 'zhangsan1@exam.com' 正好 18 个字符 ⇒ 旧顺序先撞上"18 位身份证"分支,
    // 脱成 'zhangs********.com'(还泄露了域名形状)。现在邮箱优先。
    render(<SensitiveField value="zhangsan1@exam.com" type="auto" />)
    expect(screen.getByText('z***@exam.com')).toBeInTheDocument()
  })

  it('其他文本 → 按姓名脱敏', () => {
    render(<SensitiveField value="张三丰" type="auto" />)
    expect(screen.getByText('张**')).toBeInTheDocument()
  })
})

describe('SensitiveField · 可访问性', () => {
  it('⚠️ 眼睛按钮有可访问名(以前只有图标,读屏念不出来)', () => {
    render(<SensitiveField value="13800000001" type="phone" canView />)

    // antd 会把两个汉字之间插空格,所以用正则
    expect(screen.getByRole('button', { name: /查\s*看\s*明\s*文/ })).toBeInTheDocument()
  })

  it('明文态下的按钮叫"隐藏"', () => {
    render(<SensitiveField value="13800000001" type="phone" canView defaultVisible />)

    expect(screen.getByRole('button', { name: /隐\s*藏/ })).toBeInTheDocument()
  })
})
