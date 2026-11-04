// C6 · 阿里云 sender 的凭证热替换(2026-10-29)
//
// # 这一组守的是什么
//
//	面板上换 AccessKey 之后,下一次发送必须用新密钥 —— 而且**不用重启**。
//	实现是"凭证指纹(AK|SK|Region)变了就重建 client",这条网守三件事:
//
//  1. 指纹没变 ⇒ 复用同一个 client(不重复握手)
//  2. 指纹变了 ⇒ 重建 client
//  3. 凭证/签名/模板没配全 ⇒ **在发请求之前**就报错(而不是把空模板号
//     发给阿里云,换回一句含糊的 isv.SMS_TEMPLATE_ILLEGAL)
//
// 全部离线:client 的构造不联网,真正发短信之前就返回了。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 clientFor 里的指纹比较去掉(永远复用第一个 client)
//	  → TestC6_AliyunSender_ClientRebuiltOnCredentialChange 变红
//	· 把 SendWith 里的 validate(cfg) 去掉
//	  → TestC6_AliyunSender_SendWithRejectsIncompleteConfig 变红
//	· 把"模板号为空"的检查去掉
//	  → TestC6_AliyunSender_EmptyTemplateRejectedBeforeRequest 变红
package aliyun

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/config"
)

// testCfg 一份"齐活"的配置(凭证是假的,构造 client 不联网)
func testCfg() config.SMSConfig {
	return config.SMSConfig{
		Provider:     "aliyun",
		AccessKey:    "AK-test",
		AccessSecret: "SK-test",
		SignName:     "签名-test",
		Region:       "cn-hangzhou",
		Templates: config.SMSTemplate{
			Login:         "TPL-test",
			ViewFullPhone: "TPL-test",
			ResetPassword: "TPL-test",
			ChangePhone:   "TPL-test",
		},
	}
}

// TestC6_AliyunSender_ClientRebuiltOnCredentialChange 指纹变了才重建 client
func TestC6_AliyunSender_ClientRebuiltOnCredentialChange(t *testing.T) {
	s, err := New(testCfg())
	require.NoError(t, err)

	c1, err := s.clientFor(testCfg())
	require.NoError(t, err)
	require.NotNil(t, c1)

	c2, err := s.clientFor(testCfg())
	require.NoError(t, err)
	assert.Same(t, c1, c2, "配置没变 ⇒ 复用同一个 client")

	// 面板上换了 AccessKey
	changed := testCfg()
	changed.AccessKey = "AK-rotated"
	c3, err := s.clientFor(changed)
	require.NoError(t, err)
	assert.NotSame(t, c1, c3, "凭证变了 ⇒ 必须重建 client")

	assert.Equal(t, credFingerprint(changed), s.credFP, "指纹要跟着更新")
}

// TestC6_AliyunSender_SendWithRejectsIncompleteConfig 凭证/签名没配全 ⇒ 发请求前就报错
func TestC6_AliyunSender_SendWithRejectsIncompleteConfig(t *testing.T) {
	s, err := New(testCfg())
	require.NoError(t, err)

	noKey := testCfg()
	noKey.AccessKey = ""
	err = s.SendWith(context.Background(), noKey, "13800000000", "123456", "login")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_key")

	noSign := testCfg()
	noSign.SignName = ""
	err = s.SendWith(context.Background(), noSign, "13800000000", "123456", "login")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sign_name")
}

// TestC6_AliyunSender_EmptyTemplateRejectedBeforeRequest 模板号为空 ⇒ 明确报错
func TestC6_AliyunSender_EmptyTemplateRejectedBeforeRequest(t *testing.T) {
	s, err := New(testCfg())
	require.NoError(t, err)

	cfg := testCfg()
	cfg.Templates.Login = ""
	err = s.SendWith(context.Background(), cfg, "13800000000", "123456", "login")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "模板号未配置"),
		"要明确说出是哪个场景没配模板,实际:%v", err)
}

// TestC6_AliyunSender_UnknownSceneRejected 未知场景直接拒绝
func TestC6_AliyunSender_UnknownSceneRejected(t *testing.T) {
	s, err := New(testCfg())
	require.NoError(t, err)

	err = s.SendWith(context.Background(), testCfg(), "13800000000", "123456", "not_a_scene")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知 scene")
}
