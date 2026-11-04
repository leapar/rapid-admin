// 数据库初始化 + 事务工具
// 文档 §4
// 支持 MySQL / SQLite,通过 config.DatabaseConfig.Driver 切换;
// driver=mysql 时,若 database.mysql 子块全空则回退到顶层 mysql 块(兼容老配置)。
package repository

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/config"
)

var (
	DB *gorm.DB
	mu sync.RWMutex
)

// NewDB 根据 dbCfg.Driver 分发 MySQL / SQLite
//   dbCfg         - 新的 database: 块(driver + mysql + sqlite)
//   mysqlFallback - 顶层 mysql: 块,dbCfg.MySQL 为空时使用
func NewDB(dbCfg config.DatabaseConfig, mysqlFallback config.MySQLConfig) (*gorm.DB, error) {
	gormLogLevel := logger.Warn
	switch {
	case dbCfg.Driver == config.DriverSQLite && dbCfg.SQLite.LogLevel != "":
		gormLogLevel = resolveLogLevel(dbCfg.SQLite.LogLevel)
	case dbCfg.Driver == config.DriverMySQL && dbCfg.MySQL.LogLevel != "":
		gormLogLevel = resolveLogLevel(dbCfg.MySQL.LogLevel)
	case mysqlFallback.LogLevel != "":
		gormLogLevel = resolveLogLevel(mysqlFallback.LogLevel)
	}

	gormCfg := &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "[gorm] ", log.LstdFlags),
			logger.Config{
				SlowThreshold: 200 * time.Millisecond,
				LogLevel:      gormLogLevel,
				Colorful:      true,
			},
		),
		// 禁用默认事务(SingleCreate),让 service 显式控制
		SkipDefaultTransaction: true,
		// 准备语句缓存
		PrepareStmt: true,
	}

	dialector, err := buildDialector(dbCfg, mysqlFallback)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open db (%s): %w", dbCfg.Driver, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	switch dbCfg.Driver {
	case config.DriverSQLite:
		// SQLite 单写者惯例:MaxOpenConns 由 SQLiteConfig.WithDefaults() 决定,
		// 这里强制覆盖 pool 相关设置,MaxIdleConns 取同值以避免连接反复创建
		openConns := dbCfg.SQLite.WithDefaults().MaxOpenConns
		sqlDB.SetMaxOpenConns(openConns)
		sqlDB.SetMaxIdleConns(openConns)
		// SQLite 下 ConnMaxLifetime 没意义,跳过
	default:
		m := resolveMySQL(dbCfg, mysqlFallback)
		sqlDB.SetMaxOpenConns(m.MaxOpenConns)
		sqlDB.SetMaxIdleConns(m.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(m.ConnMaxLifetime)
	}

	// 连通性检查
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db (%s): %w", dbCfg.Driver, err)
	}

	// casbin adapter 自动建表
	if err := db.AutoMigrate(); err != nil { // casbin 自己的表(占位,实际由 casbinx 注册)
		// ignore: casbin adapter 内部会建 sys_casbin_rule
	}

	mu.Lock()
	DB = db
	mu.Unlock()

	return db, nil
}

// buildDialector 根据 driver 返回对应 GORM dialector
func buildDialector(dbCfg config.DatabaseConfig, mysqlFallback config.MySQLConfig) (gorm.Dialector, error) {
	switch dbCfg.Driver {
	case "", config.DriverMySQL:
		m := resolveMySQL(dbCfg, mysqlFallback)
		if m.Host == "" {
			return nil, fmt.Errorf("mysql driver requires host (database.mysql or top-level mysql)")
		}
		return mysql.Open(m.DSN()), nil

	case config.DriverSQLite:
		s := dbCfg.SQLite.WithDefaults()
		// 确保父目录存在,避免 Open 直接失败
		if dir, ok := parentDir(s.Path); ok {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("sqlite mkdir %s: %w", dir, err)
			}
		}
		return sqlite.Open(s.DSN()), nil

	default:
		return nil, fmt.Errorf("unsupported database driver: %q", dbCfg.Driver)
	}
}

// resolveMySQL 优先用 database.mysql,空则 fallback 顶层 mysql
func resolveMySQL(dbCfg config.DatabaseConfig, mysqlFallback config.MySQLConfig) config.MySQLConfig {
	if !dbCfg.IsMySQLZero() {
		return dbCfg.MySQL
	}
	return mysqlFallback
}

// parentDir 提取文件路径的父目录;无父目录返回 false.
func parentDir(path string) (string, bool) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i], true
		}
	}
	return "", false
}

func resolveLogLevel(level string) logger.LogLevel {
	switch level {
	case "silent":
		return logger.Silent
	case "error":
		return logger.Error
	case "info":
		return logger.Info
	default:
		return logger.Warn
	}
}

// SetGlobal 注入全局依赖(在 main.go 启动后期调用)
func SetGlobal(db *gorm.DB, _ any, _ any) {
	mu.Lock()
	DB = db
	mu.Unlock()
}

// GetDB 取全局 DB(供 repository 实现)
func GetDB() *gorm.DB {
	return DB
}

// WithTx 在事务中执行 fn
// 文档 §5.7 关键业务规则(用 Service 层事务保护)
func WithTx(ctx context.Context, fn func(tx *gorm.DB) error) (err error) {
	return DB.WithContext(ctx).Transaction(fn)
}