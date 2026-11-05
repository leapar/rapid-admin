# validatorx —— 统一参数校验:自定义规则 + 中文错误

> DTO 上的规则用 `validate:"..."` 标签声明,校验逻辑集中在一处,
> 错误信息统一是中文(跟业务码的默认文案风格一致)。

## 功能

validatorx 解决三件事:

1. **单例校验器** —— `Get()` 返回全局唯一的 `*Validator`(基于
   go-playground/validator/v10),线程安全,业务全局复用。
2. **字段名取 json tag** —— 报错的字段名是 `phone` 而不是 Go 的 `Phone`,
   前端对得上。
3. **中文错误文案** —— `Explain(err)` 返回 `(field, message)`,
   规则名翻译成"不能为空" / "手机号格式不正确(11 位,1 开头)"这种短句。

另外提供框架级自定义规则 `cn_phone`(11 位中国手机号)与 `idcard`(18 位身份证)。

**不解决**:业务级规则(业务自己在 `init()` 里注册)、错误码
(用 `errcode.InvalidParam`)。

## 配置

**本包无 yaml 配置** —— 规则写在 DTO 的 `validate` 标签上,不是运行期配置。

## 使用方法

### 装配:注册内置规则

```go
if err := validatorx.RegisterBuiltins(); err != nil {
    log.Error("validator 内置规则注册失败", zap.Error(err))
}
```

`cmd/server/main.go` 在启动装配阶段调一次。**它不会自动执行** —— 不调,
用到 `cn_phone` / `idcard` 的 DTO 会校验失败。

### 声明规则

```go
type loginDTO struct {
    Phone    string `json:"phone" validate:"required,cn_phone"`
    Password string `json:"password" validate:"required,min=10,max=32"`
    Code     string `json:"code" validate:"omitempty,len=6"`
    Email    string `json:"email" validate:"omitempty,email"`
}
```

### 校验 + 解释错误

```go
v := validatorx.Get()

if err := v.Struct(dto); err != nil {
    field, msg := v.Explain(err)
    // field = "phone", msg = "手机号格式不正确(11 位,1 开头)"
}
```

推荐直接用 `handler.BaseHandler.BindAndValidate(c, dto)`:它把解析失败归到
`errcode.BadRequest`、校验失败归到 `errcode.InvalidParam`,并把
`{field, message}` 塞进 `Detail`。

### 注册业务自己的规则

```go
err := v.RegisterCustom("starts_with_hz", func(fl validator.FieldLevel) bool {
    return len(fl.Field().String()) >= 2 && fl.Field().String()[:2] == "hz"
})
```

业务侧建议加业务前缀(如 `wx_appid`),避免与内置保留字撞名。

完整示例见 `examples/validatorx/main.go`。

## 注意事项

- ⚠️ **注册"受限 tag"会直接 panic,不是返 error**。
  受限 tag = validator 的内置保留字(`required` / `dive` / `omitempty` / `keys` 等),
  或名字里含 `.` `[` `]` `,` `|` `=` `+` `(` `)` `` ` `` `~` `!` `@` `#` `$` `%` `^`
  `&` `*` `\` `"` `/` `?` `<` `>` `{` `}`。panic 信息形如
  `Tag 'required' either contains restricted characters or is the same as a
  restricted tag needed for normal operation`。
  ⇒ **注册失败要用 `recover` 兜住 / 选好名字**,别指望 `err != nil`。
- ⚠️ **重复注册同名 tag:既不 panic 也不报错 —— 直接覆盖注册表**。
  老注释写的"重复注册会 panic(tag already exists)"是**错的**
  (按 validator v10.30.5 实测)。⇒ "靠 panic 兜底"这条防护从来不存在;
  业务模块在 `init()` 里注册同名规则会**悄悄顶掉别人的**,谁最后注册谁生效。
- ⚠️ **真正返 error 的只有两种情况:空 tag 名 / 空函数**。
  这两种要处理,否则规则没注册 ⇒ DTO 上的 `validate:"xxx"` 变成"未知规则" ⇒
  校验时报 `Undefined validation function`,而且没人知道为什么。
  `RegisterBuiltins()` 因此:返回 error + **立刻打一条 Error 日志**(必须让人看见)。
- ⚠️ **报错只报第一个失败字段**。这是有意的(避免一次弹一堆吓用户),
  后续错误在 log 里落 detail。
- ⚠️ **`Explain` 走 `errors.As`,能穿透任意层 `%w` 包装**。
  裸类型断言 `err.(validator.ValidationErrors)` 只在 err **正好**是那个类型时成立;
  任何一层 `fmt.Errorf("...: %w", err)` 都会让它失配 ⇒ **字段名整个丢掉**,
  用户只看到"参数校验失败",不知道是哪个字段错了。
- ⚠️ **字段名只认 `json` tag**。`RegisterTagNameFunc` 读的是 `json` 标签;
  用了 `form` / `query` tag 或干脆没 tag 的字段,报出来的字段名会回落到 Go 字段名
  (如 `UserName`),前端对不上。
  `json:"-"` 的字段也回落成 Go 字段名。
- ⚠️ **`ruleLabels` 表里有一个永远命不中的条目**:`phone`。
  `RegisterBuiltins()` 只注册 `cn_phone` 与 `idcard`,`phone` 规则**没有注册** ——
  所以 `ruleLabels["phone"]` 这条文案除非业务自己注册了 `phone` 规则,否则永远用不上。
  DTO 上写 `validate:"phone"` 会得到 `Undefined validation function`。
- ⚠️ **源码包注释里的错误码写错了**:注释写"错误码统一 `errcode.InvalidParam`(40100)",
  但 `InvalidParam` 实际是 **40003**,`40100` 是 `Unauthorized`。代码用常量,是对的。
- ⚠️ **规则名会原样出现在未知规则的文案里**(`"不符合 xxx 规则"`)。
  自定义规则若没进 `ruleLabels`,用户看到的就是英文 tag 名。

## 相关

- 源码:`internal/pkg/validatorx/`(`validatorx.go` 单例 + 中文翻译 / `register.go` 内置规则)
- 可运行示例:`examples/validatorx/main.go` —— `go run ./examples/validatorx`
- 错误码 → [errcode](errcode.md)
- 统一响应体 → [response](response.md)
