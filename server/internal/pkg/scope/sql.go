// sql.go —— 把 Scope 翻译成**纯 SQL** 片段 —— M159(2026-11-02)
//
// # 为什么需要这个出口
//
// apply.go 的 Apply 只吐 **GORM 链式**(tx.Where(...)),
// 而动态业务集合(dyn)的数据面是**裸 SQL** ——
// 它自己拼 "WHERE a = ? AND b = ?" 再 db.Raw(sql, args...)。
//
// 两套写法接不上 ⇒ 数据范围这层对 dyn 的数据面**完全不生效**
// (清单 §1.13 #3 记的就是这条)。
//
// ⇒ 这里给一个**不依赖 GORM** 的出口:
// 返回一段 SQL 片段 + 一组参数,调用方自己拼进 WHERE。
//
// # ⚠️ **语义只有一份,不允许有两套实现**
//
// Apply(GORM)与 ApplySQL(裸 SQL)必须由**同一个核心函数**编译,
// 否则改了一处、另一处漂移 ⇒ 同一套规则在 GORM 查询与裸 SQL 查询上表现不同。
//
// 那比"没接"更糟:GORM 那条路过滤了,裸 SQL 那条路没过滤 ——
// 而且两条路都跑得好好的,没有任何报错,没有任何日志。
// 等发现的时候,已经有一批数据被越权看过了。
//
// # ⚠️ **返回空串 = "不过滤",不是"匹配不到任何行"**
//
// 两者都可能出现,调用方必须分得清:
//
//	""        ⇒ 没有任何限制性规则(不限行)
//	"1 = 0"   ⇒ 有限制性规则但一条都表达不出来(看不到任何行)
//
// 把这两种混为一谈的后果:把 "" 拼进 SQL ⇒ 语法错误(响亮,可接受);
// 把 "1 = 0" 当成 "" ⇒ **数据越权**(安静,不可接受)。

package scope

import (
	"fmt"
	"strings"
)

// ApplySQL 把作用域翻译成一段 WHERE 条件 + 一组参数
//
// ⚠️ **返回空串表示"不过滤"** —— 调用方不要把空串拼进 SQL。
//
// ⚠️ **参数与占位符一一对应,顺序不能错** ——
// 调用方把它拼进一条更长的 SQL 时,必须把 args 放在**对应的位置**:
// 前面已经有别的参数时,不能直接 append 到末尾。
//
// ⚠️ **片段自带外层括号** —— "(A OR B)"。
// 调用方直接 AND 在 WHERE 后面即可,不必自己加括号。
func ApplySQL(s *Scope, cols Columns) (frag string, args []any, err error) {
	if s == nil {
		// ⚠️ **空指针是编程错误,不是"没有限制"** ——
		// 静默当成"不过滤"就是**数据越权**。
		// 这里必须响亮失败。
		return "", nil, fmt.Errorf("scope.ApplySQL: 作用域为空指针")
	}
	if s.All || !s.Restrictive {
		// 没有限制性规则 ⇒ 不过滤
		return "", nil, nil
	}

	parts := make([]string, 0, 4)
	args = make([]any, 0, 8)

	for _, dim := range []scopeDim{
		{name: "本项目", tmpl: cols.Project, ids: s.ProjectIDs},
		{name: "本部门", tmpl: cols.Dept, ids: s.DeptIDs},
		{name: "仅本人", tmpl: cols.Self, ids: s.SelfUserIDs},
	} {
		if len(dim.ids) == 0 {
			continue
		}
		expr, lerr := listExpr(dim.tmpl, dim.ids, dim.name)
		if lerr != nil {
			return "", nil, lerr
		}
		parts = append(parts, expr)
		for _, id := range dim.ids {
			args = append(args, id)
		}
	}

	for _, c := range s.Customs {
		if len(c.Values) == 0 {
			continue
		}
		// ⚠️ 自定义列名在 Resolve 时已校验过是合法标识符,
		// 这里再校验一次 —— ApplySQL 可能被外部直接调用(不经过 Resolve)。
		if cerr := ValidateCustomColumn(c.Column); cerr != nil {
			return "", nil, cerr
		}
		parts = append(parts, c.Column+" IN "+placeholders(len(c.Values)))
		for _, v := range c.Values {
			args = append(args, v)
		}
	}

	if len(parts) == 0 {
		// ⚠️ 有限制但一条都表达不出来 ⇒ 匹配不到任何行(**不是**不过滤)
		return "1 = 0", nil, nil
	}

	return "(" + strings.Join(parts, " OR ") + ")", args, nil
}

// scopeDim 一个维度的列模板与取值
type scopeDim struct {
	// name 维度名(错误信息里用)
	name string
	// tmpl 列模板(恰好一个 `?`)
	tmpl string
	// ids 该维度的取值
	ids []uint64
}

// listExpr 把列模板编译成一条带占位符的子句
//
// ⚠️ **模板里的 `?` 是"值列表"的占位符** ——
// 它会被换成 `(?, ?, ?)`(几个值几个占位符)。
//
// ⚠️ **模板必须恰好一个 `?`** ——
// 0 个:占位符没填上,SQL 会少条件;
// 2 个以上:参数个数对不上,SQL 会串位(而且不报错,只是查到错的数据)。
// ⇒ 这里直接报错,不猜调用方的意图。
func listExpr(tmpl string, ids []uint64, dim string) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		return "", fmt.Errorf("%s范围落不到这张表上:%w", dim,
			fmt.Errorf("没有配置这一维度的列(规则要求按这一维度过滤,但目标表上找不到对应列)"))
	}
	if n := strings.Count(tmpl, "?"); n != 1 {
		return "", fmt.Errorf("列模板 %q 里必须有且只有一个 ?(实际 %d 个)", tmpl, n)
	}
	if err := checkListPlaceholder(tmpl); err != nil {
		return "", err
	}
	return strings.Replace(tmpl, "?", placeholders(len(ids)), 1), nil
}

// checkListPlaceholder 校验模板里的 `?` 真的是"值列表"的占位符
//
// ⚠️ **必须紧跟在 IN 后面** ——
// 它会被换成 `(?, ?, ?)`(几个值几个占位符),
// 所以 `col = ?` 会编译成 `col = (?, ?)` —— 那不是合法 SQL,
// 而数据库报的错离现场很远("near ?": syntax error),管理员根本看不懂。
//
// ⇒ 在编译期就拒,给出能指到问题的话。
func checkListPlaceholder(tmpl string) error {
	i := strings.Index(tmpl, "?")
	prefix := strings.TrimSpace(tmpl[:i])
	if !strings.HasSuffix(strings.ToUpper(prefix), "IN") {
		return fmt.Errorf("列模板 %q 的 ? 必须紧跟在 IN 后面(它是值列表的占位符)", tmpl)
	}
	// ⚠️ **"IN" 前面不能是字母/数字/下划线** ——
	// 否则 "origin IN ?" 这种"词尾恰好是 in"会被误判成合法。
	if len(prefix) > 2 {
		c := prefix[len(prefix)-3]
		if c == '_' || (c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return fmt.Errorf("列模板 %q 的 ? 必须紧跟在 IN 后面(它是值列表的占位符)", tmpl)
		}
	}
	return nil
}

// placeholders n 个占位符
//
// ⚠️ **空列表给 `(NULL)` 而不是 `()`** ——
// `IN ()` 是语法错误;`(NULL)` 是合法 SQL 且**匹配不到任何行**。
// (与 GORM 绑定空切片时的行为一致 —— 见 gorm clause.Expr 的 AddVar。)
func placeholders(n int) string {
	if n <= 0 {
		return "(NULL)"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}
