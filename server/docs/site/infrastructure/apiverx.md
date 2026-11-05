# apiverx —— API 版本协商

> 把 HTTP header `X-API-Version` 解析成 major 版本挂到 ctx 上,
> handler 用 `apiverx.Major(ctx)` 做版本分支。

## 功能

apiverx 提供三件事:

1. **解析** —— `Parse(raw string) (int, bool)`,接受 `"1"` / `"2"` / `"v2"` / `"V2"`
2. **注入 / 读取** —— `WithVersion(ctx, v)` 写、`Major(ctx)` 读
3. **上限约束** —— `WithMaxMajor(n)`,给中间件用

中间件(`internal/api/middleware/apiver.go`)负责:读 header → 解析 → 可选校验 →
注入 ctx → **把生效的版本回写到响应头** `X-API-Version`(供客户端 / 网关观测)。

**设计要点**:

- **默认版本 1**:没有 header → 视为 1 ⇒ 老客户端不被拒(向后兼容)
- **中间件只解析 + 注入,不强制校验** —— 要不要拒绝由 `WithMaxMajor` 决定
- **解析失败不报错**,按默认版本处理

**不解决什么**:不管理路由版本(`/v1/` `/v2/` 两套路由树)、不生成版本化文档、
不做字段级的新旧兼容 —— 那两件事只有"一个 handler 里按 `Major(ctx)` 分支"这一种做法。

## 配置

### 代码侧(构造选项)

```go
apiverx.WithMaxMajor(n int) func(*apiverx.Opt)
apiverx.Opt{ MaxMajor int }   // 0 = 不校验
```

| 常量 | 值 | 说明 |
|---|---|---|
| `apiverx.HeaderName` | `"X-API-Version"` | 标准 header 名(HTTP header 不分大小写) |
| `apiverx.DefaultVersion` | `1` | 无 header / 解析失败时的版本 |

### yaml 侧

**该包无 yaml 配置。** 当前版本上限是**代码里写死的**:
`internal/api/router/router.go` 挂的是 `middleware.APIVersion()`(不带任何选项)
⇒ `MaxMajor == 0` ⇒ **不校验、不拒绝**。

## 使用方法

### 装配(router.go)

```go
h.Use(middleware.APIVersion())                          // 仅解析 + 注入
h.Use(middleware.APIVersion(apiverx.WithMaxMajor(3)))   // 超 3 拒绝
```

### handler 里做版本分支

```go
if apiverx.Major(ctx) >= 2 {
    // v2+ 的新字段结构
} else {
    // v1 的老字段结构
}
```

### 单元里直接用

```go
v, ok := apiverx.Parse("v3")   // → 3, true
ctx = apiverx.WithVersion(ctx, 2)
apiverx.Major(ctx)             // → 2
```

## 注意事项

- ⚠️ **超限返回的 HTTP 状态是 500,不是 422**。中间件注释和包注释都写
  "422",但它走的是 `response.Fail(c, errcode.VersionNotSupported, ...)`,
  而 `errcode.VersionNotSupported = 50500` 落在 `errcode.HTTPStatus` 的
  **5xxxx 兜底桶**里 ⇒ HTTP **500**(body 里 `code: 50500`)。
  客户端按 5xx 走"服务器内部错误"分支,看不到"版本不支持"这层语义。
- ⚠️ **`Parse` 的注释与实现不一致**。包注释写
  `"1.2" / "abc" → DefaultVersion, !ok`,但实现里遇到 `.` 是**截断**
  (`"1.2"` → `1`)而不是判失败 ⇒ `"1.2"` 实际返回 `(1, true)`。
  只有 `"abc"` 这类非数字才真的 `!ok`。
- ⚠️ **中间件**不**看 `Parse` 的 `ok`** —— 解析失败时它直接用
  `apiverx.DefaultVersion` 覆盖掉。所以 `X-API-Version: 0`、
  `X-API-Version: abc` 都会**静默**当成 v1 放行,请求方不会收到任何提示。
  要严格校验就得自己在 handler 里判 `ok`。
- ⚠️ **`Parse` 不做 trim**:`" 2"`(带空格)是**非法**的 → 落到默认版本。
  和 `uainfo.Parse` 会先 `strings.TrimSpace` 不一样。
- ⚠️ **`Parse` 接受前导零**(`"01"` → 1),也接受 `"v"`(去前缀后为空串 → `!ok`)。
  数字有 `1 << 20` 的溢出保护,不会因为超长数字串把 CPU 打满。
- ⚠️ **`Major(ctx)` 永远返回一个值,不会"失败"**。没挂中间件 / ctx 里没有值
  ⇒ 返 `DefaultVersion`。这意味着**忘记挂中间件不会报错,只会让版本分支
  永远走 v1** —— 一个静默的功能缺失。
- ⚠️ **当前生产装配没有开上限校验**,`errcode.VersionNotSupported` 这条错误码
  在现有装配下**不可达**。要给"老客户端"设一个明确的退役线,得先把
  `middleware.APIVersion(apiverx.WithMaxMajor(n))` 挂上。
- ⚠️ **版本号回写在响应头** `X-API-Version` —— 网关和客户端能观测到
  "这次实际生效的是哪个版本",排查版本分支问题时先看这个头。

## 相关

- 源码:`internal/pkg/apiverx/`(中间件在 `internal/api/middleware/apiver.go`)
- 可运行示例:`examples/apiverx/main.go` —— `go run ./examples/apiverx`
- 版本不支持的错误码 → [errcode](errcode.md)
- 统一响应体(错误码怎么落到 body)→ [response](response.md)
