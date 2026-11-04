// 全仓扫描器:找出"整数类型 + id 类 json 名 + 没有 ,string"的字段。
//
// 为什么需要它:ID 序列化改造是**分模块推进**的,几百个字段不可能一次改完。
// 靠人记不住、靠 review 也拦不住 —— 上线新接口时随手写个 `json:"user_id"`
// 就又漏一个,而漏掉的表现是"偶尔查不到那条数据",最难查的那种。
//
// 所以做成棘轮(ratchet):
//   - 未改造完的字段登记进 allowlist.txt,带着一行说明为什么还没改
//   - 新增违规 → 测试红(这是它主要的活)
//   - 已改完的字段若还留在 allowlist → 也红(防止清单永远只增不减)
//   - 每次改造提交顺手删一行,清单单调收缩直到清空并删文件
//
// 用 stdlib go/ast 而不是 x/tools/go/packages:不想为一个测试把
// golang.org/x/tools 从 indirect 提成直接依赖。代价是类型判定按**名字**猜
// (见 integerish),不解析类型别名链 —— 对这个用途够用,而且误报方向是
// "多查",不会漏。
package jsonid

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"

	"strings"
)

// AllowlistFile 未改造完的字段清单(相对本包目录)
const AllowlistFile = "id_tag_allowlist.txt"

// Violation 一个 json tag 违规
type Violation struct {
	File string // 相对仓库根的路径
	Line int
	// Struct 类型名
	Struct string
	// Field 字段名
	Field string
	// JSON json 名
	JSONName string
	// GoType 字段的 Go 类型写法
	GoType string
	// Tagged 是否已经打了 ,string
	Tagged bool
}

// Key 稳定标识一个违规(allowlist 用)
func (v Violation) Key() string {
	return fmt.Sprintf("%s %s.%s json=%q", v.File, v.Struct, v.Field, v.JSONName)
}

// Fix 给出这一条该怎么改 —— 直接抄进 tag 就行,不用自己想。
//
// 标量和切片走**完全不同的两条路**,这是最容易搞错的地方:
//
//	标量  json:"user_id"  →  json:"user_id,string"
//	      内置选项,不碰 Go 类型,GORM / service 层一行都不用改。235 条里绝大多数。
//
//	切片  json:"identity_ids"  →  json:"identity_ids"  +  字段类型改成 jsonid.IDList
//	      内置 ,string 对数组**无效**(标准库只支持 string/float/int/bool 标量),
//	      必须换真类型。
func (v Violation) Fix() string {
	if isSliceName(v.GoType) {
		return fmt.Sprintf("%s → jsonid.IDList", v.GoType)
	}
	if v.Tagged {
		// 切片打了 ,string 但无效,还是得换类型
		return fmt.Sprintf("%s + ,string(无效) → jsonid.IDList", v.GoType)
	}
	return fmt.Sprintf(`%s → 加 ,string`, v.GoType)
}

// String allowlist 里的行格式
func (v Violation) String() string {
	return fmt.Sprintf("%s %s.%s json=%q  # %s",
		v.File, v.Struct, v.Field, v.JSONName, v.Fix())
}

func isSliceName(typeName string) bool { return len(typeName) > 2 && typeName[:2] == "[]" }

// integerish 按名字判定"这是不是整数类型"。
//
// 故意保守:只认常见整型 + jsonid 自己的类型。认错了就是一次误报,
// 误报会被写进 allowlist,不会漏。
var integerish = map[string]bool{
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"ID": true, "IDList": true,
}

// looksLikeIDField 判断 json 名是不是"主键/外键"语义。
//
// 排除掉两类:
//   - 明显不是 ID 的:captcha_id(验证码令牌,本来就是随机字符串)、
//     trace_id / request_id(链路追踪)、open_id(微信 unionid 类)
//   - 复数批量:交给 IDList,判定逻辑一样但类型不同
//
// `_by` 后缀(created_by / review_by / last_changed_by …)也是 ID —— 都是
// 操作人 user_id,同样会溢出。漏掉这类的教训:第一版规则只认 `_id`,结果
// 全仓 22 个"谁干的"字段一个没扫到,而它们和 id 的精度问题一模一样。
//
// 同名的非 ID 字段(order_by 是排序列名、last_changed_by_name 是展示名)
// 靠 integerish 的**类型**判定自动排除 —— 它们是 string。
func looksLikeIDField(name string) bool {
	switch name {
	case "captcha_id", "trace_id", "request_id", "open_id", "key_id", "handler_id":
		return false
	}
	return name == "id" ||
		strings.HasSuffix(name, "_id") ||
		strings.HasSuffix(name, "_ids") ||
		strings.HasSuffix(name, "_by")
}

// ScanDir 递归扫描 .go 文件,返回违规列表(已排序)
func ScanDir(root string) ([]Violation, error) {
	var out []Violation
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		// 清单行给的是 "internal/xxx.go" 这种 server 内的相对路径 —— 够定位,
		// 又不会因为仓库目录改名(仓库放在哪个目录都不影响)而失效
		rel, relErr := filepath.Rel(filepath.Dir(root), path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if len(field.Names) == 0 || field.Tag == nil {
					continue // 匿名嵌入 / 无 tag
				}
				name, tagged, ok := parseJSONTag(field.Tag.Value)
				if !ok || !looksLikeIDField(name) {
					continue
				}
				typeName := exprTypeName(field.Type)
				isSlice := isSliceType(field.Type)
				// 切片按元素类型判定 —— 批量 ID 同样是 ID 语义
				elemType := strings.TrimPrefix(typeName, "[]")
				if !integerish[elemType] {
					continue
				}
				// 已经是 jsonid.ID / jsonid.IDList(或 []jsonid.ID —— 每个元素
				// 自己 marshal 成字符串,同样是对的)
				if typeName == "ID" || typeName == "IDList" || elemType == "ID" {
					continue
				}
				// 标量打了 ,string 是合法的另一种解法,不该报;
				// 切片打了不算数(内置选项只支持标量)。
				if tagged && !isSlice {
					continue
				}
				out = append(out, Violation{
					File: rel, Line: fset.Position(field.Tag.Pos()).Line,
					Struct: structName(f, st), Field: field.Names[0].Name,
					JSONName: name, GoType: typeName, Tagged: tagged && !isSlice,
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// LoadAllowlist 读清单。文件不存在 = 已全部改造完,返回空。
func LoadAllowlist(p string) (map[string]string, error) {
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, found := strings.Cut(line, " # ")
		if !found {
			return nil, fmt.Errorf("allowlist 行缺少 ' # ' 说明(每行都要写为什么还没改): %s", line)
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return out, nil
}

// parseJSONTag 从 `json:"name,opt"` 里取出 name 与是否含 string 选项
func parseJSONTag(raw string) (name string, hasString bool, ok bool) {
	tag := strings.Trim(raw, "`")
	i := strings.Index(tag, `json:"`)
	if i < 0 {
		return "", false, false
	}
	rest := tag[i+len(`json:"`):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return "", false, false
	}
	parts := strings.Split(rest[:j], ",")
	if parts[0] == "-" || parts[0] == "" {
		return "", false, false
	}
	for _, p := range parts[1:] {
		if p == "string" {
			hasString = true
		}
	}
	return parts[0], hasString, true
}

func exprTypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprTypeName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.ArrayType:
		return "[]" + exprTypeName(t.Elt)
	case *ast.MapType:
		return "map"
	case *ast.InterfaceType:
		return "interface{}"
	default:
		return "?"
	}
}

func isSliceType(e ast.Expr) bool {
	_, ok := e.(*ast.ArrayType)
	return ok
}

// structName 找出这个结构体字面量属于哪个顶层类型声明
func structName(f *ast.File, st *ast.StructType) string {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name == nil {
				continue
			}
			if ts.Type == st {
				return ts.Name.Name
			}
		}
	}
	return "(匿名)"
}

// ParseAllowlistLine 供外部脚本用
func ParseAllowlistLine(line string) (key, val string, err error) {
	key, val, found := strings.Cut(strings.TrimSpace(line), " # ")
	if !found {
		return "", "", fmt.Errorf("缺少 ' # ' 说明: %s", line)
	}
	return strings.TrimSpace(key), strings.TrimSpace(val), nil
}

// RenderAllowlist 把违规渲染成清单文件内容(按 Key 去重)
//
// 去重的原因:同一个文件里经常有多个匿名请求 DTO 带同名字段(典型是每个
// handler 的 body struct 都有一份 HouseID)。Key 里不含行号 —— 行号会在
// 任何一次编辑后漂移,进了清单就永远对不上,棘轮会误报成"新增违规"。
// 代价是两个同名匿名结构体只记一条,改的时候按文件定位即可。
func RenderAllowlist(vs []Violation) string {
	var b strings.Builder
	b.WriteString("# 未完成 jsonid 改造的字段清单(棘轮,由 idtag_scan_test.go 维护)\n")
	b.WriteString("#\n")
	b.WriteString("#   每行 = <位置>  # <怎么改>\n")
	b.WriteString("#   新增违规让测试红;已改完的行还留着也红。\n")
	b.WriteString("#   改造一个删一行 —— 本文件只减不增,最终整体删除。\n")
	b.WriteString("#\n")
	b.WriteString("#   重新生成(会覆盖整个文件,只在建清单时用):\n")
	b.WriteString("#     UPDATE_ID_ALLOWLIST=1 go test ./internal/pkg/jsonid/ -run TestNoBareIntegerIDTags\n")
	b.WriteString("#\n")
	seen := map[string]bool{}
	n := 0
	for _, v := range vs {
		key := v.Key()
		if seen[key] {
			continue
		}
		seen[key] = true
		b.WriteString(v.String())
		b.WriteString("\n")
		n++
	}
	b.WriteString(fmt.Sprintf("\n# 共 %d 条(去重后)\n", n))
	return b.String()
}
