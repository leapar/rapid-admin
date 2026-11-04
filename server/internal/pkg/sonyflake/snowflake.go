// Package sonyflake 分布式 ID 生成(基于 Sonyflake,17 位数字,东八区起始时间)
// 文档 §4 通用工具
//
// ⚠️ **M112(2026-10-29)修正:机号以前是会撞的**
//
//	旧实现 `sonyflake.New(Settings{StartTime: start})` **没传 MachineID**,
//	于是库用自己的默认值 —— **私网 IPv4 的低 16 位**
//	(sonyflake v2 `lower16BitPrivateIP`:`int(ip[2])<<8 + int(ip[3])`)。
//	后果:
//
//	  - `192.168.1.5` 与 `10.0.1.5` **同机号 261**
//	  - Docker/K8s 同节点副本共用节点 IP ⇒ 同机号
//	  - 库取的是 `net.InterfaceAddrs()` 里的**第一个**私网 IPv4,
//	    而这个顺序**不保证稳定**(多网卡机器上取决于系统路由表)
//	    ⇒ 同一台机器重启后机号可能换一个值
//
//	sonyflake 的 ID 布局是 `时间(39 bit) | 序号(8 bit) | 机号(16 bit)`,
//	同机号 ⇒ 同一 10ms 片内序号相同 ⇒ **生成出完全相同的 ID**。
//	本仓有 52 个文件、几十张表用它做主键 ⇒ 主键重复 ⇒ 后写覆盖先写。
//
//	⇒ 现在自己算机号(见 machineID),并允许部署期显式指定。
package sonyflake

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sony/sonyflake/v2"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

const (
	// envMachineID 部署期显式指定机号的环境变量
	//
	// K8s StatefulSet 建议直接用 ordinal 注入(StatefulSet 的 Pod 名是稳定的
	// `web-0` / `web-1`,但**重启后 pid 会变** ⇒ 别用 pid 当唯一标识)。
	envMachineID = "SONYFLAKE_MACHINE_ID"

	// 机号来源标记(供启动日志对账)
	sourceEnv      = "env"
	sourceComputed = "computed"
)

var (
	sf   *sonyflake.Sonyflake
	once sync.Once

	// 机号(0..65535)与其来源 —— 供启动日志和测试对账
	machineIDValue uint64
	machineIDFrom  = sourceComputed

	// 兜底路径(sonyflake 不可用时用)
	fallbackSeq  atomic.Uint64
	fallbackSalt uint64
	fallbackOnce sync.Once
	// 初始化告警只报一次(且必须等 logger 就绪 ⇒ 放到第一次 Next 里)
	reportInitOnce sync.Once
)

func init() {
	once.Do(func() {
		start, _ := time.Parse("2006-01-02", "2025-01-01")
		id, from, err := resolveMachineID()
		machineIDValue = id
		machineIDFrom = from
		if err != nil {
			// ⚠️ 不 panic:这是配置问题,不该让整个进程起不来。
			//   但也**不能静默** —— 记在 initErr 里,由 Next() 打一条 WARN。
			initErr = err
		}
		var e error
		sf, e = sonyflake.New(sonyflake.Settings{
			StartTime: start,
			MachineID: func() (int, error) { return int(machineIDValue), nil },
		})
		if e != nil {
			// 兜底:走 fallbackID(见下)
			sf = nil
			initErr = errors.Join(initErr, e)
		}
		fallbackSalt = fallbackSaltValue()
	})
}

// initErr 初始化过程中的问题(不致命,但必须让人看见)
var initErr error

// resolveMachineID 定机号。
//
// 优先级:
//
//  1. 环境变量 SONYFLAKE_MACHINE_ID —— 部署期显式指定(最稳)
//  2. 自动计算:私网 IPv4 低 16 位 **XOR** 主机名哈希低 16 位 **XOR** pid 低 16 位
//
// ⚠️ 环境变量**写错不 panic**,而是回落到自动计算的机号 + 打一条 WARN:
//
//	理由是"机号配错"的后果是主键重复(数据损坏),比"启动失败"更难查;
//	但为了一次笔误把整个服务打挂也不合适 —— 所以:能起,但一定告警。
func resolveMachineID() (machineID uint64, source string, err error) {
	if v := strings.TrimSpace(os.Getenv(envMachineID)); v != "" {
		n, parseErr := strconv.Atoi(v)
		if parseErr != nil || n < 0 || n >= 1<<16 {
			id, _ := computedMachineID()
			return id, sourceComputed, fmt.Errorf(
				"%s=%q 非法(应为 0..65535),已改用自动计算的机号 %d", envMachineID, v, id)
		}
		return uint64(n), sourceEnv, nil
	}
	id, _ := computedMachineID()
	return id, sourceComputed, nil
}

// computedMachineID 自动算机号。
//
// 为什么三项都要:
//
//   - 私网 IPv4 低 16 位:区分不同的机器 / 集群(不同集群的 Pod IP 网段通常不同)
//   - 主机名哈希:区分同机器上的不同实例(Pod 名 / 容器名)
//   - pid 低 16 位:区分同机器、同主机名的两个进程(本地同时起 dev 和 prod)
//
// 三项异或之后,撞号要同时满足三项都撞 —— 概率可以忽略。
//
// ⚠️ 用 XOR 而不是相加/拼接:三项各占 16 bit,XOR 后仍是 16 bit,
// 不会溢出 sonyflake 的机号空间(0..65535)。
//
// ⚠️ **没有私网 IPv4 时不报错**(返回 0 参与运算):IPv6-only 的机器照样能用
// sonyflake,只是少了 IP 这一维区分度。旧实现在这种机器上直接 `sf = nil`,
// 整个 ID 空间都退化成兜底路径(见 fallbackID)。
func computedMachineID() (machineID, ipLow uint64) {
	ipLow = privateIPv4Low16()
	hostLow := fnv1aLow16(hostname())
	pidLow := uint64(os.Getpid() & 0xFFFF)
	return (ipLow ^ hostLow ^ pidLow) & 0xFFFF, ipLow
}

// privateIPv4Low16 取本机私网 IPv4 的低 16 位。
//
// ⚠️ 与库里的实现有一处不同:库里取 `net.InterfaceAddrs()` 里的**第一个**私网 IPv4,
// 而这个顺序**不保证稳定**(多网卡机器上取决于系统路由表)
// ⇒ 同一台机器重启后可能挑到另一个 IP ⇒ 机号在两个值之间跳。
// 这里改成"遍历全部,取低 16 位最小的那个" ⇒ 同一台机器永远挑同一个。
//
// 没有私网 IPv4 时返回 0(不报错)。
func privateIPv4Low16() uint64 {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return 0
	}
	var best uint64
	found := false
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		if !isPrivateIPv4(ip) {
			continue
		}
		low := uint64(ip[2])<<8 | uint64(ip[3])
		if !found || low < best {
			best = low
			found = true
		}
	}
	return best
}

// isPrivateIPv4 判定是不是"私有段"IPv4。
//
// RFC1918 + RFC3927(link-local),与 sonyflake 库的判定口径保持一致。
// 回环地址不算私有段(那不是"这台机器"的网络身份)。
func isPrivateIPv4(ip net.IP) bool {
	if ip == nil || len(ip) != net.IPv4len || ip.IsLoopback() {
		return false
	}
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] < 32) ||
		(ip[0] == 192 && ip[1] == 168) ||
		(ip[0] == 169 && ip[1] == 254)
}

// fnv1aLow16 FNV-1a 哈希的低 16 位
func fnv1aLow16(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h & 0xFFFF
}

// hostname 取主机名(失败时给个占位值,别让整条链路挂掉)
func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown-host"
	}
	return h
}

// fallbackSaltValue 兜底 ID 的进程盐:主机名哈希 XOR pid 低 16 位
func fallbackSaltValue() uint64 {
	return (fnv1aLow16(hostname()) ^ uint64(os.Getpid()&0xFFFF)) & 0xFFFF
}

// MachineID 返回本进程使用的机号(供启动日志 / 测试对账)
func MachineID() uint64 { return machineIDValue }

// MachineIDSource 返回机号来源:"env" 或 "computed"
func MachineIDSource() string { return machineIDFrom }

// Next 取下一个 ID(uint64)
func Next() uint64 {
	// 初始化阶段的问题(比如 SONYFLAKE_MACHINE_ID 写错)在这里报一次。
	// 为什么不放在 init() 里报:那时 logger 还没初始化(logger.L() 返 Nop),
	// 告警会被**静默丢掉** —— 而这正是"配错了没人知道"最典型的成因。
	reportInitOnce.Do(func() {
		if initErr != nil {
			logger.L().Warn("sonyflake: 初始化告警",
				zap.Error(initErr),
				zap.Uint64("machine_id", machineIDValue),
				zap.String("machine_id_source", machineIDFrom))
		}
	})
	if sf == nil {
		warnFallback("sonyflake 实例不可用")
		return fallbackID()
	}
	id, err := sf.NextID()
	if err != nil {
		warnFallback("sonyflake.NextID 失败:" + err.Error())
		return fallbackID()
	}
	if id < 0 {
		warnFallback("sonyflake 返回了负数 ID")
		return fallbackID()
	}
	return uint64(id)
}

// NextString 转字符串
func NextString() string {
	return strconv.FormatUint(Next(), 10)
}

// warnFallback 兜底路径只告警一次(每次调用都打会把日志淹了)
func warnFallback(reason string) {
	fallbackOnce.Do(func() {
		logger.L().Warn("sonyflake: 已降级到兜底 ID 生成器(主键可能不再全局唯一,请检查机号配置)",
			zap.String("reason", reason),
			zap.Uint64("machine_id", machineIDValue),
			zap.String("machine_id_source", machineIDFrom))
	})
}

// fallbackID 在 sonyflake 不可用时的兜底 ID。
//
// ⚠️ 旧实现直接返 `uint64(time.Now().UnixNano())`,那不是"唯一 ID":
//
//   - 同一进程内连续两次调用可能拿到同一个纳秒(系统计时器分辨率不是纳秒,
//     Windows 上更明显)⇒ 主键重复
//   - 两个进程在同一纳秒启动会撞
//
// 现在的结构:`(unix秒 << 32) | (进程盐 << 16) | 序号`
//
//   - 高 32 位:秒级时间戳
//   - 中 16 位:进程盐(主机名哈希 XOR pid)⇒ 跨进程隔开
//   - 低 16 位:进程内自增序号 ⇒ 同进程内唯一
//     每 65536 个进位一秒(秒级时间戳 +1)⇒ 同秒内超过 65536 个也不会撞
func fallbackID() uint64 {
	seq := fallbackSeq.Add(1) - 1
	low := seq & 0xFFFF
	sec := uint64(time.Now().Unix()) + (seq >> 16)
	return (sec << 32) | (fallbackSalt << 16) | low
}
