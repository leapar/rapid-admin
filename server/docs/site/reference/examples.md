# 示例目录

> `server/examples/` 下 **53 个最小可运行示例**,一个框架模块一个。
> 别人拿到 `pkg/cachex` 要自己读 200 行源码、拼出正确的初始化顺序、猜哪些参数
> 不配会静默降级 —— 示例是开源框架降低上手成本最有效的一件工具。

## 怎么跑

```bash
cd server
go run ./examples/cachex
```

示例之间**互不依赖**,可以单独跑。需要外部基础设施的(Redis / DB / OSS)在示例里
用内存实现或 `miniredis` 顶替,**不真连** —— 没配也能跑通,不会 panic。

一次跑全部:

```bash
cd server
for d in examples/*/; do n=$(basename "$d"); go run "./examples/$n" >/dev/null || echo "FAIL: $n"; done
```

## 目录约定

```
examples/
├── README.md              # 索引表(53 个包 → 这个示例演示什么)
├── doc.go                 # 目录级说明
├── examples_test.go       # 漂移网(4 条测试)
└── <包名>/main.go         # 一包一个目录,package main + func main()
```

**每个示例的文件头注释就是它的说明书** —— 写清了这个包解决什么问题、关键点是什么、
跑法是什么。本手册各章的「功能」小节大多直接取材于它。

## 漂移网:有测试守着

`server/examples/examples_test.go` 里的 4 条测试:

| # | 约束 | 违反的后果 |
|---|---|---|
| 1 | `internal/pkg/` 下每个一级包目录都要有同名示例目录 | 新增框架模块忘写示例 ⇒ **测试红** |
| 2 | 反向:不许有多余的示例目录 | 删了一个包却忘了删示例 ⇒ **测试红** |
| 3 | 每个示例必须是 `package main` + `func main()`(`go/parser` 校验) | 写成了伪代码 ⇒ **测试红** |
| 4 | 示例**不许 import 业务包**(`internal/service` / `internal/model` / `internal/api` / `cmd`) | 别人 clone 下来编不过 ⇒ **测试红** |

⚠️ `internal/config` **不在禁用名单里** —— 它是框架的配置层,框架自己的公开 API
就会用到它。

第 4 条不是洁癖:框架示例如果 import 了业务模型,别人拿这个框架做自己的项目时,
第一个示例就编译不过 —— 而**第一个示例编译不过,整套文档就废了**。

## 两条容易被违反的纪律

### ① 示例必须是**能跑的代码**,不是伪代码

```go
// ❌ 伪代码:能编过,但跑起来啥也不干
func purgeOldRecords(ctx context.Context) error {
    return nil // TODO: 真的去删
}

// ✅ 真的跑一遍,输出看得见
```

本仓的判定标准很简单:**逐个 `go run` 冒烟全过**。写示例时挖出来的真 bug
(`smsaudit` 的 `Shutdown()` 不幂等、ctx key 自成一套)就是这么发现的。

### ② 审计三兄弟手写 DDL

`piiaudit` / `cryptaudit` / `smsaudit` 的表在生产上由 AutoMigrate 建,但示例
不许 import `internal/model`(那是业务模型包)。所以示例里**手写 `CREATE TABLE`
+ 裸 SQL 回查**:

```go
const piiauditDDL = `
CREATE TABLE IF NOT EXISTS sys_pii_access_log (
    id            INTEGER PRIMARY KEY,
    created_at    DATETIME,
    ...
)`
```

## 索引

| 分组 | 包 |
|---|---|
| 基础设施 | logger / errcode / response / validatorx / pagination / jsonid / sonyflake / timeutil / buildinfo / syncx / randomx / retryx / uainfo / metricsx / otelx / apiverx / grayx |
| 存储与缓存 | cachex / kvstore / searchidx / media / oss |
| 安全与合规 | auth / authz / casbinx / crypto / kms / desensitize / signx / idempotentx / ratelimitx / loginlock / anomalylogin / scope / fieldpolicy |
| 审计 | piiaudit / cryptaudit / smsaudit |
| 消息与通知 | inbox / notifyx / push / pushx / permnotify / msn / smslimit / smstpl / emailx |
| 调度与可观测 | cronx / jobs / slowquery |
| 语音与媒体 | voicex |
| 业务扩展 | dynx |
| 接口文档 | swaggerx |

每个示例演示什么,见 [`examples/README.md`](../../examples/README.md)。
