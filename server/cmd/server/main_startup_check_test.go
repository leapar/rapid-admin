// main_startup_check_test.go —— 启动期 fail-fast 的机械守卫 —— M166(2026-11-02)
//
// # 这条测试守的是什么
//
// `main.go` 里有几处**启动期检查**:账本对不上 ⇒ `log.Fatal` ⇒ 带着病的服务**起不来**。
//
// ⚠️ **这些检查是"手工维护"的** —— 它们不像单测那样被 CI 盯着,
// 改 main.go 的人删掉那几行,**没有任何报错**,服务照常启动,
// 只是**那张网不再响了**。
//
// ⇒ 与 M161 的 `biz_scope_wiring_test.go` 同一个思路:
// **扫源码**,把"该有的 fail-fast 调用"和"main.go 里实际有的"对账。
//
// # 为什么用 go/ast 而不是正则
//
// 正则扫源码会撞上三件事:
//  ① 注释里的 `service.CheckScopeWiring()` 被当成真调用;
//  ② 字符串里的 `//` 被当成注释起点;
//  ③ 调用点跨行时正则匹配不到。
//
// go/parser 完全不碰注释,给的是 AST ⇒ 判据精确。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//   · 把 main.go 里 service.CheckFieldPolicyWiring() 那几行删掉 → 本用例红
//   · 把 `log.Fatal(...)` 换成 `log.Warn(...)`               → 本用例红
//   · 把 `if err := ...; err != nil` 改成 `_ = ...`           → 本用例红

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// startupChecks 必须在 main.go 里 fail-fast 的启动期检查
//
// ⚠️ **新加一条启动期检查,这里也要加一行** ——
// 否则那条检查没人守,删了没人知道。
var startupChecks = []string{
	// 行级数据范围:路由层登记的业务域必须三选一(接了 / ctx 那条路 / 明确不接)
	"CheckScopeWiring",
	// 字段级可见性:接了行级范围的业务域必须在字段层登记(M166)
	"CheckFieldPolicyWiring",
}

// TestStartupChecks_启动期检查都fail_fast 每个检查都必须真的把服务拦住
//
// ⚠️ **只查"调用在不在"是不够的** ——
//
//	if err := service.CheckScopeWiring(); err != nil {
//		log.Warn("...", zap.Error(err))   ← 查出来却只 warn,等于没查
//	}
//
// ⇒ 判据是"**调用 + 出错时 log.Fatal**"两件事都在。
func TestStartupChecks_启动期检查都fail_fast(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("main.go 解析失败: %v", err)
	}

	// 记下"哪些检查被 fail-fast 了"
	fataled := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || ifs.Init == nil {
			return true
		}
		// 只认 `if err := f(); err != nil { ... }` 这种形状
		as, ok := ifs.Init.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		if !inList(startupChecks, name) {
			return true
		}
		if bodyCallsFatal(ifs.Body) {
			fataled[name] = true
		}
		return true
	})

	for _, name := range startupChecks {
		if !fataled[name] {
			t.Errorf("main.go 里没有 %s 的 fail-fast 调用 —— "+
				"启动期检查被删掉或只 warn ⇒ 那张网静默失效,带病的服务照样起来", name)
		}
	}
}

// bodyCallsFatal 这个语句块里有没有 log.Fatal / logger.Fatal 之类的终止调用
func bodyCallsFatal(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Fatal", "Fatalf", "Fatalw", "Panic", "Panicf":
			found = true
		}
		return true
	})
	return found
}

// inList 一个小工具(这个包里没有同名的东西)
func inList(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
