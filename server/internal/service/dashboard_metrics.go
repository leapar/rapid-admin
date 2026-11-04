// DashboardService 服务器资源监控(CPU / 内存 / 磁盘 / Redis)—— 2026-11-03
//
// # 为什么单独一个文件
//
//	GetServerMetrics 不碰 DB —— dashboardService 的其它 5 个方法全是
//	聚合查询,只有这个是系统探针(sysmetrics.Sample)。分开放,
//	"这层是 DB 口径"的直觉不会被一个系统调用搅浑。
//
// # 权限口径(与 GetLoginHeatmap 不同!)
//
//	在线人数 / 热力图是纯计数,dashboard:read(每个登录角色都有)就够;
//	本接口暴露的是**服务器内部信息**(CPU / 内存 / 磁盘 / Redis 内存 /
//	goroutine 数 / Go 版本),不该广发给所有登录角色 —— 路由挂的是
//	rbac("dashboard", "server"),`dashboard:server` 只绑
//	平台运营负责人 / 平台运营员(super_admin 直通全量,无需单绑)。
//	前端卡片也按这个码显隐(读 accessible 清单,显示层真相)。
package service

import (
	"context"
	"strconv"
	"strings"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sysmetrics"
	"rapid-admin/internal/repository"
)

// RedisMetrics Redis 概览(仪表盘一屏看完"缓存还活着吗 / 内存涨没涨")
//
// ⚠️ 只取聚合数字(DBSIZE + INFO memory/server),**不列 key** ——
//
//	key 名单是排查工具(redis-cli --scan)的活,放网页里既慢(SCAN 全库)
//	又把内部存储结构暴露给不该看的人。
type RedisMetrics struct {
	// Available Redis 是否可用(没装配 / ping 不通都算 false)
	Available bool `json:"available"`
	// Version redis_version(如 "8.2.0",Memurai 兼容上报)
	Version string `json:"version"`
	// UsedMemoryBytes redis 自身数据占用的内存(INFO used_memory)
	UsedMemoryBytes uint64 `json:"used_memory_bytes"`
	// Keys 当前库的 key 总数(DBSIZE,含过期未回收的)
	Keys int64 `json:"keys"`
}

// ServerMetricsView 服务器资源 + Redis 概览的 wire 视图
//
// 内嵌 *sysmetrics.Snapshot 让系统指标原样平铺进 JSON,
// Redis 单独挂 redis 一个 key(枚举不出时也不影响系统指标那段)。
type ServerMetricsView struct {
	*sysmetrics.Snapshot
	Redis *RedisMetrics `json:"redis"`
}

// GetServerMetrics 采一次服务器资源快照 + Redis 概览
//
// ⚠️ **CPU 采样阻塞 ~200ms**(sysmetrics.DefaultSampleInterval)——
//
//	这是刻意的:瞬时 CPU 占用没有意义,必须观测一个窗口。
//	前端卡片是"打开工作台采一次 + 手动刷新",不做轮询,
//	所以这个延迟落在低频路径上,不值得为它开异步。
func (s *dashboardService) GetServerMetrics(ctx context.Context) (*ServerMetricsView, error) {
	snap, err := sysmetrics.Sample(0)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "采集服务器资源信息失败").WithDetail(err.Error())
	}
	view := &ServerMetricsView{Snapshot: snap, Redis: s.redisMetrics(ctx)}
	return view, nil
}

// redisMetrics 采 Redis 概览 —— **任何失败都不报错**,降级成 Available:false
//
// 看板是展示层:Redis 挂了恰恰是用户要看的状态,不该让整个接口 500
// 把 CPU / 内存那段也一起带走。
func (s *dashboardService) redisMetrics(ctx context.Context) *RedisMetrics {
	rdb := repository.GetRedis()
	if rdb == nil {
		return &RedisMetrics{Available: false}
	}
	m := &RedisMetrics{Available: true}
	if n, err := rdb.DBSize(ctx).Result(); err == nil {
		m.Keys = n
	}
	if info, err := rdb.Info(ctx, "memory", "server").Result(); err == nil {
		m.Version = redisInfoField(info, "redis_version")
		if used := redisInfoField(info, "used_memory"); used != "" {
			if v, perr := strconv.ParseUint(used, 10, 64); perr == nil {
				m.UsedMemoryBytes = v
			}
		}
	}
	return m
}

// redisInfoField 从 INFO 响应文本里取一个字段的值(行格式 "key:value\r\n")
func redisInfoField(info, key string) string {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimRight(line, "\r")
		if k, v, ok := strings.Cut(line, ":"); ok && k == key {
			return v
		}
	}
	return ""
}
