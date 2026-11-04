// 自定义 TimeRotator —— 按天分目录 + 按级别分文件 + 按大小滚动
// 文档 §10.2
package logger

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// TimeRotator 按天分目录,按级别分文件,按大小滚动
type TimeRotator struct {
	baseDir     string
	level       string
	mu          sync.Mutex
	currentDay  string
	currentFile *os.File
	currentSize int64
	maxSize     int64 // bytes
	maxBackups  int
	maxAge      int // days
	compress    bool
}

func newTimeRotator(baseDir, level string, maxSize int64, maxBackups, maxAge int, compress bool) *TimeRotator {
	return &TimeRotator{
		baseDir:    baseDir,
		level:      level,
		maxSize:    maxSize,
		maxBackups: maxBackups,
		maxAge:     maxAge,
		compress:   compress,
	}
}

// Write 实现 io.Writer
func (r *TimeRotator) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	// ⚠️ M99(2026-10-26)补:`|| r.currentFile == nil` 不能省。
	//
	// 原判据只有 `r.currentDay != today`,而 `rotateSize()` 在"重开文件失败"
	// 这条路径上会留下 **currentFile == nil 但 currentDay 仍是今天** 的状态:
	//
	//	rotateSize(): Close → currentFile = nil → Rename → OpenFile 失败 → return err
	//	             (currentDay 没动、currentSize 归零)
	//
	// 下一次 Write 进来时 `currentDay == today` → **跳过 rotate** →
	// 后面直接 `r.currentFile.Write(p)`。
	//
	// ⚠️ 这里**不是空指针 panic**:`os.File` 的方法对 nil 接收者有保护 ——
	// `File.Write` 先 `f.checkValid("write")`,而 checkValid 里 `if f == nil
	// { return ErrInvalid }` → 返回 `invalid argument`。
	//
	// 后果比 panic 更阴险:**日志从此全部丢失**。
	// 每次写都返 ErrInvalid,zap 把它当成 WriteSyncer 错误丢给 errorOutput,
	// 业务侧**一条日志都看不到**,而且没有任何告警。
	//
	// 触发条件是文件系统故障(磁盘满 / 目录被删 / 权限变更),平时看不到,
	// 一旦触发就是"静默丢日志",现场极难查。
	// 这里把 currentFile == nil 也当成"需要重新开文件",rotate 会自愈。
	if r.currentDay != today || r.currentFile == nil {
		if err := r.rotate(today); err != nil {
			return 0, err
		}
	}
	// 跨大小阈值时也滚动
	if r.currentSize+int64(len(p)) >= r.maxSize && r.currentFile != nil {
		if err := r.rotateSize(); err != nil {
			return 0, err
		}
	}
	n, err = r.currentFile.Write(p)
	r.currentSize += int64(n)
	return n, err
}

func (r *TimeRotator) rotate(today string) error {
	// 1. 关闭旧文件
	if r.currentFile != nil {
		old := r.currentFile
		oldName := old.Name()
		_ = old.Close()
		r.currentFile = nil
		r.currentSize = 0
		// 2. gzip 压缩
		if r.compress {
			go compressFile(oldName)
		}
		// 3. 清理
		r.cleanup()
	}
	// 4. 创建新目录 + 文件
	dir := filepath.Join(r.baseDir, today)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	filename := filepath.Join(dir, r.level+".log")
	f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	r.currentFile = f
	r.currentDay = today
	info, _ := f.Stat()
	if info != nil {
		r.currentSize = info.Size()
	}
	return nil
}

// rotateSize 大小滚动:不切目录,把当前文件重命名为 .1.log
func (r *TimeRotator) rotateSize() error {
	if r.currentFile == nil {
		return nil
	}
	oldName := r.currentFile.Name()
	_ = r.currentFile.Close()
	r.currentFile = nil
	r.currentSize = 0
	// 文件名加时间戳避免覆盖
	ts := time.Now().Format("150405.000")
	backupName := strings.TrimSuffix(oldName, ".log") + "." + ts + ".log"
	_ = os.Rename(oldName, backupName)
	// 压缩
	if r.compress {
		go compressFile(backupName)
	}
	// 重建
	f, err := os.OpenFile(oldName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	r.currentFile = f
	return nil
}

// dateLayout 日期目录名布局
const dateLayout = "2006-01-02"

// isDateDirName 目录名是否是合法的日期目录(YYYY-MM-DD)
func isDateDirName(name string) bool {
	_, err := time.Parse(dateLayout, name)
	return err == nil
}

// expiredDir 判断日期目录是否过期
//
// # 规则(纯函数,不看当前时间)
//
//   - 名字不是合法日期目录 → **不删**(别的子目录不能被误伤)
//   - 名字 <= cutoff(YYYY-MM-DD 字典序,等价于时间先后)→ 删
//
// # 为什么用日期串比较而不是解析成 time.Time 再比
//
// `time.Parse("2006-01-02", ...)` 对**只有日期**的布局按 **UTC** 解析,
// 而 cutoff 来自本地时间 —— 两边时区不一致,边界行为会随"当天几点"漂移。
// 目录名与 cutoff 都用本地日历天,且 ISO 日期串定长 → 字典序即时间序,
// 规则与时区、与运行时刻都无关。
//
// cutoff 由调用方算好传进来(而不是函数内部取 now)是为了让这条规则
// 可以被确定性地测。
func expiredDir(name, cutoff string) bool {
	if !isDateDirName(name) {
		return false
	}
	return name <= cutoff
}

// cleanup 清理过期目录(> maxAge 天) + 同目录历史文件按 maxBackups 保留
func (r *TimeRotator) cleanup() {
	// ⚠️ M99(2026-10-26)改:过期判定从"瞬时比较"改成**日期串比较**。
	//
	// 原实现:`time.Parse("2006-01-02", e.Name())` 把目录名按 **UTC** 解析,
	// 再和 `time.Now().AddDate(0, 0, -r.maxAge)`(**本地时间**)比大小。
	// 两边时区不一致 → 边界上"删不删"取决于当天几点,最多飘 14 小时
	// (UTC+8 东八区 vs UTC-6 美西),同一份配置在两地表现不同。
	//
	// 现在的语义(见 expiredDir):**按本地日历天**保留最近 maxAge 天 ——
	// 目录名 <= cutoff(今天 - maxAge 天)即删除。
	// 换句话说:今天、昨天、……、前 (maxAge-1) 天保留,再往前的删掉。
	//
	// 判定规则抽成纯函数 expiredDir(不碰 time.Now),这样规则本身
	// 可以被穷举测试 —— 见 logger/rotator_test.go。
	cutoff := time.Now().AddDate(0, 0, -r.maxAge).Format(dateLayout)
	entries, _ := os.ReadDir(r.baseDir)
	// 1. 过期目录
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if expiredDir(e.Name(), cutoff) {
			_ = os.RemoveAll(filepath.Join(r.baseDir, e.Name()))
		}
	}
	// 2. 当前目录下保留 maxBackups 个备份
	if r.maxBackups <= 0 {
		return
	}
	if r.currentDay == "" {
		return
	}
	cur := filepath.Join(r.baseDir, r.currentDay)
	files, _ := os.ReadDir(cur)
	var backups []string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if strings.HasPrefix(name, r.level+".") && strings.HasSuffix(name, ".log") {
			backups = append(backups, filepath.Join(cur, name))
		}
	}
	if len(backups) <= r.maxBackups {
		return
	}
	// 按 mtime 排序,删最早的
	type fi struct {
		path  string
		mtime time.Time
	}
	infos := make([]fi, 0, len(backups))
	for _, p := range backups {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		infos = append(infos, fi{p, st.ModTime()})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].mtime.Before(infos[j].mtime) })
	for i := 0; i < len(infos)-r.maxBackups; i++ {
		_ = os.Remove(infos[i].path)
	}
}

// Close 关闭当前文件
//
// 关掉之后 rotator 仍可继续用:下一次 Write 会因为 currentFile == nil
// 重新 rotate 打开(见 Write 里的判据)。
func (r *TimeRotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentFile == nil {
		return nil
	}
	err := r.currentFile.Close()
	r.currentFile = nil
	r.currentSize = 0
	return err
}

// Sync 刷盘
func (r *TimeRotator) Sync() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentFile != nil {
		return r.currentFile.Sync()
	}
	return nil
}

// compressCopy 测试注入点:单测借它让 Copy 在中途报错
var compressCopy = io.Copy

func compressFile(src string) {
	dst := src + ".gz"
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	gz := gzip.NewWriter(out)
	// ⚠️ 顺序必须是 Copy → gz.Close → out.Close → Remove,且每步错误都要查:
	// gzip 的尾部(CRC32 + 长度)在 gz.Close() 里才写,数据在 out.Close() 才落盘。
	// 旧实现先 os.Remove(src) 再靠 defer 关闭且错误全吞 —— 进程中途退出或
	// 磁盘满时,原件已删、.gz 缺尾部,日志永久丢失。
	// 任一步失败:删掉半成品 .gz(留着会误导解压)、保留原文件,下次 rotate 重试。
	fail := func(step string, err error) {
		_ = gz.Close()
		_ = out.Close()
		_ = os.Remove(dst)
		L().Warn("compressFile failed, keep original file",
			zap.String("step", step),
			zap.String("file", src),
			zap.Error(err))
	}
	if _, err := compressCopy(gz, in); err != nil {
		fail("copy", err)
		return
	}
	if err := gz.Close(); err != nil {
		fail("gzip close", err)
		return
	}
	if err := out.Close(); err != nil {
		fail("file close", err)
		return
	}
	_ = os.Remove(src)
}

// (M99 删掉了 baseName —— 全仓零引用的私有死函数)
