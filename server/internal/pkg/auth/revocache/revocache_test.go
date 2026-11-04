// revocache 单测 —— 之前整包零覆盖。
// 这里只锁两件最要紧的事:过期条目会被 Add 写路径顺带清扫(sweepLocked)、
// 已过期的条目不进缓存。订阅广播链路需要真 Redis,归集成测,不在此处。
package revocache

import (
	"testing"
	"time"
)

func TestCache_Add_SweepsExpired(t *testing.T) {
	c := New(nil)
	c.Add("gone", time.Now().Add(5*time.Millisecond))
	time.Sleep(20 * time.Millisecond) // "gone" 已过期
	c.Add("alive", time.Now().Add(time.Hour))
	if got := c.Snapshot(); got != 1 {
		t.Fatalf("过期条目未被清扫: snapshot=%d, want 1", got)
	}
	if c.IsRevoked("gone") {
		t.Fatal("过期条目应已被删除")
	}
	if !c.IsRevoked("alive") {
		t.Fatal("未过期条目不应被误删")
	}
}

func TestCache_Add_ExpiredNotStored(t *testing.T) {
	c := New(nil)
	c.Add("past", time.Now().Add(-time.Second))
	if got := c.Snapshot(); got != 0 {
		t.Fatalf("过期条目不应入缓存: snapshot=%d, want 0", got)
	}
}
