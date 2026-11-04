// 审计中心三个列表接口的 **query 参数接线** 单测 —— 2026-10-30
//
// # 这一组守的是什么
//
//	审计中心此前有一个"看不见的洞":页面上的搜索框、成功/失败筛选、时间范围
//	**全部是摆设** —— 前端老老实实发了 `keyword` / `status` / `startAt`,
//	后端 service 层**早就实现了**这些条件,handler 却一个都没往 query struct 里塞。
//
//	表现最恶劣的地方在于它**不报错**:HTTP 200、结果不变、不弹任何 toast。
//	运营只会得出一个错误结论:"这个搜索好像不生效"。
//
//	另一个同源缺陷是 **IDOR 收敛写漏一半**:
//	旧代码 `if q.UserID > 0 && !特权 { q.UserID = caller.UserID }`
//	—— 只有"点名查别人"才收敛,"什么都不传"(最常见的路径)反而拿到**全平台**日志。
//
//	所以这一组断言两件事:
//	  1. 前端发的每个参数**真的**进了 query struct(含"0 不能当不筛"这条)
//	  2. 非特权调用方的 userId **无条件**收敛到自己
//
// # 可逆探针
//
//	· 把 buildXxxLogQuery 里的 Keyword 去掉 → 「关键字进了 query」变红
//	· 把 auditScopeUserID 改回 `if userID > 0 {...}` → 「不点名就全看」变红
//	· 把 queryOptionalInt 改成返回普通 int → 「status=0 只看失败」变红
package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/service"
)

// auditQ 把 map 变成 auditQuery
func auditQ(params map[string]string) auditQuery {
	return func(key string) string { return params[key] }
}

// 普通业务角色(非特权)
func normalCaller() *service.Caller {
	return &service.Caller{UserID: 42, Username: "bob", RoleCode: "role_ops"}
}

// 超管
func superCaller() *service.Caller {
	return &service.Caller{UserID: 1, Username: "root", RoleCode: "role_super_admin"}
}

func TestAuditQuery_Wiring(t *testing.T) {
	t.Run("操作日志:前端发的每个参数都进了 query", func(t *testing.T) {
		q := buildOperationLogQuery(auditQ(map[string]string{
			"keyword": "alice",
			"module":  "user",
			"action":  "create",
			"ip":      "10.0.0.1",
			"status":  "1",
			"startAt": "1700000000",
			"endAt":   "1700003600",
		}), normalCaller())

		require.Equal(t, "alice", q.Keyword)
		require.Equal(t, "user", q.Module)
		require.Equal(t, "create", q.Action)
		require.Equal(t, "10.0.0.1", q.IP)
		require.NotNil(t, q.Status)
		require.Equal(t, 1, *q.Status)
		require.NotNil(t, q.StartAt)
		require.Equal(t, int64(1700000000), *q.StartAt)
		require.NotNil(t, q.EndAt)
		require.Equal(t, int64(1700003600), *q.EndAt)
	})

	t.Run("操作日志:status=0 是「只看失败」,不是「不筛」", func(t *testing.T) {
		q := buildOperationLogQuery(auditQ(map[string]string{"status": "0"}), normalCaller())
		require.NotNil(t, q.Status, "status=0 必须是「有筛选」,nil 会被当成不筛")
		require.Equal(t, 0, *q.Status)

		// 不传 = 不筛
		q2 := buildOperationLogQuery(auditQ(map[string]string{}), normalCaller())
		require.Nil(t, q2.Status)
	})

	t.Run("操作日志:非法 status / 非法时间 → 退化成不筛(而不是整条查询失败)", func(t *testing.T) {
		q := buildOperationLogQuery(auditQ(map[string]string{
			"status":  "abc",
			"startAt": "not-a-ts",
			"endAt":   "",
		}), normalCaller())
		require.Nil(t, q.Status)
		require.Nil(t, q.StartAt)
		require.Nil(t, q.EndAt)
	})

	t.Run("数据变更:表名 / 记录 id / 操作类型", func(t *testing.T) {
		q := buildDataChangeLogQuery(auditQ(map[string]string{
			"tableName": "sys_user",
			"recordId":  "12345",
			"keyword":   "sys_user",
			"operation": "2",
			"startAt":   "1700000000",
		}), normalCaller())

		require.Equal(t, "sys_user", q.TableName)
		require.Equal(t, uint64(12345), q.RecordID)
		require.Equal(t, "sys_user", q.Keyword)
		require.NotNil(t, q.Operation)
		require.Equal(t, 2, *q.Operation)
		require.NotNil(t, q.StartAt)
	})

	t.Run("登录日志:success 三态(true / false / 不传)", func(t *testing.T) {
		yes := buildLoginLogQuery(auditQ(map[string]string{"success": "true"}), normalCaller())
		require.NotNil(t, yes.Success)
		require.True(t, *yes.Success)

		no := buildLoginLogQuery(auditQ(map[string]string{"success": "false"}), normalCaller())
		require.NotNil(t, no.Success)
		require.False(t, *no.Success)

		// "1"/"0" 也认(跟短信发送审计同一个口径)
		one := buildLoginLogQuery(auditQ(map[string]string{"success": "1"}), normalCaller())
		require.NotNil(t, one.Success)
		require.True(t, *one.Success)

		none := buildLoginLogQuery(auditQ(map[string]string{}), normalCaller())
		require.Nil(t, none.Success)
	})
}

func TestAuditScopeUserID(t *testing.T) {
	t.Run("非特权:点名查别人 → 被收敛回自己", func(t *testing.T) {
		q := buildOperationLogQuery(auditQ(map[string]string{"userId": "999"}), normalCaller())
		require.Equal(t, uint64(42), q.UserID)
	})

	t.Run("非特权:什么都不传 → 也是只看自己(不是全平台)", func(t *testing.T) {
		// ⚠️ 这条是旧实现漏掉的那一半:不传 userId 时旧代码**完全不筛**,
		//   于是普通角色拿到的是全平台日志。
		q := buildOperationLogQuery(auditQ(map[string]string{}), normalCaller())
		require.Equal(t, uint64(42), q.UserID)
		require.NotEqual(t, uint64(0), q.UserID)
	})

	t.Run("非特权:数据变更 / 登录日志同样收敛", func(t *testing.T) {
		dc := buildDataChangeLogQuery(auditQ(map[string]string{"userId": "999"}), normalCaller())
		require.Equal(t, uint64(42), dc.UserID)

		lg := buildLoginLogQuery(auditQ(map[string]string{}), normalCaller())
		require.Equal(t, uint64(42), lg.UserID)
	})

	t.Run("超管:userId 原样透传(可以查任何人)", func(t *testing.T) {
		q := buildOperationLogQuery(auditQ(map[string]string{"userId": "999"}), superCaller())
		require.Equal(t, uint64(999), q.UserID)

		// 超管不传 = 全平台
		all := buildOperationLogQuery(auditQ(map[string]string{}), superCaller())
		require.Equal(t, uint64(0), all.UserID)
	})
}
