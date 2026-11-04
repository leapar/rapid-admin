// pkg/smstpl 单测 —— M115
//
// # 这张网守的是什么
//
// 短信模板审核流的三块地基:
//  1. 状态机(谁有资格把状态改成什么)
//  2. 登记册(换模板号 ⇒ 旧审核结论立即作废)
//  3. 守卫(只拦"确定不行"的,不确定的一律放行)
//
// # ⚠️ 这里最容易写错、也最该被网住的三处
//
//  1. **守卫把 unknown / pending 也拦了** ⇒ 同步任务一挂,登录验证码全挂,
//     而没人知道为什么(状态停在 pending,页面看着"审核中",像在正常等审核)。
//  2. **换模板号后旧状态没作废** ⇒ 拿旧号的"已通过"担保新号 = 假保证。
//  3. **服务商同步把人工停用的模板复活** ⇒ 运维刚止血,同步任务又把它打开。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· machine.go:`manualTargets` 里加上 StatusApproved → 「人工不能判通过」变红
//	· machine.go:删掉 `from == StatusDisabled` 那段 → 「服务商不能复活」变红
//	· registry.go:Register 里换号不重置状态 → 「换号状态作废」变红
//	· guard.go:`Blocked()` 改成 `s != StatusApproved` → 「未知状态放行」变红
//	· store.go:拉取失败时把状态置成 StatusRejected → 「拉取失败不改状态」变红
package smstpl

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---- 夹具 ----

// memStore 内存版 Store
type memStore struct {
	templates []Template
	logs      []Transition
	loadErr   error
	saveErr   error
}

func (m *memStore) Load(_ context.Context) ([]Template, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return append([]Template(nil), m.templates...), nil
}

func (m *memStore) SaveTemplates(_ context.Context, items []Template) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.templates = append([]Template(nil), items...)
	return nil
}

func (m *memStore) AppendLogs(_ context.Context, logs []Transition) error {
	m.logs = append(m.logs, logs...)
	return nil
}

func (m *memStore) ListLogs(_ context.Context, _ string, _ int) ([]Transition, error) {
	return append([]Transition(nil), m.logs...), nil
}

// stubFetcher 打桩的服务商查询
type stubFetcher struct {
	byCode map[string]ProviderStatus
	err    error
	calls  []string
}

func (s *stubFetcher) FetchTemplateStatus(_ context.Context, code string) (ProviderStatus, error) {
	s.calls = append(s.calls, code)
	if s.err != nil {
		return ProviderStatus{}, s.err
	}
	st, ok := s.byCode[code]
	if !ok {
		return ProviderStatus{}, errors.New("stub: 没有这个模板")
	}
	return st, nil
}

// newTestRegistry 造一个登记册(时钟固定,断言时间可控)
func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	fixed := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	r.SetClock(func() time.Time { return fixed })
	return r
}

// ---- 状态机 ----

func TestParseStatus(t *testing.T) {
	if s, err := ParseStatus(""); err != nil || s != StatusUnknown {
		t.Fatalf("空串应解析成 unknown, got %q err=%v", s, err)
	}
	if _, err := ParseStatus("approvd"); err == nil {
		t.Fatal("拼错的状态必须报错,不能静默归一到 unknown")
	}
	if s, err := ParseStatus("approved"); err != nil || s != StatusApproved {
		t.Fatalf("approved 应解析成功, got %q err=%v", s, err)
	}
}

func TestStatusBlocked(t *testing.T) {
	blocked := []Status{StatusRejected, StatusCancelled, StatusDisabled}
	for _, s := range blocked {
		if !s.Blocked() {
			t.Errorf("%q 应算【确定发不出去】", s)
		}
	}
	allowed := []Status{StatusUnknown, StatusPending, StatusReviewing, StatusApproved}
	for _, s := range allowed {
		if s.Blocked() {
			t.Errorf("%q 不应算【确定发不出去】(同步任务一挂就会把验证码通道锁死)", s)
		}
	}
}

func TestCanTransition(t *testing.T) {
	cases := []struct {
		name    string
		from    Status
		to      Status
		src     Source
		wantErr error
	}{
		{"同状态幂等", StatusApproved, StatusApproved, SourceProvider, nil},
		{"人工不能判通过", StatusPending, StatusApproved, SourceManual, ErrManualCannotJudge},
		{"人工不能判驳回", StatusPending, StatusRejected, SourceManual, ErrManualCannotJudge},
		{"人工可以停用", StatusApproved, StatusDisabled, SourceManual, nil},
		{"人工可以标待审核", StatusApproved, StatusPending, SourceManual, nil},
		{"人工可以把审核中标成待审核", StatusReviewing, StatusPending, SourceManual, nil},
		{"服务商可以判通过", StatusReviewing, StatusApproved, SourceProvider, nil},
		{"服务商可以置审核取消", StatusReviewing, StatusCancelled, SourceProvider, nil},
		{"人工不能标审核取消", StatusPending, StatusCancelled, SourceManual, ErrManualCannotJudge},
		// ⚠️ 这条是"可达表必须对称"的证据 ——
		// 同步任务 10 分钟才跑一轮,服务商两次状态之间可能整个跳过去
		// (重新送审后几分钟就通过,中间的"审核中"我们根本没看见)。
		// 少一条边,模板就永远卡在 cancelled 上,再也不会变成 approved。
		{"重新送审后直接通过(同步看不到中间的审核中)", StatusCancelled, StatusApproved, SourceProvider, nil},
		{"服务商不能复活人工停用", StatusDisabled, StatusApproved, SourceProvider, ErrProviderCannotRevive},
		{"服务商不能直接置停用", StatusApproved, StatusDisabled, SourceProvider, ErrIllegalTransition},
		{"未知目标状态", StatusPending, Status("wat"), SourceProvider, ErrIllegalTransition},
		{"未知来源", StatusPending, StatusApproved, Source("robot"), ErrIllegalTransition},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CanTransition(c.from, c.to, c.src)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("应合法, got %v", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("应报 %v, got %v", c.wantErr, err)
			}
		})
	}
}

// ---- 登记册 ----

func TestRegistryRegisterResetsStatusOnCodeChange(t *testing.T) {
	r := newTestRegistry(t)
	if _, _, err := r.Register("login", "SMS_1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply("login", StatusApproved, SourceProvider, "ok", ""); err != nil {
		t.Fatal(err)
	}
	tpl, _ := r.Get("login")
	if tpl.Status != StatusApproved {
		t.Fatalf("前置:状态应为 approved, got %q", tpl.Status)
	}

	// 换模板号 ⇒ 旧审核结论立即作废
	changed, tr, err := r.Register("login", "SMS_2")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("换号应算实质变化")
	}
	if tr.At.IsZero() {
		t.Fatal("换号应记一条历史")
	}
	tpl, _ = r.Get("login")
	if tpl.Status != StatusUnknown {
		t.Fatalf("换号后状态应回到 unknown, got %q", tpl.Status)
	}
	if tpl.Code != "SMS_2" {
		t.Fatalf("模板号应更新为 SMS_2, got %q", tpl.Code)
	}
}

func TestRegistryRegisterSameCodeIsNoop(t *testing.T) {
	r := newTestRegistry(t)
	if _, _, err := r.Register("login", "SMS_1"); err != nil {
		t.Fatal(err)
	}
	changed, tr, err := r.Register("login", "SMS_1")
	if err != nil {
		t.Fatal(err)
	}
	if changed || !tr.At.IsZero() {
		t.Fatal("同一个号重复登记不应算变化,也不该记历史")
	}
}

func TestRegistryApplyNoChangeDoesNotRecordHistory(t *testing.T) {
	r := newTestRegistry(t)
	if _, _, err := r.Register("login", "SMS_1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply("login", StatusApproved, SourceProvider, "通过", ""); err != nil {
		t.Fatal(err)
	}
	before := len(r.AllHistory())

	// 同一个状态 + 同一个理由再来一次(服务商每次同步都这样)
	changed, tr, err := r.Apply("login", StatusApproved, SourceProvider, "通过", "")
	if err != nil {
		t.Fatal(err)
	}
	if changed || !tr.At.IsZero() {
		t.Fatal("没变化不应算变化")
	}
	if got := len(r.AllHistory()); got != before {
		t.Fatalf("历史条数不该变: before=%d after=%d", before, got)
	}
	tpl, _ := r.Get("login")
	if tpl.ReviewedAt.IsZero() {
		t.Fatal("没变化时也要更新【服务商最后确认时间】")
	}
}

func TestRegistryApplyReasonChangeRecordsHistory(t *testing.T) {
	r := newTestRegistry(t)
	if _, _, err := r.Register("login", "SMS_1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Apply("login", StatusRejected, SourceProvider, "内容含错别字", ""); err != nil {
		t.Fatal(err)
	}
	before := len(r.AllHistory())

	changed, tr, err := r.Apply("login", StatusRejected, SourceProvider, "内容含错别字(第 2 条)", "")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || tr.At.IsZero() {
		t.Fatal("理由变了是**新信息**,必须记历史")
	}
	if got := len(r.AllHistory()); got != before+1 {
		t.Fatalf("历史应 +1: before=%d after=%d", before, got)
	}
}

func TestRegistryApplyUnregisteredScene(t *testing.T) {
	r := newTestRegistry(t)
	_, _, err := r.Apply("login", StatusApproved, SourceProvider, "", "")
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("未登记的场景应报 ErrNotRegistered, got %v", err)
	}
}

// ---- 守卫 ----

// guardFixture 造一个"某个场景处于某状态"的守卫
//
// ⚠️ **来源要按状态选** —— pending / disabled 只有人工能置(服务商置不了),
// 照抄成 SourceProvider 会被状态机当场拒绝,夹具就变成了测别的东西。
func guardFixture(t *testing.T, status Status, code string) *Guard {
	t.Helper()
	r := newTestRegistry(t)
	if _, _, err := r.Register("login", code); err != nil {
		t.Fatal(err)
	}
	if status != StatusUnknown {
		src := SourceProvider
		switch status {
		case StatusPending, StatusDisabled:
			src = SourceManual
		}
		if _, _, err := r.Apply("login", status, src, "测试", ""); err != nil {
			t.Fatal(err)
		}
	}
	return NewGuard(r, PolicyBlock)
}

func TestGuardBlocksOnlyDefiniteBadStates(t *testing.T) {
	cases := []struct {
		status    Status
		wantBlock bool
		wantErr   error
	}{
		{StatusUnknown, false, nil},
		{StatusPending, false, nil},
		{StatusReviewing, false, nil},
		{StatusApproved, false, nil},
		{StatusRejected, true, ErrTemplateRejected},
		{StatusCancelled, true, ErrTemplateCancelled},
		{StatusDisabled, true, ErrTemplateDisabled},
	}
	for _, c := range cases {
		t.Run(string(c.status), func(t *testing.T) {
			g := guardFixture(t, c.status, "SMS_1")
			err := g.Check("login", "SMS_1")
			if c.wantBlock {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("应报 %v, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应拦, got %v", err)
			}
		})
	}
}

// TestBlockedStatesAreExactlyTheExpectedThree 被拦的状态必须**恰好**是这三个
//
// ⚠️ **这条盯的是"判据的唯一真源"** ——
//
//	`Status.Blocked()` 与守卫的拦截文案都从 blockedInfo 派生(同源),
//	所以"新增状态忘了决定要不要拦"这种漂移已经不可能了。
//	但还有另一类漂移:有人把某个状态**从被拦改成不被拦**(或者反过来),
//	而单看状态机测试是发现不了的 —— 它会安静地改变线上行为。
//	⇒ 这条用例把"到底哪几个状态算确定发不出去"钉成一份显式清单。
func TestBlockedStatesAreExactlyTheExpectedThree(t *testing.T) {
	want := map[Status]bool{
		StatusRejected:  true,
		StatusCancelled: true,
		StatusDisabled:  true,
	}
	for _, s := range allStatuses {
		if got := s.Blocked(); got != want[s] {
			t.Errorf("状态 %q 的【确定发不出去】判定 = %v,期望 %v", s, got, want[s])
		}
	}
	// 每个被拦的状态都必须有哨兵错误 + 默认理由
	for s := range want {
		sentinel, reason := blockedInfo(s)
		if sentinel == nil {
			t.Errorf("%q 算【确定发不出去】,但没有哨兵错误 ⇒ 守卫会落到兜底分支", s)
		}
		if reason == "" {
			t.Errorf("%q 缺默认理由(页面/日志会只剩一个空括号)", s)
		}
	}
}

func TestGuardPolicyWarnDoesNotBlockButKeepsReason(t *testing.T) {
	g := guardFixture(t, StatusRejected, "SMS_1")
	g.policy = PolicyWarn

	d := g.Evaluate("login", "SMS_1")
	if d.Blocked || d.Err != nil {
		t.Fatalf("warn 策略不该拦: %+v", d)
	}
	if d.Reason == "" {
		t.Fatal("warn 策略也要给出原因,调用方要靠它记日志")
	}
}

func TestGuardPolicyOffNeverBlocks(t *testing.T) {
	g := guardFixture(t, StatusRejected, "SMS_1")
	g.policy = PolicyOff
	if err := g.Check("login", "SMS_1"); err != nil {
		t.Fatalf("off 策略不该拦, got %v", err)
	}
}

func TestGuardUnknownSceneIsAllowed(t *testing.T) {
	g := guardFixture(t, StatusRejected, "SMS_1")
	// 另一个场景没登记 ⇒ 不知道 ⇒ 放行
	if err := g.Check("reset_password", "SMS_1"); err != nil {
		t.Fatalf("未登记场景应放行, got %v", err)
	}
}

func TestGuardCodeMismatchIsAllowed(t *testing.T) {
	// 登记册里是 SMS_1(已驳回),这次要发的是 SMS_2(刚换的号,还没同步)
	g := guardFixture(t, StatusRejected, "SMS_1")
	if err := g.Check("login", "SMS_2"); err != nil {
		t.Fatalf("号不匹配时登记册的结论不适用,应放行, got %v", err)
	}
}

// ---- 同步器 ----

func TestSyncerFetchFailureKeepsStatus(t *testing.T) {
	reg := newTestRegistry(t)
	if _, _, err := reg.Register("login", "SMS_1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Apply("login", StatusApproved, SourceProvider, "通过", ""); err != nil {
		t.Fatal(err)
	}

	store := &memStore{}
	fetch := &stubFetcher{err: errors.New("服务商 500")}
	s := NewSyncer(reg, fetch, store)

	res, err := s.SyncOnce(context.Background(), map[string]string{"login": "SMS_1"})
	if err != nil {
		t.Fatalf("单个场景拉取失败不该让整轮失败: %v", err)
	}
	if len(res.Failed) != 1 || res.Failed[0] != "login" {
		t.Fatalf("应记进 Failed: %+v", res)
	}
	tpl, _ := reg.Get("login")
	if tpl.Status != StatusApproved {
		t.Fatalf("拉取失败不该改状态, got %q", tpl.Status)
	}
}

func TestSyncerAppliesProviderStatus(t *testing.T) {
	reg := newTestRegistry(t)
	store := &memStore{}
	fetch := &stubFetcher{byCode: map[string]ProviderStatus{
		"SMS_1": {Status: StatusApproved, Reason: "审核通过"},
	}}
	s := NewSyncer(reg, fetch, store)

	res, err := s.SyncOnce(context.Background(), map[string]string{"login": "SMS_1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Updated) != 1 {
		t.Fatalf("状态应更新: %+v", res)
	}
	tpl, _ := reg.Get("login")
	if tpl.Status != StatusApproved {
		t.Fatalf("状态应为 approved, got %q", tpl.Status)
	}
	if len(store.logs) != 1 {
		t.Fatalf("应落一条历史, got %d", len(store.logs))
	}
	if len(store.templates) != 1 {
		t.Fatalf("应落一条登记, got %d", len(store.templates))
	}
}

func TestSyncerCodeChangeInvalidatesOldStatus(t *testing.T) {
	reg := newTestRegistry(t)
	store := &memStore{}
	fetch := &stubFetcher{byCode: map[string]ProviderStatus{
		"SMS_1": {Status: StatusApproved, Reason: "审核通过"},
		"SMS_2": {Status: StatusReviewing, Reason: "审核中"},
	}}
	s := NewSyncer(reg, fetch, store)

	if _, err := s.SyncOnce(context.Background(), map[string]string{"login": "SMS_1"}); err != nil {
		t.Fatal(err)
	}
	// 运营换了模板号
	if _, err := s.SyncOnce(context.Background(), map[string]string{"login": "SMS_2"}); err != nil {
		t.Fatal(err)
	}
	tpl, _ := reg.Get("login")
	if tpl.Code != "SMS_2" {
		t.Fatalf("模板号应更新, got %q", tpl.Code)
	}
	if tpl.Status != StatusReviewing {
		t.Fatalf("新号的状态应是 reviewing, got %q", tpl.Status)
	}
}

func TestSyncerProviderCannotReviveDisabled(t *testing.T) {
	reg := newTestRegistry(t)
	if _, _, err := reg.Register("login", "SMS_1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Apply("login", StatusDisabled, SourceManual, "运维停用", ""); err != nil {
		t.Fatal(err)
	}

	store := &memStore{}
	fetch := &stubFetcher{byCode: map[string]ProviderStatus{
		"SMS_1": {Status: StatusApproved, Reason: "审核通过"},
	}}
	s := NewSyncer(reg, fetch, store)

	res, err := s.SyncOnce(context.Background(), map[string]string{"login": "SMS_1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 {
		t.Fatalf("服务商复活被拒应记进 Failed: %+v", res)
	}
	tpl, _ := reg.Get("login")
	if tpl.Status != StatusDisabled {
		t.Fatalf("人工停用的模板不能被服务商复活, got %q", tpl.Status)
	}
}

func TestSyncerSkipsEmptyTemplateCode(t *testing.T) {
	reg := newTestRegistry(t)
	store := &memStore{}
	fetch := &stubFetcher{}
	s := NewSyncer(reg, fetch, store)

	res, err := s.SyncOnce(context.Background(), map[string]string{"login": ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetch.calls) != 0 {
		t.Fatalf("空模板号不该去查服务商, calls=%v", fetch.calls)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("空模板号不算失败: %+v", res)
	}
}
