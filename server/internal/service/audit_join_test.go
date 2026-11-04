// 审计日志的用户名回填 + 身份切换审计的字段(2026-10-30)
//
// # 这两条洞是一起查出来的
//
//	① **「用户」那一列对着一批行永远是空的**
//	   `sys_operation_log.username` 不是每条都写:
//	     - 中间件那条路径写(见 middleware/audit.go)
//	     - service 层直写的那几条(switch-identity 等)**直写时没有 caller
//	       上下文**,不写
//	     - 历史上这一列压根不存在
//	   ⇒ 审计中心那一列对着一批行永远是空的。
//
//	② **失败的身份切换被记成成功**
//	   `RecordRoleSwitch` 写的兜底操作日志 `status` **写死 1**,
//	   而 handler 在切换失败时把 ToRole 写成 "FAIL:<原因>"。
//	   ⇒ 失败的切换记成成功 —— 而失败的切换恰恰是审计最要看的那批
//	   (有人想切高敏身份但没切成)。
//
// # 回填为什么在**读侧**
//
//	① 历史行已经落库了,写侧补不回来;
//	② **用户可能改名**。回填用的是**当前**的用户名,而行里自己存的那份
//	   (写的时候拍的)才是**当时**的名字 —— 所以优先级是"行里的优先,
//	   为空才回填"。审计要回答的是"这条记录对应谁",不是"他现在叫什么"。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	拆法                                                必须变红
//	──────────────────────────────────────────────  ─────────────────────────────
//	去掉 ListOperationLogs 的 LEFT JOIN                「空 username 的行被回填」
//	把 COALESCE 换成只取 sys_user.username             「行里自带的用户名不被覆盖」
//	去掉 RecordRoleSwitch 的 Username 赋值              「身份切换也带用户名」
//	把 status 恒 1 改回去                             「失败的切换记成失败」
//	去掉 keyword 那行的括号                             「关键字不越权」
package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// auditJoinDB 建一个带 sys_user 的内存库(LEFT JOIN 的目标表必须有)
func auditJoinDB(t *testing.T) *gorm.DB {
	t.Helper()
	// ⚠️ 用 glebarez/sqlite(纯 Go,不需要 cgo)—— 与仓库其余测试一致
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysOperationLog{},
		&model.SysLoginLog{},
		&model.SysUser{},
		&model.RoleSwitchAudit{},
	))
	return db
}

// mkUser 造一个用户
func mkUser(t *testing.T, db *gorm.DB, id uint64, username string) {
	t.Helper()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel:    model.BaseModel{ID: id},
		Username:     username,
		PasswordHash: "x",
		Nickname:     username,
	}).Error)
}

// opRows 取操作日志第一页的行
//
// ⚠️ `model.PageResponse.List` 是 `interface{}`(它要同时装多种列表),所以得整体断言成 `[]model.SysOperationLog` —— 逐元素断言是错的。
func opRows(t *testing.T, svc AuditService, q OperationLogQuery) []model.SysOperationLog {
	t.Helper()
	page, err := svc.ListOperationLogs(context.Background(), q)
	require.NoError(t, err)
	rows, ok := page.List.([]model.SysOperationLog)
	require.True(t, ok, "列表类型不对:%T", page.List)
	return rows
}

// TestAuditLogUsernameBackfill username 的读侧回填
func TestAuditLogUsernameBackfill(t *testing.T) {
	db := auditJoinDB(t)
	svc := NewAuditService(db, nil)

	// 用户 100 现在叫 "alice";用户 200 现在叫 "bob"
	mkUser(t, db, 100, "alice")
	mkUser(t, db, 200, "bob")
	// 用户 300 已不存在(硬删)—— 他的日志行不该把查询搞崩

	// 行 A:自带 username "alice_old" —— **当时**的名字,不能被覆盖
	rowA := model.SysOperationLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next(), CreatedAt: time.Now()},
		UserID:    100, Username: "alice_old", Module: "user", Action: "update", Status: 1,
	}
	// 行 B:username 空 —— 该从 sys_user 回填成 "alice"
	rowB := model.SysOperationLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next(), CreatedAt: time.Now().Add(time.Second)},
		UserID:    100, Username: "", Module: "auth", Action: "switch-identity", Status: 1,
	}
	// 行 C:username 空 + 用户已不存在 —— 保持空,不能报错
	rowC := model.SysOperationLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next(), CreatedAt: time.Now().Add(2 * time.Second)},
		UserID:    300, Username: "", Module: "auth", Action: "switch-identity", Status: 1,
	}
	for _, r := range []model.SysOperationLog{rowA, rowB, rowC} {
		require.NoError(t, db.Create(&r).Error)
	}

	t.Run("行里自带的用户名不被 sys_user 覆盖", func(t *testing.T) {
		rows := opRows(t, svc, OperationLogQuery{UserID: 100})
		require.Len(t, rows, 2)
		byAction := map[string]model.SysOperationLog{}
		for _, r := range rows {
			byAction[r.Action] = r
		}
		require.Equal(t, "alice_old", byAction["update"].Username,
			"行里存的是**当时**的名字,不能被当前名字覆盖")
	})

	t.Run("行里为空的从 sys_user 回填", func(t *testing.T) {
		rows := opRows(t, svc, OperationLogQuery{UserID: 100})
		for _, r := range rows {
			if r.Action == "switch-identity" {
				require.Equal(t, "alice", r.Username, "空 username 必须被回填")
			}
		}
	})

	t.Run("用户已不存在时保持空且不报错", func(t *testing.T) {
		rows := opRows(t, svc, OperationLogQuery{UserID: 300})
		require.Len(t, rows, 1)
		require.Equal(t, "", rows[0].Username)
	})

	t.Run("关键字能命中回填出来的名字", func(t *testing.T) {
		// 行 B 自己没有 "alice",只有回填后才有 —— 搜得到才说明回填真的进了 WHERE
		rows := opRows(t, svc, OperationLogQuery{Keyword: "alice"})
		found := false
		for _, r := range rows {
			if r.Action == "switch-identity" {
				found = true
				require.Equal(t, "alice", r.Username)
			}
		}
		require.True(t, found, "按回填出来的用户名搜索,必须能命中那条行")
	})

	t.Run("关键字不越权(OR 组不能漏出 AND 组)", func(t *testing.T) {
		// 用户 200 有一行 module="bob-only"(只有他命中关键字)
		require.NoError(t, db.Create(&model.SysOperationLog{
			BaseModel: model.BaseModel{ID: sonyflake.Next(), CreatedAt: time.Now()},
			UserID:    200, Username: "bob", Module: "bob-only", Action: "update", Status: 1,
		}).Error)
		// 非特权用户(user_id=100)搜 "bob" —— 必须**一条都搜不到**
		rows := opRows(t, svc, OperationLogQuery{UserID: 100, Keyword: "bob"})
		require.Empty(t, rows,
			"user_id=100 的查询里混进了别人的行 —— OR 组漏出了 AND 组,"+
				"非特权用户搜关键字就能看到别人的日志")
	})
}

// TestAuditLoginLogUsernameBackfill 登录日志的回填
func TestAuditLoginLogUsernameBackfill(t *testing.T) {
	db := auditJoinDB(t)
	svc := NewAuditService(db, nil)
	ctx := context.Background()

	mkUser(t, db, 100, "alice")
	require.NoError(t, db.Create(&model.SysLoginLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next(), CreatedAt: time.Now()},
		UserID:    100, Username: "", Status: 1, Message: "ok",
		LoginAt: model.Time{Time: time.Now()},
	}).Error)

	page, err := svc.ListLoginLogs(ctx, LoginLogQuery{})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	rows, ok := page.List.([]model.SysLoginLog)
	require.True(t, ok, "列表类型不对:%T", page.List)
	require.Len(t, rows, 1)
	require.Equal(t, "alice", rows[0].Username, "登录日志的空 username 也要回填")

	// 关键字也要能命中
	page, err = svc.ListLoginLogs(ctx, LoginLogQuery{Keyword: "alice"})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
}

// TestRecordRoleSwitchFillsFields 身份切换的兜底操作日志字段
func TestRecordRoleSwitchFillsFields(t *testing.T) {
	db := auditJoinDB(t)
	svc := NewAuditService(db, nil)
	ctx := context.Background()

	mkUser(t, db, 100, "alice")

	// --- 成功 ---
	require.NoError(t, svc.RecordRoleSwitch(ctx, &model.RoleSwitchAudit{
		UserID:   100,
		FromRole: "role_owner",
		ToRole:   "role_supplier",
		IP:       "10.0.0.9",
		UA:       "UA/1.0",
		Reason:   "开工",
	}))

	var rows []model.SysOperationLog
	require.NoError(t, db.Where("action = ?", "switch-identity").Find(&rows).Error)
	require.Len(t, rows, 1)
	got := rows[0]
	require.Equal(t, "alice", got.Username, "身份切换的操作日志也要带用户名")
	require.Equal(t, "role_owner", got.UserRole, "user_role 记的是切换**前**的角色")
	require.Equal(t, 1, got.Status)
	require.Equal(t, "10.0.0.9", got.IP)

	// --- 失败 ---
	require.NoError(t, svc.RecordRoleSwitch(ctx, &model.RoleSwitchAudit{
		UserID:   100,
		FromRole: "role_owner",
		ToRole:   model.RoleSwitchFailPrefix + "验证码错误",
		Reason:   "切高敏身份",
	}))

	rows = nil
	require.NoError(t, db.Where("action = ?", "switch-identity").Order("id DESC").Find(&rows).Error)
	require.Len(t, rows, 2)
	failed := rows[0]
	require.Equal(t, 0, failed.Status, "失败的切换必须记成失败(不是成功)")
	require.Equal(t, "验证码错误", failed.ErrorMsg)
	require.Equal(t, "alice", failed.Username)
}
