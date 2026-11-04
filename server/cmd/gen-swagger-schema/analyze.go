// gen-swagger-schema —— 静态分析器:从源码里抽出「路由 handler → 请求体 DTO」
//
// 为什么需要它:OpenAPI 的 requestBody 要有字段级 schema,就得知道每条写接口
// 到底收什么 DTO。这个信息**只存在于 handler 源码里**(var req service.XxxRequest
// + h.BindJSON(c, &req)),没有任何一张表、任何一份配置记着它。
//
// 所以这里直接读源码 —— 不引 x/tools/go/packages(理由同 internal/pkg/jsonid:
// 不想为一个工具把 golang.org/x/tools 提成直接依赖),代价是类型判定按**名字**猜。
//
// 产出两样东西:
//
//	① 路由 handler → DTO 类型表达式(编译期由 Go 编译器兜底)
//	② DTO 类型 → 字段说明(取自结构体字段的行尾注释)
//
// ② 的注释来源很关键:本仓 DTO 的字段语义**只写在行尾注释里**
// (例:`RoleCode string `json:"role_code"` // 必填,如 "role_owner_manager"`),
// 没有集中的字段字典 —— 不把注释搬进 Swagger UI,字段契约就还是半张。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// pkgInfo 一个包的解析结果
type pkgInfo struct {
	alias string         // 组件名前缀(import 路径最后一段),如 "service"
	path  string         // 包目录(绝对路径)
	fset  *token.FileSet // 解析用的 FileSet
	files []*ast.File    // 包内全部非测试文件
	// types 类型名 → 结构体定义(只收 struct)
	types map[string]*ast.StructType
	// typeOrder 类型名的稳定顺序(生成文件要可复现)
	typeOrder []string
}

// loadPkg 解析一个目录下的全部 .go 文件(跳过 _test.go)
func loadPkg(dir, alias string) (*pkgInfo, error) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读目录 %s: %w", dir, err)
	}
	pi := &pkgInfo{
		alias: alias,
		path:  dir,
		fset:  fset,
		types: map[string]*ast.StructType{},
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("解析 %s: %w", name, err)
		}
		pi.files = append(pi.files, f)
		pi.collectTypes(f)
	}
	return pi, nil
}

// collectTypes 收下文件里定义的全部具名 struct
func (pi *pkgInfo) collectTypes(f *ast.File) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			if _, dup := pi.types[ts.Name.Name]; dup {
				continue // 同名类型(跨文件)取第一个,生成物里以先到者为准
			}
			pi.types[ts.Name.Name] = st
			pi.typeOrder = append(pi.typeOrder, ts.Name.Name)
		}
	}
}

// fileImports 取一个文件里"限定名 → import 路径"的映射
//
// 只收本仓内的包(路径以 "rapid-admin/" 开头)—— 第三方包的类型不进组件表
// (它们由 swaggerx 的通用规则处理,见 schema.go)。
func fileImports(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		path, err := importPath(imp)
		if err != nil || !strings.HasPrefix(path, "rapid-admin/") {
			continue
		}
		alias := pathElem(path)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		out[alias] = path
	}
	return out
}

// importPath 取 import 字面量的路径
func importPath(imp *ast.ImportSpec) (string, error) {
	if imp.Path == nil {
		return "", fmt.Errorf("import 没有路径")
	}
	return strings.Trim(imp.Path.Value, `"`), nil
}

// pathElem 取 import 路径最后一段(组件名前缀)
func pathElem(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// dtoRef 一个请求体 DTO 的引用
type dtoRef struct {
	// expr 是可编译的 Go 类型表达式(限定名照抄源码里的写法)
	expr string
	// compKey 组件名(如 "service.CreateUserRequest"),运行期由 reflect 复算
	compKey string
	// qualifier 生成物里用的限定名(照抄源码的 import 别名);本包类型为空
	qualifier string
	// importPath qualifier 对应的 import 路径(生成物照抄)
	importPath string
	// pkg 组件名里的包前缀(如 "service")
	pkg string
	// typeName 类型名(如 "CreateUserRequest")
	typeName string
	// local 是否是本包(生成文件所在包)内的类型 —— 本地类型不需要 import
	local bool
	// inlineDecl 非空 = 生成文件里要额外声明这个类型(源码里的匿名 struct)
	inlineDecl string
	// inlineStruct 匿名 struct 的定义(用于抽字段说明)
	inlineStruct *ast.StructType
	// extraImports 这个类型额外需要的 import(限定名 → import 路径)
	//
	// 匿名 struct 的字段里可能引用别的包(如 jsonid.IDList),
	// 那条 import 也要搬进生成文件 —— 漏了生成文件就编译不过。
	extraImports map[string]string
}

// handlerDTO 扫描 handler 包,产出「handler 方法 → 请求体 DTO」
//
// key 形如 "(*UserHandler).Create" —— 运行期由 hertz 路由表的
// utils.NameOfFunction(handlerFunc) 推出来(去掉包路径前缀与 -fm 后缀),
// 两边必须逐字对得上,否则 schema 挂不到路由上。
func handlerDTOs(hp *pkgInfo) (dtos map[string]*dtoRef, problems []string) {
	dtos = map[string]*dtoRef{}
	inlineSeq := 0

	for _, f := range hp.files {
		imports := fileImports(f)
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) == 0 {
				continue
			}
			recv := recvTypeName(fd.Recv.List[0].Type)
			if recv == "" {
				continue
			}
			key := fmt.Sprintf("(*%s).%s", recv, fd.Name.Name)

			exprs := boundTypes(fd)
			if len(exprs) == 0 {
				continue
			}
			if len(exprs) > 1 {
				problems = append(problems, fmt.Sprintf(
					"%s 绑定了多个不同 DTO(%s),跳过 —— 猜不出该用哪个",
					key, strings.Join(exprTexts(exprs, hp.fset), " / ")))
				continue
			}
			ref, err := resolveExpr(exprs[0], imports, hp.alias, hp.fset, &inlineSeq)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", key, err))
				continue
			}
			dtos[key] = ref
		}
	}
	return dtos, problems
}

// recvTypeName 取接收者类型名(只认指针接收者与值接收者)
func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr: // 泛型接收者
		return recvTypeName(t.X)
	}
	return ""
}

// boundTypes 找出 handler 里 BindJSON / BindAndValidate 绑定的类型表达式
//
// 认两种写法:
//
//	var req service.XxxRequest    → 声明里拿类型
//	var req = service.XxxRequest{} → 从初始化值里拿类型
//
// 一个方法里绑了多个**不同**类型时返回多个 —— 调用方据此判定"猜不准",跳过。
func boundTypes(fd *ast.FuncDecl) []ast.Expr {
	// 先收 var 声明:name → 类型表达式
	declared := map[string]ast.Expr{}
	ast.Inspect(fd, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			return true
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			typ := vs.Type
			if typ == nil && len(vs.Values) == 1 {
				if cl, ok := vs.Values[0].(*ast.CompositeLit); ok {
					typ = cl.Type
				}
			}
			if typ == nil {
				continue
			}
			for _, name := range vs.Names {
				declared[name.Name] = typ
			}
		}
		return true
	})

	var out []ast.Expr
	seen := map[string]bool{}
	ast.Inspect(fd, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "BindJSON" && sel.Sel.Name != "BindAndValidate" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		un, ok := call.Args[1].(*ast.UnaryExpr)
		if !ok || un.Op != token.AND {
			return true // 绑的不是 &ident(比如 &body.Field),放弃
		}
		id, ok := un.X.(*ast.Ident)
		if !ok {
			return true
		}
		typ, ok := declared[id.Name]
		if !ok {
			return true
		}
		text := exprText(typ, nil)
		if text == "" || seen[text] {
			return true
		}
		seen[text] = true
		out = append(out, typ)
		return true
	})
	return out
}

// exprTexts 渲染一组类型表达式(给报错信息用)
func exprTexts(exprs []ast.Expr, fset *token.FileSet) []string {
	out := make([]string, 0, len(exprs))
	for _, e := range exprs {
		out = append(out, oneLine(exprText(e, fset)))
	}
	return out
}

// exprText 把一个类型表达式渲染成源码文本
func exprText(expr ast.Expr, fset *token.FileSet) string {
	return nodeText(expr, fset)
}

// nodeText 把任意 AST 节点渲染成源码文本
func nodeText(node ast.Node, fset *token.FileSet) string {
	if fset == nil {
		fset = token.NewFileSet()
	}
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, node); err != nil {
		return ""
	}
	return sb.String()
}

// oneLine 把多行文本压成一行(报错信息进注释时用)
func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// resolveExpr 把 handler 源码里的类型表达式解析成组件引用
//
// 规则:
//   - 匿名 struct:在生成文件里声明一个新类型(swaggerInlineN),再引用它
//   - 限定名(pkg.Type):pkg 走 import 表 → import 路径最后一段 = 组件名前缀
//   - 无限定名(Type):本包内的类型
//   - 指针类型:*T → 组件名取 T,运行期由 reflect 解引用
func resolveExpr(expr ast.Expr, imports map[string]string, selfAlias string,
	fset *token.FileSet, inlineSeq *int) (*dtoRef, error) {
	switch t := expr.(type) {
	case *ast.StructType:
		// 匿名 struct:源码里就地写的结构体字面量 —— 生成文件里声明一个新类型
		*inlineSeq++
		name := fmt.Sprintf("swaggerInline%d", *inlineSeq)
		ts := &ast.TypeSpec{Name: ast.NewIdent(name), Type: t}
		// ⚠️ 要打印 GenDecl 而不是 TypeSpec —— TypeSpec 只印 "名字 + 类型",
		//   少了 "type" 关键字,生成文件编译不过。
		gd := &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{ts}}
		decl := nodeText(gd, fset)
		return &dtoRef{
			expr:         name + "{}",
			compKey:      selfAlias + "." + name,
			pkg:          selfAlias,
			typeName:     name,
			local:        true,
			inlineDecl:   decl,
			inlineStruct: t,
			extraImports: collectQualifiers(t, imports),
		}, nil

	case *ast.StarExpr:
		ref, err := resolveExpr(t.X, imports, selfAlias, fset, inlineSeq)
		if err != nil {
			return nil, err
		}
		// 指针类型:new(T) 造值,运行期 reflect 解引用
		ref.expr = "new(" + strings.TrimSuffix(strings.TrimPrefix(ref.expr, "new("), "{}") + ")"
		if ref.inlineDecl != "" {
			ref.expr = "new(" + ref.typeName + ")"
		}
		return ref, nil

	case *ast.SelectorExpr:
		pkgID, ok := t.X.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("看不懂的类型 %s", oneLine(exprText(expr, fset)))
		}
		path, ok := imports[pkgID.Name]
		if !ok {
			return nil, fmt.Errorf("未知包限定名 %s", pkgID.Name)
		}
		compPkg := pathElem(path)
		return &dtoRef{
			expr:       pkgID.Name + "." + t.Sel.Name + "{}",
			compKey:    compPkg + "." + t.Sel.Name,
			qualifier:  pkgID.Name,
			importPath: path,
			pkg:        compPkg,
			typeName:   t.Sel.Name,
		}, nil

	case *ast.Ident:
		return &dtoRef{
			expr:     t.Name + "{}",
			compKey:  selfAlias + "." + t.Name,
			pkg:      selfAlias,
			typeName: t.Name,
			local:    true,
		}, nil
	}
	return nil, fmt.Errorf("暂不支持的类型 %s", oneLine(exprText(expr, fset)))
}

// collectQualifiers 收下结构体字段里用到的包限定名(限定名 → import 路径)
//
// 匿名 struct 被搬进生成文件时,它的字段类型可能引用别的包
// (例:`IdentityIDs jsonid.IDList`)。那条 import 也得一起搬 ——
// 漏了生成文件就编译不过。
func collectQualifiers(st *ast.StructType, imports map[string]string) map[string]string {
	out := map[string]string{}
	if st == nil {
		return out
	}
	ast.Inspect(st, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if path, ok := imports[id.Name]; ok {
			out[id.Name] = path
		}
		return true
	})
	return out
}

// structDocs 取一个结构体的字段说明表(json 名 → 行尾注释)
//
// ⚠️ **注释来源是字段的 Doc / Comment** —— 本仓 DTO 的字段语义就写在行尾
// (`// 必填,如 "role_owner_manager"`),没有集中的字段字典。
// 注释为空的字段不进表(别塞空串占位 —— 那会让"有没有说明"这件事看不出来)。
func structDocs(st *ast.StructType) map[string]string {
	out := map[string]string{}
	if st == nil {
		return out
	}
	for _, f := range st.Fields.List {
		name, ok := jsonNameOf(f)
		if !ok {
			continue
		}
		doc := commentText(f)
		if doc == "" {
			continue
		}
		out[name] = doc
	}
	return out
}

// jsonNameOf 取字段的 json 名(没有 json tag 时用 Go 字段名)
//
// `json:"-"` 的字段跳过(不参与序列化 —— 密码哈希、密文这类)。
func jsonNameOf(f *ast.Field) (string, bool) {
	tag := ""
	if f.Tag != nil {
		tag = strings.Trim(f.Tag.Value, "`")
	}
	tags := parseStructTag(tag)
	if v, ok := tags["json"]; ok {
		name := v
		if c := strings.Index(v, ","); c >= 0 {
			name = v[:c]
		}
		if name == "-" {
			return "", false
		}
		if name == "" {
			// `json:",omitempty"` —— 用 Go 字段名
			if len(f.Names) > 0 {
				return f.Names[0].Name, true
			}
			return "", false
		}
		return name, true
	}
	if len(f.Names) > 0 {
		return f.Names[0].Name, true
	}
	return "", false
}

// parseStructTag 把结构体标签拆成键值对
//
// 与 reflect.StructTag 的解析规则一致:`json:"a,omitempty" binding:"required"`
// → {"json": "a,omitempty", "binding": "required"}。
func parseStructTag(tag string) map[string]string {
	out := map[string]string{}
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		i = 0
		for i < len(tag) && tag[i] != ':' && tag[i] != ' ' && tag[i] != '"' {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := tag[:i]
		tag = tag[i+1:]
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		out[name] = tag[1:i]
		tag = tag[i+1:]
	}
	return out
}

// commentText 取字段的说明文字
//
// 优先用字段上方的文档注释(Doc),没有就用行尾注释(Comment)。
// 多行注释折成一行(空格连接),免得在 Swagger UI 里撑出一大块。
func commentText(f *ast.Field) string {
	raw := ""
	if f.Doc != nil {
		raw = f.Doc.Text()
	} else if f.Comment != nil {
		raw = f.Comment.Text()
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, " ")
}

// walkTypes 从一批根类型出发,沿字段类型往下走,收集所有涉及的具名 struct
//
// 目的:给嵌套类型也补上字段说明(比如 CreateUserRequest.Identities 的元素类型
// service.IdentityItem —— 它的字段说明在 service 包里,不在 handler 包里)。
//
// visited 防环(模型之间互相引用时会出现环)。
func walkTypes(pkgs map[string]*pkgInfo, roots []string,
	extra map[string]*ast.StructType, extraOwner *pkgInfo) map[string]*ast.StructType {
	out := map[string]*ast.StructType{}
	visited := map[string]bool{}
	queue := append([]string{}, roots...)
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		if visited[key] {
			continue
		}
		visited[key] = true
		st, ok := lookupType(pkgs, extra, key)
		if !ok {
			continue
		}
		out[key] = st
		owner := ownerOf(pkgs, extraOwner, key)
		for _, t := range nestedTypeKeys(st, owner) {
			if !visited[t] {
				queue = append(queue, t)
			}
		}
	}
	return out
}

// lookupType 按组件名找结构体定义
func lookupType(pkgs map[string]*pkgInfo, extra map[string]*ast.StructType,
	compKey string) (*ast.StructType, bool) {
	if st, ok := extra[compKey]; ok {
		return st, true
	}
	i := strings.LastIndex(compKey, ".")
	if i < 0 {
		return nil, false
	}
	pi, ok := pkgs[compKey[:i]]
	if !ok {
		return nil, false
	}
	st, ok := pi.types[compKey[i+1:]]
	return st, ok
}

// ownerOf 取组件名所属的包(用于解析包内的无限定名引用)
func ownerOf(pkgs map[string]*pkgInfo, extraOwner *pkgInfo, compKey string) *pkgInfo {
	if extraOwner != nil && strings.HasPrefix(compKey, extraOwner.alias+".") {
		return extraOwner
	}
	i := strings.LastIndex(compKey, ".")
	if i < 0 {
		return extraOwner
	}
	if pi, ok := pkgs[compKey[:i]]; ok {
		return pi
	}
	return extraOwner
}

// nestedTypeKeys 取一个结构体里引用到的其它具名 struct 的组件名
func nestedTypeKeys(st *ast.StructType, owner *pkgInfo) []string {
	if st == nil {
		return nil
	}
	var out []string
	for _, f := range st.Fields.List {
		if f.Type == nil {
			continue
		}
		if key, ok := nestedKey(f.Type, owner); ok {
			out = append(out, key)
		}
	}
	return out
}

// nestedKey 把一个字段类型表达式解析成组件名(解析不了就返回 false)
func nestedKey(expr ast.Expr, owner *pkgInfo) (string, bool) {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return nestedKey(t.X, owner)
	case *ast.ArrayType:
		return nestedKey(t.Elt, owner)
	case *ast.MapType:
		return nestedKey(t.Value, owner)
	case *ast.Ident:
		if owner == nil {
			return "", false
		}
		return owner.alias + "." + t.Name, true
	case *ast.SelectorExpr:
		pkg, ok := t.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		return pkg.Name + "." + t.Sel.Name, true
	}
	return "", false
}
