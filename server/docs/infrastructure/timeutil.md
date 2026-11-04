# timeutil —— 统一东八区的时间工具

> `time.Now()` 拿的是服务器本地时区。容器里通常是 UTC,
> 于是"今日"跨天点跟业务预期差 8 小时 —— 报表口径、每日重置、冷却期判断全都会偏。

## 功能

timeutil 提供三个函数 + 一个时区常量,把"业务时间"统一到东八区:

| 符号 | 说明 |
|---|---|
| `timeutil.AsiaShanghai` | 固定东八区时区(`time.FixedZone("CST", 8*3600)`) |
| `timeutil.Now()` | 当前时间(东八区) |
| `timeutil.Today()` | 今日,`"2006-01-02"` 字符串 |
| `timeutil.ParseShanghai(layout, value)` | 按东八区解析时间串 |

**不解决**:时区配置(东八区是写死的)、定时任务调度(在 [cronx](../scheduling/cronx.md),
那边有独立的 `job.timezone` 配置)。

## 配置

**本包无 yaml 配置** —— 东八区是写死的常量。

> 注:定时任务那一层的时区是**可配的**(`job.timezone`,默认 `Asia/Shanghai`,
> 见 `configs/config.example.yaml`),与本包不是同一套东西。

## 使用方法

```go
now := timeutil.Now()                       // 东八区的时间点
today := timeutil.Today()                   // "2026-11-01"

t, err := timeutil.ParseShanghai("2006-01-02 15:04:05", "2026-11-01 08:30:00")
// t.Format(time.RFC3339) == "2026-11-01T08:30:00+08:00"
```

跨时区比较:同一时刻,UTC 和东八区表示不同,但 `unix` 时间戳相同。

```go
t.Unix()          // 同一个数
t.UTC().Format("2006-01-02 15:04:05")
```

存量数据里那些不带时区的时间串,按东八区口径解释:

```go
t, err := timeutil.ParseShanghai("2006-01-02 15:04:05", raw)
```

模型层已经在用这套口径(`internal/model/time.go` 的 `MarshalJSON` /
`UnmarshalJSON` 都按 `timeutil.AsiaShanghai` 换算)。

完整示例见 `examples/timeutil/main.go`。

## 注意事项

- ⚠️ **`AsiaShanghai` 是 `time.FixedZone`,不是 `time.LoadLocation("Asia/Shanghai")`**。
  固定偏移 +8,**不带历史时区规则**(中国自 1991 年起无夏令时,所以实际上无差别);
  但它也不会读 tzdata。
- ⚠️ **`Today()` 返回的是字符串,不是 `time.Time`**。
  要拿"今日零点"做时间比较,得自己 `timeutil.ParseShanghai("2006-01-02", timeutil.Today())`。
- ⚠️ **`ParseShanghai` 只在"输入不带时区"时才是东八区**。
  `time.ParseInLocation` 的语义是:输入串里**带了**偏移量就以它为准,
  没带才用 `AsiaShanghai`。传 RFC3339(带 `Z` 或 `+08:00`)进去,结果是那个偏移量,
  不是东八区。
- ⚠️ **`Now()` 是"时间点 + 固定时区",不是把 UTC 换算一下**。
  `Now().UTC()` 与 `Now()` 是同一个瞬间的两种表示 —— 存库 / 传输用 `time.Time`
  本来就带时区,别手动 `Format` 成不带时区的字符串再存。
- ⚠️ **本包不提供格式化函数**。要"东八区的 `2006-01-02 15:04:05`"得自己
  `timeutil.Now().Format("2006-01-02 15:04:05")`(先 `Now()` 拿到东八区时间点)。
- ⚠️ **别把本包和定时任务的时区混为一谈**。`cronx` / `job` 有自己的
  `job.timezone` 配置(缺省会告警),改那个不影响本包,反之亦然。

## 相关

- 源码:`internal/pkg/timeutil/`(`time.go`)
- 可运行示例:`examples/timeutil/main.go` —— `go run ./examples/timeutil`
- 定时任务时区 → [cronx](../scheduling/cronx.md)
- 日志时间戳 → [logger](logger.md)
