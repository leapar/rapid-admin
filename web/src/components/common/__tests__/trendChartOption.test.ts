/**
 * 趋势图 option 构造(纯函数)
 *
 * ## 这一组守的是什么
 *
 *   折线图画出来了,只是**数字对不上**,是这一层最容易出的错:
 *   X 轴日期错位一天、序列接错字段、颜色全一样、图例挡住图。
 *   这些在页面上肉眼很难发现 ⇒ option 的形状必须有单测钉住。
 *
 * ## 为什么单独测纯函数
 *
 *   `trendChartOption.ts` 里只有 `import type`(类型导入,编译后被擦除)
 *   ⇒ 本文件**不加载 echarts 运行时** ⇒ 不需要 canvas 补丁,
 *   跑得快也跑得稳。
 *
 * ## 可逆探针
 *
 *   · 把 `formatAxisLabel` 的 `slice(5)` 改成 `slice(0)` → 「X 轴只留月日」红;
 *   · 把 `showSymbol` 的阈值 14 改成 999 → 「点数多时不画数据点」红。
 */
import { describe, it, expect } from 'vitest'
import { buildTrendOption, formatAxisLabel, seriesColor } from '../trendChartOption'
import { getSkin } from '@/theme/registry'

/** echarts 的 option 类型太宽,测试里只看关心的几个字段 */
interface Opt {
  xAxis?: { data?: string[] }
  yAxis?: { minInterval?: number }
  legend?: { show?: boolean }
  series?: { name?: string; data?: number[]; showSymbol?: boolean }[]
}

describe('趋势图 option 构造', () => {
  it('X 轴标签只留月日(30 天窗口下全量日期会挤成一团)', () => {
    expect(formatAxisLabel('2026-10-30')).toBe('10-30')
    expect(formatAxisLabel('2026-01-05')).toBe('01-05')
    // 不是标准日期就原样返回(别把别的格式切坏)
    expect(formatAxisLabel('abc')).toBe('abc')
  })

  it('序列颜色:0 号 = 皮肤品牌主色,其余按色板循环', () => {
    expect(seriesColor(0)).toBe(getSkin().brand.primary)
    expect(seriesColor(1)).toBe('#52c41a')
    expect(seriesColor(4)).toBe(getSkin().brand.primary)
    // 显式指定优先
    expect(seriesColor(0, '#000000')).toBe('#000000')
  })

  it('X 轴数据与序列数据原样透传', () => {
    const opt = buildTrendOption({
      dates: ['2026-10-28', '2026-10-29'],
      series: [{ name: '业主注册趋势', data: [1, 2] }],
    }) as unknown as Opt

    expect(opt.xAxis?.data).toEqual(['10-28', '10-29'])
    expect(opt.series?.[0]?.data).toEqual([1, 2])
    expect(opt.series?.[0]?.name).toBe('业主注册趋势')
    // 计数类指标不该出现小数刻度
    expect(opt.yAxis?.minInterval).toBe(1)
  })

  it('单序列不显示图例,多序列显示图例', () => {
    const one = buildTrendOption({
      dates: ['2026-10-28'],
      series: [{ name: 'a', data: [1] }],
    }) as unknown as Opt
    expect(one.legend?.show).toBe(false)

    const two = buildTrendOption({
      dates: ['2026-10-28'],
      series: [
        { name: 'a', data: [1] },
        { name: 'b', data: [2] },
      ],
    }) as unknown as Opt
    expect(two.legend?.show).toBe(true)
    expect(two.series).toHaveLength(2)
  })

  it('点数多(>14)时不画数据点,少时画', () => {
    const many = buildTrendOption({
      dates: Array.from({ length: 30 }, (_, i) => `2026-10-${String(i + 1).padStart(2, '0')}`),
      series: [{ name: 'a', data: Array.from({ length: 30 }, () => 1) }],
    }) as unknown as Opt
    expect(many.series?.[0]?.showSymbol).toBe(false)

    const few = buildTrendOption({
      dates: ['2026-10-28', '2026-10-29'],
      series: [{ name: 'a', data: [1, 2] }],
    }) as unknown as Opt
    expect(few.series?.[0]?.showSymbol).toBe(true)

    // 显式指定优先
    const forced = buildTrendOption({
      dates: ['2026-10-28'],
      series: [{ name: 'a', data: [1] }],
      showSymbol: false,
    }) as unknown as Opt
    expect(forced.series?.[0]?.showSymbol).toBe(false)
  })
})
