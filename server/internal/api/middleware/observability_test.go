// 可观测性中间件的共享测试设施 —— §1.8(M99)
//
// # 这个包里此前**零可观测性测试**
//
// metrics / access_log / audit / otel 四个中间件全都挂在**全局请求链**上,
// 而它们的实现此前一条测试都没有 —— 见各测试文件里记的真 bug。
//
// # 本文件提供的东西
//
//   - TestMain:装好 logger(落临时目录)+ metricsx registry + 静音 hertz 路由日志
//   - obsScrapeMetrics:抓一次 /metrics,拿 Prometheus 真正能看到的东西
//   - obsFindSample:在 exposition 报文里按 label 集找样本
//   - obsReadLog / obsLogEntries:读日志文件
package middleware

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"

	appconfig "rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/metricsx"
)

// obsLogDir 测试用的日志目录(TestMain 建,退出时删)
var obsLogDir string

// TestMain 装好整个可观测性链路需要的全局设施
//
// ⚠️ 为什么必须有 TestMain:中间件依赖两个**包级全局**
// (`logger.Default` 与 metricsx 的 registry),而
//  1. logger 的 rotator 句柄是常开的 —— 每个用例各建一个 logger 的话,
//     Windows 上 t.TempDir() 删不掉(文件被占用)→ 用例报错;
//  2. metricsx.Init 是 sync.Once —— 只能在进程里做一次。
//
// 所以这里建**一套**,全程复用。
func TestMain(m *testing.M) {
	// hertz 默认打 Debug 级路由日志,会把测试输出淹掉
	hlog.SetLevel(hlog.LevelError)

	// metricsx 的 registry 是包级单例
	metricsx.Init(appconfig.MetricsConfig{
		Enabled:   true,
		Path:      "/metrics",
		Namespace: "app",
	}, "m99-test", "0.0.0-test", "test")

	dir, err := os.MkdirTemp("", "m99-obs-*")
	if err != nil {
		panic("创建测试日志目录失败: " + err.Error())
	}
	obsLogDir = dir

	// Env 用 "pre":不挂 stdout core(免得测试输出被日志淹没),也不强制 gzip
	log, err := logger.New(logger.Config{
		Level:      "info",
		Format:     "json",
		Dir:        dir,
		MaxSizeMB:  1,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Env:        "pre",
	})
	if err != nil {
		panic("创建测试 logger 失败: " + err.Error())
	}
	logger.Default = log

	code := m.Run()

	// 关句柄 → Windows 上才删得掉目录
	_ = logger.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// ── hertz 小工具 ────────────────────────────────────────────────────────

// obsNewEngine 建一个干净的 hertz engine
func obsNewEngine() *route.Engine {
	return route.NewEngine(config.NewOptions([]config.Option{}))
}

// ── Prometheus 报文 ─────────────────────────────────────────────────────

// obsScrapeMetrics 抓一次 /metrics,返回 exposition 文本
//
// ⚠️ 为什么不查 `Registry().Gather()`:那返回的是 `[]*dto.MetricFamily`,
// dto 来自 client_model —— 是个 **indirect 依赖**,直接用就要动 go.mod。
// 而 exposition 文本本来就是 Prometheus 实际看到的东西。
func obsScrapeMetrics(t *testing.T) string {
	t.Helper()
	e := obsNewEngine()
	e.GET("/metrics", metricsx.Handler())
	w := ut.PerformRequest(e, "GET", "/metrics", nil)
	if w.Code != 200 {
		t.Fatalf("/metrics 返 %d,want 200", w.Code)
	}
	return w.Body.String()
}

// obsFindSample 在 exposition 报文里找一个样本的值
//
// metric 是**完整**的样本名(含 _count / _sum 之类后缀),
// labels 是期望的 label 集(顺序无关)。
//
// 返回 (值, 是否找到)。
func obsFindSample(body, metric string, labels map[string]string) (float64, bool) {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, metric) {
			continue
		}
		rest := line[len(metric):]
		if rest == "" {
			continue
		}
		switch rest[0] {
		case '{':
			end := strings.Index(rest, "}")
			if end < 0 {
				continue
			}
			if !obsLabelsMatch(rest[1:end], labels) {
				continue
			}
			rest = rest[end+1:]
		case ' ':
			if len(labels) > 0 {
				continue
			}
		default:
			// 例如查 "app_http_response_size_bytes" 时撞上
			// "app_http_response_size_bytes_count{...}" —— 不是要的样本
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
}

// obsLabelsMatch 比对 label 集(顺序无关)
func obsLabelsMatch(part string, want map[string]string) bool {
	got := map[string]string{}
	for _, kv := range strings.Split(part, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		i := strings.Index(kv, "=")
		if i < 0 {
			return false
		}
		got[kv[:i]] = strings.Trim(kv[i+1:], `"`)
	}
	if len(got) != len(want) {
		return false
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

// obsHasMetricLabel 报文里有没有出现过某个 label 值
//
// 用来断言"原始路径没有进指标标签"(基数控制的底线)。
func obsHasMetricLabel(body, metric, label, value string) bool {
	needle := label + `="` + value + `"`
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, metric) {
			continue
		}
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// ── 日志文件 ────────────────────────────────────────────────────────────

// obsReadLog 读当天某个级别的日志文件全文
func obsReadLog(t *testing.T, level string) string {
	t.Helper()
	b, err := os.ReadFile(obsLogPath(level))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("读 %s 失败: %v", obsLogPath(level), err)
	}
	return string(b)
}

// obsLogPath 当天某个级别的日志文件路径
func obsLogPath(level string) string {
	return filepath.Join(obsLogDir, time.Now().Format("2006-01-02"), level+".log")
}

// obsLogEntries 解析日志文件里的每一行 JSON
func obsLogEntries(t *testing.T, level string) []map[string]any {
	t.Helper()
	content := obsReadLog(t, level)
	var out []map[string]any
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("日志行不是合法 JSON: %v (line=%q)", err, line)
		}
		out = append(out, m)
	}
	return out
}

// obsFindEntry 在日志条目里找一条 path 匹配的
//
// ⚠️ access 日志的字段叫 "path",审计日志的字段叫 "module" ——
// 所以审计那条走 obsFindEntryByField。
func obsFindEntry(entries []map[string]any, path string) map[string]any {
	return obsFindEntryByField(entries, "path", path)
}

// obsFindEntryByField 在日志条目里找某个字段值匹配的
func obsFindEntryByField(entries []map[string]any, field, value string) map[string]any {
	for _, e := range entries {
		if v, _ := e[field].(string); v == value {
			return e
		}
	}
	return nil
}
