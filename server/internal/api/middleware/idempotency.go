// Idempotency-Key 框架级中间件 — §1.3 #17 P1
//
// 行为:
//  1. 读 Idempotency-Key 头;空 → 跳过
//  2. method ∉ {POST, PUT, PATCH, DELETE} → 跳过
//  3. 调 idempotentx.Manager.AcquireOrGet
//     - 拿到锁 → c.Next → 业务跑完后按"跑没跑成"决定缓存还是释放锁
//     - 没拿到 + 命中结果 → 直接返缓存的 body + status
//     - 没拿到 + ErrInFlight → 返 409 Conflict
//
// 配合客户端:前端 axios 拦截器已自动为 POST/PUT/PATCH/DELETE 注入
//
// 用法:
//
//	idempotentx.Init(rdb, 30*time.Second, 24*time.Hour)
//	h.Use(middleware.Idempotency())
package middleware

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"rapid-admin/internal/pkg/idempotentx"
	"rapid-admin/internal/pkg/response"
)

// Idempotency 框架级幂等中间件
func Idempotency() app.HandlerFunc { return idempotencyWith(nil) }

// idempotencyWith 用指定的 manager 构造幂等中间件;manager 为 nil 时
// 每次请求现取全局(idempotentx.M())。
//
// 为什么要留这个口子:全局 manager 是**进程级单例**,测试里直接 Init 覆盖它
// 会污染同包其它用例(比如"没 Init 时必须完全放行"那条)。注入一个独立实例
// 就完全不用碰全局。
func idempotencyWith(m *idempotentx.Manager) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		mgr := m
		if mgr == nil {
			mgr = idempotentx.M()
		}
		if mgr == nil {
			c.Next(ctx)
			return
		}
		// 1. 仅写方法生效
		method := string(c.Method())
		if !isWriteMethod(method) {
			c.Next(ctx)
			return
		}
		// 2. 读 key
		idemKey := string(c.Request.Header.Peek("Idempotency-Key"))
		if idemKey == "" {
			c.Next(ctx)
			return
		}

		// 3. 抢锁 / 取结果
		acquired, cached, err := mgr.AcquireOrGet(ctx, idemKey)
		if err == idempotentx.ErrInFlight {
			// 正在跑:返 409
			// ⚠️ `c.Abort()` 少一行的话**业务 handler 照样会跑** —— hertz 的
			// Next() 是索引 for 循环,直接 return 只跳过 Next 里的循环体,引擎
			// 那个 for 会继续往下走。详见 bodylimit.go respondTooLarge 的注释。
			// 这一处的后果比 413 更重:handler 真的执行了一遍(幂等要防的正是
			// 这个),然后用 200 + 自己的 body 把 409 覆盖掉,客户端看到的是
			// "成功了两次"。
			c.Abort()
			c.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
			c.SetStatusCode(consts.StatusConflict)
			c.Response.SetBody([]byte(`{"code":40900,"message":"request with same Idempotency-Key is in flight","data":null}`))
			return
		}
		// 命中结果
		if !acquired && cached != nil {
			// 同上:不 Abort 的话这里就是"假装重放",handler 重新执行一遍。
			c.Abort()
			c.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
			c.Response.Header.Set("Idempotent-Replay", "true") // 标记这是重放,客户端可选择不显示
			c.SetStatusCode(cached.HTTPStatus)
			c.Response.SetBody(cached.Body)
			return
		}

		// 4. 拿到锁了(或者 Redis 故障降级放行)。
		//
		//    ⚠️ **从这一行起,panic 展开时必须把锁还回去** ——
		//    否则 handler 一崩,这个 key 就被锁住 30s:客户端拿同一个 key 重试
		//    只会拿到 409 "request in flight"(而实际上没有任何请求在跑),
		//    这个写操作再也发不出去。
		//
		//    旧实现只在"状态码异常"分支里 Release。panic 走的是 unwind ——
		//    c.Next 之后的代码根本不执行,锁就这么挂到 TTL 到期为止。
		lockHeld := true
		defer func() {
			if !lockHeld {
				return
			}
			releaseCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			mgr.Release(releaseCtx, idemKey)
		}()

		// 5. 业务 handler 跑
		c.Next(ctx)

		// 6. 只有"业务真的跑过"的结果才缓存 —— 判据见 cacheableResult
		status := c.Response.StatusCode()
		body := c.Response.Body()
		if !cacheableResult(c, status) {
			// 没跑成 → 释放锁,让客户端拿**同一个 key** 重试能真的跑起来
			lockHeld = false
			releaseCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			mgr.Release(releaseCtx, idemKey)
			return
		}
		// 写结果(用短 ctx,不能因为客户端断连就放弃保存)
		saveCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		mgr.Save(saveCtx, idemKey, status, body)
		lockHeld = false // Save 内部已经把锁删了
	}
}

// cacheableResult 判断这个响应能不能当"幂等结果"缓存。
//
// # 判据:业务到底跑没跑成
//
// 幂等结果的语义是"**这次业务执行的结局**"。只有业务真的跑了,才有结局
// 可缓存。前置中间件(鉴权 / 权限 / CSRF / 限流)拒掉的请求,业务压根没跑,
// 缓存它等于告诉客户端"**这个 key 已经用掉了**"。
//
// # 为什么这条必须要有(M97)
//
// 旧实现是 `if status >= 200 && status < 600 { Save }` —— **401 也被缓存**。
// 而 401 是**鉴权中间件**产生的(Auth 是组级中间件,位于本中间件的 c.Next
// 之内),于是整条链变成:
//
//	客户端带 key K 发写请求 → 401(比如 access token 刚好过期)
//	  → 服务端把 401 当成"结果"缓存 24h
//	  → 前端 axios 拦截器刷新 token 后**用同一个 config 重发**
//	    (同一个 config = 同一个 Idempotency-Key)
//	  → 服务端命中缓存,把 401 **原样重放**回去
//	  → 业务永远不执行;前端第二次收到 401 又走 handleUnauthorized(),
//	    **把用户踢回登录页** —— 尽管 token 刚刷新成功、其实完全有效。
//
// 症状是"偶尔被踢回登录页",且必现于"token 刚好过期时点提交"那一刻。
//
// # 两条判据
//
//  1. c.IsAborted():下游中间件调了 c.Abort() —— 框架自己拒了它。
//     (Auth 401 / RBAC 403 / CSRF 403 / RateLimit 429 / APIVersion 400 …)
//     业务没跑。
//     注:本中间件**上游**的中间件(BodyLimit 413 / RequestGzip / Recover)
//     拒绝时,本中间件压根不会执行(引擎的 for 循环已被 Abort 打断),
//     所以 IsAborted 为真只可能是"下游拒的"。
//
//  2. 状态码是"拒绝"语义:401 / 403 / 413 / 429。
//     补的是**handler 层的前置守卫** —— 它们不 Abort(那只是 handler 返回),
//     但同样是在任何写操作之前就返回了:
//     - handler.RequireCaller → 401("未登录")
//     - asset.ListMyAssigns   → 403(IDOR 阻断)
//     - auth.ResetPassword   → 403(IDOR 阻断)
//
// ⚠️ **5xx 不在拒绝之列**:handler 抛错返回的 500、超时的 504,业务**可能
// 已经写了数据**,缓存它 = 至多一次(宁可少跑一次,不能重跑)。这条边界不能动。
//
// ⚠️ 413 其实是 BodyLimit 产生的,而 BodyLimit 挂在本中间件**之前**
// (router.go 里 BodyLimit 在 Idempotency 前面),所以它走不到这里 ——
// 列进来是为了"哪天把 BodyLimit 挪到组级中间件里"时行为依然正确。
func cacheableResult(c *app.RequestContext, status int) bool {
	// 前置中间件拒了 → 业务没跑
	if c.IsAborted() {
		return false
	}
	// 拒绝语义的状态码 → 同样没跑
	switch status {
	case 401, 403, 413, 429:
		return false
	}
	// 其余:0(压根没写响应)/异常状态不缓存
	return status >= 200 && status < 600
}

// isWriteMethod 写操作白名单
func isWriteMethod(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// 避免 unused 警告
var (
	_ = bytes.NewBuffer
	_ = io.Discard
	_ = response.OK
)
