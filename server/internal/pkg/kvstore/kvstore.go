// 计数 / 滑动窗口的键值存储 —— 限流和登录锁定的落地处。
//
// # 为什么要有这一层(2026-10-21)
//
// 限流和登录锁定此前各自写着同一种退化:
//
//	if repository.RDB == nil { return nil }   // ratelimit.go:30 / loginlock.go:26
//
// 于是 **dev 环境里限流和登录锁定是完全不存在的**:没有 Redis 就没有保护,
// 而且不报错、不告警,只是"请求都过去了"。想测一下限流触发是什么效果,
// 本地根本测不出来 —— 因为触发它的开关在本地永远关着。
//
// 这和 token 吊销(`if bl := DefaultBlacklist(); bl != nil`)是**同一类错误**:
// 把"没配存储"当成"不需要保护"。吊销那边已经在 auth.RevocationStore 修掉了,
// 这一层是把同样的修法搬到计数/窗口上。
//
// # 唯一决策点
//
// 有没有 Redis 只在 Resolve() 里判断一次,main 启动时调它并注入。
// 之后 ratelimit / loginlock 只问 Default() 要东西,自己不碰 repository.RDB ——
// 否则每加一个用存储的地方就多一个"忘了判空"的漏点。
//
// # 两种数据形状,两个接口
//
// 故意不合成一个接口:计数(登录失败次数)和滑动窗口(请求频率)的
// 数据结构与操作都不一样。硬合成一个只能取两者公约数,结果是
// Redis 侧也退化成 GET/SET 轮询,把 ZSET 的原子性丢了 ——
// 那才是真的把限流写坏了。分开保真,代价是多两个小类型。
package kvstore

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// 存储模式。配 kv.mode。
const (
	// ModeAuto 有 Redis 用 Redis,没有退内存(默认)。保证"限流存在",
	// 不保证多副本一致 —— 单副本部署 / 本地开发够用。
	ModeAuto = "auto"
	// ModeRedis 强制 Redis。没配 redis.addr 直接启动失败。
	// **生产多副本必须用这个**:内存限流在每个副本各数各的,
	// N 个副本的实际阈值是配置值的 N 倍。
	ModeRedis = "redis"
	// ModeMemory 强制内存,即使配了 Redis 也不连。仅调试用。
	ModeMemory = "memory"
)

// Counter 计数 + TTL。登录失败次数用这个。
type Counter interface {
	// Incr 加一并返回**加完后的值**;key 不存在时按 1 起算,并在 ttl 内不过期。
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	// Get 读当前值。key 不存在返回 0,err 为 nil —— 调用方不该靠 err 区分"不存在"。
	Get(ctx context.Context, key string) (int64, error)
	// Del 删 key(登录成功后清零)。keys 为空时直接返回 nil。
	Del(ctx context.Context, keys ...string) error
}

// Window 滑动窗口限流。返回**是否放行**。
type Window interface {
	// Allow 记录一次命中,窗口内计数超过 max 则返回 false。
	//
	// 语义与 Redis ZSET 版本一致:**本次请求计入窗口**,
	// 所以 "窗口内第 max 次"放行、第 max+1 次拦。
	Allow(ctx context.Context, key string, max int, window time.Duration) (bool, error)
}

// Store 一次决策的结果。
type Store struct {
	Counter Counter
	Window  Window
	// Backend "redis" | "memory",只用于日志和 /healthz。
	Backend string
}

// Resolve 按模式构造存储。rdb 为 nil 表示 Redis 不可用。
//
// ModeRedis + rdb==nil 直接返错让 main panic —— 与 ResolveRevocation 同一个理由:
// 静默降级到内存意味着多副本部署下限流形同虚设,而没有人知道。
//
// # 为什么参数是 *redis.Client 而不是 redis.UniversalClient(2026-10-21,实测)
//
// **接口会把 typed nil 变成非 nil。** repository.GetRedis() 返回的是具体类型
// *redis.Client,而它为 nil 时(redis.addr 没配),传进 UniversalClient 参数就变成
// 一个装着 nil 指针的**非 nil 接口**:
//
//	var concrete *redis.Client        // nil
//	var iface redis.UniversalClient = concrete
//	concrete == nil  // true
//	iface    == nil  // false   ← 这里!
//
// 于是本函数里所有的 `if rdb == nil` 都是死代码,显式配 redis 模式 + 没配
// addr 会**照常启动**,然后每次调用都往一台死 Redis 上打。真机启动日志当场
// 露了马脚:redis.addr 为空、日志明写 "redis skipped",kvstore 却报
// backend=redis。
//
// 取具体类型就绕开了整个陷阱(concrete == nil 是诚实的),而且调用方本来就
// 只有 *redis.Client,不存在为了接口而把 nil 塞进去的必要。
func Resolve(mode string, rdb *redis.Client) (*Store, error) {
	switch mode {
	case ModeRedis:
		if rdb == nil {
			return nil, fmt.Errorf(
				"kv.mode=redis 但 Redis 不可用(检查 redis.addr)" +
					" —— 多副本部署下用内存限流等于把阈值放大 N 倍,拒绝静默降级")
		}
		return redisStore(rdb), nil

	case ModeMemory:
		return memoryStore(), nil

	case ModeAuto, "":
		if rdb == nil {
			return memoryStore(), nil
		}
		return redisStore(rdb), nil

	default:
		return nil, fmt.Errorf("kv.mode 非法:%q(可选 %s / %s / %s)",
			mode, ModeAuto, ModeRedis, ModeMemory)
	}
}

func redisStore(rdb *redis.Client) *Store {
	// ⚠️ **必须走构造器,不许写结构体字面量** —— 2026-11-01 抓到 P0。
	//
	// 这里以前是:
	//
	//	return &Store{Counter: &RedisCounter{rdb: rdb}, Window: &RedisWindow{rdb: rdb}, ...}
	//
	// `&RedisWindow{rdb: rdb}` **绕过了 NewRedisWindow** ⇒ `rand` 字段是 nil
	// ⇒ `RedisWindow.Allow` 里的 `w.rand(4)` 是**空函数指针调用** ⇒ panic。
	//
	// 后果不是"限流不准",是**每个带限流的接口直接 500**:
	// 登录 / 发短信 / 刷新 token / 验证码 / 切换身份 全在限流名单上
	// (见 api/router/router.go 的 ratelimit 中间件挂载点)。
	// Recover 中间件把 panic 兜成 500,日志里只有一条 recovered panic,
	// 看起来像"偶发"。
	//
	// 为什么一直没被发现:
	//   1. `RedisWindow` 的 Redis 版**从来没被任何测试跑过**(连 miniredis 都没有);
	//      kvstore 的单测只跑内存版。
	//   2. 内存版(`MemoryWindow`)根本没有 `rand` 字段 ⇒ 内存版永远是对的。
	//   3. `Resolve` 是**唯一**的装配入口,main 走的就是它 ⇒ 一旦配了 redis.addr,
	//      `kv.mode=auto` 就会拿到这个坏掉的窗口。
	//
	// 回归网:`internal/integration/redis/kvstore_test.go`
	// 的 `TestKVStore_Redis窗口_Resolve装配出来的不许panic`(走真 Memurai)。
	return &Store{
		Counter: NewRedisCounter(rdb),
		Window:  NewRedisWindow(rdb),
		Backend: "redis",
	}
}

func memoryStore() *Store {
	return &Store{Counter: NewMemoryCounter(), Window: NewMemoryWindow(), Backend: "memory"}
}

// global 全局实例,由 main 在初始化阶段注入
var global *Store

// SetDefault 注入(main 调)。传 nil 是**误用**而不是"恢复默认" ——
// 忘了注入就让它悄悄退回内存,等于回到本文件开头那个 bug。
func SetDefault(s *Store) {
	if s == nil {
		panic("kvstore.SetDefault(nil):必须显式注入,包括测试里 —— " +
			"静默退回内存限流会让所有限流断言假绿")
	}
	global = s
}

// Default 取全局存储,**保证非 nil**(未注入时给一份进程级内存实现)。
//
// 同样返回内存兜底,是刻意的:这里的能力**降级了但不会消失**,
// 而吊销那边选择"配了就必须对"。区别在于本层的降级后果小得多
// (限流阈值不准 ≠ 令牌不吊销),不值得为它 panic。
var memoryFallback = memoryStore()

func init() { global = memoryFallback }

// DefaultStore 返回全局存储。
func DefaultStore() *Store {
	if global == nil {
		global = memoryFallback
	}
	return global
}
