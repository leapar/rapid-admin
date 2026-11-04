/**
 * 表格列标题扫描 —— 给「列标题必须接 i18n」守门测试用
 *
 * 为什么单独一个模块(而不是塞进 sourceScan.ts):
 *   sourceScan 扫的是 **JSX 属性**(placeholder="…"),这里扫的是
 *   **对象字面量里的 title 字段**(`{ title: '状态', dataIndex: … }`),
 *   两者形态完全不同,放一起反而互相干扰。
 *
 * 核心判据:**一个 title 属于表格列,当且仅当它所在的对象字面量里
 * 同时出现了 dataIndex / render / key: / sorter / ellipsis 之一。**
 *
 *   - 列定义必然有这些(`{ title, dataIndex, width, render }`);
 *   - 而 `modal.confirm({ title, content, width })`、`actionConfig = { x: { title, desc } }`
 *     这些**不是**列 —— 所以 `width` 故意**不在**同伴名单里:
 *     它在弹窗配置里太常见,加上它会把一堆弹窗标题误判成列标题。
 *
 * ⚠️ **必须按"所在对象字面量"判,不能按"所在数组"判** —— 列数组有 5 种写法
 *   (`const columns = [` / `const columns: ColumnsType<X> = [` /
 *   `useMemo(() => [...], [])` / `columns={[...]}` 内联 / `columns={cols}`),
 *   按数组判就是加一种漏一种;按对象判则一次到位。
 */
import { stripComments } from './sourceScan'

/** 一个表格列的 title */
export interface ColumnTitle {
  /** 在**去注释后**源码里的行号(1 起) */
  line: number
  /** title 表达式的原始文本,例如 `'状态'` 或 `t('common.status')` */
  expr: string
  /** expr 是 t('key') 形式时的 key;否则 null */
  key: string | null
  /** expr 是纯字符串字面量时的内容(含引号);否则 null */
  literal: string | null
}

/** 列定义必然有的同伴属性 —— 见文件头说明 */
const COLUMN_SIBLINGS = ['dataIndex', 'render', 'key:', 'sorter', 'ellipsis']

/** 跳过一段字符串(含模板字符串),返回结束后的下标 */
function skipString(s: string, i: number): number {
  const q = s[i]!
  let j = i + 1
  while (j < s.length && s[j] !== q) {
    if (s[j] === '\\') j++
    j++
  }
  return j + 1
}

/** 位置 i 处是不是一个 `title`(后接可选空白 + `:`) */
function isTitleKey(s: string, i: number): boolean {
  if (!s.startsWith('title', i)) return false
  // 前面不能是标识符字符 / 点 —— 否则是 subTitle / obj.title / document.title
  const prev = i > 0 ? s[i - 1]! : ''
  if (/[A-Za-z0-9_$.]/.test(prev)) return false
  let j = i + 5
  while (j < s.length && /\s/.test(s[j]!)) j++
  return s[j] === ':'
}

/**
 * 读一个表达式的结束位置。
 *
 * 深度 0 的 `,` 或换行 = 表达式结束;字符串整体跳过;
 * 括号/方括号/花括号算深度 —— 所以 `t('a')` 和 `` `x${y}` `` 都不会被截断。
 */
function readExpr(s: string, start: number): number {
  let i = start
  let depth = 0
  while (i < s.length) {
    const c = s[i]!
    if (c === '"' || c === "'" || c === '`') {
      i = skipString(s, i)
      continue
    }
    if (c === '(' || c === '[' || c === '{') {
      depth++
      i++
      continue
    }
    if (c === ')' || c === ']' || c === '}') {
      if (depth === 0) return i
      depth--
      i++
      continue
    }
    if (depth === 0 && (c === ',' || c === '\n')) return i
    i++
  }
  return i
}

/**
 * 取对象字面量**自己这一层**的属性文本 —— 嵌套的 {...} / [...] / (...) 整块抹掉。
 *
 * ⚠️ 为什么不能直接拿整个对象文本去 `includes`(2026-10-25 Module B-4 撞上的):
 *   判「是不是列」靠同伴属性 `key:`,而列定义里最常见的写法是
 *   `{ title: t('x', undefined, { key: id }) }` —— **内层**那个 `{ key: … }`
 *   恰好含 "key:"。于是 `modal.error({ title: t(…), content: …, width: 600 })`
 *   这种**弹窗**配置会被误判成"表格列",守门网就报了个假阳性
 *   (说"列标题用了 page.job.* 这种非 field.* 的键")。
 *   同伴属性必须是这个对象**自己**的,嵌套在里面的不算。
 */
function ownPropertyText(s: string, objStart: number, close: number): string {
  let out = ''
  let depth = 0
  let i = objStart
  while (i < close) {
    const c = s[i]!
    if (c === '"' || c === "'" || c === '`') {
      const end = skipString(s, i)
      if (depth === 1) out += s.slice(i, end)
      i = end
      continue
    }
    if (c === '{' || c === '[' || c === '(') {
      depth++
      i++
      continue
    }
    if (c === '}' || c === ']' || c === ')') {
      depth--
      i++
      continue
    }
    if (depth === 1) out += c
    i++
  }
  return out
}

/**
 * 找出源码里所有**表格列**的 title。
 *
 * 传入的源码可以带注释 —— 内部会先 stripComments。
 */
export function findColumnTitles(src: string): ColumnTitle[] {
  const s = stripComments(src)
  const n = s.length
  /** '{' 位置栈 */
  const stack: number[] = []
  /** 开括号位置 → 闭括号位置 */
  const closeOf = new Map<number, number>()
  const raw: { exprStart: number; exprEnd: number; objStart: number }[] = []

  let i = 0
  while (i < n) {
    const c = s[i]!
    if (c === '"' || c === "'" || c === '`') {
      i = skipString(s, i)
      continue
    }
    if (c === '{') {
      stack.push(i)
      i++
      continue
    }
    if (c === '}') {
      const open = stack.pop()
      if (open !== undefined) closeOf.set(open, i)
      i++
      continue
    }
    if (isTitleKey(s, i)) {
      const colon = s.indexOf(':', i)
      let j = colon + 1
      while (j < n && /\s/.test(s[j]!)) j++
      const exprEnd = readExpr(s, j)
      raw.push({
        exprStart: j,
        exprEnd,
        objStart: stack.length > 0 ? stack[stack.length - 1]! : -1,
      })
      i = exprEnd > j ? exprEnd : colon + 1
      continue
    }
    i++
  }

  const out: ColumnTitle[] = []
  let line = 1
  let lastIdx = 0
  for (const r of raw) {
    for (let k = lastIdx; k < r.exprStart; k++) if (s[k] === '\n') line++
    lastIdx = r.exprStart

    const expr = s.slice(r.exprStart, r.exprEnd).trim()

    // ① 表达式既不是字符串字面量也不是 t() 调用 → 不判
    //    (类型注解 `title: string`、变量 `title: someVar` 都不是我们要管的)
    const lit = /^(['"`])([\s\S]*?)\1$/.exec(expr)
    const tCall = /^t\(\s*(['"])([^'"]+)\1/.exec(expr)
    if (!lit && !tCall) continue

    // ② 所在对象字面量里没有列的同伴属性 → 不是列(弹窗标题 / 动作配置等)
    if (r.objStart < 0) continue
    const close = closeOf.get(r.objStart)
    if (close === undefined) continue
    const objText = ownPropertyText(s, r.objStart, close)
    if (!COLUMN_SIBLINGS.some((sib) => objText.includes(sib))) continue

    out.push({
      line,
      expr,
      key: tCall ? tCall[2]! : null,
      literal: lit ? lit[2]! : null,
    })
  }
  return out
}
