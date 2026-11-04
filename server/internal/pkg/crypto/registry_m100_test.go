package crypto

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cryptaudit"
)

// ── InitKeys 的错误路径(M100:panic → error)────────────────────

// TestInitKeys_InvalidKey_ReturnsError key 非法时必须返 error,**不能 panic**
//
// # M100 修的那个 bug
//
// 老实现 `mustBuildTier` 在 key 长度不对时直接 panic。而
// `kms.NewManager` 里那个 `if err := crypto.InitKeys(...); err != nil`
// 是**死代码**(InitKeys 永远返 nil)—— 于是 main.go 里承诺的
// "kms init failed; tiered encrypt/decrypt disabled" **降级是假的**:
// 密钥配错(比如 `KEY_PII_ACTIVE=V2` 但 `KEY_PII_V2` 没配)= **进程 panic 退出**。
func TestInitKeys_InvalidKey_ReturnsError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("InitKeys 不该 panic(应该返 error): %v", r)
		}
	}()
	err := InitKeys(KeysConfig{
		PII: []KeyEntry{{KeyID: "pii-bad", KeyVersion: 1, Key: []byte("too-short"), Active: true}},
	})
	if err == nil {
		t.Fatal("key 非法时 InitKeys 必须返 error")
	}
}

// TestInitKeys_ActiveCountMustBeExactlyOne active key 数必须恰好 1
//
// 0 个 → 加密没有 key 可用;2 个 → 轮转时"到底用哪把"没有答案。
// 两种都必须在**启动期**报出来,而不是等到第一次加密时才炸。
func TestInitKeys_ActiveCountMustBeExactlyOne(t *testing.T) {
	cases := []struct {
		name    string
		entries []KeyEntry
	}{
		{"没有 active", []KeyEntry{{KeyID: "k", KeyVersion: 1, Key: testKey32(0x11)}}},
		{"两个 active", []KeyEntry{
			{KeyID: "k1", KeyVersion: 1, Key: testKey32(0x11), Active: true},
			{KeyID: "k2", KeyVersion: 2, Key: testKey32(0x22), Active: true},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("不该 panic: %v", r)
				}
			}()
			if err := InitKeys(KeysConfig{PII: c.entries}); err == nil {
				t.Fatal("active key 数不对时必须返 error")
			}
		})
	}
}

// TestInitKeys_DuplicateKeyVersion_ReturnsError 同一个 tier 里版本号不能重复
//
// 重复版本号会让"按版本找 key"变成不确定 —— 密文里标的版本号解出的是哪把 key
// 取决于 map 遍历顺序。老实现不查重(直接覆盖),于是这种配置能启动,
// 但**老密文会随机解不出来**。
func TestInitKeys_DuplicateKeyVersion_ReturnsError(t *testing.T) {
	err := InitKeys(KeysConfig{
		PII: []KeyEntry{
			{KeyID: "k1", KeyVersion: 1, Key: testKey32(0x11), Active: true},
			{KeyID: "k1-dup", KeyVersion: 1, Key: testKey32(0x22)},
		},
	})
	if err == nil {
		t.Fatal("重复的 key 版本号必须返 error")
	}
}

// TestInitKeys_Failure_LeavesRegistryUnchanged 失败必须**什么都不改**
//
// # M100 修的那个 bug
//
// 老实现三个 tier **逐个赋值**:`tierPII` 装好了、`tierSecret` panic 了 →
// PII 用新 key、Secret 用老 key、Normal 还是老 registry —— **半更新状态**。
// 而轮转正是最需要原子性的时刻:半更新的 registry 会让一部分数据用新 key
// 加密、另一部分用老 key,而**信封里标的版本号对不上实际用的 key**。
//
// 现在:三个 tier + legacy 全部先在局部建好,成功才一次性换上去。
func TestInitKeys_Failure_LeavesRegistryUnchanged(t *testing.T) {
	// 先装一份好的
	if err := InitKeys(KeysConfig{
		PII: []KeyEntry{{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true}},
	}); err != nil {
		t.Fatal(err)
	}
	// 用旧 key 加密一份,后面要验证它还解得开
	//
	// ⚠️ 用 audit=false 的内部实现:**本包只有审计用例该往审计表写行**。
	// 别的用例往 batch 里塞行,会让审计用例的行数断言变得不确定
	// (batch 攒够 100 行会走异步 flush,落库时机不可测)。
	oldCT, err := encryptFor(context.Background(), TierPII, []byte("before-rotation"), false)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ActiveKeyMeta(TierPII)
	if err != nil {
		t.Fatal(err)
	}

	// 换一份:Secret 的 key 非法 → 整体失败
	failErr := InitKeys(KeysConfig{
		PII:    []KeyEntry{{KeyID: "pii-v2", KeyVersion: 2, Key: testKey32(0x22), Active: true}},
		Secret: []KeyEntry{{KeyID: "sec-bad", KeyVersion: 1, Key: []byte("bad"), Active: true}},
	})
	if failErr == nil {
		t.Fatal("Secret key 非法时应该返 error")
	}

	// PII 必须还是旧的
	after, err := ActiveKeyMeta(TierPII)
	if err != nil {
		t.Fatalf("失败的 InitKeys 不该把 PII 打成未初始化: %v", err)
	}
	if after.KeyVersion != before.KeyVersion || after.KeyID != before.KeyID {
		t.Fatalf("PII 被半更新了: before=%+v after=%+v", before, after)
	}
	// 旧密文还解得开
	pt, err := decryptFor(context.Background(), TierPII, oldCT, false)
	if err != nil {
		t.Fatalf("旧密文应该还解得开: %v", err)
	}
	if string(pt) != "before-rotation" {
		t.Fatalf("解密结果 = %q", pt)
	}
}

// ── tiered 路径的密钥使用审计(M100:主路径一条审计都不写)────

var (
	auditDBOnce sync.Once
	auditDBInst *gorm.DB
)

// auditTestDB 开一个内存库并把 cryptaudit 接上(整个包共用一个)
//
// ⚠️ 日志级别设成 Silent:GORM 默认的 Warn 级别会把**超过 200ms 的 SQL**
// 连同完整语句打进 stdout —— 一包几十行 INSERT 打印出来能把测试输出刷爆,
// 而且在多包并发跑(-race 时尤其明显)会把慢 SQL 计时放大到秒级,
// 让人误以为是代码慢。
func auditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	auditDBOnce.Do(func() {
		dsn := "file:crypto_m100_audit?mode=memory&cache=shared"
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		// 单连接:共享缓存的 sqlite 两个连接同时写同一张表会直接报锁错
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("db handle: %v", err)
		}
		sqlDB.SetMaxOpenConns(1)
		if err := db.AutoMigrate(&model.SysCryptoAuditLog{}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		cryptaudit.Init(db)
		auditDBInst = db
	})
	return auditDBInst
}

func countAuditRows(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.SysCryptoAuditLog{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// countAuditRowsByKey 数某个 key_id 的审计行数
func countAuditRowsByKey(t *testing.T, db *gorm.DB, keyID string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.SysCryptoAuditLog{}).Where("key_id = ?", keyID).Count(&n).Error; err != nil {
		t.Fatalf("count by key: %v", err)
	}
	return n
}

// TestTieredPath_WritesKeyUsageAudit tiered 这条**主路径**必须落密钥使用审计
//
// # M100 修的那个 bug
//
// 老实现里,`crypto.Field()` 那条(字段级 Cipher)路径落 cryptaudit,
// 而 KMS 装配好的 tiered 这条**主路径一条审计都不写** ——
// 等于"审计覆盖的是没人用的那条路,漏掉的是真在用的那条"。
//
// 这条用例同时盯两件事:
//  1. tiered 加密/解密确实落审计(行数 + 字段)
//  2. 轮转探针(RotationState)**不落**审计(它是体检,不是业务读数据)
//
// ⚠️ **为什么事件数刻意压在 100 以下**
//
// cryptaudit 的批量 flush 阈值是 100 行:攒够 100 行就 `go flush()`(异步,
// 完成时间不可测)。断言要**完全确定**,就不能让任何一行落在异步 flush 里。
//
// 所以:
//   - 本包**其它**用例一律走 `encryptFor/decryptFor(..., audit=false)`,
//     不往审计表里写(见 TestInitKeys_Failure_LeavesRegistryUnchanged)
//   - 本用例的事件数 = 探针最坏 20 行 + 真加解密 60 行 = 80 行 < 100
//   - 落库只靠 `cryptaudit.Shutdown()` —— 它**同步**等最后一次 flush 结束
//
// 早期版本用"轮询等行数"的写法(5s 超时):多包并发跑 -race 时 sqlite 写入
// 被 CPU 抢占,行数到了但超时先到 → **偶发红**。轮询等待 = 跟机器抢时间。
func TestTieredPath_WritesKeyUsageAudit(t *testing.T) {
	db := auditTestDB(t)
	const auditKeyID = "pii-m100-audit"
	if err := InitKeys(KeysConfig{
		PII: []KeyEntry{{KeyID: auditKeyID, KeyVersion: 1, Key: testKey32(0x11), Active: true}},
	}); err != nil {
		t.Fatal(err)
	}

	// ── 1. 探针不该写审计 ────────────────────────────────
	// 10 次探针:万一探针每次写 2 行(加 + 解),也就 20 行,仍在阈值内
	for i := 0; i < 10; i++ {
		if _, err := RotationState(TierPII); err != nil {
			t.Fatalf("RotationState: %v", err)
		}
	}

	// ── 2. 真正的加密 / 解密必须落审计 ────────────────────
	const n = 30
	for i := 0; i < n; i++ {
		ct, err := EncryptStringFor(context.Background(), TierPII, "13800001234")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecryptStringFor(context.Background(), TierPII, ct); err != nil {
			t.Fatal(err)
		}
	}

	// 同步落库(Shutdown 会等最后一次 flush 结束)
	cryptaudit.Shutdown()

	// 行数必须**恰好**是 2n:探针一行都不能多
	//
	// ⚠️ 这里按 key_id 数而不是数全表:全表还会带上本包其它用例写的行,
	// 而那些行与本用例无关(而且一旦本包新增了写审计的用例,全表断言就假红)。
	if got := countAuditRowsByKey(t, db, auditKeyID); got != 2*n {
		t.Fatalf("审计行数 = %d, want %d(多出来的就是轮转探针写的 —— "+
			"探针是体检不是业务读数据,不该混进审计表)", got, 2*n)
	}

	var rows []model.SysCryptoAuditLog
	if err := db.Where("key_id = ?", auditKeyID).Order("id asc").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2*n {
		t.Fatalf("审计行数 = %d, want %d", len(rows), 2*n)
	}
	var enc, dec int
	for _, r := range rows {
		if r.KeyVersion != 1 {
			t.Fatalf("key_version = %d, want 1", r.KeyVersion)
		}
		if r.Algorithm != "aes-256-gcm" {
			t.Fatalf("algorithm = %q", r.Algorithm)
		}
		if !r.Success {
			t.Fatalf("success = false: %+v", r)
		}
		if r.ID == 0 {
			t.Fatalf("审计行主键为 0 —— BaseModel.ID 是 autoIncrement:false,"+
				"不自己填的话第二批往后的行全部撞主键插不进去: %+v", r)
		}
		switch r.Operation {
		case "encrypt":
			enc++
		case "decrypt":
			dec++
		default:
			t.Fatalf("operation = %q", r.Operation)
		}
	}
	if enc != n || dec != n {
		t.Fatalf("encrypt=%d decrypt=%d, want %d/%d", enc, dec, n, n)
	}
}

// ── legacy key 的并发读(M100:裸读包级变量 = data race)────

// TestDecryptFor_ConcurrentWithInitKeys_NoRace 解密与密钥轮转并发时不能有 data race
//
// # M100 修的那个 bug
//
// 老实现的 v1 分支**裸读**包级变量 `legacyEntry`(不拿读锁),
// 而 `InitKeys` 会写它 —— 轮转与读并发 = data race。
//
// ⚠️ 这条用例**只有加 -race 跑才有意义**(仓库的 CI 就是 go test -race)。
//
// ⚠️ 用 `decryptFor(..., audit=false)`:这条用例要跑几万次解密,
// 每次都落一行审计的话 batch 会瞬间攒过 100 行的异步 flush 阈值,
// 把审计表搞得一团乱(还会跟审计用例的行数断言打架)。
// 这里要验的是"并发读 registry 没有 data race",与审计无关。
func TestDecryptFor_ConcurrentWithInitKeys_NoRace(t *testing.T) {
	legacy := testKey32(0x99)
	ct := makeV1Ciphertext(t, legacy, 0x11, "race")
	if err := InitKeys(KeysConfig{
		PII:    []KeyEntry{{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true}},
		Legacy: &KeyEntry{KeyID: "legacy-v1", KeyVersion: 1, Key: legacy},
	}); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// 读侧:不停解密 v1 密文(走 legacy key)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = decryptFor(context.Background(), TierPII, ct, false)
			}
		}()
	}

	// 写侧:轮转(反复换 registry)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = InitKeys(KeysConfig{
				PII:    []KeyEntry{{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true}},
				Legacy: &KeyEntry{KeyID: "legacy-v1", KeyVersion: 1, Key: legacy},
			})
		}
		close(stop)
	}()

	wg.Wait()
}

// ── 盲索引 key 校验(M100:注释写着 ≥16B,代码一行没查)──────

// TestNewBlindIndex_RejectsShortKey 短 key 必须被拒
//
// # M100 修的那个 bug
//
// 老实现只有一句注释("key 至少 16 字节"),**没有任何代码兑现** ——
// `NewBlindIndex(nil)` 会造出一个**空 key** 的 HMAC-SHA256。
// 空 key 的 HMAC 是"任何人都能算出来的公开哈希":拿到库的人拿候选明文
// 算一遍就能把手机号 / 身份证反查出来 —— 盲索引"不落明文"的意义当场归零。
func TestNewBlindIndex_RejectsShortKey(t *testing.T) {
	for _, n := range []int{0, 1, 8, 15} {
		if _, err := NewBlindIndex(make([]byte, n)); !errors.Is(err, ErrBlindIndexKeyTooShort) {
			t.Fatalf("%d 字节 key 应该被拒,得到 %v", n, err)
		}
	}
	if _, err := NewBlindIndex(make([]byte, BlindIndexMinKeyLen)); err != nil {
		t.Fatalf("%d 字节 key 应该合法: %v", BlindIndexMinKeyLen, err)
	}
}

// TestInitBlindIndex_RejectsShortKey 失败的初始化不能污染全局状态
func TestInitBlindIndex_RejectsShortKey(t *testing.T) {
	before := TryBlind()
	if err := InitBlindIndex([]byte("short")); !errors.Is(err, ErrBlindIndexKeyTooShort) {
		t.Fatalf("应该返 ErrBlindIndexKeyTooShort,得到 %v", err)
	}
	if after := TryBlind(); after != before {
		t.Fatal("失败的 InitBlindIndex 不该改变全局索引器")
	}
}

// TestBlindIndex_ComputeNorm_Normalizes 规范化的语义必须与实现一致
//
// ⚠️ 老注释写的是"去**首尾**空格",实际实现是**中间的空也一并去掉**
// (手机号 "138 0000 1234" → "13800001234")。这是**有意的**:同一个号码
// 带不带空格必须算出同一个索引,否则建索引和查询两边对不上,查不到人。
func TestBlindIndex_ComputeNorm_Normalizes(t *testing.T) {
	b, err := NewBlindIndex([]byte("0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if b.ComputeNorm("138 0000 1234") != b.ComputeNorm("13800001234") {
		t.Fatal("中间空格应该被去掉")
	}
	if b.ComputeNorm("138\t0000\t1234") != b.ComputeNorm("13800001234") {
		t.Fatal("制表符应该被去掉")
	}
	if b.ComputeNorm("ABC") != b.ComputeNorm("abc") {
		t.Fatal("ASCII 大写应该被转小写")
	}
	// 不同 key 必须算出不同索引(否则等于没做盲索引)
	b2, err := NewBlindIndex([]byte("fedcba9876543210"))
	if err != nil {
		t.Fatal(err)
	}
	if b.Compute("13800001234") == b2.Compute("13800001234") {
		t.Fatal("不同 key 必须算出不同索引")
	}
}
