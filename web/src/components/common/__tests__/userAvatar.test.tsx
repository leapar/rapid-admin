/**
 * UserAvatar · 头像兜底回归网(M113,2026-10-29)
 *
 * ## 这条网守的是什么
 *
 * antd 5 的 Avatar 渲染顺序(`node_modules/antd/es/avatar/avatar.js`):
 *
 *   if (typeof src === 'string' && isImgExist) → <img src={src} />
 *   else if (hasImageElement)                 → src
 *   else if (icon)                             → icon   ← 兜底在这
 *   else                                       → children(首字母)
 *
 * ⇒ **只要 `src` 是字符串,antd 就一定先渲染 `<img>`**,哪怕这个字符串是空串。
 *
 * 而 `<img src="">` 在浏览器里是**裂图**(不是"没有图"):
 *   · 它会向当前页面 URL 发一次请求,拿到 HTML → 解析失败 → 裂图
 *   · 空 src **不触发 onError**(浏览器压根没有"加载失败"这个事件)
 *   ⇒ antd 的 icon 兜底**永远等不到**
 *
 * 用户 2026-10-29 报的就是这个:"头像加载失败时候要有个默认的头像"。
 * 以前个人中心(80px 大头像)和用户管理列表(**每一个**没头像的用户)
 * 都是裂图 —— 这不是显示瑕疵,是把 `<img src="">` 直接甩给了浏览器。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   · 把 UserAvatar 里的 `if (!url || broken)` 改成 `if (false)`
 *     → 「空 src 不渲染 img」「加载失败回落」两条立刻变红
 *   · 把 `avatarInitial` 里的 `Array.from(trimmed)[0]` 改回 `trimmed[0]`
 *     → 「emoji / 扩展汉字(代理对)不能被切成半个」变红
 *     (`str[0]` 是 UTF-16 **码元**下标,代理对字符只拿到半个 ⇒ 豆腐块)
 *   · 删掉 `useEffect(() => setBroken(false), [url])`
 *     → 「换头像后能重新加载」变红(失败标记会永远粘住)
 */
import { describe, it, expect } from 'vitest'
import { render, fireEvent } from '@testing-library/react'
import { UserAvatar } from '@/components/common/UserAvatar'
import { avatarColor, avatarInitial } from '@/components/common/avatar'

describe('UserAvatar · 没有头像 / 头像挂了,都不能是裂图', () => {
  it('src 是空串:不渲染 <img>(空 src 在浏览器里就是裂图)', () => {
    const { container } = render(<UserAvatar src="" name="张三" />)
    expect(container.querySelector('img')).toBeNull()
  })

  it('src 是 undefined / null:同样不渲染 <img>', () => {
    const a = render(<UserAvatar name="张三" />)
    expect(a.container.querySelector('img')).toBeNull()
    const b = render(<UserAvatar src={null} name="张三" />)
    expect(b.container.querySelector('img')).toBeNull()
  })

  it('src 只有空白:也当"没有头像"(不该为一个空格去请求页面 URL)', () => {
    const { container } = render(<UserAvatar src="   " name="张三" />)
    expect(container.querySelector('img')).toBeNull()
  })

  it('有 src:渲染 <img src=...>', () => {
    const { container } = render(<UserAvatar src="https://cdn.example.com/a.png" name="张三" />)
    const img = container.querySelector('img')
    expect(img).not.toBeNull()
    expect(img!.getAttribute('src')).toBe('https://cdn.example.com/a.png')
  })

  it('图片加载失败:回落成首字母,<img> 消失', () => {
    const { container } = render(<UserAvatar src="https://cdn.example.com/a.png" name="张三" />)
    const img = container.querySelector('img')
    expect(img).not.toBeNull()

    fireEvent.error(img!)

    expect(container.querySelector('img')).toBeNull()
    expect(container.textContent).toContain('张')
  })

  it('换了一张新头像:失败标记必须重置(否则换了好图也还是显示不出来)', () => {
    const { container, rerender } = render(
      <UserAvatar src="https://cdn.example.com/bad.png" name="张三" />,
    )
    fireEvent.error(container.querySelector('img')!)
    expect(container.querySelector('img')).toBeNull()

    rerender(<UserAvatar src="https://cdn.example.com/good.png" name="张三" />)
    expect(container.querySelector('img')).not.toBeNull()
  })

  it('连名字都没有:退回通用人形图标(不是空白)', () => {
    const { container } = render(<UserAvatar src="" />)
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('.ant-avatar-icon')).not.toBeNull()
  })
})

describe('avatarInitial · 首字母必须按「码点」取,不是按 UTF-16 码元取', () => {
  it('中文名取第一个**字**', () => {
    // ⚠️ 中文在 BMP 里、只占一个码元 ⇒ `str[0]` 和按码点取**结果一样**。
    //   这条断言守的是"别退化成按字节/按半个字符切"的**底线**,
    //   但它**不能**单独证伪 `str[0]` 那种写法(见下面那条 emoji 用例)。
    expect(avatarInitial('张三')).toBe('张')
    expect(avatarInitial('张三丰')).toBe('张')
    expect(avatarInitial('欧阳修')).toBe('欧')
  })

  it('emoji / 扩展汉字(代理对)不能被切成半个', () => {
    // ⚠️ **这条才是真能证伪的那条。**
    //   JS 的 `str[0]` 是 UTF-16 **码元**下标:代理对字符占两个码元,
    //   `str[0]` 只拿到**半个代理对** ⇒ 渲染出来是「豆腐块」(U+FFFD)。
    //   (中文不会中招 —— 它在 BMP 里。)
    expect(avatarInitial('😀 张三')).toBe('😀')
    expect(avatarInitial('𠮷家')).toBe('𠮷')

    // 半截代理对长这样:码元数是 1,但它不是一个完整的字符
    const half = '😀'[0]!
    expect(half).toHaveLength(1)
    expect(avatarInitial('😀')).not.toBe(half)
  })

  it('英文名取首字母并大写', () => {
    expect(avatarInitial('admin')).toBe('A')
    expect(avatarInitial('Bob')).toBe('B')
  })

  it('空名 / 空白名 ⇒ 空串(交给图标兜底)', () => {
    expect(avatarInitial('')).toBe('')
    expect(avatarInitial('   ')).toBe('')
    expect(avatarInitial(undefined)).toBe('')
    expect(avatarInitial(null)).toBe('')
  })
})

describe('avatarColor · 同一个名字永远是同一个颜色', () => {
  it('同名字同色(不能随机 —— 随机色每次刷新都变,用户会以为头像坏了)', () => {
    expect(avatarColor('张三')).toBe(avatarColor('张三'))
  })

  it('颜色取自固定色板(是个合法的 hex)', () => {
    expect(avatarColor('张三')).toMatch(/^#[0-9a-f]{6}$/)
  })

  it('色板不是退化的(20 个名字至少落在 2 个颜色上)', () => {
    // ⚠️ 故意**不**断言"任意两个不同名字颜色必不同":
    //   色板只有 8 个颜色,不同名字撞色是**设计上允许**的
    //   (初字母不一样,人还是分得出来的)。
    //   写死"这两个名字必须不同色"就是拿一条假命题当断言。
    const names = [
      '张', '李', '王', '赵', '陈', '刘', '杨', '黄', '周', '吴',
      '徐', '孙', '马', '朱', '胡', '郭', '何', '高', '林', '罗',
    ]
    const colors = new Set(names.map((n) => avatarColor(n)))
    expect(colors.size).toBeGreaterThan(1)
  })
})
