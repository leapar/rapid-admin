// 品牌信息配置的派生规则(2026-11-01 开源通用化)
//
// ## 这条网守的是什么
//
// 品牌名有三个来源,谁优先必须**钉死** —— 否则会出现"改了 yaml 但界面没变"
// 或者"两个地方各显示一个名字"的分裂。
//
// 优先级:branding.app_title > branding.app_name > (前端回退到语言包)
//
// ⚠️ **不用 system.app_name 兜底**:那是服务标识(rapid-admin),
// 出现在浏览器标签页上会让使用者以为在访问内部服务而不是产品。
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBranding_TabTitle派生顺序(t *testing.T) {
	cases := []struct {
		name string
		in   BrandingConfig
		want string
	}{
		{"两个都配 → 用 app_title", BrandingConfig{AppName: "Acme", AppTitle: "Acme Console"}, "Acme Console"},
		{"只配 app_name → 用 app_name", BrandingConfig{AppName: "Acme"}, "Acme"},
		{"都没配 → 空串(前端回退语言包)", BrandingConfig{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.in.TabTitle())
		})
	}
}

func TestBranding_DisplayName(t *testing.T) {
	assert.Equal(t, "Acme", BrandingConfig{AppName: "Acme"}.DisplayName())
	assert.Equal(t, "", BrandingConfig{}.DisplayName())
}
