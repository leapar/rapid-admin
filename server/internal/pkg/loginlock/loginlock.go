// 登录失败计数 + 账号锁定
// 文档 §8.7
// 中立于 service / middleware 之外,供两者共用,避免循环依赖
package loginlock

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/kvstore"
)

// 本包不再直接依赖 repository —— 存储由 kvstore 决定(redis / 内存)。
//
// 2026-10-21 之前三个函数开头都是 `if repository.RDB == nil { return nil }`,
// 后果是**本地 dev 环境里登录锁定整个不存在**:没有 Redis,暴力破解可以
// 无限试。而这恰恰是最需要它的地方 —— 生产有 Redis 挡着,本地反而裸奔。
// 现在没 Redis 时走进程内计数:阈值一样生效,只是重启后清零、多副本不共享。
//
// # 存储故障时的取向
//
// PreCheck 出错**放行**(维持历史行为):Redis 抖一下就把所有账号锁死,
// 比放行更糟。AfterFail 出错**返回错误**:登录已经失败了,计数写不进去
// 意味着这次失败不累计 —— 但调用方要能看见它,所以返错而不是静默吞掉。
// 两条方向相反,是刻意的:一个面向"可用性优先",一个面向"不要假装成功"。

// PreCheck 登录前预检失败次数,达到 max 时返回 errcode.UserLocked。
func PreCheck(ctx context.Context, username, ip string, max int) error {
	// max<=0 = 显式关闭锁定。今天唯一调用方传的是硬编码 10,所以这行够不到;
	// 留着是因为一旦有人把 10 改成配置项并默认填 0,下面的 `n >= max` 会
	// 变成"失败一次就锁死所有账号"。两行换掉一个把全站锁死的默认值。
	if max <= 0 {
		return nil
	}
	n, err := kvstore.DefaultStore().Counter.Get(ctx, keyOf(username, ip))
	if err != nil || n < int64(max) {
		return nil
	}
	return errcode.New(errcode.UserLocked, "登录失败次数过多,账号已临时锁定")
}

// AfterFail 登录失败 +1,首次时设 lockDur 过期。
func AfterFail(ctx context.Context, username, ip string, lockDur time.Duration) error {
	_, err := kvstore.DefaultStore().Counter.Incr(ctx, keyOf(username, ip), lockDur)
	return err
}

// Clear 登录成功清零
func Clear(ctx context.Context, username, ip string) error {
	return kvstore.DefaultStore().Counter.Del(ctx, keyOf(username, ip))
}

func keyOf(username, ip string) string {
	// 顺序是 (ip, username),与历史一致 —— Redis 里已积累的计数沿用同一 key。
	return fmt.Sprintf("login:fail:%s:%s", ip, username)
}