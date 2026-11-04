package dynfilter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/dynx"
)

// fields 测试用的字段表
var fields = map[string]Field{
	"status": {Column: "f_status", Type: dynx.TypeSelect},
	"title":  {Column: "f_title", Type: dynx.TypeText},
	"amount": {Column: "f_amount", Type: dynx.TypeInt},
	"price":  {Column: "f_price", Type: dynx.TypeNumber},
	"flag":   {Column: "f_flag", Type: dynx.TypeBool},
	"due":    {Column: "f_due", Type: dynx.TypeDate},
	"tags":   {Column: "f_tags", Type: dynx.TypeMultiSelect},
	"extra":  {Column: "f_extra", Type: dynx.TypeJSON},
}

func lookup(key string) (Field, bool) {
	f, ok := fields[key]
	return f, ok
}

func mustCompile(t *testing.T, expr string) (string, []any) {
	t.Helper()
	sql, args, err := Compile(expr, lookup)
	require.NoError(t, err)
	return sql, args
}

func TestCompile_单条件与占位符化(t *testing.T) {
	sql, args := mustCompile(t, `status = 'active'`)
	assert.Equal(t, "(`f_status` = ?)", sql)
	assert.Equal(t, []any{"active"}, args)

	sql, args = mustCompile(t, `amount > 100`)
	assert.Equal(t, "(`f_amount` > ?)", sql)
	// ⚠️ 整数字段进的是 int64,不是字符串 —— 隐式转换既吃索引又可能比错
	assert.Equal(t, []any{int64(100)}, args)

	sql, args = mustCompile(t, `price >= 1.5`)
	assert.Equal(t, "(`f_price` >= ?)", sql)
	assert.Equal(t, []any{1.5}, args)
}

func TestCompile_逻辑组合与优先级(t *testing.T) {
	// ⚠️ **依赖 SQL 的 AND 优先级高于 OR** —— 解析器优先级(|| 最低,&& 其次)
	// 与 SQL 完全一致,所以比较项不需要逐个加括号;
	// 关键保证是**出口整体包一层括号**,AND 进外部条件(deleted_at IS NULL 之类)不会结合错。
	sql, args := mustCompile(t, `status = 'a' || status = 'b' && amount > 1`)
	// SQL 解析:`a OR (b AND c)` —— 与解析器一致
	assert.Equal(t, "(`f_status` = ? OR `f_status` = ? AND `f_amount` > ?)", sql)
	assert.Len(t, args, 3)

	// 括号改变结合:整体仍包一层
	sql, _ = mustCompile(t, `(status = 'a' || status = 'b') && amount > 1`)
	assert.Equal(t, "((`f_status` = ? OR `f_status` = ?) AND `f_amount` > ?)", sql)
}

func TestCompile_IN与包含与NULL(t *testing.T) {
	sql, args := mustCompile(t, `status ?= ('a','b','c')`)
	assert.Equal(t, "(`f_status` IN (?, ?, ?))", sql)
	assert.Len(t, args, 3)

	// 单值 IN 也允许(等价于 =,但走 IN 通道)
	sql, args = mustCompile(t, `status ?= 'a'`)
	assert.Equal(t, "(`f_status` IN (?))", sql)
	assert.Len(t, args, 1)

	sql, args = mustCompile(t, `title ~ '验收'`)
	assert.Equal(t, "(COALESCE(`f_title`, '') LIKE ?)", sql)
	assert.Equal(t, []any{"%验收%"}, args)

	sql, args = mustCompile(t, `title !~ '废弃'`)
	assert.Contains(t, sql, "NOT LIKE")

	// = null / != null → IS NULL / IS NOT NULL,零参数
	sql, args = mustCompile(t, `due = null`)
	assert.Equal(t, "(`f_due` IS NULL)", sql)
	assert.Empty(t, args)

	sql, _ = mustCompile(t, `due != null`)
	assert.Equal(t, "(`f_due` IS NOT NULL)", sql)
}

func TestCompile_类型与白名单错误(t *testing.T) {
	// 整数字段拿字符串
	_, _, err := Compile(`amount = 'abc'`, lookup)
	assert.ErrorContains(t, err, "整数")
	// 布尔字段拿别的
	_, _, err = Compile(`flag = 'maybe'`, lookup)
	assert.ErrorContains(t, err, "true / false")
	// ~ 用在非文本字段上
	_, _, err = Compile(`amount ~ '1'`, lookup)
	assert.ErrorContains(t, err, "只能用在文本类字段")
	// multiselect / json 不开放按值筛选(JSON 文本上做等于语义是错的)
	_, _, err = Compile(`tags = 'a'`, lookup)
	assert.ErrorContains(t, err, "不支持")
	_, _, err = Compile(`extra = 'x'`, lookup)
	assert.ErrorContains(t, err, "不支持")
	// 字段不存在 —— 必须报错,静默忽略会返回全表
	_, _, err = Compile(`staus = 'a'`, lookup)
	assert.ErrorContains(t, err, "不存在")
	// null 用在 > 上
	_, _, err = Compile(`amount > null`, lookup)
	assert.ErrorContains(t, err, "null 只能跟")
}

func TestCompile_字符串转义与注入面(t *testing.T) {
	// 单引号双写转义 —— 值里有引号也不破坏 SQL
	sql, args := mustCompile(t, `title = 'it''s ok'`)
	assert.Equal(t, "(`f_title` = ?)", sql)
	assert.Equal(t, []any{"it's ok"}, args)

	// ⚠️ **注入面测试:值里有引号、分号、注释,全部进参数不进 SQL** ——
	// 编译结果里不允许出现值本身的任何片段
	sql, args = mustCompile(t, `title = 'x''; DROP TABLE dyn_x; --'`)
	assert.Equal(t, "(`f_title` = ?)", sql)
	assert.Equal(t, []any{"x'; DROP TABLE dyn_x; --"}, args)
	assert.NotContains(t, sql, "DROP")
	_ = args
}

func TestCompile_体积上限(t *testing.T) {
	// 超长
	long := "status = 'a'"
	for len(long) < MaxExprLength+10 {
		long += " && status = 'a'"
	}
	_, _, err := Compile(long, lookup)
	assert.ErrorContains(t, err, "太长")

	// 比较项太多
	expr := "status = 'a'"
	for i := 0; i < MaxNodes; i++ {
		expr += " && amount > 1"
	}
	_, _, err = Compile(expr, lookup)
	assert.ErrorContains(t, err, "太复杂")
}

func TestCompile_垃圾尾部与空表达式(t *testing.T) {
	// 尾部没吃干净
	_, _, err := Compile(`status = 'a' 垃圾`, lookup)
	assert.ErrorContains(t, err, "无法理解")
	// 空
	_, _, err = Compile("", lookup)
	assert.Error(t, err)
	// 缺右括号
	_, _, err = Compile(`(status = 'a'`, lookup)
	assert.ErrorContains(t, err, "右括号")
	// 缺运算符
	_, _, err = Compile(`status 'a'`, lookup)
	assert.ErrorContains(t, err, "运算符")
}

func TestCompile_负数与系统列(t *testing.T) {
	// 负数字面量
	sql, args := mustCompile(t, `amount > -5`)
	assert.Equal(t, "(`f_amount` > ?)", sql)
	assert.Equal(t, []any{int64(-5)}, args)
}
