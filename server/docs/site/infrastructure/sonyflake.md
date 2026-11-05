# sonyflake —— 分布式 ID 生成 + worker id 分配

> 自增主键在分库分表 / 多实例下会撞;UUID 36 字符占索引空间;
> snowflake 的时间戳依赖外部时钟服务。Sonyflake 把方案改成
> `时间(39 bit) | 序号(8 bit) | 机号(16 bit)` —— **不自增** ⇒ 插入顺序不敏感、
> 不怕并发写热点;数字串 ⇒ 索引友好。

## 功能

sonyflake 提供:

1. **进程内唯一的 ID 生成器** —— `Next()`(uint64)/ `NextString()`(十进制串)。
2. **自己算机号(worker id)** —— 环境变量优先,否则 `私网 IPv4 低 16 位 XOR
   主机名哈希低 16 位 XOR pid 低 16 位`。
3. **启动自检** —— `MachineID()` / `MachineIDSource()` 供启动日志与测试对账。
4. **兜底路径** —— sonyflake 实例不可用时不返 0、不 panic,退到一个进程内唯一的降级 ID。

**不解决**:跨机房时钟同步(依赖 NTP)、ID 的业务语义(它是纯技术主键)。

## 配置

**本包无 yaml 配置** —— 机号走**环境变量**:

| 变量 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `SONYFLAKE_MACHINE_ID` | int | 无 | 显式指定机号,取值 `0..65535`。K8s StatefulSet 建议用 ordinal 注入 |

不配就走自动计算。`cmd/server/main.go` 启动时会把机号和来源打进日志:

```text
sonyflake 机号  machine_id=…  machine_id_source=env|computed
```

## 使用方法

```go
id := sonyflake.Next()          // uint64
s  := sonyflake.NextString()    // "94883951113533126"

// 对账用
sonyflake.MachineID()          // 本进程用的机号
sonyflake.MachineIDSource()    // "env" 或 "computed"
```

生成的主键是十进制数字串 ⇒ 索引友好;JSON 上**必须用字符串传**
(JS 的 2^53 装不下),见 [jsonid](jsonid.md)。

完整示例见 `examples/sonyflake/main.go`。

## 注意事项

- ⚠️ **机号是自己算的,不是库默认**。旧实现 `sonyflake.New(Settings{StartTime: start})`
  **没传 `MachineID`** ⇒ 库用自己的默认值 —— **私网 IPv4 的低 16 位**
  (`lower16BitPrivateIP`:`int(ip[2])<<8 + int(ip[3])`)。后果:
  - `192.168.1.5` 与 `10.0.1.5` **同机号 261**
  - Docker / K8s 同节点副本共用节点 IP ⇒ 同机号
  - 库取的是 `net.InterfaceAddrs()` 里的**第一个**私网 IPv4,而这个顺序**不保证稳定**
    (多网卡机器上取决于系统路由表)⇒ 同一台机器重启后机号可能换一个值
  - sonyflake 的 ID 布局是 `时间(39 bit) | 序号(8 bit) | 机号(16 bit)`,
    同机号 ⇒ 同一 10ms 片内序号相同 ⇒ **生成出完全相同的 ID**
  - 本仓有 52 个文件、几十张表用它做主键 ⇒ 主键重复 ⇒ **后写覆盖先写**
- ⚠️ **自动计算的机号含 pid ⇒ 同一台机器重启后机号会变**。
  这不是问题(机号只需在**并发存活**的进程之间唯一),但意味着
  **`MachineID()` 不能当成实例的稳定标识**写进业务表或做灰度路由。
  要稳定标识,K8s 用 StatefulSet ordinal 显式注入 `SONYFLAKE_MACHINE_ID`。
- ⚠️ **`SONYFLAKE_MACHINE_ID` 写错不 panic**,而是回落到自动计算的机号 + 打一条 WARN。
  理由:"机号配错"的后果是主键重复(数据损坏),比"启动失败"更难查;
  但为了一次笔误把整个服务打挂也不合适 —— 所以:**能起,但一定告警**。
  非法值(`非数字` / `< 0` / `>= 65536`)同样走这条路。
- ⚠️ **那条 WARN 可能永远不打出来**。初始化在 `init()` 里跑,那时 logger 还没就绪
  (`logger.L()` 返 Nop,日志被静默丢掉)⇒ 告警推迟到**第一次 `Next()`** 才打。
  ⇒ 进程启动后如果从没生成过 ID,就看不到这条告警。
  `cmd/server/main.go` 另外主动打了一条机号日志做对账。
- ⚠️ **IPv6-only 的机器也能用**。没有私网 IPv4 时该维返回 0 参与运算(不报错),
  只是少了 IP 这一维区分度。旧实现在这种机器上直接 `sf = nil`,整个 ID 空间
  都退化成兜底路径。
- ⚠️ **兜底路径的 ID 不保证全局唯一**。结构是
  `(unix秒 << 32) | (进程盐 << 16) | 序号`:
  - 高 32 位秒级时间戳 ⇒ **2106 年溢出**
  - 中 16 位进程盐(主机名哈希 XOR pid)⇒ 跨进程隔开
  - 低 16 位进程内自增序号 ⇒ 同进程内唯一(每 65536 个进位一秒)
  降级时只打一次 WARN(每次都打会把日志淹了),文案明确说"主键可能不再全局唯一"。
  旧兜底实现直接返 `uint64(time.Now().UnixNano())` —— 那**不是唯一 ID**
  (同一进程连续两次调用可能拿到同一个纳秒,Windows 上更明显)。
- ⚠️ **起始时间是 `2025-01-01`,而且是按 UTC 解析的**。
  代码写的是 `time.Parse("2006-01-02", "2025-01-01")`(`time.Parse` 对只有日期的布局
  默认按 **UTC** 解析),不是 `ParseInLocation(..., AsiaShanghai)`。
  ⇒ 包注释里"东八区起始时间"的说法与实现不符,**实际起始点比东八区的 2025-01-01
  早 8 小时**。影响:ID 的绝对值起点偏 8 小时(单调性与唯一性不受影响)。
- ⚠️ **ID 是十进制串,前端必须用字符串比较**。
  `"94883951113533126" === 94883951113533126` 是 `false`,而且大数在 `JSON.parse`
  里已经丢精度了。

## 相关

- 源码:`internal/pkg/sonyflake/`(`snowflake.go`)
- 可运行示例:`examples/sonyflake/main.go` —— `go run ./examples/sonyflake`
- JSON 上的 ID 类型 → [jsonid](jsonid.md)
- 启动日志与链路追踪 → [logger](logger.md)
