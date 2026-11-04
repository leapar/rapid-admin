// logger 包单测 —— §1.8 日志/可观测性链路(M99)
//
// # 这个包此前**零测试**,而它挂在全局路径上
//
// logger.New 的返回值直接给了 zap.ReplaceGlobals,所有请求的日志都从这里出去。
// 零覆盖的全局组件 = 任何一条日志链路的坏法都不会有人发现。
//
// # M99 在这里实证出的 4 个真 bug
//
//  1. audit.log / access.log **两条通道一个字节都没写进去**
//     —— 路由靠 `zapcore.Entry.LoggerName`,而 LoggerName 只有 `Named(...)`
//     派生的 logger 才非空;AccessLog 中间件此前写的是**无名** logger。
//  2. audit/access 用的是默认 encoder(ts = epoch 秒),
//     info/warn/error 用 ISO8601 → 跨文件时间对不上。
//  3. `SyncFlush()` 是 `return nil` 的**空壳**,名字承诺"同步所有 rotator",
//     实现什么都不做。
//  4. hook 挂的 core **不在 tee 里** → `log.Sync()` 刷不到 audit/access。
//  5. hook **拿不到每次调用的结构化字段**(`zapcore.hooked.Write` 把 fields
//     丢了)→ audit.log / access.log 里只剩 level/ts/logger/caller/msg,
//     path / method / status / user 全没了。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                              必须变红
//	──────────────────────────────────────────  ──────────────────────────────
//	P1 AccessLog 改回无名 logger(L().Info)      TestAuditAccessHook_RoutesNamedLoggersToOwnFiles
//	P2 audit/access 换回默认 encoder             TestAuditAccessTimestamps_ISO8601
//	P3 SyncFlush 改成 return nil(空壳)            TestSyncFlush_ReportsRotatorSyncError
//	P4 registerRotators 不登记 audit/access       TestNew_RegistersFiveRotators
//	P16 路由改回 zap.Hooks                        TestAuditAccessChannels_KeepStructuredFields
//
//	⚠️ **P3 是这条纪律的典型**:SyncFlush 此前根本没有实现,
//	"它刷不刷得到"从外面**看不见** —— 所以不能只断言"调用不报错",
//	必须造一个**刷盘会失败**的 rotator(把文件句柄关掉),
//	断言错误**被返回出来**。只断言"返回 nil"的测试,拆掉实现照样绿。
package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// ── 测试辅助 ──────────────────────────────────────────────────────────

// newTestLogger 建一个**只落文件**的 logger
//
// Env 用 "pre":既不是 dev/test(不挂 stdout core,免得测试输出被日志淹没),
// 也不是 prod(不会强制开 gzip 压缩)。
func newTestLogger(t *testing.T, dir string) *zap.Logger {
	t.Helper()
	log, err := New(Config{
		Level:      "info",
		Format:     "json",
		Dir:        dir,
		MaxSizeMB:  1,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Env:        "pre",
	})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	// rotator 的文件句柄是常开的,Windows 上不关掉的话 t.TempDir() 清理会失败
	t.Cleanup(func() { _ = Close() })
	return log
}

// dailyFile 读当天目录下某个级别的日志文件
func dailyFile(t *testing.T, dir, level string) string {
	t.Helper()
	p := filepath.Join(dir, time.Now().Format("2006-01-02"), level+".log")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", p, err)
	}
	return string(b)
}

// firstJSONLine 取第一行并解析成 map
func firstJSONLine(t *testing.T, content string) map[string]any {
	t.Helper()
	line := strings.TrimSpace(strings.SplitN(content, "\n", 2)[0])
	if line == "" {
		t.Fatal("日志文件是空的")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("日志行不是合法 JSON: %v (line=%q)", err, line)
	}
	return m
}

// useDefault 把包级 Default 指到 log,测试结束恢复
//
// logger.Audit() / logger.Access() 走的是 L() → Default。
func useDefault(t *testing.T, log *zap.Logger) {
	t.Helper()
	old := Default
	Default = log
	t.Cleanup(func() { Default = old })
}

// ── 1. audit / access 两条通道 ─────────────────────────────────────────

// TestAuditAccessHook_RoutesNamedLoggersToOwnFiles 具名 logger 必须落到自己的文件
//
// # M99 修的第一个 bug
//
// 路由规则:`zapcore.Entry.LoggerName` 为 "audit" → audit.log;
// 为 "access" → access.log;**空串 → 两条都不进**。
//
// LoggerName 只有 `Named(...)` 派生的 logger 才非空 —— `L().Named("audit")` 有,
// `L()` 没有。
//
// 此前 `AccessLog` 中间件写的是 `L().Info("access", ...)`(无名),
// 于是 LoggerName == "" → hook 直接 return nil → **access.log 连文件都不会被创建**
// (rotator 是懒创建的)。
//
// 这条测试同时钉住三件事:
//   - 具名 logger 落自己的文件
//   - 不串台(audit.log 里不能有 access 条目)
//   - 无名 logger **不进**任何一条独立通道(它只该在 info.log 里)
func TestAuditAccessHook_RoutesNamedLoggersToOwnFiles(t *testing.T) {
	dir := t.TempDir()
	log := newTestLogger(t, dir)
	useDefault(t, log)

	Audit().Info("audit-msg")
	Access().Info("access-msg")
	L().Info("plain-msg")
	_ = log.Sync()

	audit := dailyFile(t, dir, "audit")
	access := dailyFile(t, dir, "access")
	info := dailyFile(t, dir, "info")

	if !strings.Contains(audit, "audit-msg") {
		t.Errorf("audit.log 里没有 audit 条目:\n%s", audit)
	}
	if strings.Contains(audit, "access-msg") {
		t.Errorf("audit.log 里混进了 access 条目:\n%s", audit)
	}
	if !strings.Contains(access, "access-msg") {
		t.Errorf("access.log 里没有 access 条目:\n%s", access)
	}
	if strings.Contains(access, "audit-msg") {
		t.Errorf("access.log 里混进了 audit 条目:\n%s", access)
	}
	// 无名 logger 不该进任何独立通道
	if strings.Contains(access, "plain-msg") {
		t.Errorf("access.log 里混进了无名 logger 的条目:\n%s", access)
	}
	if strings.Contains(audit, "plain-msg") {
		t.Errorf("audit.log 里混进了无名 logger 的条目:\n%s", audit)
	}
	// 但它必须在 info.log 里(否则说明整个 tee 断了)
	if !strings.Contains(info, "plain-msg") {
		t.Errorf("info.log 里没有普通日志:\n%s", info)
	}

	// logger 字段(= zapcore.Entry.LoggerName)必须是对应的名字
	if got := firstJSONLine(t, audit)["logger"]; got != "audit" {
		t.Errorf("audit.log 里的 logger 字段 = %v, want \"audit\"", got)
	}
	if got := firstJSONLine(t, access)["logger"]; got != "access" {
		t.Errorf("access.log 里的 logger 字段 = %v, want \"access\"", got)
	}
}

// TestAuditAccessChannels_KeepStructuredFields 独立通道必须带上结构化字段
//
// # M99 修的第五个 bug
//
// 路由此前用 `zap.Hooks(...)`,而 zap 的 hook **拿不到每次调用的结构化字段**:
// `zapcore.hooked.Write(ent Entry, _ []Field)` 把 fields 直接丢了。
//
// 于是 audit.log / access.log 里只剩 level/ts/logger/caller/msg ——
// path / method / status / user 这些**全没了**。
// 一份没有内容的访问日志,等于没有访问日志。
//
// ⚠️ 断言要点:必须查 **audit.log / access.log 本身**,
// 不能查 info.log(具名条目同时也会进 info.log,查它是假绿)。
func TestAuditAccessChannels_KeepStructuredFields(t *testing.T) {
	dir := t.TempDir()
	log := newTestLogger(t, dir)
	useDefault(t, log)

	Audit().Info("audit-fields", zap.String("module", "/m99/audit"), zap.Int("status_code", 200))
	Access().Info("access-fields", zap.String("path", "/m99/access"), zap.Int("status", 200))
	_ = log.Sync()

	audit := firstJSONLine(t, dailyFile(t, dir, "audit"))
	if got := audit["module"]; got != "/m99/audit" {
		t.Errorf("audit.log 里的 module 字段 = %v,want \"/m99/audit\" —— 结构化字段被丢了", got)
	}
	if got := audit["status_code"]; got != float64(200) {
		t.Errorf("audit.log 里的 status_code 字段 = %v,want 200", got)
	}
	access := firstJSONLine(t, dailyFile(t, dir, "access"))
	if got := access["path"]; got != "/m99/access" {
		t.Errorf("access.log 里的 path 字段 = %v,want \"/m99/access\" —— 结构化字段被丢了", got)
	}
	if got := access["status"]; got != float64(200) {
		t.Errorf("access.log 里的 status 字段 = %v,want 200", got)
	}
	// logger 级别的字段(app/env)也必须在 ——
	// 旧实现里 audit/access 的 encoder 是现建的,没继承 zap.Fields
	if got := audit["app"]; got != "rapid-admin" {
		t.Errorf("audit.log 里的 app 字段 = %v,want \"rapid-admin\"", got)
	}
	if got := access["env"]; got != "pre" {
		t.Errorf("access.log 里的 env 字段 = %v,want \"pre\"", got)
	}
}

// TestAuditAccessHook_UnnamedLoggerCreatesNoFile 无名 logger 不该创建 access/audit 文件
//
// 上一条已经断言了"无名条目不进独立通道"。这条从**文件是否存在**的角度再钉一次:
// 一个纯无名 logger 的进程,不该凭空多出 access.log / audit.log。
func TestAuditAccessHook_UnnamedLoggerCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	log := newTestLogger(t, dir)
	useDefault(t, log)

	L().Info("only-plain")
	_ = log.Sync()

	dayDir := filepath.Join(dir, time.Now().Format("2006-01-02"))
	for _, level := range []string{"audit", "access"} {
		p := filepath.Join(dayDir, level+".log")
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s 不该被创建 —— 只有具名 logger 才该写它", level+".log")
		}
	}
	// info.log 必须存在
	if _, err := os.Stat(filepath.Join(dayDir, "info.log")); err != nil {
		t.Errorf("info.log 应该存在: %v", err)
	}
}

// ── 2. 时间戳格式 ──────────────────────────────────────────────────────

// TestAuditAccessTimestamps_ISO8601 audit/access 的 ts 必须是 ISO8601
//
// # M99 修的第二个 bug
//
// audit/access 的 core 此前用 `zap.NewProductionEncoderConfig()` 现建的,
// 它的 `EncodeTime` 默认是 **EpochTimeEncoder**(epoch 秒),
// 而 info/warn/error 用的是上面自定义的 `zapcore.ISO8601TimeEncoder`。
//
// 后果:同一个进程的三个文件,时间戳是**两种格式** ——
// 运维拿 info.log 的 "2026-10-26T21:08:17.683+0800" 和 audit.log 的
// "1793022497.683" 对不上,得自己心算。
//
// 这条测试断言 audit/access 的 ts 能被 ISO8601 布局解析 ——
// epoch 秒解析不了 → 变红。
func TestAuditAccessTimestamps_ISO8601(t *testing.T) {
	dir := t.TempDir()
	log := newTestLogger(t, dir)
	useDefault(t, log)

	Audit().Info("audit-ts")
	Access().Info("access-ts")
	L().Info("info-ts")
	_ = log.Sync()

	const isoLayout = "2006-01-02T15:04:05.000Z0700"
	for _, level := range []string{"audit", "access", "info"} {
		ts, _ := firstJSONLine(t, dailyFile(t, dir, level))["ts"].(string)
		if ts == "" {
			t.Errorf("%s.log 里没有 ts 字段", level)
			continue
		}
		if _, err := time.Parse(isoLayout, ts); err != nil {
			t.Errorf("%s.log 的 ts = %q,不是 ISO8601(旧实现是 epoch 秒): %v", level, ts, err)
		}
	}
}

// ── 3. SyncFlush ───────────────────────────────────────────────────────

// TestSyncFlush_ReportsRotatorSyncError SyncFlush 必须真的去刷盘并把错误返回
//
// # M99 修的第三个 bug
//
// 旧的 SyncFlush 是 `return nil` 的空壳 —— 名字承诺"同步所有 rotator",
// 实现什么都不做。
//
// ⚠️ **这条测试为什么不能只断言"调用不报错"**:
// 空壳实现返回 nil,一个"断言返回 nil"的测试**照样绿** —— 拆掉实现测不出来。
//
// 所以这里造一个**刷盘一定会失败**的 rotator(把它的文件句柄手动关掉,
// `os.File.Sync()` 对已关闭文件返回 "file already closed"),
// 断言 SyncFlush **把这个错误返回出来**。
// 空壳实现返回 nil → 变红。
func TestSyncFlush_ReportsRotatorSyncError(t *testing.T) {
	dir := t.TempDir()
	r := newTimeRotator(dir, "info", 1<<20, 3, 7, false)
	if _, err := r.Write([]byte("x")); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	// 同包测试能碰私有字段:把句柄关掉,让 Sync 必然失败
	if err := r.currentFile.Close(); err != nil {
		t.Fatalf("关闭文件失败: %v", err)
	}
	registerRotators(r)
	defer registerRotators()

	if err := SyncFlush(); err == nil {
		t.Fatal("SyncFlush 应该把 rotator 的 Sync 错误返回出来,却返回了 nil —— 它是个空壳")
	}
}

// TestSyncFlush_NoRotators_ReturnsNil 没登记 rotator 时不能 panic
func TestSyncFlush_NoRotators_ReturnsNil(t *testing.T) {
	registerRotators()
	defer registerRotators()
	if err := SyncFlush(); err != nil {
		t.Fatalf("没有 rotator 时 SyncFlush 应返回 nil,got %v", err)
	}
}

// TestSyncFlush_FlushesAllRegisteredRotators SyncFlush 要覆盖到 audit/access
//
// audit / access 两个 rotator 是挂在 `zap.Hooks(...)` 上的,**不在 tee 里**
// → `log.Sync()` 刷不到它们 → 进程退出时最后几条审计/访问日志可能丢。
// SyncFlush 就是兜这一段的。
func TestSyncFlush_FlushesAllRegisteredRotators(t *testing.T) {
	dir := t.TempDir()
	log := newTestLogger(t, dir)
	useDefault(t, log)

	Audit().Info("audit-flush")
	Access().Info("access-flush")
	if err := SyncFlush(); err != nil {
		t.Fatalf("SyncFlush 失败: %v", err)
	}
	// 刷盘后内容必须已经在文件里(至少写进去了)
	if !strings.Contains(dailyFile(t, dir, "audit"), "audit-flush") {
		t.Error("audit.log 里没有内容")
	}
	if !strings.Contains(dailyFile(t, dir, "access"), "access-flush") {
		t.Error("access.log 里没有内容")
	}
}

// ── 4. rotator 登记表 ──────────────────────────────────────────────────

// TestNew_RegistersFiveRotators New 之后 5 个 rotator 都要登记
//
// 登记表的用途就是 SyncFlush/Close 能找到它们。
// 少登记一个 = 那个文件的日志退出时刷不到、句柄不关。
func TestNew_RegistersFiveRotators(t *testing.T) {
	dir := t.TempDir()
	_ = newTestLogger(t, dir)

	got := currentRotators()
	if len(got) != 5 {
		t.Fatalf("登记的 rotator 数 = %d,want 5(info/warn/error/audit/access)", len(got))
	}
	for i, r := range got {
		if r == nil {
			t.Errorf("第 %d 个 rotator 是 nil", i)
		}
	}
}

// TestRegisterRotators_ReplacesPrevious 重复 New 不该攒僵尸 rotator
//
// 一次 New 一套 —— 后建的覆盖先建的(测试里反复 New 不会攒出一堆僵尸)。
func TestRegisterRotators_ReplacesPrevious(t *testing.T) {
	defer registerRotators()
	registerRotators(newTimeRotator(t.TempDir(), "a", 1, 1, 1, false))
	registerRotators(newTimeRotator(t.TempDir(), "b", 1, 1, 1, false))
	if got := len(currentRotators()); got != 1 {
		t.Fatalf("rotator 数 = %d,want 1(后建的覆盖先建的)", got)
	}
}

// ── 5. Close ───────────────────────────────────────────────────────────

// TestClose_ClosesFileAndSelfHeals Close 关掉句柄,之后还能继续写
//
// 关掉之后 rotator 是**可自愈**的:下一次 Write 发现 currentFile == nil
// 会重新 rotate 打开。
func TestClose_ClosesFileAndSelfHeals(t *testing.T) {
	dir := t.TempDir()
	r := newTimeRotator(dir, "info", 1<<20, 3, 7, false)
	if _, err := r.Write([]byte("hello")); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	registerRotators(r)
	defer registerRotators()
	// 自愈那步又把文件打开了,收尾必须关掉,否则 Windows 上 TempDir 删不掉
	t.Cleanup(func() { _ = r.Close() })

	if err := Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if r.currentFile != nil {
		t.Fatal("Close 之后 currentFile 还开着")
	}
	// 自愈:再写一次应该能重新打开
	if _, err := r.Write([]byte("world")); err != nil {
		t.Fatalf("Close 之后再写失败(应该自愈): %v", err)
	}
	if r.currentFile == nil {
		t.Fatal("再写之后 currentFile 还是 nil")
	}
}

// TestClose_NoRotators_ReturnsNil 没有 rotator 时 Close 不能 panic
func TestClose_NoRotators_ReturnsNil(t *testing.T) {
	registerRotators()
	defer registerRotators()
	if err := Close(); err != nil {
		t.Fatalf("没有 rotator 时 Close 应返回 nil,got %v", err)
	}
}
