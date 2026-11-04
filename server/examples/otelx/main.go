// otelx —— OpenTelemetry TracerProvider 初始化
//
// ## 这个包解决什么问题
//
// 接上链路追踪的"起点":把 TracerProvider 建起来、把资源信息
// (服务名 / 环境 / 版本)挂上、把 trace context 的传播格式定下来。
//
// ## 关键点
//
//   - **enabled=false 时用 InitNoop**。main 的 defer 统一调 Shutdown,
//     noop 版本返 nil,调用方一行都不用分支
//   - **protocol 只有 grpc / http 两种**,写错直接报错(不会静默走默认)
//   - **sampleRatio 会被夹到 [0,1]**。配错成 5 或 -1 不会让采样器炸掉
//   - **shutdown 会先 ForceFlush 再 Shutdown** —— 直接 Shutdown 会丢掉
//     缓冲区里还没导出的 span
//   - exporter 是**懒连接**的:Init 成功不代表 OTLP 后端可达
//
// ## 跑法
//
//	go run ./examples/otelx
package main

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/otelx"
)

func main() {
	ctx := context.Background()

	// ── 一、noop 版(enabled=false)──────────────────────────────
	fmt.Println("=== noop ===")
	noopShutdown := otelx.InitNoop()
	if noopShutdown == nil {
		fmt.Println("InitNoop 返了 nil —— main 的 defer 会 panic")
		return
	}
	err := noopShutdown(ctx)
	fmt.Printf("noop shutdown → err = %v\n", err)
	fmt.Println("  ⇒ enabled=false 时用这个,main 的 defer 统一调,不用分支")

	// ── 二、protocol 写错 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== protocol 写错 ===")
	_, err = otelx.Init(ctx, "demo-service", "dev", "tcp", "localhost:4317", true, 1.0)
	fmt.Printf("protocol=\"tcp\" → err = %v\n", err)
	fmt.Println("  ⇒ 只有 grpc / http,写错直接报错,不静默走默认")

	// ── 三、正常初始化 ─────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 正常初始化 ===")
	shutdown, err := otelx.Init(ctx, "demo-service", "dev", "grpc", "localhost:4317", true, 1.0)
	if err != nil {
		fmt.Println("初始化失败:", err)
		return
	}
	fmt.Println("TracerProvider ready")
	fmt.Println("  ⇒ exporter 是懒连接的:这一步成功不代表 OTLP 后端可达")

	// ── 四、采样率夹取 ─────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 采样率夹取 ===")
	for _, ratio := range []float64{-1, 0, 0.5, 5} {
		sh, err := otelx.Init(ctx, "demo-service", "dev", "grpc", "localhost:4317", true, ratio)
		if err != nil {
			fmt.Printf("  ratio=%-4v → err = %v\n", ratio, err)
			continue
		}
		fmt.Printf("  ratio=%-4v → 生效(夹到 [0,1])\n", ratio)
		_ = sh(ctx)
	}

	// ── 五、关闭 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 关闭 ===")
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := shutdown(flushCtx); err != nil {
		fmt.Printf("shutdown → err = %v\n", err)
	} else {
		fmt.Println("shutdown OK")
	}
	fmt.Println("  ⇒ shutdown 内部先 ForceFlush 再 Shutdown,缓冲区里的 span 不会丢")
}
