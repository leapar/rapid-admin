// PublicPaths 与 router 的 pub 组必须逐条对齐。
//
// # 为什么这条约束值得用测试钉住
//
// PublicPaths 今天**不影响生产**:Auth() 只挂在受保护组(/rapid-admin/v1)上,pub 组的
// 路由根本走不到它。它是一份防御性清单 —— 防的是"哪天有人把 Auth() 改成全局
// 挂载",那时任何一条漏网的公开路由都会变成 401。
//
// 缺陷的形状非常恶劣:最典型的漏网路由是**登录接口**,后果不是"某个功能坏了",
// 而是"所有人登不进去、服务看起来全挂了",而排查方向会先怀疑刚改的鉴权代码。
//
// 2026-10-21 这份清单只有 6 条,实际公开路由有 9 条 —— 漏了 login-sms、
// reset-password-sms、public/config。它当时不影响生产,但它是一份**骗人的清单**:
// 读的人以为"列在这儿就是全部公开路由",于是改 router 时不会想到要同步。
//
// 所以这条测试从 router.go 里把 pub 组实际注册的路径抠出来,和 PublicPaths
// 逐条比对。改一边忘了改另一边,这里立刻红。
package middleware

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routerSource 读 router.go 源码(与本包不同目录,按 ../router/router.go 定位)
func routerSource(t *testing.T) string {
	t.Helper()
	f, err := os.Open("../router/router.go")
	require.NoError(t, err, "打不开 router.go —— 本测试靠解析它的源码工作")
	defer func() { require.NoError(t, f.Close()) }()
	b, err := io.ReadAll(f)
	require.NoError(t, err)
	return string(b)
}

// pubRouteRe 匹配 router.go 里 `pub.POST("/auth/login", ...)` / `pub.GET(...)` 这种注册。
//
// 只认 pub 组:受保护组用的是另一个变量名(api),并且挂在 Auth() 上,不该出现在这里。
var pubRouteRe = regexp.MustCompile(`pub\.(?:GET|POST|PUT|DELETE)\("([^"]+)"`)

// registeredPublicRoutes 从 router.go 抠出 pub 组的全部路由(含方法)
func registeredPublicRoutes(t *testing.T) []string {
	t.Helper()
	src := routerSource(t)

	pubGroup := strings.Index(src, `h.Group("/rapid-admin/v1/public"`)
	require.Positive(t, pubGroup, "router.go 里找不到 pub 组 —— 路由结构变了,本测试要跟着改")

	// pub 组是 `{ ... }` 块,从它后面第一个独立的 `{` 到配对的 `}`
	open := strings.Index(src[pubGroup:], "\n\t{")
	require.Positive(t, open, "找不到 pub 组的代码块起点")
	open += pubGroup

	depth := 0
	end := -1
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end > 0 {
			break
		}
	}
	require.Positive(t, end, "pub 组的代码块没有闭合 —— 解析失败")
	body := src[open:end]

	out := make([]string, 0)
	for _, m := range pubRouteRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	require.NotEmpty(t, out, "从 pub 组里一条路由都没解析出来 —— 正则失效了")
	return out
}

// TestAuth_PublicPathsCoverEveryRegisteredPublicRoute 本文件唯一的用例。
//
// 双向比对:
//   - router 里注册了但 PublicPaths 漏了 → 将来 Auth() 一全局挂载就 401
//   - PublicPaths 列了但 router 里没有 → 清单已经腐烂(路由被删/改名),读的人会被误导
func TestAuth_PublicPathsCoverEveryRegisteredPublicRoute(t *testing.T) {
	registered := registeredPublicRoutes(t)

	// router 上的路由以 pub 组的完整前缀书写,PublicPaths 也用完整路径
	const prefix = "/rapid-admin/v1/public"

	listed := make(map[string]bool, len(PublicPaths))
	for _, p := range PublicPaths {
		listed[p] = true
	}

	var missing []string
	for _, r := range registered {
		full := prefix + r
		if !listed[full] {
			missing = append(missing, full)
		}
	}
	assert.Empty(t, missing,
		"router 的 pub 组注册了这些公开路由,但 PublicPaths 没列 —— "+
			"将来 Auth() 一旦全局挂载(或挂到 pub 组上),这些接口会直接 401。"+
			"最典型的受害者是登录接口,现场表现是「所有人都登不进去」")
}

// TestAuth_PublicPathsHaveNoStaleEntries 反向:清单里不许有 router 上不存在的路径。
//
// 一条失效的公开路径比一条缺失的更隐蔽:它让读的人高估这份清单的可信度,
// 于是改路由时不再同步 —— 于是回到上一条那种"少了一半"的局面。
func TestAuth_PublicPathsHaveNoStaleEntries(t *testing.T) {
	registered := make(map[string]bool)
	for _, r := range registeredPublicRoutes(t) {
		registered["/rapid-admin/v1/public"+r] = true
	}
	// 非 /public 的健康检查类路径由 router 根挂载,不在 pub 组里,单独认可
	alwaysPublic := map[string]bool{"/healthz": true, "/readyz": true}

	var stale []string
	for _, p := range PublicPaths {
		if !registered[p] && !alwaysPublic[p] {
			stale = append(stale, p)
		}
	}
	assert.Empty(t, stale,
		"PublicPaths 列了这些路径,但 router 里没有注册它们 —— 清单已经腐烂。"+
			"要么路由被删/改名忘了同步,要么这里一开始就没对过")
}
