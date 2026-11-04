// anomalylogin 单测(§1.1 #11 P2)
//
// ## ⚠️ M128(2026-10-31):这组用例原来把一个 bug 当成了规范
//
//   `Detect` 里硬编码 `len(active) > 1`,**`MaxConcurrent` 从来没被读过**
//   ⇒ 字段注释说的"允许 N 端同时在线"是假的,实际第 2 端就报 High。
//
//   下面三条用例(`ConcurrentDifferentIP` / `ConcurrentDifferentDevice` /
//   `MultipleAnomalies`)当时用的都是 `New()`(= MaxConcurrent 3),
//   断言的是"2 个活跃会话 ⇒ 报异常" —— **那正是 bug 的行为**。
//   修好旋钮之后它们变红(这是对的:红灯说明用例编码的是错的规范)。
//
//   ⇒ 现在改成 `New().WithMaxConcurrent(1)`:显式说"只允许 1 个并发,
//     来了第 2 个就报",断言的还是同一件事(并发 + 不同 IP ⇒ High),
//     但**不再依赖旋钮是死的**。
//
//   另外补了 `TestDetect_MaxConcurrent_*` 四条,把阈值本身钉死 ——
//   那几条对着**修之前的实现**跑必须是红的(可逆探针)。
package anomalylogin

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"rapid-admin/internal/pkg/logger"
)

func mkLogin(sid string, ip, device string, at time.Time, logout *time.Time) LoginInfo {
	return LoginInfo{
		SessionID: sid,
		UserID:    42,
		IP:        ip,
		Device:    device,
		LoginAt:   at,
		LogoutAt:  logout,
	}
}

func TestDetect_NoHistory_NoAnomaly(t *testing.T) {
	d := New()
	cur := mkLogin("s1", "1.1.1.1", "iPhone", time.Now(), nil)
	if got := d.Detect(cur, nil); len(got) != 0 {
		t.Fatalf("no history = %d anomalies, want 0", len(got))
	}
}

func TestDetect_OnlySelf_NoAnomaly(t *testing.T) {
	d := New()
	cur := mkLogin("s1", "1.1.1.1", "iPhone", time.Now(), nil)
	// history 含自己(s1) + 已登出(s2) → 不应触发
	logoutOld := time.Now().Add(-1 * time.Hour)
	hist := []LoginInfo{
		cur,
		mkLogin("s2", "2.2.2.2", "Android", time.Now().Add(-1*time.Hour), &logoutOld),
	}
	if got := d.Detect(cur, hist); len(got) != 0 {
		t.Fatalf("no concurrent active = %d anomalies, want 0", len(got))
	}
}

func TestDetect_ConcurrentDifferentIP_High(t *testing.T) {
	// ⚠️ WithMaxConcurrent(1):显式"只允许 1 个并发"。
	// 原来这里是 New()(=3)—— 那时旋钮是死的,行为等同于 1;
	// 旋钮修好后 New() 不再是这个意思(见文件头)。
	d := New().WithMaxConcurrent(1)
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil), // 仍活跃
	}
	got := d.Detect(cur, hist)
	if len(got) < 1 {
		t.Fatalf("concurrent + diff IP = %d, want >=1", len(got))
	}
	foundIP := false
	for _, a := range got {
		if a.Type == ConcurrentDifferentIP && a.Severity == High {
			foundIP = true
		}
	}
	if !foundIP {
		t.Fatalf("expected ConcurrentDifferentIP High, got: %+v", got)
	}
}

func TestDetect_ConcurrentDifferentDevice_Medium(t *testing.T) {
	d := New().WithMaxConcurrent(1) // 见上:旋钮修好后必须显式给
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "1.1.1.1", "Android", now.Add(-1*time.Minute), nil), // 同 IP,不同设备
	}
	got := d.Detect(cur, hist)
	foundDev := false
	for _, a := range got {
		if a.Type == ConcurrentDifferentDevice && a.Severity == Medium {
			foundDev = true
		}
	}
	if !foundDev {
		t.Fatalf("expected ConcurrentDifferentDevice Medium, got: %+v", got)
	}
}

func TestDetect_ImpossibleTravel_Triggered(t *testing.T) {
	d := New().WithImpossibleTravelWindow(5 * time.Minute)
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil), // 1 分钟前 + 不同 IP
	}
	got := d.Detect(cur, hist)
	foundIT := false
	for _, a := range got {
		if a.Type == ImpossibleTravel && a.Severity == High {
			foundIT = true
		}
	}
	if !foundIT {
		t.Fatalf("expected ImpossibleTravel High, got: %+v", got)
	}
}

func TestDetect_ImpossibleTravel_DisabledByDefault(t *testing.T) {
	d := New() // 默认不开
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil),
	}
	got := d.Detect(cur, hist)
	for _, a := range got {
		if a.Type == ImpossibleTravel {
			t.Fatalf("ImpossibleTravel should be off by default, got: %+v", a)
		}
	}
}

func TestDetect_ImpossibleTravel_OutsideWindow_NoTrigger(t *testing.T) {
	d := New().WithImpossibleTravelWindow(1 * time.Minute)
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-10*time.Minute), nil), // 10 分钟前
	}
	got := d.Detect(cur, hist)
	for _, a := range got {
		if a.Type == ImpossibleTravel {
			t.Fatalf("ImpossibleTravel outside window should not fire, got: %+v", a)
		}
	}
}

func TestDetect_MultipleAnomalies(t *testing.T) {
	// 同时满足 Concurrent IP + Device + ImpossibleTravel
	d := New().WithMaxConcurrent(1).WithImpossibleTravelWindow(5 * time.Minute)
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil),
	}
	got := d.Detect(cur, hist)
	// 期望至少 3 条:IP, Device, ImpossibleTravel
	if len(got) < 3 {
		t.Fatalf("multi-anomaly = %d, want >=3: %+v", len(got), got)
	}
}

// ===== M128:把"并发阈值"这个旋钮本身钉死 =====
//
// 这四条对着**修之前的实现**跑必须是红的 —— 修之前 `Detect` 硬编码
// `len(active) > 1`,阈值字段是死的:
//   - 第 1 条(3 个活跃不报)会红(旧实现报了)
//   - 第 2 条(4 个活跃要报)旧实现也报 ⇒ 它单独不够,靠第 1 条一起钉死
//   - 第 3 条(配高阈值不报)会红(旧实现照报)
//   - 第 4 条(规则 3 不受门控)旧实现也过 ⇒ 守的是"别把规则 3 一起门控掉"

func TestDetect_MaxConcurrent_UnderThreshold_NoAnomaly(t *testing.T) {
	// 默认 MaxConcurrent=3:active = 本次 + 2 个别人 = 3 ⇒ 没超限 ⇒ 不报
	d := New()
	now := time.Now()
	cur := mkLogin("s3", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil),
		mkLogin("s2", "3.3.3.3", "iPad", now.Add(-2*time.Minute), nil),
	}
	if got := d.Detect(cur, hist); len(got) != 0 {
		t.Fatalf("3 个活跃会话(阈值 3)不该报,却报了 %d 条: %+v", len(got), got)
	}
}

func TestDetect_MaxConcurrent_OverThreshold_Anomaly(t *testing.T) {
	// 超过阈值:active = 本次 + 3 个别人 = 4 > 3 ⇒ 报
	d := New()
	now := time.Now()
	cur := mkLogin("s4", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil),
		mkLogin("s2", "3.3.3.3", "iPad", now.Add(-2*time.Minute), nil),
		mkLogin("s3", "4.4.4.4", "Windows", now.Add(-3*time.Minute), nil),
	}
	got := d.Detect(cur, hist)
	if len(got) == 0 {
		t.Fatalf("4 个活跃会话(阈值 3)该报,却一条没报")
	}
}

func TestDetect_MaxConcurrent_HigherThreshold_NoAnomaly(t *testing.T) {
	// 把阈值调高 ⇒ 2 个活跃会话不报(旧实现照报 ⇒ 这条会红)
	d := New().WithMaxConcurrent(5)
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil),
	}
	if got := d.Detect(cur, hist); len(got) != 0 {
		t.Fatalf("阈值 5、只有 2 个活跃会话不该报,却报了 %d 条: %+v", len(got), got)
	}
}

func TestDetect_ImpossibleTravel_NotGatedByMaxConcurrent(t *testing.T) {
	// 规则 3 判的是"时间差"不是"数量" ⇒ 不该被 MaxConcurrent 门控掉。
	// 阈值开到 10(远高于活跃数),ImpossibleTravel 仍要报。
	d := New().WithMaxConcurrent(10).WithImpossibleTravelWindow(5 * time.Minute)
	now := time.Now()
	cur := mkLogin("s2", "1.1.1.1", "iPhone", now, nil)
	hist := []LoginInfo{
		mkLogin("s1", "2.2.2.2", "Android", now.Add(-1*time.Minute), nil),
	}
	found := false
	for _, a := range d.Detect(cur, hist) {
		if a.Type == ImpossibleTravel {
			found = true
		}
	}
	if !found {
		t.Fatalf("ImpossibleTravel 不该被 MaxConcurrent 门控掉")
	}
}

func TestDetect_ZeroValueDetector_NoAnomalyForSelfOnly(t *testing.T) {
	// 零值 Detector(MaxConcurrent=0)不该让"只有自己"也变成超限。
	// ⇒ maxConcurrent() 把 ≤0 兜底成 1。
	d := &Detector{}
	now := time.Now()
	cur := mkLogin("s1", "1.1.1.1", "iPhone", now, nil)
	if got := d.Detect(cur, nil); len(got) != 0 {
		t.Fatalf("零值 Detector 只有自己时不该报,却报了 %d 条: %+v", len(got), got)
	}
}

func TestSeverity_String(t *testing.T) {
	cases := map[Severity]string{
		Low:    "low",
		Medium: "medium",
		High:   "high",
		99:     "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestLogNotifier_Hook(t *testing.T) {
	called := 0
	n := &LogNotifier{
		Hook: func(ctx context.Context, a Anomaly) { called++ },
	}
	n.Notify(context.Background(), Anomaly{Type: ConcurrentDifferentIP, Severity: High})
	if called != 1 {
		t.Fatalf("Hook called %d, want 1", called)
	}
}

// M128:默认通知器原来是个**空壳** —— 注释写着"落 stderr",实现里一行日志都没有。
// ⇒ 这条对着旧实现跑必须是红的(旧实现:0 条日志)。
func TestLogNotifier_LogsByDefault(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	old := logger.Default
	logger.Default = zap.New(core)
	t.Cleanup(func() { logger.Default = old })

	n := &LogNotifier{} // **没有 Hook** —— 默认通知器自己也必须留痕
	n.Notify(context.Background(), Anomaly{
		Type:     ConcurrentDifferentIP,
		Severity: High,
		UserID:   42,
		Reason:   "同一账号在两个不同 IP 同时活跃",
		Current:  LoginInfo{SessionID: "s2", IP: "1.1.1.1"},
		Existing: LoginInfo{SessionID: "s1", IP: "2.2.2.2"},
	})

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("默认通知器应记 1 条 WARN,实际 %d 条: %+v", len(entries), entries)
	}
	if entries[0].Level != zapcore.WarnLevel {
		t.Fatalf("级别 = %v, want warn", entries[0].Level)
	}
	fields := entries[0].ContextMap()
	if fields["user_id"] != uint64(42) {
		t.Fatalf("user_id = %v, want 42", fields["user_id"])
	}
	if fields["type"] != string(ConcurrentDifferentIP) {
		t.Fatalf("type = %v, want %v", fields["type"], ConcurrentDifferentIP)
	}
}

// 日志与 Hook **都要**:Hook 是业务副作用,日志是留痕,不能互相替代。
func TestLogNotifier_LogsAndCallsHook(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	old := logger.Default
	logger.Default = zap.New(core)
	t.Cleanup(func() { logger.Default = old })

	called := 0
	n := &LogNotifier{Hook: func(ctx context.Context, a Anomaly) { called++ }}
	n.Notify(context.Background(), Anomaly{Type: ConcurrentDifferentDevice, Severity: Medium})

	if called != 1 {
		t.Fatalf("Hook called %d, want 1", called)
	}
	if len(logs.All()) != 1 {
		t.Fatalf("有 Hook 时也必须记 1 条日志,实际 %d 条", len(logs.All()))
	}
}
