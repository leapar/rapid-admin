# signx —— HMAC 接口签名(防篡改 + 限窗防重放)

> Bearer token 只能证明"你是谁",证明不了"这次请求没被改"。
> signx 把 method / path / 时间戳 / 请求体绑进一个 HMAC,写操作上校验,
> 篡改任何一项都失配。

## 功能

signx 提供四个纯函数,无状态、无配置、无依赖:

| 函数 | 作用 |
|---|---|
| `Sign` | 算签名(客户端侧),返回小写 hex |
| `Verify` | 验签(服务端侧),常量时间比较 |
| `ParseHeader` | 拆 `X-Signature` 头 |
| `ValidTS` | 校验时间戳是否落在时钟漂移窗口内 |

### 签名构造(逐字确定,两端必须一致)

```
拼接串 = METHOD + "\n" + PATH + "\n" + TS + "\n" + sha256(BODY)
sig    = hex(HMAC-SHA256(secret, 拼接串))
```

- `METHOD` 大写,如 `POST`
- `PATH` 含前导 `/`,**不含 query string**
- `TS` 是 unix 秒的十进制字符串
- `BODY` 取**原始字节**的 sha256

客户端 header:

```
X-Signature: t=<unix_seconds>,sig=<hex>
```

`ParseHeader` 认 `t` / `ts` / `timestamp` 和 `sig` / `signature`,逗号分隔,
两边空格会 trim;`ts` 或 `sig` 缺一个就返 `false`。

### 它不解决什么

- **不做 nonce、不记录已用过的签名** ⇒ 只挡"跨窗口重放",**不挡窗口内重放**
- 不管 secret 怎么存、怎么轮换(框架层**没有** secret 的配置项)
- 不做鉴权(那是 [auth](auth.md)):签名证明的是"请求没被改",不是"你是谁"

## 配置

### yaml 侧

**该包无 yaml 配置。**

`config.Server` 结构体里**没有** `Sign` 字段 —— secret 只能由调用方以 `[]byte`
传进 `middleware.Sign(secret)`。框架层不提供 secret 的存放位置。

### 代码侧

| 入口 | 说明 |
|---|---|
| `middleware.Sign(secret []byte, opts ...func(*signxCfg))` | HMAC 共享密钥;GET / HEAD / OPTIONS 直接透传,其余方法都要验签 |
| `middleware.WithSkew(n int64)` | 允许的时钟漂移(秒),默认 **300**(5 分钟) |
| `middleware.WithClock(now func() int64)` | 注入时钟(测试用) |

⚠️ `middleware.Sign` 的文档注释里写的 `cfg.Sign.Secret` **是个不存在的字段** ——
`config.Server` 里没有 `Sign` 块,照抄那行编译不过。

## 使用方法

### 客户端:算签名

```go
secret := []byte("s3cr3t-shared-key")
ts := strconv.FormatInt(time.Now().Unix(), 10)
body := []byte(`{"phone":"13800001234","scene":"login"}`)

sig := signx.Sign(secret, "POST", "/rapid-admin/v1/auth/sms-code", ts, body)
header := "t=" + ts + ",sig=" + sig   // → X-Signature
```

### 服务端:拆头 → 校验时间窗 → 验签

```go
ts, sig, ok := signx.ParseHeader(raw)
if !ok {
    // errcode.SignatureMissing (50600)
}
if !signx.ValidTS(ts, time.Now().Unix(), 300) {
    // errcode.SignatureExpired (50601)
}
if !signx.Verify(secret, method, path, ts, body, sig) {
    // errcode.SignatureInvalid (50602)
}
```

错误码落在 `5xxxx` 段(框架 / 运行时):

| 业务码 | 值 | HTTP |
|---|---|---|
| `errcode.SignatureMissing` | 50600 | **500** |
| `errcode.SignatureExpired` | 50601 | **500** |
| `errcode.SignatureInvalid` | 50602 | **500** |

### 中间件(现成的,但还没接进链路)

```go
h.Use(middleware.Sign(secret, middleware.WithSkew(300)))
```

中间件自己拆头、校验 ts、读 body、验签,失败就 `response.Fail` + `c.Abort()`。
读 body 用 `c.Request.Body()`(已缓冲的字节),读不到才 fallback 到 `BodyStream`
并一次性读掉,验签成功后用 `c.Request.SetBody` 写回,下游 handler 仍能读到。

完整可运行示例见 `examples/signx/main.go`。

## 注意事项

- ⚠️ **签名失败返的是 HTTP 500,不是 401/403**。三个码(50600 / 50601 / 50602)都在
  `5xxxx` 段,`errcode.HTTPStatus` 把 `50000-59999` 兜底成 **500**。
  ⚠️ `internal/api/middleware/sign.go` 的注释写着"401/403",**与实现不符** ——
  前端按 HTTP 状态码分支的话,签名失败会被当成"服务器内部错误"。
- ⚠️ **签名不覆盖 query string**。中间件取的是 `path := string(c.Path())`,不含
  `?a=1`。⇒ 如果业务把 query 参数当输入(页码、筛选条件),**改 query 签名照样通过**。
- ⚠️ **"防重放"只有时间窗,没有 nonce**。包名写着防重放,机制却只是"ts 距现在
  不超过 5 分钟"。同一个签名在窗口内可以重放任意次,`signx` 不记录已用过的 ts。
  ⇒ 真正的重复提交防护靠 [idempotentx](idempotentx.md) 或服务端 nonce 表。
- ⚠️ **`middleware.Sign` 目前零调用方**。全仓 `grep "middleware.Sign("` 只命中它
  自己文件里的注释;`internal/api/router/router.go` 只挂了 `middleware.Idempotency()`。
  中间件是现成的,但**没接进链路**,而且没有 secret 的配置项 —— 接的时候得自己
  解决 secret 从哪来、怎么轮换。
- ⚠️ **hex 必须小写**。`Sign` 返回小写 hex,而 `Verify` 是**字符串全等比较**
  (不是解码后比字节)。客户端做大写转换 ⇒ 恒定返 `SignatureInvalid`。
- ⚠️ **body 必须是原始字节,只序列化一次**。签名覆盖的是 `sha256(body)`;JSON 的
  key 顺序、空格、数字格式全在字节里。序列化两次、或"签名的字节"和"发出的字节"
  不是同一份 ⇒ 必然失配。
- ⚠️ **`ValidTS` 接受未来时间戳**。它取绝对值差,所以客户端时钟快 5 分钟以内
  **会通过**而不是被拒。
- ⚠️ **中间件的挂载位置**要在任何会消费 body 的中间件之后 —— 它读不到已缓冲的
  body 时会 fallback 去读 `BodyStream`,那时 body 可能已经被上游读空了。
- ⚠️ **`ParseHeader` 对未知键静默跳过,对重复键后者覆盖前者**。`t=1,t=2` 取 2。
- ⚠️ **`Verify` 先比长度再做常量时间比较**(`len(want) != len(got)` 在
  `subtle.ConstantTimeCompare` 之前)。sig 长度固定 64(hex sha256),实际不构成
  泄露,但别照抄这个写法去比变长数据。
- ⚠️ **中间件只挡读方法** —— GET / HEAD / OPTIONS 直接透传不验签,其余方法
  (POST / PUT / PATCH / DELETE 等)都要验签。

## 相关

- 源码:`internal/pkg/signx/`
- 中间件:`internal/api/middleware/sign.go`
- 可运行示例:`examples/signx/main.go` —— `go run ./examples/signx`
- 错误码分段与 HTTP 映射 → [errcode](../infrastructure/errcode.md)
- 重复提交防护(签名管不了的那一半) → [idempotentx](idempotentx.md)
- JWT 签发 / 解析 / 吊销 → [auth](auth.md)
