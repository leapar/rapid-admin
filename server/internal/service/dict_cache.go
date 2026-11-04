// 字典缓存(M95)
//
// # 这份文件解决的是审计表里点名的两行
//
// `docs/框架层未完成项清单v1.md` §1.7.1 缓存层审计:
//
//	| **最后一公里缺口原封不动仍在** | 字典直查 DB(`service/role_impl.go:833`,
//	  写路径也没有失效调用)…… |
//	| **自相矛盾** | `internal/config/cache.go:41` 的注释白纸黑字把 dict/config
//	  列为"改了必须马上全网生效"的对象,而这两个恰恰一个都没接上 |
//
// 也就是说:**框架的缓存层写完了,业务一条都没接**。`cachex` 到现在只有
// `pkg/authz` 一个生产调用方(而且刻意不用全局池),`TwoLevel` /
// `GetOrLoadTyped` / `DeletePrefix` 全是"代码完备、零业务接入"。
//
// 字典是第一个接上去的 —— 它是"代码 enum 的中央仓库",每个页面的下拉都要读它,
// 读得最频繁;而且它天生带一条明确的写路径(见下面"失效"那一节)。
//
// # 为什么只用 L2(Redis),不用 L1(本地 LRU)
//
// `config/cache.go` 自己写着:
//
//	凡是"改了必须马上全网生效"的数据(dict/config/authz),
//	要么显式失效各副本,要么就别走 L1(用 L2-only)。
//
// L1 是**进程内**的:写路径的失效只能删掉本实例的 L1,别的副本仍持有旧值。
// "改完立刻生效"会退化成"最多 L1TTL 之后生效"(默认 10s)。
// 与 `authz.CachedLoader`(principal 缓存)同一判断。
//
// # 失效为什么必须早于广播(M94 的承诺靠它兜底)
//
// M94 已经让前端在收到 `dict.changed` 后**立刻重拉**。如果后端此刻还吐着
// 缓存里的旧字典,前端重拉回来的就是旧值 —— 用户看到的是"改了没生效",
// 正是 M94 要修的那个症状,而且这次还多了一层"我明明让它刷新了"。
//
// 所以写路径的顺序是死的:
//
//	写库 → 失效缓存 → 广播事件
//
// 这个顺序有单测守着(`role_dict_cache_test.go`),也有可逆探针守着。
//
// # key 方案
//
//	dict:items:<groupCode>   某个组的字典项
//	dict:items:              全部字典项(groupCode 为空 = 查全部)
//	dict:groups              分组列表(有数据的 group_code + 中文 label)
//
// ⚠️ **"查全部"的 key 是 `dict:items:`(空后缀),不是 `dict:items:all`** ——
//
//	后者会和一个真的叫 "all" 的 group_code 撞车(那一次查询会返回全部字典项)。
//	空后缀天然不可能与任何非空 group_code 相同。
//
// ⚠️ **失效用前缀删(`dict:`),不是逐个 key 删** —— 改一条字典时并不知道
//
//	有多少个 key 缓存了它(可能有 `dict:items:user_status`、
//	`dict:items:`、`dict:groups` 三个)。`DeletePrefix` 就是为这件事存在的
//	(见 `cachex/prefix.go` 的注释:"这是缓存一份实体能不能安全落地的前提")。
package service

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/seed"
)

const (
	// dictCacheKeyPrefix 字典缓存的 key 前缀。失效时按它删。
	//
	// ⚠️ **改这个前缀必须同 commit 改失效那一侧** —— 前缀是"写"和"删"之间
	// 唯一的约定,两边不一致的后果是"写进去的永远删不掉"(陈旧数据直到 TTL
	// 过期),而且**没有任何报错**。
	dictCacheKeyPrefix = "dict:"

	// dictCacheItemsKeyPrefix 字典项的 key 前缀(在前缀之后拼 group_code)
	dictCacheItemsKeyPrefix = "items:"

	// dictCacheGroupsKey 分组列表的 key
	dictCacheGroupsKey = "groups"

	// dictCacheMaxGroupLen group_code 的长度上限
	//
	// ⚠️ `sys_dict.group_code` 列宽是 64 —— 超过这个长度的 group_code
	//   **必然查不到任何东西**。给它建缓存条目是白占 Redis 内存,
	//   还让 SCAN 的 key 空间变大。直接回源,一次查空,完事。
	dictCacheMaxGroupLen = 64
)

// dictCacheTTL 字典缓存的 TTL
//
// ⚠️ **这是兜底,不是主要机制** —— 主要机制是写路径的显式失效。
//
//	TTL 只在"失效没生效"时兜底:失效失败(Redis 当时不可用 / 后端不支持
//	扫描)的情况下,陈旧数据最多活到 TTL。
//
// 10 分钟的取舍:
//
//	· 太短(比如 30s)→ DB 查询频率翻 20 倍,缓存白做
//	· 太长(比如 1h)→ 一次失效失败就陈旧一小时,而前端缓存是 30 分钟
//	  (`web/src/stores/dict.ts` 的 CACHE_TTL),两边一叠加,用户看到的
//	  陈旧时间比前端自己的窗口还长 —— 那就不该做后端缓存
//
// 10 分钟 < 前端 30 分钟,保证**后端缓存永远不是那个更长的陈旧窗口**。
//
// ⚠️ **为什么 TTL 是常量而不是配置项** —— `config/cache.go` 的注释写着:
//
//	"哪些数据缓存、TTL 多长、改数据时怎么失效,是**调用方**传参进来的",
//	并明确反对把 TTL 收进配置("那会让'这个 key 到底该不该缓存'变成全局
//	约定,而它本质是每份数据自己的性质")。这里就是那个"调用方"。
const dictCacheTTL = 10 * time.Minute

// DictCache 字典缓存。
//
// ⚠️ **允许为 nil** —— 单测里不装缓存是常态。所有方法都对 nil receiver 安全:
//
//	退化成"每次都回源",**不改变函数的成功/失败语义**(与 cachex 全体一致)。
type DictCache struct {
	// safe L2 客户端(nil = 没有缓存)
	safe *cachex.Safe
	// ttl 缓存有效期
	ttl time.Duration
}

// DictItemsLoader 加载某个组的字典项
type DictItemsLoader func(ctx context.Context) ([]model.SysDict, error)

// DictGroupsLoader 加载分组列表
type DictGroupsLoader func(ctx context.Context) ([]seed.DictGroupKV, error)

// NewDictCache 构造字典缓存。
//
// ⚠️ **safe 传 nil 是合法的**(没有 Redis / cache.mode=off)—— 退化成每次回源。
//
//	生产装配期传 `cachePool.Safe()`(见 cmd/server/main.go):
//	**刻意不取全局池本身**,因为全局池带 L1,而 L1 只能被本实例的失效清掉 ——
//	多副本下"改完立刻生效"会退化成"最多 10 秒后生效"。
func NewDictCache(safe *cachex.Safe) *DictCache {
	return &DictCache{safe: safe, ttl: dictCacheTTL}
}

// Items 取某个组的字典项(带缓存)。
//
// ⚠️ **loader 的错误一律透传** —— 缓存是加速器不是依赖:
//
//	"有没有缓存"不改变函数的成功/失败语义。
func (c *DictCache) Items(ctx context.Context, groupCode string, loader DictItemsLoader) ([]model.SysDict, error) {
	if c == nil {
		return loader(ctx)
	}
	if len(groupCode) > dictCacheMaxGroupLen {
		// 必然查不到 → 不值得缓存
		return loader(ctx)
	}
	v, _, err := cachex.GetOrLoadTypedL2(ctx, c.safe, c.itemsKey(groupCode), c.ttl,
		func(ctx context.Context) ([]model.SysDict, error) {
			return loader(ctx)
		})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// Groups 取分组列表(带缓存)。
func (c *DictCache) Groups(ctx context.Context, loader DictGroupsLoader) ([]seed.DictGroupKV, error) {
	if c == nil {
		return loader(ctx)
	}
	v, _, err := cachex.GetOrLoadTypedL2(ctx, c.safe, c.groupsKey(), c.ttl,
		func(ctx context.Context) ([]seed.DictGroupKV, error) {
			return loader(ctx)
		})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// Invalidate 作废全部字典缓存。
//
// ⚠️ **只删"字典"这一族** —— 前缀是 `dict:`,不会碰到别的业务缓存。
//
// ⚠️ **后端不支持扫描时这里返回 error,不是静默空操作** ——
//
//	`cachex.DeletePrefix` 在不支持 SCAN 的后端上返回 (0, nil),
//	语义是"没有可枚举的 key,不是错误"。对"清理一下不用的缓存"没问题,
//	对"改完必须马上生效"是**灾难**:什么都没删 = 数据没更新,
//	而且没有任何报错。
//	所以这里先问一句 `SupportsScan()`,不能扫就**报错**,绝不假装失效成功
//	—— 与 `authz.CachedLoader.InvalidateUser` 同一判断。
func (c *DictCache) Invalidate(ctx context.Context) error {
	if c == nil || c.safe == nil || !c.safe.L2Available() {
		// 没有 L2 → 没有缓存 → 没有东西要失效(不是错误)
		return nil
	}
	if !c.safe.SupportsScan() {
		return fmt.Errorf("service: 缓存后端不支持前缀扫描,字典缓存无法失效(期间会读到旧字典,前缀 %q)", dictCacheKeyPrefix)
	}
	_, err := c.safe.DeletePrefix(ctx, dictCacheKeyPrefix)
	return err
}

// itemsKey 某个组的字典项的缓存 key
//
// ⚠️ **"查全部"是空后缀(`dict:items:`),不是 `dict:items:all`** ——
//
//	后者会和一个真的叫 "all" 的 group_code 撞车。
func (c *DictCache) itemsKey(groupCode string) string {
	return dictCacheKeyPrefix + dictCacheItemsKeyPrefix + groupCode
}

// groupsKey 分组列表的缓存 key
func (c *DictCache) groupsKey() string {
	return dictCacheKeyPrefix + dictCacheGroupsKey
}
