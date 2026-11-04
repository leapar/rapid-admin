/**
 * 定时任务 类型 —— M69 配套
 * 2026-10-21 新建,对齐后端 server/internal/service/job.go 的 View DTO
 *
 * 配套文档: docs/定时任务调度框架层设计方案V1.0.md
 *
 * ⚠️ 三条 wire 约定,写错都是**静默失效**,不会报错:
 *
 *   1. query 参数 **camelCase**、body 字段 **snake_case**。
 *      handler 层只对 query 做了翻译(见 handler/v1/job.go:1-15),
 *      body 走标准 json tag。c.Query("job_key") 匹配不到 "jobKey" ——
 *      Hertz 的 c.Query 是精确取值,不像 Go 的 json 解码那样忽略下划线/大小写。
 *
 *   2. 分页字段是 `page` / **`size`**,不是全仓 PageResponse 的 `page_size`。
 *      后端 handler.GetPage 读的是 c.Query("size")(api/handler/handler.go:88),
 *      response.PageData 也只回 `size`(pkg/response/response.go:23-28)。
 *      照抄 PageResponse<T> 会让"每页条数"永远落到后端默认值上,
 *      而且**看起来一切正常** —— 这正是 config/audit.tsx 现在的状况(存量 bug,未修)。
 *      所以本文件单独定义 JobRunPage,不 extends PageResponse。
 *
 *   3. JobRun.id 是 string(后端 `json:"id,string"`)。sonyflake 19 位数超出
 *      JS 的 2^53 安全整数,裸数字会被 JSON.parse 静默截断(见 types/config.ts:11-19)。
 */

/** 任务状态(后端 model.JobStatus —— 只有三态,没有 skipped) */
export type JobStatus = 'running' | 'success' | 'failed' | string

/** 触发来源(后端 model.JobTriggerType) */
export type JobTriggerType = 'schedule' | 'manual' | string

/** 单个定时任务(后端 JobItemView) */
export interface JobItem {
  job_key: string
  name: string
  /** 运营备注(DB 覆盖)。为空表示用代码里的默认值 */
  description: string

  /** 当前**生效**的表达式(代码默认 ∪ DB 覆盖) */
  cron_expr: string
  /** 代码里的默认表达式。改坏了要能一眼看出「原来是什么」 */
  default_cron_expr: string
  /** 生效值来自 DB 覆盖而非代码默认 */
  cron_overridden: boolean

  /** 最终生效的启停(code && DB) */
  enabled_effective: boolean
  /** 代码侧开关 */
  enabled_code: boolean
  /** DB 侧开关(没有 DB 行 / 行已失效时为 true) */
  enabled_db: boolean
  /** DB 覆盖行是否仍有效。false = 代码里已删任务,sys_job 只剩没人认领的残留行 */
  active: boolean

  /** 单次执行超时(毫秒) */
  timeout_ms: number
  /** 声明为非幂等:去重降级(锁不可用)时会被跳过而不是重复跑 */
  non_idempotent: boolean

  sort: number

  /**
   * 下次触发时间(RFC3339)。
   * 停用 / 未注册时是**空字符串**而不是零值时间 —— 空串显示「未调度」,
   * 1970 那一眼很吓人。后端刻意这么返,前端不要写 `new Date(v).getTime() || now`。
   */
  next_run_at: string

  /** 当前有没有 DB 覆盖行(决定「恢复默认」按钮是否可点) */
  overridden: boolean
}

/** 执行记录(后端 JobRunView) */
export interface JobRun {
  /** string,不是 number —— 见文件头第 3 条 */
  id: string
  job_key: string
  trigger_type: JobTriggerType
  status: JobStatus
  /** Unix 秒 */
  started_at: number
  /** Unix 秒,未完成为 0 */
  finished_at: number
  duration_ms: number
  /** 失败原因,服务端截断 2KB */
  error: string
  instance_id: string
  trace_id: string
  /** 仅手动触发有值 */
  operator: string
  /** 本次执行时分布式锁不可用 —— 多副本下可能重复执行 */
  degraded: boolean
}

/**
 * 执行记录分页。
 *
 * 刻意**不**复用 @/types/api 的 PageResponse<T>:那个的字段是 page_size,
 * 后端 job 的 runs 接口返的是 size(见文件头第 2 条)。
 */
export interface JobRunPage {
  list: JobRun[]
  total: number
  page: number
  size: number
}

/** 调度器运行态(后端 cronx.RuntimeInfo) */
export interface JobRuntime {
  /** job.enabled 配置开关。false = 整个调度器没跑 */
  enabled: boolean
  /** 锁模式:redis / memory / none */
  lock_mode: string
  lock_available: boolean
  /** 锁不可用 → 多副本部署下任务可能重复执行 */
  degraded: boolean
  instance_id: string
  timezone: string
  /** 当前注册任务数 */
  registered: number
}

/**
 * 任务列表 query。
 *
 * ⚠️ GET /jobs **不分页** —— 任务本体只在代码里注册,数量是几十量级,
 * 不存在需要翻页的必要。后端返回裸数组(不是 PageResponse)。
 */
export interface JobListQuery {
  keyword?: string
  enabledOnly?: boolean
}

/** 执行记录 query(注意 camelCase) */
export interface JobRunListQuery {
  page?: number
  size?: number
  jobKey?: string
  status?: JobStatus
  triggerType?: JobTriggerType
  degradedOnly?: boolean
  /** Unix 秒 */
  startAt?: number
  /** Unix 秒 */
  endAt?: number
}

/**
 * PATCH body(**snake_case**,与 query 相反)。
 *
 * `reset_to_default` 是**显式布尔**,不是「cron_expr 传空串」表达恢复默认:
 * 空串在前端表单里天然是"用户把输入框清空了",把它解释成恢复默认的话,
 * 一次误删键就会永久丢掉代码里的默认表达式,而且没有任何提示。
 * 详见 server/internal/api/handler/v1/job.go:131-135。
 */
export interface UpdateJobBody {
  name?: string
  description?: string
  cron_expr?: string
  enabled?: boolean
  sort?: number
  reset_to_default?: boolean
}