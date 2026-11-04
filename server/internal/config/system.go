// 系统配置
package config

import "time"

type SystemConfig struct {
	Addr         int           `mapstructure:"addr"`
	Mode         string        `mapstructure:"mode"` // debug / release / test
	AppName      string        `mapstructure:"app_name"`
	AppVersion   string        `mapstructure:"app_version"`
	Timezone     string        `mapstructure:"timezone"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	MaxBodySize  int64         `mapstructure:"max_body_size"`
}
