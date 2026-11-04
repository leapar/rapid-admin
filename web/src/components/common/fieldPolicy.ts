/**
 * 字段级可见性(列级权限)· 界面层的适配 —— M168(2026-11-02)
 *
 * # 这一层是"纯函数",不碰 React
 *
 * 它回答的是同一个问题的三个界面版本:
 *
 *   · **表格**:哪些列不该存在?(服务端把那个键删了 ⇒ 列还在、格是空的)
 *   · **表单**:哪些字段不该存在?哪些字段只能看不能改?
 *   · **详情**:哪些字段不该存在?
 *
 * ⚠️ **为什么单独一个文件**
 *
 *   纯函数不能跟组件同文件(`react-refresh/only-export-components`,
 *   而本仓 lint 是 `--max-warnings 0`)。同 `rowClick.ts` 一个道理。
 *
 * # ⚠️ **判据只有一条:服务端说这个字段被收窄了没有**
 *
 *   报告来自服务端响应头(见 `utils/fieldPolicy.ts`),
 *   **前端不自己算规则**(与 M149 同一条纪律:
 *   「显示层不许自己算,必须读执行层那个权威字段」)。
 *
 * # ⚠️ **"隐藏"与"打码"在界面上的处理不一样**
 *
 *   · **隐藏** ⇒ 服务端**删掉了那个键** ⇒ 界面也**不渲染这一列 / 这一项**
 *     (留着就是"列还在、格是空的")。
 *   · **打码** ⇒ 值还在(但是打码后的)⇒ **界面照常显示**
 *     —— 用户要能看到"这里有个手机号,只是看不全";
 *     但**表单里不能让它被改**(改了就是拿 `138****1234` 覆盖真实值)。
 */
import type { ColumnsType, ColumnType, ColumnGroupType } from 'antd/es/table'
import type { FieldPolicyReport } from '@/utils/fieldPolicy'

/**
 * 过滤表格列 —— 藏掉"服务端已经删掉键"的那些列
 *
 * ⚠️ **列的字段名从 `dataIndex` 取,不是从 `key` 取** ——
 *   `key` 是 React 的 key(可以随便起),`dataIndex` 才是"这一列取哪个字段"。
 *
 * ⚠️ **`dataIndex` 是数组路径时,每一段都算** ——
 *   服务端的规则是**按字段名匹配,不按路径**
 *   (见 `server/internal/pkg/fieldpolicy` 的 `ApplyTree` 注释:
 *   一个字段名在树里出现几次就作用几次)。
 *   ⇒ `dataIndex: ['a', 'phone']` 的列,在 `phone` 被隐藏时也要一起藏。
 */
export function filterColumnsByReport<T extends object = Record<string, unknown>>(
  columns: ColumnsType<T>,
  report: FieldPolicyReport | null,
): ColumnsType<T> {
  if (!report || report.hidden.length === 0) return columns
  const hidden = new Set(report.hidden)
  const out: ColumnsType<T> = []
  for (const col of columns) {
    if (isColumnGroup(col)) {
      const children = filterColumnsByReport(col.children ?? [], report)
      // ⚠️ **整组都被藏了 ⇒ 整组不渲染** ——
      //   留一个空标题行比不留更怪(表头上一个空格子)。
      if (children.length === 0) continue
      out.push({ ...col, children })
      continue
    }
    if (columnFieldNames(col).some((name) => hidden.has(name))) continue
    out.push(col)
  }
  return out
}

/**
 * 过滤表单字段 —— 藏掉被隐藏的字段,把被打码的字段变成只读
 *
 * ⚠️ **打码 ⇒ `disabled: true`,而不是删掉** ——
 *   用户要能看到"这里有个手机号,只是看不全";
 *   删掉的话用户会以为这个字段不存在,反而会去问客服。
 *
 * ⚠️ **只读字段的值还要从提交载荷里剔掉** ——
 *   见 `omitRestrictedFields`:不然 `138****1234` 会被原样写回数据库
 *   (真实的手机号被自己的打码值覆盖)。
 */
export function filterEditFieldsByReport<T extends { name: string; disabled?: boolean }>(
  fields: T[],
  report: FieldPolicyReport | null,
): T[] {
  if (!report) return fields
  const hidden = new Set(report.hidden)
  const masked = new Set(report.masked)
  const out: T[] = []
  for (const f of fields) {
    if (hidden.has(f.name)) continue
    if (masked.has(f.name)) {
      out.push({ ...f, disabled: true })
      continue
    }
    out.push(f)
  }
  return out
}

/**
 * 过滤详情字段 —— 藏掉被隐藏的字段
 *
 * ⚠️ **打码的字段照常显示**(值已经是打码后的,服务端改过了)。
 */
export function filterViewFieldsByReport<T extends { name: string }>(
  fields: T[],
  report: FieldPolicyReport | null,
): T[] {
  if (!report || report.hidden.length === 0) return fields
  const hidden = new Set(report.hidden)
  return fields.filter((f) => !hidden.has(f.name))
}

/**
 * 把"被收窄的字段"从提交载荷里剔掉
 *
 * ⚠️ **打码的字段必须剔掉** ——
 *   它在表单里是只读的,值是服务端打码后的(`138****1234`),
 *   原样提交回去 = **用打码值覆盖真实值**(数据损坏)。
 *
 * ⚠️ **隐藏的字段也剔掉** ——
 *   正常情况下它压根没渲染、也不会进 `validateFields()` 的结果;
 *   这里再挡一道,是为了"表单值是别处塞进来的"这种情形。
 *
 * ⚠️ **不改动传入的对象** ——
 *   返回的是一份拷贝,调用方的 `values` 保持原样(便于比对 / 调试)。
 */
export function omitRestrictedFields<T>(values: T, report: FieldPolicyReport | null): T {
  if (!report) return values
  if (report.hidden.length === 0 && report.masked.length === 0) return values
  if (!values || typeof values !== 'object') return values
  const out: Record<string, unknown> = { ...(values as Record<string, unknown>) }
  for (const name of report.hidden) delete out[name]
  for (const name of report.masked) delete out[name]
  return out as unknown as T
}

/** 列是不是一个"列组"(有 children) */
function isColumnGroup<T extends object>(
  col: ColumnGroupType<T> | ColumnType<T>,
): col is ColumnGroupType<T> {
  return Array.isArray((col as ColumnGroupType<T>).children)
}

/** 这一列取的是哪些字段名 */
function columnFieldNames<T extends object>(col: ColumnType<T>): string[] {
  const di = col.dataIndex as string | number | readonly (string | number)[] | undefined
  if (typeof di === 'string') return [di]
  if (Array.isArray(di)) {
    return (di as readonly (string | number)[]).filter((x): x is string => typeof x === 'string')
  }
  // ⚠️ **没有 dataIndex 的列(纯操作列)永远不藏** ——
  //   它不取任何字段,藏了只会把"新建 / 删除"这些按钮一起藏掉。
  return []
}
