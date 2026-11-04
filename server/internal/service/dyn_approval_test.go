// dyn_approval_test.go —— 集合 × 审批模板绑定的守门用例(2026-11-03)
//
// ⚠️ **这批用例只测错误路径与绑定校验,不跑完整审批流** ——
// 引擎本身(建单 / 提交 / 关卡推进 / D9)在 approval_engine_test.go 有整套用例;
// 这层的增量是"绑定的校验"与"送审入口的前置检查",
// 全链路跑通需要模板 + 关卡 + 审批人解析整套夹具,在本层是重复建设。
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// dynApprovalFixture 起一个带审批模板表的夹具
func dynApprovalFixture(t *testing.T) (*gorm.DB, DynService) {
	t.Helper()
	db, svc := newDynFixture(t)
	require.NoError(t, db.AutoMigrate(&model.SysApprovalTemplate{}, &model.SysApprovalInstance{}))
	return db, svc
}

// TestDyn_审批_绑定不存在或停用的模板被拒
//
// 绑定的校验必须落在**写进去之前** —— 坏绑定存进去之后,
// 每条记录点「送审」才炸,用户只会觉得"这功能坏了"。
func TestDyn_审批_绑定不存在或停用的模板被拒(t *testing.T) {
	db, svc := dynApprovalFixture(t)
	ctx := context.Background()
	dynSeedRole(t, db, "role_dyn_tester")

	// 建一条启用模板 + 一条停用模板
	require.NoError(t, db.Create(&model.SysApprovalTemplate{
		BaseModel: model.BaseModel{ID: 8001}, Code: "t_on", Name: "启用", BizType: DynBizType, IsActive: true,
	}).Error)
	require.NoError(t, db.Create(&model.SysApprovalTemplate{
		BaseModel: model.BaseModel{ID: 8002}, Code: "t_off", Name: "停用", BizType: DynBizType, IsActive: false,
	}).Error)

	// 建集合:绑不存在的 id → 拒,且**不留半张物理表**
	_, err := svc.CreateCollection(ctx, dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "bad_tpl", Name: "坏模板", ApprovalTemplateID: 9999,
	})
	require.Error(t, err)

	// 建集合:绑停用的模板 → 同样拒
	_, err = svc.CreateCollection(ctx, dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "off_tpl", Name: "停用模板", ApprovalTemplateID: 8002,
	})
	require.Error(t, err)

	// 建集合:绑启用的模板 → 成,且存进去的是传进去的 id
	id, err := svc.CreateCollection(ctx, dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "ok_tpl", Name: "好模板", ApprovalTemplateID: 8001,
	})
	require.NoError(t, err)
	col, err := svc.GetCollection(ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint64(8001), col.ApprovalTemplateID)

	// 改绑成停用的模板 → 拒
	off := uint64(8002)
	require.Error(t, svc.UpdateCollection(ctx, dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name: "好模板", ApprovalTemplateID: &off,
	}))

	// 解绑(传 0)→ 成
	zero := uint64(0)
	require.NoError(t, svc.UpdateCollection(ctx, dynCaller(1001, "role_dyn_tester"), id, &DynCollectionUpdate{
		Name: "好模板", ApprovalTemplateID: &zero,
	}))
}

// TestDyn_审批_送审前置检查
func TestDyn_审批_送审前置检查(t *testing.T) {
	db, svc := dynApprovalFixture(t)
	ctx := context.Background()
	dynSeedRole(t, db, "role_dyn_tester")

	// 集合 1:没绑模板;集合 2:绑了模板但 service 没装引擎(单测默认 nil)
	_, err := svc.CreateCollection(ctx, dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "nobind", Name: "没绑定",
	})
	require.NoError(t, err)
	// ⚠️ 模板先落库再建集合 —— CreateCollection 当场校验绑定
	require.NoError(t, db.Create(&model.SysApprovalTemplate{
		BaseModel: model.BaseModel{ID: 8001}, Code: "t1", Name: "模板", BizType: DynBizType, IsActive: true,
	}).Error)
	_, err = svc.CreateCollection(ctx, dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "bound", Name: "绑了", ApprovalTemplateID: 8001,
	})
	require.NoError(t, err)

	// 没绑模板 → 明确说"该集合未绑定审批模板"
	_, err = svc.SubmitRecordApproval(ctx, dynCaller(1001, "role_dyn_tester"), "nobind", 1)
	require.ErrorContains(t, err, "未绑定审批模板")

	// 绑了但引擎没装(单测装配)→ 明确说"未装配审批引擎"
	_, err = svc.SubmitRecordApproval(ctx, dynCaller(1001, "role_dyn_tester"), "bound", 1)
	require.ErrorContains(t, err, "未装配审批引擎")
}

// TestDyn_审批_送审全链路_建草稿与提交不嵌套事务
//
// ## 这条网守的是什么
//
//	送审 = CreateDraft + Submit 一步走(dyn_approval.go 文件头 ③)。
//	第一版把 Submit 包进了 CreateDraft 的外层事务 —— 而 Submit 的实现
//	(withLockedInstance)自己再开一个事务:两个连接嵌套等锁。
//	本夹具的连接池限死 1 条(approval 夹具的规矩,shared-cache SQLite
//	两个连接并发写会直接 SQLITE_LOCKED),旧代码在这套夹具上就是
//	**当场挂死** —— 外层事务攥着唯一的连接等 Submit 返回,Submit 等
//	连接池腾出连接,谁也不让谁。真 server 上(dev.db 多连接)换了个
//	死法:文件锁死锁,整库焊死,所有请求 30s 超时。
//
//	修法 = 草稿事务先落袋,Submit 在事务外面(资料线的两步走)。
//
// ## 为什么用 10s 的 ctx 超时
//
//	回归的时候(有人把 Submit 又包回事务里),database/sql 等连接
//	尊重 ctx —— 用例会**在 10 秒内**红着退出,而不是挂到 go test 的
//	全局超时把整包拖死。
func TestDyn_审批_送审全链路_建草稿与提交不嵌套事务(t *testing.T) {
	db, dyn := newDynFixture(t)
	dynSeedRole(t, db, "role_dyn_tester")
	// 全链路要真引擎 → 审批七张表 + 解析器要的组织表都要在
	require.NoError(t, db.AutoMigrate(approvalTestModels...))

	// 真引擎 + 假解析器(按模板关卡返回固定审批人)+ dyn 的空转 handler
	publisher := &recordingPublisher{}
	engine := NewApprovalEngine(db,
		&stubResolver{byTemplate: map[uint64]map[int][]uint64{
			8001: {1: {2002}}, // 模板 8001 的第 1 关 → 审批人 2002(≠ 提交人 1001,D11)
		}},
		map[string]BizHandler{DynBizType: NewDynApprovalHandler()},
		publisher,
	)
	svc := NewDynService(db, nil, nil, engine).(*dynService)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 模板 + 一关(引擎要求 stages[0].StageIndex == 1)
	require.NoError(t, db.Create(&model.SysApprovalTemplate{
		BaseModel: model.BaseModel{ID: 8001}, Code: "t_full", Name: "全链路", BizType: DynBizType, IsActive: true,
	}).Error)
	require.NoError(t, db.Create(&model.SysApprovalStage{
		BaseModel: model.BaseModel{ID: 8101}, TemplateID: 8001, StageIndex: 1,
		Name: "审批", ReviewType: model.ApprovalReviewAny,
	}).Error)

	colID, err := dyn.CreateCollection(ctx, dynCaller(1001, "role_dyn_tester"), &DynCollectionInput{
		Code: "fullpath", Name: "全链路集合", ApprovalTemplateID: 8001,
	})
	require.NoError(t, err)
	require.NotZero(t, colID)

	// 记录 payload 的键必须逐个对上集合字段 —— 先建一个字段
	_, err = dyn.CreateField(ctx, dynCaller(1001, "role_dyn_tester"), colID, &DynFieldInput{
		FieldKey: "title", Label: "标题", Type: "text",
	})
	require.NoError(t, err)

	recID, err := dyn.CreateRecord(ctx, dynCaller(1001, "role_dyn_tester"), "fullpath",
		map[string]any{"title": "送审我"})
	require.NoError(t, err)

	instID, err := svc.SubmitRecordApproval(ctx, dynCaller(1001, "role_dyn_tester"), "fullpath", recID)
	require.NoError(t, err)
	require.NotZero(t, instID)

	// 提交成功:实例进 running、第 1 关有待办任务、事件发过一次 assigned
	var inst model.SysApprovalInstance
	require.NoError(t, db.First(&inst, instID).Error)
	require.Equal(t, model.ApprovalStatusRunning, inst.Status)
	require.Equal(t, 1, inst.CurrentStage)
	require.Equal(t, 1, len(publisher.assignees()), "提交后要给审批人发 assigned 事件")
}
