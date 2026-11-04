package syncx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPool_SubmitAndExecute(t *testing.T) {
	p := NewWorkerPool(4)
	defer p.Close()
	var n atomic.Int32
	for i := 0; i < 100; i++ {
		p.Submit(func() { n.Add(1) })
	}
	// 等所有任务完成
	for i := 0; i < 1000; i++ {
		if n.Load() == 100 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if n.Load() != 100 {
		t.Fatalf("expected 100, got %d", n.Load())
	}
	stats := p.Stats()
	if stats.Executed != 100 {
		t.Fatalf("Executed = %d, want 100", stats.Executed)
	}
	if stats.Running != 0 {
		t.Fatalf("after drain, Running should be 0, got %d", stats.Running)
	}
}

func TestWorkerPool_SubmitAfterClose(t *testing.T) {
	p := NewWorkerPool(2)
	p.Close()
	if p.Submit(func() {}) {
		t.Fatal("Submit after Close should return false")
	}
	if p.Stats().Rejected != 1 {
		t.Fatalf("Rejected = %d, want 1", p.Stats().Rejected)
	}
}

func TestWorkerPool_SubmitCtx_Cancel(t *testing.T) {
	p := NewWorkerPool(1, 1) // 1 worker, 1 queue
	defer p.Close()
	// 填满队列
	p.Submit(func() { time.Sleep(50 * time.Millisecond) }) // 占住 worker
	p.Submit(func() {})                                     // 填满队列
	// SubmitCtx 应该被 ctx cancel
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if p.SubmitCtx(ctx, func() {}) {
		t.Fatal("SubmitCtx with full queue + ctx cancel should return false")
	}
}

func TestWorkerPool_QueueBlocking(t *testing.T) {
	// 1 worker, 1 queue:第一个任务占 worker,第二个占队列,第三个应阻塞
	p := NewWorkerPool(1, 1)
	defer p.Close()
	var done atomic.Int32
	p.Submit(func() {
		time.Sleep(20 * time.Millisecond)
		done.Add(1)
	})
	p.Submit(func() { done.Add(1) })
	// 第三个:队列满 + worker 忙 → 阻塞直到上面那个完
	p.Submit(func() { done.Add(1) })
	// 等所有完成
	for i := 0; i < 1000; i++ {
		if done.Load() == 3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if done.Load() != 3 {
		t.Fatalf("expected 3 done, got %d", done.Load())
	}
	if p.Stats().Blocked < 1 {
		t.Fatalf("expected Blocked >= 1, got %d", p.Stats().Blocked)
	}
}

func TestWorkerPool_PanicRecovered(t *testing.T) {
	p := NewWorkerPool(1)
	defer p.Close()
	p.Submit(func() { panic("boom") })
	time.Sleep(50 * time.Millisecond) // 等 worker 处理
	// 应能继续 Submit
	var done atomic.Int32
	if !p.Submit(func() { done.Add(1) }) {
		t.Fatal("Submit after panic should work")
	}
	for i := 0; i < 100; i++ {
		if done.Load() == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if done.Load() != 1 {
		t.Fatal("post-panic task didn't run")
	}
}

func TestWorkerPool_CloseGraceful(t *testing.T) {
	p := NewWorkerPool(2, 10)
	var done atomic.Int32
	for i := 0; i < 5; i++ {
		p.Submit(func() {
			time.Sleep(5 * time.Millisecond)
			done.Add(1)
		})
	}
	p.Close() // 等所有任务完成
	if done.Load() != 5 {
		t.Fatalf("expected 5, got %d", done.Load())
	}
	if !p.Stats().Closed {
		t.Fatal("should be closed")
	}
}

// TestWorkerPool_CloseNow_DiscardsPending CloseNow 之后,队列里没开始过的任务一个都不许跑。
//
// 这条用例原来写成"Submit 完立刻 CloseNow,然后断言 done == 1",于是稳定红(拿到 0):
// `Submit` 只是把任务放进 channel 就返回,**不保证 worker 已经取走**。worker 如果在
// CloseNow 之后才被调度,那次 select 看到 stop 与 tasks 同时 ready,随机挑了 stop,
// 任务就被丢掉了 —— 而"丢掉没开始过的任务"正是 CloseNow 的语义。
//
// 所以这里改成**等任务真的开始**再 CloseNow:要验的是"在跑的跑完、排队的不跑",
// 而不是"提交过的一定在跑"。上一条的"跑几个都算对"由
// TestWorkerPool_CloseNow_NeverRunsPending 兜底。
func TestWorkerPool_CloseNow_DiscardsPending(t *testing.T) {
	p := NewWorkerPool(1, 100)
	var done atomic.Int32
	started := make(chan struct{})
	// 占 worker 50ms
	p.Submit(func() { close(started); time.Sleep(50 * time.Millisecond); done.Add(1) })
	<-started // 确认它已经在跑,而不是还躺在队列里
	// 队列里堆 50 个
	for i := 0; i < 50; i++ {
		p.Submit(func() { done.Add(1) })
	}
	p.CloseNow() // 立即关,丢弃队列
	// 等 in-flight 完
	time.Sleep(100 * time.Millisecond)
	// done 应该是 1(in-flight 完成) + 0 队列任务
	if done.Load() != 1 {
		t.Fatalf("expected 1 (only in-flight), got %d", done.Load())
	}
}

// ── CloseNow 三条用例的可逆探针记录(2026-10-21)────────────────────────
//
// 规矩:改了这块实现,先按下面两张表把实现拆回去看用例会不会红。红不了就是用例
// 假绿,比没有用例更危险(会让人以为这块有网)。
//
//   拆法                                必须变红
//   ──────────────────────────────────  ──────────────────────────────────
//   S1 stopped 落回 wg.Wait() 之后(原 bug) TestWorkerPool_CloseNow_NeverRunsPending
//   S2 drainPending 不再扣 queued 计数      TestWorkerPool_CloseNow_QueuedCounter
//
//   S3 删掉"收到 task 之后那次 stopped 二次检查" —— **测不到,是已知的**
//
//   ⚠️ 第一版 S1 写错了,报了一次假 LEAK,差点反过来去改用例。它只把 stopped 和
//   close(stop) 两行对调:窗口缩到几纳秒,而 in-flight 任务 20ms 后才醒,那时
//   stopped 早就是 true 了,复现不了。**真原 bug 是 stopped 落在 wg.Wait() 之后**
//   —— 整个 in-flight 期间它都是 false。这才是 6 跑 5 红、拿到过 3 和 7 的窗口。
//   教训:探针要还原的是**当初那个版本**,不是"看起来差不多的错误版本"。
//
//   S3 之所以测不到:它守的是 pre-check 读到 false 之后、进 select 之前的几纳秒
//   缝。要命中它,得让 worker **停在 select 里**、同时有一个"已发出但还没被接收"
//   的任务 —— Go 里做不到。无缓冲 channel 的发送方会**立刻**把任务交给停着的
//   接收方(按这个形状写过一条用例,第二轮直接红在 done==1:第二个 worker 在
//   CloseNow **之前**就把任务拿走了,那是正确行为,是断言写错了);有缓冲 channel
//   则必须队列空,而队列空就意味着没有在途任务可争。
//
//   两道闸冗余但互补,所以 S3 那道**不能删**:pre-check 兜住"跑完长任务回到循环"
//   这个大窗口,二次检查兜住那几纳秒。真要删只能两个一起删,而那个变异就是 S1。
// ────────────────────────────────────────────────────────────────────────

// TestWorkerPool_CloseNow_NeverRunsPending 不预先同步,连跑 30 轮,任何一轮都不许多跑。
//
// CloseNow 与 worker 的 select 是**同时**发生的:`stop` 关闭时 tasks 里通常还有货,
// 而 Go 的 select 在两个 case 都 ready 时是**均匀随机**挑的,没有优先级。所以
// "随机拉到 task 之后又把它丢掉"这件事必须有一道显式的二次检查兜着。
//
// 修复前这里是 6 跑 5 次红,拿到过 3 和 7:CloseNow 把 stopped 落在 wg.Wait() 之后,
// 于是整个 in-flight 期间 stopped 都是 false,worker 跑完任务回到循环读到 false,
// 进 select 两边同时 ready 随机一抽就照跑。把置位挪到 close(p.stop) 之前就好了。
//
// ⚠️ **`<-started` 那一步不是可有可无的**,去掉它这条用例就变成永远绿的空跑:
// 51 次 Submit 只需要几微秒,worker goroutine 通常还没被调度过,于是 CloseNow 触发时
// 它还停在循环开头,读到 stopped 直接 drain 走人 —— done 是 0,而断言只查 "got > 1"。
// 只有先确认 worker 已经**正在跑**那个 20ms 的任务,CloseNow 落地时它才会真的撞上
// 那个竞态窗(跑完 task1 → pre-check → select 两边都 ready → 随机)。
// 这就是"删掉 started 信号它还绿吗"的答案:会绿,而且是假绿。
func TestWorkerPool_CloseNow_NeverRunsPending(t *testing.T) {
	const rounds = 30
	for r := 0; r < rounds; r++ {
		p := NewWorkerPool(1, 100)
		var done atomic.Int32
		started := make(chan struct{})
		p.Submit(func() { close(started); time.Sleep(20 * time.Millisecond); done.Add(1) })
		<-started // 必须等它真的**正在跑**才能复现那个竞态,理由见下
		for i := 0; i < 50; i++ {
			p.Submit(func() { done.Add(1) })
		}
		p.CloseNow()
		// CloseNow 返回时 worker 已经全部退出,这里不需要再 sleep
		if got := done.Load(); got > 1 {
			t.Fatalf("第 %d 轮:CloseNow 之后仍跑了 %d 个排队任务(最多只允许 1 个 in-flight)", r, got)
		}
	}
}

// TestWorkerPool_CloseNow_QueuedCounter归零 CloseNow 丢掉的任务也必须从 queued 里扣掉。
//
// 不扣的话 Stats().Queued 会在关闭之后永远停在一个假数字上 —— 池子都关了还报
// "队列里还有 50 个",排查的人会先怀疑别的地方。
func TestWorkerPool_CloseNow_QueuedCounter(t *testing.T) {
	p := NewWorkerPool(1, 100)
	var done atomic.Int32
	started := make(chan struct{})
	p.Submit(func() { close(started); time.Sleep(20 * time.Millisecond); done.Add(1) })
	<-started
	for i := 0; i < 50; i++ {
		p.Submit(func() { done.Add(1) })
	}
	if got := p.Stats().Queued; got != 50 {
		t.Fatalf("前置条件:queued 应为 50,got %d", got)
	}
	p.CloseNow()
	if got := p.Stats().Queued; got != 0 {
		t.Fatalf("CloseNow 之后 queued 应归零,got %d —— 被丢弃的任务没从计数里扣掉", got)
	}
	if got := p.Stats().Executed; got != 1 {
		t.Fatalf("Executed 应为 1(只算 in-flight),got %d", got)
	}
}

func TestSemaphore_BasicAcquireRelease(t *testing.T) {
	s := NewSemaphore(3)
	if s.Available() != 3 {
		t.Fatal("initial available = 3")
	}
	if !s.TryAcquire() || s.Available() != 2 {
		t.Fatal("TryAcquire 1")
	}
	if !s.TryAcquire() || s.Available() != 1 {
		t.Fatal("TryAcquire 2")
	}
	if !s.TryAcquire() || s.Available() != 0 {
		t.Fatal("TryAcquire 3")
	}
	if s.TryAcquire() {
		t.Fatal("4th should fail (full)")
	}
	s.Release()
	if s.Available() != 1 {
		t.Fatalf("after release, available = %d", s.Available())
	}
}

func TestSemaphore_AcquireBlocksUntilRelease(t *testing.T) {
	s := NewSemaphore(1)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 第二个 Acquire 应阻塞
	acquired := make(chan struct{})
	go func() {
		_ = s.Acquire(context.Background())
		close(acquired)
	}()
	// 100ms 内不应完成
	select {
	case <-acquired:
		t.Fatal("2nd Acquire should block")
	case <-time.After(100 * time.Millisecond):
	}
	// Release 后应能拿到
	s.Release()
	select {
	case <-acquired:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Fatal("2nd Acquire should unblock after release")
	}
}

func TestSemaphore_AcquireCtxCancel(t *testing.T) {
	s := NewSemaphore(1)
	_ = s.Acquire(context.Background()) // 占满
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Acquire(ctx)
	if err == nil {
		t.Fatal("should error on ctx cancel")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v", err)
	}
}

func TestSemaphore_ExcessRelease(t *testing.T) {
	s := NewSemaphore(2)
	_ = s.TryAcquire()
	_ = s.TryAcquire()
	s.Release()
	s.Release()
	s.Release() // 第 3 次 release 应 no-op(不会爆 buffer)
	if s.Available() != 2 {
		t.Fatalf("after 3 release on cap=2, available = %d", s.Available())
	}
}

func TestSemaphore_Close(t *testing.T) {
	s := NewSemaphore(1)
	_ = s.Acquire(context.Background())
	s.Close()
	if err := s.Acquire(context.Background()); err != ErrSemaphoreClosed {
		t.Fatalf("after Close, Acquire err = %v, want ErrSemaphoreClosed", err)
	}
	// TryAcquire 返 false
	if s.TryAcquire() {
		t.Fatal("TryAcquire after Close should false")
	}
	// 重复 close no-op
	s.Close()
}

func TestSemaphore_Concurrent_RaceFree(t *testing.T) {
	// 跑 -race 验证
	s := NewSemaphore(5)
	var wg sync.WaitGroup
	var ran atomic.Int32
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Acquire(context.Background()); err != nil {
				return
			}
			ran.Add(1)
			time.Sleep(time.Millisecond)
			s.Release()
		}()
	}
	wg.Wait()
	if ran.Load() != 100 {
		t.Fatalf("expected 100, got %d", ran.Load())
	}
	if s.Available() != 5 {
		t.Fatalf("after all release, available = %d", s.Available())
	}
}

// ============== M75 补的两条断言(见 框架层未完成项清单v1.md §1.11 #7) ==============

// TestWorkerPool_ConcurrencyCap —— 池子的核心不变式,此前 15 个测试零覆盖。
//
// ⚠️ **为什么以前漏了**:唯一碰 Running 的旧断言是"排空后 Running == 0",
// 那测的是收尾,不是上限。把 NewWorkerPool 里的 `i < cfg.Size` 改成 `i < 1`,
// 15 个旧测试**全部照过** —— 一个只跑一个 goroutine 的池子,在测试里和正常
// 池子没有区别。
//
// 设计要点:**任务在 release 上阻塞,谁都不会提前退出**,所以「第 size+1 个
// 任务进来时,前 size 个必然还没退」—— 入口采样就足以抓到任何超限,不需要
// 高频轮询 Running 去猜。等待用的是有上限的轮询而非 `time.Sleep(固定值)`,
// 慢机器上不会假绿,也不会因为机器快就白等。
func TestWorkerPool_ConcurrencyCap(t *testing.T) {
	const size = 4
	p := NewWorkerPool(size, 64)
	defer p.Close()

	release := make(chan struct{})
	// ⚠️ **必须在 p.Close() 之后注册**:defer 是 LIFO,写在后面的先执行。
	// 这行是**失败路径唯一的逃生门** —— 池子若退化成"起不满 worker",
	// 下面的 t.Fatalf 会中止测试,而此时还有 worker 卡在 <-release 上,
	// `defer p.Close()` 会 wg.Wait() 等它。没有这行,测试不是失败而是
	// **永久挂起**,CI 只能等超时 —— 挂起比红难查得多。
	//
	// 这个坑是变异探针逼出来的:把 `i < cfg.Size` 改成 `i < 1` 之后,测试
	// 既没红也没绿,直接卡死。第一版探针因此作废,改成"必须产出断言失败"。
	//
	// 用 sync.Once 而不是裸 close:正常路径要显式放行(好让 p.Close() 排空),
	// 失败路径要靠 defer 兜底,两条路径都会 close 一次。
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	var mu sync.Mutex
	var peak, live int

	task := func() {
		mu.Lock()
		live++
		if live > peak {
			peak = live
		}
		mu.Unlock()

		<-release

		mu.Lock()
		live--
		mu.Unlock()
	}

	// 提交 3×size 个,远多于 worker 数 —— 只有真的卡住上限才不会超
	total := size * 3
	for i := 0; i < total; i++ {
		if !p.Submit(task) {
			t.Fatalf("submit #%d 失败", i)
		}
	}

	// 先确认并发**能**打满 size(证明没退化成"根本不起 worker")
	deadline := time.Now().Add(2 * time.Second)
	for p.Stats().Running < size {
		if time.Now().After(deadline) {
			t.Fatalf("并发未在 2s 内打满:Running=%d,want %d", p.Stats().Running, size)
		}
		time.Sleep(time.Millisecond)
	}

	unblock()
	p.Close() // 优雅关闭 = 等全部跑完(第二次调是 no-op,Close 内部 Swap 守卫)

	mu.Lock()
	got := peak
	mu.Unlock()

	if got != size {
		t.Fatalf("峰值并发 = %d,want %d —— 池子的全部意义就是这条上限", got, size)
	}
	if n := p.Stats().Executed; n != int64(total) {
		t.Fatalf("Executed = %d,want %d", n, total)
	}
}

// TestWorkerPool_Stats_Workers —— 曾经的 bug:Workers 字段写的是 cap(p.tasks),
// 也就是**队列缓冲容量**而不是 worker 数。默认构造(size=4, queue=64)下监控
// 面板上会显示"64 个 worker"。字段带 json tag,是设计给监控端点用的,所以这个
// 错值会被原样发出去。
//
// 断言用 worker 数 ≠ queue size 的参数组 —— 用默认参数(4 / 64)虽然也能测出
// bug,但两组都测,能顺带锁住"两个值不会互相串"。
func TestWorkerPool_Stats_Workers(t *testing.T) {
	cases := []struct {
		name      string
		size      int
		queueSize int
	}{
		{"默认", 4, 64},
		{"worker < queue", 3, 17},
		{"worker > queue", 8, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := NewWorkerPool(c.size, c.queueSize)
			defer p.Close()

			s := p.Stats()
			if s.Workers != c.size {
				t.Fatalf("Workers = %d,want %d(曾错报 queue size %d)", s.Workers, c.size, c.queueSize)
			}
			if s.QueueSize != c.queueSize {
				t.Fatalf("QueueSize = %d,want %d", s.QueueSize, c.queueSize)
			}
		})
	}
}

// TestSemaphore_Close_WakesBlockedAcquire Close 必须唤醒阻塞中的 Acquire。
//
// 曾经 Close 只是"清空 tokens channel"—— 那叫不醒任何人:阻塞者在等
// channel 里有东西**可收**,把 token 拿走只会让它等得更久;ctx 不可取消时
// 就是 goroutine 永久泄漏(审计报告 syncx #1)。
//
// 收敛是确定性的,不赌时序:无论等待者是否已挂起,Close 之后它要么从 done
// 分支被唤醒、要么在入口被 closed 挡下 —— 两条路都返回 ErrSemaphoreClosed。
func TestSemaphore_Close_WakesBlockedAcquire(t *testing.T) {
	s := NewSemaphore(1)
	if err := s.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	res := make(chan error, 1)
	go func() {
		// ctx 不可取消:修复前这里永久阻塞
		res <- s.Acquire(context.Background())
	}()
	s.Close()
	select {
	case err := <-res:
		if !errors.Is(err, ErrSemaphoreClosed) {
			t.Fatalf("阻塞中的 Acquire 应收到 ErrSemaphoreClosed,got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close 没有唤醒阻塞中的 Acquire(goroutine 泄漏)")
	}
}

// TestWorkerPool_CloseNow_BlockedSubmitReturnsFalse 阻塞 send 期间 CloseNow
// 已发生 ⇒ Submit 必须返 false 并回滚计数,而不是返 true 但任务被静默丢弃、
// queued 永久泄漏(审计报告 syncx #2)。
//
// 全程确定性,不赌时序:
//   - 两个 worker 都被 release 挡在任务里,且任务内自旋等 stopped 置位 ——
//     保证 worker 恢复时 CloseNow 的丢弃路径必然已激活,B/C 都不会被真的执行;
//   - 队列缓冲 1 被 B 占满 → C 的 Submit 必然阻塞在 send 上(先等 Blocked==1
//     确认它已越过 closed 快速失败分支);
//   - C 的 send 只可能在 worker 排干队列时完成(那是缓冲区唯一出现空位的时机),
//     而 stopped 必然先于任何排队动作置位 ⇒ C 返 false 是确定的。
func TestWorkerPool_CloseNow_BlockedSubmitReturnsFalse(t *testing.T) {
	p := NewWorkerPool(2, 1)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	// 同包测试直接读内部 stopped:任务内等 CloseNow 置位,消除
	// "worker 先于 CloseNow 恢复、把排队任务执行掉" 的另一条时间线
	waitStopped := func() {
		for !p.stopped.Load() {
			time.Sleep(time.Millisecond)
		}
	}
	p.Submit(func() { started <- struct{}{}; waitStopped(); <-release })
	p.Submit(func() { started <- struct{}{}; waitStopped(); <-release })
	<-started
	<-started
	if !p.Submit(func() {}) { // B:占满队列
		t.Fatal("前置条件:B 应成功入队")
	}
	res := make(chan bool, 1)
	go func() { res <- p.Submit(func() {}) }() // C:队列满 → 阻塞
	deadline := time.Now().Add(2 * time.Second)
	for p.Stats().Blocked != 1 {
		if time.Now().After(deadline) {
			t.Fatal("C 的 Submit 应阻塞在队列满上(blocked 计数未置位)")
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan struct{})
	go func() { p.CloseNow(); close(done) }()
	close(release) // 放行 worker:它们只会走丢弃路径
	<-done
	if ok := <-res; ok {
		t.Fatal("阻塞 send 期间 CloseNow 已发生,Submit 必须返 false")
	}
	st := p.Stats()
	if st.Executed != 2 {
		t.Fatalf("Executed 应为 2(只有两个 in-flight),got %d", st.Executed)
	}
	if st.Submitted != 3 { // A1 A2 B;C 已回滚
		t.Fatalf("Submitted 应为 3(C 被回滚不计),got %d", st.Submitted)
	}
	if st.Rejected != 1 {
		t.Fatalf("Rejected 应为 1(C),got %d", st.Rejected)
	}
	// queued 的精确值取决于 C 的任务是否恰好被 drain 接走(0 或 -1),
	// 但**不得残留正数** —— 正数 = 修复前的永久泄漏
	if st.Queued > 0 {
		t.Fatalf("关闭 + 回滚后 queued 不得残留正数(泄漏),got %d", st.Queued)
	}
}
