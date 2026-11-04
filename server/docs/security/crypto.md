# crypto —— 字段级加密(AES-256-GCM)+ 盲索引

> 敏感字段(手机号、身份证、银行卡)不能明文落库,但又不能丢:
> 需要能查、能比对、能改。
> crypto 给两套原语 + 一套密钥分级机制。

## 功能

crypto 提供三组能力:

1. **`Cipher` —— AES-256-GCM 字段级加密**
   - `Encrypt` / `Decrypt` / `EncryptString` / `DecryptString`
   - `DecryptStringAudited` / `EncryptStringAudited`:解密自动落 **PII 访问审计**
     + **密钥使用审计**
2. **`BlindIndex` —— HMAC-SHA256 盲索引**
   - `Compute` / `ComputeNorm`:不可逆派生值,可做"完全相等"查询
3. **密钥分级 + 轮转 registry**
   - 三个 tier:`pii`(手机/身份证/银行卡)/ `secret`(密码哈希、API 密钥)/
     `normal`(一般业务字段)。**不同 tier 用不同 key**,一把泄漏不波及全部
   - 每个 tier 可挂多把 key(`KeyVersion` 递增),只有 `Active=true` 的那把用于新加密,
     老的保留用于解旧密文
   - 密文是**信封格式**:`Base64(0x02 || key_version || nonce || ciphertext)` ——
     密文自己带着"哪把 key 加密的",不需要额外 DB 列

**不解决什么**:不接 KMS(那是 [kms](kms.md));不做"谁看了 PII"的审计落库
(那是 [piiaudit](../audit/piiaudit.md));**不重写业务表里的历史密文** ——
每张表的密文列在哪、怎么批量 re-encrypt 只有业务自己知道。

## 配置

### yaml 侧(`crypto:` 块)

```yaml
crypto:
  field_key: ${FIELD_AES_KEY}     # AES-256-GCM 密钥,必须 32 字节
  blind_key: ${FIELD_BLIND_KEY}   # HMAC-SHA256 盲索引密钥,>=16 字节
```

⚠️ **两条 key 的解析规则不一样**:

- `field_key`:base64(标准 / 无 padding)**或** 32 字节原文**都认**;
- `blind_key`:**原样使用,不做 base64 解码**。

两种都不满足 → 启动时打 Error,字段加密**不启用**(PII 字段明文存取)——
"配了"不等于"加密了"。

### yaml 侧(`crypto_keys:` 块)

```yaml
crypto_keys:
  enabled: true
  warn_no_active: true
```

⚠️ **这一整块是死配置**:`crypto_keys` / `enabled` / `warn_no_active` 在全仓
**没有任何代码读取**(没有对应的 `mapstructure` 字段,也没有读取点)。
分级密钥实际来自**环境变量**,由 KMS 的 local provider 装载(见 [kms](kms.md))。
写在 yaml 里不会生效。

### 代码侧(构造选项)

```go
crypto.InitKeys(cfg crypto.KeysConfig) error   // 分级密钥(KMS 路径)
crypto.InitFieldCipher(key []byte) error        // 单把字段密钥
crypto.InitFieldCipherWithMeta(key []byte, meta crypto.KeyMeta) error
crypto.InitBlindIndex(key []byte) error
crypto.InitBlindIndexWithMeta(key []byte, meta crypto.KeyMeta) error
```

## 使用方法

### 分级密钥 + 加解密

```go
err := crypto.InitKeys(crypto.KeysConfig{
    PII: []crypto.KeyEntry{
        {KeyID: "env:PII_KEY", KeyVersion: 1, Key: piiKey, Active: true},
    },
    Secret: []crypto.KeyEntry{
        {KeyID: "env:SECRET_KEY", KeyVersion: 1, Key: secretKey, Active: true},
    },
    Normal: []crypto.KeyEntry{
        {KeyID: "env:NORMAL_KEY", KeyVersion: 1, Key: normalKey, Active: true},
    },
})
if err != nil { return err } // 配错 → 返 error,不 panic

ct, err := crypto.EncryptStringFor(ctx, crypto.TierPII, "110101199001011234")
pt, err := crypto.DecryptStringFor(ctx, crypto.TierPII, ct)
```

### 盲索引

```go
bi, err := crypto.NewBlindIndex([]byte("blind-index-key-32-bytes-long!!"))
if err != nil { return err } // key < 16 字节直接拒绝

h1 := bi.Compute("13800001234")
h2 := bi.ComputeNorm("138 0000 1234") // 先归一化(去空格 / 转小写)再算
```

### 字段级 Cipher(带审计)

```go
// main 启动期:InitFieldCipherWithMeta(key, crypto.KeyMeta{KeyID: "crypto.field", ...})
phone, err := crypto.Field().DecryptStringAudited(ctx, user.PhoneCipher, "user", user.ID, "phone")
```

### 轮转就绪状态

```go
state, err := crypto.RotationState(crypto.TierPII)
// state.Configured / ActiveKeyID / ActiveVersion / RetainedVersions
// state.ActiveIsHighest / state.RoundTripOK / state.Problem
```

## 注意事项

- ⚠️ **`InitKeys` 是"要么全装上,要么一点不改"**。三个 tier + legacy 全部先在局部
  建好,成功才一次性换上去 —— 轮转中途失败不会留下"一半新一半旧"的 registry
  (那正是最需要原子性的时刻)。
- ⚠️ **每个 tier 必须恰好一把 `Active` 的 key**。0 把或 2 把都返 error
  (`must have exactly 1 active key, got N`)。轮转期的正确形态是
  **1 active + N-1 老 key 仅用于解密**。key 版本重复也返 error。
- ⚠️ **`InitBlindIndex` 现在返 error**(`ErrBlindIndexKeyTooShort`)。以前"key 至少
  16 字节"只写在注释里,`InitBlindIndex(nil)` 会造出一个**空 key 的 HMAC** ——
  空 key 的 HMAC-SHA256 等价于"任何人都能算出来的公开哈希",拿到库的人直接对
  候选明文算一遍就能把手机号反查出来,盲索引的意义当场归零。
- ⚠️ **`NewCipher` 要求 key 恰好 32 字节**(不是"至少")。
- ⚠️ **v1 / v2 的首字节歧义**:v1 密文的首字节是**随机 nonce 的第一个字节**,而 v2
  信封的首字节必须恰好是 `0x02` ⇒ **每 256 份 v1 密文里就有 1 份**被当成 v2 解析,
  后面跟着的 nonce 字节被当成 key version → 查不到 key → 那份数据**永久解不开**
  (不是报错,是丢数据)。批量加密的 PII 表里 1 万条大约中招 39 条。
  解法是"不靠猜,靠试":v2 解不开就按 v1 再解一次(v2 带 GCM 认证,误判概率 ~2^-128)。
- ⚠️ **`crypto.Field()` / `crypto.Blind()` 未初始化时 panic**。配错 key 时 main 打
  Error 且**不初始化** —— 真用到就 panic,而不是拿着错误的 key 把 PII 加密成解不开的
  东西。要安全探测用 `TryField()` / `TryBlind()`(返回 nil 而不 panic)。
- ⚠️ **两条加密路径的审计覆盖不一样**。`crypto.Field()` 那条(`DecryptStringAudited`)
  落 cryptaudit + **piiaudit**;tiered 那条(`EncryptFor` / `DecryptFor`)只落
  cryptaudit。业务要"谁看了手机号"的审计,必须走 `Field()` 那条。
- ⚠️ **`RotationState` 查不出"历史数据还在用退役 key"**。它只看 registry,不扫任何
  业务表。一个天天报"一切正常"却看不见真问题的检查比没有检查更坏 —— 它会让人以为
  轮转没问题。返回的 `error` 只表示"这个 tier 压根没初始化"(没配 ≠ 配坏了)。
- ⚠️ **轮转探针不写密钥使用审计**。它会**真的加解密一次**(固定探针明文),但那是
  体检不是业务读数据 —— 混进 `sys_crypto_audit_log` 会让人误以为有人在读数据。
- ⚠️ **信封里的 key version 是 1..255 的单字节**。`EnvelopeFormatVersion` 恒为
  `0x02`;老版本 v1 格式(裸 `nonce || ciphertext`)本包**可读不可写**,解密时靠
  `Legacy` key 兜底。没注册 legacy key 时,任何 v1 格式密文都解不开
  (`HasLegacyKey()` 用来区分"没配"与"老部署丢了")。

## 相关

- 源码:`internal/pkg/crypto/`
- 可运行示例:`examples/crypto/main.go` —— `go run ./examples/crypto`
- KMS 装配(启动期把 keys 灌进本包)→ [kms](kms.md)
- PII 访问审计 → [../audit/piiaudit.md](../audit/piiaudit.md)
- 密钥使用审计 → [../audit/cryptaudit.md](../audit/cryptaudit.md)
