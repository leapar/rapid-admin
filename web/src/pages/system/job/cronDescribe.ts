/**
 * cron 表达式 → 人话 —— M120
 *
 * # 为什么要有这个
 *
 * 定时任务列表那一列一直是裸表达式(`17 4 * * *`)。运营看不懂就问,
 * 问一次要一个人答一次 —— 而"这条任务到底什么时候跑"是**列表页最该自明**的信息。
 * 旁边的「下次触发」只给**下一次**的时刻,看不出规律("每天?每周?月初?")。
 *
 * # 三条硬规则
 *
 *  1. **描述不了就闭嘴**。认不出的形态一律返回 null(调用方什么也不显示),
 *     绝不用"大概是…"去糊弄 —— 描述错了比不描述更糟:
 *     运营会照着错的描述去核对"我以为它是每天跑的"。
 *  2. **不猜 robfig 的语义**。服务端用的是 robfig/cron v3,它在
 *     **日和星期同时被限制**时走的是 **OR**(`dom || dow`),不是直觉上的 AND
 *     (见 robfig spec.go 的 dayMatches)。这种表达式的含义**不唯一**
 *     ("每月 1 号"和"每周一"两个都触发),所以直接不描述。
 *  3. **文案全在字典里**。这个文件里一个中文字都没有 —— 描述是人话,
 *     人话要能翻译。
 *
 * # 为什么把 `t` 传进来而不是返回"键 + 参数"
 *
 * 返回键 + 参数的话,渲染方要自己拼短语序列,而"星期几"这种**要翻译的名字**
 * 就得变成参数 —— 参数是字符串,没法再翻译,于是中文侧只能写死。
 * 直接传 `t` 进来:星期名先翻译成"一"/"Monday",再按语言拼成
 * "每周一、三"/"Every Monday, Wednesday"。
 *
 * 代价是这个函数不再是"只依赖入参"的纯函数 —— 但它仍然是**确定性的**
 * (同一个 t + 同一个表达式 = 同一句话),测试里传真的 tKey 就能验。
 */

/** 翻译函数 —— 与 `useI18n().t` / `tKey` 同签名 */
export type TranslateFn = (
  key: string,
  fallback?: string,
  vars?: Record<string, string | number>,
) => string

/** 一句话的描述 */
export interface CronDescription {
  /** 日期短语(空串 = 不限日期) */
  day: string
  /** 时刻短语 */
  time: string
  /** 拼好的完整描述 */
  text: string
}

/** 一段 cron 字段解析结果 */
interface Field {
  /** 该字段是否被限制(不是 `*`) */
  restricted: boolean
  /** 展开后的取值(升序去重) */
  values: number[]
}

/** 时刻短语的附带信息:`isClock` 决定"不限日期"时要不要补"每天" */
interface TimeDesc {
  text: string
  isClock: boolean
  /**
   * true = `text` 本身已经是**完整的一句话**(前面不用再拼日期短语)
   *
   * 只有"每小时的第 N 分钟"这一形态是这样 —— 它把日期范围整个写进了
   * 模板里(见 MINUTE_LIST_KEY)。
   */
  whole?: boolean
}

/** 日期范围的形态 */
type DayKind = 'all' | 'weekly' | 'monthlyDay' | 'everyNDays'

/**
 * 日期范围(不是最终文案,是"形态 + 插值参数")
 *
 * 为什么要拆成两层:同一种形态在不同句子里的**模板不一样** ——
 * "每周一" 在短句式里是 `每周{dows}`,在长句式里是
 * `每周{dows}每小时的第 {mins} 分钟各执行一次`。所以这里只保留
 * "哪种形态 + 哪些参数",具体用哪句模板由**调用点**决定。
 */
interface DayScope {
  kind: DayKind
  /** 插值参数(按 kind 不同:`dows` / `d` / `n`) */
  vars: Record<string, string | number>
}

/**
 * "每小时的第 N 分钟"长句式 —— 按日期形态挑模板
 *
 * ⚠️ **为什么是四句整句而不是"范围 + 分句"两块拼**:
 *   中文「每月每日每星期每小时的第 7 分钟各执行一次」中间**不能有空格**
 *   (「…每小时的第…」),英文 "Every hour at minutes 7 on every day of
 *   every week of every month" 又**必须有空格**。拼接规则要随语言变,
 *   那就得把分隔符也做成字典项 —— 三个可变件拼一句,不如让翻译直接
 *   看到**完整的一句话**(译者能整句调整语序,不会被拆散的片段绑死)。
 */
const MINUTE_LIST_KEY: Record<DayKind, string> = {
  all: 'minuteListAll',
  weekly: 'minuteListWeekly',
  monthlyDay: 'minuteListMonthlyDay',
  everyNDays: 'minuteListEveryNDays',
}

/** 星期缩写 → 数字(robfig 允许 SUN/MON/… 这类名字) */
const DOW_NAMES: Record<string, number> = {
  sun: 0,
  mon: 1,
  tue: 2,
  wed: 3,
  thu: 4,
  fri: 5,
  sat: 6,
}

/** 一次描述最多列几个时刻(超过就放弃描述 —— 列一长串反而没人看) */
const MAX_TIMES = 12

/** 把数字补成两位(时刻显示用) */
function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

/**
 * 取值是否构成"从下界起步的等差数列"
 *
 * `0,15,30,45` → 15;`15,45` → null(不从 0 起步,不是"每 N 分钟")
 */
function uniformStep(values: number[], min: number): number | null {
  if (values.length < 2) return null
  const first = values[0]
  const second = values[1]
  if (first === undefined || second === undefined) return null
  if (first !== min) return null
  const step = second - first
  if (step < 1) return null
  for (let i = 2; i < values.length; i++) {
    const cur = values[i]
    const prev = values[i - 1]
    if (cur === undefined || prev === undefined) return null
    if (cur - prev !== step) return null
  }
  return step
}

/**
 * 取值是否构成**走完一整轮**的等差数列
 *
 * 与 uniformStep 的区别是**必须走满一圈**:
 *   - `0,2,4,…,22`(span=24)→ 2  ⇒ "每 2 小时" ✅
 *   - `0,1,2,3`(span=24)    → null ⇒ 不能叫"每 1 小时"(它一天只跑 4 次)
 *   - `0,15,30,45`(span=60) → 15 ⇒ "每 15 分钟" ✅
 *   - `0,30`(span=60)       → 30 ⇒ "每 30 分钟" ✅
 *
 * 少了这一层检查,`0 0,1,2,3 * * *` 会被说成"每 1 小时" —— 而它一天只触发 4 次。
 * **说错的频率比说不清楚更糟。**
 */
function fullCycleStep(values: number[], min: number, span: number): number | null {
  const step = uniformStep(values, min)
  if (step === null) return null
  const last = values[values.length - 1]
  if (last === undefined || last + step !== min + span) return null
  return step
}

/** 数字或星期名 → 数字;认不出来返 null */
function toNumber(raw: string, names?: Record<string, number>): number | null {
  const s = raw.trim().toLowerCase()
  if (s === '') return null
  if (/^\d+$/.test(s)) return Number(s)
  if (names) {
    const v = names[s]
    if (v !== undefined) return v
  }
  return null
}

/**
 * 解析一段字段(星号 / 单个数字 / 区间 / 区间带步长 / 逗号列表)
 *
 * 认不出的形态一律返 null —— 宁可整个表达式都不描述,也不能描述错。
 */
function parseSegment(
  part: string,
  min: number,
  max: number,
  names?: Record<string, number>,
): number[] | null {
  const pieces = part.split('/')
  if (pieces.length > 2) return null

  const base = pieces[0] ?? ''
  let step = 1
  if (pieces.length === 2) {
    const stepText = pieces[1] ?? ''
    if (!/^\d+$/.test(stepText)) return null
    step = Number(stepText)
    if (step < 1) return null
  }

  let from: number
  let to: number
  if (base.trim() === '*') {
    from = min
    to = max
  } else if (base.includes('-')) {
    const bounds = base.split('-')
    if (bounds.length !== 2) return null
    const lo = toNumber(bounds[0] ?? '', names)
    const hi = toNumber(bounds[1] ?? '', names)
    if (lo === null || hi === null) return null
    from = lo
    to = hi
  } else {
    const v = toNumber(base, names)
    if (v === null) return null
    // `5/2` 这种 robfig 有自己的解释(5..max 步长 2),但很少有人这么写,
    // 解释错了比不解释更糟 —— 认不出来就返 null
    if (pieces.length === 2) return null
    from = v
    to = v
  }

  if (from < min || to > max || from > to) return null

  const out: number[] = []
  for (let v = from; v <= to; v += step) out.push(v)
  return out
}

/** 解析一整段字段 */
function parseField(
  raw: string,
  min: number,
  max: number,
  names?: Record<string, number>,
): Field | null {
  const text = raw.trim()
  if (text === '') return null

  const set = new Set<number>()
  for (const part of text.split(',')) {
    const seg = parseSegment(part, min, max, names)
    if (!seg) return null
    for (const v of seg) set.add(v)
  }
  if (set.size === 0) return null

  const values = [...set].sort((a, b) => a - b)
  // 覆盖整个取值范围 = 等价于 `*`(如 `0-59` / `1-31`)
  const coversAll = values.length === max - min + 1
  return { restricted: text !== '*' && !coversAll, values }
}

/** 星期字段归一:robfig 里 0 和 7 都是周日;`0-7` 其实是"每天" */
function normalizeDow(field: Field): Field {
  const values = [...new Set(field.values.map((v) => (v === 7 ? 0 : v)))].sort(
    (a, b) => a - b,
  )
  const coversAll = values.length === 7
  return { restricted: field.restricted && !coversAll, values }
}

/** 星期名(翻译) */
function weekdayName(dow: number, t: TranslateFn): string {
  switch (dow) {
    case 0:
      return t('page.job.cronDesc.dow.sun')
    case 1:
      return t('page.job.cronDesc.dow.mon')
    case 2:
      return t('page.job.cronDesc.dow.tue')
    case 3:
      return t('page.job.cronDesc.dow.wed')
    case 4:
      return t('page.job.cronDesc.dow.thu')
    case 5:
      return t('page.job.cronDesc.dow.fri')
    case 6:
      return t('page.job.cronDesc.dow.sat')
    default:
      return String(dow)
  }
}

/** 日期范围(形态 + 参数) */
function describeDay(dom: Field, dow: Field, t: TranslateFn): DayScope | null {
  // 星期受限时是"每周 X"
  if (dow.restricted) {
    if (dow.values.length === 0) return null
    const names = dow.values.map((v) => weekdayName(v, t))
    return {
      kind: 'weekly',
      vars: { dows: names.join(t('page.job.cronDesc.listSep')) },
    }
  }

  if (!dom.restricted) return { kind: 'all', vars: {} }

  // 单日 → 每月 X 日
  if (dom.values.length === 1) {
    const d = dom.values[0]
    if (d === undefined) return null
    return { kind: 'monthlyDay', vars: { d } }
  }

  // `*/N` 形态 → 每 N 天
  const step = uniformStep(dom.values, 1)
  if (step !== null) {
    return { kind: 'everyNDays', vars: { n: step } }
  }

  return null
}

/** 日期范围 → 短句式的日期短语("每周一" / "每月 1 日" / "每 5 天" / "") */
function dayPhrase(day: DayScope, t: TranslateFn): string {
  switch (day.kind) {
    case 'weekly':
      return t('page.job.cronDesc.weekly', undefined, day.vars)
    case 'monthlyDay':
      return t('page.job.cronDesc.monthlyDay', undefined, day.vars)
    case 'everyNDays':
      return t('page.job.cronDesc.everyNDays', undefined, day.vars)
    case 'all':
      return ''
    default:
      return ''
  }
}

/** 时刻短语 */
function describeTime(
  minute: Field,
  hour: Field,
  day: DayScope,
  t: TranslateFn,
): TimeDesc | null {
  // 小时不受限 → 只有"频率",没有"具体时刻"
  if (!hour.restricted) {
    if (!minute.restricted) {
      return { text: t('page.job.cronDesc.everyMinute'), isClock: false }
    }
    // `*/15` / `0,15,30,45` → 每 15 分钟
    // ⚠️ 必须走满一整轮才算(见 fullCycleStep):`0,1,2,3` 这种不是"每 1 分钟"
    const step = fullCycleStep(minute.values, 0, 60)
    if (step !== null) {
      return { text: t('page.job.cronDesc.everyMinutes', undefined, { n: step }), isClock: false }
    }
    // `0 * * * *` → 每小时
    if (minute.values.length === 1 && minute.values[0] === 0) {
      return { text: t('page.job.cronDesc.everyHour'), isClock: false }
    }
    // 其余(如 `7,17,27,37,47,57 * * * *`)→ 长句式
    //
    // ⚠️ **这里必须说清楚"范围"**,不能只说"每小时第 7 分":
    //   运营看的是"这条任务到底什么时候跑",光看"每小时第 7 分"不知道
    //   日期上有没有限制(是每天都跑,还是只每周一跑?)。所以这一形态
    //   把日期范围整个写进句子里:
    //     「每月每日每星期每小时的第 7、17、27、37、47、57 分钟各执行一次」
    //
    //   ⚠️ 分钟**不补零**("第 7 分"不是"第 07 分")—— 补零是给
    //   "03:00"这种时刻用的,列表里的分钟数本来就是裸数字。
    return {
      text: t(`page.job.cronDesc.${MINUTE_LIST_KEY[day.kind]}`, undefined, {
        ...day.vars,
        mins: minute.values.join(t('page.job.cronDesc.listSep')),
      }),
      isClock: false,
      whole: true,
    }
  }

  // `0 */2 * * *` → 每 2 小时(整点 + 走满一天的小时步长)
  if (minute.values.length === 1 && minute.values[0] === 0) {
    const hstep = fullCycleStep(hour.values, 0, 24)
    if (hstep !== null) {
      return { text: t('page.job.cronDesc.everyHours', undefined, { n: hstep }), isClock: false }
    }
  }

  // 列出具体钟点。
  //
  // ⚠️ **上限只有一个,不要再加第二道**:
  //   "分钟是 `*`"(`* 3 * * *` = 那一小时里的每一分钟)看起来需要单独挡一道,
  //   但它必然产生 60 × 小时数 ≥ 60 个时刻,MAX_TIMES 一样挡得住 ——
  //   加第二道的结果是**两个判定点做同一件事**,而"拆掉哪一道测试都不会红"
  //   (探针 P6 实测:拆掉单独那道,62 条用例全绿)。
  //   留一道就够,意图写在下面的注释里。
  const times: string[] = []
  for (const h of hour.values) {
    for (const m of minute.values) {
      times.push(`${pad2(h)}:${pad2(m)}`)
    }
  }
  if (times.length === 0 || times.length > MAX_TIMES) return null

  return {
    text: t('page.job.cronDesc.atTime', undefined, {
      time: times.join(t('page.job.cronDesc.listSep')),
    }),
    isClock: true,
  }
}

/** `@every 1h30m` 这类时长 → 短语 */
function describeDuration(dur: string, t: TranslateFn): string | null {
  const text = dur.trim().toLowerCase()
  if (text === '') return null

  const units: Array<{ value: number; unit: string }> = []
  const re = /(\d+)([hms])/g
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) {
    units.push({ value: Number(m[1]), unit: m[2] ?? '' })
  }
  if (units.length === 0) return null

  // 必须是 h → m → s 的顺序且不重复(与 Go 的 ParseDuration 一致)
  const order: Record<string, number> = { h: 0, m: 1, s: 2 }
  let last = -1
  for (const u of units) {
    const o = order[u.unit]
    if (o === undefined || o <= last) return null
    last = o
  }

  const h = units.find((u) => u.unit === 'h')
  const mi = units.find((u) => u.unit === 'm')
  const s = units.find((u) => u.unit === 's')

  if (h && mi && !s) {
    return t('page.job.cronDesc.everyHoursMinutes', undefined, { h: h.value, m: mi.value })
  }
  if (h && !mi && !s) {
    return t('page.job.cronDesc.everyHours', undefined, { n: h.value })
  }
  if (!h && mi && !s) {
    return t('page.job.cronDesc.everyMinutes', undefined, { n: mi.value })
  }
  if (!h && !mi && s) {
    return t('page.job.cronDesc.everySeconds', undefined, { n: s.value })
  }
  return null
}

/** `@daily` / `@every 1h` 这类描述符 */
function describeDescriptor(raw: string, t: TranslateFn): CronDescription | null {
  const text = raw.trim().toLowerCase()

  // @yearly / @annually 的语义是"每年 1 月 1 日 00:00",
  // 而月份受限的普通表达式我们不描述(见文件头规则 2),所以这里单独给一句
  if (text === '@yearly' || text === '@annually') {
    return assemble(
      t('page.job.cronDesc.yearly'),
      t('page.job.cronDesc.atTime', undefined, { time: '00:00' }),
      true,
      t,
    )
  }

  // 其余描述符等价于一个具体的 5 段表达式,直接复用同一套逻辑
  const equivalents: Record<string, string> = {
    '@monthly': '0 0 1 * *',
    '@weekly': '0 0 * * 0',
    '@daily': '0 0 * * *',
    '@midnight': '0 0 * * *',
    '@hourly': '0 * * * *',
  }
  const eq = equivalents[text]
  if (eq) return describeCron(eq, t)

  if (text.startsWith('@every')) {
    const dur = text.slice('@every'.length).trim()
    const phrase = describeDuration(dur, t)
    if (!phrase) return null
    return assemble('', phrase, false, t)
  }

  return null
}

/** 把日期 / 时刻两段拼成一句话 */
function assemble(day: string, time: string, isClock: boolean, t: TranslateFn): CronDescription {
  // 日期不限 + 时刻是具体钟点 → 补一句"每天"
  // (`0 3 * * *` → "每天 03:00";而 `*/10 * * * *` 补"每天"就变成"每天 每 10 分钟",
  //  反而更看不懂)
  const dayText = day === '' && isClock ? t('page.job.cronDesc.daily') : day
  return {
    day: dayText,
    time,
    text: dayText === '' ? time : `${dayText} ${time}`,
  }
}

/**
 * 把 cron 表达式翻译成人话
 *
 * @param expr cron 表达式(5 段,或 `@daily` 这类描述符)
 * @param t    翻译函数(`useI18n().t` / `tKey`)
 * @returns 描述;**认不出来时返回 null**(调用方什么也不显示)
 */
export function describeCron(expr: string, t: TranslateFn): CronDescription | null {
  const raw = (expr ?? '').trim()
  if (raw === '') return null

  if (raw.charAt(0) === '@') return describeDescriptor(raw, t)

  const parts = raw.split(/\s+/)
  if (parts.length !== 5) return null

  const minute = parseField(parts[0] ?? '', 0, 59)
  const hour = parseField(parts[1] ?? '', 0, 23)
  const dom = parseField(parts[2] ?? '', 1, 31)
  const month = parseField(parts[3] ?? '', 1, 12)
  const dowParsed = parseField(parts[4] ?? '', 0, 7, DOW_NAMES)
  if (!minute || !hour || !dom || !month || !dowParsed) return null

  // 月份受限 → 不描述(月份名要翻译,而且和 dom/dow 组合出来的语义不唯一)
  if (month.restricted) return null

  const dow = normalizeDow(dowParsed)
  // 日和星期同时受限 → robfig 是 OR 语义,含义不唯一 → 不描述
  if (dom.restricted && dow.restricted) return null

  const day = describeDay(dom, dow, t)
  if (day === null) return null

  const time = describeTime(minute, hour, day, t)
  if (!time) return null

  // 长句式自带完整句子(日期范围已经写在里面)→ 前面不再拼日期短语
  const dayText = time.whole ? '' : dayPhrase(day, t)

  return assemble(dayText, time.text, time.isClock, t)
}
