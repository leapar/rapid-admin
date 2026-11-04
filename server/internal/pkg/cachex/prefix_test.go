package cachex

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ── 本轮新增代码的可逆探针记录(2026-10-21)─────────────────────────────
//
//   拆法                                     必须变红
//   ──────────────────────────────────────  ──────────────────────────────────
//   P1 DeletePrefix 不再拒绝空前缀            TestDeletePrefix_RejectsEmptyPrefix
//   P2 l1PrefixOf 改手拼 "l1:" 前缀          TestTwoLevel_l1PrefixOf_* / _PurgesL1OnlyMatching
//   P3 purgeL1Prefix 循环不删                TestTwoLevel_DeletePrefix_PurgesL1OnlyMatching
//   P4 DeletePrefix 连 L1 都不清              TestTwoLevel_DeletePrefix_* / TestTwoLevel_l1PrefixOf_*
//   P5 GetOrLoadTyped 不处理 ErrNotFound      TestGetOrLoadTyped_ErrNotFound_ZeroValueNoError
//   P6 GetOrLoadTyped 吞掉 loader 的真错误    TestGetOrLoadTyped_LoaderError_Propagates
//   P7 Build 未知 mode 静默当 auto            TestBuild_ModeMatrix / _UnknownMode_NoSideEffect
//   P8 Build(auto+无Redis) 仍塞个 Safe       TestBuild_ModeMatrix
//   P9 WithJitter 越界不回落默认              TestWithJitter_OutOfRangeFallsBack
//
//   9/9 全部变红。其中 **P8 是探针替测试抓出来的真 bug**:
//
//   Build 在"auto + 没配 Redis"时如果返回 NewTwoLevel(..., New(nil)),
//   造出来的是一个**非 nil 但没有连接**的 Safe。它躲过了原先那条
//   `got.safe.L2Available()` 断言(后端是 nil,返 false,看着完全正常),
//   但 TwoLevel.GetOrLoad 只判 `c.safe != nil`,照样会走进去,在
//   `s.rdb.Get` 上 panic —— "没配 Redis"直接变成"跑起来就崩"。
//   断言已改成判 `safe == nil`。教训:探一个属性时别用那个属性**自己的**降级视图
//   去判,L2Available 只看后端在不在,挡不住 nil receiver。
//
//   ✅ 已补(2026-10-25 M90):Safe.DeletePrefix 的 SCAN 分页 / 批量 DEL。
//   当时"未覆盖"是因为只能拿真 Redis 去跑;后端抽象成 Backend 之后,
//   内存后端也实现了 Scanner,这条路径终于是可测的。
//
// ── M96(2026-10-26)可逆探针记录 ─────────────────────────────────────
//
//   拆法                                        必须变红
//   ─────────────────────────────────────────  ────────────────────────────────
//   P1 escapeGlob 退化成恒等映射(不转义)        M96_Safe_DeletePrefix_GlobMeta/Redis_后端
//   P2 Expire 退回 Redis 原生 EXPIRE key 0      M96_RedisBackend_Expire_NonPositiveTTL_...
//   P3 Set 去掉 ttl<0 归一化                     M96_RedisBackend_Set_NonPositiveTTL_...
//   P4 SetNX 去掉 ttl<0 归一化                   M96_RedisBackend_SetNX_NonPositiveTTL_...
//   P5 扫描分页循环只跑第一批就 break             M96_DeletePrefix_PagedScan
//   P6 内存后端 Scan 退回 "*" 截断解析            M96_Safe_DeletePrefix_GlobMeta/内存后端
//   P7 DeletePrefix 退回拼 pattern(前缀+"*")     M96_Safe_DeletePrefix_GlobMeta(两个子例)
//
//   7/7 全部变红。⚠️ **P5 第一次探针是假红** —— 拆法写成"删掉 if next == 0 {break}",
//   结果 next 变成未使用变量,go test 直接 build failed。
//   **编译失败不是变红**:它证明不了"这条断言抓得住这个 bug"。
//   改成"保留 if、只在后面加一句 break"之后,才真的红在断言上
//   ("应删 6 条,got 2")。教训:探针本身也要验 —— 先看它红在**哪一行**。
// ───────────────────────────────────────────────────────────────────────

// 注:DeletePrefix 的 L2(Redis SCAN)路径沿用本包既有约定 —— 测试不依赖真 Redis,
// 只覆盖能在 nil rdb 下验证的部分(前缀拒绝 / fail-open / L1 清除)。
// SCAN 分页与批量 DEL 靠 dev 环境端到端跑,不假装这里覆盖到了。

// TestDeletePrefix_RejectsEmptyPrefix 空前缀必须报错,而且**一条都不能删**
//
// 这是本文件最重要的一条:DeletePrefix(ctx, "") 最常见的成因是
// fmt.Sprintf 少写了个 %s。如果实现"好心"地当成清空整个命名空间,
// 一次字符串拼接失误就是全量缓存清空,而且返回值 0 删除会让人以为"没有东西要删"。
func TestDeletePrefix_RejectsEmptyPrefix(t *testing.T) {
	cases := []string{"", "   ", "\t\n"}
	for _, prefix := range cases {
		s := &Safe{}
		if _, err := s.DeletePrefix(context.Background(), prefix); !errors.Is(err, ErrEmptyPrefix) {
			t.Fatalf("Safe.DeletePrefix(%q) 应返回 ErrEmptyPrefix,got %v", prefix, err)
		}
		tl := NewTwoLevel(100, time.Second, s)
		if _, err := tl.DeletePrefix(context.Background(), prefix); !errors.Is(err, ErrEmptyPrefix) {
			t.Fatalf("TwoLevel.DeletePrefix(%q) 应返回 ErrEmptyPrefix,got %v", prefix, err)
		}
	}
}

// TestDeletePrefix_NilRedis_FailOpen 没配 Redis 时不该报错 —— "没有缓存可删"不是错误
func TestDeletePrefix_NilRedis_FailOpen(t *testing.T) {
	ctx := context.Background()
	s := &Safe{}
	n, err := s.DeletePrefix(ctx, "dict:")
	if err != nil || n != 0 {
		t.Fatalf("nil rdb: 期望 (0, nil),got (%d, %v)", n, err)
	}
	tl := NewTwoLevel(10, time.Second, nil)
	if n, err := tl.DeletePrefix(ctx, "dict:"); err != nil || n != 0 {
		t.Fatalf("nil TwoLevel: 期望 (0, nil),got (%d, %v)", n, err)
	}
	// nil receiver 也不能炸 —— Build(ModeOff) 就是返 nil 的
	var nilTL *TwoLevel
	if n, err := nilTL.DeletePrefix(ctx, "dict:"); err != nil || n != 0 {
		t.Fatalf("nil receiver: 期望 (0, nil),got (%d, %v)", n, err)
	}
}

// TestTwoLevel_DeletePrefix_PurgesL1OnlyMatching
//
// L1 不清的后果:调了失效,这台机器还在吐旧值,要等 l1_ttl 甚至重启才好 ——
// 这是"改了没生效"最常见的现场。
func TestTwoLevel_DeletePrefix_PurgesL1OnlyMatching(t *testing.T) {
	c := NewTwoLevel(100, time.Minute, nil) // 无 L2 → key 就是原样
	ctx := context.Background()

	load := func(key, val string) {
		t.Helper()
		if _, _, err := c.GetOrLoad(ctx, key, time.Minute, func(ctx context.Context) (any, error) {
			return val, nil
		}); err != nil {
			t.Fatalf("预热 %s 失败: %v", key, err)
		}
	}
	load("dict:1:items", "a")
	load("dict:1:group", "b")
	load("dict:2:items", "c")
	load("cfg:app:name", "d")
	if got := c.l1.Len(); got != 4 {
		t.Fatalf("前置条件: L1 应有 4 条,got %d", got)
	}

	if _, err := c.DeletePrefix(ctx, "dict:1:"); err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if got := c.l1.Len(); got != 2 {
		t.Fatalf("失效后 L1 应剩 2 条(dict:2:、cfg:),got %d", got)
	}
	// 精确核对:留下的必须是没被误删的两条
	if _, ok := c.Get("dict:1:items"); ok {
		t.Fatal("dict:1:items 不该还在 L1")
	}
	if _, ok := c.Get("cfg:app:name"); !ok {
		t.Fatal("cfg:app:name 被误删了 —— 前缀匹配过宽")
	}
	if _, ok := c.Get("dict:2:items"); !ok {
		t.Fatal("dict:2:items 被误删了 —— 前缀匹配过宽")
	}
}

// TestDeletePrefix_ScannerBackend 走 SCAN 的那条路径(2026-10-25 M90 补上)
//
// 这条路径以前**没法测**:DeletePrefix 硬依赖 Redis 的 SCAN,包内约定又不依赖
// 真 Redis,于是"前缀失效"最核心的那一步一直是裸的。后端抽象成 Backend 之后,
// 内存后端也实现了 Scanner,这条路径终于是可测的。
//
// 断言两件事:
//  1. 只删掉前缀匹配的(别的 key 不能被误删 —— 失效删多了和没删一样危险)
//  2. 返回的删除条数是真实条数(调用方靠它判断"到底删了几条")
func TestDeletePrefix_ScannerBackend(t *testing.T) {
	ctx := context.Background()
	b := NewMemoryBackend()
	s := NewWithBackend(b)

	for _, k := range []string{"dict:1:items", "dict:1:group", "dict:2:items", "cfg:app:name"} {
		if err := b.Set(ctx, k, "v", time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.DeletePrefix(ctx, "dict:1:")
	if err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if n != 2 {
		t.Fatalf("应删 2 条,got %d", n)
	}
	for _, k := range []string{"dict:1:items", "dict:1:group"} {
		if _, err := b.Get(ctx, k); !errors.Is(err, ErrMiss) {
			t.Fatalf("%s 应被删掉,got %v", k, err)
		}
	}
	for _, k := range []string{"dict:2:items", "cfg:app:name"} {
		if _, err := b.Get(ctx, k); err != nil {
			t.Fatalf("%s 被误删了 —— 前缀匹配过宽: %v", k, err)
		}
	}
}

// TestDeletePrefix_BackendWithoutScanner 后端不支持扫描时 fail-open
//
// 不支持扫描 = 没有可枚举的 key,不是错误。返回 0,nil 跟"没有东西可删"同义。
func TestDeletePrefix_BackendWithoutScanner(t *testing.T) {
	ctx := context.Background()
	s := NewWithBackend(&noScanBackend{Backend: NewMemoryBackend()})
	n, err := s.DeletePrefix(ctx, "dict:")
	if err != nil || n != 0 {
		t.Fatalf("期望 (0, nil),got (%d, %v)", n, err)
	}
}

// noScanBackend 只实现 Backend,故意不实现 Scanner
//
// ⚠️ 嵌入的是 **Backend 接口**而不是 MemoryBackend 具体类型 ——
// 嵌具体类型会把它的 Scan/Ping 方法一起带上来,那它就"支持扫描"了,
// 这个 fake 也就失去意义。
type noScanBackend struct{ Backend }

// TestSafe_SupportsScan 前缀失效能力探测
//
// DeletePrefix 在后端不支持扫描时是 **fail-open**(返回 0, nil)——
// 那个语义对"清点不用的缓存"没问题,但对"撤销授权后清权限画像"是灾难:
// 静默什么都没删 = 权限没撤销。所以依赖"确实删掉了"的调用方必须先问一句。
func TestSafe_SupportsScan(t *testing.T) {
	// 内存后端实现了 Scan → 支持
	if !NewWithBackend(NewMemoryBackend()).SupportsScan() {
		t.Fatal("内存后端实现了 Scan,SupportsScan 应为 true")
	}
	// 只实现 Backend 的后端 → 不支持
	if NewWithBackend(&noScanBackend{Backend: NewMemoryBackend()}).SupportsScan() {
		t.Fatal("没实现 Scanner 的后端,SupportsScan 应为 false")
	}
	// nil / 无后端 → 不支持(nil receiver 也不能炸)
	var nilSafe *Safe
	if nilSafe.SupportsScan() {
		t.Fatal("nil Safe 的 SupportsScan 应为 false")
	}
	if NewWithBackend(nil).SupportsScan() {
		t.Fatal("无后端的 Safe 的 SupportsScan 应为 false")
	}
}

// TestTwoLevel_l1PrefixOf_NoL2 无 L2 时 L1 前缀是恒等映射
//
// 这条盯的是一个具体的事故:如果 l1PrefixOf 手拼 "l1:" 前缀(而不是走 fkey),
// 无 L2 场景下 L1 的真实 key 是 "dict:x" 而清的是 "l1:dict:x" ——
// **一个都匹配不到,失效静默不生效**,而且没有任何报错。
func TestTwoLevel_l1PrefixOf_NoL2(t *testing.T) {
	c := NewTwoLevel(10, time.Second, nil)
	if got, want := c.l1PrefixOf("dict:"), "dict:"; got != want {
		t.Fatalf("无 L2 时 l1PrefixOf 应是恒等映射,got %q want %q", got, want)
	}
	// 实锤:按算出来的前缀清,必须真能清掉
	ctx := context.Background()
	if _, _, err := c.GetOrLoad(ctx, "dict:9", time.Minute, func(ctx context.Context) (any, error) {
		return "x", nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeletePrefix(ctx, "dict:"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("dict:9"); ok {
		t.Fatal("按 l1PrefixOf 算出的前缀清除,应该真能清掉")
	}
}

// TestTwoLevel_l1PrefixOf_WithL2 有 L2 时 L1 key 带 l1: 前缀
func TestTwoLevel_l1PrefixOf_WithL2(t *testing.T) {
	// Safe 不接真 Redis 也行:这里只验 key 拼装,不发生 IO
	c := NewTwoLevel(10, time.Second, New(nil, WithPrefix("app")))
	if got, want := c.l1PrefixOf("dict:"), "l1:app:dict:"; got != want {
		t.Fatalf("有 L2 时 l1PrefixOf: got %q want %q", got, want)
	}
}

// ── M96(2026-10-26):SCAN 分页循环的覆盖 ────────────────────────────────
//
// # 为什么 miniredis 覆盖不到
//
// **miniredis 忽略 SCAN 的 COUNT 参数,永远一次返回全部匹配的 key、游标恒为 0。**
// (见 miniredis 源码 cmd_generic.go 的 cmdScan:"We ignore COUNT, which is
// allowed according to the Redis docs.")
//
// 于是"扫一批 → 删 → 再扫下一批"这条循环**在 miniredis 上只跑得出 1 批**。
// 要覆盖它,只能用一个**会真的返回非零游标**的假后端。
//
// # 这条循环为什么值得覆盖
//
// "扫完第一批就 break"的实现,在只有一批的场景下表现**完全正常** ——
// 只有 key 多到要分批时,漏掉的那几批就是"失效没生效"。
// 这正是那种"测试全绿、上线偶发"的那类 bug。
type pagedBackend struct {
	Backend
	keys     []string // 全量 key(按扫描顺序)
	pageLen  int      // 每批返回几条
	calls    int      // Scan 被调了几次
	prefixes []string // 每次 Scan 收到的 prefix(用来断言"没塞通配符")
}

// Scan 按页返回,**游标非零**(这是 miniredis 造不出来的那部分)
func (p *pagedBackend) Scan(_ context.Context, cursor uint64, prefix string, _ int64) ([]string, uint64, error) {
	p.calls++
	p.prefixes = append(p.prefixes, prefix)

	// 先按字面前缀过滤(与两个真后端一致)
	var matched []string
	for _, k := range p.keys {
		if strings.HasPrefix(k, prefix) {
			matched = append(matched, k)
		}
	}

	start := int(cursor)
	if start >= len(matched) {
		return nil, 0, nil
	}
	end := start + p.pageLen
	if end > len(matched) {
		end = len(matched)
	}
	next := uint64(0)
	if end < len(matched) {
		next = uint64(end)
	}
	return matched[start:end], next, nil
}

// TestM96_DeletePrefix_PagedScan 多批次扫描必须全部删掉(游标循环)
//
// 断言四件事:
//  1. 跨批的 key **一个都不能漏**(漏了 = 失效没生效 = "改了没生效")
//  2. 返回的条数是真实条数
//  3. 别的业务的 key 一个都不能被误删
//  4. 传给 Scan 的是**字面前缀**(不带 "*")—— 后端自己转义
func TestM96_DeletePrefix_PagedScan(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryBackend()
	pb := &pagedBackend{Backend: base, pageLen: 2}

	all := []string{
		"dict:1", "dict:2", "dict:3", "dict:4", "dict:5", "dict:6",
		"cfg:x", "cfg:y",
	}
	for _, k := range all {
		if err := base.Set(ctx, k, "v", time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	pb.keys = all

	s := NewWithBackend(pb)
	n, err := s.DeletePrefix(ctx, "dict:")
	if err != nil {
		t.Fatalf("DeletePrefix: %v", err)
	}
	if n != 6 {
		t.Fatalf("应删 6 条,got %d", n)
	}
	// 6 条 / 每批 2 条 = 3 批。**最后一批返回的 next 就是 0**(扫完了),
	// 所以循环正好调 3 次 —— 只调 1 次说明"扫完第一批就 break",后面的全漏删。
	if pb.calls < 3 {
		t.Fatalf("Scan 只被调 %d 次 —— 分页循环没跑完,后面的批次会被漏删", pb.calls)
	}
	for _, k := range all[:6] {
		if _, err := base.Get(ctx, k); !errors.Is(err, ErrMiss) {
			t.Fatalf("%s 应被删掉,got %v", k, err)
		}
	}
	for _, k := range all[6:] {
		if _, err := base.Get(ctx, k); err != nil {
			t.Fatalf("%s 被误删了 —— 前缀匹配过宽: %v", k, err)
		}
	}
	// 传下去的是字面前缀,不是 pattern
	for _, p := range pb.prefixes {
		if strings.ContainsAny(p, "*?[]") {
			t.Fatalf("传给 Scan 的应该是字面前缀,got %q(含 glob 元字符)", p)
		}
	}
}
