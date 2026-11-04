// /swagger 的 BasicAuth 闸(2026-11-01 开源通用化 · §8.1 G1)
//
// ## 这条网守的是什么
//
// /swagger/doc.json 是**全量接口清单**(路径 / 方法 / 每个接口的 RBAC 权限码)。
// 没有这层闸,开源就等于把攻击面画好送出去。
//
// 这条网守三件事:
//  1. 凭据正确 ⇒ 放行(不 Abort)
//  2. 凭据错 / 没有 / 格式坏 ⇒ 401 **且带 WWW-Authenticate**
//     (少了这个头,浏览器只显示一个空 401 页,用户根本不知道要登录)
//  3. 密码里带冒号 ⇒ 解析成"第一个冒号前是用户名,其余全是密码"
package swaggerx

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/assert"
)

func basicHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// driveAuth 跑一次鉴权中间件,返回跑完之后的 RequestContext
func driveAuth(t *testing.T, cfg AuthConfig, header string) *app.RequestContext {
	t.Helper()
	c := &app.RequestContext{}
	c.Request.Header.Set("Authorization", header)
	BasicAuth(cfg)(context.Background(), c)
	return c
}

func TestBasicAuth_凭据正确时放行(t *testing.T) {
	cfg := AuthConfig{Enabled: true, Username: "alice", Password: "s3cret"}
	c := driveAuth(t, cfg, basicHeader("alice", "s3cret"))

	assert.False(t, c.IsAborted(), "凭据正确却被拦下了")
	assert.NotEqual(t, 401, c.Response.StatusCode(), "凭据正确却返了 401")
}

func TestBasicAuth_凭据错误时401(t *testing.T) {
	cfg := AuthConfig{Enabled: true, Username: "alice", Password: "s3cret"}
	c := driveAuth(t, cfg, basicHeader("alice", "wrong"))

	assert.True(t, c.IsAborted(), "凭据错误却放行了")
	assert.Equal(t, 401, c.Response.StatusCode())
	// ⚠️ 必须带这个头,否则浏览器不弹登录框
	assert.Contains(t, c.Response.Header.Get("WWW-Authenticate"), "Basic")
}

func TestBasicAuth_用户名错也算错(t *testing.T) {
	cfg := AuthConfig{Enabled: true, Username: "alice", Password: "s3cret"}
	c := driveAuth(t, cfg, basicHeader("bob", "s3cret"))

	assert.True(t, c.IsAborted(), "用户名错了却放行了 —— 只比密码的话用户名就成了公开信息")
	assert.Equal(t, 401, c.Response.StatusCode())
}

func TestBasicAuth_没有Authorization头时401(t *testing.T) {
	cfg := AuthConfig{Enabled: true, Username: "alice", Password: "s3cret"}
	c := driveAuth(t, cfg, "")

	assert.True(t, c.IsAborted())
	assert.Equal(t, 401, c.Response.StatusCode())
}

func TestBasicAuth_头部格式坏时401(t *testing.T) {
	cfg := AuthConfig{Enabled: true, Username: "alice", Password: "s3cret"}
	cases := []string{
		"abc",                    // 没有 Basic 前缀
		"Basic !!!not-base64!!!", // base64 解不开
		"Basic " + base64.StdEncoding.EncodeToString([]byte("no-colon")), // 没有冒号
	}
	for _, h := range cases {
		c := driveAuth(t, cfg, h)
		assert.True(t, c.IsAborted(), "头部 %q 竟然放行了", h)
		assert.Equal(t, 401, c.Response.StatusCode(), "头部 %q 竟然不是 401", h)
	}
}

func TestBasicAuth_密码里带冒号(t *testing.T) {
	cfg := AuthConfig{Enabled: true, Username: "alice", Password: "pa:ss:word"}
	c := driveAuth(t, cfg, basicHeader("alice", "pa:ss:word"))

	assert.False(t, c.IsAborted(), "密码里的冒号被当成用户名/密码分隔符了")
}

func TestBasicAuth_没启用时不返回handler(t *testing.T) {
	// Enabled=false ⇒ 返回 nil,调用方直接不挂 —— 不会有"看起来挂了其实没鉴权"的中间件
	assert.Nil(t, BasicAuth(AuthConfig{Enabled: false}))
}
