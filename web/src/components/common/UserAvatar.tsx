/**
 * UserAvatar —— 用户头像(带默认头像兜底)
 *
 * # 为什么要有这个组件(而不是直接用 antd 的 <Avatar src=...>)
 *
 * antd 的 Avatar 有个**很反直觉**的行为:
 *
 * ```jsx
 * if (typeof src === 'string' && isImgExist) → <img src={src} />
 * else if (hasImageElement)               → src
 * else if (icon)                           → icon      ← 兜底在这
 * else                                     → children  ← 首字母在这
 * ```
 *
 * 也就是说:**只要 `src` 是字符串,antd 就一定会先渲染 `<img>`**,
 * 哪怕这个字符串是**空串**。
 *
 * 而 `src=""` 在浏览器里 = **裂图**(不是"没有图"):
 *   · `<img src="">` 会向当前页面 URL 发一次请求,拿到的是 HTML → 解析失败 → 裂图
 *   · 空 src **不会触发 `onError`**(浏览器压根没"加载失败"这个事件)
 *   ⇒ antd 的 icon 兜底**永远等不到**
 *
 * 后果(2026-10-29 M113 修):
 *   · 个人中心左边的 80px 大头像:没设过头像的用户看到的是**一个裂图图标**
 *   · 用户管理页的列表头像:同一个坑,而且**每一个没头像的用户都是裂图**
 *
 * # 这个组件做三件事
 *
 *  1. **空 src 直接不渲染 `<img>`** —— 没有头像就是没有头像,不是裂图
 *  2. **图片加载失败自动回落** —— 自己管 `onError`,回落成首字母/图标
 *  3. **首字母兜底** —— 中文取第一个字(按**码点**取,不是按 UTF-16 码元),
 *     英文取第一个字母大写;背景色由名字散列决定 ⇒ 同一个人永远是同一个颜色
 *
 * # 颜色从哪来
 *
 * 8 个品牌色按名字散列取一个。**不用随机** —— 随机色每次刷新都变,
 * 用户会以为头像"坏了"。
 *
 * ⚠️ `avatarInitial` / `avatarColor` 这两个**纯函数**放在
 * `components/common/avatar.ts` 里(不在本文件)——
 * 一是能单独测,二是别触发 `react-refresh/only-export-components` 警告。
 */
import { useEffect, useMemo, useState } from 'react'
import type { CSSProperties, FC } from 'react'
import { Avatar } from 'antd'
import { UserOutlined } from '@ant-design/icons'
import { avatarColor, avatarInitial } from './avatar'

export interface UserAvatarProps {
  /** 头像 URL(OSS 直传后的地址);空串 / undefined = 没有头像 */
  src?: string | null
  /** 用于生成首字母与背景色:优先 nickname,其次 username */
  name?: string | null
  /** antd Avatar 的尺寸:number = 像素,或 'small' | 'default' | 'large' */
  size?: number | 'small' | 'default' | 'large'
  style?: CSSProperties
  className?: string
  /** 无障碍:读屏时念出来的名字 */
  alt?: string
}

/** 兜底背景色与首字母取法在 `./avatar` 里(纯函数单独一个文件,见文件头说明) */

export const UserAvatar: FC<UserAvatarProps> = ({
  src,
  name,
  size = 'default',
  style,
  className,
  alt,
}) => {
  const url = (src ?? '').trim()
  // 图片加载失败 ⇒ 回落
  const [broken, setBroken] = useState(false)

  // ⚠️ src 变了要重置"失败"标记 —— 否则换了一张**能加载**的图,
  //   组件仍然停在"失败"状态,头像再也不出来了。
  useEffect(() => {
    setBroken(false)
  }, [url])

  const initial = useMemo(() => avatarInitial(name), [name])
  const bg = useMemo(() => avatarColor(name), [name])

  // 没有头像 / 头像挂了 ⇒ 渲染首字母(或通用人形图标)
  //
  // ⚠️ **图标走 antd 的 `icon` prop,不是 children**
  //   antd 的渲染顺序是 `icon` **优先于** `children`:
  //     if (src 是字符串) → <img>
  //     else if (hasImageElement) → src
  //     else if (icon) → icon        ← 在 children 前面
  //     else → children
  //   所以两者只能给一个:有首字母就给首字母,没首字母才给 icon。
  //   (以前把 <UserOutlined/> 当 children 传,antd 会把它塞进
  //    `.ant-avatar-string` 里 —— 尺寸按"文字"算,不是按图标算。)
  if (!url || broken) {
    return (
      <Avatar
        size={size}
        className={className}
        style={{ backgroundColor: bg, ...style }}
        icon={initial ? undefined : <UserOutlined />}
        aria-label={alt ?? name ?? undefined}
      >
        {initial || undefined}
      </Avatar>
    )
  }

  return (
    <Avatar
      size={size}
      className={className}
      style={style}
      src={url}
      alt={alt ?? name ?? undefined}
      /**
       * ⚠️ 返回 false = "别用 antd 自己的兜底"(antd 会把 isImgExist 置 false,
       *   然后渲染 `icon` —— 而我们想渲染的是**首字母**,不是图标)。
       *   返回 false 让 antd 保持现状,由我们自己的 `broken` 状态接管。
       */
      onError={() => {
        setBroken(true)
        return false
      }}
    >
      {initial || <UserOutlined />}
    </Avatar>
  )
}

export default UserAvatar
