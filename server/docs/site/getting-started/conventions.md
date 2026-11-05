# 框架约定

> 这几条约定是**跨模块**的:响应体长什么样、错误码怎么分段、ID 在 JSON 上是什么类型、
> 字段叫什么名、时间用哪个时区。写业务模块之前先读一遍,能省掉一轮返工。

## 1. 响应体:三种形状,一个信封

所有接口返回同一个信封(`internal/pkg/response`):

```go
type Response struct {
    Code      int         `json:"code"`      // 0 = 成功
    Data      interface{} `json:"data,omitempty"`
    Message   string      `json:"message"`
    Timestamp int64       `json:"timestamp"` // 毫秒
    TraceID   string      `json:"trace_id,omitempty"`
}
```

三种调用:

| 场景 | 函数 | `data` |
|---|---|---|
| 无返回值的写操作 | `response.OK(c)` | 省略 |
| 返回一个对象 | `response.OKWithData(c, obj)` | 对象 |
| 分页 | `response.Page(c, list, total, page, size)` | `{list, total, page, size}` |

失败:

```go
response.Fail(c, errcode.ParamInvalid, "手机号格式不对")
response.FailWithData(c, errcode.ParamInvalid, "参数不合法", fieldErrors) // 带校验明细
response.FailError(c, err) // 从 error 里取 code + msg
```

⚠️ **业务代码不要自己拼 JSON**。信封是前端唯一的契约来源,自己拼就一定会漏字段。

### HTTP 状态码与业务码的关系

- **HTTP 状态码**由 `errcode.HTTPRaw(code)` / `errcode.HTTPStatus(code)` 从业务码映射。
- **业务码**(`code` 字段)才是前端判断逻辑的依据。
- HTTP 200 + `code != 0` 是**正常**的失败返回;前端只看 `code`。

## 2. 错误码:分段,一段一个域

`internal/pkg/errcode` 里错误码按**业务域**分段,段位本身有意义:

| 段 | 域 | 例 |
|---|---|---|
| `0` | 成功 | `Success` |
| `20xxx` | 登录 / 验证码 / 权限申请 | `LoginFailed` `SmsTooFrequent` `SameAuditor` |
| `30xxx` | 平台配置 | `ConfigNotFound` `ConfigSelfReview` |
| `40xxx` | 通用 HTTP 语义 | `BadRequest` `Unauthorized` `TokenExpired` `Forbidden` `NotFound` |
| `41300` | 通用 HTTP 语义(续) | `PayloadTooLarge` |
| `42900` | 通用 HTTP 语义(续) | `TooManyRequests` |
| `50xxx` | 框架 / 运行时 | `Internal` `ServiceUnavailable` `SignatureInvalid` |

⚠️ **不要往已占用的段里插**。`20203` 与 `20210` 之间已经被
`CoOwnerCodeRequired` 占着 —— 审批系统专段 `206xx` 也是这么开的。
新增码之前先看 `internal/pkg/errcode/errcode.go`。

## 3. 分页:请求与响应

请求(`internal/pkg/pagination`):

```go
type PageRequest struct {
    Page int `json:"page" form:"page" query:"page"`
    Size int `json:"size" form:"size" query:"size"`
}
```

- 默认 `page=1`、`size=20`;
- **`size` 上限 100**(`MaxSize`)—— 超了会被截断,不是报错。
  想要更多就分页,别把上限调大。
- `Normalize()` 把非法值归一(`page <= 0` → 1,`size <= 0` → 20,`size > 100` → 100);
  `Offset()` 算偏移量。

响应:`response.Page(c, list, total, page, size)` → `data = {list, total, page, size}`。

⚠️ `total` 是 `int64`。列表接口一律走分页响应,不要自己返回裸数组 ——
前端分页组件认的是 `data.total`。

## 4. 主键:JSON 上是**字符串**

雪花 ID(`sonyflake`)是 `uint64`,量级会超过 2^53。**JS 的 `number` 装不下**,
所以所有 ID 在 JSON 上以**字符串**出现:

```json
{ "id": "190374829374829374", "user_id": "190374829374829375" }
```

两种写法:

```go
// 推荐:类型层面就是对的
type User struct {
    ID   jsonid.ID   `json:"id"`
    Role jsonid.ID   `json:"role_id"`
}

// 也行:uint64 + `json:",string"`
type User struct {
    ID uint64 `json:"id,string"`
}
```

⚠️ **JS 里必须用字符串比较**。`"190374829374829374" === 190374829374829374` 是 `false`,
而且大数在 JSON.parse 里已经丢精度了。

`jsonid` 包还带一个**扫描器**(`jsonid.ScanDir`):扫全仓找出"整数类型 + id 类
字段名 + 漏了 `,string`"的地方,产出 `Violation` 列表。接新模块时跑一遍。

## 5. 字段命名:**snake_case**

请求和响应里的 JSON 字段名一律 **snake_case**:

```go
type CreateUserReq struct {
    UserName string `json:"user_name"`   // ✅
    UserName string `json:"userName"`    // ❌
}
```

Go 结构体字段名用 CamelCase,但 **json tag 决定 wire 上的名字**。
两条纪律:

1. **改字段名必须同一次改动里改 server + web + 文档** —— 漏一边就是运行时才炸。
2. **DTO 上必须写 json tag**,包括嵌套 struct 和时间字段。

## 6. 时间:统一东八区

`internal/pkg/timeutil`:

```go
timeutil.Now()                  // 东八区当前时间
timeutil.Today()                // "2026-11-01"
timeutil.ParseShanghai(layout, s)
```

- 时间**存**用 UTC / 数据库的 `DATETIME`,**展示**用东八区。
- `system.timezone` 默认 `Asia/Shanghai`;容器里 TZ 没设时靠这个配置兜底。
- ⚠️ 容器里没设 `TZ` 时 `time.Local == UTC`,`0 3 * * *` 会在北京时间 11:00 跑 ——
  这就是 `job.timezone` 必须显式配的原因。

## 7. 中间件链

全局链(`internal/api/router/router.go`,顺序固定):

```
CORS → Trace → SecHeaders → Otel → Recover → BodyLimit → RequestGzip → Gzip
      → Idempotency → APIVersion → AccessLog → Metrics → [Auth → Principal → CSRF → Audit]
```

后四个(Auth / Principal / CSRF / Audit)只在**受保护路由组**上挂。
`Recover` 一定在业务 handler 之前 —— 后台任务里的 panic 不该把进程带走。

单条路由上还可以再挂:

| 中间件 | 用途 |
|---|---|
| `middleware.RBAC(module, action)` | 接口级权限码(见 [casbinx](../security/casbinx.md)) |
| `middleware.RateLimit(scene, n, window, dim)` | 限流(`dim` = `ip` / `user`) |
| `middleware.Breaker(name)` | 熔断 |
| `middleware.Timeout(d)` | handler 超时 |

## 8. 三层权限

| 层 | 谁管 | 怎么写 |
|---|---|---|
| 接口级 | `middleware.RBAC("users", "list")` | 路由注册时挂 |
| 数据级 | [authz](../security/authz.md) | handler 里 `assertor.AssertCan(caller, principal, resource, permCode)` |
| 字段级 | [desensitize](../security/desensitize.md) / [crypto](../security/crypto.md) | 出参脱敏 / 入参加密 |

⚠️ **RBAC 中间件只管"能不能进 handler"**,不保证"能不能看这一条数据"。
数据级校验漏了就是 IDOR。详见 [authz](../security/authz.md)。

## 9. 错误处理

- handler 里返回 `error`,由 `response.FailError(c, err)` 统一转成响应体。
- 业务错误用 `errcode.New(code, msg)` 造,它同时实现了 `error`。
- ⚠️ **不要 `panic` 业务错误**。`Recover` 会兜住它,但那样会丢掉 trace_id 和
  错误码,前端只能看到 500。
- 需要带明细的校验错误用 `errcode.Error.WithDetail(...)` + `response.FailWithData`。

## 10. 审计:写操作会自动留痕

`middleware.Audit()` 挂在受保护路由组上,自动写 `sys_operation_log` +
`sys_data_change_log`。**你不需要在 handler 里手写审计**。

但有两类审计要手写,因为框架不知道:

- **PII 访问**(看了完整手机号 / 身份证)→ [piiaudit](../audit/piiaudit.md)
- **密钥使用** → [cryptaudit](../audit/cryptaudit.md)

## 相关

- [配置体系](configuration.md)
- [包速查表](../reference/packages.md)
