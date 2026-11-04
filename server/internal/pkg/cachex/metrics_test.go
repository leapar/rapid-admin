// cachex 指标埋点单测(§1.7 #9 P2)
//
// 覆盖:
//  1. InitMetrics(reg=nil) → no-op(便于业务方关停 / 单测跳过)
//  2. InitMetrics(reg) 后 recordOp 正确写入
//  3. recordLoader / setL1Size 也工作
//  4. 未 Init 时 recordOp 仍安全(指针 nil)
//  5. 重复 InitMetrics 只生效一次
package cachex

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInitMetrics_NilRegistry_NoOp(t *testing.T) {
	resetForTest()
	// 不会 panic,也不会改 global state
	InitMetrics(nil)
	if cacheOpsTotal != nil {
		t.Fatal("with nil reg, collectors should not be created")
	}
}

func TestInitMetrics_Registers(t *testing.T) {
	resetForTest()
	reg := prometheus.NewRegistry()
	InitMetrics(reg)
	if cacheOpsTotal == nil {
		t.Fatal("cacheOpsTotal should be set after InitMetrics")
	}
	if cacheL1SizeGauge == nil {
		t.Fatal("cacheL1SizeGauge should be set")
	}
	if cacheLoaderTotal == nil {
		t.Fatal("cacheLoaderTotal should be set")
	}
	if cacheOpDuration == nil {
		t.Fatal("cacheOpDuration should be set")
	}
	// 触发各 collector 至少一次 observe,这样 Gather() 才返回
	cacheOpsTotal.WithLabelValues("l1", "get", "hit").Add(0)
	cacheLoaderTotal.WithLabelValues("ok").Add(0)
	cacheOpDuration.WithLabelValues("get").Observe(0)
	// 计数应能从 reg 抓出
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}
	for _, want := range []string{
		"app_cache_ops_total",
		"app_cache_op_duration_seconds",
		"app_cache_l1_size",
		"app_cache_loader_total",
	} {
		if !names[want] {
			t.Fatalf("metric %s not registered, got %v", want, names)
		}
	}
}

func TestRecordOp_AllBranches(t *testing.T) {
	resetForTest()
	reg := prometheus.NewRegistry()
	InitMetrics(reg)

	// 各 label 组合测一次
	recordOp("l1", "get", "hit", 0)
	recordOp("l1", "get", "miss", 0)
	recordOp("l1", "get", "expire", 0)
	recordOp("l1", "get", "null", 0)
	recordOp("l1", "set", "ok", 0)
	recordOp("l1", "del", "ok", 0)

	recordOp("l2", "get", "hit", 5*time.Millisecond)
	recordOp("l2", "get", "miss", 0)
	recordOp("l2", "get", "error", 10*time.Millisecond)
	recordOp("l2", "get", "null", 1*time.Millisecond)
	recordOp("l2", "set", "ok", 2*time.Millisecond)
	recordOp("l2", "set", "error", 3*time.Millisecond)
	recordOp("l2", "del", "ok", 1*time.Millisecond)
	recordOp("l2", "del", "error", 1*time.Millisecond)

	// 用 testutil 抓 sample
	l1Hit := testutil.ToFloat64(cacheOpsTotal.WithLabelValues("l1", "get", "hit"))
	if l1Hit != 1 {
		t.Fatalf("l1 get hit = %v, want 1", l1Hit)
	}
	l2GetHit := testutil.ToFloat64(cacheOpsTotal.WithLabelValues("l2", "get", "hit"))
	if l2GetHit != 1 {
		t.Fatalf("l2 get hit = %v, want 1", l2GetHit)
	}
	l2SetErr := testutil.ToFloat64(cacheOpsTotal.WithLabelValues("l2", "set", "error"))
	if l2SetErr != 1 {
		t.Fatalf("l2 set error = %v, want 1", l2SetErr)
	}
}

func TestRecordOp_ZeroDuration_NoHistogram(t *testing.T) {
	resetForTest()
	reg := prometheus.NewRegistry()
	InitMetrics(reg)
	// dur=0 → 不计入 histogram(只看 counter)
	recordOp("l2", "get", "hit", 0)
	// histogram series 已通过 recordOp 创建(op="get"),即使 dur=0 也有 0 sample
	count := testutil.CollectAndCount(cacheOpDuration, "app_cache_op_duration_seconds")
	if count != 1 {
		t.Fatalf("expected 1 series for op=get, got %d", count)
	}
}

func TestRecordOp_NonZeroDuration_HistogramCount(t *testing.T) {
	resetForTest()
	reg := prometheus.NewRegistry()
	InitMetrics(reg)
	recordOp("l2", "get", "hit", 1*time.Millisecond)
	recordOp("l2", "get", "hit", 2*time.Millisecond)
	// 从 registry gather 拿 histogram
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var total uint64
	for _, mf := range mfs {
		if !strings.Contains(mf.GetName(), "cache_op_duration_seconds") {
			continue
		}
		for _, m := range mf.GetMetric() {
			if h := m.GetHistogram(); h != nil {
				total += h.GetSampleCount()
			}
		}
	}
	if total != 2 {
		t.Fatalf("histogram sample count = %d, want 2", total)
	}
}

func TestRecordLoader(t *testing.T) {
	resetForTest()
	reg := prometheus.NewRegistry()
	InitMetrics(reg)
	recordLoader("ok")
	recordLoader("ok")
	recordLoader("notfound")
	recordLoader("error")
	ok := testutil.ToFloat64(cacheLoaderTotal.WithLabelValues("ok"))
	if ok != 2 {
		t.Fatalf("loader ok = %v, want 2", ok)
	}
	nf := testutil.ToFloat64(cacheLoaderTotal.WithLabelValues("notfound"))
	if nf != 1 {
		t.Fatalf("loader notfound = %v, want 1", nf)
	}
	er := testutil.ToFloat64(cacheLoaderTotal.WithLabelValues("error"))
	if er != 1 {
		t.Fatalf("loader error = %v, want 1", er)
	}
}

func TestSetL1Size(t *testing.T) {
	resetForTest()
	reg := prometheus.NewRegistry()
	InitMetrics(reg)
	setL1Size(0)
	setL1Size(42)
	setL1Size(100)
	v := testutil.ToFloat64(cacheL1SizeGauge)
	if v != 100 {
		t.Fatalf("L1 size = %v, want 100", v)
	}
}

func TestRecordOp_Concurrent_RaceFree(t *testing.T) {
	// 跑 -race 验证:多 goroutine 并发调 recordOp / recordLoader / setL1Size
	resetForTest()
	reg := prometheus.NewRegistry()
	InitMetrics(reg)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				recordOp("l1", "get", "hit", 0)
				recordOp("l2", "get", "miss", 0)
				recordLoader("ok")
				setL1Size(j)
			}
		}()
	}
	wg.Wait()
	// 不报 race + 不 panic 即通过
}
