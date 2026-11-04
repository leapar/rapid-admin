// OpenAPI 文档标题的派生规则(2026-11-01 开源通用化)
//
// ## 这条网守的是什么
//
// 文档标题以前是**代码里写死的项目名** —— 开源之后每个 clone 下来的人
// 第一眼看到的是别人的项目名,改名字要改 Go 源码重编译。
//
// 优先级:branding.app_title > branding.app_name > system.app_name(服务标识)
//
// ⚠️ 这条网同时守着"不许写死项目名"这件事 —— 它一旦被改回字面量,
// 下面的断言会立刻红(而不是等到别人 clone 下来才发现)。
package router

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"rapid-admin/internal/config"
)

func TestSwaggerDocTitle_派生顺序(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Server
		want string
	}{
		{
			"branding.app_title 优先",
			&config.Server{
				Branding: config.BrandingConfig{AppName: "Acme", AppTitle: "Acme Console"},
				System:   config.SystemConfig{AppName: "svc"},
			},
			"Acme Console",
		},
		{
			"没配 app_title → 用 app_name",
			&config.Server{
				Branding: config.BrandingConfig{AppName: "Acme"},
				System:   config.SystemConfig{AppName: "svc"},
			},
			"Acme",
		},
		{
			"branding 全空 → 落回服务标识(不能是空串)",
			&config.Server{System: config.SystemConfig{AppName: "svc"}},
			"svc",
		},
		{
			"连配置都没有 → 通用兜底",
			nil,
			"Backend API",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, swaggerDocTitle(tc.cfg))
		})
	}
}

// TestSwaggerDocTitle_不写死项目名 —— 防止有人把字面量塞回来
func TestSwaggerDocTitle_不写死项目名(t *testing.T) {
	// 标题必须来自配置(branding.app_name),不允许写死任何项目名
	got := swaggerDocTitle(&config.Server{
		Branding: config.BrandingConfig{AppName: "My Platform"},
	})
	assert.Equal(t, "My Platform", got)
	assert.NotContains(t, got, "Digital")
}
