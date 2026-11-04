// Breaker 中间件单测 —— 熔断阈值 / 5xx 才算失败 / open 时快速失败
//
// # 本文件存在的理由
//
// M105 审计 §1.3 时发现:熔断器**一张用例都没有**(零覆盖),而它挂在
// biz.owner / biz.issue / biz.material / biz.upload / platform.wxtemplate
// 五组业务路由上 —— 一旦跳闸,这些接口全部 503。
//
// 三条要守的:
//  1. 阈值配成 0 的语义要**定死**(不因连续失败次数跳闸),而不是留一个
//     "看起来像没配、实际最激进"的坑(见 TestBreaker_ZeroThresholdNeverTrips)
//  2. open 之后要真的快速失败 503,而不是把请求放给下游
//  3. **熔断打开时,下游 handler 一次都不能再跑**(M105 修的那条)
package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	appcfg "rapid-admin/internal/config"
)

// resetBreaker 重置熔断器的进程级全局并按新配置初始化
//
// InitBreaker 内部用 sync.Once 保证"只初始化一次",那是给 main 用的;
// 测试里要反复换配置,只能直接重置这两个包级变量(同包,允许)。
func resetBreaker(t *testing.T, cfg appcfg.BreakerConfig) {
	t.Helper()
	mgr = nil
	mgrOnce = sync.Once{}
	InitBreaker(cfg)
	t.Cleanup(func() {
		mgr = nil
		mgrOnce = sync.Once{}
	})
}

// runBreaker 跑一次带熔断器的请求,handler 固定返 status
//
// ran 记录 handler 真正执行过的次数 —— 熔断的意义就是"不碰下游",
// 所以光看状态码不够,还要看 handler 到底跑没跑。
func runBreaker(t *testing.T, status int, ran *int) *ut.ResponseRecorder {
	t.Helper()
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	e.Use(Breaker("m105.test"))
	e.GET("/m105/breaker", func(ctx context.Context, c *app.RequestContext) {
		if ran != nil {
			*ran++
		}
		c.String(status, "handler")
	})
	return ut.PerformRequest(e, "GET", "/m105/breaker", nil)
}

// TestBreaker_ZeroThresholdNeverTrips threshold=0 的语义:不因连续失败次数跳闸
//
// # ⚠️ M105 的这条是**查证后改写**的,不是"修好的"
//
// 原先的用例写"配成 0 会永远跳闸"—— 那是**错的**。gobreaker v1.0.0 只在
// **失败之后**才问 ReadyToTrip:
//
//	func (cb *CircuitBreaker) onFailure(state State, now time.Time) {
//	    case StateClosed:
//	        cb.counts.onFailure()             // ← ConsecutiveFailures 先 ++
//	        if cb.readyToTrip(cb.counts) {   // ← 才问
//
// 所以 ReadyToTrip 拿到的 ConsecutiveFailures **恒 ≥ 1** →
// `>= 0` 与 `>= 1` 行为**完全一样**(都是"第一次失败就跳闸")。
//
// 证据:M105 的可逆探针把"阈值夹逼到 ≥1"那段逻辑整段拆掉,这个用例
// **没有变红** —— 正因为两个值的行为本来就一样。原来的用例只发成功请求,
// 而成功路径根本不问 ReadyToTrip,所以它压根没碰到这个分支。
//
// 现在把 0 的语义定死:**不因连续失败次数跳闸**。
// 要关整个熔断器请用 Enabled=false(全局开关)。
//
// 0 在任何配置文件里都没出现过(default=5),所以这个语义对现网零影响;
// 它消掉的是一个真实的坑 —— "配 0"看起来像"没配 / 不限制",
// 实际却是**最激进**的设置(第一次失败就跳闸),直觉正好相反。
func TestBreaker_ZeroThresholdNeverTrips(t *testing.T) {
	resetBreaker(t, appcfg.BreakerConfig{
		Enabled:             true,
		ConsecutiveFailures: 0, // ← 0 = 不按连续失败次数跳闸
		Interval:            60,
		Timeout:             60,
		HalfOpenMaxRequests: 1,
	})

	// 连续失败 3 次 —— 一次都不该跳闸
	for i := 1; i <= 3; i++ {
		w := runBreaker(t, http.StatusInternalServerError, nil)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("第 %d 次失败 = %d, want 500 —— consecutive_failures=0 不该跳闸", i, w.Code)
		}
	}
	// 之后再来的成功请求照常放行
	if w := runBreaker(t, http.StatusOK, nil); w.Code != http.StatusOK {
		t.Fatalf("成功请求 = %d, want 200", w.Code)
	}
}

// TestBreaker_InitWarnsAboutUnimplementedFailureRate 是 M105 修的那条
//
// `breaker.failure_rate_threshold` 是**死配置键**:
//   - 它在 BreakerConfig 里声明了、在两份 yaml 里都配了 0.5;
//   - 但**全仓没有任何代码读它** —— 熔断只按 ConsecutiveFailures 判定。
//
// 也就是说:运维在 yaml 里看到 `failure_rate_threshold: 0.5`,会理所当然地
// 以为"失败率到 50% 就跳闸",而实际上这个配置**完全不生效**,且没有任何提示。
//
// 修法:启动时打一条 WARN。**不实现它** —— 启用它等于给 5 组业务路由
// (biz.owner / biz.issue / biz.material / biz.upload / platform.wxtemplate)
// 加一条新的跳闸条件,那是行为变更,不该在一次审计修复里悄悄生效。
func TestBreaker_InitWarnsAboutUnimplementedFailureRate(t *testing.T) {
	logs := captureLogs(t) // 同包共享:ratelimit_test.go 里的 helper
	resetBreaker(t, appcfg.BreakerConfig{
		Enabled:              true,
		ConsecutiveFailures:  5,
		Interval:             60,
		Timeout:              60,
		HalfOpenMaxRequests:  1,
		FailureRateThreshold: 0.5, // ← 配了但不生效
	})

	found := false
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "failure_rate_threshold") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("failure_rate_threshold 配了但当前未实现,启动时没有任何提示 —— 运维会以为它生效了")
	}
}

// TestBreaker_OpensAfterConsecutiveFailures 连续失败到阈值 → 跳闸 → 快速失败 503
func TestBreaker_OpensAfterConsecutiveFailures(t *testing.T) {
	resetBreaker(t, appcfg.BreakerConfig{
		Enabled:             true,
		ConsecutiveFailures: 1,
		Interval:            60,
		Timeout:             60,
		HalfOpenMaxRequests: 1,
	})

	w := runBreaker(t, http.StatusInternalServerError, nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("第一次失败 = %d, want 500", w.Code)
	}
	w2 := runBreaker(t, http.StatusOK, nil)
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("熔断后 = %d, want 503 —— open 状态必须快速失败", w2.Code)
	}
}

// TestBreaker_OpenStateDoesNotCallDownstream 是 M105 修的那条(最重要的一条)
//
// hertz 的 `RequestContext.Next()`(app/context.go:830)是这么写的:
//
//	ctx.index++
//	for ctx.index < int8(len(ctx.handlers)) {
//		ctx.handlers[ctx.index](ctx, c)
//		ctx.index++
//	}
//
// 所以"不调 c.Next()"**并不能**阻止后面的 handler 执行 —— 外层那个 for
// 循环会接着往下走。真正能断链的只有 `c.Abort()`。
//
// 修之前熔断的快速失败分支没写 c.Abort(),实测:
//   - 熔断打开后,业务 handler **每次仍然被执行**(ran 从 1 → 2 → 3)
//   - handler 的响应盖掉了熔断的 503 → 客户端拿到 500 而不是 503
//   - 响应体是两段拼起来的畸形 JSON → 前端 JSON.parse 直接失败
//
// 也就是说熔断本该保护的下游,压根没被保护住。
func TestBreaker_OpenStateDoesNotCallDownstream(t *testing.T) {
	resetBreaker(t, appcfg.BreakerConfig{
		Enabled:             true,
		ConsecutiveFailures: 1,
		Interval:            60,
		Timeout:             60,
		HalfOpenMaxRequests: 1,
	})

	ran := 0
	w := runBreaker(t, http.StatusInternalServerError, &ran)
	if ran != 1 {
		t.Fatalf("第一次请求 handler 跑了 %d 次, want 1", ran)
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("第一次失败 = %d, want 500", w.Code)
	}

	// 熔断已经打开:这一次必须被挡在下游之前
	w2 := runBreaker(t, http.StatusOK, &ran)
	if ran != 1 {
		t.Fatalf("熔断打开后 handler 仍然跑了(累计 %d 次)—— 熔断没有保护住下游", ran)
	}
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("熔断后 = %d, want 503", w2.Code)
	}
	// 响应体必须是**纯 JSON**(熔断的 503),不能混进 handler 的内容
	if !strings.HasPrefix(w2.Body.String(), `{"code":50300`) {
		t.Fatalf("熔断响应体混进了别的东西:%q", w2.Body.String())
	}
}

// TestBreaker_ClientErrorsAreNotFailures 4xx 是客户端的问题,不该把熔断器打开
func TestBreaker_ClientErrorsAreNotFailures(t *testing.T) {
	resetBreaker(t, appcfg.BreakerConfig{
		Enabled:             true,
		ConsecutiveFailures: 1,
		Interval:            60,
		Timeout:             60,
		HalfOpenMaxRequests: 1,
	})

	w := runBreaker(t, http.StatusForbidden, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("第一次 = %d, want 403", w.Code)
	}
	w2 := runBreaker(t, http.StatusOK, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("第二次 = %d, want 200 —— 4xx 不该被算成下游故障", w2.Code)
	}
}

// TestBreaker_DisabledIsPassThrough 全局开关关掉时全部透传
func TestBreaker_DisabledIsPassThrough(t *testing.T) {
	resetBreaker(t, appcfg.BreakerConfig{Enabled: false})

	w := runBreaker(t, http.StatusInternalServerError, nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("HTTP 状态码 = %d, want 500", w.Code)
	}
	w2 := runBreaker(t, http.StatusOK, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, want 200 —— 熔断关掉时不该有 503", w2.Code)
	}
}

// TestBreaker_NotInitializedIsPassThrough 没调 InitBreaker 时也不能 panic
func TestBreaker_NotInitializedIsPassThrough(t *testing.T) {
	mgr = nil
	mgrOnce = sync.Once{}

	w := runBreaker(t, http.StatusOK, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP 状态码 = %d, want 200", w.Code)
	}
}
