# ratelimitx —— 进程内限流原语(令牌桶 / 漏桶 / per-key 桶)

> 限流有两种做法:跨副本一致的(Redis 滑动窗口),和单进程内的(纯内存桶)。
> ratelimitx 是后者 —— 极快、零依赖,但**只在一个进程内有效**。
>
> ⚠️ **Windows 上没有官方 Redis** —— 要跨副本一致的限流(走 kvstore)就得先有 Redis:
> 用 [Memurai](https://www.memurai.com/get-memurai)(Redis 协议兼容,装成 Windows 服务,
> 默认 `127.0.0.1:6379`、无密码)。**Developer 版仅供开发/测试,不许上生产。**
> 详见 [快速开始](../getting-started/README.md)。

## 功能

ratelimitx 提供三种限流算法,覆盖三类场景:

| 类型 | 构造 | 语义 | 适合 |
|---|---|---|---|
| 令牌桶 `TokenBucket` | `NewTokenBucket(capacity, ratePerSec)` | 桶里攒着令牌就能一次放行一批,之后匀速补 | 突发可放过 + 平均速率控制 |
| 漏桶 `LeakyBucket` | `NewLeakyBucket(capacity, ratePerSec)` | 队列水位满就拒,没有"攒令牌" | 削峰填谷 / 并发上限 |
| per-key 桶池 `KeyedBucket` | `NewKeyedBucket(cap, rate, maxKeys, ttl)` | 每个 key 一个桶,LRU 上限 + 空闲 TTL | per-user / per-IP / per-API-key |

三者都是**纯内存、无外部依赖、线程安全**:

- 单桶用 `atomic.Int64` + `lastRefill` 做无锁算法(CAS 重试)
- `KeyedBucket` 持 LRU + `sync.Mutex`

### 它不解决什么

- **不跨副本**。多副本部署时每个副本各限各的(见「注意事项」第 1 条)
- 不排队、不阻塞 —— 拒绝时返 `false`,由调用方决定返 429 还是排队
- 不做滑动窗口(那是 [kvstore](../storage/kvstore.md) 的 `Window`)
- 不做分布式锁

## 配置

### yaml 侧

**该包无 yaml 配置。**

⚠️ yaml 里那个 `ratelimit:` 块**不是给这个包的**:

```yaml
ratelimit:
  global_qps: 1000
  per_ip_qps: 50
  login_fail_per_hour: 10
```

这三个键在 `config.RateLimitConfig` 里定义了,但**全仓零消费方** ——
`grep "GlobalQPS\|PerIPQPS\|LoginFailPerHour"` 只命中定义行。真正的限流参数是
`internal/api/router/router.go` 里硬编码的:

```go
middleware.RateLimit("login", 10, time.Minute, "ip")
```

而 `middleware.RateLimit` 走的是 kvstore 的**跨副本滑动窗口**,不是 ratelimitx。
⇒ 改 yaml 那三个键**不会有任何效果**。

### 代码侧

所有参数都在构造函数上,没有配置项:

| 参数 | 含义 | 非法值的兜底 |
|---|---|---|
| `capacity` / `cap` | 桶容量(最大突发) | `< 1` → **1** |
| `ratePerSec` / `rate` | 每秒补充 / 漏出速率 | `< 1` → **1** |
| `maxKeys` | LRU 上限 | `< 1` → **1000** |
| `ttl` | 单桶空闲 TTL | `≤ 0` → **10 分钟** |

## 使用方法

### 令牌桶:允许突发

```go
tb := ratelimitx.NewTokenBucket(5, 2)   // 容量 5,每秒补 2
if !tb.Allow() {
    return errTooManyRequests
}
fmt.Println(tb.Available(), tb.Stats())
```

`AllowN(n)` 一次要多个 token(批量导入按条数扣);桶里不够就**整笔拒绝**。

### 漏桶:严格平滑

```go
lb := ratelimitx.NewLeakyBucket(5, 2)   // 队列上限 5,每秒漏 2
if !lb.Allow() {
    return errOverflow
}
fmt.Println(lb.Level(), lb.Stats())
```

### per-key 桶池

```go
kb := ratelimitx.NewKeyedBucket(2, 1, 1000, 10*time.Minute)
defer kb.Stop()                          // 必须 Stop,见「注意事项」

if !kb.Allow("IP-A") { /* 限流 */ }
if !kb.Allow("IP-B") { /* 另一个 key,互不影响 */ }

fmt.Println(kb.Len())                    // 当前 key 数
fmt.Println(kb.Get("IP-A").Stats())      // 拿桶看状态
```

完整可运行示例见 `examples/ratelimitx/main.go`。

## 注意事项

- ⚠️ **根本局限:进程内。多副本部署时每个副本各限各的,实际阈值 = 配额 × 副本数**。
  配 50 QPS、部署 4 个副本,集群实际能过 200 QPS。
  ⇒ **真正跨副本的限流走 kvstore**(Redis ZSET 滑动窗口)—— `middleware.RateLimit(scene, max, window, dim)`,
  那是另一个包的事,见 [kvstore](../storage/kvstore.md)。
- ⚠️ **包注释里的 `ratelimitx.InitMetrics(metricsx.Registry())` 不存在**。
  全仓只有 `cachex.InitMetrics` 和 `cronx.InitMetrics`。照抄那行**编译不过**。
  指标要自己从 `Stats()` / `BucketStats` / `LeakyStats` 里取。
- ⚠️ **`KeyedBucket` 的热路径也有锁**。结构体注释写着"已有桶访问无锁",但
  `Get` 一进来就 `k.mu.Lock()`,而 `Allow` 正是走 `Get` ⇒ **每次 Allow 一次全局
  互斥**。注释与实现不符,别照着注释估并发能力。
- ⚠️ **漏桶不排队、不阻塞**。`LeakyBucket.Allow()` 只判断"队列是否满",
  **没有取队列的 API** —— 所谓"严格平滑"得由调用方自己按 `rate` 的节奏取走东西
  才成立。实际用起来它就是"并发数上限"。
- ⚠️ **`NewKeyedBucket` 会起一个后台 goroutine**(每分钟扫一次清过期桶)。
  不调 `Stop()` 就泄漏 —— 反复构造 `KeyedBucket` 的代码会一路漏 goroutine。
- ⚠️ **参数 < 1 会被兜底成 1,不是"关闭"**。`NewTokenBucket(0, 0)` 得到的是
  "容量 1、每秒 1 个",不是不限流。
- ⚠️ **令牌桶初始是满桶**。刚构造出来能一次性放行 `capacity` 个请求
  (`tokens` 初始化成 `capacity * 1e6`)。
- ⚠️ **`AllowN` 不做部分放行**。桶里只剩 2 个、要 3 个 ⇒ 整个拒,不是放 2 个。
- ⚠️ **`Available()` / `Level()` 会改状态**。它们先 `refill` / `drain` 再读,
  不是纯查询 —— 拿它当探针会消耗桶的额度。
- ⚠️ **`Stats().Allowed` 对 `AllowN` 记的是 token 数不是请求数**(`allowed.Add(int64(n))`)。
- ⚠️ **零生产调用方**。`internal/guard` 的可达性扫描把它列为"库类原语 + 单测齐全,
  但零生产调用方;它们**是故意留着的**(接线点在业务线)"。
  ⇒ 用它之前先确认自己要接的是"进程内限流"而不是"跨副本限流"。

## 相关

- 源码:`internal/pkg/ratelimitx/`
- 可运行示例:`examples/ratelimitx/main.go` —— `go run ./examples/ratelimitx`
- 跨副本限流(计数 + 滑动窗口) → [kvstore](../storage/kvstore.md)
- 短信发送限流(冷却 / 1 分钟 / 1 天三道闸) → [smslimit](../messaging/smslimit.md)
- 登录失败计数 → [loginlock](loginlock.md)
