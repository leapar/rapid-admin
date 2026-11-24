// dyn 记录写路径的显式审计接线测试(方案 A:裸 SQL + auditx.RecordChange)。
//
// ⚠️ 断言一律查 sys_data_change_log 落了什么,不看 service 内部状态
// —— 同 dyn_test.go 的纪律。
package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/auditx"
)

// dynSeedAuditCollection 建一个集合:no(text 必填)/ note(text 选填)
func dynSeedAuditCollection(t *testing.T, db *gorm.DB, svc DynService) {
	t.Helper()
	// 显式通道的取人走 auditx.SetCallerLookup(生产里在 main 装配);
	// 测试里注册同一份实现,并让 ctx 带上 caller
	auditx.SetCallerLookup(func(ctx context.Context) (uint64, string) {
		if c, ok := FromContext(ctx); ok {
			return c.UserID, c.Username
		}
		return 0, ""
	})
	dynSeedRole(t, db, "role_dyn_tester")
	id, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "audit",
		Name: "审计测试",
	})
	require.NoError(t, err)
	for _, f := range []DynFieldInput{
		{FieldKey: "no", Label: "单号", Type: "text", Required: true},
		{FieldKey: "note", Label: "备注", Type: "text"},
	} {
		fi := f
		_, err := svc.CreateField(context.Background(), dynCaller(1001, "role_dyn_tester"), id, &fi)
		require.NoError(t, err)
	}
}

// auditCaller 带用户名的调用者(审计行 Username 断言用)
func auditCaller() *Caller {
	c := dynCaller(1001, "role_dyn_tester")
	c.Username = "tester1"
	return c
}

// dynAuditRows 按操作类型读审计行(新→旧)
func dynAuditRows(t *testing.T, db *gorm.DB, op int) []model.SysDataChangeLog {
	t.Helper()
	var rows []model.SysDataChangeLog
	require.NoError(t, db.Where("operation = ?", op).Order("id DESC").Find(&rows).Error)
	return rows
}

func TestDynRecordAudit_CreateUpdateDelete(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedAuditCollection(t, db, svc)
	caller := auditCaller()
	ctx := WithCaller(context.Background(), caller)

	// ── 新增:after 有全量列 ──
	newID, err := svc.CreateRecord(ctx, caller, "audit", map[string]any{"no": "N001", "note": "v1"})
	require.NoError(t, err)
	creates := dynAuditRows(t, db, auditx.OpCreate)
	require.Len(t, creates, 1)
	// 表名列的是**物理表名**(dyn_<编码>),不是集合码
	assert.Equal(t, "dyn_audit", creates[0].TableNameField)
	assert.Equal(t, "tester1", creates[0].Username)
	var after map[string]any
	require.NoError(t, json.Unmarshal([]byte(creates[0].After), &after))
	assert.Equal(t, "N001", after["f_no"])
	// created/updated_at 不进快照(全局纪律)
	assert.NotContains(t, after, "created_at")
	assert.NotContains(t, after, "updated_at")
	assert.Empty(t, creates[0].Before)

	// ── 修改:只记本次改的列 ──
	require.NoError(t, svc.UpdateRecord(ctx, caller, "audit", newID, map[string]any{"no": "N002"}))
	updates := dynAuditRows(t, db, auditx.OpUpdate)
	require.Len(t, updates, 1)
	var before, afterUp map[string]any
	require.NoError(t, json.Unmarshal([]byte(updates[0].Before), &before))
	require.NoError(t, json.Unmarshal([]byte(updates[0].After), &afterUp))
	// 只有 f_no 进快照,note 没提交就不出现
	assert.Equal(t, "N001", before["f_no"])
	assert.Equal(t, "N002", afterUp["f_no"])
	assert.NotContains(t, before, "f_note")
	assert.NotContains(t, afterUp, "f_note")

	// ── 改个一样的值:没有 diff,不产生审计行 ──
	n := len(dynAuditRows(t, db, auditx.OpUpdate))
	require.NoError(t, svc.UpdateRecord(ctx, caller, "audit", newID, map[string]any{"no": "N002"}))
	assert.Len(t, dynAuditRows(t, db, auditx.OpUpdate), n, "无 diff 的更新不应产生审计行")

	// ── 删除:before 是整行 ──
	require.NoError(t, svc.DeleteRecord(ctx, caller, "audit", newID))
	deletes := dynAuditRows(t, db, auditx.OpDelete)
	require.Len(t, deletes, 1)
	var delBefore map[string]any
	require.NoError(t, json.Unmarshal([]byte(deletes[0].Before), &delBefore))
	assert.Equal(t, "N002", delBefore["f_no"])
	assert.Empty(t, deletes[0].After)
}

func TestDynRecordAudit_MissingRecordNoAudit(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedAuditCollection(t, db, svc)
	ctx := context.Background()
	caller := dynCaller(1001, "role_dyn_tester")

	// 改 / 删不存在的记录:404,且不产生任何审计行(根本没碰到数据)
	require.Error(t, svc.UpdateRecord(ctx, caller, "audit", 999999, map[string]any{"no": "hack"}))
	require.Error(t, svc.DeleteRecord(ctx, caller, "audit", 999999))
	assert.Empty(t, dynAuditRows(t, db, auditx.OpUpdate))
	assert.Empty(t, dynAuditRows(t, db, auditx.OpDelete))
}

func TestDynRecordAudit_BatchDeleteAuditsEachRow(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedAuditCollection(t, db, svc)
	caller := auditCaller()
	ctx := WithCaller(context.Background(), caller)

	id1, err := svc.CreateRecord(ctx, caller, "audit", map[string]any{"no": "B1"})
	require.NoError(t, err)
	id2, err := svc.CreateRecord(ctx, caller, "audit", map[string]any{"no": "B2"})
	require.NoError(t, err)

	n, err := svc.BatchDeleteRecords(ctx, caller, "audit", []string{
		dynIDString(id1), dynIDString(id2),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)

	deletes := dynAuditRows(t, db, auditx.OpDelete)
	require.Len(t, deletes, 2, "批量软删逐行留审计")
	got := map[string]bool{}
	for _, r := range deletes {
		var before map[string]any
		require.NoError(t, json.Unmarshal([]byte(r.Before), &before))
		got[before["f_no"].(string)] = true
	}
	assert.True(t, got["B1"] && got["B2"], "两行的快照都在: %v", got)
}

func TestDynRecordAudit_ExplicitChannelSanitizesCredentials(t *testing.T) {
	db, svc := newDynFixture(t)
	dynSeedAuditCollection(t, db, svc)
	caller := auditCaller()
	ctx := WithCaller(context.Background(), caller)

	// 字段标识直接叫 password:显式通道与钩子同纪律,凭据列不进快照
	id, err := svc.CreateCollection(ctx, caller, &DynCollectionInput{Code: "sec", Name: "安全"})
	require.NoError(t, err)
	_, err = svc.CreateField(ctx, caller, id, &DynFieldInput{
		FieldKey: "password", Label: "密码", Type: "text", Required: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateRecord(ctx, caller, "sec", map[string]any{"password": "top-secret"})
	require.NoError(t, err)

	creates := dynAuditRows(t, db, auditx.OpCreate)
	require.Len(t, creates, 1)
	var after map[string]any
	require.NoError(t, json.Unmarshal([]byte(creates[0].After), &after))
	assert.NotContains(t, after, "f_password", "凭据列不得进审计快照")
}
