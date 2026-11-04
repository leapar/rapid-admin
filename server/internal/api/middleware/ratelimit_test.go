// RateLimit 中间件单测 —— 限流真的咬得住 / user 维度的桶键 / 降级要留痕
//
// # 本文件存在的理由
//
// M105 审计 §1.3 时发现:限流中间件**零覆盖**。而它是登录防爆破、
// 短信防刷这些接口的唯一防线 —— 限流不生效时,登录接口可以无限次尝试。
//
// 三条要守的:
//  1. **限流真的要咬住**(第 max+1 次必须 429)。
//     语义是"本次请求计入窗口",所以第 max 次放行、第 max+1 次拦。
//  2. **user 维度的桶键是用户**,不是 IP。
//     同一个用户换 IP(NAT 出口轮换、移动网络)不该绕过限流。
//  3. **降级要留痕**。dim=user 但 ctx 里没有 caller 时会退化成按 IP 限流,
//     这在公开端点是正常的;但"静默降级"会让"限流没按预期生效"根本查不出来。
//     M105 给它加了一条 WARN,这里断言这条 WARN 真的发出来。
package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"rapid-admin/internal/pkg/kvstore"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/service"
)

// useMemoryKV 把限流存储换成一份干净的内存实现,用例结束后换回新的
//
// 必须换新的而不是还原旧的:内存窗口是有状态的,还原旧实例等于把
// 上一条用例的计数带过来,断言会飘。
func useMemoryKV(t *testing.T) {
	t.Helper()
	kvstore.SetDefault(&kvstore.Store{
		Counter: kvstore.NewMemoryCounter(),
		Window:  kvstore.NewMemoryWindow(),
		Backend: "memory",
	})
	t.Cleanup(func() {
		kvstore.SetDefault(&kvstore.Store{
			Counter: kvstore.NewMemoryCounter(),
			Window:  kvstore.NewMemoryWindow(),
			Backend: "memory",
		})
	})
}

// captureLogs 把全局 logger 换成可观测的,返回日志收集器
func captureLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, logs := observer.New(zapcore.WarnLevel)
	old := logger.Default
	logger.Default = zap.New(core)
	t.Cleanup(func() { logger.Default = old })
	return logs
}

// runRL 跑一次带限流中间件的请求
//
//   - scene: 限流场景名(进桶键)
//   - dim:   限流维度("ip" / "user")
//   - ip:    这次请求的客户端 IP(测试里用假 clientIPFunc 注入)
//   - caller: 非 nil 时把 caller 塞进 ctx(user 维度才有意义)
func runRL(t *testing.T, scene string, max int, dim, ip string, caller *service.Caller) *ut.ResponseRecorder {
	t.Helper()
	e := route.NewEngine(config.NewOptions([]config.Option{}))
	// 假对端地址:让每个用例能精确控制"这次请求来自哪个 IP"。
	//
	// ⚠️ 不能用 `e.SetClientIPFunc(...)`:ut.PerformRequest 走的是
	// `engine.NewContext()` 而不是 `engine.allocateContext()`,
	// 后者才会把 engine.clientIPFunc 装到 ctx 上 —— 于是 SetClientIPFunc
	// 在这个测试夹具里**不生效**,所有请求的 IP 会都是 0.0.0.0。
	// 改成塞一个假 conn,让 `c.ClientIP()` 走到 `ctx.RemoteAddr()`。
	e.Use(func(ctx context.Context, c *app.RequestContext) {
		c.SetConn(&fakeConn{remote: &net.TCPAddr{IP: net.ParseIP(ip), Port: 12345}})
		c.Next(ctx)
	})
	if caller != nil {
		// 必须排在限流**之前**:限流是从 ctx 里取 caller 的
		e.Use(func(ctx context.Context, c *app.RequestContext) {
			c.Next(service.WithCaller(ctx, caller))
		})
	}
	e.Use(RateLimit(scene, max, time.Minute, dim))
	e.GET("/m105/rl", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	return ut.PerformRequest(e, "GET", "/m105/rl", nil)
}

// TestRateLimit_BlocksAfterMax 是最基本的一条:第 max+1 次必须 429
func TestRateLimit_BlocksAfterMax(t *testing.T) {
	useMemoryKV(t)

	for i := 1; i <= 2; i++ {
		w := runRL(t, "m105.basic", 2, "ip", "10.0.0.1", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求 = %d, want 200 —— 限流过早咬住了", i, w.Code)
		}
	}
	w := runRL(t, "m105.basic", 2, "ip", "10.0.0.1", nil)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 3 次请求 = %d, want 429 —— 限流没咬住", w.Code)
	}
}

// TestRateLimit_UserDimensionUsesUserBucket user 维度的桶键是用户,不是 IP
//
// 同一个用户换 IP 不该绕过限流 —— 这正是"dim=user"存在的理由
// (否则同一 NAT 出口的人互相挤兑,换出口就能重来)。
func TestRateLimit_UserDimensionUsesUserBucket(t *testing.T) {
	useMemoryKV(t)

	caller := &service.Caller{UserID: 42, Username: "u"}

	// 两个不同的 IP,同一个用户
	w1 := runRL(t, "m105.user", 2, "user", "10.0.0.1", caller)
	if w1.Code != http.StatusOK {
		t.Fatalf("第 1 次 = %d, want 200", w1.Code)
	}
	w2 := runRL(t, "m105.user", 2, "user", "10.0.0.2", caller)
	if w2.Code != http.StatusOK {
		t.Fatalf("第 2 次 = %d, want 200", w2.Code)
	}
	// 第三次换个 IP —— 按用户限流的话必须照样被拦
	w3 := runRL(t, "m105.user", 2, "user", "10.0.0.3", caller)
	if w3.Code != http.StatusTooManyRequests {
		t.Fatalf("第 3 次(换了 IP)= %d, want 429 —— user 维度的桶键不是用户,换个 IP 就绕过了", w3.Code)
	}
}

// TestRateLimit_OtherUsersHaveSeparateBuckets 换个用户就该换个桶
//
// 上一条的反向守卫:如果所有用户共用一个桶,上一条会"因为别的原因"变红,
// 两条一起才构成守卫。
func TestRateLimit_OtherUsersHaveSeparateBuckets(t *testing.T) {
	useMemoryKV(t)

	caller := &service.Caller{UserID: 42}
	for i := 1; i <= 2; i++ {
		if w := runRL(t, "m105.sep", 2, "user", "10.0.0.1", caller); w.Code != http.StatusOK {
			t.Fatalf("用户 42 第 %d 次 = %d, want 200", i, w.Code)
		}
	}
	other := &service.Caller{UserID: 99}
	if w := runRL(t, "m105.sep", 2, "user", "10.0.0.1", other); w.Code != http.StatusOK {
		t.Fatalf("用户 99 的第一次请求 = %d, want 200 —— 不同用户共用了同一个桶", w.Code)
	}
}

// TestRateLimit_UserDimensionDegradesToIPWithoutCaller 没有 caller 时降级成按 IP
//
// 降级本身要保留(公开端点没有 caller 是正常的),但必须留痕。
// 这条断言两件事:降级后的行为(不同 IP 不同桶)+ 那条 WARN 真的发出来了。
func TestRateLimit_UserDimensionDegradesToIPWithoutCaller(t *testing.T) {
	useMemoryKV(t)
	logs := captureLogs(t)

	// 同一个 IP 打满
	for i := 1; i <= 2; i++ {
		if w := runRL(t, "m105.degrade", 2, "user", "10.0.0.1", nil); w.Code != http.StatusOK {
			t.Fatalf("第 %d 次 = %d, want 200", i, w.Code)
		}
	}
	if w := runRL(t, "m105.degrade", 2, "user", "10.0.0.1", nil); w.Code != http.StatusTooManyRequests {
		t.Fatalf("同 IP 第 3 次 = %d, want 429", w.Code)
	}

	// 换个 IP —— 降级成按 IP 之后就是另一个桶,应该放行
	if w := runRL(t, "m105.degrade", 2, "user", "10.0.0.2", nil); w.Code != http.StatusOK {
		t.Fatalf("换 IP 后 = %d, want 200 —— 降级成按 IP 后不该共用桶", w.Code)
	}

	// 降级必须留痕
	found := false
	for _, e := range logs.All() {
		if strings.Contains(e.Message, "ratelimit") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("降级成按 IP 时没有留下任何日志 —— '限流没按预期生效'会彻底静默")
	}
}
