// 迁移层单测
//
// ⚠️ **这里只验"全量表能建出来"** —— DDL 本身没有可断言的语义,
// 真正的断言在 `model/approval_test.go`(索引断言 + 真插重复行)。
package migrate

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAll_AutoMigrate 全量表能建出来
//
// ⚠️ **这条测试防的是"新加的 model 忘了登记进 All()"** —— 漏登记不会报错,
// 只会在生产上"表不存在",而那时权限码已经发下去、接口已经挂上去了。
//
// ⚠️ 用 `glebarez/sqlite`(纯 Go,不需要 cgo)。`cmd/migrate -dry-run` 在 sqlite
// 驱动下不支持("dry run mode unsupported"),所以真跑一次 AutoMigrate 是
// 唯一能验的办法 —— 用的是内存库,不动 dev.db。
func TestAll_AutoMigrate(t *testing.T) {
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, Run(db, false, false))

	// ⚠️ 审批七张表必须在里面 —— 单独点一次名,漏登记会在这里现形
	for _, name := range []string{
		"sys_approval_template", "sys_approval_stage", "sys_approval_assignee_rule",
		"sys_approval_instance", "sys_approval_task", "sys_approval_log", "sys_approval_round",
	} {
		var cnt int64
		require.NoError(t, db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name = ?", name,
		).Scan(&cnt).Error)
		require.Equal(t, int64(1), cnt, "表 %s 必须存在 —— 漏登记进 All() 的话生产上会「表不存在」", name)
	}
}
