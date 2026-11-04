// Swagger 文档端点配置 —— OpenAPI 3.0 spec + Swagger UI(开源通用化新增)
//
// # 这个块管什么
//
// /swagger 这条文档端点有两个正交的开关:
//
//   - enabled:要不要挂这条端点(完全不想要 API 文档的部署可以直接关掉)
//   - auth.mode:挂了以后要不要鉴权
//
// ⚠️ **默认必须带鉴权** —— /swagger/doc.json 里是**全量接口清单**
// (路径 / 方法 / 每个接口的 RBAC 权限码),裸奔等于把攻击面画好送出去。
// 这正是《框架层未完成项清单》§8.1 G1 记的那条欠账。
//
// # 三种 mode
//
//	mode=auto(默认):配了固定凭据就用固定凭据;没配就**本次启动随机生成**
//	mode=basic     :必须有固定凭据,没有直接报错(拒绝启动)
//	mode=none      :不鉴权 —— 只允许本地开发用,启动会打 WARN
//
// 详见 ResolveAuth。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// SwaggerConfig swagger 文档端点配置
type SwaggerConfig struct {
	// Enabled 是否挂载 /swagger 端点(默认 true)
	Enabled bool `mapstructure:"enabled"`
	// Auth /swagger 的鉴权方式
	Auth SwaggerAuthConfig `mapstructure:"auth"`
}

// SwaggerAuthConfig /swagger 的 BasicAuth 凭据
//   - Mode:     "auto"(默认) / "basic" / "none"
//   - Username: BasicAuth 用户名(auto 模式下留空 ⇒ 用 "swagger")
//   - Password: BasicAuth 密码(auto 模式下留空 ⇒ 本次启动随机生成)
//
// ⚠️ 下面两个字段是 ResolveAuth 回填的**结论**,不从 yaml 读 ——
// yaml 里写它们没有意义(mapstructure:"-" 直接跳过)。
type SwaggerAuthConfig struct {
	Mode     string `mapstructure:"mode"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`

	// Enabled 解析后的结论 —— 是否真的挂鉴权
	Enabled bool `mapstructure:"-"`
	// Generated 密码是本次启动随机生成的(不是配置里的固定值)
	Generated bool `mapstructure:"-"`
}

// ResolveAuth 解析 /swagger 的鉴权配置,并把结论回填到 c.Auth。
//
// ⚠️ **auto 模式不会"没配就裸奔"** —— 这是本仓一贯的纪律:
// 漏配必须等于"照常跑(且是安全的那一档)",而不是"静默降级成不设防"。
// 代价是密码每次重启都变(开发机无所谓,生产请配固定凭据或环境变量
// SWAGGER_AUTH_USERNAME / SWAGGER_AUTH_PASSWORD)。
//
// ⚠️ **本函数是幂等的** —— 第一次调用会把随机密码回填进结构体,
// 第二次调用看到密码非空就不会再生成一个新的。
// main.go 先调它做 fail-fast,router.Register 再调一次拿结论,
// 两次不会打架(否则日志里印的密码跟真正生效的不是同一个)。
func (c *SwaggerConfig) ResolveAuth() error {
	switch strings.ToLower(strings.TrimSpace(c.Auth.Mode)) {
	case "", "auto":
		c.Auth.Enabled = true
		if c.Auth.Username == "" {
			c.Auth.Username = "swagger"
		}
		if c.Auth.Password == "" {
			c.Auth.Password = randomSwaggerPassword()
			c.Auth.Generated = true
		}
	case "basic":
		c.Auth.Enabled = true
		if c.Auth.Username == "" || c.Auth.Password == "" {
			return fmt.Errorf("swagger.auth.mode=basic 要求同时配置 username 和 password" +
				"(或环境变量 SWAGGER_AUTH_USERNAME / SWAGGER_AUTH_PASSWORD)")
		}
	case "none":
		c.Auth.Enabled = false
	default:
		return fmt.Errorf("swagger.auth.mode 取值非法:%q(只能是 auto / basic / none)", c.Auth.Mode)
	}
	return nil
}

// randomSwaggerPassword 生成 24 位十六进制随机密码。
//
// 只用于 /swagger 的临时 BasicAuth 凭据 —— 不是任何账号的密码,
// 不落库、不进配置文件,只打到启动日志里。
func randomSwaggerPassword() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败是不可恢复的环境问题,但这里只是生成个临时凭据,
		// 不值得为它拒绝启动。退化成时间戳派生值 —— 强度低,但仍不是常量。
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
