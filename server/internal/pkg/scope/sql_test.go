// sql_test.go —— 纯 SQL 出口的单测 —— M159(2026-11-02)
//
// # 这套用例守的是"两个出口不许漂移"
//
// Apply(GORM 链式)与 ApplySQL(裸 SQL)必须由同一个核心函数编译 ——
// 否则同一套规则在两条路上表现不同,而两条路都跑得好好的,没有任何报错。
//
// ⇒ 最有价值的一条是 TestApplySQL_与GORM那条路查出来的行完全一样 ——
// 它在真 sqlite 上把两条路各跑一遍,比对**返回的行**(不是 SQL 文本)。
//
// # 探针标记
//
// GR1 空片段 = 不过滤
// GR2 有限制但表达不出来 ⇒ "1 = 0"
// GR3 占位符个数 == 参数个数
// GR4 两条路结果一致
// GR5 模板的 ? 必须在 IN 后面
package scope

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// rawQuery 走裸 SQL 那条路查,返回命中的 id 列表(升序)
//
// ⚠️ **刻意不经过 GORM 的链式 API** ——
// 这里拼的是完整 SQL,和动态业务集合(dyn)的数据面一个写法。
func rawQuery(t *testing.T, db *gorm.DB, s Scope, c Columns) []uint64 {
	t.Helper()
	frag, args, err := ApplySQL(&s, c)
	require.NoError(t, err)
	if frag == "" {
		// 没有限制性规则 ⇒ 不拼条件
		var ids []uint64
		require.NoError(t, db.Raw("SELECT id FROM scope_item ORDER BY id").Scan(&ids).Error)
		return ids
	}
	var ids []uint64
	sql := "SELECT id FROM scope_item WHERE " + frag + " ORDER BY id"
	require.NoError(t, db.Raw(sql, args...).Scan(&ids).Error)
	return ids
}

// GR1 —— 没有限制性规则时返回**空片段**(不是 "1 = 0")
func TestApplySQL_没有限制性规则时返回空片段(t *testing.T) {
	s, err := Resolve(nil, Actor{UserID: 1})
	require.NoError(t, err)
	frag, args, err := ApplySQL(&s, cols())
	require.NoError(t, err)
	assert.Empty(t, frag, "空片段 = 不过滤")
	assert.Empty(t, args)

	// all 也是空片段
	s, err = Resolve([]Rule{{Type: TypeAll}}, Actor{UserID: 1})
	require.NoError(t, err)
	frag, args, err = ApplySQL(&s, cols())
	require.NoError(t, err)
	assert.Empty(t, frag)
	assert.Empty(t, args)
}

// GR2 —— 有限制但一条都表达不出来 ⇒ "1 = 0"(不是空片段)
//
// ⚠️ 这是**安全方向**上的关键区别:
// 退化成空片段 = 不过滤 = 他看到了全部数据 —— 那是安全事故。
func TestApplySQL_有限制但表达不出来时是1等于0(t *testing.T) {
	s, err := Resolve([]Rule{{Type: TypeProject}}, Actor{UserID: 1})
	require.NoError(t, err)
	require.True(t, s.Restrictive)
	require.True(t, s.Empty())

	frag, args, err := ApplySQL(&s, cols())
	require.NoError(t, err)
	assert.Equal(t, "1 = 0", frag)
	assert.Empty(t, args)
}

// GR3 —— 占位符个数必须等于参数个数
//
// ⚠️ 对不上时 SQL 会串位 —— 而且**不报错**,只是查到错的数据。
// 这条是机械不变量,比逐个断言 SQL 文本更能守住"以后重构不改坏"。
func TestApplySQL_占位符个数等于参数个数(t *testing.T) {
	actor := Actor{UserID: 7, ProjectIDs: []uint64{10, 20}, DeptIDs: []uint64{100, 101, 102}}
	cases := [][]Rule{
		{{Type: TypeProject}},
		{{Type: TypeDept}},
		{{Type: TypeSelf}},
		{{Type: TypeProject}, {Type: TypeSelf}},
		{{Type: TypeProject}, {Type: TypeDept}, {Type: TypeSelf}},
		{{Type: TypeCustom, CustomColumn: "kind", CustomValues: []string{"a", "b", "c"}}},
		{{Type: TypeProject}, {Type: TypeCustom, CustomColumn: "kind", CustomValues: []string{"x"}}},
	}
	for _, rules := range cases {
		s, err := Resolve(rules, actor)
		require.NoError(t, err)
		frag, args, err := ApplySQL(&s, cols())
		require.NoError(t, err)
		assert.Equal(t, strings.Count(frag, "?"), len(args),
			"片段 %q 的占位符个数与参数个数对不上", frag)
	}
}

// GR4 —— 两个出口查出来的行必须完全一样
//
// ⚠️ **这是本文件最有价值的一条** ——
// Apply 与 ApplySQL 是两条不同的接线(GORM 链式 vs 裸 SQL),
// 一旦语义漂移,管理员配的规则在一条路上生效、在另一条路上不生效,
// 而且**没有任何报错**。
//
// 比对的是**返回的行**,不是 SQL 文本:SQL 文本对得上不代表过滤对得上。
func TestApplySQL_与GORM那条路查出来的行完全一样(t *testing.T) {
	db := newDB(t)
	actor := Actor{UserID: 1, ProjectIDs: []uint64{10, 20}, DeptIDs: []uint64{100}}

	cases := []struct {
		name  string
		rules []Rule
	}{
		{"本项目", []Rule{{Type: TypeProject}}},
		{"本部门", []Rule{{Type: TypeDept}}},
		{"仅本人", []Rule{{Type: TypeSelf}}},
		{"本部门 或 仅本人", []Rule{{Type: TypeDept}, {Type: TypeSelf}}},
		{"本项目 或 本部门 或 仅本人", []Rule{
			{Type: TypeProject}, {Type: TypeDept}, {Type: TypeSelf}}},
		{"本项目但调用方没有项目", []Rule{{Type: TypeProject}}},
	}
	for _, tc := range cases {
		a := actor
		if tc.name == "本项目但调用方没有项目" {
			a = Actor{UserID: 1}
		}
		s, err := Resolve(tc.rules, a)
		require.NoError(t, err)
		gormIDs := query(t, db, s, cols())
		rawIDs := rawQuery(t, db, s, cols())
		assert.Equal(t, normalizeIDs(gormIDs), normalizeIDs(rawIDs),
			"%s:两条路查出来的行不一样", tc.name)
	}
}

// normalizeIDs 把 nil 归一成空切片
//
// ⚠️ **只为让断言可比** ——
// GORM 的 Pluck 返回空切片([]uint64{}),而 Scan 进一个 nil 切片返回 nil,
// 两者都是"没有行",但 reflect.DeepEqual 判不相等。
//
// ⚠️ **这不是在掩盖真实差异** —— 行数与行内容都由上面的断言覆盖,
// 这里只统一"空"的表示形式。
func normalizeIDs(ids []uint64) []uint64 {
	if ids == nil {
		return []uint64{}
	}
	return ids
}

// GR4 续 —— 子查询形式的模板,两条路也要一致
//
// ⚠️ 用户与部门是多对多,没有 dept_id 列,模板只能写成子查询 ——
// 而子查询模板里的 `?` 在两条路上的绑定方式不同(GORM 展开 vs 手工展开),
// 这正是最容易漂的地方。
func TestApplySQL_子查询模板两条路也一致(t *testing.T) {
	db := newDB(t)
	c := Columns{
		Project: "id IN (SELECT id FROM scope_item WHERE project_id IN ?)",
		Dept:    "id IN (SELECT id FROM scope_item WHERE dept_id IN ?)",
		Self:    "owner_id IN ?",
	}
	s, err := Resolve([]Rule{{Type: TypeDept}}, Actor{UserID: 1, DeptIDs: []uint64{100}})
	require.NoError(t, err)
	assert.Equal(t, query(t, db, s, c), rawQuery(t, db, s, c))
}

// GR5 —— 模板里的 ? 必须紧跟在 IN 后面
//
// ⚠️ `col = ?` 会被编译成 `col = (?, ?)` —— 那不是合法 SQL,
// 而数据库报的错离现场很远("near ?": syntax error),管理员根本看不懂。
func TestApplySQL_模板的问号必须在IN后面(t *testing.T) {
	s, err := Resolve([]Rule{{Type: TypeProject}}, Actor{UserID: 1, ProjectIDs: []uint64{10}})
	require.NoError(t, err)

	for _, bad := range []string{
		"project_id = ?",             // 等号不是 IN
		"project_id > ?",             // 比较不是 IN
		"project_id IN (?)",          // 已经带括号 ⇒ 会被编成 IN ((?, ?))
		"originIN ?",                 // 没有空格 ⇒ "IN" 是词尾,不是关键字
		"project_id IN ? AND x IN ?", // 两个 ?(这条另有专门的理由)
	} {
		_, _, err := ApplySQL(&s, Columns{Project: bad})
		require.Error(t, err, "模板 %q 应当被拒", bad)
	}

	// 合法的两个
	for _, ok := range []string{
		"project_id IN ?",
		"id IN (SELECT id FROM scope_item WHERE project_id IN ?)",
		"project_id NOT IN ?",
	} {
		_, _, err := ApplySQL(&s, Columns{Project: ok})
		require.NoError(t, err, "模板 %q 应当被接受", ok)
	}
}

// 模板里有两个 ? ⇒ 报错(参数会串位,而且不报错就查到了错的数据)
func TestApplySQL_模板里有两个问号时报错(t *testing.T) {
	s, err := Resolve([]Rule{{Type: TypeProject}}, Actor{UserID: 1, ProjectIDs: []uint64{10}})
	require.NoError(t, err)
	_, _, err = ApplySQL(&s, Columns{Project: "project_id IN ? AND dept_id IN ?"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "有且只有一个 ?")
}

// 空模板 ⇒ 报错(这张表没有这一维度)
func TestApplySQL_空模板时报错(t *testing.T) {
	s, err := Resolve([]Rule{{Type: TypeProject}}, Actor{UserID: 1, ProjectIDs: []uint64{10}})
	require.NoError(t, err)
	_, _, err = ApplySQL(&s, Columns{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "本项目范围落不到这张表上")
}

// 空指针是编程错误 ⇒ 报错,不是"没有限制"
//
// ⚠️ 静默当成"不过滤"就是**数据越权**。
func TestApplySQL_空指针报错(t *testing.T) {
	_, _, err := ApplySQL(nil, cols())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "空指针")
}

// 自定义范围:列名 + 取值
func TestApplySQL_自定义范围(t *testing.T) {
	db := newDB(t)
	require.NoError(t, db.Exec("ALTER TABLE scope_item ADD COLUMN kind TEXT").Error)
	require.NoError(t, db.Exec("UPDATE scope_item SET kind='x' WHERE id IN (1,2)").Error)

	s, err := Resolve(
		[]Rule{{Type: TypeCustom, CustomColumn: "kind", CustomValues: []string{"x"}}},
		Actor{},
	)
	require.NoError(t, err)
	frag, args, err := ApplySQL(&s, cols())
	require.NoError(t, err)
	assert.Equal(t, "(kind IN (?))", frag)
	assert.Equal(t, []any{"x"}, args)
	assert.Equal(t, []uint64{1, 2}, rawQuery(t, db, s, cols()))
}

// 自定义列名非法 ⇒ 报错(防 SQL 注入的那道闸)
func TestApplySQL_自定义列名非法时报错(t *testing.T) {
	s := Scope{
		Restrictive: true,
		Customs:     []CustomTerm{{Column: "a; DROP TABLE scope_item", Values: []string{"x"}}},
	}
	_, _, err := ApplySQL(&s, cols())
	require.Error(t, err)
}
