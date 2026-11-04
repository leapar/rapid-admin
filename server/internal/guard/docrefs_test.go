package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot 找到仓库根(同时含 server/go.mod 与 docs/)。
//
// ⚠️ 本包在 `server/` 这个 Go module 里,而要扫的文档在**仓库根**的
// `docs/` 下 —— 也就是 module 的上一级。所以这里不能复用 FindModuleRoot。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("取绝对路径失败: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "server", "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "docs")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("从 %s 往上找不到仓库根(要同时有 server/go.mod 和 docs/)", dir)
	return ""
}

// TestPathTokenOf 路标形状判定。
func TestPathTokenOf(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		isRef bool
	}{
		// —— 是路标 ——
		{"pkg/oss/aliyun/aliyun.go", "pkg/oss/aliyun/aliyun.go", true},
		{"pkg/oss/aliyun/aliyun.go:1-280", "pkg/oss/aliyun/aliyun.go", true},
		{"pkg/oss/aliyun/advanced.go:169-174", "pkg/oss/aliyun/advanced.go", true},
		{"service/auth_impl.go:VerifySms", "service/auth_impl.go", true},
		{"pkg/httputil/", "pkg/httputil", true},
		{"pkg/httputil", "pkg/httputil", true},
		{"pkg/msn/", "pkg/msn", true},
		{"web/src/utils/sentry.ts", "web/src/utils/sentry.ts", true},
		{"config/__tests__/audit.test.tsx", "config/__tests__/audit.test.tsx", true},
		{"configs/config.example.yaml", "configs/config.example.yaml", true},
		{"deploy/systemd/rapid-admin.service", "deploy/systemd/rapid-admin.service", true},
		{"cmd/server/main.go", "cmd/server/main.go", true},
		{"pkg/swaggerx/", "pkg/swaggerx", true},

		// —— 拼错的扩展名**也得看得见** ——
		//
		// ⚠️ 这条是探针逼出来的:把 `deploy/systemd/rapid-admin.service`
		// 的扩展名拼成 `.servce` ⇒ 那个文件**不存在**,但守卫**一声不吭**
		// (因为 ".servce" 不在扩展名白名单里)。
		// ⇒ "像不像文件名"不能靠"认不认得它",只能靠**形状**。
		{"deploy/systemd/rapid-admin.servce", "deploy/systemd/rapid-admin.servce", true},

		// —— 不是路标 ——
		{"pkg/msn.Sender", "", false},                             // 类型
		{"pkg/sms.VerifyStore", "", false},                        // 类型
		{"authz.CacheBackend", "", false},                         // 类型(扩展名不是全小写)
		{"pkg/cachex/TwoLevel", "", false},                        // 类型
		{"sys_sms_code", "", false},                               // 表名
		{"0431415", "", false},                                    // commit 号
		{"/swagger/doc.json", "", false},                          // URL 路径
		{"github.com/aliyun/aliyun-oss-go-sdk v3.0.2", "", false}, // 依赖坐标(带空格)
		{"SignUploadToken", "", false},                            // 函数名
		{"provider.go", "", false},                                // 单文件名(本仓有重名,查了也是噪音)
	}
	for _, c := range cases {
		got, ok := pathTokenOf(c.in)
		if ok != c.isRef {
			t.Errorf("pathTokenOf(%q) 是路标=%v,期望 %v", c.in, ok, c.isRef)
			continue
		}
		if ok && got != c.want {
			t.Errorf("pathTokenOf(%q) = %q,期望 %q", c.in, got, c.want)
		}
	}
}

// TestFindMarkers_批量删除不算删除陈述
//
// §1.10 里 "`Delete` 批量删除(按 public/ 前缀自动分桶)" —— 那是
// OSS Delete 方法的正常描述,不是"这里删过东西"。
func TestFindMarkers_批量删除不算删除陈述(t *testing.T) {
	line := "| 3 | 阿里云 OSS provider | `pkg/oss/aliyun/aliyun.go:1-280` …+ `Delete` 批量删除(按前缀自动分桶);`SafeKey` 防 path traversal |"
	markers := findMarkers(line)
	for _, m := range markers {
		if strings.Contains(m.text, "删除") {
			t.Errorf("「批量删除」被当成了删除陈述: %+v", m)
		}
	}
}

// TestExemptByDeletion_删除动词只解释它旁边那个路标
//
// 这一条守的是整条判定的地基。判据 = 删除动词与路标之间
// **没有别的路标**隔在中间。
//
// ⚠️ "别的路标"不是"别的反引号段" —— 类型名(`authz.CacheBackend`)
// 不算。清单第 637 行正是这个形状:
//
//	`repository/principal_cache.go` 与 `authz.CacheBackend` 一并删除
//
// 用"反引号段"当判据会把这一行误报(文件确实没了,但那是在讲删除历史)。
func TestExemptByDeletion_删除动词只解释它旁边那个路标(t *testing.T) {
	t.Run("类型名隔在中间不算路标", func(t *testing.T) {
		line := "| `repository/principal_cache.go` 与 `authz.CacheBackend` 一并删除 |"
		spans := pathSpansOf(t, line)
		if len(spans) != 1 {
			t.Fatalf("这一行应该只有 1 个路标,实际 %d 个: %+v", len(spans), spans)
		}
		markers := findMarkers(line)
		if len(markers) == 0 {
			t.Fatalf("没找到删除动词")
		}
		if !exemptByDeletion(line, spans, markers, spans[0]) {
			t.Errorf("`repository/principal_cache.go` 旁边的「一并删除」应该把它算成历史陈述")
		}
	})

	t.Run("另一个路标隔在中间就不算", func(t *testing.T) {
		line := "| `pkg/oss/one.go` | `pkg/oss/two.go` **已删除** |"
		spans := pathSpansOf(t, line)
		if len(spans) != 2 {
			t.Fatalf("这一行应该只有 2 个路标,实际 %d 个: %+v", len(spans), spans)
		}
		markers := findMarkers(line)
		if len(markers) == 0 {
			t.Fatalf("没找到删除动词")
		}
		if !exemptByDeletion(line, spans, markers, spans[1]) {
			t.Errorf("`pkg/oss/two.go` 紧挨着「已删除」,应该算成历史陈述")
		}
		if exemptByDeletion(line, spans, markers, spans[0]) {
			t.Errorf("`pkg/oss/one.go` 与「已删除」之间隔着 `pkg/oss/two.go`,不该被算成历史陈述")
		}
	})
}

// pathSpansOf 抽出一行里**真的是路标**的反引号段。
func pathSpansOf(t *testing.T, line string) []codeSpan {
	t.Helper()
	var out []codeSpan
	for _, sp := range extractCodeSpans(line) {
		if _, ok := pathTokenOf(sp.content); ok {
			out = append(out, sp)
		}
	}
	return out
}

// TestCheckDocRefs_抓断链的路标(可逆探针)
//
// 造一个临时仓库 + 一份文档,验证:
//   - 指向不存在路径的路标 ⇒ 报出来;
//   - 指向存在路径的路标 ⇒ 不报;
//   - 写着"已删除"的历史陈述 ⇒ 不报;
//   - 不是仓库路径的东西(相对 URL / 错误码区间)⇒ 不报;
//   - **扩展名拼错**的路标 ⇒ 报出来,而且提示里给出同名词干的真文件
//     (探针把 `.service` 拼成 `.servce` 时,守卫当时一声不吭 ——
//     那次修完扩展名规则,但"只报个错不给答案"还是差点让人多翻一遍仓)。
func TestCheckDocRefs_抓断链的路标(t *testing.T) {
	root := t.TempDir()
	// 造一个最小的仓库形状:server/internal/pkg/oss/oss.go + docs/
	mustMkdir(t, filepath.Join(root, "server", "internal", "pkg", "oss"))
	mustMkdir(t, filepath.Join(root, "docs"))
	mustWrite(t, filepath.Join(root, "server", "internal", "pkg", "oss", "oss.go"), "package oss\n")
	mustWrite(t, filepath.Join(root, "docs", "t.md"), strings.Join([]string{
		"| 现役 | `pkg/oss/oss.go` | 好的 |",
		"| 断链 | `pkg/oss/gone.go` | 坏的 |",
		"| 扩展名拼错 | `pkg/oss/oss.goo` | 坏的 |",
		"| 历史 | `pkg/oss/old.go` 已删除(M112) | 不报 |",
		"| 路由 | `owner/system/users` | 不报 |",
		"| 错误码 | `40100/40300` | 不报 |",
		"| 类型 | `pkg/oss.Thing` | 不报 |",
	}, "\n"))

	issues, err := CheckDocRefs(root, []string{"docs/t.md"})
	if err != nil {
		t.Fatalf("扫文档失败: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("应该只报 2 条(断链 + 扩展名拼错),实际 %d 条: %+v", len(issues), issues)
	}
	byToken := map[string]DocRefIssue{}
	for _, is := range issues {
		byToken[is.Token] = is
	}
	gone, ok := byToken["pkg/oss/gone.go"]
	if !ok {
		t.Fatalf("没报出 `pkg/oss/gone.go`: %+v", issues)
	}
	if gone.Line != 2 {
		t.Errorf("行号报的是 %d,期望 2", gone.Line)
	}
	typo, ok := byToken["pkg/oss/oss.goo"]
	if !ok {
		t.Fatalf("没报出 `pkg/oss/oss.goo`(扩展名拼错的那条): %+v", issues)
	}
	if typo.Line != 3 {
		t.Errorf("行号报的是 %d,期望 3", typo.Line)
	}
	if !strings.Contains(typo.Note, "oss.go") {
		t.Errorf("扩展名拼错时,提示里应给出同名词干的真文件,实际提示是 %q", typo.Note)
	}
}

// TestCheckDocRefs_扫描集里没有断链的路标
//
// ⚠️ 这是本守卫的主断言:扫描集里的每一份文档,它上面的每一个路径都必须
// 还指得到东西 —— 除非那一处明写着"已删除"。
//
// 清单是排期的人唯一会去读的文档,README 是仓库结构说明 —— 两个都是
// "描述现状"的,所以它们上面的反引号路径,读者都当成指路牌。
func TestCheckDocRefs_扫描集里没有断链的路标(t *testing.T) {
	root := repoRoot(t)
	issues, err := CheckDocRefs(root, ScannedDocs)
	if err != nil {
		t.Fatalf("扫文档失败: %v", err)
	}
	if len(issues) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("\n文档里有断链的路标:\n")
	for _, is := range issues {
		b.WriteString("  " + is.String() + "\n")
		if is.Note != "" {
			b.WriteString("      " + is.Note + "\n")
		}
	}
	t.Fatal(b.String())
}

// TestScannedDocs_每一份都真的存在
//
// ⚠️ 抄 M129 的那条规矩:allowlist 不许留已经不存在的名字。
//
// 这里留一条不存在的文件名 = **悄悄把那份文档移出覆盖范围**,而且
// 没人会发现(守卫扫不到它 ⇒ 扫不到 ⇒ 没有报错 ⇒ 一切"正常")。
// 这种后门比没有守卫还坏:它让人以为文档有人看着。
func TestScannedDocs_每一份都真的存在(t *testing.T) {
	root := repoRoot(t)
	if len(ScannedDocs) == 0 {
		t.Fatal("扫描集是空的 —— 守卫什么都没看,却看上去是\"绿了\"")
	}
	for _, d := range ScannedDocs {
		p := filepath.Join(root, filepath.FromSlash(d))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("扫描集里的 %s 不存在: %v", d, err)
		}
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("建目录 %s 失败: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件 %s 失败: %v", p, err)
	}
}
