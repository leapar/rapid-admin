// Package revocache —— 跨实例 token 撤销广播 + 本地缓存
//
// 文档 §1.2 #10 P1
//
// 背景(为什么需要这个包):
//   - 现有 Blacklist 只在单 Redis 实例里 SET jti → 其它 Redis / 其它数据中心没同步
//   - 多实例部署:实例 A 撤销 JTI,实例 B 仍可能让该 token 通过(直到它的 next Redis check)
//   - 跨数据中心:RTT 高 + Redis 同步方案贵
//
// 解决:Redis pub/sub 广播 + 实例本地缓存
//   - 任何实例 Revoke → 1) Redis Set(持久) + 2) Redis pub/sub "auth:revoke:channel" 发消息
//   - 每个实例 Subscribe 同一个 channel,收到消息 → 写本地 map[jti]expiry
//   - IsRevoked 优先查本地 O(1),miss 才打 Redis(单实例 miss 率 ~0)
//
// 关键设计:
//   - 本地缓存用 map + sync.RWMutex,过期条目靠两条路径回收:
//     IsRevoked 命中时的懒清理 + Add 写路径顺带清扫(sweepLocked,无后台 janitor)
//   - 进程启动时,先全量加载已存在的 blacklist(一次性 Redis SCAN)
//     防止刚启动的实例在收到第一条 pub/sub 之前漏掉旧 revoke
//   - rdb == nil 时(rdb 未配置)→ 退化为直接返回 false(由业务层 fallback)
package revocache

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

const (
	// ChannelName Redis pub/sub 频道名
	ChannelName = "auth:revoke:channel"

	// blacklistPrefix 与 pkg/auth.Blacklist 内的 prefix 保持一致
	blacklistPrefix = "auth:bl:"

	// scanBatchSize SCAN 一次拉多少
	scanBatchSize = 200
)

// Message pub/sub 消息体(JSON)
type Message struct {
	JTI      string `json:"jti"`
	ExpiryAt int64  `json:"exp"` // unix 秒
	ActorID  uint64 `json:"actor"` // 发起人(便于审计)
	Reason   string `json:"reason,omitempty"`
}

// Cache 本地缓存 + 订阅
type Cache struct {
	mu     sync.RWMutex
	items  map[string]time.Time // jti → 过期时间
	rdb    redis.UniversalClient
	closed chan struct{}
}

// New 构造(不启动订阅)
func New(rdb redis.UniversalClient) *Cache {
	return &Cache{
		items:  make(map[string]time.Time, 256),
		rdb:    rdb,
		closed: make(chan struct{}),
	}
}

// Start 启动后台订阅 + 启动期全量加载已存在的 blacklist
//   - rdb nil → 立即返回(业务层 fallback 到直查 Redis)
//   - 加载失败 → log warn 但不阻塞启动(后续增量同步)
func (c *Cache) Start(ctx context.Context) {
	if c.rdb == nil {
		logger.L().Info("revocache: rdb nil, 本地缓存不启用(始终走直查)")
		return
	}

	// 启动期加载已有 blacklist
	if err := c.bulkLoad(ctx); err != nil {
		logger.L().Warn("revocache: 启动期加载 blacklist 失败,后续走增量",
			zap.Error(err))
	}

	go c.subscribeLoop(ctx)
	logger.L().Info("revocache: 订阅启动",
		zap.String("channel", ChannelName))
}

// Stop 关闭订阅
func (c *Cache) Stop() {
	close(c.closed)
}

// IsRevoked 本地 O(1) 查询
//   - 命中且未过期 → true
//   - miss 或已过期 → false(由业务层 fallback 到 Redis 直查)
func (c *Cache) IsRevoked(jti string) bool {
	if jti == "" {
		return false
	}
	c.mu.RLock()
	exp, ok := c.items[jti]
	c.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		// 懒清理(并发可能再次写回,无害)
		c.mu.Lock()
		// 双重检查:避免被新 publish 覆盖
		if cur, ok := c.items[jti]; ok && time.Now().After(cur) {
			delete(c.items, jti)
		}
		c.mu.Unlock()
		return false
	}
	return true
}

// Add 内部加本地(供 subscribeLoop 用,也供 Revoke 本地立即生效)
func (c *Cache) Add(jti string, exp time.Time) {
	if jti == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().After(exp) {
		// 过期不入缓存
		return
	}
	c.items[jti] = exp
	c.sweepLocked()
}

// sweepLocked 清掉已过期的条目(调用方必须持写锁)。
//
// 包头注释曾经承诺"后台 janitor 兜底清理"——那个 janitor 从来没存在过。
// 过期条目只有 IsRevoked 恰好命中时才被懒清理,而 pub/sub 是全量广播写:
// 每次登出 / 身份切换 / refresh 轮换都会往**所有实例**的 map 里塞一条,
// 过期后无人再查它 ⇒ 永不回收。refresh 轮换是常态操作,长期运行下单调增长。
// 与 auth.MemoryStore.sweepLocked 同款口径:写路径顺带清扫,不引入后台协程。
func (c *Cache) sweepLocked() {
	now := time.Now()
	for jti, exp := range c.items {
		if now.After(exp) {
			delete(c.items, jti)
		}
	}
}

// Snapshot 当前缓存大小(给 metrics/调试用)
func (c *Cache) Snapshot() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// subscribeLoop 长连接订阅
func (c *Cache) subscribeLoop(parentCtx context.Context) {
	for {
		select {
		case <-c.closed:
			return
		case <-parentCtx.Done():
			return
		default:
		}

		ctx, cancel := context.WithCancel(parentCtx)
		ps := c.rdb.Subscribe(ctx, ChannelName)
		ch := ps.Channel()

		// 自动重连循环
		reconnect := func() {
			cancel()
			_ = ps.Close()
			// 避免崩太快
			time.Sleep(2 * time.Second)
		}

		// 阻塞收消息
		func() {
			defer reconnect()
			for {
				select {
				case <-c.closed:
					return
				case <-parentCtx.Done():
					return
				case msg, ok := <-ch:
					if !ok {
						logger.L().Warn("revocache: 订阅 channel 关闭,准备重连")
						return
					}
					c.handleMessage(msg.Payload)
				}
			}
		}()
	}
}

// handleMessage 解析 pub/sub 消息 → 写本地
func (c *Cache) handleMessage(payload string) {
	var m Message
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		logger.L().Warn("revocache: 解析消息失败",
			zap.Error(err), zap.String("payload", payload))
		return
	}
	if m.JTI == "" || m.ExpiryAt == 0 {
		return
	}
	exp := time.Unix(m.ExpiryAt, 0)
	c.Add(m.JTI, exp)
}

// bulkLoad 启动期 SCAN blacklist prefix,加载已存在条目
// 数量小(< 1w)通常一次性搞定;数量大(几十万 token)需要分批
func (c *Cache) bulkLoad(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var cursor uint64
	loaded := 0
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, blacklistPrefix+"*", scanBatchSize).Result()
		if err != nil {
			return err
		}
		for _, k := range keys {
			jti := k[len(blacklistPrefix):]
			if jti == "" {
				continue
			}
			ttl, err := c.rdb.TTL(ctx, k).Result()
			if err != nil || ttl <= 0 {
				continue
			}
			c.Add(jti, time.Now().Add(ttl))
			loaded++
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	logger.L().Info("revocache: 启动期加载 blacklist 完成",
		zap.Int("loaded", loaded))
	return nil
}
