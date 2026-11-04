// otelx 包单测 —— §1.8(M99)
//
// # 这个包此前**零测试**,而它挂在全局路径上
//
// `otelx.Init` 干两件事:设全局 TracerProvider、设全局 propagator。
// 两者都是 `otel` 包的**进程级全局** —— 设错了会影响每一个请求的 span。
//
// # M99 在这里实证出的 1 个真 bug
//
//	**OTLP endpoint 带了 scheme 就全废**
//
//	`otlptracehttp.WithEndpoint` / `otlptracegrpc.WithEndpoint` 只接受
//	"host:port"(官方注释:"configures the trace host and port only")。
//	而 `internal/config/otel.go` 的注释写的是 "http: http://host:port" ——
//	运营照着填,exporter 的实际地址会变成:
//
//		http://http://collector:4318/v1/traces
//
//	**每次导出都失败**:span 被静默丢弃(业务侧毫无感知),
//	只有 stderr 里有一行报错。开了 otel 却一条 trace 都没有,现场极难查。
//
//	修法:`splitEndpoint` 统一剥掉 scheme;path 单独交给 WithURLPath。
//	走 http 还是 https 由 `insecure` 决定(一个开关说了算)。
//
// # M99(2026-10-26)可逆探针记录
//
//	拆法                                                        必须变红
//	────────────────────────────────────────────  ──────────────────────────────────
//	P15 http 分支改回 `WithEndpoint(endpoint)` 原样传   TestInit_HTTPExporter_ReachesCollector
//	P16 去掉 SetTextMapPropagator                      TestInit_SetsGlobalPropagator
//	P17 去掉 resolveServiceName 兜底                    TestResolveServiceName_Default
//
//	⚠️ **P15 必须走"真发一次 HTTP"才能验**:只断言"endpoint 被剥掉了 scheme"
//	是**验不到接线**的 —— 剥完不传进去照样坏。
//	所以这条测试起一个本地 httptest server 当 collector,
//	真的导一个 span 出去,断言 server 收到 "POST /v1/traces"。
package otelx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TestMain 把 OTel 的错误处理换成静默 ——
// 导出失败(collector 不存在)会走全局 error handler,默认往 stderr 打字,
// 把测试输出淹掉。
func TestMain(m *testing.M) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))
	os.Exit(m.Run())
}

// ── 1. 服务名兜底 ────────────────────────────────────────────────────────

// TestResolveServiceName_Default 空服务名要兜底
func TestResolveServiceName_Default(t *testing.T) {
	if got := resolveServiceName(""); got != defaultServiceName {
		t.Errorf("resolveServiceName(\"\") = %q,want %q", got, defaultServiceName)
	}
}

// TestResolveServiceName_KeepsGiven 给了名字就用自己的
func TestResolveServiceName_KeepsGiven(t *testing.T) {
	if got := resolveServiceName("my-svc"); got != "my-svc" {
		t.Errorf("resolveServiceName(%q) = %q,want %q", "my-svc", got, "my-svc")
	}
}

// ── 2. resource ─────────────────────────────────────────────────────────

// TestBuildResource_Attributes resource 上要带服务名和环境
func TestBuildResource_Attributes(t *testing.T) {
	res, err := buildResource(context.Background(), "m99-svc", "prod")
	if err != nil {
		t.Fatalf("buildResource 失败: %v", err)
	}
	attrs := map[string]string{}
	for _, kv := range res.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["service.name"] != "m99-svc" {
		t.Errorf("service.name = %q,want %q", attrs["service.name"], "m99-svc")
	}
	if attrs["deployment.environment.name"] != "prod" {
		t.Errorf("deployment.environment.name = %q,want %q", attrs["deployment.environment.name"], "prod")
	}
}

// ── 3. endpoint 拆分 ────────────────────────────────────────────────────

// TestSplitEndpoint_Rule endpoint 拆分规则
//
// 规则:
//
//   - "host:port"                    → ("host:port", "")
//   - "http://host:port"             → ("host:port", "")      ← 本次修的坑
//   - "https://host:port"            → ("host:port", "")
//   - "host:port/v1/traces"          → ("host:port", "/v1/traces")
//   - "http://host:port/v1/traces"   → ("host:port", "/v1/traces")
//   - 前后空白 / 查询串 / 片段        → 去掉
func TestSplitEndpoint_Rule(t *testing.T) {
	cases := []struct {
		in       string
		wantHost string
		wantPath string
	}{
		{"localhost:4317", "localhost:4317", ""},
		{"http://localhost:4318", "localhost:4318", ""},
		{"https://collector.example.com:4318", "collector.example.com:4318", ""},
		{"localhost:4318/v1/traces", "localhost:4318", "/v1/traces"},
		{"http://localhost:4318/v1/traces", "localhost:4318", "/v1/traces"},
		{"  localhost:4317  ", "localhost:4317", ""},
		{"localhost:4317?x=1", "localhost:4317", ""},
		{"localhost:4317#frag", "localhost:4317", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		host, path := splitEndpoint(tc.in)
		if host != tc.wantHost || path != tc.wantPath {
			t.Errorf("splitEndpoint(%q) = (%q, %q),want (%q, %q)",
				tc.in, host, path, tc.wantHost, tc.wantPath)
		}
	}
}

// ── 4. Init 的错误路径 ──────────────────────────────────────────────────

// TestInit_UnknownProtocol_ReturnsError 协议写错要报错
//
// 协议写错 → 报错 → main 里 log.Fatal → 进程起不来。
// 这是**故意的 fail-fast**:宁可起不来,也不要"otel 开着但一条 trace 都没有"。
func TestInit_UnknownProtocol_ReturnsError(t *testing.T) {
	shutdown, err := Init(context.Background(), "m99-svc", "test", "tcp", "localhost:4317", true, 1.0)
	if err == nil {
		t.Fatal("协议写成 tcp 应该报错,却返回 nil")
	}
	if shutdown != nil {
		t.Error("出错时不该返回 shutdown 函数")
	}
}

// TestInit_EmptyProtocol_DefaultsToGRPC 空协议按 grpc 处理
func TestInit_EmptyProtocol_DefaultsToGRPC(t *testing.T) {
	shutdown, err := Init(context.Background(), "m99-svc", "test", "", "localhost:4317", true, 1.0)
	if err != nil {
		t.Fatalf("空协议应该按 grpc 处理,却报错: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Logf("shutdown 返回(导出失败是预期的,collector 不存在): %v", err)
	}
}

// ── 5. 全局接线 ─────────────────────────────────────────────────────────

// TestInit_SetsGlobalPropagator Init 之后全局 propagator 要能解 W3C traceparent
//
// ⚠️ OTel 的全局 propagator 默认是**空实现**(什么都不做)。
// 不设它 → 上游传下来的 traceparent 全丢 → 每条 trace 都是新 root,
// 跨服务链路断成一段一段。
func TestInit_SetsGlobalPropagator(t *testing.T) {
	shutdown, err := Init(context.Background(), "m99-svc", "test", "grpc", "localhost:4317", true, 1.0)
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	carrier := propagation.MapCarrier{
		"traceparent": "00-" + traceID + "-00f067aa0ba902b7-01",
	}
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		t.Fatal("全局 propagator 解不出 traceparent —— SetTextMapPropagator 没生效")
	}
	if sc.TraceID().String() != traceID {
		t.Errorf("trace_id = %q,want %q", sc.TraceID().String(), traceID)
	}
	if !sc.IsSampled() {
		t.Error("traceparent 里的 sampled 标志没透传过来")
	}
}

// TestInit_SamplerFollowsSampleRatio 采样率要真的生效
//
// sample_ratio=1 → span 被采样(会被导出);
// sample_ratio=0 → 不采样(不导出,省 collector 的钱)。
//
// ⚠️ 这条测的是**采样语义**,不是"clamp 逻辑":
// OTel SDK 的 `TraceIDRatioBased` 自己就会把 >1 当 1、<0 当 0,
// 所以 otelx 里那个 clamp 与 SDK 的行为**完全等价** ——
// 拿它当探针是"拆了也不会红"的假绿,不能那么写。
func TestInit_SamplerFollowsSampleRatio(t *testing.T) {
	cases := []struct {
		ratio float64
		want  bool
	}{
		{1.0, true},
		{0.0, false},
	}
	for _, tc := range cases {
		shutdown, err := Init(context.Background(), "m99-svc", "test", "grpc", "localhost:4317", true, tc.ratio)
		if err != nil {
			t.Fatalf("Init(ratio=%v) 失败: %v", tc.ratio, err)
		}
		_, span := otel.Tracer("m99-test").Start(context.Background(), "probe")
		got := span.SpanContext().IsSampled()
		span.End()
		// ⚠️ 这里**故意不调 shutdown**:它会 ForceFlush,而 collector 不存在
		// → 每次导出都要等满 5s 超时,白等 5 秒。测的是采样标志,不需要真导出。
		_ = shutdown
		if got != tc.want {
			t.Errorf("sample_ratio=%v → sampled=%v,want %v", tc.ratio, got, tc.want)
		}
	}
}

// ── 6. 端到端:真的导出去 ───────────────────────────────────────────────

// TestInit_HTTPExporter_ReachesCollector 导出的 span 必须真的到达 collector
//
// # M99 修的那个 bug 的**决定性证据**
//
// 起一个本地 httptest server 当 OTLP collector,把它的 URL(带 scheme!)
// 交给 Init,起一个 span 再 shutdown(内部会 ForceFlush → 真的发一次 HTTP),
// 然后断言 server 收到了 "POST /v1/traces"。
//
// 旧实现把带 scheme 的 endpoint 原样丢给 WithEndpoint,
// 实际地址变成 "http://http://127.0.0.1:PORT/v1/traces"
// → 请求构造失败 → collector 一个请求都收不到 → 这里变红。
//
// ⚠️ 为什么必须"真的发一次":
// 只断言"endpoint 被剥掉了 scheme"验不到**接线** ——
// 剥完不传进 exporter 一样坏。端到端才验得到。
func TestInit_HTTPExporter_ReachesCollector(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	shutdown, err := Init(context.Background(), "m99-svc", "test", "http", srv.URL, true, 1.0)
	if err != nil {
		t.Fatalf("Init 失败: %v", err)
	}

	_, span := otel.Tracer("m99-test").Start(context.Background(), "probe")
	span.End()

	// Shutdown 内部会 ForceFlush → 触发一次真实导出
	if err := shutdown(context.Background()); err != nil {
		t.Logf("shutdown 返回(collector 回了 200 空 body,SDK 可能报错): %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("collector 没收到任何请求(endpoint=%q)—— 导出地址八成拼错了", srv.URL)
	}
	if got[0] != "POST /v1/traces" {
		t.Errorf("collector 收到 %q,want %q", got[0], "POST /v1/traces")
	}
}

// ── 7. InitNoop ─────────────────────────────────────────────────────────

// TestInitNoop 关闭开关时的 noop 关闭函数
//
// main 里 `otel.enabled=false` 走这条路,defer 仍会调它 ——
// 不能 panic,也不能报错。
func TestInitNoop(t *testing.T) {
	shutdown := InitNoop()
	if shutdown == nil {
		t.Fatal("InitNoop 返回 nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("noop shutdown 返回 %v,want nil", err)
	}
}
