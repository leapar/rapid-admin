# jsonid —— 以字符串形式上 wire 的主键(雪花 ID 不能用数字)

> 本仓主键由 sonyflake 生成,量级约 `1e17`,而 JavaScript 的 `Number` 只能精确表示到
> `2^53-1`(约 `9.0e15`)。ID 比上限大一个数量级,超出部分在 `JSON.parse` 时被**静默截断**。

```text
wire 上真实的 id:  94883951113533126
JS 解析后变成:     94883951113533120   ← 末 6 位被吃掉
```

信息在解析那一刻就没了,**前端侧任何补救都拿不回真值**。把截断后的值回传后端就会
404 —— 身份切换就死在这里。这不是前端能修的问题:JS 语言层面就没有 64 位整数,
大整数只能靠字符串传。

## 功能

jsonid 提供三件东西:

1. **`ID`** —— `uint64` 的新类型,在 JSON 上**序列化成字符串、反序列化成字符串**。
   只影响 JSON,**不碰 GORM 列类型**(库里仍是 `uint64`)。
2. **`IDList`** —— `[]ID` 的批量形式。`json:",string"` 内置选项**管不了数组**
   (标准库只支持 string / float / int / bool 标量),批量 ID 字段必须换真类型。
3. **全仓扫描器**(`ScanDir` / `Violation` / `RenderAllowlist`)——
   找出"该打 `,string` 却是 integer"的 json tag,作为一条**自动化的棘轮守卫**。

**不解决**:前端拿 ID 之后的比较逻辑(JS 里必须用字符串比较)。

## 配置

**本包无 yaml 配置** —— 这是 wire 格式契约,不是运行期配置。

## 使用方法

### 类型声明

```go
type IdentityView struct {
    ID   jsonid.ID    `json:"id"`
    Role jsonid.ID    `json:"role_id"`
    Tags jsonid.IDList `json:"tag_ids"`
}
```

### 转换

```go
view.ID = jsonid.FromUint(identity.ID)          // uint64 → ID
id := view.ID.Uint64()                           // ID → uint64(给 service / GORM 用)

list := jsonid.FromUints([]uint64{1, 2, 3})      // []uint64 → IDList
us := list.Uints()                               // IDList → []uint64
```

### 序列化

```go
b, _ := json.Marshal(jsonid.FromUint(97013624807423348))
// "97013624807423348"
```

### 反序列化

```go
var got jsonid.ID
err := json.Unmarshal([]byte(`"97013624807423348"`), &got)
```

### 扫描器(接新模块的人必读)

扫描器是**棘轮**:它扫全仓 `.go` 文件,找出"整数字段 + id 类 json 名 + 没按规矩序列化"的
违规,产出一张 `Violation` 列表。

```go
violations, err := jsonid.ScanDir(root)   // root = internal/ 之类
for _, v := range violations {
    fmt.Println(v.File, v.Line, v.Struct+"."+v.Field, v.JSONName, v.GoType)
    fmt.Println(v.Fix())                   // 这一条该怎么改
}
```

`Violation` 的字段:`File`(相对仓库根的路径)、`Line`、`Struct`、`Field`、
`JSONName`、`GoType`、`Tagged`(是否已打 `,string`)。
`v.Key()` 给出稳定标识(用于清单对账),`v.Fix()` 直接给出改法。

**怎么跑**:

- **日常**:`go test ./internal/pkg/jsonid/ -run TestNoBareIntegerIDTags`
  —— 新增违规让测试红;已改完却还挂在清单上,同样红(清单不许只增不减)。
- **确需临时豁免时**,重建清单(会**覆盖整个文件**,只在建清单时用):

  ```bash
  UPDATE_ID_ALLOWLIST=1 go test ./internal/pkg/jsonid/ -run TestNoBareIntegerIDTags
  ```

  之后改造就**手工删对应行**,不要重新生成 —— 重新生成会让清单永远只反映当下,
  失去"不许新增"的约束力。
- 清单文件路径:`internal/pkg/jsonid/id_tag_allowlist.txt`
  (常量 `jsonid.AllowlistFile`)。**当前这个文件不存在** —— 2026-10-21 259 个字段
  已全部改完,现在是**零容忍**模式,allowlist 机制只在"确实需要临时豁免"时才重建。
- 清单每行的格式由 `LoadAllowlist` 强制:
  `<位置>  # <怎么改>` —— 缺 `" # "` 分隔符会返错
  (`allowlist 行缺少 ' # ' 说明(每行都要写为什么还没改)`)。
  渲染用 `jsonid.RenderAllowlist(violations)`,解析单行用 `jsonid.ParseAllowlistLine(line)`。

完整示例见 `examples/jsonid/main.go`。

## 注意事项

- ⚠️ **`json:",string"` 对数组/切片/结构体无效**。
  标准库文档原话:"It applies only to fields of string, floating point, integer, or
  boolean types"。本仓有 `identity_ids` / `department_ids` / `order_ids` 这类批量 ID
  字段,它们是 `[]uint64` ⇒ **必须换成 `jsonid.IDList`**,加 `,string` 一点用没有。
- ⚠️ **标量有两种合规写法,别混**:
  - `jsonid.ID` 类型(推荐,类型层面就对了)
  - `uint64` + `json:"id,string"`(也可以,GORM / service 层一行都不用改)
  两种方向必须**一致** —— 一边发字符串一边收数字,就是"登录能过、提交报 400"的经典成因。
- ⚠️ **`UnmarshalJSON` 也接受裸数字**(如 `123`),那是"改造尚未完成的兄弟接口"的
  兼容通道 —— 拒绝会让整个前端在改造完成前全挂,而改造是分模块推进的。
  **新代码别依赖它**;规范格式是字符串。改完全部走字符串之后这条路径自然死掉。
- ⚠️ **`IDList` 的 `null` 与 `[]` 是两回事**。反序列化时 `null` → `nil`,
  保留"没传"与"传了空数组"的区别 —— 前端据此区分"不过滤"和"过滤到空"。
  `IDList` 为 nil 时序列化结果是 `null`(不是 `[]`)。
- ⚠️ **`ID` 反序列化时 `""`(空串)→ 0**,不报错。空串与 0 在下游是同一个值,
  排查"查不到数据"时别忘了这一条。
- ⚠️ **扫描器按**类型名**猜整数类型**,不解析类型别名链。
  用 `type UserID uint64` 这种别名的字段**可能漏扫**(别名在 AST 里就是 `UserID`,
  不在 `integerish` 表里)。误报方向是"多查",不会漏 —— 但别名那类**是真的会漏**。
- ⚠️ **扫描器排除了一类"看起来像 ID"的字段**:
  `captcha_id` / `trace_id` / `request_id` / `open_id` / `key_id` / `handler_id`
  —— 验证码令牌、链路追踪 id、微信 unionid 这类本来就不是数字 ID。
  后缀 `_by`(`created_by` / `review_by` / `last_changed_by`)**算 ID**
  (都是操作人 user_id,同样会溢出)—— 第一版规则只认 `_id`,结果全仓 22 个"谁干的"
  字段一个没扫到。
- ⚠️ **`Violation.Key()` 不含行号** —— 行号会在任何一次编辑后漂移,进了清单就永远对不上,
  棘轮会误报成"新增违规"。代价是两个同名匿名结构体只记一条,改的时候按文件定位即可。
- ⚠️ **`examples/jsonid/main.go` 的注释里写的 `go run ./cmd/scan-jsonid -root . -out ...`
  这条命令在本仓不存在** —— `cmd/` 下只有 `server` / `migrate` / `seed` /
  `seed_audit` / `seed_dept` / `gen-errcode-doc` / `gen-swagger-schema`。
  真正的入口是上面那个 `UPDATE_ID_ALLOWLIST=1` 的测试。

## 相关

- 源码:`internal/pkg/jsonid/`(`jsonid.go` 类型 / `idtag_scan.go` 扫描器)
- 可运行示例:`examples/jsonid/main.go` —— `go run ./examples/jsonid`
- ID 生成 → [sonyflake](sonyflake.md)
- 跨模块约定(主键在 JSON 上是字符串) → [框架约定](../getting-started/conventions.md)
