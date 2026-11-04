// pkg/smstpl 运行期装配单测 —— M115-4
//
// # 这张网守的是什么
//
// Runtime 是**三处调用方的共同底座**:后台管理页、每 10 分钟的同步任务、
// 发送链路。它自己不出错还不够 —— 它要保证:
//
//  1. **人工停用能落到"这张号"上** —— 配置中心刚换了模板号,
//     点停用必须停的是**新号**(否则守卫按"号不一致"放行,停用形同虚设)。
//  2. **重启不丢人工停用** —— Load 之后登记册里必须还是 disabled,
//     否则下一轮同步就把服务商那句"已通过"写回去,硬规则 2 失效。
//  3. **人工不能判通过/驳回** —— 状态机的那道闸门在 Runtime 这条路上也生效。
//  4. **服务商同步不能复活人工停用** —— 走 Runtime.SyncOnce 这条路也一样。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· runtime.go:ApplyManual 里删掉 `case code != "" && cur.Code != code` 分支
//	  → 「换号后停用落在新号上」变红
//	· runtime.go:Load 改成直接 return nil
//	  → 「重启不丢人工停用」变红
//	· runtime.go:ApplyManual 里的 Apply 换成"直接改 map"
//	  → 「人工不能判通过」变红
//	· runtime.go:SyncOnce 里把 fetch==nil 当成"成功返回"
//	  → 「没有查询器时报 ErrNoFetcher」变红
package smstpl

import (
	"context"
	"errors"
	"testing"

	"rapid-admin/internal/config"
)

// ---- 夹具 ----

// fixedResolver 固定返回一份生效配置
func fixedResolver(cfg config.SMSConfig) ConfigResolver {
	return func(context.Context) (config.SMSConfig, error) { return cfg, nil }
}

// smsCfg 造一份只配了 login 模板号的生效配置
func smsCfg(code string) config.SMSConfig {
	return config.SMSConfig{
		Provider:  "aliyun",
		Templates: config.SMSTemplate{Login: code},
	}
}

// ---- Runtime.ApplyManual ----

func TestRuntime_ApplyManual_RegistersUnregisteredScene(t *testing.T) {
	store := &memStore{}
	rt := NewRuntime(RuntimeOptions{
		Store:   store,
		Resolve: fixedResolver(smsCfg("SMS_1")),
	})

	tpl, err := rt.ApplyManual(context.Background(), SceneLogin, StatusDisabled, "模板有问题", "zhangsan")
	if err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if tpl.Scene != SceneLogin || tpl.Code != "SMS_1" {
		t.Fatalf("登记应带上场景与模板号, got %+v", tpl)
	}
	if tpl.Status != StatusDisabled {
		t.Fatalf("状态应为 disabled, got %q", tpl.Status)
	}
	if len(store.templates) != 1 || store.templates[0].Status != StatusDisabled {
		t.Fatalf("停用必须落库, got %+v", store.templates)
	}
	if len(store.logs) != 1 {
		t.Fatalf("应有 1 条流转历史, got %d", len(store.logs))
	}
	if store.logs[0].Operator != "zhangsan" {
		t.Fatalf("流转历史必须记下操作人, got %q", store.logs[0].Operator)
	}
}

// ⚠️ **本条是 M115 最容易错的一处** ——
//
// 配置中心把模板号换了(SMS_1 → SMS_2),运营此时点"停用"。
// 如果不先对号就流转,登记册停在 SMS_1/disabled,而发送时用的是 SMS_2
// ⇒ 守卫按"号不一致 ⇒ 放行"处理 ⇒ **运营点了停用却拦不住任何东西**。
func TestRuntime_ApplyManual_AfterCodeChanged_DisablesTheNewNumber(t *testing.T) {
	reg := newTestRegistry(t)
	// 登记册里是旧号 + 已通过
	if _, _, err := reg.Register(SceneLogin, "SMS_1"); err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if _, _, err := reg.Apply(SceneLogin, StatusApproved, SourceProvider, "已通过", ""); err != nil {
		t.Fatalf("置为已通过失败: %v", err)
	}

	store := &memStore{}
	rt := NewRuntime(RuntimeOptions{
		Registry: reg,
		Store:    store,
		Resolve:  fixedResolver(smsCfg("SMS_2")), // ← 生效配置已换成新号
	})

	tpl, err := rt.ApplyManual(context.Background(), SceneLogin, StatusDisabled, "换号后停用", "lisi")
	if err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if tpl.Code != "SMS_2" {
		t.Fatalf("登记册必须换成新号, got %q", tpl.Code)
	}
	if tpl.Status != StatusDisabled {
		t.Fatalf("新号必须被停用, got %q", tpl.Status)
	}
	// 守卫必须真的拦得住(号一致 ⇒ 结论有效)
	if err := rt.Guard().Check(SceneLogin, "SMS_2"); err == nil {
		t.Fatal("守卫必须拦住已停用的模板")
	}
	// 旧号那条历史必须留着(审计链)
	if len(store.logs) == 0 {
		t.Fatal("换号 + 停用应各记一条流转历史")
	}
}

func TestRuntime_ApplyManual_CannotJudgeApproved(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{
		Store:   &memStore{},
		Resolve: fixedResolver(smsCfg("SMS_1")),
	})
	_, err := rt.ApplyManual(context.Background(), SceneLogin, StatusApproved, "我说了算", "zhangsan")
	if !errors.Is(err, ErrManualCannotJudge) {
		t.Fatalf("人工判通过必须被状态机拒绝, got %v", err)
	}
}

func TestRuntime_ApplyManual_EmptyScene(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{Store: &memStore{}, Resolve: fixedResolver(smsCfg("SMS_1"))})
	if _, err := rt.ApplyManual(context.Background(), "", StatusDisabled, "x", "y"); !errors.Is(err, ErrEmptyScene) {
		t.Fatalf("空场景必须报错, got %v", err)
	}
}

// ⚠️ **重启场景** —— 进程重启后登记册是空的,
// 人工停用的模板会被下一轮同步"复活"(服务商说通过 → approved)。
// Load 是唯一挡住这条路的动作。
func TestRuntime_Load_RestoresManualDisable(t *testing.T) {
	store := &memStore{templates: []Template{
		{Scene: SceneLogin, Code: "SMS_1", Status: StatusDisabled, Source: SourceManual},
	}}
	rt := NewRuntime(RuntimeOptions{Store: store, Resolve: fixedResolver(smsCfg("SMS_1"))})

	if err := rt.Load(context.Background()); err != nil {
		t.Fatalf("装载失败: %v", err)
	}
	tpl, ok := rt.Registry().Get(SceneLogin)
	if !ok {
		t.Fatal("装载后登记册里必须有这个场景")
	}
	if tpl.Status != StatusDisabled {
		t.Fatalf("重启后必须还是 disabled, got %q", tpl.Status)
	}
	if err := rt.Guard().Check(SceneLogin, "SMS_1"); err == nil {
		t.Fatal("重启后守卫必须仍然拦得住")
	}
}

// ---- Runtime.SyncOnce ----

func TestRuntime_SyncOnce_NoFetcher_ErrNoFetcher(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{
		Store:   &memStore{},
		Resolve: fixedResolver(smsCfg("SMS_1")),
		// NewFetcher 为 nil ⇒ 没有查询器
	})
	_, err := rt.SyncOnce(context.Background())
	if !errors.Is(err, ErrNoFetcher) {
		t.Fatalf("没有查询器必须报 ErrNoFetcher, got %v", err)
	}
}

func TestRuntime_SyncOnce_FetcherReturnsNilMeansNoFetcher(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{
		Store:   &memStore{},
		Resolve: fixedResolver(smsCfg("SMS_1")),
		NewFetcher: func(context.Context, config.SMSConfig) (Fetcher, error) {
			return nil, nil // console 通道:没有模板审核状态可查
		},
	})
	if _, err := rt.SyncOnce(context.Background()); !errors.Is(err, ErrNoFetcher) {
		t.Fatalf("查询器为 nil 必须报 ErrNoFetcher, got %v", err)
	}
}

// 没有配模板号 ⇒ 没东西可同步(不是失败)
func TestRuntime_SyncOnce_NoTemplateConfigured_Noop(t *testing.T) {
	fetch := &stubFetcher{}
	rt := NewRuntime(RuntimeOptions{
		Store:   &memStore{},
		Resolve: fixedResolver(config.SMSConfig{Provider: "console"}),
		NewFetcher: func(context.Context, config.SMSConfig) (Fetcher, error) {
			return fetch, nil
		},
	})
	res, err := rt.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("没配模板号不该报错: %v", err)
	}
	if len(fetch.calls) != 0 {
		t.Fatalf("没配模板号不该发请求, got %v", fetch.calls)
	}
	if len(res.Updated) != 0 || len(res.Failed) != 0 {
		t.Fatalf("没配模板号不该有更新/失败, got %+v", res)
	}
}

// ⚠️ **硬规则 2** —— 人工停用的模板,服务商同步不能自动复活
func TestRuntime_SyncOnce_DoesNotReviveManuallyDisabled(t *testing.T) {
	reg := newTestRegistry(t)
	if _, _, err := reg.Register(SceneLogin, "SMS_1"); err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	if _, _, err := reg.Apply(SceneLogin, StatusDisabled, SourceManual, "运维停用", "zhangsan"); err != nil {
		t.Fatalf("停用失败: %v", err)
	}

	fetch := &stubFetcher{byCode: map[string]ProviderStatus{
		"SMS_1": {Status: StatusApproved, Reason: "已通过"},
	}}
	rt := NewRuntime(RuntimeOptions{
		Registry: reg,
		Store:    &memStore{},
		Resolve:  fixedResolver(smsCfg("SMS_1")),
		NewFetcher: func(context.Context, config.SMSConfig) (Fetcher, error) {
			return fetch, nil
		},
	})

	res, err := rt.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("同步失败: %v", err)
	}
	tpl, _ := rt.Registry().Get(SceneLogin)
	if tpl.Status != StatusDisabled {
		t.Fatalf("服务商说通过也不能复活人工停用, got %q", tpl.Status)
	}
	// ⚠️ mfa 复用 login 的模板号(M150):它没被人工停用 ⇒ 会被同步成 approved;
	//    login 本身被规则拒绝,既不算更新、也要记进 Failed。
	for _, sc := range res.Updated {
		if sc == SceneLogin {
			t.Fatalf("被规则拒绝的复活不能算更新, got %v", res.Updated)
		}
	}
	if len(res.Failed) != 1 || res.Failed[0] != SceneLogin {
		t.Fatalf("被规则拒绝的场景应记进 Failed, got %v", res.Failed)
	}
}

// 同步拿到的状态要落回登记册 + 落库
func TestRuntime_SyncOnce_UpdatesRegistryAndStore(t *testing.T) {
	store := &memStore{}
	fetch := &stubFetcher{byCode: map[string]ProviderStatus{
		"SMS_1": {Status: StatusApproved, Reason: "审核通过"},
	}}
	rt := NewRuntime(RuntimeOptions{
		Store:   store,
		Resolve: fixedResolver(smsCfg("SMS_1")),
		NewFetcher: func(context.Context, config.SMSConfig) (Fetcher, error) {
			return fetch, nil
		},
	})

	res, err := rt.SyncOnce(context.Background())
	if err != nil {
		t.Fatalf("同步失败: %v", err)
	}
	// ⚠️ login + mfa(M150 复用 login 模板号)两个场景各记一条更新
	seen := map[string]bool{}
	for _, sc := range res.Updated {
		seen[sc] = true
	}
	if !seen[SceneLogin] || !seen[SceneMFA] || len(res.Updated) != 2 {
		t.Fatalf("应记 login+mfa 两条更新, got %v", res.Updated)
	}
	tpl, ok := rt.Registry().Get(SceneLogin)
	if !ok || tpl.Status != StatusApproved {
		t.Fatalf("登记册应为 approved, got %+v", tpl)
	}
	if len(store.templates) != 2 {
		t.Fatalf("两个场景的状态都必须落库, got %+v", store.templates)
	}
	for _, tpl := range store.templates {
		if tpl.Status != StatusApproved {
			t.Fatalf("落库状态必须都是 approved, got %+v", store.templates)
		}
	}
}

// ---- Runtime.History ----

func TestRuntime_History_PrefersStore(t *testing.T) {
	store := &memStore{logs: []Transition{
		{Scene: SceneLogin, From: StatusPending, To: StatusDisabled, Operator: "zhangsan"},
	}}
	rt := NewRuntime(RuntimeOptions{Store: store, Resolve: fixedResolver(smsCfg("SMS_1"))})

	hist, err := rt.History(context.Background(), SceneLogin, 50)
	if err != nil {
		t.Fatalf("读历史失败: %v", err)
	}
	if len(hist) != 1 || hist[0].Operator != "zhangsan" {
		t.Fatalf("必须优先读库里的历史, got %+v", hist)
	}
}

func TestRuntime_History_FallsBackToMemoryWhenNoStore(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{Resolve: fixedResolver(smsCfg("SMS_1"))})
	if _, err := rt.ApplyManual(context.Background(), SceneLogin, StatusDisabled, "内存里也要留痕", "zhangsan"); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	hist, err := rt.History(context.Background(), SceneLogin, 50)
	if err != nil {
		t.Fatalf("读历史失败: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("没配 Store 时退回内存历史, got %d 条", len(hist))
	}
}

// ---- Runtime 基础口径 ----

func TestRuntime_Policy_UnknownValueFallsBackToBlock(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{Policy: ParsePolicy("blcok")})
	if rt.Policy() != PolicyBlock {
		t.Fatalf("写错的策略值必须落到 block, got %q", rt.Policy())
	}
}

func TestRuntime_Guard_SharesRegistryWithManualActions(t *testing.T) {
	rt := NewRuntime(RuntimeOptions{Store: &memStore{}, Resolve: fixedResolver(smsCfg("SMS_1"))})
	if _, err := rt.ApplyManual(context.Background(), SceneLogin, StatusDisabled, "停用", "zhangsan"); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	// 同一个 Runtime ⇒ 守卫和人工动作看的是同一本登记册
	if err := rt.Guard().Check(SceneLogin, "SMS_1"); err == nil {
		t.Fatal("人工停用后守卫必须立刻拦得住")
	}
}
