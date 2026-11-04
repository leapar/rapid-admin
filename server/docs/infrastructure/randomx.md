# randomx —— 安全随机 token 生成

> 验证码 / API key / one-time token / 会话 id 这些"猜不到"的东西,
> 统一走 crypto/rand,不要用 `math/rand`。

## 功能

randomx 提供一组**加密学安全 + URL-safe** 的随机串生成器,目标是替掉散落在
业务代码里的 `fmt.Sprintf("%d", rand.Int())` 和 `uuid.New().String()`:

| 函数 | 产出 | 典型场景 |
|---|---|---|
| `Token(n)` | n **字符** URL-safe | 通用 token / 会话 id / JTI |
| `String()` | 32 字符 URL-safe | 替代 `fmt.Sprintf("%d", rand.Int())` |
| `MustToken(n)` | 同 `Token`,失败 panic | 需要"绝不能返回空"的场合 |
| `NumericCode(n)` | n 位纯数字(允许前导 0) | 短信验证码 |
| `PINCode(n)` | 同上 | 4~6 位密码 |
| `AlnumCode(n)` | n 位大小写字母+数字 | 通用 |
| `InviteCode(n)` | n 位**无易混字符** | 人眼要读的邀请码 / 兑换码 |
| `Hex(n)` | n **字节** 的 hex(2n 字符) | secret key / hash salt |
| `Base64(n)` | n **字节** 的 base64(无 padding) | header / cookie |
| `FromCharset(n, charset)` | 自定义字符集 | 特殊需求 |
| `Entropy(n, charsetLen)` | 熵估算(bit) | 决定"该发多长的验证码" |

### 为什么不用 `math/rand`

`math/rand` 是**伪随机**的:seed 可见、状态可推。Go 1.20 起全局 `rand` 会被
随机播种,但只要攻击者拿到一次输出(或 seed),后续序列就完全可预测 ——
用它生成 token 等于把 token 送出去。

`math/rand` 的正确用途是**抖动 / 采样 / 打散**(比如 retryx 的退避抖动),
那种场景不需要不可预测性,追求的反而是快。

### 为什么不是 uuid

uuid 固定 36 字符且含 4 个连字符,做文件名 / URL 参数不好看;
randomx 支持任意长度,字符集全部 URL-safe(无 `+` `/` `=`,不用转义)。

## 配置

### 代码侧

**该包无 yaml 配置,也没有构造选项** —— 全是包级函数,直接调。

字符集常量:

| 常量 | 内容 | 说明 |
|---|---|---|
| `CharsetURLSafe` | `A-Za-z0-9-_` | 默认(`Token` 走这个),64 字符 = 6 bit/字符 |
| `CharsetAlphaNum` | `A-Za-z0-9` | 62 字符 |
| `CharsetNoConfusing` | 大写字母(去掉 `I`/`L`/`O`/`Q`)+ `2`-`9` | 32 字符 = 5 bit/字符,排除 `0/O/Q`、`1/I/L`,给人眼读 |
| `CharsetNumeric` | `0-9` | 10 字符 = 3.32 bit/字符 |

### yaml 侧

**该包无 yaml 配置。**

## 使用方法

```go
// 通用 token:URL-safe,适合 header / cookie / 上传 ID
id := randomx.Token(32)

// 短信验证码:纯数字,允许前导 0
code := randomx.NumericCode(6)

// 邀请码:排除易混字符(0/O/Q、1/I/L),人眼能读
invite := randomx.InviteCode(8)

// 密钥 / 盐:hex 编码,长度按**字节**
salt := randomx.Hex(16)   // 16 字节 = 32 字符 = 128 bit

// 熵估算 —— 决定"该发多长的验证码"时用这个
e := randomx.Entropy(6, len(randomx.CharsetNumeric))
```

框架里的实际用法:JWT 的 JTI 和会话 id 都是 `randomx.Token(16)`
(`internal/pkg/auth/jwt.go` / `session.go`)。

## 注意事项

- ⚠️ **`Token(n)` 的 n 是**字符数**,不是字节数** —— 熵 = n × log2(字符集长度)。
  URL-safe 字符集是 64 个字符 ⇒ 每字符 6 bit。`Token(32)` = **192 bit**,
  不是 256 bit(包头注释里"43 字节原始熵 → 256 位"那句与实现对不上)。
  `Hex(n)` / `Base64(n)` 的 n 才是字节数。
  ⚠️ 框架里就有这个口径错位:`internal/pkg/auth/jwt.go` 的常量叫
  `jtiRandomBytes = 16`,注释写"16 字节 = 128 bit",但传给 `randomx.Token`
  的是**字符数** ⇒ 实际熵是 16 × 6 = **96 bit**。`session.go` 的
  `sessionIDBytes` 同理。够用,但别按注释里的 128 bit 去算碰撞概率。
- ⚠️ **`MustToken(0)` 会 panic**,而且 panic 信息极具误导性
  (`crypto/rand failed (impossible)`)。原因是 `Token(0)` 合法地返回空串,
  `MustToken` 把"空串"一律当成"crypto/rand 失败"。`n <= 0` 的其它函数
  (`Token` / `Hex` / `Base64` / `FromCharset`)都只是返回空串,不 panic。
- ⚠️ **`FromCharset` 的字符集长度会被截成 `byte`**:
  `csLen := byte(len(charset))`。字符集长度**正好是 256 的倍数**时
  `csLen` 变成 0,`buf[i] % csLen` 直接 **divide by zero panic**。
  自定义字符集时长度控制在 255 以内。
- ⚠️ **`FromCharset` 有模偏置**:`buf[i] % csLen`,而 `buf[i]` 只有 256 种取值。
  字符集长度不是 256 的因数时,前 `256 % csLen` 个字符会比其余的多约
  `1/26` 的概率。`CharsetNumeric`(10 个)上,数字 `0`~`5` 比 `6`~`9`
  略常见一点 —— 做**短信验证码**时无所谓,做**长期有效的凭据**时
  优先用 64 字符的 URL-safe 字符集。
- ⚠️ **`rand.Read` 失败时返回空串,没有 error 返回值**。
  `Token` / `Hex` / `Base64` / `FromCharset` 在 `crypto/rand` 失败时一律返回 `""`,
  调用方分不清"熵源坏了"和"参数不合法"。需要区分就用 `MustToken`(它会 panic),
  但要先自己挡掉 `n <= 0`。
- ⚠️ **短信验证码用 `NumericCode` 而不是 `Token`** —— 但要注意
  `NumericCode(6)` 只有约 19.9 bit 熵(6 × 3.32),**必须配合频次限制**用
  (见 `smslimit`),单靠长度不够。
- ⚠️ **别把 randomx 用在"需要可复现"的场景**。它是不可预测的;
  要可复现就明确用 `math/rand` 并自己管 seed。

## 相关

- 源码:`internal/pkg/randomx/`
- 可运行示例:`examples/randomx/main.go` —— `go run ./examples/randomx`
- JTI / 会话 id 的生成点 → [auth](../security/auth.md)
- 短信验证码的频次闸门 → [smslimit](../messaging/smslimit.md)
