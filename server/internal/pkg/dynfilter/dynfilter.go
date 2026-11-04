// Package dynfilter 记录筛选表达式 → 参数化 SQL(M4,对标 PocketBase 的 fexpr)
//
// # 语法(与 PocketBase 同形状,子集)
//
//	status = 'active' && amount > 1000
//	status ?= ('active','pending')          -- 多值 IN,命中任意一个即入选
//	title ~ '验收'                          -- 包含(LIKE %值%)
//	remark !~ '废弃'
//	amount = null                           -- IS NULL(没填的行)
//	(a = 1 || b = 2) && c != null
//
// 运算符:= != > >= < <= ~ !~ ?=;逻辑:&& || ( )。
//
// # 安全边界(这条注释比代码重要)
//
// ⚠️ **值一律占位符化,字段一律白名单解析** ——
// 表达式里唯一能进 SQL 文本的是**列名**,而列名只能来自
// Resolver(服务层用「在用字段 + 系统列」喂进来),
// 用户写的字符串/数字全部走 ? 参数 —— 注入面从结构上不存在,
// 不靠"过滤关键字"那种必然漏的补丁。
//
// ⚠️ **字段不存在直接报错,不静默忽略** ——
// 用户把 status 拼成了 staus,静默忽略会返回"全表数据",
// 调用方以为筛选生效了;报错才能当场发现。
package dynfilter

import (
	"fmt"
	"strconv"
	"strings"

	"rapid-admin/internal/pkg/dynx"
)

// Field 参与筛选的字段元数据(服务层从「在用字段」喂进来)
type Field struct {
	// Column 物理列名(用户列是 f_<key>;系统列传自身)
	Column string
	// Type 字段类型(决定值怎么解析、运算符开放哪些)
	Type dynx.Type
}

// Limits 表达式的体积上限
//
// ⚠️ **没有上限的递归下降解析器就是一条 DoS 通道** ——
// 一条两万字符、一千层括号的表达式能把解析栈打爆;
// 筛选表达式是给人手写的,超过这些上限的都是别有用心的。
const (
	MaxExprLength = 2000
	MaxNodes      = 100
)

// nodeCount 编译期计数器(超过 MaxNodes 报错)
type nodeCount struct{ n int }

func (c *nodeCount) bump() error {
	c.n++
	if c.n > MaxNodes {
		return fmt.Errorf("表达式太复杂(超过 %d 个比较项)", MaxNodes)
	}
	return nil
}

// Compile 把筛选表达式编译成 (WHERE 片段, 参数)
//
// ⚠️ **返回的片段整体带括号** ——
// 调用方直接 AND 进既有条件;不带括号的话 `a=1 || b=2` 会
// 和外面的 `deleted_at IS NULL` 结合错(AND 优先级高于 ||)。
func Compile(expr string, lookup func(key string) (Field, bool)) (string, []any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil, fmt.Errorf("筛选表达式为空")
	}
	if len(expr) > MaxExprLength {
		return "", nil, fmt.Errorf("筛选表达式太长(上限 %d 字符)", MaxExprLength)
	}
	p := &parser{src: expr, lookup: lookup, cnt: &nodeCount{}}
	sql, args, err := p.parseExpr()
	if err != nil {
		return "", nil, err
	}
	// ⚠️ **表达式必须吃干净** —— `a = 1 别的垃圾` 不能静默忽略尾部
	p.ws()
	if p.pos < len(p.src) {
		return "", nil, fmt.Errorf("筛选表达式在 %d 字符附近有无法理解的内容", p.pos+1)
	}
	return "(" + sql + ")", args, nil
}

// ============================================================================
// 解析器(递归下降;|| 优先级最低,&& 其次,括号最高)
// ============================================================================

type parser struct {
	src    string
	pos    int
	lookup func(key string) (Field, bool)
	cnt    *nodeCount
}

func (p *parser) ws() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

// parseExpr or 层
func (p *parser) parseExpr() (string, []any, error) {
	left, args, err := p.parseAnd()
	if err != nil {
		return "", nil, err
	}
	for {
		p.ws()
		if !p.eat("||") {
			return left, args, nil
		}
		right, rargs, err := p.parseAnd()
		if err != nil {
			return "", nil, err
		}
		left = left + " OR " + right
		args = append(args, rargs...)
	}
}

func (p *parser) parseAnd() (string, []any, error) {
	left, args, err := p.parseAtom()
	if err != nil {
		return "", nil, err
	}
	for {
		p.ws()
		if !p.eat("&&") {
			return left, args, nil
		}
		right, rargs, err := p.parseAtom()
		if err != nil {
			return "", nil, err
		}
		left = left + " AND " + right
		args = append(args, rargs...)
	}
}

// parseAtom 括号 / 单个比较
//
// ⚠️ **括号分组必须保留在产物里** ——
// `(a || b) && c` 丢掉括号的话,SQL 会按自己的优先级重新解析成
// `a || (b && c)`,语义悄悄变了;解析器的优先级只在"没写括号"时生效。
func (p *parser) parseAtom() (string, []any, error) {
	p.ws()
	if p.eat("(") {
		inner, args, err := p.parseExpr()
		if err != nil {
			return "", nil, err
		}
		p.ws()
		if !p.eat(")") {
			return "", nil, fmt.Errorf("筛选表达式缺少右括号")
		}
		return "(" + inner + ")", args, nil
	}
	return p.parseComparison()
}

// parseComparison 左操作数(字段)+ 运算符 + 右操作数(字面量或字面量列表)
func (p *parser) parseComparison() (string, []any, error) {
	if err := p.cnt.bump(); err != nil {
		return "", nil, err
	}
	p.ws()
	key, ok := p.ident()
	if !ok {
		return "", nil, fmt.Errorf("筛选表达式这里应该是一个字段名(%d 字符附近)", p.pos+1)
	}
	f, known := p.lookup(key)
	if !known {
		return "", nil, fmt.Errorf("字段 %s 不存在", key)
	}
	p.ws()
	op, ok := p.operator()
	if !ok {
		return "", nil, fmt.Errorf("字段 %s 后面缺少比较运算符", key)
	}

	// null:只对 = / != 有意义(= null 即 IS NULL)
	p.ws()
	if p.nullKeyword() {
		switch op {
		case "=":
			return "`" + f.Column + "` IS NULL", nil, nil
		case "!=":
			return "`" + f.Column + "` IS NOT NULL", nil, nil
		default:
			return "", nil, fmt.Errorf("null 只能跟 = 或 != 一起用")
		}
	}

	// ?= 多值 IN:右侧可以是括号列表或单个字面量
	if op == "?=" {
		return p.compileIn(f)
	}

	val, err := p.literal()
	if err != nil {
		return "", nil, err
	}
	return compileOp(f, op, val)
}

// compileOp 单值比较 → SQL
//
// ⚠️ **值按字段类型解析成真类型** ——
// 理由同 M3 的 filterArg:字符串比 int 在两个方言下的隐式转换行为不一致,
// 既吃索引又可能比出错的结果。
func compileOp(f Field, op string, lit literal) (string, []any, error) {
	switch op {
	case "~", "!~":
		// 包含匹配:只对文本类开放 —— 在数字/日期上 LIKE 没有意义
		switch f.Type {
		case dynx.TypeText, dynx.TypeLongText, dynx.TypeSelect:
			// ok
		default:
			return "", nil, fmt.Errorf("运算符 %s 只能用在文本类字段上", op)
		}
		v, err := lit.asString()
		if err != nil {
			return "", nil, err
		}
		// ⚠️ **不做转义,% 和 _ 原样透传** ——
		// 高级用户可以自己写 % 前缀匹配;文档里写清楚,这比方言各异的
		// ESCAPE 子句(MySQL 要 '\\'、SQLite 要 '\')可靠。
		like := "%" + v + "%"
		// ⚠️ 不带括号 —— Compile 出口会整体包一层,这里再包就成双重括号了
		if op == "!~" {
			return "COALESCE(`" + f.Column + "`, '') NOT LIKE ?", []any{like}, nil
		}
		return "COALESCE(`" + f.Column + "`, '') LIKE ?", []any{like}, nil
	}

	// 数字/布尔/文本 → 参数化比较
	arg, err := typedValue(f, lit)
	if err != nil {
		return "", nil, err
	}
	col := "`" + f.Column + "`"
	switch op {
	case "=":
		return col + " = ?", []any{arg}, nil
	case "!=":
		// ⚠️ **!= 对 NULL 行永远不成立(SQL 三值逻辑)** ——
		// `amount != 100` 拿不到"没填金额"的行,想拿它们写 `amount != 100 || amount = null`。
		// 这里不加 OR col IS NULL:那会违背直觉("我说了 != 就是 !="),
		// 把语义写进文档,让用户显式选择。
		return col + " != ?", []any{arg}, nil
	case ">":
		return col + " > ?", []any{arg}, nil
	case ">=":
		return col + " >= ?", []any{arg}, nil
	case "<":
		return col + " < ?", []any{arg}, nil
	case "<=":
		return col + " <= ?", []any{arg}, nil
	default:
		return "", nil, fmt.Errorf("不支持的运算符 %s", op)
	}
}

// compileIn ?= 多值 → IN 列表
func (p *parser) compileIn(f Field) (string, []any, error) {
	p.ws()
	var lits []literal
	if p.eat("(") {
		for {
			p.ws()
			if p.eat(")") {
				break
			}
			lit, err := p.literal()
			if err != nil {
				return "", nil, err
			}
			lits = append(lits, lit)
			p.ws()
			if !p.eat(",") && !strings.HasPrefix(p.src[p.pos:], ")") {
				return "", nil, fmt.Errorf("IN 列表里缺少逗号")
			}
		}
	} else {
		lit, err := p.literal()
		if err != nil {
			return "", nil, err
		}
		lits = append(lits, lit)
	}
	if len(lits) == 0 {
		return "", nil, fmt.Errorf("IN 列表不能为空")
	}
	if len(lits) > 50 {
		return "", nil, fmt.Errorf("IN 列表最多 50 个值")
	}
	args := make([]any, 0, len(lits))
	for _, lit := range lits {
		arg, err := typedValue(f, lit)
		if err != nil {
			return "", nil, err
		}
		args = append(args, arg)
	}
	ph := strings.TrimSuffix(strings.Repeat("?, ", len(lits)), ", ")
	return "`" + f.Column + "` IN (" + ph + ")", args, nil
}

// typedValue 按字段类型把字面量解析成进 SQL 的真类型
func typedValue(f Field, lit literal) (any, error) {
	switch f.Type {
	// relation 落库是目标记录 id,与 int 同型比较(数字,不是文本)
	case dynx.TypeInt, dynx.TypeRelation:
		n, ok := lit.asInt()
		if !ok {
			return nil, fmt.Errorf("字段需要整数值,拿到的是 %s", lit.raw)
		}
		return n, nil
	case dynx.TypeNumber:
		fl, ok := lit.asNumber()
		if !ok {
			return nil, fmt.Errorf("字段需要数字值,拿到的是 %s", lit.raw)
		}
		return fl, nil
	case dynx.TypeBool:
		s, err := lit.asString()
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(s) {
		case "true", "1":
			return int64(1), nil
		case "false", "0":
			return int64(0), nil
		default:
			return nil, fmt.Errorf("布尔字段需要 true / false,拿到的是 %q", s)
		}
	case dynx.TypeDate, dynx.TypeDateTime, dynx.TypeText, dynx.TypeLongText, dynx.TypeSelect:
		// 文本/时间:字符串原样进参数(时间格式对不对由比较结果说话)
		s, err := lit.asString()
		if err != nil {
			return nil, err
		}
		return s, nil
	default:
		// multiselect(JSON 文本)/ json / file —— 按"值等于"筛选语义是错的
		return nil, fmt.Errorf("该字段类型不支持按值筛选")
	}
}

// ============================================================================
// 词法
// ============================================================================

// literal 一个字面量(带原文,报错时给上下文)
type literal struct {
	kind int // 0=string 1=number 2=bool
	s    string
	raw  string
}

func (l literal) asString() (string, error) {
	if l.kind != 0 {
		return "", fmt.Errorf("这里需要字符串字面量,拿到的是 %s", l.raw)
	}
	return l.s, nil
}

func (l literal) asInt() (int64, bool) {
	if l.kind != 1 {
		return 0, false
	}
	n, err := strconv.ParseInt(l.s, 10, 64)
	return n, err == nil
}

func (l literal) asNumber() (float64, bool) {
	if l.kind != 1 {
		return 0, false
	}
	fl, err := strconv.ParseFloat(l.s, 64)
	return fl, err == nil
}

// ident 读一个标识符(字段名)
func (p *parser) ident() (string, bool) {
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return "", false
	}
	return p.src[start:p.pos], true
}

// operator 读比较运算符(长的优先:!~ / >= 优先于 ! / >)
func (p *parser) operator() (string, bool) {
	for _, op := range []string{"?=", "!~", "!=", ">=", "<=", "=", ">", "<", "~"} {
		if p.eat(op) {
			return op, true
		}
	}
	return "", false
}

// nullKeyword 当前位置是不是 null 关键字
func (p *parser) nullKeyword() bool {
	if strings.HasPrefix(strings.ToLower(p.src[p.pos:]), "null") {
		// null 后面必须跟边界(空格 / 结尾 / 括号 / 逻辑符),否则是字段名的一部分
		rest := p.src[p.pos+4:]
		if rest == "" || strings.ContainsRune(" \t\n\r(),&|", rune(rest[0])) {
			p.pos += 4
			return true
		}
	}
	return false
}

// literal 读一个字面量(字符串 / 数字 / 布尔)
func (p *parser) literal() (literal, error) {
	p.ws()
	if p.pos >= len(p.src) {
		return literal{}, fmt.Errorf("筛选表达式在这里缺一个值")
	}
	c := p.src[p.pos]
	// 字符串:单引号或双引号,双写转义
	if c == '\'' || c == '"' {
		quote := c
		p.pos++
		var sb strings.Builder
		for {
			if p.pos >= len(p.src) {
				return literal{}, fmt.Errorf("字符串没有闭合")
			}
			ch := p.src[p.pos]
			if ch == quote {
				// 双写 = 转义(PB 同款)
				if p.pos+1 < len(p.src) && p.src[p.pos+1] == quote {
					sb.WriteByte(quote)
					p.pos += 2
					continue
				}
				p.pos++
				return literal{kind: 0, s: sb.String(), raw: p.src[:p.pos]}, nil
			}
			sb.WriteByte(ch)
			p.pos++
		}
	}
	// 布尔
	if strings.HasPrefix(strings.ToLower(p.src[p.pos:]), "true") {
		p.pos += 4
		return literal{kind: 2, s: "true", raw: "true"}, nil
	}
	if strings.HasPrefix(strings.ToLower(p.src[p.pos:]), "false") {
		p.pos += 5
		return literal{kind: 2, s: "false", raw: "false"}, nil
	}
	// 数字
	start := p.pos
	if c == '-' || c == '+' {
		p.pos++
	}
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if (ch >= '0' && ch <= '9') || ch == '.' {
			p.pos++
			continue
		}
		break
	}
	if p.pos == start {
		return literal{}, fmt.Errorf("筛选表达式在这里需要一个值(%d 字符附近)", p.pos+1)
	}
	return literal{kind: 1, s: p.src[start:p.pos], raw: p.src[start:p.pos]}, nil
}

// eat 前缀匹配并前进(只在运算符/括号上用,字符串字面量不走这里)
func (p *parser) eat(tok string) bool {
	if strings.HasPrefix(p.src[p.pos:], tok) {
		p.pos += len(tok)
		return true
	}
	return false
}
