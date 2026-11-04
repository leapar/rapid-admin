// MySQL 配置
package config

import (
	"fmt"
	"time"
)

type MySQLConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Username        string        `mapstructure:"username"`
	Password        string        `mapstructure:"password"`
	DBName          string        `mapstructure:"dbname"`
	Charset         string        `mapstructure:"charset"`
	ParseTime       bool          `mapstructure:"parse_time"`
	Loc             string        `mapstructure:"loc"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	LogLevel        string        `mapstructure:"log_level"`
}

// DSN 拼接 GORM DSN
func (m MySQLConfig) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=%t&loc=%s",
		m.Username, m.Password, m.Host, m.Port, m.DBName,
		m.Charset, m.ParseTime, m.Loc,
	)
}
