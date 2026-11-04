// OpenAPI 字段级 schema 的接线测试(2026-11-01 开源通用化 §8 G3)
//
// ## 这条网守的是什么
//
// 渲染器有单测(见 internal/pkg/swaggerx/schema_test.go),但**接线**没有:
// 路由表 → handler 名 → 生成物 → schema 这条链,断在哪一环都**不报错**,
// 只会让所有请求体悄悄退回空 object —— 文档看着还在,字段全没了。
//
// 所以这里端到端跑一次真实路由表,断言:
//   - 请求体真的有字段(不是空 object)
//   - x-request-schema 挂在正确的路由上
//   - x-schema-coverage 的数字是真的(不是写死的宣传语)
package router

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/swaggerx"
)

// buildSpecForTest 装一遍真实路由,生成 spec
func buildSpecForTest(t *testing.T) *swaggerx.OpenAPI {
	t.Helper()
	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, nil, nil)
	return swaggerx.BuildSpec(h, swaggerx.Info{Title: "测试"}, nil)
}

// TestSwaggerSpec_写接口带字段级schema —— 主断言
func TestSwaggerSpec_写接口带字段级schema(t *testing.T) {
	spec := buildSpecForTest(t)

	cov := spec.XSchemaCoverage
	require.NotNil(t, cov, "spec 必须带 x-schema-coverage —— 覆盖率不藏着")
	assert.Greater(t, cov.Routes, 0, "spec 里应有路由")
	assert.Greater(t, cov.WriteRoutes, 0, "spec 里应有写接口")
	assert.Greater(t, cov.WithRequestSchema, 0, "至少要有一条写接口拿到字段级 schema")

	// 百分比必须跟分子分母自洽(不能是写死的数字)
	require.NotZero(t, cov.WriteRoutes)
	assert.Equal(t, cov.WithRequestSchema*100/cov.WriteRoutes, cov.Percent)
}

// TestSwaggerSpec_请求体不是空object —— 防"接线断在生成物上"
func TestSwaggerSpec_请求体不是空object(t *testing.T) {
	spec := buildSpecForTest(t)

	// 组件区里必须有真实业务类型,而且有字段
	comp, ok := spec.Components.Schemas["service.CreateUserRequest"]
	require.True(t, ok, "组件区应含 service.CreateUserRequest")
	props, ok := comp["properties"].(map[string]any)
	require.True(t, ok, "请求体组件应摊平出 properties")
	assert.NotEmpty(t, props, "请求体组件不该是空对象")
}

// TestSwaggerSpec_xRequestSchema挂在对的路由上 —— 防 key 对不上
//
// 生成物里的 key 形如 "(*UserHandler).Create",运行期由 hertz 的
// utils.NameOfFunction 推出来 —— 两边必须逐字对得上。
// 对不上不会报错,只会让这条路由的 schema 静默消失。
func TestSwaggerSpec_xRequestSchema挂在对的路由上(t *testing.T) {
	spec := buildSpecForTest(t)

	item, ok := spec.Paths["/rapid-admin/v1/system/users"]
	require.True(t, ok, "spec 里应有 /rapid-admin/v1/system/users")
	require.NotNil(t, item.Post, "POST /rapid-admin/v1/system/users")
	assert.Equal(t, "service.CreateUserRequest", item.Post.XRequestSchema)

	// 请求体内容必须指向同一个组件
	require.NotNil(t, item.Post.RequestBody)
	mt, ok := item.Post.RequestBody.Content["application/json"]
	require.True(t, ok)
	ref, ok := mt.Schema["$ref"].(string)
	require.True(t, ok, "请求体应指向组件,实际 %#v", mt.Schema)
	assert.Equal(t, "#/components/schemas/service.CreateUserRequest", ref)
}

// TestSwaggerSpec_ID字段以字符串上wire —— jsonid.IDList 的元素是字符串
//
// ⚠️ 这条曾经是错的:jsonid.ID 的 Kind 是 Uint64,整数分支先返回了,
//
//	"有 MarshalJSON 就是字符串"那条判定根本碰不到它 —— 于是 ID 数组
//	的元素被写成 integer,而 wire 上其实是字符串。
func TestSwaggerSpec_ID字段以字符串上wire(t *testing.T) {
	spec := buildSpecForTest(t)

	comp, ok := spec.Components.Schemas["service.CreateUserRequest"]
	require.True(t, ok)
	props, ok := comp["properties"].(map[string]any)
	require.True(t, ok)

	dept, ok := props["department_ids"].(map[string]any)
	require.True(t, ok, "CreateUserRequest 应有 department_ids")
	assert.Equal(t, "array", dept["type"])
	items, ok := dept["items"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", items["type"], "jsonid.IDList 的元素在 wire 上是字符串")
}

// TestSwaggerSpec_GET接口没有请求体 —— 防渲染器把查询参数也当请求体
func TestSwaggerSpec_GET接口没有请求体(t *testing.T) {
	spec := buildSpecForTest(t)

	item, ok := spec.Paths["/rapid-admin/v1/system/users"]
	require.True(t, ok)
	require.NotNil(t, item.Get, "GET /rapid-admin/v1/system/users")
	assert.Nil(t, item.Get.RequestBody, "GET 不该有请求体")
}
