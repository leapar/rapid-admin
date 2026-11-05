# pagination —— 分页参数解析与上限保护

> 手写分页有三处容易踩的坑,本包把它们收在一起:
> page/size 越界、`Offset` 算成负数、忘了调 `Normalize`。

## 功能

pagination 提供两件东西:

1. **`PageRequest`** —— 请求侧的分页参数(`page` / `size`),
   带 `json` / `form` / `query` 三个 tag,三种取参方式都能用。
2. **`Normalize()` / `Offset()`** —— 规整与偏移计算,`Offset()` **自带兜底**,
   忘了调 `Normalize()` 也不会算出负数。

**不解决**:响应侧的分页结构(在 [response](response.md) 的 `PageData`)、
SQL 生成(在 repository 层)。

## 配置

**本包无 yaml 配置** —— 上限(`MaxSize`)是常量,写死在代码里。

## 使用方法

### 请求侧

```go
pr := pagination.PageRequest{Page: page, Size: size}
pr.Normalize()
// pr.Page ∈ [1, ∞),pr.Size ∈ [1, 100]

offset := pr.Offset()   // (page-1)*size,永远 ≥ 0
```

`handler.BaseHandler.GetPage(c)` 已经封装好这一步:从 query 读 `page` / `size`,
`Normalize()` 后返回。

### 响应侧

```go
response.Page(c, list, total, page, size)
// data = {"list": [...], "total": 137, "page": 2, "size": 20}
```

### 常量

| 常量 | 值 | 说明 |
|---|---|---|
| `pagination.DefaultPage` | `1` | `page <= 0` 时的默认值 |
| `pagination.DefaultSize` | `20` | `size <= 0` 时的默认值 |
| `pagination.MaxSize` | `100` | `size` 上限,超出**静默截断**到 100 |

完整示例见 `examples/pagination/main.go`。

## 注意事项

- ⚠️ **`MaxSize = 100` 是静默截断,不是报错**。后端把 `size=1000` 悄悄拉回 100,
  **前端必须知道** —— 被截到 100 却仍按 1000 翻页,前端会以为"还有更多",
  一直翻到底。要更细的分页就分多次拉,别把上限调大。
- ⚠️ **`Offset()` 对副本做 `Normalize()`,不改调用方的结构体**。
  一个"取值"方法不该有副作用 ⇒ `pr.Offset()` 之后 `pr.Page` 仍是原值。
  需要规整后的值,就显式调 `pr.Normalize()`。
- ⚠️ **`Offset()` 自己兜底是为了防负数**。老实现直接 `(p.Page - 1) * p.Size`,
  `Normalize()` 是**另一个方法** —— 调用方忘了调 ⇒ `Page=0` ⇒ `Offset = -Size`
  ⇒ SQL `OFFSET -20` 直接报错,而且是在 repository 层炸,
  报错信息跟"分页参数非法"八竿子打不着。
- ⚠️ **`Normalize()` 只规整 `page` / `size`,不校验 `total`**。
  `page` 超出总页数时不会报错,返回空列表 + 正确的 `total`。
- ⚠️ **`pagination.PageResponse` 不是 HTTP 响应体**。
  实际线上响应体是 `response.PageData`(外面多包了一层 `code` / `message` / `timestamp`)。
  `PageResponse` 只是个通用 result 类型,别拿它直接返给前端。
- ⚠️ **`total` 是 `int64`**。列表接口一律走分页响应,不要自己返回裸数组 ——
  前端分页组件认的是 `data.total`。

## 相关

- 源码:`internal/pkg/pagination/`(`page.go`)
- 可运行示例:`examples/pagination/main.go` —— `go run ./examples/pagination`
- 统一响应体 → [response](response.md)
- 跨模块约定(分页请求/响应) → [框架约定](../getting-started/conventions.md)
