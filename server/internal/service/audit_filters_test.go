// 审计日志的筛选维度(2026-10-30)
//
// # 这一组守的是什么
//
//		审计中心三个页面(操作日志 / 数据变更 / 登录日志)此前有两个同源缺陷:
//
//	 1. **前端发了、后端不读** —— 前端搜索框发 `keyword`、成功/失败筛选发 `status`、
//	    时间筛选发 `startAt`/`endAt`,而 handler 从来没往 query 里塞过它们。
//	    service 层其实**早就实现了**这些条件(OperationLogQuery.StartAt 等),
//	    只是 handler 没接线 ⇒ 页面上的控件全是摆设(HTTP 200,结果不变,不报错)。
//	 2. **IDOR 收敛写漏了一半** —— 旧代码是
//	    `if q.UserID > 0 && !特权 { q.UserID = caller.UserID }`:
//	    **只有"点名查别人"才收敛**,而"什么都不传"这条最常见的路径反而不筛,
//	    拿到的是全平台日志。注释写的意图("其他角色只看自己")与实现不符。
//
// ⇒ 现在:三个 handler 都把筛选参数接上;收敛改为**无条件**。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 handler 里的 Keyword/Status/StartAt/EndAt 去掉 → 「筛选真的进了 SQL」变红
//	· 把 `auditScopeUserID` 改回 `if userID > 0 {...}` → 「不点名就全看」变红
//	· 把 Keyword 的 WHERE 去掉 → 「关键字过滤」变红
package service

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

func newAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysOperationLog{},
		&model.SysDataChangeLog{},
		&model.SysLoginLog{},
		// ⚠️ sys_user **必须建**(2026-10-30)——
		//   操作日志 / 登录日志的查询现在 LEFT JOIN 它回填 username。
		//   少建这张表 ⇒ 查询直接报 "no such table: sys_user"(HTTP 500)。
		&model.SysUser{},
	))
	return db
}

// pageIDs 取一页结果的 id 列表
func pageIDs(t *testing.T, page *model.PageResponse) []string {
	t.Helper()
	raw, err := json.Marshal(page.List)
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(raw, &items))
	out := make([]string, 0, len(items))
	for _, m := range items {
		id, _ := m["id"].(string)
		out = append(out, id)
	}
	return out
}

func intPtr(v int) *int { return &v }

// TestAuditServiceFilters 三类日志的筛选维度
func TestAuditServiceFilters(t *testing.T) {
	db := newAuditTestDB(t)
	// scope 传 nil —— 本用例只验三类日志的筛选维度,不做行级过滤
	svc := NewAuditService(db, nil)
	ctx := context.Background()

	now := time.Now()

	// --- 操作日志 3 条 ---
	opIDs := make([]uint64, 3)
	opMeta := []struct {
		username, module, action, desc string
		status                         int
	}{
		{"alice", "user", "create", "新建用户", 1},
		{"bob", "config", "update", "改配置", 0},
		{"alice", "role", "delete", "删角色", 1},
	}
	for i, m := range opMeta {
		id := sonyflake.Next()
		opIDs[i] = id
		require.NoError(t, db.Create(&model.SysOperationLog{
			BaseModel:     model.BaseModel{ID: id, CreatedAt: now.Add(time.Duration(i) * time.Minute)},
			UserID:        sonyflake.Next(),
			Username:      m.username,
			Module:        m.module,
			Action:        m.action,
			Description:   m.desc,
			RequestURL:    "/rapid-admin/v1/x",
			RequestMethod: "POST",
			IP:            "10.0.0.1",
			Status:        m.status,
			Duration:      5,
		}).Error)
	}

	// --- 数据变更 3 条 ---
	dcIDs := make([]uint64, 3)
	dcMeta := []struct {
		table, user string
		op          int
	}{
		{"sys_user", "alice", 1},
		{"sys_role", "bob", 2},
		{"sys_user", "alice", 3},
	}
	for i, m := range dcMeta {
		id := sonyflake.Next()
		dcIDs[i] = id
		require.NoError(t, db.Create(&model.SysDataChangeLog{
			BaseModel:      model.BaseModel{ID: id, CreatedAt: now.Add(time.Duration(i) * time.Minute)},
			TableNameField: m.table,
			RecordID:       sonyflake.Next(),
			Operation:      m.op,
			Before:         "{}",
			After:          "{}",
			UserID:         sonyflake.Next(),
			Username:       m.user,
		}).Error)
	}

	// --- 登录日志 3 条 ---
	lgIDs := make([]uint64, 3)
	lgMeta := []struct {
		user, ip string
		status   int
	}{
		{"alice", "10.0.0.1", 1},
		{"bob", "10.0.0.2", 0},
		{"alice", "10.0.0.3", 1},
	}
	for i, m := range lgMeta {
		id := sonyflake.Next()
		lgIDs[i] = id
		require.NoError(t, db.Create(&model.SysLoginLog{
			BaseModel: model.BaseModel{ID: id, CreatedAt: now.Add(time.Duration(i) * time.Minute)},
			UserID:    sonyflake.Next(),
			Username:  m.user,
			IP:        m.ip,
			Status:    m.status,
			Message:   "ok",
			LoginAt:   model.Time{Time: now.Add(time.Duration(i) * time.Minute)},
		}).Error)
	}

	t.Run("操作日志:关键字命中用户名/模块/动作/描述任一列", func(t *testing.T) {
		// 用户名
		page, err := svc.ListOperationLogs(ctx, OperationLogQuery{Keyword: "alice"})
		require.NoError(t, err)
		require.Equal(t, int64(2), page.Total)

		// 模块
		page, err = svc.ListOperationLogs(ctx, OperationLogQuery{Keyword: "config"})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)

		// 描述(中文)
		page, err = svc.ListOperationLogs(ctx, OperationLogQuery{Keyword: "删角色"})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)
	})

	t.Run("操作日志:status=0 只看失败(指针语义,不是「不筛」)", func(t *testing.T) {
		page, err := svc.ListOperationLogs(ctx, OperationLogQuery{Status: intPtr(0)})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)

		page, err = svc.ListOperationLogs(ctx, OperationLogQuery{Status: intPtr(1)})
		require.NoError(t, err)
		require.Equal(t, int64(2), page.Total)

		// 不传 = 不筛
		page, err = svc.ListOperationLogs(ctx, OperationLogQuery{})
		require.NoError(t, err)
		require.Equal(t, int64(3), page.Total)
	})

	t.Run("操作日志:时间范围", func(t *testing.T) {
		// 只要第 2、3 条(now+1min / now+2min)
		from := now.Add(30 * time.Second).Unix()
		page, err := svc.ListOperationLogs(ctx, OperationLogQuery{StartAt: &from})
		require.NoError(t, err)
		require.Equal(t, int64(2), page.Total)
	})

	t.Run("数据变更:表名关键字 + 操作类型", func(t *testing.T) {
		page, err := svc.ListDataChangeLogs(ctx, DataChangeLogQuery{Keyword: "sys_user"})
		require.NoError(t, err)
		require.Equal(t, int64(2), page.Total)

		page, err = svc.ListDataChangeLogs(ctx, DataChangeLogQuery{Operation: intPtr(2)})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)

		// 表名 + 操作类型同时给 = AND
		page, err = svc.ListDataChangeLogs(ctx, DataChangeLogQuery{
			TableName: "sys_user",
			Operation: intPtr(1),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)
		require.Equal(t, []string{strconv.FormatUint(dcIDs[0], 10)}, pageIDs(t, page))
	})

	t.Run("登录日志:用户名关键字 + 成功/失败 + IP", func(t *testing.T) {
		page, err := svc.ListLoginLogs(ctx, LoginLogQuery{Keyword: "alice"})
		require.NoError(t, err)
		require.Equal(t, int64(2), page.Total)

		yes := true
		page, err = svc.ListLoginLogs(ctx, LoginLogQuery{Success: &yes})
		require.NoError(t, err)
		require.Equal(t, int64(2), page.Total)

		no := false
		page, err = svc.ListLoginLogs(ctx, LoginLogQuery{Success: &no})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)
		require.Equal(t, []string{strconv.FormatUint(lgIDs[1], 10)}, pageIDs(t, page))

		page, err = svc.ListLoginLogs(ctx, LoginLogQuery{IP: "10.0.0.3"})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)
	})
}
