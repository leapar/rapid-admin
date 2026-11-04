// dynfilter —— 记录筛选表达式 → 参数化 SQL(对标 PocketBase 的 fexpr)
//
// ## 语法(与 PocketBase 同形状,子集)
//
//	status = 'active' && amount > 1000
//	status ?= ('active','pending')     -- 多值 IN
//	title ~ '验收'                     -- 包含(LIKE %值%)
//	amount = null                      -- IS NULL
//	(a = 1 || b = 2) && c != null
//
// ## 关键点
//
//   - 值一律占位符化,字段一律白名单解析 —— 注入面从结构上不存在
//   - 字段拼错直接报错,不静默忽略(静默返回"全表"比报错危险得多)
//   - 返回的 WHERE 片段整体带括号,调用方直接 AND 进既有条件
//
// ## 跑法
//
//	go run ./examples/dynfilter
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/dynfilter"
	"rapid-admin/internal/pkg/dynx"
)

func main() {
	// 字段白名单:服务层用「在用字段 + 系统列」喂进来
	fields := map[string]dynfilter.Field{
		"status": {Column: "f_status", Type: dynx.TypeSelect},
		"amount": {Column: "f_amount", Type: dynx.TypeNumber},
		"title":  {Column: "f_title", Type: dynx.TypeText},
	}
	lookup := func(key string) (dynfilter.Field, bool) {
		f, ok := fields[key]
		return f, ok
	}

	for _, expr := range []string{
		"status = 'active' && amount > 1000",
		"status ?= ('active','pending')",
		"title ~ '验收'",
		"(status = 'active' || status = 'frozen') && amount = null",
	} {
		where, args, err := dynfilter.Compile(expr, lookup)
		if err != nil {
			fmt.Printf("%s\n  ✗ %v\n\n", expr, err)
			continue
		}
		fmt.Printf("%s\n  WHERE %s\n  args  = %v\n\n", expr, where, args)
	}

	// 字段拼错:报错而不是静默忽略全表
	_, _, err := dynfilter.Compile("staus = 'active'", lookup)
	fmt.Printf("拼错字段名 ⇒ %v\n", err)
}
