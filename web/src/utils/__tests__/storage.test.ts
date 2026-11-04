/**
 * utils/storage.ts 回归网(2026-10-28 M108)
 *
 * 这一层此前**一条网都没有**,而它是"记住我"能不能落地的地基:
 * 前缀怎么拼、TTL 怎么算、坏数据怎么办、会话键清没清干净、activeBackend 认谁。
 *
 * 锁住的四条:
 *   ① 键前缀**幂等**(常量自带前缀,本模块再拼一次 —— 不能拼成双前缀);
 *   ② TTL 到点读不到,且过期项会被**顺手删掉**;
 *   ③ 坏 JSON **自愈**(读不到 + 把坏 key 删掉,不是永远占着);
 *   ④ 会话键(含空闲基线 LAST_ACTIVE_AT)在登出时**两个后端都清干净**。
 *
 * 反向探针(见 scripts/probe_m108.mjs):
 *   · 把 `SESSION_KEYS` 里的 LAST_ACTIVE_AT 去掉 → 「登出清干净」用例变红;
 *   · 把 get() 的坏数据自愈删掉 → 「坏 JSON 自愈」用例变红;
 *   · 把 fullKey 改回"无条件加前缀" → 「前缀幂等」用例变红。
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  storage,
  session,
  activeBackend,
  clearSessionFrom,
  purgeLegacyKeys,
  SESSION_KEYS,
} from '@/utils/storage'
import { STORAGE_KEYS } from '@/constants/app'
import * as storageModule from '@/utils/storage'

beforeEach(() => {
  localStorage.clear()
  sessionStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('storage · 键与信封', () => {
  it('键已经带前缀时不再叠一层(幂等)', () => {
    storage.set(STORAGE_KEYS.TOKEN, 'A1')

    expect(localStorage.getItem(STORAGE_KEYS.TOKEN)).toContain('A1')
    // 双前缀是历史遗留(见 storage.ts fullKey 注释)—— 新写入不能再造一份
    expect(localStorage.getItem('rapid-admin:rapid-admin:token')).toBeNull()
  })

  it('带 TTL 的值到点读不到,并且过期项被顺手删掉', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-28T00:00:00Z'))

    storage.set('short_lived', 'v', 60)
    expect(storage.get('short_lived')).toBe('v')

    vi.setSystemTime(new Date('2026-10-28T00:01:01Z'))
    expect(storage.get('short_lived')).toBeNull()
    expect(localStorage.getItem('rapid-admin:short_lived')).toBeNull()
  })

  it('坏 JSON 读不出来,并且这个 key 被删掉(自愈,不是永远占着)', () => {
    localStorage.setItem('rapid-admin:broken', '{not-json')

    expect(storage.get('broken')).toBeNull()
    // 关键:下一次读不会再撞上同一份坏数据
    expect(localStorage.getItem('rapid-admin:broken')).toBeNull()
  })
})

describe('storage · 会话后端锚点', () => {
  it('activeBackend 跟着 refresh token 走(session 有就 session,否则 storage)', () => {
    expect(activeBackend()).toBe(storage)

    session.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    expect(activeBackend()).toBe(session)
  })

  it('登出时把两个后端的会话键都清干净(含空闲基线)', () => {
    storage.set(STORAGE_KEYS.TOKEN, 'A1')
    storage.set(STORAGE_KEYS.REFRESH_TOKEN, 'R1')
    storage.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now())
    session.set(STORAGE_KEYS.TOKEN, 'A2')
    session.set(STORAGE_KEYS.REFRESH_TOKEN, 'R2')
    session.set(STORAGE_KEYS.LAST_ACTIVE_AT, Date.now())

    clearSessionFrom()

    expect(storage.get(STORAGE_KEYS.TOKEN)).toBeNull()
    expect(storage.get(STORAGE_KEYS.REFRESH_TOKEN)).toBeNull()
    expect(storage.get(STORAGE_KEYS.LAST_ACTIVE_AT)).toBeNull()
    expect(session.get(STORAGE_KEYS.TOKEN)).toBeNull()
    expect(session.get(STORAGE_KEYS.REFRESH_TOKEN)).toBeNull()
    expect(session.get(STORAGE_KEYS.LAST_ACTIVE_AT)).toBeNull()
  })

  it('空闲基线 LAST_ACTIVE_AT 属于会话键(不留在会话外)', () => {
    // 语义:空闲计时的起点是**会话**的一部分。留在会话外 ⇒ 上一段会话的
    // 旧时间戳会替下一段会话把 30 分钟的空闲预算花光。
    expect(SESSION_KEYS).toContain(STORAGE_KEYS.LAST_ACTIVE_AT)
  })

  it('清掉双前缀的遗留 key(它们是"登出后仍有效"的孤儿 token)', () => {
    localStorage.setItem('rapid-admin:rapid-admin:refresh_token', 'orphan')

    purgeLegacyKeys()

    expect(localStorage.getItem('rapid-admin:rapid-admin:refresh_token')).toBeNull()
  })

  it('不再导出 getAcross —— 它是"记住我好像没实现"的历史成因', () => {
    // 留着它 = 把"两个后端都读一遍、localStorage 优先"这种写法留在工具箱里,
    // 下一个人拿它读 token 就会把那个 P0 原样复现(而且不会报错)。
    expect((storageModule as unknown as Record<string, unknown>).getAcross).toBeUndefined()
  })
})

describe('storage · 与 zustand persist 的边界', () => {
  it('清会话不会误伤 persist 写的 key(主题 / 语言 / 侧栏 / tabs)', () => {
    // stores/ui.ts 的 persist key 是 'rapid-admin:theme'(装着 theme/lang/sidebarCollapsed),
    // stores/tabs.ts 的是 'rapid-admin:tabs' —— 它们与本 Store **同前缀**。
    // 清会话只该动 SESSION_KEYS,不该把用户的界面偏好一起抹掉。
    localStorage.setItem(STORAGE_KEYS.THEME, '{"state":{"theme":"dark"},"version":0}')
    localStorage.setItem(STORAGE_KEYS.TABS_STATE, '{"state":{"tabs":[]},"version":0}')
    storage.set(STORAGE_KEYS.TOKEN, 'A1')

    clearSessionFrom()

    expect(storage.get(STORAGE_KEYS.TOKEN)).toBeNull()
    expect(localStorage.getItem(STORAGE_KEYS.THEME)).not.toBeNull()
    expect(localStorage.getItem(STORAGE_KEYS.TABS_STATE)).not.toBeNull()
  })

  it('store.clear() 会连 persist 的 key 一起清掉(所以它不是"清会话"的入口)', () => {
    // 这条锁住的是**已知的坑**:clear() 清的是"本后端下所有 rapid-admin: 前缀的 key",
    // 而 persist 的 theme / tabs 也在这个前缀下 ⇒ 会被一起删掉。
    // 源码里 clear() 上方有对应警告;要清会话请用 clearSessionFrom()。
    localStorage.setItem(STORAGE_KEYS.THEME, '{"state":{"theme":"dark"},"version":0}')
    storage.set(STORAGE_KEYS.TOKEN, 'A1')

    storage.clear()

    expect(localStorage.getItem(STORAGE_KEYS.THEME)).toBeNull()
    expect(storage.get(STORAGE_KEYS.TOKEN)).toBeNull()
  })
})
