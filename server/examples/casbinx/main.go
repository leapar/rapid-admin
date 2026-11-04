// casbinx —— Casbin 初始化 + 策略行格式
//
// ## 这个包解决什么问题
//
// 后台权限的**执行层**:一个角色能不能对某个资源做某个动作。
// 显示层(前端菜单 / 按钮)和执行层(后端 403)读的是同一份绑定矩阵。
//
// ## 关键点
//
//   - **obj 装的是资源码,不是 URL 路径** —— "user:list" / "house:view"。
//     模型用 keyMatch 而不是 keyMatch2:资源码里的 ":" 是字面量分隔符,
//     keyMatch2 会把 ":xxx" 当路径参数,于是绑了 user:list 的角色
//     **也能过 user:delete 的检查**
//   - **显式 deny 永远赢**:模型是
//     `some(where (p.eft == allow)) && !some(where (p.eft == deny))`
//   - **p.dom == "global" 时跨任意域生效**
//   - **PolicyRow 是策略行格式的唯一来源** —— seed 整体重建和运行时的
//     同步都调它。两处各抄一份格式的话,迟早有一处改了另一处没改,
//     而且**没有任何报错**(casbin 只按行里的字符串匹配)
//   - **AddPolicy 自动补 eft="allow"** —— 模型是 5 段
//     (sub/dom/obj/act/eft),而 casbin 的 AddPolicy **不校验段数**:
//     传 4 段会写出一行 4 段的脏策略,下次 LoadPolicy 报
//     "invalid policy rule size: expected 5, got 4" ⇒ **整个 enforcer
//     加载失败**,进程起不来
//   - **写完策略行必须刷新内存副本**(ReloadIfInitialized)——
//     内存模型不会自动读库
//
// ## 跑法
//
//	go run ./examples/casbinx
package main

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/pkg/casbinx"
)

func main() {
	// ── 一、策略行格式 ──────────────────────────────────────────
	//
	// 这一行是 [sub, dom, obj, act, eft]:
	//
	//	role:house_admin | global | house:view | * | allow
	fmt.Println("=== 策略行格式 ===")
	row := casbinx.PolicyRow("house_admin", "house:view")
	for i, f := range row {
		fmt.Printf("  [%d] %s\n", i, f)
	}
	fmt.Println("  ⇒ sub=角色前缀 / dom=global / obj=资源码 / act=* / eft=allow")

	fmt.Println()
	fmt.Println("=== 同一角色的多条行 ===")
	for _, res := range []string{"house:view", "house:edit", "user:list"} {
		fmt.Printf("  %v\n", casbinx.PolicyRow("house_admin", res))
	}

	// ── 二、没初始化时的行为 ────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 未初始化 ===")
	ok, err := casbinx.Enforce("user_10001", "global", "house:view", "list")
	fmt.Printf("  Enforce → ok=%v err=%v\n", ok, err)
	fmt.Println("  ⇒ 未 Init 时返回 error 而不是 panic —— 调用方要么忽略要么报错")

	// ── 三、初始化 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 初始化 ===")
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Println("开内存 sqlite 失败:", err)
		return
	}
	if err := casbinx.Init(db, "configs/casbin_rbac.conf"); err != nil {
		fmt.Println("初始化失败:", err)
		return
	}
	fmt.Println("enforcer ready(策略表由 gorm-adapter 自动建)")

	// ── 四、装策略 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 装策略 ===")
	// 角色 → 用户
	if err := casbinx.AddRoleForUser("user_10001", "role:house_admin", "global"); err != nil {
		fmt.Println("加角色失败:", err)
		return
	}
	fmt.Println("  + g(user_10001, role:house_admin, global)")

	// 角色的权限行(eft 默认 allow)
	for _, res := range []string{"house:view", "house:edit"} {
		if err := casbinx.AddPolicy("role:house_admin", "global", res, "*"); err != nil {
			fmt.Println("加策略失败:", err)
			return
		}
		fmt.Printf("  + [role:house_admin global %s * allow]\n", res)
	}
	// 一条显式 deny:deny 永远赢
	if err := casbinx.AddPolicyWithEft("role:house_admin", "global", "house:delete", "*", "deny"); err != nil {
		fmt.Println("加 deny 失败:", err)
		return
	}
	fmt.Println("  + [role:house_admin global house:delete * deny]  ← 显式 deny")

	// 内存副本要刷新才看得到刚写的行
	if err := casbinx.ReloadIfInitialized(); err != nil {
		fmt.Println("刷新失败:", err)
		return
	}

	// ── 五、鉴权 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 鉴权 ===")
	cases := []struct {
		sub, obj string
	}{
		{"user_10001", "house:view"},
		{"user_10001", "house:edit"},
		{"user_10001", "house:delete"}, // 有显式 deny ⇒ 拒
		{"user_10001", "user:list"},    // 没授权 ⇒ 拒
		{"user_99999", "house:view"},   // 不是这个角色 ⇒ 拒
	}
	for _, c := range cases {
		ok, err := casbinx.Enforce(c.sub, "global", c.obj, "list")
		if err != nil {
			fmt.Printf("  %-12s %-14s → err=%v\n", c.sub, c.obj, err)
			continue
		}
		verdict := "拒绝"
		if ok {
			verdict = "允许"
		}
		fmt.Printf("  %-12s %-14s → %s\n", c.sub, c.obj, verdict)
	}
	fmt.Println("  ⇒ 显式 deny 赢过 grant;没授权就是拒绝(默认拒绝)")

	// ── 六、撤策略 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 撤策略 ===")
	// allow / deny 两行都删
	if err := casbinx.RemovePolicy("role:house_admin", "global", "house:delete", "*"); err != nil {
		fmt.Println("撤策略失败:", err)
		return
	}
	if err := casbinx.ReloadIfInitialized(); err != nil {
		fmt.Println("刷新失败:", err)
		return
	}
	ok, _ = casbinx.Enforce("user_10001", "global", "house:delete", "list")
	fmt.Printf("  撤掉 deny 行后 house:delete → %v\n", ok)
	fmt.Println("  ⇒ RemovePolicy 把 allow / deny 两行都删 —— 只删一半会留下 deny 行")

	// ── 七、运行时同步 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 运行时同步 ===")
	fmt.Println("  casbin_rule 是绑定矩阵的**物化副本**,只在 seed / cmd/migrate 时整体重建。")
	fmt.Println("  运行时「建角色 / 分配权限 / 删角色」改了 sys_role_resource,")
	fmt.Println("  casbin_rule 不动 ⇒ 前端按绑定显示、后端按 casbin 判 ⇒ 勾了权限照样 403。")
	fmt.Println("  ⇒ 绑定改动后必须调 SyncRolePolicies(tx, roleCode, resCodes),")
	fmt.Println("    并且和绑定改动走**同一个事务**(回滚了却留着放行行,失败方向是「放行」)。")
}
