/**
 * 头像的两个纯函数 —— 首字母 + 稳定底色(2026-10-29 M113)
 *
 * ## 为什么单独一个文件
 *
 * ① **能测**:这两个是**纯函数**,塞在组件文件里就只能靠渲染整个组件来测。
 *    抽出来之后 `__tests__/userAvatar.test.tsx` 能直接对它们下断言。
 * ② **不污染组件模块**:它们是组件文件里的**非组件导出**,
 *    放在一起会触发 `react-refresh/only-export-components` 警告
 *    (而本仓 `npm run lint` 是 `--max-warnings 0`,警告即红)。
 *
 * 跟 `pages/system/dict/isActive.ts` 是同一个套路(那边是字典 `is_active` 的
 * 表单层 ↔ wire 层转换)。
 */

/**
 * 兜底背景色(与 antd 5 的色板同一套,不会在浅色底上糊掉)
 * ⚠️ 2026-11-05 去蓝:皮肤体系里蓝是外来色(政务红/文博棕金下打架),
 * blue / geekblue 两个槽位换成暖橙 / 深绿,其余不动。
 */
const FALLBACK_COLORS = [
  '#d46b08',
  '#722ed1',
  '#eb2f96',
  '#fa8c16',
  '#a0d911',
  '#13c2c2',
  '#389e0d',
  '#08979c',
]

/**
 * 取名字的第一个字符当头像文字
 *
 * ⚠️ **按「码点」取,不是按「UTF-16 码元」取**
 *
 * JS 的 `str[0]` 是 **UTF-16 码元**下标,既不是字节下标也不是码点下标。
 * 这个区别在两种字符上表现完全不同,别混为一谈:
 *
 *  · **BMP 字符**(中文 / 拉丁文 / 数字 / 常用标点)只占**一个**码元
 *    ⇒ `str[0]` 和按码点取**结果完全一样**。
 *    ⚠️ 所以「按字节切中文名会乱码」是句**错话** —— JS 字符串压根没有
 *      "字节下标"这回事,`str[0]` 拿到的就是完整的「张」。
 *
 *  · **代理对字符**(emoji、`𠮷` 这类 supplementary plane)占**两个**码元
 *    ⇒ `str[0]` 只拿到**半个代理对** ⇒ 渲染出来是「豆腐块」(U+FFFD)。
 *    **这才是真正会坏的那种。**
 *
 * `Array.from(str)` 按**码点**迭代 ⇒ 代理对整块交出来,两种字符都对。
 */
export function avatarInitial(name?: string | null): string {
  if (!name) return ''
  const trimmed = name.trim()
  if (!trimmed) return ''
  // Array.from 按码点切分,中文/emoji 都拿得完整
  // (TS 的 noUncheckedIndexedAccess 下取出来是 string | undefined —— 兜一层)
  const first = Array.from(trimmed)[0] ?? ''
  return first.toUpperCase()
}

/**
 * 名字 → 稳定的背景色
 *
 * 用的是 FNV-1a 的简化版(和后端 sonyflake 里的 fnv1aLow16 同一个思路):
 * 同一个人永远同一个颜色,不同人大概率不同。
 *
 * ⚠️ **不用随机** —— 随机色每次刷新都变,用户会以为头像"坏了"。
 */
export function avatarColor(name?: string | null): string {
  const s = name ?? ''
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  // `?? 兜底` 是给 TS 的 noUncheckedIndexedAccess 用的(下标取值可能是 undefined)
  return FALLBACK_COLORS[Math.abs(h) % FALLBACK_COLORS.length] ?? FALLBACK_COLORS[0]!
}
