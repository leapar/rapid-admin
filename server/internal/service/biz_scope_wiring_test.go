// biz_scope_wiring_test.go —— 数据范围「接线」的机械守卫 —— M161(2026-11-02)
//
// # 为什么要这条测试
//
// §1.13 #2 要求的是:"让每个业务查询**必须声明**自己受哪个业务域管
// (不声明 ⇒ 启动失败),而不是'忘了接就当没这回事'"。
//
// `CheckScopeWiring()`(启动期 fail-fast)只管**一半**:
// 它拿**路由层登记的业务域**去比对三张账本 ⇒ 能抓住"新加了一个业务域却没登记"。
//
// ⚠️ **但它抓不住另一半:账本里登记了,业务查询却根本没调它。**
//
// 那个洞长这样:
//
//	bizScopeSpecs 里加了 {module: "family", ...}   // 登记表说它接了
//	// …… 而 family.go 里一行 applyBizScope 都没有
//
// ⇒ 后台候选列表里**出现**了「家庭组」,管理员**配得出**规则,
// 而运行时**永远不查**这条规则 ⇒ **配了也不生效,一行报错都没有**。
// 这正是这套机制要治的病,却从机制内部漏了出来。
//
// ⇒ 所以这里补上另一半:**扫源码**,把"账本"和"真实的调用点"对账。
//
// # 为什么用 go/ast 而不是正则
//
// 正则扫源码会撞上三件事:
//  ① 注释里的 `applyBizScope(..., "house")` 被当成真调用点;
//  ② 字符串里的 `//` 被当成注释起点;
//  ③ 调用点跨行时正则匹配不到。
//
// go/parser 完全不碰注释,给的是 AST ⇒ 判据精确。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//   · 把 family.go 里的 applyBizScope 调用删掉   → W1 红(登记表与调用点对不上)
//   · 把某处业务域字面量改成 "houze"             → W2 红(调用点没登记)
//   · 把 bizScopeCtxWired 的 columnsVar 改错      → W3 红(列模板不存在)
//   · 把 applyScope 的调用删掉                    → W3 红(列模板没人用)
//   · 把同一个业务域同时登记进两张账本            → W4 红(账本重叠)

package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// scopeCallSite 一处数据范围接线调用点
type scopeCallSite struct {
	file string
	line int
	// module applyBizScope 的最后一个参数(业务域字面量)
	module string
	// columnsVar applyScope 的最后一个参数(列模板变量名)
	columnsVar string
}

// scanScopeCallSites 扫 service 包(不含 _test.go)里的接线调用点
//
// ⚠️ **只看 service 包自己** —— 接线只可能发生在 service 层
// (handler 层没有 tx,拼不出 WHERE)。
func scanScopeCallSites(t *testing.T) []scopeCallSite {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	require.NoError(t, err, "service 包源码必须能解析 —— 解析不了说明有语法错误")

	var out []scopeCallSite
	for _, pkg := range pkgs {
		for fname, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fun, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				switch fun.Name {
				case "applyBizScope":
					// ⚠️ **最后一个参数必须是业务域字面量** ——
					// 传变量的话,账本就对不上账(扫源码看不见真正的域)。
					lit, ok := call.Args[len(call.Args)-1].(*ast.BasicLit)
					require.True(t, ok && lit.Kind == token.STRING,
						"%s: applyBizScope 的最后一个参数必须是业务域字符串字面量(不能是变量)",
						fset.Position(call.Pos()))
					mod, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					out = append(out, scopeCallSite{
						file: fname, line: fset.Position(call.Pos()).Line,
						module: mod,
					})
				case "applyScope":
					// 最后一个参数是列模板变量名
					id, ok := call.Args[len(call.Args)-1].(*ast.Ident)
					require.True(t, ok,
						"%s: applyScope 的最后一个参数必须是列模板变量",
						fset.Position(call.Pos()))
					out = append(out, scopeCallSite{
						file: fname, line: fset.Position(call.Pos()).Line,
						columnsVar: id.Name,
					})
				}
				return true
			})
		}
	}
	return out
}

// pkgLevelVars 列出 service 包里所有包级变量名(排除测试文件)
func pkgLevelVars(t *testing.T) map[string]bool {
	t.Helper()

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.SkipObjectResolution)
	require.NoError(t, err)

	out := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						out[name.Name] = true
					}
				}
			}
		}
	}
	return out
}

// TestBizScope_登记表里的每个业务域都真的被调用过(W1)
//
// ⚠️ **这条是本文件的核心** ——
// 它防的是"登记表说接了,业务查询其实没调"这一类**静默失效**。
//
// 真 bug 记录:M161 第一版把 "family" 登记进了 `bizScopeSpecs`,
// 而 `service/family.go` 里**一行 applyBizScope 都没有**
// ⇒ 后台候选列表里会出现「家庭组」,管理员配得出规则,运行时永远不生效。
func TestBizScope_登记表里的每个业务域都真的被调用过(t *testing.T) {
	calls := scanScopeCallSites(t)

	called := map[string]bool{}
	for _, c := range calls {
		if c.module != "" {
			called[c.module] = true
		}
	}

	var missing []string
	for _, sp := range bizScopeSpecs {
		if !called[sp.module] {
			missing = append(missing, sp.module)
		}
	}
	require.Empty(t, missing,
		"这些业务域在 bizScopeSpecs 里登记了(后台候选列表里会出现,管理员配得出规则),"+
			"但业务查询里没有 applyBizScope 调用 ⇒ 规则永远不生效,而且没有任何报错。"+
			"要么给它接上,要么从登记表里拿掉并写进 bizScopeUnwired")

	// ⚠️ **反向也要对** —— 调用了但没登记 ⇒ 运行时才炸(applyBizScope 会报"没有登记")。
	var unregistered []string
	for module := range called {
		if _, ok := bizScopeSpecFor(module); !ok {
			unregistered = append(unregistered, module)
		}
	}
	require.Empty(t, unregistered,
		"这些业务域被 applyBizScope 调用了,却不在 bizScopeSpecs 里登记 ⇒ "+
			"运行时每个查询都会报「业务域 X 没有登记数据范围接线」")
}

// TestBizScope_ctx那条路的业务域有列模板且真被用上(W3)
//
// ⚠️ **bizScopeCtxWired 里的两项不走 applyBizScope** ——
// 它们走 `applyScope(ctx, s.scope, tx, 列模板)`(业务域从 ctx 读)。
// ⇒ 它们的"有没有接上"要看列模板变量在不在、且有没有被 applyScope 用。
func TestBizScope_ctx那条路的业务域有列模板且真被用上(t *testing.T) {
	calls := scanScopeCallSites(t)
	vars := pkgLevelVars(t)

	used := map[string]bool{}
	for _, c := range calls {
		if c.columnsVar != "" {
			used[c.columnsVar] = true
		}
	}

	for module, sp := range bizScopeCtxWired {
		require.True(t, vars[sp.columnsVar],
			"业务域 %s 登记的列模板变量 %s 在 service 包里不存在 —— "+
				"要么它被改名/删了,要么登记写错了", module, sp.columnsVar)
		require.True(t, used[sp.columnsVar],
			"业务域 %s 登记的列模板变量 %s 从来没被 applyScope 用过 ⇒ "+
				"这个业务域其实没接上数据范围(但它在后台候选列表里)", module, sp.columnsVar)
	}
}

// TestBizScope_三张账本互不重叠(W4)
//
// ⚠️ **重叠 = 两处真相** —— 同一个业务域在两张表里各登记一次,
// 改了一处忘了另一处 ⇒ 行为取决于哪张表先被查到。
func TestBizScope_三张账本互不重叠(t *testing.T) {
	for _, sp := range bizScopeSpecs {
		_, inCtx := bizScopeCtxWired[sp.module]
		require.False(t, inCtx,
			"业务域 %s 同时登记在 bizScopeSpecs 和 bizScopeCtxWired 里", sp.module)
		_, inUnwired := bizScopeUnwired[sp.module]
		require.False(t, inUnwired,
			"业务域 %s 同时登记在 bizScopeSpecs 和 bizScopeUnwired 里", sp.module)
	}
	for module := range bizScopeCtxWired {
		_, inUnwired := bizScopeUnwired[module]
		require.False(t, inUnwired,
			"业务域 %s 同时登记在 bizScopeCtxWired 和 bizScopeUnwired 里", module)
	}
}
