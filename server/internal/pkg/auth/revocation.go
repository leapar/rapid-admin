// 吊销存储抽象 —— JWT 登出 / 改密码 / 身份切换的落地处。
//
// # 为什么要抽这一层(2026-10-21)
//
// 之前叫 Blacklist,只有一个实现:Redis。Redis 没配时 main.go 干脆不构造它,
// DefaultBlacklist() 返回 nil,而四处调用点都写成了:
//
//	if caller.JTI != "" {
//		if bl := auth.DefaultBlacklist(); bl != nil {   // ← 永远不成立
//			bl.Revoke(...)
//
// 两层守卫、层层短路,整条吊销链路**从来没有执行过一次**。表现是:
// 登出后 token 照样能用、改密码后人还在线上、身份切换不吊销旧会话。
// 而且它不报错、不告警 —— 跟"黑名单"这个安全机制的预期完全相反:
// 配错 Redis = 静默失去吊销能力。
//
// 现在:
//   - DefaultRevocation() **永不返回 nil**,没注入时给内存实现
//   - 调用点只剩一层 `if err := ...Revoke(...)`,守卫全删
//   - 显式配置 redis 模式但没配 redis.addr → **启动直接 panic**(专治生产漏配)
//
// # 多实例注意
//
// MemoryStore 是**进程内**的,只在本实例有效。生产多副本必须用 RedisStore,
// 否则用户换个负载均衡节点,被吊销的 token 又活了。
// 这就是 revocation_mode 的意义:把"我现在到底是不是安全的"变成显式配置,
// 而不是隐式继承"有没有 Redis"。
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RevocationStore token 吊销存储。
//
// 实现必须满足:Revoke 之后同一进程内的 IsRevoked 必须立刻返回 true
// (RedisStore 靠本地 cache 保证,不依赖下一次 Redis 查询)。
type RevocationStore interface {
	// Revoke 吊销 jti,ttl 是"还要记多久"(通常 = 该 token 的剩余寿命)。
	//   ttl <= 0 视为无需记录(等价于 no-op),返回 nil。
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
	// IsRevoked 查询 jti 是否已被吊销。
	//   出错时返回 (false, err) —— 调用点必须自己决定降级策略,
	//   不要把 err 当成"已吊销",也不要把 err 当成"没吊销"。
	IsRevoked(ctx context.Context, jti string) (bool, error)

	// ---- 强制下线(按用户整体作废,见 invalidate.go) ----
	//
	// 下面两个方法与上面两个放同一个接口,不是因为"都是缓存",而是因为
	// **它们对部署的要求完全相同**:都需要跨进程共享、都在 memory 模式下降级成
	// 进程内、都需要同一个 Redis 才能在多副本下正确。拆成两个接口就得再加一个
	// 开关,于是"有没有 Redis"又变回两个问题 —— 而那正是 revocation_mode
	// 这个开关当初要消灭的东西。

	// InvalidateBefore 取该用户的作废线(该时刻之前签发的令牌一律作废)。
	//   返回零值 time.Time 表示该用户从未被强制下线过(绝大多数情况)。
	//   出错时调用点必须自己决定降级策略,不要把 err 当成"已作废"。
	InvalidateBefore(ctx context.Context, userID uint64) (time.Time, error)
	// InvalidateFrom 把该用户的作废线推到 at —— 幂等,且**只前进不后退**。
	//   重复调用、或 at 早于现有作废线,都不会让作废线回退(回退等于让已经被
	//   踢掉的 token 又活过来)。
	InvalidateFrom(ctx context.Context, userID uint64, at time.Time) error
}

// 吊销模式。配 auth.revocation_mode。
const (
	// RevocationAuto 有 Redis 用 Redis,没有就退内存。只保证"能吊销",
	// 不保证多副本一致 —— 适合本地开发与单副本部署。
	RevocationAuto = "auto"
	// RevocationRedis 强制 Redis。没配 redis.addr 就启动失败。
	RevocationRedis = "redis"
	// RevocationMemory 强制内存。即便配了 Redis 也用内存(仅调试用)。
	RevocationMemory = "memory"
)

// globalRevocation 全局实例,由 main 注入或首次访问时兜底创建
var globalRevocation RevocationStore

// SetDefaultRevocation 注入(由 main 在初始化阶段调用)
func SetDefaultRevocation(rs RevocationStore) {
	if rs != nil {
		globalRevocation = rs
	}
}

// DefaultRevocation 取全局吊销存储,**保证非 nil**。
//
// 为什么是"保证"而不是"可能为 nil":之前每个调用点都要写
// `if bl := ...; bl != nil`,而那层判空正是吊销失效的直接原因
// —— 看着是防御性代码,实际是把"存储没配好"和"不需要吊销"混成了一个分支。
// 现在判空无处可藏:忘了注入会拿到内存实现(能吊销),配错会在启动时炸。
func DefaultRevocation() RevocationStore {
	if globalRevocation == nil {
		globalRevocation = NewMemoryStore()
	}
	return globalRevocation
}

// memoryFallback 全局内存兜底单例,保证 DefaultRevocation 在未注入时
// 也复用同一个实例(否则每次调用新建一个,吊销记录当场蒸发)。
var memoryFallback = NewMemoryStore()

func init() { globalRevocation = memoryFallback }

// ResolveRevocation 按模式构造吊销存储。
//
// rdb 为 nil 表示 Redis 不可用(没配 addr,或连不上)。
// RevocationRedis + rdb==nil 是**配置错误**,直接返错让 main panic ——
// 这种"配了就必须对"的语义比"尽量用"安全:静默降级到内存意味着
// 多副本部署下吊销形同虚设,而没有任何人知道。
//
// # 参数是 *redis.Client 而不是 redis.UniversalClient —— 不是随手选的
//
// 接口会把 typed nil 变成非 nil:repository.GetRedis() 在 redis.addr 为空时
// 返回一个 nil 的 *redis.Client,传进接口参数后 `rdb == nil` 恒为 false,
// 于是下面这个检查形同虚设 —— 显式配 redis 模式却没配 addr 会照常启动,
// 然后每次吊销查询都往一台死 Redis 上打。真机启动日志当场露了马脚:
// 同一屏里 "redis skipped (empty addr)" 和 "backend=redis" 并排出现。
//
// 取具体类型让 nil 判断变诚实。kvstore.Resolve 是同一个原因改的。
func ResolveRevocation(mode string, rdb *redis.Client) (RevocationStore, error) {
	switch mode {
	case RevocationRedis:
		if rdb == nil {
			return nil, fmt.Errorf(
				"auth.revocation_mode=redis 但 Redis 不可用(检查 redis.addr)" +
					" —— 多副本部署下用内存吊销等于没有吊销,拒绝静默降级")
		}
		return NewRedisStore(rdb), nil

	case RevocationMemory:
		return NewMemoryStore(), nil

	case RevocationAuto, "":
		if rdb == nil {
			return NewMemoryStore(), nil
		}
		return NewRedisStore(rdb), nil

	default:
		return nil, fmt.Errorf("auth.revocation_mode 非法:%q(可选 %s / %s / %s)",
			mode, RevocationAuto, RevocationRedis, RevocationMemory)
	}
}
