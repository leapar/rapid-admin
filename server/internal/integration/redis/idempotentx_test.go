package redisint

import (
	"testing"
	"time"

	"rapid-admin/internal/pkg/idempotentx"
)

// idempotentx 的两把 key(结果 + 锁)在真 Redis 上的往返。
//
// ⚠️ 之前只有 miniredis 覆盖。这里的差别在于真 Redis 的
// **MULTI/EXEC 事务**与 **SETNX 的原子性**。
func TestIdempotentx_结果与锁的往返(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	m := idempotentx.New(rdb, 30*time.Second, 24*time.Hour)

	// 第一次:拿到锁
	acquired, result, err := m.AcquireOrGet(ctx, "key-1")
	if err != nil {
		t.Fatalf("AcquireOrGet: %v", err)
	}
	if !acquired {
		t.Fatal("第一次应拿到锁")
	}
	if result != nil {
		t.Fatal("第一次不该有缓存结果")
	}

	// 锁存在时:另一个请求拿到 ErrInFlight
	_, _, err = m.AcquireOrGet(ctx, "key-1")
	if err == nil {
		t.Fatal("锁被占时第二个请求应拿到 ErrInFlight")
	}

	// 业务跑完 → 写结果 + 删锁
	m.Save(ctx, "key-1", 200, []byte(`{"code":0}`))

	// 锁应当已被删掉
	if n, _ := rdb.Exists(ctx, "idem:lock:key-1").Result(); n != 0 {
		t.Fatal("Save 之后锁应当被删掉")
	}
	// 结果 key 应当在
	if n, _ := rdb.Exists(ctx, "idem:res:key-1").Result(); n != 1 {
		t.Fatal("Save 之后结果 key 应当存在")
	}

	// 重放:拿到缓存结果,不再拿锁
	acquired, result, err = m.AcquireOrGet(ctx, "key-1")
	if err != nil {
		t.Fatalf("AcquireOrGet: %v", err)
	}
	if acquired {
		t.Fatal("重放时不该拿到锁(业务会跑第二遍)")
	}
	if result == nil {
		t.Fatal("重放时应拿到缓存结果")
	}
	if result.HTTPStatus != 200 {
		t.Fatalf("重放结果的 HTTP 状态应为 200,got %d", result.HTTPStatus)
	}
	if string(result.Body) != `{"code":0}` {
		t.Fatalf("重放结果的 body 不对:%s", result.Body)
	}
}

// 键空间必须是单射:客户端 key "foo:lock" 不能撞上客户端 key "foo" 的锁。
//
// ⚠️ 这是 M97 修过的契约级 bug(旧实现用 `idem:<k>` / `idem:<k>:lock`)。
// 这条网守着它没退化。
func TestIdempotentx_键空间不重叠(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	m := idempotentx.New(rdb, 30*time.Second, 24*time.Hour)

	// "foo" 的请求跑完
	acquired, _, err := m.AcquireOrGet(ctx, "foo")
	if err != nil || !acquired {
		t.Fatalf("第一次应拿到锁:acquired=%v err=%v", acquired, err)
	}
	m.Save(ctx, "foo", 200, []byte(`{"n":1}`))

	// "foo:lock" 的请求:它的结果 key 与 "foo" 的锁 key 是**不同的 key**
	acquired, _, err = m.AcquireOrGet(ctx, "foo:lock")
	if err != nil {
		t.Fatalf("AcquireOrGet: %v", err)
	}
	if !acquired {
		t.Fatal("foo:lock 的请求应拿到自己的锁(没有被 foo 的结果占掉)")
	}

	// "foo" 的重放仍应命中它自己的结果
	acquired, result, err := m.AcquireOrGet(ctx, "foo")
	if err != nil {
		t.Fatalf("AcquireOrGet: %v", err)
	}
	if acquired || result == nil {
		t.Fatal("foo 的重放应命中缓存结果")
	}
}

// 空 key:两把 key 都会被 `idem:lock:` / `idem:res:` 前缀占住,
// 所以必须在入口就挡住,不能让所有人共用同一把锁。
func TestIdempotentx_空key直接放行(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)
	m := idempotentx.New(rdb, 30*time.Second, 24*time.Hour)

	acquired, result, err := m.AcquireOrGet(ctx, "")
	if err != nil {
		t.Fatalf("AcquireOrGet: %v", err)
	}
	if !acquired || result != nil {
		t.Fatal("空 key 应直接放行(不抢锁、不查结果)")
	}
}
