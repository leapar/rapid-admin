/**
 * 源码扫描工具 —— 给"不许写死中文"这类守门测试用
 *
 * 为什么需要它:
 *   `t()` 查不到 key 时**原样返回 key**(永不空白、永不报错),所以"漏接 i18n"
 *   在运行时是**完全静默**的 —— 英文用户看到一句中文,没有日志、没有异常、
 *   CI 全绿。唯一的兜底就是**在测试里扫源码**。
 *
 * 为什么单独一个模块而不是每个测试各写一份:
 *   扫描器本身有真实坑(见下面 ⚠️),踩过一次就得记一辈子。抄一份 = 抄一份坑。
 *   2026-10-25 抽出本模块前,页头守门测试里的扫描器就漏了"属性值里嵌 JSX"
 *   这一种形态,同一个坑在两个测试文件里各埋了一次。
 *
 * ⚠️ **本模块不保证解析完整 JSX** —— 它是**正则 + 逐字符扫描**,不是解析器。
 *   它只需要回答一个问题:*某个开标签上有没有某个属性、那个属性是不是
 *   字符串字面量、字面量里有没有中文*。这个问题的答案不依赖完整解析。
 *   代价:遇到含 `>` 的字符串字面量等极端写法可能算错行号 —— 对守门测试
 *   来说,宁可多报(人来看一眼)也不能漏报。
 */
import { readdirSync } from 'node:fs'
import { join } from 'node:path'

/** 一个开标签上的直接属性 */
export interface JsxProp {
  name: string
  /**
   * 属性值的原始文本:
   *   - 字面量 → 含引号,例如 `"搜索用户名"` / `'中文'`
   *   - 表达式 → 含花括号,例如 `{t('common.placeholder.searchUser')}`
   */
  value: string
}

/** 一个 JSX 开标签 */
export interface JsxTag {
  /** 组件名,带点号,例如 `Input.Search` */
  name: string
  /** 在**去注释后**源码里的行号(1 起) */
  line: number
  /** 开标签原文 */
  openTag: string
  /** 深度 0 的直接属性 */
  props: JsxProp[]
}

/** CJK 统一表意文字 + 常用中文标点(全角括号/顿号也算,描述里常有) */
export const CJK = /[一-鿿　-〿]/

/**
 * 去掉注释,但**保留换行**。
 *
 * ⚠️ 保留换行是必须的:去掉换行会让后面算出来的行号全部漂移,
 *   报出来的位置对不上源码,守门测试就没人愿意看了。
 *
 * 字符串字面量(含模板字符串)整体跳过 —— 否则 `//` 出现在字符串里会被当注释。
 */
export function stripComments(src: string): string {
  let out = ''
  let i = 0
  const n = src.length
  while (i < n) {
    const c = src[i]!
    if (c === '/' && src[i + 1] === '/') {
      while (i < n && src[i] !== '\n') i++
      continue
    }
    if (c === '/' && src[i + 1] === '*') {
      i += 2
      while (i < n && !(src[i] === '*' && src[i + 1] === '/')) {
        if (src[i] === '\n') out += '\n'
        i++
      }
      i += 2
      continue
    }
    if (c === '"' || c === "'" || c === '`') {
      const q = c
      let j = i + 1
      while (j < n && src[j] !== q) {
        if (src[j] === '\\') j++
        j++
      }
      out += src.slice(i, j + 1)
      i = j + 1
      continue
    }
    out += c
    i++
  }
  return out
}

/**
 * 取出开标签的**直接属性**。
 *
 * ⚠️ **只认深度 0 的属性** —— 属性值里嵌套的 JSX 不算:
 *   <ManagementTable inlineToolBar={[<Tooltip title="已用 3/10" />]} />
 *   这里的 title 属于 Tooltip,不属于 ManagementTable。
 *
 * ⚠️ **必须先跳过泛型实参**(`<ManagementTable<ConfigAuditLog>`):
 *   否则那个 `>` 会被当成标签结束,后面的属性一个都扫不到 ——
 *   测试会**假绿**(实测踩过:改了字面量也不红)。
 */
export function directProps(openTag: string): JsxProp[] {
  const out: JsxProp[] = []
  let i = 1
  // 跳过标签名
  while (i < openTag.length && /[A-Za-z0-9_.]/.test(openTag[i]!)) i++
  // 跳过泛型实参 <T, U>
  if (openTag[i] === '<') {
    let d = 0
    while (i < openTag.length) {
      const c = openTag[i]
      if (c === '<') d++
      else if (c === '>') { d--; if (d === 0) { i++; break } }
      i++
    }
  }

  let depth = 0
  while (i < openTag.length) {
    const c = openTag[i]!
    if (c === '{' || c === '(' || c === '[') { depth++; i++; continue }
    if (c === '}' || c === ')' || c === ']') { depth--; i++; continue }
    if (depth === 0 && /[A-Za-z_]/.test(c)) {
      const mm = /^([A-Za-z_][A-Za-z0-9_]*)\s*=\s*/.exec(openTag.slice(i))
      if (mm) {
        const name = mm[1]!
        const v = i + mm[0].length
        if (openTag[v] === '{') {
          // 表达式值 → 记下原文,交给调用方判断是不是字符串字面量
          let d = 0
          let j = v
          while (j < openTag.length) {
            if (openTag[j] === '{') d++
            else if (openTag[j] === '}') { d--; if (d === 0) { j++; break } }
            j++
          }
          out.push({ name, value: openTag.slice(v, j) })
          i = j
          continue
        }
        if (openTag[v] === '"' || openTag[v] === "'") {
          const q = openTag[v]!
          const j = openTag.indexOf(q, v + 1)
          out.push({ name, value: openTag.slice(v, j === -1 ? v + 1 : j + 1) })
          i = j === -1 ? v + 1 : j + 1
          continue
        }
      }
    }
    i++
  }
  return out
}

/**
 * 找出源码里所有 JSX 开标签(大写字母开头的组件名)。
 *
 * ⚠️ **扫描位置必须从「标签名之后」继续,不能从「整个开标签之后」跳** ——
 *   开标签的属性值里常嵌着 JSX:
 *     <ManagementTable searchExtra={[<Select placeholder="按状态过滤" />]}>
 *   从开标签末尾起步会把整个 searchExtra 数组跳过,里面的 Select 一个都扫不到。
 *   (实测踩过:通扫版扫描器 40 → 33 处,平白少扫了 7 个筛选下拉。)
 *
 * 传入的源码**应当已经去过注释**(见 stripComments),否则注释里的示例
 * 会被当成真的标签。
 */
export function findJsxTags(src: string): JsxTag[] {
  const out: JsxTag[] = []
  const re = /<([A-Z][A-Za-z0-9_]*(?:\.[A-Za-z0-9_]+)*)\b/g
  let pos = 0
  // 行号用递增计数维护 —— start 单调递增,不用每次重扫全文
  let lastIdx = 0
  let line = 1
  while (pos < src.length) {
    re.lastIndex = pos
    const m = re.exec(src)
    if (m === null) break
    const name = m[1]!
    const start = m.index
    let i = start + m[0].length

    // ① 跳过泛型实参 <T, U>(可能嵌套,如 Record<string, (x) => void>)
    if (src[i] === '<') {
      let d = 0
      while (i < src.length) {
        const c = src[i]
        if (c === '<') d++
        else if (c === '>' && src[i - 1] !== '=') {
          d--
          if (d === 0) { i++; break }
        }
        i++
      }
    }

    // ② 扫到开标签自己的结束
    let depth = 0
    while (i < src.length) {
      const c = src[i]
      if (c === '{' || c === '(' || c === '[') depth++
      else if (c === '}' || c === ')' || c === ']') depth--
      else if (c === '>' && depth <= 0) break
      i++
    }
    const openTag = src.slice(start, i + 1)

    // 行号:把 [lastIdx, start) 之间的换行数加上
    for (let k = lastIdx; k < start; k++) if (src[k] === '\n') line++
    lastIdx = start

    out.push({ name, line, openTag, props: directProps(openTag) })
    pos = start + m[0].length
  }
  return out
}

/**
 * 从属性值里取出**字符串字面量**的内容。
 *
 * 三种写法都要认(本仓库三种都在用):
 *   "中文"   '中文'                 —— 裸引号
 *   {'中文'}  {"中文"}               —— JSX 里更常见的花括号包法
 *   {`中文`}                        —— 模板字符串(不含插值)
 *
 * ⚠️ 曾经只认第一种 —— `title={'中文'}` 整个漏网。
 *   而本仓库的写法恰恰是花括号形式,说明写的人习惯这么写;
 *   网只覆盖不常用的写法,等于没有网。
 *
 * ⚠️ 表达式(`{t('x')}` / `{cond ? 'a' : 'b'}`)不匹配 —— 它们
 *   本来就不该被判成"写死的中文"。表达式里内嵌的中文串用
 *   cjkLiteralsInExpr 单独查。
 */
export function literalValue(raw: string): string | null {
  const s = raw.trim()
  // 花括号包着的字面量
  const braced = /^\{\s*(['"`])([\s\S]*?)\1\s*\}$/.exec(s)
  if (braced) {
    const q = braced[1]!
    const body = braced[2]!
    if (q === '`' && body.includes('${')) return null
    return body
  }
  // 裸引号
  const bare = /^(['"`])([\s\S]*?)\1$/.exec(s)
  if (bare) {
    const q = bare[1]!
    const body = bare[2]!
    if (q === '`' && body.includes('${')) return null
    return body
  }
  return null
}

/**
 * 取出**表达式**里内嵌的中文字符串字面量。
 *
 * 覆盖这种写法:
 *   placeholder={it.value || '当前为空'}
 *   placeholder={usersLoading ? '加载中...' : '请选择负责人'}
 *
 * ⚠️ `t('common.placeholder.xxx')` 里的 key 不含中文,不会被误报 ——
 *   字典 key 一律用 ASCII。
 */
export function cjkLiteralsInExpr(raw: string): string[] {
  const out: string[] = []
  const re = /(['"`])([^'"`\n]*)\1/g
  let m: RegExpExecArray | null
  while ((m = re.exec(raw)) !== null) {
    const body = m[2]!
    if (CJK.test(body)) out.push(body)
  }
  return out
}

/**
 * 递归列出目录下的所有 .tsx。
 *
 * `__tests__` 和 `test` 目录**跳过** —— 守门测试要扫的是产品代码,
 * 测试夹具里写中文 placeholder 是正常的(那是"造数据",不是"漏接 i18n")。
 */
export function listTsx(dir: string, skipDirs: readonly string[] = ['__tests__', 'test']): string[] {
  return listByExt(dir, ['.tsx'], skipDirs)
}

/**
 * 递归列出目录下的所有 .ts / .tsx(写死中文扫描要用,那是全仓范围,
 * 不只是页面 —— api/ utils/ components/ 里写死的中文同样是 bug)。
 */
export function listSourceFiles(
  dir: string,
  skipDirs: readonly string[] = ['__tests__', 'test'],
): string[] {
  return listByExt(dir, ['.ts', '.tsx'], skipDirs)
}

/** 按后缀列文件的公共实现 */
function listByExt(dir: string, exts: string[], skipDirs: readonly string[]): string[] {
  const out: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name)
    if (entry.isDirectory()) {
      if (skipDirs.includes(entry.name)) continue
      out.push(...listByExt(p, exts, skipDirs))
    } else if (exts.some((e) => entry.name.endsWith(e))) {
      out.push(p)
    }
  }
  return out
}
