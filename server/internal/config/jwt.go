// JWT 配置 —— 三个受众独立 secret
// 文档 §9.6
package config

import "time"

type JWTConfig struct {
	AdminSecret    string        `mapstructure:"admin_secret"`
	OwnerSecret    string        `mapstructure:"owner_secret"`
	SupplierSecret string        `mapstructure:"supplier_secret"`
	Issuer         string        `mapstructure:"issuer"`
	AccessTTL      time.Duration `mapstructure:"access_ttl"`
	RefreshTTL     time.Duration `mapstructure:"refresh_ttl"`
	ShortTTL       time.Duration `mapstructure:"short_ttl"`
}
