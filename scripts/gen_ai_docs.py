#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""AI 接入文档生成器 —— docs/ai-coding.md §5.3 / §5.5(P1)。

从**机器可核对的数据源**渲染两份"给 AI 看的索引",原则是永不手写:

  数据源                                → 产物
  server/internal/pkg × SUMMARY × examples →  docs/ai/module-list.md(51 模块速查)
  server/docs/SUMMARY.md                   →  llms.txt(仓库根,llmstxt.org v2)

module-list 的三边对齐:SUMMARY 条目的 .md 名要能对上 internal/pkg/ 下的
真实目录,否则这条**不渲染**(而不是渲染一条死链)—— 与 check_docs_links
"文档不撒谎"同一哲学;对不上的模块说明手册缺章,由 check_docs_links 的
孤儿/引用检查另案报红。

用法:
    python scripts/gen_ai_docs.py           # 生成(仅在有变化时写盘)
    python scripts/gen_ai_docs.py --check   # 产物过期 ⇒ 退出码 1(pre-commit 用)
"""

from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "server" / "docs"
PKG_DIR = ROOT / "server" / "internal" / "pkg"
EXAMPLES_DIR = ROOT / "server" / "examples"
OUT_MODULE_LIST = ROOT / "docs" / "ai" / "module-list.md"
OUT_LLMS = ROOT / "llms.txt"

RAW_BASE = "https://raw.githubusercontent.com/leapar/rapid-admin/main/"

# SUMMARY 条目:* [标题](路径.md) — 说明(兼容 — 与 ——)
SUMMARY_ENTRY = None  # 编译正则放 main 里(见 _compile_patterns)


def _compile_patterns():
    global SUMMARY_ENTRY, SUMMARY_H2
    import re

    SUMMARY_ENTRY = re.compile(
        r"^\s*\*\s+\[([^\]]+)\]\(([^)]+)\)(?:\s*[—-]+\s*(.*))?$"
    )
    SUMMARY_H2 = re.compile(r"^##\s+(.+)$")


def read_summary():
    """[(h2 组名, 标题, 路径, 说明), ...]"""
    text = (DOCS / "SUMMARY.md").read_text(encoding="utf-8")
    out, group = [], ""
    for line in text.splitlines():
        m = SUMMARY_H2.match(line)
        if m:
            group = m.group(1).strip()
            continue
        m = SUMMARY_ENTRY.match(line)
        if m:
            out.append((group, m.group(1).strip(), m.group(2).strip(),
                        (m.group(3) or "").strip()))
    return out


def gen_module_list() -> str:
    """51 个框架模块速查:SUMMARY 条目 × internal/pkg 目录 × examples 三边对齐。"""
    rows = []
    for group, title, path, desc in read_summary():
        stem = Path(path).stem
        # 模块条目判定:.md 文件名 === internal/pkg/ 下的目录名
        # (web/theming.md、api-docs/swaggerx.md 等非模块页自然被排除)
        if not (PKG_DIR / stem).is_dir():
            continue
        has_example = (EXAMPLES_DIR / stem / "main.go").is_file()
        example = f"`go run ./examples/{stem}`" if has_example else "—"
        rows.append((group, stem, desc, path, example))

    lines = [
        "<!-- 本文件由 scripts/gen_ai_docs.py 生成 —— 手改会被覆盖;数据源见脚本文件头 -->",
        "",
        "# module-list —— 框架模块速查",
        "",
        f"> 共 {len(rows)} 个框架模块(`server/internal/pkg/`),每个模块都有可运行示例"
        "与手册章节。数据源:SUMMARY.md × internal/pkg × examples 三边对齐。",
        "",
        "| 组 | 模块 | 一句话 | 手册 | 示例 |",
        "|---|---|---|---|---|",
    ]
    for group, stem, desc, path, example in rows:
        rel = f"../../server/docs/{path}"
        lines.append(f"| {group} | {stem} | {desc} | [手册]({rel}) | {example} |")
    lines.append("")
    lines.append("跑某模块示例:`cd server && go run ./examples/<模块名>`;"
                 "新增模块的流程见 [module-add.md](module-add.md)。")
    return "\n".join(lines) + "\n"


def gen_llms() -> str:
    """仓库根 llms.txt(llmstxt.org v2):H1 + blockquote + H2 分组链接 + Optional。"""
    lines = [
        "# Rapid Admin",
        "",
        "> 开箱即用的 Go + React 通用后台管理框架:RBAC 权限、数据范围、字段级加密脱敏、"
        "双级缓存、审计、通知、定时任务;51 个框架模块各配手册章节与 `go run` 即跑的示例。"
        "技术栈:Go 1.22+(Hertz + GORM + Casbin)· React 18 + Ant Design 5。",
        "",
        "改本仓库代码的 AI agent 先读 [AGENTS.md](AGENTS.md)(构建/测试命令与硬约束);"
        "拿框架开发自己业务的 agent 从下面的手册章节按需取。",
        "",
    ]
    current_group = None
    for group, title, path, desc in read_summary():
        # 首页在 SUMMARY 第一个 H2 之前 —— 归入"开始"组,别让它单开一节
        if group == "":
            group = "开始"
        if group != current_group:
            current_group = group
            lines.append(f"## 手册 · {group}")
            lines.append("")
        url = f"{RAW_BASE}server/docs/{path}"
        if desc:
            lines.append(f"- [{title}]({url}): {desc}")
        else:
            lines.append(f"- [{title}]({url})")
        lines.append("")

    lines += [
        "## 仓库文档(给 AI 的入口)",
        "",
        f"- [AGENTS.md]({RAW_BASE}AGENTS.md): 改仓库代码必读 —— 常用命令 + 硬约束",
        f"- [install.md]({RAW_BASE}docs/ai/install.md): 从零跑起来并打第一个接口的可执行剧本",
        f"- [module-add.md]({RAW_BASE}docs/ai/module-add.md): 新增一个框架模块的七步配方",
        f"- [module-list.md]({RAW_BASE}docs/ai/module-list.md): 51 个框架模块速查表",
        f"- [错误码全表]({RAW_BASE}docs/reference/error-codes.md): 业务错误码与语义",
        "",
        "## Optional",
        "",
        f"- [设计文档目录]({RAW_BASE}docs/design/): SRS 与设计稿",
        f"- [ROADMAP]({RAW_BASE}docs/ROADMAP.md): 里程碑与状态",
        f"- [AI 接入规划]({RAW_BASE}docs/ai-coding.md): 本文件的生成依据与后续计划",
        f"- [UI 皮肤规划]({RAW_BASE}docs/theming.md): 换肤架构与皮肤编辑器规划",
        "",
    ]
    return "\n".join(lines)


def write_if_changed(path: Path, content: str) -> bool:
    if path.exists() and path.read_text(encoding="utf-8") == content:
        return False
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8", newline="\n")
    return True


def main() -> int:
    _compile_patterns()
    check = "--check" in sys.argv
    changed = []
    for path, gen in ((OUT_MODULE_LIST, gen_module_list), (OUT_LLMS, gen_llms)):
        content = gen()
        if check:
            current = path.read_text(encoding="utf-8") if path.exists() else ""
            if current != content:
                print(f"❌ {path.relative_to(ROOT)} 过期 —— 跑 python scripts/gen_ai_docs.py 重新生成")
                changed.append(path)
        else:
            if write_if_changed(path, content):
                changed.append(path)
                print(f"==> 已生成 {path.relative_to(ROOT)}")
    if check and changed:
        return 1
    if not check and not changed:
        print("==> 产物已是最新,无变化")
    return 0


if __name__ == "__main__":
    sys.exit(main())
