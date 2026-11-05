/**
 * 趋势图 option 构造 —— 纯函数(2026-10-30 · M116 工作台)
 *
 * ## 为什么要单独一个 `.ts` 文件
 *
 *   仓库 lint 是 `eslint . --max-warnings 0` —— **warning 也算失败**。
 *   `react-refresh/only-export-components` 禁止"组件文件里导出非组件",
 *   所以"造 option 的纯函数"不能塞在 `TrendChart.tsx` 里
 *   (塞进去 ⇒ 一条 warning ⇒ lint 直接红)。
 *   纯 `.ts` 模块没有组件导出 ⇒ 这条规则不适用。
 *
 * ## 为什么只 `import type`,不 import echarts 的运行时
 *
 *   `import type { EChartsOption } from 'echarts'` 是**类型导入**,
 *   编译后被完全擦除 ⇒ 本文件不引入 echarts 的运行时 ⇒
 *   单测里不需要 canvas、不需要 jsdom 的 canvas 补丁,
 *   纯函数的断言可以跑在**任何**环境(Node / jsdom 都行)。
 *   运行时只在 `TrendChart.tsx` 里通过 `echarts-for-react` 引入一次。
 *
 * ## 这一层为什么值得单独测
 *
 *   X 轴标签、桶补全、序列颜色都是"算错了也看不出来"的东西 ——
 *   图画出来了,只是数字对不上。所以 option 的形状必须有单测钉住。
 */
import type { EChartsOption } from 'echarts'
// getSkin 是模块级查表(纯对象),图表构造期调用无副作用;
// 0 号序列颜色跟随皮肤品牌主色(2026-11-05 皮肤沉浸式)
import { getSkin } from '@/theme/registry'

/** 一个数据序列(一条折线) */
export interface TrendSeries {
  /** 序列名 —— 图例 / tooltip 用 */
  name: string
  /** 数据点 —— 与 `dates` **一一对应**(后端已按天补全,缺的天是 0) */
  data: number[]
  /** 折线颜色;不传就按顺序取默认色 */
  color?: string
}

/** 构造 option 的入参 */
export interface TrendOptionInput {
  /** X 轴 —— `YYYY-MM-DD`(东八区自然日) */
  dates: string[]
  /** 序列 */
  series: TrendSeries[]
  /** 是否画数据点;不传则"点数 ≤ 14 才画"(30 天时太密) */
  showSymbol?: boolean
}

/** 默认配色(0 号序列 = 皮肤品牌主色,其余 antd 色板同族) */
export const DEFAULT_SERIES_COLORS = ['#52c41a', '#fa8c16', '#eb2f96'] as const

/**
 * X 轴标签:`YYYY-MM-DD` → `MM-DD`
 *
 * ⚠️ 30 天窗口下全量显示 `YYYY-MM-DD` 会挤成一团(标签互相压),
 *   而"哪一年"对看板毫无意义 —— 月日足够。
 */
export function formatAxisLabel(date: string): string {
  return date.length >= 10 ? date.slice(5) : date
}

/** 取第 i 个序列的颜色(0 号 = 皮肤品牌主色,跟随换肤;周期 = 主色 + 色板长) */
export function seriesColor(index: number, override?: string): string {
  if (override) return override
  const cycle = DEFAULT_SERIES_COLORS.length + 1
  const i = ((index % cycle) + cycle) % cycle
  if (i === 0) return getSkin().brand.primary
  return DEFAULT_SERIES_COLORS[i - 1] ?? getSkin().brand.primary
}

/**
 * 构造折线图 option
 *
 * ⚠️ **入参的 `series[].data` 必须与 `dates` 等长** ——
 *   后端 `fillTrendGaps` 已保证每个序列都是"每天一个点、缺的天补 0",
 *   不等长的话 echarts 会按最短的对齐 ⇒ 曲线**平移**而不是"少一个点"。
 */
export function buildTrendOption(input: TrendOptionInput): EChartsOption {
  const { dates, series, showSymbol } = input
  const multi = series.length > 1
  const symbol = showSymbol ?? dates.length <= 14

  return {
    tooltip: { trigger: 'axis' },
    legend: multi
      ? { show: true, bottom: 0, itemWidth: 12, itemHeight: 8, textStyle: { fontSize: 11 } }
      : { show: false },
    grid: { left: 4, right: 12, top: 16, bottom: multi ? 28 : 4, containLabel: true },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: dates.map(formatAxisLabel),
      axisLine: { lineStyle: { color: '#e8e8e8' } },
      axisTick: { show: false },
      axisLabel: { color: '#8c8c8c', fontSize: 11 },
    },
    yAxis: {
      type: 'value',
      // ⚠️ 计数类指标不该出现 0.5 个 —— 强制整数刻度
      minInterval: 1,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: '#8c8c8c', fontSize: 11 },
      splitLine: { lineStyle: { color: '#f0f0f0' } },
    },
    series: series.map((s, i) => {
      const color = seriesColor(i, s.color)
      return {
        name: s.name,
        type: 'line' as const,
        smooth: true,
        showSymbol: symbol,
        symbolSize: 6,
        lineStyle: { width: 2, color },
        itemStyle: { color },
        areaStyle: { color, opacity: 0.08 },
        data: s.data,
      }
    }),
  }
}
