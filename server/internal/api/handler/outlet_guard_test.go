// outlet_guard_test.go —— 响应出口的机械守卫 —— M167(2026-11-02)
//
// # 这条测试守的是什么
//
// 字段级可见性(M163)收口在 BaseHandler 的五个出口:
//
//	OK / OKWithMessage / OKID / Page / PageWith
//
// 只要业务响应都走这五个出口,"某个接口忘了脱敏"这件事就**结构上不可能发生**
// —— 这是 M163 写在 `field_policy.go` 头部的那句承诺。
//
// ⚠️ **但那五个出口是"约定",不是"强制"** ——
//
//	// 某个新 handler
//	c.JSON(200, rows)     ← 写得很自然,编译器不会报错,评审也可能一眼滑过去
//
// ⇒ 响应照常返回,**该脱敏的字段原样上 wire,而且没有任何报错**。
// 清单 §1.13 #4 里那句"全仓绕过这几个出口的地方只有 4 处"是一句
// **手工核对过的话** —— 人走了话就过期了,而且没人会再去核一遍。
//
// ⇒ 这里把它变成一条断言:**扫源码**,把"实际写了响应的地方"和
// "账本上登记的豁免"对账。
//
// # 判据:扫源码,不是扫运行时
//
// 运行时去抓"哪些响应没过脱敏"抓不到 —— 没配规则时响应逐字节不变,
// 脱敏与否在 wire 上看不出差别(这正是 M163 的设计目标)。
// ⇒ 只能在**源码层**判:这行代码有没有走出口。
//
// # 为什么用 go/ast 而不是正则
//
// 正则扫源码会撞上四件事:
//  ① 注释里的 `c.JSON(...)` 被当成真调用(本文件自己就满是这种注释);
//  ② 字符串里的 `//` 被当成注释起点;
//  ③ 调用点跨行时正则匹配不到;
//  ④ `c.JSON` 与 `h.JSON`(某个业务方法)分不清。
//
// go/parser 完全不碰注释,给的是 AST ⇒ 判据精确。
//
// # ⚠️ **扫描范围 = internal/api/ 整棵树**
//
// 覆盖 handler(业务) / router(路由注册) / middleware(中间件)三层 ——
// 因为"绕过出口"这件事不只发生在业务 handler 里:
//
//	router.go 的 /version —— 内联 func,直接 c.JSON
//	middleware/bodylimit.go —— 413 错误体
//	middleware/gzip.go —— 把已写好的 body 换成压缩后的字节
//
// 三者今天都**不是**漏洞,但它们都是"绕过出口的写入点" ⇒
// 都要在账本上登记一句"为什么它可以绕过"。
//
// ⚠️ **范围之外的两处(故意)**::
//
//	internal/pkg/metricsx —— /metrics 是 Prometheus 文本,不是 JSON 业务行
//	internal/docs          —— 静态手册 + swagger,BasicAuth 保护,不是业务数据
//
// 它们都在 internal/api/ 之外 ⇒ 扫不到。这是**边界**,不是漏网 ——
// 真有业务数据从那儿上 wire,那是另一个洞(而且今天没有)。
//
// # ⚠️ **账本不是逃逸名单** ——
//
// 每一条都必须写清楚"为什么它可以绕过出口",而且**条数要对得上**:
// 同一个函数里再加一处 `c.JSON`,这条豁免就失效了(计数对不上 ⇒ 用例红)。
// 反过来,账本上写了却已经不存在的那一条也要红(过期条目)。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//   · 在 v1 里加一个 `c.JSON(200, rows)`            → 本用例红(没登记)
//   · 把 health.go 的一处 c.JSON 改成两处            → 本用例红(条数对不上)
//   · 把 allowlist 里的一条删掉                      → 本用例红(有写入点没登记)
//   · 把 BaseHandler.OK 里的 response.OKWithData 删掉 → 本用例红(出口自己绕过了)

package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ── 检测面 ────────────────────────────────────────────────────────────────

// hertzWriters *app.RequestContext 上"把字节写上 wire"的方法
//
// ⚠️ **只列"写 body"的,不含只读方法** ——
// `c.StatusCode()` / `c.Flush()` / `c.Response.Body()` 都不写 body,
// 混进来就是误报(一个天天误报的守卫,没人愿意跑第二次)。
var hertzWriters = map[string]bool{
	"JSON":        true,
	"String":      true,
	"Data":        true,
	"Render":      true,
	"Redirect":    true,
	"ProtoBuf":    true,
	"HTML":        true,
	"XML":         true,
	"Write":       true,
	"WriteString": true,
}

// hertzBodyWriters c.Response.* 上"把字节写上 wire"的方法
//
// ⚠️ **不含 SetStatusCode** —— 它只改状态码,body 仍是空的。
var hertzBodyWriters = map[string]bool{
	"SetBody":              true,
	"SetBodyRaw":           true,
	"SetBodyString":        true,
	"AppendBody":           true,
	"AppendBodyString":     true,
	"SetBodyStream":        true,
	"SetBodyStreamNoReset": true,
	"ConstructBodyStream":  true,
}

// responseWriters pkg/response 里**带业务数据**的出口
//
// ⚠️ **Fail / FailWithData 不在列** ——
// 错误响应不承载业务行,那是 `h.Fail` 这条**正当**的失败出口。
// (FailWithData 的 data 是校验错误明细,不是业务行的投影。)
var responseWriters = map[string]bool{
	"OKWithData":      true,
	"OKWithRawData":   true,
	"OKWithMessage":   true,
	"Page":            true,
	"PageWithRawList": true,
}

// outletNames BaseHandler 上**承载业务数据**的出口
//
// ⚠️ **这五个名字同时也是"豁免规则"的一部分** ——
// pkg/response 的业务出口调用,**只在这五个函数体内**才算正当。
// 改名字 ⇒ 豁免失效 ⇒ 守卫转红(这是对的:出口改名必须同步改这里)。
var outletNames = map[string]bool{
	"OK":            true,
	"OKWithMessage": true,
	"OKID":          true,
	"Page":          true,
	"PageWith":      true,
}

// ── 账本 ──────────────────────────────────────────────────────────────────

// outletBypass 一条"允许绕过出口"的登记
type outletBypass struct {
	// File 相对 internal/api/ 的路径(用 / 分隔)
	File string
	// Func 所在的最外层函数名(匿名函数算在它外层那个函数里)
	Func string
	// Writer 写响应的方法,例:"ctx.JSON" / "ctx.Response.SetBody" / "fmt.Fprintf"
	Writer string
	// Count 期望出现次数 —— **不是"至少一次"**
	Count int
	// Reason 为什么它可以绕过出口
	Reason string
}

// outletBypassAllowlist 允许绕过响应出口的写入点 —— **账本,不是逃逸名单**
//
// ⚠️ **每一条都必须写清楚理由** —— 一条没有理由的豁免 = 一个没人看过的洞。
//
// ⚠️ **Count 是精确期望值** —— 同一个函数里再加一处写入也要被发现。
var outletBypassAllowlist = []outletBypass{
	{
		File: "handler/v1/health.go", Func: "Liveness", Writer: "ctx.JSON", Count: 1,
		Reason: "健康检查:body 只有 {\"status\":\"ok\"},没有任何业务行 ⇒ 字段级规则没有作用对象",
	},
	{
		File: "handler/v1/health.go", Func: "Readiness", Writer: "ctx.JSON", Count: 1,
		Reason: "就绪探针:body 只有依赖状态(db/redis),没有任何业务行",
	},
	{
		File: "handler/v1/file.go", Func: "Content", Writer: "ctx.Response.SetBodyStream", Count: 2,
		Reason: "文件字节流(二进制),不是 JSON 业务行 ⇒ 字段级规则作用不到它身上;" +
			"两处分别是 206 分段(rangePartial)与 200 全量(default)",
	},
	{
		File: "handler/v1/voice.go", Func: "Convert", Writer: "ctx.Write", Count: 1,
		Reason: "TTS 整段合成的音频字节(audio/wav|mpeg 二进制),不是 JSON 业务行 ⇒ 字段级规则作用不到;" +
			"包进统一响应体会变成一个巨大的 base64 字符串,下载/播放都坏",
	},
	{
		File: "handler/v1/voice.go", Func: "convertStream", Writer: "ctx.Write", Count: 1,
		Reason: "TTS 流式合成的 chunked 音频转发(循环里唯一一处写入点)," +
			"同样是二进制音频流而非 JSON 业务行;错误都发生在首字节写出之前",
	},
	{
		File: "handler/v1/event.go", Func: "Stream", Writer: "fmt.Fprint", Count: 1,
		Reason: "SSE 心跳行(:heartbeat),没有任何数据",
	},
	{
		File: "handler/v1/event.go", Func: "writeSSE", Writer: "fmt.Fprintf", Count: 1,
		Reason: "SSE 事件行 —— ⚠️ 载荷**只有信号**(站内信未读增量 / 审批动作名 / " +
			"字典 group_code),不含业务行;⚠️ 若将来往 payload 里塞业务行," +
			"这一条就失效了,必须改走出口",
	},
	{
		File: "handler/v1/event.go", Func: "writeSSERetry", Writer: "fmt.Fprintf", Count: 1,
		Reason: "SSE 重连指令(retry:),没有数据",
	},
	{
		File: "router/router.go", Func: "Register", Writer: "ctx.JSON", Count: 1,
		Reason: "/version 构建信息快照(app_name/version/commit/build_time/...),没有业务行",
	},
	{
		File: "middleware/bodylimit.go", Func: "respondTooLarge", Writer: "ctx.Response.SetBody", Count: 1,
		Reason: "413 请求体过大的错误体 —— 错误响应不承载业务行",
	},
	{
		File: "middleware/idempotency.go", Func: "idempotencyWith", Writer: "ctx.Response.SetBody", Count: 2,
		Reason: "两处都是幂等中间件的响应:409 冲突体 + 命中缓存的原样重放" +
			"(那份 body 是 c.Next 之后才抓的,**已经过出口脱敏**)",
	},
	{
		File: "middleware/gzip.go", Func: "Gzip", Writer: "ctx.Response.SetBody", Count: 1,
		Reason: "把**已经写好的**响应体换成压缩后的字节 —— 不是另写一份,是同一份的压缩形态",
	},
	{
		File: "handler/v1/dyn.go", Func: "ExportRecords", Writer: "ctx.Data", Count: 1,
		Reason: "CSV 全量导出(流式附件,Content-Disposition 强制下载)—— 载荷不是 JSON " +
			"业务行,字段级脱敏出口(h.OK/h.Page)本来就不适用;" +
			"⚠️ 列敏感信息的脱敏在 service 导出链里做,若将来改走 JSON 必须**撤掉本条**",
	},
}

// ── 扫描 ──────────────────────────────────────────────────────────────────

// bypassKey 一个写入点的身份
type bypassKey struct {
	File   string
	Func   string
	Writer string
}

// bypassSite 一处写入点(带行号,报错要能直接跳过去)
type bypassSite struct {
	bypassKey
	Line int
}

// apiRoot 扫描根 = internal/api/
//
// ⚠️ **认一下再扫** —— 路径写错的话会扫到一个空目录,
// 守卫会"全绿"而实际什么都没扫(假绿)。
func apiRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	require.NoError(t, err)
	for _, d := range []string{"handler", "middleware", "router"} {
		st, err := os.Stat(filepath.Join(root, d))
		require.NoError(t, err, "internal/api 下必须有 %s 子目录(扫错了目录 ⇒ 守卫假绿)", d)
		require.True(t, st.IsDir(), "internal/api/%s 必须是目录", d)
	}
	return root
}

// goFiles 收集目录树下的非测试 .go 文件
func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

// importAlias 取某个 import 路径在**本文件里**的本地名
//
// ⚠️ **不能用固定的 "app" / "response"** ——
// 别名可以改(goimports 有时会改),写死了就漏判。
func importAlias(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		// 默认本地名 = 路径最后一段
		parts := strings.Split(path, "/")
		return parts[len(parts)-1]
	}
	return ""
}

// isRequestContextType 判断一个类型是不是 *app.RequestContext
func isRequestContextType(expr ast.Expr, appAlias string) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return x.Name == appAlias && sel.Sel.Name == "RequestContext"
}

// collectCtxVars 找出**这个函数里**的 *app.RequestContext 变量
//
// ⚠️ **靠类型找,不靠变量名** ——
// 变量名叫 c / ctx / rc 都行,写死名字就会漏。
//
// ⚠️ **为什么按函数收,而不是按文件收** ——
// 按文件收的话,同一个文件里另一个函数的 `c`(**别的类型**)也会被算进来,
// 于是那边任何 `.Data(...)` / `.JSON(...)` 都会被误判成"写响应"。
// 误报比漏报更致命 —— 一个天天误报的守卫,没人愿意跑第二次。
func collectCtxVars(fd *ast.FuncDecl, pkgLevel map[string]bool, appAlias string) map[string]bool {
	vars := map[string]bool{}
	for k := range pkgLevel {
		vars[k] = true
	}
	if appAlias == "" {
		return vars
	}
	mark := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, fld := range fields.List {
			if !isRequestContextType(fld.Type, appAlias) {
				continue
			}
			for _, name := range fld.Names {
				vars[name.Name] = true
			}
		}
	}
	mark(fd.Type.Params)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.FuncLit:
			mark(d.Type.Params)
		case *ast.ValueSpec:
			if isRequestContextType(d.Type, appAlias) {
				for _, name := range d.Names {
					vars[name.Name] = true
				}
			}
		}
		return true
	})
	return vars
}

// pkgLevelCtxVars 文件级(包级)声明的 *app.RequestContext 变量
func pkgLevelCtxVars(f *ast.File, appAlias string) map[string]bool {
	vars := map[string]bool{}
	if appAlias == "" {
		return vars
	}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || !isRequestContextType(vs.Type, appAlias) {
				continue
			}
			for _, name := range vs.Names {
				vars[name.Name] = true
			}
		}
	}
	return vars
}

// isOutletMethod 判断一个方法是不是 BaseHandler 的五个出口之一
func isOutletMethod(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return false
	}
	star, ok := fd.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "BaseHandler" && outletNames[fd.Name.Name]
}

// scanFile 扫一个文件,返回它里面所有"绕过出口的写入点"
func scanFile(t *testing.T, path, rel string) []bypassSite {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	require.NoError(t, err, "%s 解析失败", rel)

	appAlias := importAlias(f, "github.com/cloudwego/hertz/pkg/app")
	respAlias := importAlias(f, "rapid-admin/internal/pkg/response")
	fmtAlias := importAlias(f, "fmt")
	ioAlias := importAlias(f, "io")
	ctxVarsPkg := pkgLevelCtxVars(f, appAlias)

	var out []bypassSite
	record := func(fn, writer string, pos token.Pos) {
		out = append(out, bypassSite{
			bypassKey: bypassKey{File: rel, Func: fn, Writer: writer},
			Line:      fset.Position(pos).Line,
		})
	}

	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fnName := fd.Name.Name
		// ⚠️ **豁免只对 pkg/response 的业务出口生效,且只在出口函数体内** ——
		// 别的地方调 response.OKWithData 依然是绕过(字段层要脱敏,那里没有)。
		inOutlet := isOutletMethod(fd)
		// ⚠️ **ctx 变量按函数收** —— 见 collectCtxVars 的注释。
		ctxVars := collectCtxVars(fd, ctxVarsPkg, appAlias)

		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			// c.Response.SetBody(...) / c.Response.SetBodyStream(...)
			if inner, ok := sel.X.(*ast.SelectorExpr); ok {
				base, ok := inner.X.(*ast.Ident)
				if ok && ctxVars[base.Name] && inner.Sel.Name == "Response" &&
					hertzBodyWriters[sel.Sel.Name] {
					record(fnName, "ctx.Response."+sel.Sel.Name, call.Pos())
				}
				return true
			}

			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}

			// c.JSON(...) / c.String(...) …
			if ctxVars[x.Name] && hertzWriters[sel.Sel.Name] {
				record(fnName, "ctx."+sel.Sel.Name, call.Pos())
				return true
			}

			// response.OKWithData(...) …
			if respAlias != "" && x.Name == respAlias && responseWriters[sel.Sel.Name] {
				if !inOutlet {
					record(fnName, "response."+sel.Sel.Name, call.Pos())
				}
				return true
			}

			// fmt.Fprintf(c, ...) / io.Copy(c, ...) ——
			// *app.RequestContext 实现了 io.Writer,所以它能被当成输出流用。
			// ⚠️ 这条**很容易漏**:SSE 就是这么写的(fmt.Fprintf(c, "event: ..."))。
			if len(call.Args) == 0 {
				return true
			}
			first, ok := call.Args[0].(*ast.Ident)
			if !ok || !ctxVars[first.Name] {
				return true
			}
			if fmtAlias != "" && x.Name == fmtAlias {
				switch sel.Sel.Name {
				case "Fprint", "Fprintf", "Fprintln":
					record(fnName, "fmt."+sel.Sel.Name, call.Pos())
				}
				return true
			}
			if ioAlias != "" && x.Name == ioAlias {
				switch sel.Sel.Name {
				case "Copy", "CopyBuffer":
					record(fnName, "io."+sel.Sel.Name, call.Pos())
				}
			}
			return true
		})
	}
	return out
}

// scanAll 扫整棵 internal/api/ 树
func scanAll(t *testing.T) []bypassSite {
	t.Helper()
	root := apiRoot(t)
	var out []bypassSite
	for _, path := range goFiles(t, root) {
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)
		out = append(out, scanFile(t, path, rel)...)
	}
	return out
}

// ── 断言 ──────────────────────────────────────────────────────────────────

// TestResponseOutlets_绕过出口的写入点必须登记在账本上
//
// ⚠️ **两条方向都要查** ——
//
//	① 扫出来的每一处,账本上必须有(否则:新写的 handler 悄悄绕过了出口)
//	② 账本上的每一处,必须真的还在、条数对得上(否则:过期条目会让人以为有人管过)
//
// 只查①的话,账本会慢慢变成"什么都豁免"的垃圾桶;
// 只查②的话,新洞没人拦。
func TestResponseOutlets_绕过出口的写入点必须登记在账本上(t *testing.T) {
	sites := scanAll(t)

	got := map[bypassKey][]int{}
	for _, s := range sites {
		got[s.bypassKey] = append(got[s.bypassKey], s.Line)
	}

	ledger := map[bypassKey]outletBypass{}
	for _, e := range outletBypassAllowlist {
		k := bypassKey{File: e.File, Func: e.Func, Writer: e.Writer}
		require.NotContains(t, ledger, k,
			"账本里有两条一样的记录:%s / %s / %s —— 合并成一条,Count 写对", e.File, e.Func, e.Writer)
		require.NotEmpty(t, strings.TrimSpace(e.Reason),
			"账本条目 %s / %s / %s 没写理由 —— 一条没有理由的豁免 = 一个没人看过的洞",
			e.File, e.Func, e.Writer)
		ledger[k] = e
	}

	// ① 扫出来的每一处都要在账本里
	keys := make([]bypassKey, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].File != keys[j].File {
			return keys[i].File < keys[j].File
		}
		if keys[i].Func != keys[j].Func {
			return keys[i].Func < keys[j].Func
		}
		return keys[i].Writer < keys[j].Writer
	})
	for _, k := range keys {
		lines := got[k]
		e, ok := ledger[k]
		if !ok {
			t.Errorf("%s:%d %s() 直接写了响应(%s),没走 BaseHandler 的出口 ⇒ "+
				"该接口的响应**完全不过字段级脱敏**。\n"+
				"    要么改走 h.OK / h.OKWithMessage / h.OKID / h.Page / h.PageWith;"+
				"要么在 outletBypassAllowlist 里登记一条,并写清楚为什么它可以绕过。",
				k.File, lines[0], k.Func, k.Writer)
			continue
		}
		if len(lines) != e.Count {
			t.Errorf("%s %s() 的 %s 出现 %d 次,账本上登记的是 %d 次 ⇒ "+
				"账本与代码对不上(多出来的那处没人看过,或者少登记的那处已经不在了)。\n"+
				"    实际位置:%v", k.File, k.Func, k.Writer, len(lines), e.Count, lines)
		}
	}

	// ② 账本上的每一处必须真的还在(过期条目也要红)
	for _, e := range outletBypassAllowlist {
		k := bypassKey{File: e.File, Func: e.Func, Writer: e.Writer}
		if _, ok := got[k]; !ok {
			t.Errorf("账本条目 %s / %s / %s 在代码里已经找不到了 ⇒ "+
				"过期豁免:要么代码改了(把这条删掉),要么它藏在**扫不到的地方**"+
				"(守卫漏判了 ⇒ 那才是真问题)", e.File, e.Func, e.Writer)
		}
	}
}
