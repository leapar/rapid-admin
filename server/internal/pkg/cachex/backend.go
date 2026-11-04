// 缓存后端抽象 —— 让「三防」跑在接口上,而不是绑死在 Redis 上。
//
// # 为什么要有这一层(2026-10-25 M90)
//
// 之前 `Safe` 直接持有 *redis.Client,于是有三个后果:
//
//  1. **三防只能配 Redis 用**。别的后端(内存 / 文件 / 其它缓存服务)想用
//     singleflight + null sentinel + TTL 抖动,只能把逻辑抄一遍 ——
//     抄第二遍就是两份实现、两处 bug。
//  2. **三防的真路径基本没被测过**。测试里塞的是一个指向 127.0.0.1:6379 的
//     死客户端(见 build_test.go:15 "不会真连,这里只验装配"),于是每次读都走
//     "连接失败 → 降级回源"那条分支 —— 测的是**降级**,不是缓存。
//     singleflight / null sentinel / 抖动三样,一行都没被断言过。
//  3. **真正在生产里跑的那个缓存因此另起炉灶**。authz 的 principal 缓存
//     (每个受保护请求都要走)嫌"这套只能配 Redis",自己实现了一遍
//     get/set + singleflight,于是三防缺两样 —— 没有 null 缓存
//     (查不存在的身份每次穿透打库)、没有 TTL 抖动(5 分钟同时到期即雪崩),
//     也没有任何指标:全平台唯一一个在跑的缓存,恰恰是唯一看不见的那个。
//
// 拆出 Backend 之后:三防跑在接口上;任何后端都能用;**用内存 fake 就能把
// 真路径测起来**(M90 的测试就是这么写的);authz 也能直接复用同一套三防。
//
// # 为什么不干脆让 authz 继续用自己的接口
//
// authz 当初用接口是为了「不直接依赖 go-redis」。M90 之后 authz 依赖的是
// cachex(而不是 go-redis 本身),依赖方向没变坏;而换来的好处是:
// 三防只有**一份**实现,principal 缓存第一次拿到 null 缓存 + 抖动 + 指标。
package cachex

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrMiss 缓存未命中。
//
// 用哨兵错误而不是 (value, bool, err) 三元组:未命中是**正常路径**而不是错误,
// 用哨兵可以让调用方用 errors.Is 判断,不必在每个分支里解包 bool。
// 与 redis.Nil 的关系:RedisBackend 负责把 redis.Nil 翻译成 ErrMiss,
// 于是包内代码不再出现任何 redis 专有语义。
var ErrMiss = errors.New("cachex: miss")

// Backend 缓存后端 —— 三防(singleflight / null sentinel / TTL 抖动)跑在它上面。
//
// 实现约定:
//   - Get 未命中必须返回 ErrMiss(不要返回别的错误,也不要用空串表示未命中)
//   - Set 的 ttl <= 0 表示"不过期"
//   - SetNX 的 ttl <= 0 表示"不过期"
//   - Expire 的 ttl <= 0 表示"取消过期"(不是"删掉这个 key")
//   - 所有实现都必须对 ctx 取消做出响应(不能拿着 ctx 阻塞到超时)
//
// ⚠️ **"ttl <= 0" 的语义是框架定的,不是各后端原生的** —— Redis 原生语义在这三处
//
//	都跟框架不一样(Set 不带 EX = 不过期 ✓,但 EXPIRE key 0 = **删掉 key**,
//	 而 SET key val EX -1 直接报错 "invalid expire time")。
//
// Redis 后端因此做了归一化(见 backend.go 里那三个方法的注释)。
// 2026-10-26 M96 之前,Redis 后端与内存后端在这三处行为**不一致** ——
// 单测只跑内存后端,于是没人发现。
type Backend interface {
	// Get 读一个 key。未命中返回 ("", ErrMiss)。
	Get(ctx context.Context, key string) (string, error)
	// Set 写一个 key。ttl <= 0 表示不过期。
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	// SetNX 仅当 key 不存在时写。写成功返回 true。ttl <= 0 表示不过期。
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	// Del 删一个或多个 key。
	Del(ctx context.Context, keys ...string) error
	// Expire 重设某个 key 的过期时间。ttl <= 0 表示取消过期(key 留着,不再过期)。
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// Scanner 可选能力:按**前缀**扫描 key。
//
// 单独成接口而不是塞进 Backend:扫描是 Redis 特有的命令,内存后端实现它
// 没有意义,硬塞进 Backend 等于逼每个实现写一个假扫描。
// Safe.DeletePrefix 通过类型断言探测 —— 不支持就当"没有可删的东西"(fail-open)。
//
// ⚠️ **prefix 是字面前缀,不是 glob pattern**(2026-10-26 M96 改)。
//
// 旧契约是"传 pattern",由 `Safe.DeletePrefix` 拼 `前缀 + "*"`。
// Redis 的 SCAN MATCH 走 **glob 语法**(`*` `?` `[...]` 都是通配符),于是:
//
//	DeletePrefix(ctx, "a*b")  →  pattern "a*b*"
//	  → 中间的 `*` 成了通配符 → "axb:2" 也被删了
//
// **症状是"删多了"** —— 别的业务的缓存被连带清掉,而且没有任何报错。
// 内存后端也不对:它取第一个 `*` 之前当前缀,`a*b*` 变成前缀 "a",范围更宽。
//
// ⚠️ **如实说:今天还没炸过** —— 现网两个失效前缀(`dict:`、`principal:<id>:`)
// 都不含 glob 元字符,所以这个 bug **是潜伏的**,不是"线上正在丢数据"。
// 但它是**契约级的错**:接口写的是"前缀",实现做的是"glob pattern",
// 下一个用带元字符前缀的调用方就会踩,而且**没有任何报错**。
//
// 现在契约是"字面前缀",**由会 glob 的那个后端(Redis)自己转义**。
type Scanner interface {
	// Scan 从 cursor 开始扫,返回本批 key 与下一个 cursor(0 表示扫完了)。
	//
	// ⚠️ prefix 是**字面量**:后端负责把它翻译成本后端的匹配语法,
	// 调用方不要往里塞通配符。
	Scan(ctx context.Context, cursor uint64, prefix string, count int64) ([]string, uint64, error)
}

// Pinger 可选能力:探活。
//
// 同样单独成接口:不是每个后端都有"ping"这个概念。
// Safe.PingL2 通过类型断言探测 —— 不支持就当"L2 未配置"。
type Pinger interface {
	// Ping 探活。通了返回 nil。
	Ping(ctx context.Context) error
}

// RedisBackend Backend 的 Redis 实现。
//
// 存在的意义只有一个:把 redis.Nil 翻译成 ErrMiss,让包内其余部分
// 不再知道 redis 的存在。**这是本包唯一真正发 Redis 命令的地方** ——
// `safe.go` / `build.go` 里出现的 `*redis.Client` 只是构造函数签名上的类型,
// 没有第二条命令从别处发出去。
type RedisBackend struct {
	rdb *redis.Client
}

// NewRedisBackend 构造 Redis 后端。rdb 为 nil 时各方法按"无后端"降级。
func NewRedisBackend(rdb *redis.Client) *RedisBackend { return &RedisBackend{rdb: rdb} }

// Get 读 key;未命中返回 ErrMiss。
func (b *RedisBackend) Get(ctx context.Context, key string) (string, error) {
	if b == nil || b.rdb == nil {
		return "", ErrMiss
	}
	v, err := b.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrMiss
		}
		return "", err
	}
	return v, nil
}

// Set 写 key。ttl <= 0 → 永不过期。
//
// ⚠️ **ttl < 0 必须归一化成 0**,不能直接传给 go-redis:
//
//   - `redis.KeepTTL == -1`,传 -1 会让 go-redis 发出 `SET k v KEEPTTL` ——
//     **保留旧 TTL**,而不是"不过期"。键会**比预期更早消失**。
//   - 其它负数会变成 `SET k v PX -5000` —— Redis 直接报错
//     "invalid expire time in 'set' command"。
//
// 框架契约是"ttl <= 0 = 不过期",所以这里统一收到 0(`SET k v`,不带 EX)。
func (b *RedisBackend) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if b == nil || b.rdb == nil {
		return nil
	}
	if ttl < 0 {
		ttl = 0
	}
	return b.rdb.Set(ctx, key, value, ttl).Err()
}

// SetNX 仅当 key 不存在时写。ttl <= 0 → 永不过期。
//
// ⚠️ 归一化的理由同 Set:go-redis 的 SetNX 也有 `case KeepTTL` 分支
// (`SET k v KEEPTTL NX`),而且负数会走到 `SET k v EX -N NX` 直接报错。
func (b *RedisBackend) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if b == nil || b.rdb == nil {
		return false, nil
	}
	if ttl < 0 {
		ttl = 0
	}
	return b.rdb.SetNX(ctx, key, value, ttl).Result()
}

// Del 删 key。
func (b *RedisBackend) Del(ctx context.Context, keys ...string) error {
	if b == nil || b.rdb == nil || len(keys) == 0 {
		return nil
	}
	return b.rdb.Del(ctx, keys...).Err()
}

// Expire 重设过期时间。ttl <= 0 → 取消过期(key 留着,不再过期)。
//
// ⚠️ **Redis 原生的 `EXPIRE key 0` 是"删掉这个 key"**,不是"取消过期"。
// Redis 的删除语义是 `DEL`;`EXPIRE key 0` / `EXPIRE key -N` 都是"立刻删掉"。
// 内存后端的 Expire 则是"ttl <= 0 → 清除过期时间,key 留着"。
//
// 框架契约选"取消过期"(与 Set 的 ttl<=0 语义一致,也与内存后端一致),
// 所以这里翻译成 `PERSIST`。
func (b *RedisBackend) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if b == nil || b.rdb == nil {
		return nil
	}
	if ttl <= 0 {
		return b.rdb.Persist(ctx, key).Err()
	}
	return b.rdb.Expire(ctx, key, ttl).Err()
}

// Scan 扫描**以 prefix 开头**的 key(实现 Scanner)。
//
// prefix 是**字面前缀**:Redis 的 SCAN MATCH 走 glob 语法,`*` `?` `[` `]`
// 都是通配符,所以这里必须先转义再拼尾部的 `*`。
// 不转义的后果:DeletePrefix("a*b") 会把 "axb:2" 之类**别的业务的 key** 一起删掉。
//
// 详见 Scanner 接口的注释。
func (b *RedisBackend) Scan(ctx context.Context, cursor uint64, prefix string, count int64) ([]string, uint64, error) {
	if b == nil || b.rdb == nil {
		return nil, 0, nil
	}
	return b.rdb.Scan(ctx, cursor, escapeGlob(prefix)+"*", count).Result()
}

// escapeGlob 把字面前缀里的 glob 元字符转义,使其能安全地当 SCAN MATCH 用。
//
// Redis 的 glob 元字符:`*` `?` `[` `]` `\`。
// Redis 的 glob 支持用 `\` 转义(`stringmatchlen` 里 `case '\\': ...`),
// 所以逐个前置反斜杠即可。
//
// ⚠️ 漏掉任何一个都会让前缀失效**删多了别人的 key**。
func escapeGlob(s string) string {
	if !strings.ContainsAny(s, `*?[]\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Ping 探活(实现 Pinger)。
func (b *RedisBackend) Ping(ctx context.Context) error {
	if b == nil || b.rdb == nil {
		return errors.New("cachex: Redis 后端未配置")
	}
	return b.rdb.Ping(ctx).Err()
}

// 编译期断言:RedisBackend 必须同时满足三个接口
var (
	_ Backend = (*RedisBackend)(nil)
	_ Scanner = (*RedisBackend)(nil)
	_ Pinger  = (*RedisBackend)(nil)
)
