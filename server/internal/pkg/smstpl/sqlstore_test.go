// GORM 版 Store 的 DB 级测试 —— M115
//
// # 为什么必须有这一层
//
// smstpl_test.go 里 20 条用例全跑在内存 memStore 上(不连 DB),
// 好处是状态机/守卫可以被单独拆开测;代价是 **sqlStore 的 SQL 一行都没被测过**。
// 而这一层最容易出问题的不是"读不出来",是**读出来了但读的不是你以为的那件事**:
//
//   - upsert 写成 insert ⇒ 同步任务每跑一轮就多一行,场景数从 4 变 400
//   - 覆盖保存顺手删了没传的行 ⇒ 运营清空一个模板号,另外三个场景的状态全丢
//   - 时间列时区错乱 ⇒ 页面显示"最后确认于 8 小时前"(实际是刚刚)
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· sqlstore.go:把 OnConflict 换成普通 Create ⇒ 「upsert 不产生重复行」变红
//	· sqlstore.go:SaveTemplates 末尾加一句全表 Delete ⇒ 「不删没传的行」变红
//	· sqlstore.go:templateFromModel 里解析失败时返回空串 ⇒ 「坏状态值按 unknown 处理」变红
//	· sqlstore.go:AppendLogs 改成只写第一条 ⇒ 「历史全部落库」变红
package smstpl

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// openTestDB 每个用例一套独立 sqlite 内存库
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysSmsTemplate{}, &model.SysSmsTemplateLog{}))
	return db
}

// countRows 数物理行数(Unscoped 绕过软删作用域)
func countRows(t *testing.T, db *gorm.DB, m any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Unscoped().Model(m).Count(&n).Error)
	return n
}

func TestSQLStoreSaveAndLoadRoundTrip(t *testing.T) {
	db := openTestDB(t)
	st := NewSQLStore(db)
	ctx := context.Background()

	reviewed := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	items := []Template{
		{
			Scene: SceneLogin, Code: "SMS_1", Status: StatusApproved,
			Reason: "审核通过", Source: SourceProvider,
			ReviewedAt: reviewed, UpdatedAt: reviewed,
		},
		{
			Scene: SceneResetPassword, Code: "SMS_2", Status: StatusRejected,
			Reason: "内容含错别字", Source: SourceProvider,
			ReviewedAt: reviewed, UpdatedAt: reviewed,
		},
	}
	require.NoError(t, st.SaveTemplates(ctx, items))

	got, err := st.Load(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)

	byScene := map[string]Template{}
	for _, tpl := range got {
		byScene[tpl.Scene] = tpl
	}
	login := byScene[SceneLogin]
	assert.Equal(t, "SMS_1", login.Code)
	assert.Equal(t, StatusApproved, login.Status)
	assert.Equal(t, "审核通过", login.Reason)
	assert.Equal(t, SourceProvider, login.Source)
	assert.True(t, login.ReviewedAt.Equal(reviewed), "服务商确认时间应原样往返: got %v", login.ReviewedAt)

	rp := byScene[SceneResetPassword]
	assert.Equal(t, StatusRejected, rp.Status)
	assert.Equal(t, "内容含错别字", rp.Reason)
}

func TestSQLStoreSaveTemplatesUpsertsByScene(t *testing.T) {
	db := openTestDB(t)
	st := NewSQLStore(db)
	ctx := context.Background()

	require.NoError(t, st.SaveTemplates(ctx, []Template{{
		Scene: SceneLogin, Code: "SMS_1", Status: StatusUnknown, Source: SourceManual,
	}}))
	// 同一个场景再存一次,状态变了
	require.NoError(t, st.SaveTemplates(ctx, []Template{{
		Scene: SceneLogin, Code: "SMS_1", Status: StatusApproved,
		Reason: "审核通过", Source: SourceProvider,
	}}))

	// ⚠️ 一行都不能多 —— 同步任务每 10 分钟跑一轮,多一行就是历史垃圾
	assert.Equal(t, int64(1), countRows(t, db, &model.SysSmsTemplate{}))

	got, err := st.Load(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, StatusApproved, got[0].Status)
	assert.Equal(t, "审核通过", got[0].Reason)
}

func TestSQLStoreSaveTemplatesKeepsRowsNotPassed(t *testing.T) {
	db := openTestDB(t)
	st := NewSQLStore(db)
	ctx := context.Background()

	require.NoError(t, st.SaveTemplates(ctx, []Template{
		{Scene: SceneLogin, Code: "SMS_1", Status: StatusApproved, Source: SourceProvider},
		{Scene: SceneResetPassword, Code: "SMS_2", Status: StatusApproved, Source: SourceProvider},
	}))
	// 只回写一个场景(同步时另一个场景的模板号被清空了)
	require.NoError(t, st.SaveTemplates(ctx, []Template{{
		Scene: SceneLogin, Code: "SMS_1", Status: StatusRejected, Source: SourceProvider,
	}}))

	assert.Equal(t, int64(2), countRows(t, db, &model.SysSmsTemplate{}),
		"没传的行不该被删 —— 清空模板号 = 停止发送,不是忘掉这张模板")

	got, err := st.Load(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestSQLStoreAppendLogsAndList(t *testing.T) {
	db := openTestDB(t)
	st := NewSQLStore(db)
	ctx := context.Background()

	base := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	logs := []Transition{
		{Scene: SceneLogin, Code: "SMS_1", From: StatusPending, To: StatusReviewing, Source: SourceProvider, Reason: "审核中", At: base},
		{Scene: SceneLogin, Code: "SMS_1", From: StatusReviewing, To: StatusApproved, Source: SourceProvider, Reason: "通过", At: base.Add(time.Hour)},
		{Scene: SceneResetPassword, Code: "SMS_2", From: StatusUnknown, To: StatusRejected, Source: SourceProvider, Reason: "驳回", At: base.Add(2 * time.Hour)},
	}
	require.NoError(t, st.AppendLogs(ctx, logs))
	assert.Equal(t, int64(3), countRows(t, db, &model.SysSmsTemplateLog{}))

	// 全部:最新在前
	all, err := st.ListLogs(ctx, "", 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, SceneResetPassword, all[0].Scene)
	assert.Equal(t, StatusApproved, all[1].To)
	assert.True(t, all[1].At.Equal(base.Add(time.Hour)))

	// 按场景
	login, err := st.ListLogs(ctx, SceneLogin, 0)
	require.NoError(t, err)
	require.Len(t, login, 2)
	assert.Equal(t, StatusApproved, login[0].To)

	// 限条数
	one, err := st.ListLogs(ctx, "", 1)
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, SceneResetPassword, one[0].Scene)
}

func TestSQLStoreLoadCorruptStatusBecomesUnknown(t *testing.T) {
	db := openTestDB(t)
	st := NewSQLStore(db)
	ctx := context.Background()

	// 手工改坏的状态值(只可能来自手工改库)
	require.NoError(t, db.Create(&model.SysSmsTemplate{
		BaseModel:    model.BaseModel{ID: 9001},
		Scene:        SceneLogin,
		TemplateCode: "SMS_1",
		Status:       "approvd",
		Source:       string(SourceProvider),
	}).Error)

	got, err := st.Load(ctx)
	require.NoError(t, err, "坏状态值不该让整个装载失败")
	require.Len(t, got, 1)
	assert.Equal(t, StatusUnknown, got[0].Status,
		"坏状态值按 unknown 处理(与『同步没跑』同口径),而不是当成 rejected 把通道锁死")
	assert.Contains(t, got[0].Reason, "approvd", "原文要留在原因里,页面要能看出这条数据坏了")
}

func TestSQLStoreNilDBIsNoop(t *testing.T) {
	st := NewSQLStore(nil)
	ctx := context.Background()

	got, err := st.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, got)

	require.NoError(t, st.SaveTemplates(ctx, []Template{{Scene: SceneLogin, Code: "SMS_1"}}))
	require.NoError(t, st.AppendLogs(ctx, []Transition{{Scene: SceneLogin, Code: "SMS_1"}}))

	logs, err := st.ListLogs(ctx, "", 10)
	require.NoError(t, err)
	assert.Empty(t, logs)
}

// TestSQLStoreStateSurvivesRestart 进程重启后状态还在
//
// 这是"持久化"的意义所在:登记册在内存里,DB 是它的快照。
// 重启后从快照读回来,人工停用不能丢 —— 丢了就等于停用没发生过。
func TestSQLStoreStateSurvivesRestart(t *testing.T) {
	db := openTestDB(t)
	store := NewSQLStore(db)
	ctx := context.Background()

	// 第一个"进程":登记 + 服务商判通过 + 人工停用
	reg := NewRegistry()
	_, _, err := reg.Register(SceneLogin, "SMS_1")
	require.NoError(t, err)
	_, _, err = reg.Apply(SceneLogin, StatusApproved, SourceProvider, "审核通过", "")
	require.NoError(t, err)
	_, _, err = reg.Apply(SceneLogin, StatusDisabled, SourceManual, "运维停用", "")
	require.NoError(t, err)
	require.NoError(t, store.SaveTemplates(ctx, reg.List()))

	// 第二个"进程":从快照读回来
	reg2 := NewRegistry()
	snap, err := store.Load(ctx)
	require.NoError(t, err)
	reg2.Load(snap)

	tpl, ok := reg2.Get(SceneLogin)
	require.True(t, ok)
	assert.Equal(t, StatusDisabled, tpl.Status, "人工停用必须扛过一次重启")
	assert.Equal(t, "SMS_1", tpl.Code)
	assert.Equal(t, "运维停用", tpl.Reason)
}

// TestSysSmsTemplateSceneIsUnique scene 是唯一索引
//
// ⚠️ 这条不是"测 GORM" —— 是把**并发收敛**这件事钉死:
// 同步任务和"手动同步"可能同时在跑,两个 upsert 打同一行时,
// 靠的就是这个唯一索引 + OnConflict。
// 索引写错(比如漏了 uniqueIndex)的话,表里会出现两行 login,
// 而 Load 会读出两行同场景 —— 页面上出现两个"登录验证码"。
func TestSysSmsTemplateSceneIsUnique(t *testing.T) {
	db := openTestDB(t)

	require.NoError(t, db.Create(&model.SysSmsTemplate{
		BaseModel: model.BaseModel{ID: 9101},
		Scene:     SceneLogin,
		Status:    string(StatusApproved),
	}).Error)

	err := db.Create(&model.SysSmsTemplate{
		BaseModel: model.BaseModel{ID: 9102},
		Scene:     SceneLogin,
		Status:    string(StatusRejected),
	}).Error
	require.Error(t, err, "scene 必须唯一 —— 两行同场景会让页面出现两条记录")
}
