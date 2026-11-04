// 数据库配置 —— 支持 MySQL / SQLite 通过 driver 切换
// 文档 §4
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DatabaseDriver 数据库驱动枚举
type DatabaseDriver string

const (
	DriverMySQL  DatabaseDriver = "mysql"
	DriverSQLite DatabaseDriver = "sqlite"
)

// DatabaseConfig 数据库总配置
//   - Driver:   "mysql" / "sqlite"
//   - MySQL:    driver=mysql 时使用;为空时自动 fallback 到顶层 MySQL
//   - SQLite:   driver=sqlite 时使用
type DatabaseConfig struct {
	Driver DatabaseDriver `mapstructure:"driver"`
	MySQL  MySQLConfig    `mapstructure:"mysql"`
	SQLite SQLiteConfig   `mapstructure:"sqlite"`
}

// IsZero 判断 MySQL 子块是否全空(用于 fallback 判定)
func (d DatabaseConfig) IsMySQLZero() bool {
	return d.MySQL.Host == "" && d.MySQL.DBName == "" && d.MySQL.Username == ""
}

// SQLiteConfig SQLite 配置
type SQLiteConfig struct {
	Path         string        `mapstructure:"path"`
	BusyTimeout  time.Duration `mapstructure:"busy_timeout"`
	JournalMode  string        `mapstructure:"journal_mode"` // wal / delete / truncate / memory
	ForeignKeys  bool          `mapstructure:"foreign_keys"`
	MaxOpenConns int           `mapstructure:"max_open_conns"`
	LogLevel     string        `mapstructure:"log_level"`
}

// WithDefaults 填充 SQLite 必需默认值(导出供 repository 等使用)
func (s SQLiteConfig) WithDefaults() SQLiteConfig {
	if s.Path == "" {
		s.Path = "./data/dev.db"
	}
	if s.BusyTimeout == 0 {
		s.BusyTimeout = 5 * time.Second
	}
	if s.JournalMode == "" {
		s.JournalMode = "wal"
	}
	if !s.ForeignKeys {
		s.ForeignKeys = true
	}
	if s.MaxOpenConns == 0 {
		s.MaxOpenConns = 1 // SQLite 单写者惯例,避免 "database is locked"
	}
	return s
}

// DSN 拼接 glebarez/sqlite 的 DSN,带必要 pragma
// 例: file:./data/dev.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)
func (s SQLiteConfig) DSN() string {
	s = s.WithDefaults()
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", s.BusyTimeout.Milliseconds()))
	q.Add("_pragma", fmt.Sprintf("journal_mode(%s)", strings.ToUpper(s.JournalMode)))
	if s.ForeignKeys {
		q.Add("_pragma", "foreign_keys(on)")
	}
	return fmt.Sprintf("file:%s?%s", s.Path, q.Encode())
}