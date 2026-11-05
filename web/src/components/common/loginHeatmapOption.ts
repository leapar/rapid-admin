/**
 * 登录热力图 option 构造 —— 纯函数(2026-11-03 工作台)
 *
 * 与 `trendChartOption.ts` 同一套纪律(理由看那份文件的头注释):
 *   - 造 option 的纯函数放独立 `.ts`(lint `--max-warnings 0` ⇒
 *     组件文件里不能导出非组件);
 *   - 只 `import type` echarts ⇒ 单测不需要 canvas / jsdom;
 *   - 图形是"算错了也看不出来"的东西 ⇒ 形状必须有单测钉住。
 *
 * # 配色(数据可视化纪律)
 *
 *   登录次数是**量级**(magnitude)⇒ **sequential**:单色相、浅→深,
 *   绝不用彩虹。空(0 次)用中性灰 —— 与"有但很少"必须一眼可分。
 *   五档阈值从**当期数据**的四分位算(GitHub 同款口径):
 *   淡季整体浅、旺季整体深,同一张图里"多"与"少"始终可比。
 */
import type { EChartsOption } from 'echarts'
// 空档/格线/标签色跟随皮肤中性面(黑皮肤下是深灰,不再按浅色假设画死)
import { getSkin } from '@/theme/registry'

/** 一个数据点(后端已按天补全,缺的天是 0) */
export interface HeatmapPoint {
  date: string
  value: number
}

/** 非零档颜色 —— 红/玫瑰系,浅 → 深,亮度单调递减 */
export const HEATMAP_COLORS = ['#ffe4e1', '#ffb9b3', '#ff8f86', '#f5554a', '#d3281e'] as const

/** 空档(0 次)颜色 —— 跟随皮肤中性面(不写死浅灰,黑皮肤下是深灰) */
export function heatmapEmptyColor(): string {
  return getSkin().palette.colorBorderSecondary
}

/** 热力图的档位:thresholds 是 4 个分界值,colors 是 5 个填充色(含空档) */
export interface HeatmapLevels {
  thresholds: [number, number, number, number]
  colors: string[]
}

/**
 * 从数据算分档(四分位,GitHub 同款)
 *
 * ⚠️ 非零值不足 4 个时阈值兜底成 1/2/3/4 ——
 *   否则分位数全是 0,四档全被 0 压扁,整张图只剩最深一色。
 */
export function heatmapLevels(points: HeatmapPoint[]): HeatmapLevels {
  const nonzero = points
    .map((p) => p.value)
    .filter((v) => v > 0)
    .sort((a, b) => a - b)
  const pick = (q: number): number => {
    if (nonzero.length === 0) return 0
    const idx = Math.min(nonzero.length - 1, Math.floor(q * nonzero.length))
    return nonzero[idx] ?? 0
  }
  // ⚠️ 分位数相同(数据很平)时逐档 +1 兜底,保证阈值严格递增
  //   (否则 piecewise 区间为空,那一档永远画不出来)
  const t1 = Math.max(1, pick(0.25))
  let t2 = Math.max(t1 + 1, pick(0.5))
  let t3 = Math.max(t2 + 1, pick(0.75))
  let t4 = Math.max(t3 + 1, pick(1))
  if (t2 <= t1) t2 = t1 + 1
  if (t3 <= t2) t3 = t2 + 1
  if (t4 <= t3) t4 = t3 + 1
  return {
    thresholds: [t1, t2, t3, t4],
    colors: [heatmapEmptyColor(), ...HEATMAP_COLORS],
  }
}

/** 构造 option 的入参 —— 文案全部由调用方传(本文件不认识 i18n) */
export interface HeatmapOptionInput {
  points: HeatmapPoint[]
  /** 月份名,12 个,下标 0 = 1 月(图顶部标签) */
  monthNames: string[]
  /** 星期名,7 个,**下标 0 = 周日**(echarts 的 dayLabel 约定;行序由 firstDay 控制) */
  dayNames: string[]
  /** 单元格 tooltip 文案(日期, 次数)→ 文案 */
  tooltipLabel: (date: string, count: number) => string
}

/** 构造日历热力图 option */
export function buildHeatmapOption(input: HeatmapOptionInput): EChartsOption {
  const { points, monthNames, dayNames, tooltipLabel } = input
  const levels = heatmapLevels(points)
  const dates = points.map((p) => p.date)
  const palette = getSkin().palette

  const pieces: Array<Record<string, unknown>> = [{ value: 0, color: heatmapEmptyColor(), label: '0' }]
  let lo = 1
  for (let i = 0; i < levels.thresholds.length; i++) {
    const hi = levels.thresholds[i]!
    pieces.push({ min: lo, max: hi, color: levels.colors[i + 1] })
    lo = hi + 1
  }
  // 超出最高阈值的尾巴(数据分布极端时可能存在)归入最深一色
  pieces.push({ min: lo, color: levels.colors[levels.colors.length - 1] })

  return {
    tooltip: {
      trigger: 'item',
      // echarts 的 formatter 参数类型很宽(单个/数组/…都可能)——
      // 热力图 item 触发时一定是单点,这里在函数体里收窄成我们关心的 data 字段
      formatter: (p) => {
        const data = (p as { data?: [string, number] }).data
        if (!data || data[0] === undefined || data[1] === undefined) return ''
        return tooltipLabel(data[0], data[1])
      },
    },
    // ⚠️ 图例不用 echarts 的 visualMap 图 —— 档位阈值是四分位数,
    //   打出"1-3 次"这种数字标签意义不大;组件在卡片底部画
    //   「少 ▢▢▢▢▢ 多」的定性图例(与 GitHub 同款),数字交给 tooltip。
    visualMap: { show: false, type: 'piecewise', pieces },
    calendar: {
      top: 28,
      left: 28,
      right: 8,
      bottom: 4,
      // 行序:周一在最上(与附图一致);names 仍按 echarts 的"周日开头"约定传
      // (echarts 要的是元组类型,不收窄就是 string[]/number[] ⇒ TS 报错)
      range: [dates[0] ?? '1970-01-01', dates[dates.length - 1] ?? '1970-01-01'] as [string, string],
      cellSize: [14, 14] as [number, number],
      splitLine: { show: false },
      itemStyle: {
        // 2px 表面留白 —— 相邻色块必须能分清(数据可视化:spacer 纪律)
        // ⚠️ calendar 的 itemStyle 不支持 borderRadius(echarts 类型定义如此)
        borderWidth: 2,
        borderColor: palette.colorBgContainer,
      },
      monthLabel: {
        color: palette.colorTextTertiary,
        fontSize: 11,
        // ⚠️ echarts 传的是参数对象 { nameMap, yyyy, MM, M }(直接 String() 会
        //   变成 "[object Object]")—— M 是月份数字,直接当 nameMap 的下标用
        formatter: (params) => {
          const m = Number(params.M)
          return monthNames[m - 1] ?? params.nameMap
        },
      },
      dayLabel: {
        firstDay: 1,
        color: palette.colorTextTertiary,
        fontSize: 11,
        // echarts 5 的字段是 nameMap(不是 v4 的 names),仍按"周日起头"约定传
        nameMap: dayNames,
      },
      yearLabel: { show: false },
    },
    series: [
      {
        type: 'heatmap',
        coordinateSystem: 'calendar',
        data: points.map((p) => [p.date, p.value]),
      },
    ],
  }
}
