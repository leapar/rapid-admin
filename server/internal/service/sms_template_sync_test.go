// M115 · 短信模板审核状态同步任务(2026-10-30)
//
// # 这一组守的是什么
//
// 同步任务是**守卫的数据源** —— 登记册里的状态如果永远停在"上次同步时的样子",
// 模板被服务商驳回之后平台会一直按旧状态放行,守卫形同虚设。
//
// 这一组守的是**任务这一层**的四件事:
//
//  1. **任务真的注册了** —— 注册表是启动时一次性快照,漏注册 = 任务永远不跑,
//     而且**没有任何报错**(NextRunAt 是空的)。
//  2. **频率是每 10 分钟** —— 再慢,"提前拦截"就不成立了。
//  3. **三种"不成功"处置各不相同** ——
//     通道不支持查询(console)= 跳过,不是失败;
//     单个场景拉不到 = 不失败任务(状态保持原样,下轮重试);
//     系统性失败(配置读不到 / 落库失败)= 任务记 failed。
//  4. **人工停用的模板不会被同步复活** —— 硬规则 2 在任务这一层的落法。
//
// # 可逆探针(拆掉实现看会不会红)
//
//	· 把 handler 里的 `errors.Is(err, smstpl.ErrNoFetcher)` 分支删掉
//	  → TestM115_SmsTemplateSync_UnsupportedChannelIsNotFailure 变红
//	· 把 `if len(res.Failed) > 0` 改成 `return err`(部分失败失败整个任务)
//	  → TestM115_SmsTemplateSync_PartialFailureDoesNotFailJob 变红
//	· 把 NonIdempotent 改成 true
//	  → TestM115_SmsTemplateSync_JobRegistered 变红
//	· 把 cron 表达式改成 "7 * * * *"(每小时一次)
//	  → TestM115_SmsTemplateSync_JobRegistered 变红
package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/cronx"
	"rapid-admin/internal/pkg/smstpl"
)

// m115RegisterOnce 保证"注册"这件事在测试进程里只发生一次
//
// ⚠️ **cronx.Register 撞 key 会 panic** —— 所以测试里不能每个用例都注册一次。
var m115RegisterOnce sync.Once

// m115RegisteredJob 取注册表里那个任务(必要时先注册一次)
func m115RegisteredJob(t *testing.T) cronx.Effective {
	t.Helper()
	m115RegisterOnce.Do(func() {
		RegisterSmsTemplateSyncJob(nil)
	})
	// ⚠️ **用 cronx 自己的解析器验 cron 表达式** —— 校验用一套、调度用另一套的话,
	//	// 会出现"保存时通过、实际没注册上"的静默失败。
	sched, err := cronx.New(cronx.Config{Enabled: true}, nil, nil)
	require.NoError(t, err)
	eff, err := sched.Get(KeySmsTemplateSync)
	require.NoError(t, err, "任务没注册 = 永远不跑,而且没有任何报错")
	return eff
}

// m115Fetcher 假的服务商状态查询器
type m115Fetcher struct {
	mu     sync.Mutex
	status map[string]smstpl.Status
	errs   map[string]error
	calls  []string
}

func (f *m115Fetcher) FetchTemplateStatus(_ context.Context, code string) (smstpl.ProviderStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, code)
	if err, ok := f.errs[code]; ok {
		return smstpl.ProviderStatus{}, err
	}
	st, ok := f.status[code]
	if !ok {
		return smstpl.ProviderStatus{}, errors.New("服务商:没有这张模板")
	}
	return smstpl.ProviderStatus{Status: st, Reason: "服务商备注"}, nil
}

// m115Runtime 造一个测试用的运行期装配
//
// ⚠️ **Store 留 nil** —— 落库那部分由 pkg/smstpl 自己的用例守着,
// 这里只关心"任务这一层"的处置。
func m115Runtime(fetch smstpl.Fetcher, resolveErr error) *smstpl.Runtime {
	cfg := config.SMSConfig{
		Provider: "aliyun",
		Templates: config.SMSTemplate{
			Login:         "TPL-login",
			ResetPassword: "TPL-reset",
		},
	}
	return smstpl.NewRuntime(smstpl.RuntimeOptions{
		Registry: smstpl.NewRegistry(),
		Resolve: func(context.Context) (config.SMSConfig, error) {
			if resolveErr != nil {
				return config.SMSConfig{}, resolveErr
			}
			return cfg, nil
		},
		NewFetcher: func(context.Context, config.SMSConfig) (smstpl.Fetcher, error) {
			// fetch 为 nil ⇒ 返回 (nil, nil) ⇒ Runtime 判定"这个通道没有审核状态"
			return fetch, nil
		},
		Policy: smstpl.PolicyBlock,
	})
}

// TestM115_SmsTemplateSync_JobRegistered 任务注册了、频率是每 10 分钟
func TestM115_SmsTemplateSync_JobRegistered(t *testing.T) {
	eff := m115RegisteredJob(t)

	assert.Equal(t, "framework.sms_template.sync", eff.Key,
		"前缀必须是 framework.* —— 这是框架层能力,不是某个业务模块的私事")
	assert.True(t, eff.Enabled, "任务必须是启用的")
	assert.NotNil(t, eff.Handler)
	assert.NotEmpty(t, eff.Name)
	assert.NotEmpty(t, eff.Description)

	// ⚠️ **NonIdempotent 必须留 false** ——
	// 非幂等任务在锁降级(Redis 不可用)时会被**整个跳过**,
	// 于是"服务商把模板驳回了"永远同步不回来 ⇒ 守卫拿着过期状态放行。
	assert.False(t, eff.NonIdempotent, "同步是幂等的,不能声明成非幂等")

	// ⚠️ **Timeout 不能用默认的 30 分钟** ——
	// 一轮最多 4 个场景 × 1 次 HTTP 调用;30 分钟会让"服务商卡住"的任务
	// 占着去重锁不放(锁 TTL 比它短,第二个副本会抢进来并发跑)。
	assert.Equal(t, 2*time.Minute, eff.Timeout)

	// ⚠️ **cron 是 5 段,且真的每 10 分钟一次** ——
	// 频率是守卫时效性的上限:再慢,"提前拦截"就不成立了。
	sched, err := cronx.ParseCron(eff.CronExpr)
	require.NoError(t, err, "表达式必须能被调度器的解析器接受(5 段)")
	require.NotNil(t, sched)

	runs, err := cronx.NextRuns(eff.CronExpr, 3, time.Local)
	require.NoError(t, err)
	require.Len(t, runs, 3)
	assert.Equal(t, 10*time.Minute, runs[1].Sub(runs[0]), "必须是每 10 分钟一次")
	assert.Equal(t, 10*time.Minute, runs[2].Sub(runs[1]), "必须是每 10 分钟一次")

	// ⚠️ **分钟数要错开整点** —— 全仓约定(见 pkg/jobs 的包注释):
	// 所有副本按绝对墙钟触发,整点那一刻大家一起醒来,去重锁最拥挤。
	for _, r := range runs {
		assert.NotZero(t, r.Minute()%10, "不能落在整点/整 10 分上")
	}
}

// TestM115_SmsTemplateSync_UnsupportedChannelIsNotFailure 通道不支持查询 ⇒ 跳过,不是失败
//
// ⚠️ console provider 压根没有模板可审 —— 这是"这个通道没这个能力",
// 不是"同步坏了"。标红会让运维以为出故障。
func TestM115_SmsTemplateSync_UnsupportedChannelIsNotFailure(t *testing.T) {
	rt := m115Runtime(nil, nil) // NewFetcher 返回 (nil, nil) ⇒ ErrNoFetcher
	require.NoError(t, syncSmsTemplateStatus(context.Background(), rt),
		"通道不支持查询审核状态 ≠ 同步失败")
}

// TestM115_SmsTemplateSync_PartialFailureDoesNotFailJob 单个场景拉不到 ⇒ 不失败整个任务
func TestM115_SmsTemplateSync_PartialFailureDoesNotFailJob(t *testing.T) {
	f := &m115Fetcher{
		status: map[string]smstpl.Status{"TPL-login": smstpl.StatusApproved},
		errs:   map[string]error{"TPL-reset": errors.New("服务商超时")},
	}
	rt := m115Runtime(f, nil)

	require.NoError(t, syncSmsTemplateStatus(context.Background(), rt),
		"一个服务商抖一下,不该把整个同步标成坏的")

	// 拉得到的那个场景,状态要真的写进登记册
	tpl, ok := rt.Registry().Get(smstpl.SceneLogin)
	require.True(t, ok)
	assert.Equal(t, smstpl.StatusApproved, tpl.Status, "拉得到的场景状态要更新")

	// ⚠️ **拉不到 ≠ 被驳回** —— 状态必须保持原样(这里是 unknown),
	// 写成 rejected 的话,一次网络抖动就把模板判成"确定发不出去"了。
	tpl2, ok := rt.Registry().Get(smstpl.SceneResetPassword)
	require.True(t, ok)
	assert.Equal(t, smstpl.StatusUnknown, tpl2.Status, "拉不到不能当成被驳回")
}

// TestM115_SmsTemplateSync_SystemicErrorFailsJob 系统性失败 ⇒ 任务记 failed
func TestM115_SmsTemplateSync_SystemicErrorFailsJob(t *testing.T) {
	boom := errors.New("配置中心读不到")
	rt := m115Runtime(&m115Fetcher{}, boom)

	err := syncSmsTemplateStatus(context.Background(), rt)
	require.Error(t, err, "系统性失败必须抛出去 —— 吞掉的话任务永远显示成功")
	assert.ErrorIs(t, err, boom)
}

// TestM115_SmsTemplateSync_AllScenesFailedFailsJob 这一轮一个状态都没定下来 ⇒ 任务记 failed
//
// ⚠️ **这是用户 2026-10-30 报的现象的正主** ——
//
//	"同步短信模版定时器记录显示成功,短信模版列表还是显示未同步"。
//
//	旧口径「单个场景失败不影响整个任务」漏了"**所有**场景都失败"这一种:
//	那一轮什么也没干成(凭证错了 / 服务商挂了 / 解析出 bug),
//	却记成一条绿色的"成功" ⇒ 运维无从下手。
func TestM115_SmsTemplateSync_AllScenesFailedFailsJob(t *testing.T) {
	f := &m115Fetcher{
		errs: map[string]error{
			"TPL-login": errors.New("服务商超时"),
			"TPL-reset": errors.New("模板号在阿里云不存在"),
		},
	}
	rt := m115Runtime(f, nil)

	err := syncSmsTemplateStatus(context.Background(), rt)
	require.Error(t, err,
		"这一轮一个状态都没定下来 = 什么也没干成,必须记 failed(不能记成功)")
	assert.Contains(t, err.Error(), "服务商超时",
		"失败原因要能进 sys_job_run.error —— 运维点开执行记录就能看到")
	assert.Contains(t, err.Error(), "模板号在阿里云不存在")
}

// TestM115_SmsTemplateSync_NoTemplateCodesIsNotFailure 一个模板号都没配 ⇒ 不是失败
//
// ⚠️ **"没什么可查" ≠ "查了全失败"** ——
//
//	Attempted=0(没配模板号)⇒ 这一轮压根没去查,任务不该记 failed。
//	判据必须是"尝试数 > 0 且没定下来的数 == 尝试数",
//	只看"失败数 == 尝试数"的话,0 == 0 会把"没配模板号"误判成"全挂"。
func TestM115_SmsTemplateSync_NoTemplateCodesIsNotFailure(t *testing.T) {
	f := &m115Fetcher{
		errs: map[string]error{"TPL-login": errors.New("服务商超时")},
	}
	// ⚠️ **一个模板号都不配** —— 四个场景全都没号
	rt := smstpl.NewRuntime(smstpl.RuntimeOptions{
		Registry: smstpl.NewRegistry(),
		Resolve: func(context.Context) (config.SMSConfig, error) {
			return config.SMSConfig{Provider: "aliyun"}, nil
		},
		NewFetcher: func(context.Context, config.SMSConfig) (smstpl.Fetcher, error) {
			return f, nil
		},
		Policy: smstpl.PolicyBlock,
	})

	require.NoError(t, syncSmsTemplateStatus(context.Background(), rt),
		"没配模板号 = 没什么可查,不是同步失败")
}

// TestM115_SmsTemplateSync_NilRuntimeIsError 没装配运行期 ⇒ 明确报错
func TestM115_SmsTemplateSync_NilRuntimeIsError(t *testing.T) {
	require.Error(t, syncSmsTemplateStatus(context.Background(), nil),
		"没装配时要明确报错,不能静默成功")
}

// TestM115_SmsTemplateSync_DoesNotReviveManuallyDisabled 人工停用的模板不被同步复活
//
// ⚠️ **这是硬规则 2 在任务这一层的落法** ——
// 运营点了"停用",下一轮同步看到服务商说"通过"就把它打开,
// 那"停用"就只是个按钮。
func TestM115_SmsTemplateSync_DoesNotReviveManuallyDisabled(t *testing.T) {
	f := &m115Fetcher{status: map[string]smstpl.Status{
		"TPL-login": smstpl.StatusApproved,
		"TPL-reset": smstpl.StatusApproved,
	}}
	rt := m115Runtime(f, nil)

	// 第一轮:登记 + 拿到服务商的结论
	require.NoError(t, syncSmsTemplateStatus(context.Background(), rt))

	// 人工停用登录模板
	_, err := rt.ApplyManual(context.Background(), smstpl.SceneLogin,
		smstpl.StatusDisabled, "先停掉", "admin")
	require.NoError(t, err)

	// 第二轮:服务商说"通过",但不许复活
	require.NoError(t, syncSmsTemplateStatus(context.Background(), rt),
		"服务商想复活人工停用的模板 —— 这是按规则拒绝,不是任务失败")

	tpl, ok := rt.Registry().Get(smstpl.SceneLogin)
	require.True(t, ok)
	assert.Equal(t, smstpl.StatusDisabled, tpl.Status,
		"人工停用的模板不能被同步复活")
}
