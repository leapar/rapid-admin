package cronx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// Locker 分布式互斥。
//
// 只负责「同一时刻只有一个副本能进临界区」,**不负责任务本身的状态** ——
// 那是 Store 的事。两层分工见 cronx.go 包注释。
type Locker interface {
	// Available 锁是否真的可用(Redis 连不通时返 false → 降级到只有 Layer 0)
	Available() bool
	// TryLock 抢锁。返 false = 别人拿着。
	TryLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	// Renew 续期,只有持锁方成功(compare-and-pexpire)
	Renew(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	// Unlock 释放,只有持锁方成功(compare-and-delete)
	Unlock(ctx context.Context, key, owner string) error
}

// ⚠️ 两段 Lua 都必须是 CAS。理由写在各自函数上方,不要图省事改成裸 DEL / PEXPIRE:
//
//	非 CAS 释放:A 跑超时 → 锁 TTL 到期 → B 抢到锁开始跑 → A 结束时裸 DEL
//	              → **把 B 的锁删了** → C 立刻抢到并发跑。
//	              CAS 是这条路径上唯一的防线。
var luaRenew = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

var luaUnlock = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

type redisLocker struct {
	rdb    *redis.Client
	prefix string
}

func (l *redisLocker) Available() bool { return l != nil && l.rdb != nil }

func (l *redisLocker) fkey(key string) string {
	if l.prefix == "" {
		return key
	}
	return l.prefix + key
}

func (l *redisLocker) TryLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if !l.Available() {
		return false, nil
	}
	return l.rdb.SetNX(ctx, l.fkey(key), owner, ttl).Result()
}

func (l *redisLocker) Renew(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if !l.Available() {
		return false, nil
	}
	n, err := luaRenew.Run(ctx, l.rdb, []string{l.fkey(key)}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (l *redisLocker) Unlock(ctx context.Context, key, owner string) error {
	if !l.Available() {
		return nil
	}
	// CAS 删除:锁已经不属于我(超时被别人抢走)时,脚本返回 0 且不删。
	// 这正是我们想要的 —— 此时"删掉别人的锁"会造成三副本并发。
	_, err := luaUnlock.Run(ctx, l.rdb, []string{l.fkey(key)}, owner).Int64()
	return err
}

// memoryLocker 进程内锁,给单副本 / 无 Redis 的开发与降级场景。
//
// 语义上比 Redis 锁弱一档:**只在本进程内互斥**。多副本各跑各的,拦不住。
// 所以它在 Config.LockMode=auto + 没 Redis 时会连带打降级告警。
type memoryLocker struct {
	mu   sync.Mutex
	held map[string]bool
}

func newMemoryLocker() *memoryLocker { return &memoryLocker{held: make(map[string]bool)} }

func (l *memoryLocker) Available() bool { return l != nil }

// TryLock 进程内互斥。
//
// ttl 参数在这里**故意不用**:进程内 map 随进程一起消失,不存在"崩溃后残留导致
// 永久锁死";而 entry 的增删由 Trigger 的成对 Unlock 保证,也不会无界增长。
// 留着这个参数纯粹是为了和 Locker 接口(以及 Redis 实现)保持同一形状。
func (l *memoryLocker) TryLock(_ context.Context, key, _ string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[key] {
		return false, nil
	}
	l.held[key] = true
	return true, nil
}

func (l *memoryLocker) Renew(_ context.Context, _ string, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (l *memoryLocker) Unlock(_ context.Context, key, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.held, key)
	return nil
}

// nopLocker lock_mode=off
type nopLocker struct{}

func (nopLocker) Available() bool { return false }
func (nopLocker) TryLock(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (nopLocker) Renew(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}
func (nopLocker) Unlock(context.Context, string, string) error { return nil }

// newOwnerToken 生成本次运行的 owner 标识
//
// ⚠️ **必须每次运行都不同**。用固定的 instanceID 的话,同一副本重试两次会拿到
// 同一个 owner,第一次的 CAS 释放会误删第二次的锁(见 Unlock 的 CAS 注释)。
func newOwnerToken(instanceID string) string {
	return fmt.Sprintf("%s:%d:%d", instanceID, time.Now().UnixNano(), nextSeq())
}

var seqCounter struct {
	sync.Mutex
	n uint64
}

func nextSeq() uint64 {
	seqCounter.Lock()
	defer seqCounter.Unlock()
	seqCounter.n++
	return seqCounter.n
}

// ResolveLocker 按 lock_mode 决定用哪种锁 —— **唯一的决策点**。
//
// 与 kvstore.Resolve / cachex.Build / auth.ResolveRevocation 同一个模式:
// "有没有 Redis" 只在一个地方判断,散在各处就会出现"读路径判断了、写路径忘了"
// 这种最难查的不一致。
//
// ⚠️ cron 不能像缓存那样 fail-open 就完事(缓存失效只变慢,这里重复执行会写坏数据),
// 也不能 fail-closed(没锁就不跑)—— Redis 抖一下让全站定时任务停摆,
// 明源同步不更新、过期授权不下榜,是更难发现的静默故障。
// 折中见 cronx.go 包注释:降级继续跑 + 告警 + degraded 标记。
func ResolveLocker(mode string, rdb *redis.Client, prefix string) (Locker, error) {
	switch mode {
	case LockModeOff:
		logger.L().Warn("cronx: lock_mode=off,定时任务没有任何互斥保护 —— 多副本部署会重复执行",
			zap.String("lock_mode", mode))
		return nopLocker{}, nil

	case LockModeRedis:
		if rdb == nil {
			// 专治生产漏配:吊销/锁的降级 = 安全事故 → 直接启动失败
			return nil, errors.New("cronx: lock_mode=redis 但未配置 Redis,拒绝启动")
		}
		return &redisLocker{rdb: rdb, prefix: prefix}, nil

	case LockModeMemory:
		if rdb != nil {
			logger.L().Warn("cronx: lock_mode=memory,即使配了 Redis 也不连 —— 仅适用于单副本部署",
				zap.String("lock_mode", mode))
		}
		return newMemoryLocker(), nil

	case LockModeAuto, "":
		if rdb == nil {
			logger.L().Warn("cronx: 未配置 Redis,去重降级为进程内锁(多副本部署下任务会重复执行)",
				zap.String("job_lock_degraded", "true"))
			return newMemoryLocker(), nil
		}
		return &redisLocker{rdb: rdb, prefix: prefix}, nil

	default:
		// 不静默当 auto 处理:"我配了 X,系统跑的是 Y"是最难查的一类配置问题
		return nil, fmt.Errorf("cronx: 未知 lock_mode %q(可选 auto|redis|memory|off)", mode)
	}
}
