# grayx —— 灰度发布分组透传

> 让 canary / beta / production 这些分组信息在请求处理链中可读,
> 业务代码据此走不同分支。

## 功能

grayx 提供的只有两件事:

1. **`WithReleaseTag(ctx, tag)`** —— 把分组标签塞进 ctx
2. **`ReleaseTag(ctx)`** —— 从 ctx 读标签(缺失返 `""`)

外加一组 header 名常量:`HeaderRelease`(`X-Release`)、`HeaderCanary`(`X-Canary`)、
`HeaderCohort`(`X-Cohort`)。

入口中间件(`internal/api/middleware/gray.go`)负责:读 header → 注入 ctx →
**把生效的 tag 回写到响应头** `X-Release`(供客户端 / 网关观测)。

**关键设计**:

- tag 跟随 **context** 流转,不是 goroutine local ⇒ 跨 RPC / 异步任务也能带过去
- 单一 source of truth:入口 middleware 读 header,下游一律从 ctx 取
- 缺失 tag → 返 `""`,不强制要求,业务判空即可

**不解决什么**:不判定分组、不做流量分配、不做开关管理 ——
"这个用户该不该进灰度"是业务/网关的事,grayx 只负责**把已经决定好的分组
往下游传**。

### 接线状态(读源码前先看这个)

⚠️ **`middleware.GrayRelease` 目前没有挂在任何路由上** ——
`internal/api/router/router.go` 里找不到它。所以现网
`grayx.ReleaseTag(ctx)` **恒返 `""`**,灰度分组这条链路是"先落地、后接线"。
要用得先在 router 里挂上中间件。

## 配置

### 代码侧

**该包无构造选项** —— 两个包级函数 + 四个常量:

| 常量 | 值 | 说明 |
|---|---|---|
| `grayx.HeaderRelease` | `"X-Release"` | 主推荐 |
| `grayx.HeaderCanary` | `"X-Canary"` | 备选,语义更直白 |
| `grayx.HeaderCohort` | `"X-Cohort"` | 备选,按用户分桶 |
| `grayx.DefaultRelease` | `""` | 缺失时的默认 tag(空 ⇒ 当作 production) |

### yaml 侧

**该包无 yaml 配置。** header 名在中间件装配时给:

```go
h.Use(middleware.GrayRelease(grayx.HeaderRelease))
```

## 使用方法

```go
// 入口中间件
h.Use(middleware.GrayRelease(grayx.HeaderRelease))

// 业务 handler
tag := grayx.ReleaseTag(ctx)
if tag == "canary" {
    // 走新逻辑
}

// 业务日志带 tag
logger.L().Info("order paid",
    zap.String("release", grayx.ReleaseTag(ctx)),
    zap.Int64("order_id", id),
)

// 跨服务 / 异步任务:派生带 tag 的新 ctx
derived := grayx.WithReleaseTag(parentCtx, "beta")
```

## 注意事项

- ⚠️ **header 是客户端填的 ⇒ tag 是提示,不是授权**。
  任何人发一个 `X-Release: canary` 就能进灰度分支。把灰度当成"新功能开关"
  没问题,把它当成"权限"就是安全洞。真正的灰度准入(用户是否在名单里)
  要在**服务端**判,header 只用来告诉服务端"客户端自称是哪一组"。
- ⚠️ **tag 值不做任何校验** —— 空串以外的任何字符串都会被原样存进 ctx、
  原样回写到响应头。原样进日志字段时要注意格式串注入。
- ⚠️ **`WithReleaseTag(nil, tag)` 会 panic**(`context.WithValue` 不接受 nil 父 ctx)。
  `ReleaseTag(nil)` 是安全的(返 `""`)。
- ⚠️ **中间件读的 header 有优先级,不是"读指定的那个"**:
  `primary` → `X-Canary` → `X-Cohort`。所以配 `GrayRelease("X-Canary")` 时
  **`X-Release` 永远不会被读**;配 `GrayRelease("X-Cohort")` 时读的是
  `X-Cohort` → `X-Canary`,同样跳过 `X-Release`。
- ⚠️ **回写响应的头恒是 `X-Release`**,哪怕 tag 是从 `X-Canary` 读来的 ——
  客户端发 `X-Canary: beta`,响应里回来的是 `X-Release: beta`。
- ⚠️ **缺失 tag 时 ctx 里根本没有这个 key**(中间件只在 tag 非空时才注入),
  但 `ReleaseTag` 一律返 `""` —— 业务**判空即可,不用判 nil**。
- ⚠️ **下游服务自己的 `GrayRelease` 中间件会覆盖上游带过来的 tag** ——
  这是**故意的**(防客户端伪造 header 直接进灰度分组)。跨服务传递灰度信息
  要走 `WithReleaseTag` 派生的 ctx,并且下游要有意识地决定是否采信。
- ⚠️ **`WithReleaseTag` 是派生,不改父 ctx** —— 父 ctx 的 tag 保持原样。

## 相关

- 源码:`internal/pkg/grayx/`(中间件在 `internal/api/middleware/gray.go`)
- 可运行示例:`examples/grayx/main.go` —— `go run ./examples/grayx`
- tag 打进日志的写法 → [logger](logger.md)
- 真正做权限判定的地方 → [authz](../security/authz.md)
