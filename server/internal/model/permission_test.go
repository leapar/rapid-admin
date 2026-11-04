// Phase 2.1-2.3 model 单测 — 验证表名 + 关键字段约束
package model

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPermission_TableNames(t *testing.T) {
	cases := []struct {
		name     string
		model    any
		expected string
	}{
		{"PermissionGrant", PermissionGrant{}, "auth_permission_grant"},
		{"UserPermissionOverride", UserPermissionOverride{}, "auth_user_permission_override"},
		{"UserFeatureFlag", UserFeatureFlag{}, "auth_user_feature_flag"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			type tabler interface{ TableName() string }
			tab, ok := c.model.(tabler)
			assert.True(t, ok, "model must implement TableName()")
			assert.Equal(t, c.expected, tab.TableName())
		})
	}
}

func TestUserPermissionOverride_UniqueIndex(t *testing.T) {
	typ := reflect.TypeOf(UserPermissionOverride{})
	uidTag := typ.Field(0).Tag // BaseModel.ID
	_ = uidTag
	// 找到 UserID 和 PermissionCode 字段
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		switch f.Name {
		case "UserID":
			assert.Contains(t, f.Tag.Get("gorm"), "uniqueIndex:uk_user_perm",
				"UserID 应有 uk_user_perm 唯一索引")
		case "PermissionCode":
			assert.Contains(t, f.Tag.Get("gorm"), "uniqueIndex:uk_user_perm",
				"PermissionCode 应共享 uk_user_perm 唯一索引")
		}
	}
}

func TestUserFeatureFlag_UniqueIndex(t *testing.T) {
	typ := reflect.TypeOf(UserFeatureFlag{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		switch f.Name {
		case "UserID":
			assert.Contains(t, f.Tag.Get("gorm"), "uniqueIndex:uk_user_feature",
				"UserID 应有 uk_user_feature 唯一索引")
		case "FeatureKey":
			assert.Contains(t, f.Tag.Get("gorm"), "uniqueIndex:uk_user_feature",
				"FeatureKey 应共享 uk_user_feature 唯一索引")
		}
	}
}

// TestPermissionGrant_KeyFields 验证关键字段存在(防止重构时漏字段)
func TestPermissionGrant_KeyFields(t *testing.T) {
	typ := reflect.TypeOf(PermissionGrant{})
	required := []string{"GrantType", "GranterUserID", "GranteeUserID", "ResourceType", "ResourceID", "Permissions", "Status"}
	have := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		have[typ.Field(i).Name] = true
	}
	for _, r := range required {
		assert.True(t, have[r], "PermissionGrant 应有字段 %s", r)
	}
}
