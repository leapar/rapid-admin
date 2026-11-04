// dynx —— 动态业务集合的字段类型注册表
//
// ## 这个包解决什么问题
//
// 「业务表单设计器」让用户自己设计业务表(比如"设备巡检记录")。
// 每设计一个字段,系统要同时回答三个问题:
//
//  1. 这个字段在数据库里落成什么列?(DDL 类型)
//  2. 前端提交的 JSON 值怎么变成能存进去的值?(归一化)
//  3. 存进去的值怎么变成前端能显示的值?(反归一化)
//
// 这三个问题必须由**同一张表**回答 ——
// 各写各的就会出现"DDL 说它是 VARCHAR、归一化却塞了个 int64"这种分叉,
// 表现是"存进去看着对,查出来类型变了",而且不报错。
//
// ## 跑法
//
//	go run ./examples/dynx
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/dynx"
)

func main() {
	// 一、有哪些字段类型 —— 前端下拉框直接吃这张表
	fmt.Println("=== 字段类型 ===")
	for _, spec := range dynx.Types() {
		fmt.Printf("%-12s %-8s mysql=%-14s sqlite=%-8s 选项集=%v\n",
			spec.Type, spec.Label, spec.MySQLType, spec.SQLiteType, spec.HasOptions)
	}

	// 二、归一化:前端给什么,库里存什么
	//
	// ⚠️ 这是**唯一写进数据库的入口** —— 不归一化的话,
	// int 字段可能存进 "123"(字符串)、日期可能存进 "2026/11/01"。
	// 这些值当场不报错,等到按类型读出来做计算时才炸,而那时数据已经写进去了。
	fmt.Println()
	fmt.Println("=== 归一化 ===")
	opts := dynx.Options{Choices: []dynx.Choice{
		{Value: "pass", Label: "合格"},
		{Value: "fail", Label: "不合格"},
	}}

	show := func(label string, tp dynx.Type, raw any) {
		stored, err := dynx.Normalize(tp, raw, opts)
		if err != nil {
			fmt.Printf("%-16s ✗ %v\n", label, err)
			return
		}
		fmt.Printf("%-16s %v  (%T)\n", label, stored, stored)
	}
	show("整数 42", dynx.TypeInt, 42)
	show("小数 1.5", dynx.TypeNumber, 1.5)
	show("开关 true", dynx.TypeBool, true)
	show("日期", dynx.TypeDate, "2026-11-01")
	show("日期时间", dynx.TypeDateTime, "2026-11-01 08:30:00")
	show("单选 合格", dynx.TypeSelect, "pass")
	show("单选 不存在", dynx.TypeSelect, "unknown")
	show("多选", dynx.TypeMultiSelect, []any{"pass", "fail"})
	show("JSON", dynx.TypeJSON, map[string]any{"k": 1})

	// 三、反归一化:库里读出来的值变成前端能显示的值
	//
	// ⚠️ 这一层是**宽松地读** —— 同一个列在 MySQL 和 SQLite 下
	// 读出来的 Go 类型可能不一样(MySQL 的 VARCHAR 给 string、
	// DECIMAL 给 []byte,BIGINT 给 int64)。
	// 严格校验放在写入那一侧,读出来只需要能显示。
	fmt.Println()
	fmt.Println("=== 反归一化 ===")
	fmt.Printf("%-16s %v\n", "整数列", dynx.Denormalize(dynx.TypeInt, int64(42)))
	fmt.Printf("%-16s %v\n", "小数列", dynx.Denormalize(dynx.TypeNumber, []byte("1.50")))
	fmt.Printf("%-16s %v\n", "开关列", dynx.Denormalize(dynx.TypeBool, int64(1)))
	fmt.Printf("%-16s %v\n", "多选列", dynx.Denormalize(dynx.TypeMultiSelect, `["pass","fail"]`))
	fmt.Printf("%-16s %v\n", "JSON 列", dynx.Denormalize(dynx.TypeJSON, `{"k":1}`))

	// 四、字段标识 → 物理列名
	//
	// ⚠️ 统一加 f_ 前缀 —— 物理表上还有 id / owner_id / created_at 等系统列,
	// 前缀保证用户字段**不可能**跟系统列撞名。
	// (撞名的后果不是"报错",而是用户的字段悄悄写进了系统列。)
	fmt.Println()
	fmt.Println("=== 物理列名 ===")
	fmt.Printf("%-16s %s\n", "device_name", dynx.ColumnName("device_name"))
	fmt.Printf("%-16s %s\n", "集合 inspection", dynx.PhysTableName("inspection"))
}
