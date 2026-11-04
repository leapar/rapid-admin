/**
 * 审计日志 类型 —— **wire 契约的唯一真相源**
 *
 * ⚠️ 2026-10-30 重写
 *
 * # 为什么重写
 *
 *   此前有**三份**互相打架的审计类型:
 *     1. 本文件(types/audit.ts)里的一份 —— 字段全是**凭空写的**
 *        (`role_code` / `method` / `path` / `duration_ms` / `response_body`),
 *        后端 model 里**一个都没有**;
 *     2. `api/issue.ts` 里又抄了一份 camelCase 版
 *        (`requestMethod` / `requestUrl` / `responseResult` / `errorMsg` …);
 *     3. 而后端 wire 是 **snake_case**(`request_method` / `request_url` /
 *        `response_summary` / `error_msg` …)。
 *
 *   三份都不对,而且 TypeScript **不会报错** —— 类型是自己写的,编译器信它。
 *   结果:操作日志页的"请求方法 / 请求 URL / 响应结果"三列**永远是空的**,
 *   而且没有任何东西会红。
 *
 *   现在:**只有这一份**,逐字段对着后端 model 的 json tag 写。
 *   改后端字段时同 commit 改这里(项目规则)。
 *
 * # 对照表(后端 model)
 *
 *   model.SysOperationLog  → OperationLog
 *   model.SysDataChangeLog  → DataChangeLog
 *   model.SysLoginLog      → LoginLog
 *
 * ⚠️ 2026-10-21 全仓 ID 改字符串
 *
 *   后端主键是 sonyflake 19 位数(~9.5e16),超出 JS 的 2^53 安全整数上限。
 *   JSON.parse 会静默截断末 6 位 —— 回传后端必然 404,而且**不报错**。
 *   所以 wire 上所有 ID 一律是 string。本文件里的 id / *_id 一律 string,
 *   **不要**再做算术或隐式转换。
 */
import type { PageRequest } from './api'

/**
 * 操作日志
 *
 * ⚠️ **module 与 action 记的是"请求路径"和"HTTP 方法"**,不是业务枚举:
 *   审计中间件写的是 `Module: string(c.Path())`(真实请求路径,如
 *   `/rapid-admin/v1/system/users`)与 `Action: method`(如 `POST`)。
 *   另有几处 service 层直写的是逻辑名(如 `auth` / `user`)。
 *   ⇒ **module 列既可能是路径也可能是逻辑名**,前端不要按枚举硬映射,
 *     搜索用 keyword(后端对 module 做 LIKE)。
 */
export interface OperationLog {
  id: string
  user_id: string
  username: string
  /** 调用者角色码 */
  user_role: string
  /** 请求路径(中间件写)或逻辑模块名(service 直写) */
  module: string
  /** HTTP 方法(中间件写)或动作名(service 直写) */
  action: string
  description: string
  request_url: string
  request_method: string
  /** 脱敏后的请求体 */
  request_body: string
  /** 脱敏后的 query / path */
  request_params: string
  /** 响应摘要 */
  response_summary: string
  ip: string
  location: string
  /** User-Agent 原文 */
  user_agent: string
  /** 耗时(ms) */
  duration: number
  /** 冗余耗时(ms),便于聚合 */
  cost_ms: number
  /** 1 成功 / 0 失败 */
  status: number
  error_msg: string
  trace_id: string
  request_id: string
  created_at: string
  updated_at: string
}

/** 数据变更日志 */
export interface DataChangeLog {
  id: string
  /** 变更的表名(如 `biz_user`) */
  table_name: string
  /** 被变更记录的主键 */
  record_id: string
  /** 1 新增 / 2 修改 / 3 删除 */
  operation: number
  /** 变更前的值(JSON 文本) */
  before: string
  /** 变更后的值(JSON 文本) */
  after: string
  user_id: string
  username: string
  created_at: string
  updated_at: string
}

/**
 * 登录日志
 *
 * ⚠️ 2026-10-24 修正:此前这个类型是**凭空写的**,与后端 wire 几乎一个字段都对不上:
 *
 *   - status  后端是 int(1 成功 / 0 失败),这里写的是 'success'|'failed'|'locked'
 *   - 后端有 message / device / browser / os / location / login_at / logout_at,
 *     这里一个都没有;写的是 failure_reason / user_agent / geo / mfa_type / created_at
 *
 * 之所以一直没炸:auditApi.loginLogs 全仓**零调用方**(审计页的"登录日志"还是占位页)。
 * 但它是**导出的类型** —— 任何时候有人把那个接口接上,所有字段都是 undefined,
 * 而且 TypeScript **不会报错**(类型是自己写的,编译器信它)。
 *
 * 现在按后端 model.SysLoginLog 的真实 json tag 写。改后端字段时同 commit 改这里。
 */
export interface LoginLog {
  id: string
  user_id: string
  username: string
  ip: string
  location: string
  /** User-Agent 原文 */
  device: string
  browser: string
  os: string
  /** 1 成功 / 0 失败 */
  status: number
  /** 登录结果说明("ok" / "密码错误" / "账号已锁定" / ...) */
  message: string
  /**
   * 这一次登录的时刻。
   *
   * ⚠️ **可以是 null** —— 后端 model.Time 在零值时序列化成 `null`
   *   (server/internal/model/time.go:18-20),不是 "0001-01-01"。
   */
  login_at: string | null
  /** 被登出 / 强制下线的时刻;null = 还没下线 */
  logout_at: string | null
  /**
   * 会话 id(sid)—— 一次登录一条会话(2026-10-24 新增)。
   * 空串 = 登录失败的记录(失败时没有会话)。
   */
  session_id: string
  /**
   * 最后一次确认这条会话还活着的时刻(续期时更新);null = 还没续期过。
   * 判"会话是否还活着"用它,不要用 login_at —— refresh 每次轮换都重起窗口。
   */
  last_active_at: string | null
  created_at: string
}

/**
 * 操作日志查询条件
 *
 * ⚠️ **字段名必须与后端 handler 读的一字不差**(server/internal/api/handler/v1/audit.go):
 *   keyword / status / startAt / endAt / module / action / ip / userId
 *   名字对不上 = 后端**不读这个参数**(HTTP 200、结果不变、不报错)。
 */
export interface OperationLogQuery extends PageRequest {
  /** 命中 用户名 / 模块 / 动作 / 描述 任一列 */
  keyword?: string
  module?: string
  action?: string
  ip?: string
  /** 1 成功 / 0 失败;**不传 = 不筛** */
  status?: number
  /** Unix 秒 */
  startAt?: number
  /** Unix 秒 */
  endAt?: number
  userId?: string
}

/** 数据变更日志查询条件 */
export interface DataChangeLogQuery extends PageRequest {
  /** 命中 表名 */
  keyword?: string
  /** 表名精确匹配 */
  tableName?: string
  /** 记录主键精确匹配 */
  recordId?: string
  /** 1 新增 / 2 修改 / 3 删除;**不传 = 不筛** */
  operation?: number
  startAt?: number
  endAt?: number
  userId?: string
}

/** 登录日志查询条件 */
export interface LoginLogQuery extends PageRequest {
  /** 命中 用户名 */
  keyword?: string
  ip?: string
  /** true 只看成功 / false 只看失败;**不传 = 不筛** */
  success?: boolean
  startAt?: number
  endAt?: number
  userId?: string
}

/**
 * 系统运行日志(2026-11-03)
 *
 * 逐字段对着后端 model.SysSystemLog 的 json tag 写(契约规则同上)。
 * 来源四类 —— level 字典 sys_log_level(info / warn / error):
 *
 *   slow_sql    慢 SQL(>500ms,slowquery 钩子)   path 列 = 表名
 *   slow_http   慢接口(>2s,AccessLog 钩子)
 *   http_error  接口 5xx(AccessLog 钩子)
 *   panic       panic(Recover 钩子)               detail 列 = panic 栈
 */
export interface SystemLog {
  id: string
  /** info / warn / error */
  level: string
  /** slow_sql / slow_http / http_error / panic */
  source: string
  /** HTTP 方法(SQL 类为空) */
  method: string
  /** 接口路径(SQL 类为表名) */
  path: string
  /** 摘要 */
  message: string
  /** 详情(完整 SQL / panic 栈);可能被截断 */
  detail: string
  trace_id: string
  /** 出事时谁在操作;0 = 无身份(migrate / 定时任务 / 匿名) */
  user_id: string
  /** 耗时(ms);panic 无此值 */
  duration_ms: number
  created_at: string
  updated_at: string
}

/**
 * 系统日志查询条件
 *
 * ⚠️ 字段名必须与后端 handler 读的一字不差:
 *   level / source / keyword / startAt / endAt(见 buildSystemLogQuery)。
 */
export interface SystemLogQuery extends PageRequest {
  /** info / warn / error;**不传 = 不筛** */
  level?: string
  /** slow_sql / slow_http / http_error / panic;**不传 = 不筛** */
  source?: string
  /** 命中 path / message 任一列 */
  keyword?: string
  /** Unix 秒 */
  startAt?: number
  /** Unix 秒 */
  endAt?: number
}
