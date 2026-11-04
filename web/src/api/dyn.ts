/**
 * 动态业务集合 API —— M151(后端) + M152(前端页面)
 *
 * 路径: 后端 /rapid-admin/v1/system/dyn/**(设计面) 与 /rapid-admin/v1/biz/dyn/:collection/**(数据面)
 *       —— 前端写的路径相对 baseURL(/rapid-admin/v1),所以这里不带 /rapid-admin/v1 前缀。
 *
 * ⚠️ **URL 一律内联在 http.<verb>() 的第一个实参里** ——
 *   `web/src/api/__tests__/api-route-parity.test.ts` 靠扫 `http.<verb>(...)`
 *   的**第一个实参是不是字面量/模板串**来核对路径打不打得通后端路由。
 *   写成 `http.post(buildUrl(code))` 的话那条网就扫不到这次调用 ⇒
 *   路径打错会变成"点一下 404,CI 全绿"。
 *
 * ⚠️ **两个面的资源码形状不同,别把它们混在一起写**:
 *   · 设计面  `dyn:design:collection:*` / `dyn:design:field:*` / `dyn:design:type:list`(四段)
 *   · 数据面  `dyn:<集合编码>:<动作>`(三段,集合编码由用户起、运行期才存在)
 *   集合编码里不可能出现 ":"(后端 dynx.ValidateCode 挡着)⇒ 两套码撞不了。
 */
import { http, instance } from './client'
import type {
  DynChoice,
  DynCollection,
  DynField,
  DynFieldType,
  DynIndexDef,
  DynOptionsDraft,
  DynRecord,
  DynRecordQuery,
  DynRelationSource,
  DynSchema,
  DynImportResult,
} from '@/types/dyn'
import type { PageResponse } from '@/types/api'

/**
 * 把后端存的选项集 JSON 文本转成数组
 *
 * ⚠️ **解析失败返回空数组,不抛错** ——
 *   选项集是设计面写的,写坏了属于"配置坏了",不该让整个列表页打不开。
 *   空数组的表现是"下拉框没有选项",用户自己能看出来。
 */
export function parseDynOptions(raw: string | undefined | null): DynChoice[] {
  return parseDynOptionsDraft(raw).choices
}

/**
 * 把后端存的选项集 JSON 文本转成「候选项 + 来源」
 *
 * ⚠️ **来源是第二份真相,不能丢** ——
 *   关联表来源的字段,候选项是空的(选项是读的时候现查的),
 *   唯一能认出"这个字段是从表里取选项的"就是 source。
 *   只解析 choices 的话,编辑这个字段时页面会显示成"手工添加且一个候选都没有",
 *   用户一保存就把来源配置抹掉了。
 */
export function parseDynOptionsDraft(raw: string | undefined | null): DynOptionsDraft {
  const trimmed = (raw ?? '').trim()
  if (!trimmed) return { choices: [], source: '' }
  try {
    const parsed = JSON.parse(trimmed)
    const choices = Array.isArray(parsed?.choices) ? parsed.choices : []
    const sourceKey =
      parsed?.source && typeof parsed.source.key === 'string' ? parsed.source.key.trim() : ''
    return {
      choices: choices
        .filter((c: unknown) => c && typeof (c as DynChoice).value === 'string')
        .map((c: DynChoice) => ({ value: c.value, label: String(c.label ?? c.value) })),
      source: sourceKey,
      // relation 配置(M5)—— 同 source 一样是"第二份真相",丢了
      // 编辑保存一次就把目标集合抹掉,字段退化成没有任何选项的空字段
      relation_target:
        typeof parsed?.relation?.target === 'string' ? parsed.relation.target.trim() : undefined,
      relation_display_field:
        typeof parsed?.relation?.display_field === 'string'
          ? parsed.relation.display_field.trim()
          : undefined,
    }
  } catch {
    return { choices: [], source: '' }
  }
}

/**
 * 把任意值规整成选项集草稿
 *
 * ⚠️ **给表单里的值用** —— EditModal 的 getValueProps 提供了就等于
 *   **替换**了 rc-field-form 的默认实现,不返回 value 的话控件会变成完全不受控。
 *   而表单里的值可能是 undefined(新建字段时)、旧数组(老代码),
 *   所以这里必须容错。
 */
export function toDynOptionsDraft(value: unknown): DynOptionsDraft {
  if (!value || typeof value !== 'object') return { choices: [], source: '' }
  const v = value as Partial<DynOptionsDraft>
  return {
    choices: Array.isArray(v.choices) ? v.choices : [],
    source: typeof v.source === 'string' ? v.source : '',
    // relation 配置(M5)—— 可选,空串归 undefined(不是 relation)
    relation_target: (typeof v.relation_target === 'string' && v.relation_target.trim()) || undefined,
    relation_display_field:
      (typeof v.relation_display_field === 'string' && v.relation_display_field.trim()) || undefined,
    // 值约束(M1)—— 可选,undefined = 不启用;数字字段防 "0" 字符串等脏形态
    min: toOptNumber(v.min),
    max: toOptNumber(v.max),
    min_length: toOptNumber(v.min_length),
    max_length: toOptNumber(v.max_length),
    pattern: typeof v.pattern === 'string' ? v.pattern : undefined,
  }
}

/** 约束数字的容错读取:undefined / null / NaN 一律归 undefined(不启用) */
function toOptNumber(v: unknown): number | undefined {
  if (typeof v !== 'number' || !Number.isFinite(v)) return undefined
  return v
}

/**
 * 把选项数组序列化成后端要的 JSON 文本
 *
 * ⚠️ **形状必须是 `{choices:[...]}`** —— 后端 `dynx.ParseOptions` 反序列化后
 *   直接读 `Options.Choices`(见 server/internal/pkg/dynx/options.go)。
 *   写成裸数组的话后端解析出来是空选项集 ⇒ 单选字段存不进值,
 *   而且**不报错**(选项集为空 ⇒ 校验被跳过)。
 */
export function serializeDynOptions(choices: DynChoice[]): string {
  return serializeDynOptionsDraft({ choices, source: '' })
}

/**
 * 把「候选项 + 来源」序列化成后端要的 JSON 文本
 *
 * ⚠️ **有来源时不能返回空串** ——
 *   后端 `MarshalOptions` 在 choices 为空时返回空串,
 *   那样"关联表来源"的配置会被**静默丢掉**:
 *   字段建好了,却变成一个没有任何选项的单选 —— 而前端不报错。
 *
 * ⚠️ **来源与候选项互斥** ——
 *   选了来源就不该再带手工候选项(后端 Validate 会认这两份,
 *   但界面上给用户看的是"二选一")。
 */
export function serializeDynOptionsDraft(draft: DynOptionsDraft | undefined | null): string {
  const choices = Array.isArray(draft?.choices) ? draft!.choices : []
  const source = (draft?.source ?? '').trim()
  // relation 配置(M5):有值才带 —— 后端 Options.Relation 为 nil = 不是 relation
  const relation_target = (draft?.relation_target ?? '').trim()
  const relation_display_field = (draft?.relation_display_field ?? '').trim()
  // 值约束(M1):有值才带 —— 后端 dynx.Options 读 *float64 / *int,undefined = 不启用
  const constraints: Record<string, unknown> = {}
  if (typeof draft?.min === 'number') constraints.min = draft.min
  if (typeof draft?.max === 'number') constraints.max = draft.max
  if (typeof draft?.min_length === 'number') constraints.min_length = draft.min_length
  if (typeof draft?.max_length === 'number') constraints.max_length = draft.max_length
  const pattern = (draft?.pattern ?? '').trim()
  if (pattern) constraints.pattern = pattern
  const hasConstraints = Object.keys(constraints).length > 0

  // ⚠️ **relation 的配置也不能丢** —— 与 source 同一个坑:
  // 序列化时漏掉 relation,保存一次字段的目标集合就被抹掉
  if (relation_target) {
    const relation: Record<string, string> = { target: relation_target }
    if (relation_display_field) relation.display_field = relation_display_field
    return JSON.stringify({ choices: [], relation, ...constraints })
  }

  if (!source) {
    if (choices.length === 0 && !hasConstraints) return ''
    return JSON.stringify(hasConstraints ? { choices, ...constraints } : { choices })
  }
  return JSON.stringify(hasConstraints ? { choices: [], source: { kind: 'table', key: source }, ...constraints } : { choices: [], source: { kind: 'table', key: source } })
}

/**
 * 把集合行里存的索引定义 JSON 文本转成数组(M2)
 *
 * ⚠️ **解析失败返回空数组,不抛错** ——
 *   同 parseDynOptions 的口径:配置坏了不该让整个设计页打不开。
 */
export function parseDynIndexes(raw: string | undefined | null): DynIndexDef[] {
  const trimmed = (raw ?? '').trim()
  if (!trimmed) return []
  try {
    const parsed = JSON.parse(trimmed)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((d: unknown) => d && typeof (d as DynIndexDef).name === 'string')
      .map((d: DynIndexDef) => ({
        name: d.name,
        columns: Array.isArray(d.columns) ? d.columns.filter((c) => typeof c === 'string') : [],
        unique: d.unique === true,
      }))
  } catch {
    return []
  }
}

/**
 * 把索引定义数组序列化成集合行要存的 JSON 文本
 *
 * ⚠️ **空数组返空串** —— 后端 ParseIndexes 对空串返 nil(没配索引),
 *   存 "[]" 的话每次保存都会带着一条空定义白跑一遍。
 * ⚠️ **name 统一小写、columns 去首尾空格** —— 后端 ValidateIndex 按字段标识规则
 *   校验索引名,前端先归一化,别让用户的大小写手误变成一次保存失败。
 */
export function serializeDynIndexes(defs: DynIndexDef[] | undefined | null): string {
  const cleaned = (defs ?? [])
    .map((d) => ({
      name: (d.name ?? '').trim().toLowerCase(),
      columns: (d.columns ?? []).map((c) => c.trim()).filter(Boolean),
      unique: d.unique === true,
    }))
    .filter((d) => d.name && d.columns.length > 0)
  if (cleaned.length === 0) return ''
  return JSON.stringify(cleaned)
}

/** 设计面 —— 集合与字段的增删改查 */export const dynApi = {
  /**
   * 集合列表
   * 后端: GET /system/dyn/collections?keyword=&page=&size=
   */
  listCollections: (params?: { keyword?: string; page?: number; size?: number }) =>
    http.get<PageResponse<DynCollection>>('/system/dyn/collections', params),

  /**
   * 集合详情
   * 后端: GET /system/dyn/collections/:id
   */
  getCollection: (id: string) => http.get<DynCollection>(`/system/dyn/collections/${id}`),

  /**
   * 新建集合(建表 + 建资源 + 授权给创建者当前角色)
   * 后端: POST /system/dyn/collections
   */
  createCollection: (body: Partial<DynCollection>) =>
    http.post<string>('/system/dyn/collections', body),

  /**
   * 改集合(**不能改 code** —— 后端 DTO 里根本没有这个字段)
   * 后端: PATCH /system/dyn/collections/:id
   */
  updateCollection: (id: string, body: Partial<DynCollection>) =>
    http.patch<void>(`/system/dyn/collections/${id}`, body),

  /**
   * 删集合(软删 + 停用资源,物理表保留)
   * 后端: DELETE /system/dyn/collections/:id
   */
  deleteCollection: (id: string) => http.delete<void>(`/system/dyn/collections/${id}`),

  /**
   * 字段列表(**含已停用的** —— 设计器要看得见"被停用了"才能恢复它)
   * 后端: GET /system/dyn/collections/:id/fields
   */
  listFields: (collectionId: string) =>
    http.get<DynField[]>(`/system/dyn/collections/${collectionId}/fields`),

  /**
   * 新建字段(加列)
   * 后端: POST /system/dyn/collections/:id/fields
   */
  createField: (collectionId: string, body: Partial<DynField>) =>
    http.post<string>(`/system/dyn/collections/${collectionId}/fields`, body),

  /**
   * 改字段(**不能改 field_key / type** —— 后端 DTO 里没有这两个字段)
   * 后端: PATCH /system/dyn/fields/:id
   */
  updateField: (id: string, body: Partial<DynField>) =>
    http.patch<void>(`/system/dyn/fields/${id}`, body),

  /**
   * 删字段(软删,物理列保留 —— 数据不丢)
   * 后端: DELETE /system/dyn/fields/:id
   */
  deleteField: (id: string) => http.delete<void>(`/system/dyn/fields/${id}`),

  /**
   * 字段类型词表(**后端下发,前端不许硬编码**)
   * 后端: GET /system/dyn/field-types
   */
  fieldTypes: () => http.get<DynFieldType[]>('/system/dyn/field-types'),

  /**
   * 可关联的系统表清单(**后端下发,前端不许硬编码表名**)
   *
   * ⚠️ **后端注册表是白名单** —— 让用户填表名等于把 SQL 的表名交给用户拼。
   *   每个来源还带 `readable`(当前账号能不能读那张表)与 `resource_code`
   *   (读不到时缺哪个权限码)。
   * 后端: GET /system/dyn/relation-sources
   */
  relationSources: () => http.get<DynRelationSource[]>('/system/dyn/relation-sources'),
}

/**
 * 数据面 —— 某个集合的记录 CRUD
 *
 * ⚠️ **这里的权限由后端按集合判定** ——
 *   资源码 `dyn:<集合编码>:<动作>` 的集合编码来自 URL 路径参数,
 *   注册期不存在 ⇒ 挂不上编译期常量式的 rbac()。
 *   ⇒ 前端要判权限就在运行期拼字符串 `dyn:${code}:create` 去问权限 store
 *   (前端 store 里装的就是后端下发的 api 资源码,含运行期生成的那几条)。
 */
export const dynRecordApi = {
  /**
   * 表单结构(渲染表格列 + 录入表单的依据)
   * 后端: GET /biz/dyn/:collection/schema
   */
  schema: (collection: string) => http.get<DynSchema>(`/biz/dyn/${collection}/schema`),

  /**
   * 记录列表
   * 后端: GET /biz/dyn/:collection/records?keyword=&owner=&trash=1&order_by=&order_dir=&page=&size=
   *       &skip_total=1&fields=a,b&f_<字段>=v1,v2
   *
   * ⚠️ **trash 必须翻成 "1"** —— 后端认 `c.Query("trash") == "1"`,
   *   布尔 true 会被 axios 序列化成 "true" ⇒ 后端当成 false,回收站永远是空的。
   *
   * ⚠️ **filters 翻成 `f_<字段>=v1,v2`** —— 后端按 `f_` 前缀认筛选参数,
   *   普通名字会跟 keyword / page 这些系统参数撞。
   */
  listRecords: (
    collection: string,
    params?: DynRecordQuery & { page?: number; size?: number },
  ) => {
    // ⚠️ filters / skip_total 不能进 spread —— 对象会被 axios 序列化成 [object Object]
    const { filters, skip_total, trash, ...rest } = params ?? {}
    const query: Record<string, unknown> = { ...rest, trash: trash ? '1' : undefined }
    if (skip_total) query.skip_total = '1'
    for (const [key, values] of Object.entries(filters ?? {})) {
      if (values.length > 0) query[`f_${key}`] = values.join(',')
    }
    return http.get<PageResponse<DynRecord>>(`/biz/dyn/${collection}/records`, query)
  },

  /**
   * 记录详情
   * 后端: GET /biz/dyn/:collection/records/:id
   */
  getRecord: (collection: string, id: string) =>
    http.get<DynRecord>(`/biz/dyn/${collection}/records/${id}`),

  /**
   * 新建记录(键是**字段标识**,不是物理列名)
   * 后端: POST /biz/dyn/:collection/records
   */
  createRecord: (collection: string, payload: Record<string, unknown>) =>
    http.post<string>(`/biz/dyn/${collection}/records`, payload),

  /**
   * 改记录(只改请求里出现的字段)
   * 后端: PATCH /biz/dyn/:collection/records/:id
   */
  updateRecord: (collection: string, id: string, payload: Record<string, unknown>) =>
    http.patch<void>(`/biz/dyn/${collection}/records/${id}`, payload),

  /**
   * 删记录(软删)
   * 后端: DELETE /biz/dyn/:collection/records/:id
   */
  deleteRecord: (collection: string, id: string) =>
    http.delete<void>(`/biz/dyn/${collection}/records/${id}`),

  /**
   * 恢复一条软删的记录(回收站)
   * 后端: PUT /biz/dyn/:collection/records/:id/restore
   */
  restoreRecord: (collection: string, id: string) =>
    http.put<void>(`/biz/dyn/${collection}/records/${id}/restore`),

  /**
   * 把记录送进集合绑定的审批流(2026-11-03)
   *
   * ⚠️ **集合没绑模板时后端会明确报错**(40002)——
   *   前端只在 schema.approval_template_id 非零时渲染这个动作,
   *   两边口径一致;直连接口的人拿到的也是能读懂的错。
   * 返回审批单 id(详情页跳转用)。
   * 后端: POST /biz/dyn/:collection/records/:id/approval
   */
  submitRecordApproval: (collection: string, id: string) =>
    http.post<string>(`/biz/dyn/${collection}/records/${id}/approval`),

  /**
   * 批量软删,返回实际删掉的行数
   *
   * ⚠️ **ids 是字符串数组**(19 位 uint64 在 wire 上是字符串);
   *   返回数 < ids.length 是正常行为 —— 看不见的行被数据范围滤掉了。
   * 后端: POST /biz/dyn/:collection/records/batch-delete
   */
  batchDeleteRecords: (collection: string, ids: string[]) =>
    http.post<{ count: number }>(`/biz/dyn/${collection}/records/batch-delete`, { ids }),

  /**
   * 批量修改单字段(走后端元数据校验 + 归一化),返回实际改掉的行数
   * 后端: POST /biz/dyn/:collection/records/batch-update
   */
  batchUpdateRecords: (collection: string, ids: string[], field: string, value: unknown) =>
    http.post<{ count: number }>(`/biz/dyn/${collection}/records/batch-update`, {
      ids,
      field,
      value,
    }),

  /**
   * 按当前筛选条件导出全量 CSV(带 BOM + 公式注入防护)
   *
   * ⚠️ **用 instance 拿原始响应** —— http 包装层解的是 JSON 信封,
   *   CSV 字节流走它会被当"没有 code 字段"处理;blob 下载必须直连 axios 实例
   *   (口径同 upload.ts 的 fetchFileContentUrl)。
   * ⚠️ **筛选口径与列表一致(M3)** —— 后端 list / export 共用同一份解析,
   *   前端也要把同样的 filters 翻成 f_<字段> 带上,否则"导出的跟看到的不一样"。
   * 后端: GET /biz/dyn/:collection/records/export?keyword=&owner=&trash=1&order_by=&order_dir=&f_<字段>=v1,v2
   */
  exportRecords: async (
    collection: string,
    params?: Omit<DynRecordQuery, 'skip_total' | 'fields'>,
  ): Promise<Blob> => {
    const query = new URLSearchParams()
    if (params?.keyword) query.set('keyword', params.keyword)
    if (params?.owner) query.set('owner', params.owner)
    if (params?.trash) query.set('trash', '1')
    if (params?.order_by) query.set('order_by', params.order_by)
    if (params?.order_dir) query.set('order_dir', params.order_dir)
    if (params?.filter) query.set('filter', params.filter)
    for (const [key, values] of Object.entries(params?.filters ?? {})) {
      if (values.length > 0) query.set(`f_${key}`, values.join(','))
    }
    const qs = query.toString()
    const resp = await instance.get<Blob>(`/biz/dyn/${collection}/records/export${qs ? `?${qs}` : ''}`, {
      responseType: 'blob',
    })
    return resp.data
  },

  /**
   * 导入 CSV。mode 缺省 = all(整批全有才进),'skip' = 跳过错误行只进合法行。
   * 返回错误行报告(row/field/message),前端弹 Modal 展示。
   *
   * ⚠️ **URL 内联**(parity 网只扫第一个实参是字面量/模板串的调用),
   *   FormData 不能进 URL,mode 拼在查询串里。
   * 后端: POST /biz/dyn/:collection/records/import?mode=all|skip
   */
  importRecords: async (collection: string, file: File, mode: 'all' | 'skip') => {
    const form = new FormData()
    form.append('file', file)
    const resp = await instance.post<DynImportResult>(
      `/biz/dyn/${collection}/records/import?mode=${mode}`,
      form,
    )
    return resp.data
  },
}

/**
 * 数据面的资源码 —— 运行期拼出来的
 *
 * ⚠️ **这里只能是函数,不能是常量表** ——
 *   集合编码是用户起名、运行期才有的,编译期枚举不了。
 *   (常量表里能列的只有设计面那几条,见 @/constants/permissions 的 PermDyn。)
 *
 * ⚠️ **拼出来的字符串必须与后端完全一致** ——
 *   后端 internal/pkg/dynx/actions.go:
 *     RecordModule(collection) = "dyn:" + collection
 *     RecordCode(collection, action) = RecordModule + ":" + action
 *   少一个冒号或多一个前缀 ⇒ 前端显示"能新增"而后端 403。
 */
export function dynRecordPerm(collection: string, action: string): string {
  return `dyn:${collection}:${action}`
}
