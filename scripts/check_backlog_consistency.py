#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""框架层未完成项清单 —— 一致性守门网

## 为什么要这个

`docs/ROADMAP.md`(原 框架层未完成项清单v1.md)曾经**大面积说谎**:14 个「❌ 未完成(N 项)」块里,
有 9 个的每一行都已经打了 ✅ M-xx(早就做完了),另有一堆计数跟实际行数对不上。
读这份清单排期的人会被直接带偏 —— 而这正是本仓纪律里反复强调要防的事
(见 memory/可逆探针纪律、docs 里的"读规划不要跳过本节")。

**光靠人记得改,是守不住的** —— 所以给它加一张网:
清单里每一个计数都必须和它下面的表格对得上,否则这条命令非零退出。

## 计数模型(⚠️ 改这个脚本前先读完这一段)

清单里每一条子项都落在一张表里,而表按**块**分:

| 块标题 | 行怎么算 | 标题里的数怎么算 |
|---|---|---|
| `#### ✅ 已完成(...)` | 整块算**已完成** | `已完成(N 个)` → N = 块里的**行数**;`已完成(d/t)` → d = 块里的**行数**、t = 本节**总子项数** |
| `#### ❌ 未完成(N 项)` | 带 ✅ 的行 = 已完成;没带的 = **还开着** | N = 块里**还开着**的行数 |
| 其它标记(`⚠️` / `🔧` / `📋`) | **不计数** —— 那是叙述表 / 范围说明 / 证据表 | 不检查 |

由此:

- 本节 **已完成** = ✅ 块行数 + ❌ 块里带 ✅ 的行数
- 本节 **还开着** = ❌ 块里没带 ✅ 的行数
- 本节 **总子项** = 已完成 + 还开着
- 小节标题上的**百分比** = round(已完成 / 总子项 × 100)

⚠️ **为什么"带 ✅ 的行算已完成"** —— 因为 M-xx 模块做完之后,清单里那一行
   只是加了个 ✅ 标记,**没从 ❌ 块搬到 ✅ 块**。这条规则让"这一行到底还开不开着"
   有唯一的判据(行上有没有 ✅),而不是靠它落在哪个块里。

⚠️ **为什么"其它标记不计数"** —— `#### ⚠️` / `#### 🔧` / `#### 📋` 这几种块
   装的是**范围说明**、**修复记录**、**证据表**,行数跟"子项数"不是一回事
   (比如 §2.10 的 API TODO 表是 6 行,但代表 23 个 endpoint)。
   把它们算进分母,百分比立刻变成噪音。

## 守什么

1. `#### ✅ 已完成(N 个)` —— N 必须等于该块的行数
2. `#### ✅ 已完成(d/t)` —— d 必须等于该块的行数,t 必须等于本节总子项数
3. `#### ❌ 未完成(N 项)` —— N 必须等于该块里**没有 ✅ 标记**的行数
   (这一条最容易骗人:标题说 5 项、表里 5 行、5 行全是 ✅)
4. `## N.M ...(P%)` —— P 必须等于 round(已完成 / 总子项 × 100)
5. **百分比自引用** —— 表里每一个百分比格都必须写成 `**92%** (§1.2)`,
   意思是"这个数字来自 §1.2"。守门网会去 §1.2 把它算一遍,对不上就红。
   ⚠️ 为什么必须这么写:§1 / §2 两张总览表和 §6 的"当前完整度"列
   **每一格都是从别处抄来的数** —— 抄的那一格改了,抄来的忘了改,
   读的人就会看见两个不一样的数(以前 JWT 就是 §1.2 写 92%、§6 写 90%)。
   **自引用把"抄来的"变成"指着的"**,抄错立刻暴露。

## 不守什么

- 不检查 ✅ 行里的证据链接是否还有效(那是人工核验的事,附录 B 的规则管)
- 不检查 P0/P1/P2 的优先级判断对不对
- **不检查"该不该做"** —— 只检查"说自己做完了的,是不是真的做完了"

## 用法

    python scripts/check_backlog_consistency.py           # 检查
    python scripts/check_backlog_consistency.py --quiet   # 只报退出码

退出码 0 = 一致;非 0 = 有对不上的地方(逐条打印行号)。
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DOC = os.path.join(ROOT, "docs", "ROADMAP.md")

# 行首第一格是子项编号 —— 纯数字(#7)或数字+字母(#7a / #8a / #15a)。
# ⚠️ 只认 `(\d+)` 会把 8a / 15a / 7a 漏掉 —— §1.4 少算 1 行、§1.8 少算 1 行,
#   §1.3 少算 1 行,而少算会让"实际还开着"和"已完成"两边都偏。
ROW_RE = re.compile(r"^\|\s*(\d+[A-Za-z]?)\s*\|")
# 任意 `##` 都是小节边界 —— 哪怕标题里没有百分比(§3 / §4 / 附录 都没有)。
# ⚠️ 以前只认带 (NN%) 的,结果 §3 之后的表格全串进了 §2.10,统计全歪。
# 括号里除了百分比还能有别的字(「(91%,10/11 完成)」),所以 `[^)]*` 兜住尾巴。
SEC_RE = re.compile(r"^##\s+(?:(\d+(?:\.\d+)*)\s+)?(.*?)\s*(?:\((\d+)%\s*([^)]*)\))?\s*$")
# `#### ✅ 已完成(...)` / `#### ❌ 未完成(...)` / `#### ⚠️ ...` / `#### 🔧 ...`
SUB_RE = re.compile(r"^####\s+(\S+)\s*(.*?)\s*$")
# 「❌ 未完成(7 个待补,P0 = 0 个)」「❌ 未完成(2 项)」「❌ 未完成(3 项,P0 = 1 个)」
OPEN_HEAD_RE = re.compile(r"^未完成(?:\((\d+)\s*(?:项|个)[^)]*\))?")
# 「✅ 已完成(14 个,文件证据)」「✅ 已完成(9/10)」
DONE_COUNT_RE = re.compile(r"\((\d+)\s*个")
DONE_FRAC_RE = re.compile(r"\((\d+)\s*/\s*(\d+)\)")

# ── 百分比自引用 ────────────────────────────────────────────────
# 一格**整个就是**百分比(带不带加粗都算)→ 这格必须自引用。
# ⚠️ 只认「整格是百分比」是有意的:正文里写「±20% 抖动」那种带修饰词的
#   不是完成度,不该被要求写小节引用。
PCT_CELL_RE = re.compile(r"^(?:\*\*)?(\d+)%(?:\*\*)?$")
# 加粗百分比后面还跟着别的东西(例如「**100%** ✅M7」)—— 也算百分比格,
# 同样必须自引用(否则「✅M7」会把自引用挤掉,悄悄失去绑定)。
PCT_HEAD_RE = re.compile(r"^\*\*(\d+)%\*\*")
# 自引用格式:**92%** (§1.2) / **92%** (§2.1-§2.10 合计)
REF_CELL_RE = re.compile(
    r"^(?:\*\*)?(\d+)%(?:\*\*)?\s*\(§\s*([\d.]+)(?:\s*-\s*§?\s*([\d.]+))?[^)]*\)$")


class Block:
    """一个 `####` 标题 + 它下面的表格行。"""

    def __init__(self, line_no, marker, title):
        self.line_no = line_no
        self.marker = marker        # ✅ / ❌ / ⚠️ / 🔧 / 📋 …
        self.title = title
        self.rows = []              # 表格数据行(已去掉表头 / 分隔行)


def parse(path):
    with open(path, encoding="utf-8") as f:
        lines = f.read().split("\n")

    sections = []   # {no, title, pct, pct_line, blocks}
    cur_sec = None
    cur_block = None

    for i, line in enumerate(lines):
        if line.startswith("#"):
            m = SEC_RE.match(line)
            if m:
                pct = m.group(3)
                cur_sec = {
                    "no": m.group(1),
                    "title": m.group(2),
                    "pct": int(pct) if pct is not None else None,
                    "pct_line": i + 1,
                    "blocks": [],
                }
                sections.append(cur_sec)
                cur_block = None      # `##` 也要断开,否则上一节的表会串到下一节
                continue
            m = SUB_RE.match(line)
            if m:
                cur_block = Block(i + 1, m.group(1), m.group(2))
                if cur_sec is not None:
                    cur_sec["blocks"].append(cur_block)
                continue
            # 其它层级的标题(### / #####)—— 同样断开,别让上一块的表串进来
            cur_block = None
            continue

        if line.startswith("|"):
            if re.match(r"^\|[\s\-:|]+\|$", line):   # 分隔行
                continue
            if ROW_RE.match(line) and cur_block is not None:
                cur_block.rows.append((i + 1, line))
            continue

    return sections


def is_done(row_text):
    """一行是不是"已完成"。判据:整行出现过 ✅(跟清单自己的写法一致)。"""
    return "✅" in row_text


def expand_range(a, b):
    """`2.1` - `2.10` → 2.1, 2.2, …, 2.10(按数字展开,不是字符串切)。

    ⚠️ 必须按数字展开:字符串排序会把 `2.10` 排在 `2.2` 前面。
    """
    pa, pb = a.split("."), b.split(".")
    if len(pa) != 2 or len(pb) != 2 or pa[0] != pb[0]:
        return [a, b]
    return [f"{pa[0]}.{m}" for m in range(int(pa[1]), int(pb[1]) + 1)]


def stated_open(title):
    """从「❌ 未完成(7 个待补,P0 = 0 个)」里抠出声明的未完成数;没写就返回 None。"""
    m = OPEN_HEAD_RE.match(title)
    if not m or m.group(1) is None:
        return None          # 「❌ 未完成」(没写数)—— 不检查计数
    return int(m.group(1))


def stated_done(title):
    """从 ✅ 块标题里抠出声明的数。

    返回 (d, t):
      · 「已完成(14 个,文件证据)」→ (14, None)
      · 「已完成(9/10)」        → (9, 10)
      · 「已完成」/「本节三项均已完成」→ (None, None)
    """
    m = DONE_FRAC_RE.search(title)
    if m:
        return int(m.group(1)), int(m.group(2))
    m = DONE_COUNT_RE.search(title)
    if m:
        return int(m.group(1)), None
    return None, None


def main():
    quiet = "--quiet" in sys.argv
    sections = parse(DOC)
    problems = []

    for sec in sections:
        done = 0        # 本节已完成的子项数
        opened = 0      # 本节还开着的子项数
        done_blocks = []    # (block, d, t)
        # §3 / §4 / §5 / §6 的标题是「## 3. …」这种,没有 N.M 编号 ——
        # 报错时直接显示整段标题,免得打出一句「§None」。
        disp = f"§{sec['no']}" if sec["no"] else sec["title"]

        for b in sec["blocks"]:
            if b.marker == "✅":
                # 整块算已完成(这些是纯证据表,行上未必有 ✅ 标记)
                done += len(b.rows)
                d, t = stated_done(b.title)
                done_blocks.append((b, d, t))
            elif b.marker == "❌":
                # 带 ✅ 的 = 后来做完了;没带的 = 还开着
                open_here = sum(1 for _, r in b.rows if not is_done(r))
                done += len(b.rows) - open_here
                opened += open_here
                declared = stated_open(b.title)
                if declared is not None and declared != open_here:
                    problems.append(
                        f"L{b.line_no}  {disp} 标题说「{b.title}」未完成 {declared} 项,"
                        f"表里实际还开着 {open_here} 项"
                        + ("(其实全做完了 —— 这块该改成 ✅)" if open_here == 0 else "")
                    )
            # 其它标记(⚠️ / 🔧 / 📋)不计数

        total = done + opened
        # 存下来给「百分比自引用」那项检查用(它要拿源小节的完成度来核对)
        sec["done"] = done
        sec["total"] = total

        # ── ✅ 块的计数 ──────────────────────────────────────
        for b, d, t in done_blocks:
            if d is None:
                continue
            if d != len(b.rows):
                problems.append(
                    f"L{b.line_no}  {disp} 标题说「{b.title}」已完成 {d} 项,"
                    f"该块表里实际有 {len(b.rows)} 行"
                )
            if t is not None and t != total:
                problems.append(
                    f"L{b.line_no}  {disp} 标题说「{b.title}」总数 {t},"
                    f"本节实际总子项 {total}(已完成 {done} + 还开着 {opened})"
                )

        # ── 小节百分比 ────────────────────────────────────────
        if sec["pct"] is not None and total:
            real = round(done / total * 100)
            if real != sec["pct"]:
                problems.append(
                    f"L{sec['pct_line']}  {disp} 标题写 {sec['pct']}%,"
                    f"按表里实际算({done}/{total})是 {real}%"
                )

    # ── 百分比自引用 ────────────────────────────────────────────
    # 表里每一格百分比都必须指向它**真正来源**的那一节,否则"抄来的数"会自己漂。
    # ⚠️ 这一项扫的是**原始行**,不是块里的行 —— §1 / §2 两张总览表
    #   没有 #### 块,块解析器根本看不见它们,但恰恰是它们最容易抄错。
    by_no = {s["no"]: s for s in sections if s["no"]}
    with open(DOC, encoding="utf-8") as f:
        raw_lines = f.read().split("\n")

    for i, line in enumerate(raw_lines, 1):
        if not line.startswith("|"):
            continue
        for cell in line.split("|"):
            st = cell.strip()
            if not st:
                continue
            if not (PCT_CELL_RE.match(st) or PCT_HEAD_RE.match(st)):
                continue
            m = REF_CELL_RE.match(st)
            if not m:
                problems.append(
                    f"L{i}  百分比格 {st!r} 没写成自引用格式 —— 必须写成 "
                    f"**NN%** (§X.Y),否则源小节一改,这里就跟着撒谎"
                )
                continue
            pct = int(m.group(1))
            nos = [m.group(2)]
            if m.group(3):
                nos = expand_range(m.group(2), m.group(3))
            done = total = 0
            broken = False
            for no in nos:
                src = by_no.get(no)
                if src is None:
                    problems.append(f"L{i}  引用的 §{no} 在本文档里不存在")
                    broken = True
                    break
                done += src.get("done", 0)
                total += src.get("total", 0)
            if broken or not total:
                continue
            real = round(done / total * 100)
            if real != pct:
                problems.append(
                    f"L{i}  这里写 **{pct}%**,但 §{'、§'.join(nos)} 实际是 "
                    f"{real}%({done}/{total})"
                )

    if problems:
        print(f"❌ 清单和表格对不上,{len(problems)} 处:\n")
        for p in problems:
            print("  " + p)
        print("\n(每一处都是「标题声称的」和「表里实际的」对不上 —— "
              "要么补证据,要么改标题。)")
        return 1

    if not quiet:
        print(f"✅ 清单自洽:{len(sections)} 个小节,标题计数 / 百分比与表格全部对得上")
    return 0


if __name__ == "__main__":
    sys.exit(main())
