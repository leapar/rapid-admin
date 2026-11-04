/**
 * 跨标签页 token 同步协议(2026-10-25 M8)—— 纯协议测试
 *
 * ⚠️ 这里**不碰 store、不碰 axios**:协议本身是纯状态机,依赖全部注入
 *   (见 TokenSyncOptions)。这样测出来的失败才真的指向协议,而不是环境。
 *
 * ⚠️ **真广播通道在测试里用不了**:jsdom 至今没实现 BroadcastChannel,
 *   Node 环境里全局那个是 worker_threads 的实现(派发 Node 的 MessageEvent
 *   会跟 jsdom 的 Event 打架,还会吊住事件循环)。所以
 *   createBroadcastTokenChannel() 在测试里必然返回 null。
 *   于是这里用假总线(FakeBus)覆盖"有通道"的协议行为 —— 这正是把通道
 *   抽成接口的理由。
 */
import { describe, it, expect, vi } from 'vitest'
import { TokenSync, createBroadcastTokenChannel } from '@/utils/tokenSync'
import type { TokenChannel, TokenMessage, TokenPayload, TokenSyncOptions } from '@/utils/tokenSync'

/** 固定"现在" —— 协议里所有过期判断都基于它 */
const NOW = 1_700_000_000_000

/**
 * 假的跨标签页总线 —— 语义对齐真 BroadcastChannel:
 *   · 同源多通道
 *   · 消息**不**回送给发送方(真 BroadcastChannel 就是这样)
 */
class FakeBus {
  private peers: Array<{ deliver: (m: TokenMessage) => void }> = []

  create(): TokenChannel {
    const handlers = new Set<(m: TokenMessage) => void>()
    const peer = {
      deliver: (m: TokenMessage) => {
        for (const h of [...handlers]) h(m)
      },
    }
    this.peers.push(peer)
    return {
      post: (message) => {
        for (const p of [...this.peers]) {
          if (p !== peer) p.deliver(message)
        }
      },
      subscribe: (handler) => {
        handlers.add(handler)
        return () => {
          handlers.delete(handler)
        }
      },
      close: () => {
        this.peers = this.peers.filter((p) => p !== peer)
        handlers.clear()
      },
    }
  }
}

function payload(over: Partial<TokenPayload> = {}): TokenPayload {
  return {
    userId: 'u1',
    accessToken: 'A2',
    refreshToken: 'R2',
    expiresAt: NOW + 3600_000,
    expiresIn: 3600,
    refreshExpiresIn: 7200,
    ...over,
  }
}

function makeSync(
  channel: TokenChannel | null,
  over: Partial<TokenSyncOptions> = {},
): { sync: TokenSync; onTokens: ReturnType<typeof vi.fn> } {
  const onTokens = vi.fn()
  const options: TokenSyncOptions = {
    getUserId: () => 'u1',
    getLocalRefreshToken: () => 'R1',
    getLocal: () => payload({ accessToken: 'A1', refreshToken: 'R1' }),
    onTokens,
    now: () => NOW,
    ...over,
  }
  return { sync: new TokenSync(channel, options), onTokens }
}

/**
 * 造一个"兄弟标签页"挂到总线上
 *
 * ⚠️ **不返回引用**:总线自己持有它(peer 在 peers 里、订阅 handler 在通道的
 *   handlers 集里)—— 局部变量留在这里只会是个没人用的变量。
 */
function mountSibling(bus: FakeBus, options: Partial<TokenSyncOptions> = {}): void {
  new TokenSync(bus.create(), {
    getUserId: () => 'u1',
    getLocalRefreshToken: () => 'R2',
    getLocal: () => payload(),
    onTokens: vi.fn(),
    ...options,
  })
}

describe('TokenSync · 主动同步(收到广播)', () => {
  it('同一个人发来的新 token 组会被采用', () => {
    const bus = new FakeBus()
    const { onTokens } = makeSync(bus.create())
    const sibling = new TokenSync(bus.create(), {
      getUserId: () => 'u1',
      getLocalRefreshToken: () => 'R2',
      getLocal: () => payload(),
      onTokens: vi.fn(),
    })

    sibling.publish(payload())

    expect(onTokens).toHaveBeenCalledTimes(1)
    expect(onTokens.mock.calls[0]?.[0]).toMatchObject({ userId: 'u1', refreshToken: 'R2' })
  })

  it('**别人**的 token 组不会被采用(防会话串号)', () => {
    const bus = new FakeBus()
    const { onTokens } = makeSync(bus.create())
    const sibling = new TokenSync(bus.create(), {
      getUserId: () => 'u2',
      getLocalRefreshToken: () => 'R2',
      getLocal: () => payload(),
      onTokens: vi.fn(),
    })

    sibling.publish(payload({ userId: 'u2' }))

    expect(onTokens).not.toHaveBeenCalled()
  })

  it('access token 已过期的那组不会被采用', () => {
    const bus = new FakeBus()
    const { onTokens } = makeSync(bus.create())
    const sibling = new TokenSync(bus.create(), {
      getUserId: () => 'u1',
      getLocalRefreshToken: () => 'R2',
      getLocal: () => payload(),
      onTokens: vi.fn(),
    })

    sibling.publish(payload({ expiresAt: NOW - 1 }))

    expect(onTokens).not.toHaveBeenCalled()
  })

  it('跟本地一模一样的 refresh token 不会被采用(不跟自己乒乓)', () => {
    const bus = new FakeBus()
    const { onTokens } = makeSync(bus.create())
    const sibling = new TokenSync(bus.create(), {
      getUserId: () => 'u1',
      getLocalRefreshToken: () => 'R1',
      getLocal: () => payload({ accessToken: 'A1', refreshToken: 'R1' }),
      onTokens: vi.fn(),
    })

    sibling.publish(payload({ accessToken: 'A1', refreshToken: 'R1' }))

    expect(onTokens).not.toHaveBeenCalled()
  })

  it('缺 token 的载荷不会被采用', () => {
    const bus = new FakeBus()
    const { onTokens } = makeSync(bus.create())
    const sibling = new TokenSync(bus.create(), {
      getUserId: () => 'u1',
      getLocalRefreshToken: () => 'R2',
      getLocal: () => payload(),
      onTokens: vi.fn(),
    })

    sibling.publish(payload({ accessToken: '' }))

    expect(onTokens).not.toHaveBeenCalled()
  })
})

describe('TokenSync · 求助(刷新被拒后向兄弟要一组)', () => {
  it('兄弟回应的那组会被认领', async () => {
    const bus = new FakeBus()
    const { sync } = makeSync(bus.create())
    // 兄弟标签页:手里是更新的一组(R2)
    mountSibling(bus)

    const got = await sync.request('R1', 500)

    expect(got).not.toBeNull()
    expect(got?.refreshToken).toBe('R2')
  })

  it('回应带的是"刚失败的那个 token" → 不认,等到超时返回 null', async () => {
    const bus = new FakeBus()
    const { sync } = makeSync(bus.create())
    // 兄弟还没刷新成功,手里仍是刚被消费掉的那个 R1
    mountSibling(bus, {
      getLocalRefreshToken: () => 'R1',
      getLocal: () => payload({ accessToken: 'A1', refreshToken: 'R1' }),
    })

    const got = await sync.request('R1', 30)

    expect(got).toBeNull()
  })

  it('别的用户回应 → 不认', async () => {
    const bus = new FakeBus()
    const { sync } = makeSync(bus.create())
    mountSibling(bus, {
      getUserId: () => 'u2',
      getLocal: () => payload({ userId: 'u2' }),
    })

    const got = await sync.request('R1', 30)

    expect(got).toBeNull()
  })

  it('没人回应 → 超时返回 null', async () => {
    const bus = new FakeBus()
    const { sync } = makeSync(bus.create())

    const got = await sync.request('R1', 30)

    expect(got).toBeNull()
  })
})

describe('TokenSync · 降级', () => {
  it('没有通道时 publish 不抛错', () => {
    const { sync, onTokens } = makeSync(null)
    expect(sync.available).toBe(false)
    expect(() => sync.publish(payload())).not.toThrow()
    expect(onTokens).not.toHaveBeenCalled()
  })

  it('没有通道时 request 立刻返回 null(不等超时)', async () => {
    const { sync } = makeSync(null)
    const started = Date.now()
    const got = await sync.request('R1', 5000)
    expect(got).toBeNull()
    expect(Date.now() - started).toBeLessThan(100)
  })

  it('没登录(用户 id 为空)时不发求助', async () => {
    const post = vi.fn()
    const channel: TokenChannel = {
      post,
      subscribe: () => () => {},
      close: () => {},
    }
    const { sync } = makeSync(channel, { getUserId: () => null })

    const got = await sync.request('R1', 30)

    expect(got).toBeNull()
    expect(post).not.toHaveBeenCalled()
  })

  it('生产通道工厂:Node 环境(测试)返回 null —— 不用 Node 那版 BroadcastChannel', () => {
    /**
     * ⚠️ jsdom 至今没实现 BroadcastChannel,所以在 Node 环境里全局那个
     *   是 worker_threads 的实现:派发 Node 的 MessageEvent 会跟 jsdom 的
     *   Event 打架("must be an instance of Event"),还会吊住事件循环。
     *   工厂必须识别出来并降级 —— 这条断言锁住"降级而不是用坏实现"。
     */
    expect(typeof BroadcastChannel).toBe('function')
    expect(createBroadcastTokenChannel()).toBeNull()
  })
})

describe('TokenSync · 生命周期', () => {
  it('dispose 之后 pending 的求助会被唤醒(返回 null),不吊死等待者', async () => {
    const bus = new FakeBus()
    const { sync } = makeSync(bus.create())

    const pending = sync.request('R1', 10_000)
    sync.dispose()

    await expect(pending).resolves.toBeNull()
  })
})
