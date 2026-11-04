// DashboardService 真实实现 —— 平台看板(框架指标)
//
// 设计要点:
//  1. 概览 / 趋势 / 登录热力 / 服务器指标,均使用 gorm 聚合查询
//  2. 时间序列按**东八区自然日**分桶(SQLite / MySQL 时间存储语义不同,
//     见 dashboardDayExpr;这是历史上踩过的"按 UTC 分桶,早上 8 点前的
//     记录被算到前一天"的坑)
//  3. 每个指标/序列独立判权限 —— 无权**不出现在响应里**且**不查库**
package service

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/timeutil"
)

// dashboardService 真实实现
type dashboardService struct {
	db *gorm.DB
}

// NewDashboardService 构造
func NewDashboardService(db *gorm.DB) DashboardService {
	return &dashboardService{db: db}
}

// dashboardDayExpr 返回"按**东八区自然日**分桶"的 SQL 表达式。
//
// ⚠️ **SQLite 与 MySQL 的时间存储语义不同,不能共用一个表达式**:
//
//   - **SQLite**(glebarez/sqlite):时间列存的是**带偏移量的 ISO8601 字符串**。
//     SQLite 的 `date(col)` 会把带偏移量的字符串**按 UTC 解释**,
//     于是东八区 00:00~08:00 创建的记录被算到**前一天**。
//     `date(col, '+8 hours')` 则是"先把瞬时值换算到东八区,再取日期",
//     **恒等于东八区日期**,与服务器进程时区无关。
//
//   - **MySQL**:DATETIME 列没有时区,存的是写入那一刻的**墙钟时间**。
//     `DATE_FORMAT(col,'%Y-%m-%d')` 直接取墙钟日期 ——
//     部署时区就是东八区(timeutil.AsiaShanghai),墙钟日期 = 东八区日期。
//
// ⚠️ MySQL 分支隐含"进程时区 = 东八区"这个前提(与全仓 model.Time 的
//
//	展示口径一致)。真要部署到别的时区,得改成 CONVERT_TZ —— 但那需要
//	MySQL 的时区表,sqlite 没有,跨库统一不了,所以这里选"部署地就是东八区"。
func dashboardDayExpr(db *gorm.DB, column string) string {
	if db.Dialector.Name() == "sqlite" {
		return fmt.Sprintf("date(%s, '+8 hours')", column)
	}
	return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column)
}

// userOnlineWindow "当前在线"的活动窗口
//
// 会话活动心跳(auth_touch.go)的写库节流是 5min 一次 —— 正在用的人,
// last_active_at 最多陈旧 5min。窗口取 **2 倍节流间隔(10min)**:
// 再小的话,窗口边界会把正在用的人抖出去(上一跳刚写过、下一跳还没到);
// 再大的话,"看了一眼就放着"的人会被算成在线。10min 是后台工作台
// "现在有多少人在线"的常用精度,不是会话判活(refresh_ttl 720h 那套)。
const userOnlineWindow = 10 * time.Minute

// ===== Overview =====

func (s *dashboardService) GetOverview(ctx context.Context, vis DashboardVisibility) (*Overview, error) {
	o := &Overview{}

	// ⚠️ **每个指标独立判权限** ——
	//
	//	没权限的域**不出现在响应里**(字段是 *int64 + omitempty
	//	⇒ JSON 里压根没这个 key ⇒ 前端"字段缺省 = 不显示这张卡片")。
	//
	//	⚠️ **判了权限就不要再查库**:无权 → 连 COUNT 都不发
	//	(否则"看不见"的东西照样从库里捞了一遍,白花 IO)。
	//
	//	⚠️ 权限码一律从 dashboardMetricPerms 查表拿 —— 不写字面量
	//	(字面量散在各 if 里,改权限码就得逐处改)。

	// ── 文件系统(M80)统计 ──

	// 文件总数(软删的自动被 GORM 作用域排除)
	if vis.Allows(dashboardMetricPerms["file_total"]) {
		var n int64
		if err := s.db.WithContext(ctx).Model(&model.SysFile{}).Count(&n).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询文件数失败").WithDetail(err.Error())
		}
		o.FileTotal = &n
	}
	// 今日上传(以**东八区**自然日计)
	if vis.Allows(dashboardMetricPerms["file_today"]) {
		todayStart := timeutil.Today()
		var n int64
		if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
			Where(dashboardDayExpr(s.db, "created_at")+" = ?", todayStart).
			Count(&n).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询今日上传数失败").WithDetail(err.Error())
		}
		o.FileToday = &n
	}
	// 存储占用(字节数,前端格式化成 KB/MB/GB)
	if vis.Allows(dashboardMetricPerms["file_storage_bytes"]) {
		var sum int64
		if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
			Select("COALESCE(SUM(file_size), 0)").Scan(&sum).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询存储占用失败").WithDetail(err.Error())
		}
		o.FileStorageBytes = &sum
	}
	// 文件夹总数
	if vis.Allows(dashboardMetricPerms["folder_total"]) {
		var n int64
		if err := s.db.WithContext(ctx).Model(&model.SysFolder{}).Count(&n).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询文件夹数失败").WithDetail(err.Error())
		}
		o.FolderTotal = &n
	}

	// ── 当前在线人数 ──
	//
	// 数据源就是**会话登记簿**(sys_login_log):它本来就记着
	// "这条会话最后一次被确认活着是什么时候"(last_active_at,
	// 心跳节流 5min 一次),判"在不在线"不需要新表、新心跳、新存储。
	//
	// 口径(见 Overview.UserOnline 的注释):
	//   status=1 + logout_at IS NULL + 活动时刻落在窗口内,**按 user_id 去重**。
	//
	// ⚠️ 时间比较**写进 SQL**(audit_impl.go 的登录日志时间过滤同款写法)——
	//   全表捞回来在 Go 里判的话,登记簿是热路径上的表,行数随历史线性涨,
	//   每次刷新工作台都全量捞一遍不可接受;SQL 端用两个 OR 分支代替 COALESCE
	//   (last_active_at >= ? OR (last_active_at IS NULL AND login_at >= ?)),
	//   语义恒等于 COALESCE(last_active_at, login_at) >= ?,还能吃上索引。
	if vis.Allows(dashboardMetricPerms["user_online"]) {
		cutoff := timeutil.Now().Add(-userOnlineWindow)
		var n int64
		if err := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
			Where("status = ? AND logout_at IS NULL AND "+
				"(last_active_at >= ? OR (last_active_at IS NULL AND login_at >= ?))",
				1, cutoff, cutoff).
			Distinct("user_id").
			Count(&n).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询当前在线人数失败").WithDetail(err.Error())
		}
		o.UserOnline = &n
	}

	return o, nil
}

// ===== Trend =====

func (s *dashboardService) GetTrend(ctx context.Context, vis DashboardVisibility, days int) (*Trend, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	// ⚠️ 窗口起点也按**东八区**算(不用 time.Now(),进程时区一变就飘)
	since := timeutil.Now().AddDate(0, 0, -days+1).Format("2006-01-02")

	tr := &Trend{}

	// ⚠️ **每条序列独立判权限** —— 无权的序列**不出现在响应里**
	//   (切片 + omitempty ⇒ JSON 里压根没这个 key ⇒ 前端不画这张图)。

	// fileCreate:每日新增文件数(框架口径;业务方接入后在此登记自己的序列)
	if vis.Allows(dashboardSeriesPerms["file_create"]) {
		var rows []struct {
			D string
			C int64
		}
		fileDay := dashboardDayExpr(s.db, "created_at")
		if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
			Select(fileDay+" as d, COUNT(*) as c").
			Where(fileDay+" >= ?", since).
			Group(fileDay).
			Scan(&rows).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询文件新增趋势失败").WithDetail(err.Error())
		}
		tr.FileCreate = fillTrendGaps(rows, since, days)
	}

	return tr, nil
}

// fillTrendGaps 桶补全:DB 返回的是有数据的日期,缺失日期补 0
func fillTrendGaps(rows []struct {
	D string
	C int64
}, since string, days int) []TrendPoint {
	m := make(map[string]int64, len(rows))
	for _, r := range rows {
		m[r.D] = r.C
	}
	out := make([]TrendPoint, 0, days)
	start, _ := time.Parse("2006-01-02", since)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, TrendPoint{Date: d, Value: m[d]})
	}
	return out
}

// ===== LoginHeatmap =====

// GetLoginHeatmap 每日成功登录次数(热力图)
//
// 数据源是会话登记簿:一行 = 一次登录(status=1 才算成功)。
// 按**东八区自然日**分桶(dashboardDayExpr,SQLite/MySQL 两套表达式,
// 与 GetTrend 同一条纪律),缺的天补 0(fillTrendGaps 同款)。
//
// ⚠️ 不做业务域过滤 —— 纯计数,能打开工作台就能看(接口注释见 dashboard.go)。
func (s *dashboardService) GetLoginHeatmap(ctx context.Context, days int) ([]TrendPoint, error) {
	if days <= 0 {
		// 默认约半年 —— GitHub 式热力图的一屏
		days = 182
	}
	if days > 366 {
		days = 366
	}
	// 窗口起点按**东八区**算(与 GetTrend 同款,不用 time.Now())
	since := timeutil.Now().AddDate(0, 0, -days+1).Format("2006-01-02")

	day := dashboardDayExpr(s.db, "login_at")
	var rows []struct {
		D string
		C int64
	}
	if err := s.db.WithContext(ctx).Model(&model.SysLoginLog{}).
		Select(day+" as d, COUNT(*) as c").
		Where("status = ? AND "+day+" >= ?", 1, since).
		Group(day).
		Scan(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询登录热力图失败").WithDetail(err.Error())
	}
	return fillTrendGaps(rows, since, days), nil
}
