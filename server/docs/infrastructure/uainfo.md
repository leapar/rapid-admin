# uainfo —— 解析 User-Agent,取出浏览器与操作系统

> 登录日志要记"什么浏览器、什么系统"。uainfo 零依赖、可测、认不出来就**不猜**。

## 功能

uainfo 从 User-Agent 头里解析出两个字段:

```go
type Info struct {
    Browser string   // "Chrome 120" / "Edge 120" / "微信 8.0.30" / "Postman"
    OS      string   // "Windows 10/11" / "macOS 14" / "iOS 16" / "Android 13"
}
```

三个设计决定:

1. **不引第三方库**。UA 解析本来就没有标准,主流库说到底也是一堆 if/else,
   再加一层依赖不划算 —— 自己写一份可控、可测、零依赖的,想改一条规则就改一条。
2. **输出的是产品名,不是 code**。绝大多数是产品名(Chrome / Safari / Windows /
   macOS),中文界面里也是这么叫的;真需要中文的只有国产 App(微信 / 企业微信 /
   钉钉),它们的产品名本身就是中文 ⇒ 存显示名,前端直接渲染,
   不必再维护一张 `code → 标签` 的表(那种表一旦加上,后端和前端就得同步改两处,
   还得防漏 —— 字典页就是这么长出来的)。
3. **认不出来就是空字符串,不猜**。审计里最怕的是"看起来确定、其实错"的
   信息(比如把一个爬虫认成 Chrome),那比空着更容易误导人。

除了 `Parse`,还提供 `Clip` —— 按**字符**截断 UA,给有列宽上限的表用。

**不解决什么**:不解析设备型号、不做 UA 指纹、不判断 UA 是否可信 ——
UA 是客户端自己填的,只能当**展示用标签**,不能当**判据**。

## 配置

### 代码侧

**该包无构造选项,也没有 yaml 配置** —— 两个包级函数:

```go
uainfo.Parse(ua string) Info
uainfo.Clip(ua string, max int) string
```

规则表是包内私有的(`browserRules` / `parseOS` 的 switch),要加规则得改源码。

### yaml 侧

**该包无 yaml 配置。**

## 使用方法

```go
info := uainfo.Parse(req.UserAgent)
// info.Browser → "Chrome 120";info.OS → "Windows 10/11"
// 认不出来 → 两个字段都是空串(前端渲染成 "-")

// 落库前必须按列宽截断(sys_login_log 三列:device=128 / browser=64 / os=64)
rec := model.SysLoginLog{
    Device:  uainfo.Clip(req.UserAgent, 128),
    Browser: uainfo.Clip(info.Browser, 64),
    OS:      uainfo.Clip(info.OS, 64),
}
```

框架里的实际用法:`internal/service/auth_impl.go` 的登录日志登记,
以及 `internal/service/auth_anomaly.go` 的异常登录检测。

## 注意事项

- ⚠️ **UA 是不可信输入,解析结果只能当标签**。客户端可以随便填
  `User-Agent: Chrome 120`,也可以填 `Postman`。它**不能**用来做授权判断、
  不能用来识别"是不是我们的 App",更不能作为安全证据。
  它能回答的只有"这个客户端**自称**是什么"。
- ⚠️ **规则表是顺序敏感的,命中即停**。每一对相邻规则都有真实的坑:
  - Edge / Opera / 企业微信 / UC / 夸克 / QQ 的 UA 里**含 "Chrome"**
  - 微信(iOS)的 UA 里**含 "Safari"**
  - Chrome 的 UA 里**含 "Safari"**
  - iPad 的 UA 里**含 "Mac OS X"**
  - HarmonyOS 的 UA 里**含 "Android"**

  所以 Edge / 微信 / 企业微信这些规则必须排在 Chrome 之前,iOS/iPad 必须排在
  macOS 之前。**新增规则时插错位置会静默认错浏览器**,而不是报错 ——
  想加规则先看 `browserRules` 的注释。
- ⚠️ **空串有两个完全不同的含义**:"压根没有 UA"和"这个 UA 没认出来"。
  包本身不区分,需要区分得自己看原始 header。
- ⚠️ **Safari 的版本号在 `Version/` 里,不在 `Safari/` 里**。
  `Safari/605.1.15` 是 WebKit 的构建号,拿它当浏览器版本就是报错信息 ——
  所以 Safari 规则的版本标记**只**写 `Version/`,取不到就只输出 "Safari"。
- ⚠️ **Windows 11 的 UA 也是 `Windows NT 10.0`**,跟 10 无法区分。
  所以输出的是 `"Windows 10/11"` —— 写死 "Windows 10" 是谎报。
- ⚠️ **`Clip` 按字符( rune)截,不是按字节**。中文是 3 字节 UTF-8,
  按字节切会把汉字劈成两半,落库就是乱码。
- ⚠️ **`Clip` 不是万能药:超长会让整条记录写不进去,不只是被截断**。
  `sys_login_log.device` 是 `varchar(128)`,而 UA 原文经常超 128 字符
  (微信 / 企业微信那种带一长串 token 的能到 200+)。MySQL 严格模式下超长
  直接报 `Data too long for column` ⇒ **整条登录日志写不进去**
  (只在日志里留一条 warn)⇒ 审计链断一条。SQLite 不校验长度,所以
  **本地开发期看不出这个问题**,上生产才炸,那时候历史登录记录已经查不到了。
- ⚠️ **`Clip(ua, 0)` 返回空串** —— 不是"不截断",是"全清空"。
  `max <= 0` 一律返 `""`。
- ⚠️ **`Clip` 不做字符消毒**。它只按长度切,不剔除控制字符。
  UA 原文直接进 `device` 列,拼进日志时要注意换行。

## 相关

- 源码:`internal/pkg/uainfo/`
- 可运行示例:`examples/uainfo/main.go` —— `go run ./examples/uainfo`
- UA 参与异常登录检测 → [anomalylogin](../security/anomalylogin.md)
- 登录失败计数与账号锁定 → [loginlock](../security/loginlock.md)
