// sysmetrics —— 采集整机 CPU / 内存 / 磁盘指标,给工作台"服务器监控"卡片
//
// ## 这个包解决什么问题
//
// 工作台要展示"服务器状态",此前要么上 Prometheus 全家桶(部署重),
// 要么直接调 gopsutil 散落各处。sysmetrics 收口成一个 Sample 调用。
//
// ## 关键点
//
//   - CPU 占用需要两次采样点才能算,所以必须给 interval(建议 500ms 以上)
//   - 磁盘测的是**进程工作目录所在盘** —— 即数据目录所在的那块
//   - 采集失败降级返回错误,由调用方决定降级展示(不能把工作台打挂)
//
// ## 跑法
//
//	go run ./examples/sysmetrics
package main

import (
	"fmt"
	"time"

	"rapid-admin/internal/pkg/sysmetrics"
)

func main() {
	snap, err := sysmetrics.Sample(500 * time.Millisecond)
	if err != nil {
		fmt.Println("采集失败(调用方决定降级展示):", err)
		return
	}

	fmt.Printf("CPU: %.1f%% × %d 核\n", snap.CPUPercent, snap.CPUCores)
	for i, p := range snap.PercpuPercent {
		fmt.Printf("  核 %2d: %5.1f%%\n", i, p)
	}
	fmt.Printf("内存: %d MB / %d MB(%.1f%%)\n",
		snap.MemUsedBytes/1024/1024, snap.MemTotalBytes/1024/1024, snap.MemPercent)
	fmt.Printf("磁盘(%s): %d GB / %d GB(%.1f%%)\n",
		snap.DiskPath, snap.DiskUsedBytes/1024/1024/1024, snap.DiskTotalBytes/1024/1024/1024, snap.DiskPercent)
}
