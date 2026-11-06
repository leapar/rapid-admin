// ConfigCenterService 平台配置中心 —— AES-256-GCM 字段级加密 + 只读 audit
// 2026-10-18 收窄:仅保留 update 通道
//   - 用户不会创建/删除配置项(走 seed.go + 软删 is_active=false)
//   - 查看明文不调后端(SecretInput 纯前端切)
//   - 死代码清:workflow 8 个函数 + DirectCreate / DirectDelete / DirectUpsert /
//     ReloadConfig / GetConfigPlain / Import / Export / KeysRotation / Diff
package service

import (
	"context"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
)

// ConfigListQuery 配置列表
type ConfigListQuery struct {
	model.PageRequest
	Group   string `json:"group"`
	Keyword string `json:"keyword"`
	IsSecret *bool `json:"is_secret"`
}

// ConfigCenterService 平台配置中心
//   2026-10-18 收窄:接口只留读 + update 一条写
type ConfigCenterService interface {
	// ===== 配置项查询 =====
	ListConfigs(ctx context.Context, q ConfigListQuery) (*model.PageResponse, error)
	GetConfig(ctx context.Context, id uint64) (*model.SysConfig, error)
	GetConfigByKey(ctx context.Context, key string) (*model.SysConfig, error)
	GetDecryptedValue(ctx context.Context, key string) (string, error)

	// ===== update 通道(super_admin 直改) =====
	DirectUpdate(ctx context.Context, caller *Caller, id uint64, patch map[string]any) error
	ListGroups(ctx context.Context) ([]ConfigGroupKV, error)

	// ===== View DTO 路径(对齐前端 ConfigItem) =====
	ListConfigsView(ctx context.Context, q ConfigListQuery) (*model.PageResponse, error)
	GetConfigView(ctx context.Context, id uint64) (*ConfigItemView, error)
	GetConfigByKeyView(ctx context.Context, key string) (*ConfigItemView, error)

	// ===== 审计查询 =====
	ListConfigAudit(ctx context.Context, q ConfigAuditListQuery) (*model.PageResponse, error)
	ListHotReload(ctx context.Context, q HotReloadListQuery) (*model.PageResponse, error)

	// ===== 运维统计 =====
	Stats(ctx context.Context) (map[string]any, error)
	Health(ctx context.Context) (map[string]any, error)

	// ===== 运行时消费(不是 CRUD,是"把配置喂给某个组件")=====
	// ResolveSMSConfig 用配置中心的 aliyun.sms.* 覆盖配置文件里的短信配置
	//   合并规则见 config_center_sms.go 的注释(核心:"运营改过的才算数")
	ResolveSMSConfig(ctx context.Context, base config.SMSConfig) (config.SMSConfig, error)
	// ResolveVoiceConfig 用配置中心的 voice.*(百炼克隆 / 合成)覆盖配置文件里的语音配置
	//   合并规则同上;消费方在每次调用时解析,面板改完即生效
	ResolveVoiceConfig(ctx context.Context, base config.VoiceConfig) (config.VoiceConfig, error)
}

// ConfigAuditListQuery
//
// ⚠️ **两个过滤条件的语义不一样,别混**:
//   - ConfigKey   —— **子串**匹配(搜索框用)
//   - ConfigGroup —— **精确**匹配某个配置组(组 = sys_config.config_group)
type ConfigAuditListQuery struct {
	model.PageRequest
	// ConfigKey 配置键过滤 —— 子串匹配
	ConfigKey string `json:"config_key"`
	// ConfigGroup 配置组过滤 —— 精确匹配该组下的全部配置项
	ConfigGroup string `json:"config_group"`
	UserID      uint64 `json:"user_id,string"`
	Action      string `json:"action"`
	StartAt     *int64 `json:"start_at"`
	EndAt       *int64 `json:"end_at"`
}

// HotReloadListQuery
type HotReloadListQuery struct {
	model.PageRequest
	ConfigKey string `json:"config_key"`
	StartAt   *int64 `json:"start_at"`
	EndAt     *int64 `json:"end_at"`
}

// ============================================================================
// View DTO(对齐前端 types/config.ts,后端原始 model 有 json:"-" 隐藏值)
// ============================================================================

// ConfigItemView 配置项视图
type ConfigItemView struct {
	ID                uint64                `json:"id,string"`
	ConfigGroup       string                `json:"config_group"`
	ConfigKey         string                `json:"config_key"`
	ConfigName        string                `json:"config_name"`
	Description       string                `json:"description"`
	ValueType         model.ConfigValueType `json:"value_type"`
	Value             string                `json:"value"`
	ValuePreview      string                `json:"value_preview"`
	IsSensitive       bool                  `json:"is_sensitive"`
	RegexValidate     string                `json:"regex_validate"`
	MinLength         int                   `json:"min_length"`
	MaxLength         int                   `json:"max_length"`
	IsRequired        bool                  `json:"is_required"`
	IsReadonly        bool                  `json:"is_readonly"`
	EnvVarName        string                `json:"env_var_name"`
	Sort              int                   `json:"sort"`
	IsActive          bool                  `json:"is_active"`
	Remarks           string                `json:"remarks"`
	Version           int                   `json:"version"`
	LastChangedBy     uint64                `json:"last_changed_by,string"`
	LastChangedByName string                `json:"last_changed_by_name"`
	LastChangedAt     *model.Time           `json:"last_changed_at"`
}

// ConfigGroupKV 配置分组 KV
type ConfigGroupKV struct {
	Group string `json:"group"`
	Count int    `json:"count"`
}
