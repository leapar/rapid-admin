package smsaudit

import "testing"

// TestShutdown_IsIdempotent Shutdown 必须幂等
//
// # 缺陷:第二次调用 panic("close of closed channel")
//
// 退出路径上"`defer smsaudit.Shutdown()` + 显式调一次"是很容易写出来的组合
// (先显式调一次把最后一批 flush 掉,再靠 defer 兜底)。第一次 close 之后,
// 第二次再 close 同一个 channel 就是 panic。
//
// 兄弟包 piiaudit / cryptaudit 在 M100 都已经改成幂等,本包漏了。
//
// 用例刻意**换掉包级 global**:这样不依赖测试执行顺序,
// 也不会把整包里其它用例依赖的 recorder 弄停。
func TestShutdown_IsIdempotent(t *testing.T) {
	db := auditTestDB(t)

	old := global
	rec := &Recorder{db: db, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
	go rec.loop()
	global = rec
	defer func() { global = old }()

	Shutdown()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("第二次 Shutdown panic 了: %v —— close 一个已关闭的 channel", r)
		}
	}()
	Shutdown()
}
