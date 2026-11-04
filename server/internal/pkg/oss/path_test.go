package oss

import (
	"errors"
	"strings"
	"testing"
)

// ── SafeKey ──────────────────────────────────────────────────────────

// TestSafeKey 这组断言逐条对应 path.go 里的一行守卫,每一行删掉都该变红。
func TestSafeKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want bool
		why  string
	}{
		{"正常 ASCII", "res/123/asset/456/a.pdf", true, ""},
		{"正常中文", "res/123/asset/456/户型图.pdf", true, "中文文件名是常态,不许被 ASCII 白名单拒掉"},
		{"混排", "res/123/asset/456/宣传片 (1).mp4", true, ""},
		{"点开头文件", "res/123/asset/456/.gitignore", true, "键名合法"},

		{"空串", "", false, "删掉 len==0 判断会红"},
		{"单斜杠", "/res/a", false, "删掉 HasPrefix('/') 会红"},
		{"尾斜杠", "res/a/", false, "删掉 HasSuffix('/') 会红"},
		{"空段", "res//a", false, "双斜杠会在 Join 时拼出意料外的路径"},
		{"点穿越", "res/../../../etc/passwd", false, "删掉 Contains('..') 会红 —— 本模块最关键的一条"},
		{"名字里有点穿越", "res/123/a..b.pdf", false, "宁可误杀也不能放过"},
		{"反斜杠", `res\123\a.pdf`, false, "Windows 路径分隔符,删掉会红"},
		{"控制字符", "res/123/a\x00b", false, "删掉 unicode.IsControl 会红"},
		{"换行", "res/123/a\nb", false, "会在日志里制造假行"},
		{"超长", strings.Repeat("a", maxKeyBytes+1), false, "撑爆 file_key 列"},
		{"非法 UTF-8", "res/123/\xff\xfe.pdf", false, "JSON 序列化时才炸就晚了"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SafeKey(c.key); got != c.want {
				t.Fatalf("SafeKey(%q) = %v,want %v(%s)", c.key, got, c.want, c.why)
			}
		})
	}
}

// TestSafeKey_LenBoundary 边界值:正好 512 字节要过,513 不过。
// 这条盯的是 `> maxKeyBytes` 的比较符 —— 写成 `>=` 或 `<` 都只错一侧。
func TestSafeKey_LenBoundary(t *testing.T) {
	prefix := "res/1/asset/2/"
	exact := prefix + strings.Repeat("a", maxKeyBytes-len(prefix))
	if len(exact) != maxKeyBytes {
		t.Fatalf("前置条件:构造的 key 应正好 %d 字节,got %d", maxKeyBytes, len(exact))
	}
	if !SafeKey(exact) {
		t.Fatalf("正好 %d 字节应通过", maxKeyBytes)
	}
	if SafeKey(exact + "a") {
		t.Fatalf("%d 字节应被拒", maxKeyBytes+1)
	}
}

// ── 文件名清洗 ────────────────────────────────────────────────────────

func TestSanitizeObjectName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"原样", "户型图.pdf", "户型图.pdf"},
		{"剥 unix 路径", "a/b/c/户型图.pdf", "户型图.pdf"},
		{"剥 windows 路径", `a\b\c\户型图.pdf`, "户型图.pdf"},
		{"剥完为空", "///", ""}, // 上面单独测了 err 分支,这里只测剥的结果形态

		{"控制字符", "a\x00b.txt", "a_b.txt"},
		{"换行", "a\nb.txt", "a_b.txt"},
		{"windows 禁用集", `a<b>c:d"e|f?g*h.txt`, "a_b_c_d_e_f_g_h.txt"},
		{"保留点横杠", "户型图-v1.2_final.pdf", "户型图-v1.2_final.pdf"},
		{"保留中文括号", "宣传片(1).mp4", "宣传片(1).mp4"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SanitizeObjectName(c.in)
			if c.want == "" {
				// 剥完为空是**错误**而不是空串返回
				if err == nil {
					t.Fatalf("期望报错,got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q,want %q", got, c.want)
			}
		})
	}
}

func TestSanitizeObjectName_Rejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"空串", ""},
		{"纯点", "."},
		{"双点", ".."},
		{"斜杠到底", "///"},
		{"反斜杠到底", `\\\`},
		{"超长中文", strings.Repeat("户", maxObjectNameBytes/3+1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SanitizeObjectName(c.in)
			if err == nil {
				t.Fatalf("期望报错,got %q", got)
			}
			var e *ErrInvalidObjectName
			if !errors.As(err, &e) {
				t.Fatalf("错误类型 = %T,want *ErrInvalidObjectName", err)
			}
		})
	}
}

// TestSanitizeObjectName_NFC 是这条清洗存在的**主要理由**,必须能盯住它。
//
// 「户」这类汉字在 macOS 上被存成 NFD(组合字符),在 Windows/Linux 上是 NFC。
// 不归一化的话,同一个视觉文件名在两个平台上会变成两个 key —— 文件夹列表
// 里看起来就是"同一份文件出现了两次",而且删一份不删另一份就永远删不干净。
func TestSanitizeObjectName_NFC(t *testing.T) {
	// ⚠️ 下面两个常量**必须**是两种不同的字节形式:nfc 是 1 字节的预组合 é
	//    (U+00E9),nfd 是 2 字节的 e + U+0301。写这个用例时第一版两个常量
	//    被编辑器归一化成了同一串,直接被下面第一条前置条件断言挡下来了 ——
	//    那道断言不是装饰,是这个用例能被信任的前提。
	const (
		nfc = "é.pdf"  // U+00E9 预组合
		nfd = "é.pdf" // 'e' + U+0301 组合尖音符
		han = "戸.pdf"  // 日语「戸」(兼容字符,NFKC 才映射,NFC 不动它)
	)
	if nfc == nfd {
		t.Fatalf("前置条件:两个常量必须真的是不同的字节序列 —— nfc=%x nfd=%x", nfc, nfd)
	}
	if !strings.Contains(nfd, "́") {
		t.Fatalf("前置条件:nfd 必须是分解形(应含 U+0301),实际 %x", nfd)
	}
	a, err := SanitizeObjectName(nfc)
	if err != nil {
		t.Fatalf("NFC 输入不该报错: %v", err)
	}
	b, err := SanitizeObjectName(nfd)
	if err != nil {
		t.Fatalf("NFD 输入不该报错: %v", err)
	}
	if a != b {
		t.Fatalf("NFC/NFD 归一化后必须相同:\n NFC %x → %q\n NFD %x → %q", nfc, a, nfd, b)
	}
	if _, err := SanitizeObjectName(han); err != nil {
		t.Fatalf("日文兼容字符不该被拒: %v", err)
	}
}

// ── BuildKey ──────────────────────────────────────────────────────────

func TestBuildKey(t *testing.T) {
	base := PathSpec{UserID: 123, Biz: "asset", FolderID: 456, ObjectName: "a.pdf"}

	t.Run("默认", func(t *testing.T) {
		k, err := BuildKey(base)
		if err != nil {
			t.Fatal(err)
		}
		if want := DefaultRoot + "/123/asset/456/a.pdf"; k != want {
			t.Fatalf("got %q,want %q", k, want)
		}
	})

	t.Run("中文对象名", func(t *testing.T) {
		s := base
		s.ObjectName = "户型图.pdf"
		k, err := BuildKey(s)
		if err != nil {
			t.Fatalf("中文对象名必须能拼出 key: %v", err)
		}
		if !strings.HasSuffix(k, "户型图.pdf") {
			t.Fatalf("原名必须留在末尾: %q", k)
		}
	})

	t.Run("自定义 root", func(t *testing.T) {
		s := base
		s.Root = "media"
		k, err := BuildKey(s)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(k, "media/") {
			t.Fatalf("got %q", k)
		}
	})
}

// TestBuildKey_Rejects 逐条盯住 validateSegment 和那几个零值守卫。
func TestBuildKey_Rejects(t *testing.T) {
	good := PathSpec{UserID: 123, Biz: "asset", FolderID: 456, ObjectName: "a.pdf"}
	mut := func(f func(*PathSpec)) PathSpec {
		s := good
		f(&s)
		return s
	}
	cases := []struct {
		name string
		spec PathSpec
	}{
		{"biz 大写", mut(func(s *PathSpec) { s.Biz = "Asset" })},
		{"biz 含中文", mut(func(s *PathSpec) { s.Biz = "户" })},
		{"biz 以数字开头", mut(func(s *PathSpec) { s.Biz = "1asset" })},
		{"biz 空", mut(func(s *PathSpec) { s.Biz = "" })},
		{"biz 超长", mut(func(s *PathSpec) { s.Biz = strings.Repeat("a", 33) })},
		{"biz 含点", mut(func(s *PathSpec) { s.Biz = "a.b" })},
		{"biz 以连字符开头", mut(func(s *PathSpec) { s.Biz = "-a" })},
		{"userID 为 0", mut(func(s *PathSpec) { s.UserID = 0 })},
		{"folderID 为 0", mut(func(s *PathSpec) { s.FolderID = 0 })},
		{"root 大写", mut(func(s *PathSpec) { s.Root = "RES" })},
		{"对象名带斜杠", mut(func(s *PathSpec) { s.ObjectName = "a/b.pdf" })},
		{"对象名带点穿越", mut(func(s *PathSpec) { s.ObjectName = "..pdf" })},
		{"对象名为空", mut(func(s *PathSpec) { s.ObjectName = "" })},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, err := BuildKey(c.spec)
			if err == nil {
				t.Fatalf("期望报错,got key %q", k)
			}
			var e *ErrInvalidKey
			var e2 *ErrInvalidObjectName
			if !errors.As(err, &e) && !errors.As(err, &e2) {
				t.Fatalf("错误类型 = %T,应该是 *ErrInvalidKey 或 *ErrInvalidObjectName", err)
			}
		})
	}
}

// ── 对象名生成 ────────────────────────────────────────────────────────

func TestNewObjectName(t *testing.T) {
	name, err := NewObjectName("户型图.pdf")
	if err != nil {
		t.Fatal(err)
	}
	// {unix}-{rand8}-{原名}
	parts := strings.SplitN(name, "-", 3)
	if len(parts) != 3 {
		t.Fatalf("对象名应有三段 {unix}-{rand8}-{name},got %q", name)
	}
	if len(parts[0]) != 10 {
		t.Fatalf("时间戳应为 10 位,got %q", parts[0])
	}
	if len(parts[1]) != 8 {
		t.Fatalf("随机段应为 8 位 hex,got %q", parts[1])
	}
	if parts[2] != "户型图.pdf" {
		t.Fatalf("原名必须完整保留在末尾,got %q", parts[2])
	}
}

// TestNewObjectName_Uniqueness 前缀里那个随机段就是防这个的 ——
// 同一秒内同目录两次上传同名文件,没有它就会撞。
func TestNewObjectName_Uniqueness(t *testing.T) {
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		n, err := NewObjectName("户型图.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if seen[n] {
			t.Fatalf("第 %d 次生成撞名:%q", i, n)
		}
		seen[n] = true
	}
}

func TestObjectExt(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a.pdf", ".pdf"},
		{"1730483200-ab12cd34-户型图.pdf", ".pdf"},
		{"宣传片 (1).MP4", ".MP4"},
		{"README", ""},
		{".gitignore", ""},                   // 点开头不算扩展名
		{"a." + strings.Repeat("x", 20), ""}, // 不合理的长扩展名
	}
	for _, c := range cases {
		if got := ObjectExt(c.in); got != c.want {
			t.Fatalf("ObjectExt(%q) = %q,want %q", c.in, got, c.want)
		}
	}
}

func TestFolderPrefix(t *testing.T) {
	p, err := FolderPrefix("", 123, "asset", 456)
	if err != nil {
		t.Fatal(err)
	}
	if want := DefaultRoot + "/123/asset/456/"; p != want {
		t.Fatalf("got %q,want %q", p, want)
	}
	if !strings.HasSuffix(p, "/") {
		t.Fatalf("prefix 必须带尾斜杠,否则 res/1/2/3/ 会误匹配 res/1/2/30/,got %q", p)
	}
	if _, err := FolderPrefix("", 0, "asset", 456); err == nil {
		t.Fatal("userID=0 应报错")
	}
}

// ── ParseKey ──────────────────────────────────────────────────────────

// TestParseKey 盯住"段数是死的"这条规则。
//
// ⚠️ **为什么这条规则重要**:ParseKey 是 key 的唯一反解入口,归属校验
// (这个 key 属不属于当前用户)全靠它。旧版"尽力而为"解析出一半的做法会在
// key 形态变了的时候**错位取到别人的 uid** —— 归属校验静默失效,文件被别人
// 删掉/改掉都不报错。所以段数不对必须**整条拒收**,不许猜。
func TestParseKey(t *testing.T) {
	t.Run("正常", func(t *testing.T) {
		p, err := ParseKey("", DefaultRoot+"/123/asset/456/a.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if p.Root != DefaultRoot || p.UserID != 123 || p.Biz != "asset" || p.FolderID != 456 || p.ObjectName != "a.pdf" {
			t.Fatalf("解析结果不对: %+v", p)
		}
	})

	t.Run("中文对象名", func(t *testing.T) {
		p, err := ParseKey("", DefaultRoot+"/123/asset/456/户型图.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if p.ObjectName != "户型图.pdf" {
			t.Fatalf("got %q", p.ObjectName)
		}
	})

	t.Run("自定义 root", func(t *testing.T) {
		p, err := ParseKey("media", "media/123/asset/456/a.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if p.Root != "media" {
			t.Fatalf("got %q", p.Root)
		}
	})

	// ⚠️ 下面几条是"少一段 / 多一段"——**必须整条拒收**,不能解析出一半。
	// 尤其 "res/public/123/asset/456/a.pdf"(6 段,旧公开布局):它现在是
	// 非法 key,不是"某个用户的公开文件"。
	bad := []struct {
		name string
		key  string
	}{
		{"少一段", "res/123/asset/a.pdf"},
		{"多一段(旧公开布局)", "res/public/123/asset/456/a.pdf"},
		{"多两段", "res/a/b/c/456/a.pdf"},
		{"root 不匹配", "media/123/asset/456/a.pdf"},
		{"uid 非法", "res/abc/asset/456/a.pdf"},
		{"uid 为 0", "res/0/asset/456/a.pdf"},
		{"fid 非法", "res/123/asset/xyz/a.pdf"},
		{"fid 为 0", "res/123/asset/0/a.pdf"},
		{"biz 含中文", "res/123/户/456/a.pdf"},
		{"空串", ""},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			p, err := ParseKey("", c.key)
			if err == nil {
				t.Fatalf("期望报错,got %+v", p)
			}
			var e *ErrInvalidKey
			if !errors.As(err, &e) {
				t.Fatalf("错误类型 = %T,want *ErrInvalidKey", err)
			}
		})
	}
}

// TestParseKey_BuildKeyRoundTrip BuildKey 拼出来的 key 必须能被 ParseKey 原样解回去。
//
// 这条防的是"拼的时候 5 段、解的时候按 4 段的下标取"这类错位 —— 它平时
// 不一定报错,只是 uid 取到了别的段。
func TestParseKey_BuildKeyRoundTrip(t *testing.T) {
	spec := PathSpec{UserID: 123, Biz: "asset", FolderID: 456, ObjectName: "户型图.pdf"}
	key, err := BuildKey(spec)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseKey("", key)
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != spec.UserID || p.Biz != spec.Biz || p.FolderID != spec.FolderID || p.ObjectName != spec.ObjectName {
		t.Fatalf("往返不一致: %+v vs %+v", p, spec)
	}
}

// ── 可逆探针记录(2026-10-22)────────────────────────────────────────────
//
// 规矩:改了 path.go,先按下面这张表把实现拆回去看用例会不会红。红不了 = 用例
// 假绿,比没有用例更危险(会让人以为这块有网)。
//
//   实跑结果                          拆法                                    必须变红
//   ─────────────────────────────  ──────────────────────────────────────  ──────────────
//   ✅ RED                          S1 删 SafeKey 的 Contains("..")          点穿越 + 名字里有点穿越
//   ✅ RED                          S2 删 SafeKey 的 unicode.IsControl       控制字符 + 换行
//   ✅ RED                          S3 剥路径 LastIndexAny → Index          剥 unix/windows 路径
//   ✅ RED                          S4 删 NFC 归一化那一行                   TestSanitizeObjectName_NFC
//   ✅ RED                          S5 删 validateSegment("biz", ...) 调用    biz 那 7 条
//   ✅ RED                          S6 删 BuildKey 的 UserID==0 守卫         userID 为 0
//   ✅ RED                          S7 NewObjectName 的随机段改常量          TestNewObjectName_Uniqueness
//   ✅ RED                          S8 maxKeyBytes 的 > 改成 >=               TestSafeKey_LenBoundary
//   ✅ RED                          S9 删内部空段检查(//)                   TestSafeKey/空段
//   ⬜ GREEN(**预期之内**)           S10 删 BuildKey 末尾的 SafeKey           —— 没有任何用例会红
//
//   ⚠️ **S10 测不到,这是有理由的,不是漏写用例**。它是纵深防御的第二层:上面每一段
//   (userID 零值 / biz 段校验 / 对象名字符)都已经在校验,构造不出"每段都合法但拼起来
//   不合法"的输入。要让它红就得先放松前面某一条,那测的就不是 S10 了。记下来,不假装
//   它被盯住 —— 它的价值是"上面某条规则将来被改松时"兜底。
//
//   ⚠️ **S7 的第一版探针报 BROKE(编译失败)而不是 RED**:把 `hex.EncodeToString(b[:])`
//   换成常量之后 `crypto/rand` 和 `encoding/hex` 同时变成未使用引用,包直接编不过。
//   **BROKE 是探针写坏了,不是用例盯住了** —— 第二版同时替换两处才拿到有意义的红:
//   `第 1 次生成撞名:"1792649898-aabbccdd-帪型图.pdf"`。
//
//   ⚠️ root 的断言和 biz 的断言**必须分开写**:S5 只拆 biz 那一条,所以
//   `TestBuildKey_Rejects/root 大写` 不受 S5 影响 —— 混在一起写就会误以为 root 段
//   也被 S5 的结果覆盖了。
//
//   ✅ 另外有一条不是探针发现的,是**用例自己首跑就红的**:SafeKey 最初只查了
//   HasPrefix/HasSuffix 的斜杠,漏了内部空段,`res//a` 漏过去了。是
//   `TestSafeKey/空段` 在第一次运行时报出来的(S9 就是为它留的回归)。
// ────────────────────────────────────────────────────────────────────────
