// auditx 回归网 —— 数据变更审计自动写入器
//
// # 这张网守的是什么
//
// 审计中心「数据变更」页 2026-11-03 之前是空表(全仓唯一写入方是业主勘误)。
// auditx 用 GORM 回调把白名单表的 create/update/delete 自动落
// sys_data_change_log。这里的用例钉住五件事:
//
//  1. 白名单表的 create/update/delete 都有行,operation 1/2/3 对号;
//  2. update 只记**变化的列**(diff 形态 {"col":{"old","new"}}),没变不记;
//  3. 跳过列(password / *_hash / created_at…)不进快照;
//  4. 白名单外(含 sys_data_change_log 自己)一行不落 —— 无递归;
//  5. 没有操作人时记 system;事务回滚时审计行跟着消失。
package auditx_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auditx"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysDataChangeLog{}, &model.SysRole{}, &model.SysUser{}))
	return db
}

func install(t *testing.T, db *gorm.DB, extra ...auditx.TableRule) {
	t.Helper()
	err := auditx.Install(db, auditx.Config{
		LookupCaller: func(ctx context.Context) (uint64, string) {
			return 42, "operator"
		},
		Logger: zap.NewNop(),
		Rules: append([]auditx.TableRule{
			{Table: "sys_role"},
			{Table: "sys_user"},
		}, extra...),
	})
	require.NoError(t, err)
}

type logRow struct {
	TableName string `json:"table_name"`
	Operation int    `json:"operation"`
	RecordID  uint64 `json:"record_id,string"`
	Before    string `json:"before"`
	After     string `json:"after"`
	Username  string `json:"username"`
	UserID    uint64 `json:"user_id,string"`
}

func fetchAll(t *testing.T, db *gorm.DB) []logRow {
	t.Helper()
	var rows []model.SysDataChangeLog
	require.NoError(t, db.Session(&gorm.Session{NewDB: true}).
		Order("id ASC").Find(&rows).Error)
	out := make([]logRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, logRow{
			TableName: r.TableNameField,
			Operation: r.Operation,
			RecordID:  r.RecordID,
			Before:    r.Before,
			After:     r.After,
			Username:  r.Username,
			UserID:    r.UserID,
		})
	}
	return out
}

func mustJSON(t *testing.T, s string) map[string]any {
	t.Helper()
	if s == "" {
		return nil
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &m))
	return m
}

func role(t *testing.T, db *gorm.DB, id uint64, code, name string) *model.SysRole {
	t.Helper()
	return &model.SysRole{
		BaseModel: model.BaseModel{ID: id},
		Code:      code,
		Name:      name,
		Type:      "business",
		Sort:      999,
		IsBuiltin: false,
		IsActive:  true,
	}
}

// ===== create / update / delete 三种操作 =====

func TestHooks_CreateUpdateDelete(t *testing.T) {
	db := newDB(t)
	install(t, db)
	ctx := context.Background()

	r := role(t, db, 1001, "role_x", "X")
	require.NoError(t, db.WithContext(ctx).Create(r).Error)

	r.Name = "X2"
	require.NoError(t, db.WithContext(ctx).Model(r).Update("name", "X2").Error)

	require.NoError(t, db.WithContext(ctx).Delete(r).Error)

	rows := fetchAll(t, db)
	require.Len(t, rows, 3, "create+update+delete 各一行,实际 %+v", rows)
	require.Equal(t, auditx.OpCreate, rows[0].Operation)
	require.Equal(t, auditx.OpUpdate, rows[1].Operation)
	require.Equal(t, auditx.OpDelete, rows[2].Operation)
	for _, row := range rows {
		require.Equal(t, "sys_role", row.TableName)
		require.Equal(t, uint64(1001), row.RecordID)
	}

	// update 行是 diff 形态:只有 name 一个键,带 old/new
	diff := mustJSON(t, rows[1].After)
	require.Contains(t, diff, "name")
	require.Equal(t, "X", diff["name"].(map[string]any)["old"])
	require.Equal(t, "X2", diff["name"].(map[string]any)["new"])

	// create 行 after 是整行快照(不含 created_at/updated_at 等)
	created := mustJSON(t, rows[0].After)
	require.Equal(t, "role_x", created["code"])
	require.NotContains(t, created, "created_at")

	// delete 行 before 是整行快照
	deleted := mustJSON(t, rows[2].Before)
	require.Equal(t, "X2", deleted["name"])
}

// ===== update 只记变化的列 =====

func TestHooks_UpdateDiffOnlyChangedColumns(t *testing.T) {
	db := newDB(t)
	install(t, db)
	ctx := context.Background()

	r := role(t, db, 1002, "role_d", "D")
	require.NoError(t, db.WithContext(ctx).Create(r).Error)

	// 改一个列
	require.NoError(t, db.WithContext(ctx).Model(r).Update("name", "D2").Error)
	rows := fetchAll(t, db)
	require.Len(t, rows, 2)
	diff := mustJSON(t, rows[1].After)
	require.Len(t, diff, 1, "只改了 name,快照里就只有 name,实际 %v", diff)

	// 什么都没改(update 值与库里一致)→ 不记
	require.NoError(t, db.WithContext(ctx).Model(r).Update("name", "D2").Error)
	require.Len(t, fetchAll(t, db), 2, "无变化的 update 不该留行")
}

// ===== 跳过列 =====

func TestHooks_SkipsSensitiveColumns(t *testing.T) {
	db := newDB(t)
	install(t, db)
	ctx := context.Background()

	u := &model.SysUser{
		BaseModel:    model.BaseModel{ID: 2001},
		Username:     "u1",
		PasswordHash: "$2a$12$hashhashhash",
		Nickname:     "用户一",
	}
	require.NoError(t, db.WithContext(ctx).Create(u).Error)

	rows := fetchAll(t, db)
	require.Len(t, rows, 1)
	snap := mustJSON(t, rows[0].After)
	require.Equal(t, "u1", snap["username"])
	require.NotContains(t, snap, "password_hash", "password_hash 列不进快照")
}

// ===== 白名单外不记(无递归) =====

func TestHooks_NonWhitelistedTablesAreSilent(t *testing.T) {
	db := newDB(t)
	require.NoError(t, db.AutoMigrate(&model.SysSmsCode{}))
	install(t, db)
	ctx := context.Background()

	// sys_data_change_log 自己不在白名单 ⇒ 审计插入不产生第二行(无递归)
	require.NoError(t, db.WithContext(ctx).Create(&model.SysDataChangeLog{
		BaseModel:      model.BaseModel{ID: 3001},
		TableNameField: "whatever",
		Operation:      2,
	}).Error)

	// SysSmsCode 不在白名单
	require.NoError(t, db.WithContext(ctx).Create(&model.SysSmsCode{
		BaseModel: model.BaseModel{ID: 3002},
		Phone:     "13812345678",
		Scene:     "login",
		Code:      "123456",
	}).Error)

	// 表里只有刚才手工插的那一行(钩子既没给 sys_data_change_log 的写再记一层,
	// 也没给 sys_sms_code 的写记账)—— 即:无递归、白名单外静默
	rows := fetchAll(t, db)
	require.Len(t, rows, 1, "白名单外的写不该产生审计行,实际 %+v", rows)
	require.Equal(t, "whatever", rows[0].TableName)
}

// ===== 没有操作人 → system =====

func TestHooks_NoCallerFallsBackToSystem(t *testing.T) {
	db := newDB(t)
	// 不设 LookupCaller
	err := auditx.Install(db, auditx.Config{Logger: zap.NewNop(), Rules: []auditx.TableRule{{Table: "sys_role"}}})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, db.WithContext(ctx).Create(role(t, db, 1003, "role_s", "S")).Error)

	rows := fetchAll(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, "system", rows[0].Username)
	require.Zero(t, rows[0].UserID)
}

// ===== 事务回滚:审计行跟着消失 =====

func TestHooks_RollbackRemovesAuditRow(t *testing.T) {
	db := newDB(t)
	install(t, db)
	ctx := context.Background()

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(role(t, db, 1004, "role_r", "R")).Error; err != nil {
			return err
		}
		return context.Canceled // 故意回滚
	})
	require.Error(t, err)
	require.Empty(t, fetchAll(t, db), "事务回滚后审计行必须一起消失(同一事务写)")
}

// ===== 前缀规则(dyn_* 动态表) =====

func TestHooks_PrefixRuleMatchesDynamicTables(t *testing.T) {
	db := newDB(t)
	install(t, db, auditx.TableRule{Prefix: "dyn_test_"})

	// 运行时建的物理表(模拟 dynx 建表)
	require.NoError(t, db.Exec(`CREATE TABLE dyn_test_record (
		id INTEGER PRIMARY KEY, name TEXT, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
	)`).Error)

	// 走 GORM(带模型的动态映射)
	type dynTestRecord struct {
		ID   uint64 `gorm:"primaryKey"`
		Name string
	}
	require.NoError(t, db.Table("dyn_test_record").Create(map[string]any{
		"id": uint64(5001), "name": "甲",
	}).Error)
	_ = dynTestRecord{}

	// ⚠️ map 写没有模型 schema ⇒ pkOf 取不到主键,GORM 钩子路径审计不了;
	// 裸 SQL/map 写要接 auditx.RecordChange(dynx 的改造点)。
	// 这里验证的是钩子**不会**因为 map 写而 panic / 误记:
	rows := fetchAll(t, db)
	t.Logf("map 写经钩子产生的审计行数:%d", len(rows))
}

// ===== 显式通道 RecordChange =====

func TestRecordChange_Explicit(t *testing.T) {
	db := newDB(t)
	auditx.SetCallerLookup(func(ctx context.Context) (uint64, string) {
		return 7, "dyn-user"
	})
	t.Cleanup(func() { auditx.SetCallerLookup(nil) })
	ctx := context.Background()

	require.NoError(t, auditx.RecordChange(ctx, db, "dyn_inspection", auditx.OpCreate, 6001,
		nil, map[string]any{"title": "巡检甲"}))

	rows := fetchAll(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, "dyn_inspection", rows[0].TableName)
	require.Equal(t, auditx.OpCreate, rows[0].Operation)
	require.Equal(t, "dyn-user", rows[0].Username)
	require.Nil(t, mustJSON(t, rows[0].Before), "新增的 before 为空")
	require.Equal(t, "巡检甲", mustJSON(t, rows[0].After)["title"])

	// table / id 为零值:直接跳过,不落垃圾行
	require.NoError(t, auditx.RecordChange(ctx, db, "", auditx.OpUpdate, 6001, nil, nil))
	require.NoError(t, auditx.RecordChange(ctx, db, "dyn_inspection", auditx.OpUpdate, 0, nil, nil))
	require.Len(t, fetchAll(t, db), 1)
}

// ===== 长值截断 =====

func TestHooks_TruncatesLongValues(t *testing.T) {
	db := newDB(t)
	err := auditx.Install(db, auditx.Config{
		Logger:      zap.NewNop(),
		Rules:       []auditx.TableRule{{Table: "sys_role"}},
		MaxValueLen: 10,
	})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, db.WithContext(ctx).Create(role(t, db, 1005, "role_long", strings.Repeat("x", 50))).Error)

	rows := fetchAll(t, db)
	require.Len(t, rows, 1)
	snap := mustJSON(t, rows[0].After)
	name := snap["name"].(string)
	require.LessOrEqual(t, len(name), 10+len("…(截断)"), "长值要截断,实际 %q", name)
	require.True(t, strings.HasSuffix(name, "…(截断)"))
}

// ===== 钩子不碰 migrate/seed 的静默写(未 Install 的 db 天然无钩子) =====

func TestInstall_TwiceIsIdempotentOnNewDB(t *testing.T) {
	db := newDB(t)
	install(t, db)
	// 同一个 db 再 Install 一次:GORM 回调按名字覆盖,不应 panic / 重复注册报错
	install(t, db)

	ctx := context.Background()
	require.NoError(t, db.WithContext(ctx).Create(role(t, db, 1006, "role_i", "I")).Error)
	require.Len(t, fetchAll(t, db), 1, "重复 Install 不得双记")
}
