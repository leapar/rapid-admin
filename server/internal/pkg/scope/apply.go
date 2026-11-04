// apply.go —— 把 Scope 翻译成 WHERE 条件
// M155(2026-11-02)
//
// # 为什么要单独一个文件
//
// scope.go 只管"规则 → 作用域"的语义,这里只管"作用域 → SQL"。
// 分开是因为**两边的失败方式完全不同**:
//   - scope.go 失败 = 配置/解析问题(列名非法、部门查不到)
//   - apply.go 失败 = **接线问题**(规则要求的列在目标表上不存在)
//
// 后者更危险 —— 它意味着"这条数据范围的规则在这张表上落不下去",
// 而落不下去时**必须响亮失败**,不能悄悄退化成"不过滤"。

package scope

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Columns 目标表上的作用域列
//
// ⚠️ **三个字段都是 SQL 模板,各带恰好一个 `?` 占位符** ——
// 占位符收什么由调用方定:项目/部门收 **id 列表**,归属人收 **单个用户 id**
// (实现上统一收列表,Self 传单元素切片)。
//
// 为什么不给"裸列名"而给模板:
// 用户与部门的归属关系**不一定是表上的一个列** ——
// 用户表 sys_user 的部门是**多对多**(sys_user_department),
// 没有 dept_id 列 ⇒ 只能写成子查询:
//
//	Dept: "id IN (SELECT user_id FROM sys_user_department WHERE dept_id IN ?)"
//
// ⚠️ **模板由框架代码提供,不是管理员填的** ——
// 管理员能填的只有"自定义范围"的列名(走 validColumnName 校验)。
// 自定义范围不走 Columns,见 Apply。
type Columns struct {
	// Project 本项目列,模板形如 "project_id IN ?"
	//
	// 空串 = 这张表没有项目维度 ⇒ "本项目"规则在这张表上落不下去 ⇒ Apply 报错。
	Project string
	// Dept 本部门列,模板形如 "dept_id IN ?" 或子查询形式
	Dept string
	// Self 归属人列,模板形如 "owner_id IN ?"
	//
	// ⚠️ **不支持 "owner_id = ?" 等等值形式** —— 占位符统一按"值列表"
	// 展开(几个值几个占位符),checkListPlaceholder 强制 ? 紧跟 IN,
	// 写 "= ?" 接线必报错。归属人收单个用户 id 就是传单元素切片,
	// 编译出来 "owner_id IN (?)" 语义等同等值(全仓 8 处接线均此形式)。
	Self string
}

// Apply 把作用域翻译成 WHERE 条件挂到 tx 上
//
// # 语义(与包注释的约定一一对应)
//
//   - 没有任何限制性规则(Restrictive=false)⇒ **原样返回 tx**,不过滤
//   - 有限制性规则但一条子句都表达不出来 ⇒ **匹配不到任何行**("1 = 0")
//   - 规则要求的列在目标表上不存在 ⇒ **报错**
//
// ⚠️ **第三条是这里最重要的一条** ——
// "规则要求的列不存在"有两种可能的处理:跳过那条子句,或者报错。
// 跳过在"或"组里会让可见集**变窄**(不是变宽),但那仍然是错的:
// 用户会莫名看不到自己该看的数据,而且**没有任何报错**。
// ⇒ 这里选报错:接线漏了当场炸,而不是上线后变成"某人看不到某批数据"的谜案。
//
// ⚠️ **有限制但表达不出来 ⇒ 匹配不到任何行,不是"不过滤"** ——
// 这是**安全方向**上的关键区别。
// 例:"本项目"规则 + 调用方一个项目都没有 ⇒ 他一条都看不到;
// 如果这里退化成"不过滤",他就看到了全部数据 —— 那是安全事故。
//
// ⚠️ **s 传指针** —— Scope 有四个切片,按值传就是 104 字节的拷贝
// (gocritic hugeParam 会报)。Apply 不修改它,只读。
//
// # ⚠️ **本函数只是 ApplySQL 的 GORM 外壳** ——
// 语义只有一份,编译在 sql.go 的 ApplySQL 里。
// 裸 SQL 的调用方(动态业务集合)走同一个编译函数,
// 免得"同一套规则在两条路上表现不同" ——
// 那比"没接"更糟:GORM 那条路过滤了,裸 SQL 那条路没过滤,
// 而且两条路都跑得好好的,没有任何报错。
func Apply(tx *gorm.DB, s *Scope, cols Columns) (*gorm.DB, error) {
	if tx == nil {
		return nil, fmt.Errorf("scope.Apply: tx 为 nil")
	}
	frag, args, err := ApplySQL(s, cols)
	if err != nil {
		return nil, err
	}
	if frag == "" {
		// 没有限制性规则 ⇒ 原样返回 tx,不过滤
		return tx, nil
	}
	return tx.Where(clause.Expr{SQL: frag, Vars: args}), nil
}

// Describe 人类可读的作用域描述(后台页面 / 审计日志用)
//
// ⚠️ **这是给人看的,不是给机器解析的** ——
// 别拿它做判断。它的价值是"让管理员在页面上看懂这条规则到底收窄了什么"。
//
// ⚠️ **指针接收者** —— 见 Apply 的注释(Scope 104 字节)。
func (s *Scope) Describe() string {
	if s.All {
		return "全部数据"
	}
	if !s.Restrictive {
		return "未配置(等于全部数据)"
	}
	parts := make([]string, 0, 4)
	if len(s.ProjectIDs) > 0 {
		parts = append(parts, fmt.Sprintf("本项目(%d 个)", len(s.ProjectIDs)))
	}
	if len(s.DeptIDs) > 0 {
		parts = append(parts, fmt.Sprintf("本部门(%d 个)", len(s.DeptIDs)))
	}
	if len(s.SelfUserIDs) > 0 {
		parts = append(parts, "仅本人")
	}
	for _, c := range s.Customs {
		parts = append(parts, fmt.Sprintf("%s ∈ 自定义集合(%d 个值)", c.Column, len(c.Values)))
	}
	if len(parts) == 0 {
		return "有限制,但当前解析不出任何可见范围(看不到任何数据)"
	}
	return strings.Join(parts, " 或 ")
}
