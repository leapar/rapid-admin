# sysmetrics —— 服务器资源采样(CPU / 内存 / 磁盘)

> 工作台"服务器监控"卡片的数据源:一次 `Sample()` 调用拿到整机
> CPU / 内存 / 磁盘快照。此前要么上 Prometheus 全家桶(部署重),要么
> gopsutil 调用散落各处 —— 它把这件事收口成一个调用。
> 纯系统层,不认识 DB、不认识 caller,所以独立成包而不塞进 service:
> **service 只做一件事,调 `Sample()` 把快照递出去**。
> 与 [metricsx](../infrastructure/metricsx.md)(Prometheus 进程指标)、
> [slowquery](slowquery.md)(慢 SQL)同属可观测族,它管的是**机器本身**。

## 功能

- **CPU**:按核采样(`cpu.Percent(interval, true)`),一次窗口同时拿
  每核数组 + 自行聚合整机平均(调两次各阻塞一个窗口,延迟翻倍,不这么干);
  `interval <= 0` 用 `DefaultSampleInterval`(200ms);
- **内存**:物理内存 used / total / percent(不含交换);
- **主磁盘**:目标是**进程工作目录所在盘**(数据目录所在盘)—— 平台数据
  (SQLite dev.db / 本地磁盘 oss)都在 `<工作目录>/data` 下,运维关心的是
  "数据把盘占满了没有",不是"C 盘还剩多少";
- **全盘枚举**:整机各分区(跳过空槽与重复挂载点);
- **运行时信息**:GOOS / Compiler / NumGoroutine / GoVersion / 本次采样耗时;
- **降级而不是失败**:mem / disk 子指标失败只把该段置零并记进 `Warnings`,
  绝不整体报错 —— Windows 上 `disk.Usage` 偶发拿不到,整个看板 500 是
  不成比例的代价;只有 CPU 采样失败才返回 error。

## 使用方法

一个入口(片段出自 [examples/sysmetrics/main.go](../../examples/sysmetrics/main.go)):

```go
	snap, err := sysmetrics.Sample(500 * time.Millisecond)
	if err != nil {
		fmt.Printf("采集失败(调用方决定降级展示): %v\n", err)
		return
	}
	fmt.Printf("CPU: %.1f%% × %d 核\n", snap.CPUPercent, snap.CPUCores)
	fmt.Printf("内存: %d MB / %d MB(%.1f%%)\n",
		snap.MemUsedBytes/1024/1024, snap.MemTotalBytes/1024/1024, snap.MemPercent)
	fmt.Printf("磁盘(%s): %d GB / %d GB(%.1f%%)\n",
		snap.DiskPath, snap.DiskUsedBytes/1024/1024/1024, snap.DiskTotalBytes/1024/1024/1024, snap.DiskPercent)
```

跑最小示例:

```bash
go run ./examples/sysmetrics
```

平台内接线:工作台接口 `GET /dashboard/server-metrics`
(权限码 `dashboard:server`,**比 `dashboard:read` 窄一档** —— 本接口暴露
服务器内部信息,只绑平台运营负责人 / 运营员),service 包一层
`ServerMetricsView`(快照 + Redis 指标)透传。

## 采样模型

- **没有定时任务、不落库、不进 ring buffer** —— 即采即返。前端卡片是
  "打开工作台采一次 + 手动刷新",不做轮询,200ms 延迟落在低频路径上,
  不值得为它开异步;
- **Sample 是阻塞调用**(CPU 要观测一个窗口才有值),HTTP handler
  goroutine 里安全,但**禁止放进每请求热路径**;
- 无配置项:唯一可调是 `Sample(interval)` 入参(0 = 默认 200ms)。

## 关键纪律

- **Sample 阻塞 `interval`**,设计就是"看板点开才采一次" —— 别塞进热路径;
- **mem / disk 失败只降级不报错**(该段置零 + `Warnings`),只有 CPU
  失败才 error —— 看板不能因为一个子指标挂掉整体 500;
- **磁盘测的是工作目录所在盘,不是系统盘**:`DiskPath` 必须回传前端,
  否则用户分不清是系统盘还是数据盘;
- **CPU 只调一次 `cpu.Percent(interval, true)`** 再自己聚合平均;
- wire 字段一律 snake_case(`cpu_percent` / `mem_used_bytes` / …)。
