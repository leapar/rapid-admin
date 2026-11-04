/**
 * 工作台(仪表盘)API
 *
 * 后端端点(router.go 的 platform 组,base = /rapid-admin/v1):
 *   GET /platform/dashboard              概览(框架指标)
 *   GET /platform/dashboard/login-heatmap  每日登录热力图
 *   GET /platform/dashboard/server-metrics 服务器资源快照
 *
 * ⚠️ **字段名是 snake_case**(server/internal/service/dashboard.go 的
 *   `Overview` 结构体 json tag)。这类错**编译期不报错**(类型是前端自己写的),
 *   运行时也不报错 —— 只是读到 undefined ⇒ KPI 卡片永远是 "-"。
 *
 * ⚠️ **字段是"可缺省"的 —— 服务端按域权限过滤**
 *
 *   后端把**没权限的域整个从响应里省掉**
 *   (`Overview` 的字段是 `*int64` + `omitempty`
 *   ⇒ JSON 里**压根没有这个 key**),所以:
 *
 *   · **字段缺省 = 当前身份看不到这个域** → 前端**不画**这张卡片;
 *   · **字段在、值是 0 = 看得见,只是这个月一个都没有** → 前端**照画**(显示 0)。
 *
 *   ⚠️ 判"在不在"必须用 `!== undefined`,**不能用真值判断** ——
 *     真值判断会把 0 当成"没有" ⇒ 有权限的域显示成空卡片。
 *
 *   ⚠️ 前端**不自己判权限**(显示层不许自己算,必须读执行层那个权威字段):
 *     前端能拿到的权限码来自 `/resource/accessible`(绑定矩阵 = 显示层真相),
 *     后端算可见性用的是 casbin(执行层真相),
 *     运行时改绑定 → casbin 同步之间有一个窗口,两者会分叉。
 *     ⇒ 前端**只认"服务端给了没有"**。
 */
import { http } from './client'

/**
 * 概览 —— 框架指标(文件统计 / 在线人数)
 *
 * ⚠️ **每个字段都是可选的** —— 缺省 = 当前身份没有该域的权限,
 *   服务端**不会**把这个 key 放进响应(见文件头)。
 */
export interface DashboardOverview {
  /** 文件总数(缺省 = 没有 file:list)(2026-11-03 存储统计) */
  file_total?: number
  /** 今日上传(东八区自然日)(缺省 = 没有 file:list) */
  file_today?: number
  /** 存储占用字节数(缺省 = 没有 file:list) */
  file_storage_bytes?: number
  /** 文件夹总数(缺省 = 没有 folder:list) */
  folder_total?: number
  /**
   * 当前在线人数(缺省 = 没有 dashboard:read;但它在 authBaseCodes 里,
   * 实际上每个登录角色都看得到 —— 纯计数,不含任何人的明细)。
   * 口径:成功登录 + 未登出 + 最近活动在 10min 窗口内,**按用户去重**。
   */
  user_online?: number
}

/** 趋势上的一个点 */
export interface DashboardTrendPoint {
  /** YYYY-MM-DD(东八区) */
  date: string
  value: number
}

/** Redis 概览(聚合数字;后端刻意不列 key —— key 名单是 redis-cli --scan 的活) */
export interface ServerRedisMetrics {
  /** Redis 是否可用(没装配 / ping 不通都算 false) */
  available: boolean
  /** redis_version(如 "8.2.0",Memurai 兼容上报) */
  version: string
  /** Redis 自身数据占用的内存(INFO used_memory) */
  used_memory_bytes: number
  /** 当前库的 key 总数(DBSIZE) */
  keys: number
}

/** 整机上的一块分区(sysmetrics.DiskMount 的 wire 形状) */
export interface ServerDiskMount {
  /** 挂载点 / 盘符(Windows 上是 "C:\",Linux 上是 "/") */
  mount: string
  total_bytes: number
  used_bytes: number
  percent: number
}

/**
 * 服务器资源快照(CPU / 内存 / 磁盘)
 *
 * ⚠️ **权限窄一档** —— 路由挂 dashboard:server(只绑平台运营线),
 *   不是 dashboard:read。前端卡片按权限 store 里有没有这个码决定显隐。
 *
 * ⚠️ CPU 的值是 **200ms 观测窗口的平均占用**(不是瞬时值),
 *   所以此接口的响应天然比别的慢 ~200ms,是刻意的不是卡了。
 */
export interface ServerMetrics {
  /** CPU 整机平均占用(0-100)—— 每核数值的平均 */
  cpu_percent: number
  /** CPU 逻辑核数 */
  cpu_cores: number
  /** 每核占用(下标 = 核序号,长度 = cpu_cores)—— 照参考样式画每核进度条 */
  percpu_percent: number[]
  mem_used_bytes: number
  mem_total_bytes: number
  mem_percent: number
  /** 磁盘 = 进程工作目录所在盘(即数据目录所在那块盘);disk_path 告诉用户测的是哪块 */
  disk_path: string
  /** Redis 概览 —— 后端任何失败都降级成 available:false,不拖垮系统指标段 */
  redis?: ServerRedisMetrics
  disk_used_bytes: number
  disk_total_bytes: number
  disk_percent: number
  /** 操作系统(runtime.GOOS,如 "windows" / "linux") */
  os: string
  /**
   * 整机各分区(数据盘之外的盘都在这)—— 后端枚举 disk.Partitions,
   * 空槽 / 虚拟盘(Total=0)已跳过。枚举失败时是 undefined,渲染要兜底。
   */
  disks?: ServerDiskMount[]
  /** 编译器(runtime.Compiler,恒 "gc") */
  compiler: string
  num_goroutine: number
  go_version: string
  /** CPU 采样实际花的毫秒数(≈200) */
  sample_ms: number
}

export const dashboardApi = {
  /** 关键指标 */
  overview: () => http.get<DashboardOverview>('/platform/dashboard'),

  /** 每日登录热力图(默认 182 天;后端上限 366,缺的天补 0) */
  loginHeatmap: (days: number = 182) =>
    http.get<DashboardTrendPoint[]>('/platform/dashboard/login-heatmap', { days }),

  /** 服务器资源快照(打开工作台采一次 + 手动刷新,不轮询) */
  serverMetrics: () => http.get<ServerMetrics>('/platform/dashboard/server-metrics'),
}
