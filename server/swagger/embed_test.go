// 内嵌的 swagger-ui 资产(2026-11-01 开源通用化 · §8.1 G4)
//
// ## 这条网守的是什么
//
// swagger-ui 的静态资产以前来自 Go module —— **页面长什么样不由本仓库决定**。
// 放进仓库之后,这里守住三件事:
//
//  1. 该有的文件都在(少一个 index.html 页面就白屏)
//  2. **没有任何外链** —— 内网/离线部署时外链请求会挂住,页面渲染被拖到超时
//     (上游 5.10.5 本身没有外链;这条是留给以后改 index.html 的人的网)
//  3. **没有 .map 文件** —— 4.6MB 的 source map 对调试本仓库毫无用处,
//     却让仓库体积翻倍、每次升级都要 review 一堆不可读的 JSON
//
// 顺带守住"我们自己写的两个文件没被上游覆盖"——
// index.html / swagger-initializer.js 是本仓库改写的(见 ui/README.md)。
package swagger

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssets_该有的文件都在(t *testing.T) {
	assets := Assets()
	required := []string{
		"index.html",
		"swagger-initializer.js",
		"swagger-ui.css",
		"swagger-ui.js",
		"swagger-ui-bundle.js",
		"swagger-ui-es-bundle.js",
		"swagger-ui-es-bundle-core.js",
		"swagger-ui-standalone-preset.js",
		"index.css",
		"oauth2-redirect.html",
		"favicon-16x16.png",
		"favicon-32x32.png",
		// ⚠️ 第三方二进制必须带自己的许可证 —— 开源分发时这是硬要求
		"LICENSE",
	}
	for _, name := range required {
		_, err := fs.Stat(assets, name)
		assert.NoError(t, err, "资产 %s 不在 —— 页面会缺东西", name)
	}
}

func TestAssets_没有sourceMap(t *testing.T) {
	assets := Assets()
	entries, err := fs.ReadDir(assets, ".")
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".map", "不该收录 %s —— source map 对本仓库没用,却让体积翻倍", e.Name())
	}
}

func TestAssets_indexHTML没有外链(t *testing.T) {
	assets := Assets()
	data, err := fs.ReadFile(assets, "index.html")
	require.NoError(t, err)
	html := string(data)

	// ⚠️ 只查 <link href> / <script src> 里的 http(s):// ——
	// 外链会拖住离线部署下的页面渲染(症状是"转圈半天不出来")。
	for _, line := range strings.Split(html, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "<link") && !strings.HasPrefix(trimmed, "<script") {
			continue
		}
		assert.NotContains(t, trimmed, "http://", "index.html 里有外链:%s", trimmed)
		assert.NotContains(t, trimmed, "https://", "index.html 里有外链:%s", trimmed)
	}
}

func TestAssets_initializer指向docJSON(t *testing.T) {
	assets := Assets()
	data, err := fs.ReadFile(assets, "swagger-initializer.js")
	require.NoError(t, err)
	init := string(data)

	// spec 由本服务实时生成 —— 相对路径,挂载点改了不用动这个文件
	assert.Contains(t, init, "./doc.json")
	// 上游默认指 petstore,那会让 UI 拉一份跟本服务无关的 spec
	assert.NotContains(t, init, "petstore")
}
