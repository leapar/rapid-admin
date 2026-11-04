// 带类型的 GetOrLoad —— 让业务侧拿到 T 而不是 any
package cachex

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// TypedLoader 业务加载函数(带类型)
type TypedLoader[T any] func(ctx context.Context) (T, error)

// GetOrLoadTyped 带类型的 cache-aside 入口。
//
// **为什么必须存在:**Safe.GetOrLoad 返 any,而它内部把 JSON 反序列化进了 any,
// 所以一个 *model.SysDict 到调用方手上会变成 *map[string]any。业务要拿回结构体
// 只能自己再 Marshal 一次。这个摩擦正是 cachex 写完却零调用的原因 ——
// 框架提供的能力不能比手写 map 缓存更难用。
//
// 用法:
//
//	type Dict struct{ Code, Label string }
//	v, hit, err := cachex.GetOrLoadTyped(ctx, tl, "dict:"+code, 5*time.Minute,
//	    func(ctx context.Context) (*Dict, error) { return repo.Get(ctx, code) })
//
// 失效:cachex.GetOrLoadTyped 拿到的 key 前缀,改动数据后调
// tl.DeletePrefix(ctx, "dict:")。
//
// fail-open:缓存里的值反序列化不回来(发版改了结构、老 key 还在)时**不当错误**,
// 而是记 warn 后直接回源 —— 慢一点总比把错数据返给业务好。
func GetOrLoadTyped[T any](
	ctx context.Context, c *TwoLevel, key string, ttl time.Duration, loader TypedLoader[T],
) (T, bool, error) {
	var zero T

	if c == nil {
		// 没初始化 → 透传。保证"有没有缓存"不改变函数的成功/失败语义。
		v, err := loader(ctx)
		if err != nil && errors.Is(err, ErrNotFound) {
			return zero, false, nil
		}
		return v, false, err
	}

	raw, hit, err := c.GetOrLoad(ctx, key, ttl, func(ctx context.Context) (any, error) {
		v, lerr := loader(ctx)
		if lerr != nil {
			// ErrNotFound 原样透传 —— Safe 靠它决定写不写 null sentinel,
			// 在这里吞掉的话穿透防御就失效了(查不存在的东西会每次都打库)。
			return nil, lerr
		}
		return v, nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return zero, false, nil
		}
		return zero, false, err
	}
	if raw == nil {
		// null 占位命中 → "确认过确实没有",返零值
		return zero, hit, nil
	}

	out, cerr := reType[T](raw)
	if cerr == nil {
		return out, hit, nil
	}

	// 到这里说明缓存里存的是旧结构的值(schema drift)。
	// 不当错误返回:那会让一次发版把线上打成 500。回源重来。
	logger.L().Warn("cachex: 缓存值反序列化失败,回源重取(可能是发版改了结构)",
		zap.String("key", key), zap.Error(cerr))
	v, lerr := loader(ctx)
	if lerr != nil {
		if errors.Is(lerr, ErrNotFound) {
			return zero, false, nil
		}
		return zero, false, lerr
	}
	return v, false, nil
}

// reType 把 GetOrLoad 返出来的 any 还原成 T
//
// 多一次 Marshal/Unmarshal:any → JSON → T。值本来就是 JSON 存进来的,
// 所以这一步只是把它再组装回去,不是重复编码。
func reType[T any](raw any) (T, error) {
	var out T
	b, err := json.Marshal(raw)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	return out, nil
}

// Safe 版(只有 L2,没有 L1)
//
// 与 TwoLevel 版分开是因为 TwoLevel 不能为 nil 安全地"借用" ——
// 内部处处解引用 c.l1/c.safe。给纯 L2 场景一个直接入口,免得调用方为了用
// L2-only 还得先 NewTwoLevel(0, 0, safe) 包一层。
//
// ⚠️ **这条路径不走 any 往返**(2026-10-25 M90):JSON 直接反序列化进 T。
// TwoLevel 版(GetOrLoadTyped)因为 L1 存的是 any,命中 L2 时仍要经一次
// any 中转,uint64 超过 2^53 会掉精度 —— 需要 uint64 精度的场景
// (principal 缓存里的 UserID / GrantID)必须用这条 L2-only 路径。
func GetOrLoadTypedL2[T any](
	ctx context.Context, s *Safe, key string, ttl time.Duration, loader TypedLoader[T],
) (T, bool, error) {
	return getOrLoadJSON(ctx, s, key, ttl, func(ctx context.Context) (T, error) {
		return loader(ctx)
	})
}
