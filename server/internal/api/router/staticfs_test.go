// 静态挂载的集成测试 —— 设计文档 §7 明确要求这一条
//
// 理由:§7 的 `PathRewrite` 剥除段数算错、`Root` 少写一层 path_root,
// **症状都是"请求命中了路由、然后 404"**,没有任何报错。只靠读代码
// review 是抓不住的,必须真的 GET 一个已知存在的文件。
//
// ⚠️ **挂的是整棵树** —— 存储侧没有"公开/私有"两套布局,私密靠 key 不可猜
// (见 oss/path.go 的文件头)。所以这里没有"私有文件不该被读到"这种断言;
// 取而代之的是"**树外**的文件读不到"。
package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	appcfg "rapid-admin/internal/config"
)

// fixtureRoot 造一个可复用可清理的 fixture 根目录。
//
// ⚠️ **不能用 t.TempDir()**:hertz 的 fsHandler 把打开的文件句柄缓存
// `FSHandlerCacheDuration`(10 秒,consts/fs.go:52),后台清理协程 5 秒一轮 ——
// t.TempDir 的自动清理在测试一结束就触发,Windows 上必然删不掉(断言全过,
// 但整条用例会以 "TempDir RemoveAll cleanup" 失败收场)。
//
// 折中:固定目录名 + **每次跑的开头先清一遍**。本进程删不掉的那份,
// 下次跑的开头一定删得掉(上一个进程已经退出、句柄已释放),所以
// %TEMP% 下最多留一个目录,不会越攒越多。
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(os.TempDir(), "ha-fs-static-fixture")
	_ = os.RemoveAll(root) // 清上一次可能没删掉的那份
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) }) // 尽力而为,失败留给下次跑的开头
	return root
}

// staticFixture 造一个磁盘布局,返回绝对 root 和 cfg
func staticFixture(t *testing.T) (string, *appcfg.Server) {
	t.Helper()
	root := fixtureRoot(t)
	// 与 local provider 的落盘规则一致:磁盘路径 = <root>/<key>,
	// 而 key = <path_root>/<uid>/<biz>/<folderID>/<name>
	dir := filepath.Join(root, "res", "1", "house", "7")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("JPEGDATA"), 0o640); err != nil {
		t.Fatal(err)
	}
	// ⚠️ **树外的文件** —— 静态挂载的 Root 是 `<root>/res`,这个在 `<root>`
	// 下面一级,不该被服务到。它是穿越测试的靶子。
	if err := os.WriteFile(filepath.Join(root, "secret.txt"), []byte("OUTSIDE"), 0o640); err != nil {
		t.Fatal(err)
	}

	return root, &appcfg.Server{File: appcfg.FileConfig{
		Provider: "local",
		Root:     root,
		PathRoot: "res",
	}}
}

// newStaticEngine 挂一个和 Register 走同一条路径的引擎
func newStaticEngine(cfg *appcfg.Server) *server.Hertz {
	h := server.New()
	mountFileStatic(h, cfg)
	return h
}

func get(t *testing.T, h *server.Hertz, url string) *ut.ResponseRecorder {
	t.Helper()
	w := ut.PerformRequest(h.Engine, "GET", url, nil)
	return w
}

// TestMountFileStatic_ServesKnownFile 是 §7 要求的那条主用例:
// 直接 GET 一个已知存在的文件,断言 200 + 内容对得上。
func TestMountFileStatic_ServesKnownFile(t *testing.T) {
	_, cfg := staticFixture(t)
	h := newStaticEngine(cfg)
	w := get(t, h, "/res/1/house/7/a.jpg")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (PathRewrite 段数或 Root 拼错了)", w.Code)
	}
	if w.Body.String() != "JPEGDATA" {
		t.Fatalf("body = %q, want JPEGDATA", w.Body.String())
	}
}

// TestStaticFSTarget_RootAndStrip 直接断言算出来的 mount / root / strip,
// 而不是靠 HTTP 往返反推。
//
// ⚠️ 为什么要单独断言:"Root 少写一层 path_root"这种错**在 HTTP 上测不出来**
// —— 请求会先命中路由、然后 404,和"文件不存在"是同一个现象。
func TestStaticFSTarget_RootAndStrip(t *testing.T) {
	base := t.TempDir()
	cfg := appcfg.FileConfig{Root: base, PathRoot: "res"}
	mount, root, strip, ok := staticFSTarget(cfg)
	if !ok {
		t.Fatal("配了 path_root 却算出 ok=false")
	}
	if mount != "/res" || strip != 1 {
		t.Fatalf("mount=%q strip=%d, want /res + 1", mount, strip)
	}
	if want := filepath.Join(base, "res"); root != want {
		t.Fatalf("root = %q, want %q", root, want)
	}
}

// TestStaticFSTarget_DisabledWhenNoPathRoot path_root 为空 → 不挂。
//
// ⚠️ path_root 为空时挂载点会变成空串,静态路由挂不上,而直链会变成
// "/1/house/7/a.jpg"(少了第一段)—— 每个文件的链接都 404,而且没有任何
// 日志提示它跟 path_root 有关。
func TestStaticFSTarget_DisabledWhenNoPathRoot(t *testing.T) {
	_, _, _, ok := staticFSTarget(appcfg.FileConfig{Root: t.TempDir()})
	if ok {
		t.Fatal("path_root 为空时 ok 应为 false")
	}
	_, _, _, ok = staticFSTarget(appcfg.FileConfig{Root: t.TempDir(), PathRoot: "  "})
	if ok {
		t.Fatal("path_root 只有空白时 ok 应为 false")
	}
}

// TestStaticFSTarget_NotMountedForAliyun **文件在 OSS 上时不挂本机磁盘树**
//
// ⚠️ 挂了会怎样:本机磁盘上切 provider 之前存的旧文件**照常能访问**,
// 而 URL 形状跟 OSS 直链一模一样(`/rapid-admin/...`)⇒
//   · 本机有的旧文件 → 200(内容是过期的,OSS 上根本没有这个对象)
//   · 只有 OSS 有的新文件 → 404
// 症状是"切到 OSS 之后有的文件能开有的不能,而且随机",日志里只有一条
// "file static fs mounted"。
//
// ⚠️ **判定必须是"不是 local 就不挂"**,不能是"等于 local 才挂" ——
// 后者在 yaml 漏写 provider 时(空)会把本机树挂出去,而文件其实在 OSS 上。
func TestStaticFSTarget_NotMountedForAliyun(t *testing.T) {
	base := t.TempDir()
	for _, prov := range []string{"aliyun", "ALIYUN", " aliyun "} {
		_, _, _, ok := staticFSTarget(appcfg.FileConfig{Provider: prov, Root: base, PathRoot: "res"})
		if ok {
			t.Errorf("provider=%q 时不该挂本机磁盘树", prov)
		}
	}
	// 空 = local(全局约定),仍然要挂
	if _, _, _, ok := staticFSTarget(appcfg.FileConfig{Root: base, PathRoot: "res"}); !ok {
		t.Error("provider 为空(默认 local)时应该挂")
	}
}

// TestMountFileStatic_NotServedWhenAliyun HTTP 层的确认:aliyun 模式下
// 本机磁盘上的文件**取不到**(不是 404 的另一种,而是压根没这个路由)。
func TestMountFileStatic_NotServedWhenAliyun(t *testing.T) {
	_, cfg := staticFixture(t)
	cfg.File.Provider = "aliyun"
	h := newStaticEngine(cfg)
	w := get(t, h, "/res/1/house/7/a.jpg")
	if w.Code == http.StatusOK {
		t.Fatalf("aliyun 模式下本机磁盘的文件还能被读到:code=%d body=%q", w.Code, w.Body.String())
	}
}

// TestMountFileStatic_TreeOutsideNotReachable 锁住"树外读不到"。
//
// ⚠️ 这条**测不出 Root 挂低**(挂到 `<root>` 的话 `<root>/secret.txt` 反而
// 能被读到,这条会红)。Root 那一侧由 TestStaticFSTarget_RootAndStrip
// 直接断言,两条合起来才完整。
func TestMountFileStatic_TreeOutsideNotReachable(t *testing.T) {
	_, cfg := staticFixture(t)
	h := newStaticEngine(cfg)
	// 穿越到树外
	for _, u := range []string{
		"/res/../secret.txt",
		"/res/1/house/7/../../../../secret.txt",
		"/res/%2e%2e/secret.txt",
	} {
		w := get(t, h, u)
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "OUTSIDE") {
			t.Fatalf("穿越路径读到了树外的文件:%s body=%q", u, w.Body.String())
		}
	}
}

// TestMountFileStatic_NoDirectoryListing GenerateIndexPages=false 的直接体现。
func TestMountFileStatic_NoDirectoryListing(t *testing.T) {
	_, cfg := staticFixture(t)
	h := newStaticEngine(cfg)
	w := get(t, h, "/res/1/house/7/")
	if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "a.jpg") {
		t.Fatalf("开出了目录列表:body=%q", w.Body.String())
	}
}

// TestMountFileStatic_RangeSupported 视频拖进度条依赖 206
func TestMountFileStatic_RangeSupported(t *testing.T) {
	_, cfg := staticFixture(t)
	h := newStaticEngine(cfg)
	w := ut.PerformRequest(h.Engine, "GET", "/res/1/house/7/a.jpg", nil,
		ut.Header{Key: "Range", Value: "bytes=0-3"})
	if w.Code != http.StatusPartialContent {
		t.Fatalf("Range 请求 code = %d, want 206", w.Code)
	}
	if w.Body.String() != "JPEG" {
		t.Fatalf("Range body = %q, want JPEG", w.Body.String())
	}
}

func TestURLSegments(t *testing.T) {
	cases := map[string]int{
		"/res":       1,
		"res":        1,
		"/a/b":       2,
		"/":          0,
		"":           0,
		"/res/":      1,
		"//res//pub": 3,
	}
	for in, want := range cases {
		if got := urlSegments(in); got != want {
			t.Errorf("urlSegments(%q)=%d want %d", in, got, want)
		}
	}
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
