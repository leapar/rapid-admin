/**
 * 通用记录页的纯规则 —— 抽出来是为了能单测
 *
 * # 这一层为什么重要
 *
 * 动态业务表的**列**与**表单**都是运行期按 schema 生成的:
 * 用户在设计器里加一个字段,这一页就得自动多一列、多一个输入框。
 * ⇒ 这层逻辑是"用户输入 → 后端要的形状"的**唯一转换点**,
 *   而它一旦错了,症状是"存进去了但显示不对"或者"报一个看不懂的错"。
 *
 * ⚠️ **值的进出必须与后端 dynx 一一对应** ——
 *   写:  dynx.Normalize(类型, raw, 选项集) —— 见 server/internal/pkg/dynx/normalize.go
 *   读:  dynx.Denormalize(类型, 库里的值)   —— 见 server/internal/pkg/dynx/denormalize.go
 *   前端这两个方向各做一次转换,少一次就错位。
 *
 * # ⚠️ 日期时间的口径(最容易错的一处)
 *
 * 后端把 datetime 存成 **UTC 的 ISO-8601**("2026-11-01T00:30:00Z"),
 * 而**不带时区的输入按东八区解释**(见 dynx/normalize.go 的 parseTime)。
 * 所以:
 *   · 前端提交"不带时区的字符串" → 后端按东八区解释 → 存成 UTC
 *   · 前端提交带偏移的字符串      → 后端按字面解析 → 存成 UTC
 * 两种都对,但**前端不能自己换算** ——
 * 一旦前端把本地时间转成 UTC 再发,遇上"浏览器时区 ≠ 东八区"就整整差 8 小时,
 * 而且**当场不报错**(存进去了,只是时间不对)。
 *
 * ⇒ 所以 datetime 字段**用文本框**,让用户按后端认得的格式直接填。
 *   date 字段没有时区问题(只有年月日),所以可以用日期选择器。
 */
import dayjs from 'dayjs'
import type { EditField, EditFieldType } from '@/components/common/EditModal'
import type { DynFileRef, DynSchemaField } from '@/types/dyn'

/** t() 的最小签名(与 locales/useI18n 的 t 兼容) */
export type Translate = (
  key: string,
  fallback?: string,
  vars?: Record<string, string | number>
) => string

/**
 * dyn 字段类型 → EditModal 控件类型
 *
 * ⚠️ **未知类型一律当单行文本** —— 不是当"不支持"而是当文本:
 *   后端加了新类型而前端还不知道时,这一列至少还能显示、还能编辑字符串,
 *   而不是整个页面炸掉(TypeError: unknown type)。
 *   真要严格,应该由后端在 schema 里带上"前端能不能编辑"的标志。
 */
export function editTypeOf(type: string): EditFieldType {
  switch (type) {
    case 'longtext':
      return 'textarea'
    case 'int':
    case 'number':
      return 'number'
    case 'bool':
      return 'switch'
    case 'date':
      return 'date'
    case 'select':
      return 'select'
    case 'relation':
      // ⚠️ **关联字段就是一个下拉框,不新做控件** ——
      //   候选项(目标集合的记录)由后端在 schema 里现查好塞进 field.options
      //   (value = 目标记录 id 文本,label = 显示字段值),
      //   与单选的候选形状完全一样 ⇒ 复用 select 控件零成本。
      return 'select'
    case 'multiselect':
      return 'multiSelect'
    case 'json':
      return 'textarea'
    case 'file':
      // ⚠️ **附件不是文本框** —— 它要能上传、能显示原文件名、能点开。
      //   具体控件由页面注入(见 DynFieldEditOptions.renderFile)。
      return 'custom'
    default:
      // text / datetime / 未知类型
      return 'text'
  }
}

/**
 * 记录里的值 → 表单控件的初始值
 *
 * ⚠️ **JSON 字段必须在这里变回字符串** ——
 *   后端读出来的是**已解析的 JSON 值**(对象 / 数组 / 数字,见 dynx.Denormalize
 *   的 denormJSON),而表单控件是文本框 ⇒ 不转回去的话,
 *   文本框里会显示 "[object Object]"。
 */
export function toFormValue(field: DynSchemaField, raw: unknown): unknown {
  if (field.type === 'date') {
    const s = typeof raw === 'string' ? raw.trim() : ''
    // 后端存的是 "yyyy-mm-dd" —— dayjs 认这个格式
    return s ? dayjs(s) : undefined
  }
  if (field.type === 'multiselect') {
    if (Array.isArray(raw)) return raw.map((v) => String(v))
    // ⚠️ **默认值的形态是文本** —— 设计器里"默认值"是一个字符串输入框,
    //   多选的默认值只能写成 '["a","b"]' 这种 JSON 数组文本。
    //   不解析的话,新建记录时默认值会被当成一个普通字符串塞进单选框。
    if (typeof raw === 'string') {
      const s = raw.trim()
      if (!s) return []
      if (s.startsWith('[')) {
        try {
          const parsed = JSON.parse(s)
          if (Array.isArray(parsed)) return parsed.map((v) => String(v))
        } catch {
          // 解析不了就按"单个值"处理(后端也认这个形态)
        }
      }
      return [s]
    }
    return []
  }
  if (field.type === 'bool') {
    if (typeof raw === 'boolean') return raw
    // ⚠️ **默认值的形态是文本** —— "true" / "1" / "yes" 都要认,
    //   否则设计器里把默认值填成 "true",新建出来却是关的。
    if (typeof raw === 'string') {
      const s = raw.trim().toLowerCase()
      return s === 'true' || s === '1' || s === 't' || s === 'yes' || s === 'y' || s === 'on'
    }
    if (typeof raw === 'number') return raw !== 0
    return false
  }
  if (field.type === 'json') {
    if (typeof raw === 'string') return raw
    if (raw === null || raw === undefined) return ''
    return JSON.stringify(raw, null, 2)
  }
  if (field.type === 'file') {
    // ⚠️ **值就是文件 key(一个字符串)** —— 原样带过去,
    //   展示信息(原文件名 / 直链)在 `_files` 里,不在这个值里。
    if (raw === null || raw === undefined) return ''
    return typeof raw === 'string' ? raw.trim() : String(raw)
  }
  if (field.type === 'int' || field.type === 'number') {
    if (raw === null || raw === undefined || raw === '') return undefined
    const n = Number(raw)
    return Number.isNaN(n) ? undefined : n
  }
  if (raw === null || raw === undefined) return ''
  return raw
}

/**
 * 表单控件的值 → 提交给后端的值
 *
 * ⚠️ **空值统一成"没有值"而不是空串** ——
 *   数字字段清空时 InputNumber 给的是 null;
 *   发 '' 的话后端 toInt64 会报"需要整数,收到 ''"。
 *   ⇒ 数字空 → null;文本空 → ''(后端存空串,这是"清空"而不是"不填")。
 *
 * ⚠️ **发 null 是有落点的** —— 后端把它落成 **NULL**( = 没填),
 *   不是"跳过这一列"(跳过的话旧值会留着,用户清空不掉)。
 *   权威:server/internal/pkg/dynx/normalize.go 的 Normalize(nil) → (nil, nil)
 *   + server/internal/service/dyn_impl.go 的建表口径(字段列可空)。
 */
export function toWireValue(field: DynSchemaField, raw: unknown): unknown {
  switch (field.type) {
    case 'date': {
      // 日期选择器给的是 dayjs —— 后端要 "yyyy-mm-dd"
      if (dayjs.isDayjs(raw)) return raw.format('YYYY-MM-DD')
      if (typeof raw === 'string') return raw.trim()
      return ''
    }
    case 'datetime':
      // ⚠️ **原样发字符串** —— 换算留给后端(见文件头的口径说明)
      return typeof raw === 'string' ? raw.trim() : ''
    case 'multiselect':
      return Array.isArray(raw) ? raw.map((v) => String(v)) : []
    case 'int':
    case 'number': {
      if (raw === null || raw === undefined || raw === '') return null
      const n = Number(raw)
      return Number.isNaN(n) ? null : n
    }
    case 'bool':
      return toWireBool(raw)
    case 'json':
      return typeof raw === 'string' ? raw.trim() : ''
    case 'file':
      // ⚠️ **发的是 key,不是 URL** ——
      //   后端 dynx.normalizeFile 拒带域名的值(400 "文件路径不能带域名"),
      //   也拒 `..` / 反斜杠 / 前导斜杠。
      return typeof raw === 'string' ? raw.trim() : ''
    default:
      if (raw === null || raw === undefined) return ''
      return String(raw)
  }
}

/**
 * 开关值 → 布尔
 *
 * ⚠️ **认不出来的字符串原样发出去,不要静默当 false** ——
 *   后端 dynx.toBool 对认不出来的值是**报错**的,它必须报错:
 *   把 "开" 静默当成 false 的话,用户存进去的是"关",而且当场不报错。
 *   前端要是先一步把它变成 false,后端那个报错就没了 ——
 *   数据就变成"用户填的是开、存下来是关",而且**全程没有任何一条错误信息**。
 *
 *   认得的那些(与后端 toBool 完全一致):1/true/t/yes/y/on 与 0/false/f/no/n/off。
 */
function toWireBool(raw: unknown): boolean | string {
  if (typeof raw === 'boolean') return raw
  if (typeof raw === 'number') return raw !== 0
  if (typeof raw === 'string') {
    const s = raw.trim().toLowerCase()
    if (s === '1' || s === 'true' || s === 't' || s === 'yes' || s === 'y' || s === 'on') {
      return true
    }
    if (s === '0' || s === 'false' || s === 'f' || s === 'no' || s === 'n' || s === 'off') {
      return false
    }
    // 认不出来 → 原样发出去,让后端报错(见上面的注释)
    return raw
  }
  return false
}

/**
 * 记录里的值 → 表格单元格里显示的文本
 *
 * ⚠️ **单选/多选显示的是"显示名"不是"值"** ——
 *   存的是 "pending",但人看到的应该是"待处理"。
 *   选项集是随 schema 一起下发的,所以这里查得到。
 *
 * ⚠️ **附件显示的是"原文件名"不是 key** ——
 *   key 是一长串 `rapid-admin/1844/dyn/0/xxx-户型图.pdf`,没人认得。
 *   原文件名由后端在读记录时解析好(记录视图的 `_files`),取不到就退回显示 key。
 *
 * ⚠️ **关联字段显示的是"目标记录的显示名"不是 id** ——
 *   值是目标记录的 id(19 位数字),人认不得;
 *   显示名由后端在读记录时解析好(记录视图的 `_rels`),取不到就退回显示 id
 *   (id 至少还能让人拿着它去问,空白格子只会让人以为"这条记录没有关联")。
 */
export function formatCell(
  field: DynSchemaField,
  raw: unknown,
  t: Translate,
  file?: DynFileRef,
  relLabel?: string,
): string {
  if (raw === null || raw === undefined || raw === '') return t('common.emptyValue')
  switch (field.type) {
    case 'relation':
      return relLabel || String(raw)
    case 'bool':
      return toWireBool(raw) === true ? t('common.yes') : t('common.no')
    case 'select': {
      const v = String(raw)
      return field.options.find((o) => o.value === v)?.label ?? v
    }
    case 'multiselect': {
      if (!Array.isArray(raw) || raw.length === 0) return t('common.emptyValue')
      return raw
        .map((v) => {
          const s = String(v)
          return field.options.find((o) => o.value === s)?.label ?? s
        })
        .join(', ')
    }
    case 'file':
      return file?.name || String(raw)
    case 'json':
      return typeof raw === 'string' ? raw : JSON.stringify(raw)
    case 'datetime':
      // 后端给的是 UTC ISO —— 显示成本地时间(与全仓其它时间列同一口径)
      return dayjs(String(raw)).format('YYYY-MM-DD HH:mm:ss')
    case 'int':
    case 'number':
      return String(raw)
    default:
      return String(raw)
  }
}

/**
 * 表单项的额外装配(附件字段要)
 *
 * ⚠️ **附件控件由页面注入** ——
 *   recordForm.ts 是**纯 .ts**(不能放 JSX,也不该为了放 JSX 就变成组件文件),
 *   而附件控件是个组件(见 pages/dyn/FileFieldControl)。
 *   ⇒ 这里只留一个"渲染器"的口子,真正的 JSX 在页面里。
 */
export interface DynFieldEditOptions {
  /** 记录里的附件展示信息(键是**字段标识**) */
  files?: Record<string, DynFileRef>
  /** 附件字段的控件渲染器 */
  renderFile?: (field: DynSchemaField, file: DynFileRef | undefined) => EditField['render']
}

/**
 * schema 字段 → EditModal 的表单项
 *
 * ⚠️ **name 用字段标识(field_key),不用显示名** ——
 *   提交时按 name 取值,字段标识才是后端 JSON 的键。
 *   显示名可能重复、可能被改,字段标识不会。
 */
export function buildFieldEditFields(
  fields: DynSchemaField[],
  t: Translate,
  opts?: DynFieldEditOptions,
): EditField[] {
  return fields.map((f) => {
    const item: EditField = {
      name: f.field_key,
      label: f.label,
      type: editTypeOf(f.type),
      required: f.required,
    }
    if (f.type === 'select' || f.type === 'multiselect' || f.type === 'relation') {
      item.options = f.options.map((o) => ({ value: o.value, label: o.label }))
    }
    if (f.required) {
      item.rules = [{ required: true }]
    }
    if (f.type === 'file') {
      // ⚠️ **值是文件 key(一个字符串)** ——
      //   getValueProps 给了就等于替换了 rc-field-form 的默认实现,
      //   不返回 value 的话控件会变成完全不受控(改一次就再也不跟表单同步)。
      item.getValueProps = (v) => ({ value: typeof v === 'string' ? v : '' })
      const render = opts?.renderFile?.(f, opts?.files?.[f.field_key])
      if (render) {
        item.render = render
      } else {
        // ⚠️ **没有渲染器就退回文本框** ——
        //   type='custom' 而没有 render 时 EditModal 会**什么都不渲染**
        //   (renderField 里 `case 'custom': return null`),
        //   那个字段会**整条消失** —— 用户看不出"这个字段被吞了"。
        //   退回文本框至少还能看见 key(后端会校验这个文件存不存在)。
        item.type = 'text'
      }
    }
    if (f.type === 'json') {
      // ⚠️ **JSON 的合法性在提交前就查** ——
      //   后端也会查(报"不是合法 JSON"),但那时用户已经填完一整张表了。
      //   这里当场提示,改一个字段不用重填其它字段。
      item.rules = [
        ...(item.rules ?? []),
        {
          validator: async (_rule, value: unknown) => {
            const s = typeof value === 'string' ? value.trim() : ''
            if (s === '') return
            try {
              JSON.parse(s)
            } catch {
              throw new Error(t('page.dyn.records.jsonInvalid'))
            }
          },
        },
      ]
    }
    return item
  })
}

/**
 * 表单值 → 提交给后端的记录载荷
 *
 * ⚠️ **键是字段标识** —— 后端 dynx 侧按字段标识找列
 *   (认错键会得到"字段 xxx 不存在",而且是 400,不会静默丢数据)。
 */
export function buildRecordPayload(
  fields: DynSchemaField[],
  values: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) {
    out[f.field_key] = toWireValue(f, values[f.field_key])
  }
  return out
}
