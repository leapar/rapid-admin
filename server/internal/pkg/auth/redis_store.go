// Redis 吊销存储 —— RevocationStore 的多副本实现。
// 文档 §8.5/§9.6
//
// §1.2 #10 P1 多实例撤销同步(2026-10-19 commit):
//   - Revoke 时:Redis SET 持久 + 立刻本地 cache + pub/sub 广播给其它实例
//   - IsRevoked 时:本地 cache O(1) 优先,miss 才打 Redis
//   - 跨数据中心:每个数据中心启一个 Redis pub/sub,广播各自扇出
//
// 2026-10-21:原名 Blacklist,现改名 RedisStore —— 名字要能自证"这是哪种实现",
// 因为它现在有一个兄弟(MemoryStore),而选错实现是有生产后果的。
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/auth/revocache"
	"rapid-admin/internal/pkg/logger"
)

// RedisStore token 吊销存储(Redis SET + TTL + 跨实例 pub/sub 广播)。
//
// 多副本部署下唯一正确的实现 —— 内存版只在单副本有效。
type RedisStore struct {
	rdb      *redis.Client
	prefix   string
	revcache *revocache.Cache // 可选(为 nil 时退化)
}

// NewRedisStore 创建 Redis 吊销存储
func NewRedisStore(rdb *redis.Client) *RedisStore {
	return &RedisStore{rdb: rdb, prefix: "auth:bl:"}
}

// SetRevocache 注入跨实例广播缓存(由 main 在 init 阶段调用)
func (b *RedisStore) SetRevocache(c *revocache.Cache) { b.revcache = c }

// Revoke 将 jti 加入黑名单,ttl 至少到 token 过期时间
//   - 同步:Redis SET 持久(可重入读时看到)
//   - 本地:立即写 cache,本实例后续请求立即生效
//   - 广播:pub/sub 推其它实例,它们的 subscribe loop 收到后写本地
func (b *RedisStore) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	// 1. Redis SET 持久
	if err := b.rdb.Set(ctx, b.prefix+jti, 1, ttl).Err(); err != nil {
		return err
	}
	// 2. 本地 cache 立即生效
	exp := time.Now().Add(ttl)
	if b.revcache != nil {
		b.revcache.Add(jti, exp)
	}
	// 3. pub/sub 广播(失败仅 warn,不影响主流程)
	if b.rdb != nil {
		msg, _ := json.Marshal(revocache.Message{
			JTI:      jti,
			ExpiryAt: exp.Unix(),
		})
		if err := b.rdb.Publish(ctx, revocache.ChannelName, string(msg)).Err(); err != nil {
			logger.L().Warn("revocation: pub/sub 广播失败(其它实例可能延迟生效)",
				zap.Error(err), zap.String("jti", jti))
		}
	}
	return nil
}

// IsRevoked 校验 jti 是否已被吊销
//   - 本地 cache 命中 → O(1)
//   - miss → Redis Exists(主源);命中时回填本地 cache
func (b *RedisStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	// 1. 本地 cache 优先
	if b.revcache != nil && b.revcache.IsRevoked(jti) {
		return true, nil
	}
	// 2. miss → 查 Redis
	n, err := b.rdb.Exists(ctx, b.prefix+jti).Result()
	if err != nil {
		return false, err
	}
	revoked := n > 0
	if revoked && b.revcache != nil {
		// 回填(从 Redis TTL 推算本地 exp)
		ttl, terr := b.rdb.TTL(ctx, b.prefix+jti).Result()
		if terr == nil && ttl > 0 {
			b.revcache.Add(jti, time.Now().Add(ttl))
		}
	}
	return revoked, nil
}

// ====================== 强制下线(按用户整体作废) ======================

// 推进作废线的 Lua 脚本。
//
// 必须原子:"只前进不后退"在 GET-then-SET 的写法下守不住 —— 两个实例并发改密码,
// 后到的请求可能带着更早的 time.Now() 到达,把线从 T2 退回 T1,于是 [T1,T2)
// 之间签发的 token 整批复活。脚本把"比较 + 写入"合成一步,跨实例也成立。
//
// 存 UnixMilli 而不是 UnixNano:Lua 的 number 是 double,2^53 约 9.007e15,
// 而 UnixNano 现在约 1.7e18 —— 超出后尾数被静默舍入。毫秒精度对
// ClockSkewGrace(60s)来说绰绰有余,不值得为它引入一个精度陷阱。
var invalidateFromScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
local at  = tonumber(ARGV[1])
if cur == false or at > tonumber(cur) then
  redis.call('SET', KEYS[1], ARGV[1])
  return ARGV[1]
end
return cur
`)

// InvalidateBefore 取该用户的作废线。键不存在 = 从未作废,返回零值 + nil。
//
// ⚠️ 这里**刻意不做本地缓存**。IsRevoked 可以缓存 —— 它只缓存"已吊销"这个肯定
// 结论,未命中就回落 Redis,缓存永远不会"过期成错"。作废线不同:它缓存的是
// **当前值**,一旦某个实例的缓存因广播丢失而陈旧,被改过密码的用户就能在那台
// 实例上继续通行,而且没有上界(无 TTL 就永远不会自愈)。
//
// 每请求多一次 Redis GET 是明确取舍:这是内部后台,QPS 远低于公开接口,
// 而「改完密码立刻全场生效」是必须成立的安全属性。宁可多一次读,也不换一个有
// 静默失效窗口的缓存 —— 这正是本轮反复在消灭的那类故障。
//
// 将来若真成瓶颈,正确的优化方向是 pub/sub 广播 + 短 TTL 兜底(而不是无 TTL 的
// 纯本地缓存),且必须先有「广播丢失」的测试。
func (b *RedisStore) InvalidateBefore(ctx context.Context, userID uint64) (time.Time, error) {
	raw, err := b.rdb.Get(ctx, b.invKey(userID)).Result()
	if err != nil {
		if err == redis.Nil {
			return time.Time{}, nil // 从未作废 —— 这不是错误
		}
		return time.Time{}, err
	}
	ms, convErr := strconv.ParseInt(raw, 10, 64)
	if convErr != nil {
		// 值坏了比没有值更难办:当成"未作废"是静默失效,当成"全部作废"是随机
		// 踢人。交回 err,由调用点显式决定降级 —— 不在这里替它做决定。
		return time.Time{}, fmt.Errorf("auth: 作废线格式损坏 user=%d raw=%q: %w",
			userID, raw, convErr)
	}
	return time.UnixMilli(ms), nil
}

// InvalidateFrom 把该用户的作废线推到 at(原子、只前进不后退)。
func (b *RedisStore) InvalidateFrom(ctx context.Context, userID uint64, at time.Time) error {
	if at.IsZero() {
		return nil
	}
	// 无 TTL:见 MemoryStore.invalid 的注释 —— 标记早于它要作废的令牌消失,
	// 那批令牌就整批复活。条数上界 = 改过密码的用户数。
	_, err := invalidateFromScript.Run(ctx, b.rdb,
		[]string{b.invKey(userID)}, at.UnixMilli()).Result()
	return err
}

func (b *RedisStore) invKey(userID uint64) string {
	return b.prefix + "inv:" + strconv.FormatUint(userID, 10)
}
