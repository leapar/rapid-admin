// C6 · 运行时可切换的 SMS sender(2026-10-29)
//
// # 这一组守的是什么
//
//	平台配置中心的 aliyun.sms.* 组接上之后,"生效配置"是**每次发送时**解析出来的,
//	不是启动时定死的。这条网守三件事:
//
//  1. **provider 能运行时切换**(面板上把 aliyun 改成 console ⇒ 立刻只打日志)
//  2. **解析失败 ⇒ 放弃这次发送**,而不是静默用旧配置发出去
//     (旧配置可能已经过期 —— 换密钥换到一半失败时,拿旧密钥发出去的短信会被阿里云拒)
//  3. **解析出来的配置真的传到了阿里云后端**(面板上填的 AK/签名/模板确实被用上)
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 Send 里的 `d.resolveCfg(ctx)` 换成 `d.base`(不再每次解析)
//	  → TestC6_DynamicSender_ProviderSwitchedAtRuntime 变红
//	· 把 resolveCfg 的错误吞掉(返回 d.base, nil)
//	  → TestC6_DynamicSender_ResolverErrorAbortsSend 变红
//	· 把 `d.aliyun.SendWith(ctx, cfg, ...)` 换成 `d.aliyun.SendWith(ctx, d.base, ...)`
//	  → TestC6_DynamicSender_AliyunGetsResolvedConfig 变红
package msn

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/config"
)

// fakeAliyun 记录收到的调用与配置
type fakeAliyun struct {
	calls []string
	got   config.SMSConfig
}

func (f *fakeAliyun) SendWith(_ context.Context, cfg config.SMSConfig, phone, code, scene string) error {
	f.calls = append(f.calls, "aliyun:"+phone)
	f.got = cfg
	return nil
}

// fakeConsole 记录收到的调用
type fakeConsole struct{ calls []string }

func (f *fakeConsole) Send(_ context.Context, phone, code, scene string) error {
	f.calls = append(f.calls, "console:"+phone)
	return nil
}

func (f *fakeConsole) Provider() string { return "console" }

// baseCfg 打底配置
func baseCfg() config.SMSConfig {
	return config.SMSConfig{
		Provider:  "aliyun",
		AccessKey: "AK-file",
		SignName:  "签名-file",
		Templates: config.SMSTemplate{Login: "TPL-file"},
	}
}

// TestC6_DynamicSender_ProviderSwitchedAtRuntime provider 运行时切换
func TestC6_DynamicSender_ProviderSwitchedAtRuntime(t *testing.T) {
	ali := &fakeAliyun{}
	con := &fakeConsole{}
	provider := "aliyun"
	resolve := func(_ context.Context, base config.SMSConfig) (config.SMSConfig, error) {
		out := base
		out.Provider = provider
		return out, nil
	}
	s := NewDynamicSender(baseCfg(), resolve, ali, con, nil)
	ctx := context.Background()

	require.NoError(t, s.Send(ctx, "13800000001", "123456", "login"))
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls)
	assert.Empty(t, con.calls)

	// 面板上把 provider 切成 console ⇒ 下一条就走 console
	provider = "console"
	require.NoError(t, s.Send(ctx, "13800000002", "123456", "login"))
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls, "第二次不该再走 aliyun")
	assert.Equal(t, []string{"console:13800000002"}, con.calls)
}

// TestC6_DynamicSender_AliyunGetsResolvedConfig 解析出来的配置真的传下去了
func TestC6_DynamicSender_AliyunGetsResolvedConfig(t *testing.T) {
	ali := &fakeAliyun{}
	resolve := func(_ context.Context, base config.SMSConfig) (config.SMSConfig, error) {
		out := base
		out.AccessKey = "AK-from-panel"
		out.SignName = "签名-from-panel"
		out.Templates.Login = "TPL-from-panel"
		return out, nil
	}
	s := NewDynamicSender(baseCfg(), resolve, ali, &fakeConsole{}, nil)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"))
	assert.Equal(t, "AK-from-panel", ali.got.AccessKey)
	assert.Equal(t, "签名-from-panel", ali.got.SignName)
	assert.Equal(t, "TPL-from-panel", ali.got.Templates.Login)
}

// TestC6_DynamicSender_ResolverErrorAbortsSend 解析失败 ⇒ 放弃这次发送
func TestC6_DynamicSender_ResolverErrorAbortsSend(t *testing.T) {
	ali := &fakeAliyun{}
	con := &fakeConsole{}
	boom := errors.New("配置中心读不到")
	resolve := func(_ context.Context, _ config.SMSConfig) (config.SMSConfig, error) {
		return config.SMSConfig{}, boom
	}
	s := NewDynamicSender(baseCfg(), resolve, ali, con, nil)

	err := s.Send(context.Background(), "13800000001", "123456", "login")
	require.Error(t, err)
	assert.ErrorIs(t, err, boom, "解析失败要把错误原样抛出去")
	assert.Empty(t, ali.calls, "解析失败不该调用任何后端")
	assert.Empty(t, con.calls, "解析失败不该调用任何后端")
}

// TestC6_DynamicSender_UnknownProviderFallsBackToConsole 未知 provider 降级到 console
func TestC6_DynamicSender_UnknownProviderFallsBackToConsole(t *testing.T) {
	ali := &fakeAliyun{}
	con := &fakeConsole{}
	resolve := func(_ context.Context, base config.SMSConfig) (config.SMSConfig, error) {
		out := base
		out.Provider = "mock" // 非法值
		return out, nil
	}
	s := NewDynamicSender(baseCfg(), resolve, ali, con, nil)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"))
	assert.Equal(t, []string{"console:13800000001"}, con.calls)
	assert.Empty(t, ali.calls)
}

// TestC6_DynamicSender_ProviderReflectsLastResolution Provider() 反映最近一次解析结果
func TestC6_DynamicSender_ProviderReflectsLastResolution(t *testing.T) {
	provider := "aliyun"
	resolve := func(_ context.Context, base config.SMSConfig) (config.SMSConfig, error) {
		out := base
		out.Provider = provider
		return out, nil
	}
	s := NewDynamicSender(baseCfg(), resolve, &fakeAliyun{}, &fakeConsole{}, nil)
	ctx := context.Background()

	assert.Equal(t, "unknown", s.Provider(), "还没发过时是 unknown")
	require.NoError(t, s.Send(ctx, "13800000001", "123456", "login"))
	assert.Equal(t, "aliyun", s.Provider())

	provider = "console"
	require.NoError(t, s.Send(ctx, "13800000002", "123456", "login"))
	assert.Equal(t, "console", s.Provider(), "审计行要写当次的 provider")
}
