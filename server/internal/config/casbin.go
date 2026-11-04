// Casbin RBAC 配置
// 文档 §5.2
package config

type CasbinConfig struct {
	ModelPath string `mapstructure:"model_path"`
	AutoLoad  bool   `mapstructure:"auto_load"`
}
