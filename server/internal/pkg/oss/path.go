// 文件系统模块 M76 —— 路径规范 + key 校验 + 列举类型
//
// 设计见 `docs/文件系统模块设计方案V1.0.md` §3。
//
// 路径形态(**只有一种**):
//
//	rapid-admin/{userId}/{biz}/{folderId}/{对象名}
//
// ⚠️ **没有"公开子树"这一层**。存储侧只有一个树(本地磁盘)或一个桶(阿里云 OSS),
// **所有文件都能按 URL 直接取到**;"私密"只意味着**外部用户猜不到这个 key**
// (uid 是雪花 id、对象名带随机后缀),而不是"存在另一个不给人看的目录"。
//
// 所以 key 里**不许有 public 段**,也不许有"公开/私有"两套形态 —— 两套形态
// 意味着两套路径规则、两套前缀清单、两处删除前缀逻辑,而它们迟早会分叉。
//
// 三个段(userId / biz / folderId)**全部由服务端决定**,客户端只能提交文件名。
// 客户端往 key 里塞任何东西都要在 BuildKey 这里被拒 —— 这是整个模块
// 防止越权写别人目录的第一道闸;第二道闸在 local.Provider.resolve 的
// filepath.Rel 边界检查(见 local.go)。
package oss

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// ============== 常量 ==============

const (
	// DefaultRoot 默认路径根词。
	//
	// ⚠️ **2026-10-31 从 `res` 改成 `rapid-admin`** —— 平台后台的名字。
	// 改名是一次**数据迁移**,不是改个常量就完了:key 的第一段同时是
	// 静态挂载点,改了之后存储上的对象和库里的 key 必须一起搬,
	// 否则每个历史文件 404(而且直链不经后端,日志里可能一次请求都没有)。
	//
	// ⚠️ **它必须跟 `config` 里的 `file.path_root` 默认值一致** ——
	// config 里显式配了值时这个常量只是兜底,但两者一旦漂移,
	// "用兜底值的路径"和"用配置值的路径"会长成两棵树。
	// `internal/config` 的测试盯着这件事。
	//
	// 为什么不用 `assets`:本仓 `biz_digital_asset` 已经是「交付资产」
	// 的业务表,用 `assets/` 当存储根会让人误以为这里放的是那张表的数据。
	DefaultRoot = "rapid-admin"

	// maxObjectNameBytes 对象名(含扩展名)字节上限。
	//
	// 240 是这么来的:key 全长上限给 1024,固定前缀 rapid-admin/{uid}/{biz}/{fid}/
	// 约 60 字节,时间戳+随机 16 字节,留 512 给 file_key 列 —— 中文文件名按
	// UTF-8 算,240 字节 ≈ 80 个汉字,日常够用。
	maxObjectNameBytes = 240

	// maxKeyBytes key 总长上限。sys_file.file_key 是 varchar(512),超了写不进去。
	maxKeyBytes = 512
)

// ============== 错误 ==============

// ErrInvalidKey key 不合法(路径穿越 / 超长 / 段非法)
type ErrInvalidKey struct {
	Key    string
	Reason string
}

func (e *ErrInvalidKey) Error() string {
	return fmt.Sprintf("oss: invalid key %q: %s", e.Key, e.Reason)
}

// ErrInvalidObjectName 文件名不合法
type ErrInvalidObjectName struct {
	Name   string
	Reason string
}

func (e *ErrInvalidObjectName) Error() string {
	return fmt.Sprintf("oss: invalid object name %q: %s", e.Name, e.Reason)
}

// ============== SafeKey ==============

// SafeKey 校验对象 key 是否安全 —— 拒目录穿越、控制字符、空段、超长。
//
// ⚠️ **语义与旧版不同,这是有意的**。旧版(`aliyun` 包)是纯 ASCII 白名单
// `[a-zA-Z0-9/_.-]`,会拒掉 `res/123/asset/456/户型图.pdf` —— 而中文文件名
// 在本平台是常态。旧版零调用方,所以是零成本修正。
//
// 新语义是**"这个 key 拿去拼磁盘路径 / URL 会不会出事"**,而不是"是不是 ASCII":
//
//   - 允许 Unicode 字母数字(含中日韩),因为文件名要给人看
//   - 仍然拒绝 `..`(目录穿越)、`\`(Windows 路径分隔符)、控制字符、空段
//   - 仍然拒绝单段超长 —— 防止构造出几千层的 key 撑爆日志和数据库列
//
// ⚠️ **SafeKey 不等于安全**:它只管字符,**不管这个 key 属不属于当前用户**。
// 归属校验在 service 层(§8.4),别拿 SafeKey 当授权用。
func SafeKey(key string) bool {
	if key == "" || len(key) > maxKeyBytes {
		return false
	}
	// 目录穿越。宁可误杀 `报告..最终.pdf`(真有人这么命名)也不放过一个穿越。
	if strings.Contains(key, "..") {
		return false
	}
	// 空段:`a//b`、首尾 `/` 都会被拼出意料之外的路径。
	// ⚠️ 内部空段最容易漏 —— 只查 HasPrefix/HasSuffix 的话 `res//a` 会漏过去,
	// 而它在 filepath.Join 之后会静默塌成 `res/a`,让"这条 key 属于哪个目录"
	// 变得不可预测。TestSafeKey/空段 盯的就是这一行。
	if strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") || strings.Contains(key, "//") {
		return false
	}
	if strings.Contains(key, "\\") {
		return false
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return false
		}
		// 路径分隔符只在 '/' 这一位允许(上面已单独查过 \)
		if r == '/' {
			continue
		}
	}
	// 必须真的是合法 UTF-8 —— 非法字节序列会在 JSON 序列化时炸,不如提前拒
	return utf8.ValidString(key)
}

// ============== 文件名清洗 ==============

// SanitizeObjectName 清洗客户端提交的文件名,返回可安全放进 key 的对象名。
//
// 做四件事:
//  1. Unicode NFC 归一化 —— macOS 的 HFS+/APFS 用 NFD 存名,同一个字「户」
//     有两种编码。不归一化的话 `户型图.pdf` 和 `户型图.pdf` 会变成两个 key。
//  2. 剥路径 —— 只取最后一段,客户端传 `../../etc/passwd` 拿到的是 `passwd`
//  3. 替换控制字符和 Windows 禁用字符为 `_`
//  4. NFC 之后判长度(归一化会让长度变化,必须归一化完再判)
//
// ⚠️ **不返回空串**:清洗完什么都没有(比如原名是 `///`)时,返回一个保底的
// `unnamed`,让上层总有可用的对象名去拼 key。
func SanitizeObjectName(name string) (string, error) {
	if name == "" {
		return "", &ErrInvalidObjectName{Name: name, Reason: "空文件名"}
	}
	// 归一化必须在剥路径之前 —— 否则 NFD 的组合字符可能跨分隔符边界
	name = norm.NFC.String(name)

	// 剥路径:只保留最后一段。客户端传 a/b/c.pdf 拿到 c.pdf。
	// 这里不查 `..` 是有意的:上面已经允许它被剥掉了,而拒绝整个请求
	// 对用户没有价值 —— 上传结果就是他要的那个文件名。
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	// 剥完可能是空串(原名是 `///`)或纯点(`.` / `..`)
	if name == "" || name == "." || name == ".." {
		return "", &ErrInvalidObjectName{Name: name, Reason: "剥路径后为空或纯点"}
	}

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r == 0 || unicode.IsControl(r):
			// 控制字符(含 \n \r \0)一律换掉 —— 它们会在日志和终端里制造假行
			b.WriteByte('_')
		case r == '<' || r == '>' || r == ':' || r == '"' || r == '|' || r == '?' || r == '*':
			// Windows 禁用集 + glob 元字符。local 模式下这些是真会出事的
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()

	// 归一化之后才判长度,而且按字节判(数据库列按字节算)
	if len(out) > maxObjectNameBytes {
		return "", &ErrInvalidObjectName{
			Name:   name,
			Reason: fmt.Sprintf("超过 %d 字节(归一化后 %d)", maxObjectNameBytes, len(out)),
		}
	}
	return out, nil
}

// ============== 路径构造 ==============

// PathSpec 一次路径构造的输入。userID / folderID 由服务端从 DB/会话取,
// **绝不能从请求体直接透传**。
type PathSpec struct {
	// Root 路径根,空则用 DefaultRoot
	Root string
	// UserID 归属用户
	UserID uint64
	// Biz 业务英文名
	Biz string
	// FolderID 文件夹 ID(扁平结构,没有父层 —— 见文档 §2.7)
	FolderID uint64
	// ObjectName 对象名(已经 SanitizeObjectName 清洗过)
	ObjectName string
}

// BuildKey 拼出完整 key。任一段不合法返错,绝不"尽力而为"拼一个半合法的出来。
func BuildKey(spec PathSpec) (string, error) {
	root := spec.Root
	if root == "" {
		root = DefaultRoot
	}
	if err := validateSegment("root", root); err != nil {
		return "", err
	}
	if spec.UserID == 0 {
		return "", &ErrInvalidKey{Reason: "user_id 为 0"}
	}
	if err := validateSegment("biz", spec.Biz); err != nil {
		return "", err
	}
	if spec.FolderID == 0 {
		return "", &ErrInvalidKey{Reason: "folder_id 为 0"}
	}
	if spec.ObjectName == "" {
		return "", &ErrInvalidKey{Reason: "object_name 为空"}
	}
	if strings.ContainsAny(spec.ObjectName, "/\\") || strings.Contains(spec.ObjectName, "..") {
		return "", &ErrInvalidObjectName{Name: spec.ObjectName, Reason: "含路径分隔符或 .."}
	}

	parts := []string{root,
		fmt.Sprintf("%d", spec.UserID),
		spec.Biz,
		fmt.Sprintf("%d", spec.FolderID),
		spec.ObjectName,
	}
	key := strings.Join(parts, "/")

	// 拼完再过一遍 SafeKey —— 这是"防御纵深"的最后一层:
	// 万一上面某条规则被改松了,这里还能兜住。
	if !SafeKey(key) {
		return "", &ErrInvalidKey{Key: key, Reason: "拼接结果未通过 SafeKey"}
	}
	return key, nil
}

// ============== key 解析 ==============

// KeyParts 解析后的 key 片段。
//
// ⚠️ **它是 key 的唯一反解入口** —— "这个 key 属于谁 / 哪个文件夹"这个问题
// 全仓只能问这一份实现。不许在 service / handler / 前端各写一遍
// `strings.Split(key, "/")` 再按下标取:那种写法在"biz 段数变了"、
// "root 可配"这两种情况下会错位,而且错位之后取到的 uid 是别人的 ——
// 归属校验会静默失效。
type KeyParts struct {
	// Root 路径根词
	Root string
	// UserID 归属用户
	UserID uint64
	// Biz 业务英文名
	Biz string
	// FolderID 文件夹 ID
	FolderID uint64
	// ObjectName 对象名(含时间戳+随机前缀)
	ObjectName string
}

// ParseKey 解析 key 成结构化片段。
//
// ⚠️ **root 必须传** —— "哪一段是 root"只有调用方知道(root 可配,默认 DefaultRoot)。
//
// ⚠️ **段数是死的,这是刻意的**:对象名里不许有 "/"(BuildKey 拒),
// 根词是单段(validateSegment 拒 "/"),所以一个合法 key 一定是 5 段:
// root / uid / biz / folder / name。多一段少一段都是非法 key ——
// **不许"尽力而为"解析出一半**:归属校验宁可失败也不能猜。
func ParseKey(root, key string) (*KeyParts, error) {
	if root == "" {
		root = DefaultRoot
	}
	if !SafeKey(key) {
		return nil, &ErrInvalidKey{Key: key, Reason: "未通过 SafeKey"}
	}
	segs := strings.Split(key, "/")
	if len(segs) != 5 {
		return nil, &ErrInvalidKey{Key: key, Reason: "段数必须是 5(root/uid/biz/folder/name)"}
	}
	if segs[0] != root {
		return nil, &ErrInvalidKey{Key: key, Reason: fmt.Sprintf("根词不匹配(期望 %q)", root)}
	}
	uid, err := strconv.ParseUint(segs[1], 10, 64)
	if err != nil || uid == 0 {
		return nil, &ErrInvalidKey{Key: key, Reason: "user_id 段不合法"}
	}
	fid, err := strconv.ParseUint(segs[3], 10, 64)
	if err != nil || fid == 0 {
		return nil, &ErrInvalidKey{Key: key, Reason: "folder_id 段不合法"}
	}
	biz := segs[2]
	if err := validateSegment("biz", biz); err != nil {
		return nil, err
	}
	name := segs[4]
	if name == "" {
		return nil, &ErrInvalidKey{Key: key, Reason: "对象名为空"}
	}
	return &KeyParts{
		Root:       root,
		UserID:     uid,
		Biz:        biz,
		FolderID:   fid,
		ObjectName: name,
	}, nil
}

// validateSegment 校验 root / biz 这类**结构段**。
//
// 刻意比 SafeKey 严:这两段会出现在目录路径和 DB 索引里,必须是纯 ASCII 小写,
// 不允许 Unicode —— 否则 `biz=户` 和 `biz=戶` 会是两个目录,而运维在控制台里分不清。
func validateSegment(name, v string) error {
	if v == "" {
		return &ErrInvalidKey{Reason: name + " 为空"}
	}
	if len(v) > 32 {
		return &ErrInvalidKey{Key: v, Reason: name + " 超过 32 字符"}
	}
	for i, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
			continue
		case r >= '0' && r <= '9':
			// 首位不能是数字(避免和纯数字段混淆)
			if i == 0 {
				return &ErrInvalidKey{Key: v, Reason: name + " 不能以数字开头"}
			}
			continue
		case r == '_' || r == '-':
			if i == 0 {
				return &ErrInvalidKey{Key: v, Reason: name + " 不能以下划线/连字符开头"}
			}
			continue
		default:
			return &ErrInvalidKey{Key: v, Reason: fmt.Sprintf("%s 含非法字符 %q(仅允许小写字母/数字/下划线/连字符)", name, r)}
		}
	}
	return nil
}

// ============== 对象名生成 ==============

// NewObjectName 生成存储用对象名:{unix秒}-{rand8}-{清洗后原名}。
//
// 为什么要前缀而不用原名直接当 key:
//
//  1. **大小写撞车** —— local 模式的磁盘在 Windows/macOS 上大小写不敏感。
//     `A.pdf` 和 `a.pdf` 在 DB 里是两行,在磁盘上是同一个文件,第二行会静默
//     覆盖第一行。带上前缀就绕开了:前缀保证唯一,原名只用来让人看得懂。
//  2. **并发同秒** —— 没有 rand8 的��,同一秒内同目录两个同名文件会撞。
//
// 扩展名从清洗后的名字上取,所以 `.pdf` 一定留在末尾(hertz 静态服务靠它
// 定 Content-Type)。
func NewObjectName(originalName string) (string, error) {
	clean, err := SanitizeObjectName(originalName)
	if err != nil {
		return "", err
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 失败是不可预期状态,宁可上传失败也不要生成可预测的名字
		return "", fmt.Errorf("oss: 生成对象名随机段失败: %w", err)
	}
	return fmt.Sprintf("%d-%s-%s", time.Now().Unix(), hex.EncodeToString(b[:]), clean), nil
}

// ObjectExt 取扩展名(含点),无扩展名返回空串。
func ObjectExt(objectName string) string {
	i := strings.LastIndex(objectName, ".")
	if i <= 0 {
		// i == 0 是 `.gitignore` 这种"点开头"的名字,按没有扩展名处理
		return ""
	}
	ext := objectName[i:]
	if len(ext) > 16 {
		// 不合理的长扩展名通常是攻击或误传
		return ""
	}
	return ext
}

// FolderPrefix 文件夹前缀(带尾斜杠),用于 List / DeletePrefix。
//
// ⚠️ **只有一个前缀** —— 存储侧没有公开/私有两套布局(见文件头),所以一个
// 文件夹在存储里只占一段前缀。以前这里返回两条(公开 + 私有),少删一条就是
// "文件夹删了但存储用量没降",而且没有任何东西会告诉你。
func FolderPrefix(root string, userID uint64, biz string, folderID uint64) (string, error) {
	spec := PathSpec{Root: root, UserID: userID, Biz: biz, FolderID: folderID, ObjectName: "x"}
	key, err := BuildKey(spec)
	if err != nil {
		return "", err
	}
	// BuildKey 保证 key 以 "x" 结尾,砍掉这一位
	return key[:len(key)-1], nil
}

// ============== 列举结果 ==============

// ObjectMeta 单个对象的元信息
type ObjectMeta struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mime_type"`
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"last_modified"`
}

// ListOption List 选项
type ListOption struct {
	// Prefix 列举前缀
	Prefix string
	// Marker 分页游标(上次返回的 NextMarker),空表示第一页
	Marker string
	// MaxKeys 单页上限,0 = provider 默认
	MaxKeys int
	// IncludeSelf 列举 CommonPrefixes(虚拟目录),OSS 有效,local 由实现自行决定
	IncludeSelf bool
}

// ListResult List 结果
type ListResult struct {
	// Objects 对象列表
	Objects []ObjectMeta `json:"objects"`
	// CommonPrefixes 虚拟目录名(OSS 里"目录"只是前缀,不是真实体)
	CommonPrefixes []string `json:"common_prefixes"`
	// NextMarker 下一页游标,IsTruncated 为 true 时有效
	NextMarker string `json:"next_marker"`
	// IsTruncated 还有更多
	IsTruncated bool `json:"is_truncated"`
}
