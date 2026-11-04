/**
 * 页头文案不许写死中文 + 页头一律不许传 title(2026-10-25)
 *
 * **这条守的是 2026-10-25 用户报的那个 bug:「有几个地方 <PageHeader 的
 * description 没有兼容 i18n」。**
 *
 * 为什么值得单独一条测试:
 *   - `t()` 查不到 key 时**原样返回 key**(永不空白、永不报错),
 *     所以"漏接 i18n"在运行时是**完全静默**的 —— 英文用户看到的是一句中文,
 *     没有任何日志、没有异常、CI 全绿。
 *   - 字典本身是有键的(`page.dept.description` 早就在),缺的只是**接线**;
 *     键在不在字典里由 locale-parity.test.ts 管,接没接线由本条管。
 *     两条合起来才是"页头文案既能切语言、也不会缺键"。
 *
 * 第二条守的是同一天的另一条指令:「所有 <PageHeader> 的地方都不要传 title」。
 *   标题的唯一权威是布局里的面包屑(BasicLayout → Breadcrumb,按路由自动
 *   生成菜单名)。页内再写一遍就是同一句话说两遍。
 *
 *   为什么这条要独立于"typecheck 会报错":
 *   PageHeader / ManagementTable 的 props 接口里**已经没有** title / pageTitle
 *   了 —— 编译器当然会拦。但 web/ 的 tsc 基线有 53 个既有错误,再多一个
 *   混在里面很容易被漏看。测试是独立于 tsc 基线的第二道网。
 *
 * 检查方式:扫源码,取 <PageHeader> / <ManagementTable> 的**开标签**,
 *   ① 直接属性里 title / pageTitle 一律不许出现
 *   ② 其余直接属性的值如果是**字符串字面量**且含中日韩字符 → 写死的中文
 *
 * 为什么只扫这两个标签:
 *   `description=` 这个属性名在 antd 的 Modal.confirm / Popconfirm 上到处都有
 *   (确认框的说明文字),那些是"业务弹窗文案",不在本条范围内 —— 混进来全是
 *   误报。所以只取 PageHeader / ManagementTable 这两个真正渲染页头的元素。
 *
 * 为什么不能用"查某个中文串在不在"来写:
 *   那等于把当前这几句抄一遍,以后有人改文案就误报(而且新的写死文案查不出来)。
 *   按"属性值是不是字面量 + 含不含 CJK"来判,跟具体文案无关。
 *
 * 扫描器在 `src/test/sourceScan.ts`(与 placeholder-i18n.test.ts 共用)。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { CJK, findJsxTags, listTsx, literalValue, stripComments } from '@/test/sourceScan'

/** 参与"写死中文"检查的页面 */
const PAGE_FILES = [
  'pages/profile/index.tsx',
  'pages/system/config/index.tsx',
  'pages/system/config/audit.tsx',
  'pages/system/department/index.tsx',
  'pages/audit/operation.tsx',
  'pages/file/folder.tsx',
  'pages/file/list.tsx',
  'pages/flow/stage/[stageId].tsx',
]

/** 渲染页头的两个组件 */
const HEADER_TAGS = ['PageHeader', 'ManagementTable']

/** 页头属性里现在还存在的"文案"属性(值可能是写死的中文) */
const TEXT_PROPS = ['description', 'pageDescription', 'subTitle']

/** 一律不许出现的属性(标题归面包屑管) */
const FORBIDDEN_PROPS = ['title', 'pageTitle']

const SRC_ROOT = join(__dirname, '..', '..')

describe('页头文案 i18n', () => {
  it('PageHeader / ManagementTable 的 title/description 不得写死中文', () => {
    const offenders: string[] = []

    for (const rel of PAGE_FILES) {
      const src = stripComments(readFileSync(join(SRC_ROOT, rel), 'utf8'))
      for (const tag of findJsxTags(src)) {
        if (!HEADER_TAGS.includes(tag.name)) continue
        for (const prop of tag.props) {
          if (!TEXT_PROPS.includes(prop.name)) continue
          const val = literalValue(prop.value)
          if (val && CJK.test(val)) {
            offenders.push(`${rel}:${tag.line} · <${tag.name} ${prop.name}="${val}">`)
          }
        }
      }
    }

    expect(offenders).toEqual([])
  })

  it('页头元素一律不许传 title / pageTitle(标题归面包屑管)', () => {
    const offenders: string[] = []

    for (const file of listTsx(SRC_ROOT)) {
      const rel = file.slice(SRC_ROOT.length + 1).replace(/\\/g, '/')
      const src = stripComments(readFileSync(file, 'utf8'))
      for (const tag of findJsxTags(src)) {
        if (!HEADER_TAGS.includes(tag.name)) continue
        for (const prop of tag.props) {
          if (FORBIDDEN_PROPS.includes(prop.name)) {
            offenders.push(`${rel}:${tag.line} · <${tag.name} ${prop.name}={...}>`)
          }
        }
      }
    }

    expect(offenders).toEqual([])
  })
})
