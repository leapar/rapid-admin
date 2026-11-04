// M115 · 发送前的模板守卫接线(2026-10-30)
//
// # 这一组守的是什么
//
// 模板在阿里云那边没过审时,阿里云会拒发,平台**只能在失败之后**知道:
// 服务端日志只有一句 isv.SMS_TEMPLATE_ILLEGAL,用户看到的是"发送失败"。
//
// 守卫把这件事提前:发送之前先问登记册"这张模板能不能用",
// 确定不能用就**当场说清楚是哪张模板、为什么**。
//
// 这一组守的是**接线本身**(判定规则在 pkg/smstpl 里测):
//
//  1. **拦下时后端一次都不能被调用** —— 否则"守卫"只是个日志装饰器;
//  2. **warn / off 档位不拦** —— 策略是运维的兜底,平台不能自己改;
//  3. **模板号必须取自"这次解析出来的配置"** ——
//     取自打底配置就会出现"按 A 号的审核状态放行、实际发的是 B 号";
//  4. **登记册里的号 ≠ 这次要发的号 ⇒ 放行** —— 守卫第 3 条口径。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 Send 里的 `d.checkTemplate(cfg, scene)` 那一行删掉
//	  → TestM115_Guard_BlocksSendWhenTemplateRejected 变红
//	· 把模板号从 `smstpl.TemplateCode(cfg, scene)` 改成打底配置那一份
//	  (调用点在 Send 里)
//	  → TestM115_Guard_UsesResolvedTemplateCode 变红
//	· 把 checkTemplate 里的 `if dec.Blocked` 改成 `if false`
//	  → TestM115_Guard_BlocksSendWhenTemplateRejected 变红
//	· 把 checkTemplate 挪到 provider 分支之后(只拦 aliyun)
//	  → TestM115_Guard_BlocksConsoleProviderToo 变红
package msn

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/smstpl"
)

// loginTplCode 登记册与生效配置里**同一个**模板号
//
// ⚠️ 两者必须一致 —— 守卫第 3 条口径是"登记册的号 ≠ 这次要发的号 ⇒ 放行",
//
//	登记册里登记 "TPL-1" 而配置里是 "TPL-file" 的话,守卫会放行,
//	拦下用的用例就全成假绿了。
const loginTplCode = "TPL-file"

// guardCall 守卫被问到的 (场景, 模板号)
type guardCall struct {
	scene string
	code  string
}

// recordingGuard 记录调用参数的假守卫
type recordingGuard struct {
	calls []guardCall
	dec   smstpl.Decision
}

func (g *recordingGuard) Evaluate(scene, code string) smstpl.Decision {
	g.calls = append(g.calls, guardCall{scene: scene, code: code})
	return g.dec
}

// rejectedRegistry 造一本"登录模板被服务商驳回"的登记册
func rejectedRegistry(t *testing.T, scene, code string) *smstpl.Registry {
	t.Helper()
	reg := smstpl.NewRegistry()
	_, _, err := reg.Register(scene, code)
	require.NoError(t, err)
	// 服务商来源才能判"驳回"(人工只能停用 / 标待审核)
	_, _, err = reg.Apply(scene, smstpl.StatusRejected, smstpl.SourceProvider, "内容违规", "")
	require.NoError(t, err)
	return reg
}

// staticResolver 一个"什么都不改"的解析器(生效配置 == 打底配置)
func staticResolver(_ context.Context, base config.SMSConfig) (config.SMSConfig, error) {
	return base, nil
}

// TestM115_Guard_BlocksSendWhenTemplateRejected 模板被驳回 ⇒ 拦下,后端一次都不调
func TestM115_Guard_BlocksSendWhenTemplateRejected(t *testing.T) {
	reg := rejectedRegistry(t, "login", loginTplCode)
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

	ali := &fakeAliyun{}
	con := &fakeConsole{}
	s := NewDynamicSender(baseCfg(), staticResolver, ali, con, guard)

	err := s.Send(context.Background(), "13800000001", "123456", "login")
	require.Error(t, err, "模板确定发不出去,必须拦下")
	assert.ErrorIs(t, err, smstpl.ErrTemplateRejected, "错误要能让人看出是哪一类问题")
	assert.Contains(t, err.Error(), "内容违规", "错误里要带服务商给的驳回理由")
	assert.Empty(t, ali.calls, "拦下时不能调阿里云")
	assert.Empty(t, con.calls, "拦下时不能调 console")
}

// TestM115_Guard_BlocksConsoleProviderToo console 通道同样拦
//
// ⚠️ **这是有意选的统一口径** —— 登记册是按**场景**登记的,
//
//	"人工停用"是一个平台级开关,不是"某个 provider 的开关"。
//	如果只拦 aliyun,运维把 provider 切成 console 就绕过了停用开关。
func TestM115_Guard_BlocksConsoleProviderToo(t *testing.T) {
	reg := rejectedRegistry(t, "login", loginTplCode)
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

	con := &fakeConsole{}
	cfg := baseCfg()
	cfg.Provider = "console"
	s := NewDynamicSender(cfg, staticResolver, &fakeAliyun{}, con, guard)

	err := s.Send(context.Background(), "13800000001", "123456", "login")
	require.Error(t, err)
	assert.Empty(t, con.calls, "console 通道也要拦 —— 否则停用开关能被 provider 绕过")
}

// TestM115_Guard_WarnPolicyDoesNotBlock warn 档位只告警,不拦
func TestM115_Guard_WarnPolicyDoesNotBlock(t *testing.T) {
	reg := rejectedRegistry(t, "login", loginTplCode)
	guard := smstpl.NewGuard(reg, smstpl.PolicyWarn)

	ali := &fakeAliyun{}
	s := NewDynamicSender(baseCfg(), staticResolver, ali, &fakeConsole{}, guard)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"),
		"warn 档位不能拦 —— 那是运维留的兜底")
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls, "warn 档位下短信照发")
}

// TestM115_Guard_OffPolicySkipsCheck off 档位完全不检查
func TestM115_Guard_OffPolicySkipsCheck(t *testing.T) {
	reg := rejectedRegistry(t, "login", loginTplCode)
	guard := smstpl.NewGuard(reg, smstpl.PolicyOff)

	ali := &fakeAliyun{}
	s := NewDynamicSender(baseCfg(), staticResolver, ali, &fakeConsole{}, guard)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"))
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls)
}

// TestM115_Guard_UsesResolvedTemplateCode 模板号取自"这次解析出来的配置"
//
// ⚠️ **这条是最容易写错的一条** ——
//
//	拿打底配置(base)里的模板号去问守卫,会出现:
//	配置中心已经把模板换成 SMS_2,登记册里还是 SMS_1(且被驳回),
//	守卫拿 SMS_1 的结论去担保 SMS_2 —— 或者反过来。
func TestM115_Guard_UsesResolvedTemplateCode(t *testing.T) {
	base := baseCfg() // 打底:Login = TPL-file
	g := &recordingGuard{}
	resolve := func(_ context.Context, b config.SMSConfig) (config.SMSConfig, error) {
		out := b
		out.Templates.Login = "TPL-panel" // 配置中心换号了
		return out, nil
	}
	s := NewDynamicSender(base, resolve, &fakeAliyun{}, &fakeConsole{}, g)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"))
	require.Len(t, g.calls, 1, "守卫必须被问一次")
	assert.Equal(t, "login", g.calls[0].scene)
	assert.Equal(t, "TPL-panel", g.calls[0].code, "必须用解析出来的模板号,不是打底那份")
}

// TestM115_Guard_SceneWithoutTemplateCodeStillConsulted 没配模板号也要问守卫
//
// ⚠️ 没配模板号 ⇒ 模板号传空串,守卫**只按场景**判登记册里的状态。
//
//	不问的话,"人工停用"就漏了"没配模板号"的场景。
func TestM115_Guard_SceneWithoutTemplateCodeStillConsulted(t *testing.T) {
	g := &recordingGuard{}
	cfg := baseCfg()
	cfg.Templates.Login = "" // 没配模板号
	s := NewDynamicSender(cfg, staticResolver, &fakeAliyun{}, &fakeConsole{}, g)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"))
	require.Len(t, g.calls, 1)
	assert.Equal(t, "", g.calls[0].code, "没配模板号时传空串(守卫据此只按场景判)")
}

// TestM115_Guard_CodeMismatchPassesThrough 登记册的号 ≠ 这次要发的号 ⇒ 放行
//
// 这是守卫第 3 条口径:登记册的结论只对它登记的那个号有效。
// 配置中心刚换了号、同步还没跑到 ⇒ 拿旧号的"已驳回"去拦新号 = 拦错对象。
func TestM115_Guard_CodeMismatchPassesThrough(t *testing.T) {
	reg := rejectedRegistry(t, "login", "TPL-OLD")
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

	cfg := baseCfg()
	cfg.Templates.Login = "TPL-NEW" // 配置中心换号了,登记册还没同步
	ali := &fakeAliyun{}
	s := NewDynamicSender(cfg, staticResolver, ali, &fakeConsole{}, guard)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"),
		"登记册的结论不能担保另一个号")
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls)
}

// TestM115_Guard_NilGuardStillSends 没装配守卫时不拦(显式表达"这条链路不检查")
func TestM115_Guard_NilGuardStillSends(t *testing.T) {
	ali := &fakeAliyun{}
	s := NewDynamicSender(baseCfg(), staticResolver, ali, &fakeConsole{}, nil)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"))
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls)
}

// TestM115_Guard_BlockedSendCarriesReason 拦下时错误里带得清"哪张模板、为什么"
func TestM115_Guard_BlockedSendCarriesReason(t *testing.T) {
	reg := rejectedRegistry(t, "login", loginTplCode)
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

	s := NewDynamicSender(baseCfg(), staticResolver, &fakeAliyun{}, &fakeConsole{}, guard)
	err := s.Send(context.Background(), "13800000001", "123456", "login")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login", "错误里要带场景")
	assert.Contains(t, err.Error(), "内容违规", "错误里要带服务商给的驳回理由")
}

// TestM115_Guard_DisabledTemplateBlocks 人工停用同样拦
//
// ⚠️ 人工停用走的是 manual 来源(不是 provider),状态机允许
//
//	unknown → disabled,所以这里能造出来。
func TestM115_Guard_DisabledTemplateBlocks(t *testing.T) {
	reg := smstpl.NewRegistry()
	_, _, err := reg.Register("login", loginTplCode)
	require.NoError(t, err)
	_, _, err = reg.Apply("login", smstpl.StatusDisabled, smstpl.SourceManual, "先停掉", "admin")
	require.NoError(t, err)

	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)
	ali := &fakeAliyun{}
	s := NewDynamicSender(baseCfg(), staticResolver, ali, &fakeConsole{}, guard)

	err = s.Send(context.Background(), "13800000001", "123456", "login")
	require.Error(t, err)
	assert.ErrorIs(t, err, smstpl.ErrTemplateDisabled)
	assert.Empty(t, ali.calls)
}

// TestM115_Guard_UnregisteredScenePasses 场景没登记 ⇒ 放行
//
// ⚠️ "没登记 = 不知道 = 放行" —— 同步任务一挂,状态就停在 pending,
//
//	此时拦掉 = 平台自己把验证码通道锁死,而且没人知道为什么。
func TestM115_Guard_UnregisteredScenePasses(t *testing.T) {
	reg := smstpl.NewRegistry() // 空登记册
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

	ali := &fakeAliyun{}
	s := NewDynamicSender(baseCfg(), staticResolver, ali, &fakeConsole{}, guard)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "login"))
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls)
}

// TestM115_Guard_UnknownScenePasses 不认识的场景也放行
func TestM115_Guard_UnknownScenePasses(t *testing.T) {
	reg := rejectedRegistry(t, "login", loginTplCode)
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

	ali := &fakeAliyun{}
	s := NewDynamicSender(baseCfg(), staticResolver, ali, &fakeConsole{}, guard)

	require.NoError(t, s.Send(context.Background(), "13800000001", "123456", "some_new_scene"))
	assert.Equal(t, []string{"aliyun:13800000001"}, ali.calls)
}
