// 公开配置接口 —— 品牌信息下发(2026-11-01 开源通用化)
//
// ## 这条网守的是什么
//
// 开源通用化要求"改个 yaml 就能换平台名"。品牌名必须在**登录之前**拿到
// (登录页标题就是它),而平台配置中心(sys_config)的读接口挂在 Auth() 下
// 且只绑 super_admin —— 未登录根本读不到。
// ⇒ 走 GET /rapid-admin/v1/public/config 这个白名单接口下发。
//
// 这条网守三件事:
// 1. 配了 branding → 响应里必须带 app_name / app_title(前端靠它换名字)
// 2. 没配 branding → 必须是**空串**而不是 null —— 前端 `appName || t(...)`
// 这种回退判断遇到 null 会走进"有值"分支,界面显示 "null"
// 3. 这个接口**只多返品牌名**,不能顺手把 sys_config 的配置值漏出去
// (密钥 / endpoint 一旦漏出去就是未授权信息泄露面)
package v1

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/config"
)

// publicConfigData 调一次 handler,把响应体里的 data 段解出来
func publicConfigData(t *testing.T) map[string]any {
	t.Helper()
	var c app.RequestContext
	h := NewConfigHandler(nil)
	h.PublicConfig(context.Background(), &c)

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(c.Response.Body(), &resp), "响应体不是合法 JSON: %s", string(c.Response.Body()))
	require.Equal(t, 0, resp.Code, "业务码不是 0: %s", string(c.Response.Body()))
	return resp.Data
}

func TestPublicConfig_Branding被下发(t *testing.T) {
	orig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = orig })

	config.AppConfig = &config.Server{
		Branding: config.BrandingConfig{
			AppName:  "Acme 交付平台",
			AppTitle: "Acme 交付平台 - 后台管理",
			Skin:     "gov",
		},
	}

	data := publicConfigData(t)
	assert.Equal(t, "Acme 交付平台", data["app_name"])
	assert.Equal(t, "Acme 交付平台 - 后台管理", data["app_title"])
	assert.Equal(t, "gov", data["skin"])
}

func TestPublicConfig_Branding没配时是空串而不是null(t *testing.T) {
	orig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = orig })

	config.AppConfig = &config.Server{}

	data := publicConfigData(t)
	// ⚠️ 必须是 ""(空串),不能是 nil —— 前端靠 `appName || t('...')` 回退到语言包,
	//   null 也会让 `||` 短路,但 `appName ?? t(...)` 之外的写法会显示 "null"
	assert.Equal(t, "", data["app_name"])
	assert.Equal(t, "", data["app_title"])
	assert.Equal(t, "", data["skin"])
}

func TestPublicConfig_只返白名单字段(t *testing.T) {
	orig := config.AppConfig
	t.Cleanup(func() { config.AppConfig = orig })

	config.AppConfig = &config.Server{
		Branding: config.BrandingConfig{AppName: "Acme"},
	}

	data := publicConfigData(t)
	// 白名单 = 结构体字段强制。这里把**实际会出现的字段**列全 ——
	// 多一个字段就说明"这里开始塞业务配置了",那正是这条接口不能做的事。
	for _, k := range []string{
		"captcha_login_enabled",
		"captcha_sms_send_enabled",
		"app_name",
		"app_title",
		"skin",
	} {
		assert.Contains(t, data, k)
	}
	assert.Len(t, data, 5, "公开配置接口多返了字段 —— 白名单被撑破了")
}
