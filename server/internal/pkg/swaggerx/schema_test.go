// swaggerx —— 字段级 schema 渲染器的单测
//
// ## 这套测试守的是什么
//
// 渲染器把 Go 结构体翻译成 OpenAPI schema,规则全在 schema.go 里:
// json tag 决定字段名、`,string` 决定 wire 上是不是字符串、omitempty 只是
// "可以不传"、必填只认显式标签。
//
// ⚠️ **必填性是最容易搞砸的一条**:猜多了 Swagger UI 会拦着不让提交,
// 猜少了服务端会返回明确报错(可接受)。所以这里钉死"不猜"。
package swaggerx

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/jsonid"
)

// schemaEmbed 嵌入结构体 —— 字段会提升到父级
type schemaEmbed struct {
	EmbedField string `json:"embed_field"` // 提升上来的字段
}

// schemaInner 内层对象 —— 用来测嵌套与 $ref
type schemaInner struct {
	Title string `json:"title"` // 标题
}

// schemaNode 自引用结构体 —— 渲染时不能无限展开
type schemaNode struct {
	Name     string       `json:"name"`
	Children []schemaNode `json:"children"`
}

// schemaDTO 渲染器的样本 DTO
type schemaDTO struct {
	schemaEmbed
	Name     string            `json:"name"`                    // 名称
	Nickname string            `json:"nickname"`                // 昵称(没 omitempty,也不是必填)
	Age      int               `json:"age,omitempty"`           // 年龄
	BigID    int64             `json:"big_id,string"`           // 大数字
	Ptr      *string           `json:"ptr"`                     // 指针字段
	Inner    schemaInner       `json:"inner"`                   // 内层对象
	List     []schemaInner     `json:"list"`                    // 列表
	Dict     map[string]string `json:"dict"`                    // 字典
	Created  time.Time         `json:"created_at"`              // 创建时间
	Secret   string            `json:"-"`                       // 不上 wire
	Req      string            `json:"req" validate:"required"` // 显式必填
	Bind     string            `json:"bind" binding:"required"` // 显式必填
	hidden   string            // 非导出字段
}

// TestSchemaOf_字段规则 —— json tag / 指针 / 嵌套 / 时间 / 跳过项
func TestSchemaOf_字段规则(t *testing.T) {
	b := NewSchemaBuilder(nil)
	// 非导出字段带上值 —— 它不该出现在 schema 里
	s := b.SchemaOf(reflect.TypeOf(schemaDTO{hidden: "不该上 wire"}))

	ref, ok := s["$ref"].(string)
	require.True(t, ok, "具名结构体应收进组件区并返回 $ref,实际 %#v", s)
	assert.Equal(t, "#/components/schemas/swaggerx.schemaDTO", ref)

	comp, ok := b.Components()["swaggerx.schemaDTO"]
	require.True(t, ok, "组件区里应有 swaggerx.schemaDTO")
	assert.Equal(t, "object", comp["type"])
	props, ok := comp["properties"].(map[string]any)
	require.True(t, ok, "组件应摊平出 properties,实际 %#v", comp)

	// json tag 决定字段名
	assert.Contains(t, props, "name")
	// 嵌入结构体的字段提升到父级(encoding/json 的行为)
	assert.Contains(t, props, "embed_field")
	assert.NotContains(t, props, "schemaEmbed")
	// json:"-" 不上 wire;非导出字段也不上
	assert.NotContains(t, props, "secret")
	assert.NotContains(t, props, "hidden")

	// ,string → wire 上是字符串
	assert.Equal(t, map[string]any{"type": "string"}, props["big_id"])

	// 指针 → nullable
	ptr, ok := props["ptr"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", ptr["type"])
	assert.Equal(t, true, ptr["nullable"])

	// 嵌套结构体 → $ref
	inner, ok := props["inner"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "#/components/schemas/swaggerx.schemaInner", inner["$ref"])

	// 列表 → array
	list, ok := props["list"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "array", list["type"])

	// map → object + additionalProperties
	dict, ok := props["dict"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", dict["type"])
	assert.Contains(t, dict, "additionalProperties")

	// 时间 → ISO8601 字符串
	created, ok := props["created_at"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", created["type"])
	assert.Equal(t, "date-time", created["format"])
}

// TestSchemaOf_必填只认显式标签 —— **本仓最容易踩的坑**
//
// "没写 omitempty" ≠ "必填"(service.CreateUserRequest.Nickname 就是反例:
// 它没写 omitempty,却是可选的)。
//
// 猜多了的后果很具体:Swagger UI 会拦着不让提交没填的必填字段。
func TestSchemaOf_必填只认显式标签(t *testing.T) {
	b := NewSchemaBuilder(nil)
	b.SchemaOf(reflect.TypeOf(schemaDTO{}))

	comp := b.Components()["swaggerx.schemaDTO"]
	req, _ := comp["required"].([]string)

	assert.Contains(t, req, "req", "validate:\"required\" 应判为必填")
	assert.Contains(t, req, "bind", "binding:\"required\" 应判为必填")
	assert.NotContains(t, req, "nickname", "没写 omitempty 不等于必填 —— 不许猜")
	assert.NotContains(t, req, "name", "没写 omitempty 不等于必填 —— 不许猜")
	assert.NotContains(t, req, "age", "omitempty 的字段更不是必填")
}

// TestSchemaOf_挂上字段说明 —— 字段语义从生成物搬过来
func TestSchemaOf_挂上字段说明(t *testing.T) {
	b := NewSchemaBuilder(map[string]map[string]string{
		"swaggerx.schemaDTO": {
			"name":  "名称",
			"inner": "内层对象",
		},
	})
	b.SchemaOf(reflect.TypeOf(schemaDTO{}))

	comp := b.Components()["swaggerx.schemaDTO"]
	props, ok := comp["properties"].(map[string]any)
	require.True(t, ok)

	name, ok := props["name"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "名称", name["description"])

	inner, ok := props["inner"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "内层对象", inner["description"])
}

// TestSchemaOf_自引用不死循环 —— 模型之间互相引用时会出现
//
// 没有 building 守卫的话,这里会无限递归直到栈溢出。
func TestSchemaOf_自引用不死循环(t *testing.T) {
	b := NewSchemaBuilder(nil)
	s := b.SchemaOf(reflect.TypeOf(schemaNode{}))

	ref, ok := s["$ref"].(string)
	require.True(t, ok)
	comp := b.Components()["swaggerx.schemaNode"]
	props, ok := comp["properties"].(map[string]any)
	require.True(t, ok)
	children, ok := props["children"].(map[string]any)
	require.True(t, ok)
	items, ok := children["items"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, ref, items["$ref"], "自引用应指向同一个组件")
}

// TestSchemaOf_ID以字符串上wire —— 本仓的 ID 约定(sonyflake 19 位,JS 装不下)
func TestSchemaOf_ID以字符串上wire(t *testing.T) {
	b := NewSchemaBuilder(nil)

	s := b.SchemaOf(reflect.TypeOf(jsonid.ID(0)))
	assert.Equal(t, map[string]any{"type": "string"}, s, "jsonid.ID 上 wire 是字符串")

	s = b.SchemaOf(reflect.TypeOf(jsonid.IDList{}))
	assert.Equal(t, "array", s["type"])
	items, ok := s["items"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", items["type"], "IDList 的元素也是字符串")
}

// TestComponentName_包前缀加类型名 —— 必须与生成器的算法一致
//
// 两边算法不一致 ⇒ 字段说明挂不上(而且**不报错**)。
func TestComponentName_包前缀加类型名(t *testing.T) {
	assert.Equal(t, "swaggerx.schemaDTO", ComponentName(reflect.TypeOf(schemaDTO{})))
	assert.Equal(t, "", ComponentName(nil))
}

// TestHandlerKey_跟hertz的handler全名对齐 —— 接线对不上的话 schema 挂不上
//
// hertz 的 RouteInfo.Handler 形如:
//
//	rapid-admin/internal/api/handler/v1.(*UserHandler).Create-fm
//
// 生成物里的 key 形如 "(*UserHandler).Create" —— 两边必须逐字对得上。
func TestHandlerKey_跟hertz的handler全名对齐(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"方法形式",
			"rapid-admin/internal/api/handler/v1.(*UserHandler).Create-fm",
			"(*UserHandler).Create",
		},
		{
			"另一个 handler",
			"rapid-admin/internal/api/handler/v1.(*AuthHandler).SwitchIdentity-fm",
			"(*AuthHandler).SwitchIdentity",
		},
		{
			"没有 -fm 后缀也认",
			"pkg.(*X).Y",
			"(*X).Y",
		},
		{
			"普通函数(闭包)没有 DTO",
			"pkg.Func",
			"",
		},
		{
			"空串",
			"",
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, HandlerKey(tc.in))
		})
	}
}

// TestIsRequired_只认显式标签 —— 单测直接钉住这条规则
func TestIsRequired_只认显式标签(t *testing.T) {
	type probe struct {
		Validate string `json:"a" validate:"required"`
		Binding  string `json:"b" binding:"required"`
		Mixed    string `json:"c" binding:"omitempty,required"`
		Plain    string `json:"d"`
		Omit     string `json:"e,omitempty"`
		Other    string `json:"f" form:"required"`
	}
	typ := reflect.TypeOf(probe{})
	field := func(name string) *reflect.StructField {
		f, ok := typ.FieldByName(name)
		if !ok {
			return nil
		}
		return &f
	}
	assert.True(t, isRequired(field("Validate")))
	assert.True(t, isRequired(field("Binding")))
	assert.True(t, isRequired(field("Mixed")), "规则列表里夹着别的选项也算 required")
	assert.False(t, isRequired(field("Plain")), "没标签就不是必填 —— 不许猜")
	assert.False(t, isRequired(field("Omit")), "omitempty 的字段不是必填")
	assert.False(t, isRequired(field("Other")), "form:\"required\" 不是本仓的必填约定")
}
