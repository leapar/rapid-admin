// 平台看板 service 的**真实口径**测试
//
// # 这一组守的是什么
//
//	看板四个接口的口径与权限过滤。开源口径下框架自带指标 =
//	文件系统统计 + 在线人数 + 上传趋势 + 登录热力 + 服务器资源快照;
//	业务方接入自己的模块后按 dashboard_visibility.go 登记域即可复用这套过滤。
//
// # 为什么用 glebarez/sqlite(生产同款驱动)
//
//	时间列存成什么格式是**驱动相关**的,这一组验的正是"按东八区分桶",
//	所以必须用生产那个驱动验,不能拿 gorm.io/driver/sqlite 凑。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 dashboardDayExpr 的 SQLite 分支改回 `date(col)` → 「时区分桶」变红
//	· 把 GetOverview 里的 `if vis.Allows(...)` 去掉 → 「无权字段不出现在响应里」红
//	· 把 Overview 的指针改回 int64 → 「零值仍要显示」变红(0 被 omitempty 吃掉)
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/pkg/timeutil"
)

func newDashboardTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		// 文件系统(M80)—— 看板的文件统计用
		&model.SysFile{},
		&model.SysFolder{},
		// 会话登记簿 —— 在线人数 / 登录热力图用
		&model.SysLoginLog{},
	))
	return db
}

// allVisible 造一份"所有域都可见"的可见性
//
// ⚠️ 只给**不测权限过滤**的用例用(口径类的那些)——
//
//	测过滤的用例自己造 vis,别用这个。
func allVisible() DashboardVisibility {
	vis := DashboardVisibility{}
	for _, d := range DashboardDomains() {
		vis[d.Code] = true
	}
	return vis
}

// visOf 造一份"只有指定权限码可见"的可见性
func visOf(codes ...string) DashboardVisibility {
	vis := DashboardVisibility{}
	for _, c := range codes {
		vis[c] = true
	}
	return vis
}

// ===== 文件系统统计(M80)=====

// mkFile 造一条文件记录(指定大小 / 创建时刻)
func mkFile(t *testing.T, db *gorm.DB, size int64, createdAt time.Time) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysFile{
		BaseModel: model.BaseModel{ID: sonyflake.Next(), CreatedAt: createdAt},
		FolderID:  1,
		UserID:    1,
		Biz:       "test",
		FileKey:   fmt.Sprintf("k/%d", sonyflake.Next()),
		FileName:  "f.bin",
		FileSize:  size,
	}).Error)
}

func TestDashboard_Overview_FileStats(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	now := timeutil.Now()
	earlyToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 30, 0, 0, timeutil.AsiaShanghai)
	mkFile(t, db, 100, now.AddDate(0, 0, -1)) // 昨天 → file_total 有它,file_today 没有
	mkFile(t, db, 200, earlyToday)            // 今天(东八区 00:30)→ 两边都有
	mkFile(t, db, 400, earlyToday)            // 今天
	// 软删一条 → 三个指标都不算它
	require.NoError(t, db.Delete(&model.SysFile{}, `file_size = ?`, int64(400)).Error)

	o, err := svc.GetOverview(ctx, allVisible())
	require.NoError(t, err)
	require.NotNil(t, o.FileTotal)
	require.Equal(t, int64(2), *o.FileTotal, "软删的文件不计入总数")
	require.NotNil(t, o.FileToday)
	require.Equal(t, int64(1), *o.FileToday, "今天上传的按东八区自然日计(昨天那条不算)")
	require.NotNil(t, o.FileStorageBytes)
	require.Equal(t, int64(300), *o.FileStorageBytes, "软删的 400 字节不计入占用")
	require.NotNil(t, o.FolderTotal)
	require.Equal(t, int64(0), *o.FolderTotal, "没造文件夹 → 0(0 也要画,不能用真值判断)")
}

// TestDashboard_Trend_BucketsByShanghaiDay 东八区 00:30 创建的文件必须算在**今天**
//
// 旧口径 `date(created_at)`(SQLite 按 UTC 解释)会把这条算到**昨天**。
func TestDashboard_Trend_BucketsByShanghaiDay(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	now := timeutil.Now()
	early := time.Date(now.Year(), now.Month(), now.Day(), 0, 30, 0, 0, timeutil.AsiaShanghai)
	mkFile(t, db, 1, early)

	tr, err := svc.GetTrend(ctx, allVisible(), 7)
	require.NoError(t, err)
	require.Len(t, tr.FileCreate, 7)
	last := tr.FileCreate[len(tr.FileCreate)-1]
	require.Equal(t, timeutil.Today(), last.Date)
	require.Equal(t, int64(1), last.Value, "趋势的最后一天(=今天)应该有这 1 个文件")
}

func TestDashboard_Overview_FileGatedByPerm(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	mkFile(t, db, 1, timeutil.Now())

	// 只有 file:list → 4 个文件指标里 folder_total 不出现(指针 = nil ⇒ omitempty)
	o, err := svc.GetOverview(ctx, visOf("file:list"))
	require.NoError(t, err)
	require.NotNil(t, o.FileTotal)
	require.Nil(t, o.FolderTotal, "没有 folder:list ⇒ folder_total 不该出现在响应里")

	// 只有 folder:list → 反过来
	o, err = svc.GetOverview(ctx, visOf("folder:list"))
	require.NoError(t, err)
	require.NotNil(t, o.FolderTotal)
	require.Nil(t, o.FileTotal, "没有 file:list ⇒ 文件指标不该出现在响应里")
}

// ===== 桶补全 =====

func TestDashboard_Trend_FillsMissingDays(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	tr, err := svc.GetTrend(ctx, allVisible(), 7)
	require.NoError(t, err)
	require.Len(t, tr.FileCreate, 7)

	// 日期必须是从"今天-6"到"今天"的连续 7 天
	want := timeutil.Now().AddDate(0, 0, -6).Format("2006-01-02")
	require.Equal(t, want, tr.FileCreate[0].Date)
	require.Equal(t, timeutil.Today(), tr.FileCreate[6].Date)

	// 没有数据的日期补 0,而不是缺一个点
	for _, p := range tr.FileCreate {
		require.Equal(t, int64(0), p.Value)
	}
}

// ===== wire 形状(snake_case)=====
//
// ⚠️ 前端读的是**驼峰**就会拿到 undefined —— 而 TypeScript 不会报错
//
//	(类型是前端自己写的)⇒ KPI 卡片恒为 "-",而且没有任何东西会红。
//	这一条把 wire 形状钉死:改名必须同 commit 改前端。
//
// ⚠️ **必须 marshal 一个"有值"的 Overview** —— 字段是指针 + omitempty,
//
//	空结构体 marshal 出来是 `{}`,断言"字段在"会**永远失败**
//	(或者更糟:有人把断言改成"字段不在"就绿了,而线上字段全没了)。
func TestDashboard_WireShapeIsSnakeCase(t *testing.T) {
	zero := int64(0)
	raw, err := json.Marshal(&Overview{
		FileTotal:        &zero,
		FileToday:        &zero,
		FileStorageBytes: &zero,
		FolderTotal:      &zero,
		UserOnline:       &zero,
	})
	require.NoError(t, err)
	var ov map[string]any
	require.NoError(t, json.Unmarshal(raw, &ov))
	for _, k := range []string{
		"file_total", "file_today", "file_storage_bytes", "folder_total", "user_online",
	} {
		require.Contains(t, ov, k)
	}

	raw, err = json.Marshal(&Trend{
		FileCreate: []TrendPoint{{Date: "2026-11-01", Value: 0}},
	})
	require.NoError(t, err)
	var tr map[string]any
	require.NoError(t, json.Unmarshal(raw, &tr))
	for _, k := range []string{"file_create"} {
		require.Contains(t, tr, k)
	}
}

// ===== 权限过滤 =====
//
// # 这一组守的是什么
//
//	拆成两半:
//	  ① **接口不再 403** —— dashboard:read 进了 authBaseCodes
//	     (seed 那边有 TestRun_BindsAuthBaseToAllRoles 钉着);
//	  ② **内容按域权限过滤** —— 没权限的域**不出现在响应里**。
//
//	②的失败症状是**静默**的:字段多一个少一个,前端多画一张少画一张卡片,
//	**没有报错、没有日志**。所以下面几条网必须钉死。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 GetOverview 里的 `if vis.Allows(...)` 去掉 → 「无权字段不出现在响应里」红;
//	· 把 Overview 的指针改回 int64 → 编译不过(字段类型变了)。

// TestDashboard_Overview_OmitsUnauthorizedMetrics 无权限的指标**不出现在响应里**
func TestDashboard_Overview_OmitsUnauthorizedMetrics(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	mkFile(t, db, 1, timeutil.Now())

	// 只有 file:list 可见
	o, err := svc.GetOverview(ctx, visOf("file:list"))
	require.NoError(t, err)

	require.NotNil(t, o.FileTotal, "file:list 可见 ⇒ file_total 必须在")
	require.Nil(t, o.UserOnline, "user_online 挂的是 dashboard:read —— 没被授予 ⇒ 也不该在")
	require.Nil(t, o.FolderTotal, "folder:list 不可见 ⇒ folder_total 必须**不在**响应里")

	// ⚠️ **JSON 里也不能有那些 key** —— 前端靠"字段缺省"决定不画卡片。
	//   结构体里是 nil 不够:万一 omitempty 被去掉,前端就会读到 0 并画出来。
	raw, err := json.Marshal(o)
	require.NoError(t, err)
	var ov map[string]any
	require.NoError(t, json.Unmarshal(raw, &ov))
	require.NotContains(t, ov, "folder_total", "无权域的字段**不能出现在 JSON 里**(前端靠字段缺省决定不显示)")
	require.Contains(t, ov, "file_total")
}

// TestDashboard_Overview_ZeroStillPresent 有权限但没数据 ⇒ 字段在,值为 0
//
// ⚠️ 这条守的是 omitempty 的坑:字段是**指针**,所以"值为 0"依然会被序列化
//
//	("看得见但这个月一个都没有" ≠ "看不见")。
//	如果哪天把字段改回 int64,这条会红(0 被 omitempty 吃掉)。
func TestDashboard_Overview_ZeroStillPresent(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	o, err := svc.GetOverview(ctx, visOf("folder:list"))
	require.NoError(t, err)
	require.NotNil(t, o.FolderTotal)
	require.Equal(t, int64(0), *o.FolderTotal)

	raw, err := json.Marshal(o)
	require.NoError(t, err)
	var ov map[string]any
	require.NoError(t, json.Unmarshal(raw, &ov))
	require.Contains(t, ov, "folder_total", "有权限但没数据 ⇒ 字段必须在(值 0),不能被 omitempty 吃掉")
	require.Equal(t, float64(0), ov["folder_total"])
}

// TestDashboard_Trend_OmitsUnauthorizedSeries 无权限的序列不出现在响应里
func TestDashboard_Trend_OmitsUnauthorizedSeries(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	tr, err := svc.GetTrend(ctx, visOf("dashboard:read"), 7)
	require.NoError(t, err)

	require.Nil(t, tr.FileCreate, "file:list 不可见 ⇒ file_create 必须不在")

	raw, err := json.Marshal(tr)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	require.NotContains(t, m, "file_create", "无权序列不能出现在 JSON 里")
}

// TestDashboard_NilVisibilityDeniesEverything nil 可见性 ⇒ 什么都不显示
//
// ⚠️ 安全侧倒向:"没判定过"必须落在**不可见**这一侧。
//
//	(nil map 上 vis.Allows 读出零值 false —— 这条钉住这个语义。)
func TestDashboard_NilVisibilityDeniesEverything(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	var vis DashboardVisibility // nil
	o, err := svc.GetOverview(ctx, vis)
	require.NoError(t, err)
	require.Nil(t, o.FileTotal)
	require.Nil(t, o.UserOnline)

	raw, err := json.Marshal(o)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(raw), "没有任何域可见 ⇒ 响应就是一个空对象")
}

// TestDashboard_MetricPermsCoverAllFields 指标表必须覆盖 Overview / Trend 的每一个字段
//
// ⚠️ 漏一个 ⇒ 那个指标**永远不出现**(查表拿到空串 ⇒ Allows("") 恒 false),
//
//	而且**不报错**。这条网把"漏了"变成"变红"。
func TestDashboard_MetricPermsCoverAllFields(t *testing.T) {
	// Overview 的字段 ↔ json tag
	// ⚠️ 用一个"全有值"的副本取字段名(空结构体 omitempty 后是 {})
	zero := int64(0)
	raw, err := json.Marshal(&Overview{
		FileTotal: &zero, FileToday: &zero, FileStorageBytes: &zero,
		FolderTotal: &zero, UserOnline: &zero,
	})
	require.NoError(t, err)
	var ov map[string]any
	require.NoError(t, json.Unmarshal(raw, &ov))
	for k := range ov {
		require.Contains(t, dashboardMetricPerms, k,
			"Overview 的字段 %q 没有对应的权限码 —— 它会**永远不出现**在响应里(且不报错)", k)
	}
	require.Len(t, dashboardMetricPerms, len(ov),
		"指标表和 Overview 字段数对不上 —— 多出来的那个指标是死码")

	// Trend 的字段 ↔ json tag
	raw, err = json.Marshal(&Trend{
		FileCreate: []TrendPoint{{Date: "d", Value: 0}},
	})
	require.NoError(t, err)
	var tr map[string]any
	require.NoError(t, json.Unmarshal(raw, &tr))
	for k := range tr {
		require.Contains(t, dashboardSeriesPerms, k,
			"Trend 的字段 %q 没有对应的权限码 —— 它会**永远不出现**在响应里(且不报错)", k)
	}
	require.Len(t, dashboardSeriesPerms, len(tr))
}

// TestDashboard_DomainsCoverAllPerms 域表必须覆盖指标表用到的所有权限码
//
// ⚠️ 域表是 handler 层**判定**的依据 —— 少一个 ⇒ 那个域对所有人都不显示
//
//	(CanAccess 压根没被调用过)。
func TestDashboard_DomainsCoverAllPerms(t *testing.T) {
	inDomains := map[string]bool{}
	for _, d := range DashboardDomains() {
		inDomains[d.Code] = true
	}
	for _, m := range dashboardMetricPerms {
		require.True(t, inDomains[m], "域表里没有 %q —— 它的指标永远不显示", m)
	}
	for _, m := range dashboardSeriesPerms {
		require.True(t, inDomains[m], "域表里没有 %q —— 它的序列永远不显示", m)
	}
	// 域的 Code 必须等于 Module+":"+Action(判定时拼出来的就是它)
	for _, d := range DashboardDomains() {
		require.Equal(t, d.Module+":"+d.Action, d.Code,
			"域 %q 的 Code 与 Module/Action 对不上 —— 判定的资源码和显示用的码不是同一个", d.Code)
	}
}

// mkLoginLog 造一条登录登记
//
// lastActive 传 nil = 从未活动过的心跳(判活走 login_at 兜底)
func mkLoginLog(t *testing.T, db *gorm.DB, userID uint64, loginAt time.Time, status int, lastActive *time.Time, logout *time.Time) {
	t.Helper()
	var la *model.Time
	if lastActive != nil {
		la = &model.Time{Time: *lastActive}
	}
	var lo *model.Time
	if logout != nil {
		lo = &model.Time{Time: *logout}
	}
	require.NoError(t, db.Create(&model.SysLoginLog{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		UserID:       userID,
		Username:     fmt.Sprintf("u%d", userID),
		Status:       status,
		SessionID:    fmt.Sprintf("sid-%d", sonyflake.Next()),
		LoginAt:      model.Time{Time: loginAt},
		LastActiveAt: la,
		LogoutAt:     lo,
	}).Error)
}

// TestDashboard_UserOnline 当前在线人数的口径
//
// 口径(见 Overview.UserOnline 注释):status=1 + logout_at IS NULL +
// 活动时刻落在 10min 窗口内,按 user_id 去重。
//
// # 可逆探针
//
//	· 去掉 status=1 过滤 → 登录失败的也被算进来,红;
//	· 去掉 logout_at IS NULL → 登出过的回来,红;
//	· 窗口改 5min(= 心跳节流间隔)→ "5 分钟前刚活动"的那条掉出去,红;
//	· 去掉 DISTINCT → 两个人各开两个浏览器数成 4,红。
func TestDashboard_UserOnline(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	now := timeutil.Now()
	active := func(d time.Duration) *time.Time { t := now.Add(-d); return &t }

	// 在线:7 分钟前活动(窗口 10min 内)—— 用户 1,两个会话(两个浏览器)算 1 人
	mkLoginLog(t, db, 1, now.Add(-30*time.Minute), 1, active(7*time.Minute), nil)
	mkLoginLog(t, db, 1, now.Add(-10*time.Minute), 1, active(8*time.Minute), nil)
	// 在线:1 分钟前活动 —— 用户 2
	mkLoginLog(t, db, 2, now.Add(-2*time.Hour), 1, active(1*time.Minute), nil)
	// 不算:11 分钟前活动(窗口外)—— 用户 3
	mkLoginLog(t, db, 3, now.Add(-1*time.Hour), 1, active(11*time.Minute), nil)
	// 不算:从未活动过,但 login_at 在 20 分钟前(兜底走 login_at,窗口外)
	mkLoginLog(t, db, 4, now.Add(-20*time.Minute), 1, nil, nil)
	// 不算:已登出(哪怕 1 分钟前还在活动)
	mkLoginLog(t, db, 5, now.Add(-1*time.Hour), 1, active(1*time.Minute), active(30*time.Second))
	// 不算:登录失败(status=0,压根没有会话)
	mkLoginLog(t, db, 6, now.Add(-1*time.Minute), 0, nil, nil)

	o, err := svc.GetOverview(ctx, allVisible())
	require.NoError(t, err)
	require.NotNil(t, o.UserOnline)
	require.Equal(t, int64(2), *o.UserOnline,
		"在线 = 用户 1(两会话去重)+ 用户 2;3/4 窗口外,5 已登出,6 登录失败")

	// 权限:dashboard:read 不可见 ⇒ 字段整个不在(与其它指标同一套纪律)
	o2, err := svc.GetOverview(ctx, visOf("file:list"))
	require.NoError(t, err)
	require.Nil(t, o2.UserOnline, "dashboard:read 不可见 ⇒ user_online 不出现在响应里")
}

// TestDashboard_LoginHeatmap 每日登录热力图的口径
//
// 只数 status=1 的登录,按东八区自然日分桶,缺的天补 0。
//
// # 可逆探针
//
//	· 去掉 status=1 → 失败登录也被数进去,红;
//	· SQLite 分支改回 date(login_at)(按 UTC)→ 东八区 00:30 那条掉到前一天,红;
//	· 去掉 fillTrendGaps → 窗口里没登录的日子整个缺失,红。
func TestDashboard_LoginHeatmap(t *testing.T) {
	db := newDashboardTestDB(t)
	svc := NewDashboardService(db)
	ctx := context.Background()

	now := timeutil.Now()
	today := timeutil.Today()

	// 今天:2 次成功登录(同一用户两次也算 2 —— 数的是"登录次数"不是"人数")
	mkLoginLog(t, db, 1, now.Add(-1*time.Hour), 1, nil, nil)
	mkLoginLog(t, db, 1, now.Add(-2*time.Hour), 1, nil, nil)
	// 今天:1 次失败登录 —— 不算
	mkLoginLog(t, db, 2, now.Add(-3*time.Hour), 0, nil, nil)
	// 昨晚:1 次成功登录
	yest := now.AddDate(0, 0, -1)
	mkLoginLog(t, db, 3, yest, 1, nil, nil)

	points, err := svc.GetLoginHeatmap(ctx, 7)
	require.NoError(t, err)
	require.Len(t, points, 7, "7 天窗口 = 7 个点,缺的天补 0")

	// 桶补全:窗口里没有登录的日子也要在(值为 0),不能整个缺失
	byDate := map[string]int64{}
	for _, p := range points {
		byDate[p.Date] = p.Value
	}
	require.Equal(t, int64(2), byDate[today], "今天 = 2 次成功登录(失败的不算)")

	// 昨天的桶:昨晚那 1 次成功登录必须落在昨天(不是被 UTC 分桶吃掉)
	require.Equal(t, int64(1), byDate[now.AddDate(0, 0, -1).Format("2006-01-02")],
		"昨天 = 1 次成功登录")
}
