# kms —— 密钥管理服务抽象层 + 轮转

> 业务模块只拿 tier(pii / secret / normal),不直接读 env。
> 切换 KMS 后端(aliyun / tencent / vault / local)只改启动配置,业务代码零改动。

## 功能

kms 只做**启动期**的三件事:

1. **选 provider** —— `Config.Provider` 决定用哪个 `Provider` 实现
2. **取 key** —— `Provider.ListKeys(ctx)` 列出所有 tier 的所有 key version
3. **灌进 crypto** —— 转成 `crypto.KeysConfig` 调 `crypto.InitKeys`

之后业务侧始终走 `crypto.EncryptFor(ctx, crypto.TierPII, ...)`,不碰本包。

外加:**轮转就绪检查**(`jobs` 包的 `kmsRotateCheck` 定时任务)—— 回答两个问题:
"现在加密/解密这条路走得通吗"、"轮转流程有没有停在半路上"。

**不解决什么**:不重写业务表里的历史密文(每张表的密文列在哪只有业务知道);
不生成密钥;不替 KMS 后端做灰度发布。

## 配置

### yaml 侧(`kms:` 块)

```yaml
kms:
  provider: local     # local(默认)/ aliyun / tencent / vault
  endpoint: ""        # aliyun/tencent 专用
  access_key: ""      # 凭证
  secret_key: ""      # 凭证
  region: ""          # 区域(aliyun:cn-hangzhou 等)
  key_prefix: ""      # 业务前缀(隔离多业务)
```

⚠️ **`key_prefix` 目前没有实际作用** —— `Provider` 接口只有一个 `ListKeys`,
local provider 直接扫 env,不吃前缀。切到真正的 KMS 后端时它才会被用上。

⚠️ 代码侧的 `kms.Config` 还有 `CacheTTL int`(启动期 ListKeys 缓存秒数)字段,
但 **yaml 里没有 `cache_ttl`,全仓也没人读它** —— 同样是死配置。

### 环境变量(local provider)

```
KEY_PII_V1=<64 位 hex>        # 32 字节 AES-256 key
KEY_PII_ACTIVE=V1             # 当前 active 版本
KEY_SECRET_V1=...
KEY_SECRET_ACTIVE=V1
KEY_NORMAL_V1=...
KEY_NORMAL_ACTIVE=V1
KEY_LEGACY=<64 位 hex>        # v1 格式密文兜底(可选)
```

`ACTIVE` 的值接受 `V2` / `v2` / `2` 三种写法(老文档只写了 `V<N>`,运维漏个 `V`
就踩坑)。

### 代码侧

```go
kms.NewManager(ctx context.Context, cfg kms.Config) (*kms.Manager, error)
mgr.ProviderName() string
mgr.Close() error
```

## 使用方法

### 装配

```go
mgr, err := kms.NewManager(ctx, kms.Config{Provider: "local"})
if err != nil {
    log.Warn("kms init failed; tiered encrypt/decrypt disabled", zap.Error(err))
    // 不 panic —— 服务照常起,只是分级加密不可用
} else {
    defer mgr.Close()
}
```

### 业务侧(始终走 crypto)

```go
ct, err := crypto.EncryptStringFor(ctx, crypto.TierPII, phone)
pt, err := crypto.DecryptStringFor(ctx, crypto.TierPII, ct)
```

### 检查装配结果

```go
for _, tier := range []crypto.Tier{crypto.TierPII, crypto.TierSecret, crypto.TierNormal} {
    meta, err := crypto.ActiveKeyMeta(tier)
    if err != nil { continue } // 该 tier 没配
    log.Info("kms tier ready", zap.String("tier", string(tier)), zap.String("key_id", meta.KeyID))
}
```

## 注意事项

- ⚠️ **没配 KMS 时相关任务会跳过而不是失败**。`jobs.kmsRotateCheck` 遍历三个 tier,
  **没配的 tier 记一条 Debug 就跳过**(只用 PII 不加密 Secret 的部署完全合法);
  三个都没配就记一条 Info("未启用 KMS 的部署")后返回 nil。**不算失败**。
- ⚠️ **密钥配错 → 降级继续,但降级的是整个分级加密**。`crypto.InitKeys` 失败 ⇒
  三个 tier 一个都没换 ⇒ `crypto.EncryptFor` 一律返 `tier not initialized`。
  main 打的那条 Warn("kms init failed; tiered encrypt/decrypt disabled")**不能忽略** ——
  它意味着 PII 字段将以明文存取。
- ⚠️ **local provider:任何一个 tier 配错就整体返 error**。理由是"配错意味着以为有
  key、其实没有" —— 半装(一个 tier 好了、另一个悄悄禁用)比整体不装更危险:业务会
  以为 PII 已经加密了。
- ⚠️ **`KEY_<TIER>_ACTIVE` 指向不存在的版本 ⇒ 启动就报清楚**。以前这两种情况结果
  完全相反:`ACTIVE=V2` 而 `V2` 没配 → crypto 侧 panic 崩进程;`ACTIVE=V2` 而
  `V1/V2` 都没配 → 该 tier 被**静默禁用**。现在统一成"启动时报清楚"。
- ⚠️ **`KEY_<TIER>_V<N>` 必须是 32 字节 hex(64 个字符)**。不是就 skip(只 Warn),
  于是 `ACTIVE` 找不到对应 key ⇒ 报错。多一个少一个字符都会在这里被抓住。
- ⚠️ **aliyun / tencent / vault 三个 provider 都还没实现**。配了直接返 error
  ("本期请用 local")。注意 `Config.Validate` 对这三个只要求
  `AccessKey + SecretKey` 非空 —— **校验能过,但仍然不可用**。
- ⚠️ **切 provider 时旧 env 残留会被忽略**(只取 provider 返回的 key)。从 local 切到
  aliyun 之后,机器上残留的 `KEY_PII_V1` 不会影响新 provider。
- ⚠️ **legacy key 只有 local 支持**。`local.LoadLegacyKey()` 读 `KEY_LEGACY`;
  没注册 legacy key 时,**任何 v1 格式密文都解不开**(报
  `no legacy key registered for v1 ciphertext`)。新部署从没写过 v1 密文,这是正常的。
- ⚠️ **KMS 灌的是 tiered 路径,不是 `crypto.Field()` 那条**。`crypto.field_key` /
  `crypto.blind_key` 是**另一条线**(main 单独装载 `InitFieldCipherWithMeta` /
  `InitBlindIndexWithMeta`)。两套都得配 —— 只配一套的话,一半的敏感字段仍是明文。
- ⚠️ **`Provider.Close()` 要调**(`defer mgr.Close()`),否则 SDK 资源不释放。
  `Manager.Close()` 对 `m == nil` / `provider == nil` 都是安全的 no-op。
- ⚠️ **`crypto_keys:` 那个 yaml 块是死的** —— 分级密钥只从 env 走,配在 yaml 里
  不生效(详见 [crypto](crypto.md))。

## 相关

- 源码:`internal/pkg/kms/`(local provider 在 `internal/pkg/kms/local/`)
- 可运行示例:`examples/kms/main.go` —— `go run ./examples/kms`
- 密钥本体在另一个包 → [crypto](crypto.md)
- 轮转就绪检查跑在定时任务里 → [../scheduling/jobs.md](../scheduling/jobs.md)
