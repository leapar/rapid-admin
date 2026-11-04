// 权限扩展模型 — permission_grant / user_permission_override / user_feature_flag
// 文档:后台权限模型设计V1.0.md §5
//
// 作用:
//   - permission_grant 资源级授权(B/C 场景:业主甲对业主乙的某房屋授权)
//   - user_permission_override 单点 deny/allow(F 场景:风控拒绝覆盖)
//   - user_feature_flag 个人功能开关(E 场景:灰度 / 个人化)
//
// 与 casbin_rule 关系:本批表都是应用层,casbin_rule 不动;
// rbac.go 读 Principal 时会调 PrincipalLoader 加载这些,见 Phase 2.4
package model

// PermissionGrant 资源级授权(场景 B/C 核心载体)
//   - grant_type: role_override / user_toggle / delegation / family_invite / project_grant
//   - granter_user_id: 授权人(系统策略时可空)
//   - grantee_user_id: 被授权人
//   - grantee_identity_id: 限定到具体身份,空=任意身份
//   - resource_type + resource_id: 资源范围(空 resource_id = 该类型全部)
//   - scope_project_id: 项目域(空=不限)
//   - permissions: json 数组,权限码白名单
//   - constraints: json,包含 expires_at / max_uses / ip_whitelist / time_range
//   - status: pending / active / revoked / expired
type PermissionGrant struct {
	BaseModel
	GrantType          string `gorm:"size:32;not null;index" json:"grant_type"`
	GranterUserID      uint64 `gorm:"index" json:"granter_user_id,string"`
	GranteeUserID      uint64 `gorm:"index;not null" json:"grantee_user_id,string"`
	GranteeIdentityID  uint64 `gorm:"index" json:"grantee_identity_id,string"`
	ResourceType       string `gorm:"size:32;index" json:"resource_type"`
	ResourceID         uint64 `gorm:"index" json:"resource_id,string"`
	ScopeProjectID     uint64 `gorm:"index" json:"scope_project_id,string"`
	Permissions        string `gorm:"type:json" json:"permissions"`     // JSON 数组字符串
	Constraints        string `gorm:"type:json" json:"constraints"`     // JSON 对象字符串
	Status             string `gorm:"size:16;not null;index" json:"status"`
	Note               string `gorm:"size:500" json:"note"`
	ActivatedAt        *Time  `json:"activated_at"`
	RevokedAt          *Time  `json:"revoked_at"`
	RevokedBy          uint64 `json:"revoked_by,string"`
	RevokeReason       string `gorm:"size:255" json:"revoke_reason"`
	ExpiresAt          *Time  `gorm:"index" json:"expires_at"`
}

func (PermissionGrant) TableName() string { return "auth_permission_grant" }

// UserPermissionOverride 单点 deny/allow(场景 F 风控优先级)
//   - effect: allow / deny
//   - reason: 风控 / 临时授权原因
//   - expires_at: 临时性,过期自动失效
type UserPermissionOverride struct {
	BaseModel
	UserID        uint64 `gorm:"index;not null;uniqueIndex:uk_user_perm,priority:1" json:"user_id,string"`
	PermissionCode string `gorm:"size:64;not null;uniqueIndex:uk_user_perm,priority:2" json:"permission_code"`
	Effect        string `gorm:"size:8;not null" json:"effect"` // allow / deny
	Reason        string `gorm:"size:255" json:"reason"`
	ExpiresAt     *Time  `gorm:"index" json:"expires_at"`
	CreatedBy     uint64 `gorm:"index" json:"created_by,string"`
}

func (UserPermissionOverride) TableName() string { return "auth_user_permission_override" }

// UserFeatureFlag 个人功能开关(场景 E 灰度)
//   - feature_key: ai_inspection_assistant / wechat_smart_home / family_share_qr ...
//   - rollout_group: beta / canary / production(灰度分组)
//   - enabled_by: 平台超管/运维
//
// 注:Enabled 用 *bool 而非 bool — 避免 GORM "default:true" 把显式 false 当零值覆盖
// 业务逻辑:有记录 = 显式设置(无论 true/false);无记录 = 视为关闭
type UserFeatureFlag struct {
	BaseModel
	UserID       uint64 `gorm:"index;not null;uniqueIndex:uk_user_feature,priority:1" json:"user_id,string"`
	FeatureKey   string `gorm:"size:64;not null;uniqueIndex:uk_user_feature,priority:2" json:"feature_key"`
	Enabled      *bool  `gorm:"not null;index" json:"enabled"`
	EnabledBy    uint64 `gorm:"index" json:"enabled_by,string"`
	ExpiresAt    *Time  `gorm:"index" json:"expires_at"`
	RolloutGroup string `gorm:"size:32" json:"rollout_group"`
}

func (UserFeatureFlag) TableName() string { return "auth_user_feature_flag" }
