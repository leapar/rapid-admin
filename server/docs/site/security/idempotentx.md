# idempotentx —— 幂等键(防重复提交)

> 写操作(POST / PUT / PATCH / DELETE)重复请求不重复执行业务逻辑。
> 客户端在 `Idempotency-Key` 头里带一个 key,同 key 在 TTL 内重放直接返首次结果。
>
> ⚠️ **Windows 上没有官方 Redis** —— 本包**没有 Redis 就整个 no-op**(幂等保护直接失效),
> 开发机上要它就装 [Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,
> 装成 Windows 服务,默认 `127.0.0.1:6379`、无密码)。
> **Developer 版仅供开发/测试,不许上生产。** 详见 [快速开始](../getting-started/README.md)。

## 功能

idempotentx 提供**两把 key**的幂等管理,而不是一个状态机:

```
idem:res:<key>    结果(TTL 24h)—— 业务跑完才有,内容是 {state, http_status, body}
idem:lock:<key>   锁  (TTL 30s)—— 有人在跑,跑完就删
```

为什么要分两个:锁要短(30s,免得业务卡住把 key 锁死),结果要长(24h,免得用户
重试时业务又跑一遍)。**一个 key 做不到"两套 TTL"。**

核心接口:

| 接口 | 作用 |
|---|---|
| `idempotentx.New(rdb, lockTTL, resultTTL)` | 造一个独立实例(不碰全局) |
| `idempotentx.Init(rdb, lockTTL, resultTTL)` | 装全局,只调一次 |
| `idempotentx.M()` | 取全局 manager(未 Init 返 nil) |
| `Manager.AcquireOrGet(ctx, idemKey)` | 抢锁 / 命中结果 / `ErrInFlight` |
| `Manager.Save(ctx, idemKey, httpStatus, body)` | 业务完成后写结果 |
| `Manager.Release(ctx, idemKey)` | 业务异常时释放锁 |
| `Manager.String()` | 调试用 |

### "正在跑"不是第三种状态

结果 key 里**只有** `StateDone` 和 `StateFailed`。"正在跑"这个状态活在
**锁 key 的存在性**里(`idem:lock:<k>` 存在 = 有人在跑)。旧版本那个
`StateRunning` 常量已被删掉 —— 留着它是有害的:会让人写出
"结果里 State==running 就当未命中"这种错代码,而结果 key 里压根不会出现 running。

### 它不解决什么

- 不做分布式锁(锁只保证"同一个 key 同时只有一个请求在跑",不保证跨 key 的顺序)
- 不做业务事务回滚(它只重放响应,不撤销已写入的数据)
- 不保证客户端会带 key(见下文「注意事项」第 10 条)

## 配置

### yaml 侧

**该包无 yaml 配置。**

锁 TTL / 结果 TTL 在 `cmd/server/main.go` 里硬编码:

```go
idempotentx.Init(repository.GetRedis(), 30*time.Second, 24*time.Hour)
```

`New()` 在参数 ≤0 时兜底成 `DefaultLockTTL`(30s)/ `DefaultResultTTL`(24h)。

### 代码侧

| 常量 | 值 | 说明 |
|---|---|---|
| `idempotentx.DefaultLockTTL` | `30 * time.Second` | 锁 TTL |
| `idempotentx.DefaultResultTTL` | `24 * time.Hour` | 结果缓存 TTL |

跨域调用时 `Idempotency-Key` 必须出现在 `cors.allow_headers` 里(配置模板已经
放行了)。

## 使用方法

### 装配(main 里一次)

```go
idempotentx.Init(repository.GetRedis(), 30*time.Second, 24*time.Hour)
```

### 挂中间件(router 里)

```go
h.Use(middleware.Idempotency())
```

中间件自己读 `Idempotency-Key` 头、按 method 过滤、抢锁、重放、写结果。

### 直接用 Manager(不挂中间件时)

```go
m := idempotentx.New(rdb, 30*time.Second, 24*time.Hour)

acquired, cached, err := m.AcquireOrGet(ctx, idemKey)
switch {
case err == idempotentx.ErrInFlight:
    // 有另一个请求正在跑 → 409
case !acquired && cached != nil:
    // 命中:cached.State / cached.HTTPStatus / cached.Body 原样重放
case acquired:
    // 首次:跑业务,跑完
    m.Save(ctx, idemKey, status, body)
}
```

`Result` 的形状:

```go
type Result struct {
    State      State     `json:"state"`       // StateDone / StateFailed
    HTTPStatus int       `json:"http_status"`  // 原始响应状态(200/201/...)
    Body       []byte    `json:"body"`         // 原始响应 body(含 code/message/data/request_id)
    WrittenAt  time.Time `json:"written_at"`
}
```

`Body` 存的是**原始响应字节**,所以重放时客户端拿到的和第一次完全一样。

完整可运行示例见 `examples/idempotentx/main.go`。

## 注意事项

- ⚠️ **Redis 没配 ⇒ 中间件整个 no-op,不是报错**。`Init(nil, ...)` 只打一条 WARN
  (`idempotentx: rdb is nil, manager disabled`);`AcquireOrGet` 里 `m.rdb == nil`
  直接放行。⇒ 生产忘了配 Redis ⇒ **所有写操作都没有幂等保护**,症状只是日志里
  一行 WARN。
- ⚠️ **Redis 运行期故障也是放行(fail-open)**。`Get` 出错、`SetNX` 出错,都返
  `acquired=true`。⇒ Redis 抖动期间写操作全部失去幂等保护,**没有错误码、没有 5xx**。
  (取舍的理由是"挡住所有写操作比不幂等更糟";但代价是这段时间里重复提交会真的重复执行。)
- ⚠️ **同 key 并发 ⇒ 409,不是排队**。第二个请求拿到 `ErrInFlight`,中间件返
  HTTP 409 + `{"code":40900,...}`。客户端必须自己退避重试;当成成功处理会丢写。
- ⚠️ **409 的响应体是手写的,不走统一信封**。中间件直接 `c.Response.SetBody(...)`
  塞一段字面量 JSON(`code` 40900 + `message` + `data: null`),
  **没有 `timestamp` / `trace_id` 字段**(`response.Fail` 才有)。前端按统一信封解析
  时要能容忍缺字段。命中重放的路径走的是另一条(`c.Response.SetBody(cached.Body)`),
  那份 body 是首次响应的**原始字节**,信封是全的 —— 两条路径的 body 形状不一样。
- ⚠️ **重放响应带一个 `Idempotent-Replay: true` 头**,客户端可以据此区分
  "这次是重放"。
- ⚠️ **业务失败(4xx/5xx)也缓存**。`Save` 里 `httpStatus >= 400` 会把 state 置成
  `StateFailed`,但**照样写进结果 key**(TTL 24h)。⇒ 客户端拿同一个 key 重试,
  拿回同一个错误,业务不会重跑。
- ⚠️ **"业务压根没跑成"的响应不缓存**。判据在 `internal/api/middleware/idempotency.go`
  的 `cacheableResult`:`c.IsAborted()` 或状态码 ∈ {401, 403, 413, 429} ⇒ 不缓存,
  并且**释放锁**。⚠️ **5xx 不在拒绝之列** —— handler 抛的 500、超时的 504 都缓存
  (业务可能已经写了数据 ⇒ 宁可少跑一次,不能重跑)。这条边界不能动。
- ⚠️ **panic 不缓存,但锁会被释放**。中间件用 `defer` 调 `mgr.Release`;不释放的话
  这个 key 被锁 30s,客户端拿同一个 key 重试只会拿到 409 —— 而实际上没有任何请求在跑。
- ⚠️ **空 key 直接放行**。否则所有人共用 `idem:lock:` 一把锁,而 `Save` / `Release`
  对空 key 直接 return ⇒ 那把锁**永远不会被释放**,之后每个空 key 的请求都 409
  直到 TTL 到期。
- ⚠️ **键空间必须是单射**。旧实现是 `idem:<k>`(结果)与 `idem:<k>:lock`(锁),两个
  命名空间**重叠**:客户端 key `"foo:lock"` 的结果 key 正好是客户端 key `"foo"` 的
  锁 key ⇒ 两个互不相干的请求互相把对方的幂等状态搞坏,**且没有任何报错**。
  现在用 `idem:res:` / `idem:lock:` 两个前缀分开,任何客户端 key 都撞不上。
- ⚠️ **前端每个写请求自动生成一个全新的 Idempotency-Key**。`web/src/api/client.ts`
  的请求拦截器对 POST/PUT/PATCH/DELETE 自动注入 `genIdempotencyKey()`(UUID v4)。
  ⇒ **用户双击提交 = 两个请求 = 两个 key = 后端幂等不生效**。幂等只在"同一个 key
  被重放"时有效;调用方要防双击,得自己把 key 固定下来传进去(拦截器会保留调用方
  已设的值 —— M101 修过"大小写不同就被覆盖"的老 bug)。
- ⚠️ **中间件不挂在全局时 `M()` 可能返 nil**。`idempotentx.M()` 未 `Init` 返 nil,
  中间件判到 nil 直接放行(不是报错)。

## 相关

- 源码:`internal/pkg/idempotentx/`
- 中间件:`internal/api/middleware/idempotency.go`
- 可运行示例:`examples/idempotentx/main.go` —— `go run ./examples/idempotentx`
- 计数 / 滑动窗口存储 → [kvstore](../storage/kvstore.md)
- 接口签名(另一种"请求没被改"的保证) → [signx](signx.md)
- 统一响应体(被缓存的 `Body` 长什么样) → [response](../infrastructure/response.md)
