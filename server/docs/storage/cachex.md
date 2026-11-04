# cachex —— 两级缓存(L1 本地 LRU + L2 Redis)

> 字典、配置、资源树这些"读多写少、不能每次都打 DB"的数据,需要一层缓存。
> cachex 提供 **L1 进程内 LRU + L2 Redis** 两级,以及跑在上面的**三防**。
>
> ⚠️ **Windows 上没有官方 Redis** —— 用 [Memurai](https://www.memurai.com/get-memurai)
> (Redis 协议兼容,装成 Windows 服务,默认 `127.0.0.1:6379`、无密码)。
> **Developer 版仅供开发/测试,不许上生产。** 详见 [快速开始](../getting-started/README.md)。

## 功能

cachex 解决三件事:

1. **两级缓存**
   - **L1**:进程内 LRU,微秒级,**不跨实例**
   - **L2**:Redis,跨实例共享
2. **cache-aside**:`GetOrLoadTyped` —— 未命中才回源,回源逻辑只写一次。
3. **三防**(跑在接口上,不是绑死在 Redis 上):
   - **单飞(singleflight)**:同一个 key 并发未命中时,只回源一次
   - **空值占位(null sentinel)**:查不到的数据也缓存一小段时间,防穿透
   - **TTL 抖动**:过期时间加 ±20% 随机偏移,防批量同刻过期(雪崩)

三防**只有一份实现**(`safe.go` 里的泛型函数)—— 包内没有任何第二处
singleflight / null sentinel / jitter 的组合逻辑。

## 配置

### 代码侧(构造选项)

```go
cachex.New(rdb, opts...)                    // Redis 后端
cachex.NewWithBackend(backend, opts...)      // 任意后端(内存 / Redis)
cachex.NewTwoLevel(l1Cap, l1TTL, safe)      // 组两级
```

| 选项 | 默认 | 说明 |
|---|---|---|
| `WithPrefix(prefix string)` | `""` | key 前缀。同一套 Redis 跑多业务时用它隔离;**空前缀会被拒绝**(`ErrEmptyPrefix`) |
| `WithJitter(pct float64)` | `0.2` | TTL 抖动比例,取值 0~0.5 |
| `WithNullTTL(ttl time.Duration)` | 2 分钟 | 空值占位 TTL,`0` = 禁用 |

### yaml 侧(`cache:` 块)

```yaml
cache:
  mode: auto          # auto = 有 Redis 用 Redis,没有退纯 L1
  prefix: ""          # key 前缀,多套系统共用一套 Redis 时填
  l1_cap: 5000        # L1 按 entry 数算;0 → 5000
  l1_ttl: 10s         # L1 TTL;0 → 10s
  null_ttl: 2m        # 空值占位 TTL,穿透防御;设 0 禁用
  jitter: 0.2         # TTL 随机抖动比例 0~0.5,防批量同刻过期
```

`cache.mode` 漏配不会出事 —— 缓存失效只会变慢,不会出安全事故,所以它默认 `auto`
(而不是像 `captcha.*` 那样给安全默认值)。

## 使用方法

### 装配

```go
backend := cachex.NewMemoryBackend()          // 或 cachex.NewRedisBackend(rdb)
safe := cachex.NewWithBackend(backend,
    cachex.WithPrefix("demo:"),
    cachex.WithJitter(0.2),
    cachex.WithNullTTL(2*time.Minute),
)
tl := cachex.NewTwoLevel(5000, 10*time.Second, safe)
cachex.SetDefault(tl)
```

生产上由 `cmd/server` 从 `cache:` 配置块装配,业务代码**只调 `cachex.SetDefault` 之后
拿全局实例**。

### 读:cache-aside 主力入口

```go
d, hit, err := cachex.GetOrLoadTyped(ctx, tl, "demo:dict:cacheaside", 5*time.Minute,
    func(ctx context.Context) (*dict, error) {
        return loadDictFromDB(ctx, "gongqu")
    })
```

⚠️ **用 `GetOrLoadTyped`,不要用 `Safe.GetOrLoad`**。后者返 `any`,内部把 JSON
反序列化进了 `any` ⇒ 业务拿到的是 `*map[string]any`,要拿回结构体还得自己再
Marshal 一次。这个摩擦正是"框架能力写完却零调用"的原因。

### 写 / 删 / 批量失效

```go
tl.Set(ctx, key, value, ttl)
tl.Get(key)                    // (any, bool)
tl.Delete(ctx, key)
tl.DeletePrefix(ctx, "demo:dict:")   // 按前缀批量失效
```

前缀 + `DeletePrefix` 是按业务批量失效的手段(`dict:` / `cfg:` / `resource:`)。

### 回源失败怎么办

回源的 error **原样抛出**,不会被当成"缓存命中"。

### 缓存里的值反序列化不回来(fail-open)

发版改了结构、老 key 还在时,`GetOrLoadTyped` **不当错误**,记一条 warn 后直接回源。
慢一点总比把错数据返给业务好。

### 启动期自检

```go
cachex.Verify(ctx, tl)   // 探一次 L2 —— "配了但连不上"变成启动日志里的一行
```

而不是之后每个请求 WARN 一次。

## 注意事项

- ⚠️ **`MemoryBackend` 不能当多副本部署的 L2**。每个副本各持一份,失效通知到不了
  别的副本 —— "改完权限立刻生效"会退化成"最多 TTL 后生效"。需要跨副本一致就用 Redis。
- ⚠️ **L2 不可用时自动降级到 L1,不报错**。这是有意的(fail-open),但意味着
  "Redis 挂了"在业务上是看不出来的 —— 看指标和启动日志。
- ⚠️ **`WithPrefix` 拒绝空前缀**(`ErrEmptyPrefix`)。空前缀会让 `DeletePrefix`
  变成"删掉整个库"。
- ⚠️ **前缀是拼在 key 上还是拼在业务 key 上,取决于装配方式**。示例里
  `WithPrefix("demo:")` 与手写的 `"demo:dict:a"` 都在 —— 实际接入时按你自己的
  key 规划走,别两处都加。

## 相关

- 源码:`internal/pkg/cachex/`
- 可运行示例:`examples/cachex/main.go` —— `go run ./examples/cachex`
- 指标:`app_cache_ops_total{layer,op,result}`(见 [metricsx](../infrastructure/metricsx.md))
- 键值计数 / 滑动窗口是另一回事 → [kvstore](kvstore.md)
