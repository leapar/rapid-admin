// Package scope —— 数据范围(行级权限)框架原语 —— M155(2026-11-02)
//
// # 这层解决什么问题
//
// 本框架的鉴权只到"能不能调这个接口"(RBAC + casbin)。
// 一旦接口放行,返回多少数据由各个业务 service 自己写 filter 决定,
// 于是同一条规则被抄了 N 遍,而且抄漏了没人知道:
//
//	asset_impl.go:65     if q.ProjectID > 0 { tx.Where("project_id = ?", ...) }
//	contract_impl.go:47  同一形状
//	delivery_impl.go     同一形状
//	activity_impl.go:49  同一形状
//
// 更要紧的是**语义是空的**:q.ProjectID 是调用方在 query 里自己填的,
// 不是"这个人有权看哪些项目"。
// RBAC 中间件那道闸(rbac.go:164)只在**传了** projectId 时才校验,
// 而 InProject 的语义是 projectID==0 ⇒ 放行(context.go:90)
// ⇒ 不传 projectId 就等于不看项目域 ⇒ 通过了鉴权,看到了不该看的数据。
//
// # 这一层给的是"规则",不是"过滤"
//
// 本包只做三件事:
//
//  1. 定义数据范围的类型(全部 / 本项目 / 本部门 / 本部门及下级 / 仅本人 / 自定义)
//  2. 把「角色上的规则 + 当前调用者」解析成一个具体的 Scope
//  3. 把 Scope 翻译成一段 WHERE 条件
//     (Apply 走 GORM 链式,ApplySQL 走裸 SQL —— 两者由同一个函数编译,语义不会漂移)
//
// **规则存在哪、谁去解析、怎么落到查询上,都不在本包** ——
// 本包不碰 DB、不导入 model、不导入 service(examples 漂移网的要求)。
// 落库与接线在 internal/service/scope.go。
//
// # 语义约定(改之前先读这一段)
//
//   - **多条规则之间是"或"** —— 角色同时勾了"本部门"和"仅本人",
//     那它能看���本部门的行 + 自己建的行。
//   - **"全部数据"一票放行** —— 规则里有 all 时,其余规则无意义。
//   - **有限制但一条都表达不出来 ⇒ 匹配不到任何行**,绝不能退化成"不过滤"。
//     (例:"本项目"规则 + 调用方一个项目都没有 ⇒ 他一条都看不到,
//     而不是"他能看到全部"。后者是安全事故。)
//   - **规则里点名的列在目标表上不存在 ⇒ 报错**,不静默丢弃那条子句。
//     丢一条子句在"或"组里会让可见集**变窄**(不是变宽),
//     但那同样是错的 —— 用户会莫名看不到自己该看的数据。
//
// # 业务域(module)从哪来
//
// 业务域的权威定义是 `rbac(module, action)` 的**第一个参数** ——
// 路由里写死的字面量,判定权限的那一层拿到的就是它。
// 本包用 module.go 的登记表把它记下来(路由层写,service 层读),
// **不从资源码反推** —— 反推是不确定的,详见 module.go 的注释。
package scope

import (
	"fmt"
	"sort"
	"strings"
)

// Type 数据范围类型
type Type string

const (
	// TypeAll 全部数据(不施加任何行级限制)
	TypeAll Type = "all"
	// TypeProject 本项目 —— 调用方身份所属的项目
	TypeProject Type = "project"
	// TypeDept 本部门 —— 调用方身份所属的部门
	TypeDept Type = "dept"
	// TypeDeptAndChild 本部门及下级部门
	TypeDeptAndChild Type = "dept_and_child"
	// TypeSelf 仅本人 —— 只有归属人是调用方自己的行
	TypeSelf Type = "self"
	// TypeCustom 自定义 —— 指定列的值落在给定集合里
	TypeCustom Type = "custom"
)

// allTypes 全部合法类型(顺序 = 前端下拉框顺序)
//
// ⚠️ **顺序不要随便改** —— 前端按这个顺序渲染下拉框,
// 改顺序等于改 UI 契约。
var allTypes = []Type{
	TypeAll,
	TypeProject,
	TypeDept,
	TypeDeptAndChild,
	TypeSelf,
	TypeCustom,
}

// AllTypes 全部合法类型(拷贝一份,调用方随便改)
func AllTypes() []Type {
	out := make([]Type, len(allTypes))
	copy(out, allTypes)
	return out
}

// ParseType 解析类型字符串(空串 = all)
//
// ⚠️ **空串按 all 处理,而不是报错** ——
// 数据库里存的是 VARCHAR,老数据/手工改库可能留空串;
// 而"没配范围"在语义上就是"全部",跟 all 等价。
func ParseType(s string) (Type, error) {
	t := Type(strings.TrimSpace(strings.ToLower(s)))
	if t == "" {
		return TypeAll, nil
	}
	for _, v := range allTypes {
		if v == t {
			return v, nil
		}
	}
	return "", fmt.Errorf("未知的数据范围类型 %q(合法值:%s)", s, typeListString())
}

// typeListString 合法类型列表(拼给错误信息看)
func typeListString() string {
	parts := make([]string, 0, len(allTypes))
	for _, t := range allTypes {
		parts = append(parts, string(t))
	}
	return strings.Join(parts, ", ")
}

// Rule 一条数据范围规则:某个角色在某个业务域上启用的一种范围
//
// ⚠️ **一条规则 = 一种类型** ——
// 同一角色同一业务域可以有**多行**(不同类型),它们之间是"或"。
// 落库见 model.SysDataScope(唯一键 role_id + module + scope_type)。
type Rule struct {
	// Type 范围类型
	Type Type
	// CustomColumn 自定义范围的列名(仅 TypeCustom)
	CustomColumn string
	// CustomValues 自定义范围允许的值(仅 TypeCustom)
	CustomValues []string
}

// CustomTerm 自定义范围的一组子句(列名 + 允许的值)
type CustomTerm struct {
	// Column 列名(必须是合法标识符,不许是表达式)
	Column string
	// Values 允许的值
	Values []string
}

// Actor 规则的适用对象(当前调用者)
type Actor struct {
	// UserID 调用者用户 id
	UserID uint64
	// ProjectIDs 调用者有权访问的项目 id 列表
	//
	// ⚠️ **空列表 = 一个项目都没有**(不是"全部")。
	// 框架里"空 = 全平台"是 identity.project_id=0 的语义,
	// 而 collectProjectIDs(auth_impl.go:288)**把 0 过滤掉了**
	// ⇒ 调用方拿到的 ProjectIDs 为空,只可能是"没有项目身份"。
	// ⇒ 这里不能沿用"空 = 全平台",否则"本项目"会退化成"看全部"。
	ProjectIDs []uint64
	// DeptIDs 调用者归属的部门 id 列表(可能多个 —— 用户可属于多个部门)
	//
	// 空列表 = 一个部门都没有(不是"全部部门")。
	// 与 ProjectIDs 同理:空就是空,不允许退化成"不过滤"。
	DeptIDs []uint64
	// DeptSubtree 给一组部门 id 返回「自己 + 所有下级」的 id 列表(并集)
	//
	// ⚠️ **由上层注入,本包不碰 DB** —— 框架原语不依赖数据库。
	// TypeDeptAndChild 需要它;为 nil 时该类型解析为空子句。
	//
	// ⚠️ **入参是列表不是单个 id** —— 用户可能同时属于多个部门,
	// "本部门及下级"要把这些部门各自的子树并起来,
	// 传单个 id 会漏掉其余部门的下级。
	DeptSubtree func(deptIDs []uint64) ([]uint64, error)
}

// Scope 解析后的作用域 —— 可以直接翻译成 WHERE 条件
//
// ⚠️ **All 与 Restrictive 是两件事,别混**:
//   - All:规则里明确写了"全部数据"
//   - Restrictive:存在至少一条**限制性**规则(非 all)
//
// 二者都不会同时为真(有 all 就直接短路了)。
// Restrictive=true 而所有子句都为空 ⇒ 匹配不到任何行(见包注释的语义约定)。
type Scope struct {
	// All 规则明确是"全部数据"
	All bool
	// Restrictive 存在限制性规则 ⇒ 必须过滤
	Restrictive bool
	// ProjectIDs 允许的项目 id
	ProjectIDs []uint64
	// DeptIDs 允许的部门 id
	DeptIDs []uint64
	// SelfUserIDs 允许的归属人 id(一般只有一个)
	SelfUserIDs []uint64
	// Customs 自定义子句
	Customs []CustomTerm
}

// Empty 是否是一条**空的作用域**(没有任何子句)
//
// ⚠️ **指针接收者** —— Scope 有 104 字节,按值接收会被 gocritic hugeParam 报。
// 与 Describe 同理。
func (s *Scope) Empty() bool {
	return len(s.ProjectIDs) == 0 && len(s.DeptIDs) == 0 &&
		len(s.SelfUserIDs) == 0 && len(s.Customs) == 0
}

// Resolve 把一组规则解析成作用域
//
// ⚠️ **多条规则之间是"或"** —— 各类型的子句合并成一个 OR 组。
//   - 规则里出现 TypeAll ⇒ 立刻返回"全部数据"(一票放行)
//   - "本项目"在调用方没有项目时**不产生任何子句**,
//     但 Restrictive 仍为 true ⇒ 最终匹配不到任何行(不是"看到全部")
//   - "本部门"同理:调用方没有部门 ⇒ 不产生子句
//   - "仅本人"在 UserID=0 时不产生子句
//
// ⚠️ **自定义列名在这里校验** —— 非法列名(不是标识符)在解析期就报错,
// 不留到 Apply 阶段(那时错误信息已经离上下文太远)。
func Resolve(rules []Rule, actor Actor) (Scope, error) {
	s := Scope{}
	for _, r := range rules {
		if r.Type == TypeAll {
			// 一票放行 —— 后面几条规则无意义
			return Scope{All: true}, nil
		}
		if err := s.applyRule(r, actor); err != nil {
			return Scope{}, err
		}
	}
	s.dedup()
	return s, nil
}

// applyRule 把一条规则累加到作用域上
//
// ⚠️ **拆出来是为了圈复杂度** —— Resolve 里塞了六种类型的分支,
// 加一种类型就 +2,迟早越过仓库的复杂度上限(15)。
// 拆开之后每种类型只负责自己那一件事。
func (s *Scope) applyRule(r Rule, actor Actor) error {
	switch r.Type {
	case TypeProject:
		s.Restrictive = true
		s.ProjectIDs = append(s.ProjectIDs, actor.ProjectIDs...)
	case TypeDept:
		s.Restrictive = true
		s.DeptIDs = append(s.DeptIDs, actor.DeptIDs...)
	case TypeDeptAndChild:
		s.Restrictive = true
		if len(actor.DeptIDs) == 0 || actor.DeptSubtree == nil {
			return nil
		}
		ids, err := actor.DeptSubtree(actor.DeptIDs)
		if err != nil {
			return fmt.Errorf("解析部门下级失败(dept=%v):%w", actor.DeptIDs, err)
		}
		s.DeptIDs = append(s.DeptIDs, ids...)
	case TypeSelf:
		s.Restrictive = true
		// ⚠️ **UserID=0 不产生子句** ——
		// append 一个 0 进去会变成"归属人是 0 号用户",那是真的错数据。
		if actor.UserID != 0 {
			s.SelfUserIDs = append(s.SelfUserIDs, actor.UserID)
		}
	case TypeCustom:
		s.Restrictive = true
		col := strings.TrimSpace(r.CustomColumn)
		if err := ValidateCustomColumn(col); err != nil {
			return err
		}
		vals := make([]string, 0, len(r.CustomValues))
		for _, v := range r.CustomValues {
			if v == "" {
				continue
			}
			vals = append(vals, v)
		}
		if len(vals) == 0 {
			// 自定义规则没给值 ⇒ 不产生子句(但 Restrictive 仍为 true)
			return nil
		}
		s.Customs = append(s.Customs, CustomTerm{Column: col, Values: vals})
	default:
		return fmt.Errorf("未知的数据范围类型 %q", r.Type)
	}
	return nil
}

// dedup 去掉重复的 id 与自定义值(同一角色可能配出重复的行)
func (s *Scope) dedup() {
	s.ProjectIDs = dedupUint64(s.ProjectIDs)
	s.DeptIDs = dedupUint64(s.DeptIDs)
	s.SelfUserIDs = dedupUint64(s.SelfUserIDs)
	for i := range s.Customs {
		s.Customs[i].Values = dedupString(s.Customs[i].Values)
	}
}

// dedupUint64 去重并排序(排序让生成的 SQL 稳定,便于比对与缓存)
func dedupUint64(in []uint64) []uint64 {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[uint64]struct{}, len(in))
	out := make([]uint64, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// dedupString 去重并排序
func dedupString(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// ValidateCustomColumn 校验自定义列名是合法标识符
//
// ⚠️ **这是防 SQL 注入的一道闸** ——
// 自定义列名由管理员在页面上填,拼进 SQL 时必须是**裸列名**,
// 不能是表达式、不能带括号或引号。
//
// ⚠️ **保存规则时也要调它** —— 不能只在解析时校验:
// 解析发生在"用这条规则的人"身上,保存发生在"配这条规则的人"身上。
// 前者校验 ⇒ 配错的人当场看不到报错,只有受害者(用规则的人)查询时才炸,
// 而且错误信息离现场十万八千里。
func ValidateCustomColumn(col string) error {
	if col == "" {
		return fmt.Errorf("自定义数据范围必须指定列名")
	}
	if len(col) > 64 {
		return fmt.Errorf("自定义数据范围的列名过长(%d 字符)", len(col))
	}
	for i, r := range col {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			// 合法
		case r >= '0' && r <= '9':
			if i == 0 {
				return fmt.Errorf("自定义数据范围的列名 %q 不能以数字开头", col)
			}
		default:
			return fmt.Errorf("自定义数据范围的列名 %q 含非法字符", col)
		}
	}
	return nil
}
