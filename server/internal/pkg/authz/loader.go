// Phase 2.4 — PrincipalLoader 接口 + DBLoader 实现
// 文档:后台权限模型设计V1.0.md §7.1 / §7.2
package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
)

// Loader 加载 Principal(可被缓存装饰)
type Loader interface {
	Load(ctx context.Context, userID, identityID uint64) (*Principal, error)
	// InvalidateUser 清除某**用户**的全部 principal 缓存(所有身份)
	//
	// ⚠️ **失效粒度是用户,不是身份**(2026-10-25 M91 定的口径)。
	//
	// principal 里装的东西几乎全是**用户级**的:grant 挂在 grantee_user_id 上、
	// override 挂在 user_id 上、feature flag 挂在 user_id 上;只有 roleCode /
	// projectIDs 是身份级的。而授权一改就是改"这个人" —— 只清一个身份,
	// 这个用户的**其它身份**继续拿着旧授权,最长一个 TTL(默认 5 分钟)。
	//
	// 反过来多清几个身份没有代价(只是多几次回源),所以接口里只留**粗粒度**一种:
	// 宁可多清,不可漏清。
	InvalidateUser(ctx context.Context, userID uint64) error
}

// DBLoader 直接从 DB 加载(无缓存,适合测试 / 单实例)
type DBLoader struct {
	DB *gorm.DB
}

// NewDBLoader 构造
func NewDBLoader(db *gorm.DB) *DBLoader { return &DBLoader{DB: db} }

// Load 从 DB 加载 active grants / overrides / feature flags
func (l *DBLoader) Load(ctx context.Context, userID, identityID uint64) (*Principal, error) {
	if l.DB == nil {
		return nil, fmt.Errorf("DBLoader: nil db")
	}
	// 1. 查身份(取 roleCode / projectIDs)
	cf, err := l.loadCaller(ctx, userID, identityID)
	if err != nil {
		// 身份不存在 → 映射成 cachex.ErrNotFound。
		//
		// 这样上层缓存能写 null 占位(穿透防御):"这个身份压根不存在"是
		// 确定的事实,没必要为它每次请求都打一遍库(4 条 SQL)。
		//
		// ⚠️ **只认 gorm.ErrRecordNotFound**。连接失败、超时、权限不足等
		// 其它错误**绝不能**被当成"查无此数据" —— 那样一次 DB 抖动会被
		// 缓存成 null 占位 2 分钟,把瞬时故障固化成持续故障。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("identity not found: %w: %w", err, cachex.ErrNotFound)
		}
		return nil, fmt.Errorf("load caller: %w", err)
	}
	p := FromCallerBase(cf)

	// 2. 加载 active grants
	grants, err := l.loadGrants(ctx, userID, identityID)
	if err != nil {
		return nil, fmt.Errorf("load grants: %w", err)
	}
	p.ActiveGrants = grants

	// 3. 加载 overrides(未过期)
	overrides, err := l.loadOverrides(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load overrides: %w", err)
	}
	p.Overrides = overrides

	// 4. 加载 feature flags
	flags, err := l.loadFeatureFlags(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load feature flags: %w", err)
	}
	p.FeatureFlags = flags

	p.LoadedAt = time.Now()
	return p, nil
}

// InvalidateUser DBLoader 无缓存,no-op
func (l *DBLoader) InvalidateUser(_ context.Context, _ uint64) error { return nil }

// loadCaller 从 SysUserIdentity 查身份(不依赖 service 包)
func (l *DBLoader) loadCaller(ctx context.Context, userID, identityID uint64) (CallerFields, error) {
	var id model.SysUserIdentity
	q := l.DB.WithContext(ctx).Where("user_id = ?", userID)
	if identityID > 0 {
		q = q.Where("id = ?", identityID)
	} else {
		// 业务层软约束:service/user_impl.go::ensureSinglePrimary 保证"1 user 1 is_primary=true"
		// (MySQL 无 partial unique index,故 DB 层不加唯一约束)
		q = q.Where("is_primary = ? AND is_active = ?", true, true)
	}
	if err := q.First(&id).Error; err != nil {
		return CallerFields{}, err
	}
	return CallerFields{
		UserID:     id.UserID,
		IdentityID: id.ID,
		RoleCode:   id.RoleCode,
	}, nil
}

func (l *DBLoader) loadGrants(ctx context.Context, userID, identityID uint64) ([]GrantRef, error) {
	now := time.Now()
	var rows []model.PermissionGrant
	q := l.DB.WithContext(ctx).
		Where("grantee_user_id = ? AND status = ?", userID, "active").
		Where("expires_at IS NULL OR expires_at > ?", now)
	if identityID > 0 {
		q = q.Where("grantee_identity_id = 0 OR grantee_identity_id = ?", identityID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]GrantRef, 0, len(rows))
	for _, r := range rows {
		perms := parseJSONArray(r.Permissions)
		out = append(out, GrantRef{
			GrantID:        r.ID,
			GrantType:      r.GrantType,
			GranterUserID:  r.GranterUserID,
			ResourceType:   r.ResourceType,
			ResourceID:     r.ResourceID,
			ScopeProjectID: r.ScopeProjectID,
			Permissions:    perms,
			ExpiresAt:      timePtr(r.ExpiresAt),
		})
	}
	return out, nil
}

func (l *DBLoader) loadOverrides(ctx context.Context, userID uint64) ([]Override, error) {
	now := time.Now()
	var rows []model.UserPermissionOverride
	if err := l.DB.WithContext(ctx).
		Where("user_id = ? AND (expires_at IS NULL OR expires_at > ?)", userID, now).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Override, 0, len(rows))
	for _, r := range rows {
		out = append(out, Override{
			PermissionCode: r.PermissionCode,
			Effect:         r.Effect,
			ExpiresAt:      timePtr(r.ExpiresAt),
		})
	}
	return out, nil
}

func (l *DBLoader) loadFeatureFlags(ctx context.Context, userID uint64) (map[string]bool, error) {
	now := time.Now()
	var rows []model.UserFeatureFlag
	if err := l.DB.WithContext(ctx).
		Where("user_id = ? AND enabled = ? AND (expires_at IS NULL OR expires_at > ?)", userID, true, now).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		// r.Enabled 是 *bool,可能为 nil(无显式设置)
		if r.Enabled != nil && *r.Enabled {
			out[r.FeatureKey] = true
		}
	}
	return out, nil
}

// parseJSONArray 解析 ["a","b"] 形式的 JSON 数组字符串
// 失败/为空时返回空切片(不报错,降级为「无权限」)
func parseJSONArray(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func timePtr(t *model.Time) *time.Time {
	if t == nil {
		return nil
	}
	return &t.Time
}
