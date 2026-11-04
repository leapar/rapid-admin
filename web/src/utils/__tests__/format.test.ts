/**
 * utils/format.ts 回归网(2026-10-29 M110)
 *
 * 这一层此前**一条网都没有**,而它挂在两个活着的渲染路径上:
 *   · `components/common/SensitiveField.tsx` —— 敏感字段展示(脱敏 + 显隐切换)
 *   · `components/layout/InboxBell.tsx` —— 站内信时间
 *
 * 锁住的:
 *   ① **formatDate.relative 一调就炸**(M110 P1 真 bug)
 *      此前是 `dayjs(v).fromNow()`,而仓库**从没** `dayjs.extend(relativeTime)`
 *      (`grep dayjs.extend` 全仓 0 处)⇒ `fromNow` 这个方法根本不存在
 *      ⇒ `TypeError: dayjs(...).fromNow is not a function`。
 *      tsc 早就报了这条(`src/utils/format.ts(55,59) TS2339`),
 *      只是它混在几十条基线错误里没人看 —— **tsc 报了 ≠ 有人修了**。
 *      现在改用 `Intl.RelativeTimeFormat`(浏览器原生,locale 感知,零依赖)。
 *   ② **掩码形同虚设**(M110 P1 真 bug)
 *      固定切法(3+4 / 6+4 / 4+4)遇到比标准长度短的输入时**一个字符都藏不住**:
 *      `mask.phone('1234567')` → `123****4567` —— 看着打了码,原值一字不差。
 *      现在短输入按比例缩小保留位数,保证中段一定盖住东西。
 *   ③ locale 感知:数字千分位 / 货币 / 相对时间随语言变。
 *
 * 反向探针(见 scripts/probe_m110.mjs):把每一处修复拆掉,对应用例必须变红。
 */
import { describe, it, expect, afterEach } from 'vitest'
import { useUIStore } from '@/stores/ui'
import {
  formatDate,
  formatNumber,
  mask,
  truncate,
  getCurrentLang,
  toIntlLocale,
} from '@/utils/format'

const MIN = 60 * 1000
const HOUR = 60 * MIN
const DAY = 24 * HOUR

afterEach(() => {
  useUIStore.setState({ lang: 'zh-CN' })
})

describe('format · 语言桥接', () => {
  it('默认中文,store 改成 en-US 后跟着变', () => {
    expect(getCurrentLang()).toBe('zh-CN')
    expect(toIntlLocale()).toBe('zh-CN')
    useUIStore.setState({ lang: 'en-US' })
    expect(getCurrentLang()).toBe('en-US')
    expect(toIntlLocale()).toBe('en-US')
  })
})

describe('formatDate.relative(M110 P1 回归)', () => {
  it('**不抛异常**(此前 dayjs().fromNow is not a function)', () => {
    expect(() => formatDate.relative(Date.now() - 3 * MIN)).not.toThrow()
    expect(() => formatDate.relative(Date.now())).not.toThrow()
  })

  it('空值 / 非法值 → "-"', () => {
    expect(formatDate.relative()).toBe('-')
    expect(formatDate.relative('')).toBe('-')
    expect(formatDate.relative('not-a-date')).toBe('-')
    expect(formatDate.relative(Number.NaN)).toBe('-')
  })

  it('过去用"前",将来用"后"', () => {
    expect(formatDate.relative(Date.now() - 3 * MIN)).toBe('3分钟前')
    expect(formatDate.relative(Date.now() + 2 * HOUR)).toBe('2小时后')
  })

  it('单位选得对(分钟 / 小时 / 天 / 月 / 年)', () => {
    expect(formatDate.relative(Date.now() - 30 * MIN)).toContain('分钟前')
    expect(formatDate.relative(Date.now() - 5 * HOUR)).toContain('小时前')
    expect(formatDate.relative(Date.now() - 3 * DAY)).toBe('3天前')
    expect(formatDate.relative(Date.now() - 60 * DAY)).toContain('个月前')
    // numeric:'auto' 下 -1 年是"去年"、-2 年才是"2年前"
    expect(formatDate.relative(Date.now() - 400 * DAY)).toBe('去年')
    expect(formatDate.relative(Date.now() - 800 * DAY)).toBe('2年前')
  })

  it('不足 1 秒 → "现在"', () => {
    expect(formatDate.relative(Date.now())).toBe('现在')
  })

  it('locale 感知:英文是 "3 minutes ago"', () => {
    useUIStore.setState({ lang: 'en-US' })
    expect(formatDate.relative(Date.now() - 3 * MIN)).toBe('3 minutes ago')
    expect(formatDate.relative(Date.now() - 3 * DAY)).toBe('3 days ago')
  })
})

describe('formatDate · 绝对时间', () => {
  const d = new Date(2026, 0, 2, 3, 4, 5)

  it('datetime / date / time', () => {
    expect(formatDate.datetime(d)).toBe('2026-01-02 03:04:05')
    expect(formatDate.date(d)).toBe('2026-01-02')
    expect(formatDate.time(d)).toBe('03:04:05')
  })

  it('空值 → "-"', () => {
    expect(formatDate.datetime()).toBe('-')
    expect(formatDate.date()).toBe('-')
    expect(formatDate.time()).toBe('-')
  })

  it('timeLocalized / weekday / month / longDate 都有值', () => {
    expect(formatDate.timeLocalized(d)).toMatch(/\d{2}:\d{2}:\d{2}/)
    expect(formatDate.weekday(d)).toBe('星期五')
    expect(formatDate.month(d)).toBe('一月')
    expect(formatDate.longDate(d)).toBe('2026年1月2日')
  })

  it('空值 → "-"', () => {
    expect(formatDate.timeLocalized()).toBe('-')
    expect(formatDate.weekday()).toBe('-')
    expect(formatDate.month()).toBe('-')
    expect(formatDate.longDate()).toBe('-')
  })
})

describe('formatNumber', () => {
  it('千分位', () => {
    expect(formatNumber.thousands(1234567)).toBe('1,234,567')
    expect(formatNumber.thousands(0)).toBe('0')
  })

  it('货币', () => {
    expect(formatNumber.currency(1234.5)).toBe('¥1,234.50')
    expect(formatNumber.currency(10, 'USD')).toContain('10')
  })

  it('百分比', () => {
    expect(formatNumber.percent(0.1234)).toBe('12.34%')
    expect(formatNumber.percent(0.5, 0)).toBe('50%')
  })

  it('文件大小', () => {
    expect(formatNumber.fileSize(512)).toBe('512 B')
    expect(formatNumber.fileSize(1024)).toBe('1.0 KB')
    expect(formatNumber.fileSize(1024 * 1024)).toBe('1.0 MB')
    expect(formatNumber.fileSize(1024 * 1024 * 1024)).toBe('1.00 GB')
  })

  it('持续时间', () => {
    expect(formatNumber.duration(999)).toBe('999ms')
    expect(formatNumber.duration(1500)).toBe('1.50s')
  })

  it('空值 → "-"', () => {
    expect(formatNumber.thousands()).toBe('-')
    // null 在运行时也会走到 `n == null` 分支(JS 调用方可能传 null)
    expect(formatNumber.thousands(null as unknown as number)).toBe('-')
    expect(formatNumber.currency()).toBe('-')
    expect(formatNumber.percent()).toBe('-')
    expect(formatNumber.fileSize()).toBe('-')
    expect(formatNumber.duration()).toBe('-')
  })
})

/**
 * `formatNumber.mediaDuration` —— 音视频时长(M141)
 *
 * ## 为什么单独一节
 *
 * 这个函数的输入是**后端的 `duration_ms`**,而那个 0 有**三种完全不同的含义**:
 *
 * | duration_ms | 含义 | 该显示什么 |
 * |---|---|---|
 * | > 0 | 探到了 | "03:25" |
 * | 0 | 不是媒体 / 没装 ffmpeg / 探测失败 | "-" |
 *
 * ⚠️ **0 不是"零秒"** —— 显示成 "00:00" 会让用户以为文件是空的,
 *   而实际上后端只是**不知道**。所以这一节的边界值比正面值更重要。
 */
describe('formatNumber.mediaDuration(M141)', () => {
  it('不到一小时:mm:ss(秒补零,分钟不补零)', () => {
    expect(formatNumber.mediaDuration(0)).toBe('-')
    expect(formatNumber.mediaDuration(1000)).toBe('0:01')
    expect(formatNumber.mediaDuration(59_000)).toBe('0:59')
    expect(formatNumber.mediaDuration(60_000)).toBe('1:00')
    expect(formatNumber.mediaDuration(205_000)).toBe('3:25')
    // ⚠️ **不到一秒的零头被截掉** —— 59.999s 显示 "0:59"(不是 "1:00",
    //   也不是 "0:60")
    expect(formatNumber.mediaDuration(59_999)).toBe('0:59')
    expect(formatNumber.mediaDuration(60_000)).toBe('1:00')
  })

  it('超过一小时:h:mm:ss', () => {
    expect(formatNumber.mediaDuration(3_723_000)).toBe('1:02:03')
    expect(formatNumber.mediaDuration(3_600_000)).toBe('1:00:00')
  })

  it('后端探不到时(0)→ "-",不是 "00:00"', () => {
    expect(formatNumber.mediaDuration(0)).toBe('-')
    expect(formatNumber.mediaDuration(-1)).toBe('-')
    expect(formatNumber.mediaDuration(undefined)).toBe('-')
    // JS 调用方可能传 null(后端某天改成 omitempty 就会出现)
    expect(formatNumber.mediaDuration(null as unknown as number)).toBe('-')
  })

  it('非法值(NaN / Infinity)→ "-",不能渲染成 "NaN:NaN"', () => {
    expect(formatNumber.mediaDuration(Number.NaN)).toBe('-')
    expect(formatNumber.mediaDuration(Number.POSITIVE_INFINITY)).toBe('-')
  })
})

describe('mask(M110 P1 回归:假掩码)', () => {
  /** 掩码之后还剩几个字符露在外面 */
  function leaked(masked: string): number {
    return masked.replace(/[^A-Za-z0-9]/g, '').length
  }

  it('标准长度:手机号 138****1234', () => {
    expect(mask.phone('13812341234')).toBe('138****1234')
  })

  it('标准长度:身份证 110101********1234', () => {
    expect(mask.idCard('110101199001011234')).toBe('110101********1234')
  })

  it('标准长度:银行卡 6222 **** **** 7890', () => {
    expect(mask.bankCard('6222021234567890')).toBe('6222 **** **** 7890')
  })

  it('**短输入也必须真的藏住东西**(此前一个字符都藏不住)', () => {
    // 回归:mask.phone('1234567') 此前是 '123****4567' —— 看着打了码,其实没藏
    const cases: Array<[string, string]> = [
      ['1234567', mask.phone('1234567')],
      ['12345678', mask.phone('12345678')],
      ['1234567890', mask.phone('1234567890')],
      ['1234567890', mask.idCard('1234567890')],
      ['12345678', mask.bankCard('12345678')],
      ['123456789', mask.bankCard('123456789')],
    ]
    for (const [src, out] of cases) {
      expect(leaked(out), `src=${src} out=${out}`).toBeLessThan(src.length)
    }
  })

  it('掩码后不能等于原值', () => {
    expect(mask.phone('1234567')).not.toBe('1234567')
    expect(mask.idCard('1234567890')).not.toBe('1234567890')
    expect(mask.bankCard('12345678')).not.toBe('12345678')
  })

  it('短到不是号码(长度不足阈值)→ 原样返回', () => {
    expect(mask.phone('12345')).toBe('12345')
    expect(mask.idCard('123456789')).toBe('123456789')
    expect(mask.bankCard('1234567')).toBe('1234567')
  })

  it('姓名:1 / 2 / 3+ 位', () => {
    expect(mask.name('张')).toBe('张')
    expect(mask.name('张三')).toBe('张*')
    expect(mask.name('张三丰')).toBe('张**')
  })

  it('邮箱:只露首字母 + 完整域名', () => {
    expect(mask.email('zhangsan@example.com')).toBe('z***@example.com')
    expect(mask.email('a@b.cn')).toBe('a***@b.cn')
  })

  it('没有 @ 的不按邮箱处理', () => {
    expect(mask.email('notanemail')).toBe('notanemail')
  })

  it('空值 → "-"', () => {
    expect(mask.phone()).toBe('-')
    expect(mask.idCard()).toBe('-')
    expect(mask.bankCard()).toBe('-')
    expect(mask.name()).toBe('-')
    expect(mask.email()).toBe('-')
  })
})

describe('truncate', () => {
  it('超长 → 截断 + 省略号', () => {
    expect(truncate('abcdef', 3)).toBe('abc...')
  })

  it('不超长 → 原样', () => {
    expect(truncate('abc', 3)).toBe('abc')
    expect(truncate('ab', 30)).toBe('ab')
  })

  it('空值 → "-"', () => {
    expect(truncate()).toBe('-')
    expect(truncate('')).toBe('-')
  })
})
