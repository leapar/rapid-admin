// 公开只读(public_read)service 单测(2026-11-03)
//
// ⚠️ **这批用例钉的是"闸门",不是"读得到"** ——
// 公开只读的危险方向只有一个:不该公开的集合被匿名读到了。
// 所以每条用例都在闸门上,读路径本身(list / getOne)只各钉一条正向用例,
// 证明公开路径与登录路径共用同一份查询主体时行为没走样。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// newPublicDynFixture 建一个公开只读集合 + 一个字段 + 两条记录(属主 1001)
func newPublicDynFixture(t *testing.T) (*gorm.DB, DynService, string, []uint64) {
	t.Helper()
	db, svc := newDynFixture(t)
	cid, err := svc.CreateCollection(context.Background(), dynCaller(1001, "role_pub"), &DynCollectionInput{
		Code:       "notices",
		Name:       "公告",
		PublicRead: true,
	})
	require.NoError(t, err)
	_, err = svc.CreateField(context.Background(), dynCaller(1001, "role_pub"), cid, &DynFieldInput{
		FieldKey: "title", Label: "标题", Type: "text",
	})
	require.NoError(t, err)
	var ids []uint64
	for _, title := range []string{"第一条", "第二条"} {
		rid, err := svc.CreateRecord(context.Background(), dynCaller(1001, "role_pub"), "notices",
			map[string]any{"title": title})
		require.NoError(t, err)
		ids = append(ids, rid)
	}
	return db, svc, "notices", ids
}

// TestDyn_公开只读_闸门
//
// ⚠️ **四类"不该公开"一律 401,不区分原因** ——
// 不存在 / 没开 public_read / 停用 / owner_scoped(防御纵深:互斥校验挡住了
// 这条组合能被 service 建出来,但直接改库能造出来,闸门必须自己兜住)。
// 区分原因 = 给探测者一个"这个编码存在,只是没公开"的枚举信号。
func TestDyn_公开只读_闸门(t *testing.T) {
	db, svc, code, _ := newPublicDynFixture(t)
	ctx := context.Background()
	yes := true

	// ① 正常公开集合:读得到
	page, err := svc.PublicListRecords(ctx, code, DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)

	// ② 集合编码不存在 → 401
	_, err = svc.PublicListRecords(ctx, "no_such_code", DynRecordQuery{})
	assert.True(t, errCodeIs(err, errcode.Unauthorized), "不存在的集合也按未登录处理,实际 err=%v", err)

	// ③ 没开 public_read 的集合 → 401
	_, err = svc.CreateCollection(ctx, dynCaller(1001, "role_pub"), &DynCollectionInput{
		Code: "private_one", Name: "私有表",
	})
	require.NoError(t, err)
	_, err = svc.PublicListRecords(ctx, "private_one", DynRecordQuery{})
	assert.True(t, errCodeIs(err, errcode.Unauthorized), "未开公开的集合不该能匿名读,实际 err=%v", err)

	// ④ 停用集合 → 401(公开开关跟着 is_active 走)
	one, err := svc.GetCollectionByCode(ctx, code)
	require.NoError(t, err)
	no := false
	require.NoError(t, svc.UpdateCollection(ctx, dynCaller(1001, "role_pub"), one.ID, &DynCollectionUpdate{Name: "公告", IsActive: &no}))
	_, err = svc.PublicListRecords(ctx, code, DynRecordQuery{})
	assert.True(t, errCodeIs(err, errcode.Unauthorized), "停用集合不该能匿名读,实际 err=%v", err)
	require.NoError(t, svc.UpdateCollection(ctx, dynCaller(1001, "role_pub"), one.ID, &DynCollectionUpdate{Name: "公告", IsActive: &yes}))

	// ⑤ 防御纵深:直接改库造出 owner_scoped + public_read 的组合,闸门照样挡
	require.NoError(t, db.Model(&model.SysDynCollection{}).Where("code = ?", code).
		Update("owner_scoped", true).Error)
	_, err = svc.PublicListRecords(ctx, code, DynRecordQuery{})
	assert.True(t, errCodeIs(err, errcode.Unauthorized), "owner_scoped+public_read 的脏组合必须被闸门挡住,实际 err=%v", err)
}

// TestDyn_公开只读_getOne与软删不可见
func TestDyn_公开只读_getOne与软删不可见(t *testing.T) {
	_, svc, code, ids := newPublicDynFixture(t)
	ctx := context.Background()

	view, err := svc.PublicGetRecord(ctx, code, ids[0])
	require.NoError(t, err)
	assert.Equal(t, "第一条", view["title"])

	// 不存在的记录 → NotFound(不是 401:集合闸门已过,单条找不到就是找不到)
	_, err = svc.PublicGetRecord(ctx, code, ids[0]+999)
	assert.True(t, errCodeIs(err, errcode.NotFound), "实际 err=%v", err)

	// 软删的记录:详情 404,列表不再出现
	require.NoError(t, svc.DeleteRecord(ctx, dynCaller(1001, "role_pub"), code, ids[0]))
	_, err = svc.PublicGetRecord(ctx, code, ids[0])
	assert.True(t, errCodeIs(err, errcode.NotFound), "软删记录不该能匿名读,实际 err=%v", err)
	page, err := svc.PublicListRecords(ctx, code, DynRecordQuery{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), page.Total, "软删记录不该出现在公开列表里")
}

// TestDyn_公开只读_翻不了回收站
//
// ⚠️ **Trash 开关是登录用户的恢复工具,不是公开查询参数** ——
// 公开列表与登录列表共用 recordWhere,q.Trash 原样透传的话,
// 匿名请求带 trash=true 就能把 `deleted_at IS NULL` 翻成 `IS NOT NULL`,
// 把回收站整个读走。公开路径必须强制压掉它。
func TestDyn_公开只读_翻不了回收站(t *testing.T) {
	_, svc, code, ids := newPublicDynFixture(t)
	ctx := context.Background()

	// ⚠️ **两条全删** —— 只删一条时"正常视图剩 1 条"和"回收站有 1 条"
	// 的 Total 恰好都是 1,断言恒绿;全删之后正常视图必须是 0,
	// 翻进回收站才是 2,计数才区分得出两种行为。
	for _, id := range ids {
		require.NoError(t, svc.DeleteRecord(ctx, dynCaller(1001, "role_pub"), code, id))
	}
	page, err := svc.ListRecords(ctx, dynCaller(1001, "role_pub"), code, DynRecordQuery{})
	require.NoError(t, err)
	require.Equal(t, int64(0), page.Total, "前置:正常视图里已经没有记录")

	// 公开路径:trash=true 也只能看到正常视图
	page, err = svc.PublicListRecords(ctx, code, DynRecordQuery{Trash: true})
	require.NoError(t, err)
	assert.Equal(t, int64(0), page.Total, "匿名带 trash=true 不得翻回收站")

	// 对照:登录路径同样的参数能看到回收站 —— 证明上面的断言不是恒绿
	page, err = svc.ListRecords(ctx, dynCaller(1001, "role_pub"), code, DynRecordQuery{Trash: true})
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total, "登录用户的回收站视图必须还看得见(对照组)")
}

// TestDyn_公开只读_与ownerScoped互斥
func TestDyn_公开只读_与ownerScoped互斥(t *testing.T) {
	db, svc := newDynFixture(t)
	ctx := context.Background()
	dynSeedRole(t, db, "role_pub")

	// ① 新建时两个一起开 → 拒绝
	_, err := svc.CreateCollection(ctx, dynCaller(1001, "role_pub"), &DynCollectionInput{
		Code: "both_one", Name: "组合表", OwnerScoped: true, PublicRead: true,
	})
	assert.Error(t, err, "owner_scoped 与 public_read 互斥,新建时必须拒绝")

	// ② 改:给 owner_scoped 集合开 public_read → 拒绝(按生效值判,不看单边)
	id, err := svc.CreateCollection(ctx, dynCaller(1001, "role_pub"), &DynCollectionInput{
		Code: "both_two", Name: "组合表2", OwnerScoped: true,
	})
	require.NoError(t, err)
	yes := true
	err = svc.UpdateCollection(ctx, dynCaller(1001, "role_pub"), id, &DynCollectionUpdate{Name: "组合表2", PublicRead: &yes})
	assert.Error(t, err, "owner_scoped 集合不得再开 public_read")

	// ③ 反方向:公开集合开 owner_scoped → 同样拒绝
	id2, err := svc.CreateCollection(ctx, dynCaller(1001, "role_pub"), &DynCollectionInput{
		Code: "both_three", Name: "组合表3", PublicRead: true,
	})
	require.NoError(t, err)
	err = svc.UpdateCollection(ctx, dynCaller(1001, "role_pub"), id2, &DynCollectionUpdate{Name: "组合表3", OwnerScoped: &yes})
	assert.Error(t, err, "公开集合不得再开 owner_scoped")

	// ④ 改动被拒后库里没被写脏
	col, err := svc.GetCollectionByCode(ctx, "both_two")
	require.NoError(t, err)
	assert.False(t, col.PublicRead, "被拒的更新不得落库")
}
