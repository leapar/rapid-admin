// buildinfo_test.go —— 指纹算法的反向探针(2026-11-01)
//
// ## 这条网守的是什么
//
// server/docs/docs_test.go 有一条"产物不落后于源"的断言。
// 但那条断言**依赖指纹是对的** —— 一个恒返同一个值的 SourceHash,
// IsStale 永远说"没过期",那条网就是假绿。
//
// ⇒ 这里守的是指纹本身:"该变的时候必须变,不该变的时候必须不变"。
package docsbuild

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile 在临时目录里造一个文件。
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

// TestSourceHash_该变的时候必须变
func TestSourceHash_该变的时候必须变(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "a.md", "one")
	h1, err := SourceHash(dir)
	require.NoError(t, err)

	// 改内容
	writeFile(t, dir, "a.md", "two")
	h2, err := SourceHash(dir)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2, "内容变了指纹却没变 —— 同步网抓不到漂移")

	// 加文件
	writeFile(t, dir, "b.md", "new")
	h3, err := SourceHash(dir)
	require.NoError(t, err)
	assert.NotEqual(t, h2, h3, "加了文件指纹却没变")

	// 改文件名(路径参与指纹)
	require.NoError(t, os.Rename(filepath.Join(dir, "b.md"), filepath.Join(dir, "c.md")))
	h4, err := SourceHash(dir)
	require.NoError(t, err)
	assert.NotEqual(t, h3, h4, "改了文件名指纹却没变")
}

// TestSourceHash_不该变的时候必须不变
//
// ⚠️ 这条守的是"别把噪声算进去" —— 构建产物、依赖目录、本包自己的 .go 源码
// 都**不该**影响指纹。算进去的后果是"每次提交都判定过期 ⇒ 永远在重建",
// 那等于没有这个检查(而且会让人养成"红就红吧"的习惯)。
func TestSourceHash_不该变的时候必须不变(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "one")
	writeFile(t, dir, "book.json", `{"title":"x"}`)

	before, err := SourceHash(dir)
	require.NoError(t, err)

	// 构建产物
	writeFile(t, dir, "site/index.html", "<html></html>")
	writeFile(t, dir, "site/gitbook/style.css", "body{}")
	// 依赖目录
	writeFile(t, dir, "node_modules/x/index.js", "1")
	// Go 源码(手册源目录同时是一个 Go 包)
	writeFile(t, dir, "docs.go", "package docs")

	after, err := SourceHash(dir)
	require.NoError(t, err)
	assert.Equal(t, before, after, "构建产物 / node_modules / .go 不该参与指纹")
}

// TestSourceHash_行尾归一 —— CRLF 与 LF 必须是同一个指纹。
//
// ⚠️ **这条守的是"跨平台"** —— 本仓 git 配了 core.autocrlf=true(Windows 默认),
// 同一个源文件在 Windows 工作区是 CRLF、在仓库里是 LF、在 Linux 工作区是 LF。
// 不归一的话,提交在仓库里的产物指纹谁都匹配不上 ⇒ 漂移网在每台机器上都是红的。
//
// (实测踩过:探针把 getting-started/README.md 改坏后 git checkout 复原,
// 复原的是 **CRLF 版** —— 指纹和构建时记录的 LF 版对不上,漂移网红着不放。)
func TestSourceHash_行尾归一(t *testing.T) {
	lf := t.TempDir()
	writeFile(t, lf, "a.md", "line1\nline2\n")
	hLF, err := SourceHash(lf)
	require.NoError(t, err)

	crlf := t.TempDir()
	writeFile(t, crlf, "a.md", "line1\r\nline2\r\n")
	hCRLF, err := SourceHash(crlf)
	require.NoError(t, err)

	assert.Equal(t, hLF, hCRLF, "CRLF 与 LF 必须算出同一个指纹")
}

// TestSourceHash_稳定 —— 同样的输入必须给同样的指纹。
func TestSourceHash_稳定(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.md", "one")
	writeFile(t, dir, "sub/b.md", "two")

	h1, err := SourceHash(dir)
	require.NoError(t, err)
	h2, err := SourceHash(dir)
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "同样的输入给出了两个指纹")
}

// TestIsStale_没构建过也算过期 —— 而且不报错。
//
// ⚠️ "没构建过"是**正常状态**(第一次 clone 之后还没构建),
// 不是故障 —— 所以不能返回错误,只能返回"过期"。
func TestIsStale_没构建过也算过期(t *testing.T) {
	dir := t.TempDir()
	stale, _, err := IsStale(dir)
	require.NoError(t, err, "没构建过不该是错误")
	assert.True(t, stale, "没构建过却说不 stale")
}
