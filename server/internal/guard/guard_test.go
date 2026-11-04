// 框架原语可达性守卫 —— M129(2026-10-31)
//
// # 这条守卫守的是什么
//
// 本仓反复踩同一个坑:**一个包写出来了、有单测、清单里标着 "✅ 已交付",
// 却从来没有任何生产代码调用过。**
//
//   - `pkg/anomalylogin/` —— 172 行单测、清单标 "P2 ✅",零生产调用方(M128 才发现)
//   - `pkg/sms/` —— 零调用方零单测,四块能力另有活着的实现(M129 删除)
//
// 根因同源:**「有单测」被当成了「在跑」**。
//
// ⇒ 这条测试把那条纪律机械化:每个 `internal/pkg/*` 包,
// **要么**能从 `cmd/` 入口走到,**要么**在 allowlist 里写明理由。
//
// ⚠️ allowlist 不是"逃逸名单",是**账本** —— 每一条都必须写清楚
// "为什么它现在可以没有调用方"。理由写不出来 ⇒ 它就是死代码 ⇒ 该删。
package guard_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/guard"
)

// allowlist 已知"暂时没有生产调用方"的包 + **为什么**。
//
// ⚠️ 理由必须写清楚"接线点在哪儿"。写不出来 ⇒ 这个包就是死代码。
var allowlist = map[string]string{
	"ratelimitx": "库类原语(令牌桶/漏桶),接线点在**具体端点** —— " +
		"现在的限流走 `api/middleware/ratelimit.go` 的 Redis 那套,两者互补不冲突。" +
		"接线属于业务侧决策(哪些端点要进程内限流),框架层先把原语备好。" +
		"⚠️ 计划里已排期:业务侧一旦接了某个端点,这里要立刻删掉这条。",
	"searchidx": "库类原语(n-gram 盲索引),接线点在**业务侧的数据模型** —— " +
		"要先有一个字段走 `crypto` 落密文 + 一张 side 表存 gram,查询侧才有东西可搜。" +
		"⚠️ 计划里已排期:第一个密文字段落地时,这里要立刻删掉这条。",
}

// repoRoot 找仓库根(server/ 那一层,带 go.mod)。
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := guard.FindModuleRoot(".")
	require.NoError(t, err, "找不到仓库根 —— 守卫必须在仓库里跑")
	return root
}

// ===== 1. 守卫本体 =====

// TestEveryInternalPkgPackage_IsReachableOrAllowlisted 每个 internal/pkg 包
// 要么在生产链路上跑,要么在账本上。
func TestEveryInternalPkgPackage_IsReachableOrAllowlisted(t *testing.T) {
	root := repoRoot(t)
	res, err := guard.Scan(root, []string{"internal/pkg"})
	require.NoError(t, err)

	require.NotEmpty(t, res.Packages, "一个包都没扫到 —— 守卫自己坏掉了(假绿)")

	var bad []string
	for _, p := range res.Unreachable() {
		name := strings.TrimPrefix(p.ImportPath, res.ModulePath+"/internal/pkg/")
		reason, ok := allowlist[name]
		if !ok {
			bad = append(bad, p.ImportPath+
				" —— 从 cmd/ 入口走不到它,但它不在 allowlist 里。"+
				"要么它有别的实现(删掉它),要么它是库类原语(写进 allowlist 并说明接线点)")
			continue
		}
		if strings.TrimSpace(reason) == "" {
			bad = append(bad, p.ImportPath+" —— allowlist 里的理由是空的。"+
				"账本不接受空条目:写不出理由 = 写不出接线点 = 死代码")
		}
	}
	assert.Empty(t, bad, "\n"+strings.Join(bad, "\n"))
}

// TestAllowlist_EntriesActuallyExist allowlist 里写的包名不能是**想象出来的** ——
// 包被删了却还留在账本上,会让守卫**永远绿**。
//
// ⚠️ 这一条防的是"守卫自己腐烂":allowlist 是白名单,
// 白名单里留一个已经不存在的包名,就等于给守卫开了一个永久的后门。
func TestAllowlist_EntriesActuallyExist(t *testing.T) {
	root := repoRoot(t)
	res, err := guard.Scan(root, []string{"internal/pkg"})
	require.NoError(t, err)

	existing := map[string]bool{}
	for _, p := range res.Packages {
		existing[strings.TrimPrefix(p.ImportPath, res.ModulePath+"/internal/pkg/")] = true
	}
	for name := range allowlist {
		assert.True(t, existing[name],
			"allowlist 里的 %q 在 internal/pkg 下**不存在** —— "+
				"包已经没了(或者改名了),这条账本条目该删。"+
				"留着它 = 守卫的后门", name)
	}
}

// ===== 2. 扫描器自己的行为(守卫不能是假绿) =====

// TestScan_FindsPackagesAndExcludesTestdata 扫得到包,扫不到 testdata 夹具。
func TestScan_FindsPackagesAndExcludesTestdata(t *testing.T) {
	root := repoRoot(t)
	res, err := guard.Scan(root, []string{"internal/pkg"})
	require.NoError(t, err)

	dirs := map[string]bool{}
	for _, p := range res.Packages {
		dirs[p.Dir] = true
	}
	assert.True(t, dirs["internal/pkg/logger"], "internal/pkg/logger 应该在扫描结果里")
	assert.True(t, dirs["internal/pkg/kms/local"], "嵌套子包 internal/pkg/kms/local 也应该在")
	assert.False(t, dirs["internal/pkg/jsonid/testdata/scanfixture"],
		"testdata/ 是 Go 工具链约定忽略的目录,里面的 .go 是夹具不是包")
	assert.False(t, dirs["internal/pkg"],
		"internal/pkg/ 自己没有 .go 文件 —— 它是容器,不是包")
}

// TestScan_ReachabilityIsPerPackage 已知可达 / 不可达的包,判定要对。
func TestScan_ReachabilityIsPerPackage(t *testing.T) {
	root := repoRoot(t)
	res, err := guard.Scan(root, []string{"internal/pkg"})
	require.NoError(t, err)

	byDir := map[string]guard.PackageInfo{}
	for _, p := range res.Packages {
		byDir[p.Dir] = p
	}

	// logger 被 cmd/server 直接 import ⇒ 可达
	assert.True(t, byDir["internal/pkg/logger"].Reachable,
		"pkg/logger 被 cmd/server import,必须判为可达")

	// 这两个是账本上的库类原语 ⇒ 不可达
	for _, d := range []string{"internal/pkg/ratelimitx", "internal/pkg/searchidx"} {
		require.Contains(t, byDir, d)
		assert.False(t, byDir[d].Reachable,
			"%s 目前确实没有生产调用方 —— 如果哪天接上了,allowlist 要跟着删", d)
	}
}

// TestScan_TestOnlyImportDoesNotCount 夹具:**只有测试文件 import** 的包不可达。
//
// ⚠️ 这是本守卫最核心的一条语义:测试 import 一个包,只证明"有人在测它",
// 不证明"它在生产链路上跑过"。M128 的 `pkg/anomalylogin` 就是这样
// 骗过了所有人:172 行单测 + 零生产调用方。
func TestScan_TestOnlyImportDoesNotCount(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	write("go.mod", "module example.com/fixture\n\ngo 1.21\n")
	write("cmd/app/main.go", "package main\n\nimport (\n\t\"example.com/fixture/internal/pkg/live\"\n)\n\nfunc main() { live.Hello() }\n")
	write("internal/pkg/live/live.go", "package live\n\nfunc Hello() {}\n")
	write("internal/pkg/live/live_test.go", "package live\n\nimport (\n\t\"example.com/fixture/internal/pkg/other\"\n)\n\nfunc TestX() { other.Hello() }\n")
	write("internal/pkg/other/other.go", "package other\n\nfunc Hello() {}\n")

	res, err := guard.Scan(dir, []string{"internal/pkg"})
	require.NoError(t, err)

	byDir := map[string]guard.PackageInfo{}
	for _, p := range res.Packages {
		byDir[p.Dir] = p
	}
	require.Contains(t, byDir, "internal/pkg/live")
	require.Contains(t, byDir, "internal/pkg/other")

	assert.True(t, byDir["internal/pkg/live"].Reachable,
		"live 被 cmd/app import ⇒ 可达")
	assert.False(t, byDir["internal/pkg/other"].Reachable,
		"other 只被**测试文件**import ⇒ 不可达(测试有单测 ≠ 它在跑)")
}

// TestScan_UnreachablePropagates 传递性:A import B,而没人 import A
// ⇒ B 也算不可达。
//
// ⚠️ 只数"谁 import 了我"会漏掉这一种:B 的 import 方计数是 1(就是 A),
// 但它同样一行都没跑。
func TestScan_UnreachablePropagates(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	write("go.mod", "module example.com/fixture\n\ngo 1.21\n")
	write("cmd/app/main.go", "package main\n\nfunc main() {}\n")
	write("internal/pkg/orphan/orphan.go", "package orphan\n\nimport (\n\t\"example.com/fixture/internal/pkg/leaf\"\n)\n\nfunc Use() { leaf.Hello() }\n")
	write("internal/pkg/leaf/leaf.go", "package leaf\n\nfunc Hello() {}\n")

	res, err := guard.Scan(dir, []string{"internal/pkg"})
	require.NoError(t, err)

	byDir := map[string]guard.PackageInfo{}
	for _, p := range res.Packages {
		byDir[p.Dir] = p
	}
	assert.False(t, byDir["internal/pkg/orphan"].Reachable, "orphan 没人 import ⇒ 不可达")
	assert.False(t, byDir["internal/pkg/leaf"].Reachable,
		"leaf 只被不可达的 orphan import ⇒ 同样不可达(可达性是传递的)")
}

// TestFindModuleRoot_NotInRepo 找不到 go.mod 时必须**响亮失败**。
//
// ⚠️ 不能返回空串 —— 守卫在"找不到仓库根"时安静地扫了个空目录
// 然后报"一切正常",那是**最坏的一种假绿**。
func TestFindModuleRoot_NotInRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := guard.FindModuleRoot(dir)
	assert.Error(t, err, "没有 go.mod 的目录里必须报错,不能返回空路径")
}
