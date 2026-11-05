# desensitize —— 请求体 / 查询串脱敏

> 审计中间件要把"这次请求改了什么"记进 `sys_operation_log`,那两张列是
> `request_body TEXT -- 脱敏后` 与 `response_summary TEXT`。
> desensitize 负责把明文变成能入审计表的文本。

## 功能

desensitize 提供两组能力:

1. **按类型脱敏**(调用方知道这个字段是什么类型)
   - `Phone` / `IDCard` / `Name` / `Email` / `BankCard` / `ContractNo`
2. **按字段名脱敏**(调用方不知道结构 —— 请求体是**结构未知的 JSON**)
   - `IsSensitiveKey` —— 判断字段名要不要遮
   - `RedactJSON` —— 递归遍历 JSON,命中的字段值替换成 `***`
   - `RedactQuery` —— query string 用**同一份词表**脱敏

外加**盲索引**:`PhoneHash` / `IDCardHash` —— 不可逆但可做"完全相等"查询。

**不解决什么**:不判断"这次请求该不该带这个字段"(那是业务的事);不写审计表
(那是 `middleware.Audit()` 与 [piiaudit](../audit/piiaudit.md));不加密 ——
加密是 [crypto](crypto.md) 的事。

## 配置

**该包无 yaml 配置块** —— 它是纯函数包,没有构造器、没有配置结构体。

唯一与配置相关的是**盲索引 key 的来源**,这是一条代码侧的优先级链
(`desensitize.BlindKeySource()` 可查当前用的是哪个):

| 优先级 | 来源 | 说明 |
|---|---|---|
| 1 | `crypto.blind_key`(`crypto.Blind()`) | 唯一正解,见 [crypto](crypto.md) |
| 2 | 环境变量 `PHONE_HASH_KEY`(≥16 字节) | 兜底 |
| 3 | dev 占位常量 | ⚠️ **写在仓库里 = 公开的**,只限本地开发 |

## 使用方法

### 按类型脱敏

```go
desensitize.Phone("13800001234")        // 138****1234
desensitize.IDCard("110101199001011234")// 1101**********1234
desensitize.Name("张伟丰")               // 张*丰
desensitize.Email("zhangwei@example.com")// z*******@example.com
desensitize.BankCard("6222021234567890123")
desensitize.ContractNo("HT20261101000123")
```

### 盲索引

```go
desensitize.PhoneHash("13800001234")   // HMAC-SHA256 hex
desensitize.IDCardHash("110101199001011234")
```

### 审计采集端(主要用途)

```go
// 请求体
body, ok := desensitize.RedactJSON(raw)
// ok=false ⇒ 不是合法 JSON,调用方不要把原文写进审计表

// query string
qs := desensitize.RedactQuery(c.Request.URI().QueryString())
```

### 判断单个字段

```go
desensitize.IsSensitiveKey("user_phone") // true
desensitize.IsSensitiveKey("order_id")     // false
```

## 注意事项

- ⚠️ **脱敏词表是"宁可多遮"的子串匹配**。`token_count`、`email_flag` 这类无害字段
  会被误伤(只是审计记录少一个数字);漏遮一个 `password` 就是明文落库。
- ⚠️ **`code` / `key` / `name` / `address` / `sign` 刻意不在词表里**。
  遮蔽 `code` 等于把审计记录废掉(业务错误码 / 字典码);`key` 太泛
  (`idempotency_key` / `cache_key` 都会被误伤);`name` / `address` 是业务内容 ——
  审计日志要回答的是"谁、什么时候、把哪个用户的什么字段改成了什么",全遮成
  `***` 谁也查不出问题。验证码另用 `sms_code` / `captcha_code` / `verify_code`
  精确匹配。
- ⚠️ **`RedactJSON` 的第二个返回值是"是否成功解析",不是"有没有改动"**。解析失败
  返回 `("", false)` —— 调用方**绝不能把原文写进审计表**(本仓约定写
  `"<非 JSON, N 字节>"`)。
- ⚠️ **非 JSON 一律 `ok=false`**。尾部还有内容(`{}{}`)也判无效。
- ⚠️ **数字用 `json.Number` 保留原文**。sonyflake 是 19 位整数,float64 只能保
  15~16 位有效数字 —— 不开 `UseNumber` 的话审计记录里的 ID 会变成
  `1.2345678901234568e+18`,和库里对不上。
- ⚠️ **有上限**:深度 8 层 / 数组 50 项(超出丢并标注 `<超出条数上限>`)/ 单字符串
  200 字符 / 总体积 4096 字节。截断时按 rune 切,不劈开 UTF-8(直接 `s[:n]` 会切出
  半个字符 ⇒ 非法 UTF-8 ⇒ JSON 编码成 `U+FFFD` 乱码)。
- ⚠️ **太短的串原样返回**。`Phone` < 7 位、`IDCard` < 8 位、`BankCard` < 8 位、
  `ContractNo` < 6 位 ⇒ 直接返回原文(盖了等于全没了)。别把"原样返回"当漏洞 ——
  那些长度的值本来就不是合法的对应类型。
- ⚠️ **`maskMiddle` 在"藏不住 1/3"时改用首尾各 1/4**。标准切法(手机 3+4 /
  身份证 4+4 / 银行卡只留尾 4)只在藏得住 ≥ 1/3 时用,否则
  `Phone("1234567")` 会变成 `"123****4567"` —— **看着打了码,一个字没藏**。
- ⚠️ **`Name` 两字时输出 `张*`**(以前 `Repeat("*", n-2)` = 0 个星号 ⇒ 原样返回,
  等于完全没脱敏);≥ 3 字时是"首字 + n-2 个星号 + 尾字"。
- ⚠️ **`Email` 按 rune 切**。以前按字节切,local-part 首字符是多字节(中文 / 带音标
  字母)时切出半个字符 ⇒ 输出非法 UTF-8,`json.Marshal` 编码成 `U+FFFD` 乱码。
- ⚠️ **`desensitize.PhoneHash` 不做归一化**。它直接调 `crypto.Blind().Compute()` ——
  而归一化在另一个方法 `crypto.BlindIndex.ComputeNorm` 里。所以 `"138 0000 1234"` 与
  `"13800001234"` 算出**不同的索引** ⇒ 按 `phone_hash` 查用户时手机号带空格就查不到。
  建索引与查询两侧必须用同一个方法。
- ⚠️ **dev 占位 key 是公开的**。`BlindKeySource()` 返回 `"dev-placeholder"` ⇒
  **生产环境这是事故**:盲索引等于明文哈希,拿到库的人算一遍就能反查手机号。
  启动日志要对这个值对账。
- ⚠️ **换 key ⇒ 已有的 `phone_hash` / `real_name_hash` / `email_hash` 全部失效**
  (索引是对明文的派生值,key 一变就对不上)。dev 库里运行时创建的数据需要重灌种子。
- ⚠️ **本包自带的 `README.md` 与源码不一致**:README 函数清单里写的 `NameHash` /
  `MaskByRule` / `ToOwnerViews` 在源码里**不存在**。以源码为准。

## 相关

- 源码:`internal/pkg/desensitize/`
- 可运行示例:`examples/desensitize/main.go` —— `go run ./examples/desensitize`
- 盲索引的密钥从 crypto 借 → [crypto](crypto.md)
- PII 访问审计(谁看了完整手机号)→ [../audit/piiaudit.md](../audit/piiaudit.md)
