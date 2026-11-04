/**
 * 本地存储工具 —— 封装 localStorage / sessionStorage
 *
 * 提供两个实例:
 *   - storage: localStorage 后端, 数据长期保留(浏览器关闭/重启都在)
 *   - session: sessionStorage 后端, 数据随标签页生命周期(关闭标签页即清空)
 *
 * 二者共用同一套 API + 同一前缀, 业务侧可以"透明切换"后端, 不用改调用代码.
 *
 * 用法上:
 *   - 勾选"记住我"  → 写 storage (记住登录)
 *   - 未勾"记住我"  → 写 session  (关闭浏览器即退出)
 *   - 读取时         → 走 activeBackend(),**不要**"两个后端都读一遍"来读 token
 *
 * 统一行为:
 *   - 统一异常处理(JSON parse 失败)
 *   - 支持过期时间(TTL, 秒)
 *   - 类型安全
 *
 * ⚠️ **本 Store 的落盘格式是信封 `{ value, expire_at }`** —— 与 zustand persist
 *   写出来的**裸值**不是一回事:
 *     · `stores/ui.ts` 的 theme、`stores/tabs.ts` 的 tabs 由 persist **直写**
 *       localStorage(`{"state":…,"version":0}` 这种裸值),不是信封;
 *     · 用本 Store 的 `get()` 去读它们 ⇒ `JSON.parse` 成功但取不到 `.value`
 *       ⇒ 返回 **undefined**(连 null 都不是),调用方会以为"有值但是空"。
 *   所以:**theme / lang / sidebar_collapsed / tabs 这几个 key 不许走本 Store**,
 *   它们各自有主人(zustand persist)。本 Store 只管会话键。
 */
import { STORAGE_KEYS } from '@/constants/app'

const PREFIX = 'rapid-admin:'

/**
 * STORAGE_KEYS 里的值**已经带了前缀**(如 'rapid-admin:token'),而本文件的
 * 存取方法又会拼一次 PREFIX —— 于是真实落盘的 key 一直是
 * 'rapid-admin:rapid-admin:token'(实测确认)。
 *
 * 之所以不能简单地把常量里的前缀去掉:同一份 STORAGE_KEYS 被两类消费者共用 ——
 *   - stores/ui.ts 的 theme、stores/tabs.ts 的 tabs 走 **zustand persist**,
 *     那是**直写 localStorage** 的,常量必须自带前缀,去掉就变成了裸 'theme',
 *     既丢了命名空间,又可能和同源其他应用撞 key;
 *   - 本文件这个 Store 会自己拼前缀。
 *
 * 所以改成**幂等加前缀**:已经带前缀的原样用,没带的才补。
 * 结果是两类消费者终于落在同一个命名空间下,常量里的名字也和 devtools 里
 * 看到的对得上。代价是历史上双前缀的 key 会成为孤儿 —— purgeLegacyKeys()
 * 在启动时清掉它们(不清的话那是一批永远躺在 localStorage 里的 refresh token)。
 */
function fullKey(key: string): string {
  return key.startsWith(PREFIX) ? key : PREFIX + key
}

/** 双前缀遗留 key 的前缀(见 fullKey 注释) */
const LEGACY_PREFIX = PREFIX + PREFIX

interface StorageItem<T> {
  value: T
  /** 过期时间戳(epoch ms); 缺省表示永不过期 */
  expire_at?: number
}

export interface Store {
  /**
   * 设置值
   * @param key 键(自动加前缀)
   * @param value 值
   * @param ttlSeconds 可选, 过期时间(秒)
   */
  set<T>(key: string, value: T, ttlSeconds?: number): void

  /**
   * 获取值
   * @returns 值或 null(过期或不存在返回 null)
   */
  get<T>(key: string): T | null

  /** 删除指定 key */
  remove(key: string): void

  /** 清空本实例下所有带前缀的 key(不影响另一个后端) */
  clear(): void
}

/** 通用 store 工厂 —— 接受任意符合 Storage 接口的对象 */
function createStore(backend: Storage): Store {
  return {
    set<T>(key: string, value: T, ttlSeconds?: number): void {
      try {
        const item: StorageItem<T> = {
          value,
          expire_at: ttlSeconds ? Date.now() + ttlSeconds * 1000 : undefined,
        }
        backend.setItem(fullKey(key), JSON.stringify(item))
      } catch (e) {
        console.error('[storage.set] failed', e)
      }
    },

    get<T>(key: string): T | null {
      const k = fullKey(key)
      try {
        const raw = backend.getItem(k)
        if (!raw) return null
        const item = JSON.parse(raw) as StorageItem<T>
        if (item.expire_at && Date.now() > item.expire_at) {
          backend.removeItem(k)
          return null
        }
        return item.value
      } catch (e) {
        console.error('[storage.get] failed', e)
        // ⚠️ **坏数据自愈**:解析失败说明这个 key 里躺着一份没人认领的数据
        //   (手改过 / 被别的版本写坏 / 被截断)。留着它的话:
        //     · 每次读都刷一条 error 日志;
        //     · 每次读的结果都是 null —— 调用方分不清"没这数据"和"这数据坏了"。
        //   删掉它,让它退回"没有"这个明确的状态。
        try {
          backend.removeItem(k)
        } catch {
          /* 删不掉就算了(配额/隐私模式),不能因为清理失败把读也炸掉 */
        }
        return null
      }
    },

    remove(key: string): void {
      backend.removeItem(fullKey(key))
    },

    clear(): void {
      // Storage.key(i) 的 key 已经包含前缀, 但要清的是我们前缀下的, 遍历 + 过滤
      //
      // ⚠️ **它会连 zustand persist 写的 key 一起清掉**(2026-10-28 M108):
      //   `stores/ui.ts` 的 persist key 是 'rapid-admin:theme'(里面装着
      //   theme / lang / sidebar_collapsed),`stores/tabs.ts` 是 'rapid-admin:tabs'
      //   —— 都在同一个前缀下,都会被这一段遍历删掉。
      //   它们的**值格式是裸值**(persist 直写),不是本 Store 的信封 ——
      //   所以「用本 Store 的 clear() 清会话」是**误伤**。
      //   要清会话请用 `clearSessionFrom()`(只清 SESSION_KEYS)。
      for (let i = backend.length - 1; i >= 0; i--) {
        const k = backend.key(i)
        if (k && k.startsWith(PREFIX)) {
          backend.removeItem(k)
        }
      }
    },
  }
}

/** 长期存储 —— localStorage(浏览器关掉也在) */
export const storage = createStore(localStorage)

/** 会话存储 —— sessionStorage(关掉标签页即清空) */
export const session = createStore(sessionStorage)

/* ====================== 会话后端锚点 ====================== */

/**
 * 会话键 —— 登出 / 重置时要从**两个**后端都清干净的那一组。
 *
 * 之所以要跨后端清:旧版本会在两个后端里各留一份(见下方 activeBackend 的说明),
 * 只清"当前那个"会把另一份留下来变成孤儿 token。
 *
 * ⚠️ **LAST_ACTIVE_AT 也在这一组里**(2026-10-28 M108):空闲计时的时间戳
 *   是**会话的一部分**,不是"这台电脑的全局状态"。留在外面的话它会跨会话残留:
 *   上一段会话的最后一次活动(比如 3 小时前)会替下一段会话把空闲预算花光 ——
 *   表现是"刚登录,15 秒内又被踢回登录页"。语义见 utils/idleTimer.ts。
 */
export const SESSION_KEYS = [
  STORAGE_KEYS.TOKEN,
  STORAGE_KEYS.REFRESH_TOKEN,
  STORAGE_KEYS.ACCESS_TOKEN_EXPIRES_AT,
  STORAGE_KEYS.REFRESH_TOKEN_EXPIRES_AT,
  STORAGE_KEYS.CURRENT_IDENTITY,
  STORAGE_KEYS.USER,
  STORAGE_KEYS.LAST_ACTIVE_AT,
] as const

/**
 * 当前会话所在的后端 —— **整个登录态的唯一真相源**。
 *
 * 不变式:REFRESH_TOKEN 恰好存在于一个后端,它落在哪,会话就在哪。
 * 选它当锚点而不是"哪个后端有数据就往哪读",是因为 refresh token 是唯一
 * 在服务端留有权威副本的凭证(服务端按 jti 吊销它),它的位置就是用户
 * 在登录页那次选择("记住我")的落点。
 *
 * ⚠️ 历史故障:token 读取走两个后端都读一遍、localStorage 优先。于是"不勾记住我"
 * 的 sessionStorage 登录,在浏览器里还残留着上一次勾选时写的 localStorage
 * token 时,读到的是那一份 —— **勾选框的效果撑不过一次 reload**,
 * 表现成"记住我好像没实现"。
 *
 * ⚠️ 平局时(session 也有)为什么偏向 session 而不是 local —— 刻意反着来:
 * 这是**失败向更严格一侧倒**的选择。历史脏数据(两个后端都残留)最可能来自
 * "先勾了记住我登录 → 后又取消勾选登录",此时偏向 local 会让用户刚作出的
 * "不要记住我"被静默推翻;偏向 session 最多让他重新登录一次。
 * 另外本文件下面的 clearSessionFrom() 保证新写入永远只落一个后端,
 * 所以正常路径下根本不会走到平局。
 */
export function activeBackend(): Store {
  return session.get<string>(STORAGE_KEYS.REFRESH_TOKEN) !== null ? session : storage
}

/** 从两个后端都删掉 key(登出 / 重置 / 切换持久化策略时用) */
export function removeAcross(key: string): void {
  storage.remove(key)
  session.remove(key)
}

/** 从两个后端都删掉全部会话键 */
export function clearSessionFrom(): void {
  for (const k of SESSION_KEYS) removeAcross(k)
}

/**
 * 清掉双前缀遗留 key(见 fullKey 注释)。
 *
 * 必须在启动时跑一次:不跑的话,历史上以 'rapid-admin:rapid-admin:*' 落盘的
 * refresh token 会永远躺在 localStorage 里 —— 它们不再被任何代码读取,但确实
 * 还是有效的凭证,直到自然过期(最长 720h)。留着它们等于把已经"登出"的会话
 * 又续了 30 天。
 *
 * 幂等且便宜:扫一遍 key 列表,只在真有遗留项时才动手。
 */
export function purgeLegacyKeys(): void {
  for (const backend of [localStorage, sessionStorage]) {
    try {
      for (let i = backend.length - 1; i >= 0; i--) {
        const k = backend.key(i)
        if (k && k.startsWith(LEGACY_PREFIX)) backend.removeItem(k)
      }
    } catch (e) {
      console.error('[storage.purgeLegacyKeys] failed', e)
    }
  }
}

/**
 * ⚠️ **这里曾经有一个 `getAcross(key)`(两个后端都读一遍、localStorage 优先),
 *   2026-10-28 M108 删掉了。**
 *
 * 删它的理由不是"没人用",是它**有毒**:
 *   · 它正是"记住我好像没实现"那个 P0 的**历史成因** —— 拿它读 token 时,
 *     浏览器里残留的上一份 localStorage token 会把本次 sessionStorage 登录顶掉;
 *   · 它零调用方,却把"两个后端都读"这种写法留在工具箱里,下一个人拿它读 token
 *     就会把那个 P0 原样复现(而且不会有任何报错)。
 *
 * 真需要"两个后端都算对"的数据(主题 / 语言 / 侧栏折叠),那些 key 由
 * **zustand persist** 直写 localStorage,各自有主人,别经过本 Store
 * (格式不兼容,见文件头)。
 */
