// 枚举常量
package model

// 角色编码(开源口径:框架内置 3 角色 —— 超管直通全量 /
// 平台运营员框架读写 / 只读查看员框架只读;业务方接入自己的模块后在这里加)
const (
	RoleSuperAdmin  = "role_super_admin"  // 超级管理员
	RolePlatformOps = "role_platform_ops" // 平台运营员
	RoleReadOnly    = "role_readonly"     // 只读查看员
)

// 角色类型(sys_role.type 列,2026-10-17 新增,文档 §6.4)
const (
	RoleTypePlatform = "platform" // 平台级:超管/平台运营
	RoleTypeBusiness = "business" // 业务级:业务方接入自己的模块后使用
)

// 通用状态
type BizStatus int

const (
	StatusDisabled BizStatus = 0
	StatusEnabled  BizStatus = 1
	StatusArchived BizStatus = 2
)

// 配置中心 - 变更状态机
type ConfigChangeStatus string

const (
	ConfigChangePending   ConfigChangeStatus = "pending"   // 待审核
	ConfigChangeApproved  ConfigChangeStatus = "approved"  // 已通过
	ConfigChangeRejected  ConfigChangeStatus = "rejected"  // 驳回
	ConfigChangeActivated ConfigChangeStatus = "activated" // 已生效
)

// 配置值类型
type ConfigValueType string

const (
	ConfigTypeString     ConfigValueType = "string"
	ConfigTypeInt        ConfigValueType = "int"
	ConfigTypeBool       ConfigValueType = "bool"
	ConfigTypeJSON       ConfigValueType = "json"
	ConfigTypeSecret     ConfigValueType = "secret"
	ConfigTypeCertPEM    ConfigValueType = "cert_pem"
	ConfigTypePrivateKey ConfigValueType = "private_key_pem"
)

// 生效方式
type ConfigEffectiveType string

const (
	ConfigEffectiveImmediate   ConfigEffectiveType = "immediate"
	ConfigEffectiveNextRestart ConfigEffectiveType = "next_restart"
	ConfigEffectiveScheduled   ConfigEffectiveType = "scheduled"
)
