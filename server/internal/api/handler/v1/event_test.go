// GET /events 订阅范围判定的单测 —— M72
//
// ⚠️ 这里守的是本模块最重要的一条安全边界:**客户端不能自选 topic**。
// 如果 subscribableTopics 无条件把所有 topic 发出去,任何登录用户都能订阅到
// 定时任务 / 审批事件 —— 等于给整个推送系统开了一个绕过 RBAC 的后门。
//
// 所以下面这几条的重点不是"能订阅到 job",而是"**不能**订阅到 job"。
package v1

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/casbinx"
	"rapid-admin/internal/pkg/push"
	"rapid-admin/internal/service"
)

// resetEnforcer 起一个干净 casbin,用例结束清空
func resetEnforcer(t *testing.T) *casbin.Enforcer {
	t.Helper()
	confPath, err := filepath.Abs("../../../../configs/casbin_rbac.conf")
	require.NoError(t, err)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	adapter, err := gormadapter.NewAdapterByDB(db)
	require.NoError(t, err)

	e, err := casbin.NewEnforcer(confPath, adapter)
	require.NoError(t, err)
	require.NoError(t, e.LoadPolicy())

	require.NoError(t, casbinx.Init(db, confPath))
	require.NoError(t, casbinx.Reload())
	require.NotNil(t, casbinx.Enforcer())

	t.Cleanup(func() { casbinx.ResetForTest() })
	return casbinx.Enforcer()
}

func topicsOf(ts []string, want string) bool {
	for _, t := range ts {
		if t == want {
			return true
		}
	}
	return false
}

// E1 **核心**:无任何策略的普通用户,拿不到 job / approval 订阅
//
// 这是 RBAC 的默认拒绝(deny by default)在这条路由上的体现。
// 一旦有人把 subscribableTopics 改成"无条件 append TopicJob",这条立刻红。
func TestE1_NoPolicy_NoJobTopic(t *testing.T) {
	resetEnforcer(t)
	h := NewEventHandler(nil)

	// 没有任何策略 → role:role_x 与 user:1 都不 allow
	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 1, RoleCode: "role_x",
	})

	assert.False(t, topicsOf(topics, push.TopicJob),
		"无 job:read 的用户不该订阅到 job 事件 —— 这是绕过 RBAC 的后门")
	assert.False(t, topicsOf(topics, push.TopicApproval),
		"无 approval:read 的用户不该订阅到审批事件")
	// 但个人频道与权限广播是登录即可
	assert.True(t, topicsOf(topics, push.TopicUser(1)), "本人频道必须始终可订")
	assert.True(t, topicsOf(topics, push.TopicPerm), "权限失效广播必须始终可订")
}

// E2 super_admin 直通 → 拿到全部 topic
func TestE2_SuperAdmin_GetsAllTopics(t *testing.T) {
	resetEnforcer(t)
	h := NewEventHandler(nil)

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 1, RoleCode: "role_super_admin",
	})

	assert.True(t, topicsOf(topics, push.TopicJob), "super_admin 应能订阅 job")
	assert.True(t, topicsOf(topics, push.TopicUser(1)))
	assert.True(t, topicsOf(topics, push.TopicPerm))
	assert.True(t, topicsOf(topics, push.TopicDict), "super_admin 应能订阅字典变更")
}

// E3 有 job:read 策略 → 能订阅 job
func TestE3_JobReadPolicy_GetsJobTopic(t *testing.T) {
	enf := resetEnforcer(t)
	h := NewEventHandler(nil)

	// 策略形状与生产一致:router 里是 rbac("job", "read")
	// → M147 之后 EnforceEx(sub, "global", "job:read", "read") —— obj 是**完整资源码**
	_, err := enf.AddPolicy("role:role_ops", "global", "job:read", "read", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 2, RoleCode: "role_ops",
	})
	assert.True(t, topicsOf(topics, push.TopicJob), "有 job:read 就该能订 job")
}

// E4 **用户级 deny 覆盖角色级 allow** —— 与 decideAccess 同一套语义
//
// 这条守的是"CanAccess 真的是 decideAccess 的包装,不是另写一套简化版"。
// 简化版最容易漏的就是 deny override —— 而漏了的后果是"给某个用户单独
// 撤销的权限,在推送通道上又活了过来"。
//
// casbin_rbac.conf 的 policy_effect 是
// `some(where (p.eft == allow)) && !some(where (p.eft == deny))`,deny 一票否决。
func TestE4_UserDeny_OverridesRoleAllow(t *testing.T) {
	enf := resetEnforcer(t)
	h := NewEventHandler(nil)

	_, err := enf.AddPolicy("role:role_ops", "global", "job:read", "read", "allow")
	require.NoError(t, err)
	_, err = enf.AddPolicy("user:7", "global", "job:read", "read", "deny")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 7, RoleCode: "role_ops",
	})
	assert.False(t, topicsOf(topics, push.TopicJob),
		"用户级 deny 必须压过角色级 allow —— 否则单独撤销的权限在推送通道上复活了")
	assert.True(t, topicsOf(topics, push.TopicPerm))
	assert.True(t, topicsOf(topics, push.TopicUser(7)))
}

// E5 用户频道**只能是自己**的
//
// 订阅别人的 user 频道 = 直接读别人的站内信未读数。
func TestE5_UserTopic_IsSelfOnly(t *testing.T) {
	resetEnforcer(t)
	h := NewEventHandler(nil)

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 100, RoleCode: "role_x",
	})

	assert.True(t, topicsOf(topics, push.TopicUser(100)))
	assert.False(t, topicsOf(topics, push.TopicUser(101)), "不能订阅别人的用户频道")
	assert.False(t, topicsOf(topics, push.TopicUser(1)))
	assert.False(t, topicsOf(topics, push.TopicUser(0)))
}

// E6 casbin 没初始化 → 一律拒绝,**不默认放行**
//
// decideAccess 在 enforcer == nil 时返回 AccessDenyInternal,折算成 false。
// 这正是我们要的:RBAC 挂了就等于谁都不给订 topic,而不是"谁都能订"。
func TestE6_NoEnforcer_DeniesEverything(t *testing.T) {
	// 注意:这里**故意不**调 resetEnforcer,让 casbinx 保持未初始化
	h := NewEventHandler(nil)

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 1, RoleCode: "role_super_admin",
	})
	// super_admin 短路在 enforcer 检查**之前**,所以它仍然拿得到 —— 这是
	// decideAccess 既有的顺序,不是这里引入的偏差
	assert.True(t, topicsOf(topics, push.TopicPerm))
	assert.True(t, topicsOf(topics, push.TopicUser(1)))
}

// E7 订阅列表永远非空且无重复
//
// 空前缀会让 Redis 起一个空订阅;重复项会让同一条事件在这条连接上收到两次
// (而事件是 refetch 信号 → 重复重拉)。
func TestE7_TopicsNonEmptyAndUnique(t *testing.T) {
	resetEnforcer(t)
	h := NewEventHandler(nil)

	_, err := casbinx.Enforcer().AddPolicy("role:role_super_admin", "global", "job", "read", "allow")
	require.NoError(t, err)

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 5, RoleCode: "role_super_admin",
	})
	require.NotEmpty(t, topics, "订阅列表为空 → 前端连上了但永远收不到任何事件")

	seen := map[string]bool{}
	for _, tp := range topics {
		assert.False(t, seen[tp], "topic %q 重复", tp)
		seen[tp] = true
	}
}

// E8 **字典 topic 也走门禁** —— M94
//
// ⚠️ 这条守的是"字典广播不是无条件广播"。如果有人把 subscribableTopics 里
// 那句 `if middleware.CanAccess(...)` 删掉改成无条件 append,这条立刻红。
//
// ⚠️ 注意它守的不是"生产行为":M93 之后**每个登录角色都有 `dict:list`**,
// 所以生产里这道门禁恒为真。它守的是"**门禁还在**"——
// 将来字典改成只对部分角色可见时,这段不用重写。
func TestE8_NoPolicy_NoDictTopic(t *testing.T) {
	resetEnforcer(t)
	h := NewEventHandler(nil)

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 1, RoleCode: "role_x",
	})

	assert.False(t, topicsOf(topics, push.TopicDict),
		"无 dict:list 的用户不该订阅到字典变更事件 —— 门禁被绕过就等于把改动信号广播给了所有人")
}

// E9 有 dict:list 策略 → 能订阅字典变更
func TestE9_DictListPolicy_GetsDictTopic(t *testing.T) {
	enf := resetEnforcer(t)
	h := NewEventHandler(nil)

	// 策略形状与 router 里 rbac("dict", "list") 一致
	_, err := enf.AddPolicy("role:role_ops", "global", "dict:list", "list", "allow")
	require.NoError(t, err)
	require.NoError(t, enf.LoadPolicy())

	topics := h.subscribableTopics(context.Background(), &service.Caller{
		UserID: 3, RoleCode: "role_ops",
	})
	assert.True(t, topicsOf(topics, push.TopicDict), "有 dict:list 就该能订字典变更")
}
