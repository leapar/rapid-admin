// Package sysmetrics 服务器资源采样 —— CPU / 内存 / 磁盘(2026-11-03)
//
// # 为什么是独立的小包而不是塞进 service
//
//	采样逻辑(gopsutil 调用 + 路径解析)是**纯系统层**的东西,不认识 DB、
//	不认识 caller —— 放 service 层会让"看板服务"多背一个系统探针的职责。
//	独立成包后,service 只做一件事:调 Sample() 把快照递出去。
//
// # 为什么磁盘量的是"进程工作目录所在的那块盘"
//
//	本平台的数据(SQLite dev.db / 本地磁盘对象存储 oss)都落在
//	`<工作目录>/data` 下 —— 运维关心的是"数据把盘占满了没有",
//	不是"C 盘还剩多少"。用 os.Getwd() 解析,失败兜底 "/"(与
//	参考实现 hertz-antd-admin-saas 的 disk.Usage("/") 口径一致)。
//
// # 参考
//
//	D:\gocodes\hertz-antd-admin-saas\admin-saas\server\utils\server.go
//	(用户点名要看的实现):CPU 用 cpu.Percent(200ms, true) 按核采样,
//	工作台也照这个口径画每核进度条(2026-11-03 用户拍了参考样式)——
//	整机平均与每核两条都给,前端各取所需。
package sysmetrics

import (
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// DefaultSampleInterval CPU 采样的观测窗口
//
// cpu.Percent 不是瞬时值 —— 它要两次读数才能算出"这段时间的平均占用"。
// 200ms 与参考实现一致:足够平滑,又不至于让接口明显变慢。
const DefaultSampleInterval = 200 * time.Millisecond

// 采样的可注入接缝 —— 测试里换成必失败的桩,验证"子指标失败只降级不报错"
// (gopsutil 在正常机器上不会失败,不注入就没法在测试里制造失败路径)
var (
	virtualMemory = mem.VirtualMemory
	diskUsage     = disk.Usage
)

// DiskMount 整机上的一块分区(一块盘一个条目)
//
// ⚠️ wire 字段一律 snake_case(全仓口径)。
type DiskMount struct {
	// 挂载点 / 盘符(Windows 上是 "C:\",Linux 上是 "/")
	Mount string `json:"mount"`
	// TotalBytes 分区总容量
	TotalBytes uint64 `json:"total_bytes"`
	// UsedBytes 已使用
	UsedBytes uint64 `json:"used_bytes"`
	// Percent 已使用百分比(0-100)
	Percent float64 `json:"percent"`
}

// Snapshot 一次采样的结果
//
// ⚠️ **wire 字段一律 snake_case**(全仓口径,2026-10-18 统一)。
type Snapshot struct {
	// CPU 整机平均占用(0-100),interval 窗口内的平均值
	CPUPercent float64 `json:"cpu_percent"`
	// CPU 逻辑核数(runtime.NumCPU)
	CPUCores int `json:"cpu_cores"`
	// 每核占用(下标 = 核序号,长度 = CPUCores)—— 工作台照参考样式画每核进度条
	PercpuPercent []float64 `json:"percpu_percent"`
	// 内存(物理内存,不含交换)
	MemUsedBytes  uint64  `json:"mem_used_bytes"`
	MemTotalBytes uint64  `json:"mem_total_bytes"`
	MemPercent    float64 `json:"mem_percent"`
	// 磁盘(进程工作目录所在盘 / 分区 —— 即数据目录所在的那块)
	// DiskPath 把"测的是哪块盘"告诉前端(如 D:\...\server)——
	// 否则卡片上只有总量数字,用户分不清是系统盘还是数据盘
	DiskPath       string  `json:"disk_path"`
	DiskUsedBytes  uint64  `json:"disk_used_bytes"`
	DiskTotalBytes uint64  `json:"disk_total_bytes"`
	DiskPercent    float64 `json:"disk_percent"`
	// 整机各分区(数据盘之外的 C 盘 / D 盘等都在这)—— 工作台单列一张卡
	// 枚举失败不算 fatal:只少这一段展示,数据盘那段照常
	Disks []DiskMount `json:"disks"`
	// Warnings 采样过程中的**非致命**失败原因(mem/disk 子指标降级时记录)
	//
	// ⚠️ 与本函数 partitions 及调用方 redisMetrics(Available:false)同一条
	// 纪律:看板是展示面,一个子指标拿不到不该把整个接口 500 把其余卡片
	// 一起带走。降级段置零,但**失败原因不吞** —— 记在这里,运维排查有据。
	Warnings []string `json:"warnings,omitempty"`
	// 运行时信息(给运维多看两眼,不是给业务用的)
	Os           string `json:"os"`
	Compiler     string `json:"compiler"`
	NumGoroutine int    `json:"num_goroutine"`
	GoVersion    string `json:"go_version"`
	// SampleMs CPU 采样实际花的毫秒数 —— 让调用方知道这一响应**带了一次
	//	200ms 的阻塞采样**,排查接口耗时时不会被这个数吓到
	SampleMs int64 `json:"sample_ms"`
}

// Sample 采一次快照
//
// ⚠️ **阻塞 interval** —— cpu.Percent 要观测一个窗口才有值。
//
//	传 0 用 DefaultSampleInterval(200ms)。HTTP handler 的 goroutine 里
//	调用是安全的,但不要放进每请求热路径 —— 这个包的存在形式就是
//	"看板点开才采一次"。
func Sample(interval time.Duration) (*Snapshot, error) {
	if interval <= 0 {
		interval = DefaultSampleInterval
	}
	s := &Snapshot{
		CPUCores:     runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
		GoVersion:    runtime.Version(),
		Os:           runtime.GOOS,
		Compiler:     runtime.Compiler,
	}

	// 按核采样 —— 一次窗口观测同时给出整机平均和每核数值
	// (两次调用各阻塞一个窗口,所以只采一次 true 再自己聚合)
	start := time.Now()
	percpu, err := cpu.Percent(interval, true)
	if err != nil {
		return nil, err
	}
	s.SampleMs = time.Since(start).Milliseconds()
	s.PercpuPercent = percpu
	var sum float64
	for _, p := range percpu {
		sum += p
	}
	if len(percpu) > 0 {
		s.CPUPercent = sum / float64(len(percpu))
	}

	// mem/disk 失败**只降级不报错**(与下方 partitions 及调用方 redisMetrics
	// 的 Available:false 口径一致):该子段置零 + 记一条 warning,其余卡片照常。
	// Windows 上 disk.Usage 偶发拿不到时,整个看板 500 是不成比例的代价。
	if u, merr := virtualMemory(); merr == nil {
		s.MemUsedBytes = u.Used
		s.MemTotalBytes = u.Total
		s.MemPercent = u.UsedPercent
	} else {
		s.Warnings = append(s.Warnings, "mem: "+merr.Error())
	}

	diskPath := dataPath()
	s.DiskPath = diskPath
	if u, derr := diskUsage(diskPath); derr == nil {
		s.DiskUsedBytes = u.Used
		s.DiskTotalBytes = u.Total
		s.DiskPercent = u.UsedPercent
	} else {
		s.Warnings = append(s.Warnings, "disk: "+derr.Error())
	}

	// 整机各分区 —— 虚拟盘 / 读卡器空槽(Total=0)跳过;同挂载点去重
	if parts, perr := disk.Partitions(false); perr == nil {
		seen := make(map[string]bool, len(parts))
		disks := make([]DiskMount, 0, len(parts))
		for _, p := range parts {
			mp := p.Mountpoint
			if mp == "" || seen[mp] {
				continue
			}
			seen[mp] = true
			if du, uerr := disk.Usage(mp); uerr == nil && du.Total > 0 {
				disks = append(disks, DiskMount{
					Mount:      mp,
					TotalBytes: du.Total,
					UsedBytes:  du.Used,
					Percent:    du.UsedPercent,
				})
			}
		}
		s.Disks = disks
	}

	return s, nil
}

// dataPath 磁盘统计的目标路径 —— 进程工作目录(数据目录的所在盘)
func dataPath() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "/"
	}
	return wd
}
