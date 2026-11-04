package piiaudit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
)

var (
	auditDBOnce sync.Once
	auditDBInst *gorm.DB
)

// roundSeq 每轮自增 —— 让 -count=N 的每一轮用**互不相同**的过滤键。
//
// 本包的测试共用**同一个进程内**的内存库(DSN 固定 + cache=shared),
// 上一轮写进去的行还留在库里 ⇒ 用固定过滤键查会把上一轮的行一起查出来
// ⇒ `go test -count=2` 必红。
//
// (三个审计包 piiaudit / cryptaudit / smsaudit 是同一个根因,同款修法。)
var roundSeq atomic.Int64

// roundKey 生成本轮独占的过滤键
func roundKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, roundSeq.Add(1))
}

// auditTestDB 开内存库并接上全局 Recorder(整个包共用一个)
func auditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	auditDBOnce.Do(func() {
		dsn := "file:piiaudit_m100?mode=memory&cache=shared"
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("db handle: %v", err)
		}
		sqlDB.SetMaxOpenConns(1)
		if err := db.AutoMigrate(&model.SysPIIAccessLog{}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		Init(db)
		auditDBInst = db
	})
	return auditDBInst
}

// TestRecord_FlushesRowsToDB PII 访问必须真的落库,而且**每一行都要有主键**
//
// # M100 修的那个 bug(最狠的一个)
//
// `model.BaseModel.ID` 的 tag 是 `primaryKey;autoIncrement:false` ——
// GORM 因此**不让数据库自增**,把零值 0 原样写进去。
// 而 id 是主键 → **第二批往后的插入全部撞 "UNIQUE constraint failed"** →
// 整批重试 3 次之后被丢掉(只留一条 Warn 日志)。
//
// 后果:**`sys_pii_access_log` 这张表永远只能存下 1 行** ——
// 而它是 §1.5 #5 的 P0 等保项("谁在什么时候看了手机号")。
// 表在、行有,但**查不出东西**。
//
// 这条用例一次记 100 条(= 批量阈值),一次就把"只能存 1 行"打穿。
//
// ⚠️ piiaudit 攒够 100 行是**同步** flush(不是 `go flush()`),
// 所以这里不需要等,断言是确定的。
func TestRecord_FlushesRowsToDB(t *testing.T) {
	db := auditTestDB(t)

	ctx := authz.WithPrincipal(context.Background(), &authz.Principal{
		UserID:     1001,
		IdentityID: 2002,
		RoleCode:   "role_admin",
	})
	// 过滤键每轮换一个 —— 同一个进程里跑第二轮时,上一轮的行还在库里
	reqID := roundKey("req-m100")
	ctx = InjectRequestID(ctx, reqID)
	ctx = InjectIP(ctx, "10.1.2.3")
	ctx = InjectUA(ctx, "M100-Test-Agent/1.0")

	const n = 100
	for i := 0; i < n; i++ {
		Record(ctx, Access{
			ResourceType: "owner",
			ResourceID:   uint64(3000 + i),
			Field:        model.PIIFieldPhone,
		})
	}

	// 按 request_id 取:本包别的用例也往同一个 recorder 写(不相关的行)
	var rows []model.SysPIIAccessLog
	if err := db.Where("request_id = ?", reqID).Order("id asc").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != n {
		t.Fatalf("落库行数 = %d, want %d(主键没自己填 → 第二批往后全撞主键)", len(rows), n)
	}
	ids := map[uint64]bool{}
	for _, r := range rows {
		if r.ID == 0 {
			t.Fatalf("审计行主键为 0: %+v", r)
		}
		if ids[r.ID] {
			t.Fatalf("主键重复: %d", r.ID)
		}
		ids[r.ID] = true
		if r.UserID != 1001 || r.IdentityID != 2002 || r.RoleCode != "role_admin" {
			t.Fatalf("主体没提出来: %+v", r)
		}
		if r.RequestID != reqID {
			t.Fatalf("request_id = %q", r.RequestID)
		}
		if r.IP != "10.1.2.3" {
			t.Fatalf("ip = %q", r.IP)
		}
		if r.UserAgent != "M100-Test-Agent/1.0" {
			t.Fatalf("user_agent = %q", r.UserAgent)
		}
		if r.Action != model.PIIActionRead {
			t.Fatalf("action = %q(空 action 应该默认成 read)", r.Action)
		}
		if r.Field != model.PIIFieldPhone || r.ResourceType != "owner" {
			t.Fatalf("字段没落上: %+v", r)
		}
	}
}

// TestRecord_NilContext_NoPanic ctx 为 nil 不能把业务请求带崩
//
// # M100 修的那个 bug
//
// 老实现直接 `ctx.Value(...)` —— nil ctx 会 panic。
// 而审计是在**后台 goroutine** 里跑的:panic = 进程退出。
//
// ⚠️ **必须先 Init**:老实现里 `Record` 开头是 `if global == nil { return }`
// —— recorder 没初始化时它**直接返回**,根本走不到 ctx.Value。
// 只跑这一条用例时 global 还是 nil → 用例**假绿**(探针 ⑬ 实测踩过这个坑)。
func TestRecord_NilContext_NoPanic(t *testing.T) {
	auditTestDB(t) // 先 Init,让 Record 真的走到 ctx 那一段
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Record(nil ctx) 不该 panic: %v", r)
		}
	}()
	//nolint:staticcheck // 故意传 nil ctx
	Record(nil, Access{ResourceType: "owner", ResourceID: 1, Field: model.PIIFieldPhone})
}

// TestShutdown_Idempotent 退出路径上重复 Shutdown 不能 panic
//
// # M100 修的那个 bug
//
// 老实现第二次调用会对已关闭的 channel 再 close 一次 →
// panic("close of closed channel")。
// `defer piiaudit.Shutdown()` + 显式调一次是很容易写出来的组合。
//
// ⚠️ **必须先 Init**:老实现的 Shutdown 在 `global == nil` 时直接 return ——
// 只跑这一条用例时 recorder 根本没初始化,重复 Shutdown 什么也不会发生,
// 用例就是**假绿**(探针 ⑭ 实测踩过这个坑)。
func TestShutdown_Idempotent(t *testing.T) {
	auditTestDB(t) // 先 Init,让 Shutdown 真的走到 close 那一步
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("重复 Shutdown 不该 panic: %v", r)
		}
	}()
	Shutdown()
	Shutdown()
	Shutdown()
}

// TestTraceIDFromContext_FallsBackToOTelSpanContext trace_id 必须能从 OTel span 取到
//
// # M100 修的那个 bug
//
// otel 中间件把 trace_id 放在 **OTel span context** 里,
// 而本包原来只认**自己的 ctx key** —— 两套完全不同的键,谁也读不到谁。
// 而 `InjectTraceID` 全仓零调用方 → `sys_pii_access_log.trace_id` 恒为空。
//
// 现在:先读自己的 key(显式注入优先),读不到再从 span context 取。
func TestTraceIDFromContext_FallsBackToOTelSpanContext(t *testing.T) {
	traceID := [16]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	spanID := [8]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
		Remote:  true,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	got := TraceIDFromContext(ctx)
	want := sc.TraceID().String()
	if got != want {
		t.Fatalf("trace_id = %q, want %q", got, want)
	}

	// 显式注入的优先级更高
	ctx2 := InjectTraceID(ctx, "explicit-trace-id")
	if got := TraceIDFromContext(ctx2); got != "explicit-trace-id" {
		t.Fatalf("显式注入应优先,得到 %q", got)
	}

	// 什么都没有 → 空串(不是 panic)
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("没有 span 时应返空串,得到 %q", got)
	}
}

// TestRequestIDFromContext 注入 / 读取 request_id
func TestRequestIDFromContext(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Fatalf("没有注入时应该返空串,得到 %q", got)
	}
	ctx := InjectRequestID(context.Background(), "req-1")
	if got := RequestIDFromContext(ctx); got != "req-1" {
		t.Fatalf("request_id = %q", got)
	}
}

// TestInjectIPAndUA ip / ua 的注入与读取
//
// # M100 修的那个 bug
//
// `CtxKeyIP` / `CtxKeyUA` 这两个 ctx key 早就存在、cryptaudit 一直在读,
// 但**从来没有任何地方写入过** → `sys_pii_access_log.ip` / `user_agent`
// 恒为空。审计表少了"从哪台机器来"这一维,等保场景下这条记录基本没用。
func TestInjectIPAndUA(t *testing.T) {
	ctx := InjectIP(context.Background(), "192.168.1.1")
	ctx = InjectUA(ctx, "Mozilla/5.0")
	if got, _ := ctx.Value(CtxKeyIP{}).(string); got != "192.168.1.1" {
		t.Fatalf("ip = %q", got)
	}
	if got, _ := ctx.Value(CtxKeyUA{}).(string); got != "Mozilla/5.0" {
		t.Fatalf("user_agent = %q", got)
	}
	// 没注入时读出来是空串而不是 panic
	empty := context.Background()
	if got, _ := empty.Value(CtxKeyIP{}).(string); got != "" {
		t.Fatalf("未注入时 ip 应为空串,得到 %q", got)
	}
}

// TestRecordMany 批量记录按行计(列表 / 导出场景"每行算 1 条")
//
// ⚠️ 断言写成"按 resource_id 查这 2 行在不在",而不是"全表行数 +2":
// 本包其它用例也往同一个 recorder 写,全表行数会随用例顺序变。
func TestRecordMany(t *testing.T) {
	db := auditTestDB(t)
	// 资源 ID 每轮换一个 —— 同一个进程里跑第二轮时,上一轮的行还在库里
	seq := roundSeq.Add(1)
	id1 := uint64(9000 + seq*2 + 1)
	id2 := uint64(9000 + seq*2 + 2)
	RecordMany(context.Background(), []Access{
		{ResourceType: "owner", ResourceID: id1, Field: model.PIIFieldPhone},
		{ResourceType: "owner", ResourceID: id2, Field: model.PIIFieldPhone},
	})
	global.flush() // 同包内部测试可以直接调未导出的 flush(同步落库)

	for _, id := range []uint64{id1, id2} {
		var n int64
		if err := db.Model(&model.SysPIIAccessLog{}).Where("resource_id = ?", id).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("resource_id=%d 的行数 = %d, want 1", id, n)
		}
	}
}

// TestRecord_ConcurrentWithFlush_NoRace 并发记录 + flush 不能有数据竞争
//
// # M100 修的那个 bug
//
// 老实现的 flush 写的是 `r.batch = r.batch[:0]` —— **复用同一块底层数组**。
// flush 释放锁之后才去 CreateInBatches(那一段要几十毫秒),
// 而这期间 append() 会往**同一块数组**里写新行
// → **正在被 GORM 读的那批数据被就地改写**。
//
// 后果有两个:
//   - 数据竞争(-race 一抓一个准)
//   - 落库的行**内容可能是别的行的**(字段被串了)—— 比丢行更坏:
//     审计记录写着"张三看了手机号",字段却是李四的那一行的
//
// ⚠️ 这条用例**只有加 -race 跑才有意义**(仓库的 CI 就是 go test -race)。
//
// 断言写成"每行的 reason 必须和它自己的 resource_id 对得上":
// 字段被串行时这两个字段会来自不同的 append,必然对不上。
func TestRecord_ConcurrentWithFlush_NoRace(t *testing.T) {
	db := auditTestDB(t)
	ctx := context.Background()

	const (
		workers = 4
		perWork = 200
		// 本用例独占的 resource_id 段(本包其它用例的行不参与断言)
		idBase = 1000000
	)
	var wg sync.WaitGroup
	for g := 0; g < workers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWork; i++ {
				Record(ctx, Access{
					ResourceType: "owner",
					ResourceID:   uint64(idBase + i),
					Field:        model.PIIFieldPhone,
					Reason:       fmt.Sprintf("r%d", i),
				})
			}
		}()
	}
	wg.Wait()
	global.flush() // 收尾:把最后不满 100 的那批也落库

	var rows []model.SysPIIAccessLog
	if err := db.Where("resource_id >= ?", idBase).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("一行都没落库")
	}
	for _, r := range rows {
		want := fmt.Sprintf("r%d", r.ResourceID-idBase)
		if r.Reason != want {
			t.Fatalf("行内容被串了: resource_id=%d 但 reason=%q(应为 %q)—— "+
				"flush 与 append 用了同一块底层数组", r.ResourceID, r.Reason, want)
		}
	}
}
