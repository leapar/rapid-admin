#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""扫 "Where/And/Or 的 SQL 片段里 ? 个数 != 实参个数" 的 bug。

背景:GORM 的 clause.Expr.Build 在 "占位符比参数少" 时,会把多出来的参数
当成 sql.NamedArg 塞进 stmt.Vars —— **但不给它写对应的 ?**。
于是最终 SQL 有 N 个 ?,却带了 N+1 个参数。SQLite 驱动不查个数(单测恒绿),
MySQL 驱动查 ⇒ "sql: expected N arguments, got N+1"(用户线上报的就是这个)。

本脚本的三条"必须做对"的细节(前两条都踩过坑):
  1. **容忍换行链式调用** —— `tx.Model(&X{}).\n\t\tWhere(...)`,`Where(` 前面
     隔着换行,只按 ".Where(" 找会漏掉一大半。
  2. **识别字符串拼接** —— `"a = ? AND " + cond + " >= ?"` 是**一个**实参,
     ? 的个数要跨所有片段加起来算。
  3. **尾逗号不算分隔符** —— `f(a, b,)` 是 2 个实参不是 3 个。

已知**假阳性**(脚本会报出来,但它们是对的):
  - `IN (?)` / `IN ?` 传切片 —— GORM 在 AddVar 里把切片展开成 `(?,?,?)`,
    占位符数会随切片长度变,静态数不出来。
  - 第一个实参是**函数调用**而不是字面量(比如 fmt.Sprintf)—— 脚本直接跳过,
    不报(宁可漏报也不要误报)。

用法:python scripts/scan_placeholder_args.py [根目录]
"""
import os
import sys

ROOT = sys.argv[1] if len(sys.argv) > 1 else "."
SKIP_DIRS = {"vendor", ".git", "node_modules", "testdata", ".idea"}


def skip_space(src, i):
    while i < len(src) and src[i] in " \t\r\n":
        i += 1
    return i


def count_q_in_literals(src, start, end):
    """数 [start, end) 里**字符串字面量内部**的 ? 个数。

    字符串外的 ? 不算(SQL 片段理论上都在字面量里,但拼接进来的变量里也可能
    带 ? —— 那种情况脚本数不到,会在报告里当"可能假阳性"提示)。
    """
    qs = 0
    i = start
    while i < end:
        c = src[i]
        if c == '"':
            i += 1
            while i < end:
                if src[i] == "\\":
                    i += 2
                    continue
                if src[i] == '"':
                    i += 1
                    break
                if src[i] == "?":
                    qs += 1
                i += 1
            continue
        i += 1
    return qs


def find_calls(src):
    """产出 (start, region_end, question_marks)。

    region_end = 第一个实参表达式的结束位置(第一个顶层逗号或右括号)。
    """
    for name in ("Where", "And", "Or", "Raw", "Exec"):
        i = 0
        while True:
            i = src.find(name + "(", i)
            if i < 0:
                break
            start = i
            i += len(name) + 1
            # 往前看:跳过空白后必须紧挨着 "." 才算链式调用
            b = start - 1
            while b >= 0 and src[b] in " \t\r\n":
                b -= 1
            if b < 0 or src[b] != ".":
                continue
            j = skip_space(src, start + len(name) + 1)
            if j >= len(src) or src[j] != '"':
                continue  # 第一个实参不是字面量 —— 跳过(不误报)
            # 第一个实参表达式 = 从字面量开始,到第一个顶层逗号/右括号为止
            k = j
            depth = 0
            while k < len(src):
                c = src[k]
                if c == '"':
                    k += 1
                    while k < len(src):
                        if src[k] == "\\":
                            k += 2
                            continue
                        if src[k] == '"':
                            k += 1
                            break
                        k += 1
                    continue
                if c in "([{":
                    depth += 1
                elif c in ")]}":
                    if depth == 0:
                        break  # 调用结束
                    depth -= 1
                elif c == "," and depth == 0:
                    break  # 第一个实参结束
                k += 1
            yield start, k, count_q_in_literals(src, j, k)


def count_args(src, start):
    """从 start(第一个实参之后)开始数剩下的实参个数。"""
    i = start
    depth = 1
    commas = 0
    last_comma = -1
    end = start
    while i < len(src) and depth > 0:
        c = src[i]
        if c in "([{":
            depth += 1
        elif c in ")]}":
            depth -= 1
            if depth == 0:
                end = i
                break
        elif c == "," and depth == 1:
            commas += 1
            last_comma = i
        i += 1
    # 尾逗号:`f(a, b,)` 的实参是 2 个,不是 3 个
    if last_comma >= 0 and src[last_comma + 1:end].strip() == "":
        commas -= 1
    return commas


def main():
    bad = []
    for root, dirs, files in os.walk(ROOT):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        for f in files:
            if not f.endswith(".go"):
                continue
            p = os.path.join(root, f)
            src = open(p, encoding="utf-8", errors="replace").read()
            for start, region_end, qs in find_calls(src):
                # GORM 的签名是 Where(query, args...):query 是 SQL 片段,
                # **args 才是绑到 ? 上的参数** —— 所以个数就是逗号数。
                args = count_args(src, region_end)
                if qs != args:
                    line = src[:start].count("\n") + 1
                    snippet = src[start:src.find("\n", start)].strip()[:110]
                    bad.append((p.replace("\\", "/"), line, qs, args, snippet))
    for p, line, qs, args, snip in sorted(bad):
        print("%s:%d  ?=%d args=%d  %s" % (p, line, qs, args, snip))
    print("total:", len(bad))


if __name__ == "__main__":
    main()
