/**
 * 登录热力图 —— GitHub 风格的每日登录日历(M182?否,2026-11-03 工作台)
 *
 * ## 为什么是 echarts 的 calendar 坐标系
 *
 *   echarts 已经在工作台的依赖图里(TrendChart),calendar + heatmap
 *   是它的标准搭配 —— 自己画 SVG 等于把月份/星期排版、闰年、起始对齐
 *   全部重写一遍,零收益。
 *
 * ## 为什么不加进 `components/common/index.ts` 桶文件
 *
 *   与 TrendChart 同一个理由:echarts(1MB+)只服务看板,
 *   桶文件被所有业务页 import ⇒ 按路径单独引。
 *
 * ## 与 TrendChart 的分工
 *
 *   本组件只负责"把 option 交给 echarts + 空态/加载态 + 定性图例";
 *   option 的算法(分档/分桶/标签)在 `loginHeatmapOption.ts`(可单测)。
 *   文案全部由调用方传 —— 本组件不认识 i18n(与 TrendChart 同一约定)。
 */
import { useMemo } from 'react'
import type { FC } from 'react'
import { Empty, Skeleton } from 'antd'
import ReactECharts from 'echarts-for-react'
import { buildHeatmapOption, HEATMAP_COLORS, HEATMAP_EMPTY_COLOR } from './loginHeatmapOption'
import type { HeatmapPoint } from './loginHeatmapOption'

export interface LoginHeatmapProps {
  /** 数据点 —— 后端已按天补全(缺的天是 0),不必再补 */
  points: HeatmapPoint[]
  /** 加载中(且还没有任何数据时显示骨架屏) */
  loading?: boolean
  /** 没有数据时的空态文案 —— 由调用方 i18n */
  emptyText?: string
  /** 定性图例两端的字(「少」/「多」) */
  legendStart: string
  legendEnd: string
  /** 月份名,12 个,下标 0 = 1 月 */
  monthNames: string[]
  /** 星期名,7 个,下标 0 = 周日(echarts 约定) */
  dayNames: string[]
  /** 单元格 tooltip 文案 */
  tooltipLabel: (date: string, count: number) => string
  /** 图表高度(px),默认 180 */
  height?: number
}

export const LoginHeatmap: FC<LoginHeatmapProps> = ({
  points,
  loading = false,
  emptyText,
  legendStart,
  legendEnd,
  monthNames,
  dayNames,
  tooltipLabel,
  height = 180,
}) => {
  const option = useMemo(
    () =>
      buildHeatmapOption({
        points,
        monthNames,
        dayNames,
        tooltipLabel,
      }),
    [points, monthNames, dayNames, tooltipLabel],
  )

  if (loading && points.length === 0) {
    return <Skeleton active paragraph={{ rows: 4 }} />
  }
  if (points.length === 0) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyText} />
  }

  return (
    <div>
      <ReactECharts option={option} style={{ height }} notMerge opts={{ renderer: 'svg' }} />
      {/* 定性图例:少 ▢▢▢▢▢ 多(数字进 tooltip,不印在格子上) */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          gap: 3,
          marginTop: -8,
          paddingRight: 4,
        }}
      >
        <span style={{ color: 'var(--app-text-tertiary)', fontSize: 11, marginRight: 4 }}>
          {legendStart}
        </span>
        {[HEATMAP_EMPTY_COLOR, ...HEATMAP_COLORS].map((c) => (
          <span
            key={c}
            style={{
              width: 11,
              height: 11,
              borderRadius: 3,
              background: c,
              display: 'inline-block',
            }}
          />
        ))}
        <span style={{ color: 'var(--app-text-tertiary)', fontSize: 11, marginLeft: 4 }}>{legendEnd}</span>
      </div>
    </div>
  )
}

export default LoginHeatmap
