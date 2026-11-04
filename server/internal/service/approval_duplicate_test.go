// 重复发起同一业务记录 —— D9 硬唯一索引的错误码收口(M88-5 / §13 M86-2h)
//
// ⚠️ **为什么 HTTP 通道已有的那条用例不够** ——
// `approval_query_test.go` 里的 `TestCreateDraft_DuplicateBizRejected` 走的是
// HTTP 通道的**预检查**(`approval_query_impl.go` 里那一句 `Count`),
// 预检查先命中,所以**永远走不到引擎的 INSERT**。
//
// 真出问题的那条路是:
// - 两个请求同时建草稿 → 预检查**双双通过** → 后一个 INSERT 撞唯一索引
// - 业务线直接调引擎(`SubmitMaterial`)→ **根本没有预检查**
//
// M88-5 之前这两条路撞唯一索引都是**裸数据库错误**,到 handler 那里是 **500**,
// 前端弹"服务器开小差了",而用户真正该看到的是"这条已经发过审批了"。
//
// 所以下面的用例盯的是**引擎那一道翻译**,不是预检查。
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

// draftWithBizID 用指定的 biz_id 建一张草稿(引擎层,不经 HTTP 预检查)
func draftWithBizID(t *testing.T, f *engineFixture, bizID uint64, title, tplCode string) error {
	t.Helper()
	ctx := context.Background()
	return f.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := f.engine.CreateDraft(ctx, tx, &CreateDraftInput{
			BizType:             testBizType,
			BizID:               bizID,
			Title:               title,
			Variables:           "{}",
			TemplateCode:        tplCode,
			ApplicantUserID:     100,
			ApplicantIdentityID: 1,
		})
		return err
	})
}

// TestEngineCreateDraft_DuplicateBizReturnsBusinessError
//
// 🔴 **这条是 M88-5 的核心断言** —— 撞唯一索引必须落成 **409 业务错误码**。
// 拆掉引擎里那一句 `isDuplicateErr` 翻译,这里立刻变红(错误码变成 500)。
func TestEngineCreateDraft_DuplicateBizReturnsBusinessError(t *testing.T) {
	f := newEngineFixture(t)
	tpl := "tpl_" + tplSuffix(t)
	f.newTemplate(t, tpl, []string{model.ApprovalReviewAny}, []int{1})

	const bizID = uint64(90210)

	// 第一条:成功
	require.NoError(t, draftWithBizID(t, f, bizID, "第一条", tpl))

	// 第二条:同一个 (biz_type, biz_id) → 业务错误码
	err := draftWithBizID(t, f, bizID, "第二条", tpl)
	require.Error(t, err, "同一条业务记录不该能建出第二张审批单")

	// ⚠️ **必须是 Conflict(409),不是 Internal(500)** ——
	// 500 的意思是"服务器坏了",而这里是"你已经发过了",用户重试多少次都没用。
	assert.Equal(t, errcode.Conflict, errcode.CodeOf(err))
	assert.NotEqual(t, errcode.Internal, errcode.CodeOf(err),
		"重复发起是业务错误,不能落成 500(前端会弹'服务器开小差了')")

	// ⚠️ **文案要是人话,不能是数据库原文** ——
	// 原样抛出来的话前端会显示 "UNIQUE constraint failed: sys_approval_instance.biz_type, ..."
	assert.Contains(t, err.Error(), "已经发过审批了")
	assert.NotContains(t, err.Error(), "UNIQUE constraint failed")
	assert.NotContains(t, err.Error(), "Duplicate entry")
}

// TestEngineCreateDraft_NonDuplicateErrorNotSwallowed
//
// 🔴 **反向断言** —— 只有确认是重复键才翻译,其余真故障原样上抛。
// 拆掉 `isDuplicateErr` 的判断(无脑翻译成 Conflict),这里立刻变红。
//
// 造一个"不是重复键"的 DB 错误:把实例表删掉,INSERT 直接报 "no such table"。
func TestEngineCreateDraft_NonDuplicateErrorNotSwallowed(t *testing.T) {
	f := newEngineFixture(t)
	tpl := "tpl_" + tplSuffix(t)
	f.newTemplate(t, tpl, []string{model.ApprovalReviewAny}, []int{1})

	require.NoError(t, f.db.Exec("DROP TABLE sys_approval_instance").Error)

	err := draftWithBizID(t, f, 4242, "表都没了", tpl)
	require.Error(t, err)

	// ⚠️ **不许被翻译成"你已经发过了"** —— 那是在骗用户,而且把真故障藏起来了
	assert.NotEqual(t, errcode.Conflict, errcode.CodeOf(err),
		"非重复键的 DB 错误不许翻译成'重复发起'")
}

// TestEngineCreateDraft_DuplicateLeavesExactlyOneInstance
//
// 🔴 **重复发起不能留下半截数据** —— 第二次建草稿失败,库里必须还是**一张**单子。
// 漏了这句,断言只查错误码就放过"写了半条"的实现。
func TestEngineCreateDraft_DuplicateLeavesExactlyOneInstance(t *testing.T) {
	f := newEngineFixture(t)
	tpl := "tpl_" + tplSuffix(t)
	f.newTemplate(t, tpl, []string{model.ApprovalReviewAny}, []int{1})

	const bizID = uint64(31337)
	require.NoError(t, draftWithBizID(t, f, bizID, "第一条", tpl))
	require.Error(t, draftWithBizID(t, f, bizID, "第二条", tpl))

	var n int64
	require.NoError(t, f.db.Model(&model.SysApprovalInstance{}).
		Where("biz_type = ? AND biz_id = ?", testBizType, bizID).
		Count(&n).Error)
	assert.Equal(t, int64(1), n, "重复发起之后库里应该还是一张单子")
}
