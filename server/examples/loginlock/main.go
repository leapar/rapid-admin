// loginlock —— 登录失败计数 + 账号锁定
//
// ## 这个包解决什么问题
//
// 防暴力破解:同一 IP + 同一账号连续登录失败到阈值 ⇒ 临时锁住。
//
// ## 关键点
//
//   - **存储由 kvstore 决定(redis / 内存),本包不直接依赖 repository**。
//     2026-10-21 之前三个函数开头都是 `if repository.RDB == nil { return nil }`,
//     后果是**本地 dev 环境里登录锁定整个不存在**:没 Redis 时暴力破解可以
//     无限试。而这恰恰是最需要它的地方 —— 生产有 Redis 挡着,本地反而裸奔。
//     现在没 Redis 时走进程内计数:阈值一样生效,只是重启后清零、多副本不共享
//   - **PreCheck 出错 ⇒ 放行**(可用性优先)。Redis 抖一下就把所有账号锁死,
//     比放行更糟
//   - **AfterFail 出错 ⇒ 返回错误**(不要假装成功)。登录已经失败了,计数写
//     不进去意味着这次失败不累计 —— 但调用方要能看见它
//     两条方向相反是刻意的:一个面向"可用性优先",一个面向"不要假装成功"
//   - **max <= 0 = 显式关闭锁定**。不这么判的话,一旦有人把阈值改成配置项
//     并默认填 0,`n >= max` 会变成"失败一次就锁死所有账号"
//   - **key 是 (ip, username)**,两个都算。攻击者换 IP 刷同一账号 ⇒ 计数不涨;
//     同一 IP 撞不同账号 ⇒ 也不涨 —— 单维度都挡不住分布式撞库
//
// ## 跑法
//
//	go run ./examples/loginlock
package main

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/kvstore"
	"rapid-admin/internal/pkg/loginlock"
)

func main() {
	ctx := context.Background()

	// ── 一、装配存储 ────────────────────────────────────────────
	//
	// 生产:kvstore.Resolve(kv.mode, repository.GetRedis())
	// 这里用内存实现 —— 本地 dev 没 Redis 时登录锁定也必须生效。
	fmt.Println("=== 装配 ===")
	store, err := kvstore.Resolve(kvstore.ModeMemory, nil)
	if err != nil {
		fmt.Println("装配存储失败:", err)
		return
	}
	kvstore.SetDefault(store)
	fmt.Printf("后端 = %s\n", store.Backend)
	fmt.Println("  ⇒ 显式 redis 模式但没配 addr ⇒ 直接报错,不静默退回内存")
	fmt.Println("    (多副本部署下用内存限流等于把阈值放大 N 倍)")

	// ── 二、失败累积 ────────────────────────────────────────────
	const max = 3
	fmt.Println()
	fmt.Println("=== 失败累积(阈值 3)===")
	for i := 1; i <= 4; i++ {
		// 登录前先问:还锁着吗
		pre := loginlock.PreCheck(ctx, "zhangsan", "10.0.0.1", max)
		// 登录失败 ⇒ 计数 +1
		if err := loginlock.AfterFail(ctx, "zhangsan", "10.0.0.1", 15*time.Minute); err != nil {
			fmt.Println("计数失败:", err)
			return
		}
		fmt.Printf("  第 %d 次失败 → 预检:%v\n", i, pre)
	}
	fmt.Println("  ⇒ 预检在第 3 次失败之后开始报错")

	// ── 三、被锁住 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 被锁住 ===")
	err = loginlock.PreCheck(ctx, "zhangsan", "10.0.0.1", max)
	fmt.Printf("  PreCheck → %v\n", err)
	fmt.Println("  ⇒ 锁住时登录接口直接拒,不再去比对密码(省掉一次密码哈希)")

	// ── 四、key 是 (ip, username)两个维度 ──────────────────────
	fmt.Println()
	fmt.Println("=== key 是 (ip, username)===")
	fmt.Printf("  换个 IP 同账号   → %v\n", loginlock.PreCheck(ctx, "zhangsan", "10.0.0.2", max))
	fmt.Printf("  同 IP 换账号     → %v\n", loginlock.PreCheck(ctx, "lisi", "10.0.0.1", max))
	fmt.Println("  ⇒ 两个维度都算:换 IP 刷同一账号、同一 IP 撞不同账号,计数都不涨")

	// ── 五、登录成功 ⇒ 清零 ────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 登录成功清零 ===")
	if err := loginlock.Clear(ctx, "zhangsan", "10.0.0.1"); err != nil {
		fmt.Println("清零失败:", err)
		return
	}
	fmt.Printf("  Clear 之后 PreCheck → %v\n", loginlock.PreCheck(ctx, "zhangsan", "10.0.0.1", max))
	fmt.Println("  ⇒ 登录成功要清,否则用户密码改对了还是进不去")

	// ── 六、max <= 0 ⇒ 关闭锁定 ────────────────────────────────
	fmt.Println()
	fmt.Println("=== max <= 0 = 显式关闭锁定 ===")
	for i := 1; i <= 5; i++ {
		_ = loginlock.AfterFail(ctx, "wangwu", "10.0.0.3", 15*time.Minute)
	}
	fmt.Printf("  失败 5 次后 PreCheck(max=0) → %v\n", loginlock.PreCheck(ctx, "wangwu", "10.0.0.3", 0))
	fmt.Println("  ⇒ 不这么判的话,阈值一旦被改成配置项且默认填 0,")
	fmt.Println("    `n >= max` 会变成「失败一次就锁死所有账号」")

	// ── 七、ttl ─────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== ttl ===")
	fmt.Println("  AfterFail(ctx, username, ip, lockDur) —— 首次计数时设过期")
	fmt.Println("  ⇒ 15 分钟后计数自动消失,用户重新获得完整额度")
	fmt.Println("  (注意:不是每次失败都续期,否则永远锁不死)")
}
