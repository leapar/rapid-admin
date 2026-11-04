// 缓存层配置 —— cachex(L1 本地 LRU + L2 Redis)的行为开关
package config

import "time"

// CacheConfig 缓存层配置。
//
// ⚠️ **这里配的是"缓存层本身",不是"哪些数据该缓存"。** 哪些数据缓存、TTL 多长、
// 改数据时怎么失效,是**调用方**传参进来的(cachex.GetOrLoad(ctx, key, ttl, loader)
// 的 key 和 ttl 都在调用处),配置里只管"缓存层跑成什么样"。原因:把 TTL 收到配置里
// 会让"这个 key 到底该不该缓存"变成全局约定,而它本质是每份数据自己的性质 ——
// 配置中心改了要立刻全网生效,字典条目一个月不动,同一个 TTL 对两者都是错的。
//
// Mode 的三档与 auth.revocation_mode / kv.mode 同构,判定点也统一在启动时一次:
//   - auto   有 Redis 用 Redis(带 L1),没有退纯 L1(默认)
//   - l1     强制只用本地 LRU,即使配了 Redis 也不连。多副本会各自持有不同副本,
//            收敛上界 = L1TTL(默认 10s)。
//            ⚠️ **只适合"陈旧十几秒无所谓"的数据** —— 凡是"改了必须马上全网生效"的
//            (配置中心、权限画像、字典),走 l1 模式就是**明确放弃**多副本一致性:
//            一台改了,别的机器要等 L1TTL 才知道。
//            (2026-10-26 M95 修正:这段注释原先写着"对'陈旧 10 秒无所谓'的数据
//            (字典、配置)反而更省",而字典/配置恰恰是**必须马上生效**的那类 ——
//            自相矛盾,照着它配就会踩坑。)
//            字典(M95)刻意只用 L2,就是为了不踩这条 —— 见 service/dict_cache.go。
//   - off    完全不启用,所有 GetOrLoad 直接透传 loader。
//            **默认不选它** —— 那等于每个调用点都要自己判断有没有缓存
//
// 降级方向一律 fail-open:Redis 挂了 / 没配,退化成"每次都查库",**不会**退化成
// "报错"。缓存是加速器不是依赖,把它变成硬依赖会让 Redis 的一次抖动升级成全站故障。
type CacheConfig struct {
	// Mode 缓存实现:auto | l1 | off(默认 auto)
	Mode string `mapstructure:"mode"`

	// Prefix 全局 key 前缀。同一套 Redis 跑多套系统时用它隔开,
	// 顺带让 DeletePrefix 有了天然的作用域边界(不会误删别人的 key)。
	// 留空则不加前缀 —— 单系统独占 Redis 时可以留空。
	Prefix string `mapstructure:"prefix"`

	// L1Cap L1(本地 LRU)容量,0 → 5000
	//
	// 按 entry 算而不是按字节,单个 entry 大小差异很大(一条字典项 vs 一棵权限树),
	// 按字节要引入权重,复杂度不值当。5000 条 × 平均 1KB ≈ 5MB,可接受。
	L1Cap int `mapstructure:"l1_cap"`

	// L1TTL L1 过期时间,0 → 10s
	//
	// **这是"多副本多久收敛"的唯一上界**:改了一条数据,别的副本最迟 L1TTL 之后
	// 才看到新的。所以凡是"改了必须马上全网生效"的数据(dict/config/authz),
	// 要么显式失效各副本,要么就别走 L1(用 L2-only)。
	//
	// (2026-10-26 M95:字典已经按这条规则走了 L2-only —— 见
	//  service/dict_cache.go。配置中心仍然直查库,没接缓存。)
	L1TTL time.Duration `mapstructure:"l1_ttl"`

	// NullTTL 穿透防御:null 占位 TTL,0 → 2min。设 0 表示禁用穿透防御
	NullTTL time.Duration `mapstructure:"null_ttl"`

	// Jitter 雪崩防御:TTL 随机抖动比例(0~0.5),0 → 0.2
	//
	// 批量灌缓存(比如启动预热)时,不加抖动会让几百个 key 在同一毫秒过期,
	// 下一瞬间全部回源把库打穿。
	Jitter float64 `mapstructure:"jitter"`
}
