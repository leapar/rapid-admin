# cryptaudit —— 密钥使用审计:哪个 key 被用过

> "这个字段是用哪把密钥加密的""上周那次解密失败到底是哪把 key"—— 合规审计要能回答
> 这类问题。业务代码解密 / 加密后调一行 `cryptaudit.Record(ctx, ...)`,攒批、落库、
> 重试由框架负责。

## 功能

cryptaudit 解决一件事:**把"哪把密钥被谁用过、成功还是失败"变成可查的表**。

能力:

1. **全量密钥使用记录** —— 不只 PII 字段,所有加密 / 解密 / 盲索引操作都能记。
2. **失败也落库** —— `Success=false` + `ErrorMsg` 是排查"哪个 key 解不开"的直接线索
   (直接 grep `error_msg`)。
3. **异步攒批** —— 100 条或 5s 落一次库,单次插入按 50 条分片。
4. **ctx 上下文自动带** —— principal / request_id / trace_id / ip / user_agent。
5. **error_msg 按 rune 截断**(200 字符),不劈开 UTF-8。

不解决:

- 不存密钥材料本身 —— 只记 `KeyID` / `KeyVersion` 这些**标识**。
- 不判断密钥是否该轮转、是否过期 —— 那是 [kms](../security/kms.md) 的事。
- 不做加解密 —— 那是 [crypto](../security/crypto.md) 的事。

## 配置

### 代码侧(没有构造选项)

cryptaudit 是包级单例,只有两个生命周期函数:

| 符号 | 说明 |
|---|---|
| `cryptaudit.Init(d *gorm.DB)` | 启动期调一次(`sync.Once` 包着)。`d == nil` ⇒ 打一条 Warn(`cryptaudit: db is nil, recorder disabled`),但**后台 loop 照样起** |
| `cryptaudit.Shutdown()` | 退出前调一次,等最后一批 flush 完。**幂等** |

攒批参数是**包内常量 / 包级变量**,不可配:`batch` 阈值 100、ticker `tick = 5s`、
重试 `maxTry = 3`、重试间隔固定 200ms、单次插入分片 `CreateInBatches(cur, 50)`。

⚠️ 与兄弟包的结构差异:piiaudit / smsaudit 的状态装在一个独立的 recorder 结构体里,
**cryptaudit 用的是包级变量**(`db` / `batch` / `mu` / `stopCh` / `doneCh`)。
所以本包里**没有**那种独立的 recorder 类型,测试里也拿不到未导出的 flush 入口。

### yaml 侧(`audit:` 块)

`configs/config.example.yaml` 里确实有一个 `audit:` 块:

```yaml
audit:
  batch_size: 100
  flush_interval: 5000   # ms(整数不带单位 —— 写成 "5s" 会让整份配置反序列化失败)
  queue_size: 10000
```

⚠️ **这三个键本包不读**。`config.AuditConfig`(`internal/config/crypto.go`)定义了它们,
但全仓**零消费方** —— 三个审计包的攒批参数都写死在包内常量里。**改 yaml 不生效**。

## 使用方法

### 装配

```go
cryptaudit.Init(repository.GetDB())
defer cryptaudit.Shutdown()
```

### 记一次成功的解密

```go
cryptaudit.Record(ctx, cryptaudit.Event{
    KeyID:        "KEY_PII_V1",
    KeyVersion:   1,
    Algorithm:    "aes-256-gcm",
    Operation:    "decrypt",
    ResourceType: "user",
    ResourceID:   user.ID,
    ByteCount:    11,
    Success:      true,
})
```

### 失败的也要记(这是本包最有用的一处)

```go
cryptaudit.Record(ctx, cryptaudit.Event{
    KeyID:      "KEY_PII_V1",
    KeyVersion: 1,
    Algorithm:  "aes-256-gcm",
    Operation:  "decrypt",
    Success:    false,
    ErrorMsg:   err.Error(),
})
```

### `Event` 字段

| 字段 | 类型 | 说明 |
|---|---|---|
| `KeyID` | `string` | 密钥标识(env var 名 / KMS key id) |
| `KeyVersion` | `int` | 密钥版本(轮转用) |
| `Algorithm` | `string` | `"aes-256-gcm"` / `"hmac-sha256"` / ... |
| `Operation` | `string` | `"encrypt"` / `"decrypt"` / `"blind-index"` |
| `ResourceType` | `string` | `"user"` / `"order"` / ... |
| `ResourceID` | `uint64` | 资源 ID |
| `ByteCount` | `int` | 处理字节数 |
| `Success` | `bool` | 成没成 |
| `ErrorMsg` | `string` | 失败原因(按 rune 截到 200) |

### 完整可运行示例

`examples/cryptaudit/main.go` —— `go run ./examples/cryptaudit`

示例里手写 DDL 建表(框架示例**不许 import 业务模型**),成功 / 失败两条路径各记一条。

## 注意事项

- ⚠️ **攒够 100 行是 `go flush()` —— 异步的**。这跟 piiaudit / smsaudit 不同(那两个是
  同步 `r.flush()`)。后果:**"显式 flush 返回"不等于"数据已经落库"** —— 谁先拿到锁
  谁插入,另一个看到空批次直接返回。测试因此**只能记 80 行而不是 100 行**:正好 100 行
  会触发那次异步 flush,和显式 flush 抢同一批数据,查询可能读到 0 行 ⇒ **偶发红**。
  这条推理写在源码注释里(`cryptaudit_test.go` 的 `TestRecord_FlushesRow`)。
- ⚠️ **主键必须自己填(雪花)**。`model.BaseModel.ID` 的 tag 是
  `primaryKey;autoIncrement:false` —— GORM 不会让库自增,零值 0 原样入库 ⇒ id 撞主键 ⇒
  第二批往后的行全部插不进去(整批重试 3 次后丢弃)⇒ **`sys_crypto_audit_log` 永远只有
  1 行**。本包用 `model.BaseModel{ID: sonyflake.Next()}` 补上了(M100 修的)。
- ⚠️ **`Record` 开头是 `if db == nil { return }`** —— 没 `Init` 时它直接返回,连 ctx
  那一段都走不到。单元测试里如果忘了先 `Init`,"ctx 为 nil 不 panic" 这类用例会
  **假绿**。
- ⚠️ **`Init(nil)` 也会起后台 goroutine**(`run()`),而 piiaudit / smsaudit 的
  `Init(nil)` 直接 return、**不起 loop**。三兄弟行为不一致,写文档或排查时别互相套用。
- ⚠️ **丢批次的日志级别比兄弟包低一级**:cryptaudit 重试 3 次全失败后只打一条
  **Warn**(`cryptaudit flush failed`),piiaudit / smsaudit 打的是 **Error**
  (`...after retries, dropping`)。同一个故障在两个包里看起来严重程度不一样,排障时
  容易漏掉 cryptaudit 那条。
- ⚠️ **`flush()` 在 `db == nil` 时是"先取走批次再返回"**。代码顺序是先 `batch = nil`
  再判 `if db == nil { return }` ⇒ 批次数据被静默丢弃。正常路径碰不到(`Record` 在
  db 为 nil 时就不写),但如果你绕过 `Record` 往包里塞数据,会丢得无声无息。
- ⚠️ **error_msg 按 rune 截断到 200,不是按字节**。原来 `errMsg[:200]` 是字节切片 ——
  一个中文错误信息(3 字节/字)正好在第 200 字节被劈开,落库就是一个**半个汉字** +
  乱码尾巴。排障时看到这种 error_msg,第一反应会变成"程序写错了",而不是"被截断了"。
- ⚠️ **重试间隔固定 200ms,不指数退避**。3 次尝试各失败一次就是 3 × 200ms。sleep 发生
  在**锁外**,所以不阻塞 `Record` —— 但 flush 协程自己会挂在那儿。
- ⚠️ **`Operation` / `Algorithm` 是自由字符串**,没有常量约束(不像 piiaudit 的
  `action` / `field` 在 `internal/model` 里有常量)。写错不会编译报错,只会让聚合查询
  对不上。
- ⚠️ **落库是异步的,`Shutdown()` 幂等**。进程退出前不调 `Shutdown()`,最后一批(最多
  99 条)会丢;重复调 `Shutdown()` 不会再 panic(M100 改的,原来第二次会 close 已关闭的
  channel)。
- ⚠️ **`Shutdown()` 之后还能 `Record`,但大多数数据不会落库**。后台 loop 已退出,不再有
  5s 定时 flush;只有攒到 100 条触发 `go flush()` 分支才会写下去。

## 相关

- 源码:`internal/pkg/cryptaudit/`
- 可运行示例:`examples/cryptaudit/main.go` —— `go run ./examples/cryptaudit`
- 兄弟包:[piiaudit](piiaudit.md) / [smsaudit](smsaudit.md)
- 加解密与密钥轮转:[crypto](../security/crypto.md) / [kms](../security/kms.md)
