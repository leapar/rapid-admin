// swaggerx —— Swagger UI + 自动生成的 OpenAPI 3.0 spec
//
// ## 这个包解决什么问题
//
// 把后台接口的**全量清单**(路径 / 方法 / 每个接口的 RBAC 权限码)
// 变成一份机器可读的 OpenAPI 文档。
//
// ## 关键点
//
//   - **spec 是从运行期的路由表推出来的**,不是另写一份注解。
//     路由表只有 Hertz 实例自己知道 ⇒ 所以 BuildSpec 收一个 *server.Hertz
//   - **字段级 schema 也是反射出来的**(json tag 说字段名 / 指针说可选性 /
//     行尾注释说含义)。不用 swaggo 注解 ⇒ 不用给 185+ 个 handler
//     各加 6-10 行注释,也不用多一条工具链依赖
//   - **/swagger/doc.json 里是全量接口清单** ⇒ 裸奔等于把攻击面画好送出去。
//     BasicAuth 闸是默认要装的东西,不是可选项
//   - **比较必须用 subtle.ConstantTimeCompare** —— 普通字符串比较会在
//     第一个不同的字节上短路,理论上可被计时攻击逐字节爆破
//   - **UI 资产由调用方提供**,本包不内置 swagger-ui(那是一份 ~4MB 的
//     第三方产物,塞进 internal/pkg 会让框架原语背上一个没法审计的依赖)
//
// ## 跑法
//
//	go run ./examples/swaggerx
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"

	"rapid-admin/internal/pkg/swaggerx"
)

func main() {
	ctx := context.Background()

	// ── 一、造一个 Hertz 实例 ───────────────────────────────────
	//
	// server.New 只建引擎不监听端口 —— 不用起 HTTP 服务就能看 spec。
	//
	// ⚠️ Hertz 默认把路由注册打进 Debug 日志,示例里用 WithDisablePrintRoute
	//    关掉(生产环境同理:路由表不该出现在常规日志里)。
	h := server.New(server.WithDisablePrintRoute(true))
	h.GET("/rapid-admin/v1/houses", func(ctx context.Context, c *app.RequestContext) {})
	h.GET("/rapid-admin/v1/houses/:id", func(ctx context.Context, c *app.RequestContext) {})
	h.POST("/rapid-admin/v1/houses", func(ctx context.Context, c *app.RequestContext) {})
	h.GET("/rapid-admin/v1/system/users", func(ctx context.Context, c *app.RequestContext) {})
	h.GET("/healthz", func(ctx context.Context, c *app.RequestContext) {})
	h.GET("/metrics", func(ctx context.Context, c *app.RequestContext) {})

	// ── 二、生成 spec ───────────────────────────────────────────
	fmt.Println("=== 生成 spec ===")
	spec := swaggerx.BuildSpec(h, swaggerx.Info{
		Title:       "示例平台 - 后台 API",
		Description: "由框架层 swaggerx 自动生成",
		Version:     "1.0.0",
		BaseURL:     "https://api.example.com/rapid-admin/v1",
	}, map[string]bool{
		// 权限码表由 router 包在注册路由时共享进来(见 SetRBACCodesProvider)。
		// 这里是 best-effort 推断:从 URL 段拼 "<资源>:<动作>" 再查这张表。
		"houses:list": true,
		"houses:read": true,
		"users:list":  true,
	})

	fmt.Printf("openapi = %s\n", spec.OpenAPI)
	fmt.Printf("title   = %s\n", spec.Info["title"])
	fmt.Printf("version = %s\n", spec.Info["version"])

	fmt.Println()
	fmt.Println("=== 路径 ===")
	paths := make([]string, 0, len(spec.Paths))
	for p := range spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Printf("  %-28s %v\n", p, methods(spec.Paths[p]))
	}
	fmt.Println("  ⇒ /healthz /metrics 这类框架端点不进业务文档")
	fmt.Println("  ⇒ :id 会被转成 OpenAPI 的 {id}")

	fmt.Println()
	fmt.Println("=== 公共 schema ===")
	names := make([]string, 0, len(spec.Components.Schemas))
	for n := range spec.Components.Schemas {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Printf("  %v\n", names)
	fmt.Println("  ⇒ ApiResponse / PageRequest / PageResponse 是框架内置的通用信封")

	// ── 三、rbac code ───────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== RBAC 权限码 ===")
	for _, p := range paths {
		for _, o := range ops(spec.Paths[p]) {
			if o.op.XRbacCode != "" {
				fmt.Printf("  %-6s %-28s → %s\n", o.method, p, o.op.XRbacCode)
			}
		}
	}
	fmt.Println("  ⇒ 权限码是框架扩展字段(x-rbac-code),OpenAPI 标准里没有 ——")
	fmt.Println("    集成方靠它知道这个接口要什么权限才能调")
	fmt.Println("  ⇒ 推断是 best-effort:从 URL 段拼「<资源>:<动作>」再查权限码表。")
	fmt.Println("    同一个 URL 命中多个动作时挑哪个不确定 ⇒ 真要精确就靠 router 侧注入")

	// ── 四、序列化成 JSON ───────────────────────────────────────
	fmt.Println()
	fmt.Println("=== doc.json ===")
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		fmt.Println("序列化失败:", err)
		return
	}
	fmt.Printf("  长度 = %d 字节\n", len(b))
	fmt.Printf("  前 200 字节:\n%s\n", cut(string(b), 200))

	// ── 五、BasicAuth 闸 ────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== BasicAuth 闸 ===")
	auth := swaggerx.AuthConfig{
		Enabled:  true,
		Username: "docs",
		Password: "s3cret",
	}
	mw := swaggerx.BasicAuth(auth)
	if mw == nil {
		fmt.Println("BasicAuth 返回 nil ⇒ 闸没装上")
		return
	}

	// 没带 Authorization 头
	c1 := app.NewContext(0)
	mw(ctx, c1)
	fmt.Printf("  无凭据 → 状态码 %d, WWW-Authenticate = %q\n",
		c1.Response.StatusCode(), c1.Response.Header.Get("WWW-Authenticate"))
	fmt.Printf("    body = %s\n", string(c1.Response.Body()))

	// 密码错
	c2 := app.NewContext(0)
	c2.Request.Header.Set("Authorization", basic("docs", "wrong"))
	mw(ctx, c2)
	fmt.Printf("  密码错 → 状态码 %d\n", c2.Response.StatusCode())

	// 凭据对
	c3 := app.NewContext(0)
	c3.Request.Header.Set("Authorization", basic("docs", "s3cret"))
	mw(ctx, c3)
	fmt.Printf("  凭据对 → 状态码 %d(200 = 放行)\n", c3.Response.StatusCode())
	fmt.Println("  ⇒ 401 必须带 WWW-Authenticate,否则浏览器只显示一个空页面")

	// ── 六、关掉闸 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== Enabled=false ===")
	fmt.Printf("  BasicAuth 返回 nil = %v\n", swaggerx.BasicAuth(swaggerx.AuthConfig{Enabled: false}) == nil)
	fmt.Println("  ⇒ 关掉闸时返回 nil(不产生任何 handler),调用方直接跳过挂载")
}

// basic 拼一个 Basic 认证头
func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// opItem 一条 operation + 它的方法名
type opItem struct {
	method string
	op     *swaggerx.Operation
}

// ops 把 PathItem 拆成"方法 + operation"的列表
func ops(item *swaggerx.PathItem) []opItem {
	if item == nil {
		return nil
	}
	all := []struct {
		method string
		op     *swaggerx.Operation
	}{
		{"GET", item.Get},
		{"POST", item.Post},
		{"PUT", item.Put},
		{"PATCH", item.Patch},
		{"DELETE", item.Delete},
		{"OPTIONS", item.Options},
		{"HEAD", item.Head},
	}
	out := make([]opItem, 0, len(all))
	for _, a := range all {
		if a.op != nil {
			out = append(out, opItem{method: a.method, op: a.op})
		}
	}
	return out
}

// methods 只取方法名
func methods(item *swaggerx.PathItem) []string {
	list := ops(item)
	out := make([]string, 0, len(list))
	for _, o := range list {
		out = append(out, o.method)
	}
	return out
}

// cut 截断字符串
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
