// Command docs 构建「通用后台管理框架手册」,并守住产物与源文件的同步。
//
// # 用法
//
//	go run ./cmd/docs build    # 源变了才重建(默认子命令)
//	go run ./cmd/docs force    # 强制重建
//	go run ./cmd/docs check    # 只检查:产物落后于源 ⇒ 退出码 1
//
// # 它在整条链上的位置
//
//	server/docs/*.md  ──honkit──▶  server/docs/site/  ──go:embed──▶  二进制  ──▶  GET /docs
//
// 构建这一步需要 Node.js(npm ci 装 honkit 与插件)。
// ⚠️ **但只有"源真的变了"才需要 node** —— 指纹没变就直接跳过,
// 所以在没有 node 的机器上 `make build` 依然能过(仓库里已经提交了产物)。
//
// # 为什么产物要提交进仓库
//
// 二进制里嵌的就是产物 ⇒ "改了手册没重建" = "线上手册是旧的"。
// 仓库里存着产物,至少 clone 下来的人拿到的就是对的;
// 同步由 server/docs 的单测盯着(源指纹 vs 产物里的 buildinfo.json)。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"rapid-admin/internal/docsbuild"
)

func main() {
	cmd := "build"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	root, err := locateRoot()
	if err != nil {
		fail(err)
	}

	switch cmd {
	case "build":
		err = build(root, false)
	case "force":
		err = build(root, true)
	case "check":
		err = check(root)
	default:
		err = fmt.Errorf("未知子命令 %q(可用:build / force / check)", cmd)
	}
	if err != nil {
		fail(err)
	}
}

// locateRoot 找到手册源目录(那一层有 book.json)。
//
// ⚠️ **不能只靠 cwd** —— 从仓库根跑 `go run ./server/cmd/docs` 时,
// cwd 下的 "docs" 是仓库根那个放 SRS 的 docs/，不是手册。
// 所以候选里同时放 cwd/docs、cwd/server/docs,以及**源码所在位置**推出来的
// (cmd/docs/main.go 往上两级 = server/,再拼 docs/)。
func locateRoot() (string, error) {
	var candidates []string
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "docs"),
			filepath.Join(cwd, "server", "docs"),
		)
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "..", "..", "docs"))
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "book.json")); err == nil {
			return abs, nil
		}
	}
	return "", fmt.Errorf("找不到手册源目录(要能找到 book.json);试过的位置: %s",
		strings.Join(candidates, " | "))
}

// build 构建手册。force=true 时无视指纹强制重建。
func build(root string, force bool) error {
	stale, bi, err := docsbuild.IsStale(root)
	if err != nil {
		return err
	}
	if !stale && !force {
		fmt.Println("==> 手册产物已是最新,跳过构建(要强制重建:go run ./cmd/docs force)")
		return nil
	}
	if bi.SourceHash != "" {
		fmt.Printf("==> 手册产物落后于源文件(产物指纹 %s)\n", short(bi.SourceHash))
	}

	if err := ensureNodeModules(root); err != nil {
		return err
	}

	// honkit 不会自己清输出目录 —— 旧的页面会留在那儿(改名过的章节会变成幽灵链接)。
	site := filepath.Join(root, "site")
	if err := os.RemoveAll(site); err != nil {
		return fmt.Errorf("清理旧产物失败: %w", err)
	}

	if err := run(root, "npm", "run", "build"); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(site, "index.html")); err != nil {
		return fmt.Errorf("构建结束但没找到 site/index.html —— 构建没成功")
	}

	// AI 可读面(纯 markdown 版手册 + llms.txt 索引)—— ai-coding.md §5.6
	if err := copyMarkdownSources(root, site); err != nil {
		return fmt.Errorf("拷贝手册 markdown 源失败: %w", err)
	}
	if err := writeSiteLlmsTxt(root, site); err != nil {
		return fmt.Errorf("生成 site/llms.txt 失败: %w", err)
	}

	// ⚠️ 指纹在构建**之后**算 —— site/ 不参与指纹,两者等价;
	// 放在后面是为了让"构建过程改了源"这种情况也能被如实记下来。
	hash, err := docsbuild.SourceHash(root)
	if err != nil {
		return err
	}
	pages, files, err := docsbuild.CountSite(root)
	if err != nil {
		return err
	}
	bi = docsbuild.BuildInfo{
		SourceHash: hash,
		BuiltAt:    time.Now().Format(time.RFC3339),
		Pages:      pages,
		Files:      files,
		Tool:       toolVersion(root),
	}
	if err := docsbuild.WriteBuildInfo(root, bi); err != nil {
		return err
	}
	fmt.Printf("==> 手册已构建:%d 页 / %d 文件(%s,指纹 %s)\n",
		pages, files, bi.Tool, short(hash))
	return nil
}

// check 只检查产物是否与源同步;落后则返回错误(退出码 1)。
func check(root string) error {
	stale, bi, err := docsbuild.IsStale(root)
	if err != nil {
		return err
	}
	if stale {
		return fmt.Errorf("手册产物落后于源文件(产物指纹 %s)—— 跑 `make docs` 重新构建并提交 site/",
			short(bi.SourceHash))
	}
	fmt.Println("==> 手册产物是最新的")
	return nil
}

// ensureNodeModules 保证 honkit 装好了。
func ensureNodeModules(root string) error {
	if _, err := exec.LookPath("npm"); err != nil {
		return fmt.Errorf("找不到 npm —— 构建手册需要 Node.js(装完再跑一次)")
	}
	honkit := filepath.Join(root, "node_modules", "honkit", "package.json")
	if _, err := os.Stat(honkit); err == nil {
		return nil
	}
	fmt.Println("==> 安装手册构建依赖")
	// 有 lock 文件就用 npm ci(可复现),没有就退回 npm install。
	if _, err := os.Stat(filepath.Join(root, "package-lock.json")); err == nil {
		return run(root, "npm", "ci")
	}
	return run(root, "npm", "install")
}

// toolVersion 读已安装的 honkit 版本(写进 buildinfo,便于追"这份产物是谁构建的")。
func toolVersion(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "node_modules", "honkit", "package.json"))
	if err != nil {
		return "honkit"
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || pkg.Version == "" {
		return "honkit"
	}
	return "honkit " + pkg.Version
}

// run 在指定目录跑一条命令,输出直接透传。
func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s 执行失败: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// short 指纹取前 12 位,日志里够用。
func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

// fail 报错并退出(退出码 1)。
func fail(err error) {
	fmt.Fprintln(os.Stderr, "✗ "+err.Error())
	os.Exit(1)
}
