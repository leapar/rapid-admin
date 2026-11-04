/**
 * 趋势图 —— ECharts 折线图的薄封装(2026-10-30 · M116 工作台)
 *
 * ## 为什么不直接用 `echarts-for-react`
 *
 *   1. **option 的算法要能单测** —— 造 option 的纯函数拆在
 *      `trendChartOption.ts`(见那里的理由:纯 `.ts` 才躲得过
 *      `react-refresh/only-export-components`)。
 *      本组件只负责"把 option 交给 echarts + 处理空态/加载态"。
 *   2. **空态与加载态必须有** —— 看板是"看板",空图和骨架屏都要有文案,
 *      而文案要走 i18n ⇒ 文案由调用方传进来(本组件不认识字典)。
 *
 * ## 为什么**不**加进 `components/common/index.ts` 桶文件
 *
 *   那个桶文件被**所有**业务页 import。加进来 = 每个页面都把
 *   echarts(1MB+)拖进依赖图。echarts 只服务看板 ⇒ 单独按路径引。
 */
import { useMemo } from 'react'
import type { FC } from 'react'
import { Empty, Skeleton } from 'antd'
import ReactECharts from 'echarts-for-react'
import { buildTrendOption } from './trendChartOption'
import type { TrendSeries } from './trendChartOption'

export interface TrendChartProps {
  /** X 轴 —— `YYYY-MM-DD` */
  dates: string[]
  /** 序列 */
  series: TrendSeries[]
  /** 图表高度(px) */
  height?: number
  /** 加载中(且还没有任何数据时显示骨架屏) */
  loading?: boolean
  /** 没有数据时的空态文案 —— 由调用方 i18n,本组件不认识字典 */
  emptyText?: string
}

export const TrendChart: FC<TrendChartProps> = ({
  dates,
  series,
  height = 240,
  loading = false,
  emptyText,
}) => {
  const option = useMemo(() => buildTrendOption({ dates, series }), [dates, series])

  // 首次加载(还没有任何数据)→ 骨架屏
  if (loading && dates.length === 0) {
    return <Skeleton active paragraph={{ rows: 5 }} />
  }
  // 真的没数据(接口失败 / 后端返回空)→ 空态
  if (dates.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={emptyText}
        style={{ marginTop: 48 }}
      />
    )
  }
  return <ReactECharts option={option} style={{ height, width: '100%' }} notMerge lazyUpdate />
}

export default TrendChart
