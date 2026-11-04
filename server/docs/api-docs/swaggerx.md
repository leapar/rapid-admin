# swaggerx —— Swagger UI + 自动生成的 OpenAPI 3.0 spec

> 把后台接口的**全量清单**(路径 / 方法 / 每个接口的 RBAC 权限码)
> 变成一份机器可读的 OpenAPI 3.0 文档,挂在 `/swagger` 上。

## 功能

swaggerx 做四件事:

1. **spec 是从运行期的路由表推出来的**,不是另写一份注解。
   路由表只有 Hertz 实例自己知道 ⇒ `BuildSpec` 收一个 `*server.Hertz`。
   框架/健康/指标端点(`/swagger*` / `/healthz` / `/readyz` / `/metrics`)
   不进业务文档;只收 `/rapid-admin/v1/` 前缀的路由。
2. **字段级 schema 是反射出来的**:json tag 说字段名、指针说可选性、
   行尾注释说含义。不用 swaggo 注解 ⇒ 不用给 185+ 个 handler 各加 6-10 行
   注释,也不用多一条工具链依赖(185 路由 × 8 行 ≈ 1500 行标注)。
3. **BasicAuth 闸**:`/swagger/doc.json` 里是全量接口清单,
   裸奔等于把攻击面画好送出去 ⇒ 鉴权闸是**默认要装的东西,不是可选项**。
4. **两个框架扩展字段**:
   - `x-rbac-code` —— 每条接口的 RBAC 权限码,集成方靠它知道"这个接口要什么权限"
   - `x-schema-coverage` —— 字段级 schema 的覆盖率(路由数 / 写接口数 /
     拿到 schema 的写接口数 / 百分比)。**覆盖率不藏着**,它是可核对的数字。

它**不解决**:

- 不生成**响应体**的字段级 schema(只做请求体)。响应体形状由
  [统一响应体](../infrastructure/response.md)约定,不是每个 DTO 逐个反射。
- 不生成 `swag init` 那样的注解式描述(summary / description 是按 URL 段自动拼的)。
- 不做文档的版本管理(改 handler 不会产生"新版本文档",只有一份活的 spec)。

## 配置

### yaml 侧(`swagger:` 块)

```yaml
swagger:
  enabled: true
  auth:
    mode: auto
    username: ""           # 留空 ⇒ auto 模式下用 "swagger"
    password: ""           # 留空 ⇒ auto 模式下本次启动随机生成(见启动日志)
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `swagger.enabled` | bool | `true` | 是否挂载 `/swagger` 端点。关掉 = 整条端点不存在 |
| `swagger.auth.mode` | string | `auto` | `auto` / `basic` / `none` |
| `swagger.auth.username` | string | `""` | BasicAuth 用户名。auto 模式下留空 ⇒ 用 `"swagger"` |
| `swagger.auth.password` | string | `""` | BasicAuth 密码。auto 模式下留空 ⇒ 本次启动随机生成 |

三种 `mode` 的行为:

| mode | 行为 |
|---|---|
| `auto`(默认,空值等同) | 配了固定凭据就用固定凭据;**没配就本次启动随机生成一个密码并打进启动日志**(重启会变) |
| `basic` | **必须**配固定凭据,没配直接报错 → 拒绝启动(fail-fast) |
| `none` | **不鉴权**。⚠️ 只有本地开发能用 |

⚠️ **凭据建议走环境变量** `SWAGGER_AUTH_USERNAME` / `SWAGGER_AUTH_PASSWORD`,
别写进 yaml。

### 标题与品牌

文档标题**不在 `swagger:` 块里**,走 `branding:` 配置:

```yaml
branding:
  app_name: ""            # 平台全名
  app_title: ""           # 浏览器标签页标题;留空 = 用 app_name
```

派生顺序:`branding.app_title` → `branding.app_name` → `system.app_name`(服务标识)。
⚠️ **不许在代码里写死项目名** —— 开源通用化要求"改个 yaml 就能换名字"。

### 代码侧(构造选项)

```go
swaggerx.Mount(h *server.Hertz, mountAt string, info Info, assets fs.FS, auth AuthConfig)
swaggerx.BuildSpec(h *server.Hertz, info Info, rbacCodes map[string]bool) *OpenAPI
swaggerx.BasicAuth(cfg AuthConfig) app.HandlerFunc
swaggerx.SetRBACCodesProvider(fn func() map[string]bool)
swaggerx.SetSchemaSource(dtos map[string]any, docs map[string]map[string]string)
swaggerx.SortPathsStable(spec *OpenAPI)
```

| 类型 | 说明 |
|---|---|
| `Info` | `Title` / `Description` / `Version` / `BaseURL` |
| `AuthConfig` | `Enabled` / `Username` / `Password` |
| `OpenAPI` | 完整 spec 结构(`Paths` / `Components` / `Tags` / `XSchemaCoverage` …) |
| `SchemaCoverage` | `Routes` / `WriteRoutes` / `WithRequestSchema` / `Percent` |

## 使用方法

完整示例:`examples/swaggerx/main.go`。

### 一、挂载(生产装配在 `internal/api/router/router.go`)

```go
swaggerx.SetRBACCodesProvider(RBACCodesProvider)      // 让每条 route 带 x-rbac-code
swaggerx.SetSchemaSource(v1.RequestDTOs, v1.RequestFieldDocs)  // 字段级 schema 的来源

swaggerx.Mount(h, "/swagger", swaggerx.Info{
    Title:       swaggerDocTitle(cfg),
    Description: "Backend API reference. ...",
    Version:     cfg.System.AppVersion,
    BaseURL:     "/rapid-admin/v1",
}, swagger.Assets(), swaggerx.AuthConfig{
    Enabled:  cfg.Swagger.Auth.Enabled,
    Username: cfg.Swagger.Auth.Username,
    Password: cfg.Swagger.Auth.Password,
})
```

⚠️ **这层必须挂在全局中间件链之前** —— router.go 里它是第一件注册的事,
所以拿不到 `middleware.Recover()`;本包自己兜 panic(见 `safeHandler`)。

挂完得到三个路由:

| 路径 | 作用 |
|---|---|
| `/swagger` | 302 跳 `/swagger/index.html`(浏览器不会自动补 `index.html`) |
| `/swagger/doc.json` | OpenAPI 3.0 原始 JSON |
| `/swagger/*any` | swagger-ui 静态资产 |

### 二、不挂 UI,只生成 spec

```go
spec := swaggerx.BuildSpec(h, swaggerx.Info{
    Title:       "示例平台 - 后台 API",
    Description: "由框架层 swaggerx 自动生成",
    Version:     "1.0.0",
    BaseURL:     "https://api.example.com/rapid-admin/v1",
}, map[string]bool{
    "orders:list": true,
    "orders:read": true,
    "users:list":  true,
})
```

`rbacCodes` 是权限码表,由 router 包在注册路由时共享进来
(`SetRBACCodesProvider`);`x-rbac-code` 的推断是 best-effort:
从 URL 段拼"`<资源>:<动作>`"再查这张表,同一个 URL 命中多个动作时挑哪个不确定。

### 三、BasicAuth 闸

```go
auth := swaggerx.AuthConfig{Enabled: true, Username: "docs", Password: "s3cret"}
mw := swaggerx.BasicAuth(auth)   // Enabled=false 时返回 nil(不产生任何 handler)
```

`BasicAuth` 返回 `nil` 表示闸没装上 —— `Mount` 里的 `withAuth` 会跳过这一层中间件,
路由照挂(但不鉴权)。

## 注意事项

- ⚠️ **`doc.json` 里是全量接口清单**(路径 / 方法 / 每个接口的 RBAC 权限码)
  —— 必须默认带鉴权,否则等于把攻击面画好送出去。
  `mode=none` 只允许本地开发。

- ⚠️ **`mode=basic` 却没配凭据 ⇒ 直接拒绝启动**(fail-fast)。
  `config.SwaggerConfig.ResolveAuth()` 返回 error,`cmd/server` 在
  **logger 初始化之前**就 `panic` 掉了(所以是 panic 而不是 `log.Fatal`)。
  理由写在源码注释里:"mode=basic 却没配凭据,是'以为有鉴权其实没有'的典型形态:
  服务能起、能访问、文档也在,但谁都能看" —— 这种洞必须在启动时就炸掉。
  (router.go 里那层"解析失败就跳过挂载"是第二道防线,生产路径上走不到,
  因为 main 先 panic 了。)

- ⚠️ **`mode=auto` 没配凭据 ⇒ 本次启动随机生成一个密码并打进启动日志**。
  密码是 24 位十六进制(`crypto/rand`),**重启就变**。
  生产环境请配固定凭据或走环境变量。
  两种情况启动时都会出声:随机密码打 WARN(带密码),
  `mode=none` 打 WARN("swagger 文档端点**未鉴权**(/swagger 全公开,含全量接口清单)")。

- ⚠️ **改了 handler 的 DTO 之后要重跑生成器**:
  `go run ./cmd/gen-swagger-schema`。生成物落在
  `internal/api/handler/v1/swagger_dto_gen.go`,里面是"路由 handler 方法 → 请求体 DTO"
  的注册表 + 字段说明表。手写一份注册表就是**第二份真相**:handler 里把
  `service.CreateUserRequest` 换成别的类型,那张表不会跟着变,
  而且**不会报错**,只会让文档里的字段悄悄对不上实际接口。
  漂移由 `cmd/gen-swagger-schema/main_test.go` 盯着(重跑一遍逐字比)。

- ⚠️ **覆盖率数字写在文档里**:`x-schema-coverage` 实测
  `routes=183` / `write_routes=125` / `with_request_schema=104` / `percent=83`。
  拿不到字段级 schema 的写接口**保留空 object,不猜、不编** ——
  取不到的原因会反映在这个数字里。

- ⚠️ **`SetSchemaSource` 的 key 必须与运行期逐字对齐**。
  `HandlerKey` 把 hertz 的 handler 全名
  `rapid-admin/internal/api/handler/v1.(*UserHandler).Create-fm`
  取成 `(*UserHandler).Create`,与生成物里的 key 对齐。
  **对不上不会报错**,只会让那条路由的 schema 静默消失。

- ⚠️ **必填性只认显式标签**(`validate:"required"` / `binding:"required"`)。
  本仓 DTO 大多没有这两个标签,只有 json tag —— 而"没有 omitempty"
  **并不等于"必填"**。猜错的后果很具体:Swagger UI 会**拦着不让提交**没填的必填字段。
  宁可少标必填(服务端会返回明确报错),也不要多标。

- ⚠️ **ID 字段以字符串上 wire**。`jsonid.ID` 的 Kind 是 `Uint64`,
  但它实现了 `json.Marshaler` —— 渲染器里"数字 + 自定义 MarshalJSON ⇒ string"
  这条判定**必须放在 kind 分支之前**,否则先走整数分支就返回了,
  `jsonid.ID` 会被写成 integer,而 wire 上其实是字符串
  (sonyflake 是 19 位数,JS 的 2^53 装不下)。

- ⚠️ **静态资产路径必须过 `fs.ValidPath`** —— 它拒绝 `..` / `.` 段和绝对路径。
  没有这道闸,`GET /swagger/../../etc/passwd` 就能读到文件系统别的东西。
  (embed.FS 自己也会挡一部分,但那是"碰巧",不能当成防线。)

- ⚠️ **`.js` 必须是 `application/javascript`** —— swagger-ui 靠 `<script src>` 加载。
  服务端返 `text/plain` 会被浏览器拒绝执行(控制台报 MIME 错),页面一片空白。

- ⚠️ **`assets` 传 `nil` 时只挂 `doc.json`,UI 页面 404**。UI 资产由调用方提供
  —— 本包**不内置** swagger-ui(那是一份 ~4MB 的第三方产物,
  塞进 `internal/pkg` 会让这个"框架原语"背上一个没法审计的依赖)。
  仓库里那份在 `server/swagger/ui/`(swagger-ui 5.10.5,Apache-2.0,
  许可证全文随资产收在同目录)。

- ⚠️ **401 必须带 `WWW-Authenticate` 头**,否则浏览器只显示一个空的 401 页面,
  用户完全不知道要登录。

- ⚠️ **密码里带冒号是允许的** —— 统一按**第一个**冒号切:前面是用户名,
  其余全是密码。按最后一个冒号切就反了(`"a:b:c"` 会变成 用户 `"a:b"` / 密码 `"c"`)。

- ⚠️ **`Mount` 幂等**:同一个 Hertz 实例只挂一次路由;换个实例(单测场景)会再挂一份。
  鉴权结论由 `ResolveAuth()` 解析 —— 它**是幂等的**,main.go 先调一次做 fail-fast、
  router 里再调一次拿结论,两次不会打架(否则日志里印的密码跟真正生效的不是同一个)。

- ⚠️ **比较必须用 `subtle.ConstantTimeCompare`** —— 普通字符串比较会在
  第一个不同的字节上短路,理论上可被计时攻击逐字节爆破出密码。
  用户名和密码**都要比**:只比密码的话,用户名就成了可以随便猜的公开信息。

## 相关

- 源码:`internal/pkg/swaggerx/`
- UI 静态资产:`server/swagger/`(`Assets()` 提供 `fs.FS`)
- 字段级 schema 生成器:`cmd/gen-swagger-schema/` —— `go run ./cmd/gen-swagger-schema`
- 可运行示例:`examples/swaggerx/main.go` —— `go run ./examples/swaggerx`
- 权限码与资源授权 → [authz](../security/authz.md)
- 平台名 / 品牌配置 → [配置体系](../getting-started/configuration.md)
