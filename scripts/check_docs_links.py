#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""GitBook(server/docs)链接与引用一致性检查。

手册是给人读的,最容易腐化的地方是"文档里写的路径跟仓库里实际的对不上"。
本脚本把能机械判定的那部分钉死:

1. SUMMARY.md 列出的每个页面都存在;
2. server/docs 下每个 .md(除 SUMMARY.md)都被 SUMMARY.md 收录 —— 漏写进目录 = 孤儿页;
3. 章节里所有相对链接(同目录/上级目录的 .md)都能解析到真实文件;
4. 章节里提到的 internal/pkg/<包> 目录真实存在;
5. 章节里提到的 examples/<示例> 目录真实存在。

用法:
    python scripts/check_docs_links.py
退出码 0 = 全部通过,1 = 有问题(逐条打印)。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "server" / "docs"
PKG_DIR = ROOT / "server" / "internal" / "pkg"
EXAMPLES_DIR = ROOT / "server" / "examples"

# SUMMARY.md 里的一级条目形如 "* [标题](路径.md) —— 一句话说明"
SUMMARY_LINK = re.compile(r"^\s*\*\s+\[[^\]]*\]\(([^)]+)\)", re.MULTILINE)
# 章节正文里的 markdown 链接
MD_LINK = re.compile(r"\[[^\]]*\]\(([^)\s]+)\)")
# 反引号里的包路径
PKG_REF = re.compile(r"`internal/pkg/([a-z0-9_]+)`")
EX_REF = re.compile(r"`examples/([a-z0-9_]+)`")


SKIP_DIRS = {"node_modules", "_book"}


def collect_md() -> set[Path]:
    out: set[Path] = set()
    for p in DOCS.rglob("*.md"):
        if p.name == "SUMMARY.md":
            continue
        if SKIP_DIRS & set(p.relative_to(DOCS).parts):
            continue
        out.add(p)
    return out


def rel(p: Path) -> str:
    return p.relative_to(DOCS).as_posix()


def main() -> int:
    problems: list[str] = []

    summary = DOCS / "SUMMARY.md"
    if not summary.exists():
        print("❌ 找不到 server/docs/SUMMARY.md")
        return 1

    summary_links = [m.group(1) for m in SUMMARY_LINK.finditer(summary.read_text(encoding="utf-8"))]

    # 1. SUMMARY 里列的页面必须存在
    for link in summary_links:
        if link.startswith(("http://", "https://")):
            continue
        target = (DOCS / link).resolve()
        if not target.exists():
            problems.append(f"SUMMARY.md 指向不存在的页面: {link}")

    # 2. 每个 .md 都必须被 SUMMARY 收录
    listed = {(DOCS / l).resolve() for l in summary_links if not l.startswith(("http://", "https://"))}
    for md in sorted(collect_md()):
        if md.resolve() not in listed:
            problems.append(f"孤儿页面(未被 SUMMARY.md 收录): {rel(md)}")

    # 3~5. 逐章检查链接与引用
    for md in sorted(collect_md()):
        text = md.read_text(encoding="utf-8")
        for link in MD_LINK.findall(text):
            if link.startswith(("http://", "https://", "#", "mailto:")):
                continue
            target = (md.parent / link.split("#")[0]).resolve()
            if not target.exists():
                problems.append(f"{rel(md)} 里的链接指向不存在的文件: {link}")
        for pkg in PKG_REF.findall(text):
            if not (PKG_DIR / pkg).is_dir():
                problems.append(f"{rel(md)} 引用了不存在的包目录: internal/pkg/{pkg}")
        for ex in EX_REF.findall(text):
            if not (EXAMPLES_DIR / ex).is_dir():
                problems.append(f"{rel(md)} 引用了不存在的示例目录: examples/{ex}")

    if problems:
        print(f"❌ 手册一致性检查发现 {len(problems)} 个问题:")
        for p in problems:
            print(f"  - {p}")
        return 1

    n_pages = len(collect_md())
    print(f"✅ 手册自洽:{n_pages} 个页面,{len(summary_links)} 条目录项,链接与包引用全部可解析")
    return 0


if __name__ == "__main__":
    sys.exit(main())
