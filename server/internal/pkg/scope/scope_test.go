// scope_test.go —— 数据范围原语的单测 —— M155(2026-11-02)
//
// # 这套用例守的是"用户看得见的行为",不是"内部实现"
//
// 最重要的一条是 TestApply_真库上按范围过滤 ——
// 它在真 sqlite 上建表、插数据、按不同范围查,断言**返回的行**。
// 比断言 SQL 文本强:SQL 文本对得上不代表过滤对得上
// (列顺序、括号、OR/AND 优先级都能骗过文本断言)。
package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// item 测试用表:三种作用域维度都有
type item struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	ProjectID uint64
	DeptID    uint64
	OwnerID   uint64
	Name      string
}

func (item) TableName() string { return "scope_item" }

// newDB 建一个内存 sqlite 并灌 5 行数据
//
// 行设计(故意让三种维度的组合各不相同,才能测出"或"组不是"与"组):
//
//	id=1 project=10 dept=100 owner=1
//	id=2 project=10 dept=101 owner=2
//	id=3 project=20 dept=100 owner=3
//	id=4 project=20 dept=200 owner=1
//	id=5 project=30 dept=300 owner=5
func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&item{}))
	rows := []item{
		{ID: 1, ProjectID: 10, DeptID: 100, OwnerID: 1, Name: "a"},
		{ID: 2, ProjectID: 10, DeptID: 101, OwnerID: 2, Name: "b"},
		{ID: 3, ProjectID: 20, DeptID: 100, OwnerID: 3, Name: "c"},
		{ID: 4, ProjectID: 20, DeptID: 200, OwnerID: 1, Name: "d"},
		{ID: 5, ProjectID: 30, DeptID: 300, OwnerID: 5, Name: "e"},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	return db
}

// cols 测试表的三种维度列
func cols() Columns {
	return Columns{
		Project: "project_id IN ?",
		Dept:    "dept_id IN ?",
		Self:    "owner_id IN ?",
	}
}

// query 按范围查,返回命中的 id 列表(升序)
func query(t *testing.T, db *gorm.DB, s Scope, c Columns) []uint64 {
	t.Helper()
	tx := db.Model(&item{})
	tx, err := Apply(tx, &s, c)
	require.NoError(t, err)
	var ids []uint64
	require.NoError(t, tx.Order("id").Pluck("id", &ids).Error)
	return ids
}

func TestParseType(t *testing.T) {
	for _, s := range []string{"all", "project", "dept", "dept_and_child", "self", "custom"} {
		typ, err := ParseType(s)
		require.NoError(t, err, s)
		assert.Equal(t, Type(s), typ)
	}
	// 空串 = all(不是报错)—— 见 ParseType 的注释
	typ, err := ParseType("  ")
	require.NoError(t, err)
	assert.Equal(t, TypeAll, typ)
	// 大小写不敏感
	typ, err = ParseType("ALL")
	require.NoError(t, err)
	assert.Equal(t, TypeAll, typ)

	_, err = ParseType("nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知的数据范围类型")
}

func TestResolve_各类型落到对应的子句(t *testing.T) {
	actor := Actor{
		UserID:     7,
		ProjectIDs: []uint64{10, 20},
		DeptIDs:    []uint64{100},
		DeptSubtree: func(ids []uint64) ([]uint64, error) {
			return []uint64{ids[0], 101, 102}, nil
		},
	}

	// 本项目
	s, err := Resolve([]Rule{{Type: TypeProject}}, actor)
	require.NoError(t, err)
	assert.True(t, s.Restrictive)
	assert.Equal(t, []uint64{10, 20}, s.ProjectIDs)

	// 本部门
	s, err = Resolve([]Rule{{Type: TypeDept}}, actor)
	require.NoError(t, err)
	assert.Equal(t, []uint64{100}, s.DeptIDs)

	// 本部门及下级
	s, err = Resolve([]Rule{{Type: TypeDeptAndChild}}, actor)
	require.NoError(t, err)
	assert.Equal(t, []uint64{100, 101, 102}, s.DeptIDs)

	// 仅本人
	s, err = Resolve([]Rule{{Type: TypeSelf}}, actor)
	require.NoError(t, err)
	assert.Equal(t, []uint64{7}, s.SelfUserIDs)

	// 自定义
	s, err = Resolve([]Rule{{Type: TypeCustom, CustomColumn: "kind", CustomValues: []string{"a", "b"}}}, actor)
	require.NoError(t, err)
	require.Len(t, s.Customs, 1)
	assert.Equal(t, "kind", s.Customs[0].Column)
	assert.Equal(t, []string{"a", "b"}, s.Customs[0].Values)
}

func TestResolve_all一票放行(t *testing.T) {
	s, err := Resolve([]Rule{
		{Type: TypeAll},
		{Type: TypeProject},
	}, Actor{UserID: 1, ProjectIDs: []uint64{10}})
	require.NoError(t, err)
	assert.True(t, s.All)
	assert.False(t, s.Restrictive)
	assert.Empty(t, s.ProjectIDs)
}

func TestResolve_本项目但调用方没有项目时是看不到不是看全部(t *testing.T) {
	// ⚠️ 这是**安全方向**上的关键区别 ——
	// "本项目"规则 + 调用方没有项目 ⇒ 应当一条都看不到。
	// 若解析成"空 = 不过滤",他就看到了全部数据。
	s, err := Resolve([]Rule{{Type: TypeProject}}, Actor{UserID: 1})
	require.NoError(t, err)
	assert.True(t, s.Restrictive, "有限制性规则就必须标记为 restrictive")
	assert.True(t, s.Empty(), "没有任何子句")
	assert.False(t, s.All)
}

func TestResolve_自定义列名非法时报错(t *testing.T) {
	for _, col := range []string{"", "a b", "a;b", "1abc", "a-b", "a.b", "a'"} {
		_, err := Resolve([]Rule{{Type: TypeCustom, CustomColumn: col, CustomValues: []string{"x"}}}, Actor{})
		require.Error(t, err, "列名 %q 应当被拒", col)
	}
	_, err := Resolve([]Rule{{Type: TypeCustom, CustomColumn: "kind", CustomValues: []string{"x"}}}, Actor{})
	require.NoError(t, err)
}

func TestResolve_未知的类型报错(t *testing.T) {
	_, err := Resolve([]Rule{{Type: Type("nope")}}, Actor{})
	require.Error(t, err)
}

// TestApply_真库上按范围过滤 —— 这条是本文件最有价值的用例
//
// ⚠️ 断言的是**返回的行**,不是 SQL 文本 ——
// SQL 文本对得上不代表过滤对得上(列顺序 / 括号 / OR-AND 优先级都能骗过文本断言)。
func TestApply_真库上按范围过滤(t *testing.T) {
	db := newDB(t)
	actor := Actor{UserID: 7, ProjectIDs: []uint64{10, 20}}

	// 本项目(10/20)⇒ id 1,2,3,4
	s, err := Resolve([]Rule{{Type: TypeProject}}, actor)
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 2, 3, 4}, query(t, db, s, cols()))

	// 本部门(100)⇒ id 1,3
	s, err = Resolve([]Rule{{Type: TypeDept}}, Actor{UserID: 7, DeptIDs: []uint64{100}})
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 3}, query(t, db, s, cols()))

	// 仅本人(7)⇒ id 5 不对 —— owner=7 的行一行都没有
	// (id 5 的 owner 是 5,不是 7)⇒ 空结果
	s, err = Resolve([]Rule{{Type: TypeSelf}}, Actor{UserID: 7})
	require.NoError(t, err)
	assert.Empty(t, query(t, db, s, cols()))

	// 仅本人(owner=1)⇒ id 1,4
	s, err = Resolve([]Rule{{Type: TypeSelf}}, Actor{UserID: 1})
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 4}, query(t, db, s, cols()))
}

// TestApply_多子句是或不是与 —— 核心语义
func TestApply_多子句是或不是与(t *testing.T) {
	db := newDB(t)
	// 本部门 100 **或** 仅本人(owner=1)
	// 与的话结果会是空集;或的话是 1,3,4
	s, err := Resolve([]Rule{
		{Type: TypeDept},
		{Type: TypeSelf},
	}, Actor{UserID: 1, DeptIDs: []uint64{100}})
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 3, 4}, query(t, db, s, cols()))
}

func TestApply_没有限制性规则就不过滤(t *testing.T) {
	db := newDB(t)
	s, err := Resolve(nil, Actor{UserID: 1})
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 2, 3, 4, 5}, query(t, db, s, cols()))
}

func TestApply_列不存在时报错而不是静默放过(t *testing.T) {
	db := newDB(t)
	s, err := Resolve([]Rule{{Type: TypeProject}}, Actor{UserID: 1, ProjectIDs: []uint64{10}})
	require.NoError(t, err)

	// 空模板 = 这张表没有这一维度 ⇒ 必须报错
	_, err = Apply(db.Model(&item{}), &s, Columns{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "本项目范围落不到这张表上")

	// 模板里没有 ? ⇒ 报错
	_, err = Apply(db.Model(&item{}), &s, Columns{Project: "project_id"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "有且只有一个 ?")

	// 模板里有两个 ? ⇒ 报错(参数会串位,而且不报错就查到了错的数据)
	_, err = Apply(db.Model(&item{}), &s, Columns{Project: "project_id IN ? AND dept_id IN ?"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "有且只有一个 ?")
}

// TestApply_有限制但表达不出来时匹配不到任何行 —— 不是"不过滤"
func TestApply_有限制但表达不出来时匹配不到任何行(t *testing.T) {
	db := newDB(t)
	// "本项目"规则 + 调用方没有项目 ⇒ 空作用域
	s, err := Resolve([]Rule{{Type: TypeProject}}, Actor{UserID: 1})
	require.NoError(t, err)
	require.True(t, s.Empty())

	// ⚠️ 关键:结果必须是**空集**,不是全表
	assert.Empty(t, query(t, db, s, cols()))
}

// TestApply_部门子查询形式 —— 用户与部门是多对多,没有 dept_id 列
func TestApply_部门子查询形式(t *testing.T) {
	db := newDB(t)
	// 这张"表"上只能按 id 过滤,部门条件走子查询
	c := Columns{
		Project: "id IN (SELECT id FROM scope_item WHERE project_id IN ?)",
		Dept:    "id IN (SELECT id FROM scope_item WHERE dept_id IN ?)",
		Self:    "owner_id IN ?",
	}
	s, err := Resolve([]Rule{{Type: TypeDept}}, Actor{UserID: 1, DeptIDs: []uint64{100}})
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 3}, query(t, db, s, c))
}

func TestApply_自定义范围(t *testing.T) {
	db := newDB(t)
	// item 表没有 kind 列 —— 先加一列
	require.NoError(t, db.Exec("ALTER TABLE scope_item ADD COLUMN kind TEXT").Error)
	require.NoError(t, db.Exec("UPDATE scope_item SET kind='x' WHERE id IN (1,2)").Error)

	s, err := Resolve([]Rule{{Type: TypeCustom, CustomColumn: "kind", CustomValues: []string{"x"}}}, Actor{})
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 2}, query(t, db, s, cols()))
}

func TestDescribe(t *testing.T) {
	// ⚠️ **Describe 是指针接收者** —— Scope 有 104 字节,按值接收会被
	// gocritic hugeParam 报。复合字面量不可寻址,所以这里先落成变量。
	all := Scope{All: true}
	none := Scope{}
	empty := Scope{Restrictive: true}
	proj := Scope{Restrictive: true, ProjectIDs: []uint64{1}}

	assert.Equal(t, "全部数据", all.Describe())
	assert.Equal(t, "未配置(等于全部数据)", none.Describe())
	assert.Contains(t, empty.Describe(), "看不到任何数据")
	assert.Contains(t, proj.Describe(), "本项目")
}
