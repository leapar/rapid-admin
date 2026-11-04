// examples —— 漂移网
//
// ## 这套测试守的是什么
//
//  1. internal/pkg 下每个顶层框架包都要有对应示例 —— 新增模块忘了写示例就红
//  2. 每个示例都必须是能 `go run` 的 main 包(有 package main + func main)
//  3. 示例不许 import 业务包 —— 框架示例如果引用了 internal/service,
//     别人拿这个框架就编译不过(业务代码不在开源范围内)
package examples

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pkgDir 框架包目录(相对本测试文件)
const pkgDir = "../internal/pkg"

// exampleDir 示例目录(相对本测试文件)
const exampleDir = "."

// forbiddenImports 示例里不许出现的 import 前缀
//
// ⚠️ internal/model 是业务模型,internal/api / internal/service 更是业务层 ——
// 这些都不在开源范围里。示例引用它们 ⇒ 别人 clone 下来编译不过。
//
// internal/config **不在这张表里**:它是框架的配置层(跟 kms.Config 同级),
// 框架自己的公开 API 就会用到它(比如 metricsx.Init 收 config.MetricsConfig)。
var forbiddenImports = []string{
	"rapid-admin/internal/service",
	"rapid-admin/internal/model",
	"rapid-admin/internal/api",
	"rapid-admin/cmd",
}

// frameworkPackages 列出 internal/pkg 下的顶层包目录
func frameworkPackages(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		t.Fatalf("读框架包目录失败 %s: %v", pkgDir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// TestExamples_每个框架包都有示例 —— **漂移网的主网**
//
// 新增框架模块(如 pkg/xxx)却忘了写 examples/xxx ⇒ 这里红。
func TestExamples_每个框架包都有示例(t *testing.T) {
	pkgs := frameworkPackages(t)
	if len(pkgs) == 0 {
		t.Fatalf("%s 下没找到任何框架包 —— 路径写错了?", pkgDir)
	}
	for _, name := range pkgs {
		name := name
		t.Run(name, func(t *testing.T) {
			main := filepath.Join(exampleDir, name, "main.go")
			if _, err := os.Stat(main); err != nil {
				t.Errorf("框架包 %s 没有示例 —— 缺 %s", name, main)
			}
		})
	}
}

// TestExamples_没有多余的示例目录 —— 反向漂移
//
// 示例目录存在但框架包被删了 ⇒ 这里的示例编译不过(import 路径失效),
// 但错误信息很难懂。这里提前一步报错。
func TestExamples_没有多余的示例目录(t *testing.T) {
	known := map[string]bool{}
	for _, name := range frameworkPackages(t) {
		known[name] = true
	}
	entries, err := os.ReadDir(exampleDir)
	if err != nil {
		t.Fatalf("读示例目录失败: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !known[e.Name()] {
			t.Errorf("示例目录 %s 没有对应的框架包(internal/pkg/%s)", e.Name(), e.Name())
		}
	}
}

// TestExamples_都是可运行的main包 —— 语法 + 结构双重校验
//
// 用 go/parser 解析而不是靠 go build:后者慢一个量级,而且报错指向
// 具体文件时不如解析器直观。
func TestExamples_都是可运行的main包(t *testing.T) {
	entries, err := os.ReadDir(exampleDir)
	if err != nil {
		t.Fatalf("读示例目录失败: %v", err)
	}
	fset := token.NewFileSet()
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		count++
		path := filepath.Join(exampleDir, e.Name(), "main.go")
		f, err := parser.ParseFile(fset, path, nil, parser.AllErrors)
		if err != nil {
			t.Errorf("%s 语法错误: %v", path, err)
			continue
		}
		if f.Name.Name != "main" {
			t.Errorf("%s 的 package 应为 main,实际 %s", path, f.Name.Name)
		}
		hasMain := false
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && fn.Name.Name == "main" && fn.Recv == nil {
				hasMain = true
			}
		}
		if !hasMain {
			t.Errorf("%s 缺 func main() —— 跑不起来", path)
		}
	}
	if count == 0 {
		t.Fatal("一个示例都没找到")
	}
	t.Logf("已校验 %d 个示例", count)
}

// TestExamples_不引用业务包 —— 开源可用性的硬约束
func TestExamples_不引用业务包(t *testing.T) {
	entries, err := os.ReadDir(exampleDir)
	if err != nil {
		t.Fatalf("读示例目录失败: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(exampleDir, e.Name(), "main.go")
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			continue // 语法错误由上面那个测试报
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			for _, bad := range forbiddenImports {
				if p == bad || strings.HasPrefix(p, bad+"/") {
					t.Errorf("%s import 了业务包 %s —— 开源框架的示例不能依赖业务代码", path, p)
				}
			}
		}
	}
}
