// permnotify —— 权限失效广播
//
// ## 这个包解决什么问题
//
// 管理员改了角色权限,已经登录的浏览器**手里的权限是旧的** ——
// 菜单还是老的,按钮还是该显示就显示。要让所有在线前端重拉权限,
// 就得有个广播通道。
//
// ## 关键点
//
//   - **广播给所有人,不按 userID 精确推**。角色一变,所有持该角色的用户
//     都要刷。精细化要"按 role 找 userID 列表"再 publish,本期不必要
//   - **永不返回错误**。推送是旁路 —— 权限改成功了但推送失败,不能变成
//     "改失败"。push.Publish 内部已经把投递失败记成 Warn 日志
//   - **本包不自己持有 redis.Client**,发布走 pkg/push。
//     依赖方向是 permnotify → push(反过来不成立)
//   - **频道名 `perm:invalidate:all` 沿用迁移前的名字**(故意不带
//     `push:` 前缀)—— 本包迁到 pkg/push 之后,发布端一行都不用改,
//     没有"双写 / 双订阅"的迁移窗口
//   - **payload 只有 reason + actor_id**,不带任何权限数据。
//     收到的人自己去重拉 `GET /auth/accessible`
//
// ## 跑法
//
//	go run ./examples/permnotify
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/pkg/permnotify"
	"rapid-admin/internal/pkg/push"
)

func main() {
	ctx := context.Background()

	// ── 一、装配推送通道 ────────────────────────────────────────
	fmt.Println("=== 装配 ===")
	mr, err := miniredis.Run()
	if err != nil {
		fmt.Println("启动内存 Redis 失败:", err)
		return
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	push.InitRedis(rdb)
	fmt.Printf("push.Available() = %v\n", push.Available())

	// 订阅权限频道(前端 SSE /events 端点干的就是这件事)
	ch, cancel := push.Subscribe(ctx, []string{push.TopicPerm})
	defer cancel()
	time.Sleep(200 * time.Millisecond) // 等订阅确认落地,否则紧接着发布的事件会丢

	// ── 二、广播 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 广播权限失效 ===")
	permnotify.PublishInvalidateAll(ctx, 8801, "role_resource_changed")
	select {
	case ev := <-ch:
		fmt.Printf("  收到:topic=%s event=%s payload=%+v\n", ev.Topic, ev.Event, ev.Payload)
	case <-time.After(2 * time.Second):
		fmt.Println("  超时没收到事件")
	}
	fmt.Println("  ⇒ reason 说明是什么变了:")
	fmt.Println("    role_resource_changed —— 角色绑的资源变了")
	fmt.Println("    user_role_changed     —— 用户的角色变了")
	fmt.Println("    manual                —— 人工触发")
	fmt.Println("  ⇒ actor_id 是谁触发的(审计用),前端不用它,只用来排查")

	// ── 三、没装配传输层时也不炸 ────────────────────────────────
	fmt.Println()
	fmt.Println("=== 没装配传输层 ===")
	push.InitRedis(nil) // 传 nil ⇒ InitRedis 直接 return,传输层保持原样
	push.Init(nil)      // 显式清掉
	permnotify.PublishInvalidateAll(ctx, 8801, "manual")
	fmt.Println("  ⇒ 没炸 —— 广播失败不该让「改权限」这个主流程失败")
	fmt.Println("    (push.Publish 内部只记 Debug 日志)")
}
