/**
 * 业务错误码的 i18n 映射必须与后端逐个对齐(2026-10-26)
 *
 * ## 这条网守的是什么
 *
 *   `tError(code, fallback)` 的 `code` 是**后端响应 body 里的 `code` 字段**,
 *   也就是 `server/internal/pkg/errcode/errcode.go` 里那些常量的值。
 *   `BIZ_CODE_KEYS` 用这个码做键,查表查到就显示译文,查不到就把
 *   `fallback`(= 后端那句**中文** message)原样甩给用户。
 *
 * ## 为什么值得单独一条测试
 *
 *   2026-10-26 之前的 `BizCode` 枚举用的是**前端自己另起的一套编号**
 *   (USER_LOCKED = 20002、STATE_INVALID = 20100 …),后端发的却是
 *   20101、20203 …… 于是**每一次查表都落空**,所有业务错误都回落成后端中文。
 *
 *   而且那套编号还跟后端**撞号**(前端 20100 = STATE_INVALID,后端 20100 =
 *   LoginFailed;前端 20200 = HOUSE_NOT_EXISTS,后端 20200 = SameAuditor),
 *   两套编号塞进同一张表,谁覆盖谁都说不清。
 *
 *   这类 bug 的恶劣之处在于**完全静默**:不报错、不空白、CI 全绿 ——
 *   翻译做完了,但从来没被接上。所以只能靠网。
 *
 * ## 为什么从**后端源码**里解析,而不是在前端抄一份清单
 *
 *   抄一份 = 两份清单迟早漂移,而漂移的方向永远是"前端那份少了新码"。
 *   直接读 `errcode.go`,后端加一个码而前端忘了接 → **本条测试当场变红**,
 *   报错还会指名道姓说是哪个码。
 *
 * ## 可逆探针(拆掉实现看会不会红)
 *
 *   把 `BIZ_CODE_KEYS` 的键改回前端那套旧编号(比如 20002),
 *   或者把某个码的翻译从字典里删掉 —— 本文件里对应的用例都会变红。
 */
import { describe, it, expect, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import { tError, ensureLocale } from '@/locales'
import { BIZ_CODE_KEYS, BizCode } from '@/types/api'
import { useUIStore } from '@/stores/ui'

/** 源码根 = `web/src`(本文件在 `src/locales/__tests__/` 下) */
const SRC_ROOT = join(__dirname, '..', '..')

/** 仓库根 = `web/src` 的上两级 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')

/** 后端错误码唯一出处 */
const ERRCODE_GO = join(REPO_ROOT, 'server', 'internal', 'pkg', 'errcode', 'errcode.go')

/* ==================== 解析后端 errcode.go ==================== */

/**
 * 从 Go 源码里解析出 `const` 块里的错误码常量。
 *
 * 只认 **tab 缩进 + 标识符 = 数字** 的行 —— `defaultMsg` 那张 map 的行是
 * `Name: "文案",`(冒号),`HTTPStatus` 里的 `case code >= 40000 && …:` 也不是
 * 这个形状,所以天然不会混进来。
 */
function parseConstCodes(go: string): Map<string, number> {
  const out = new Map<string, number>()
  let inConst = false
  for (const raw of go.split(/\r?\n/)) {
    const line = raw.trimEnd()
    if (/^const\s*\($/.test(line)) {
      inConst = true
      continue
    }
    if (inConst && line.trim() === ')') {
      inConst = false
      continue
    }
    if (!inConst) continue
    const m = /^\t([A-Za-z_]\w*)\s*=\s*(\d+)\s*(?:\/\/.*)?$/.exec(line)
    if (m) out.set(m[1]!, Number(m[2]))
  }
  return out
}

/**
 * 从 Go 源码里解析出 `defaultMsg` 那张 map:常量名 → 后端默认中文文案。
 *
 * 这张 map 正是**修复前会被甩给英文用户的那句话** —— 拿它当测试样本,
 * 比在测试里手写一句假的中文更有代表性(文案改了也不会失真)。
 */
function parseDefaultMessages(go: string): Map<string, string> {
  const out = new Map<string, string>()
  let inMap = false
  for (const raw of go.split(/\r?\n/)) {
    const line = raw.trimEnd()
    if (/^var defaultMsg = map\[int\]string\{$/.test(line)) {
      inMap = true
      continue
    }
    if (inMap && line.trim() === '}') {
      inMap = false
      continue
    }
    if (!inMap) continue
    const m = /^\t([A-Za-z_]\w*):\s*"((?:[^"\\]|\\.)*)",\s*$/.exec(line)
    if (m) out.set(m[1]!, m[2]!.replace(/\\"/g, '"').replace(/\\\\/g, '\\'))
  }
  return out
}

const GO_SRC = readFileSync(ERRCODE_GO, 'utf8')
/** 常量名 → 后端码(含 Success = 0) */
const BACKEND_CODES = parseConstCodes(GO_SRC)
/** 常量名 → 后端默认中文文案 */
const BACKEND_MESSAGES = parseDefaultMessages(GO_SRC)
/** 后端所有**错误**码(排除 Success = 0) */
const BACKEND_ERROR_CODES: Array<{ name: string; code: number }> = [...BACKEND_CODES]
  .filter(([, code]) => code !== 0)
  .map(([name, code]) => ({ name, code }))

/** 把嵌套字典摊平成 "点分路径 → 值" */
function flatten(obj: unknown, prefix = ''): Map<string, string> {
  const out = new Map<string, string>()
  if (obj == null || typeof obj !== 'object') return out
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    const p = prefix ? `${prefix}.${k}` : k
    if (v != null && typeof v === 'object' && !Array.isArray(v)) {
      for (const [pk, pv] of flatten(v, p)) out.set(pk, pv)
    } else if (typeof v === 'string') {
      out.set(p, v)
    }
  }
  return out
}

const ZH = flatten(zhCN)
const EN = flatten(enUS)

/** 汉字(CJK 统一表意文字)—— 英文界面里出现它 = 中文漏出去了 */
const CJK = /[一-鿿]/

/* ==================== 用例 ==================== */

describe('业务错误码 ↔ i18n 对齐(2026-10-26)', () => {
  afterEach(() => {
    // 语言是全局状态,每个用例都还原,免得影响后面别的文件
    useUIStore.getState().setLang('zh-CN')
  })

  it('后端 errcode.go 解析得出来(这张网不是空的)', () => {
    // 解析器自己也会错 —— 正则改坏了、Go 文件挪了位置,网就成了空网,
    // 后面所有"全都有翻译"的断言都会**假绿**。所以先证明网里有东西。
    expect(BACKEND_ERROR_CODES.length).toBeGreaterThan(40)
    expect(BACKEND_CODES.get('UserLocked')).toBe(20101)
    expect(BACKEND_CODES.get('StateInvalid')).toBe(20203)
    expect(BACKEND_CODES.get('Success')).toBe(0)
    // 后端默认文案那张 map 也要解析得到(测试样本的来源)
    expect(BACKEND_MESSAGES.get('UserLocked')).toBe('账号已锁定')
  })

  it('BIZ_CODE_KEYS 的每个键都必须是后端真实的错误码(方向:前端 ⊆ 后端)', () => {
    // 框架开源版:后端可能还带着前端不映射的业务码(裁剪节奏不同步),
    // 但前端映射的每一个码都必须在后端 errcode.go 里真实存在 ——
    // 映射一个后端不会发的码 = 死键。
    const backend = new Set(BACKEND_ERROR_CODES.map((c) => c.code))
    const extra = Object.keys(BIZ_CODE_KEYS)
      .map(Number)
      .filter((code) => !backend.has(code))
    expect(extra).toEqual([])
  })

  it('BIZ_CODE_KEYS 不得有多余的键(前端自造的码一律不留)', () => {
    const backend = new Set(BACKEND_ERROR_CODES.map((c) => c.code))
    const extra = Object.keys(BIZ_CODE_KEYS)
      .map(Number)
      .filter((code) => !backend.has(code))
    expect(extra).toEqual([])
  })

  it('BizCode 枚举的值必须是后端真实的错误码(方向:前端 ⊆ 后端)', () => {
    // 框架开源版:后端可能还带着业务码,但前端枚举里的每个值
    // 都必须能在后端 errcode.go 里找到 —— 自造的码一律不留。
    const enumValues = new Set(
      Object.values(BizCode).filter((v): v is number => typeof v === 'number'),
    )
    const unknown = [...enumValues]
      .filter((code) => code !== 0 && !BACKEND_ERROR_CODES.some(({ code: c }) => c === code))
      .map((code) => `${code}`)
    expect(unknown).toEqual([])
  })

  it('每个码在 zh / en 两侧都必须有翻译', () => {
    const missing: string[] = []
    for (const { name, code } of BACKEND_ERROR_CODES) {
      const key = BIZ_CODE_KEYS[code]
      if (!key) continue // 上一条用例已经报过了
      if (!ZH.has(key)) missing.push(`zh 缺 ${key}(${name})`)
      if (!EN.has(key)) missing.push(`en 缺 ${key}(${name})`)
    }
    expect(missing).toEqual([])
  })

  it('英文环境下不许把后端那句中文甩给用户(修复前就是这样的)', async () => {
    useUIStore.getState().setLang('en-US')
    expect(useUIStore.getState().lang).toBe('en-US')
    // M173 起 en-US 是懒加载的:同步 tError 在包到达前回落 zh-CN,
    // 真实 app 里 LangRoot 会在切语言时触发 ensureLocale —— 测试里同一步
    await ensureLocale('en-US')

    // 逐码走一遍:tError 的 fallback 故意给**后端的中文原文**,
    // 修复前每次都返回这句中文(查表落空 → 回落 fallback)。
    // ⚠️ 只走前端映射了的码 —— 后端还带着的业务码(前端已裁)不在映射表里,
    //   tError 本来就会回落原文,不属于"泄漏"。
    const leaked: string[] = []
    for (const { name, code } of BACKEND_ERROR_CODES) {
      if (!BIZ_CODE_KEYS[code]) continue
      const backendMsg = BACKEND_MESSAGES.get(name) ?? '后端原文'
      const out = tError(code, backendMsg)
      if (out === backendMsg || CJK.test(out)) {
        leaked.push(`${name}(${code}) → ${out}`)
      }
    }
    expect(leaked).toEqual([])
  })

  it('抽查:后端码 20101 在英文下必须是英文文案', async () => {
    useUIStore.getState().setLang('en-US')
    await ensureLocale('en-US') // M173:en-US 懒加载,先等包到(同上)
    expect(tError(20101, '账号已锁定')).toBe(
      EN.get('bizCode.20101'),
    )
    expect(tError(20101, '账号已锁定')).not.toBe('账号已锁定')
  })

  it('旧编号(前端自己那套)不再是键 —— 防止旧 bug 悄悄回来', () => {
    // 这四个是 2026-10-26 之前前端凭空造的码,后端从来没发过:
    //   1002 IDEMPOTENT_REPLAY(后端用 HTTP 头 Idempotent-Replay 标记)
    //   2001 DOUBLE_SIGN_REQUIRED(双签工作流已下线)
    //   2002 CONFIG_NEED_REVIEW(配置中心用的是 30200 一族)
    //   3001 RATE_LIMIT(后端限流码是 42900)
    for (const ghost of [1002, 2001, 2002, 3001]) {
      expect(BIZ_CODE_KEYS[ghost]).toBeUndefined()
    }
    // 旧编号里最要命的那几个:跟后端撞号
    //   前端 STATE_INVALID = 20100 / 后端 20100 = LoginFailed
    //   前端 HOUSE_NOT_EXISTS = 20200 / 后端 20200 = SameAuditor
    //   前端 PROJECT_ARCHIVE = 20201 / 后端 20201 = SelfPromotion
    expect(BIZ_CODE_KEYS[20100]).toBe('bizCode.20100') // LoginFailed
    expect(BIZ_CODE_KEYS[20200]).toBe('bizCode.20200') // SameAuditor
    expect(BIZ_CODE_KEYS[20201]).toBe('bizCode.20201') // SelfPromotion
    expect(BIZ_CODE_KEYS[20203]).toBe('bizCode.20203') // StateInvalid
  })
})
