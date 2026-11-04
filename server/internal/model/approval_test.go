// 审批模块 model 单测(M83)
//
// ⚠️ **这里的索引测试一律断言"gorm 解析出来的索引",不断言 tag 字符串**。
// 理由见 `file_test.go:1-11`:断言 tag 只能证明"我写了什么",证明不了"gorm 建了什么";
// 而唯一索引这件事,错的正好是后者。
//
// ⚠️ **光断言索引名和列还不够** —— 所以本文件最后有一组
// `TestApproval_UniqueIndexActuallyBites`:真 AutoMigrate 建表,真插重复行,
// 断言数据库**真的拒绝**。tag 写对了但 DDL 没建出来的情况,只有真库能证伪。
package model

import (
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// plainIndexes 返回 gorm 实际会建的**普通**(非唯一)索引:名字 → 列名列表
//
// ⚠️ 与 `uniqueIndexes` 分开是因为两者服务不同的断言:
// 唯一索引错 = 业务数据撞车;普通索引错 = 查询变慢(不致命,但"box=todo"是热路径)。
func plainIndexes(t *testing.T, m any) map[string][]string {
	t.Helper()
	s, err := schema.Parse(m, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	out := map[string][]string{}
	for _, idx := range s.ParseIndexes() {
		if idx.Class == "UNIQUE" {
			continue
		}
		cols := make([]string, 0, len(idx.Fields))
		for _, f := range idx.Fields {
			cols = append(cols, f.DBName)
		}
		out[idx.Name] = cols
	}
	return out
}

// TestApproval_UniqueIndexes §6.2 点名要断言的四条唯一索引
//
// ⚠️ **列顺序也要断言** —— `uk_app_inst_biz` 写成 `(biz_id, biz_type)` 名字对、
// 约束也对,但 `(biz_type, biz_id)` 才是"按业务类型查它的所有实例"要的顺序。
func TestApproval_UniqueIndexes(t *testing.T) {
	cases := []struct {
		model any
		name  string
		cols  []string
	}{
		{&SysApprovalTemplate{}, "uk_app_tpl_code", []string{"code"}},
		{&SysApprovalStage{}, "uk_app_stage_tpl_idx", []string{"template_id", "stage_index"}},
		{&SysApprovalInstance{}, "uk_app_inst_biz", []string{"biz_type", "biz_id"}},
		{&SysApprovalRound{}, "uk_app_round_inst", []string{"instance_id", "round_no"}},
	}
	for _, c := range cases {
		idx := uniqueIndexes(t, c.model)
		got, ok := idx[c.name]
		require.True(t, ok, "应有 %s 唯一索引,实际索引=%v", c.name, idx)
		assert.Equal(t, c.cols, got, "%s 的列和顺序必须完全一致", c.name)
	}
}

// TestApproval_NoUniqueIndexHasDeletedAt 锁住"唯一索引不含 deleted_at"。
//
// 原因:SQL 里 UNIQUE 认为 NULL 互不相等,而活跃行的 deleted_at 恰好是 NULL,
// 加上它唯一性就完全不生效。本仓 `file_test.go:76` 有实测。
//
// ⚠️ **审批这里的后果比文件夹更重**:`uk_app_inst_biz` 失效 = 同一条业务记录
// 能建出两个审批实例(D9 形同虚设),而且**不报错**。
func TestApproval_NoUniqueIndexHasDeletedAt(t *testing.T) {
	models := []any{
		&SysApprovalTemplate{}, &SysApprovalStage{}, &SysApprovalAssigneeRule{},
		&SysApprovalInstance{}, &SysApprovalTask{}, &SysApprovalLog{}, &SysApprovalRound{},
	}
	for _, m := range models {
		for name, cols := range uniqueIndexes(t, m) {
			for _, c := range cols {
				assert.NotEqual(t, "deleted_at", c,
					"索引 %s 含 deleted_at → 活跃行之间不再互斥", name)
			}
		}
	}
}

// TestApproval_CompositeIndexes 复合索引的列和顺序
//
// ⚠️ 这五条各自服务一条**具体**的查询,少一列或换顺序都会让那条查询退化成全表扫:
//
//	idx_app_inst_applicant       → `WHERE applicant_user_id=?`                 (box=mine)
//	idx_app_inst_status_submitted → `WHERE status='running' AND submitted_at<?` (cronx 巡检)
//	idx_app_task_assignee_status  → `WHERE assignee_user_id=? AND status=?`     (box=todo,最高频)
//	idx_app_task_inst             → `WHERE instance_id=? AND stage_index=?`     (会签分母 / stale)
//	idx_app_log_inst              → `WHERE instance_id=? ORDER BY round_no,id`
//
// ⚠️ **`idx_app_inst_applicant` 含 `status` 这件事**:GORM 支持一个字段挂多个
// `index:` tag(`schema/index.go` 的 `parseFieldIndexes` 按 `;` 逐段扫 tag,
// 不是 map 覆盖),所以 `SysApprovalInstance.Status` 同时进两个复合索引。
// 这条断言就是防止有人"简化"成单列索引时没人发现。
func TestApproval_CompositeIndexes(t *testing.T) {
	cases := []struct {
		model any
		name  string
		cols  []string
	}{
		{&SysApprovalInstance{}, "idx_app_inst_applicant", []string{"applicant_user_id", "status"}},
		{&SysApprovalInstance{}, "idx_app_inst_status_submitted", []string{"status", "submitted_at"}},
		{&SysApprovalTask{}, "idx_app_task_assignee_status", []string{"assignee_user_id", "status"}},
		{&SysApprovalTask{}, "idx_app_task_inst", []string{"instance_id", "stage_index"}},
		{&SysApprovalLog{}, "idx_app_log_inst", []string{"instance_id", "round_no"}},
	}
	for _, c := range cases {
		idx := plainIndexes(t, c.model)
		got, ok := idx[c.name]
		require.True(t, ok, "应有 %s 普通索引,实际索引=%v", c.name, idx)
		assert.Equal(t, c.cols, got, "%s 的列和顺序必须完全一致", c.name)
	}
}

// TestApproval_TableNames 表名必须带 sys_ 前缀
//
// ⚠️ 断言的是**表名**不是 Go 类型名 —— 表名才是迁移产物,前缀写错会跟
// `biz_` 命名空间撞车(§6.1 的硬规矩)。
func TestApproval_TableNames(t *testing.T) {
	assert.Equal(t, "sys_approval_template", SysApprovalTemplate{}.TableName())
	assert.Equal(t, "sys_approval_stage", SysApprovalStage{}.TableName())
	assert.Equal(t, "sys_approval_assignee_rule", SysApprovalAssigneeRule{}.TableName())
	assert.Equal(t, "sys_approval_instance", SysApprovalInstance{}.TableName())
	assert.Equal(t, "sys_approval_task", SysApprovalTask{}.TableName())
	assert.Equal(t, "sys_approval_log", SysApprovalLog{}.TableName())
	assert.Equal(t, "sys_approval_round", SysApprovalRound{}.TableName())
}

// TestApproval_ReviewTypeDefault 关卡的 review_type 默认 any
//
// ⚠️ **默认值必须是 any 而不是空串** —— 空串既不是 all 也不是 any,
// §4.1 的判定 switch 会落空,那一关既不推进也不终止,单子卡死。
//
// ⚠️ 这里断言的是 **gorm 解析出来的 tag 默认值**;真库行为由下面的
// `TestApproval_AutoMigrate` 兜底(它在真 DDL 上验证)。
func TestApproval_ReviewTypeDefault(t *testing.T) {
	s, err := schema.Parse(&SysApprovalStage{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	for _, f := range s.Fields {
		if f.DBName == "review_type" {
			assert.Equal(t, "any", f.DefaultValue, "review_type 默认必须是 any")
			assert.True(t, f.HasDefaultValue)
			return
		}
	}
	t.Fatal("SysApprovalStage 应有 review_type 列")
}

// ---------------------------------------------------------------------------
// 真库验证 —— 上面全是"gorm 会怎么解析",这里是"数据库到底建了什么"
// ---------------------------------------------------------------------------

func openApprovalTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&SysApprovalTemplate{}, &SysApprovalStage{}, &SysApprovalAssigneeRule{},
		&SysApprovalInstance{}, &SysApprovalTask{}, &SysApprovalLog{}, &SysApprovalRound{},
	))
	return db
}

// TestApproval_AutoMigrate 七张表能建出来,且 default:'any' 在真 DDL 上生效
//
// ⚠️ **`default:'any'` 带引号不是给 gorm 看的** —— gorm 会把引号剥掉
// (`schema/field.go:265-267` 再由 `Explain` 重新加引号),产出的是 `DEFAULT 'any'`。
// 但这是**读源码推出来的**,真 DDL 上是什么只有真库知道 —— 所以这里真插一行
// 不带 review_type 的关卡,断言读回来是 "any" 而不是 ""。
func TestApproval_AutoMigrate(t *testing.T) {
	db := openApprovalTestDB(t)

	tpl := &SysApprovalTemplate{Code: "t1", Name: "模板", BizType: "material"}
	tpl.ID = 1
	require.NoError(t, db.Create(tpl).Error)

	stage := &SysApprovalStage{TemplateID: 1, StageIndex: 1, Name: "第一关"}
	stage.ID = 1
	require.NoError(t, db.Create(stage).Error)

	var got SysApprovalStage
	require.NoError(t, db.First(&got, "id = ?", 1).Error)
	assert.Equal(t, ApprovalReviewAny, got.ReviewType,
		"review_type 的默认值必须在真 DDL 上生效,空串会让 §4.1 的判定落空")
}

// TestApproval_UniqueIndexActuallyBites 四条唯一索引必须在真库上真的拒绝重复
//
// ⚠️ **这是本文件最有价值的一条** —— 上面所有断言加起来也只证明了
// "gorm 解析出了这几个索引",证明不了"数据库真的建了并生效"。
// tag 写对了但 DDL 没建出来 / 建成了非唯一 / 列顺序不对,只有真插重复行能证伪。
//
// 覆盖的三种真实事故:
//
//	· uk_app_tpl_code 失效   → 同一个模板编码能建两条,业务侧按 code 取模板就取错了
//	· uk_app_inst_biz 失效   → 同一条业务记录能建两个审批实例(D9 形同虚设)
//	· uk_app_round_inst 失效 → 并发重提能撞出两个 round_no,轮次历史就乱了
func TestApproval_UniqueIndexActuallyBites(t *testing.T) {
	db := openApprovalTestDB(t)

	t.Run("模板 code 唯一", func(t *testing.T) {
		a := &SysApprovalTemplate{Code: "dup", Name: "a"}
		a.ID = 1
		require.NoError(t, db.Create(a).Error)

		b := &SysApprovalTemplate{Code: "dup", Name: "b"}
		b.ID = 2
		err := db.Create(b).Error
		assert.Error(t, err, "同 code 的模板必须被拒")
	})

	t.Run("同模板同序号只能有一个关卡", func(t *testing.T) {
		a := &SysApprovalStage{TemplateID: 9, StageIndex: 1, Name: "a"}
		a.ID = 1
		require.NoError(t, db.Create(a).Error)

		b := &SysApprovalStage{TemplateID: 9, StageIndex: 1, Name: "b"}
		b.ID = 2
		err := db.Create(b).Error
		assert.Error(t, err, "重复 stage_index 必须被拒 —— 命中多行会让这一关被算两次")
	})

	t.Run("同业务记录只能有一个审批实例", func(t *testing.T) {
		a := &SysApprovalInstance{BizType: "material", BizID: 42}
		a.ID = 1
		require.NoError(t, db.Create(a).Error)

		b := &SysApprovalInstance{BizType: "material", BizID: 42}
		b.ID = 2
		err := db.Create(b).Error
		assert.Error(t, err, "同一条业务记录必须只能有一个审批实例(D9)")
	})

	t.Run("并发重提撞不出两行", func(t *testing.T) {
		a := &SysApprovalRound{InstanceID: 7, RoundNo: 1}
		a.ID = 1
		require.NoError(t, db.Create(a).Error)

		b := &SysApprovalRound{InstanceID: 7, RoundNo: 1}
		b.ID = 2
		err := db.Create(b).Error
		assert.Error(t, err, "同实例同轮次必须被拒")
	})
}

// TestApproval_SoftDeletedRowStillBlocksDuplicate
//
// ⚠️ **软删之后仍然占着唯一坑位** —— 这是 `uk_app_tpl_code` 不含 deleted_at
// 的直接后果,也是 D7 要求"删除模板时必须把 code 改写成 code#deleted#<id>"的原因。
//
// 这条测试把那个后果**钉死**:将来谁删掉了改写 code 的逻辑,这里会红。
func TestApproval_SoftDeletedRowStillBlocksDuplicate(t *testing.T) {
	db := openApprovalTestDB(t)

	a := &SysApprovalTemplate{Code: "gone", Name: "a"}
	a.ID = 1
	require.NoError(t, db.Create(a).Error)
	require.NoError(t, db.Delete(&SysApprovalTemplate{}, "id = ?", 1).Error)

	b := &SysApprovalTemplate{Code: "gone", Name: "b"}
	b.ID = 2
	err := db.Create(b).Error
	assert.Error(t, err,
		"软删的行仍然占着唯一坑位 —— 所以 D7 要求删除时改写 code,不是靠 deleted_at 释放")
}

// TestApproval_LogSoftDeleteHazard 把 ⑥ 的警告变成可执行的证据
//
// 🔴 `sys_approval_log` **只 append**。这条测试演示**如果**有人对它调了 Delete(),
// 会发生什么:因为 `gorm.DeletedAt` 存在,GORM 给该 model 的每一条查询自动加
// `deleted_at IS NULL`,所以那条意见**从详情页消失,而且没有任何报错**。
//
// ⚠️ **本测试自己调了 Delete —— 这是故意的**,它是"为什么会这样"的证据。
// 生产代码里任何地方对 log 调 Delete 都是 bug,靠注释和 code review 挡。
func TestApproval_LogSoftDeleteHazard(t *testing.T) {
	db := openApprovalTestDB(t)

	l1 := &SysApprovalLog{InstanceID: 5, RoundNo: 1, Action: "submit"}
	l1.ID = 1
	require.NoError(t, db.Create(l1).Error)

	l2 := &SysApprovalLog{InstanceID: 5, RoundNo: 1, Action: "approve"}
	l2.ID = 2
	require.NoError(t, db.Create(l2).Error)

	var before int64
	require.NoError(t, db.Model(&SysApprovalLog{}).Where("instance_id = ?", 5).Count(&before).Error)
	require.Equal(t, int64(2), before)

	// ⚠️ 这一行就是事故本身 —— 任何生产代码都不许有
	require.NoError(t, db.Delete(&SysApprovalLog{}, "id = ?", 2).Error)

	var after int64
	require.NoError(t, db.Model(&SysApprovalLog{}).Where("instance_id = ?", 5).Count(&after).Error)
	assert.Equal(t, int64(1), after,
		"演示:软删一条 log 后,普通查询就再也看不到它了,且没有任何报错")

	// 底层数据还在 —— 这正是"静默丢意见"难查的原因
	var raw int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_approval_log WHERE instance_id = ?", 5).Scan(&raw).Error)
	assert.Equal(t, int64(2), raw, "底层行还在,只是被 GORM 的软删除作用域藏起来了")
}
