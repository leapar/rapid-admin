// swaggerx —— 字段级 OpenAPI schema 渲染(反射)
//
// 为什么不用 swaggo 注解:
//
//	swag init 要给每个 handler 加 6-10 行 @Summary/@Param/@Success 注释,
//	本仓 185+ 受保护路由,人工标注 ≈ 1500 行 + 一条额外工具链依赖。
//	而字段契约的**真相本来就在 Go 结构体里** —— json tag 说字段叫什么、
//	指针/omitempty 说它是不是可选、行尾注释说它是什么意思。
//	反射能把这三样直接读出来,注解只是把它们抄成第二份。
//
// 三条信息从哪来:
//
//	字段名 / 类型 / 可选性 → reflect(结构体定义)
//	字段语义(中文说明)   → 生成物 RequestFieldDocs(取自源码行尾注释)
//
// ⚠️ **必填性只认显式标签**(`validate:"required"` / `binding:"required"`)。
//
//	本仓 DTO 大多没有这两个标签,只有 json tag —— 而"没有 omitempty"
//	并不等于"必填"(CreateUserRequest.Nickname 就没写 omitempty,却是可选的)。
//	猜错的后果很具体:Swagger UI 会**拦着不让提交**没填的必填字段。
//	宁可少标必填(服务端会返回明确报错),也不要多标。
package swaggerx

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"time"
)

// timeType time.Time 的反射类型
var timeType = reflect.TypeOf(time.Time{})

// marshalerType json.Marshaler 接口类型
var marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()

// SchemaBuilder 把 Go 类型渲染成 OpenAPI 3.0 schema
//
// 具名结构体收进 components(组件),字段里放 $ref —— 这样同一个类型
// (比如 model.SysUser)在 20 条路由上只描述一次。
type SchemaBuilder struct {
	// components 已渲染的组件:组件名 → schema
	components map[string]Schema
	// building 正在渲染中的组件(防环)
	building map[string]bool
	// docs 组件名 → (json 字段名 → 字段说明)
	docs map[string]map[string]string
}

// NewSchemaBuilder 建一个渲染器
//
// docs 由应用层注入(见 SetSchemaSource);为 nil 时只是没有字段说明,
// schema 结构照常渲染。
func NewSchemaBuilder(docs map[string]map[string]string) *SchemaBuilder {
	return &SchemaBuilder{
		components: map[string]Schema{},
		building:   map[string]bool{},
		docs:       docs,
	}
}

// Components 返回已收进组件区的 schema
func (b *SchemaBuilder) Components() map[string]Schema {
	out := make(map[string]Schema, len(b.components))
	for k, v := range b.components {
		out[k] = v
	}
	return out
}

// SchemaOf 渲染一个类型
//
// 具名结构体 → 收进组件区,这里返回 $ref;匿名结构体 → 就地展开。
func (b *SchemaBuilder) SchemaOf(t reflect.Type) Schema {
	return b.schemaOf(t, false)
}

// schemaOf 渲染类型(nullable 标记"可以是 null")
func (b *SchemaBuilder) schemaOf(t reflect.Type, nullable bool) Schema {
	for t != nil && t.Kind() == reflect.Ptr {
		nullable = true
		t = t.Elem()
	}
	if t == nil {
		return Schema{}
	}
	s := b.baseSchema(t)
	if nullable {
		s["nullable"] = true
	}
	return s
}

// baseSchema 渲染类型本体(不解引用)
func (b *SchemaBuilder) baseSchema(t reflect.Type) Schema {
	// 数字 + 自定义 MarshalJSON → 本仓的 ID 约定:以字符串上 wire
	// (见 internal/pkg/jsonid —— sonyflake 是 19 位数,JS 的 2^53 装不下)
	//
	// ⚠️ **这条必须放在 kind 分支之前** —— jsonid.ID 的 Kind 是 Uint64,
	//   先走整数分支就返回了,下面那个"结构体里判 Marshaler"的地方根本碰不到它。
	if isIntegerKind(t.Kind()) && implementsMarshaler(t) {
		return Schema{"type": "string"}
	}
	if s, ok := b.collectionSchema(t); ok {
		return s
	}
	switch t.Kind() {
	case reflect.Bool:
		return Schema{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return Schema{"type": "integer", "format": "int32"}
	case reflect.Int64:
		return Schema{"type": "integer", "format": "int64"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Schema{"type": "integer", "format": "int64"}
	case reflect.Float32:
		return Schema{"type": "number", "format": "float"}
	case reflect.Float64:
		return Schema{"type": "number", "format": "double"}
	case reflect.String:
		return Schema{"type": "string"}
	case reflect.Struct:
		return b.structSchema(t)
	case reflect.Interface:
		// 自由形态(any)—— OpenAPI 3.0 没有"任意类型",空 schema 就是它
		return Schema{}
	}
	return Schema{"type": "object"}
}

// collectionSchema 渲染数组/切片/映射
//
// 第二个返回值 false = 不是这三类,交给调用方按 kind 继续判。
func (b *SchemaBuilder) collectionSchema(t reflect.Type) (Schema, bool) {
	switch t.Kind() {
	case reflect.Slice:
		// []byte 在 JSON 里是 base64 字符串,不是数组
		if t.Elem().Kind() == reflect.Uint8 {
			return Schema{"type": "string", "format": "byte"}, true
		}
		return Schema{"type": "array", "items": b.schemaOf(t.Elem(), false)}, true
	case reflect.Array:
		return Schema{"type": "array", "items": b.schemaOf(t.Elem(), false)}, true
	case reflect.Map:
		return Schema{"type": "object", "additionalProperties": b.schemaOf(t.Elem(), false)}, true
	}
	return Schema{}, false
}

// structSchema 渲染结构体:具名的收进组件区,匿名的就地展开
func (b *SchemaBuilder) structSchema(t reflect.Type) Schema {
	// 时间(以及包着时间的壳)→ ISO8601 字符串
	if isTimeLike(t) {
		return Schema{"type": "string", "format": "date-time"}
	}
	// 数字 + 自定义 MarshalJSON 的判定在 baseSchema 里(见那里的说明)
	if t.Name() == "" {
		// 匿名结构体:就地展开
		return b.objectSchema(t)
	}
	name := ComponentName(t)
	if _, ok := b.components[name]; ok {
		return Schema{"$ref": "#/components/schemas/" + name}
	}
	if b.building[name] {
		// 递归引用(模型之间互相引用时会出现):给个 $ref,别无限展开
		return Schema{"$ref": "#/components/schemas/" + name}
	}
	b.building[name] = true
	s := b.objectSchema(t)
	delete(b.building, name)
	b.components[name] = s
	return Schema{"$ref": "#/components/schemas/" + name}
}

// objectSchema 把结构体的字段摊平成一个 object schema
func (b *SchemaBuilder) objectSchema(t reflect.Type) Schema {
	props := map[string]any{}
	var required []string
	docs := b.docs[ComponentName(t)]

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // 非导出字段
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue // 不参与序列化(密码哈希、密文这类)
		}
		name, opts := parseJSONTag(tag)
		explicitName := name != ""
		if name == "" {
			name = f.Name
		}

		// 嵌入的结构体(无 json 名)→ 字段提升到父级(encoding/json 的行为)
		if f.Anonymous && !explicitName {
			sub, subReq, ok := b.promoted(&f)
			if ok {
				for k, v := range sub {
					props[k] = v
				}
				required = append(required, subReq...)
				continue
			}
		}

		s := b.schemaOf(f.Type, false)
		// json:",string" → wire 上是字符串(本仓所有 ID 都这么走)
		if opts["string"] {
			s = Schema{"type": "string"}
		}
		if doc, ok := docs[name]; ok && doc != "" {
			s["description"] = doc
		}
		props[name] = s

		if isRequired(&f) {
			required = append(required, name)
		}
	}

	out := Schema{"type": "object", "properties": props}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// promoted 嵌入结构体的字段提升(encoding/json 的行为)
//
// 返回提升出来的字段、它们的必填项,以及"是不是嵌入结构体"这个判定。
// 不是嵌入结构体(比如嵌了个 time.Time)→ ok=false,调用方按普通字段处理。
func (b *SchemaBuilder) promoted(f *reflect.StructField) (props map[string]any, required []string, ok bool) {
	ft := f.Type
	for ft.Kind() == reflect.Ptr {
		ft = ft.Elem()
	}
	if ft.Kind() != reflect.Struct || isTimeLike(ft) {
		return nil, nil, false
	}
	sub := b.objectSchema(ft)
	props = map[string]any{}
	if subProps, ok := sub["properties"].(map[string]any); ok {
		for k, v := range subProps {
			props[k] = v
		}
	}
	required, _ = sub["required"].([]string)
	return props, required, true
}

// isRequired 字段是否显式声明必填
//
// ⚠️ **只认显式标签** —— 理由见文件头注释:猜多了 Swagger UI 会拦着不让提交。
func isRequired(f *reflect.StructField) bool {
	for _, tag := range []string{"validate", "binding"} {
		v := f.Tag.Get(tag)
		if v == "" {
			continue
		}
		for _, rule := range strings.Split(v, ",") {
			if strings.TrimSpace(rule) == "required" {
				return true
			}
		}
	}
	return false
}

// parseJSONTag 拆 json tag:`name,omitempty,string`
func parseJSONTag(tag string) (name string, opts map[string]bool) {
	opts = map[string]bool{}
	if tag == "" {
		return "", opts
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	for _, o := range parts[1:] {
		opts[strings.TrimSpace(o)] = true
	}
	return name, opts
}

// isTimeLike 判断类型是不是"时间"或"包着时间的壳"
//
// 覆盖:time.Time / model.Time(嵌 time.Time)/ sql.NullTime / gorm.DeletedAt
// (嵌 sql.NullTime)。这几种在本仓都是 ISO8601 字符串上 wire。
func isTimeLike(t reflect.Type) bool {
	return isTimeLikeDepth(t, 0)
}

// isTimeLikeDepth 带深度上限的递归判定
//
// 深度上限 3:够穿过 time.Time → sql.NullTime → gorm.DeletedAt 三层壳,
// 又不至于在互相引用的模型里绕圈。
func isTimeLikeDepth(t reflect.Type, depth int) bool {
	if t == nil || depth > 3 {
		return false
	}
	if t == timeType {
		return true
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.Anonymous && f.Name != "Time" {
			continue
		}
		if isTimeLikeDepth(f.Type, depth+1) {
			return true
		}
	}
	return false
}

// isIntegerKind 是不是整型
func isIntegerKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

// implementsMarshaler 类型是否实现了 json.Marshaler
func implementsMarshaler(t reflect.Type) bool {
	return t.Implements(marshalerType)
}

// ComponentName 类型的组件名(与生成器 cmd/gen-swagger-schema 的算法一致)
//
// 规则:import 路径最后一段 + "." + 类型名 —— 如 "service.CreateUserRequest"。
// 两边必须一致,否则字段说明挂不上(组件名对不上 → 查不到说明)。
func ComponentName(t reflect.Type) string {
	if t == nil {
		return ""
	}
	name := t.Name()
	if name == "" {
		return ""
	}
	pkg := t.PkgPath()
	if pkg == "" {
		return name
	}
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}

// ── 应用层注入的来源 ──────────────────────────────────────────────

var (
	schemaSourceMu sync.RWMutex
	// requestDTOs handler 方法 → 请求体 DTO 类型
	requestDTOs map[string]any
	// fieldDocs 组件名 → (json 字段名 → 字段说明)
	fieldDocs map[string]map[string]string
)

// SetSchemaSource 注入「handler → 请求体 DTO」与字段说明表
//
// 由应用层在装配路由时调用(见 internal/api/router/router.go):
//
//	swaggerx.SetSchemaSource(v1.RequestDTOs, v1.RequestFieldDocs)
//
// 这两张表是**生成物**(cmd/gen-swagger-schema),swaggerx 本包不 import
// 应用层 —— 框架原语不该知道业务包的存在。
func SetSchemaSource(dtos map[string]any, docs map[string]map[string]string) {
	schemaSourceMu.Lock()
	defer schemaSourceMu.Unlock()
	requestDTOs = dtos
	fieldDocs = docs
}

// lookupRequestDTO 按 handler 方法名取请求体 DTO
func lookupRequestDTO(handlerKey string) (any, bool) {
	schemaSourceMu.RLock()
	defer schemaSourceMu.RUnlock()
	v, ok := requestDTOs[handlerKey]
	return v, ok
}

// currentFieldDocs 取当前注入的字段说明表
func currentFieldDocs() map[string]map[string]string {
	schemaSourceMu.RLock()
	defer schemaSourceMu.RUnlock()
	return fieldDocs
}

// HandlerKey 从 hertz 的 handler 函数全名里取出「方法名」形态的 key
//
// hertz 的 RouteInfo.Handler 形如:
//
//	rapid-admin/internal/api/handler/v1.(*UserHandler).Create-fm
//
// 取第一个 "(" 之后、去掉 "-fm" 后缀 → "(*UserHandler).Create",
// 与生成物里的 key 逐字对齐(见 cmd/gen-swagger-schema)。
//
// ⚠️ 对不上 ⇒ schema 挂不到那条路由上,**而且不报错** —— 所以两边必须对齐。
func HandlerKey(fullName string) string {
	i := strings.Index(fullName, "(")
	if i < 0 {
		return "" // 闭包 / 普通函数:没有 DTO
	}
	return strings.TrimSuffix(fullName[i:], "-fm")
}
