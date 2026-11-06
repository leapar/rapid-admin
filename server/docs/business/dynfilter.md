# dynfilter —— 记录筛选表达式(筛选语法 → 参数化 SQL)

> 动态业务集合(dynx)的高级筛选器底座。前端传来一段用户手写的筛选表达式
> (`status = 'active' && amount > 1000`),包把它**安全地**编译成
> 「WHERE 片段 + 参数切片」。与 [dynx](dyn.md) 同属业务扩展。
>
> 解决的矛盾:让用户自由组合筛选条件,又不给 SQL 注入留任何结构性入口。

## 功能

- **一套表达式语法**(与 PocketBase 的 fexpr 同形状,取子集):
  比较、包含、多值 IN、NULL 判断、`&&` / `||` / 括号分组 —— 见下表;
- **结构性防注入**(不靠"过滤关键字"那种必然漏的补丁):
  值一律 `?` 占位符化,字段一律白名单解析 —— 表达式里唯一能进 SQL 文本的是
  **列名**,而列名只能来自调用方喂进来的字段白名单;
- **类型约束编译期报错**:字段什么类型就按什么类型解析值,
  数字字段塞字符串、Bool 字段塞 `'yes'` 当场报错,不静默;
- **体积上限**:表达式 2000 字符 / 100 个比较项 / IN 列表 50 值 ——
  没有上限的递归下降解析器就是一条 DoS 通道。

**不解决什么**:排序(走 pagination 的 sort 参数)、跨表 join、
全文搜索(那是 [searchidx](../storage/searchidx.md) 的事)。

## 语法

| 类别 | 语法 | 编译产物 |
|---|---|---|
| 比较 | `= != > >= < <=` | `列 = ?` 等参数化比较 |
| 包含 | `~` / `!~` | `COALESCE(列, '') LIKE ?`(值包成 `%值%`) |
| 多值 IN | `?=`,右侧括号列表或单值 | `列 IN (?, ?, ...)`(上限 50 值) |
| NULL | `= null` / `!= null` | `列 IS NULL` / `列 IS NOT NULL`(零参数) |
| 逻辑 | `&&` `\|\|` `( )` | `AND` / `OR` / 保留括号;优先级:`\|\|` < `&&` < 括号 |

值形态:字符串(单/双引号,双写转义 `'it''s ok'`)、数字(`-5` / `1.5`)、
布尔(`true` / `false`,仅 Bool 字段)、`null` 关键字。

类型约束(违反编译期报错):

- `~` / `!~` 只开放给文本类字段(Text / LongText / Select),数字 / 日期上报错;
- `null` 只能跟 `=` 或 `!=`,用在 `>` 上报错;
- MultiSelect / JSON / File 类型不开放按值筛选,`tags = 'a'` 直接报错。

## 使用方法

代码侧一个入口 `Compile`(片段出自
[examples/dynfilter/main.go](../../examples/dynfilter/main.go)):

```go
// 字段白名单:服务层用「在用字段 + 系统列」喂进来
fields := map[string]dynfilter.Field{
    "status": {Column: "f_status", Type: dynx.TypeSelect},
    "amount": {Column: "f_amount", Type: dynx.TypeNumber},
    "title":  {Column: "f_title", Type: dynx.TypeText},
}
lookup := func(key string) (dynfilter.Field, bool) {
    f, ok := fields[key]
    return f, ok
}

where, args, err := dynfilter.Compile(expr, lookup)
if err != nil {
    fmt.Printf("%s\n  ✗ %v\n\n", expr, err)
    continue
}
fmt.Printf("%s\n  WHERE %s\n  args  = %v\n\n", expr, where, args)

// 字段拼错:报错而不是静默忽略全表
_, _, err := dynfilter.Compile("staus = 'active'", lookup)
fmt.Printf("拼错字段名 ⇒ %v\n", err)
```

跑最小示例:

```bash
go run ./examples/dynfilter
```

平台内的接线(动态记录列表 `GET ...?filter=<表达式>`):
handler 把 query 存进 `q.Filter`,service 查询构建时编译后 AND 进条件列表 ——
与「f_ 多值筛选」是 **AND 关系,不是二选一**;白名单 =
在用字段 + 系统列(`id` / `owner_id` / `created_at` / `updated_at`)。

## 关键纪律

- **返回的 WHERE 片段整体带括号,调用方直接 AND 进既有条件** ——
  `a = 1 || b = 2` 不带括号会和外面的 `deleted_at IS NULL` 结合错
  (AND 优先级高于 OR);
- **字段拼错必须报错,绝不静默忽略** —— 静默返回"全表数据"比报错危险;
- **值永远走 `?` 参数** —— 能进 SQL 文本的只有白名单解析出的列名
  (且统一反引号包裹);不要引入"转义关键字"之类的补丁路径;
- **`!=` 对 NULL 行永远不成立**(SQL 三值逻辑):`amount != 100` 拿不到
  没填金额的行,要显式写 `amount != 100 || amount = null`;
- **LIKE 不转义 `%` 和 `_`,原样透传是有意为之**(不做方言各异的 ESCAPE
  子句)—— 高级用户可自己写 `%` 前缀匹配,别"顺手加转义";
- **体积上限不可放松**:`MaxExprLength = 2000` / `MaxNodes = 100`。
