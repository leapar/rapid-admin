/**
 * 统一类型定义 —— 全部与后端 wire 格式(snake_case)对齐
 *
 * # ⚠️ **这里的字段名是 wire 格式,不是 Go 结构体名** ——
 *
 * 后端统一 snake_case(见仓库 docs 的 snake_case 迁移记录),
 * SDK 原样透传,**不做任何 camelCase 转换** ——
 * 转换一层就多一层"文档说的和实际收到的不一致"的分叉。
 */

/** 后端统一响应信封(response.Response) */
export interface Envelope<T = unknown> {
  /** 业务码,0 = 成功 */
  code: number
  data?: T
  message: string
  /** 毫秒时间戳 */
  timestamp: number
  /** 排查问题时把这个给后端 */
  trace_id?: string
}

/** 分页列表(后端 response.PageData) */
export interface RecordList<T = Record<string, any>> {
  list: T[]
  total: number
  page: number
  size: number
}

/**
 * 一条记录
 *
 * ⚠️ **键是字段标识(field_key),不是数据库物理列名**;
 * 另有系统字段:id / owner_id / owner_name / created_at / updated_at,
 * 软删后的行还有 deleted_at。
 */
export type RecordModel = Record<string, any>

/**
 * Schema 里的一个字段(RecordSchema 接口返回的字段视图)
 *
 * ⚠️ **选项不一定可读** —— relation / select 字段的选项可能因权限
 * 下发不了(option_readable = false),这时 options 是空数组,
 * 不要当成"没有选项"。
 */
export interface SchemaField {
  field_key: string
  label: string
  /** int / number / bool / text / date / datetime / select / multiselect / json / file / relation ... */
  type: string
  required: boolean
  options: { value: string; label: string }[]
  /** 选项是否因权限可读(false 时 options 为空数组 ≠ 没有选项) */
  option_readable?: boolean
  default_value?: string
  sort?: number
  [key: string]: unknown
}

/** RecordSchema 接口返回的集合结构 */
export interface CollectionSchema {
  /** 集合编码 */
  code: string
  name: string
  /** true = 行只归创建人自己看(拥有者隔离) */
  owner_scoped?: boolean
  /** 绑定的审批模板 id,'0' / 空 = 不绑定 */
  approval_template_id?: string
  fields: SchemaField[]
  [key: string]: unknown
}

/** getList 的查询参数 */
export interface RecordListOptions {
  /** 页码,从 1 开始 */
  page?: number
  /** 每页条数(后端上限见服务端配置,默认一般 20) */
  size?: number
  /** 关键字,模糊匹配文本字段 */
  keyword?: string
  /**
   * 筛选表达式(与 PocketBase 同形状的子集):
   *
   *   status = 'active' && amount > 1000
   *   status ?= ('active','pending')
   *   title ~ '验收'
   *   amount = null
   *
   * 运算符 := != > >= < <= ~ !~ ?=;逻辑:&& || ( )。
   * 字段不存在会报错(不是静默忽略)。
   */
  filter?: string
  /**
   * 简单等值筛选:`{ status: ['active','pending'] }` → `f_status=active,pending`。
   * 与 filter 表达式可同时使用。
   */
  filters?: Record<string, string | string[]>
  /** 排序字段标识 */
  orderBy?: string
  /** asc / desc */
  orderDir?: 'asc' | 'desc'
  /** 只取这些字段(逗号分隔),减少传输 */
  fields?: string | string[]
  /** true = 不算总数(大表翻页更快,此时 total 为 0) */
  skipTotal?: boolean
  /** true = 看回收站(软删的行) */
  trash?: boolean
  /** 按创建人过滤 */
  owner?: string
}

/** send 请求级选项 */
export interface SendOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  /** 请求体(自动 JSON.stringify + Content-Type: application/json) */
  body?: unknown
  /** 追加查询参数(已拼好的值,不再做深编码) */
  query?: Record<string, string>
  /** 追加请求头(会与默认头合并,同名时这个优先) */
  headers?: Record<string, string>
  /** 透传给 fetch 的 AbortSignal */
  signal?: AbortSignal
}

/** Client 构造选项 */
export interface ClientOptions {
  /** 自定义 fetch 实现(默认 globalThis.fetch;Node 18+ / 现代浏览器都有) */
  fetch?: typeof fetch
  /**
   * 每个请求发出前的钩子 —— 加签名头、改路径都在这里做。
   * 返回 undefined 表示不修改。
   */
  beforeSend?: (url: string, options: RequestInit) => RequestInit | void
}

/**
 * 登录响应(后端 service.AuthTokens)
 *
 * ⚠️ **token 已自动存入 authStore** —— 这个返回值主要用来拿
 * user / refresh_token:refresh_token 请集成方自己持久化,
 * access token 过期(40101)时拿它换新的,别让用户重新输密码。
 */
export interface AuthTokens {
  /** access token(已自动 authStore.save) */
  token: string
  /** 刷新令牌,单次使用(重放会被拒) */
  refresh_token: string
  /** access token 剩余秒数 */
  expires_in: number
  /** refresh token 剩余秒数 */
  refresh_expires_in: number
  user: Record<string, unknown>
  [key: string]: unknown
}

/** getCaptcha 的响应(图形验证码,base64 data URL) */
export interface CaptchaInfo {
  /** 提交登录时放进 captchaId */
  id: string
  /** data:image/png;base64,... —— 集成方自己 <img src> 或存文件 */
  image: string
  type: string
  width: number
  height: number
  /** 验证码字符数(给集成方校验输入长度用) */
  length: number
  /** 有效秒数 */
  expires_in: number
}

/** authWithPassword 的选项 */
export interface LoginOptions {
  /**
   * 图形验证码(captcha.login_enabled 开启时必填):
   * 先 getCaptcha() 拿 id,用户看图输码后一起传进来。
   * 验证码开关关闭时不用传(传了也会被忽略)。
   */
  captchaId?: string
  captchaCode?: string
  signal?: AbortSignal
}
