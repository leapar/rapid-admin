// Per-route 熔断器(基于 sony/gobreaker)
// 文档 §1.3 #14 P0
package middleware

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

// breakerManager per-name 熔断器注册表
//   - sync.Map 保证并发安全(name 维度的 lazy-init)
//   - cfg 由 Init 注入;nil 时所有 Breaker() 中间件透传(不熔断)
type breakerManager struct {
	mu       sync.RWMutex
	breakers map[string]*gobreaker.CircuitBreaker
	cfg      config.BreakerConfig
}

var (
	mgr     *breakerManager
	mgrOnce sync.Once
)

// InitBreaker 注册全局熔断配置(由 main 启动时调一次)
//   - cfg.Enabled = false → 后续 Breaker() 调用全是透传
func InitBreaker(cfg config.BreakerConfig) {
	mgrOnce.Do(func() {
		mgr = &breakerManager{
			breakers: make(map[string]*gobreaker.CircuitBreaker),
			cfg:      cfg,
		}
	})
	if !cfg.Enabled {
		L().Info("breaker disabled (global)")
		return
	}
	L().Info("breaker enabled",
		zap.Uint32("consecutive_failures", cfg.ConsecutiveFailures),
		zap.Int("interval_seconds", cfg.Interval),
		zap.Int("timeout_seconds", cfg.Timeout),
		zap.Uint32("half_open_max_requests", cfg.HalfOpenMaxRequests),
	)
	// ⚠️ M105(2026-10-28):两个"配了但不生效/容易被误解"的配置,启动时说清楚。
	//
	// 1. consecutive_failures=0 → 不因连续失败次数跳闸(见 ReadyToTrip 的说明)。
	//    不 WARN 的话,配 0 的人会以为"阈值 0 = 立刻跳闸",而实际是"永不跳闸"。
	if cfg.ConsecutiveFailures == 0 {
		L().Warn("breaker: consecutive_failures=0 → 不因连续失败次数跳闸(熔断只剩 half-open 探测失败回跳);要关熔断请用 enabled=false")
	}
	// 2. failure_rate_threshold 是**预留字段**,当前实现不读它 ——
	//    熔断只按 consecutive_failures(连续失败次数)判定。
	//    配了不生效且**没有任何提示**,运维会以为"失败率到 50% 就跳闸"是真的。
	//
	//    这里只 WARN 不实现:启用它等于给 5 组业务路由(biz.owner / biz.issue /
	//    biz.material / biz.upload / platform.wxtemplate)加了一条新的跳闸条件,
	//    那是**行为变更**,不该在一次审计修复里悄悄生效。
	if cfg.FailureRateThreshold > 0 {
		L().Warn("breaker: failure_rate_threshold 当前**未实现** —— 熔断只按 consecutive_failures(连续失败次数)判定,该配置不生效",
			zap.Float64("failure_rate_threshold", cfg.FailureRateThreshold))
	}
}

// getOrCreate 按 name 取出或创建熔断器(lazy,first-write-wins)
func (m *breakerManager) getOrCreate(name string) *gobreaker.CircuitBreaker {
	m.mu.RLock()
	if cb, ok := m.breakers[name]; ok {
		m.mu.RUnlock()
		return cb
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if cb, ok := m.breakers[name]; ok { // double-check
		return cb
	}
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        name,
		MaxRequests: m.cfg.HalfOpenMaxRequests,
		Interval:    time.Duration(m.cfg.Interval) * time.Second,
		Timeout:     time.Duration(m.cfg.Timeout) * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// 连续失败达到阈值 → 跳闸
			//
			// # ⚠️ M105(2026-10-28):threshold=0 的语义在此**定死**
			//
			// gobreaker v1.0.0 只在**失败之后**才问 ReadyToTrip:
			//
			//	func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
			//	    case StateClosed:
			//	        cb.counts.onFailure()             // ← ConsecutiveFailures 先 ++
			//	        if cb.readyToTrip(cb.counts) {   // ← 才问
			//
			// 所以 ReadyToTrip 拿到的 ConsecutiveFailures **恒 ≥ 1**。
			// 于是 `>= threshold` 在 threshold=0 与 threshold=1 时
			// **行为完全一样**(都是"第一次失败就跳闸")。
			//
			// ⚠️ 本文件原先那段"配成 0 会永远跳闸"的注释是**错的**:
			// 它不是"永远跳闸",而是"第一次失败就跳闸",和配 1 没区别。
			// M105 的可逆探针把"夹逼到 ≥1"那段逻辑拆掉,用例**没有变红** ——
			// 正因为两个值的行为本来就一样。这条是**查证后撤回**的,
			// 不是"修好的"。撤回理由见 docs/框架层未完成项清单v1.md §1.3.1。
			//
			// 现在把 0 的语义定死为 **"不因连续失败次数跳闸"**:
			//
			//	enabled: false             → 整个熔断器关掉(全局开关)
			//	consecutive_failures: 0    → 不由连续失败次数决定跳闸
			//	consecutive_failures: ≥ 1  → 连续失败达到该次数才跳闸(主路径)
			//
			// 为什么是这个语义:0 在任何配置文件里都没出现过(default=5),
			// 对现网零影响;而它消掉了一个真实的坑 ——
			// "配 0"看起来像"没配 / 不限制",实际却是**最激进**的设置
			// (第一次失败就跳闸),两者的直觉差正好相反。
			//
			// 注意:half-open 探测失败会立刻回到 open —— 那条路径在 gobreaker
			// 内部,不受本回调影响。
			if m.cfg.ConsecutiveFailures == 0 {
				return false
			}
			return counts.ConsecutiveFailures >= m.cfg.ConsecutiveFailures
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			L().Warn("breaker state change",
				zap.String("name", name),
				zap.String("from", from.String()),
				zap.String("to", to.String()),
			)
		},
	})
	m.breakers[name] = cb
	return cb
}

// Breaker 熔断中间件 — 包同 name 的所有路由,共享熔断计数
//   - name 命名建议:resource.module(如 "biz.owner", "platform.config")
//   - cfg.Enabled=false / mgr 未初始化:直接透传(no-op)
//   - 跳闸(open)状态时新请求直接返 503
//   - 5xx 视为失败,4xx 视为成功(客户端错误不算下游故障)
func Breaker(name string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if mgr == nil || !mgr.cfg.Enabled {
			c.Next(ctx)
			return
		}
		cb := mgr.getOrCreate(name)

		_, err := cb.Execute(func() (any, error) {
			c.Next(ctx)
			// 跑完 next,看 status code 决定成功/失败
			status := c.Response.StatusCode()
			if status >= 500 {
				return nil, fmt.Errorf("upstream %d", status)
			}
			return nil, nil
		})

		// 区分错误类型:open 状态 → 熔断生效;其他 → handler 已自处理
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			L().Warn("breaker open, fast-fail",
				zap.String("name", name),
				zap.Error(err),
			)
			response.Fail(c, errcode.ServiceUnavailable, "服务暂时不可用,请稍后重试")
			// ⚠️ **M105(2026-10-28)补的:c.Abort() 不能省**
			//
			// 原来这里只写了响应就 return,没断链。后果是**熔断等于没熔**:
			//
			//	hertz 的 `RequestContext.Next()`(app/context.go:830)是这么写的:
			//	    ctx.index++
			//	    for ctx.index < int8(len(ctx.handlers)) {
			//	        ctx.handlers[ctx.index](ctx, c)
			//	        ctx.index++
			//	    }
			//
			// 也就是说:**"不调 c.Next()"并不会阻止后面的 handler 执行** ——
			// 外层那个 for 循环会接着往下走,业务 handler 照样被调用。
			// 真正能断链的只有 `c.Abort()`(它把 index 设成 abortIndex)。
			//
			// 实测(修之前):熔断打开之后,业务 handler **每次仍然被执行**
			// (handlerRan 从 1 → 2 → 3),而且它的响应会盖掉熔断的 503 ——
			// 客户端拿到的是 handler 的 500,而不是 503;响应体还是
			// `{"code":50300,...}` 与 handler 的 body **拼在一起**的畸形 JSON,
			// 前端 JSON.parse 直接失败。
			//
			// 也就是说熔断本该保护的下游,压根没被保护住。
			c.Abort()
			return
		}
		// 5xx 失败已自处理(handler 自己返 5xx),不再二次写响应
	}
}
