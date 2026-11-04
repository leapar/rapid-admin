package smsaudit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
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

func auditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	auditDBOnce.Do(func() {
		dsn := "file:smsaudit_m100?mode=memory&cache=shared"
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("db handle: %v", err)
		}
		sqlDB.SetMaxOpenConns(1)
		if err := db.AutoMigrate(&model.SysSmsSendLog{}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		Init(db)
		auditDBInst = db
	})
	return auditDBInst
}

// TestRecord_FlushesRowsToDB 短信发送审计必须能存下**多行**
//
// # M100 顺带修的同源缺陷(§1.9 #7)
//
// 与 §1.5 #5 / #8 的两个兄弟包完全同源:`BaseModel.ID` 是
// `primaryKey;autoIncrement:false`,GORM 把零值 0 原样入库 →
// id 撞主键 → 第二批往后的行全部插不进去 → **这张表永远只有 1 行**。
//
// 本包属 §1.9(不在 M100 的 §1.5 范围内),但缺陷同源、修复只有一行,
// 明知故犯地留着不管不可接受,故一并修掉。
func TestRecord_FlushesRowsToDB(t *testing.T) {
	db := auditTestDB(t)

	ctx := authz.WithPrincipal(context.Background(), &authz.Principal{
		UserID:     1001,
		IdentityID: 2002,
	})
	// 过滤键每轮换一个 —— 同一个进程里跑第二轮时,上一轮的行还在库里
	reqID := roundKey("req-sms")
	ctx = InjectRequestID(ctx, reqID)

	const n = 100
	for i := 0; i < n; i++ {
		Record(ctx, SendLog{
			Phone:    "13800001234",
			Scene:    "login",
			Provider: "console",
			Success:  true,
		})
	}

	// 按 request_id 取:本包别的用例也往同一个 recorder 写(不相关的行)
	var rows []model.SysSmsSendLog
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
		if r.Phone != "13800001234" || r.Scene != "login" || !r.Success {
			t.Fatalf("字段没落上: %+v", r)
		}
		if r.RequestID != reqID {
			t.Fatalf("request_id = %q", r.RequestID)
		}
	}
}

// TestRecord_ConcurrentWithFlush_NoRace 并发记录 + flush 不能有数据竞争
//
// # M100 修的那个同源缺陷
//
// 与 piiaudit / cryptaudit 完全同源:老实现的 flush 写的是
// `r.batch = r.batch[:0]` —— **复用同一块底层数组**。
// flush 释放锁之后才去 CreateInBatches,期间 append() 会往同一块数组写新行
// → **正在被 GORM 读的那批数据被就地改写**。
//
// ⚠️ 这条用例**只有加 -race 跑才有意义**(仓库的 CI 就是 go test -race)。
//
// 断言写成"每行的 scene 必须和它自己的 phone 对得上":字段被串行时
// 这两个字段来自不同的 append,必然对不上。
func TestRecord_ConcurrentWithFlush_NoRace(t *testing.T) {
	db := auditTestDB(t)
	ctx := context.Background()

	const (
		workers = 4
		perWork = 200
	)
	var wg sync.WaitGroup
	for g := 0; g < workers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWork; i++ {
				Record(ctx, SendLog{
					// 本用例独占的号段(139 开头),本包其它用例的行不参与断言
					Phone:    fmt.Sprintf("139%08d", i),
					Scene:    fmt.Sprintf("scene-%08d", i),
					Provider: "console",
					Success:  true,
				})
			}
		}()
	}
	wg.Wait()
	global.flush() // 收尾:把最后不满 100 的那批也落库

	var rows []model.SysSmsSendLog
	if err := db.Where("phone LIKE ?", "139%").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("一行都没落库")
	}
	for _, r := range rows {
		want := fmt.Sprintf("scene-%s", r.Phone[3:])
		if r.Scene != want {
			t.Fatalf("行内容被串了: phone=%q 但 scene=%q(应为 %q)—— "+
				"flush 与 append 用了同一块底层数组", r.Phone, r.Scene, want)
		}
	}
}
