// M113 · 配置中心「操作人 / 最后修改人」取名口径的回归网(2026-10-29)
//
// # 这一组守的是什么
//
//	用户 2026-10-29 问了一句很实在的话:
//	「基本信息里面的真实姓名,修改保存以后,它用在哪里?」
//
//	查下来的答案是:**有三个消费点**,而其中两个一直是空的。
//
//	 1. 审批流(`approval_query_impl.go`)—— 审批单上的操作人姓名。
//	    那边**早就有**三段 fallback:真实姓名 → 昵称 → 账号。
//	 2. 配置中心的「最后修改人」(`sys_config.last_changed_by_name`)
//	    和配置审计日志的「操作人」(`sys_config_audit.user_name`)
//	    —— 这两处以前是**裸的 `u.real_name`**,**没有 fallback**。
//	 3. 用户管理页的关键字搜索 + 详情抽屉的「真实姓名」一行。
//
//	为什么第 2 条一直是空的?因为后台账号的 real_name **默认就是空的**
//	(seed 只给 super_admin 填了"超级管理员"),而个人中心**压根不能填**
//	(M113 之前)⇒ 管理员看到的永远是空白,根本看不出是谁改的配置。
//
//	⇒ M113 把「能填」这件事修好了(个人中心能填 + 后端真存),
//	这条网守的是**「填了要能看见」**:真实姓名 → 昵称 → 账号,第一个非空的胜出。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 `operatorNameSQL` 改回 `u.real_name`
//	  → TestM113_ConfigCenter_OperatorNameFallback 变红
package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// newConfigCenterTestDB 配置中心相关的表
func newConfigCenterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysUser{},
		&model.SysConfig{},
		&model.SysConfigAudit{},
	))
	return db
}

// seedConfigOperator 造一个操作人(realName / nickname 可以为空)
func seedConfigOperator(t *testing.T, db *gorm.DB, username, nickname, realName string) uint64 {
	t.Helper()
	id := sonyflake.Next()
	require.NoError(t, db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: id},
		Username:  username,
		Nickname:  nickname,
		RealName:  realName,
		IsActive:  true,
	}).Error)
	return id
}

// auditUserNames 查审计列表,返回 "user_id → 操作人姓名" 的映射
func auditUserNames(t *testing.T, svc ConfigCenterService, ctx context.Context) map[uint64]string {
	t.Helper()
	page, err := svc.ListConfigAudit(ctx, ConfigAuditListQuery{
		PageRequest: model.PageRequest{Page: 1, Size: 50},
	})
	require.NoError(t, err)

	// List 是 interface{}(PageResponse 的通用分页),直接整体 marshal 成数组
	raw, err := json.Marshal(page.List)
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(raw, &items))

	out := map[uint64]string{}
	for _, m := range items {
		uid, ok := m["user_id"].(string)
		require.True(t, ok, "审计项缺 user_id")
		name, _ := m["user_name"].(string)
		out[parseUint(t, uid)] = name
	}
	return out
}

// parseUint 解析十进制字符串(线上 user_id 是 string)
func parseUint(t *testing.T, s string) uint64 {
	t.Helper()
	var v uint64
	for _, c := range s {
		require.True(t, c >= '0' && c <= '9', "user_id 不是数字:%q", s)
		v = v*10 + uint64(c-'0')
	}
	return v
}

// TestM113_ConfigCenter_OperatorNameFallback 操作人姓名的三段 fallback
//
// 这条锁的是「真实姓名为空时不能显示成空白」。
func TestM113_ConfigCenter_OperatorNameFallback(t *testing.T) {
	db := newConfigCenterTestDB(t)
	svc := NewConfigCenterService(db)
	ctx := context.Background()

	// 三个操作人:有真实姓名 / 只有昵称 / 只有账号
	withReal := seedConfigOperator(t, db, "u_real", "昵称A", "真实甲")
	withNick := seedConfigOperator(t, db, "u_nick", "昵称B", "")
	onlyUser := seedConfigOperator(t, db, "u_only", "", "")

	// 一条配置 + 三条审计
	cfg := &model.SysConfig{
		BaseModel:     model.BaseModel{ID: sonyflake.Next()},
		ConfigKey:     "m113.demo.key",
		ConfigGroup:   "demo",
		ConfigName:    "演示配置",
		ValueType:     model.ConfigTypeString,
		ValuePreview:  "v",
		LastChangedBy: withReal,
		IsActive:      true,
	}
	require.NoError(t, db.Create(cfg).Error)

	for _, uid := range []uint64{withReal, withNick, onlyUser} {
		require.NoError(t, db.Create(&model.SysConfigAudit{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			ConfigID:  cfg.ID,
			ConfigKey: cfg.ConfigKey,
			Action:    "update",
			UserID:    uid,
		}).Error)
	}

	// ---- 配置项的「最后修改人」----
	view, err := svc.GetConfigView(ctx, cfg.ID)
	require.NoError(t, err)
	assert.Equal(t, "真实甲", view.LastChangedByName,
		"有真实姓名时用真实姓名")

	// ---- 审计日志的「操作人」----
	names := auditUserNames(t, svc, ctx)
	assert.Equal(t, "真实甲", names[withReal], "有真实姓名 ⇒ 用真实姓名")
	assert.Equal(t, "昵称B", names[withNick], "没有真实姓名 ⇒ 回落昵称")
	assert.Equal(t, "u_only", names[onlyUser], "真实姓名和昵称都没有 ⇒ 回落账号")
}

// TestM113_ConfigCenter_OperatorNameNeverBlank 「操作人」永远不是空白
//
// 这条是上一条的兜底:任何一种用户数据组合下,操作人都不该是空串。
func TestM113_ConfigCenter_OperatorNameNeverBlank(t *testing.T) {
	db := newConfigCenterTestDB(t)
	svc := NewConfigCenterService(db)
	ctx := context.Background()

	uid := seedConfigOperator(t, db, "u_blank", "", "")
	require.NoError(t, db.Create(&model.SysConfigAudit{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		ConfigKey: "m113.blank.key",
		Action:    "update",
		UserID:    uid,
	}).Error)

	page, err := svc.ListConfigAudit(ctx, ConfigAuditListQuery{
		PageRequest: model.PageRequest{Page: 1, Size: 10},
	})
	require.NoError(t, err)
	require.NotEmpty(t, page.List)

	raw, err := json.Marshal(page.List)
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(raw, &items))
	require.Len(t, items, 1)
	assert.NotEmpty(t, items[0]["user_name"], "操作人不能是空白 —— 管理员要看得出是谁干的")
}
