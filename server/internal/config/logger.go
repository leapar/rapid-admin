// 日志配置
// 文档 §10
package config

type LoggerConfig struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Dir        string `mapstructure:"dir"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	Compress   bool   `mapstructure:"compress"`
	Env        string `mapstructure:"env"`
}
