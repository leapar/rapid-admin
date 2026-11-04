/**
 * useTable —— 分页列表 hook(配合 DataTable 组件)
 *
 * 用法:
 *   const { tableProps, search, reset, loading, data } = useTable({
 *     fetch: (q) => userApi.list(q),
 *     initialQuery: { page: 1, size: 20 },
 *   })
 *
 * ⚠️ **分页字段是 `size`,不是 `pageSize` / `page_size`**(2026-10-25 统一)
 *   · 后端只认 `page` + `size`(`BaseHandler.GetPage`,
 *     server/internal/api/handler/handler.go:86)
 *   · 以前这个 hook 内部写的是 `pageSize`,而 `PageRequest` 声明的是
 *     `page_size` —— **后端两个都不读** → 全仓 useTable 页面的
 *     "每页条数"选择器都是失效的(选了 50 条,后端按默认 20 返回;
 *     页码是对的,因为 `page` 三方一致)
 *   · 现在内部统一成 `size`;旧的两种写法仍能在 api/client.ts 的出口
 *     被翻成 `size`(直接调 API 的老代码不用改也能跑对)
 *
 * ⚠️ **请求竞态防护**(2026-10-25 M6):
 *   两个请求同时在飞时,先发的不一定先回 —— 后发的先回、慢的后来,
 *   旧响应会把新数据覆盖掉(页面显示的是上一次筛选的结果)。
 *   每次发起请求领一个自增号,只有最后一次的号才有资格写 state。
 *
 * ⚠️ **错误态通道 + 断网恢复自动重载**(2026-10-25 M7):
 *   · 失败不再只留一个"控制台红字":error / onRetry 随 tableProps 一起交给
 *     表格外壳(ManagementTable / DataTable),由它们渲染成"加载失败 + 重试"。
 *   · 断网恢复时自动重载一次 —— 以前 OfflineBanner 只负责"提示",
 *     注释里写着"网络恢复时调 useTable 的 reload 是组件外的事",
 *     **结果这一环没人接**:用户得自己刷新页面。
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import type { PageRequest, PageResponse } from '@/types/api'
import { tKey } from '@/locales'
import { readFieldPolicyReport, type FieldPolicyReport } from '@/utils/fieldPolicy'
import { useOnline } from './useOnline'

/**
 * 表格外壳的错误通道
 *
 * 为什么塞进 `tableProps` 而不是让页面单独传:
 *   17 个业务页面的写法都是 `<ManagementTable tableProps={tableProps} />`,
 *   页面拿到的是 useTable 的返回值。错误信息如果放在 useTable 结果的
 *   **另一个字段**上,就得改 17 个页面才能透传;放进 tableProps 里,
 *   外壳直接就能读到 —— **业务页零改动**。
 *
 * ⚠️ 这两个字段**不是 Antd Table 的 prop**,外壳在展开给 `<Table>` 之前
 *   必须先摘掉(否则会漏到 DOM 上)。见 ManagementTable / DataTable。
 */
export interface TableErrorChannel {
  /** 最近一次加载的错误(成功时为 null) */
  error?: unknown
  /** 错误态的"重试"回调(重新拉一次当前查询) */
  onRetry?: () => void
  /**
   * ⚠️ **字段级可见性报告**(M168)—— 服务端这次响应收窄了哪些字段
   *
   * 服务端"隐藏"是删键 ⇒ 响应里根本没有那个键 ⇒
   * 表格的列定义还列着它 = **列还在、格是空的**。
   * 外壳(ManagementTable / DataTable)拿它把那些列过滤掉;
   * 表单/详情拿它把字段藏掉、把打码字段变只读。
   *
   * ⚠️ **报告由执行层(服务端)算,前端只读** —— 与 M149 同一条纪律,
   *   前端不自己算规则(见 utils/fieldPolicy.ts 的文件头)。
   *
   * ⚠️ 不是 Antd 的 prop,外壳展开给 `<Table>` 之前必须摘掉。
   */
  fieldPolicy?: FieldPolicyReport | null
}

/**
 * ⚠️ **接口可以带额外字段**(比如文件夹列表的 `owner_name`、
 *   用户层的 `cross_user` / `current_user_id`),但框架的 `useTable`
 *   只把 list/total/page/size 拆出来当 state。额外字段要从
 *   `raw` 里拿(见 UseTableResult.raw)。
 */
export interface UseTableOptions<Q extends PageRequest, T, R extends PageResponse<T> = PageResponse<T>> {
  /**
   * 拉取函数
   *
   * ⚠️ **返回类型是 R** —— 有些接口的响应带**非分页的字段**
   *   (文件夹列表的 `owner_name`、用户层的 `cross_user`)。
   *   把 R 显式声明出来,页面才能在 `raw` 上拿到这些字段的**类型**,
   *   而不是 `as any` 一把梭。
   */
  fetch: (q: Q) => Promise<R>
  /** 初始查询条件 */
  initialQuery?: Partial<Q>
  /** 依赖变化时自动 reload(如项目切换) */
  deps?: React.DependencyList
}

export interface UseTableResult<Q extends PageRequest, T, R extends PageResponse<T> = PageResponse<T>> {
  loading: boolean
  data: T[]
  total: number
  /**
   * 最近一次 fetch 的**原始响应**(框架分页字段之外的东西也在这儿)
   *
   * ⚠️ **为什么需要它**:有些接口的响应里带着**非分页的标志位** ——
   *   比如文件夹管理的用户层要返回 `cross_user`(调用者能不能跨用户读)
   *   和 `current_user_id`(调用者自己是谁)。`PageResponse<T>` 只有
   *   list/total/page/size 四个字段,框架会把这些额外字段**丢掉** ——
   *   页面就只能自己再发一次请求去拿,或者(更糟)在前端猜角色。
   *
   * ⚠️ **没有这一项时是 null**(还没加载成功 / 加载失败)。
   */
  raw: R | null
  /**
   * 最近一次加载的错误(成功时为 null)
   *
   * ⚠️ 2026-10-25 M6 新增:以前 fetch 失败会变成 **unhandled rejection**
   *   (useEffect 里 `load(query)` 没有 catch)→ 控制台一片红,
   *   而且页面既不知道失败了、也不知道该显示什么。
   *   错误状态交给页面自己决定怎么呈现(框架不替业务弹第二层提示 ——
   *   api/client.ts 已经弹过一次了,再弹一次是噪音)。
   */
  error: unknown
  query: Q
  /** Antd Table props(可直接 <Table {...tableProps}/>) */
  tableProps: {
    dataSource: T[]
    loading: boolean
    /**
     * ⚠️ **框架通道,不是 Antd 的 prop**(2026-10-25 M7)
     *   错误态 + 重试回调。外壳(ManagementTable / DataTable)会摘掉这两个
     *   字段再渲染表格,页面不需要(也不能)自己处理。
     */
    error: unknown
    onRetry: () => void
    /**
     * ⚠️ **框架通道,不是 Antd 的 prop**(M168)
     *   字段级可见性报告 —— 外壳拿它过滤列;详见 TableErrorChannel.fieldPolicy。
     */
    fieldPolicy: FieldPolicyReport | null
    pagination: {
      current: number
      pageSize: number
      total: number
      showSizeChanger: true
      showQuickJumper: true
      showTotal: (total: number) => string
      onChange: (page: number, pageSize: number) => void
    }
  }
  /** 触发查询(传 0/0 走默认值) */
  search: (extra?: Partial<Q>) => void
  /** 重置查询(回到 initialQuery) */
  reset: () => void
  /** 主动 reload */
  reload: () => void
  /** 修改查询条件(不触发查询) */
  setQuery: React.Dispatch<React.SetStateAction<Q>>
}

export function useTable<Q extends PageRequest, T, R extends PageResponse<T> = PageResponse<T>>(
  options: UseTableOptions<Q, T, R>
): UseTableResult<Q, T, R> {
  const { fetch: fetchFn, initialQuery, deps = [] } = options
  const [query, setQuery] = useState<Q>(() => ({
    page: 1,
    size: 20,
    ...initialQuery,
  } as Q))
  const [data, setData] = useState<T[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  /** 最近一次 fetch 的原始响应(框架分页之外的字段靠它拿) */
  const [raw, setRaw] = useState<R | null>(null)
  /**
   * 字段级可见性报告(M168)—— 从最近一次成功响应上读回
   *
   * ⚠️ **从响应信封上读,不从行上读** ——
   *   列表为空时行不存在,但"这一列该藏"照样成立
   *   (报告是"规则说了什么",不是"这次改了什么")。
   *   盖章方(api/client.ts)把报告同时盖在信封和每一行上,
   *   这里读信封这份。
   */
  const [fieldPolicy, setFieldPolicy] = useState<FieldPolicyReport | null>(null)

  /**
   * 请求序号 —— **竞态防护**(2026-10-25 M6)
   *
   * ⚠️ 为什么必须要有:
   *   两个请求同时 in-flight 时,**先发的不一定先回**。
   *   用户快速改筛选条件 / 连点翻页,后发的请求可能先回来,
   *   然后先发的那个慢悠悠回来,把新数据**覆盖成旧数据** ——
   *   页面显示的是"上一次筛选"的结果,而筛选框写的是新的。
   *   这个 bug 极难复现(要后端响应顺序反常),但一旦出现就是"数据不对",
   *   用户会认为是后端算错了。
   *
   * 做法:每次发起请求领一个号(自增),只有**最后一次发起的那个号**
   *   才有资格写 state。过期响应直接丢掉。
   *   顺带解决 loading 的同类问题:旧请求的 finally 不能再把新请求的
   *   loading 关掉(否则转圈提前消失,但数据还没到)。
   */
  const seqRef = useRef(0)

  /**
   * 最近一次加载是否失败 —— **断网恢复自动重载的判据**(M7)
   *
   * 为什么要单独一个 ref,而不是直接读 `error` state:
   *   `online` 事件是**同步回调**,它触发时 React 还没重新渲染,
   *   闭包里读到的 state 可能还是上一次的。用 ref 存"当前真实状态",
   *   读到的永远是最新值。
   */
  const hasErrorRef = useRef(false)

  const load = useCallback(async (q: Q) => {
    const seq = ++seqRef.current
    setLoading(true)
    try {
      const resp = await fetchFn(q)
      // 过期响应:丢掉,不碰 state
      if (seq !== seqRef.current) return
      setData(resp.list ?? [])
      setTotal(resp.total ?? 0)
      // ⚠️ **原始响应一起存** —— 框架分页字段之外的标志位(cross_user 之类)
      //   只能从这里拿,丢了页面就得再发一次请求。
      setRaw(resp)
      // M168:字段可见性报告从响应信封上读回(盖章方见 api/client.ts)
      setFieldPolicy(readFieldPolicyReport(resp))
      setError(null)
      hasErrorRef.current = false
    } catch (err) {
      // 过期请求的失败也不算数(更新的请求已经在路上了)
      if (seq !== seqRef.current) return
      setError(err)
      hasErrorRef.current = true
      // ⚠️ **不清空 data** —— 一次网络抖动就把整页洗白,
      //   用户看到的是"表格空了",而不是"这次没加载出来"。
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
  }, [fetchFn])

  useEffect(() => {
    load(query)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, ...deps])

  const search = useCallback((extra?: Partial<Q>) => {
    setQuery((prev) => ({ ...prev, ...extra, page: 1 } as Q))
  }, [])

  const reset = useCallback(() => {
    setQuery({ page: 1, size: 20, ...initialQuery } as Q)
  }, [initialQuery])

  const reload = useCallback(() => {
    load(query)
  }, [load, query])

  /**
   * reload 的最新引用 —— 给 `online` 事件用(M7)
   *
   * `online` 回调是**同步**触发的,那时 React 还没重新渲染,
   * 直接闭包捕获的 reload 可能是旧 query 的那一份(旧页码)。
   * 用 ref 存最新的一份,回调里读 ref 才是当前查询。
   */
  const reloadRef = useRef(reload)
  useEffect(() => {
    reloadRef.current = reload
  }, [reload])

  /**
   * 断网恢复 → 自动重载一次(M7)
   *
   * 以前这条链是断的:OfflineBanner 只负责"提示",它自己的注释写着
   * "网络恢复时调 useTable 的 reload 是组件外的事,此组件只负责提示,不做事"
   * —— 而**没有任何组件**去接这个"组件外的事"。
   * 结果:断网时列表加载失败,网络恢复后页面还是空的,
   * 用户只能自己刷新浏览器(或者去点那个"重试")。
   *
   * 判据是 `hasErrorRef`:只有"因为失败而空着"的列表才需要自动重载,
   * 正常加载成功的列表不重复请求(避免无谓的流量 + 服务端压力)。
   */
  useOnline({
    onReconnect: () => {
      if (hasErrorRef.current) reloadRef.current()
    },
  })

  return {
    loading,
    data,
    total,
    raw,
    error,
    query,
    tableProps: {
      dataSource: data,
      loading,
      // 框架通道:错误 + 重试 + 字段可见性报告(外壳会摘掉再渲染给 Antd Table)
      error,
      onRetry: reload,
      fieldPolicy,
      pagination: {
        current: query.page,
        // ⚠️ 读的是 query.size(后端 wire 名);旧名兜底,防止有人手写了
        //    initialQuery: { pageSize: 50 } 导致分页器显示 20 而后端回 50
        pageSize: query.size ?? query.pageSize ?? query.page_size ?? 20,
        total,
        showSizeChanger: true,
        showQuickJumper: true,
        showTotal: (t) => tKey('common.total', undefined, { n: t }),
        onChange: (page, pageSize) => {
          setQuery((prev) => ({ ...prev, page, size: pageSize } as Q))
        },
      },
    },
    search,
    reset,
    reload,
    setQuery,
  }
}
