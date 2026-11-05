# response —— 统一响应体:成功 / 失败 / 分页三种形状

> 后端所有接口返同一种形状,前端只写一套解析。

```json
{
  "code": 0,
  "data": { },
  "message": "ok",
  "timestamp": 1730000000000,
  "trace_id": "abc123"
}
```

## 功能

response 解决三件事:

1. **统一响应体**(`Response`)—— `code` / `data` / `message` / `timestamp` / `trace_id`。
2. **三种成功形状 + 三种失败写法** —— 见「使用方法」。
3. **TraceID 回传** —— 响应体里的 `trace_id` 与日志里的同一个 id,
   用户截图里一个 id 就能定位到那条日志。

**不解决**:错误码本身(在 [errcode](errcode.md))、分页参数解析(在
[pagination](pagination.md))、鉴权。

## 配置

**本包无 yaml 配置** —— 响应体形状是跨模块契约,写死在代码里。

## 使用方法

### 三种成功

```go
response.OK(c)                                     // 无数据
response.OKWithData(c, map[string]any{"id": 8801})  // 带数据
response.OKWithMessage(c, data, "共 3 条")          // 带数据 + 自定义文案
```

### 分页

```go
response.Page(c, list, total, page, size)
// data = {"list": [...], "total": 137, "page": 2, "size": 20}
```

### 三种失败

```go
response.Fail(c, errcode.InvalidParam, "手机号不合法")

response.FailWithData(c, errcode.InvalidParam, "手机号不合法",
    map[string]any{"field": "phone", "value": "1380000"})

response.FailError(c, err)   // 从 error 里抽 code + msg
```

业务代码里推荐直接用 `handler.BaseHandler` 的封装(`OK` / `Fail` / `FailCode` / `Page`),
它会 `errors.As` 解包 `*errcode.Error` 并自动带上 `Detail`。

### TraceID

```go
tid := response.TraceIDFrom(c)   // 优先 X-Trace-Id,其次 X-Request-Id,都没有返 ""
```

两个 header 常量:`response.HeaderTraceID`(`X-Trace-Id`)、
`response.HeaderRequestID`(`X-Request-Id`)。写入方是 `middleware/trace.go`
与 `middleware/otel.go`。

### 不起 HTTP 服务就能看输出

```go
c := app.NewContext(0)
response.OKWithData(c, map[string]any{"id": 8801})
fmt.Println(string(c.Response.Body()))
```

完整示例见 `examples/response/main.go`。

## 注意事项

- ⚠️ **HTTP 状态码与 body 里的 `code` 是两回事**。HTTP 说"请求成不成功",
  `code` 说"为什么"。网关 / 监控看前者,前端分支看后者。
  失败响应的 HTTP 状态由 `errcode.HTTPStatus(code)` 推导 —— **不要在 handler 里手写
  `c.JSON(400, …)`**,那会让同一类错误在不同接口上状态码不一致。
- ⚠️ **`FailError` 的 message 是 `err.Error()`,不是纯文案**。
  `*errcode.Error.Error()` 渲染成 `"[40003] 参数无效"` —— 带方括号和数字前缀。
  前端会原样弹出来。要干净文案,用 `handler.BaseHandler.Fail`
  (它解包后取 `e.Message`),或自己 `errcode.New(code, msg)`。
- ⚠️ **`FailError` 会丢 `Detail`** —— 它只传 code + msg,不带字段级明细。
  校验错误要用 `FailWithData` / `BaseHandler.Fail`。
- ⚠️ **`Data` 字段带 `omitempty`** —— 成功且无数据时,JSON 里**没有 `data` 键**
  (不是 `"data": null`)。前端取 `res.data.data` 拿到的是 `undefined`,
  写 `res.data.data.length` 会炸。前端要么用可选链,要么后端该给数据就给数据。
- ⚠️ **`timestamp` 是毫秒**(`time.Now().UnixMilli()`),不是秒。
  注释与示例里出现过的 `1730000000` 那种 10 位数是秒 —— 传前端算时间差会差 1000 倍。
- ⚠️ **`trace_id` 也是 `omitempty`** —— `TraceIDFrom` 拿不到就是空串,
  于是响应体里**整个 `trace_id` 键消失**。它依赖 `middleware/trace.go` /
  `middleware/otel.go` 已挂载并写回了请求头;自己 new 一个 ctx 不带 header 时必然没有。
- ⚠️ **`TraceIDFrom` 读的是请求头,不是响应头**。`otel.go` 在收尾时才把
  `X-Trace-Id` 写进响应头 —— 响应**体**里的 `trace_id` 取的是**请求进来时就有的**那个值。
- ⚠️ **HTTP 状态码映射只存在于 `errcode.HTTPStatus` 一处**。
  `response` 包原来有一份私有拷贝,`cmd/gen-errcode-doc` 里还有第三份,三份各错各的
  (413 桶、501/503/504 都对不上)。M98 已合并成一份 —— 新增码只改 `errcode` 一处。
- ⚠️ `time.go` 里的 `nowMilli()` 是**零调用方的死函数**(与 `Now()` 重复)。
  改时间戳口径时改 `Now()`,别改它。

## 相关

- 源码:`internal/pkg/response/`(`response.go` 响应体 / `time.go` 时间戳)
- 可运行示例:`examples/response/main.go` —— `go run ./examples/response`
- 错误码与 HTTP 映射 → [errcode](errcode.md)
- 分页参数 → [pagination](pagination.md)
