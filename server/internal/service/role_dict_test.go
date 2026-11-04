// 字典写路径的回归网 —— M94
//
// ⚠️ **这里验的是"改了真的会广播出去"** —— `dict_publisher_test.go`
// 验的是 publisher 这个接缝本身(事件名 / ctx / panic 隔离),
// 但**接缝接对了不代表业务路径接对了**:
// 少调一次 `PublishDictChanged`、或者广播的 group_code 取错了字段,
// 前者只在 publisher 的单测里看不出来(它直接调 publisher)。
//
// ⚠️ **最阴的那条**:广播的 group_code 取自**请求体**而不是**库里的那行**。
// group_code 是代码契约的一部分(改它等于换 enum),请求体里根本没有它 ——
// 一旦有人写成 `d.GroupCode`,前端就会去作废一个不存在的组,
// 真正的组反而不会刷新,而且**没有任何报错**。
package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/push"
)

// newDictTestDB 内存 sqlite,只迁移字典表用到的模型。
func newDictTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysDict{}))
	return db
}

const testDictID = uint64(96413000000000001)

// seedDictItem 写一条字典项
func seedDictItem(t *testing.T, db *gorm.DB, groupCode, itemKey, label string) uint64 {
	t.Helper()
	d := model.SysDict{
		BaseModel: model.BaseModel{ID: testDictID},
		GroupCode: groupCode,
		ItemKey:   itemKey,
		ItemValue: itemKey,
		Label:     label,
		Sort:      1,
		IsActive:  true,
	}
	require.NoError(t, db.Create(&d).Error)
	return d.ID
}

// 改了 label → 广播 dict.changed,group_code 是**库里那一行**的 group_code
func TestM94_UpdateDict_PublishesDictChanged(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	db := newDictTestDB(t)
	seedDictItem(t, db, "issue_status", "open", "待处理")

	svc := NewRoleService(db, NewDictPublisher(), nil)

	// ⚠️ **请求体里没有 group_code** —— 它是代码契约的一部分,改它要走别的通道。
	err := svc.UpdateDict(context.Background(), &Caller{UserID: 1}, testDictID, &model.SysDict{
		Label: "已受理",
	})
	require.NoError(t, err)

	tr.wait(t)
	events, topics, _ := tr.snapshot()
	require.Len(t, events, 1, "改完字典必须广播一条 dict.changed")
	assert.Equal(t, push.TopicDict, events[0].Topic)
	assert.Equal(t, push.EventDictChanged, events[0].Event)
	assert.Contains(t, topics, push.TopicDict)

	payload, ok := events[0].Payload.(push.DictChangedPayload)
	require.True(t, ok)
	assert.Equal(t, "issue_status", payload.GroupCode,
		"group_code 必须取自库里那一行,不是请求体")

	// 库里的 label 确实改了
	var got model.SysDict
	require.NoError(t, db.First(&got, testDictID).Error)
	assert.Equal(t, "已受理", got.Label)
}

// ⚠️ **没改任何字段 → 不广播**
//
// 广播"没变"会让前端白白作废一次缓存并重拉 —— 而重拉回来的还是一样的数据。
// 一次无意义的编辑不该打扰所有人。
func TestM94_UpdateDict_NoChange_NoEvent(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	db := newDictTestDB(t)
	seedDictItem(t, db, "issue_status", "open", "待处理")

	svc := NewRoleService(db, NewDictPublisher(), nil)

	// ⚠️ **body 里只有 is_active,且跟库里一致** —— service 的 updates 组装规则是
	// "空值跳过 + is_active 跟旧值不同才写"(见 UpdateDict)。
	// 所以"什么都没改"必须写成 is_active=true(与库里一致),
	// 写成零值 false 反而会被判成"要下线这一项",那是**真的改**。
	err := svc.UpdateDict(context.Background(), &Caller{UserID: 1}, testDictID, &model.SysDict{
		IsActive: true,
	})
	require.NoError(t, err)

	events, _, _ := tr.snapshot()
	assert.Len(t, events, 0, "什么都没改就不该广播")
}

// ⚠️ **写库失败 → 不广播**
//
// 顺序反了(先广播后写库)的话,前端会去重拉一个**没改成**的字典,
// 拿回旧值 —— 而用户明明看到的是"保存失败"。
func TestM94_UpdateDict_NotFound_NoEvent(t *testing.T) {
	tr := newCaptureTransport()
	withPushTransport(t, tr)

	db := newDictTestDB(t)
	svc := NewRoleService(db, NewDictPublisher(), nil)

	// id 不存在 → 查库就失败
	err := svc.UpdateDict(context.Background(), &Caller{UserID: 1}, 999999, &model.SysDict{
		Label: "x",
	})
	require.Error(t, err)

	events, _, _ := tr.snapshot()
	assert.Len(t, events, 0, "没写成功就不该广播")
}

// ⚠️ **publisher 为 nil 时不炸**(单测里不装 publisher 是常态)
func TestM94_UpdateDict_NilPublisher_NoCrash(t *testing.T) {
	db := newDictTestDB(t)
	seedDictItem(t, db, "issue_status", "open", "待处理")

	svc := NewRoleService(db, nil, nil)
	err := svc.UpdateDict(context.Background(), &Caller{UserID: 1}, testDictID, &model.SysDict{
		Label: "已受理",
	})
	require.NoError(t, err, "publisher 为 nil 不该让保存失败")
}
