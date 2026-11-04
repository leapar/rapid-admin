// 三防的**唯一实现**(泛型)—— 单飞 / 穿透占位 / TTL 抖动。
//
// # 为什么单独一个文件
//
// "三防"是这个包的核心能力,也是最容易被复制粘贴的地方。把它收敛成**一个**
// 泛型函数之后:
//
//   - Safe.GetOrLoad(any 版)      → 调它
//   - GetOrLoadTypedL2(泛型版)     → 调它
//   - 未来的任何新入口              → 也只能调它
//
// 换句话说,"三防只有一份实现"这句话是字面意义上的:包内没有任何第二处
// singleflight / null sentinel / jitteredTTL 的组合逻辑。
//
// # 为什么走泛型,而不是"先 any 再 reType"(2026-10-25 M90)
//
// 老实现是:JSON → any(map[string]any) → 再 JSON → T。
// 中间那一步把**所有整数都变成 float64** —— uint64 的高位精度就在这一步没了:
//
//	json.Marshal(float64(1e18)) → "1e+18" → json.Unmarshal 到 uint64 → 报错
//
// 于是"ID 一旦超过 2^53 就整个加载失败"。ID 现在还小,但这是那种
// "等它爆的时候已经上线三年"的陷阱 —— 而 principal 缓存里装的正是
// UserID / GrantID / ResourceID 这些 uint64。
//
// 泛型版把 JSON 直接反序列化进 T,中间不经过 any:精度不丢,还少一次编解码。
package cachex

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// getOrLoadJSON 三防核心。
//
// 流程:
//
//  1. 读后端 → 命中直接返(反序列化进 T)
//  2. 未命中 → singleflight 同 key 只放一个 goroutine 调 loader
//  3. loader 返 ErrNotFound → 写 null sentinel(短 TTL),挡住无效流量
//     loader 返正常值     → 写值(TTL 带随机抖动)
//     loader 返其它 error  → 不写缓存,返 error
//
// 返回值:(值, 是否命中缓存, error)
//
// ## 不变式:缓存配置不改变业务语义
//
// 无论后端在不在、null 占位开不开,loader 的 ErrNotFound 都一律翻译成
// "零值 + 无错"。"查过了,确实没有"是**事实**,不是故障 —— 它不该因为
// 缓存配置不同而一会儿是 nil 一会儿是 error。
func getOrLoadJSON[T any](
	ctx context.Context,
	s *Safe,
	key string,
	ttl time.Duration,
	loader func(ctx context.Context) (T, error),
) (T, bool, error) {
	var zero T

	// 没有后端 → 纯透传。没初始化缓存不该改变函数的成功/失败语义。
	//
	// ErrNotFound 在这里也要翻译成"零值 + 无错":它表示"查无此数据"而不是
	// "查询失败",有后端时走的就是这个语义(写 null sentinel 后返零值),
	// 没后端时不能反过来变成一个错误 —— 那会让"有没有缓存"改变函数的成败。
	if s == nil || s.backend == nil {
		v, err := loader(ctx)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return zero, false, nil
			}
			return zero, false, err
		}
		return v, false, nil
	}

	fkey := s.fkey(key)
	getStart := time.Now()

	// 1. 读后端
	raw, err := s.backend.Get(ctx, fkey)
	switch {
	case err == nil:
		if raw == NullSentinel {
			// null 占位 → "已确认查无此数据"
			recordOp("l2", "get", "null", time.Since(getStart))
			return zero, true, nil
		}
		var v T
		if jerr := json.Unmarshal([]byte(raw), &v); jerr == nil {
			recordOp("l2", "get", "hit", time.Since(getStart))
			return v, true, nil
		}
		// 反序列化失败:当 miss,记 warn,继续往下走 loader
		// (发版改了结构、老 key 还在 —— 慢一点总比把错数据返给业务好)
		logger.L().Warn("cachex: unmarshal failed, fallthrough to loader",
			zap.String("key", fkey))
	case errors.Is(err, ErrMiss):
		// miss:继续走 loader
	default:
		// 后端故障:降级直查,记 warn(不打 error 避免噪声)
		logger.L().Warn("cachex: get failed, fallthrough to loader",
			zap.String("key", fkey), zap.String("err", err.Error()))
		recordOp("l2", "get", "error", time.Since(getStart))
	}
	recordOp("l2", "get", "miss", 0)

	// 2. singleflight 防击穿
	res, sferr, _ := s.flight.Do(fkey, func() (any, error) {
		// double-check:同 key 第一个进入 flight 后,等拿到锁再查一次后端。
		// 极端场景(flight 期间另一个 caller 已经写完)这里直接命中,不再回源。
		if raw, err := s.backend.Get(ctx, fkey); err == nil {
			if raw == NullSentinel {
				return zero, nil
			}
			var v T
			if jerr := json.Unmarshal([]byte(raw), &v); jerr == nil {
				return v, nil
			}
		}

		val, lerr := loader(ctx)
		if lerr != nil {
			if errors.Is(lerr, ErrNotFound) {
				recordLoader("notfound")
				// 查无此数据:写 null sentinel 短 TTL,挡住后续无效流量。
				//
				// ⚠️ nullTTL=0 只该表示"不写占位",**不该**改变语义 ——
				// ErrNotFound 说的是"查过了,确实没有",那不是错误。
				// 如果这里把 nullTTL=0 走成"原样抛错",那么"占位开不开"就变成了
				// "函数成不成功" —— 缓存配置开始影响业务语义,这是最不该有的耦合。
				// (无后端那条路径也是这么处理的:零值 + 无错。)
				if s.nullTTL > 0 {
					setStart := time.Now()
					if serr := s.backend.Set(ctx, fkey, NullSentinel, s.nullTTL); serr != nil {
						logger.L().Warn("cachex: set null sentinel failed",
							zap.String("key", fkey), zap.String("err", serr.Error()))
						recordOp("l2", "set", "error", time.Since(setStart))
					} else {
						recordOp("l2", "set", "ok", time.Since(setStart))
					}
				}
				return zero, nil
			}
			recordLoader("error")
			return zero, lerr
		}
		recordLoader("ok")

		// 写缓存(雪崩防御:加随机抖动)
		b, merr := json.Marshal(val)
		if merr != nil {
			// 序列化失败:不写缓存,直接返业务值
			logger.L().Warn("cachex: marshal failed, skip cache",
				zap.String("key", fkey), zap.String("err", merr.Error()))
			return val, nil
		}
		jttl := s.jitteredTTL(ttl)
		setStart := time.Now()
		if serr := s.backend.Set(ctx, fkey, string(b), jttl); serr != nil {
			logger.L().Warn("cachex: set failed",
				zap.String("key", fkey), zap.String("err", serr.Error()))
			recordOp("l2", "set", "error", time.Since(setStart))
		} else {
			recordOp("l2", "set", "ok", time.Since(setStart))
		}
		return val, nil
	})
	if sferr != nil {
		return zero, false, sferr
	}
	v, _ := res.(T)
	return v, false, nil
}
