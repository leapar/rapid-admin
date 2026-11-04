// Phase 2.6 — PrincipalLoader 单测
// 文档:后台权限模型设计V1.0.md §7
package authz

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/pkg/sonyflake"
)

// newTestDB 内存 sqlite + autoMigrate 相关表
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysUserIdentity{},
		&model.PermissionGrant{},
		&model.UserPermissionOverride{},
		&model.UserFeatureFlag{},
	))
	return db
}

// seedUser 创建 sys_user + sys_user_identity(primary)
func seedUser(t *testing.T, db *gorm.DB, username, roleCode string) (userID, identityID uint64) {
	t.Helper()
	userID = sonyflake.Next()
	identityID = sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: userID},
		Username:  username,
		Nickname:  username,
		IsActive:  true,
	}).Error)
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel:  model.BaseModel{ID: identityID},
		UserID:     userID,
		RoleCode:   roleCode,
		RoleName:   roleCode,
		IsActive:   true,
		IsPrimary:  true,
		ValidFrom:  model.Time{Time: time.Now()},
	}).Error)
	return
}

// TestDBLoader_BasicLoad — 加载空 user(无 grant/override/flag)
func TestDBLoader_BasicLoad(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")

	l := NewDBLoader(db)
	p, err := l.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.Equal(t, userID, p.UserID)
	assert.Equal(t, identityID, p.IdentityID)
	assert.Equal(t, "role_user_admin", p.RoleCode)
	assert.False(t, p.IsSuperAdmin())
	assert.Empty(t, p.ActiveGrants)
	assert.Empty(t, p.Overrides)
	assert.NotNil(t, p.FeatureFlags)
	assert.False(t, p.CacheHit)
}

func TestDBLoader_ActiveGrants(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")

	// grant 1: active + 永不过期
	require.NoError(t, db.Create(&model.PermissionGrant{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		GrantType:    "user_toggle",
		GranteeUserID: userID,
		ResourceType: "house",
		ResourceID:   100,
		Permissions:  `["house:view","house:delivery:sign"]`,
		Status:       "active",
	}).Error)
	// grant 2: 已过期
	require.NoError(t, db.Create(&model.PermissionGrant{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		GrantType:    "user_toggle",
		GranteeUserID: userID,
		ResourceType: "house",
		ResourceID:   200,
		Permissions:  `["house:view"]`,
		Status:       "active",
		ExpiresAt:    &model.Time{Time: time.Now().Add(-time.Hour)},
	}).Error)
	// grant 3: revoked
	require.NoError(t, db.Create(&model.PermissionGrant{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		GrantType:    "user_toggle",
		GranteeUserID: userID,
		ResourceType: "house",
		ResourceID:   300,
		Permissions:  `["house:view"]`,
		Status:       "revoked",
	}).Error)

	l := NewDBLoader(db)
	p, err := l.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.Len(t, p.ActiveGrants, 1, "只 active+未过期 才计入")
	assert.Equal(t, uint64(100), p.ActiveGrants[0].ResourceID)
	assert.Equal(t, []string{"house:view", "house:delivery:sign"}, p.ActiveGrants[0].Permissions)
}

func TestDBLoader_OverridesDeny(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")

	// allow + deny + 过期 deny
	require.NoError(t, db.Create(&model.UserPermissionOverride{
		BaseModel:      model.BaseModel{ID: sonyflake.Next()},
		UserID:         userID,
		PermissionCode: "user:delete",
		Effect:         "allow",
		CreatedBy:      1,
	}).Error)
	require.NoError(t, db.Create(&model.UserPermissionOverride{
		BaseModel:      model.BaseModel{ID: sonyflake.Next()},
		UserID:         userID,
		PermissionCode: "user:create",
		Effect:         "deny",
		Reason:         "风控",
		CreatedBy:      1,
	}).Error)

	l := NewDBLoader(db)
	p, err := l.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.True(t, p.HasDenyOverride("user:create"))
	assert.False(t, p.HasDenyOverride("user:delete"), "allow 不算 deny")
	assert.False(t, p.HasDenyOverride("user:nonexistent"))
}

func TestDBLoader_FeatureFlags(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")

	trueVal, falseVal := true, false
	require.NoError(t, db.Create(&model.UserFeatureFlag{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		UserID:       userID,
		FeatureKey:   "ai_inspection_assistant",
		Enabled:      &trueVal,
		RolloutGroup: "beta",
		EnabledBy:    1,
	}).Error)
	require.NoError(t, db.Create(&model.UserFeatureFlag{
		BaseModel:  model.BaseModel{ID: sonyflake.Next()},
		UserID:     userID,
		FeatureKey: "wechat_smart_home",
		Enabled:    &falseVal,
		EnabledBy:  1,
	}).Error)

	l := NewDBLoader(db)
	p, err := l.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	t.Logf("  loaded flags: %+v", p.FeatureFlags)
	assert.True(t, p.HasFeature("ai_inspection_assistant"))
	assert.False(t, p.HasFeature("wechat_smart_home"), "enabled=false 不开")
	assert.False(t, p.HasFeature("not_set"))
}

// ----- CachedLoader 单测(M90:缓存后端换成 cachex.Backend)-----

// newTestCache 造一个基于内存后端的 cachex.Safe
//
// 2026-10-25 M90 之前这里是个自制的 memCache(mock 的是 authz.CacheBackend 那套
// 自己的 Redis 适配接口)。迁移之后缓存逻辑归 cachex 管,authz 这边只剩
// "拿一个 *cachex.Safe" —— 所以测试也直接用 cachex 的内存后端,不再 mock 一层。
func newTestCache() (*cachex.Safe, *cachex.MemoryBackend) {
	b := cachex.NewMemoryBackend()
	return cachex.NewWithBackend(b), b
}

func TestCachedLoader_HitMiss(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")
	cache, _ := newTestCache()
	inner := NewDBLoader(db)
	cl := NewCachedLoader(inner, cache, time.Minute)

	// 第一次:miss → 走 DB
	p1, err := cl.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.False(t, p1.CacheHit)

	// 第二次:hit
	p2, err := cl.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.True(t, p2.CacheHit)
	assert.Equal(t, p1.UserID, p2.UserID)
}

// TestCachedLoader_InvalidateUser — 失效后再次 miss
func TestCachedLoader_InvalidateUser(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")
	cache, _ := newTestCache()
	cl := NewCachedLoader(NewDBLoader(db), cache, time.Minute)

	_, err := cl.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	require.NoError(t, cl.InvalidateUser(context.Background(), userID))

	p2, err := cl.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.False(t, p2.CacheHit, "invalidate 后应 miss")
}

// TestCachedLoader_InvalidateUser_ClearsAllIdentities
//
// **这条是 M91 的核心断言**:一个用户可以有多条身份(sys_user_identity 一人多行,
// is_primary 只有一条),而**授权是挂在用户上的**(grant 记 grantee_user_id)。
// 所以"撤销授权"必须清掉这个人**所有身份**的缓存 —— 只清一条身份,
// 对方换一个身份进来照样能用那条被撤销的授权。
//
// 修复前的行为:失效只清"主身份"那一条 → 非主身份的缓存原封不动,
// 被撤销的授权最长一个 TTL(默认 5 分钟)仍然放行。
func TestCachedLoader_InvalidateUser_ClearsAllIdentities(t *testing.T) {
	db := newTestDB(t)
	userID, primaryID := seedUser(t, db, "u1", "role_user_admin")
	// 第二个身份(非主)
	secondID := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUserIdentity{
		BaseModel:  model.BaseModel{ID: secondID},
		UserID:     userID,
		RoleCode:   "role_user_admin",
		RoleName:   "role_user_admin",
		IsActive:   true,
		IsPrimary:  false,
		ValidFrom:  model.Time{Time: time.Now()},
	}).Error)
	// 另一个用户(不该被牵连)
	otherUserID, otherIdentityID := seedUser(t, db, "u2", "role_user_admin")

	cache, _ := newTestCache()
	cl := NewCachedLoader(NewDBLoader(db), cache, time.Minute)
	ctx := context.Background()

	// 三条都先加载进缓存
	for _, c := range []struct{ u, i uint64 }{
		{userID, primaryID}, {userID, secondID}, {otherUserID, otherIdentityID},
	} {
		_, err := cl.Load(ctx, c.u, c.i)
		require.NoError(t, err)
	}
	// 前置条件:三条都在缓存里
	for _, c := range []struct{ u, i uint64 }{
		{userID, primaryID}, {userID, secondID}, {otherUserID, otherIdentityID},
	} {
		p, err := cl.Load(ctx, c.u, c.i)
		require.NoError(t, err)
		require.True(t, p.CacheHit, "前置条件:应命中缓存")
	}

	require.NoError(t, cl.InvalidateUser(ctx, userID))

	// 该用户的**两个身份**都必须 miss
	for _, identityID := range []uint64{primaryID, secondID} {
		p, err := cl.Load(ctx, userID, identityID)
		require.NoError(t, err)
		assert.False(t, p.CacheHit,
			"身份 %d 的缓存没被清掉 —— 被撤销的授权还能用最长一个 TTL", identityID)
	}
	// 另一个用户不受影响
	p, err := cl.Load(ctx, otherUserID, otherIdentityID)
	require.NoError(t, err)
	assert.True(t, p.CacheHit, "失效只应影响目标用户,别人不该被清缓存")
}

// TestCachedLoader_InvalidateUser_PrefixIsolation 前缀不能误伤别的用户
//
// key 是 principal:{userID}:{identityID}。如果前缀写成 "principal:1"
// (少了末尾冒号),失效用户 1 会把用户 12 的缓存一起删掉 ——
// 删多了和没删一样危险:一个人被清了权限,现场极难查。
//
// ⚠️ 方向别搞反:这里失效的是**用户 1**(短的那个),因为只有"短前缀撞长 key"
// 才是真的撞 —— "principal:1*" 会匹配上 "principal:12:22"。
func TestCachedLoader_InvalidateUser_PrefixIsolation(t *testing.T) {
	cache, _ := newTestCache()
	ctx := context.Background()
	// 直接往缓存里塞两条,绕开 loader
	require.NoError(t, cache.Set(ctx, "principal:1:11", "x", time.Minute))
	require.NoError(t, cache.Set(ctx, "principal:12:22", "y", time.Minute))

	cl := NewCachedLoader(LoaderFunc(func(_ context.Context, u, i uint64) (*Principal, error) {
		return &Principal{UserID: u, IdentityID: i}, nil
	}), cache, time.Minute)

	require.NoError(t, cl.InvalidateUser(ctx, 1))

	hit, err := cache.Get(ctx, "principal:1:11", new(string))
	require.NoError(t, err)
	assert.False(t, hit, "用户 1 的缓存应被删掉")
	hit, err = cache.Get(ctx, "principal:12:22", new(string))
	require.NoError(t, err)
	assert.True(t, hit, "用户 12 的缓存不该被误删 —— 前缀匹配过宽")
}

// TestCachedLoader_InvalidateUser_BackendWithoutScanner 后端不支持扫描时必须报错
//
// cachex.DeletePrefix 在后端不支持 SCAN 时会 fail-open 返回 (0, nil)
// (语义是"没有可枚举的 key")。对"撤销授权"这种安全动作,静默什么都没删
// = 权限没撤销 —— 所以这里必须**报错**,而不是假装成功。
func TestCachedLoader_InvalidateUser_BackendWithoutScanner(t *testing.T) {
	cl := NewCachedLoader(LoaderFunc(func(_ context.Context, u, i uint64) (*Principal, error) {
		return &Principal{UserID: u, IdentityID: i}, nil
	}), cachex.NewWithBackend(&noScanBackend{Backend: cachex.NewMemoryBackend()}), time.Minute)

	err := cl.InvalidateUser(context.Background(), 1)
	require.Error(t, err, "后端不支持前缀失效时必须报错,不能静默返回成功")
}

// noScanBackend 只实现 Backend,故意不实现 Scanner
//
// ⚠️ 嵌入的是 **Backend 接口**而不是 MemoryBackend 具体类型 ——
// 嵌具体类型会把它的 Scan/Ping 方法一起带上来,那它就"支持扫描"了,
// 这个 fake 也就失去意义。
type noScanBackend struct{ cachex.Backend }

// TestCachedLoader_Singleflight — 100 并发同 key 只查 1 次 DB
func TestCachedLoader_Singleflight(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")
	cache, _ := newTestCache()

	// 包装 inner 计数 Load 调用次数
	var calls int32
	wrapped := LoaderFunc(func(ctx context.Context, u, i uint64) (*Principal, error) {
		atomic.AddInt32(&calls, 1)
		// 模拟慢查询,让 singleflight 生效
		time.Sleep(20 * time.Millisecond)
		return NewDBLoader(db).Load(ctx, u, i)
	})
	cl := NewCachedLoader(wrapped, cache, time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cl.Load(context.Background(), userID, identityID)
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls),
		"100 并发同 key 应只查 1 次 DB,实际 %d", calls)
}

// TestCachedLoader_IdentityNotFound_NullSentinel
//
// 身份不存在 → 写 null 占位 → 后续请求不再打库。
//
// 这是 M90 之前**没有**的能力:老的 authz 缓存只存"查到了什么",
// 查不到就每次都打库(4 条 SQL),不存在的身份 ID 打成流量放大器。
//
// 断言点:DB 只被查一次(第二次是占位命中),且两次都返回 (nil, nil) ——
// "确认没有这个身份"是事实,不是错误。
func TestCachedLoader_IdentityNotFound_NullSentinel(t *testing.T) {
	db := newTestDB(t)
	cache, _ := newTestCache()

	var calls int32
	wrapped := LoaderFunc(func(ctx context.Context, u, i uint64) (*Principal, error) {
		atomic.AddInt32(&calls, 1)
		return NewDBLoader(db).Load(ctx, u, i)
	})
	cl := NewCachedLoader(wrapped, cache, time.Minute)

	// 这个身份不存在
	p1, err := cl.Load(context.Background(), 999999, 888888)
	require.NoError(t, err, "查无此身份不该是错误")
	assert.Nil(t, p1, "查无此身份应返 nil principal")

	p2, err := cl.Load(context.Background(), 999999, 888888)
	require.NoError(t, err)
	assert.Nil(t, p2, "null 占位命中仍应返 nil principal")

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls),
		"null 占位应挡住后续回源,实际查了 %d 次", calls)
}

// TestCachedLoader_Uint64Precision 缓存往返后 ID 精度不能丢
//
// principal 里装的是 UserID / GrantID / ResourceID,全是 uint64。
// 如果缓存走 any 中转(float64),超过 2^53 的 ID 会被写成 1.15e+18,
// 再反序列化直接失败 → 每次都退回回源,缓存形同虚设。
func TestCachedLoader_Uint64Precision(t *testing.T) {
	const bigID uint64 = 1<<60 + 12345
	cache, _ := newTestCache()

	var calls int32
	wrapped := LoaderFunc(func(ctx context.Context, u, i uint64) (*Principal, error) {
		atomic.AddInt32(&calls, 1)
		return &Principal{UserID: bigID, IdentityID: i, RoleCode: "role_x"}, nil
	})
	cl := NewCachedLoader(wrapped, cache, time.Minute)

	p1, err := cl.Load(context.Background(), bigID, 1)
	require.NoError(t, err)
	assert.Equal(t, bigID, p1.UserID)

	p2, err := cl.Load(context.Background(), bigID, 1)
	require.NoError(t, err)
	assert.True(t, p2.CacheHit, "第二次应命中缓存(反序列化失败会退回回源)")
	assert.Equal(t, bigID, p2.UserID, "缓存往返后 uint64 精度不能丢")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "只该回源一次")
}

// TestCachedLoader_NilCache_Passthrough 没配缓存时每次都回源
func TestCachedLoader_NilCache_Passthrough(t *testing.T) {
	db := newTestDB(t)
	userID, identityID := seedUser(t, db, "u1", "role_user_admin")
	cl := NewCachedLoader(NewDBLoader(db), nil, time.Minute)

	p1, err := cl.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.False(t, p1.CacheHit)

	p2, err := cl.Load(context.Background(), userID, identityID)
	require.NoError(t, err)
	assert.False(t, p2.CacheHit, "没配缓存就不该有命中")

	// InvalidateUser 也不能炸(没配缓存 = 没有可失效的东西,不是错误)
	require.NoError(t, cl.InvalidateUser(context.Background(), userID))
}

// LoaderFunc 函数式 loader(单测用)
type LoaderFunc func(ctx context.Context, userID, identityID uint64) (*Principal, error)

func (f LoaderFunc) Load(ctx context.Context, u, i uint64) (*Principal, error) {
	return f(ctx, u, i)
}
func (LoaderFunc) InvalidateUser(_ context.Context, _ uint64) error { return nil }
