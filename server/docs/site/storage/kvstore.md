# kvstore —— 计数 + 滑动窗口的键值存储

> 限流和登录锁定需要"跨请求记数"的地方,需要一个能落地的计数原语。
> kvstore 提供 **Counter**(计数 + TTL)和 **Window**(滑动窗口限流),
> Redis / 内存两种后端实现同一套接口。
>
> ⚠️ **Windows 上没有官方 Redis** —— 用 [Memurai](https://www.memurai.com/get-memurai)
> (Redis 协议兼容,装成 Windows 服务,默认 `127.0.0.1:6379`、无密码)。
> **Developer 版仅供开发/测试,不许上生产。** 详见 [快速开始](../getting-started/README.md)。

## 功能

kvstore 解决两件事:

1. **Counter 计数 + TTL**
   - `Incr` 加一并返回**加完后的值**,key 不存在按 1 起算
   - `Get` 读当前值;key 不存在返回 **0,err 为 nil**
   - `Del` 删 key(登录成功后清零)
2. **Window 滑动窗口限流**
   - `Allow` 记录一次命中,窗口内计数超过 max 则返回 false
   - 语义:**本次请求计入窗口** ⇒ 窗口内第 max 次放行、第 max+1 次拦

两种后端:**Redis**(正路)/ **内存**(降级),业务代码不用改。

### 为什么是两个接口而不是一个

计数(登录失败次数)和滑动窗口(请求频率)的**数据结构与操作都不一样**。
硬合成一个只能取两者公约数,结果是 Redis 侧也退化成 GET/SET 轮询,
把 ZSET 的原子性丢了 —— 那才是真的把限流写坏了。分开保真,代价是多两个小类型。

### 唯一决策点

"有没有 Redis"只在 `Resolve()` 里判断一次,main 启动时调它并注入。
之后 ratelimit / loginlock 只问 `DefaultStore()` 要东西,自己不碰 `repository.RDB`
—— 否则每加一个用存储的地方就多一个"忘了判空"的漏点。

### 它不解决什么

- 不是通用缓存(那是 [cachex](cachex.md):KV 缓存要 TTL / 前缀 / 批量失效)
- 不做分布式锁、不做持久化计数(内存后端重启即清空)
- 不做"滑动窗口之外的算法"(令牌桶 / 漏桶)

## 配置

### yaml 侧(`kv:` 块)

```yaml
kv:
  mode: auto          # auto | redis | memory
```

| 取值 | 行为 |
|---|---|
| `auto`(默认,空值等同) | 有 Redis 用 Redis,没有退内存。保证"限流存在",不保证多副本一致 |
| `redis` | 强制 Redis。**没配 `redis.addr` 直接启动失败**,不静默降级 |
| `memory` | 强制内存,即使配了 Redis 也不连。仅调试用 |

⚠️ **`kv.mode` 与 `auth.revocation_mode` 分开是有意的** —— 后果等级不同:
吊销降级 = 令牌不失效 = 安全事故;限流降级 = 阈值不准 = 可用性问题。
所以本地/预发用内存限流完全合理,生产多副本则**必须** `mode: redis`。

配套的 `redis:` 块(`addr` / `password` / `db` / `pool_size` / 超时)决定
Redis 能不能连上 —— `mode: redis` 而 `redis.addr` 没配,启动就会失败。

### 代码侧(构造选项)

```go
kvstore.Resolve(mode string, rdb *redis.Client) (*Store, error)
kvstore.SetDefault(s *Store)
kvstore.DefaultStore() *Store
```

| 构造函数 | 说明 |
|---|---|
| `kvstore.Resolve(mode, rdb)` | 按模式装配。`rdb` 为 nil 表示 Redis 不可用 |
| `kvstore.NewMemoryCounter()` | 内存计数器 |
| `kvstore.NewMemoryWindow()` | 内存滑动窗口 |
| `kvstore.NewRedisCounter(rdb)` | Redis 计数器(INCR + EXPIRE) |
| `kvstore.NewRedisWindow(rdb)` | Redis 滑动窗口(ZSET) |

`Store` 结构体有三个字段:`Counter`、`Window`、`Backend`(值 `"redis"` / `"memory"`,
只用于日志和 `/healthz`)。

## 使用方法

完整示例:`examples/kvstore/main.go`。

### 装配

```go
counter := kvstore.NewMemoryCounter()
window := kvstore.NewMemoryWindow()
store := &kvstore.Store{Counter: counter, Window: window, Backend: "memory"}
kvstore.SetDefault(store)
```

生产上由 `cmd/server` 调 `kvstore.Resolve(cfg.KV.Mode, repository.GetRedis())`
并 `SetDefault`,业务代码**只问 `kvstore.DefaultStore()`**。

### 计数(登录失败次数)

```go
n, err := counter.Incr(ctx, "login_fail:zhangwei", 10*time.Minute)  // 返回加完后的值
cur, _ := counter.Get(ctx, "login_fail:zhangwei")                   // 不存在返 0,err 为 nil
missing, err := counter.Get(ctx, "不存在的key")                     // 0, nil
counter.Del(ctx, "login_fail:zhangwei")                            // 登录成功 ⇒ 清零
```

### 滑动窗口(1 分钟内最多 3 次)

```go
ok, err := window.Allow(ctx, "sms:13800001234:login", 3, time.Minute)
// 第 3 次 ok=true,第 4 次 ok=false
```

### 后端选择:显式 redis 却没给连接 ⇒ 直接报错

```go
s, err := kvstore.Resolve("memory", nil)   // backend=memory, err=nil
_, err = kvstore.Resolve("redis", nil)     // err≠nil —— 不静默降级成内存
```

## 注意事项

- ⚠️ **`Resolve` 的参数是 `*redis.Client` 具体类型,不是 `redis.UniversalClient`**。
  接口会把 typed nil 变成非 nil:`var iface redis.UniversalClient = concrete`
  在 `concrete == nil` 时 `iface != nil` ⇒ 函数里所有的 `if rdb == nil` 都是死代码,
  显式配 redis 模式 + 没配 addr 会**照常启动**,然后每次调用都往一台死 Redis 上打。
  取具体类型就绕开了这个陷阱。

- ⚠️ **`mode: redis` 而 Redis 不可用 ⇒ 启动失败**,不静默退回内存。
  多副本部署下静默降级意味着限流阈值放大 N 倍,而没有人知道。

- ⚠️ **`SetDefault(nil)` 会 panic** —— 传 nil 是**误用**而不是"恢复默认"。
  忘了注入就让它悄悄退回内存,等于回到本文件开头那个 bug。

- ⚠️ **`DefaultStore()` 未注入时给一份进程级内存实现**(不 panic)。
  这是刻意的:这里的能力**降级了但不会消失**,而吊销那边选择"配了就必须对"
  (`auth.RevocationStore`)。区别在于限流阈值不准 ≠ 令牌不吊销。

- ⚠️ **内存后端只在本实例有效,且重启即清空**。多副本下每个副本各数各的,
  实际阈值 = 配置值 × 副本数;已累计的登录失败次数重启就没了。

- ⚠️ **`Counter.Incr` 的 ttl 只在首次创建时生效**(INCR 不重置 TTL,内存版同样)。
  这是有意的:重复失败能把锁定窗口无限续长的话,那是个登录爆破的免死金牌。

- ⚠️ **`Counter.Get` 在 key 不存在时返回 0 + nil err**,**不要靠 err 区分"不存在"**。
  (Redis 版内部把 `redis.Nil` 吞成 0、nil —— 注释里写着"历史上 loginlock 是
  `err != nil` 就当没失败过",保持这个宽松度。)

- ⚠️ **`Window.Allow` 的语义是"本次计入窗口"** —— 第 max 次放行、第 max+1 次拦。
  写断言时别按"第 max 次就该拦"来数。

- ⚠️ **`max <= 0` 时 `Allow` 返回 false(全拦),不是全放**。两套后端一致:
  限流配置写错时**宁可全拦也不要全放**。

- ⚠️ **内存窗口和 Redis 窗口的边界判定必须一致**:内存版用 `ts > min`
  (Redis 走 `pipe.ZRemRangeByScore(ctx, key, "0", min)`,区间含上界 min)。
  内存版若写成 `>=`,会比 Redis 版多留一条边界记录 ⇒ **同一时刻两套后端给出的
  限流结果不一样**,而且极难在真机上察觉。

- ⚠️ **内存窗口不是定时归零的固定窗口**,是**真滑动窗口**:逐条记录命中时刻,
  判定时先剔掉落在窗口外的。粗心的实现会用固定桶(每分钟一个计数),那在桶边界
  能放过 2×max 次请求 —— 限流一旦能绕过去,不如没有。代价是**内存与 key 数 ×
  窗口内请求数成正比**,高频 key + 长窗口会把内存吃满。

- ⚠️ **Redis 窗口的整个流程在一个 `TxPipeline` 里**(剔旧 → 加入本次 → `ZCard`
  → 设过期),RESP2 下 MULTI/EXEC 原子,不会被并发请求数出"同一时刻两个都过"的中间态。

- ⚠️ **Redis 存储故障时 `Allow` 返回 error,绝不当成放行**。那正是本层存在
  之前的那类 bug。

- ⚠️ **内存计数器每次 `Incr` 都会扫一遍全表清过期条目**(`sweepLocked`),
  key 多的时候是 O(n) —— 内存后端只适合低频/单副本场景。

## 相关

- 源码:`internal/pkg/kvstore/`
- 可运行示例:`examples/kvstore/main.go` —— `go run ./examples/kvstore`
- 限流中间件(滑动窗口的调用方)→ [ratelimitx](../security/ratelimitx.md)
- 登录失败计数与账号锁定(Counter 的调用方)→ [loginlock](../security/loginlock.md)
- 通用 KV 缓存是另一回事 → [cachex](cachex.md)
