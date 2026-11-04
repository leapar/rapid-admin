// 构造:从配置装配出可用的缓存客户端(唯一决策点)
package cachex

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// Mode 取值
const (
	// ModeAuto 有 Redis 用 Redis(带 L1),没有退纯 L1
	ModeAuto = "auto"
	// ModeL1 强制只用本地 LRU,即使配了 Redis 也不连
	ModeL1 = "l1"
	// ModeOff 完全不启用,GetOrLoad 一律透传 loader
	ModeOff = "off"
)

// Config 缓存层配置(由 cmd/server 从 config.CacheConfig 映射而来)
//
// 单独定义一份而不是直接吃 config.CacheConfig:pkg 层不依赖 config 层,
// 跟 kms.Config 的做法一致 —— 映射写在 main,策略留在包内。
type Config struct {
	Mode    string
	Prefix  string
	L1Cap   int
	L1TTL   time.Duration
	NullTTL time.Duration
	Jitter  float64
}

// Build 按配置装配缓存客户端。
//
// **这是"要不要连 Redis / 有没有 L2"的唯一决策点**,和 kvstore.Resolve、
// auth 的 revocation_mode 一样。散在各处判断就会出现"读路径判断了、写路径忘了"
// 这种最难查的不一致。
//
// 返回值可能是 nil(ModeOff),所有 cachex 入口对 nil receiver 都是安全的:
// 退化成每次都调 loader,不报错。
func Build(cfg Config, rdb *redis.Client) (*TwoLevel, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = ModeAuto
	}

	opts := make([]Option, 0, 3)
	if cfg.Prefix != "" {
		opts = append(opts, WithPrefix(cfg.Prefix))
	}
	if cfg.NullTTL != 0 {
		opts = append(opts, WithNullTTL(cfg.NullTTL))
	}
	if cfg.Jitter != 0 {
		opts = append(opts, WithJitter(cfg.Jitter))
	}

	switch mode {
	case ModeOff:
		return nil, nil

	case ModeL1:
		return NewTwoLevel(cfg.L1Cap, cfg.L1TTL, nil), nil

	case ModeAuto:
		if rdb == nil {
			logger.L().Warn("cachex: 未配置 Redis,退化为纯 L1 本地缓存(多副本各持一份,改动收敛上界 = l1_ttl)")
			return NewTwoLevel(cfg.L1Cap, cfg.L1TTL, nil), nil
		}
		return NewTwoLevel(cfg.L1Cap, cfg.L1TTL, New(rdb, opts...)), nil

	default:
		// 不静默当 auto 处理:"我配了 X,系统跑的是 Y"是最难查的一类配置问题。
		// 返回错误让 main 打 loud warn 并降级,而不是安静地跑成另一个模式。
		return nil, fmt.Errorf("cachex: 未知 mode %q(可选 auto|l1|off)", mode)
	}
}

// Describe 把当前装配结果渲染成一行日志字段,启动时打出来。
//
// "现在到底有没有缓存"必须能从启动日志一眼看出来 —— 之前 cachex 写了半天,
// 线上跑着零缓存而没人知道,就是因为没有任何地方说这件事。
func Describe(c *TwoLevel, mode string) []zap.Field {
	fields := []zap.Field{zap.String("mode", mode)}
	if c == nil {
		return append(fields, zap.Bool("enabled", false))
	}
	fields = append(fields,
		zap.Bool("enabled", true),
		zap.Bool("l2_enabled", c.safe.L2Available()),
		zap.Duration("l1_ttl", c.l1TTL),
	)
	if c.safe != nil {
		fields = append(fields, zap.String("prefix", c.safe.keyPrefix))
	}
	return fields
}

// Verify 在启动时探一次 L2,把"配了但连不上"变成启动日志里的一行,而不是
// 之后每个请求 WARN 一次。没配 Redis 时静默跳过(fail-open,不是错误)。
func Verify(ctx context.Context, c *TwoLevel) {
	if c == nil || !c.safe.L2Available() {
		return
	}
	if err := c.safe.PingL2(ctx); err != nil {
		logger.L().Warn("cachex: L2 连不通,已按 fail-open 降级为每次回源(不影响正确性,只影响性能)",
			zap.Error(err))
	}
}

/* ====================== 全局默认实例 ====================== */

// 全局只保留一个实例:每个调用点各建一个的话,L1 容量会被平摊,
// singleflight 也就只在实例内生效(同一进程里两个池子各查一次库,防击穿等于没防)。
//
// 做法与 kvstore.SetDefault / DefaultStore 一致 —— 包级单例,不在每个调用点做 DI。

var (
	defaultMu   sync.RWMutex
	defaultPool *TwoLevel
)

// SetDefault 登记全局实例。main 在 Build 之后调一次。
//
// 允许登记 nil(cache.mode=off):Default() 返回 nil,而所有 cachex 入口对 nil
// receiver 都是安全的,退化成每次回源。**"关掉缓存"不该要求调用方写 if。**
func SetDefault(c *TwoLevel) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultPool = c
}

// Default 取全局实例;没登记过返回 nil
func Default() *TwoLevel {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultPool
}

// Enabled 报告全局缓存是否真的开着(给业务侧打日志 / /readyz 用)
func Enabled() bool { return Default() != nil }
