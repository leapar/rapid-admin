// Redis 实现 —— 限流的正路,内存实现只是降级。
package kvstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCounter 基于 INCR + EXPIRE 的计数器。
//
// 与内存版的语义差异(两处,都是刻意的):
//   - Get 在 key 不存在时返回 redis.Nil,**由调用方决定当 0 还是当错**。
//     loginlock 历史上是"err != nil 就当没失败过",保持这个宽松度。
//   - Incr 的 ttl 只在**首次创建**时生效(INCR 不会重置 TTL)。
//     内存版同样只在首次建 key 时设 exp,两者一致 ——
//     重复失败要把锁定窗口无限续长的话,那是个登录爆破的免死金牌。
//
// ⚠️ **INCR 与 EXPIRE 由一段 Lua 合成一步**(见 incrScript)——
// 2026-11-01 修:以前是两条命令,中间留了"key 已存在但没有 TTL"的窗口,
// 那个窗口里进程挂掉 ⇒ 计数永不过期 ⇒ 登录失败计数永久累积 ⇒ 账号被永久锁定。
type RedisCounter struct {
	rdb *redis.Client
}

// NewRedisCounter 创建 Redis 计数器
func NewRedisCounter(rdb *redis.Client) *RedisCounter {
	return &RedisCounter{rdb: rdb}
}

// incrScript 计数 + 设置过期时间的原子实现。
//
// ⚠️ **2026-11-01:以前的 INCR 与 EXPIRE 是两条命令,中间留了窗口。**
//
//	旧实现:
//
//	    n, _ := rdb.Incr(ctx, key)              // 第 1 条
//	    if n == 1 { rdb.Expire(ctx, key, ttl) }  // 第 2 条
//
//	中间那个窗口里,**key 已经存在、但没有 TTL**。
//	此刻进程挂掉 / 网络断 / 被 SIGKILL ⇒ key **永久不过期** ⇒
//
//	    登录失败计数永不清零 ⇒ 那个 (ip, 用户名) 被**永久锁定**。
//
//	内存版没这个洞(`MemoryCounter.Incr` 建条目时一次算好过期时刻),
//	而**只有内存版被测过** ⇒ 两版语义不一致,没人发现。
//
// 三条分支:
//
//  1. c == 1            计数器刚创建 ⇒ 设 TTL(与内存版"首次建 key 时设 exp"一致)
//  2. TTL == -1         key 存在但没有过期时间 ⇒ **补设 TTL**
//     (修旧实现留下的孤儿 key:它们没有 TTL,会永久存在)
//  3. 其余              TTL 不动 ⇒ **不把计数窗口推后**
//     (重复失败把锁定窗口无限续长 = 登录爆破的免死金牌)
//
// ⚠️ ARGV[1] <= 0 时 EXPIRE 会**立刻删掉** key —— 这与内存版一致:
//
//	内存版 ttl<=0 时条目建出来 exp=now,下一次 Incr 就当过期 ⇒ 永远从 1 起算。
//	Redis 版必须同样"永远从 1 起算",否则两套后端对同一次调用给出不同的计数。
var incrScript = redis.NewScript(`
local c = redis.call('INCR', KEYS[1])
if c == 1 or redis.call('TTL', KEYS[1]) == -1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return c
`)

// Incr 加一并返回加完后的值;首次创建时设 ttl。
//
// ⚠️ **INCR 与 EXPIRE 必须在同一步里做完** —— 见 incrScript 的注释。
func (c *RedisCounter) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if key == "" {
		return 0, nil
	}
	secs := int64(ttl / time.Second)
	if ttl > 0 && secs == 0 {
		secs = 1 // 不足 1 秒的 ttl 至少给 1 秒(否则 EXPIRE 0 = 立刻删除)
	}
	n, err := incrScript.Run(ctx, c.rdb, []string{key}, secs).Int64()
	if err != nil {
		return 0, err
	}
	return n, nil
}

// Get 读当前值。
func (c *RedisCounter) Get(ctx context.Context, key string) (int64, error) {
	n, err := c.rdb.Get(ctx, key).Int64()
	// 判 error 只用 errors.Is(wrapped error 下 == 会失配,且 errorlint 会报)
	if errors.Is(err, redis.Nil) {
		// 不存在当 0,而不是把 err 抛给调用方:
		// "没有失败记录"是**正常状态**,不是错误。
		return 0, nil
	}
	return n, err
}

// Del 删 key
func (c *RedisCounter) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.rdb.Del(ctx, keys...).Err()
}

// RedisWindow 基于 ZSET 的滑动窗口。
//
// 逻辑与 ratelimit.go 改动前**逐条一致**(同一段 pipeline):
// 剔旧 → 加入本次 → ZCard → 设过期。
// 整个过程在一个 TxPipeline 里,RESP2 下 MULTI/EXEC 原子,
// 不会被并发的另一个请求数出"同一时刻两个都过"的中间态。
//
// 这里不改成 INCR + EXPIRE:那是固定窗口,窗口边界能放过 2×max 次,
// 限流一旦能绕过去就不叫限流。
type RedisWindow struct {
	rdb *redis.Client
	// rand 生成 ZSET member,避免同毫秒的两次命中互相覆盖
	rand func(int) string
}

// NewRedisWindow 创建 Redis 滑动窗口
func NewRedisWindow(rdb *redis.Client) *RedisWindow {
	return &RedisWindow{rdb: rdb, rand: RandomID}
}

// randMember 生成 ZSET member(同毫秒的两次命中靠它区分)。
//
// ⚠️ **不能直接调 w.rand** —— 这是 2026-11-01 那个 P0 的第二道防线。
//
//	`kvstore.redisStore()` 以前用结构体字面量 `&RedisWindow{rdb: rdb}` 造窗口,
//	绕过了 NewRedisWindow ⇒ rand 是 nil ⇒ `w.rand(4)` 空函数指针调用 ⇒ panic
//	⇒ 每一个带限流的接口直接 500。
//
//	那个洞已经修了(redisStore 改走构造器),但**只要这个字段还能被留成 nil,
//	同样的错就能再犯一次** —— 而它的后果是"限流中间件 panic"。
//	这里兜住:nil 时退回 RandomID,行为不变,只是不再 panic。
func (w *RedisWindow) randMember() string {
	if w.rand == nil {
		return RandomID(4)
	}
	return w.rand(4)
}

// Allow 记录一次命中,窗口内计数 > max 则拒(本次请求计入窗口)。
func (w *RedisWindow) Allow(ctx context.Context, key string, max int, window time.Duration) (bool, error) {
	if key == "" || max <= 0 {
		return false, nil // 与内存版一致:宁可全拦也不全放
	}
	now := time.Now().UnixMilli()
	min := fmt.Sprintf("%d", now-window.Milliseconds())
	member := fmt.Sprintf("%d-%s", now, w.randMember())

	pipe := w.rdb.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", min)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now), Member: member})
	pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, window+5*time.Second)
	res, err := pipe.Exec(ctx)
	if err != nil {
		// 存储故障:返回 error 让调用方决定。**绝不能当成放行** ——
		// 那正是本层存在的理由之前的那类 bug。
		return false, err
	}
	n, err := res[2].(*redis.IntCmd).Result()
	if err != nil {
		return false, err
	}
	return int(n) <= max, nil
}
