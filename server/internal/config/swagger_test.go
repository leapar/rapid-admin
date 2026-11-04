// /swagger 鉴权配置的解析规则(2026-11-01 开源通用化 · §8.1 G1)
//
// ## 这条网守的是什么
//
// 三种 mode 的语义必须钉死,否则会出现两种典型的"以为有鉴权其实没有":
//
//   - mode=basic 却没配凭据 ⇒ 必须在启动时炸掉,不能"降级成不鉴权"
//   - mode 留空(老配置的 yaml 里根本没这个块)⇒ **不能裸奔**
//     必须落到 auto —— 没配凭据就本次启动随机生成一个
//
// ⚠️ **幂等也是这条网的一部分** —— main.go 先调一次做 fail-fast,
// router.Register 再调一次拿结论。两次如果生成两个不同的随机密码,
// 日志里印的那个就不是生效的那个 ⇒ 文档打不开,而且极难查。
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwaggerConfig_ResolveAuth_三种模式(t *testing.T) {
	t.Run("auto 没配凭据 ⇒ 随机生成(不是裸奔)", func(t *testing.T) {
		var c SwaggerConfig
		require.NoError(t, c.ResolveAuth())

		assert.True(t, c.Auth.Enabled, "auto 模式必须带鉴权 —— 漏配不等于裸奔")
		assert.Equal(t, "swagger", c.Auth.Username)
		assert.NotEmpty(t, c.Auth.Password, "没配密码却没生成")
		assert.True(t, c.Auth.Generated, "随机生成的密码必须被标记出来(启动日志要靠它提示)")
	})

	t.Run("auto 配了凭据 ⇒ 用配置的", func(t *testing.T) {
		var c SwaggerConfig
		c.Auth.Username = "alice"
		c.Auth.Password = "s3cret"
		require.NoError(t, c.ResolveAuth())

		assert.True(t, c.Auth.Enabled)
		assert.Equal(t, "alice", c.Auth.Username)
		assert.Equal(t, "s3cret", c.Auth.Password)
		assert.False(t, c.Auth.Generated)
	})

	t.Run("basic 没配凭据 ⇒ 报错(拒绝启动)", func(t *testing.T) {
		var c SwaggerConfig
		c.Auth.Mode = "basic"
		err := c.ResolveAuth()
		require.Error(t, err, "mode=basic 却没凭据,必须报错而不是降级")
		assert.Contains(t, err.Error(), "username")
		assert.Contains(t, err.Error(), "password")
	})

	t.Run("basic 配了凭据 ⇒ 正常", func(t *testing.T) {
		var c SwaggerConfig
		c.Auth.Mode = "basic"
		c.Auth.Username = "alice"
		c.Auth.Password = "s3cret"
		require.NoError(t, c.ResolveAuth())
		assert.True(t, c.Auth.Enabled)
	})

	t.Run("none ⇒ 不鉴权(显式关)", func(t *testing.T) {
		var c SwaggerConfig
		c.Auth.Mode = "none"
		require.NoError(t, c.ResolveAuth())
		assert.False(t, c.Auth.Enabled)
	})

	t.Run("mode 写错 ⇒ 报错", func(t *testing.T) {
		var c SwaggerConfig
		c.Auth.Mode = "Basic-ish"
		require.Error(t, c.ResolveAuth())
	})
}

func TestSwaggerConfig_ResolveAuth_幂等(t *testing.T) {
	var c SwaggerConfig
	require.NoError(t, c.ResolveAuth())
	first := c.Auth.Password
	require.True(t, c.Auth.Generated)

	// 再调一次(main.go → router.Register 就是这个路径)
	require.NoError(t, c.ResolveAuth())
	assert.Equal(t, first, c.Auth.Password, "第二次解析生成了新密码 —— 日志里印的就不是生效的那个")
	assert.True(t, c.Auth.Generated)
}

func TestSwaggerConfig_ResolveAuth_mode大小写与空格(t *testing.T) {
	var c SwaggerConfig
	c.Auth.Mode = "  BASIC  "
	c.Auth.Username = "alice"
	c.Auth.Password = "s3cret"
	require.NoError(t, c.ResolveAuth())
	assert.True(t, c.Auth.Enabled)
}
