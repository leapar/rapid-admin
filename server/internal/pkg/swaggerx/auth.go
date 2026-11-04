// swaggerx —— /swagger 端点的 BasicAuth 闸
//
// # 为什么要有这层闸
//
// /swagger/doc.json 里是**全量接口清单**:路径、方法、每个接口的 RBAC 权限码。
// 裸奔等于把攻击面画好送出去 —— 这是《框架层未完成项清单》§8.1 G1 记的那条欠账。
//
// 用 BasicAuth 而不是复用后台那套 JWT 会话,理由:
//   - 文档端点跟后台是两个受众 —— 给外部集成方看文档的人不一定有后台账号
//   - BasicAuth 是浏览器原生支持的(弹框输账号密码),不用另做登录页
//   - 不碰会话体系 ⇒ 不会和 token 吊销 / 身份切换那套逻辑互相干扰
//
// ⚠️ **比较必须用 subtle.ConstantTimeCompare** —— 普通字符串比较会在
// 第一个不同的字节上短路,理论上可被计时攻击逐字节爆破出密码。
package swaggerx

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// AuthConfig /swagger 的鉴权配置
type AuthConfig struct {
	// Enabled 是否挂鉴权闸(false ⇒ 全公开)
	Enabled bool
	// Username BasicAuth 用户名
	Username string
	// Password BasicAuth 密码
	Password string
}

// BasicAuth 返回 BasicAuth 鉴权中间件。
//
// Enabled=false 时返回 nil —— 调用方直接跳过挂载,不产生任何 handler。
func BasicAuth(cfg AuthConfig) app.HandlerFunc {
	if !cfg.Enabled {
		return nil
	}
	return func(ctx context.Context, c *app.RequestContext) {
		user, pass, ok := parseBasicAuth(string(c.GetHeader("Authorization")))
		if !ok || !credentialsMatch(cfg, user, pass) {
			abortUnauthorized(c)
			return
		}
		c.Next(ctx)
	}
}

// credentialsMatch 常量时间比对用户名与密码。
//
// ⚠️ 用户名和密码**都要比** —— 只比密码的话,用户名就成了可以随便猜的公开信息。
// 两个比较都用 && 短路不了信息泄露:ConstantTimeCompare 本身对同长度输入是常量的,
// 长度不同直接返 0(这也是标准做法,长度本身不是秘密)。
func credentialsMatch(cfg AuthConfig, user, pass string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(cfg.Username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(cfg.Password)) == 1
	return userOK && passOK
}

// parseBasicAuth 解析 "Basic base64(user:pass)" 头。
//
// ⚠️ 密码里带冒号是允许的 —— 统一按**第一个**冒号切:前面是用户名,其余全是密码。
// 按最后一个冒号切就反了("a:b:c" 会变成 用户 "a:b" / 密码 "c")。
func parseBasicAuth(header string) (username, password string, ok bool) {
	const prefix = "Basic "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	idx := strings.IndexByte(string(raw), ':')
	if idx < 0 {
		return "", "", false
	}
	return string(raw[:idx]), string(raw[idx+1:]), true
}

// abortUnauthorized 401 + WWW-Authenticate 头。
//
// ⚠️ **必须带 WWW-Authenticate** —— 浏览器靠它弹出账号密码输入框。
// 不带的话浏览器只会显示一个空的 401 页面,用户完全不知道要登录。
func abortUnauthorized(c *app.RequestContext) {
	c.Header("WWW-Authenticate", `Basic realm="API Docs", charset="UTF-8"`)
	c.SetStatusCode(consts.StatusUnauthorized)
	c.Response.Header.SetContentType("application/json; charset=utf-8")
	c.Response.SetBodyString(`{"code":401,"message":"unauthorized"}`)
	c.Abort()
}
