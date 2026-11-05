package main

// AI 可读面 —— docs/ai-coding.md §5.6(themeing/ai-coding 规划 P2)
//
// honkit 只产 HTML,而 coding agent 拉文档要纯 markdown(llmstxt.org v2:
// "同 URL 提供 markdown 版")。这里在 honkit 之后追加两步:
//
//  1. copyMarkdownSources:把手册源 .md(SUMMARY.md + 全部章节)按原相对路径
//     拷进 site/ —— 线上 https://<host>/docs/<路径>.md 即纯文本版;
//  2. writeSiteLlmsTxt:按 SUMMARY.md 生成 site/llms.txt(H1 + blockquote +
//     H2 分组链接清单,链接指向站内相对路径的 .md 副本)。
//
// ⚠️ 只在 build 后追加,check 不用管:.md 副本与 llms.txt 的过期会被
// 既有指纹机制连带发现(改源 ⇒ 指纹变 ⇒ docs-check 红 ⇒ 重建时重新生成)。

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// copyMarkdownSources 把手册源 .md 拷进 site/(保留相对路径)。
// 跳过 site/ 自身、node_modules、_book 与非 .md 文件。
func copyMarkdownSources(root, site string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "site", "node_modules", "_book", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(site, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return copyFile(p, dst)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// SUMMARY 条目:* [标题](路径) — 说明(说明段可无;分隔符兼容 — 与 ——)
var (
	summaryH2    = regexp.MustCompile(`^##\s+(.+)$`)
	summaryEntry = regexp.MustCompile(`^\s*\*\s+\[([^\]]+)\]\(([^)]+)\)(?:\s*[—-]+\s*(.*))?$`)
)

// writeSiteLlmsTxt 按 SUMMARY.md 生成 site/llms.txt(llmstxt.org v2 格式)。
// 链接指向站内相对路径的 .md 副本(copyMarkdownSources 已放好)。
func writeSiteLlmsTxt(root, site string) error {
	data, err := os.ReadFile(filepath.Join(root, "SUMMARY.md"))
	if err != nil {
		return fmt.Errorf("读 SUMMARY.md: %w", err)
	}

	var b strings.Builder
	b.WriteString("# Rapid Admin 框架手册\n\n")
	b.WriteString("> 通用后台管理框架(Go + Hertz + GORM + Casbin)的框架手册 —— ")
	b.WriteString("全部框架模块的功能 / 配置 / 使用方法,每模块配一个可运行的示例。\n\n")
	b.WriteString("本文件是 LLM 索引;链接指向各章节的纯 Markdown 版本,与 HTML 版同源同步。\n\n")

	section := "章节"
	for _, line := range strings.Split(string(data), "\n") {
		if m := summaryH2.FindStringSubmatch(line); m != nil {
			section = strings.TrimSpace(m[1])
			fmt.Fprintf(&b, "## %s\n", section)
			continue
		}
		m := summaryEntry.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		title, path, desc := m[1], m[2], strings.TrimSpace(m[3])
		if desc != "" {
			fmt.Fprintf(&b, "- [%s](%s): %s\n", title, path, desc)
		} else {
			fmt.Fprintf(&b, "- [%s](%s)\n", title, path)
		}
	}

	return os.WriteFile(filepath.Join(site, "llms.txt"), []byte(b.String()), 0o644)
}
