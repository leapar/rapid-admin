package sysmetrics

import (
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// TestSampleDegradesOnMemDiskFailure mem/disk 子指标失败只降级、不整体报错
//
// 修复前 mem/disk 任一失败会让 Sample 整个 return nil, err ⇒
// Windows 上 disk.Usage 一报错,整个看板接口 500 把其余卡片带走。
// 修复后应返回快照(失败段置零)+ Warnings 记录原因,与 partitions
// 及调用方 redisMetrics(Available:false)的降级口径一致。
// 桩通过包级接缝(virtualMemory/diskUsage)注入,defer 还原即恢复真函数。
func TestSampleDegradesOnMemDiskFailure(t *testing.T) {
	origMem, origDisk := virtualMemory, diskUsage
	defer func() { virtualMemory, diskUsage = origMem, origDisk }()

	virtualMemory = func() (*mem.VirtualMemoryStat, error) { return nil, errors.New("stub mem fail") }
	diskUsage = func(_ string) (*disk.UsageStat, error) { return nil, errors.New("stub disk fail") }

	s, err := Sample(time.Millisecond)
	if err != nil {
		t.Fatalf("Sample 报错 = %v,mem/disk 失败应只降级不报错", err)
	}
	if s.MemTotalBytes != 0 || s.MemPercent != 0 {
		t.Fatalf("mem 失败后该段应置零,got mem_total=%d mem_percent=%f", s.MemTotalBytes, s.MemPercent)
	}
	if s.DiskTotalBytes != 0 || s.DiskPercent != 0 {
		t.Fatalf("disk 失败后该段应置零,got disk_total=%d disk_percent=%f", s.DiskTotalBytes, s.DiskPercent)
	}
	if len(s.Warnings) != 2 {
		t.Fatalf("Warnings 应记录 2 条失败原因,got %v", s.Warnings)
	}
}
