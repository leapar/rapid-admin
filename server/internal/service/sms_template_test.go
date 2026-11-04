// SmsTemplateService 单测 —— M115-4
//
// # 这张网守的是什么
//
// service 层是**人工动作的唯一入口**。状态机(pkg/smstpl/machine.go)挡住了
// "人工判通过/驳回",但 service 层还要挡住另外三件只有在这一层才挡得住的事:
//
//  1. **没配模板号的场景不许"停用"** —— 否则登记册里会多一条空号记录,
//     运营后来配上模板号的那一刻,Register 判成"换号" ⇒ 状态重置 ⇒
//     **刚点的停用凭空消失**。
//  2. **"恢复"只能从停用状态发起** —— 否则这个接口就是"把 approved 降级成
//     pending"的暗门(运营误点一次,刚通过的模板就掉回"待审核")。
//  3. **"恢复"是恢复到 pending,不是 approved** —— 人工无权判定通过。
//
// 另外两条口径:
//   - 停用必须落在**生效的那个模板号**上(换号后停用要停新号)。
//   - 流转历史里必须能看出**是哪个运维**停的。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· requireTemplate 里删掉"登记册里有号也算"那一段
//	  → 「登记册里有号也能停用」变红
//	· Enable 里删掉 `cur.Status != StatusDisabled` 那道检查
//	  → 「非停用状态不许恢复」变红
//	· Enable 里把 StatusPending 改成 StatusApproved
//	  → 「恢复只能是待审核」变红
//	· Disable 里把 trimReason(reason) 改成 reason(不截断)
//	  → 「超长原因被截断」变红
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/config"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/smstpl"
)

// ---- 夹具 ----

// newSmsTemplateTestDB 只建模板登记册那两张表
func newSmsTemplateTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SysSmsTemplate{}, &model.SysSmsTemplateLog{}))
	return db
}

// newSmsTemplateSvc 造一个 service(生效配置固定,持久化落内存 SQLite)
func newSmsTemplateSvc(t *testing.T, cfg config.SMSConfig) SmsTemplateService {
	t.Helper()
	db := newSmsTemplateTestDB(t)
	rt := smstpl.NewRuntime(smstpl.RuntimeOptions{
		Store: smstpl.NewSQLStore(db),
		Resolve: func(context.Context) (config.SMSConfig, error) {
			return cfg, nil
		},
	})
	return NewSmsTemplateService(rt)
}

// smsTplCfg 造一份只配了 login 模板号的生效配置
func smsTplCfg(code string) config.SMSConfig {
	return config.SMSConfig{
		Provider:  "aliyun",
		Templates: config.SMSTemplate{Login: code},
	}
}

func smsCaller(name string) *Caller {
	return &Caller{UserID: 1, Username: name, RoleCode: "super_admin"}
}

// errCodeOf 取 errcode 的业务码
func errCodeOf(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	ec, ok := err.(*errcode.Error)
	require.True(t, ok, "必须是 errcode.Error, got %T", err)
	return ec.Code
}

// ---- List ----

func TestSmsTemplate_List_AllScenesAlwaysPresent(t *testing.T) {
	svc := newSmsTemplateSvc(t, smsTplCfg("SMS_1"))

	view, err := svc.List(context.Background())
	require.NoError(t, err)
	require.Len(t, view.Items, len(smstpl.Scenes), "全部场景都要出现在列表里")
	assert.Equal(t, "aliyun", view.Provider)
	assert.Equal(t, string(smstpl.PolicyBlock), view.Policy)

	// 没配号的场景也要出现(配没配是 configured 字段的事,不是"不出现")
	// ⚠️ **mfa 场景复用登录模板号**(smstpl.SceneMFA 的设计决策:登录流程里
	// "您正在登录"的文案本来就对,单独报备一张对部署是纯负担)⇒
	// 只配了 login 时,mfa 是 configured 且模板号同 login —— 这是故意的,
	// 不是"串号";哪天 mfa 有了专属模板,这里要跟着改成独立配置的断言。
	for _, it := range view.Items {
		switch it.Scene {
		case smstpl.SceneLogin, smstpl.SceneMFA:
			assert.True(t, it.Configured, "scene=%s 应回落到登录模板", it.Scene)
			assert.Equal(t, "SMS_1", it.TemplateCode, "scene=%s 应复用登录模板号", it.Scene)
		default:
			assert.False(t, it.Configured)
			assert.Equal(t, "", it.TemplateCode)
		}
	}
}

// ---- Disable ----

func TestSmsTemplate_Disable_SetsDisabledAndRecordsOperator(t *testing.T) {
	svc := newSmsTemplateSvc(t, smsTplCfg("SMS_1"))

	v, err := svc.Disable(context.Background(), smsCaller("zhangsan"), smstpl.SceneLogin, "模板内容有问题")
	require.NoError(t, err)
	assert.Equal(t, string(smstpl.StatusDisabled), v.Status)
	assert.True(t, v.Blocked)
	assert.False(t, v.Usable)
	assert.Equal(t, "manual", v.Source)

	hist, err := svc.History(context.Background(), smstpl.SceneLogin, 50)
	require.NoError(t, err)
	require.NotEmpty(t, hist)
	assert.Equal(t, "zhangsan", hist[0].Operator, "历史里必须知道是哪个运维停的")
	assert.Equal(t, string(smstpl.StatusDisabled), hist[0].To)
}

// ⚠️ **没配模板号的场景不许停用** ——
//
// 登记册里多一条空号记录 ⇒ 运营后来配上模板号的那一刻,Register 判成"换号"
// ⇒ 状态重置 ⇒ **刚点的停用凭空消失**。
func TestSmsTemplate_Disable_WithoutTemplateCode_Rejected(t *testing.T) {
	svc := newSmsTemplateSvc(t, config.SMSConfig{Provider: "aliyun"})

	_, err := svc.Disable(context.Background(), smsCaller("zhangsan"), smstpl.SceneLogin, "停用")
	require.Error(t, err)
	assert.Equal(t, errcode.BadRequest, errCodeOf(t, err))
}

// 登记册里还留着上一个模板号(配置中心刚被清空)⇒ 依然可以停用
func TestSmsTemplate_Disable_AfterConfigCleared_StillAllowed(t *testing.T) {
	db := newSmsTemplateTestDB(t)
	var cfgHolder config.SMSConfig = smsTplCfg("SMS_1")
	rt := smstpl.NewRuntime(smstpl.RuntimeOptions{
		Store: smstpl.NewSQLStore(db),
		Resolve: func(context.Context) (config.SMSConfig, error) {
			return cfgHolder, nil
		},
	})
	svc := NewSmsTemplateService(rt)
	ctx := context.Background()

	// 先停一次(此时配置里有号)
	_, err := svc.Disable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "先停")
	require.NoError(t, err)

	// 配置中心把号清空了(运营以为"不配号 = 不用管了")
	cfgHolder = config.SMSConfig{Provider: "aliyun"}
	// 登记册里还留着号 ⇒ 依然可以停用,而且不报错
	_, err = svc.Disable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "再停一次")
	require.NoError(t, err)
}

func TestSmsTemplate_Disable_UnknownScene_Rejected(t *testing.T) {
	svc := newSmsTemplateSvc(t, smsTplCfg("SMS_1"))
	_, err := svc.Disable(context.Background(), smsCaller("zhangsan"), "not_a_scene", "x")
	require.Error(t, err)
	assert.Equal(t, errcode.BadRequest, errCodeOf(t, err))
}

// ---- Enable ----

// ⚠️ **恢复 = 待审核,不是已通过** —— 人工无权判定通过(硬规则 1)
func TestSmsTemplate_Enable_GoesToPendingNotApproved(t *testing.T) {
	svc := newSmsTemplateSvc(t, smsTplCfg("SMS_1"))
	ctx := context.Background()

	_, err := svc.Disable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "停用")
	require.NoError(t, err)

	v, err := svc.Enable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "恢复")
	require.NoError(t, err)
	assert.Equal(t, string(smstpl.StatusPending), v.Status,
		"恢复只能是待审核 —— 人工判不了通过")
	assert.False(t, v.Blocked)
}

// ⚠️ **"恢复"只能从停用状态发起** ——
//
// 否则这个接口就是"把 approved 降级成 pending"的暗门:
// 运营误点一次"恢复",刚通过的模板就掉回"待审核",页面上显示的状态已经不对了。
func TestSmsTemplate_Enable_FromNonDisabledState_Rejected(t *testing.T) {
	svc := newSmsTemplateSvc(t, smsTplCfg("SMS_1"))
	ctx := context.Background()

	// 没停用过 ⇒ 登记册里没有这一行
	_, err := svc.Enable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "恢复")
	require.Error(t, err)
	assert.Equal(t, errcode.NotFound, errCodeOf(t, err))

	// 停用 → 恢复 → 再恢复(第二次必须被拒)
	_, err = svc.Disable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "停用")
	require.NoError(t, err)
	_, err = svc.Enable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "恢复")
	require.NoError(t, err)
	_, err = svc.Enable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "再恢复")
	require.Error(t, err)
	assert.Equal(t, errcode.StateInvalid, errCodeOf(t, err))
}

// ---- 换号 ----

// ⚠️ **换号后点停用,要停的是新号** ——
//
// 停用旧号的话,守卫按"登记册的号 ≠ 这次要发的号 ⇒ 放行"处理,
// **运营点了停用却拦不住任何东西**。
func TestSmsTemplate_Disable_AfterTemplateCodeChanged_TargetsNewCode(t *testing.T) {
	db := newSmsTemplateTestDB(t)
	var cfgHolder config.SMSConfig = smsTplCfg("SMS_1")
	rt := smstpl.NewRuntime(smstpl.RuntimeOptions{
		Store: smstpl.NewSQLStore(db),
		Resolve: func(context.Context) (config.SMSConfig, error) {
			return cfgHolder, nil
		},
	})
	svc := NewSmsTemplateService(rt)
	ctx := context.Background()

	// 先同步一轮,让登记册登记上 SMS_1
	_, err := svc.Disable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "先停 SMS_1")
	require.NoError(t, err)

	// 配置中心换成 SMS_2
	cfgHolder = smsTplCfg("SMS_2")

	// 运营再点一次停用 —— 必须落到新号上
	v, err := svc.Disable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "停 SMS_2")
	require.NoError(t, err)
	assert.Equal(t, "SMS_2", v.TemplateCode, "视图里的模板号应是生效的那个")
	assert.Equal(t, "SMS_2", v.RegisteredCode, "登记册快照必须换到新号")
	assert.False(t, v.CodeMismatch, "停用之后两个号必须一致")
	assert.Equal(t, string(smstpl.StatusDisabled), v.Status)
}

// ---- Sync ----

func TestSmsTemplate_Sync_ConsoleChannel_ReportsNotSupported(t *testing.T) {
	svc := newSmsTemplateSvc(t, config.SMSConfig{Provider: "console"})

	_, err := svc.Sync(context.Background())
	require.Error(t, err)
	assert.Equal(t, errcode.BadRequest, errCodeOf(t, err))
	assert.Contains(t, err.Error(), "console", "提示里要带上是哪个通道不支持")
}

// ---- History ----

func TestSmsTemplate_History_LimitClamped(t *testing.T) {
	svc := newSmsTemplateSvc(t, smsTplCfg("SMS_1"))
	ctx := context.Background()
	_, err := svc.Disable(ctx, smsCaller("zhangsan"), smstpl.SceneLogin, "停用")
	require.NoError(t, err)

	// limit<=0 → 默认 50;limit>200 → 夹到 200(都不该报错)
	_, err = svc.History(ctx, smstpl.SceneLogin, 0)
	require.NoError(t, err)
	_, err = svc.History(ctx, smstpl.SceneLogin, 99999)
	require.NoError(t, err)
}

// ---- 未装配 ----

func TestSmsTemplate_NotAssembled_503(t *testing.T) {
	svc := NewSmsTemplateService(nil)
	_, err := svc.List(context.Background())
	require.Error(t, err)
	assert.Equal(t, errcode.ServiceUnavailable, errCodeOf(t, err))
}

// ---- 原因截断 ----

func TestSmsTemplate_Disable_ReasonTruncatedToColumnWidth(t *testing.T) {
	svc := newSmsTemplateSvc(t, smsTplCfg("SMS_1"))

	long := strings.Repeat("很", 600)
	v, err := svc.Disable(context.Background(), smsCaller("zhangsan"), smstpl.SceneLogin, long)
	require.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(v.Reason)), 500,
		"原因要截到列宽(500),否则 MySQL 严格模式直接报错")
}

// ---- 上一轮失败原因 ----

// ⚠️ **页面上要能说出"上次为什么没同步"** ——
//
// 只有状态("未同步")没有原因时,运维看到的是一片"未同步"却无从下手。
// 这正是用户 2026-10-30 报的"定时器显示成功、列表还是未同步"的后半段。
func TestSmsTemplate_List_ShowsLastFailureReason(t *testing.T) {
	db := newSmsTemplateTestDB(t)
	f := &m115Fetcher{errs: map[string]error{"SMS_1": errors.New("服务商超时")}}
	rt := smstpl.NewRuntime(smstpl.RuntimeOptions{
		Store: smstpl.NewSQLStore(db),
		Resolve: func(context.Context) (config.SMSConfig, error) {
			return smsTplCfg("SMS_1"), nil
		},
		NewFetcher: func(context.Context, config.SMSConfig) (smstpl.Fetcher, error) {
			return f, nil
		},
	})
	svc := NewSmsTemplateService(rt)
	ctx := context.Background()

	_, err := svc.Sync(ctx)
	require.NoError(t, err, "手动同步本身不该报错 —— 失败原因在 failures 里")

	view, err := svc.List(ctx)
	require.NoError(t, err)

	var loginItem *SmsTemplateView
	for i := range view.Items {
		if view.Items[i].Scene == smstpl.SceneLogin {
			loginItem = &view.Items[i]
		}
	}
	require.NotNil(t, loginItem, "登录场景必须在列表里")
	assert.Contains(t, loginItem.LastError, "服务商超时",
		"上一轮失败的原因要出现在视图里,否则页面只显示'未同步',运维无从下手")
}

// ⚠️ **同步成功了,失败原因必须清掉** ——
//
// 留着上一轮的旧原因会让人以为"现在还在失败"。
func TestSmsTemplate_List_LastFailureClearedAfterSuccess(t *testing.T) {
	db := newSmsTemplateTestDB(t)
	// ⚠️ **第一轮是失败的** —— fail 初始为 true,第二轮再翻成 false
	fail := true
	f := &m115Fetcher{status: map[string]smstpl.Status{"SMS_1": smstpl.StatusApproved}}
	rt := smstpl.NewRuntime(smstpl.RuntimeOptions{
		Store: smstpl.NewSQLStore(db),
		Resolve: func(context.Context) (config.SMSConfig, error) {
			return smsTplCfg("SMS_1"), nil
		},
		NewFetcher: func(context.Context, config.SMSConfig) (smstpl.Fetcher, error) {
			return &switchingFetcher{inner: f, fail: &fail}, nil
		},
	})
	svc := NewSmsTemplateService(rt)
	ctx := context.Background()

	_, err := svc.Sync(ctx)
	require.NoError(t, err)
	view, err := svc.List(ctx)
	require.NoError(t, err)
	for _, it := range view.Items {
		if it.Scene == smstpl.SceneLogin {
			assert.NotEmpty(t, it.LastError, "第一轮是失败的,应该有原因")
		}
	}

	// 第二轮:服务商正常
	fail = false
	_, err = svc.Sync(ctx)
	require.NoError(t, err)
	view, err = svc.List(ctx)
	require.NoError(t, err)
	for _, it := range view.Items {
		if it.Scene == smstpl.SceneLogin {
			assert.Empty(t, it.LastError, "这轮成功了,上一轮的旧原因必须清掉")
		}
	}
}

// switchingFetcher 按开关切换"成功 / 失败"的查询器
type switchingFetcher struct {
	inner *m115Fetcher
	fail  *bool
}

func (s *switchingFetcher) FetchTemplateStatus(ctx context.Context, code string) (smstpl.ProviderStatus, error) {
	if s.fail != nil && *s.fail {
		return smstpl.ProviderStatus{}, errors.New("服务商超时")
	}
	return s.inner.FetchTemplateStatus(ctx, code)
}
