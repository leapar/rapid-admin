/**
 * CronInput —— cron 表达式输入 + 下次触发预览
 * 2026-10-21 新建(M69)
 *
 * 为什么不让运营裸填表达式:
 *   cron 表达式是 5 段但有大量语法糖(@every 1h / @daily / 每 2 小时一次),
 *   写错了后端只在**下一次调度**才报,那时已经过去一天了。
 *   这里在输入时就调 GET /jobs/cron/preview 把接下来 5 次触发打出来 ——
 *   "我以为每天一次其实每分钟一次"这种事故当场就能看出来。
 *
 * 为什么不做成"常用频率下拉":
 *   下拉只能覆盖预设的几档,运营真正要的是"每月 1 号 3 点"这类组合。
 *   折中:提供一排**快捷填入**按钮(不替代自由输入)。
 *
 * 预览时区固定用服务端调度器的时区(后端 cronPreview 的注释解释了原因),
 * 所以这里不调 toLocaleString,直接显示后端返的字符串 ——
 * 前端自己再格式化一次就等于把时区又拧了一遍。
 */
import { useEffect, useMemo, useRef, useState } from 'react'
import { Input, Space, Typography, Spin } from 'antd'
import { jobApi } from '@/api/job'
import { useI18n } from '@/locales'
import { describeCron } from './cronDescribe'

export interface CronInputProps {
  value?: string
  onChange?: (v: string) => void
  disabled?: boolean
  /** 代码里的默认表达式,展示成对比提示 */
  defaultExpr?: string
}

/**
 * 快捷填入:i18n 键 → robfig/cron v3 表达式
 *
 * ⚠️ 这里存的是**键**不是文案 —— 模块顶层的常量在 import 时就求值了,
 *   直接写 t('…') 的话,语言切换之后它还是切换前那句话(而且切不动)。
 *   要在渲染时查字典,就得把映射做成键,再在组件里用 t() 查。
 */
const PRESETS: Array<{ key: string; expr: string }> = [
  { key: 'page.job.cronPreset.everyMinute', expr: '* * * * *' },
  { key: 'page.job.cronPreset.everyHour', expr: '0 * * * *' },
  { key: 'page.job.cronPreset.daily3am', expr: '0 3 * * *' },
  { key: 'page.job.cronPreset.weeklyMonday3am', expr: '0 3 * * 1' },
  { key: 'page.job.cronPreset.monthly1st3am', expr: '0 3 1 * *' },
  { key: 'page.job.cronPreset.every10Minutes', expr: '*/10 * * * *' },
]

/** 预览请求防抖间隔(ms)。太短会把后端打爆,太长没有"边打边看"的感觉 */
const DEBOUNCE_MS = 400

export function CronInput({ value, onChange, disabled, defaultExpr }: CronInputProps) {
  const { t } = useI18n()
  const expr = typeof value === 'string' ? value : ''
  const [runs, setRuns] = useState<string[]>([])
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  // 存进 ref 而不是进 deps:每次输入都新建的定时器会让 effect 重新跑一遍
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  // 只认「最后一次」的结果。慢请求后到会覆盖新输入的预览,用户看到错的时刻
  const seq = useRef(0)

  // M120:把表达式翻译成人话,直接摆在输入框底下。
  // 「接下来 5 次」只给得出**时刻**,看不出规律(每天?每周?月初?);
  // 这一句才是"这条任务到底什么时候跑"。认不出来时是 null —— 那就不显示,
  // 不用一句猜测去糊弄(描述错了比不描述更糟)。
  const desc = useMemo(() => describeCron(expr, t), [expr, t])

  useEffect(() => {
    if (timer.current) clearTimeout(timer.current)
    const e = expr.trim()

    if (!e) {
      setRuns([])
      setErr('')
      setLoading(false)
      return
    }

    const my = ++seq.current
    setLoading(true)
    timer.current = setTimeout(() => {
      jobApi
        .cronPreview(e, 5, true)
        .then((list) => {
          if (my !== seq.current) return
          setRuns(Array.isArray(list) ? list : [])
          setErr('')
        })
        .catch((e: unknown) => {
          if (my !== seq.current) return
          setRuns([])
          // 拦截器已经把后端 message 弹成 toast 了,这里内联再显示一份,
          // 是因为这个输入框的 red status 只有配上原因才读得懂
          const msg = e instanceof Error ? e.message : t('page.job.cronInvalid')
          setErr(msg || t('page.job.cronInvalid'))
        })
        .finally(() => {
          if (my === seq.current) setLoading(false)
        })
    }, DEBOUNCE_MS)

    // 卸载时清掉未触发的定时器,否则会对已卸载组件 setState
    return () => {
      if (timer.current) clearTimeout(timer.current)
    }
    // ⚠️ `t` 要在依赖里 —— 报错文案是**已经渲染成文本**存进 state 的,
    //   切语言后不重跑这一遍,红字会停在旧语言上。代价是切语言会多打一次
    //   预览接口(有防抖),换来的是文案跟着切。
  }, [expr, t])

  return (
    <div>
      <Input
        value={expr}
        disabled={disabled}
        placeholder={t('common.placeholder.cronExpr')}
        status={err ? 'error' : undefined}
        onChange={(e) => onChange?.(e.target.value)}
      />

      {desc && (
        <Typography.Text type="secondary" style={{ display: 'block', marginTop: 4, fontSize: 12 }}>
          {desc.text}
        </Typography.Text>
      )}

      {!disabled && (
        <Space size={4} wrap style={{ marginTop: 8 }}>
          {PRESETS.map((p) => (
            <a key={p.expr} onClick={() => onChange?.(p.expr)} style={{ fontSize: 12 }}>
              {t(p.key)}
            </a>
          ))}
        </Space>
      )}

      {defaultExpr && defaultExpr !== expr && (
        <Typography.Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0, fontSize: 12 }}>
          {t('page.job.defaultExprLabel')}<code>{defaultExpr}</code>
        </Typography.Paragraph>
      )}

      <div style={{ marginTop: 8, minHeight: 22 }}>
        <Spin spinning={loading} size="small">
          {err && (
            <Typography.Text type="danger" style={{ fontSize: 12 }}>
              {err}
            </Typography.Text>
          )}
          {!err && runs.length > 0 && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {t('page.job.nextRuns', undefined, { n: runs.length })}:{runs.join(' / ')}
            </Typography.Text>
          )}
        </Spin>
      </div>
    </div>
  )
}

export default CronInput