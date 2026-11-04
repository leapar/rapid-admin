// OpenTelemetry TracerProvider 初始化
// 文档 §1.3 #13 + §1.8 #7 P0
package otelx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.uber.org/zap"
)

// ShutdownFunc 关闭 TracerProvider — main 用 defer 调,flush 残余 span
type ShutdownFunc func(context.Context) error

// defaultServiceName 服务名兜底
const defaultServiceName = "rapid-admin"

// noopShutdown enabled=false 时返 noop,main 仍 defer 调不会 panic
func noopShutdown(_ context.Context) error { return nil }

// resolveServiceName 服务名兜底:空 → defaultServiceName
//
// 抽成独立函数是为了能**确定性**测(见 otelx_test.go):
// 这条规则原先内联在 Init 里,而 resource 挂在 TracerProvider 内部,
// 外部拿不到 —— 只能靠"起一个 span 再去翻 span 的 resource"来观察,
// 那样测起来又重又不稳。
func resolveServiceName(name string) string {
	if name == "" {
		return defaultServiceName
	}
	return name
}

// buildResource 构造 trace resource
//
// 只放两个属性:service.name(服务标识)、deployment.environment.name(环境)。
//
// ⚠️ 刻意**不**带 resource.Default():那会把 host / process / telemetry.sdk.*
// 一起塞进来。要不要带由 collector 侧决定,这里保持最小集。
func buildResource(ctx context.Context, serviceName, env string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.DeploymentEnvironmentName(env),
		),
	)
}

// splitEndpoint 把 endpoint 拆成 (host:port, path)
//
// # ⚠️ M99(2026-10-26)修:OTLP 的 `WithEndpoint` **只接受 "host:port"**
//
// 官方注释原文:
// "configures the trace host and port only; endpoint should resemble
// "example.com" or "localhost:4317". To configure the scheme and path,
// use WithEndpointURL."
//
// 而 `internal/config/otel.go` 的注释写的是 "http: http://host:port" ——
// 运营照着填,exporter 的实际地址会变成
// "http://http://collector:4318/v1/traces",**每次导出都失败**:
// span 被静默丢弃(业务侧毫无感知),只有 stderr 里有一行报错。
//
// 这里统一处理:
//   - 剥掉 scheme(实际走 http 还是 https 由 `insecure` 决定,
//     **不由 endpoint 里写的字符串决定** —— 只有一个开关说了算)
//   - 拆出 path,交给 WithURLPath(不给就用默认的 /v1/traces)
func splitEndpoint(endpoint string) (hostPort, path string) {
	e := strings.TrimSpace(endpoint)
	if i := strings.Index(e, "://"); i >= 0 {
		e = e[i+3:]
	}
	if i := strings.IndexAny(e, "?#"); i >= 0 {
		e = e[:i]
	}
	if i := strings.Index(e, "/"); i >= 0 {
		return e[:i], e[i:]
	}
	return e, ""
}

// Init 初始化全局 TracerProvider
//   - cfg.Enabled = false → 直接返 noop,不发任何网络
//   - cfg.Protocol = "grpc"(默认)| "http"
//   - cfg.Insecure = true → 跳 TLS(内网 collector 常见)
//   - 失败:返回 error 但不 panic — main 决定降级还是 fail-fast
func Init(ctx context.Context, serviceName, env, protocol, endpoint string, insecure bool, sampleRatio float64) (ShutdownFunc, error) {
	serviceName = resolveServiceName(serviceName)

	// ---- Resource(服务标识) ----
	res, err := buildResource(ctx, serviceName, env)
	if err != nil {
		return nil, fmt.Errorf("otelx: build resource: %w", err)
	}

	// ---- Exporter ----
	var exporter sdktrace.SpanExporter
	switch protocol {
	case "http":
		hostPort, path := splitEndpoint(endpoint)
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(hostPort)}
		if path != "" {
			opts = append(opts, otlptracehttp.WithURLPath(path))
		}
		if insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	case "grpc", "":
		hostPort, _ := splitEndpoint(endpoint)
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(hostPort)}
		if insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err = otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	default:
		return nil, fmt.Errorf("otelx: unknown protocol %q (want grpc/http)", protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("otelx: build %s exporter: %w", protocol, err)
	}

	// ---- Sampler ----
	ratio := sampleRatio
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))

	// ---- TracerProvider ----
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	zap.L().Info("otelx: TracerProvider ready",
		zap.String("service", serviceName),
		zap.String("env", env),
		zap.String("protocol", protocol),
		zap.String("endpoint", endpoint),
		zap.Bool("insecure", insecure),
		zap.Float64("sample_ratio", ratio),
	)

	shutdown := func(ctx context.Context) error {
		// 5s flush 残余 span;再走 TracerProvider 关闭
		flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = tp.ForceFlush(flushCtx)
		return tp.Shutdown(ctx)
	}
	return shutdown, nil
}

// InitNoop enabled=false 时由 main 调,返 noop 关闭 — main 的 defer 仍可统一调用
func InitNoop() ShutdownFunc { return noopShutdown }
