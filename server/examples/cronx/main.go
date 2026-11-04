// cronx —— 定时任务调度
//
// ## 这个包解决什么问题
//
// 定时任务要处理两件事:代码侧注册(DB 里能改开关和表达式),运行侧调度
// (多副本下同一个任务只能有一个实例在跑)。
//
// ## 关键点
//
//   - **代码 AND DB**:最终生效 = `code.Enabled && DB.Enabled`。
//     做 AND 而不是只用 DB ⇒ **从代码里删掉任务一定等于停用**,
//     不会因为 DB 残留行而"复活"
//   - **时区必须显式配**:`0 3 * * *` 到底在哪跑,不能靠猜。
//     容器里 TZ 没设时 time.Local == UTC,任务会在北京时间 11:00 跑
//   - **分布式锁**:多副本时靠锁保证同一个任务只有一个实例在跑;
//     锁 TTL 短于任务耗时会出事 ⇒ 要么开锁续期(LockRenew),要么 TTL 给够
//   - **NonIdempotent 用反向命名**(零值 false = 幂等):
//     忘填的普通任务照常执行,只有显式声明非幂等的在降级模式下被跳过
//   - Handler **必须尊重 ctx**:ctx 到 deadline 会取消;
//     不检查 ctx 的任务会一直跑,而它的锁到期后会被第二个副本抢走,
//     两个副本并发写同一批数据
//
// ## 跑法
//
//	go run ./examples/cronx
package main

import (
	"context"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/cronx"
)

func main() {
	ctx := context.Background()

	// ── 一、cron 表达式 ─────────────────────────────────────────
	fmt.Println("=== cron 表达式 ===")
	for _, expr := range []string{"0 3 * * *", "*/5 * * * *", "0 0 1 * *", "@daily"} {
		sched, err := cronx.ParseCron(expr)
		if err != nil {
			fmt.Printf("  %-12q 解析失败: %v\n", expr, err)
			continue
		}
		runs, err := cronx.NextRuns(expr, 3, timeutilLoc())
		if err != nil {
			fmt.Printf("  %-12q 算下次时间失败: %v\n", expr, err)
			continue
		}
		fmt.Printf("  %-12q → %v\n", expr, times(runs))
		_ = sched
	}

	bad, err := cronx.ParseCron("不是 cron")
	fmt.Printf("  非法表达式 → err = %v\n", err != nil)
	_ = bad

	// ── 二、注册任务 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 注册任务 ===")
	cronx.Register(cronx.Job{
		Key:         "framework.demo_purge",
		Name:        "示例:清理过期数据",
		Description: "演示用的框架任务",
		Cron:        "0 3 * * *",
		Enabled:     true,
		Handler: func(ctx context.Context) error {
			// 真实任务在这里做清理
			return nil
		},
	})
	cronx.Register(cronx.Job{
		Key:           "framework.demo_report",
		Name:          "示例:生成报表",
		Cron:          "0 9 * * 1-5",
		Enabled:       true,
		NonIdempotent: true, // 跑两遍结果不一样
		Handler: func(ctx context.Context) error {
			return nil
		},
	})

	// ── 三、调度器(不接 DB 时用 nop store / nop locker)─────────
	//
	// 生产装配:
	//
	//	sched, err := cronx.New(cfg, cronx.NewStore(db), locker)
	//	cronx.SetDefault(sched)
	//	go sched.Start(ctx)
	sched, err := cronx.New(cronx.Config{
		Enabled:         true,
		TimeZone:        "Asia/Shanghai",
		LockMode:        "redis",
		LockTTL:         5 * time.Minute,
		DefaultTimeout:  10 * time.Minute,
		ShutdownTimeout: 30 * time.Second,
	}, nil, nil)
	if err != nil {
		fmt.Println("建调度器失败:", err)
		return
	}
	cronx.SetDefault(sched)

	list := sched.List()
	fmt.Printf("已注册 %d 个任务:\n", len(list))
	for _, e := range list {
		marker := " "
		if e.CronOverridden {
			marker = "*" // 生效值来自 DB 覆盖
		}
		fmt.Printf("  %s %-26s %-12s 生效=%v (代码=%v DB=%v)\n",
			marker, e.Key, e.CronExpr, e.EnabledEffective(), e.Enabled, e.DBEnabled)
	}
	fmt.Println("  (* = 生效值来自 DB 覆盖而非代码默认值)")

	// 单个任务的生效配置
	eff, err := sched.Get("framework.demo_purge")
	if err != nil {
		fmt.Println("  取任务失败:", err)
	} else {
		// ⚠️ 下次运行时间是零值 —— 因为调度器还没 Start,
		// 真正的 cron 条目还没注册进 robfig/cron。Start 之后才有值。
		fmt.Printf("  %s: 名称=%q 下次运行=%s(调度器未 Start 时是零值)\n",
			eff.Key, eff.Name, sched.NextRunAt(eff.Key).Format("2006-01-02 15:04:05"))
	}

	// ── 四、手动触发 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 手动触发 ===")
	run, err := sched.Trigger(ctx, "framework.demo_purge", cronx.TriggerManual)
	if err != nil {
		fmt.Println("触发失败:", err)
	} else {
		fmt.Printf("触发成功: 状态=%s 耗时=%dms 降级=%v\n", run.Status, run.DurationMs, run.Degraded)
		fmt.Println("  降级=true 是因为本示例没接 Redis 锁 —— 只有 Layer 0(进程内互斥)在挡,")
		fmt.Println("  多副本部署时**必须**配分布式锁,否则同一个任务会被多个副本同时跑。")
	}
}

// timeutilLoc 返回东八区(示例里直接用 time.FixedZone,免得多引一个包)
func timeutilLoc() *time.Location {
	return time.FixedZone("CST", 8*3600)
}

// times 把时间列表格式化
func times(ts []time.Time) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Format("01-02 15:04"))
	}
	return out
}
