// TimeRotator 单测 —— §1.8(M99)
//
// # M99 在这里实证出的 2 个真 bug
//
//  1. **静默丢日志**:rotateSize() 重开文件失败后,rotator 停在
//     `currentFile == nil` 但 `currentDay == 今天` 的状态。
//     下一次 Write 判据只有 `currentDay != today` → 跳过 rotate →
//     直接 `r.currentFile.Write(p)`。
//     ⚠️ `os.File.Write` 对 nil 接收者**不 panic**,而是返回 ErrInvalid
//     (checkValid 里 `if f == nil { return ErrInvalid }`),
//     所以症状是"每次写都失败、日志从此全丢",而不是崩溃 ——
//     **比崩溃更难查**,因为没有任何告警。
//     触发条件是文件系统故障(磁盘满 / 目录被删 / 权限变更)。
//  2. **过期目录判定跨时区**:目录名按 UTC 解析、cutoff 按本地时间算,
//     两边时区不一致 → 边界行为随"当天几点"漂移,同一份配置在两地表现不同。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                          必须变红
//	────────────────────────────────────────  ────────────────────────────────
//	P1 Write 的判据去掉 `|| r.currentFile == nil`  TestTimeRotator_RecoversAfterFailedRotateSize
//	P2 expiredDir 改成"解析成 time 再比"          TestExpiredDir_Rule
//	P3 cleanup 不调 expiredDir(全都不删)            TestTimeRotator_CleanupRemovesOnlyExpiredDirs
//
//	⚠️ **P1 的坑**:这个 bug 的症状是"写入返回 ErrInvalid",**不是 panic**
//	(os.File 对 nil 接收者有保护)。所以断言必须盯住
//	"第三次写入要成功",而不是"不许 panic" —— 后者对旧实现是绿的
//	(旧实现返回 error,压根不 panic),等于没测。
package logger

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── 1. 基本写入 ────────────────────────────────────────────────────────

// TestTimeRotator_WriteCreatesDailyDir 写日志要落到"当天目录/级别.log"
func TestTimeRotator_WriteCreatesDailyDir(t *testing.T) {
	dir := t.TempDir()
	r := newTimeRotator(dir, "info", 1<<20, 3, 7, false)
	t.Cleanup(func() { _ = r.Close() })

	if _, err := r.Write([]byte("hello rotator\n")); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	p := filepath.Join(dir, time.Now().Format("2006-01-02"), "info.log")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", p, err)
	}
	if string(b) != "hello rotator\n" {
		t.Errorf("文件内容 = %q,want %q", string(b), "hello rotator\n")
	}
}

// TestTimeRotator_SyncAndClose_NoError Sync / Close 不该报错
func TestTimeRotator_SyncAndClose_NoError(t *testing.T) {
	dir := t.TempDir()
	r := newTimeRotator(dir, "info", 1<<20, 3, 7, false)
	if _, err := r.Write([]byte("x")); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := r.Sync(); err != nil {
		t.Errorf("Sync 失败: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("Close 失败: %v", err)
	}
}

// ── 2. B7:重开文件失败后的自愈 ─────────────────────────────────────────

// writeRecover 调 Write 并把 panic 捕获回来
//
// ⚠️ 旧实现撞的是"返回 ErrInvalid"而不是 panic(os.File 对 nil 接收者有保护),
// 所以断言的主体是 err;这里的 recover 只是**防御** —— 万一将来有人把
// `r.currentFile.Write` 改成直接解引用,不至于带走整个测试二进制
// (panic 会让同包后面的测试全跑不了,探针就分不清红在哪个测试上)。
func writeRecover(r *TimeRotator, p []byte) (n int, err error, panicked any) {
	defer func() {
		if rec := recover(); rec != nil {
			panicked = rec
		}
	}()
	n, err = r.Write(p)
	return
}

// TestTimeRotator_RecoversAfterFailedRotateSize 重开文件失败后不能 panic
//
// # 复现路径(与现场一致)
//
//  1. 写一次 → 打开当天文件
//  2. maxSize 设成 1 字节 → 之后每次写都会触发"按大小滚动"
//  3. 把当天目录删掉 → rotateSize 里 Close / Rename / **OpenFile 全部失败**
//     → Write 返回 error,rotator 停在 currentFile == nil 且 currentDay == 今天
//  4. **再写一次** → 旧实现在这里永远返回 ErrInvalid(日志从此全丢)
//
// 现在的判据多了 `|| r.currentFile == nil` → 会重新 rotate → 自愈。
func TestTimeRotator_RecoversAfterFailedRotateSize(t *testing.T) {
	dir := t.TempDir()
	// maxSize = 1 字节:每次写都触发按大小滚动
	r := newTimeRotator(dir, "info", 1, 3, 7, false)
	t.Cleanup(func() { _ = r.Close() })

	if _, err, p := writeRecover(r, []byte("first")); err != nil || p != nil {
		t.Fatalf("首次写入异常: err=%v panic=%v", err, p)
	}

	// 制造"重开文件失败":把当天目录整个删掉
	//
	// ⚠️ 必须**先关掉句柄**再删目录 —— Windows 不允许删除打开的文件
	// (Linux 上删一个已打开的文件是可以的,所以这个测试在 Windows 上
	// 才需要这一步;生产环境两类系统都会撞上"重开失败"的路径)。
	dayDir := filepath.Join(dir, time.Now().Format("2006-01-02"))
	if err := r.currentFile.Close(); err != nil {
		t.Fatalf("关闭句柄失败: %v", err)
	}
	if err := os.RemoveAll(dayDir); err != nil {
		t.Fatalf("删除当天目录失败: %v", err)
	}

	// 这一写会因为目录没了而失败,并把 rotator 推进"currentFile == nil"的状态
	if _, err, p := writeRecover(r, []byte("x")); p != nil {
		t.Fatalf("第二次写入不该 panic(此时还没到坏状态): %v", p)
	} else if err == nil {
		t.Fatal("目录被删后写入本该失败,却成功了 —— 复现路径没生效")
	}
	if r.currentFile != nil {
		t.Fatal("复现没生效:currentFile 还开着,进不了坏状态")
	}

	// 关键的一写:旧实现在这里永远返回 ErrInvalid(日志从此全丢)
	n, err, panicked := writeRecover(r, []byte("y"))
	if panicked != nil {
		t.Fatalf("Write panic 了: %v", panicked)
	}
	if err != nil {
		t.Fatalf("Write 应该自愈(重新 rotate 打开文件),却报错: %v", err)
	}
	if n <= 0 {
		t.Errorf("Write 写入字节数 = %d,want > 0", n)
	}
	if r.currentFile == nil {
		t.Error("自愈之后 currentFile 还是 nil")
	}
	// 自愈之后内容必须真的落到文件里
	p := filepath.Join(dir, time.Now().Format("2006-01-02"), "info.log")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("自愈后读 %s 失败: %v", p, err)
	}
	if len(b) == 0 {
		t.Error("自愈后文件是空的")
	}
}

// ── 3. B9:过期目录判定 ─────────────────────────────────────────────────

// TestExpiredDir_Rule 过期判定规则(纯函数,可穷举)
//
// # 规则
//
//   - 名字不是合法日期目录 → 不删(别的子目录不能被误伤)
//   - 名字 <= cutoff(YYYY-MM-DD 字典序 = 时间序)→ 删
//
// # 为什么要抽成纯函数
//
// 原来的判定在 cleanup() 里内联,拿 `time.Now()` 现场算 cutoff ——
// **没法确定性地测**(同一份代码在不同日期/时区下结论不同)。
// 把 cutoff 变成参数之后,规则可以被穷举,而且与"运行时刻""机器时区"无关。
//
// ⚠️ 旧实现(`time.Parse` 按 UTC 解析 + 本地 cutoff 比大小)在**每天的
// 8 小时窗口里**结论与现在不同 —— 那种差异靠"跑一次看看"是抓不到的,
// 只能靠把规则钉死。
func TestExpiredDir_Rule(t *testing.T) {
	const cutoff = "2026-10-19"
	cases := []struct {
		name string
		want bool
	}{
		// 早于 cutoff → 删
		{"2000-01-01", true},
		{"2026-10-18", true},
		{"2026-01-01", true},
		// 等于 cutoff → 删(保留窗口是 cutoff+1 .. 今天)
		{"2026-10-19", true},
		// 晚于 cutoff → 留
		{"2026-10-20", false},
		{"2026-12-31", false},
		{"2999-12-31", false},
		// 不是日期目录 → 不动
		{"not-a-date", false},
		{"2026-10", false},
		{"2026-1-1", false}, // 非零填充 —— time.Parse 解析不了
		{"20261019", false}, // 无分隔符
		{"", false},
		{"logs", false},
	}
	for _, tc := range cases {
		if got := expiredDir(tc.name, cutoff); got != tc.want {
			t.Errorf("expiredDir(%q, %q) = %v,want %v", tc.name, cutoff, got, tc.want)
		}
	}
}

// TestIsDateDirName 目录名合法性
func TestIsDateDirName(t *testing.T) {
	valid := []string{"2026-10-19", "2000-01-01", "2999-12-31"}
	for _, v := range valid {
		if !isDateDirName(v) {
			t.Errorf("isDateDirName(%q) = false,want true", v)
		}
	}
	invalid := []string{"2026-1-1", "20261019", "2026-13-01", "not-a-date", "", "2026-10-19x"}
	for _, v := range invalid {
		if isDateDirName(v) {
			t.Errorf("isDateDirName(%q) = true,want false", v)
		}
	}
}

// TestTimeRotator_CleanupRemovesOnlyExpiredDirs cleanup 只删过期的目录
//
// 端到端:造 3 个目录(很老 / cutoff 当天 / 未来),跑 cleanup,断言只有该删的没了。
func TestTimeRotator_CleanupRemovesOnlyExpiredDirs(t *testing.T) {
	dir := t.TempDir()
	r := newTimeRotator(dir, "info", 1<<20, 3, 7, false)
	t.Cleanup(func() { _ = r.Close() })

	today := time.Now()
	cutoff := today.AddDate(0, 0, -7).Format("2006-01-02")
	old := today.AddDate(0, 0, -30).Format("2006-01-02")
	future := today.AddDate(0, 0, 30).Format("2006-01-02")

	for _, d := range []string{old, cutoff, future, "not-a-date"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatalf("建目录 %s 失败: %v", d, err)
		}
	}

	r.cleanup()

	if _, err := os.Stat(filepath.Join(dir, old)); !os.IsNotExist(err) {
		t.Errorf("%s 应该被删掉", old)
	}
	if _, err := os.Stat(filepath.Join(dir, cutoff)); !os.IsNotExist(err) {
		t.Errorf("%s(cutoff 当天)应该被删掉", cutoff)
	}
	if _, err := os.Stat(filepath.Join(dir, future)); err != nil {
		t.Errorf("%s(未来)不该被删: %v", future, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "not-a-date")); err != nil {
		t.Errorf("非日期目录不该被删: %v", err)
	}
}

// ── 4. compressFile 失败路径(M150)─────────────────────────────────────

// TestCompressFile_CopyFailed_KeepsSource 压缩中途失败必须保留原文件、清掉半成品
//
// # 背景(M150)
//
// 旧实现先 os.Remove(src) 再靠 defer 关闭且错误全吞:进程中途退出或磁盘满时,
// 原件已删、.gz 缺尾部,日志永久丢失。现在任一步失败都要:保留 src、删 .gz。
//
// 通过包级注入点 compressCopy 让 Copy 在中途报错 —— 旧实现在这条路径上
// 会留下 .gz 半成品(新实现必须清掉),所以这个用例同时是可逆探针。
func TestCompressFile_CopyFailed_KeepsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "info.2026-11-02-1.log")
	content := []byte(strings.Repeat("slow query log line\n", 100))
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("造源文件失败: %v", err)
	}

	// 写了一半就报错(模拟磁盘满 / 进程被杀前的残局)
	orig := compressCopy
	compressCopy = func(dst io.Writer, _ io.Reader) (int64, error) {
		if _, err := dst.Write([]byte("partial")); err != nil {
			return 0, err
		}
		return 0, errors.New("disk full")
	}
	t.Cleanup(func() { compressCopy = orig })

	compressFile(src)

	if _, err := os.Stat(src); err != nil {
		t.Errorf("原文件必须保留,却被删了: %v", err)
	}
	if _, err := os.Stat(src + ".gz"); !os.IsNotExist(err) {
		t.Error("半成品 .gz 必须被清掉(留着会误导解压)")
	}
}
