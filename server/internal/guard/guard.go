// Package guard 是框架原语的可达性扫描器 —— M129(2026-10-31)。
//
// # 这个包解决什么问题
//
// 本仓反复踩同一个坑:**一个包被写出来了、有单测、清单里标着 "✅ 已交付",
// 却从来没有任何生产代码调用过。**
//
// 已有的三个实例:
// - `pkg/anomalylogin/` —— 172 行单测、清单 §1.1 #11 标着 "P2 ✅",
// 而 `grep -rn 'internal/pkg/anomalylogin'` 全仓零命中(M128 才发现)。
// 清单里那句痛点「用户被盗号时无法感知」**一次都没被真正解决过**。
// - `pkg/sms/` —— 零调用方、零单测,四块能力全部另有活着的实现(M129 删除)。
// - `pkg/ratelimitx/`、`pkg/searchidx/` —— 库类原语 + 单测齐全,
// 但零生产调用方;它们**是故意留着的**(接线点在业务线),所以不能删,只能**如实标注**。
//
// 三条根因同源:**「有单测」被当成了「在跑」**。
//
// # 判据:可达性,不是"有没有人 import 我"
//
// ⚠️ 光数"谁 import 了我"是不够的 ——
// `pkg/a` import `pkg/b`,而没人 import `pkg/a`,那么 `pkg/b` 的
// "import 方"计数是 1,但它**同样没在跑**。
//
// ⇒ 这里算的是**从入口出发的可达性**:
// 入口 = `cmd/**`(所有会编译成可执行程序的目录);
// 边 = **非测试文件**里的 import;
// 结论 = 从入口走不到 ⇒ 这个包**在生产链路上一行都没跑**。
//
// ⚠️ 入口取 `cmd/**` 而不是只取 `cmd/server`:本仓还有 `cmd/seed` / `cmd/migrate`
// / `cmd/seed_audit` / `cmd/gen-errcode-doc` 这几个**一次性运维脚本** ——
// 它们也是**会编译、会被执行**的真程序,只被它们用到的包**不算死代码**。
// (实测:今天 `cmd/server` 之外的入口**没有多带出任何一个** `internal/pkg` 包,
// 所以两种取法结果一致 —— 但那是**巧合**,不是设计保证,所以取宽的那一个。)
//
// 之所以把**测试文件的 import 排除掉**:测试 import 一个包,
// 只证明"有人在测它",不证明"它在跑" —— 而这正是本仓最常见的假绿。
//
// # 判定结果怎么用
//
// 扫出来的"不可达"包分两类,处理方式完全不同:
// - **死代码**(能力已有别的实现)⇒ **删**。
// - **库类原语**(接线点在业务线)⇒ **留,但必须在清单里如实标注"零生产调用方"**,
// 并在本包的 allowlist 里写明理由。
//
// ⇒ allowlist 不是"逃逸名单",是**账本**:每一条都必须写清楚
// "为什么它现在可以没有调用方"。
package guard

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PackageInfo 一个包的扫描结果。
type PackageInfo struct {
	// ImportPath 模块内的完整 import 路径(例:rapid-admin/internal/pkg/logger)
	ImportPath string
	// Dir 相对仓库根的目录(例:internal/pkg/logger)
	Dir string
	// Reachable 从 cmd/ 入口出发能不能走到它
	Reachable bool
}

// Result 一次扫描的完整结果。
type Result struct {
	// ModuleRoot 仓库根(有 go.mod 的那一层)
	ModuleRoot string
	// ModulePath go.mod 里的 module 名(例:rapid-admin)
	ModulePath string
	// Packages 扫到的所有包(按 import path 排序)
	Packages []PackageInfo
}

// Unreachable 返回从 cmd/ 入口走不到的包。
func (r Result) Unreachable() []PackageInfo {
	var out []PackageInfo
	for _, p := range r.Packages {
		if !p.Reachable {
			out = append(out, p)
		}
	}
	return out
}

// FindModuleRoot 从 start 往上找带 go.mod 的目录。
//
// 找不到就返回错误而不是空串 —— 守卫在"找不到仓库根"时必须**响亮失败**,
// 不能安静地扫了个空目录然后报"一切正常"(那是假绿)。
func FindModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("从 %s 往上找不到 go.mod —— 不是在仓库里跑的吗", start)
		}
		dir = parent
	}
}

// modulePath 读 go.mod 的第一行 module 声明。
func modulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", fmt.Errorf("%s/go.mod 里没有 module 声明", root)
}

// isSkippedDir 跳过不该当包看的目录。
// - testdata/ —— Go 工具链本身就约定忽略它;里面的 .go 是**夹具**,
// 不是包(`internal/pkg/jsonid/testdata/scanfixture` 就是这种)。
// - 以 . 或 _ 开头的目录 —— Go 工具链同样忽略。
func isSkippedDir(name string) bool {
	return name == "testdata" ||
		strings.HasPrefix(name, ".") ||
		strings.HasPrefix(name, "_")
}

// Scan 扫整个模块,算出每个包从 cmd/ 入口的可达性。
//
// scanRoots 是要纳入扫描的**顶层目录**(相对仓库根),
// 例如 []string{"internal/pkg"}。cmd/ 永远作为入口参与,
// 即使不在 scanRoots 里。
func Scan(root string, scanRoots []string) (Result, error) {
	mod, err := modulePath(root)
	if err != nil {
		return Result{}, err
	}
	res := Result{ModuleRoot: root, ModulePath: mod}

	dirToImport, importToDir, err := collectPackages(root, mod)
	if err != nil {
		return Result{}, err
	}
	edges, err := buildEdges(root, dirToImport, importToDir)
	if err != nil {
		return Result{}, err
	}
	reachable := reachableFrom(entriesOf(importToDir, mod), edges)

	for _, scanRoot := range scanRoots {
		for rel, ip := range dirToImport {
			if !strings.HasPrefix(rel, scanRoot+"/") {
				continue
			}
			res.Packages = append(res.Packages, PackageInfo{
				ImportPath: ip,
				Dir:        rel,
				Reachable:  reachable[ip],
			})
		}
	}
	sort.Slice(res.Packages, func(i, j int) bool {
		return res.Packages[i].ImportPath < res.Packages[j].ImportPath
	})
	return res, nil
}

// collectPackages 收集仓库里所有"模块内"的包目录。
//
// 返回两张互逆的表:目录(相对根) → import path,以及 import path → 目录。
//
// ⚠️ 这里**不能**因为"本目录没有 .go 文件"就 SkipDir ——
// `internal/pkg/` 本身就没有 .go 文件(它只是一堆子包的容器),
// 跳过它等于把整棵 internal/pkg 树都跳过了。
// "是不是一个包"留到走完之后按 hasGoFiles 过滤。
func collectPackages(root, mod string) (dirToImport, importToDir map[string]string, err error) {
	dirToImport = map[string]string{}
	importToDir = map[string]string{}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		base := filepath.Base(path)
		if base != "cmd" && base != "internal" && !strings.HasPrefix(rel, "internal/") &&
			!strings.HasPrefix(rel, "cmd/") {
			// 只关心 cmd/ 与 internal/ 两棵树
			return fs.SkipDir
		}
		if isSkippedDir(base) {
			return fs.SkipDir
		}
		ip := mod + "/" + rel
		dirToImport[rel] = ip
		importToDir[ip] = rel
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// 走完之后再剔掉"不是包"的目录(只有子目录、自己没有 .go 文件)
	for rel, ip := range dirToImport {
		if !hasGoFiles(filepath.Join(root, rel)) {
			delete(dirToImport, rel)
			delete(importToDir, ip)
		}
	}
	return dirToImport, importToDir, nil
}

// buildEdges 建 import 图(只看**非测试文件**)。
func buildEdges(root string, dirToImport, importToDir map[string]string) (map[string][]string, error) {
	edges := map[string][]string{}
	for rel, ip := range dirToImport {
		deps, err := packageImports(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		var local []string
		for _, dep := range deps {
			if _, ok := importToDir[dep]; ok {
				local = append(local, dep)
			}
		}
		edges[ip] = local
	}
	return edges, nil
}

// entriesOf 取入口包(`cmd/**`)。
func entriesOf(importToDir map[string]string, mod string) []string {
	var entries []string
	for ip := range importToDir {
		if strings.HasPrefix(ip, mod+"/cmd/") {
			entries = append(entries, ip)
		}
	}
	sort.Strings(entries)
	return entries
}

// reachableFrom 从入口出发做可达性传播。
//
// ⚠️ 可达性是**传递**的:A 不可达 ⇒ 只有 A import 的 B 同样不可达。
// 只数"谁 import 了我"会漏掉这一种。
func reachableFrom(entries []string, edges map[string][]string) map[string]bool {
	reachable := map[string]bool{}
	stack := append([]string(nil), entries...)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if reachable[cur] {
			continue
		}
		reachable[cur] = true
		for _, dep := range edges[cur] {
			if !reachable[dep] {
				stack = append(stack, dep)
			}
		}
	}
	return reachable
}

// hasGoFiles 目录下有没有 .go 文件(有 _test.go 也算 —— 那是"测试专用包")。
func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

// packageImports 解析目录下**非测试文件**的 import 列表。
//
// ⚠️ 刻意跳过 _test.go:测试 import 一个包只说明"有人在测它",
// 不说明"它在生产链路上跑过"。
func packageImports(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", filepath.Join(dir, e.Name()), err)
		}
		for _, imp := range f.Imports {
			if imp.Path == nil {
				continue
			}
			out = append(out, strings.Trim(imp.Path.Value, `"`))
		}
	}
	return out, nil
}
