/**
 * 登录热力图 option 构造(纯函数)
 *
 * ## 这一组守的是什么
 *
 *   热力图画出来了,只是**深浅档分错了**,是这一层最容易出的错:
 *   数据很平时四档阈值全是 0(整张图只剩最深一色)、
 *   分位数并列导致某一档区间为空(那一档永远画不出来)、
 *   日历范围取错(1970 起点直接撑爆布局)。
 *   这些在页面上肉眼很难发现 ⇒ option 的形状必须有单测钉住。
 *
 * ## 为什么单独测纯函数
 *
 *   `loginHeatmapOption.ts` 里只有 `import type`(类型导入,编译后被擦除)
 *   ⇒ 本文件**不加载 echarts 运行时** ⇒ 不需要 canvas 补丁
 *   (与 `trendChartOption.test.ts` 同一纪律)。
 *
 * ## 可逆探针
 *
 *   · 把 `heatmapLevels` 的 `Math.max(1, pick(0.25))` 去掉 → 「平数据兜底」红;
 *   · 把 `if (t2 <= t1) t2 = t1 + 1` 三行删掉 → 「阈值严格递增」红;
 *   · 把 `range: [dates[0] ?? '1970-01-01', …]` 改成只取 `dates[0]` → 「日历范围」红。
 */
import { describe, it, expect } from 'vitest'
import { buildHeatmapOption, heatmapLevels, HEATMAP_COLORS, HEATMAP_EMPTY_COLOR } from '../loginHeatmapOption'
import type { HeatmapPoint } from '../loginHeatmapOption'

/** echarts 的 option 类型太宽,测试里只看关心的几个字段 */
interface Opt {
  calendar?: {
    range?: [string, string]
    dayLabel?: { firstDay?: number; nameMap?: string[] }
    monthLabel?: { formatter?: (p: { M: number; nameMap: string }) => string }
  }
  visualMap?: { pieces?: Record<string, unknown>[] }
  series?: { type?: string; coordinateSystem?: string; data?: [string, number][] }[]
}

/** 造点:给日期数组 + 数值数组 */
function pts(dates: string[], values: number[]): HeatmapPoint[] {
  return dates.map((d, i) => ({ date: d, value: values[i] ?? 0 }))
}

describe('heatmapLevels 分档', () => {
  it('空数据 / 全 0:阈值兜底成严格递增的 1/2/3/4', () => {
    expect(heatmapLevels([]).thresholds).toEqual([1, 2, 3, 4])
    expect(heatmapLevels(pts(['2026-11-01'], [0])).thresholds).toEqual([1, 2, 3, 4])
  })

  it('分位数并列(数据很平)时逐档 +1,阈值必须严格递增', () => {
    // 全是 1 ⇒ 四分位全 1 ⇒ 兜底成 1/2/3/4
    const flat = heatmapLevels(pts(['2026-11-01', '2026-11-02', '2026-11-03'], [1, 1, 1]))
    const [t1, t2, t3, t4] = flat.thresholds
    expect(t1).toBeGreaterThan(0)
    expect(t2!).toBeGreaterThan(t1!)
    expect(t3!).toBeGreaterThan(t2!)
    expect(t4!).toBeGreaterThan(t3!)
  })

  it('有起伏的数据:阈值取非零值的四分位', () => {
    // 5 个非零值:pick(0.25)=下标1=2,pick(0.5)=3,pick(0.75)=4,pick(1)=5
    const levels = heatmapLevels(pts(['d1', 'd2', 'd3', 'd4', 'd5'], [1, 2, 3, 4, 5]))
    expect(levels.thresholds).toEqual([2, 3, 4, 5])
    // 颜色 = 空档灰 + 5 档红系
    expect(levels.colors[0]).toBe(HEATMAP_EMPTY_COLOR)
    expect(levels.colors.slice(1)).toEqual([...HEATMAP_COLORS])
  })
})

describe('buildHeatmapOption 形状', () => {
  const input = {
    points: pts(['2026-10-01', '2026-10-02', '2026-11-02'], [0, 1, 3]),
    monthNames: ['1月', '2月', '3月', '4月', '5月', '6月', '7月', '8月', '9月', '10月', '11月', '12月'],
    dayNames: ['日', '一', '二', '三', '四', '五', '六'],
    tooltipLabel: (date: string, count: number) => `${date} ${count}`,
  }

  it('日历范围 = [首日, 末日],数据透传,坐标系是 calendar', () => {
    const opt = buildHeatmapOption(input) as unknown as Opt
    expect(opt.calendar?.range).toEqual(['2026-10-01', '2026-11-02'])
    expect(opt.series?.[0]?.type).toBe('heatmap')
    expect(opt.series?.[0]?.coordinateSystem).toBe('calendar')
    expect(opt.series?.[0]?.data).toEqual([
      ['2026-10-01', 0],
      ['2026-10-02', 1],
      ['2026-11-02', 3],
    ])
  })

  it('空数据时日历范围回落 1970(别让 undefined 撑爆布局),序列为空', () => {
    const opt = buildHeatmapOption({ ...input, points: [] }) as unknown as Opt
    expect(opt.calendar?.range).toEqual(['1970-01-01', '1970-01-01'])
    expect(opt.series?.[0]?.data).toEqual([])
  })

  it('visualMap 分档:0 是空档灰,4 个数值档 + 1 个最深尾巴,共 6 段', () => {
    const opt = buildHeatmapOption(input) as unknown as Opt
    const pieces = opt.visualMap?.pieces ?? []
    expect(pieces).toHaveLength(6)
    // 第一段 = 0 → 中性灰(不属于红系,与"有但很少"一眼可分)
    expect(pieces[0]).toMatchObject({ value: 0, color: HEATMAP_EMPTY_COLOR })
    // 第二段起是数值档
    expect(pieces[1]).toMatchObject({ min: 1, color: HEATMAP_COLORS[0] })
  })

  it('星期行序:周一在最上(firstDay: 1),nameMap 仍按周日起头传', () => {
    const opt = buildHeatmapOption(input) as unknown as Opt
    expect(opt.calendar?.dayLabel?.firstDay).toBe(1)
    expect(opt.calendar?.dayLabel?.nameMap).toEqual(['日', '一', '二', '三', '四', '五', '六'])
  })

  it('月份标签:formatter 取参数对象的 M 字段(String(params) 会渲染成 [object Object])', () => {
    const opt = buildHeatmapOption(input) as unknown as Opt
    const fmt = opt.calendar?.monthLabel?.formatter
    expect(fmt?.({ M: 5, nameMap: '2026-05' })).toBe('5月')
    expect(fmt?.({ M: 12, nameMap: '2026-12' })).toBe('12月')
    // M 缺失时回落 nameMap 原文,别渲染成 NaN月
    expect(fmt?.({ M: undefined as unknown as number, nameMap: '2026-05' })).toBe('2026-05')
  })
})
