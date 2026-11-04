/**
 * 短信 scene 契约测试 —— 前端 `SmsScene` 必须与后端白名单逐字一致
 *
 * ## 这条网守的是什么
 *
 *   2026-10-28(M104)之前,前端发短信用的 scene 是:
 *
 *     · `bizType="login-sms"`       (登录页)
 *     · `bizType="forgot-password"` (找回密码页)
 *     · `bizType="switch-identity"` (身份切换弹窗)
 *     · `bizType="change-password"` (个人中心改密)
 *
 *   **这四个后端一个都不认。** 后端 `validScene()` 只认 4 个:
 *   `login` / `reset_password` / `change_phone` / `view_full_phone`
 *   (与阿里云的 4 个短信模板一一对应)。
 *
 *   于是**短信一条都没发出去过** —— 登录页点「发送验证码」必定拿到
 *   一个 400「未知 scene: login-sms」。写错不会编译报错、不会崩,
 *   只是这个功能**从来没活过**。
 *
 * ## 为什么直接解析后端源码(而不是抄一份常量)
 *
 *   抄一份 = 再造一个会漂移的副本 —— 这正是这个 bug 的成因。
 *   这里读三处后端源码:
 *     · `model/sms.go`         —— 权威定义(model.SmsScene*)
 *     · `pkg/msn/sender.go`    —— 下发层认的场景(msn.Scene*)
 *     · `service/auth_impl.go` —— **校验白名单** validScene()
 *
 *   三处必须一致,而且必须与前端一致。
 *   后端加/改 scene 时,这张网立刻变红 —— 前端必须跟。
 */
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { SmsScene, SMS_SCENES, SMS_SCENE_LABEL_KEY } from '@/constants/sms'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

/** 源码根 = `web/src`(本文件在 `src/constants/__tests__/` 下) */
const SRC_ROOT = join(__dirname, '..', '..')
/** 仓库根 = `web/src` 的上两级 */
const REPO_ROOT = join(SRC_ROOT, '..', '..')

const MODEL_SMS_GO = join(REPO_ROOT, 'server', 'internal', 'model', 'sms.go')
const MSN_SENDER_GO = join(REPO_ROOT, 'server', 'internal', 'pkg', 'msn', 'sender.go')
const AUTH_IMPL_GO = join(REPO_ROOT, 'server', 'internal', 'service', 'auth_impl.go')

/**
 * 解析 `model/sms.go` 的 scene 常量
 *
 * 形如:`SmsSceneLogin = "login"`(注意是**没有类型**的裸常量)。
 */
function parseModelScenes(go: string): string[] {
  const re = /^\s*SmsScene([A-Za-z0-9_]+)\s*=\s*"([a-z_]+)"/gm
  const out: string[] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(go)) !== null) out.push(m[2]!)
  return out
}

/**
 * 解析 `pkg/msn/sender.go` 的 scene 常量
 *
 * 形如:`SceneLogin Scene = "login"`(带 `Scene` 类型)。
 *
 * 返回 **常量名 → 值** 的映射 —— 白名单那边写的是 `string(msn.SceneLogin)`,
 * 抓到的是**常量名**不是值,得靠这张表换算。
 */
function parseMsnScenes(go: string): Map<string, string> {
  const re = /^\s*Scene([A-Za-z0-9_]+)\s+Scene\s*=\s*"([a-z_]+)"/gm
  const out = new Map<string, string>()
  let m: RegExpExecArray | null
  while ((m = re.exec(go)) !== null) out.set(m[1]!, m[2]!)
  return out
}

/**
 * 解析 `service/auth_impl.go` 的 `validScene()` 白名单
 *
 * 形如:`string(msn.SceneLogin),` —— 抓到的是**常量名**,再用 msn 的映射换成值。
 *
 * ⚠️ 必须限定在 `validScene` 函数体内 —— 同一个文件里别处也有
 * `msn.Scene*` 的引用(比如 SendSms 的注释)。
 */
function parseValidSceneWhitelist(go: string, msn: Map<string, string>): string[] {
  const start = go.indexOf('func validScene(')
  if (start < 0) throw new Error('auth_impl.go 里找不到 validScene() —— 解析器失效,这张网成了空网')
  const nextFn = go.indexOf('\nfunc ', start + 1)
  const body = go.slice(start, nextFn < 0 ? undefined : nextFn)
  const re = /string\(msn\.Scene([A-Za-z0-9_]+)\)/g
  const out: string[] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(body)) !== null) {
    const value = msn.get(m[1]!)
    if (value === undefined) {
      throw new Error(`validScene() 里的 msn.Scene${m[1]} 在 sender.go 里不存在 —— 解析器失效`)
    }
    out.push(value)
  }
  return out
}

const MODEL_SCENES = parseModelScenes(readFileSync(MODEL_SMS_GO, 'utf8'))
const MSN_SCENE_MAP = parseMsnScenes(readFileSync(MSN_SENDER_GO, 'utf8'))
const MSN_SCENES = [...MSN_SCENE_MAP.values()]
const WHITELIST = parseValidSceneWhitelist(readFileSync(AUTH_IMPL_GO, 'utf8'), MSN_SCENE_MAP)

const FRONTEND = [...SMS_SCENES]

describe('短信 scene 契约 · 前端 ↔ 后端', () => {
  it('解析器没失效(这张网不是空的)', () => {
    expect(MODEL_SCENES.length).toBeGreaterThanOrEqual(4)
    expect(MSN_SCENES.length).toBeGreaterThanOrEqual(4)
    expect(WHITELIST.length).toBeGreaterThanOrEqual(4)
    expect(FRONTEND.length).toBeGreaterThanOrEqual(4)
  })

  it('后端三处定义彼此一致(model / msn / validScene 白名单)', () => {
    expect([...MSN_SCENES].sort()).toEqual([...MODEL_SCENES].sort())
    expect([...WHITELIST].sort()).toEqual([...MODEL_SCENES].sort())
  })

  it('前端 SmsScene 与后端白名单完全一致(不多、不少、不改名)', () => {
    expect([...FRONTEND].sort()).toEqual([...MODEL_SCENES].sort())
  })

  it('前端那套"后端从来没认过"的 scene 一条都不许残留', () => {
    // 这四个是 2026-10-28 之前前端自己编的 —— 后端 validScene() 全不认
    const LEGACY = ['login-sms', 'forgot-password', 'switch-identity', 'change-password']
    for (const s of LEGACY) {
      expect(FRONTEND, `${s} 不该出现在前端 scene 里`).not.toContain(s)
      expect(MODEL_SCENES, `${s} 不该出现在后端 scene 里`).not.toContain(s)
      expect(WHITELIST, `${s} 不该出现在后端白名单里`).not.toContain(s)
    }
  })

  it('每个 scene 都有 i18n 名(提示语里不能出现内部标识)', () => {
    for (const scene of FRONTEND) {
      const key = SMS_SCENE_LABEL_KEY[scene]
      expect(key, `${scene} 没有登记场景名`).toBeTruthy()
      const sub = key!.slice('smsScene.'.length)
      expect((zhCN.smsScene as Record<string, string>)[sub], `zh 缺 ${key}`).toBeTruthy()
      expect((enUS.smsScene as Record<string, string>)[sub], `en 缺 ${key}`).toBeTruthy()
    }
  })

  it('中英文场景名两份 locale 必须对称', () => {
    expect(Object.keys(zhCN.smsScene).sort()).toEqual(Object.keys(enUS.smsScene).sort())
  })

  it('场景名不是内部标识本身(界面不能显示 scene 原值)', () => {
    for (const scene of FRONTEND) {
      const zh = (zhCN.smsScene as Record<string, string>)[
        SMS_SCENE_LABEL_KEY[scene]!.slice('smsScene.'.length)
      ]!
      expect(zh, `${scene} 的中文名不该等于 scene 原值`).not.toBe(scene)
    }
  })

  it('SmsScene.LOGIN / RESET_PASSWORD 与后端常量同名同值', () => {
    // 这两条是最常被用到的,单独钉一下(改名会同时波及登录页和找回密码页)
    expect(SmsScene.LOGIN).toBe('login')
    expect(SmsScene.RESET_PASSWORD).toBe('reset_password')
    expect(MODEL_SCENES).toContain('login')
    expect(MODEL_SCENES).toContain('reset_password')
  })
})
