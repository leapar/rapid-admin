// Package main 业务错误码集中文档生成器 —— §1.4 #9 P1
//
// 输入: server/internal/pkg/errcode/errcode.go(const 块 + defaultMsg map)
// 输出: docs/error-codes.md(按码段分组的 Markdown 表格)
//
// 跑法(在 server/ 目录下):
//
//	go run ./cmd/gen-errcode-doc
//
// 设计:
//   - 走 Go AST 解析 const 块,只取 ErrCode*(整型)常量,跳过 HTTPStatus/Msg 函数等
//   - 同时解析 defaultMsg 映射,用 code 关联文案
//   - 输出按码段(1xxxx 基础 / 2xxxx 业务 / 3xxxx 配置 / 5xxxx 框架)分组
//   - HTTP 状态码**直接调 errcode.HTTPStatus()** —— 不再在本文件里抄一份
//   - 业务码不在 defaultMsg 也列出(标"无默认文案",提醒开发者补文案)
//
// # ⚠️ 2026-10-26 M98:HTTP 状态码推导的"第三份拷贝"被删掉了
//
// 本文件原来自己写了一个 `httpStatus(v int) int`,注释还写着"与 errcode.HTTPStatus
// 同步(避免反射引入复杂度)"。但**它没同步**:
//   - 它没有 50100 → 501(NotImplemented)
//   - 它没有 50300 → 503 / 50400 → 504
//
// 于是 `docs/error-codes.md` 里明明白白写着 50100 → 500 / 50300 → 500 / 50400 → 500,
// 而服务端 `response.Fail` 返的是 503 / 504。**文档在撒谎**,而且没有任何测试
// 能发现 —— 因为文档就是这份拷贝生成的。
//
// 现在改成 `errcode.HTTPStatus(v)`:文档里的每一格都是**运行时真值**。
//
// 与 docs/error-codes.md 的关系:
//   - 文档是"已生成",由 make 目标 docs/errcode 触发
//   - 增删业务码时只改 errcode.go,跑一次脚本即更新文档
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"

	"rapid-admin/internal/pkg/errcode"
)

const (
	// 跑法:在 server/ 目录下 go run ./cmd/gen-errcode-doc
	// srcPath 相对 server/ 模块根
	srcPath = "internal/pkg/errcode/errcode.go"
	// outPath 相对 server/ 模块根,落在 repo 根 docs/ 下
	outPath = "../docs/error-codes.md"
)

// Code 一行错误码
type Code struct {
	Name       string
	Value      int
	Msg        string // defaultMsg 里的中文文案(可能为空)
	Comment    string // const 上方紧邻的 // 注释
	Group      string // 1xxxx / 2xxxx / 3xxxx / 5xxxx
	HTTPStatus int
}

func main() {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", srcPath, err)
		os.Exit(1)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, srcPath, src, parser.ParseComments)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse %s: %v\n", srcPath, err)
		os.Exit(1)
	}

	// 两遍解析:先 const(name→value),再 defaultMsg(用 const 解析 Ident key)
	nameToVal := extractConstsMap(file)
	msgs := extractDefaultMsg(file, nameToVal)
	codes := extractConsts(file, msgs)
	grouped := groupByRange(codes)

	var b strings.Builder
	b.WriteString("# 业务错误码清单\n\n")
	b.WriteString("> 本文档由 `server/cmd/gen-errcode-doc` 自动生成(2026-10-26)。\n")
	b.WriteString("> 改码/改文案请直接编辑 `server/internal/pkg/errcode/errcode.go`,然后跑:\n")
	b.WriteString("> ```\n> cd server && go run ./cmd/gen-errcode-doc\n> ```\n\n")
	b.WriteString("## 概述\n\n")
	b.WriteString("业务码 6 位整数,按段位表达语义:\n\n")
	b.WriteString("| 段位 | 类别 | HTTP |\n|---|---|---|\n")
	b.WriteString("| 0 | 成功 | 200 |\n")
	b.WriteString("| 40000-40099 / 40500-41299 / 41400-42899 / 43000-49999 | 请求参数 / 业务参数错 | 400 |\n")
	b.WriteString("| 401xx | 未登录 / 令牌过期 | 401 |\n")
	b.WriteString("| 403xx | 无权限 / 越权 | 403 |\n")
	b.WriteString("| 404xx | 资源不存在 | 404 |\n")
	b.WriteString("| 409xx | 数据冲突 | 409 |\n")
	b.WriteString("| 413xx | 请求体过大 | 413 |\n")
	b.WriteString("| 429xx | 限流 | 429 |\n")
	b.WriteString("| 50000-59999(默认) | 框架 / 服务端错 | 500 |\n")
	b.WriteString("| 501xx | 业务未实现(该换条路走,不是故障) | 501 |\n")
	b.WriteString("| 503xx | 服务不可用(熔断 open) | 503 |\n")
	b.WriteString("| 504xx | 网关超时 | 504 |\n")
	b.WriteString("| 其他业务段(2xxxx / 3xxxx) | 业务语义错 | 200(由 i18n 文案呈现) |\n\n")
	b.WriteString("## 状态码对照\n\n")
	b.WriteString("| 业务码 | HTTP | 说明 |\n|---|---|---|\n")
	b.WriteString("| 0 | 200 | 成功 |\n")
	b.WriteString("| 4xxxx 业务参数 | 400 | 请求参数 / 业务参数错 |\n")
	b.WriteString("| 401xx | 401 | 未登录 / 令牌过期 |\n")
	b.WriteString("| 403xx | 403 | 无权限 / 越权 |\n")
	b.WriteString("| 404xx | 404 | 资源不存在 |\n")
	b.WriteString("| 409xx | 409 | 数据冲突 |\n")
	b.WriteString("| 413xx | 413 | 请求体过大(BodyLimit middleware) |\n")
	b.WriteString("| 429xx | 429 | 限流(框架级 + 业务级) |\n")
	b.WriteString("| 50000(默认) | 500 | 框架内部错 |\n")
	b.WriteString("| 501xx | 501 | 业务未实现(该换条路走,不是故障) |\n")
	b.WriteString("| 50300 | 503 | ServiceUnavailable(熔断 open) |\n")
	b.WriteString("| 50400 | 504 | GatewayTimeout(per-route timeout) |\n\n")

	// 业务码明细(按码段排序:0=成功, 4xxxx=客户端错, 2xxxx/3xxxx=业务语义, 5xxxx=服务端)
	order := []string{"0", "2xxxx", "3xxxx", "4xxxx", "5xxxx"}
	for _, g := range order {
		rows, ok := grouped[g]
		if !ok || len(rows) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s %s\n\n", g, groupTitle(g)))
		b.WriteString("| 码 | 常量名 | 默认文案 | HTTP | 说明 |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, c := range rows {
			msg := c.Msg
			if msg == "" {
				msg = "_(无默认文案)_"
			}
			comment := strings.TrimSpace(c.Comment)
			if comment == "" {
				comment = "-"
			}
			b.WriteString(fmt.Sprintf("| %d | `%s` | %s | %d | %s |\n", c.Value, c.Name, msg, c.HTTPStatus, comment))
		}
		b.WriteString("\n")
	}

	b.WriteString("## 命名约定\n\n")
	b.WriteString("- 段位:1xxxx=基础(成功/参数/认证/限流/框架),2xxxx=业务,3xxxx=平台配置,5xxxx=框架\n")
	b.WriteString("- 后两位:同一语义组内递增(如 20100 系列都属登录相关)\n")
	b.WriteString("- 文案:i18n 优先(`BIZ_CODE_KEYS` map,web/src/types/api.ts),拿不到走后端 `defaultMsg`\n")
	b.WriteString("- 错误对象:`errcode.New(code, msg...)`,带详情用 `.WithDetail(...)`\n\n")

	b.WriteString("## 添加新错误码 checklist\n\n")
	b.WriteString("1. `errcode.go` 加 const(放对段位)\n")
	b.WriteString("2. `defaultMsg` 加默认中文文案\n")
	b.WriteString("3. 业务调用 `errcode.New(errcode.YourCode, ...).WithDetail(...)`\n")
	b.WriteString("4. 跑 `cd server && go run ./cmd/gen-errcode-doc` 刷新本文档\n")
	b.WriteString("5. 涉及前端 i18n 翻译,同步加 `web/src/locales/{zh-CN,en-US}.ts::bizCode` 块\n")

	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", outPath, err)
		os.Exit(1)
	}
	fmt.Printf("[gen_errcode_doc] %s -> %s (%d codes)\n", srcPath, outPath, len(codes))
}

// extractDefaultMsg 从 var defaultMsg = map[int]string{...} 提映射
// key 可能是 int literal,也可能是顶层 const 标识符(如 `Success: "ok"`),
// 通过 nameToVal 把 Ident 解析为 int
func extractDefaultMsg(file *ast.File, nameToVal map[string]int) map[int]string {
	out := map[int]string{}
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
				if name.Name != "defaultMsg" {
					continue
				}
				cl, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range cl.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					code := resolveInt(kv.Key, nameToVal)
					val, ok2 := stringLit(kv.Value)
					// 允许 0(Success 码)
					if ok2 {
						out[code] = val
					}
				}
			}
		}
	}
	return out
}

// extractConstsMap 第一遍解析:只取 const 块里 int literal 的常量
// 返回 name→int,供 defaultMsg 解析 Ident key 时用
func extractConstsMap(file *ast.File) map[string]int {
	out := map[string]int{}
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if v, ok := intLit(vs.Values[i]); ok {
					out[name.Name] = v
				}
			}
		}
	}
	return out
}

// resolveInt 解析 int literal 或顶层 const 标识符
func resolveInt(e ast.Expr, nameToVal map[string]int) int {
	if v, ok := intLit(e); ok {
		return v
	}
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := nameToVal[id.Name]; ok {
			return v
		}
	}
	return 0
}

// extractConsts 提 const 块里的 int 常量
func extractConsts(file *ast.File, msgs map[int]string) []Code {
	var out []Code
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// 收集 spec 块上方的 // 注释(本块共享)
			blockComment := ""
			if vs.Doc != nil {
				for _, c := range vs.Doc.List {
					blockComment += strings.TrimPrefix(c.Text, "//") + " "
				}
			}
			for i, name := range vs.Names {
				// 跳过用 iota 但无 Values 的(常量继承上一行)
				if i >= len(vs.Values) {
					continue
				}
				v, ok := intLit(vs.Values[i])
				if !ok {
					continue
				}
				// 注释来源(spec 上方 // doc,共享给本块所有 name)
				comment := blockComment
				// 同行尾注释(// xxx 在 const 行末尾)
				if vs.Comment != nil {
					var lc strings.Builder
					for _, c := range vs.Comment.List {
						lc.WriteString(strings.TrimPrefix(c.Text, "//") + " ")
					}
					if c := strings.TrimSpace(lc.String()); c != "" {
						comment = c
					}
				}
				out = append(out, Code{
					Name:       name.Name,
					Value:      v,
					Msg:        msgs[v],
					Comment:    comment,
					Group:      groupOf(v),
					HTTPStatus: errcode.HTTPStatus(v),
				})
			}
		}
	}
	// 排序:按 Value
	sort.Slice(out, func(i, j int) bool { return out[i].Value < out[j].Value })
	return out
}

// groupOf 业务码 → 段位字符串
func groupOf(v int) string {
	switch {
	case v == 0:
		return "0"
	case v >= 20000 && v < 30000:
		return "2xxxx"
	case v >= 30000 && v < 40000:
		return "3xxxx"
	case v >= 40000 && v < 50000:
		return "4xxxx"
	case v >= 50000 && v < 60000:
		return "5xxxx"
	}
	return "other"
}

// groupTitle 段位 → 标题
func groupTitle(g string) string {
	switch g {
	case "0":
		return "成功"
	case "2xxxx":
		return "业务(登录 / 业主 / 资料 / 房屋 / 状态机)"
	case "3xxxx":
		return "平台配置中心"
	case "4xxxx":
		return "请求 / 客户端错(参数 / 认证 / 限流)"
	case "5xxxx":
		return "框架(未实现 / 熔断 / 网关超时)"
	}
	return "其他"
}

func groupByRange(codes []Code) map[string][]Code {
	out := map[string][]Code{}
	for _, c := range codes {
		out[c.Group] = append(out[c.Group], c)
	}
	return out
}

func intLit(e ast.Expr) (int, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.INT {
		return 0, false
	}
	var v int
	if _, err := fmt.Sscanf(bl.Value, "%d", &v); err != nil {
		return 0, false
	}
	return v, true
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	return bl.Value, true
}
