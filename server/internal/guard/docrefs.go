package guard

// docrefs.go 是"文档指路牌"扫描器 —— M130(2026-10-31)。
//
// # 这个文件解决什么问题
//
// M129 删掉 `pkg/sms/` 之后顺手发现的:清单里还留着几处指向 `pkg/httputil/`
// 的路标(那个包 M112 就删了)。**清单是排期的人唯一会去读的文档** ——
// 它上面每一个反引号包着的路径,读者都会当成"去这儿看"的指路牌。
//
// 指路牌会撒谎。M112 删 `pkg/httputil/` 时改了 3 处文档,却漏了清单里的
// 另一处 —— 读者去读扑空,或者更糟:以为"这个能力已经有人做过了"。
// 本仓已经因为"忘了改文档"吃过一次亏(清单 §1.10 #10 那个指向幻影 API
// `cachex.Counter` 的悬空指引)。
//
// ⇒ 这里把"文档里指的路还通不通"变成 `go test` 里的一条断言。
//
// # 怎么判一个反引号 token 是不是"路标"
//
// 不是所有反引号内容都是路径。判据在 pathTokenOf 里,四条:
//
// - **至少一个斜杠** —— 单文件名在本仓有重名(`router.go` 有好几个),
// 判不出它指的是哪一个,查了也是噪音;
// - **每一段都像文件名** —— 只含字母数字下划线点横线;
// - **首段不像域名** —— `github.com/...` 是依赖坐标;
// - **末段不是标识符** —— `TwoLevel` / `msn.Sender` 是类型名。
//
// ⚠️ 末段那条判据看的是**形状**,不是**认不认得它**:
// 有扩展名 ⇒ 扩展名部分**原样**全小写(`.go` / `.tsx` / `.servce`);
// 无扩展名 ⇒ 整段全小写(目录)。
// **不能用"扩展名白名单"** —— 白名单会把**拼错的扩展名**整条漏掉:
// `deploy/systemd/rapid-admin.servce` 指的是一个不存在的文件,
// 但 ".servce" 不在白名单里 ⇒ 守卫**看不见**它。
// (实测踩过:探针把 `.service` 拼成 `.servce`,守卫一声不吭 ——
// **探针逼出来的规则**,不是想出来的。)
//
// 过了这关还要过 looksLikeRepoPath:**首段必须是本仓某个目录的名字**。
//
// ⚠️ 这条过滤是本文件最要紧的一条取舍。文档里大量反引号内容**根本不是
// 仓库路径**: `antd/es/table`(npm 包内部布局)、`encoding/json`(标准库)、
// `owner/system/users`(相对 URL)、`40100/40300`(错误码区间)、
// `unknown/pending/reviewing`(枚举值)。它们的首段都不是本仓目录名,
// 一律跳过。
//
// ⇒ 代价:会漏掉"首段本身就不存在"的错指针(凭空写个 `nowhere/foo.go`)。
// ⇒ 换来:零误报。**误报比漏报更致命** —— 一个天天误报的守卫,
// 没人愿意跑第二次。
//
// # 怎么判"这一处是历史陈述,不是现役路标"
//
// 清单里合法地提到已删除的包(`pkg/httputil/` 已删除(M112:…)),
// 这**不是**断链。判据(exemptByDeletion):附近有删除动词,
// 且**中间没有别的路标** —— 中间隔了别的路标,说明这个动词说的不是它。
//
// ⚠️ "别的路标"指**别的路标**,不是别的反引号段 ——
// 清单第 637 行是 "`repository/principal_cache.go` 与 `authz.CacheBackend`
// 一并删除":`authz.CacheBackend` 是个类型名(不是路标),
// 但它就卡在路标与删除动词中间 ⇒ 用"反引号段"当判据会把这一行误报。
//
// 例:`utils/eventBus.ts` | **删除**(连同 `utils/index.ts` 里的再导出)
// —— "删除"与 `utils/eventBus.ts` 之间没有别的路标 ⇒ 它是历史陈述。
//
// ⚠️ **豁免是逐行判的** —— 删除动词必须和路标在**同一行**。
// 跨行不豁免:散文里换行会把动词甩到下一行,而表格里"上一行一个路径、
// 下一行一个删除动词"更是常见 ⇒ 跨行豁免会把不相干的路径放过。
// ⇒ 写"这个包已删"时,把删除动词和那个路径**写在同一行**。
//
// ⚠️ 判据是"没有别的路标隔在中间",**不是**"离删除动词最近的那个路标"。
// 清单第 2494 行:「本行此前写的是 `pkg/sms/provider.go` —— 那个包…已于
// M129 删除;真正在用的是 `pkg/smslimit`」—— "删除"离后面那句更近,
// 但它说的显然是**前面**那个包。按"最近"判会把这行误报。
//
// # 只查一种谎,故意不查另一种
//
// 本守卫只报 **"路标指向不存在的路径"**。
//
// **故意不查"文档说它已删除,但它还在"** —— 那种谎更隐蔽,但判不准:
// 散文里"删除"到底指哪个路径不可靠。实测两处就会误判:
// - 「那是**已删的 `pkg/sms.VerifyStore`** 的口径」—— "已删"挨着的
//   是 `pkg/sms.VerifyStore`,不是前面那句里的 `internal/model/sms.go`;
// - 「本行此前写的是 `pkg/sms/provider.go` —— 那个包…已于 M129 删除;
//   真正在用的是 `pkg/smslimit`」—— "删除"离后一句更近。
//
// ⇒ 宁可漏,不错报。这条取舍写在这里,免得后来人以为"守卫管两种谎"。
//
// # 路由后缀:一个已知的误报面,以及为什么改文档而不是改守卫
//
// 文档里写**路由**(比如 `auth/login`)时,那个 token 的形状和仓库路径
// **一模一样** —— 首段是本仓某个目录名(`server/internal/pkg/auth/`),
// 末段像文件名 ⇒ 守卫会把它当成路标 ⇒ 报"指向不存在的路径"。
//
// ⚠️ **这不是守卫的 bug,是 token 本身的歧义** —— 读者扫到 `auth/login`
//   也确实可能去找那个文件。守卫**判不了**"这一处是路由还是路径":
//   上下文在文档里,不在 token 里。
//
// ⇒ **处置:改文档,不加豁免名单。**
//   `server/README.md` 的路由表因此把每一项写成**带前导 `/`** 的形式
//   (`/auth/login`)—— 前导斜杠让 `pathTokenOf` 直接判定"不是仓库路径",
//   路由语义一点没变(前缀 + 后一段 = 完整路径)。
//
// ⚠️ **为什么不加一张"路由 token 白名单"**:白名单会随着文档增长,
//   而且它是**逃逸名单** —— 一旦表上多一项,守卫在那儿就永远不看了。
//   改文档是**一次性**的,而且顺带让读者看得更清楚。
//
// # 扫哪些文档(为什么不是"所有 md")
//
// 只有**描述现状**的文档能进扫描集(见 ScannedDocs)。设计稿**故意不进** ——
// 实测把 4 份设计稿 + README 一起扫,21 条命中里只有 **4 条是真问题**
// (README 那条断链 + 设计稿里 3 处引了本仓不存在的现役文件),其余是:
//
//   - **还没建的计划**(9 条):`pkg/shorturl` / `cmd/cache/scanner/main.go` /
//     `docs/api/config-full.md`(原文自己写着"P1+ 补")…—— "路径不存在"在这类
//     文档里意味着"还没实现",不是"指路牌烂了";
//   - **第三方库内部路径**(3 条):hertz 的 `pkg/app/context.go` /
//     `pkg/route/engine.go` / `pkg/app/fs.go` —— 本仓的 `pkg/` 指的是
//     `server/internal/pkg/`,两者同名不同物;
//   - **订正表 / 旧写法里故意引用的路径**(4 条):技术方案 §4.1 那张
//     「目录树订正表」的第一列写的就是**"设计稿里写的(错的)那个"**,
//     不写出来就没法对照;
//   - **另一个项目的路径**(1 条):`api/handler/scenes/private/oss_api.go`
//     指的是参考项目,不是本仓。
//
// ⇒ 把它们混进来,守卫就是"天天误报"的那种,没人愿意跑第二次。
// ⇒ **设计稿里真正的错(引了本仓不存在的现役文件)靠人读** ——
// M130 顺手修了 3 处(见清单 M130 落地记录),但**没有机械守门网**。

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// ScannedDocs 守卫扫描的文档清单(相对仓库根)。
//
// ⚠️ **只有"描述现状"的文档能进这里** —— 设计稿不进,原因见文件头
// 「扫哪些文档」。加新文档前先想清楚:那份文档里的路径是"现在就在那儿",
// 还是"打算在那儿"。
//
// ⚠️ 这份清单是**账本** —— 有一组测试守着它不许留已经不存在的文件名
// (留着 = 悄悄把一份文档移出覆盖范围,而且没人会发现)。
var ScannedDocs = []string{
	// 排期的人唯一会去读的文档 —— 它上面的反引号路径,读者都当成指路牌。
	"docs/ROADMAP.md",
	// 仓库结构说明 —— 目录树 + 部署章节,引的都是现役文件。
	"server/README.md",
}

// DocRefStale 路标指向一个**不存在**的路径。
const DocRefStale = "stale"

// maxMarkerDistance 删除动词与它所解释的路标之间的最大距离(**按字符数,
// 不是字节数**)。
//
// ⚠️ **必须按字符算**:文档是中文,一个汉字 3 字节 —— 用字节算的话
// 清单第 2494 行那句「本行此前写的是 `pkg/sms/provider.go` —— …已于 M129 删除」
// 实测 65 字节(= 31 字符),按字节算会被判成"离得太远",把一处合法的
// 历史陈述误报成断链。
//
// ⚠️ 40 是量出来的:清单里"删除动词 → 它解释的那个路标"的距离实测
// **最大 31 个字符**(2494 行);而同一行里**不相干**的路标距离实测 ≥ 66
// (3411 行的 `user/ui` 离"删掉" 187 个字符)。40 落在两者中间。
const maxMarkerDistance = 40

// DocRefIssue 一条指路牌问题。
type DocRefIssue struct {
	// File 相对仓库根的文档路径
	File string
	// Line 1 起的行号
	Line int
	// Token 文档里反引号包着的那个路标(已剥掉 `:行号` 后缀)
	Token string
	// Kind 目前只有 DocRefStale
	Kind string
	// Note 补充说明(给"你是不是想写这个"的提示)
	Note string
}

func (i DocRefIssue) String() string {
	if i.Kind == DocRefStale {
		return fmt.Sprintf("%s:%d 路标 `%s` 指向不存在的路径", i.File, i.Line, i.Token)
	}
	return fmt.Sprintf("%s:%d `%s`", i.File, i.Line, i.Token)
}

// codeSpan 一段反引号代码。
type codeSpan struct {
	content string
	start   int // 内容起始下标
	end     int // 内容结束下标(不含)
}

// markerSpan 一个删除动词。
type markerSpan struct {
	text  string
	start int
	end   int
}

// deletionPhrases 删除动词词表。
//
// ⚠️ **"删" 单字要按"前一个字是不是'量'"来排除** —— "批量删除"
// (OSS 的 Delete 方法的正常描述)里也有"删",把它当删除动词会让
// §1.10 那一行整行被当成历史陈述。
//
// ⚠️ 词表里收"删"单字,是因为清单里两种常见句式不带"了/掉":
// 「**删**(M129-1)」(表格的"处置"列)和「照 M112 删 `pkg/httputil` 的先例」。
var deletionPhrases = []string{
	"已删除", "已下线", "已退役", "已移除", "已废弃", "已停用",
	"一并删除", "一起删除", "均已删除",
	"删掉", "删了", "删除", "已删", "不存在", "不再存在",
	"删",
}

// vendorNames 第三方库名 —— 它们**内部**的路径不是本仓的路标。
//
// ⚠️ 只在"库名紧挨着反引号段"时才生效(见 isVendorPath)——
// 判据是"这串反引号内容是那个库的内部路径",不是"这个库名出现过"。
//
// 例:清单里「hertz `pkg/protocol/request.go:156`」—— 那是 hertz 的
// 源码路径,不是本仓的(本仓的 `pkg/` 指的是 `server/internal/pkg/`)。
var vendorNames = map[string]bool{
	"hertz": true, "hertz-contrib": true, "gin": true, "echo": true,
	"casbin": true, "gorm": true, "zap": true, "viper": true,
	"axios": true, "antd": true, "react": true, "vue": true,
	"zustand": true, "dayjs": true, "lodash": true, "vite": true,
	"vitest": true, "swagger": true, "golang": true, "node": true,
}

// repoIndex 仓库里所有路径的索引(用来解析路标)。
type repoIndex struct {
	// paths 仓库内所有文件与目录(相对根,斜杠分隔)
	paths map[string]bool
	// dirs 所有目录(相对根),按长度升序 —— 短的先试,
	// 因为文档里最常见的写法是"离某个目录最近"的那种简写。
	dirs []string
	// dirNames 所有目录的**名字**(不含路径)——
	// 用来判"这个路标的首段是不是本仓的目录"。
	dirNames map[string]bool
	// byBase 文件名 → 相对路径(给"你是不是想写这个"用)
	byBase map[string][]string
	// byStem 文件名**去掉扩展名** → 相对路径 —— 给"扩展名拼错了"用:
	// 探针把 `.service` 拼成 `.servce`,同名词干的文件就是答案。
	byStem map[string][]string
}

// ignoredDirs 扫描仓库索引时跳过的目录。
//
// ⚠️ `data` 要跳:本仓的 `server/data/` 是**本地开发数据**(dev.db + 上传的文件),
// 它不是仓库内容 —— 也不该被任何人改。
//
// ⇒ 代价:文档里若指 `server/data/xxx` 会被判成断链(全仓实测没有这种指法,
// 文档里提到本地库时写的是单段的 dev.db,单段不算路标)。
var ignoredDirs = map[string]bool{
	".git": true, ".idea": true, ".vscode": true, ".claude": true,
	"node_modules": true, "dist": true, "build": true, "out": true,
	"coverage": true, "vendor": true, "bin": true, "logs": true,
	"data": true, "__pycache__": true, ".venv": true,
}

// buildRepoIndex 索引仓库里所有文件与目录。
func buildRepoIndex(root string) (*repoIndex, error) {
	idx := &repoIndex{
		paths:    map[string]bool{},
		dirNames: map[string]bool{},
		byBase:   map[string][]string{},
		byStem:   map[string][]string{},
	}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			idx.paths[rel] = true
			idx.dirs = append(idx.dirs, rel)
			idx.dirNames[d.Name()] = true
			return nil
		}
		idx.paths[rel] = true
		base := d.Name()
		idx.byBase[base] = append(idx.byBase[base], rel)
		if stem := strings.TrimSuffix(base, path.Ext(base)); stem != "" {
			idx.byStem[stem] = append(idx.byStem[stem], rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	idx.dirs = append(idx.dirs, "") // 仓库根本身也是一个候选前缀
	sort.Slice(idx.dirs, func(i, j int) bool {
		if len(idx.dirs[i]) != len(idx.dirs[j]) {
			return len(idx.dirs[i]) < len(idx.dirs[j])
		}
		return idx.dirs[i] < idx.dirs[j]
	})
	return idx, nil
}

// resolve 把一个简写路标解析成仓库里的真实路径。
//
// 文档里写的是简写(`pkg/oss/...` 指 `server/internal/pkg/oss/...`),
// 所以按"每个目录都当一次前缀"来试 —— 短前缀优先。
//
// 找不到返回空串。
func (idx *repoIndex) resolve(token string) string {
	for _, d := range idx.dirs {
		cand := token
		if d != "" {
			cand = d + "/" + token
		}
		if idx.paths[cand] {
			return cand
		}
	}
	return ""
}

// suggest 给一个解析不出来的路标找"你是不是想写这个"。
//
// ⚠️ 两种断链长得不一样,提示也不一样:
//
//   - **文件名对得上、目录对不上** ⇒ 大概率是路径写错了(少一层 / 写成了别的分支);
//   - **词干对得上、扩展名对不上** ⇒ 大概率是扩展名拼错了
//     (探针把 `.service` 拼成 `.servce` —— 答案就在那儿)。
//
// 先看精确文件名,再看词干:文件名字面都写错了,那多半是整条路径都写错了。
func (idx *repoIndex) suggest(token string) string {
	base := path.Base(strings.TrimSuffix(token, "/"))
	if base == "" || base == "." {
		return ""
	}
	if cands := idx.byBase[base]; len(cands) > 0 {
		return "同名文件在仓里,目录对不上? → " + strings.Join(pick3(cands), " / ")
	}
	stem := strings.TrimSuffix(base, path.Ext(base))
	if stem != "" && stem != base {
		if cands := idx.byStem[stem]; len(cands) > 0 {
			return "同名词干的文件在仓里,扩展名对不上? → " + strings.Join(pick3(cands), " / ")
		}
	}
	return ""
}

// pick3 排序后取前三个(不改动索引里的切片)。
func pick3(cands []string) []string {
	out := make([]string, len(cands))
	copy(out, cands)
	sort.Strings(out)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// looksLikeRepoPath 这个路标的首段是不是本仓某个目录的名字。
//
// ⚠️ 这是一条**取舍**:它会漏掉"首段本身就不存在"的错指针
// (比如凭空写一个 `nowhere/foo.go`)—— 但换来的是零误报。
// 误报比漏报更致命:一个天天误报的守卫,没人愿意跑第二次。
func (idx *repoIndex) looksLikeRepoPath(token string) bool {
	segs := strings.Split(strings.TrimSuffix(token, "/"), "/")
	if len(segs) == 0 {
		return false
	}
	return idx.dirNames[segs[0]]
}

// pathTokenOf 判断一段反引号内容是不是"路标",是就返回剥掉 `:行号` 后的路径。
//
// 返回 ok=false 表示"这不是路径"(类型名、表名、URL、散文)。
func pathTokenOf(content string) (string, bool) {
	s := strings.TrimSpace(content)
	if s == "" {
		return "", false
	}
	// 带空格的是散文(`github.com/aliyun/aliyun-oss-go-sdk v3.0.2`)。
	if strings.ContainsAny(s, " \t") {
		return "", false
	}
	// URL / 绝对路径不是仓库里的路标。
	if strings.Contains(s, "://") || strings.HasPrefix(s, "/") {
		return "", false
	}
	// 剥掉 `:12` / `:12-34` / `:Symbol` 后缀 ——
	// 但只在"冒号前面那截像个文件名"时才剥,否则 `a:b/c` 这种会被切坏。
	if i := strings.LastIndex(s, ":"); i > 0 {
		head := s[:i]
		if isFileLike(path.Base(head)) {
			s = head
		}
	}
	trimmed := strings.TrimSuffix(s, "/")
	if trimmed == "" {
		return "", false
	}
	segs := strings.Split(trimmed, "/")
	for _, seg := range segs {
		if seg == "" || !isNameLike(seg) {
			return "", false
		}
	}
	// 首段像域名(`github.com/...`)⇒ 是依赖坐标,不是仓库路径。
	if isDomainLike(segs[0]) {
		return "", false
	}
	last := segs[len(segs)-1]
	if !isFileLike(last) {
		// `TwoLevel` / `msn.Sender` / `Sender` ⇒ 类型名或常量名,不是路径。
		return "", false
	}
	// 至少要有一个斜杠 —— 单文件名在本仓**有重名**
	// (`router.go` 就有好几个),判不出它指的是哪一个,查了也是噪音。
	if len(segs) < 2 {
		return "", false
	}
	return trimmed, true
}

// isNameLike 一段路径是不是"文件名样"。
func isNameLike(seg string) bool {
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// isLowerish 全小写(允许数字、下划线、横线、点)。
func isLowerish(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-' || c == '.':
		default:
			return false
		}
	}
	return true
}

// isFileLike 末段像文件名(而不是类型名 / 常量名)。
//
// 判据:
//
//   - **有扩展名** ⇒ 扩展名部分**原样**全小写(`.go` / `.tsx` / `.servce`);
//   - **无扩展名** ⇒ 整段全小写(目录,或无扩展名的文件)。
//
// ⚠️ **扩展名不查白名单** —— 白名单会把"拼错的扩展名"整条漏掉:
// `deploy/systemd/rapid-admin.servce` 指的是一个**不存在的**文件,
// 但 ".servce" 不在白名单里 ⇒ 守卫**看不见**它(实测踩过:探针把 `.service`
// 拼成 `.servce`,守卫一声不吭)。⇒ 判"像不像文件名"不能靠"认得它"。
//
// ⚠️ **不能先小写化再判**:那会把 `msn.Sender` 放进来
// (它的扩展名小写化之后是 ".sender",看着像文件)。
// 判的是**原样**的扩展名全不全是小写 —— "Sender" 不是。
func isFileLike(seg string) bool {
	ext := path.Ext(seg)
	if ext == "" {
		return isLowerish(seg)
	}
	return isLowerish(ext[1:])
}

// domainTLDs 常见顶级域 —— 用来把"域名开头的依赖坐标"从路标里剔出去。
//
// ⚠️ **不能用"首段含点就算域名"** —— `router.go`、`provider.go` 这些
// 本仓文件也含点,会被误判成域名而整条丢掉(实测踩过)。
// `.go` 恰好**不在**这个表里,所以 `router.go` 安全。
var domainTLDs = map[string]bool{
	"com": true, "org": true, "net": true, "io": true, "cn": true,
	"dev": true, "edu": true, "gov": true, "info": true, "me": true,
	"ai": true, "co": true, "uk": true, "de": true, "jp": true,
	"ru": true, "so": true, "sh": true, "tv": true, "xyz": true,
	"top": true, "site": true, "online": true, "app": true, "cloud": true,
}

// isDomainLike 首段是不是域名。
func isDomainLike(seg string) bool {
	i := strings.Index(seg, ".")
	if i <= 0 {
		return false
	}
	return domainTLDs[strings.ToLower(seg[i+1:])]
}

// extractCodeSpans 抽出一行里所有反引号代码段。
func extractCodeSpans(line string) []codeSpan {
	var out []codeSpan
	in := false
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '`' {
			continue
		}
		if !in {
			in = true
			start = i + 1
			continue
		}
		out = append(out, codeSpan{content: line[start:i], start: start, end: i})
		in = false
	}
	return out
}

// findMarkers 找出一行里的删除动词。
//
// ⚠️ "批量删除" 里的 "删"/"删除" **不算**删除陈述 —— 那是 OSS `Delete`
// 方法的正常描述。判据是前一个字:`量` ⇒ 跳过。
func findMarkers(line string) []markerSpan {
	var out []markerSpan
	for _, ph := range deletionPhrases {
		from := 0
		for from <= len(line)-len(ph) {
			i := strings.Index(line[from:], ph)
			if i < 0 {
				break
			}
			i += from
			if (ph == "删" || ph == "删除") && i > 0 && prevRune(line[:i]) == '量' {
				from = i + len(ph)
				continue
			}
			out = append(out, markerSpan{text: ph, start: i, end: i + len(ph)})
			from = i + len(ph)
		}
	}
	// 去掉被更长词包住的短词("已删除" 里的 "删除"、"删除" 里的 "删")。
	sort.Slice(out, func(i, j int) bool {
		if out[i].start != out[j].start {
			return out[i].start < out[j].start
		}
		return out[i].end-out[i].start > out[j].end-out[j].start
	})
	kept := out[:0]
	for _, m := range out {
		contained := false
		for _, k := range kept {
			if m.start >= k.start && m.end <= k.end {
				contained = true
				break
			}
		}
		if !contained {
			kept = append(kept, m)
		}
	}
	return kept
}

// prevRune 取 s 的最后一个 rune。
//
// ⚠️ 文档是 UTF-8 —— 直接取 line[i-1] 拿到的是**字节**,不是字。
// "批量删除" 的"删"前面那个字节根本不是'量'。
func prevRune(s string) rune {
	if s == "" {
		return 0
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// CheckDocRefs 扫若干份文档,返回所有指路牌问题。
//
// docs 是相对仓库根的路径(例:docs/框架层未完成项清单v1.md)。
func CheckDocRefs(repoRoot string, docs []string) ([]DocRefIssue, error) {
	idx, err := buildRepoIndex(repoRoot)
	if err != nil {
		return nil, err
	}
	var issues []DocRefIssue
	for _, doc := range docs {
		got, err := scanDoc(idx, repoRoot, doc)
		if err != nil {
			return nil, err
		}
		issues = append(issues, got...)
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].File != issues[j].File {
			return issues[i].File < issues[j].File
		}
		return issues[i].Line < issues[j].Line
	})
	return issues, nil
}

// scanDoc 扫一份文档。
func scanDoc(idx *repoIndex, repoRoot, docRel string) ([]DocRefIssue, error) {
	full := filepath.Join(repoRoot, filepath.FromSlash(docRel))
	b, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	text := string(b)
	var issues []DocRefIssue
	lineStart := 0 // 当前行在整篇文档里的字节偏移
	for n, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		spans := extractCodeSpans(line)
		if len(spans) > 0 {
			markers := findMarkers(line)
			// 先把这一行里**真的是路标**的反引号段挑出来 ——
			// 判"删除动词解释的是哪个路标"时,中间隔着的只能算路标,
			// 类型名(`authz.CacheBackend`)不算。
			type tokenSpan struct {
				codeSpan
				token string
			}
			var tokens []tokenSpan
			for _, sp := range spans {
				tok, ok := pathTokenOf(sp.content)
				if !ok {
					continue
				}
				// 首段不是本仓目录名 ⇒ 这压根不是仓库路径(npm 包内部 /
				// 标准库 / 相对 URL / 错误码区间 / 枚举值),跳过。
				if !idx.looksLikeRepoPath(tok) {
					continue
				}
				// 紧跟在第三方库名后面 ⇒ 那是那个库的内部路径。
				if isVendorPath(text, lineStart+sp.start-1) {
					continue
				}
				tokens = append(tokens, tokenSpan{codeSpan: sp, token: tok})
			}
			tokenCodes := make([]codeSpan, 0, len(tokens))
			for _, t := range tokens {
				tokenCodes = append(tokenCodes, t.codeSpan)
			}
			for _, t := range tokens {
				if idx.resolve(t.token) != "" {
					continue
				}
				// 解析不到 —— 有可能是"这里提过一个已经删掉的东西"。
				if exemptByDeletion(line, tokenCodes, markers, t.codeSpan) {
					continue
				}
				note := idx.suggest(t.token)
				issues = append(issues, DocRefIssue{
					File: docRel, Line: n + 1, Token: t.token,
					Kind: DocRefStale, Note: note,
				})
			}
		}
		lineStart += len(raw) + 1
	}
	return issues, nil
}

// isVendorPath 这个反引号段是不是**第三方库内部**的路径。
//
// pos 是这个反引号段**开头反引号**在整篇文档里的字节下标。
//
// ⚠️ 看的是**紧挨着的那一个标识符** ——「hertz `pkg/protocol/request.go:156`」
// 里的路径是 hertz 的源码,不是本仓的(本仓的 `pkg/` 指
// `server/internal/pkg/`,没有 `pkg/protocol/`)。
//
// ⚠️ **不能按空白切词**:清单里那句写的是「`BodyBuffer()`,hertz `pkg/...`」——
// "hertz" 紧贴在前一个逗号后面,按空白切出来的"最后一个词"是
// "`BodyBuffer()`,hertz",判不中。只能**从反引号往回吃标识符字符**。
func isVendorPath(text string, pos int) bool {
	if pos <= 0 || pos > len(text) {
		return false
	}
	// 往前跳过空白(最多 40 字节 —— 别跨过大段话去认一个不相干的词)。
	end := pos
	for end > 0 && pos-end < 40 && isSpaceByte(text[end-1]) {
		end--
	}
	// 再往前吃标识符字符。
	start := end
	for start > 0 && isWordByte(text[start-1]) {
		start--
	}
	word := text[start:end]
	if at := strings.Index(word, "@"); at > 0 {
		word = word[:at]
	}
	return vendorNames[strings.ToLower(word)]
}

// isSpaceByte 空白字符。
func isSpaceByte(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\v', '\f':
		return true
	}
	return false
}

// isWordByte 标识符里可能出现的字符。
//
// ⚠️ **不含 '/'** —— 含了的话会把前一个路径整段吃进来
// (「改 `web/src/x.ts` 后」→ 词变成 "web/src/x.ts")。
func isWordByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
	case c >= 'A' && c <= 'Z':
	case c >= '0' && c <= '9':
	case c == '-' || c == '_' || c == '.' || c == '@':
	default:
		return false
	}
	return true
}

// exemptByDeletion 这个路标附近有没有删除动词(且中间没有别的路标)。
//
// spans 传进来的必须是**这一行里的路标**(不是全部反引号段)——
// 见文件头"怎么判'这一处是历史陈述'"。
func exemptByDeletion(line string, spans []codeSpan, markers []markerSpan, sp codeSpan) bool {
	for _, m := range markers {
		switch {
		case m.start >= sp.end:
			// 动词在路标右边
			if runeGap(line, sp.end, m.start) > maxMarkerDistance {
				continue
			}
			if hasSpanBetween(spans, sp.end, m.start) {
				continue
			}
			return true
		case m.end <= sp.start:
			// 动词在路标左边
			if runeGap(line, m.end, sp.start) > maxMarkerDistance {
				continue
			}
			if hasSpanBetween(spans, m.end, sp.start) {
				continue
			}
			return true
		}
	}
	return false
}

// runeGap 两个字节下标之间隔了多少个**字符**。
//
// ⚠️ 文档是中文 —— 一个汉字 3 字节,用字节算距离会把
// 「……已于 M129 删除」这种正常的句子算成"离得太远"。
func runeGap(line string, a, b int) int {
	if a > b {
		a, b = b, a
	}
	if a < 0 {
		a = 0
	}
	if b > len(line) {
		b = len(line)
	}
	return utf8.RuneCountInString(line[a:b])
}

// hasSpanBetween (from, to) 之间有没有别的反引号段。
func hasSpanBetween(spans []codeSpan, from, to int) bool {
	for _, s := range spans {
		if s.end > from && s.start < to {
			return true
		}
	}
	return false
}
