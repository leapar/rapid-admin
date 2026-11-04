package local

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"rapid-admin/internal/pkg/oss"
)

// ── 测试脚手架 ──────────────────────────────────────────────────────────

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := New(Config{
		Root:    t.TempDir(),
		URLBase: "/res",
	})
	if err != nil {
		t.Fatalf("构造 provider 失败: %v", err)
	}
	return p
}

// mkContent 生成确定性内容(不用 rand,失败时能靠肉眼看内容定位)
func mkContent(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i%251)
	}
	return b
}

func putFile(t *testing.T, p *Provider, key string, content []byte) *oss.ObjectMeta {
	t.Helper()
	m, err := p.Put(context.Background(), key, bytes.NewReader(content), "")
	if err != nil {
		t.Fatalf("Put(%q) 失败: %v", key, err)
	}
	return m
}

func readKey(t *testing.T, p *Provider, key string) []byte {
	t.Helper()
	f, _, err := p.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open(%q) 失败: %v", key, err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("读 %q 失败: %v", key, err)
	}
	return b
}

// ── 三层穿越防御 ────────────────────────────────────────────────────────

// TestResolve_Layer1_SafeKey 第一层:字符。resolve 必须在 SafeKey 那里就把
// 穿越语法拒掉 —— 而且要在碰磁盘**之前**,否则一次恶意 key 就够在 root 里
// 留下点什么。
func TestResolve_Layer1_SafeKey(t *testing.T) {
	p := newTestProvider(t)
	bad := []string{
		"res/../../etc/passwd",
		"../outside.txt",
		"res/1/asset/../../../../tmp/x",
		"res//1/a",     // 空段:Join 会静默塌成 res/1/a
		`res\1\a.txt`,  // Windows 分隔符
		"res/1/a\x00b", // 控制字符
	}
	for _, k := range bad {
		t.Run(strconv.Quote(k), func(t *testing.T) {
			if _, err := p.resolve(k); err == nil {
				t.Fatalf("resolve(%q) 应该报错,却过了", k)
			}
		})
	}
	// 正面对照(上一版这条控制项误放进了 bad 列表里,自己把自己搞红了)
	if _, err := p.resolve("res/1/asset/2/a.pdf"); err != nil {
		t.Fatalf("正常 key 不该被拒: %v", err)
	}
	// 确认这一层**真的**拦在磁盘前面:root 底下不该多出任何东西
	assertRootFiles(t, p, nil, "第一层拦下之后 root 里不该有任何新增")
}

// assertRootFiles 断言 root 下的**全部**文件恰好等于 want(内部临时项除外)。
//
// ⚠️ 必须写成"精确相等"而不是"不该有多余的":第一版是遍历 root 找"意外文件",
// 结果 TestPut 自己刚写进去的那个正当文件被判成了意外 —— 比没断言还糟。
func assertRootFiles(t *testing.T, p *Provider, want []string, why string) {
	t.Helper()
	var got []string
	_ = filepath.WalkDir(p.Root(), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		// 内部临时项(直写中间态 / 分片会话)不算业务文件
		if strings.HasPrefix(name, putTempPrefix) || strings.HasPrefix(name, multipartTempDir) {
			return nil
		}
		rel, _ := filepath.Rel(p.Root(), path)
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(got)
	exp := append([]string(nil), want...)
	sort.Strings(exp)
	if strings.Join(got, "|") != strings.Join(exp, "|") {
		t.Fatalf("%s\n  root 里的文件 = %v\n  期望         = %v", why, got, exp)
	}
}

// TestEscapes_RejectsSiblingPrefix 盯的是第②层那条注释里写的具体坑:
// `/data/res2/x` 也 Contains "/data/res",所以 strings.Contains 做边界检查
// 是错的,必须用 filepath.Rel + ../ 前缀判定。
//
// ⚠️ 这条测的是 `escapes` 本身而不是 resolve —— 因为第①层 SafeKey 在 resolve
// 里排在前面,而 SafeKey 已经把 `..` 全拒了,第②层在 resolve 的正常路径上
// **不可达**。它是纵深防御:哪天第①层被改松(比如为了支持某些中文文件名),
// 第②层就是唯一还站着的那道。见文件末尾探针记录 S2。
func TestEscapes_RejectsSiblingPrefix(t *testing.T) {
	root := filepath.Join("/data", "res")
	sibling := filepath.Join("/data", "res2", "x")

	// 先证明 Contains 的确会误判 —— 不然下面那个断言就没有说服力
	if !strings.Contains(sibling, root) {
		t.Fatalf("前置条件失效: %q 竟然不 Contains %q,这条用例测不到想测的东西", sibling, root)
	}

	rel, err := filepath.Rel(root, sibling)
	if err != nil {
		t.Fatal(err)
	}
	if !escapes(rel) {
		t.Fatalf("sibling 路径 %q(相对 %q = %q)必须判定为逃逸", sibling, root, rel)
	}

	// 对照:真正在 root 里的路径不判逃逸
	inside := filepath.Join(root, "1", "asset", "a.pdf")
	relIn, err := filepath.Rel(root, inside)
	if err != nil {
		t.Fatal(err)
	}
	if escapes(relIn) {
		t.Fatalf("root 内的路径 %q 被误判为逃逸", inside)
	}

	// ⚠️ **这一段是为 S2 补的**:上面那个 sibling 用例打不中 `rel == ".."` 这一支
	// —— `../res2/x` 靠 HasPrefix(".."+sep) 就被判掉了,把那支删掉照样绿。
	// 只有 rel **恰好**是 ".." 时才只有精确匹配能拦住:也就是目标路径本身就是
	// root 的父目录。构造得出来:对 root 跑 Rel(root, filepath.Dir(root))。
	parentRel, err := filepath.Rel(root, filepath.Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	if !escapes(parentRel) {
		t.Fatalf("rel = %q 必须判逃逸 —— 目标就是 root 的父目录", parentRel)
	}
}

// TestResolve_Layer3_Symlink 第三层:符号链接。前两层都只看字面路径,而
// `root/data -> /etc` 的字面路径完全合法 —— 只有把目录换成真实路径才能看出来。
//
// ⚠️ Windows 上建软链要管理员或开发者模式,建不出来就 skip 并说明原因。
// 这是**条件跳过**不是假绿:探测记录里 S3 标了它,CI 上有软链权限的环境必须红。
func TestResolve_Layer3_Symlink(t *testing.T) {
	p := newTestProvider(t)

	// 在 root 外面造一个"敏感目录"
	outside := t.TempDir()
	canary := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(canary, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatalf("准备外部文件失败: %v", err)
	}

	link := filepath.Join(p.Root(), "data")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境无法创建符号链接(%v)—— Windows 未开启开发者模式时必然跳过", err)
	}

	// key 长得完全合法,但它穿过软链落到 root 外
	if _, err := p.resolve("data/secret.txt"); err == nil {
		t.Fatalf("穿过软链的 key 必须被拒")
	} else {
		var e *oss.ErrInvalidKey
		if !errors.As(err, &e) {
			t.Fatalf("错误类型 = %T,应该是 *oss.ErrInvalidKey", err)
		}
	}
	// 确认外部文件没被动过(万一实现真的写了,这里能看出来)
	if b, _ := os.ReadFile(canary); string(b) != "TOP SECRET" {
		t.Fatalf("外部文件被改写了:%q —— 第③层没拦住", b)
	}
}

// TestReadPath_RejectsFinalComponentSymlink 最终**文件**组件是软链时也要拒。
//
// 第②③层只查祖先目录,`root/a.pdf -> /etc/shadow` 的字面路径和祖先(root)
// 都完全合法 —— Stat/Get/Open 会跟随软链把 root 外的内容读出去。
func TestReadPath_RejectsFinalComponentSymlink(t *testing.T) {
	p := newTestProvider(t)

	outside := t.TempDir()
	canary := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(canary, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatalf("准备外部文件失败: %v", err)
	}
	const key = "res/1/asset/2/link.pdf"
	dir := filepath.Join(p.Root(), "res", "1", "asset", "2")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canary, filepath.Join(dir, "link.pdf")); err != nil {
		t.Skipf("当前环境无法创建符号链接(%v)—— Windows 未开启开发者模式时必然跳过", err)
	}

	ctx := context.Background()
	if _, err := p.Stat(ctx, key); err == nil {
		t.Fatal("Stat 跟随文件软链读到了 root 外 —— 必须拒绝")
	}
	if _, err := p.Get(ctx, key); err == nil {
		t.Fatal("Get 跟随文件软链读到了 root 外 —— 必须拒绝")
	}
	if _, _, err := p.Open(ctx, key); err == nil {
		t.Fatal("Open 跟随文件软链读到了 root 外 —— 必须拒绝")
	}
	// 错误要能定位:是 key 的问题,不是神秘 IO 错误
	var e *oss.ErrInvalidKey
	if _, err := p.Stat(ctx, key); !errors.As(err, &e) {
		t.Fatalf("错误类型 = %T,应该是 *oss.ErrInvalidKey", err)
	}
	// 外部文件没被动过
	if b, _ := os.ReadFile(canary); string(b) != "TOP SECRET" {
		t.Fatalf("外部文件被改写了:%q", b)
	}
}

// TestResolve_NewFolder 盯的是 ancestor-walk 那个修复。
//
// Put 到一个**还不存在**的多级目录里,是这个模块最常见的路径(用户第一次往
// 新建的文件夹上传)。第一版实现在第③层对 `filepath.Dir(目标)` 直接调
// EvalSymlinks,目录不存在 → ENOENT → 正常上传全被拒。
func TestResolve_NewFolder(t *testing.T) {
	p := newTestProvider(t)
	// 逐级建深目录
	abs, err := p.resolve("res/9/asset/deep/nested/3/a.pdf")
	if err != nil {
		t.Fatalf("多层新目录不该被拒: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Fatalf("前置条件失效:resolve 不该创建任何东西,stat err = %v", err)
	}
	putFile(t, p, "res/9/asset/deep/nested/3/a.pdf", []byte("ok"))
	if got := string(readKey(t, p, "res/9/asset/deep/nested/3/a.pdf")); got != "ok" {
		t.Fatalf("内容 = %q", got)
	}
}

// ── Put ────────────────────────────────────────────────────────────────

func TestPut(t *testing.T) {
	p := newTestProvider(t)
	const key = "res/1/asset/2/1-ab-户型图.pdf"
	content := []byte("PDF-CONTENT-中文文件名")

	m := putFile(t, p, key, content)

	if m.Key != key {
		t.Fatalf("meta.Key = %q,want %q", m.Key, key)
	}
	if m.Size != int64(len(content)) {
		t.Fatalf("meta.Size = %d,want %d", m.Size, len(content))
	}
	sum := md5.Sum(content)
	if want := hex.EncodeToString(sum[:]); m.ETag != want {
		t.Fatalf("ETag = %q,want md5 %q —— 边写边算 md5 这条失效了", m.ETag, want)
	}
	if m.LastModified.IsZero() {
		t.Fatal("LastModified 不该是零值")
	}
	if !strings.Contains(m.MimeType, "pdf") {
		t.Fatalf("MimeType = %q,应该按 .pdf 扩展名判定", m.MimeType)
	}
	if got := readKey(t, p, key); !bytes.Equal(got, content) {
		t.Fatalf("落盘内容对不上")
	}
	assertRootFiles(t, p, []string{key}, "Put 成功后只该有目标文件,不该留下临时文件")
}

// TestPut_Overwrite 覆盖写:原子 rename 之后旧内容必须**完全**消失,不能是
// "新内容 + 旧内容尾巴"(那就是没截断 / 没 rename 的典型症状)。
func TestPut_Overwrite(t *testing.T) {
	p := newTestProvider(t)
	const key = "res/1/asset/2/a.bin"
	putFile(t, p, key, mkContent(5000, 1))
	putFile(t, p, key, []byte("short"))

	got := readKey(t, p, key)
	if string(got) != "short" {
		t.Fatalf("覆盖后长度 = %d,内容 = %q —— 应恰好是 5 字节的 \"short\"", len(got), got)
	}
}

// assertNoInternalLeftovers 断言内部临时项(.put-* / .mpu)一个都没留下。
//
// ⚠️ **这条断言是 S14 的教训换来的**:TestPut_NoLeftoverOnError 原来用
// assertRootFiles 断言"没有临时文件",而 assertRootFiles **刻意过滤掉了
// .put-*** —— 于是这条断言结构上就不可能失败,探针把 defer 删掉它照样绿。
// 过滤是对的(List 不该看见内部状态),但正因为如此,"没有残留"必须用另一条
// 不做过滤的断言来盯。
func assertNoInternalLeftovers(t *testing.T, p *Provider, why string) {
	t.Helper()
	var left []string
	_ = filepath.WalkDir(p.Root(), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, putTempPrefix) || strings.HasPrefix(name, multipartTempDir) {
			rel, _ := filepath.Rel(p.Root(), path)
			left = append(left, filepath.ToSlash(rel))
		}
		return nil
	})
	if len(left) > 0 {
		t.Fatalf("%s —— 残留的内部临时项: %v", why, left)
	}
}

// TestPut_NoLeftoverOnError 写失败时临时文件必须被清掉。
// 不清的话存储目录会积一批 .put-*,而 List 过滤掉了它们 —— 它们会**静悄悄地**
// 一直占着磁盘,直到有人手动 ls -a 才看得见。
func TestPut_NoLeftoverOnError(t *testing.T) {
	p := newTestProvider(t)
	_, err := p.Put(context.Background(), "res/1/asset/2/a.bin", &failingReader{failAfter: 100}, "")
	if err == nil {
		t.Fatal("reader 报错时 Put 必须失败")
	}
	assertRootFiles(t, p, nil, "Put 失败后不该留下业务文件")
	assertNoInternalLeftovers(t, p, "Put 失败后不该留下 .put-* 临时文件")
}

// failingReader 读到 failAfter 字节之后开始报错
type failingReader struct{ failAfter int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.failAfter <= 0 {
		return 0, errors.New("磁盘满了(模拟)")
	}
	if len(p) > r.failAfter {
		p = p[:r.failAfter]
	}
	r.failAfter -= len(p)
	return len(p), nil
}

// TestCtxReader 直接盯 ctxReader 自己的 ctx 检查。
//
// ⚠️ **这条是补 S15 的假绿补出来的**。原来只有 TestPut_CtxCancelled,而那条
// 在**删掉 ctxReader 的 ctx 检查之后照样绿** —— 因为 Put 还把 ctx 传给了
// renameWithRetry → retryx.Do,ctx 一取消 rename 那步就先失败了。等于端到端
// 断言碰巧成立,却根本没验到 ctxReader。
func TestCtxReader(t *testing.T) {
	t.Run("已取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := &ctxReader{ctx: ctx, r: strings.NewReader("hello")}
		n, err := r.Read(make([]byte, 5))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v,want context.Canceled(读了 %d 字节)", err, n)
		}
	})
	t.Run("正常透传", func(t *testing.T) {
		r := &ctxReader{ctx: context.Background(), r: strings.NewReader("hello")}
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "hello" {
			t.Fatalf("读到 %q", b)
		}
	})
}

// TestPut_CtxCancelled 端到端:ctx 取消时 Put 必须失败。
//
// ⚠️ 这条**不是**在验 ctxReader(见上面那条的警告)。它连同 renameWithRetry
// 的 ctx 感知一起覆盖"取消后不会留下半个正式文件"。
func TestPut_CtxCancelled(t *testing.T) {
	p := newTestProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Put(ctx, "res/1/asset/2/a.bin", bytes.NewReader(mkContent(1000, 1)), ""); err == nil {
		t.Fatal("ctx 已取消时 Put 必须失败")
	}
	assertRootFiles(t, p, nil, "ctx 取消后不该留下正式文件")
	assertNoInternalLeftovers(t, p, "ctx 取消后不该留下 .put-* 临时文件")
}

func TestPut_InvalidKey(t *testing.T) {
	p := newTestProvider(t)
	_, err := p.Put(context.Background(), "../escape.txt", bytes.NewReader([]byte("x")), "")
	var e *oss.ErrInvalidKey
	if !errors.As(err, &e) {
		t.Fatalf("错误类型 = %T,应该是 *oss.ErrInvalidKey", err)
	}
}

// ── Stat / Open ────────────────────────────────────────────────────────

func TestStat(t *testing.T) {
	p := newTestProvider(t)
	const key = "res/1/asset/2/a.pdf"
	content := mkContent(1234, 7)
	putFile(t, p, key, content)

	m, err := p.Stat(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if m.Size != int64(len(content)) {
		t.Fatalf("Size = %d,want %d", m.Size, len(content))
	}
}

func TestStat_NotFound(t *testing.T) {
	p := newTestProvider(t)

	// 不存在的文件
	if _, err := p.Stat(context.Background(), "res/1/asset/2/nope.pdf"); !oss.IsNotFound(err) {
		t.Fatalf("不存在文件的错误 = %v,应该 IsNotFound", err)
	}
	// **目录**也算 not found —— Stat 是查文件的
	if err := os.MkdirAll(filepath.Join(p.Root(), "res", "1", "asset", "2"), 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := p.Stat(context.Background(), "res/1/asset/2")
	if !oss.IsNotFound(err) {
		t.Fatalf("目录的 Stat 错误 = %v,应该 IsNotFound(而不是别的错误)", err)
	}
	// 错误里要带得上 key,排查时要靠它
	var nf *oss.ErrNotFound
	if _, err := p.Stat(context.Background(), "res/1/asset/2/nope.pdf"); !errors.As(err, &nf) {
		t.Fatal("应该能 errors.As 取出 *ErrNotFound")
	} else if nf.Key != "res/1/asset/2/nope.pdf" {
		t.Fatalf("ErrNotFound.Key = %q,丢了 key 就没法定位", nf.Key)
	}
}

func TestOpen(t *testing.T) {
	p := newTestProvider(t)
	const key = "res/1/asset/2/a.txt"
	content := []byte("hello")
	putFile(t, p, key, content)

	f, m, err := p.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if m.Size != int64(len(content)) {
		t.Fatalf("Size = %d,want %d", m.Size, len(content))
	}
	b, _ := io.ReadAll(f)
	if !bytes.Equal(b, content) {
		t.Fatalf("读到 %q", b)
	}
}

// ── SignDownloadURL ────────────────────────────────────────────────────

// TestSignDownloadURL_AllFilesHaveURL **本地磁盘上每个文件都有直链**。
//
// ⚠️ 这条用例盯的是模型本身:以前"私有文件返 ErrDirectURLUnsupported",
// 是因为静态路由只挂了 public 子树。现在整棵树都挂出去了,再有一条
// "私有不给 URL"的分支,文件就会既没有直链、也没被静态路由覆盖,
// 变成谁也取不到的死文件。
func TestSignDownloadURL_AllFilesHaveURL(t *testing.T) {
	p := newTestProvider(t)
	const key = "res/1/asset/2/1-ab-户型 图.pdf"
	putFile(t, p, key, []byte("x"))

	u, err := p.SignDownloadURL(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// 逐段编码:斜杠必须保留(整条编码会把 / 变 %2F,静态路由就匹配不上了)
	if !strings.HasPrefix(u, "/res/1/asset/2/") {
		t.Fatalf("URL = %q,路径分隔符被吃掉了", u)
	}
	// 空格和中文必须编码
	if strings.ContainsAny(u[strings.Index(u, "2/")+2:], " ") {
		t.Fatalf("URL = %q,空格没编码", u)
	}
	if !strings.Contains(u, "%E6") {
		t.Fatalf("URL = %q,中文没编码", u)
	}
}

// TestSignDownloadURL_StripsRootSegment 挂载点已经代表 key 的第一段(path_root)。
//
// 不剥的话 URL 变成 /res/res/1/...,静态路由拼回去指向 <root>/res/res/1/...
// —— 那个路径不存在,而且**不报错**(404 而非 500),最难查的一种坏。
func TestSignDownloadURL_StripsRootSegment(t *testing.T) {
	p := newTestProvider(t)
	putFile(t, p, "res/1/asset/2/a.pdf", []byte("x"))
	u, err := p.SignDownloadURL(context.Background(), "res/1/asset/2/a.pdf", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/res/1/asset/2/a.pdf"; u != want {
		t.Fatalf("got %q,want %q", u, want)
	}
	if strings.Contains(u, "/res/res/") {
		t.Fatalf("root 段被剥了两次: %q", u)
	}
}

// TestSignDownloadURL_MissingFile 不存在的文件必须报错,不能给一个死链。
func TestSignDownloadURL_MissingFile(t *testing.T) {
	p := newTestProvider(t)
	_, err := p.SignDownloadURL(context.Background(), "res/1/asset/2/nope.pdf", time.Minute)
	if err == nil {
		t.Fatal("不存在的文件不该给出 URL")
	}
}

// TestSignDownloadURL_NoURLBase 没配静态挂载点时必须报错,不能返一个看着能用的废 URL。
func TestSignDownloadURL_NoURLBase(t *testing.T) {
	p, err := New(Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, p, "res/1/asset/2/a.pdf", []byte("x"))
	if _, err := p.SignDownloadURL(context.Background(), "res/1/asset/2/a.pdf", time.Minute); err == nil {
		t.Fatal("URLBase 为空时不该给出 URL")
	}
}

// ── List ───────────────────────────────────────────────────────────────

func TestList(t *testing.T) {
	p := newTestProvider(t)
	const prefix = "res/1/asset/2/"
	putFile(t, p, prefix+"b.pdf", []byte("bb"))
	putFile(t, p, prefix+"a.pdf", []byte("aaa"))
	if err := os.MkdirAll(filepath.Join(p.Root(), "res", "1", "asset", "2", "sub"), 0o750); err != nil {
		t.Fatal(err)
	}

	res, err := p.List(context.Background(), oss.ListOption{Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 2 {
		t.Fatalf("Objects = %d 个(%v),want 2", len(res.Objects), res.Objects)
	}
	// 必须按 key 排序 —— 分页靠二分查找,不排序整条 paginate 就错了
	if res.Objects[0].Key != prefix+"a.pdf" || res.Objects[1].Key != prefix+"b.pdf" {
		t.Fatalf("Objects 没排序: %q %q", res.Objects[0].Key, res.Objects[1].Key)
	}
	if res.Objects[0].Size != 3 {
		t.Fatalf("a.pdf Size = %d,want 3", res.Objects[0].Size)
	}
	if len(res.CommonPrefixes) != 1 || res.CommonPrefixes[0] != "sub/" {
		t.Fatalf("CommonPrefixes = %v,want [sub/]", res.CommonPrefixes)
	}
}

// TestList_TrailingSlashPrefix 盯的是 resolvePrefix 那个修复。
//
// `oss.FolderPrefix` 返回的前缀**一定带尾斜杠**,而 SafeKey 拒尾斜杠 ——
// 不剪掉的话列表接口在真实调用路径上整个哑火。
func TestList_TrailingSlashPrefix(t *testing.T) {
	p := newTestProvider(t)
	prefix, err := oss.FolderPrefix("", 1, "asset", 2)
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, p, prefix+"a.pdf", []byte("a"))

	res, err := p.List(context.Background(), oss.ListOption{Prefix: prefix})
	if err != nil {
		t.Fatalf("带尾斜杠的前缀必须能 List: %v", err)
	}
	if len(res.Objects) != 1 {
		t.Fatalf("Objects = %d,want 1", len(res.Objects))
	}
}

// TestList_HidesInternalState .mpu / .put-* 是内部状态,不能出现在用户的
// 文件夹列表里 —— 露出去等于告诉客户端"你可以来删这个"。
func TestList_HidesInternalState(t *testing.T) {
	p := newTestProvider(t)
	const prefix = "res/1/asset/2/"
	putFile(t, p, prefix+"real.pdf", []byte("a"))

	// 造一个未完成的分片会话 + 一个孤儿临时文件
	if _, err := p.InitMultipartUpload(context.Background(), prefix+"big.mp4",
		oss.WithPartSize(oss.MinPartSize), oss.WithTotalSize(int64(oss.MinPartSize))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Root(), prefix, putTempPrefix+"abc"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := p.List(context.Background(), oss.ListOption{Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, o := range res.Objects {
		keys = append(keys, o.Key)
	}
	if len(keys) != 1 || keys[0] != prefix+"real.pdf" {
		t.Fatalf("内部状态漏出来了: %v", keys)
	}
}

func TestList_NonexistentPrefix(t *testing.T) {
	p := newTestProvider(t)
	// 不存在 ≠ 报错。OSS 上 List 不存在的 prefix 也返空,两边必须一致,
	// 否则换存储后端时前端会突然开始报错。
	res, err := p.List(context.Background(), oss.ListOption{Prefix: "res/1/asset/999/"})
	if err != nil {
		t.Fatalf("不存在的目录不该报错: %v", err)
	}
	if len(res.Objects) != 0 || len(res.CommonPrefixes) != 0 {
		t.Fatalf("应该返空: %+v", res)
	}
}

func TestList_Pagination(t *testing.T) {
	p := newTestProvider(t)
	const prefix = "res/1/asset/2/"
	names := []string{"a.pdf", "b.pdf", "c.pdf", "d.pdf", "e.pdf"}
	for _, n := range names {
		putFile(t, p, prefix+n, []byte(n))
	}

	var got []string
	marker := ""
	for page := 0; page < 5; page++ {
		res, err := p.List(context.Background(), oss.ListOption{Prefix: prefix, MaxKeys: 2, Marker: marker})
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range res.Objects {
			got = append(got, strings.TrimPrefix(o.Key, prefix))
		}
		if !res.IsTruncated {
			break
		}
		if res.NextMarker == "" {
			t.Fatal("IsTruncated 为 true 时 NextMarker 不能为空")
		}
		marker = res.NextMarker
	}
	// 必须**恰好**一遍。少一条 = 分页漏了,多一条 = marker 不排他
	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Fatalf("分页结果 = %v,want %v(少一条=漏了,多一条=marker 不排他)", got, names)
	}
}

func TestList_PaginationExhausted(t *testing.T) {
	p := newTestProvider(t)
	const prefix = "res/1/asset/2/"
	putFile(t, p, prefix+"a.pdf", []byte("a"))
	// marker 指向最后一条之后 → 空页且 IsTruncated=false,不是死循环
	res, err := p.List(context.Background(), oss.ListOption{Prefix: prefix, MaxKeys: 10, Marker: prefix + "zzz"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 0 || res.IsTruncated {
		t.Fatalf("越界的 marker 应返空且不截断: %+v", res)
	}
}

// ── 删除 ───────────────────────────────────────────────────────────────

func TestDelete(t *testing.T) {
	p := newTestProvider(t)
	const key = "res/1/asset/2/a.pdf"
	putFile(t, p, key, []byte("x"))

	if err := p.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Stat(context.Background(), key); !oss.IsNotFound(err) {
		t.Fatalf("删掉之后 Stat 应该 NotFound,got %v", err)
	}
}

// TestDelete_Idempotent 删不存在的算成功。清理任务会重跑,第二遍必须还是绿 ——
// 否则清理会卡在同一条上永远跑不完。
func TestDelete_Idempotent(t *testing.T) {
	p := newTestProvider(t)
	if err := p.Delete(context.Background(), "res/1/asset/2/never-existed.pdf"); err != nil {
		t.Fatalf("删不存在的文件必须成功: %v", err)
	}
}

func TestDelete_Multiple(t *testing.T) {
	p := newTestProvider(t)
	keys := []string{"res/1/asset/2/a.pdf", "res/1/asset/2/b.pdf", "res/1/asset/2/c.pdf"}
	for _, k := range keys {
		putFile(t, p, k, []byte("x"))
	}
	if err := p.Delete(context.Background(), keys...); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if _, err := p.Stat(context.Background(), k); !oss.IsNotFound(err) {
			t.Fatalf("%q 还在", k)
		}
	}
}

func TestDeletePrefix(t *testing.T) {
	p := newTestProvider(t)
	const prefix = "res/1/asset/2/"
	putFile(t, p, prefix+"a.pdf", mkContent(100, 1))
	putFile(t, p, prefix+"b.pdf", mkContent(250, 2))
	if err := os.MkdirAll(filepath.Join(p.Root(), "res", "1", "asset", "2", "sub", "deep"), 0o750); err != nil {
		t.Fatal(err)
	}
	putFile(t, p, prefix+"sub/deep/c.pdf", mkContent(50, 3))
	// 同级目录**不能**被波及
	putFile(t, p, "res/1/asset/3/keep.pdf", []byte("keep"))

	freed, err := p.DeletePrefix(context.Background(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	// 返回的字节数是配额对账要的数 —— 少算会让配额越用越松
	if want := int64(100 + 250 + 50); freed != want {
		t.Fatalf("freed = %d,want %d(配额对账靠这个数)", freed, want)
	}
	res, _ := p.List(context.Background(), oss.ListOption{Prefix: prefix})
	if len(res.Objects) != 0 || len(res.CommonPrefixes) != 0 {
		t.Fatalf("前缀下还有残留: %+v", res)
	}
	if _, err := p.Stat(context.Background(), "res/1/asset/3/keep.pdf"); err != nil {
		t.Fatalf("同级目录被误删了: %v", err)
	}
}

func TestDeletePrefix_Nonexistent(t *testing.T) {
	p := newTestProvider(t)
	freed, err := p.DeletePrefix(context.Background(), "res/1/asset/999/")
	if err != nil {
		t.Fatalf("删不存在的目录不该报错: %v", err)
	}
	if freed != 0 {
		t.Fatalf("freed = %d,want 0", freed)
	}
}

// TestDeletePrefix_NoFollowSymlink 用 os.RemoveAll 的话,软链会被跟着走进去
// 把**外面**的东西删掉。这是我们不用 RemoveAll 的头号理由。
func TestDeletePrefix_NoFollowSymlink(t *testing.T) {
	p := newTestProvider(t)

	outside := t.TempDir()
	canary := filepath.Join(outside, "must-survive.txt")
	if err := os.WriteFile(canary, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(p.Root(), "res", "1", "asset", "2", "escape")
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境无法创建符号链接(%v)—— 见文件末尾 S4", err)
	}

	if _, err := p.DeletePrefix(context.Background(), "res/1/asset/2/"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Fatalf("软链目标里的文件被删了: %v —— DeletePrefix 跟着软链走进去了", err)
	}
}

// ── 分片 ───────────────────────────────────────────────────────────────

// mpFixture 一个已初始化的分片会话
type mpFixture struct {
	p       *Provider
	key     string
	upID    string
	partSze int64
	total   int64
}

func newMP(t *testing.T, total, partSize int64) mpFixture {
	t.Helper()
	p := newTestProvider(t)
	key := "res/1/asset/2/big.bin"
	init, err := p.InitMultipartUpload(context.Background(), key,
		oss.WithPartSize(partSize), oss.WithTotalSize(total))
	if err != nil {
		t.Fatalf("InitMultipartUpload 失败: %v", err)
	}
	if init.PartSize != partSize {
		t.Fatalf("PartSize = %d,want %d", init.PartSize, partSize)
	}
	wantNum := int((total + partSize - 1) / partSize)
	if init.PartNum != wantNum {
		t.Fatalf("PartNum = %d,want %d(向上取整)", init.PartNum, wantNum)
	}
	return mpFixture{p: p, key: key, upID: init.UploadID, partSze: partSize, total: total}
}

func (f mpFixture) part(t *testing.T, n int, seed byte) string {
	t.Helper()
	body := mkContent(int(f.partSze), seed)
	// 最后一片按余数裁
	off := int64(n-1) * f.partSze
	if rest := f.total - off; rest < f.partSze {
		body = body[:rest]
	}
	etag, err := f.p.UploadPart(context.Background(), f.key, f.upID, n, body)
	if err != nil {
		t.Fatalf("UploadPart(%d) 失败: %v", n, err)
	}
	return etag
}

func TestMultipart_HappyPath(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*3, ps)

	// 总大小 3 片整,每片都是全尺寸
	var etags []string
	for n := 1; n <= 3; n++ {
		etags = append(etags, f.part(t, n, byte(n)))
	}

	parts, err := f.p.ListParts(context.Background(), f.key, f.upID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("ListParts = %d 片,want 3", len(parts))
	}
	for i, pt := range parts {
		if pt.PartNumber != i+1 {
			t.Fatalf("第 %d 项 PartNumber = %d", i, pt.PartNumber)
		}
		if pt.ETag != etags[i] {
			t.Fatalf("第 %d 片 ETag = %q,want %q", i+1, pt.ETag, etags[i])
		}
		if pt.Size != ps {
			t.Fatalf("第 %d 片 Size = %d,want %d", i+1, pt.Size, ps)
		}
	}

	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, parts); err != nil {
		t.Fatal(err)
	}
	got := readKey(t, f.p, f.key)
	if int64(len(got)) != f.total {
		t.Fatalf("合并后 %d 字节,want %d", len(got), f.total)
	}
	// 内容必须逐片对得上 —— 大小对但内容错是定长偏移实现最容易出的 bug
	for n := 1; n <= 3; n++ {
		want := mkContent(int(ps), byte(n))
		if !bytes.Equal(got[int64(n-1)*ps:int64(n)*ps], want) {
			t.Fatalf("第 %d 片内容对不上", n)
		}
	}
}

// TestMultipart_OutOfOrder 乱序上传。这条盯的是 **WriteAt 定长偏移**而不是
// O_APPEND —— 追加的实现在这里会"成功",但合并出来的是 3+1+2 拼一起的东西。
func TestMultipart_OutOfOrder(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*3, ps)

	for _, n := range []int{3, 1, 2} { // 故意乱序
		f.part(t, n, byte(n))
	}
	parts, err := f.p.ListParts(context.Background(), f.key, f.upID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, parts); err != nil {
		t.Fatal(err)
	}
	got := readKey(t, f.p, f.key)
	for n := 1; n <= 3; n++ {
		seg := got[int64(n-1)*ps : int64(n)*ps]
		if !bytes.Equal(seg, mkContent(int(ps), byte(n))) {
			t.Fatalf("第 %d 片错位 —— 偏移算错了", n)
		}
	}
}

// TestMultipart_Retransmit 续传重传同一片必须幂等。
//
// 这是定长偏移的**全部意义**:客户端在续传时会重传它认为失败的那片。
// 用 O_APPEND 的实现这里会得到双倍大小的文件,而 Complete 的尺寸校验
// 恰好会拦住 —— 所以这条同时盯住了"幂等"和"校验"两处。
func TestMultipart_Retransmit(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*3, ps)

	e1 := f.part(t, 1, 1)
	f.part(t, 2, 2)
	e1b := f.part(t, 1, 1) // 完全相同的重传
	if e1 != e1b {
		t.Fatalf("同一份内容的 ETag 应该相同(这样客户端才能校验重传): %q vs %q", e1, e1b)
	}
	f.part(t, 3, 3)

	parts, err := f.p.ListParts(context.Background(), f.key, f.upID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("重传之后 ListParts = %d 片,want 3(重传不该新增条目)", len(parts))
	}
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, parts); err != nil {
		t.Fatal(err)
	}
	if got := readKey(t, f.p, f.key); int64(len(got)) != f.total {
		t.Fatalf("重传之后大小 = %d,want %d —— 重传是幂等的,不该把文件撑大", len(got), f.total)
	}
}

// TestMultipart_LastPartShort 最后一片通常不满。这是 partSizeOf 那段逻辑在守的。
func TestMultipart_LastPartShort(t *testing.T) {
	const ps = oss.MinPartSize
	const total = ps*2 + 12345 // 第三片是余数
	f := newMP(t, total, ps)

	for n := 1; n <= 3; n++ {
		f.part(t, n, byte(n))
	}
	parts, err := f.p.ListParts(context.Background(), f.key, f.upID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("ListParts = %d,want 3", len(parts))
	}
	if parts[2].Size != 12345 {
		t.Fatalf("末片 Size = %d,want 12345(余数没算对)", parts[2].Size)
	}
	if parts[0].Size != ps || parts[1].Size != ps {
		t.Fatalf("前两片应各 %d,got %d/%d", ps, parts[0].Size, parts[1].Size)
	}
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, parts); err != nil {
		t.Fatal(err)
	}
	if got := readKey(t, f.p, f.key); int64(len(got)) != total {
		t.Fatalf("合并后 %d 字节,want %d", len(got), total)
	}
}

// TestComplete_RejectsSizeMismatch 漏传一片必须被拒,并且**清掉临时文件**。
// 不清的话这份半成品会一直躺在 .mpu 里,直到磁盘满。
func TestComplete_RejectsSizeMismatch(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*3, ps)
	f.part(t, 1, 1)
	f.part(t, 2, 2)
	// 故意不传第 3 片

	parts, _ := f.p.ListParts(context.Background(), f.key, f.upID)
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, parts); err == nil {
		t.Fatal("漏传一片必须拒绝合并")
	}
	if _, err := f.p.Stat(context.Background(), f.key); !oss.IsNotFound(err) {
		t.Fatal("失败的合并不该留下正式文件")
	}
	assertMpuEmpty(t, f.p)
}

func TestComplete_RejectsGap(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*3, ps)
	f.part(t, 1, 1)
	f.part(t, 3, 3)

	// ListParts 只报到断点为止 —— 客户端据此知道该补第 2 片
	parts, _ := f.p.ListParts(context.Background(), f.key, f.upID)
	if len(parts) != 1 {
		t.Fatalf("ListParts = %d 片,want 1(第 2 片缺失就该断在这里)", len(parts))
	}
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, parts); err == nil {
		t.Fatal("缺中间一片必须拒绝合并")
	}
}

// TestComplete_RejectsGapFullReport 客户端**绕过 ListParts**、自己报一份
// {1,3} 上来 —— 分片数和服务端记账一样多,前面那道数量校验拦不住,
// 剩下的就只有连续性校验。
//
// ⚠️ 这条是为 S8 补的。TestComplete_RejectsGap 走的是 ListParts 的返回值,
// 而 ListParts 遇到空洞就 break 了,客户端拿到的自然少于服务端记账 ——
// 于是数量校验先炸,连续性校验那一支从来没被走到过。
func TestComplete_RejectsGapFullReport(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*3, ps)
	e1 := f.part(t, 1, 1)
	e3 := f.part(t, 3, 3)

	// 伪造一份"完整"的分片列表:数量对得上,编号有洞
	forged := []oss.Part{{PartNumber: 1, ETag: e1}, {PartNumber: 3, ETag: e3}}
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, forged); err == nil {
		t.Fatal("第 2 片缺失必须拒绝合并 —— 合并出来中间是一个洞")
	}
	assertMpuEmpty(t, f.p)
}

func TestComplete_RejectsETagMismatch(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*2, ps)
	f.part(t, 1, 1)
	f.part(t, 2, 2)

	bad := []oss.Part{{PartNumber: 1, ETag: "deadbeef"}, {PartNumber: 2, ETag: ""}}
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, bad); err == nil {
		t.Fatal("ETag 对不上必须拒绝")
	}
	assertMpuEmpty(t, f.p)
}

func TestComplete_RejectsPartCountMismatch(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*2, ps)
	f.part(t, 1, 1)
	f.part(t, 2, 2)

	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, []oss.Part{{PartNumber: 1}}); err == nil {
		t.Fatal("分片数不符必须拒绝")
	}
}

func TestUploadPart_KeyMismatch(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*2, ps)
	if _, err := f.p.UploadPart(context.Background(), "res/9/asset/9/other.bin", f.upID, 1, mkContent(int(ps), 1)); err == nil {
		t.Fatal("uploadID 和 key 对不上必须拒绝 —— 否则换个 key 就能往这个会话里写")
	}
}

func TestUploadPart_Rejects(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*2, ps)

	cases := []struct {
		name string
		n    int
		body []byte
	}{
		{"片号 0", 0, mkContent(100, 1)},
		{"片号负", -1, mkContent(100, 1)},
		{"片号超过 OSS 上限", oss.MaxPartNum + 1, mkContent(100, 1)},
		{"超过本地上限", 1, mkContent(int(MaxPartSize)+1, 1)},
		{"超出声明的总大小", 3, mkContent(100, 1)}, // 总大小只有 2 片
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := f.p.UploadPart(context.Background(), f.key, f.upID, c.n, c.body); err == nil {
				t.Fatalf("应该被拒(%s)", c.name)
			}
		})
	}
}

// 没有它,并发的 WriteAt 会互相踩(尤其两个请求写同一个 part 时)。
func TestMultipart_ConcurrentParts(t *testing.T) {
	const ps = oss.MinPartSize
	const n = 8
	f := newMP(t, ps*n, ps)

	errs := make(chan error, n)
	for i := 1; i <= n; i++ {
		go func(num int) {
			off := int64(num-1) * ps
			rest := f.total - off
			body := mkContent(int(ps), byte(num))
			if rest < ps {
				body = body[:rest]
			}
			_, err := f.p.UploadPart(context.Background(), f.key, f.upID, num, body)
			errs <- err
		}(i)
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("并发分片上传失败: %v", err)
		}
	}
	parts, err := f.p.ListParts(context.Background(), f.key, f.upID)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != n {
		t.Fatalf("ListParts = %d,want %d", len(parts), n)
	}
	// 不排序,直接验 ListParts 返回的顺序 —— 排序了就等于把这个断言测没了
	keys := make([]int, 0, n)
	for _, pt := range parts {
		keys = append(keys, pt.PartNumber)
	}
	if !sort.IntsAreSorted(keys) {
		t.Fatalf("ListParts 必须按片号升序: %v", keys)
	}
	if err := f.p.CompleteMultipartUpload(context.Background(), f.key, f.upID, parts); err != nil {
		t.Fatal(err)
	}
	if got := readKey(t, f.p, f.key); int64(len(got)) != f.total {
		t.Fatalf("并发合并后 %d 字节,want %d", len(got), f.total)
	}
}

func TestAbort(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*2, ps)
	f.part(t, 1, 1)

	if err := f.p.AbortMultipartUpload(context.Background(), f.key, f.upID); err != nil {
		t.Fatal(err)
	}
	assertMpuEmpty(t, f.p)
	// 幂等
	if err := f.p.AbortMultipartUpload(context.Background(), f.key, f.upID); err != nil {
		t.Fatalf("重复 Abort 必须成功: %v", err)
	}
}

// TestAbort_ConcurrentWithUpload_NoGhostSession abort 与 upload 并发交错后
// 不许出现「sidecar 有账、.part 已删」的僵尸会话。
//
// 无锁版 bug:Abort(不持锁)在「UploadPart(持锁)写完 .part、还没把 sidecar
// 记账写回」的间隙里删掉 .part,等 UploadPart 把 sidecar 写回来 —— 账上有、
// 盘上没有,下一次 Complete 会拿一个空洞去 rename 成正式文件。
// Abort 持同一把 partLock 之后,这条交错在结构上不可能发生:
// Abort 返回且上传协程退出后,.mpu 里必须一个字节都不剩。
func TestAbort_ConcurrentWithUpload_NoGhostSession(t *testing.T) {
	const ps = oss.MinPartSize
	p := newTestProvider(t)
	key := "res/1/asset/2/big.bin"
	ctx := context.Background()

	const rounds = 20
	for i := 0; i < rounds; i++ {
		init, err := p.InitMultipartUpload(ctx, key, oss.WithPartSize(ps), oss.WithTotalSize(ps*2))
		if err != nil {
			t.Fatal(err)
		}
		body := mkContent(int(ps), 1)
		stop := make(chan struct{})
		exited := make(chan struct{})
		go func(id string) {
			defer close(exited)
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = p.UploadPart(ctx, key, id, 1, body) // 会话没了就报错,随它去
			}
		}(init.UploadID)

		if err := p.AbortMultipartUpload(ctx, key, init.UploadID); err != nil {
			t.Fatalf("第 %d 轮 Abort 失败: %v", i, err)
		}
		// 主协程负责关 stop;exited 等上传协程真正退出,别在它还在写时断言
		close(stop)
		<-exited
		// Abort 已经成功返回、上传协程也不再写:不允许残留任何会话状态。
		// (有账无盘的僵尸恰好卡在这两种状态的缝隙里。)
		if _, err := os.Stat(p.sidecarPath(init.UploadID)); !os.IsNotExist(err) {
			t.Fatalf("第 %d 轮 sidecar 残留(僵尸会话): %v", i, err)
		}
		if _, err := os.Stat(p.partPath(init.UploadID)); !os.IsNotExist(err) {
			t.Fatalf("第 %d 轮 .part 残留: %v", i, err)
		}
	}
}

func TestPurgeExpired(t *testing.T) {
	const ps = oss.MinPartSize
	f := newMP(t, ps*2, ps)
	f.part(t, 1, 1)

	// TTL 很长 → 不该清
	if n, err := f.p.PurgeExpired(context.Background(), time.Hour, time.Now()); err != nil || n != 0 {
		t.Fatalf("未过期的会话不该被清:n=%d err=%v", n, err)
	}
	// TTL 为 0 + 把 now 推到未来 → 全清
	n, err := f.p.PurgeExpired(context.Background(), 0, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("清掉 %d 个,want 1", n)
	}
	assertMpuEmpty(t, f.p)
}

func TestPurgeExpired_OrphanPart(t *testing.T) {
	p := newTestProvider(t)
	dir := filepath.Join(p.Root(), multipartTempDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(dir, "deadbeef.part")
	if err := os.WriteFile(orphan, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 改到很久以前,确保被 cutoff 覆盖
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}
	n, err := p.PurgeExpired(context.Background(), time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("清掉 %d 个,want 1", n)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("孤儿 .part 还在 —— 没有 sidecar 的会话永远等不到 Abort,只能靠这里扫")
	}
}

func TestPurgeExpired_NoDir(t *testing.T) {
	p := newTestProvider(t)
	n, err := p.PurgeExpired(context.Background(), time.Hour, time.Now())
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func assertMpuEmpty(t *testing.T, p *Provider) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(p.Root(), multipartTempDir))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) > 0 {
		t.Fatalf(".mpu 里还有残留: %v", left)
	}
}

// ── 凭证 ───────────────────────────────────────────────────────────────

func TestSignUploadToken_Proxy(t *testing.T) {
	p := newTestProvider(t)
	tok, err := p.SignUploadToken(context.Background(), "res/1/asset/2/a.pdf", time.Minute, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Mode != oss.UploadModeProxy {
		t.Fatalf("Mode = %q,want proxy —— 本地磁盘没有直传", tok.Mode)
	}
	if tok.Key != "res/1/asset/2/a.pdf" {
		t.Fatalf("Key = %q", tok.Key)
	}
	if tok.MaxSize <= 0 {
		t.Fatal("maxSize 为 0 时必须有默认值 —— 服务端靠它限流")
	}
	if tok.Provider != "local" {
		t.Fatalf("Provider = %q", tok.Provider)
	}
	if err := tok.Check(); err != nil {
		t.Fatalf("凭证自检没过: %v", err)
	}
	if tok.Expired() {
		t.Fatal("刚签的凭证不该是过期的")
	}
}

// 不该发出去让前端去撞墙。
func TestSignUploadToken_RejectsBadKey(t *testing.T) {
	p := newTestProvider(t)
	if _, err := p.SignUploadToken(context.Background(), "res/../x", time.Minute, 0); err == nil {
		t.Fatal("穿越 key 必须被拒")
	}
}

// ── 可逆探针记录(2026-10-22)────────────────────────────────────────────
//
// 方法:把实现里一处保护拆掉,看用例会不会红。红 = 用例真的盯住了这一行。
// 探针脚本 `scripts/probe_oss_local.py`,每个探针打完自动还原。
//
// ⚠️ 纪律(SKIP 最危险,详见文件末尾"探针本身差点骗人"):
//   · RED  必须看到输出里**字面**的 `--- FAIL:`
//   · BROKE(编译/装载失败) 不算 RED,是探针写坏了
//   · GREEN(没红) 必须解释,不许默默跳过
//   · 整轮开跑前基线必须绿,否则"全红"和"根本没跑起来"长得一模一样
//
// 结果:15 红 / 2 绿(均已解释)/ 1 条 SKIP(本机无软链权限)
//
// ┌────┬──────────────────────────────────────────────┬─────┬────────────────┐
// │ 编号│ 拆掉什么                                     │ 结果│ 盯住它的用例        │
// ├────┼──────────────────────────────────────────────┼─────┼────────────────┤
// │ S1 │ resolve 的 SafeKey 前置检查                     │ RED │ Layer1_SafeKey  │
// │ S2 │ escapes() 的 `rel == ".."` 精确判定            │ RED │ Escapes_…Sibling│
// │ S3 │ resolve 的第③层符号链接检查                     │ SKIP│ Layer3_Symlink  │
// │ S4 │ DeletePrefix 不用逐文件删,换回 os.RemoveAll    │ RED │ NoFollowSymlink │
// │ S5 │ UploadPart 的 WriteAt 定偏移换追加写             │ RED │ …OutOfOrder    │
// │ S6 │ Complete 的合并尺寸校验                         │ RED │ RejectsSizeMism.│
// │ S7 │ Complete 的分片 ETag 校验                       │ RED │ RejectsETagMism.│
// │ S8 │ Complete 的分片连续性校验(缺片当没事)           │ RED │ RejectsGap      │
// │ S9 │ List 不过滤 .mpu / .put-* 内部项                │ RED │ HidesInternalSt.│
// │ S10│ List 不再按 key 排序                            │ 绿  │ (见下)         │
// │ S11│ paginate 的 sort.Search 换线性扫                │ 绿  │ (见下)         │
// │ S12│ resolvePrefix 不剪尾斜杠                         │ RED │ TrailingSlash   │
// │ S13│ 第③层不走 ancestor-walk,直接 EvalSymlinks       │ RED │ Resolve_NewDir │
// │ S14│ Put 失败时不兜底删临时文件                      │ RED │ NoLeftoverOnEr.│
// │ S15│ ctxReader 不检查 ctx                            │ RED │ TestCtxReader  │
// │ S16│ UploadPart 不检查 offset+len 越界               │ RED │ UploadPart_Rej.│
// │ S17│ SignDownloadURL 不剥 publicPrefix               │ RED │ SignDownloadURL│
// │ S18│ paginate 在对象数没超 MaxKeys 时跳过 marker      │ RED │ PaginationExh.  │
// └────┴──────────────────────────────────────────────┴─────┴────────────────┘
//
// ── 两条 GREEN,为什么留着没删探针也没补断言 ──────────────────────────
//
// **S10(List 的 sort.Slice)**:Go 的 `os.ReadDir` 返回值本身就是按文件名字典序
// 排好的,而 List 的 key 一律由同一个前缀 + 相对路径拼成,所以磁盘顺序就是 key
// 顺序 —— 排序那行在当前调用形状下**行为上不可观测**。它留着是"万一将来有人
// 改掉 key 构造形状"的保险,不是当前逻辑的必需部分。
// ⚠️ 结论是"这一行现在无人盯",不是"它有用所以绿了"。要盯住它得先造出一个
// key 顺序 ≠ 目录名的场景(比如自定义 key 而非按目录 walk),那是接口形状变了才
// 会出现的情况,现在硬造一条断言等于把实现细节钉死。
//
// **S11(paginate 的 sort.Search)**:探针换成的是**语义完全等价**的线性扫,不是
// 错误实现 —— 两者输出必然一致,所以不红是应该的。这里测的是"有没有把二分写
// 错",而二分写错**不需要**时间断言也能被发现:`PaginationExhausted`(S18)和
// `Pagination` 的游标用例已经覆盖了首/中/尾/空四种切分,二分只要在任何一个切分
// 上偏一个位置,那些用例就会红。
// ⚠️ 特意**没有**补一条耗时断言:CI 机器负载波动会把计时断言变成随机红,
// 那比不测更糟 —— 它会让人习惯性地忽略这个测试的失败。
//
// ── S3 SKIP:这两道防御在本机没有验证过 ────────────────────────────────
//
// `Layer3_Symlink` 和 `NoFollowSymlink` 在本机 **SKIP**,原因:
//   symlink ... A required privilege is not held by the client
// (Windows 未开开发者模式,非管理员建不出软链)
//
// 这是**条件跳过**不是假绿:代码路径在,断言在,只是这台机器造不出攻击前提。
// ⚠️ 但要如实说:第③层符号链接检查和 DeletePrefix 的不跟随软链,在**本地**是
// 没有被实际执行过的。"全绿"不等于"这两道跑过了"。有软链权限的环境(CI 的
// Linux 容器)必须跑一遍 —— 那是唯一能真正验证它们的地方。
//
// ── 首轮用例跑出来的两个真 bug(现在都有回归覆盖)──────────────────────
//
// **S17** `SignDownloadURL` 对 public key 做 TrimPrefix 时,key 已经是拼好的
//   `/res/public/...`,再拼一次 PublicBase 就成了 `/res/public/res/public/x`。
//   → 这条在单桶单树模型下已经**不存在**(只有一种 key 形态,也就没有二次拼接)。
// **S18** `paginate` 里 marker 查找被 `len(objs) > MaxKeys` 包着,于是"一页装得下"
//   的目录里翻页直接从头开始,第二页返回第一页的内容。
//   → 用例 `PaginationExhausted`
//
// 两个都是**第一轮 go test 就红的**,不是探针挖出来的 —— 探针只是事后确认用例
// 盯住了它们。
//
// ── 探针修正前后的三个真假绿(用例自己也会骗人)──────────────────────────
//
// **S14** `assertRootFiles` 当时会过滤 `.put-*` 前缀,而 S14 要检的正是"Put 失败
//   之后有没有留下 .put-* 临时文件" —— 断言助手把它要找的东西过滤掉了。
//   修法:另加一个**不过滤**的 `assertNoInternalLeftovers`,专门盯内部残留。
// **S15** `TestPut_CtxCancelled` 是绿的,但绿在了 `renameWithRetry` 的 ctx 检查上,
//   `ctxReader` 根本**没被走到** —— 大文件传输中途取消 ctx 这条路无人验证。
//   修法:补 `TestCtxReader` 直接单测 `ctxReader` 本身。
// **S2 / S8** 两处分支从原用例**不可达**:`escapes` 的 `rel == ".."` 那支靠 sibling
//   用例打不中(被 HasPrefix 抢先判掉);`Complete` 的连续性检查在原用例里从来没
//   构造过"客户端谎报一个完整的分片清单但中间缺片"的输入。
//   修法:S2 补"目标就是 root 父目录"那条;S8 补 `TestComplete_RejectsGapFullReport`
//   (伪造 {1,3} 的完整报告)。
//
// ── 探针本身差点骗人(最值得带走的一条)────────────────────────────────
//
// **第一轮报的是 17/17 全红,而那 17 个红全是假的。**
// 原因:脚本从仓库根目录跑 `go test ./internal/pkg/oss/local/`,包路径解析成
// `package internal/pkg/oss/local is not in std`,每个探针都没编译就挂了;而我当时
// 用退出码分类,退出码 1 就记成 RED。"探针全红"和"用例根本没跑起来"在输出上
// 完全一样 —— 是 S11 的结果跟我的预测**相反**(我预期它该红)才暴露的。
//
// 现在 `scripts/probe_oss_local.py` 里固化了三条:
//   1. `os.chdir(server)` —— go test 必须在模块根跑
//   2. RED 必须在输出里**字面**找到 `--- FAIL:`;编译/装载错误一律判 BROKE
//   3. **整轮开跑前先验基线绿**,基线不绿直接退出、一条探针都不跑
// 另外 `subprocess.run(text=True)` 在中文 Windows 控制台会 GBK 解码崩溃,补了
// `encoding='utf-8', errors='replace'`;报告写 UTF-8 文件,stdout 只出 ASCII 摘要。
//
// 一句话:**探针结果只能证明"这个用例盯住了这行",不能证明"这行是对的"。**
