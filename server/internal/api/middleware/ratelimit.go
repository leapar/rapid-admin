// 限流(滑动窗口 ZSET)+ 登录失败锁定
// 文档 §8.7
package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/kvstore"
	"rapid-admin/internal/pkg/response"
	"rapid-admin/internal/service"
)

// RateLimit 滑动窗口限流
// scene: 业务场景(login/sms/api)
// max:   窗口内允许的最大请求数
// window: 窗口大小
// dim:    维度 ip/user,user 优先用 caller.UserID
//
// 存储由 kvstore 决定(redis / 内存),**本文件不再自己判"有没有 Redis"**。
//
// 2026-10-21 之前这里是:
//
//	if repository.RDB == nil { c.Next(ctx); return }
//
// 于是本地 dev 环境下所有限流**全部不生效** —— 想验一次限流触发,
// 在本机根本验不了,因为那个开关在没 Redis 时是永久关着的,而且不告警。
// 判空集中到 kvstore.Resolve 一处,由 main 启动时决定一次。
func RateLimit(scene string, max int, window time.Duration, dim string) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		keyDim := c.ClientIP()
		if dim == "user" {
			if caller, ok := service.FromContext(ctx); ok && caller != nil && caller.UserID > 0 {
				keyDim = fmt.Sprintf("u:%d", caller.UserID)
			} else {
				// ⚠️ M105(2026-10-28):原来这里是**静默**降级 ——
				// ctx 里没有 caller 就按 IP 限流,一个字都不吭。
				// 而"按用户限流"降级成"按 IP 限流"意味着:
				//   - 同一个 NAT 出口的一整栋楼共用一个桶(互相挤兑);
				//   - 换个出口就换个桶 → 限流形同虚设。
				// 降级本身要保留(公开端点没有 caller 是正常的),
				// 但必须留痕 —— 否则"限流没按预期生效"根本查不出来。
				L().Warn("ratelimit: user 维度降级为 ip(上下文里没有 caller)",
					zap.String("scene", scene),
					zap.String("path", string(c.Path())))
			}
		}
		key := fmt.Sprintf("rl:%s:%s", scene, keyDim)

		ok, err := kvstore.DefaultStore().Window.Allow(ctx, key, max, window)
		if err != nil {
			// 存储故障 → 放行 + WARN。
			//
			// 与吊销那边相反(那边查询出错按"已吊销"拒)。区别在后果:
			//   吊销存储故障 → 令牌没拦住 = 安全问题
			//   限流存储故障 → 多放过几个请求 = 可用性问题
			// 选"放行"是因为 Redis 抖一下就把全站挡在门外更糟。
			// 但必须留痕,否则"限流一直不生效"这件事就彻底静默了。
			L().Warn("ratelimit store error, fallback pass",
				zap.String("scene", scene), zap.Error(err))
			c.Next(ctx)
			return
		}
		if !ok {
			response.Fail(c, errcode.TooManyRequests, "请求过于频繁")
			c.Abort()
			return
		}
		c.Next(ctx)
	}
}

// 注意:登录失败计数 PreCheck/AfterFail/Clear 已迁移至 internal/pkg/loginlock,
// 业务(service)和限流(middleware)双方共用,避免循环依赖。
