// 三防的真路径测试(2026-10-25 M90)
//
// # 为什么这个文件以前不存在
//
// cachex 写完之后,**真正的缓存路径一行都没被测过**。
// 原因很具体:老的测试只能塞一个指向 127.0.0.1:6379 的 *redis.Client,
// 而本机没有 Redis —— 于是每次读都走 "连接失败 → 降级回源" 那条分支。
// 测的是降级,不是缓存。singleflight / null 占位 / TTL 抖动三样,一样都没被验过。
//
// M90 把存储后端抽象成 Backend 接口之后,真路径终于可测:
// 用 MemoryBackend(或本文件里的 recordBackend)当后端,三防逐条验。
package cachex

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordBackend 记录每次 Set 的 TTL / 次数,并可注入故障
type recordBackend struct {
	*MemoryBackend
	mu     sync.Mutex
	ttls   []time.Duration
	sets   int
	getErr error
	setErr error
}

func newRecordBackend() *recordBackend {
	return &recordBackend{MemoryBackend: NewMemoryBackend()}
}

func (r *recordBackend) Get(ctx context.Context, k string) (string, error) {
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.MemoryBackend.Get(ctx, k)
}

func (r *recordBackend) Set(ctx context.Context, k, v string, ttl time.Duration) error {
	r.mu.Lock()
	r.ttls = append(r.ttls, ttl)
	r.sets++
	r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	return r.MemoryBackend.Set(ctx, k, v, ttl)
}

func (r *recordBackend) setTTLs() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Duration, len(r.ttls))
	copy(out, r.ttls)
	return out
}

func (r *recordBackend) setCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sets
}

// bigID 超过 2^53 的 uint64 —— float64 的整数精确上限是 2^53
const bigID uint64 = 1<<60 + 12345

type idHolder struct {
	ID    uint64 `json:"id"`
	Label string `json:"label"`
}

// TestGetOrLoadL2_MissThenHit 基本路径:第一次回源,第二次命中,loader 只跑一次
func TestGetOrLoadL2_MissThenHit(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		return idHolder{ID: 7, Label: "甲"}, nil
	}

	v1, hit1, err := GetOrLoadTypedL2(ctx, s, "k:1", time.Minute, loader)
	if err != nil || hit1 {
		t.Fatalf("首次: hit=%v err=%v", hit1, err)
	}
	if v1.ID != 7 {
		t.Fatalf("首次取值 = %+v", v1)
	}

	v2, hit2, err := GetOrLoadTypedL2(ctx, s, "k:1", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	if !hit2 {
		t.Fatal("第二次应命中缓存")
	}
	if v2 != v1 {
		t.Fatalf("命中值 = %+v, want %+v", v2, v1)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader 只该跑一次,got %d", calls.Load())
	}
}

// TestGetOrLoadL2_NullSentinel 穿透防御:查无此数据要写 null 占位,挡住后续流量
//
// 这条是三防里**唯一一条会让"结果看起来一样"的**防御(第二次也返零值),
// 所以最容易在重构中被悄悄弄丢 —— 用 loader 调用次数来钉死它。
func TestGetOrLoadL2_NullSentinel(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		return idHolder{}, ErrNotFound
	}

	v1, hit1, err := GetOrLoadTypedL2(ctx, s, "k:none", time.Minute, loader)
	if err != nil {
		t.Fatalf("ErrNotFound 不该冒泡成 error,got %v", err)
	}
	if v1 != (idHolder{}) {
		t.Fatalf("应返零值,got %+v", v1)
	}
	if hit1 {
		t.Fatal("首次回源不该算命中")
	}

	// 第二次:应该命中 null 占位,loader 不再跑
	v2, hit2, err := GetOrLoadTypedL2(ctx, s, "k:none", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	if !hit2 {
		t.Fatal("null 占位命中应算命中")
	}
	if v2 != (idHolder{}) {
		t.Fatalf("null 占位应返零值,got %+v", v2)
	}
	if calls.Load() != 1 {
		t.Fatalf("null 占位挡住后 loader 只该跑一次,got %d", calls.Load())
	}

	// 占位 TTL = nullTTL(2 分钟),且**不带抖动**(抖动是给真数据错峰用的)
	ttls := b.setTTLs()
	if len(ttls) != 1 {
		t.Fatalf("应只写 1 次,got %d", len(ttls))
	}
	if ttls[0] != DefaultNullTTL {
		t.Fatalf("null 占位 TTL = %v, want %v", ttls[0], DefaultNullTTL)
	}
}

// TestGetOrLoadL2_NotFoundNotCached_WhenNullTTLDisabled
//
// nullTTL=0 时不写占位 —— "查无此数据"每次都要回源。
// 这是显式配置(WithNullTTL(0)),不是 bug。
func TestGetOrLoadL2_NotFoundNotCached_WhenNullTTLDisabled(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b, WithNullTTL(0))

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		return idHolder{}, ErrNotFound
	}
	for i := 0; i < 3; i++ {
		if _, _, err := GetOrLoadTypedL2(ctx, s, "k:none", time.Minute, loader); err != nil {
			t.Fatalf("第 %d 次不该报错: %v", i, err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("nullTTL=0 时不该写占位,loader 该跑 3 次,got %d", calls.Load())
	}
}

// TestGetOrLoadL2_TTLJitter 雪崩防御:写入的 TTL 必须带 ±20% 抖动
//
// 断言两件事:
//  1. TTL 落在 [base*0.8, base*1.2] 区间内
//  2. TTL **不是恒等于 base** —— 否则抖动就是没生效(只是碰巧取了边界)
//
// 第 2 条的失败概率约 2^-53(只有 offset 恰好为 0 才会全等),不是不稳定用例。
func TestGetOrLoadL2_TTLJitter(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	const base = 100 * time.Second
	for i := 0; i < 20; i++ {
		key := "k:" + string(rune('a'+i))
		if _, _, err := GetOrLoadTypedL2(ctx, s, key, base,
			func(ctx context.Context) (idHolder, error) { return idHolder{ID: uint64(i)}, nil }); err != nil {
			t.Fatal(err)
		}
	}

	ttls := b.setTTLs()
	if len(ttls) != 20 {
		t.Fatalf("应写 20 次,got %d", len(ttls))
	}
	lo := time.Duration(float64(base) * (1 - DefaultJitterPct))
	hi := time.Duration(float64(base) * (1 + DefaultJitterPct))
	allEqual := true
	for i, ttl := range ttls {
		if ttl < lo || ttl > hi {
			t.Fatalf("第 %d 个 TTL = %v,超出 [%v, %v]", i, ttl, lo, hi)
		}
		if ttl != base {
			allEqual = false
		}
	}
	if allEqual {
		t.Fatal("所有 TTL 都恰好等于 base —— 抖动没生效")
	}
}

// TestGetOrLoadL2_Singleflight 击穿防御:同 key 并发只回源一次
func TestGetOrLoadL2_Singleflight(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		time.Sleep(30 * time.Millisecond) // 模拟慢查询
		return idHolder{ID: 99}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := GetOrLoadTypedL2(ctx, s, "k:hot", time.Minute, loader); err != nil {
				t.Errorf("并发调用报错: %v", err)
			}
		}()
	}
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("100 并发同 key 应只回源 1 次,got %d", calls.Load())
	}
}

// TestGetOrLoadL2_Uint64Precision uint64 超过 2^53 时不能掉精度
//
// 这是"泛型核心取代 any 往返"的**唯一**理由,所以必须钉死。
// 老实现(JSON → any(map[string]any) → JSON → T)会把整数变成 float64,
// 1<<60 会被写成 1.152921504606847e+18,再反序列化进 uint64 直接报错,
// 于是每次读都退回回源 —— 缓存形同虚设。
func TestGetOrLoadL2_Uint64Precision(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		return idHolder{ID: bigID, Label: "大 ID"}, nil
	}

	v1, _, err := GetOrLoadTypedL2(ctx, s, "k:big", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	if v1.ID != bigID {
		t.Fatalf("回源值 = %d, want %d", v1.ID, bigID)
	}

	v2, hit2, err := GetOrLoadTypedL2(ctx, s, "k:big", time.Minute, loader)
	if err != nil {
		t.Fatal(err)
	}
	if !hit2 {
		// 命中失败 = 缓存里的值反序列化不回来 = 精度已经丢了
		t.Fatal("第二次应命中缓存(反序列化失败会退回回源)")
	}
	if v2.ID != bigID {
		t.Fatalf("缓存往返后 ID = %d, want %d(精度丢了)", v2.ID, bigID)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader 只该跑一次,got %d", calls.Load())
	}
}

// TestGetOrLoadL2_BackendGetError_FailOpen 后端挂了要降级回源,不能把请求打挂
func TestGetOrLoadL2_BackendGetError_FailOpen(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	b.getErr = errors.New("redis down")
	s := NewWithBackend(b)

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		return idHolder{ID: 5}, nil
	}

	v, hit, err := GetOrLoadTypedL2(ctx, s, "k:1", time.Minute, loader)
	if err != nil {
		t.Fatalf("后端故障不该让业务失败,got %v", err)
	}
	if hit {
		t.Fatal("后端故障时不该算命中")
	}
	if v.ID != 5 {
		t.Fatalf("应降级回源拿到值,got %+v", v)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader 该跑一次,got %d", calls.Load())
	}
}

// TestGetOrLoadL2_BackendSetError_StillReturnsValue 写缓存失败不能影响返回值
func TestGetOrLoadL2_BackendSetError_StillReturnsValue(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	b.setErr = errors.New("write failed")
	s := NewWithBackend(b)

	v, hit, err := GetOrLoadTypedL2(ctx, s, "k:1", time.Minute,
		func(ctx context.Context) (idHolder, error) { return idHolder{ID: 3}, nil })
	if err != nil {
		t.Fatalf("写缓存失败不该冒泡,got %v", err)
	}
	if hit {
		t.Fatal("首次不该命中")
	}
	if v.ID != 3 {
		t.Fatalf("got %+v", v)
	}
}

// TestGetOrLoadL2_CorruptValue_Fallthrough 缓存里是脏数据时回源重来
//
// 发版改了结构、老 key 还在 —— 这时把错数据返给业务比慢一点糟糕得多。
func TestGetOrLoadL2_CorruptValue_Fallthrough(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	// 塞一段反序列化不回来的脏数据
	if err := b.MemoryBackend.Set(ctx, "k:dirty", "{不是合法 JSON", time.Minute); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		return idHolder{ID: 42}, nil
	}

	v, hit, err := GetOrLoadTypedL2(ctx, s, "k:dirty", time.Minute, loader)
	if err != nil {
		t.Fatalf("脏数据不该冒泡成 error,got %v", err)
	}
	if hit {
		t.Fatal("脏数据不该算命中")
	}
	if v.ID != 42 {
		t.Fatalf("应回源拿到正确值,got %+v", v)
	}
	if calls.Load() != 1 {
		t.Fatalf("loader 该跑一次,got %d", calls.Load())
	}
}

// TestGetOrLoadL2_NoBackend_Passthrough 没有后端时纯透传,语义不变
//
// "有没有缓存"不能改变函数的成功/失败语义 —— 这是 cache.mode=off 的契约。
func TestGetOrLoadL2_NoBackend_Passthrough(t *testing.T) {
	ctx := context.Background()
	for _, s := range []*Safe{nil, NewWithBackend(nil)} {
		var calls atomic.Int32
		loader := func(ctx context.Context) (idHolder, error) {
			calls.Add(1)
			return idHolder{ID: 1}, nil
		}
		for i := 0; i < 3; i++ {
			v, hit, err := GetOrLoadTypedL2(ctx, s, "k", time.Minute, loader)
			if err != nil || hit || v.ID != 1 {
				t.Fatalf("第 %d 次: v=%+v hit=%v err=%v", i, v, hit, err)
			}
		}
		if calls.Load() != 3 {
			t.Fatalf("无后端时每次都该回源,got %d", calls.Load())
		}
		// ErrNotFound 也要翻译成"零值无错"
		v, hit, err := GetOrLoadTypedL2(ctx, s, "k", time.Minute,
			func(ctx context.Context) (idHolder, error) { return idHolder{}, ErrNotFound })
		if err != nil || hit || v != (idHolder{}) {
			t.Fatalf("无后端 + ErrNotFound: v=%+v hit=%v err=%v", v, hit, err)
		}
	}
}

// TestGetOrLoadL2_KeyPrefixIsolated 前缀隔离:两个 Safe 不该串味
func TestGetOrLoadL2_KeyPrefixIsolated(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s1 := NewWithBackend(b, WithPrefix("a"))
	s2 := NewWithBackend(b, WithPrefix("b"))

	var calls atomic.Int32
	loader := func(ctx context.Context) (idHolder, error) {
		calls.Add(1)
		return idHolder{ID: uint64(calls.Load())}, nil
	}

	if _, _, err := GetOrLoadTypedL2(ctx, s1, "k", time.Minute, loader); err != nil {
		t.Fatal(err)
	}
	if _, _, err := GetOrLoadTypedL2(ctx, s2, "k", time.Minute, loader); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("不同前缀不该共用缓存条目,loader 跑了 %d 次", calls.Load())
	}
}

// TestGetOrLoadL2_LoaderError_Propagates 真错误必须冒泡,且不写缓存
func TestGetOrLoadL2_LoaderError_Propagates(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	boom := errors.New("db down")
	_, _, err := GetOrLoadTypedL2(ctx, s, "k:1", time.Minute,
		func(ctx context.Context) (idHolder, error) { return idHolder{}, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("真错误应原样冒泡,got %v", err)
	}
	if b.setCount() != 0 {
		t.Fatalf("真错误不该写缓存,writes=%d", b.setCount())
	}
}

// TestGetOrLoadL2_SafeGet_DirectNullSentinel hit 语义:直接读到的 null 占位算 miss
func TestGetOrLoadL2_SafeGet_DirectNullSentinel(t *testing.T) {
	ctx := context.Background()
	b := newRecordBackend()
	s := NewWithBackend(b)

	if err := b.MemoryBackend.Set(ctx, "k:none", NullSentinel, time.Minute); err != nil {
		t.Fatal(err)
	}
	var dest idHolder
	ok, err := s.Get(ctx, "k:none", &dest)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("null 占位不该算命中")
	}
}
