/**
 * 定时任务 API —— M69 配套
 * 2026-10-21 新建
 *
 * 路径: 后端 /rapid-admin/v1/platform/jobs (router.go:471-487)
 *
 * ⚠️ query 是 camelCase、body 是 snake_case,详见 @/types/job 文件头。
 *
 * 没有 create / delete:任务本体只在代码里 cronx.Register(发版才能增删),
 * sys_job 存的只是运行态覆盖。所以本文件**刻意**不提供这两个方法 ——
 * 调了会 404,而且是那种"我以为加了个按钮怎么没反应"的 404。
 */
import { http } from './client'
import type {
  JobItem,
  JobRun,
  JobRuntime,
  JobRunPage,
  JobListQuery,
  JobRunListQuery,
  UpdateJobBody,
} from '@/types/job'

export const jobApi = {
  /**
   * 任务列表(不分页,裸数组)
   * 后端: GET /jobs?keyword=&enabledOnly=
   */
  list: (q: JobListQuery) =>
    http.get<JobItem[]>('/platform/jobs', q as unknown as Record<string, unknown>),

  /** 任务详情 — GET /jobs/:jobKey */
  detail: (jobKey: string) => http.get<JobItem>(`/platform/jobs/${jobKey}`),

  /**
   * 调度器运行态(锁是否可用 / 副本 ID / 时区 / 注册数)
   * 后端: GET /jobs/runtime
   *
   * 单独一个接口而不是塞进列表字段:锁可用性是**进程级**状态,同进程内所有任务共享。
   * 做成 per-job 字段会让人误以为某个任务有自己的锁。
   */
  runtime: () => http.get<JobRuntime>('/platform/jobs/runtime'),

  /**
   * cron 表达式预览:返回接下来 n 次触发时间
   * 后端: GET /jobs/cron/preview?expr=&n=  (n 上限 20)
   *
   * 时区用**服务端调度器的**而不是浏览器本地时区:两边不一致时,
   * 预览出来的 03:00 和实际触发的 03:00 会差一整个时区,那样比没有更害人。
   * 非法表达式后端返 400(而不是空数组),调用方据此红字提示。
   */
  cronPreview: (expr: string, n = 5, silent = false) =>
    http.get<string[]>(
      '/platform/jobs/cron/preview',
      { expr, n },
      { silent },
    ),

  /** 执行记录(分页) — GET /jobs/runs */
  runs: (q: JobRunListQuery) =>
    http.get<JobRunPage>('/platform/jobs/runs', q as unknown as Record<string, unknown>),

  /**
   * 改运行态覆盖(启停 / 改频率 / 改名 / 备注 / 排序 / 恢复默认)
   * 后端: PATCH /jobs/:jobKey
   *
   * 这是**唯一**写通道 —— 后端没有 create/delete 路由,配置改完立即热更新生效,
   * 不需要重启(只写 DB 不 Reload 的话,重启前看着改了、重启后回滚)。
   */
  update: (jobKey: string, data: UpdateJobBody) =>
    http.patch<JobItem>(`/platform/jobs/${jobKey}`, data),

  /**
   * 手动触发一次 —— POST /jobs/:jobKey/trigger
   *
   * ⚠️⚠️ **同步**返回,不是 202。请求会挂到任务跑完,返回的是那条执行记录。
   *
   * ⚠️⚠️ 调用方**必须**判 `status !== 'success'`,不能只看 HTTP 200:
   *   任务**自身**失败时后端返 200 + 一条 status="failed" 的记录(不是 error),
   *   因为「没能跑起来」(任务不存在 / 已停用 / 正在执行 / 去重降级)才返 error。
   *   不判 status 的话,失败的定时任务会弹一个绿色的「触发成功」。
   *   契约见 server/internal/service/job.go 的 TriggerNow 注释。
   *
   * 返回 error 的是这些场景(码值见 service/job.go mapTriggerError):
   *   - 任务不存在            404 NotFound
   *   - 任务已停用            403 Forbidden  ← 刻意不是 400:这是权限语义,
   *                              "你没开这个任务"和"你参数填错了"处置不一样
   *   - 上一次还在跑(去重命中) 409 Conflict
   *   - 去重降级且任务非幂等   500 Internal(单独码,不混在 409 里,好让告警跳出来)
   *   - cron 表达式空 / 非法   400 BadRequest(手动触发实际走不到,防御性映射)
   */
  trigger: (jobKey: string) => http.post<JobRun>(`/platform/jobs/${jobKey}/trigger`),
}