/**
 * 审计中心通用:时间范围筛选
 *
 * # 为什么单独抽一个组件
 *
 *   三个审计页(操作日志 / 数据变更 / 登录日志)都要"查某个时间段",
 *   而后端三个接口的 `startAt` / `endAt` 都是 **Unix 秒**
 *   (`service.ListOperationLogs` 里是 `time.Unix(*q.StartAt, 0)`)。
 *
 *   三个页面各抄一遍的话,迟早有一个抄错单位(毫秒 vs 秒)——
 *   而这类错**不会报错**:传毫秒进去,后端 `time.Unix(1700000000000, 0)`
 *   得到一个 5 万年后的时间点,查询结果是**空列表**,看着像"这个时间段没有日志"。
 *
 * # 为什么受控
 *
 *   受控(值从 `query.startAt/endAt` 反推)是为了让**重置按钮真的清空**:
 *   非受控的 RangePicker 自己记着选中的日期,`reset()` 把 query 清空了、
 *   页面上的日期却还留着 ⇒ 用户以为还在按那个时间范围查,看到的却是全量数据。
 *
 *   另一个坑:antd 的 RangePicker 默认只到**当天 23:59:59**,
 *   而后端是 `created_at <= endAt` ⇒ 选"今天"时今天的记录会被漏掉。
 *   这里统一把结束时间**推到当天 23:59:59**。
 */
import { useMemo } from 'react'
import type { FC } from 'react'
import { DatePicker } from 'antd'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { useI18n } from '@/locales'

const { RangePicker } = DatePicker

export interface AuditRangeFilterProps {
  /** 起始时间(Unix 秒) */
  startAt?: number
  /** 结束时间(Unix 秒) */
  endAt?: number
  /** 变化回调(只传有值的两个键) */
  onChange: (range: { startAt?: number; endAt?: number }) => void
}

const AuditRangeFilter: FC<AuditRangeFilterProps> = ({ startAt, endAt, onChange }) => {
  const { t } = useI18n()

  const value = useMemo<[Dayjs | null, Dayjs | null] | null>(() => {
    if (!startAt && !endAt) return null
    // ⚠️ 必须写成**元组**而不是数组字面量 —— 数组字面量会被推成
    //   `(Dayjs | null)[]`,而 antd 的 `RangeValueType` 要的是
    //   `[Dayjs | null, Dayjs | null]`(长度固定 2 的元组)。
    return [startAt ? dayjs(startAt * 1000) : null, endAt ? dayjs(endAt * 1000) : null]
  }, [startAt, endAt])

  return (
    <RangePicker
      value={value}
      onChange={(dates) => {
        const [s, e] = dates ?? []
        onChange({
          // ⚠️ 结束时间推到当天 23:59:59 —— 否则选"今天"会漏掉今天的记录
          startAt: s ? s.startOf('day').unix() : undefined,
          endAt: e ? e.endOf('day').unix() : undefined,
        })
      }}
      placeholder={[t('common.placeholder.rangeStart'), t('common.placeholder.rangeEnd')]}
      style={{ width: 240 }}
    />
  )
}

export default AuditRangeFilter
