// 运行时增删策略行(AddPolicy / RemovePolicy)的回归网
//
// # 这张网守的是什么
//
// casbin 的 AddPolicy **不校验策略行的段数** —— 它把 rule 原样塞进模型
// 和 adapter。而本仓的模型是:
//
//	p = sub, dom, obj, act, eft   ← 5 段
//
// 所以传 4 段(sub/dom/obj/act)会**写出一行 4 段的策略**。
// 那行在库里看着好好的,但下一次 LoadPolicy 读它时报:
//
//	invalid policy rule size: expected 5, got 4
//
// ⇒ **整个 enforcer 加载失败**。而 LoadPolicy 在进程启动时必跑一次
// (casbinx.Init 里),所以后果是**服务起不来**,不是"这条策略不生效"。
//
// ⚠️ enforcer 是进程级单例(sync.Once)⇒ 这里用 ResetForTest 隔离,
//
//	测完还原,不许影响同包其它用例。
package casbinx

import (
	"testing"

	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/stretchr/testify/require"
)

// AddPolicy 写出来的行必须**五段齐全**,且 LoadPolicy 必须读得回来
func TestAddPolicy_WritesFullWidthRow(t *testing.T) {
	db := newSyncDB(t)
	initGlobalEnforcer(t, db)

	require.NoError(t, AddPolicy("role:role_mut_x", "global", "user:list", "*"))

	// 库里的行必须五段都有值
	var rows []gormadapter.CasbinRule
	require.NoError(t, db.Where("ptype = ?", "p").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "role:role_mut_x", rows[0].V0)
	require.Equal(t, "global", rows[0].V1)
	require.Equal(t, "user:list", rows[0].V2)
	require.Equal(t, "*", rows[0].V3)
	require.Equal(t, "allow", rows[0].V4,
		"eft 必须补上 —— 缺这一段 LoadPolicy 会报 rule size 不符,整个 enforcer 加载失败")

	// ⚠️ 关键断言:Reload 必须成功
	// (4 段坏行 ⇒ "invalid policy rule size: expected 5, got 4")
	require.NoError(t, Reload())

	ok, err := Enforce("role:role_mut_x", "global", "user:list", "list")
	require.NoError(t, err)
	require.True(t, ok, "写进去的策略必须真的生效")
}

// 显式 deny 行:eft 必须是 deny,且 deny 赢过 grant
func TestAddPolicyWithEft_DenyWins(t *testing.T) {
	db := newSyncDB(t)
	initGlobalEnforcer(t, db)

	require.NoError(t, AddPolicy("role:role_mut_y", "global", "house:view", "*"))
	require.NoError(t, AddPolicyWithEft("role:role_mut_y", "global", "house:delete", "*", "deny"))
	require.NoError(t, Reload())

	ok, err := Enforce("role:role_mut_y", "global", "house:view", "list")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = Enforce("role:role_mut_y", "global", "house:delete", "delete")
	require.NoError(t, err)
	require.False(t, ok, "显式 deny 必须赢过 grant")
}

// eft 只收 allow / deny —— 别的值直接报错(别让脏行进库)
func TestAddPolicyWithEft_RejectsUnknownEft(t *testing.T) {
	db := newSyncDB(t)
	initGlobalEnforcer(t, db)

	err := AddPolicyWithEft("role:role_mut_z", "global", "house:view", "*", "maybe")
	require.Error(t, err, "未知 eft 必须报错 —— 脏行会让 LoadPolicy 整个失败")
}

// RemovePolicy 必须把 allow / deny **两行都删掉**
//
// 只删一行会留下另一半:留着 deny 行等于这个资源永远被拒。
func TestRemovePolicy_RemovesBothAllowAndDeny(t *testing.T) {
	db := newSyncDB(t)
	initGlobalEnforcer(t, db)

	require.NoError(t, AddPolicy("role:role_mut_w", "global", "house:view", "*"))
	require.NoError(t, AddPolicyWithEft("role:role_mut_w", "global", "house:view", "*", "deny"))
	require.NoError(t, Reload())

	require.NoError(t, RemovePolicy("role:role_mut_w", "global", "house:view", "*"))
	require.NoError(t, Reload())

	var rows []gormadapter.CasbinRule
	require.NoError(t, db.Where("ptype = ? AND v0 = ?", "p", "role:role_mut_w").Find(&rows).Error)
	require.Empty(t, rows, "allow / deny 两行都要删掉,不能只删一半")

	ok, err := Enforce("role:role_mut_w", "global", "house:view", "list")
	require.NoError(t, err)
	require.False(t, ok)
}
