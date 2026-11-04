package idempotentx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// ── 本文件的来历 ───────────────────────────────────────────────────────
//
// 这个包之前**一张测试都没有**。而它站在**所有写接口**的路径上:
// router.go 里 `h.Use(middleware.Idempotency())` 是全局中间件,每一个
// POST/PUT/PATCH/DELETE 都会过它。它有三条真 bug(2026-10-26 M97 修的):
//
//   1. 结果 key 与锁 key 命名空间重叠(`idem:<k>` vs `idem:<k>:lock`)
//   2. AcquireOrGet 缺空 key 守卫(Save/Release 有,两边不对称)
//   3. 中间件把「业务没跑成」的 401 当成「跑过了」缓存 24h
//
// 第 3 条在 middleware 包里测(那是中间件的职责),本文件覆盖前两条 +
// 整个状态机(拿到锁 / 命中结果 / 并发在飞 / 释放锁 / Redis 故障降级)。
//
// ── M97(2026-10-26)可逆探针记录 ───────────────────────────────────────
//
//   拆法                                       必须变红
//   ────────────────────────────────────────  ─────────────────────────────────
//   P1 AcquireOrGet 去掉空 key 守卫             M97_EmptyKey_...
//   P2 结果 key 退回 "idem:"+k                 M97_KeyNamespacesAreDisjoint
//   P3 锁 key 退回 "idem:"+k+":lock"          M97_KeyNamespacesAreDisjoint
//   P4 Save 不删锁                             M97_Save_DeletesLock
//   P5 AcquireOrGet 命中结果时忽略缓存         M97_Replay_AfterSave
//   P6 Save 失败不写 StateFailed               M97_Save_FailureState
//   P7 rdb 为 nil 时不 fail-open               M97_NilRedis_FailOpen
//
//   7/7 全部变红。
// ───────────────────────────────────────────────────────────────────────

// newTestManager 造一个连着 miniredis 的 Manager
//
// miniredis 是**进程内真 Redis**(真 RESP 协议),不是 mock —— 走的是
// go-redis 的真命令路径。2026-10-26 M96 起它就是本仓的测试依赖。
func newTestManager(t *testing.T) (*Manager, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return New(rdb, 30*time.Second, 24*time.Hour), mr
}

// TestM97_Replay_AfterSave 跑完再拿 → 命中缓存的结果
//
// 这是幂等最核心的一条:同一个 key 的第二次请求必须拿到**第一次的结果**,
// 而不是再跑一遍业务。
func TestM97_Replay_AfterSave(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	acquired, res, err := m.AcquireOrGet(ctx, "k1")
	if !acquired || res != nil || err != nil {
		t.Fatalf("首次应拿到锁:acquired=%v res=%v err=%v", acquired, res, err)
	}
	m.Save(ctx, "k1", 201, []byte(`{"code":0,"data":{"id":7}}`))

	acquired, res, err = m.AcquireOrGet(ctx, "k1")
	if acquired || err != nil {
		t.Fatalf("命中结果应返 (false, res, nil):acquired=%v err=%v", acquired, err)
	}
	if res == nil {
		t.Fatal("命中结果时 res 不能为 nil")
	}
	if res.HTTPStatus != 201 {
		t.Fatalf("重放的 HTTP 状态应是 201,got %d", res.HTTPStatus)
	}
	if string(res.Body) != `{"code":0,"data":{"id":7}}` {
		t.Fatalf("重放的 body 不对:%q", string(res.Body))
	}
	if res.State != StateDone {
		t.Fatalf("2xx 的结果应是 done,got %q", res.State)
	}
}

// TestM97_InFlight_ReturnsErrInFlight 没跑完就再来 → 在飞
//
// 对应中间件返 409。前端看到 409 知道"等一下再试",而不是"失败了"。
func TestM97_InFlight_ReturnsErrInFlight(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	if acquired, _, err := m.AcquireOrGet(ctx, "k1"); !acquired || err != nil {
		t.Fatalf("首次应拿到锁:acquired=%v err=%v", acquired, err)
	}
	acquired, res, err := m.AcquireOrGet(ctx, "k1")
	if acquired || res != nil || !errors.Is(err, ErrInFlight) {
		t.Fatalf("并发同 key 应返 ErrInFlight:acquired=%v res=%v err=%v", acquired, res, err)
	}
}

// TestM97_Release_AllowsRetry 释放锁 → 同一个 key 能重来
//
// panic / ctx cancel 时中间件会调 Release:不释放的话这个 key 就被锁死到
// TTL 到期(30s),客户端拿同一个 key 重试只会拿到 409。
func TestM97_Release_AllowsRetry(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	if acquired, _, err := m.AcquireOrGet(ctx, "k1"); !acquired || err != nil {
		t.Fatalf("首次应拿到锁:acquired=%v err=%v", acquired, err)
	}
	m.Release(ctx, "k1")

	acquired, res, err := m.AcquireOrGet(ctx, "k1")
	if !acquired || res != nil || err != nil {
		t.Fatalf("释放后应能重新拿到锁:acquired=%v res=%v err=%v", acquired, res, err)
	}
}

// TestM97_EmptyKey_DoesNotLock 空 key 不能占锁
//
// ⚠️ **这是 M97 修的真 bug 之一**:Save/Release 都有 `idemKey == ""` 的守卫,
// AcquireOrGet **漏了**。于是:
//   - 空 key 会去抢 `idem:lock:` 这一把锁 —— 所有人共用
//   - 而 Save/Release 因为空 key 直接 return,**那把锁永远不会被释放**
//   - 于是之后每个空 key 的请求都拿到 ErrInFlight(409),直到 TTL 到期
//
// 旧代码的表现:第一次 acquired=true(看着正常),第二次 ErrInFlight ——
// 一条"看起来能跑通、实际把所有人锁死"的假象。
func TestM97_EmptyKey_DoesNotLock(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)

	acquired, res, err := m.AcquireOrGet(ctx, "")
	if !acquired || res != nil || err != nil {
		t.Fatalf("空 key 应直接放行:acquired=%v res=%v err=%v", acquired, res, err)
	}
	if mr.Exists(lockKey("")) {
		t.Fatalf("空 key 不该留下锁 %q", lockKey(""))
	}
	// 第二次仍然放行 —— 证明第一遍没把锁占住
	acquired, _, err = m.AcquireOrGet(ctx, "")
	if !acquired || err != nil {
		t.Fatalf("空 key 第二次应仍然放行(没被自己的锁挡住):acquired=%v err=%v", acquired, err)
	}
}

// TestM97_KeyNamespacesAreDisjoint 结果与锁必须分属两个命名空间
//
// ⚠️ **这是 M97 修的真 bug 之一**。旧实现是:
//
//	resultKey(k) = "idem:" + k
//	lockKey(k)   = "idem:" + k + ":lock"
//
// 两个命名空间**重叠**:客户端 key "foo:lock" 的**结果 key** 正好是客户端
// key "foo" 的**锁 key**。后果是两边互相破坏:
//
//	Save("foo")      → SET "idem:foo"      + DEL "idem:foo:lock"
//	Save("foo:lock") → SET "idem:foo:lock"  ← 覆盖了 "foo" 的锁!
//	Save("foo")      → DEL "idem:foo:lock"  ← 删掉了 "foo:lock" 的结果!
//
// 于是 "foo:lock" 的结果凭空消失 → 业务**重跑一遍**(幂等要防的正是这个),
// 而且没有任何报错。今天没炸过(前端 key 是 UUID,不含冒号),但键空间不是
// 单射就是**契约级的错**。
func TestM97_KeyNamespacesAreDisjoint(t *testing.T) {
	// 性质 1:任何 key 的结果 key 都不能等于任何 key 的锁 key
	keys := []string{"foo", "foo:lock", "lock:foo", "res:foo", "lock", ":", ""}
	for _, k := range keys {
		for _, j := range keys {
			if resultKey(k) == lockKey(j) {
				t.Fatalf("结果 key 与锁 key 撞了:resultKey(%q)==lockKey(%q)==%q",
					k, j, resultKey(k))
			}
		}
	}

	// 性质 2:实打实走一遍 —— "foo" 的保存不能毁掉 "foo:lock" 的结果
	ctx := context.Background()
	m, _ := newTestManager(t)

	m.Save(ctx, "foo", 200, []byte("A"))
	m.Save(ctx, "foo:lock", 201, []byte("B"))
	m.Save(ctx, "foo", 202, []byte("C")) // 这一步在旧实现里会 DEL 掉 B

	acquired, res, err := m.AcquireOrGet(ctx, "foo:lock")
	if acquired || err != nil {
		t.Fatalf("\"foo:lock\" 的结果应还在(被别的 key 的保存毁掉 = 业务会重跑):acquired=%v err=%v", acquired, err)
	}
	if res == nil || string(res.Body) != "B" {
		t.Fatalf("\"foo:lock\" 的结果应重放出 B,got %+v", res)
	}

	// "foo" 自己的结果是最后一次保存的 C
	_, res, err = m.AcquireOrGet(ctx, "foo")
	if err != nil || res == nil || string(res.Body) != "C" {
		t.Fatalf("\"foo\" 的结果应是 C,got %+v err=%v", res, err)
	}
}

// TestM97_Save_DeletesLock 保存结果时锁必须一起删
//
// 锁留着的话,业务跑完之后同一个 key 再来会拿到 409(而不是重放结果)。
func TestM97_Save_DeletesLock(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)

	if acquired, _, err := m.AcquireOrGet(ctx, "k1"); !acquired || err != nil {
		t.Fatalf("首次应拿到锁:acquired=%v err=%v", acquired, err)
	}
	if !mr.Exists(lockKey("k1")) {
		t.Fatal("前置条件:拿到锁后锁 key 应存在")
	}
	m.Save(ctx, "k1", 200, []byte("ok"))
	if mr.Exists(lockKey("k1")) {
		t.Fatal("Save 之后锁必须被删掉")
	}
}

// TestM97_Save_FailureState 业务失败的结果也要缓存(状态是 failed)
//
// 设计意图:业务 handler 失败也缓存,避免客户端重试 5 次每次都跑一遍业务。
func TestM97_Save_FailureState(t *testing.T) {
	ctx := context.Background()
	m, _ := newTestManager(t)

	m.Save(ctx, "ok", 200, []byte("x"))
	m.Save(ctx, "bad", 400, []byte(`{"code":40000}`))

	_, res, err := m.AcquireOrGet(ctx, "ok")
	if err != nil || res == nil || res.State != StateDone {
		t.Fatalf("2xx 应是 done:res=%+v err=%v", res, err)
	}
	_, res, err = m.AcquireOrGet(ctx, "bad")
	if err != nil || res == nil || res.State != StateFailed {
		t.Fatalf("4xx 应是 failed:res=%+v err=%v", res, err)
	}
	if res.HTTPStatus != 400 {
		t.Fatalf("failed 结果应保留原状态码,got %d", res.HTTPStatus)
	}
}

// TestM97_NilRedis_FailOpen 没配 Redis 时必须放行
//
// Redis 挂了不该把所有写操作都挡住(那比不幂等更糟)。Manager 的 rdb 为 nil
// 时,AcquireOrGet 返 acquired=true,Save/Release 是 no-op。
func TestM97_NilRedis_FailOpen(t *testing.T) {
	ctx := context.Background()
	m := New(nil, 0, 0)

	acquired, res, err := m.AcquireOrGet(ctx, "k1")
	if !acquired || res != nil || err != nil {
		t.Fatalf("无 Redis 应放行:acquired=%v res=%v err=%v", acquired, res, err)
	}
	// 这两个不能 panic
	m.Save(ctx, "k1", 200, []byte("x"))
	m.Release(ctx, "k1")

	var nilM *Manager
	if acquired, _, err := nilM.AcquireOrGet(ctx, "k1"); !acquired || err != nil {
		t.Fatalf("nil receiver 应放行:acquired=%v err=%v", acquired, err)
	}
}

// TestM97_RedisDown_FailOpen Redis 挂了就放行,别把写操作全挡住
//
// 降级方向和吊销那边**相反**:吊销查不到按"未吊销"放行(安全优先),
// 这里查不到按"放行"放行(可用性优先)—— Redis 抖一下就把全站写接口挡在
// 门外更糟。
func TestM97_RedisDown_FailOpen(t *testing.T) {
	ctx := context.Background()
	// 127.0.0.1:1 —— 没人监听,连接立刻被拒
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	m := New(rdb, 30*time.Second, 24*time.Hour)

	acquired, res, err := m.AcquireOrGet(ctx, "k1")
	if !acquired || res != nil || err != nil {
		t.Fatalf("Redis 挂了应放行:acquired=%v res=%v err=%v", acquired, res, err)
	}
}

// TestM97_TTLs 结果与锁的 TTL 各归各
//
// 锁要短(30s,免得业务卡住把 key 锁死),结果要长(24h,免得用户重试时
// 业务又跑一遍)。一个 key 做不到两套 TTL —— 这就是拆成两个 key 的原因。
func TestM97_TTLs(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	m := New(rdb, 30*time.Second, 120*time.Second)

	if acquired, _, err := m.AcquireOrGet(ctx, "k1"); !acquired || err != nil {
		t.Fatalf("首次应拿到锁:acquired=%v err=%v", acquired, err)
	}
	lockTTL := mr.TTL(lockKey("k1"))
	if lockTTL <= 0 || lockTTL > 30*time.Second {
		t.Fatalf("锁的 TTL 应在 (0, 30s],got %v", lockTTL)
	}

	m.Save(ctx, "k1", 200, []byte("x"))
	resTTL := mr.TTL(resultKey("k1"))
	if resTTL <= 0 || resTTL > 120*time.Second {
		t.Fatalf("结果的 TTL 应在 (0, 120s],got %v", resTTL)
	}
}

// TestM97_CorruptResult_TreatedAsMiss 结果被写坏 → 当作未命中,重新执行
//
// Redis 里的值被外部改坏(或者跨版本升级导致结构不兼容)时,不能把
// "解析失败"当成"命中"—— 那会把一个坏结果重放给用户。降级成"未命中"
// 重新执行,和 Redis 挂了的降级方向一致。
func TestM97_CorruptResult_TreatedAsMiss(t *testing.T) {
	ctx := context.Background()
	m, mr := newTestManager(t)

	mr.Set(resultKey("k1"), "not-json")
	acquired, res, err := m.AcquireOrGet(ctx, "k1")
	if !acquired || res != nil || err != nil {
		t.Fatalf("坏结果应当未命中(重新执行):acquired=%v res=%v err=%v", acquired, res, err)
	}
}
