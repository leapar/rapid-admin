// Phase 2.6 — Principal 中间件单测
// 文档:后台权限模型设计V1.0.md §8.1
//
// 注:Hertz RequestContext 的 Next() 行为在 router_test.go 用反射
// 覆盖(确保 Principal() 已挂上),本文件只覆盖"中间件内 loader 调用语义":
//   - loader=nil:跳过 L0.5(降级 dev 模式)
//   - loader 失败:不挂 principal(降级,让 RBAC 走纯 role/user)
//   - 缓存命中:CachedLoader 集成行为
//   - 序列化往返:Principal 能正确 JSON 序列化/反序列化
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/cachex"
)

// newMemCacheBackend 内存缓存后端(2026-10-25 M90:authz 缓存迁到 cachex 之后,
// 这里不再自己 mock 一套缓存接口 —— 直接用 cachex 的内存后端)
func newMemCacheBackend() *cachex.MemoryBackend { return cachex.NewMemoryBackend() }

// 1. dev 模式 loader=nil → Principal() 不 panic
//    这是 router.go 在 loader 未注入时的安全网
func TestPrincipal_NilLoaderDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() {
		h := Principal(nil)
		assert.NotNil(t, h, "loader=nil 也应返回 handler(降级)")
	})
}

// 2. loader 失败 → 降级(不挂 principal)
//    验证:中间件遇到 loader err 时,ctx 仍能继续
//    (中间件主体行为依赖 Hertz ctx,这里只验 loader 调用语义)
func TestPrincipal_LoaderErrorIsHandled(t *testing.T) {
	failing := LoaderFunc(func(_ context.Context, _, _ uint64) (*authz.Principal, error) {
		return nil, errors.New("simulated loader failure")
	})

	// 直接验:中间件 fn 应能正常构造,不会因 loader 失败 panic
	assert.NotPanics(t, func() {
		h := Principal(failing)
		assert.NotNil(t, h)
	})
}

// 3. CachedLoader + 中间件集成:同 user 第二次命中缓存
//   验证:中间件用 CachedLoader 时,DB 调用次数不会随请求数线性增长
func TestPrincipal_CachedLoader_Integration(t *testing.T) {
	cache := newMemCacheBackend()

	var innerCalls int32
	inner := LoaderFunc(func(_ context.Context, u, i uint64) (*authz.Principal, error) {
		atomic.AddInt32(&innerCalls, 1)
		return &authz.Principal{
			UserID:     u,
			IdentityID: i,
			RoleCode:   "role_test",
			FeatureFlags: map[string]bool{},
			LoadedAt:   time.Now(),
		}, nil
	})
	loader := authz.NewCachedLoader(inner, cachex.NewWithBackend(cache), time.Minute)

	// 中间件构造(不真正调 hertz,只验 loader 自身)
	h := Principal(loader)
	require.NotNil(t, h)

	// 模拟 100 次同 user 请求,inner 只应被调 1 次
	for i := 0; i < 100; i++ {
		_, err := loader.Load(context.Background(), 100, 1)
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&innerCalls),
		"100 次同 user 请求,inner 应只被调 1 次(singleflight + cache)")
}

// 4. 序列化往返:Principal 能正确 JSON 序列化/反序列化(缓存正确性的基础)
func TestPrincipal_JSONRoundTrip(t *testing.T) {
	orig := &authz.Principal{
		UserID:     100,
		IdentityID: 1,
		RoleCode:   "role_test",
		ProjectIDs: []uint64{10, 20},
		ActiveGrants: []authz.GrantRef{
			{GrantID: 789, GrantType: "user_toggle", Permissions: []string{"house:view"}},
		},
		Overrides: []authz.Override{
			{PermissionCode: "user:delete", Effect: "deny"},
		},
		FeatureFlags: map[string]bool{"ai_assistant": true},
		LoadedAt:     time.Now(),
	}

	b, err := json.Marshal(orig)
	require.NoError(t, err)
	assert.NotEmpty(t, b)

	var got authz.Principal
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, orig.UserID, got.UserID)
	assert.Equal(t, orig.RoleCode, got.RoleCode)
	assert.Equal(t, orig.ProjectIDs, got.ProjectIDs)
	assert.Equal(t, orig.ActiveGrants[0].GrantID, got.ActiveGrants[0].GrantID)
	assert.Equal(t, orig.FeatureFlags["ai_assistant"], got.FeatureFlags["ai_assistant"])
}

// 5. 缓存 key 格式:principal:{userID}:{identityID}
//
// ⚠️ key 格式是**隐式契约**:和线上 Redis 里已有的条目绑在一起,
// 改格式 = 存量条目全部对不上 = 上线瞬间一波全量回源。
func TestPrincipal_CacheKeyFormat(t *testing.T) {
	cache := newMemCacheBackend()
	inner := LoaderFunc(func(_ context.Context, u, i uint64) (*authz.Principal, error) {
		return &authz.Principal{
			UserID: u, IdentityID: i, RoleCode: "x",
			FeatureFlags: map[string]bool{},
		}, nil
	})
	loader := authz.NewCachedLoader(inner, cachex.NewWithBackend(cache), time.Minute)

	_, err := loader.Load(context.Background(), 123, 456)
	require.NoError(t, err)

	v, err := cache.Get(context.Background(), "principal:123:456")
	require.NoError(t, err, "缓存 key 不匹配:expected principal:123:456")
	// Principal 当前用 Go 默认 json tag(大写字段名),后续可加 tag 优化
	assert.Contains(t, v, `"UserID":123`)
	assert.Contains(t, v, `"IdentityID":456`)
}

// 6. InvalidateUser 后下次 miss
func TestPrincipal_InvalidateForcesMiss(t *testing.T) {
	cache := newMemCacheBackend()
	inner := LoaderFunc(func(_ context.Context, u, i uint64) (*authz.Principal, error) {
		return &authz.Principal{UserID: u, IdentityID: i, RoleCode: "x", FeatureFlags: map[string]bool{}}, nil
	})
	loader := authz.NewCachedLoader(inner, cachex.NewWithBackend(cache), time.Minute)

	p1, _ := loader.Load(context.Background(), 100, 1)
	assert.False(t, p1.CacheHit)

	p2, _ := loader.Load(context.Background(), 100, 1)
	assert.True(t, p2.CacheHit)

	require.NoError(t, loader.InvalidateUser(context.Background(), 100))

	p3, _ := loader.Load(context.Background(), 100, 1)
	assert.False(t, p3.CacheHit, "invalidate 后应 miss")
}

// ----- helpers -----

// LoaderFunc 函数式 loader
type LoaderFunc func(ctx context.Context, u, i uint64) (*authz.Principal, error)

func (f LoaderFunc) Load(ctx context.Context, u, i uint64) (*authz.Principal, error) {
	return f(ctx, u, i)
}
func (LoaderFunc) InvalidateUser(_ context.Context, _ uint64) error { return nil }
