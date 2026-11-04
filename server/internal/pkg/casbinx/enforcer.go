// Casbin 初始化 + 工具
// 文档 §5.2
package casbinx

import (
	"fmt"
	"sync"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"gorm.io/gorm"
)

var (
	e   *casbin.Enforcer
	// initMu 串行化 Init,并保护 e / initErr 的读写。
	//
	// ⚠️ 曾经用 sync.Once + 函数局部 err:第一次 Init 失败后 once 已消费、
	// 局部 err 随栈帧丢弃 —— 第二次调用直接返 nil(假成功),调用方以为
	// 初始化好了,实际 e == nil,所有鉴权调用报 "casbin not initialized",
	// 与"初始化成功但策略为空"两种故障混在一起。改成 mutex + 包级 initErr:
	// 失败可以重试(重试成功则 initErr 清零),重试再失败返回最新一次的错误。
	initMu  sync.Mutex
	initErr error
)

// Init 初始化全局 enforcer
//
//	db   - 已初始化的 gorm.DB(用于 casbin_rule 表)
//	conf - rbac_with_domains.conf 路径
//
// 语义:成功后重复调用是 no-op(返 nil);失败后允许重试 —— adapter/模型
// 文件这类故障可能是环境性的,不必为了一次失败重启整个进程。
func Init(db *gorm.DB, conf string) error {
	initMu.Lock()
	defer initMu.Unlock()
	if e != nil {
		return nil
	}
	adapter, err := gormadapter.NewAdapterByDB(db)
	if err != nil {
		initErr = err
		return err
	}
	enforcer, err := casbin.NewEnforcer(conf, adapter)
	if err != nil {
		initErr = err
		return err
	}
	enforcer.EnableAutoSave(true)
	// 预加载一次
	if err := enforcer.LoadPolicy(); err != nil {
		initErr = err
		return err
	}
	e = enforcer
	initErr = nil
	return nil
}

// Enforcer 获取全局 enforcer
func Enforcer() *casbin.Enforcer {
	return e
}

// Enforce 鉴权快捷方法
//
//	sub - 用户编码(user_10001) 或 角色编码(role_house_admin)
//	dom - 域(proj_001 / proj_all)
//	obj - **资源码**(如 "user:list"),不是 HTTP 路径 —— 见 M147 与
//	      docs/后台权限模型设计V1.0.md §6.3
//	act - 资源动作(如 "list")/ "*"
func Enforce(sub, dom, obj, act string) (bool, error) {
	if e == nil {
		return false, fmt.Errorf("casbin not initialized")
	}
	return e.Enforce(sub, dom, obj, act)
}

// AddPolicy 追加策略(供 service 层用)
//
// ⚠️ **eft 默认补 "allow"** —— 本仓的模型是
//
//	p = sub, dom, obj, act, eft   ← 5 段
//
// 而 casbin 的 AddPolicy **不校验段数**:它把 rule 原样塞进模型和 adapter。
// 传 4 段(sub/dom/obj/act)会**写出一行 4 段的策略** —— 那行在库里看着好好的,
// 但下一次 LoadPolicy 读它时报
//
//	invalid policy rule size: expected 5, got 4
//
// ⇒ **整个 enforcer 加载失败**。而 LoadPolicy 在进程启动时必跑一次
// (Init 里),所以后果是**服务起不来**,不是"这条策略不生效"。
//
// ⇒ 这里补上第 5 段,别让调用方自己拼。
func AddPolicy(sub, dom, obj, act string) error {
	return AddPolicyWithEft(sub, dom, obj, act, "allow")
}

// AddPolicyWithEft 追加带 eft 的策略(deny 行用这个)
//
// ⚠️ eft 只收 allow / deny —— 别的值直接报错:
// 脏行会让 LoadPolicy 整个失败,而失败点在**进程启动**,不在写策略的那一刻。
func AddPolicyWithEft(sub, dom, obj, act, eft string) error {
	if e == nil {
		return fmt.Errorf("casbin not initialized")
	}
	if eft != "allow" && eft != "deny" {
		return fmt.Errorf("casbinx: eft 只能是 allow/deny,收到 %q", eft)
	}
	_, err := e.AddPolicy(sub, dom, obj, act, eft)
	return err
}

// RemovePolicy 移除策略
//
// ⚠️ **allow / deny 两行都删** —— 只删一行会留下另一半,而 deny 行的优先级
// 高于 grant:留着 deny 行等于这个资源永远被拒,而且**不报错**。
func RemovePolicy(sub, dom, obj, act string) error {
	if e == nil {
		return fmt.Errorf("casbin not initialized")
	}
	if _, err := e.RemovePolicy(sub, dom, obj, act, "allow"); err != nil {
		return err
	}
	if _, err := e.RemovePolicy(sub, dom, obj, act, "deny"); err != nil {
		return err
	}
	return nil
}

// AddRoleForUser 用户 → 角色(在某个域)
func AddRoleForUser(user, role, dom string) error {
	if e == nil {
		return fmt.Errorf("casbin not initialized")
	}
	_, err := e.AddGroupingPolicy(user, role, dom)
	return err
}

// RemoveRoleForUser 移除用户角色
func RemoveRoleForUser(user, role, dom string) error {
	if e == nil {
		return fmt.Errorf("casbin not initialized")
	}
	_, err := e.RemoveGroupingPolicy(user, role, dom)
	return err
}

// Reload 重新加载策略(供后台"权限变更"后调用)
func Reload() error {
	if e == nil {
		return fmt.Errorf("casbin not initialized")
	}
	return e.LoadPolicy()
}

// ===== M147-f:运行时策略同步 =====

// PolicyRow 拼出「角色 → 资源」的策略行(格式的**唯一来源**)
//
// ⚠️ seed.buildRolePolicies(整体重建)与 service 层的运行时同步都调它 ——
//
//	两处各抄一份格式的话,迟早有一处改了另一处没改:
//	格式变了而老行还在库里 ⇒ 老角色突然失权 / 新角色莫名有权,
//	而且**没有任何报错**(casbin 只按行里的字符串匹配)。
func PolicyRow(roleCode, resCode string) []string {
	return []string{"role:" + roleCode, "global", resCode, "*", "allow"}
}

// SyncRolePolicies 把某角色当前的策略行**重写**进 casbin_rule
//
// # 为什么需要运行时同步(M147-f)
//
// casbin_rule 是绑定矩阵的**物化副本** —— 只在 seed / cmd/migrate 时整体重建。
// 运行时「建角色」「分配权限」「删角色」改了 sys_role_resource / sys_role,
// casbin_rule 不动 ⇒ 后端按 casbin 判、前端按绑定显示 ⇒
// 管理员在界面上勾了权限,接口照样 403(而且**没有任何报错**)。
//
// # 为什么走调用方的 tx
//
// 绑定改动与策略行改动必须**同一个事务**:否则事务回滚了、casbin 里却留着
// 放行行(失败方向是"放行",不能要)。
//
// ⚠️ 写完之后**必须刷新 enforcer 的内存副本**(ReloadIfInitialized)——
// 内存模型不会自动读库。
func SyncRolePolicies(tx *gorm.DB, roleCode string, resCodes []string) error {
	if err := RemoveRolePolicies(tx, roleCode); err != nil {
		return err
	}
	if len(resCodes) == 0 {
		return nil
	}
	rows := make([]gormadapter.CasbinRule, 0, len(resCodes))
	for _, code := range resCodes {
		if code == "" {
			continue
		}
		row := gormadapter.CasbinRule{Ptype: "p"}
		v := PolicyRow(roleCode, code)
		row.V0, row.V1, row.V2, row.V3, row.V4 = v[0], v[1], v[2], v[3], v[4]
		rows = append(rows, row)
	}
	return tx.Create(&rows).Error
}

// RemoveRolePolicies 删掉某角色的全部策略行(角色被删时用)
//
// ⚠️ **角色删了,策略行必须一起删** —— 否则:
//  1. 删角色 → 策略行还在 → 同 code 的新角色**继承旧角色的全部权限**;
//  2. 角色没了但行还在,库里看着像"这角色还有 425 条权限",排障时看不明白。
func RemoveRolePolicies(tx *gorm.DB, roleCode string) error {
	if err := ensureCasbinRuleTable(tx); err != nil {
		return err
	}
	return tx.Exec(
		"DELETE FROM "+gormadapter.CasbinRule{}.TableName()+" WHERE ptype = ? AND v0 = ?",
		"p", "role:"+roleCode).Error
}

// ensureCasbinRuleTable 表不存在就建一张
//
// ⚠️ 表**在生产里已经存在** —— adapter v3.41.0 的 NewAdapterByDB 构造时
// 就调了 createTable()(adapter.go:216),不是懒创建。
// 所以这个 guard 真正救的是**单测**:service 包直接起一个内存库、
// 没走 casbinx.Init ⇒ casbin_rule 表压根不存在 ⇒ 直接写行会报
// "no such table"。生产路径上它是 no-op。
//
// ⚠️ 表名用 adapter 自己的(CasbinRule.TableName()),
// 不要自己写字符串 —— 换了 adapter 的默认表名就会写到别的表上去。
func ensureCasbinRuleTable(tx *gorm.DB) error {
	if tx.Migrator().HasTable(gormadapter.CasbinRule{}.TableName()) {
		return nil
	}
	return tx.AutoMigrate(&gormadapter.CasbinRule{})
}

// ReloadIfInitialized 刷新 enforcer 的内存副本(enforcer 没初始化时静默跳过)
//
// ⚠️ **必须在事务提交之后调** —— 事务里刷的是"还没提交"的库状态。
//
// ⚠️ enforcer 没初始化时(单测、或者这个进程压根不做鉴权)**不报错**:
// 库里的行已经是最新的,下一个初始化 enforcer 的进程会 LoadPolicy 读到。
func ReloadIfInitialized() error {
	if e == nil {
		return nil
	}
	return e.LoadPolicy()
}

// ResetForTest 清除全局 enforcer(仅供单测用)
//   - 与 Init 同一把 initMu:保证与并发的 Init 互斥
func ResetForTest() {
	initMu.Lock()
	defer initMu.Unlock()
	e = nil
	initErr = nil
}
