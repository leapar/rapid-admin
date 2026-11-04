/**
 * cron 表达式 → 人话 —— M120
 *
 * # 这条守的是什么
 *
 * 定时任务列表那一列一直只有裸表达式(`17 4 * * *`)。加上"翻译成人话"之后,
 * 最大的风险不是"翻译不出来",而是**翻译错了**:
 *
 *   运营看到一句"每周一",照着它去核对"我以为这条是每天跑的" —— 比不显示更糟。
 *
 * 所以这个文件里**一半的用例是"必须描述不出来"**:
 * 认不出的形态一律返回 null(界面上什么都不显示),
 * 绝不用"大概是…"去糊弄。
 *
 * # 为什么断言的是**逐字**的整句,而不是"包含某个词"
 *
 * 因为"包含"式的断言对**顺序错乱**和**漏字**都是绿的:
 *   - "每周一 03:00" 和 "03:00 每周一" 都包含"每周一"和"03:00"
 *   - "每周一 03:00" 和 "每周一 03:00"(少了空格)都能通过
 * 逐字断言把这些全变成红。
 *
 * # 为什么用真的 tKey 而不是自己拼一个翻译函数
 *
 * 自己拼 = 用例里那套插值/查找逻辑和线上那套**可能不同** ——
 * 那样测出来的是"我以为的翻译",不是"真的翻译"。
 * `tKey` 走的是生产同一条路径(读 store 的语言 → 查字典 → 插值)。
 */
import { describe, it, expect, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { tKey } from '@/locales'
import { useUIStore } from '@/stores/ui'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import { describeCron } from '../cronDescribe'

/** 切语言(UI store 是 zustand,setState 直接改内存态) */
function setLang(lang: 'zh-CN' | 'en-US') {
  useUIStore.setState({ lang })
}

/** 取描述的整句;描述不出来时返回 null */
function desc(expr: string): string | null {
  const d = describeCron(expr, tKey)
  return d ? d.text : null
}

describe('cron 表达式 → 人话', () => {
  beforeEach(() => {
    setLang('zh-CN')
  })

  describe('仓库里真实在跑的表达式', () => {
    // ⚠️ 这几条是**代码里真的注册着**的任务(见 internal/pkg/jobs/jobs.go、
    //   internal/service/approval_scan.go、internal/service/sms_template_sync.go),
    //   不是编出来的样本 —— 它们描述不出来,这一页就还是"看不懂"的状态。
    const real: Array<[string, string]> = [
      ['17 4 * * *', '每天 04:17'],
      ['23 9 * * *', '每天 09:23'],
      ['10 3 * * *', '每天 03:10'],
      // ⚠️ 这一条**不是**"每 10 分钟" —— 它跳过了每个整点的 0 分。
      //   描述成"每 10 分钟"是**错的**(运营会以为 00:07、00:17 … 也跑,
      //   实际上 00:00 不跑),所以这里必须逐字列出分位。
      //
      //   ⚠️ **整句必须把范围说全**(2026-10-30 用户纠正):
      //   表达式是「分 时 日 月 星期」五段,后面几个 `*` 依次是
      //   **每小时 / 每日 / 每月 / 每星期**,前面的数字是**第几分钟**。
      //   所以这一句要读成:
      //     「每月每日每星期每小时的第 7、17、27、37、47、57 分钟各执行一次」
      //   而不是「每小时第 07、17 分」—— 后者只说了"小时"这一层,
      //   读者不知道日期上有没有限制(每天都跑?还是只每周一跑?)。
      ['7,17,27,37,47,57 * * * *', '每月每日每星期每小时的第 7、17、27、37、47、57 分钟各执行一次'],
    ]

    it.each(real)('%s → %s', (expr, expected) => {
      expect(desc(expr)).toBe(expected)
    })
  })

  describe('快捷填入那几个预设', () => {
    const cases: Array<[string, string]> = [
      ['* * * * *', '每分钟'],
      ['0 * * * *', '每小时'],
      ['0 3 * * *', '每天 03:00'],
      ['0 3 * * 1', '每周一 03:00'],
      ['0 3 1 * *', '每月 1 日 03:00'],
      ['*/10 * * * *', '每 10 分钟'],
    ]

    it.each(cases)('%s → %s', (expr, expected) => {
      expect(desc(expr)).toBe(expected)
    })
  })

  describe('各种能描述的形态', () => {
    const cases: Array<[string, string]> = [
      // 星期列表
      ['0 3 * * 1,3', '每周一、三 03:00'],
      // 周日:robfig 里 0 和 7 都是周日
      ['0 3 * * 0', '每周日 03:00'],
      ['0 3 * * 7', '每周日 03:00'],
      // 星期名
      ['0 3 * * MON', '每周一 03:00'],
      // 分钟列表(不从 0 起步 ⇒ 不是"每 N 分钟")
      // ⚠️ 长句式:范围要**说全**(每小时/每日/每月/每星期),分钟**不补零**
      ['15,45 * * * *', '每月每日每星期每小时的第 15、45 分钟各执行一次'],
      // ⚠️ 从 0 起步但**没走满一圈** ⇒ 也不是"每 N 分钟"
      //   (`0,10,20` 只在这三个分位触发,说成"每 10 分钟"是错的)
      ['0,10,20 * * * *', '每月每日每星期每小时的第 0、10、20 分钟各执行一次'],
      // ⚠️ 分钟列表 + **日期受限** ⇒ 范围要换成受限的那一档,
      //   不许一律说成"每月每日每星期"(那是把限制说没了)
      //   ⚠️ cron 五段的顺序是「分 时 日 月 星期」——
      //     小时必须是 `*` 才是"每小时第 N 分",写成 `1` 就变成"01 点几分"了
      ['7,17 * * * 1', '每周一每小时的第 7、17 分钟各执行一次'],
      ['7,17 * 1 * *', '每月 1 日每小时的第 7、17 分钟各执行一次'],
      ['7,17 * */5 * *', '每 5 天每小时的第 7、17 分钟各执行一次'],
      // 走满一圈的同一形态 ⇒ 就是"每 10 分钟"
      ['0,10,20,30,40,50 * * * *', '每 10 分钟'],
      // 小时等差 ⇒ 每 N 小时
      ['0 */2 * * *', '每 2 小时'],
      ['0 0,12 * * *', '每 12 小时'],
      // 一天里的多个钟点
      ['30 8,20 * * *', '每天 08:30、20:30'],
      ['0 9-17 * * *', '每天 09:00、10:00、11:00、12:00、13:00、14:00、15:00、16:00、17:00'],
      // 分钟步长 + 小时受限 → 列出那一小时里的每个触发点
      ['*/15 3 * * *', '每天 03:00、03:15、03:30、03:45'],
      // 每 N 天
      ['0 3 */5 * *', '每 5 天 03:00'],
      // 小时写满 0-23 = 等价于 `*` ⇒ 每小时(不是 24 个钟点)
      ['0 0-23 * * *', '每小时'],
      // ⚠️ 这一条**不是**"每 1 小时" —— 它一天只触发 4 次(00/01/02/03 点)。
      //   说成"每 1 小时"会让运营以为全天都在跑。走满一整轮才算"每 N 小时"。
      ['0 0,1,2,3 * * *', '每天 00:00、01:00、02:00、03:00'],
    ]

    it.each(cases)('%s → %s', (expr, expected) => {
      expect(desc(expr)).toBe(expected)
    })
  })

  describe('@ 描述符', () => {
    const cases: Array<[string, string]> = [
      ['@daily', '每天 00:00'],
      ['@midnight', '每天 00:00'],
      ['@hourly', '每小时'],
      ['@weekly', '每周日 00:00'],
      ['@monthly', '每月 1 日 00:00'],
      ['@yearly', '每年 1 月 1 日 00:00'],
      ['@annually', '每年 1 月 1 日 00:00'],
      ['@every 1h', '每 1 小时'],
      ['@every 30m', '每 30 分钟'],
      ['@every 1h30m', '每 1 小时 30 分钟'],
      ['@every 45s', '每 45 秒'],
    ]

    it.each(cases)('%s → %s', (expr, expected) => {
      expect(desc(expr)).toBe(expected)
    })
  })

  describe('认不出来的一律闭嘴(返回 null,界面上一句都不显示)', () => {
    const cases: Array<[string, string]> = [
      ['', '空串'],
      ['   ', '纯空白'],
      ['not a cron', '根本不是表达式'],
      ['0 3 * *', '只有 4 段'],
      ['0 0 3 * * *', '6 段(带秒)—— 服务端的解析器也不支持'],
      ['abc * * * *', '分钟不是数字'],
      ['60 * * * *', '分钟越界'],
      ['0 3 * * 8', '星期越界'],
      // ⚠️ 这两条是**语义不唯一**,不是"不会解析" —— robfig 在日和星期
      //   同时受限时走的是 OR(每月 1 号 **或** 每周一,两个都触发),
      //   写成一句话一定会骗人
      ['0 3 1 * 1', '日和星期同时受限(OR 语义)'],
      // 月份受限 —— 月份名要翻译,且与 dom/dow 组合出来的说法不唯一
      ['0 3 1 1 *', '月份受限'],
      ['0 3 * * * *', '7 段'],
      // 时刻太多,列出来没人看
      ['*/2 1-3 * * *', '时刻组合过多'],
      // 分钟是 `*` 而小时受限 = "那一小时里的每一分钟",太啰嗦
      ['* 3 * * *', '分钟不限 + 小时受限'],
      // 时长写法不对
      ['@every 1d', '不支持的单位'],
      ['@every', '缺时长'],
      ['@bogus', '未知描述符'],
    ]

    it.each(cases)('%s(%s)描述不出来', (expr) => {
      expect(desc(expr)).toBeNull()
    })
  })

  describe('英文侧', () => {
    beforeEach(() => {
      setLang('en-US')
    })

    const cases: Array<[string, string]> = [
      ['* * * * *', 'Every minute'],
      ['0 * * * *', 'Every hour'],
      ['0 3 * * *', 'Every day at 03:00'],
      ['0 3 * * 1', 'Every Monday at 03:00'],
      ['0 3 * * 1,3', 'Every Monday, Wednesday at 03:00'],
      ['0 3 1 * *', 'Day 1 of every month at 03:00'],
      ['*/10 * * * *', 'Every 10 minutes'],
      // ⚠️ 长句式:范围说全(每个 hour / 每个 day / 每个 week / 每个 month),
      //   分钟**不补零**;英文这边语序跟中文不同 —— 分钟紧跟在 hour 后面
      ['15,45 * * * *', 'Every hour at minutes 15, 45 on every day of every week of every month'],
      ['7,17 * * * 1', 'Every hour at minutes 7, 17 on Monday'],
      ['@yearly', 'On January 1st every year at 00:00'],
      ['@every 1h30m', 'Every 1 hours 30 minutes'],
    ]

    it.each(cases)('%s → %s', (expr, expected) => {
      expect(desc(expr)).toBe(expected)
    })
  })

  describe('字典对齐', () => {
    /** 把嵌套字典摊平成 "点分路径 → 值" */
    function flatten(obj: unknown, prefix = ''): Map<string, string> {
      const out = new Map<string, string>()
      if (obj === null || obj === undefined || typeof obj !== 'object') return out
      for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
        const p = prefix ? `${prefix}.${k}` : k
        if (v !== null && v !== undefined && typeof v === 'object' && !Array.isArray(v)) {
          for (const [pk, pv] of flatten(v, p)) out.set(pk, pv)
        } else if (typeof v === 'string') {
          out.set(p, v)
        }
      }
      return out
    }

    it('描述器用到的每个键在两侧字典里都真实存在', () => {
      const src = readFileSync(join(__dirname, '..', 'cronDescribe.ts'), 'utf8')
      const keys = [...src.matchAll(/page\.job\.cronDesc\.[A-Za-z0-9_.]+/g)].map((m) => m[0])
      expect(keys.length).toBeGreaterThan(0)

      const zh = flatten(zhCN)
      const en = flatten(enUS)
      const missing: string[] = []
      for (const k of new Set(keys)) {
        if (!zh.has(k)) missing.push(`${k} —— zh-CN 没有`)
        if (!en.has(k)) missing.push(`${k} —— en-US 没有`)
      }
      expect(missing).toEqual([])
    })

    it('两侧的占位符必须一致(中文能插值、英文插不进去 = 露出 {name})', () => {
      const zh = flatten(zhCN)
      const en = flatten(enUS)
      const mismatched: string[] = []
      for (const [key, zhValue] of zh) {
        if (!key.startsWith('page.job.cronDesc')) continue
        const enValue = en.get(key)
        if (enValue === undefined) continue
        const ph = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort().join(',')
        if (ph(zhValue) !== ph(enValue)) {
          mismatched.push(`${key} · zh=${JSON.stringify(zhValue)} en=${JSON.stringify(enValue)}`)
        }
      }
      expect(mismatched).toEqual([])
    })
  })
})
