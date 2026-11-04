// Phase 2.4 — CachedLoader:缓存 + singleflight 防击穿
// 文档:后台权限模型设计V1.0.md §7.2 / §7.3
//
// # 2026-10-25 M90:实现换成 cachex(双轨合并)
//
// 之前这个文件自己实现了一遍 Redis get/set + singleflight,理由是"authz 不直接
// 依赖 go-redis"。代价是:
//
//   - **三防缺两样**:没有 null 缓存(查不到的身份每次穿透打库)
//     没有 TTL 抖动(5 分钟同时到期 → 一次批量改权限就把全部 principal
//     缓存推到同一点上齐刷刷过期,DB 被打穿)
//   - **没有任何指标**:全平台唯一一个真正在跑的缓存,恰恰是唯一看不见的那个
//
// 现在统一走 cachex:三防(singleflight / null 占位 / TTL 抖动)只有一份实现,
// 命中率还能进 app_cache_ops_total —— 权限缓存的命中率终于看得见。
//
// # 为什么不加本地 L1
//
// cachex 有 TwoLevel(L1 本地 LRU + L2),但 principal 缓存**只用 L2**。
// 原因在安全属性:L1 是进程内的,grant/revoke 触发的 Invalidate 只能删掉
// **本实例**的 L1,别的副本仍持有旧 principal —— "改完权限立刻生效"会退化成
// "最多 10 秒后生效"(L1 默认 TTL)。L1 换来的那点 RTT 收益,不值这个代价。
//
// # 2026-10-25 M91:失效粒度从"按身份"改成"按用户"
//
// 之前是 Invalidate(userID, identityID) —— 一个身份一条 key,调用方自己决定
// 清哪条。而**授权是挂在用户上的**(grant 记 grantee_user_id),调用方如果
// 只清主身份,这个用户的其它身份继续拿着旧授权 —— 最长一个 TTL(5 分钟)。
//
// 现在统一是 InvalidateUser(userID):一次清掉这个人所有身份的缓存。
// 详见 InvalidateUser 的注释。
package authz

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/pkg/logger"
)

// CachedLoader 在 DBLoader 上加缓存(单飞 + null 占位 + TTL 抖动)
//   - 缓存 key: principal:{userID}:{identityID}
//   - TTL: 5 分钟(可配)
//   - 写时(grant/revoke)按**用户**批量失效:InvalidateUser
//   - 缓存击穿:singleflight 保证同 key 多次请求只查一次 DB
//   - 缓存后端不可用:降级直查 DB,不打错误(用 logger 记 warn)
type CachedLoader struct {
	Inner Loader
	Cache *cachex.Safe // 缓存后端(nil = 不缓存,直查 DB)
	TTL   time.Duration
}

// NewCachedLoader 构造
func NewCachedLoader(inner Loader, cache *cachex.Safe, ttl time.Duration) *CachedLoader {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &CachedLoader{Inner: inner, Cache: cache, TTL: ttl}
}

// Load 先查缓存,miss 走 DBLoader(三防由 cachex 提供),结果回写缓存
//
// 返回 (nil, nil) 表示"确认过没有这个身份"(null 占位命中,或身份确实不存在)。
// 这与"加载失败"(返回 error)是**两回事**:上层对两者的处理一样(降级到
// role/user 判定),但前者说明"这个身份不存在"是**确定的事实**,不是故障。
func (c *CachedLoader) Load(ctx context.Context, userID, identityID uint64) (*Principal, error) {
	if c.Cache == nil {
		// 没配缓存 → 直查(单副本 dev 模式)
		return c.Inner.Load(ctx, userID, identityID)
	}
	key := cacheKey(userID, identityID)
	p, hit, err := cachex.GetOrLoadTypedL2(ctx, c.Cache, key, c.TTL,
		func(ctx context.Context) (*Principal, error) {
			return c.Inner.Load(ctx, userID, identityID)
		})
	if err != nil {
		return nil, err
	}
	if p == nil {
		// null 占位命中:已确认查无此身份
		return nil, nil
	}
	// ⚠️ **必须拷贝一份再改 CacheHit**。
	//
	// singleflight 路径下,同一个 *Principal 指针会被多个 goroutine 同时拿到
	// (第一个回源的 + 等着的)。直接写它的字段就是数据竞争 —— -race 会当场报:
	//
	//	WARNING: DATA RACE
	//	Write at ... cached_loader.go:75  (*CachedLoader).Load
	//
	// 浅拷贝就够:Principal 加载完之后就是只读的,没人会改它的 slice/map 内容,
	// 调用方拿到的这份拷贝只用来标 CacheHit。
	cp := *p
	cp.CacheHit = hit
	return &cp, nil
}

// InvalidateUser 清掉某用户**所有身份**的 principal 缓存(grant/revoke 时调用)
//
// # 为什么是"按用户"而不是"按身份"
//
// principal 缓存的 key 是 principal:{userID}:{identityID}(一个身份一条),
// 但**授权是挂在用户上的**:grant 记 grantee_user_id、override 记 user_id、
// feature flag 记 user_id。改一次授权 = 改"这个人" —— 他名下每个身份
// 缓存里的那份画像都带着旧授权。只清一个身份 → 换另一个身份进来照样能
// 用旧授权(最长一个 TTL,默认 5 分钟)。
//
// # 为什么用前缀失效,而不是"先查有哪些身份再逐个删"
//
// 逐个删要多打一次 DB,而且**查的那一瞬间之后新建的身份会漏掉** ——
// 漏掉的那条缓存继续带着旧授权,正是本轮要消灭的东西。
// 前缀失效(key 前缀 = principal:{userID}:)一次扫完,不漏。
//
// ⚠️ **前缀失效依赖后端支持 SCAN**。不支持时 cachex.DeletePrefix 会
// fail-open 返回 (0, nil) —— 语义是"没有可枚举的 key",但对"撤销授权"
// 来说就是**静默什么都没删**。所以这里先问一句 SupportsScan,拿不到
// 能力就**报错**,绝不假装失效成功。
func (c *CachedLoader) InvalidateUser(ctx context.Context, userID uint64) error {
	if c.Cache == nil || !c.Cache.L2Available() {
		// 没配缓存 → 没有可失效的东西(不是错误)
		return nil
	}
	if !c.Cache.SupportsScan() {
		return fmt.Errorf("authz: 缓存后端不支持前缀失效,无法清除用户 %d 的全部身份缓存", userID)
	}
	n, err := c.Cache.DeletePrefix(ctx, userCachePrefix(userID))
	if err != nil {
		return fmt.Errorf("invalidate principal cache for user %d: %w", userID, err)
	}
	logger.L().Debug("authz: principal cache invalidated",
		zap.Uint64("user_id", userID), zap.Int64("deleted", n))
	return nil
}

// userCachePrefix 该用户所有 principal key 的公共前缀
//
// ⚠️ **末尾的冒号不能省** —— 少了它,"principal:12:" 会匹配上
// "principal:123:456"(用户 123 的缓存),把不相干的人的授权画像一起删了。
// 失效删多了和没删一样危险。
func userCachePrefix(userID uint64) string {
	return fmt.Sprintf("principal:%d:", userID)
}

// cacheKey 拼接缓存 key
//
// ⚠️ 格式**不能随便改**:改了之后线上已有的缓存条目全部对不上(变成查不到
// → 回源 → 再写新格式),上线瞬间会有一波全量回源。key 格式属于隐式契约,
// 和服务端 Redis 里的存量数据绑在一起。
func cacheKey(userID, identityID uint64) string {
	return fmt.Sprintf("principal:%d:%d", userID, identityID)
}
