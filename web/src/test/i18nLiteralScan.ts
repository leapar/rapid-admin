/**
 * 写死中文扫描 —— 给「字典里已有对应文案的硬编码中文 = 缺陷」守门测试用
 *
 * ## 这条网守的是什么
 *
 *   `t()` 查不到 key 时**原样返回 key**(永不空白、永不报错),所以"漏接 i18n"
 *   在运行时是**完全静默**的 —— 英文用户看到一句中文,没有日志、没有异常、CI 全绿。
 *   源码扫描是唯一的兜底。
 *
 * ## 为什么用 TypeScript 编译器 API 而不是正则
 *
 *   之前的手写扫描器(sourceScan / columnScan)只回答
 *   "某个 JSX 属性是不是字面量"这种**局部**问题,够用。
 *   但本模块要回答的是**全仓任意位置**的中文字面量 —— 包括
 *   对象字面量的字段值、数组元素、函数实参、模板字符串片段……
 *   这些位置没有统一的语法特征,正则要写十几条规则,而且**每条都得排除
 *   注释、字符串里的转义、t() 的兜底参数**,漏一条就误报/漏报。
 *
 *   用 `ts.createSourceFile` 拿 AST 就没有这些问题:
 *   - 注释根本不在 AST 里(不会误报);
 *   - 每个字面量的父节点是确定的(能准确判断它是不是 t() 的兜底参数);
 *   - JSX 文本 / JSX 属性 / 普通字符串 / 模板片段四种形态天然分得开。
 *
 * ## ⚠️ 兜底参数不是"漏接 i18n"
 *
 *   `t('common.status', '状态')` —— 第 2 个实参是**兜底文案**,
 *   只有在字典里**真的查不到** key 时才会被用上。
 *   字典里查不到就是 bug(那由"键必须存在"那条断言管),
 *   但"写了兜底"本身不是 bug。所以这里要**排除**它。
 *
 *   同理 `tKey()` / `tError()`(axios 拦截器用的非 hook 版本)也是一样的约定。
 */
import ts from 'typescript'
import { CJK } from './sourceScan'

/** 第 2 个实参是"兜底文案"的几个函数 */
const FALLBACK_FNS = new Set(['t', 'tKey', 'tError'])

/** 字面量的形态 —— 用来在报错里说清楚"这是哪儿写死的" */
export type CjkLiteralKind =
  /** JSX 文本:`<span>角色</span>` */
  | 'jsx-text'
  /** JSX 属性:`title="角色"` / `title={'角色'}` */
  | 'jsx-attr'
  /** 普通字符串字面量:对象字段值 / 数组元素 / 函数实参 / return '…' */
  | 'string'
  /** 模板字符串(片段) */
  | 'template'

/** 一处写死的中文 */
export interface CjkLiteral {
  /** 在源码里的行号(1 起) */
  line: number
  /** 字面量的文本内容(JSX 文本已 trim) */
  text: string
  /** 形态 */
  kind: CjkLiteralKind
  /** 是不是 t()/tKey()/tError() 的兜底参数 */
  isFallback: boolean
}

/**
 * 找出包住这个模板片段的**模板表达式**节点。
 *
 * ⚠️ TS 的 AST 里,模板片段的父节点**不是**统一的:
 *   - `TemplateHead`     的父节点是 `TemplateExpression`
 *   - `TemplateMiddle` / `TemplateTail` 的父节点是 **`TemplateSpan`**,
 *     祖父才是 `TemplateExpression`
 *   - `NoSubstitutionTemplateLiteral`(无插值的模板)父节点**直接**是调用处
 *   所以不能只往上走一层 —— 2026-10-25 实测踩到:
 *   `tKey('common.total', \`共 ${n} 条\`)` 的**前半段**被识别成兜底、
 *   **后半段**没识别,于是 useTable.ts 的分页文案被误报成"写死的中文"。
 */
function templateRoot(node: ts.Node): ts.Node {
  if (ts.isTemplateHead(node)) return node.parent
  if (ts.isTemplateMiddle(node) || ts.isTemplateTail(node)) {
    const span = node.parent
    return span && span.parent ? span.parent : node
  }
  return node
}

/** 这个字面量是不是 t()/tKey()/tError() 的**兜底参数**(第 2 个实参) */
function isFallbackArg(node: ts.Node): boolean {
  // 模板片段:先回到模板表达式这一层,再判断它是不是调用表达式的兜底实参
  const arg: ts.Node =
    ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node)
      ? templateRoot(node)
      : node
  const p = arg.parent
  if (!p || !ts.isCallExpression(p)) return false
  // 实参下标必须是 1(第 2 个);下标 0 是 key,2+ 是插值变量
  if (p.arguments.indexOf(arg as ts.Expression) !== 1) return false
  const callee = p.expression
  // 只认裸函数名 t/tKey/tError —— `message.error(t('a'), '中文')` 的 '中文'
  // 是 message.error 的第 2 个实参,不是 t 的兜底,不能排除
  if (!ts.isIdentifier(callee)) return false
  return FALLBACK_FNS.has(callee.text)
}

/** 按父节点判断形态 */
function kindOf(node: ts.Node): CjkLiteralKind {
  const p = node.parent
  if (p && (ts.isJsxAttribute(p) || ts.isJsxExpression(p))) return 'jsx-attr'
  return 'string'
}

/**
 * 找出源码里所有**写死的中文**。
 *
 * 传入的源码会被当成 TSX / TS 解析(按 `fileName` 的后缀决定),
 * 注释不参与(TS AST 里就没有注释节点)。
 */
export function findCjkLiterals(src: string, fileName = 'x.tsx'): CjkLiteral[] {
  const kind = fileName.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  const sf = ts.createSourceFile(fileName, src, ts.ScriptTarget.Latest, true, kind)
  const out: CjkLiteral[] = []

  const push = (node: ts.Node, text: string, k: CjkLiteralKind, fallback: boolean) => {
    const lc = sf.getLineAndCharacterOfPosition(node.getStart(sf))
    out.push({ line: lc.line + 1, text, kind: k, isFallback: fallback })
  }

  const walk = (node: ts.Node): void => {
    if (ts.isJsxText(node)) {
      const text = node.text.trim()
      if (text && CJK.test(text)) push(node, text, 'jsx-text', false)
    } else if (ts.isStringLiteral(node)) {
      if (CJK.test(node.text)) push(node, node.text, kindOf(node), isFallbackArg(node))
    } else if (ts.isNoSubstitutionTemplateLiteral(node)) {
      if (CJK.test(node.text)) push(node, node.text, 'template', isFallbackArg(node))
    } else if (
      ts.isTemplateHead(node) ||
      ts.isTemplateMiddle(node) ||
      ts.isTemplateTail(node)
    ) {
      if (CJK.test(node.text)) push(node, node.text, 'template', isFallbackArg(node))
    }
    ts.forEachChild(node, walk)
  }

  walk(sf)
  return out
}

/**
 * 把嵌套字典摊平成"所有字符串叶子值"的集合。
 *
 * 用它来判断"这句写死的中文,字典里是不是**已经有**对应文案"。
 */
export function dictValues(obj: unknown, out: Set<string> = new Set()): Set<string> {
  if (obj == null || typeof obj !== 'object') return out
  for (const v of Object.values(obj as Record<string, unknown>)) {
    if (v != null && typeof v === 'object' && !Array.isArray(v)) dictValues(v, out)
    else if (typeof v === 'string') out.add(v)
  }
  return out
}

/**
 * 抽出文案里的 `{占位符}` 名字。
 *
 *   '已下线 {n} 个设备' → ['n']
 *
 * ⚠️ 只认**完整的** `{name}` —— `{'{a}'}` 这种嵌套的、`{` 后面不是标识符的,
 *   一律不认(否则 JSON 片段那种会炸出一堆噪音)。
 */
export function placeholders(text: string): string[] {
  const out: string[] = []
  const re = /\{([A-Za-z_][A-Za-z0-9_]*)\}/g
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) out.push(m[1]!)
  return out
}

/** 一次 t() / tKey() / tError() 调用 */
export interface TCall {
  /** 行号(1 起) */
  line: number
  /** 第 1 个实参(字面量 key);不是字面量时为 null(动态 key,查不了) */
  key: string | null
}

/**
 * 找出源码里所有 `t(...)` / `tKey(...)` / `tError(...)` 调用的 key。
 *
 * ⚠️ 动态 key(模板字符串 / 变量)拿不到 —— 这里记成 key: null,
 *   调用方**不要**把它当成"键不存在"(那是误报),要当成"查不了"。
 */
export function findTCalls(src: string, fileName = 'x.tsx'): TCall[] {
  const kind = fileName.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  const sf = ts.createSourceFile(fileName, src, ts.ScriptTarget.Latest, true, kind)
  const out: TCall[] = []

  const walk = (node: ts.Node): void => {
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      FALLBACK_FNS.has(node.expression.text)
    ) {
      const a0 = node.arguments[0]
      const lc = sf.getLineAndCharacterOfPosition(node.getStart(sf))
      out.push({
        line: lc.line + 1,
        key: a0 && ts.isStringLiteral(a0) ? a0.text : null,
      })
    }
    ts.forEachChild(node, walk)
  }

  walk(sf)
  return out
}

/** 一处"写死的中文恰好等于字典里已有文案"的违规 */
export interface DictDuplicate {
  line: number
  text: string
  kind: CjkLiteralKind
}

/**
 * 找出"写死的中文,且**字典里已经有同样的文案**"的违规点。
 *
 * ## 这条判据守的是什么
 *
 *   字典里已经有的文案,英文侧也一定已经翻好了(en-US.ts 与 zh-CN.ts 键对齐,
 *   由 locale-parity.test.ts 管)。这时候代码里还写着一份中文,意味着
 *   **英文用户永远看不到那句英文** —— 界面在英文环境下直接显示中文,
 *   而且是**完全静默**的:`t()` 查不到 key 时原样返回 key,不报错、不告警。
 *
 *   这类"死点"是纯浪费:翻译已经做完了,接线没做。
 *
 * ## 为什么这个判据会**越用越强**
 *
 *   以后每往字典里加一个键,任何还没接线的同款写死中文就自动变成违规 ——
 *   守门网不需要跟着改,自己就长出了新的牙齿。
 *
 * ⚠️ **兜底参数不算违规**(`t('x', '中文')`):兜底只在字典查不到 key 时才生效,
 *   而"查不到 key"本身是另一条断言的事(键必须存在)。见 isFallbackArg。
 */
export function findDictDuplicates(
  src: string,
  fileName: string,
  values: ReadonlySet<string>,
): DictDuplicate[] {
  return findCjkLiterals(src, fileName)
    .filter((l) => !l.isFallback && values.has(l.text))
    .map((l) => ({ line: l.line, text: l.text, kind: l.kind }))
}
