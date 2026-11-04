// Casbin conf + enforcer 行为测试
// 文档:后台权限模型设计V1.0.md §6
//
// 真实脚本:用 in-memory sqlite + 真实 gorm-adapter 加载同一份 conf,断言
//   1) allow 策略生效
//   2) deny 策略生效
//   3) deny 覆盖 allow(policy_effect 正确)
//   4) p.dom="global" 跨 r.dom 通配
//   5) p.dom="proj_001" 不跨 r.dom 通配
package casbinx

import (
	"path/filepath"
	"testing"

	"github.com/casbin/casbin/v3"
	gormadapter "github.com/casbin/gorm-adapter/v3"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// newTestEnforcer 用临时 sqlite + 当前 conf 构造一个 enforcer(每个 case 独立)
func newTestEnforcer(t *testing.T) *casbin.Enforcer {
	t.Helper()

	// 内存 sqlite,每个测试独立 DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	adapter, err := gormadapter.NewAdapterByDB(db)
	assert.NoError(t, err)

	// conf 相对 server/configs,go test 从包目录跑 — 用 ../../../configs 回到根
	confPath, err := filepath.Abs("../../../configs/casbin_rbac.conf")
	assert.NoError(t, err)

	e, err := casbin.NewEnforcer(confPath, adapter)
	assert.NoError(t, err)
	assert.NoError(t, e.LoadPolicy())
	return e
}

func TestEnforcer_AllowWins(t *testing.T) {
	e := newTestEnforcer(t)

	// 加 allow 策略:role:house_admin 可读所有 biz/house
	_, err := e.AddPolicy("role:house_admin", "global", "/rapid-admin/v1/biz/house/*", "GET", "allow")
	assert.NoError(t, err)
	assert.NoError(t, e.LoadPolicy())

	ok, err := e.Enforce("role:house_admin", "global", "/rapid-admin/v1/biz/house/123", "GET")
	assert.NoError(t, err)
	assert.True(t, ok, "allow 策略应通过")
}

func TestEnforcer_DenyOverridesAllow(t *testing.T) {
	e := newTestEnforcer(t)

	// 角色层 allow
	_, err := e.AddPolicy("role:house_admin", "global", "/rapid-admin/v1/biz/house/*", "GET", "allow")
	assert.NoError(t, err)
	// 用户层 deny(对应 §5.2 user_permission_override → casbin_rule 同步)
	_, err = e.AddPolicy("user:123", "global", "/rapid-admin/v1/biz/house/*", "GET", "deny")
	assert.NoError(t, err)
	assert.NoError(t, e.LoadPolicy())

	// 单独 Enforce role:house_admin 仍 allow
	ok, _ := e.Enforce("role:house_admin", "global", "/rapid-admin/v1/biz/house/1", "GET")
	assert.True(t, ok)

	// user:123 也直接 deny 命中
	ok, _ = e.Enforce("user:123", "global", "/rapid-admin/v1/biz/house/1", "GET")
	assert.False(t, ok, "显式 deny 必须赢")
}

func TestEnforcer_GlobalDomainMatchesAnyRequestDomain(t *testing.T) {
	e := newTestEnforcer(t)

	// p.dom="global" 的策略应跨 r.dom 生效
	_, err := e.AddPolicy("role:platform_admin", "global", "/rapid-admin/v1/system/*", "*", "allow")
	assert.NoError(t, err)
	assert.NoError(t, e.LoadPolicy())

	ok, _ := e.Enforce("role:platform_admin", "proj_001", "/rapid-admin/v1/system/users", "GET")
	assert.True(t, ok, "p.dom=global 应匹配任意 r.dom")
}

func TestEnforcer_SpecificDomainDoesNotLeak(t *testing.T) {
	e := newTestEnforcer(t)

	// p.dom="proj_001" 的策略不应跨到 r.dom="proj_002"
	_, err := e.AddPolicy("role:owner", "proj_001", "/rapid-admin/v1/biz/house/*", "GET", "allow")
	assert.NoError(t, err)
	assert.NoError(t, e.LoadPolicy())

	ok, _ := e.Enforce("role:owner", "proj_001", "/rapid-admin/v1/biz/house/1", "GET")
	assert.True(t, ok, "同域应通过")

	ok, _ = e.Enforce("role:owner", "proj_002", "/rapid-admin/v1/biz/house/1", "GET")
	assert.False(t, ok, "跨域应被拒绝")
}

func TestEnforcer_GrantSubEnforced(t *testing.T) {
	// 验证 §6.2 grant:789 sub 路径(L1 grant 校验入口)
	e := newTestEnforcer(t)

	// 业主 owner_grant 场景:grant 789 允许读 house 123
	_, err := e.AddPolicy("grant:789", "global", "/rapid-admin/v1/biz/house/123", "GET", "allow")
	assert.NoError(t, err)
	assert.NoError(t, e.LoadPolicy())

	ok, _ := e.Enforce("grant:789", "global", "/rapid-admin/v1/biz/house/123", "GET")
	assert.True(t, ok, "grant sub 应能通过对应路径")

	// grant 789 不能跨到其他 house
	ok, _ = e.Enforce("grant:789", "global", "/rapid-admin/v1/biz/house/124", "GET")
	assert.False(t, ok, "grant 仅限其授权的资源")
}

// TestInit_FailedInitDoesNotPretendSuccess Init 失败后再次调用不得返 nil。
//
// 曾经 sync.Once + 函数局部 err:首次失败后 once 已消费、err 随栈帧丢弃,
// 第二次调用直接返 nil(假成功)—— 调用方以为初始化好了,实际 enforcer
// 为 nil,全部鉴权调用报 "casbin not initialized"(审计报告 casbinx #1)。
// 现语义:失败可重试,重试再失败返回最新一次的错误。
func TestInit_FailedInitDoesNotPretendSuccess(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	badConf := filepath.Join(t.TempDir(), "not-exist.conf")
	err1 := Init(db, badConf)
	assert.Error(t, err1, "首次 Init(不存在的 conf)必须返回错误")

	err2 := Init(db, badConf)
	assert.Error(t, err2, "Init 失败后再次调用不得返 nil(假成功)")
	assert.Nil(t, Enforcer(), "失败的 Init 不得留下 enforcer")
}
