// 配置审计的过滤条件(2026-10-29)
//
// # 这一组守的是什么
//
//	用户报了两句:
//	  「系统配置的审计日志里面 group 没有过滤效果。」
//
//	查下来是**三层叠在一起**的,任何一层单独修都不够:
//
//  1. **前端把 group code 塞进了 config_key** ——
//     配置页点"审计日志"跳 `/system/config/audit?configGroup=aliyun.sms`,
//     审计页把它当"config_key 前缀"发给后端。
//     而真实的 key 是 `aliyun.sms.access_key` —— **前缀不是等值**,
//     后端当时用的是 `config_key = ?` ⇒ 一个都匹配不上。
//  2. **参数名对不上** —— 前端发 `config_key`,后端 handler 读 `configKey`,
//     Hertz 的 `Query` 大小写敏感 ⇒ 读出来永远是空串 ⇒
//     **过滤条件从来没进过 SQL**,页面显示的是全量。
//  3. **没有"按组过滤"这个能力** —— 后端压根没有 group 这个查询维度。
//
// ⇒ 现在:group 走 `config_group`(按 sys_config.config_group 精确匹配),
// config_key 走子串匹配(搜索框),两个过滤可以同时存在(AND)。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 `config_group` 的子查询改回 `config_key = ?` → TestConfigCenterAuditFilters 变红
//	· 把 `INSTR(...)` 改回 `= ?` → 同上(子串匹配那条变红)
//	· 把子查询去掉 → group 过滤那条变红
package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// auditConfigKeys 查审计列表,返回命中的 config_key 集合
func auditConfigKeys(t *testing.T, svc ConfigCenterService, ctx context.Context, q ConfigAuditListQuery) []string {
	t.Helper()
	if q.Page == 0 {
		q.Page = 1
	}
	if q.Size == 0 {
		q.Size = 50
	}
	page, err := svc.ListConfigAudit(ctx, q)
	require.NoError(t, err)

	raw, err := json.Marshal(page.List)
	require.NoError(t, err)
	var items []map[string]any
	require.NoError(t, json.Unmarshal(raw, &items))

	out := make([]string, 0, len(items))
	for _, m := range items {
		k, _ := m["config_key"].(string)
		out = append(out, k)
	}
	return out
}

// TestConfigCenterAuditFilters 审计列表的两个过滤条件
//
//	两组配置 + 三条审计:
//	  aliyun.sms : aliyun.sms.access_key / aliyun.sms.sign_name
//	  aliyun.oss : aliyun.oss.cdn_domain
func TestConfigCenterAuditFilters(t *testing.T) {
	db := newConfigCenterTestDB(t)
	svc := NewConfigCenterService(db)
	ctx := context.Background()

	mkConfig := func(group, key string) {
		require.NoError(t, db.Create(&model.SysConfig{
			BaseModel:   model.BaseModel{ID: sonyflake.Next()},
			ConfigKey:   key,
			ConfigGroup: group,
			ConfigName:  key,
			ValueType:   model.ConfigTypeString,
			IsActive:    true,
		}).Error)
	}
	mkAudit := func(key string) {
		require.NoError(t, db.Create(&model.SysConfigAudit{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			ConfigKey: key,
			Action:    model.ConfigAuditActionUpdate,
		}).Error)
	}

	mkConfig("aliyun.sms", "aliyun.sms.access_key")
	mkConfig("aliyun.sms", "aliyun.sms.sign_name")
	mkConfig("aliyun.oss", "aliyun.oss.cdn_domain")

	mkAudit("aliyun.sms.access_key")
	mkAudit("aliyun.sms.sign_name")
	mkAudit("aliyun.oss.cdn_domain")

	// ---- 不过滤:全量 ----
	all := auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{})
	assert.Len(t, all, 3, "不过滤时三条都在")

	// ---- 按组过滤:这才是用户报的那条 ----
	sms := auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{ConfigGroup: "aliyun.sms"})
	assert.ElementsMatch(t, []string{"aliyun.sms.access_key", "aliyun.sms.sign_name"}, sms,
		"按组过滤必须只返回该组下的审计 —— 用户报的就是这条没生效")

	oss := auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{ConfigGroup: "aliyun.oss"})
	assert.Equal(t, []string{"aliyun.oss.cdn_domain"}, oss, "另一个组也要对")

	// 不存在的组 ⇒ 空
	assert.Empty(t, auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{ConfigGroup: "aliyun.ossx"}),
		"不存在的组不能把别的组捞进来")

	// ---- config_key 子串匹配(搜索框) ----
	sign := auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{ConfigKey: "sign_name"})
	assert.Equal(t, []string{"aliyun.sms.sign_name"}, sign, "关键字要能搜到片段")

	// "access_key" 里的 '_' 在 LIKE 里是单字符通配符 —— 用 INSTR 才是精确的子串
	exact := auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{ConfigKey: "access_key"})
	assert.Equal(t, []string{"aliyun.sms.access_key"}, exact,
		"下划线不能当通配符(accessXkey 那种不该命中)")

	// 全组前缀当关键字 ⇒ 三条都命中(子串)
	assert.Len(t, auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{ConfigKey: "aliyun"}), 3,
		"子串匹配:aliyun 命中全部三条")

	// ---- 两个过滤同时存在 ⇒ AND ----
	both := auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{
		ConfigGroup: "aliyun.sms",
		ConfigKey:   "access_key",
	})
	assert.Equal(t, []string{"aliyun.sms.access_key"}, both, "组 + 关键字是 AND")

	none := auditConfigKeys(t, svc, ctx, ConfigAuditListQuery{
		ConfigGroup: "aliyun.oss",
		ConfigKey:   "access_key",
	})
	assert.Empty(t, none, "组和关键字互斥时必须为空")
}
