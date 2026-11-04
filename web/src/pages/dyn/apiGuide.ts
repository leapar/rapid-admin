/**
 * 「API 接入」弹窗的内容生成 —— 抽出来是为了能单测
 *
 * # 这一层在回答什么
 *
 * 用户在设计器里建好一个业务表之后,**接口是自动生成的**
 * (后端建集合那一刻就登记了 5 条权限码 + 6 条路由)。
 * 但"自动生成了"这件事用户看不见 —— 他要的是**「我怎么调它」**。
 *
 * ⇒ 这一层把「集合编码 + 字段定义」变成一份能直接抄的接入说明:
 *   路径、权限码、请求体的键、调用示例。
 *
 * # ⚠️ 路径必须与后端 router.go 一一对应
 *
 * 后端(见 server/internal/api/router/router.go):
 *
 *	GET    /rapid-admin/v1/biz/dyn/:collection/schema
 *	GET    /rapid-admin/v1/biz/dyn/:collection/records
 *	GET    /rapid-admin/v1/biz/dyn/:collection/records/:id
 *	POST   /rapid-admin/v1/biz/dyn/:collection/records
 *	PATCH  /rapid-admin/v1/biz/dyn/:collection/records/:id
 *	DELETE /rapid-admin/v1/biz/dyn/:collection/records/:id
 *
 * 这里给的是**相对 baseURL** 的路径(baseURL 默认 `/rapid-admin/v1`)——
 * 与 @/api/dyn 里 dynRecordApi 写的路径是同一套。
 *
 * # ⚠️ 权限码也只有这五个
 *
 * 后端 dynx/actions.go 是唯一真相:list / read / create / update / delete。
 * **表单结构接口归 `list` 管**(不另开一个码)——
 * 能看列表却看不到字段,页面就是一张空表格 + 一个 403 弹窗。
 */
import type { DynSchemaField } from '@/types/dyn'

/** 一条接口 */
export interface DynEndpoint {
  method: 'GET' | 'POST' | 'PATCH' | 'DELETE'
  /** 路径(相对 baseURL) */
  path: string
  /** 需要的权限码 */
  perm: string
  /** 说明的 i18n 键(组件里用 t() 翻) */
  descKey: string
}

/**
 * 集合的记录接口清单
 *
 * ⚠️ **顺序即使用顺序** —— 先取结构、再取列表、然后才是增删改查。
 *   接入方照着从上往下读就能把流程走通。
 */
export function buildDynEndpoints(code: string): DynEndpoint[] {
  const base = `/biz/dyn/${code}`
  return [
    {
      method: 'GET',
      path: `${base}/schema`,
      perm: `dyn:${code}:list`,
      descKey: 'page.dyn.records.apiGuide.epSchema',
    },
    {
      method: 'GET',
      path: `${base}/records`,
      perm: `dyn:${code}:list`,
      descKey: 'page.dyn.records.apiGuide.epList',
    },
    {
      method: 'GET',
      path: `${base}/records/{id}`,
      perm: `dyn:${code}:read`,
      descKey: 'page.dyn.records.apiGuide.epRead',
    },
    {
      method: 'POST',
      path: `${base}/records`,
      perm: `dyn:${code}:create`,
      descKey: 'page.dyn.records.apiGuide.epCreate',
    },
    {
      method: 'PATCH',
      path: `${base}/records/{id}`,
      perm: `dyn:${code}:update`,
      descKey: 'page.dyn.records.apiGuide.epUpdate',
    },
    {
      method: 'DELETE',
      path: `${base}/records/{id}`,
      perm: `dyn:${code}:delete`,
      descKey: 'page.dyn.records.apiGuide.epDelete',
    },
  ]
}

/**
 * 一个字段的示例值
 *
 * ⚠️ **示例值必须符合该类型的归一化口径** —— 后端 dynx.Normalize 会对
 *   不认的形态报错(比如 int 收到 "12" 之外的、bool 收到 "开")。
 *   抄走示例却调不通,比不给示例更糟。
 *
 * ⚠️ **文本用尖括号包字段标识** —— 一眼能看出"这里要换成你的值",
 *   而不是被误当成真数据存进去。
 */
export function sampleValueFor(field: DynSchemaField): unknown {
  switch (field.type) {
    case 'int':
    case 'number':
      return 0
    case 'bool':
      return false
    case 'date':
      return '2026-01-01'
    case 'datetime':
      return '2026-01-01T00:00:00Z'
    case 'select':
      return field.options[0]?.value ?? ''
    case 'multiselect':
      return field.options[0] ? [field.options[0].value] : []
    case 'json':
      return {}
    case 'file':
      // ⚠️ **附件的值是文件的 key(不带域名的路径)** —— 不是 URL。
      //   后端 dynx.normalizeFile 拒带域名的值(400「文件路径不能带域名」),
      //   也拒 `..` / 反斜杠 / 前导斜杠 ⇒ 抄走示例必须能用。
      //   key 由文件服务给(POST /biz/files 的返回里 file_key 字段)。
      return '<file_key>'
    default:
      return `<${field.field_key}>`
  }
}

/**
 * 字段定义 → 示例请求体
 *
 * ⚠️ **键是字段标识,不是物理列名** —— 后端按字段标识找列,
 *   发 `f_row1` 会得到"字段 f_row1 不存在"(400)。
 */
export function buildSamplePayload(fields: DynSchemaField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) {
    out[f.field_key] = sampleValueFor(f)
  }
  return out
}

/** 示例请求体 → 格式化 JSON(给人看的) */
export function buildSamplePayloadText(fields: DynSchemaField[]): string {
  return JSON.stringify(buildSamplePayload(fields), null, 2)
}

/**
 * curl 调用示例
 *
 * ⚠️ **请求体压成一行** —— 缩进过的 JSON 里有换行,塞进 `-d '...'` 里
 *   在多数 shell 下会被当成多条命令(或者干脆语法错),抄走就调不通。
 */
export function buildCurlExample(baseURL: string, code: string, payloadText: string): string {
  const url = `${baseURL}/biz/dyn/${code}/records`
  const oneLine = payloadText.replace(/\s*\n\s*/g, '')
  return [
    `curl -X POST '${url}' \\`,
    `  -H 'Authorization: Bearer <token>' \\`,
    `  -H 'Content-Type: application/json' \\`,
    `  -d '${oneLine}'`,
  ].join('\n')
}

/**
 * 响应形状示例
 *
 * ⚠️ **只画骨架,不画真数据** —— 响应体里全是被脱敏后的业务数据,
 *   示例里给假数据反而会让人以为"字段就是这些"。
 */
export function buildResponseExample(): string {
  return JSON.stringify(
    {
      code: 0,
      message: 'ok',
      data: {
        id: '1730000000000000000',
        owner_id: '1001',
        owner_name: '<creator>',
        created_at: '2026-11-01T00:30:00Z',
        updated_at: '2026-11-01T00:30:00Z',
        '<field_key>': '<value>',
      },
      timestamp: 1730000000000,
      trace_id: '<trace_id>',
    },
    null,
    2,
  )
}

/**
 * 集合的记录接口基路径(相对 baseURL)
 *
 * ⚠️ **不带结尾斜杠** —— 拼出来的完整路径是 `{baseURL}/biz/dyn/<编码>/records`,
 *   中间多一个斜杠在多数网关下会被判成 404,而错误信息指向"路由不存在",
 *   跟真正的原因(斜杠)对不上。
 */
export function dynCollectionBasePath(code: string): string {
  return `/biz/dyn/${code}`
}

/**
 * JS SDK 用法片段(仓库 js-sdk/ 的 @rapid-admin/dyn-sdk,零依赖)
 *
 * ⚠️ **片段是给人抄的,所以 baseURL / 集合码必须是真值** ——
 *   与 curl 示例同一条纪律;方法名与 js-sdk/src/RecordService.ts 对齐,
 *   那边的公开方法被它的单测守着,这里只负责把真值拼进去。
 */
export function buildSdkSnippet(baseURL: string, code: string): string {
  return [
    `import { DynClient } from '@rapid-admin/dyn-sdk'`,
    ``,
    `// baseURL 是 API 根路径(含 /rapid-admin/v1),不是站点根`,
    `const client = new DynClient('${baseURL}')`,
    `client.authStore.save('<token>')`,
    ``,
    `const records = await client.collection('${code}').getList({ page: 1, size: 20 })`,
    ``,
    `// 新建记录:键是字段标识(见上面的字段一览),不是数据库列名`,
    `const { id } = await client.collection('${code}').create({ /* <field_key>: <value> */ })`,
  ].join('\n')
}
