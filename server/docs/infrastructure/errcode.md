# errcode —— 业务错误码:分类、HTTP 映射、错误对象

> HTTP 状态码回答"请求成不成功",业务错误码回答"为什么"。
> 两者分开,前端才能既按 HTTP 分支(网关/监控看这个)又按业务码分支(UI 给具体提示)。

## 功能

errcode 提供三件东西:

1. **分段错误码常量** —— 按业务域编排,段位本身有意义(见下表),前端按码分支。
2. **`Error` 错误对象** —— 携带 `Code` / `Message` / `Detail`。
   `Detail` 是给校验类错误用的字段级明细(前端据此把错误标红到具体输入框)。
3. **业务码 → HTTP 状态码的唯一映射**(`HTTPStatus`)—— handler 里**不要手写
   `400` / `401` / `403`**,一律从业务码推导。

**不解决**:文案国际化(i18n 在前端做)、错误码的持久化。

## 配置

**本包无 yaml 配置** —— 错误码是代码里的稳定契约,不是运行期可改的配置。
文案默认值在 `errcode.go` 的 `defaultMsg` 映射表里。

## 使用方法

### 取码(前端按码分支)

```go
errcode.Success          // 0
errcode.ParamMissing     // 40001
errcode.ParamInvalid     // 40002
errcode.InvalidParam     // 40003
errcode.Unauthorized     // 40100
errcode.TokenExpired     // 40101
errcode.Forbidden        // 40300
errcode.NotFound         // 40400
errcode.Internal         // 50000
```

### HTTP 状态码由业务码推导

```go
errcode.HTTPStatus(errcode.Unauthorized)   // 401
errcode.HTTPStatus(errcode.NotFound)       // 404
errcode.HTTPStatus(errcode.Internal)       // 500
```

### 造一个带明细的业务错误

```go
errcode.New(errcode.InvalidParam, "手机号不合法").
    WithDetail(map[string]any{"field": "phone", "rule": "cn_phone", "value": "1380000"})

errcode.Newf(errcode.InvalidParam, "手机号 %s 不是合法号码", "1380000")
```

`New(code, msg...)` 的 `msg` 是**可选**的:不传(或传空串)时用 `Msg(code)` 取默认文案。
`WithDetail` / `WithMsg` 都改同一个 receiver 并返回它 ⇒ 可以链式调。

### 从 error 取码

```go
code := errcode.CodeOf(err)   // 走 errors.As,能穿透 %w 包装
```

### 取默认文案

```go
errcode.Msg(errcode.ApprovalNoAssignee)
// "审批流程配置有误:解析不出审批人,请联系管理员检查流程配置"
```

完整示例见 `examples/errcode/main.go`。

## 注意事项

### 段位表(新增码先看这里)

| 段 | 域 | 已占用的码 |
|---|---|---|
| `0` | 成功 | `Success` |
| `20xxx` | 登录 / 验证码 / 权限与审核 / 资产 / 资料 / 房屋 / 审批 | 见下 |
| `30xxx` | 平台配置中心 | `30100` `30101` `30200` `30201` `30202` `30203` |
| `40xxx` | 通用 HTTP 语义 | `40000` `40001` `40002` `40003` `40100` `40101` `40102` `40300` `40301` `40302` `40303` `40400` `40900` `41300` `42900` |
| `50xxx` | 框架 / 运行时 | `50000` `50100` `50300` `50400` `50500` `50600` `50601` `50602` `50700` `50701` |

`20xxx` 内部再分段:

| 子段 | 域 | 已占用 |
|---|---|---|
| `201xx` | 登录 / 验证码 / 密码 | `20100` `20101` `20102` `20103` `20104` `20105` `20106` `20107` `20108` |
| `202xx` | 权限与审核 | `20200` `20201` `20202` `20203` `20210` |
| `203xx` | 资产 | `20300` |
| `204xx` | 资料 | `20400` |
| `205xx` | 房屋 / 项目 | `20500` `20501` |
| `206xx` | **审批系统专段**(M83 新开) | `20600` `20601` `20602` `20603` |

- ⚠️ **不要往已占用的段里插**。`20203`(`StateInvalid`)与 `20210`
  (`CoOwnerCodeRequired`)之间的号**已经空着但被保留** —— 不要拿来用。
  `206xx` 就是当年从 `202xx` 里"另开一段"的先例:审批系统有自己的状态机与文案。
- ⚠️ **一个码只能有一个文案**。`defaultMsg` 是"码 → 中文"的一对一映射;
  覆盖文案用 `errcode.New(code, "自定义")` / `WithMsg`,不要改 `defaultMsg`
  (那份是全站兜底文案,改了会影响所有调用点)。
- ⚠️ **分类错了前端就弹错文案**。例:`ApprovalNoAssignee`("配置错了")
  如果混进 `InvalidParam`,前端会弹"参数有误,请重试",用户永远等不到人来修流程模板。
  ⇒ 新增码前先问"**要修的人是谁**",按人分码。

### HTTP 映射

- ⚠️ **`HTTPStatus` 的 `switch` 顺序敏感:细分桶必须排在大桶前面**。
  `401xx` / `403xx` / `404xx` / `409xx` / `413xx` / `429xx` 全都落在
  `40000-49999` 这个大桶里,细分桶排后面会被大桶先吃掉、全部返 400。
  (这个坑真的错过:同一份映射曾有**三份实现**各错各的,2026-10-26 M98 合并成
  `errcode.HTTPStatus` 这一份,`response` 包与文档生成器都调它。)
- ⚠️ **`NotImplemented`(50100)必须映射到 501 而不是 500**。
  "这个 provider 没有这个能力"(该换条路走)和"它炸了"(该重试/该告警)在客户端是
  完全不同的两种处理。混成 500 的代价很具体:前端按 `status` 分支,
  500 走"服务器内部错误"通用文案、**根本不看 body 里的业务码** ——
  已登记 i18n 译文的"该功能尚未实现"永远显示不出来。
- ⚠️ **`2xxxx` / `3xxx` 的业务码映射成 HTTP 200** —— 语义由 body 里的
  `code` + i18n 承载。这是"HTTP 200 + code != 0"的正常失败返回。
- ⚠️ **`HTTPRaw` 已废弃**,与 `HTTPStatus` 完全等价(它保留只是为了不破坏外部调用方)。
  新代码直接用 `HTTPStatus`。

### 取码与包装

- ⚠️ **必须用 `errcode.CodeOf`,不能裸类型断言 `err.(*Error)`**。
  裸断言只认最外层;服务层普遍用 `fmt.Errorf("解析第 %d 关审批人失败: %w", n, err)`
  包装错误 ⇒ 断言失败 ⇒ 一律退化成 `Internal`(50000)⇒ 客户端拿到 500,
  真正的业务码在包装层就被吃掉了。`CodeOf` 走 `errors.As`,能穿透任意层 `%w`。
- ⚠️ **`CodeOf(nil)` 返回 `Success`(0)**,不是错误。
- ⚠️ **`Msg(code)` 对未登记的码返 `"未知错误"`** —— 新增码时忘了写进
  `defaultMsg`,前端就会看到"未知错误"而不是具体原因。

### 源码注释里的两处过期数字

- `errcode.go` 里第一个 const 块的分组注释写的是 **"1xxxx 基础"**,但块内实际值
  是 `40000`~`50000` —— 按**值**看,不要按注释看。
- `validatorx` 包注释与 `handler.BindAndValidate` 的注释都写
  "错误码统一 `errcode.InvalidParam`(40100)" —— **`InvalidParam` 实际是 `40003`**,
  `40100` 是 `Unauthorized`。代码本身用的是常量,是对的;注释里的数字是错的。

## 相关

- 源码:`internal/pkg/errcode/`(`errcode.go` 常量 + 映射 / `error.go` 错误对象)
- 可运行示例:`examples/errcode/main.go` —— `go run ./examples/errcode`
- 统一响应体 → [response](response.md)
- 跨模块约定(响应体 / 错误码 / 字段命名) → [框架约定](../getting-started/conventions.md)
