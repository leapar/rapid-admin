// Package local 本地磁盘 Provider —— 文件系统模块 M77
//
// 设计见 `docs/文件系统模块设计方案V1.0.md` §6。
//
// 与 OSS 的根本差别:**OSS 的"目录"只是前缀,本地磁盘的目录是真的**。于是
// OSS 上天然不成立的一批攻击面在这里全部成立 —— 最要紧的一条是 key 里的 `../`
// 在 OSS 上只是三个普通字符,在本地会真的把文件写出存储根。
//
// 三层防御(少一层都不行,它们防的是不同的人):
//
//	① 字符层 SafeKey      —— 防**调用方传入的恶意 key**
//	② 边界层 filepath.Rel —— 防 Join 之后的路径确实还在 root 下
//	③ 符号链接层           —— 防**别的进程在 root 里建了软链**
//
// ②和③不是"③更严"的关系,而是"③查的东西②查不到":②只看字面路径,而
// `root/data -> /etc` 这种软链的字面路径完全合法。
package local

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/retryx"
)

// multipartTempDir 分片临时目录名。挂在 root 下,**与业务数据隔离**:
// 它必须在 root 内(否则原子 rename 跨文件系统会失败),但绝不能出现在
// List 的结果里,更不能被客户端的 DeletePrefix 删掉。
const multipartTempDir = ".mpu"

// putTempPrefix 单次直写的临时文件前缀。List 里按前缀过滤掉。
const putTempPrefix = ".put-"

// MaxPartSize local 模式的单片上限。
//
// ⚠️ **这不是随便定的**:本仓没有内存池(见 框架层未完成项清单 §1.11.2),
// 而 `UploadPart` 的入参是 `[]byte` —— 一片就是一次堆分配。16 片并发 × 16MB
// = 256MB 峰值,这是刻意设的闸。上限比 OSS 严格(OSS 上限 5GB),因为这里的
// 约束来自**本进程内存**而不是存储服务。
const MaxPartSize int64 = 16 << 20

// Provider 本地磁盘 provider
type Provider struct {
	root string
	// urlBase 静态挂载点,如 "/res"。空 = 不提供静态直出。
	urlBase string
	// partLocks uploadID -> mutex,串行化同一会话的并发分片写。
	//
	// 放实例上而不是包级全局:测试会造多个 provider,共享一张锁表会让"这个锁
	// 是谁加的"变成排查噩梦。分片上传本来就是并发的,所以用 sync.Map 而不是
	// 一把大锁 —— 全局锁会把所有用户的分片请求排队。
	partLocks sync.Map
}

// Config 构造配置
type Config struct {
	// Root 存储根目录。相对路径会被转成绝对,不存在会被创建。
	Root string
	// URLBase 静态挂载点,如 "/rapid-admin"。
	//
	// ⚠️ **key 的第一段(path_root)会被剥掉再拼上来** —— 磁盘上是
	// `<Root>/rapid-admin/...`,而 URL 是 `/rapid-admin/...`,挂载点本身已经
	// 代表了那段前缀。不剥的话 URL 变成 `/rapid-admin/rapid-admin/...`,
	// 静态路由**永远 404,而且不报错**。
	//
	// ⚠️ 本地磁盘**只有一个树**,没有"公开/私有"两个目录 —— 所有文件都能
	// 按 URL 直取,"私密"只意味着外部用户猜不到 key(uid 是雪花 id、对象名带
	// 随机后缀)。所以这里**没有** publicPrefix 之类的前缀白名单。
	URLBase string
}

// New 构造本地 provider。
func New(cfg Config) (*Provider, error) {
	if cfg.Root == "" {
		return nil, errors.New("local: Root 必填")
	}
	abs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("local: 解析 Root 失败: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("local: 创建 Root 失败: %w", err)
	}
	// 取**真实**根路径。配了软链的部署(常见:/data -> /mnt/data)上,后面所有
	// Rel 比较都必须用真实路径,否则每一层比较都会因为"root 和 p 长得不一样"
	// 而恒不相等,把合法请求全拒掉。
	realRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("local: 解析 Root 真实路径失败: %w", err)
	}
	return &Provider{
		root:    realRoot,
		urlBase: strings.TrimSuffix(cfg.URLBase, "/"),
	}, nil
}

// Root 返回存储根真实路径(诊断/统计用)
func (p *Provider) Root() string { return p.root }

// Name provider 名
//
// ⚠️ 用 oss.ProviderNameLocal 常量,不要写字面量 —— service 层要按这个名字
// 判断"文件实际在哪"(model.FileStorage),两边必须对得上。
func (p *Provider) Name() string { return oss.ProviderNameLocal }

// ============== 三层防御 ==============

// resolve 把 key 解析成磁盘绝对路径。**不要求目标存在**(Put 时目标还不存在)。
func (p *Provider) resolve(key string) (string, error) {
	if !oss.SafeKey(key) {
		return "", &oss.ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	target := filepath.Join(p.root, filepath.FromSlash(key))

	// ② 边界:Join 之后必须仍在 root 下。
	// ⚠️ 绝不能用 strings.Contains(target, p.root) —— root=/data/res 时
	// /data/res2/x 也 Contains 成立。filepath.Rel 给出"相对 root 的路径",
	// 判 ../ 前缀是精确的。
	rel, err := filepath.Rel(p.root, target)
	if err != nil || escapes(rel) {
		return "", &oss.ErrInvalidKey{Key: key, Reason: "解析后逃出存储根"}
	}

	// ③ 符号链接:① ② 都只看字面路径。root 里若已存在软链
	// (root/data -> /etc),key=data/x 能过前两层,写出去却落在别处。
	//
	// ⚠️ 求值对象是**最近的已存在祖先目录**,不是目标本身:Put 时目标文件还没建,
	// 对文件调 EvalSymlinks 会拿到 ENOENT,把正常上传全拒掉(第一版就是这么错的)。
	// 不存在的那些段是纯粹的名字 —— `..` 已被 ① 拒掉,`/` 只当分隔符 —— 所以
	// 它们不可能引入逃逸,祖先在 root 下就够了。
	realAncestor, err := evalExistingAncestor(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	realRel, err := filepath.Rel(p.root, realAncestor)
	if err != nil || escapes(realRel) {
		return "", &oss.ErrInvalidKey{Key: key, Reason: "父目录经符号链接后逃出存储根"}
	}
	return target, nil
}

// evalExistingAncestor 从 dir 往上找到第一个存在的目录,返回它的真实路径。
func evalExistingAncestor(dir string) (string, error) {
	probe := dir
	for {
		if _, err := os.Stat(probe); err == nil {
			real, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return "", fmt.Errorf("local: 解析祖先目录 %q 真实路径失败: %w", probe, err)
			}
			return real, nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			// 到盘根了还没找到存在的目录 —— 不该发生(root 本身一定存在)
			return "", fmt.Errorf("local: 从 %q 往上找不到存在的目录", dir)
		}
		probe = parent
	}
}

// escapes 判断相对路径是否逃出了根。
func escapes(rel string) bool {
	if rel == ".." {
		return true
	}
	// 跨平台:Windows 的 Rel 返回 `..\`,POSIX 返回 `../`
	return strings.HasPrefix(rel, ".."+string(os.PathSeparator)) ||
		strings.HasPrefix(rel, "../")
}

// resolvePrefix 给 List / DeletePrefix 用。前缀天然带尾斜杠,而 SafeKey 拒尾斜杠 ——
// 不在这里剪掉,`oss.FolderPrefix` 造出来的前缀会被 SafeKey 拒掉,列表功能整个哑火。
func (p *Provider) resolvePrefix(prefix string) (string, error) {
	if prefix == "" {
		return "", errors.New("local: 前缀必填")
	}
	return p.resolve(strings.TrimSuffix(prefix, "/"))
}

// resolveFile 要求目标是**已存在的文件**(不是目录)。
func (p *Provider) resolveFile(key string) (string, os.FileInfo, error) {
	abs, err := p.resolve(key)
	if err != nil {
		return "", nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, &oss.ErrNotFound{Key: key}
		}
		return "", nil, fmt.Errorf("local: stat %q 失败: %w", key, err)
	}
	if st.IsDir() {
		return "", nil, &oss.ErrNotFound{Key: key}
	}
	// ④ 最终组件也可能是软链:② ③ 只查了**祖先目录**,而
	// `root/a.pdf -> /etc/shadow` 的字面路径和祖先(root)都完全合法,
	// Stat/Get/Open 会跟随软链把 root 外的内容读出去。
	//
	// 走到这里目标已确认存在,对整条路径 EvalSymlinks 只多一次系统调用;
	// 解析结果必须仍在 root 下,否则拒绝 —— 这是**读路径**的威胁模型:
	// 防的是"别的进程把 root 里的文件名换成指向外部的软链",不是防
	// stat 与 EvalSymlinks 之间软链被替换的 TOCTOU(与 Put 注释里记的
	// 是同一类理论窗口,Go 没有可移植的 O_NOFOLLOW,不假装堵上)。
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// 刚 stat 过还失败 = 并发删除之类的竞态,如实报错
		return "", nil, fmt.Errorf("local: 解析 %q 真实路径失败: %w", key, err)
	}
	if realRel, err := filepath.Rel(p.root, real); err != nil || escapes(realRel) {
		return "", nil, &oss.ErrInvalidKey{Key: key, Reason: "文件经符号链接后逃出存储根"}
	}
	return abs, st, nil
}

// ============== 上传 ==============

// SignUploadToken local 磁盘没有"直传"这回事 —— 浏览器没法直接写服务器的磁盘。
//
// 所以固定返回 Mode=proxy:前端拿到之后知道该 POST 给后端,而不是拿着空 host
// 去请求一个不存在的地址。
func (p *Provider) SignUploadToken(ctx context.Context, key string, ttl time.Duration, maxSize int64) (*oss.UploadToken, error) {
	if _, err := p.resolve(key); err != nil {
		return nil, err
	}
	if maxSize <= 0 {
		maxSize = 20 << 20
	}
	tok := &oss.UploadToken{
		Mode:     oss.UploadModeProxy,
		Key:      key,
		MaxSize:  maxSize,
		ExpireAt: time.Now().Add(ttl).Unix(),
		Provider: p.Name(),
	}
	if err := tok.Check(); err != nil {
		return nil, err
	}
	return tok, nil
}

// Put 服务端直写,**原子**:写同目录临时文件 → fsync → rename 覆盖。
//
// 为什么临时文件必须在**同目录**:跨文件系统的 rename 会失败(EXDEV),
// 而 /tmp 和数据盘几乎总是两个文件系统。
func (p *Provider) Put(ctx context.Context, key string, r io.Reader, contentType string) (*oss.ObjectMeta, error) {
	abs, err := p.resolve(key)
	if err != nil {
		return nil, err
	}
	// resolve 只需要最近的已存在祖先,所以新文件夹在这里才建。建完**再解析一次**:
	// 这一次整条路径都真实存在,软链检查是完整的。两次之间仍有理论上的
	// TOCTOU(别的进程在 MkdirAll 窗口里插软链),Go 的 os 包没有可移植的
	// O_NOFOLLOW 来堵 —— 记在这里,不假装堵上了。
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return nil, fmt.Errorf("local: 建目录失败: %w", err)
	}
	if abs, err = p.resolve(key); err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(abs), putTempPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("local: 建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// 任何提前返回都要清掉临时文件,否则存储目录会积一堆 .put-*
	defer func() { _ = os.Remove(tmpName) }()

	// 边写边算 md5:local 没有 OSS 的 ETag 语义,但一个稳定可复现的摘要
	// 能让"同一份内容重传两次"这件事在日志里认得出来。单独回读一遍文件只为
	// 算 md5 是白白的 IO。
	hasher := md5.New()
	n, err := io.Copy(io.MultiWriter(tmp, hasher), &ctxReader{ctx: ctx, r: r})
	if err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("local: 写入失败(已写 %d 字节): %w", n, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("local: fsync 失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("local: 关闭临时文件失败: %w", err)
	}
	if err := p.renameWithRetry(ctx, tmpName, abs); err != nil {
		return nil, err
	}

	return &oss.ObjectMeta{
		Key:          key,
		Size:         n,
		MimeType:     p.detectType(abs, key, contentType),
		ETag:         hex.EncodeToString(hasher.Sum(nil)),
		LastModified: time.Now(),
	}, nil
}

// renameWithRetry rename 覆盖,**带退避重试**。
//
// ⚠️ **Windows 上 rename 覆盖一个"正被读"的文件会 EPERM,而且不是瞬时的**
// —— 读方只要还开着 handle,退避重试 6 次约 100ms 照样失败。POSIX 无此问题。
// 本地开发跑在 Windows 上,不重试就是偶发失败,而偶发失败最难查。
// 这个坑是 wenjuan 项目在 Node 侧实测踩过的,Go 侧同样适用。
//
// 重试用 `retryx` 而不是自己写循环:退避/上限/可重试判定在那儿已经统一了,
// 再抄一份必然和它漂移。
func (p *Provider) renameWithRetry(ctx context.Context, oldPath, newPath string) error {
	err := retryx.Do(ctx, retryx.Opts{
		MaxAttempts:    6,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     50 * time.Millisecond,
		Multiplier:     2,
	}, func(context.Context) error {
		return os.Rename(oldPath, newPath)
	})
	if err != nil {
		return fmt.Errorf("local: rename %s → %s 失败(重试 6 次仍失败时先查是谁打开着目标文件): %w",
			filepath.Base(oldPath), filepath.Base(newPath), err)
	}
	return nil
}

// ============== 访问 ==============

// SignDownloadURL 返静态直链 —— **本地磁盘上的每个文件都有直链**。
//
// ⚠️ **本地没有"公开/私有"两套目录**(见 Config.URLBase 的注释):整棵树都
// 挂在静态路由上,所以**任何文件都能按 URL 直取**。"私密"只意味着外部用户
// **猜不到这个 key**(uid 是雪花 id、对象名带 8 位随机后缀),而不是"存在
// 一个别人访问不到的目录"。
//
// 所以这里**没有**"私有文件不给 URL"那条分支 —— 以前有,是为了配合
// "只有 public 子树挂静态"的老模型;模型改了就必须一起去掉,否则文件
// 会既没有直链、又没被静态路由覆盖,变成谁也取不到的死文件。
//
// ⚠️ **必须剥掉 key 的第一段(path_root)**:hertz 的静态挂载是
// `app.FS{Root: <root>/rapid-admin}` 挂在 `/rapid-admin`,所以挂载点本身已经
// 代表了那段前缀。不剥的话 URL 会变成 /rapid-admin/rapid-admin/1/...,
// 中间件再拼回去就指向 <root>/rapid-admin/rapid-admin/1/... ——
// 那个路径不存在,而且不报错。
func (p *Provider) SignDownloadURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if _, _, err := p.resolveFile(key); err != nil {
		return "", err
	}
	// ⚠️ urlBase 为空 = 没配静态挂载点 —— 这时候给不出直链,必须报出来,
	// 不能返回一个 "/xxx" 这种看着能用的废 URL。
	if p.urlBase == "" {
		return "", fmt.Errorf("local: 未配置静态挂载点(URLBase),给不出直链: %s", key)
	}
	idx := strings.Index(key, "/")
	if idx < 0 {
		return "", &oss.ErrInvalidKey{Key: key, Reason: "key 没有路径段"}
	}
	rel := key[idx+1:]
	parts := strings.Split(rel, "/")
	for i, seg := range parts {
		// 逐段编码而不是整条 key 编码一次:整条编码会把 `/` 也变成 %2F,
		// 静态路由就再也匹配不上了。
		parts[i] = url.PathEscape(seg)
	}
	return p.urlBase + "/" + strings.Join(parts, "/"), nil
}

// Open 打开读句柄 —— service 层流式返回私有文件用。
//
// 不挂在 Provider 接口上是因为它要把 io.Reader 和 Content-Type 一起交出去,
// 而接口里的 SignDownloadURL 只能给一个字符串(见文档 §2.6)。
func (p *Provider) Open(ctx context.Context, key string) (io.ReadSeekCloser, *oss.ObjectMeta, error) {
	abs, st, err := p.resolveFile(key)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, nil, fmt.Errorf("local: 打开 %q 失败: %w", key, err)
	}
	return f, &oss.ObjectMeta{
		Key:          key,
		Size:         st.Size(),
		MimeType:     p.detectType(abs, key, ""),
		LastModified: st.ModTime(),
	}, nil
}

// ============== 小元数据回读 ==============

// maxGetBytes Get 的字节上限(8MB)。
//
// ⚠️ **Get 是给"小元数据"用的**(当前唯一用途:info.json,几 KB)。
// 这个上限是防手滑的闸门 —— 拿它读大文件会 OOM。
// 要读大文件用 Open(见上面的 ContentProvider)。
const maxGetBytes = 8 << 20

// Get 读回对象的字节(小元数据专用,见 oss.MetaReader)。
func (p *Provider) Get(ctx context.Context, key string) ([]byte, error) {
	abs, st, err := p.resolveFile(key)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxGetBytes {
		return nil, fmt.Errorf("local: 对象 %q 有 %d 字节,超过 Get 上限(%d);要读大文件请用 Open",
			key, st.Size(), int64(maxGetBytes))
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		// ⚠️ resolveFile 已经 stat 过了,走到这里还 ENOENT = 文件在 stat 之后
		//   被删了(并发删文件)。按"不存在"报,跟 stat 的结论一致。
		if os.IsNotExist(err) {
			return nil, &oss.ErrNotFound{Key: key}
		}
		return nil, fmt.Errorf("local: 读取 %q 失败: %w", key, err)
	}
	return b, nil
}

// ============== 信息 ==============

// Stat 查单个对象的元信息。
func (p *Provider) Stat(ctx context.Context, key string) (*oss.ObjectMeta, error) {
	abs, st, err := p.resolveFile(key)
	if err != nil {
		return nil, err
	}
	return &oss.ObjectMeta{
		Key:          key,
		Size:         st.Size(),
		MimeType:     p.detectType(abs, key, ""),
		LastModified: st.ModTime(),
	}, nil
}

// detectType 定 Content-Type:调用方给的 → 扩展名 → 嗅探前 512 字节。
//
// ⚠️ **嗅探放在最后**:扩展名命中就不必读盘,而且 `http.DetectContentType`
// 对 .pdf/.mp4 这些其实认不出来(它认的是图片/文本/压缩包那几类),
// 拿它当权威会在文档和视频上乱标成 application/octet-stream。
func (p *Provider) detectType(abs, key, given string) string {
	if given != "" {
		return given
	}
	if ext := oss.ObjectExt(filepath.Base(key)); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	if f, err := os.Open(abs); err == nil {
		defer func() { _ = f.Close() }()
		buf := make([]byte, 512)
		if n, _ := f.Read(buf); n > 0 {
			return http.DetectContentType(buf[:n])
		}
	}
	return "application/octet-stream"
}

// ============== 列举 ==============

// List 列举前缀下的对象和子目录。
//
// ⚠️ **ListOption.IncludeSelf 在这里被忽略**:OSS 的"目录"是虚拟前缀,自不含
// 自己;local 的目录是真实目录,遍历时天然只返回它的**内容**,不需要这个开关。
// 实现它反而要多一次特判,还容易在两种语义之间选错一边。
func (p *Provider) List(ctx context.Context, opt oss.ListOption) (*oss.ListResult, error) {
	abs, err := p.resolvePrefix(opt.Prefix)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			// 前缀不存在 = 空目录,**不是错误**。OSS 上 List 不存在的 prefix 也
			// 返空 —— 两边行为必须一致,否则换存储后端时前端会突然报错。
			return &oss.ListResult{Objects: []oss.ObjectMeta{}, CommonPrefixes: []string{}}, nil
		}
		return nil, fmt.Errorf("local: 读取目录 %q 失败: %w", opt.Prefix, err)
	}

	base := strings.TrimSuffix(opt.Prefix, "/")
	res := &oss.ListResult{Objects: []oss.ObjectMeta{}, CommonPrefixes: []string{}}
	for _, e := range entries {
		name := e.Name()
		// 内部状态不对外可见:分片临时目录、还没 rename 完的直写临时文件
		if strings.HasPrefix(name, putTempPrefix) || name == multipartTempDir {
			continue
		}
		if e.IsDir() {
			res.CommonPrefixes = append(res.CommonPrefixes, name+"/")
			continue
		}
		info, err := e.Info()
		if err != nil {
			// 竞态删除(另一个请求刚删掉)。跳过一个文件,不要整个 List 失败 ——
			// 用户看到"列表加载失败"会以为是系统坏了。
			continue
		}
		res.Objects = append(res.Objects, oss.ObjectMeta{
			Key:          base + "/" + name,
			Size:         info.Size(),
			MimeType:     p.detectType(filepath.Join(abs, name), name, ""),
			LastModified: info.ModTime(),
		})
	}
	sort.Slice(res.Objects, func(i, j int) bool { return res.Objects[i].Key < res.Objects[j].Key })
	sort.Strings(res.CommonPrefixes)
	return paginate(res, opt), nil
}

// paginate 按 marker / maxKeys 截断。
//
// ⚠️ **ListResult 的公共前缀不参与分页**:OSS 的分页标记只管对象,虚拟目录
// 会在每一页里重复出现(这是 OSS 的既定行为)。跟着分页就会漏掉它们。
func paginate(res *oss.ListResult, opt oss.ListOption) *oss.ListResult {
	objs := res.Objects
	start := 0
	if opt.Marker != "" {
		// 排他:严格大于 marker。OSS 的 marker 语义也是排他。
		//
		// ⚠️ **这一步必须在 MaxKeys 判断之前**。反过来写的话,当对象数没超过
		// MaxKeys 时会直接返回,marker 被整个忽略 —— 调用方传一个越界的 marker
		// 却拿到完整列表,续传就会无限重复。(TestList_PaginationExhausted 首跑
		// 抓出来的。)
		start = sort.Search(len(objs), func(i int) bool { return objs[i].Key > opt.Marker })
	}
	if start >= len(objs) {
		res.Objects = nil
		res.NextMarker, res.IsTruncated = "", false
		return res
	}
	if opt.MaxKeys > 0 && len(objs)-start > opt.MaxKeys {
		end := start + opt.MaxKeys
		res.IsTruncated = true
		// NextMarker = 本页最后一个 key,下一页从"严格大于它"开始。少了这个
		// 标记,下一页会把本页最后一条再返回一次。
		res.NextMarker = objs[end-1].Key
		res.Objects = objs[start:end]
		return res
	}
	res.Objects = objs[start:]
	return res
}

// ============== 删除 ==============

// Delete 删对象(批量),**幂等**:删不存在的算成功。
//
// 幂等是有意的:上传流程里"写成功但 DB 事务回滚"会留下孤儿对象,清理任务
// 第一遍删一遍、第二遍再删一遍 —— 第二遍必须还是成功,否则清理任务会卡在
// 同一条上永远跑不完。
func (p *Provider) Delete(ctx context.Context, keys ...string) error {
	for _, k := range keys {
		abs, err := p.resolve(k)
		if err != nil {
			return err
		}
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("local: 删除 %q 失败: %w", k, err)
		}
	}
	return nil
}

// DeletePrefix 递归删除前缀下的全部内容,返回**释放的字节数**。
//
// 两个刻意的选择:
//
//  1. **不用 os.RemoveAll** —— 它跟着符号链接走进目标目录(我们在 resolve 里
//     费劲防的就是这个),而且不返回统计数,而配额对账需要它。
//  2. **统计的是文件大小,目录本身算 0** —— 配额按"用户存了多少字节"算,
//     一个空目录占的那点元数据不该计费。
//
// ⚠️ 调用方要自己保证 prefix 不等于存储根:传 `res` 会把整个树删光。
// 这不是 provider 该拦的(它不知道哪个前缀合法),但 service 层拼 prefix 时
// 必须用 oss.FolderPrefix,不能直接用客户端传的字符串。
func (p *Provider) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	abs, err := p.resolvePrefix(prefix)
	if err != nil {
		return 0, err
	}
	var (
		freed int64
		dirs  []string
	)
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		// 软链:**不删也不跟**。WalkDir 默认就不跟(Lstat 而非 Stat),这里显式
		// 判一次是为了让"故意留着软链"这件事写在代码里,而不是靠读者去翻
		// WalkDir 的文档。
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			if path != abs {
				dirs = append(dirs, path) // 记下来,文件删完后自底向上清
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // 竞态删除,同上
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("local: 删除 %q 失败: %w", path, err)
		}
		freed += info.Size()
		return nil
	})
	if err != nil {
		return freed, fmt.Errorf("local: 递归删除 %q 中断(已释放 %d 字节): %w", prefix, freed, err)
	}
	// 倒序清目录。非空目录的 Remove 会失败并被忽略 —— 那说明有并发在往里写,
	// 不该由这次删除来报错。
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
	_ = os.Remove(abs)
	return freed, nil
}

// ============== 分片(local 用定长偏移模拟)==============
//
// 为什么不能用 O_APPEND 追加:客户端并发传 part 3 和 part 4,或者续传时**重传**
// part 3,追加都会把内容接到错的位置 —— 文件大小看起来对,内容全花。
// 定长偏移 offset = (partNumber-1) * partSize 让重传天然幂等,而且偏移是纯函数,
// 不用存。
//
// 代价:provider 得记住每个 uploadID 的 partSize —— `UploadPart` 的接口里没有
// 这个参数。用一个 sidecar JSON 存,**不引 DB 依赖**(provider 不知道 service
// 有哪些表)。正式记录在 DB 的 sys_upload_session 里,两边各有各的用处:
// sidecar 给 provider 自己用,DB 那份给 service 层查状态用。

// sidecar 分片会话的元信息
type sidecar struct {
	Key       string            `json:"key"`
	PartSize  int64             `json:"part_size"`
	TotalSize int64             `json:"total_size"`
	CreatedAt int64             `json:"created_at"`
	Parts     map[string]string `json:"parts"` // partNumber(十进制字符串) → etag
}

func (p *Provider) sidecarPath(uploadID string) string {
	return filepath.Join(p.root, multipartTempDir, uploadID+".json")
}

func (p *Provider) partPath(uploadID string) string {
	return filepath.Join(p.root, multipartTempDir, uploadID+".part")
}

// InitMultipartUpload 初始化分片会话。
func (p *Provider) InitMultipartUpload(ctx context.Context, key string, opts ...oss.InitOption) (*oss.MultipartInit, error) {
	// resolve 只是校验,不建目录 —— 真正的目录在 Complete 时才建,这样失败的
	// 分片会话不会在存储里留下空文件夹。
	if _, err := p.resolve(key); err != nil {
		return nil, err
	}
	var o oss.InitOpts
	for _, fn := range opts {
		fn(&o)
	}
	partSize := o.PartSize
	if partSize <= 0 {
		partSize = oss.DefaultPartSize
	}
	if err := oss.CheckPartSize(partSize); err != nil {
		return nil, err
	}
	if partSize > MaxPartSize {
		return nil, fmt.Errorf("local: 分片大小 %d 超过本地上限 %d(见 MaxPartSize 注释:这是内存闸,不是存储限制)", partSize, MaxPartSize)
	}

	tmpDir := filepath.Join(p.root, multipartTempDir)
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		return nil, fmt.Errorf("local: 建临时目录失败: %w", err)
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败是不可预期状态,宁可上传失败也不要生成可预测的 uploadID
		return nil, fmt.Errorf("local: 生成 uploadID 失败: %w", err)
	}
	uploadID := hex.EncodeToString(b[:])

	f, err := os.OpenFile(p.partPath(uploadID), os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("local: 建分片临时文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("local: 关闭分片临时文件失败: %w", err)
	}

	sc := sidecar{Key: key, PartSize: partSize, TotalSize: o.TotalSize, CreatedAt: time.Now().Unix(), Parts: map[string]string{}}
	if err := p.writeSidecar(uploadID, &sc); err != nil {
		_ = os.Remove(p.partPath(uploadID))
		return nil, err
	}

	partNum := 0
	if sc.TotalSize > 0 {
		partNum = int((sc.TotalSize + partSize - 1) / partSize) // 向上取整
	}
	return &oss.MultipartInit{Key: key, UploadID: uploadID, PartSize: partSize, PartNum: partNum}, nil
}

func (p *Provider) readSidecar(uploadID string) (*sidecar, error) {
	b, err := os.ReadFile(p.sidecarPath(uploadID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &oss.ErrNotFound{Key: uploadID}
		}
		return nil, fmt.Errorf("local: 读分片元信息失败: %w", err)
	}
	var sc sidecar
	if err := json.Unmarshal(b, &sc); err != nil {
		return nil, fmt.Errorf("local: 分片元信息损坏 %s: %w", uploadID, err)
	}
	if sc.Parts == nil {
		sc.Parts = map[string]string{}
	}
	return &sc, nil
}

func (p *Provider) writeSidecar(uploadID string, sc *sidecar) error {
	b, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("local: 序列化分片元信息失败: %w", err)
	}
	if err := os.WriteFile(p.sidecarPath(uploadID), b, 0o640); err != nil {
		return fmt.Errorf("local: 写分片元信息失败: %w", err)
	}
	return nil
}

// UploadPart 写一个分片。
//
// ⚠️ **`len(body)` 的检查来得太晚**:入参是已经分配好的 []byte,超限的片早就
// 占住内存了。真正的闸在 service 层的 body limit —— 那儿能在分配**之前**拒。
// 这里这道是为"provider 被别处直接调用"兜底,不是主防线。
func (p *Provider) UploadPart(ctx context.Context, key, uploadID string, partNumber int, body []byte) (string, error) {
	if err := oss.CheckPartNumber(partNumber); err != nil {
		return "", err
	}
	if int64(len(body)) > MaxPartSize {
		return "", fmt.Errorf("local: 分片 %d 大小 %d 超过上限 %d", partNumber, len(body), MaxPartSize)
	}

	mu := lockFor(&p.partLocks, uploadID)
	mu.Lock()
	defer mu.Unlock()

	sc, err := p.readSidecar(uploadID)
	if err != nil {
		return "", err
	}
	if sc.Key != key {
		return "", fmt.Errorf("local: uploadID %s 属于 key %q,与传入的 %q 不符", uploadID, sc.Key, key)
	}

	// 超出声明的总大小就拒 —— 否则一个伪造的大分片能把临时文件撑爆磁盘。
	offset := int64(partNumber-1) * sc.PartSize
	if sc.TotalSize > 0 && offset+int64(len(body)) > sc.TotalSize {
		return "", fmt.Errorf("local: 分片 %d 写到 offset %d 会超出总大小 %d(分片大小 %d)",
			partNumber, offset, sc.TotalSize, sc.PartSize)
	}

	f, err := os.OpenFile(p.partPath(uploadID), os.O_WRONLY, 0o640)
	if err != nil {
		return "", fmt.Errorf("local: 打开分片文件失败: %w", err)
	}
	n, err := f.WriteAt(body, offset)
	cerr := f.Close()
	if err != nil {
		return "", fmt.Errorf("local: 写分片 %d(offset %d,写入 %d/%d 字节)失败: %w",
			partNumber, offset, n, len(body), err)
	}
	if cerr != nil {
		return "", fmt.Errorf("local: 关闭分片文件失败: %w", cerr)
	}

	// ETag = 内容的 md5。重传同一片得到同一个 ETag,Complete 时据此校验。
	// ⚠️ 顺序是"先写盘再记 sidecar":崩在中间的话 part 在盘上但没记账,
	// ListParts 漏报它 → 客户端重传 → WriteAt 幂等覆盖 → 自愈。反过来(先记账)
	// 崩了就是"账上有、盘上没有",Complete 会拿一个空洞去 rename 成正式文件。
	sum := md5.Sum(body)
	etag := hex.EncodeToString(sum[:])
	sc.Parts[strconv.Itoa(partNumber)] = etag
	if err := p.writeSidecar(uploadID, sc); err != nil {
		return "", err
	}
	return etag, nil
}

// ListParts 列出已上传分片(断点续传用)。
func (p *Provider) ListParts(ctx context.Context, key, uploadID string) ([]oss.Part, error) {
	sc, err := p.readSidecar(uploadID)
	if err != nil {
		return nil, err
	}
	if sc.Key != key {
		return nil, fmt.Errorf("local: uploadID %s 与 key %q 不符", uploadID, key)
	}
	parts := make([]oss.Part, 0, len(sc.Parts))
	for i := 1; i <= len(sc.Parts); i++ {
		// ⚠️ 逐号查而不是遍历 map:map 无序,而"传了第 5 片没传第 3 片"这种状态
		// 必须原样暴露(第 2 片缺失 → ListParts 只报到第 1 片),Complete 的
		// 连续性校验会拒它。让客户端看到断点在哪,而不是看到一份乱序列表。
		etag, ok := sc.Parts[strconv.Itoa(i)]
		if !ok {
			break
		}
		parts = append(parts, oss.Part{PartNumber: i, ETag: etag, Size: partSizeOf(sc, i)})
	}
	return parts, nil
}

// partSizeOf 某一片的实际字节数。总大小已知时最后一片是余数,否则按声明的分片
// 大小算(宁可多报,让客户端自己核对)。
func partSizeOf(sc *sidecar, n int) int64 {
	if sc.TotalSize > 0 {
		if rest := sc.TotalSize - int64(n-1)*sc.PartSize; rest < sc.PartSize {
			return rest
		}
	}
	return sc.PartSize
}

// CompleteMultipartUpload 合并成正式对象。
//
// 三道校验,少一道就是把坏文件变成正式文件:
//  1. 客户端报的 part 列表要和**服务端记账**的 ETag 逐条一致
//  2. 分片号必须 1..N 连续(缺片会在文件里留一个洞)
//  3. 合并后尺寸必须等于声明的 TotalSize(给了的话)
func (p *Provider) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []oss.Part) error {
	mu := lockFor(&p.partLocks, uploadID)
	mu.Lock()
	defer mu.Unlock()

	sc, err := p.readSidecar(uploadID)
	if err != nil {
		return err
	}
	if sc.Key != key {
		return fmt.Errorf("local: uploadID %s 与 key %q 不符", uploadID, key)
	}
	// 校验不过就清掉整个会话:留着它等于留着一个下次会被 Complete 再次捡起来
	// 的半成品,而客户端已经拿到失败了 —— 没人会再来 Abort 它。
	cleanup := func() {
		_ = os.Remove(p.partPath(uploadID))
		_ = os.Remove(p.sidecarPath(uploadID))
		p.partLocks.Delete(uploadID)
	}

	// 1 + 2:客户端报的列表 vs 服务端记账
	if len(parts) != len(sc.Parts) {
		cleanup()
		return fmt.Errorf("local: 分片数不符(客户端报 %d,服务端记 %d)", len(parts), len(sc.Parts))
	}
	clientParts := make(map[int]string, len(parts))
	for _, pt := range parts {
		clientParts[pt.PartNumber] = pt.ETag
	}
	for i := 1; i <= len(sc.Parts); i++ {
		want, ok := sc.Parts[strconv.Itoa(i)]
		if !ok {
			cleanup()
			return fmt.Errorf("local: 分片 %d 缺失,不能合并", i)
		}
		got, ok := clientParts[i]
		if !ok {
			cleanup()
			return fmt.Errorf("local: 分片 %d 服务端有、客户端没报", i)
		}
		// 客户端 ETag 允许为空(老客户端不上报),但填了就必须对上
		if got != "" && got != want {
			cleanup()
			return fmt.Errorf("local: 分片 %d 的 ETag 不符(客户端 %s,服务端 %s)", i, got, want)
		}
	}

	// 3:尺寸。WriteAt 不是原子的,进程崩在两次写之间会留下半截文件;
	// 客户端也可能漏传最后一片。尺寸对不上就丢弃 —— 把坏文件 rename 成正式
	// 文件,比失败难查一百倍。
	st, err := os.Stat(p.partPath(uploadID))
	if err != nil {
		// sidecar 有账而 .part 读不到 = 会话已经坏了(僵尸/竞态删除)。
		// 留着它只会让下一次 Complete 在同一处再炸一次 —— 连 sidecar 一起清掉,
		// 别残留。
		cleanup()
		return fmt.Errorf("local: stat 分片文件失败: %w", err)
	}
	if sc.TotalSize > 0 && st.Size() != sc.TotalSize {
		cleanup()
		return fmt.Errorf("local: 合并尺寸不符(期望 %d,实际 %d),已丢弃临时文件", sc.TotalSize, st.Size())
	}

	abs, err := p.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return fmt.Errorf("local: 建目录失败: %w", err)
	}
	if err := p.renameWithRetry(ctx, p.partPath(uploadID), abs); err != nil {
		return err
	}
	_ = os.Remove(p.sidecarPath(uploadID))
	p.partLocks.Delete(uploadID)
	return nil
}

// AbortMultipartUpload 取消分片会话,清临时文件。幂等。
//
// ⚠️ 必须持 partLock(UploadPart/Complete 都持锁):不持锁的 Abort 会在
// 「UploadPart 写完 .part、还没把 sidecar 记账写回」的间隙里删掉 .part,
// 等 UploadPart 把 sidecar 写回来,就落成「sidecar 有账、.part 已删」的
// 僵尸会话 —— 下一次 Complete 会拿一个空洞去 rename 成正式文件。
func (p *Provider) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	mu := lockFor(&p.partLocks, uploadID)
	mu.Lock()
	defer mu.Unlock()

	sc, err := p.readSidecar(uploadID)
	if err != nil {
		if oss.IsNotFound(err) {
			return nil // 幂等:会话已经没了(完成/取消过)
		}
		return err
	}
	if sc.Key != key {
		return fmt.Errorf("local: uploadID %s 与 key %q 不符", uploadID, key)
	}
	_ = os.Remove(p.partPath(uploadID))
	_ = os.Remove(p.sidecarPath(uploadID))
	p.partLocks.Delete(uploadID)
	return nil
}

// PurgeExpired 清掉创建时间超过 ttl 的分片会话,返回清掉几个。
//
// local 模式**没有云厂商的生命周期规则**兜底:OSS 侧未完成的 multipart 会由
// OSS 自动清理,本地这些 .part/.json 会一直堆着,直到磁盘满。
// ttl 由调用方给(service 层知道自己的会话有效期)。
func (p *Provider) PurgeExpired(ctx context.Context, ttl time.Duration, now time.Time) (int, error) {
	dir := filepath.Join(p.root, multipartTempDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // 还没初始化过分片,没有可清的
		}
		return 0, fmt.Errorf("local: 扫描分片临时目录失败: %w", err)
	}
	cutoff := now.Add(-ttl).Unix()
	n := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue // 只认 sidecar;孤立的 .part 交给下面兜底扫
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		sc, err := p.readSidecar(id)
		if err != nil || sc.CreatedAt > cutoff {
			continue
		}
		if err := p.AbortMultipartUpload(ctx, sc.Key, id); err == nil {
			n++
		}
	}
	return n + p.purgeOrphanParts(cutoff), nil
}

// purgeOrphanParts 扫没有 sidecar 的孤立 .part 文件。
//
// 什么时候会有:上传崩溃在"建 .part 之后、写 sidecar 之前"那个窗口。那种文件
// 永远等不到 Abort(没人知道它的 uploadID),只能靠时间扫。
func (p *Provider) purgeOrphanParts(cutoff int64) int {
	dir := filepath.Join(p.root, multipartTempDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".part") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Unix() > cutoff {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			n++
		}
	}
	return n
}

// ============== 实现检查 ==============

var (
	_ oss.Provider          = (*Provider)(nil)
	_ oss.MultipartProvider = (*Provider)(nil)
	// ⚠️ **MetaReader 的实现检查**(M133):以前没有这一行,而 M133 之前
	// local 压根没实现 Get —— service 层 type-assert 失败静默降级成
	// "info_json 恒为空串",没有任何编译期或运行期信号。
	_ oss.MetaReader = (*Provider)(nil)
)

// ============== 工具 ==============

// lockFor 取(或建)某个 uploadID 的锁
func lockFor(m *sync.Map, uploadID string) *sync.Mutex {
	v, _ := m.LoadOrStore(uploadID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// ctxReader 让 io.Copy 能被 ctx 打断
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		return c.r.Read(p)
	}
}

// 编译期保证 local 满足 Provider。
//
// ⚠️ **Open 故意不在接口里**。它返回 io.ReadSeekCloser,是因为本地下载需要
// Range(视频拖进度条)和流式转发;OSS 那侧走的是签名 URL 重定向,没有等价的
// 流式读接口。把它塞进主接口就等于逼 aliyun 实现一个它做不到的方法,于是
// 所有 provider 都得返回 "不支持",调用点还得到处 type-assert —— 比多一个
// 具体类型断言更糟。要用它就 `p.(*local.Provider).Open(...)`。
var _ oss.Provider = (*Provider)(nil)
