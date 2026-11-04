package redisint

import (
	"testing"
	"time"

	"rapid-admin/internal/pkg/cronx"
)

// cronx 的 Redis 分布式锁:两段 Lua(CAS 续期 / CAS 释放)。
//
// ⚠️ **这两段 Lua 从来没在真 Redis 上跑过** —— cronx 的单测只跑内存锁
// (`memoryLocker`),Redis 锁连 miniredis 都没有。
//
// 要守的性质(CAS 是这条路径上唯一的防线):
//
//	非 CAS 释放:A 跑超时 → 锁 TTL 到期 → B 抢到锁开始跑 → A 结束时裸 DEL
//	              → 把 B 的锁删了 → C 立刻抢到并发跑。
//
// 所以:
//  1. 只有持锁方能 Renew
//  2. 只有持锁方能 Unlock(别人的锁不能被删)
func TestCronx_redis锁的CAS续期与释放(t *testing.T) {
	rdb := realRedis(t)
	ctx := ctx5(t)

	locker, err := cronx.ResolveLocker(cronx.LockModeRedis, rdb, "job:lock:")
	if err != nil {
		t.Fatalf("ResolveLocker: %v", err)
	}
	if !locker.Available() {
		t.Fatal("lock_mode=redis 时锁应当可用")
	}

	const key = "task-1"
	const ownerA = "instance-a"
	const ownerB = "instance-b"

	// A 抢到锁
	ok, err := locker.TryLock(ctx, key, ownerA, 30*time.Second)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if !ok {
		t.Fatal("A 应抢到锁")
	}

	// B 抢不到
	ok, err = locker.TryLock(ctx, key, ownerB, 30*time.Second)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if ok {
		t.Fatal("B 不该抢到 A 持有的锁")
	}

	// A 续期成功
	ok, err = locker.Renew(ctx, key, ownerA, 30*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if !ok {
		t.Fatal("持锁方 Renew 应成功")
	}

	// B 续期失败(CAS 不通过)—— **B 不能把 A 的锁续上**
	ok, err = locker.Renew(ctx, key, ownerB, 30*time.Second)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}
	if ok {
		t.Fatal("非持锁方 Renew 不该成功")
	}

	// B 释放失败 —— **B 不能删掉 A 的锁**
	if err := locker.Unlock(ctx, key, ownerB); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	ok, err = locker.TryLock(ctx, key, ownerB, 30*time.Second)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if ok {
		t.Fatal("B 把 A 的锁删掉了 —— 释放不是 CAS,三副本会并发跑同一个任务")
	}

	// A 释放成功
	if err := locker.Unlock(ctx, key, ownerA); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	ok, err = locker.TryLock(ctx, key, ownerB, 30*time.Second)
	if err != nil {
		t.Fatalf("TryLock: %v", err)
	}
	if !ok {
		t.Fatal("A 释放之后 B 应能抢到锁")
	}
}

// 锁模式配错时的行为(与"要不要连 Redis"同一个决策点)。
func TestCronx_锁模式的决策点(t *testing.T) {
	rdb := realRedis(t)

	// lock_mode=redis 但没有 Redis → 报错(拒绝静默降级)
	if _, err := cronx.ResolveLocker(cronx.LockModeRedis, nil, "job:lock:"); err == nil {
		t.Fatal("lock_mode=redis 且无 Redis 时应报错")
	}
	// auto + 有 Redis → Redis 锁
	l, err := cronx.ResolveLocker(cronx.LockModeAuto, rdb, "job:lock:")
	if err != nil {
		t.Fatalf("ResolveLocker: %v", err)
	}
	if !l.Available() {
		t.Fatal("auto + 有 Redis 时锁应可用")
	}
	// auto + 无 Redis → 降级到进程内锁
	l, err = cronx.ResolveLocker(cronx.LockModeAuto, nil, "job:lock:")
	if err != nil {
		t.Fatalf("ResolveLocker: %v", err)
	}
	if !l.Available() {
		t.Fatal("auto 降级到内存锁时仍应可用(只是不跨实例)")
	}
}
