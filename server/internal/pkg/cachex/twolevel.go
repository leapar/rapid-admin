// Package cachex 二级缓存 L1(本地 LRU) + L2(Redis Safe)(§1.7 #7 P1)
//
// 设计目标:
//   - 热点 key 命中本地 L1,无网络 RTT(< 1µs)
//   - 冷数据走 L2 Redis(已有 Safe 防穿透/击穿/雪崩)
//   - L1 TTL < L2 TTL(L1 是"瞬时窗口",L2 是"持久窗口")
//   - L1 写时一并写 L2;L1 失效时由 L2 兜底
//   - singleflight 跨 L1+L2:同 key 全集群只放一个 loader goroutine
//
// 与 Safe 关系:
//   - Safe 是 L2(Redis)单层缓存,所有三防都在里面
//   - TwoLevel 在 Safe 外面再包一层 L1,提供"本地直查"加速
//   - TwoLevel.GetOrLoad 走 L1 → L2 → loader,语义与 Safe 兼容
//
// 何时用 TwoLevel:
//   - 热点配置 / 字典 / 权限 / 用户信息 — 几百 QPS+,本地化收益大
//   - 内存允许 1 万~10 万 key(LRU 自动淘汰)
//
// 何时用 Safe 单层:
//   - 多实例一致性要求高(任何实例改了立即全网生效)— L1 有 10s 延迟
//   - 内存紧张(无 L1 预算)
//
// 失效语义:
//   - Set(key, val, ttl):写 L1 + L2,L1 用 min(ttl, l1TTL),L2 用 ttl
//   - Delete(key):L1 + L2 一起删
//   - L1 过期:命中 L1 视为 miss,回 L2;L2 命中回写 L1
//   - L1 容量淘汰:LRU 淘汰,后续从 L2 重新加载
package cachex

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// TwoLevel 本地 LRU + Redis Safe 二级缓存
type TwoLevel struct {
	l1    *lru.Cache[string, l1Entry]
	safe  *Safe // L2
	l1TTL time.Duration
	stats tlStats
}

// l1Entry L1 缓存条目(带过期时间)
type l1Entry struct {
	val     any
	expire  time.Time
	isNull  bool // true = null sentinel 占位(穿透防御)
}

// DefaultL1Cap L1 默认容量
const DefaultL1Cap = 5000

// DefaultL1TTL L1 默认过期时间(短,10s)
//   - 比 L2 TTL 小 5~10 倍,保证"全网已新,本地最迟 10s 内也新"
//   - 太大导致改配置后所有实例需要 L1 TTL 才一致;太小 L1 命中率低
const DefaultL1TTL = 10 * time.Second

// NewTwoLevel 构造
//   - l1Cap: L1 容量(0 → DefaultL1Cap)
//   - l1TTL: L1 TTL(0 → DefaultL1TTL)
//   - safe:  L2 缓存(必传,可为 nil → 自动降级为只 L1,功能退化为本地缓存)
func NewTwoLevel(l1Cap int, l1TTL time.Duration, safe *Safe) *TwoLevel {
	if l1Cap <= 0 {
		l1Cap = DefaultL1Cap
	}
	if l1TTL <= 0 {
		l1TTL = DefaultL1TTL
	}
	// lru.New 返回 error 仅当 cap <= 0,我们已兜底
	c, _ := lru.New[string, l1Entry](l1Cap)
	setL1Size(0) // 初始化 gauge;InitMetrics 未调时内部 nil-safe
	return &TwoLevel{
		l1:    c,
		safe:  safe,
		l1TTL: l1TTL,
	}
}

// Safe 取 L2 客户端(2026-10-26 M95 新增)。
//
// # 为什么需要这个出口
//
// "只用 L2、不用 L1"的数据(改完必须马上全网生效的那类:权限画像、字典)
// 需要拿到**同一个** L2 客户端 —— 但 `TwoLevel.safe` 是私有的,外面拿不到。
//
// 不给这个出口的话,调用方只能自己 `cachex.New(rdb)` 再建一个池,
// 那就多了一个"要不要连 Redis"的决策点 —— 而 `Build` 的注释白纸黑字写着
// 它是**唯一决策点**。两处判断迟早会不一致(一处配了 Redis 另一处没配),
// 症状是"缓存时有时无",极难归因。
//
// # 为什么要 L2 而不是 L1
//
// L1 是**进程内**的:写路径的失效只能删掉本实例的 L1,别的副本仍持有旧值。
// "改完立刻生效"会退化成"最多 L1TTL 之后生效"(默认 10s)。
// 与 `authz.CachedLoader` 同一判断(见 authz/cached_loader.go 的注释)。
//
// # nil 语义
//
//   - `c == nil`(cache.mode=off)→ 返 nil
//   - `c.safe == nil`(cache.mode=l1,没配 Redis)→ 返 nil
//
// 两种 nil 都让调用方拿到"没有 L2",于是 cachex 的所有入口退化成每次回源 ——
// **关掉缓存不该要求调用方写 if**。
func (c *TwoLevel) Safe() *Safe {
	if c == nil {
		return nil
	}
	return c.safe
}

// GetOrLoad 读路径:L1 → L2 → loader
//   - 命中 L1 → 即返(无 RTT)
//   - miss L1 但命中 L2 → 回写 L1(下次 L1 直返)
//   - L1 L2 双 miss → safe 内 singleflight 调 loader → 写 L1 + L2
//
// 返回值:同 Safe.GetOrLoad(值 / hit bool / error)
func (c *TwoLevel) GetOrLoad(ctx context.Context, key string, l2TTL time.Duration, loader LoadFunc) (any, bool, error) {
	if c == nil || c.l1 == nil {
		// 没初始化 → 直接走 L2(若 safe 也 nil,透传 error)
		return c.safeGetOrLoad(ctx, key, l2TTL, loader)
	}
	fkey := c.fkey(key)

	// 1. L1 命中
	if e, ok := c.l1.Get(fkey); ok {
		if time.Now().Before(e.expire) {
			atomic.AddInt64(&c.stats.l1Hit, 1)
			recordOp("l1", "get", "hit", 0)
			if e.isNull {
				recordOp("l1", "get", "null", 0)
				return nil, true, nil // null 占位命中
			}
			return e.val, true, nil
		}
		// L1 已过期 → 删
		c.l1.Remove(fkey)
		setL1Size(c.l1.Len())
		atomic.AddInt64(&c.stats.l1Expire, 1)
		recordOp("l1", "get", "expire", 0)
	}
	atomic.AddInt64(&c.stats.l1Miss, 1)
	recordOp("l1", "get", "miss", 0)

	// 2. L2 命中(safe 内部已带 singleflight + 三防)
	if c.safe != nil {
		val, hit, err := c.safe.GetOrLoad(ctx, key, l2TTL, loader)
		if err == nil {
			c.writeL1(fkey, val, hit, l2TTL)
		}
		return val, hit, err
	}

	// 3. 无 L2 → 直接 loader(null 占位由 safe 处理;此处只兜底)
	val, err := loader(ctx)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			c.writeL1Null(fkey, l2TTL)
			return nil, false, nil
		}
		return nil, false, err
	}
	c.writeL1(fkey, val, false, l2TTL)
	return val, false, nil
}

// safeGetOrLoad L2 fallback
func (c *TwoLevel) safeGetOrLoad(ctx context.Context, key string, l2TTL time.Duration, loader LoadFunc) (any, bool, error) {
	if c.safe != nil {
		return c.safe.GetOrLoad(ctx, key, l2TTL, loader)
	}
	// L1 nil + L2 nil:完全裸跑
	val, err := loader(ctx)
	if errors.Is(err, ErrNotFound) {
		return nil, false, nil
	}
	return val, false, err
}

// writeL1 写 L1(命中 L2 后回写)
func (c *TwoLevel) writeL1(fkey string, val any, hitFromL2 bool, l2TTL time.Duration) {
	if c.l1 == nil {
		return
	}
	// L1 TTL = min(l1TTL, l2TTL)
	ttl := c.l1TTL
	if l2TTL > 0 && l2TTL < ttl {
		ttl = l2TTL
	}
	c.l1.Add(fkey, l1Entry{
		val:    val,
		expire: time.Now().Add(ttl),
	})
	recordOp("l1", "set", "ok", 0)
	setL1Size(c.l1.Len())
}

// writeL1Null null 占位
func (c *TwoLevel) writeL1Null(fkey string, l2TTL time.Duration) {
	if c.l1 == nil {
		return
	}
	ttl := c.l1TTL
	if l2TTL > 0 && l2TTL < ttl {
		ttl = l2TTL
	}
	c.l1.Add(fkey, l1Entry{
		val:    nil,
		expire: time.Now().Add(ttl),
		isNull: true,
	})
	recordOp("l1", "set", "null", 0)
	setL1Size(c.l1.Len())
}

// Set 主动写 L1 + L2
//   - ttl: L2 TTL(>0);L1 用 min(ttl, l1TTL)
func (c *TwoLevel) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	if c.l1 != nil {
		c.l1.Add(c.fkey(key), l1Entry{
			val:    val,
			expire: time.Now().Add(minDur(ttl, c.l1TTL)),
		})
		recordOp("l1", "set", "ok", 0)
		setL1Size(c.l1.Len())
	}
	if c.safe != nil {
		return c.safe.Set(ctx, key, val, ttl)
	}
	return nil
}

// Delete 主动删 L1 + L2
func (c *TwoLevel) Delete(ctx context.Context, key string) {
	if c.l1 != nil {
		c.l1.Remove(c.fkey(key))
		recordOp("l1", "del", "ok", 0)
		setL1Size(c.l1.Len())
	}
	if c.safe != nil {
		c.safe.Del(ctx, key)
	}
}

// Get 直接读 L1(无 loader,不查 L2,无单飞)
//   - 用于"我已知 key 一定在缓存"的快速路径
//   - miss 返 (nil, false, nil)
func (c *TwoLevel) Get(key string) (any, bool) {
	if c.l1 == nil {
		recordOp("l1", "get", "miss", 0)
		return nil, false
	}
	e, ok := c.l1.Get(c.fkey(key))
	if !ok || !time.Now().Before(e.expire) {
		recordOp("l1", "get", "miss", 0)
		return nil, false
	}
	recordOp("l1", "get", "hit", 0)
	if e.isNull {
		recordOp("l1", "get", "null", 0)
		return nil, true
	}
	return e.val, true
}

// Purge 清空 L1(测试/运维用)
//   - 不影响 L2
func (c *TwoLevel) Purge() {
	if c.l1 != nil {
		c.l1.Purge()
	}
}

// Stats 命中统计(诊断用)
type TwoLevelStats struct {
	L1Hit     int64 `json:"l1_hit"`
	L1Miss    int64 `json:"l1_miss"`
	L1Expire  int64 `json:"l1_expire"`
	L1Size    int   `json:"l1_size"`
	L2Enabled bool  `json:"l2_enabled"`
}

type tlStats struct {
	l1Hit     int64
	l1Miss    int64
	l1Expire  int64
}

// GetStats 拉取命中统计
func (c *TwoLevel) GetStats() TwoLevelStats {
	s := TwoLevelStats{
		L1Hit:    atomic.LoadInt64(&c.stats.l1Hit),
		L1Miss:   atomic.LoadInt64(&c.stats.l1Miss),
		L1Expire: atomic.LoadInt64(&c.stats.l1Expire),
	}
	if c.l1 != nil {
		s.L1Size = c.l1.Len()
	}
	s.L2Enabled = c.safe != nil
	return s
}

// fkey TwoLevel 自己的 key 前缀(同 Safe 复用,避免与 L2 key 冲突)
//   - TwoLevel 和 Safe 共享同一 Redis 的话,key 不能混淆
//   - TwoLevel 的 key 形如 "l1:<原 key>",L2 仍是 <原 key>
//   - 这样 L1 失效时 L2 还可命中
func (c *TwoLevel) fkey(key string) string {
	if c.safe == nil {
		// 无 L2 时,L1 自己的 key 直接用原 key
		return key
	}
	return "l1:" + c.safe.fkey(key)
}

func minDur(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
