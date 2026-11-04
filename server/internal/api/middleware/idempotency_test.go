package middleware

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/idempotentx"
	"rapid-admin/internal/pkg/response"
)

// ── M97(2026-10-26)可逆探针记录 ───────────────────────────────────────
//
//   拆法                                        必须变红
//   ────────────────────────────────────────  ─────────────────────────────────
//   P1 cacheableResult 去掉 IsAborted 判据         ...DownstreamRefusal/500 权限校验失败
//   P2 cacheableResult 去掉 401/403/429 判据      ...HandlerRefusal_IsNotCached
//   P3 中间件永远走缓存分支(回到旧逻辑)           ...DownstreamRefusal_IsNotCached
//   P4 panic 时不释放锁                           ...Panic_ReleasesLock
//
//   4/4 全部变红。
//
// ⚠️ **P3 的拆法要小心**:不能把整段判据删掉(那样 `cacheableResult` 变死代码
// 虽然能编过,但拆法就不是"回到旧逻辑"了),要把 `if !cacheableResult(c, status)`
// 换成 `if false` —— 让它永远走缓存分支,而且**编译得过、代码仍然被引用**。
//
// ⚠️ **P1 只能靠 500 那条子用例盯住**:401/403/429 那几条同时被"状态码判据"
// 挡住,去掉 IsAborted 判据它们照样绿 —— 这就是"被变异的条件不是唯一在挡的"
// 那条坑(见 [[mutation-probe-discipline]] 第五种失败模式)。
// ───────────────────────────────────────────────────────────────────────

// idemFixture 一个"幂等中间件 + 可控下游"的测试夹具
type idemFixture struct {
	engine *route.Engine
	mgr    *idempotentx.Manager
	mr     *miniredis.Miniredis
}

// newIdemFixture 造夹具
//
// pre:挂在幂等中间件**之前**。真实链路里是 BodyLimit / RequestGzip / Recover
// 这些全局中间件 —— 注意它们拒绝时幂等中间件压根不会执行(引擎的 for 循环已被 Abort 打断)。
//
// post:挂在幂等中间件**之后**。真实链路里是 Auth / CSRF / RateLimit 这些
// 组级中间件 —— **它们的拒绝会被幂等中间件看见**,这正是 M97 的坑。
func newIdemFixture(t *testing.T, pre []app.HandlerFunc, post []app.HandlerFunc) *idemFixture {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	mgr := idempotentx.New(rdb, 30*time.Second, 24*time.Hour)
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	for _, h := range pre {
		engine.Use(h)
	}
	engine.Use(idempotencyWith(mgr))
	for _, h := range post {
		engine.Use(h)
	}
	return &idemFixture{engine: engine, mgr: mgr, mr: mr}
}

// post 发一次带幂等键的写请求
func (f *idemFixture) post(t *testing.T, key string) *ut.ResponseRecorder {
	t.Helper()
	return ut.PerformRequest(f.engine, "POST", "/x", &ut.Body{},
		ut.Header{Key: "Idempotency-Key", Value: key})
}

// TestM97_Idempotency_DownstreamRefusal_IsNotCached 「业务没跑成」不许占幂等槽位
//
// # 这条是 M97 的核心回归网
//
// 旧实现把 200~599 一律当"幂等结果"缓存。而 401 是**鉴权中间件**产生的
// (Auth 是组级中间件,位于幂等中间件的 c.Next 之内),于是:
//
//	客户端带 key K 发写请求 → 401(access token 刚好过期)
//	  → 旧实现:把 401 当成"结果"缓存 24h
//	  → 前端 axios 刷新 token 后**用同一个 config 重发**(同一个 key)
//	  → 旧实现:命中缓存,把 401 原样重放
//	  → 业务永远不执行;前端第二次收到 401 又走 handleUnauthorized(),
//	    把用户踢回登录页 —— 尽管 token 刚刷新成功、其实完全有效
//
// 症状就是"偶尔被踢回登录页",必现于"token 刚好过期时点提交"那一刻。
//
// 现在:被拒的响应**不缓存**,锁也释放 —— 同一个 key 重试能真的跑起来。
func TestM97_Idempotency_DownstreamRefusal_IsNotCached(t *testing.T) {
	cases := []struct {
		name string
		code int
		fail func(c *app.RequestContext)
	}{
		{"401 未登录", http.StatusUnauthorized, func(c *app.RequestContext) {
			response.Fail(c, errcode.Unauthorized, "未登录")
		}},
		{"403 没有权限", http.StatusForbidden, func(c *app.RequestContext) {
			response.Fail(c, errcode.Forbidden, "没有权限")
		}},
		{"429 请求过于频繁", http.StatusTooManyRequests, func(c *app.RequestContext) {
			response.Fail(c, errcode.TooManyRequests, "请求过于频繁")
		}},
		// ⚠️ 这一条**只能**被 IsAborted() 那条判据挡住 —— 500 不在
		// "拒绝语义状态码"那张表里(401/403/413/429)。
		// 它对应的是 RBAC 里 casbin 异常那条分支(errcode.Internal → 500,
		// "权限校验失败"):那是**框架自己拒了**,业务压根没跑,却是 5xx。
		// 按状态码看它是"业务失败,缓存起来",于是客户端拿同一个 key 重试
		// 会被重放成同一个 500 —— 一次 casbin 抖动让这个写操作 24h 发不出去。
		{"500 权限校验失败(casbin 异常)", http.StatusInternalServerError, func(c *app.RequestContext) {
			response.Fail(c, errcode.Internal, "权限校验失败")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newIdemFixture(t, nil, nil)
			refuse := true
			f.engine.Use(func(ctx context.Context, c *app.RequestContext) {
				if !refuse {
					c.Next(ctx)
					return
				}
				tc.fail(c)
				c.Abort()
			})
			ran := 0
			f.engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
				ran++
				c.String(http.StatusOK, "ok")
			})

			w := f.post(t, "k1")
			if w.Code != tc.code {
				t.Fatalf("第一次应返 %d,got %d (body=%q)", tc.code, w.Code, w.Body.String())
			}
			if ran != 0 {
				t.Fatalf("被拒时业务不该跑,got ran=%d", ran)
			}

			// 同一个 key 重试(前端 401 刷新后就是这么做的:同一个 config)
			refuse = false
			w = f.post(t, "k1")
			if w.Code != http.StatusOK || w.Body.String() != "ok" {
				t.Fatalf("被拒的响应不该占掉幂等槽位:重试应真的执行,got code=%d body=%q",
					w.Code, w.Body.String())
			}
			if ran != 1 {
				t.Fatalf("业务应执行 1 次,got %d", ran)
			}
		})
	}
}

// TestM97_Idempotency_HandlerRefusal_IsNotCached handler 层的前置守卫也要让出槽位
//
// handler.RequireCaller → 401("未登录")、asset.ListMyAssigns → 403(IDOR 阻断)
// 这类守卫**不调 c.Abort()**(那只是 handler 返回),但同样是在任何写操作
// 之前就返回了 —— 所以光靠 IsAborted() 抓不住,必须靠状态码那条判据。
//
// 这条同时是"别矫枉过正"的边界:状态码判据只能覆盖"拒绝"语义
// (401/403/413/429),不能把业务自己的 400 也一起放过(见下一条)。
func TestM97_Idempotency_HandlerRefusal_IsNotCached(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		f := newIdemFixture(t, nil, nil)
		refuse := true
		ran := 0
		f.engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
			ran++
			if refuse {
				c.JSON(code, map[string]any{"code": code})
				return
			}
			c.String(http.StatusOK, "ok")
		})

		w := f.post(t, "k1")
		if w.Code != code {
			t.Fatalf("第一次应返 %d,got %d", code, w.Code)
		}
		if ran != 1 {
			t.Fatalf("handler 应跑过 1 次(它自己拒的),got %d", ran)
		}

		refuse = false
		w = f.post(t, "k1")
		if w.Code != http.StatusOK || w.Body.String() != "ok" {
			t.Fatalf("handler 拒的响应不该占掉幂等槽位:重试应真的执行,got code=%d body=%q",
				w.Code, w.Body.String())
		}
		if ran != 2 {
			t.Fatalf("重试时业务应再执行一次,got ran=%d", ran)
		}
	}
}

// TestM97_Idempotency_Business2xx_IsCachedAndReplayed 成功的写必须能重放
//
// 这是幂等**要保的东西**:同 key 第二次请求拿到第一次的结果,业务不重跑。
// M97 改的是"什么该缓存",不能把"该缓存的"也一起改没了。
func TestM97_Idempotency_Business2xx_IsCachedAndReplayed(t *testing.T) {
	f := newIdemFixture(t, nil, nil)
	ran := 0
	f.engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		ran++
		c.String(http.StatusOK, "created")
	})

	w := f.post(t, "k1")
	if w.Code != http.StatusOK || w.Body.String() != "created" {
		t.Fatalf("第一次应 200,got code=%d body=%q", w.Code, w.Body.String())
	}

	w = f.post(t, "k1")
	if w.Code != http.StatusOK || w.Body.String() != "created" {
		t.Fatalf("重放应拿到同样的结果,got code=%d body=%q", w.Code, w.Body.String())
	}
	if ran != 1 {
		t.Fatalf("业务只能跑 1 次,got %d —— 幂等没生效", ran)
	}
	if got := w.Header().Get("Idempotent-Replay"); got != "true" {
		t.Fatalf("重放应带 Idempotent-Replay: true,got %q", got)
	}
}

// TestM97_Idempotency_Business4xx_IsCached 业务自己的 4xx **要**缓存
//
// 设计意图:业务 handler 失败也缓存(返业务错误码)—— 避免客户端重试 5 次
// 每次都跑一遍业务。400 是"参数不对",重试多少次都一样。
//
// 这条是"别矫枉过正"的另一头:M97 只把**拒绝语义**的状态码(401/403/413/429)
// 排除在外,不能顺手把整个 4xx 桶都放过。
func TestM97_Idempotency_Business4xx_IsCached(t *testing.T) {
	f := newIdemFixture(t, nil, nil)
	ran := 0
	f.engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		ran++
		c.JSON(http.StatusBadRequest, map[string]any{"code": 40000, "message": "参数不对"})
	})

	w := f.post(t, "k1")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("第一次应返 400,got %d", w.Code)
	}
	w = f.post(t, "k1")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("业务 400 应被缓存并重放,got %d", w.Code)
	}
	if ran != 1 {
		t.Fatalf("业务 400 缓存后不该重跑,got ran=%d", ran)
	}
}

// TestM97_Idempotency_InFlight_Returns409 同 key 并发 → 409
//
// 409 的意思是"等一下再试",不是"失败了"。这条是 M97 之前就有的行为,
// 改缓存策略时不能把它改坏(尤其:被拒的请求释放锁之后,锁的语义不能变)。
func TestM97_Idempotency_InFlight_Returns409(t *testing.T) {
	f := newIdemFixture(t, nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	f.engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		once.Do(func() { close(started) })
		<-release
		c.String(http.StatusOK, "ok")
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ut.PerformRequest(f.engine, "POST", "/x", &ut.Body{},
			ut.Header{Key: "Idempotency-Key", Value: "k1"})
	}()
	<-started

	w := f.post(t, "k1")
	if w.Code != http.StatusConflict {
		t.Fatalf("同 key 并发应返 409,got %d (body=%q)", w.Code, w.Body.String())
	}
	close(release)
	wg.Wait()
}

// TestM97_Idempotency_Panic_ReleasesLock handler 崩了必须把锁还回去
//
// ⚠️ **这是 M97 修的第二个真 bug**。
//
// 旧实现只在"状态码异常"分支里 Release 锁。而 panic 走的是 **unwind** ——
// `c.Next(ctx)` 之后的代码**根本不执行**,于是:
//   - 业务崩了 → 锁留在 Redis 里(TTL 30s)
//   - 客户端拿同一个 key 重试 → 抢不到锁 → 拿到 **409 "request in flight"**
//   - 而实际上**没有任何请求在跑**
//
// 后果:一次 panic 让这个写操作在 30 秒内彻底发不出去,而且报的是
// "并发冲突"这种完全误导人的错。
//
// 注:测试里必须挂 Recover(而且要挂在幂等中间件**之前**,与真实 router
// 同序 —— router.go 里 Recover 在第 63 行、Idempotency 在第 89 行),
// 否则 panic 会直接冲出 PerformRequest 把测试进程带崩。
func TestM97_Idempotency_Panic_ReleasesLock(t *testing.T) {
	f := newIdemFixture(t, []app.HandlerFunc{Recover()}, nil)
	ran := 0
	f.engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		ran++
		panic("boom")
	})

	w := f.post(t, "k1")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic 应由 Recover 兜成 500,got %d", w.Code)
	}

	// 锁必须已经还回去了:同一个 key 再来一次不该是 409
	w = f.post(t, "k1")
	if w.Code == http.StatusConflict {
		t.Fatal("panic 后锁没释放 —— 客户端重试只会拿到 409(而且没有任何请求在跑)")
	}
	if ran != 2 {
		t.Fatalf("第二次请求应真的进到 handler,got ran=%d", ran)
	}
}

// TestM97_Idempotency_Get_IsIgnored GET 不受幂等影响
func TestM97_Idempotency_Get_IsIgnored(t *testing.T) {
	f := newIdemFixture(t, nil, nil)
	ran := 0
	f.engine.GET("/x", func(ctx context.Context, c *app.RequestContext) {
		ran++
		c.String(http.StatusOK, "ok")
	})

	for i := 0; i < 2; i++ {
		w := ut.PerformRequest(f.engine, "GET", "/x", &ut.Body{},
			ut.Header{Key: "Idempotency-Key", Value: "k1"})
		if w.Code != http.StatusOK {
			t.Fatalf("GET 应放行,got %d", w.Code)
		}
	}
	if ran != 2 {
		t.Fatalf("GET 不该被幂等挡,got ran=%d", ran)
	}
}

// TestM97_Idempotency_NoKey_IsPassThrough 没带 key 的写请求照常执行
func TestM97_Idempotency_NoKey_IsPassThrough(t *testing.T) {
	f := newIdemFixture(t, nil, nil)
	ran := 0
	f.engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		ran++
		c.String(http.StatusOK, "ok")
	})

	for i := 0; i < 2; i++ {
		w := ut.PerformRequest(f.engine, "POST", "/x", &ut.Body{})
		if w.Code != http.StatusOK {
			t.Fatalf("无 key 应放行,got %d", w.Code)
		}
	}
	if ran != 2 {
		t.Fatalf("无 key 时业务应各跑一次,got ran=%d", ran)
	}
}
