# searchidx —— 加密字段的全文搜索(n-gram 盲索引)

> 手机号 / 姓名 / 身份证这类字段加密后就没法 `LIKE` 查询了,
> 而普通盲索引只能做"完全相等"。
> searchidx 用 **n-gram 盲索引**把"前缀 / 包含"变成可查的。

## 功能

### 问题

- 加密字段无法走 DB 的 `LIKE` 查询
- 盲索引(blind index,整串 HMAC)只支持**完全相等**
- 需要"模糊 / 前缀 / 包含"查询时无路可走

### 方案:n-gram 盲索引

把文本切成重叠的 n-gram 片段(默认 4 字符),**每个 n-gram 单独用
HMAC-SHA256(secret) 算一个 token**,存到 side table:

```
主表:  sys_user(id, phone_cipher)          -- 密文
索引表:sys_user_phone_idx(user_id, gram)   -- n-gram 盲索引
  PRIMARY KEY (user_id, gram)
  INDEX (gram)
```

写入:`gram = searchidx.BlindNgrams(secret, plain, 4)` → 主表写密文,索引表批量写 gram。

查询:`token = searchidx.SearchTokens(secret, query, 4)` →

```sql
SELECT user_id FROM sys_user_phone_idx
 WHERE gram IN (...) GROUP BY user_id
HAVING COUNT(DISTINCT gram) >= k
```

再回主表拿密文解密。

### 提供的能力

| 符号 | 作用 |
|---|---|
| `Ngrams(s, n)` | 切 n-gram(不加密),调试 / 看切分结果用 |
| `BlindNgrams(secret, s, n)` | n-gram → HMAC-SHA256 token(建索引用) |
| `SearchTokens(secret, query, n)` | query → token(查询用;实现上就是 `BlindNgrams`) |
| `MatchAtLeast(actualHits, queryTokenCount, k)` | 命中数是否达标 |
| `MinMatchTokens(queryTokenCount, ratio)` | 按比例算出 k |
| `DefaultN` | 默认 n = 4 |

### 它不解决什么

- **不提供 DB 访问** —— 索引表要自己建、自己写、自己 join,包只给 token
- **不做模糊匹配(编辑距离 / 拼写纠错)** —— 哈希不可逆,只能做"token 集合求交"
- **不做分词 / 中文词典** —— n-gram 是纯字符窗口,中文按 rune 切
- **不读配置、不连数据库、不建表**

## 配置

**该包无 yaml 配置。**

`configs/config.example.yaml` 里**没有** `searchidx:` 块,`internal/config/` 下也没有
对应的配置结构体 —— secret 是函数参数(`secret []byte`),由调用方决定。

密钥来源有两个,语义不同,**别混用**:

| 来源 | 用途 | 规则 |
|---|---|---|
| `crypto.blind_key`(yaml `crypto.blind_key`) | `crypto.BlindIndex.Compute` —— 整串等值盲索引 | ≥ 16 字节,**原样使用不做 base64 解码**;`crypto.NewBlindIndex` 会真的校验长度 |
| 调用方自己传的 `secret []byte` | `searchidx.BlindNgrams` 的 n-gram token | **本包不校验长度** |

同属 HMAC-SHA256,所以拿 `crypto.blind_key` 当 `searchidx` 的 secret 在算法层是通的
—— 但两者算出来的 token **不是同一张索引**:crypto 是"整串一个 token",
searchidx 是"每 n 个字符一个 token"。**建索引和查询必须用同一个 secret + 同一个 n**。

## 使用方法

完整示例:`examples/searchidx/main.go`。

### 一、切 n-gram

```go
const n = 4
for _, s := range []string{"13800001234", "张伟", "RapidAdmin"} {
    fmt.Printf("%q → %v\n", s, searchidx.Ngrams(s, n))
}
```

### 二、建索引 token

```go
secret := []byte("blind-index-secret-key-32-bytes!!")
grams := searchidx.BlindNgrams(secret, "13800001234", n)
// 同 secret + 同 gram → 同 token(索引能匹配上)
// 不同 secret → 不同 token(防跨库攻击)
```

### 三、查询 token

```go
tokens := searchidx.SearchTokens(secret, "13800001234", n)
```

### 四、命中判定

```go
k := searchidx.MinMatchTokens(len(tokens), ratio)   // ratio<=0 → 1;ratio>=1 → 全部
for uid, h := range hits {                          // h = 该候选命中的 token 数
    if searchidx.MatchAtLeast(h, len(tokens), k) {
        // 命中
    }
}
```

`MatchAtLeast` 的 k 语义:`k == 0` ⇒ 至少 1 个命中;`k < 0` ⇒ 全部命中(= `queryTokenCount`)。

## 注意事项

- ⚠️ **query 长度必须 ≥ n**。比 n 短时 `SearchTokens` 把整串当成**1 个 token**,
  而索引里存的是 n-gram —— 两边对不上,**查不到**。想支持更短的 query,
  建索引时就要用更小的 n(代价是索引变大)。

- ⚠️ **哈希不可逆 ⇒ 只能"前缀 / 后缀 / 包含",不能模糊匹配**。
  原理是 token 集合求交:编辑距离、拼写纠错、相关性排序都不在这个包的能力范围内。

- ⚠️ **本包不校验 secret 长度**。`crypto.NewBlindIndex` 会拒绝 < 16 字节的 key
  (空 key 的 HMAC 是任何人都能算出来的公开函数,盲索引"不落明文"的意义当场归零),
  但 `searchidx.BlindNgrams` **没有这道闸** —— 传个短 secret 或空 secret 就是
  同样的破口,而且没有任何报错。secret 的长度校验得由调用方负责。

- ⚠️ **换 secret ⇒ token 全变 ⇒ 索引表整个作废**。轮转 secret 必须**重建索引**,
  不是"改个配置就生效"。

- ⚠️ **归一化口径与 `crypto.BlindIndex` 不同,两边不能混**:
  `searchidx` 只把**连续空白压成一个空格**并 trim,**不做 NFC 归一化**;
  `crypto.BlindIndex.ComputeNorm` 则是**删掉所有 ASCII 空格/制表符 + ASCII 转小写**。
  ⇒ `"138 0000 1234"` 在 crypto 那边算出 `"13800001234"` 的索引,在 searchidx 这边
  算出的是含空格的 token。**同一个号码两种写法,索引和查询对不上 ⇒ 查不到。**

- ⚠️ **searchidx 不做 NFC 归一化**。macOS 上来的文件名(APFS/HFS+ 用 NFD)、
  用户从各种输入法粘来的文本,同一个字可能有两种编码 ⇒ **切出的 gram 不同 ⇒
  索引和查询对不上**。要归一化就在调 `BlindNgrams` / `SearchTokens` **之前**
  自己做(两边做一样的)。

- ⚠️ **n 越小索引越大、召回越高、误报变多**;n 越大反之。手机号 11 位、n=4
  ⇒ 8 个 gram ⇒ 索引行数 ≈ 记录数 × 8。推荐 n=4(中文姓名/手机号),
  n=3 对长查询更友好。

- ⚠️ **误报是 n-gram 索引的固有属性**,靠 k(命中 token 数)过滤。
  `ratio` 越低召回越高、误报越多;`ratio = 1` 要求全部 token 命中。

- ⚠️ **索引表是 side table,写路径必须与查询路径一致**:
  写入用 `BlindNgrams`,查询用 `SearchTokens`(实现上就是 `BlindNgrams` 的别名)
  ⇒ **同一个 n、同一个 secret、同一套归一化**,三样少一样就查不到。

- ⚠️ **短串整体当一个 token**。`Ngrams` 对长度 ≤ n 的输入返回**整串一个 token**,
  不切。中文姓名"张伟"(2 字)只产出一个 token —— 索引表和查询两边都这样,
  所以"张伟"能精确匹配,但"张"这种单字查询查不到。

## 相关

- 源码:`internal/pkg/searchidx/`
- 可运行示例:`examples/searchidx/main.go` —— `go run ./examples/searchidx`
- 字段级加密(AES-256-GCM)与整串盲索引 → [crypto](../security/crypto.md)
- 密钥来源与轮转 → [kms](../security/kms.md)
