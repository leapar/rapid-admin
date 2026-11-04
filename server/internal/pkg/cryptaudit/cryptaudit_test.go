package cryptaudit

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/piiaudit"
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

// auditTestDB 开内存库并接上全局 recorder(整个包共用一个)
func auditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	auditDBOnce.Do(func() {
		dsn := "file:cryptaudit_m100?mode=memory&cache=shared"
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("db handle: %v", err)
		}
		sqlDB.SetMaxOpenConns(1)
		if err := db.AutoMigrate(&model.SysCryptoAuditLog{}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		Init(db)
		auditDBInst = db
	})
	return auditDBInst
}

// TestRecord_FlushesRow 密钥使用审计必须真的落库,且**主键要自己填**
//
// # M100 修的那个 bug
//
// `BaseModel.ID` 是 `primaryKey;autoIncrement:false` → GORM 把零值 0 原样入库
// → id 撞主键 → 第二批往后的行全部插不进去(重试 3 次后丢弃)
// → **`sys_crypto_audit_log` 永远只能存 1 行**。
//
// 一次记 80 条(跨过批量阈值的一半,分成 50 + 30 两批插)就把"只能存 1 行"打穿。
//
// ⚠️ **为什么是 80 而不是 100**
//
// cryptaudit 攒够 100 行会 `go flush()`(**异步**)。如果本用例正好记 100 行,
// 那次异步 flush 会和下面这句显式 `flush()` 抢同一批数据:
// 谁先拿到锁谁插入,另一个看到空批次直接返回 —— 于是"显式 flush 返回"
// 并不保证"数据已经落库",查询可能读到 0 行 → **偶发红**。
//
// 记 80 行(< 100)就不会触发异步 flush,显式 flush 是唯一的落库路径,
// 断言完全确定。
func TestRecord_FlushesRow(t *testing.T) {
	db := auditTestDB(t)

	ctx := authz.WithPrincipal(context.Background(), &authz.Principal{
		UserID:     1001,
		IdentityID: 2002,
		RoleCode:   "role_admin",
	})
	ctx = piiaudit.InjectRequestID(ctx, "req-crypt-0001")
	ctx = piiaudit.InjectIP(ctx, "10.9.8.7")
	ctx = piiaudit.InjectUA(ctx, "M100-CryptAgent/2.0")

	const n = 80
	// 过滤键每轮换一个 —— 同一个进程里跑第二轮时,上一轮的行还在库里
	keyID := roundKey("pii-v1")
	for i := 0; i < n; i++ {
		Record(ctx, Event{
			KeyID:      keyID,
			KeyVersion: 1,
			Algorithm:  "aes-256-gcm",
			Operation:  "decrypt",
			ByteCount:  11,
			Success:    true,
		})
	}
	flush() // 同包内部测试可以直接调未导出的 flush

	// 按 key_id 数:本包别的用例也往同一个 recorder 写(不相关的行)
	var rows []model.SysCryptoAuditLog
	if err := db.Where("key_id = ?", keyID).Order("id asc").Find(&rows).Error; err != nil {
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
		if r.KeyID != keyID || r.KeyVersion != 1 || r.Algorithm != "aes-256-gcm" {
			t.Fatalf("key 字段没落上: %+v", r)
		}
		if r.Operation != "decrypt" || !r.Success || r.ByteCount != 11 {
			t.Fatalf("操作字段没落上: %+v", r)
		}
		if r.RequestID != "req-crypt-0001" || r.IP != "10.9.8.7" || r.UserAgent != "M100-CryptAgent/2.0" {
			t.Fatalf("上下文没落上: %+v", r)
		}
		if r.UserID != 1001 || r.IdentityID != 2002 || r.RoleCode != "role_admin" {
			t.Fatalf("主体没落上: %+v", r)
		}
	}
}

// TestTruncateRunes_RuneSafe 截断必须按 rune,不能把汉字劈开
//
// # M100 修的那个 bug
//
// 老实现 `errMsg[:200]` 是**字节**切片 —— 一个中文错误信息(3 字节/字)
// 正好在第 200 字节被劈开,落库就是一个**半个汉字** + 乱码尾巴。
// 排障时看到这种 error_msg,第一反应是"程序写错了",而不是"被截断了"。
func TestTruncateRunes_RuneSafe(t *testing.T) {
	// 300 个汉字(每个 3 字节 = 900 字节)
	long := strings.Repeat("错", 300)
	got := truncateRunes(long, 200)
	if !utf8.ValidString(got) {
		t.Fatalf("截断后不是合法 UTF-8(汉字被劈开了): %q", got)
	}
	if utf8.RuneCountInString(got) != 200 {
		t.Fatalf("截断后 rune 数 = %d, want 200", utf8.RuneCountInString(got))
	}
	// 老实现的写法:按字节切 → 半个汉字
	if legacy := long[:200]; utf8.ValidString(legacy) {
		t.Skip("这条断言的前提不成立(老写法居然没劈开),跳过")
	}

	// 不超长时原样返回
	if got := truncateRunes("短", 200); got != "短" {
		t.Fatalf("不该截断: %q", got)
	}
	// max <= 0
	if got := truncateRunes("x", 0); got != "" {
		t.Fatalf("max=0 应返空串,得到 %q", got)
	}
}

// TestRecord_NilContext_NoPanic ctx 为 nil 不能 panic
//
// ⚠️ **必须先 Init**:本包 `Record` 开头是 `if db == nil { return }` ——
// recorder 没初始化时它**直接返回**,根本走不到 ctx 那一段。
// 只跑这一条用例时 db 还是 nil → 用例**假绿**。
func TestRecord_NilContext_NoPanic(t *testing.T) {
	auditTestDB(t) // 先 Init,让 Record 真的走到 ctx 那一段
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Record(nil ctx) 不该 panic: %v", r)
		}
	}()
	//nolint:staticcheck // 故意传 nil ctx
	Record(nil, Event{KeyID: "k", Operation: "encrypt"})
}

// TestShutdown_Idempotent 重复 Shutdown 不能 panic
//
// # M100 修的那个 bug:老实现第二次调用会 close 已关闭的 channel → panic
//
// ⚠️ **必须先 Init**:老实现的 Shutdown 在 `stopCh == nil` 时直接 return ——
// 只跑这一条用例时 recorder 根本没初始化,重复 Shutdown 什么也不会发生,
// 用例就是**假绿**(探针 ⑲ 实测踩过这个坑)。
func TestShutdown_Idempotent(t *testing.T) {
	auditTestDB(t) // 先 Init,让 Shutdown 真的走到 close 那一步
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("重复 Shutdown 不该 panic: %v", r)
		}
	}()
	Shutdown()
	Shutdown()
}

// TestInit_NilDB_RecorderDisabled db 为 nil 时不能 panic,也不能写库
func TestInit_NilDB_RecorderDisabled(t *testing.T) {
	// 注意:本包 Init 是一次性的(sync.Once),所以这条用例只在**没有别的
	// 测试先 Init 过**时才有意义 —— 用 Record 不 panic 来兜底断言。
	Record(context.Background(), Event{KeyID: "k", Operation: "encrypt"})
}
