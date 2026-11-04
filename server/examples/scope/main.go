// scope —— 数据范围(行级权限)原语
//
// ## 这个包解决什么问题
//
// 鉴权只回答"这个角色能不能调这个接口"。
// 一旦接口放行,返回多少数据是每个业务 service 自己写 filter 决定的 ——
// 同一条规则被抄了 N 遍,抄漏了没人知道,而且**语义是空的**
// (过滤条件是调用方自己在 query 里填的,不是"这个人有权看哪些数据")。
//
// 本包把"数据范围"变成一条**可配置、可解析、可翻译成 WHERE 的规则**:
//
//   - Type:   全部 / 本项目 / 本部门 / 本部门及下级 / 仅本人 / 自定义
//   - Rule:   某个角色在某个业务域上启用的一种范围
//   - Actor:  当前调用者(有哪些项目、属于哪个部门)
//   - Scope:  规则 + 调用者解析出来的具体作用域
//   - Apply:  把 Scope 翻译成 WHERE 条件(GORM 链式)
//   - ApplySQL: 把 Scope 翻译成**纯 SQL 片段 + 参数**(给"自己拼 SQL"的地方)
//
// ## 关键点
//
//   - **多条规则之间是"或"** —— 角色同时勾了"本部门"和"仅本人",
//     它能看本部门的行 + 自己建的行。
//   - **"全部数据"一票放行** —— 有 all 时其余规则无意义。
//   - **有限制但一条都表达不出来 ⇒ 匹配不到任何行**,不是"不过滤"。
//     (例:"本项目"规则 + 调用方一个项目都没有 ⇒ 他一条都看不到;
//     若退化成"不过滤",他就看到了全部数据 —— 那是安全事故。)
//   - **规则要求的列在目标表上不存在 ⇒ 报错**,不静默丢弃那条子句。
//   - **两个出口由同一个函数编译** —— Apply 与 ApplySQL 的语义不会漂移。
//     (漂移的后果:GORM 那条路过滤了、裸 SQL 那条路没过滤,而两边都不报错。)
//
// ## 跑法
//
//	go run ./examples/scope
package main

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/scope"
)

// row 示例表的一行
type row struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement:false"`
	ProjectID uint64
	DeptID    uint64
	OwnerID   uint64
	Name      string
}

func (row) TableName() string { return "demo_row" }

func main() {
	// ── 一、类型 ────────────────────────────────────────────────
	fmt.Println("=== 数据范围类型 ===")
	for _, t := range scope.AllTypes() {
		fmt.Printf("  %-16s %s\n", t, typeLabel(t))
	}

	// ── 二、解析规则 ────────────────────────────────────────────
	//
	// 调用方:属于项目 10、部门 100、本人 id=1
	actor := scope.Actor{
		UserID:     1,
		ProjectIDs: []uint64{10},
		DeptIDs:    []uint64{100},
		// 部门下级由上层注入(本包不碰数据库)
		DeptSubtree: func(ids []uint64) ([]uint64, error) {
			return append([]uint64{}, append(ids, 101)...), nil
		},
	}

	fmt.Println("\n=== 规则解析 ===")
	for _, typ := range []scope.Type{
		scope.TypeAll, scope.TypeProject, scope.TypeDept,
		scope.TypeDeptAndChild, scope.TypeSelf,
	} {
		s, err := scope.Resolve([]scope.Rule{{Type: typ}}, actor)
		if err != nil {
			fmt.Printf("  %-16s 解析失败: %v\n", typ, err)
			continue
		}
		fmt.Printf("  %-16s %s\n", typ, s.Describe())
	}

	// "本部门" **或** "仅本人" ⇒ 两条子句合成一个 OR 组
	s, err := scope.Resolve([]scope.Rule{
		{Type: scope.TypeDept},
		{Type: scope.TypeSelf},
	}, actor)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  %-16s %s\n", "dept 或 self", s.Describe())

	// ── 三、落到查询上 ──────────────────────────────────────────
	fmt.Println("\n=== 真库过滤 ===")
	db := mustDB()
	seed(db)

	cols := scope.Columns{
		Project: "project_id IN ?",
		Dept:    "dept_id IN ?",
		Self:    "owner_id IN ?",
	}

	show := func(title string, rules []scope.Rule) {
		sc, err := scope.Resolve(rules, actor)
		if err != nil {
			fmt.Printf("  %-22s 解析失败: %v\n", title, err)
			return
		}
		tx, err := scope.Apply(db.Model(&row{}), &sc, cols)
		if err != nil {
			fmt.Printf("  %-22s 落不下去: %v\n", title, err)
			return
		}
		var names []string
		if err := tx.Order("id").Pluck("name", &names).Error; err != nil {
			panic(err)
		}
		fmt.Printf("  %-22s %s\n", title, names)
	}

	show("全部", []scope.Rule{{Type: scope.TypeAll}})
	show("本项目(10)", []scope.Rule{{Type: scope.TypeProject}})
	show("本部门(100)", []scope.Rule{{Type: scope.TypeDept}})
	show("本部门及下级", []scope.Rule{{Type: scope.TypeDeptAndChild}})
	show("仅本人(1)", []scope.Rule{{Type: scope.TypeSelf}})
	show("本部门 或 仅本人", []scope.Rule{{Type: scope.TypeDept}, {Type: scope.TypeSelf}})

	// ── 四、落不下去时报错 ──────────────────────────────────────
	fmt.Println("\n=== 目标表没有这一维度时报错(而不是静默放过) ===")
	sc, _ := scope.Resolve([]scope.Rule{{Type: scope.TypeProject}}, actor)
	_, err = scope.Apply(db.Model(&row{}), &sc, scope.Columns{})
	fmt.Printf("  %v\n", err)

	// ── 五、纯 SQL 出口(给"自己拼 SQL"的地方用) ────────────────
	//
	// ⚠️ **GORM 链式那条路接不上裸 SQL** ——
	// 动态业务集合(dyn)的数据面是自己拼 "WHERE a = ? AND b = ?" 再 db.Raw(...),
	// 而 scope.Apply 只吐 tx.Where(...) ⇒ 数据范围这层对它是**完全不生效**的。
	//
	// ⇒ ApplySQL 给一段 SQL 片段 + 一组参数,调用方自己拼。
	// **语义与 Apply 完全一致** —— 两者由同一个函数编译,不会漂移。
	fmt.Println("\n=== 纯 SQL 出口(自己拼 WHERE 的场合) ===")
	frag, args, err := scope.ApplySQL(&sc, cols)
	if err != nil {
		panic(err)
	}
	fmt.Printf("  片段: %s\n", frag)
	fmt.Printf("  参数: %v\n", args)

	var rows []row
	sql := "SELECT * FROM demo_row WHERE " + frag + " ORDER BY id"
	if err := db.Raw(sql, args...).Scan(&rows).Error; err != nil {
		panic(err)
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Name)
	}
	fmt.Printf("  查到的行: %v\n", names)

	// ⚠️ **返回空串 = 不过滤;"1 = 0" = 看不到任何行** ——
	// 两者都可能出现,调用方必须分得清。把 "1 = 0" 当成空串 = **数据越权**。
	fmt.Println("\n=== 空串(不过滤)与 1 = 0(看不到任何行)的区别 ===")
	noRule, _ := scope.Resolve(nil, actor)
	f1, _, _ := scope.ApplySQL(&noRule, cols)
	noProject, _ := scope.Resolve([]scope.Rule{{Type: scope.TypeProject}}, scope.Actor{UserID: 1})
	f2, _, _ := scope.ApplySQL(&noProject, cols)
	fmt.Printf("  没有限制性规则        ⇒ %q\n", f1)
	fmt.Printf("  有限制但解析不出子句  ⇒ %q\n", f2)
}

// typeLabel 类型的中文说明
func typeLabel(t scope.Type) string {
	switch t {
	case scope.TypeAll:
		return "全部数据"
	case scope.TypeProject:
		return "本项目"
	case scope.TypeDept:
		return "本部门"
	case scope.TypeDeptAndChild:
		return "本部门及下级"
	case scope.TypeSelf:
		return "仅本人"
	case scope.TypeCustom:
		return "自定义"
	default:
		return ""
	}
}

// mustDB 开一个内存库
func mustDB() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:scope_demo?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(&row{}); err != nil {
		panic(err)
	}
	return db
}

// seed 灌 4 行数据
func seed(db *gorm.DB) {
	rows := []row{
		{ID: 1, ProjectID: 10, DeptID: 100, OwnerID: 1, Name: "甲(本项目本部门本人)"},
		{ID: 2, ProjectID: 10, DeptID: 101, OwnerID: 2, Name: "乙(本项目下级部门)"},
		{ID: 3, ProjectID: 20, DeptID: 100, OwnerID: 3, Name: "丙(别项目本部门)"},
		{ID: 4, ProjectID: 20, DeptID: 200, OwnerID: 1, Name: "丁(别项目别人建)"},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			panic(err)
		}
	}
}
