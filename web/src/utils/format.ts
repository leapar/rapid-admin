/**
 * 格式化工具 —— 时间、数字、文本、敏感数据
 *
 * M19:locale 感知
 *   - 数字 thousands/currency 用 useUIStore.lang(Intl 标签)
 *   - 绝对日期(datetime / date / time)走 dayjs;相对时间与日历日期走 **Intl**
 *     —— 与本文件其余几个格式化函数同一套机制,不依赖 dayjs 插件
 *   - 这里不直接 import useUIStore(避免循环依赖),通过 getCurrentLang() 桥接
 */
import dayjs from 'dayjs'
import { useUIStore } from '@/stores/ui'

/** 取当前语言(同步) — 业务代码直接调,无需 React hook */
export function getCurrentLang(): 'zh-CN' | 'en-US' {
  try {
    return useUIStore.getState().lang
  } catch {
    return 'zh-CN'
  }
}

/** Intl locale tag(zh-CN → zh-CN, en-US → en-US) */
export function toIntlLocale(lang: 'zh-CN' | 'en-US' = getCurrentLang()): string {
  return lang
}

/**
 * 相对时间的单位与毫秒数(从大到小,取第一个"够大"的单位)
 *
 * ⚠️ month 按 30 天算、year 按 365 天算 —— 这是 Intl.RelativeTimeFormat
 *   的通行做法(它自己也是按固定天数近似月/年),不是日历精确差值。
 */
const RELATIVE_UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ['year', 365 * 24 * 60 * 60 * 1000],
  ['month', 30 * 24 * 60 * 60 * 1000],
  ['week', 7 * 24 * 60 * 60 * 1000],
  ['day', 24 * 60 * 60 * 1000],
  ['hour', 60 * 60 * 1000],
  ['minute', 60 * 1000],
  ['second', 1000],
]

/** 用 Intl.RelativeTimeFormat 格式化相对时间(负数 = 过去,正数 = 将来) */
function formatRelative(value: number, unit: Intl.RelativeTimeFormatUnit): string {
  return new Intl.RelativeTimeFormat(toIntlLocale(), { numeric: 'auto' }).format(value, unit)
}

/** 格式化时间 */
export const formatDate = {
  /** YYYY-MM-DD HH:mm:ss */
  datetime: (v?: string | number | Date) => (v ? dayjs(v).format('YYYY-MM-DD HH:mm:ss') : '-'),
  /** YYYY-MM-DD */
  date: (v?: string | number | Date) => (v ? dayjs(v).format('YYYY-MM-DD') : '-'),
  /** HH:mm:ss */
  time: (v?: string | number | Date) => (v ? dayjs(v).format('HH:mm:ss') : '-'),
  /** M28 §2.6 补完 —— 本地化时间(11:30:45 / 11:30:45 AM) */
  timeLocalized: (v?: string | number | Date) => {
    if (!v) return '-'
    return new Intl.DateTimeFormat(toIntlLocale(), {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: getCurrentLang() === 'en-US',
    }).format(new Date(v))
  },
  /** M28 §2.6 补完 —— 星期几(周一 / Monday) */
  weekday: (v?: string | number | Date) => {
    if (!v) return '-'
    return new Intl.DateTimeFormat(toIntlLocale(), { weekday: 'long' }).format(new Date(v))
  },
  /** M28 §2.6 补完 —— 月份(十月 / October) */
  month: (v?: string | number | Date) => {
    if (!v) return '-'
    return new Intl.DateTimeFormat(toIntlLocale(), { month: 'long' }).format(new Date(v))
  },
  /**
   * 相对时间(如 "3 分钟前" / "3 minutes ago")
   *
   * ⚠️ **M110 修正**:此前这里是 `dayjs(v).fromNow()` —— 而仓库**从没**
   *   `dayjs.extend(relativeTime)`(`grep dayjs.extend` 全仓 0 处)
   *   ⇒ `fromNow` 这个方法**根本不存在** ⇒ 一调就
   *   `TypeError: dayjs(...).fromNow is not a function`。
   *   tsc 其实早就报了这条(`src/utils/format.ts(55,59) TS2339`),
   *   只是它混在 48 条基线错误里没人看 —— **tsc 报了 ≠ 有人修了**。
   *   旁证:`components/layout/InboxBell.tsx:236` 的注释明写
   *   "⚠️ 不用 formatDate.relative:仓库里从没 dayjs.extend(relativeTime)" ——
   *   作者知道有雷,绕开了,但**雷留在了导出的公共 API 上**,
   *   业务页哪天照着用就白屏。
   *
   * 现在改用 `Intl.RelativeTimeFormat`:
   *   · 浏览器原生,locale 感知,**不需要 dayjs 插件**
   *     (插件是 `export =` 的 CJS 模块,本仓 tsconfig 没开 esModuleInterop,
   *      默认导入过不了类型检查 —— 与其为了一个格式化函数改全局 tsconfig,
   *      不如用原生 API)
   *   · 与本文件其余的 timeLocalized / weekday / month / longDate 同一套机制
   */
  relative: (v?: string | number | Date) => {
    if (!v) return '-'
    const ts = new Date(v).getTime()
    if (Number.isNaN(ts)) return '-'
    const diff = ts - Date.now()
    const abs = Math.abs(diff)
    for (const [unit, ms] of RELATIVE_UNITS) {
      if (abs >= ms) {
        return formatRelative(Math.round(diff / ms), unit)
      }
    }
    // 不足 1 秒
    return formatRelative(0, 'second')
  },
  /** 本地化长日期(2026年10月19日 / October 19, 2026) */
  longDate: (v?: string | number | Date) => {
    if (!v) return '-'
    const lang = getCurrentLang()
    return new Intl.DateTimeFormat(toIntlLocale(lang), {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    }).format(new Date(v))
  },
}

/** 数字格式化 */
export const formatNumber = {
  /** 千分位 — locale 感知(12,345 / 12.345) */
  thousands: (n?: number) => {
    if (n == null) return '-'
    return n.toLocaleString(toIntlLocale())
  },
  /** 货币(默认 CNY,可传 USD 等) */
  currency: (n?: number, currency = 'CNY') => {
    if (n == null) return '-'
    return new Intl.NumberFormat(toIntlLocale(), {
      style: 'currency',
      currency,
    }).format(n)
  },
  /** 百分比 */
  percent: (n?: number, decimals = 2) => (n == null ? '-' : `${(n * 100).toFixed(decimals)}%`),
  /** 文件大小 */
  fileSize: (bytes?: number) => {
    if (bytes == null) return '-'
    if (bytes < 1024) return `${bytes} B`
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
    return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
  },
  /** 持续时间(ms -> "1.2s" / "234ms") */
  duration: (ms?: number) => {
    if (ms == null) return '-'
    if (ms < 1000) return `${ms}ms`
    return `${(ms / 1000).toFixed(2)}s`
  },
  /**
   * 媒体时长(ms -> "03:25" / "1:02:03")
   *
   * ⚠️ **跟 `duration` 是两回事** —— `duration` 是"请求耗时"(1.2s),
   *   这里要的是"这个视频有多长"(播放器上的那种写法)。
   *
   * ⚠️ **0 / 负数 / 空 ⇒ "-"** —— 后端在"不是媒体 / 没装 ffmpeg / 探测失败"
   *   三种情况下都返 0。那是**不知道**,不是"零秒",显示成 "00:00" 会让用户
   *   以为这个文件是空的。
   *
   * ⚠️ **用 `=== undefined` 而不是 `== null`** —— 本仓 eslint 的 `eqeqeq`
   *   是 error,而 `ms === null` 在 `strict: true` 下是 TS2367
   *   (类型是 `number | undefined`,跟 null 没交集)。`Number.isFinite`
   *   顺带把 NaN / Infinity 也挡掉了(渲染成 "NaN:NaN" 更糟)。
   */
  mediaDuration: (ms?: number) => {
    if (ms === undefined || !Number.isFinite(ms) || ms <= 0) return '-'
    const total = Math.floor(ms / 1000)
    const h = Math.floor(total / 3600)
    const m = Math.floor((total % 3600) / 60)
    const s = total % 60
    const pad = (n: number) => String(n).padStart(2, '0')
    return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
  },
}

/**
 * 中段掩码 —— 保留首 head 位、尾 tail 位,中间盖成 '*'
 *
 * ⚠️ **M110 修的真 bug:之前的"假掩码"**
 *   三处都是固定切法(手机号 3+4 / 身份证 6+4 / 银行卡 4+4),
 *   遇到**比标准长度短**的输入时,保留段会把整串都露出来:
 *     `mask.phone('1234567')` → `123****4567` —— **看着打了码,一个字没藏**
 *     `mask.idCard('1234567890')` → `123456********7890` —— 同样 0 位被藏
 *     `mask.bankCard('12345678')` → `1234 **** **** 5678` —— 同样 0 位被藏
 *   这三处都有**活着的调用方**(`components/common/SensitiveField.tsx`),
 *   而那个组件正是"敏感字段展示"的入口 —— 掩码形同虚设比不掩码更危险
 *   (看的人以为藏了)。
 *
 *   现在:标准切法能用就用(11 位手机号仍然是 `138****1234`);
 *   用不了(盖不住 1/3)就按比例缩小保留位数,保证中段**一定盖住东西**。
 */
function maskMiddle(s: string, head: number, tail: number): string {
  const len = s.length
  if (len <= 0) return s
  const hidden = len - head - tail
  if (hidden >= len / 3) {
    return s.slice(0, head) + '*'.repeat(hidden) + s.slice(len - tail)
  }
  // 太短:按比例保留首尾各 1/4,其余全盖
  const h = Math.floor(len / 4)
  const t = Math.floor(len / 4)
  if (h + t >= len) return '*'.repeat(len)
  return s.slice(0, h) + '*'.repeat(len - h - t) + s.slice(len - t)
}

/** 脱敏工具(显示用,真实数据不能传前端) */
export const mask = {
  /** 手机号: 138****1234 */
  phone: (s?: string) => {
    if (!s) return '-'
    if (s.length < 7) return s
    return maskMiddle(s, 3, 4)
  },
  /** 身份证: 110101********1234 */
  idCard: (s?: string) => {
    if (!s) return '-'
    if (s.length < 10) return s
    return maskMiddle(s, 6, 4)
  },
  /** 银行卡: 6222 **** **** 1234 */
  bankCard: (s?: string) => {
    if (!s) return '-'
    if (s.length < 8) return s
    if (s.length >= 16) return `${s.slice(0, 4)} **** **** ${s.slice(-4)}`
    return maskMiddle(s, 4, 4)
  },
  /** 姓名: 张* */
  name: (s?: string) => {
    if (!s) return '-'
    if (s.length === 1) return s
    if (s.length === 2) return `${s[0]}*`
    return `${s[0]}**`
  },
  /** 邮箱: a***@example.com */
  email: (s?: string) => {
    if (!s || !s.includes('@')) return s || '-'
    const [name, domain] = s.split('@')
    if (!name || !domain) return s
    return `${name[0]}***@${domain}`
  },
}

/** 文本截断 */
export const truncate = (s?: string, max = 50) => {
  if (!s) return '-'
  return s.length > max ? `${s.slice(0, max)}...` : s
}
