# AI 接入(vibe coding)

> 本章给"用 AI coding agent 改这个仓库"的读者:agent 从哪进、哪些文件是
> 生成物不许手改、守卫红了怎么读。人和 agent 读同一本手册,但入口不同。

## 给 AI agent 的入口(按顺序)

以下文件都在**仓库根**,是 agent 的专用入口,人读的导览在 `README.md`:

| 顺序 | 文件 | 作用 |
|---|---|---|
| 1 | `AGENTS.md` | 常用命令 + 6 条硬约束(每条都标了守它的检查),改代码前必读 |
| 2 | `docs/ai/install.md` | 从零跑起来并打第一个接口的可执行剧本 |
| 3 | `docs/ai/module-add.md` | 新增一个框架模块的七步配方 |
| 4 | `docs/ai/module-list.md` | 框架模块速查表(SUMMARY × internal/pkg × examples 三边对齐) |
| 5 | `llms.txt` | 全部手册章节的 raw 链接索引(llmstxt.org v2 格式) |

**部署实例上拿不到仓库?** 本站每页都有同路径的纯 Markdown 副本
(如 `/docs/getting-started/ai-coding.md`),站点根有 `/docs/llms.txt` ——
构建时由 `server/cmd/docs/aiiface.go` 与 HTML 同源生成,永远同步。

## 生成物清单(手改会被覆盖或被守卫红掉)

| 生成物 | 生成命令 |
|---|---|
| `server/docs/site/`(HTML + .md 副本 + llms.txt) | `cd server && go run ./cmd/docs build` |
| `docs/ai/module-list.md`、仓库根 `llms.txt` | `python scripts/gen_ai_docs.py` |

## 守卫红了先读守卫的话

本仓库的检查报错都写着"怎么修",把报错原文丢给 agent 通常就能自愈:

| 守卫 | 红了意味着 |
|---|---|
| `server/examples/examples_test.go` | 新模块没配示例,或框架包反向依赖了业务层 |
| `internal/guard` | 写了但没人调用的"死"框架原语 |
| `scripts/check_docs_links.py` | 手册断链 / 孤儿页 / 引用了不存在的包或示例 |
| `make docs-check`(或 `go run ./cmd/docs check`) | site/ 产物与源漂移,重建即可 |
| pre-commit 第 4 步 | module-list / llms.txt 过期,跑 `python scripts/gen_ai_docs.py` |

## 为什么这个框架对 agent 友好

- 51 个框架模块每个都有 `go run` 即跑的示例 —— agent 可以自我验证,不用猜;
- 错误码、响应体、配置项全表都在手册里 —— 不存在"藏在代码里的约定";
- 守卫把"撒谎的文档"和"没人调用的代码"挡在提交之前,agent 的常见事故
  (幻觉模块名、忘记示例、产物漂移)在 CI 就红。
