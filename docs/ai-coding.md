# AI Coding 接入规划

> 目标:让任何人用任何 AI coding 工具(Claude Code / Cursor / Codex / Jules / Copilot / Windsurf)
> 接入本框架时,AI 能在**一次上下文加载内**拿到正确的事实 —— 模块有哪些、命令怎么跑、
> 新代码往哪放、硬约束是什么 —— 而不是靠 AI 猜目录结构或读完整本手册。

状态:规划稿(2026-11-05)。本文档本身不引入任何代码改动,落地时按 §7 的优先级逐项实施。

---

## 1. 背景

AI agent 读代码的方式和人不一样:

- 人的入口是 README + GitBook 手册;agent 的入口是**约定文件名**(AGENTS.md、llms.txt),
  找不到约定文件就会自己 ls + grep,命中率低、浪费上下文。
- agent 的上下文窗口有限:56 页手册不可能整本塞进去,需要"一小页索引 + 按需取详情"的结构。
- agent 最容易犯的错是**违反仓库约定**(比如业务包 import 框架层、写了模块不写示例) ——
  这些约束现在散在手册各章和测试里,没有一处"给 AI 看的汇总"。

## 2. 调研结论

(2026-11 核实,来源:llmstxt.org v2 规范全文、agents.md 官网全文、
docs.claude.com Claude Code memory 文档)

### 2.1 AGENTS.md —— 仓库内 agent 指令的事实标准

- 一个普通 Markdown 文件,放仓库根,内容自定;常见小节:项目概览、构建/测试命令、
  代码风格、测试说明、PR 约定、"会告诉新同事的任何事"。
- 被 60k+ 开源项目采用,由 Linux 基金会旗下 Agentic AI Foundation 托管;
  读取方包括 Codex、Cursor、Jules、Windsurf、GitHub Copilot coding agent、Zed、Aider 等。
- **Claude Code 已原生支持**:仓库没有 CLAUDE.md 时,直接读 AGENTS.md 顶替
  (官方 memory 文档明确写了 "Use an existing AGENTS.md as your project instructions")。
- 大仓库用嵌套 AGENTS.md:agent 自动读被改文件**最近**的那个,就近生效。
- 没有必填字段、没有 schema,冲突规则:就近优先,用户对话指令最高。

### 2.2 llms.txt v2(llmstxt.org)—— 文档站点的 agent 索引标准

格式(顺序固定):

```markdown
# 项目名                          ← H1,唯一必需
> 一句话摘要                       ← blockquote
自由段落/列表(不放标题)
## H2 分组的文件清单
- [链接名](url): 说明              ← 必须是 markdown 超链接
## Optional                       ← 约定俗成:次要内容,短上下文时 agent 可跳过
- [链接名](url)
```

要点:

- 可放站点根 `/llms.txt`,也可放任意子路径(如 `/docs/llms.txt`),覆盖该路径下所有页面;
  多个文件同时适用时,agent 用最具体的那个。
- 设计意图:**索引保持一屏内**,详情在链接后面按需取。
- v2 建议每个文档页同时提供纯 markdown 版(`page.html.md` 或 `page.md`),
  并用 `Link:` 响应头(`rel="describedby"` 指向 llms.txt、`rel="alternate"` 指向 .md 版)暴露。
- Mintlify / GitBook 等文档平台已自动生成;OpenAI、Anthropic、Gemini 的开发者文档都发布了 llms.txt。
- Chrome Lighthouse 已把 llms.txt 纳入 agentic browsing 检查项。

### 2.3 对本仓库的含义

两类文件互补,不是二选一:

| 文件 | 管什么 | 谁读 |
|---|---|---|
| AGENTS.md | **改这个仓库代码**的 agent(二次开发、贡献) | 仓库内,agent 自动加载 |
| llms.txt | **拿框架去开发自己业务**的 agent(查 API、抄用法) | 文档站点 + GitHub 仓库外 |
| install.md / module-list.md / module-add.md | 上面两者的**弹药库**,被 AGENTS.md 和 llms.txt 链接 | 按需加载 |

## 3. 现状盘点

已有(资产):

- 51 个框架模块(`server/internal/pkg/`),每个都有 51 个 `go run` 即跑的示例,
  `examples/examples_test.go` 守着"有模块必有示例"。
- 62 页 HonKit 手册,源在 `server/docs/*.md`(含 `SUMMARY.md` 结构文件),
  产物进二进制(`go:embed`)也出 `site/`。
- 已有守卫文化:`internal/guard`(原语可达性)、`check_docs_links.py`(断链/漂移)、
  `docs-check`(产物与源同步)。
- 根 README 是给人看的快速导览。

缺(缺口):

- 无 AGENTS.md / CLAUDE.md —— agent 进仓库没有约定入口。
- 无机器可读的模块速查(module-list):51 个模块的"一句话+在哪读+在哪跑"散在手册里。
- 无"新增一个模块"的机器可执行配方(module-add):流程在手册纪律里,没串成 checklist。
- 无 install.md:起步步骤散在根 README、`getting-started/`、config.example.yaml 注释里。
- 无 llms.txt;手册源 .md 在仓库里有,但**站点产物里只有 HTML**,线上 agent 拿不到纯文本。

## 4. 规划总览

| # | 文件 | 位置 | 受众 | 维护方式 | 守卫 |
|---|---|---|---|---|---|
| 1 | `AGENTS.md` | 仓库根 | 改仓库代码的 agent | 手写 | 命令真实性 CI 抽查 |
| 2 | `install.md` | `docs/ai/` | 起步的 AI / 人 | 手写 | 链接检查(入 check_docs_links) |
| 3 | `module-list.md` | `docs/ai/` | 查模块的 AI | **脚本生成** | 生成脚本 check 模式,过期即红 |
| 4 | `module-add.md` | `docs/ai/` | 加新模块的 AI | 手写配方 | DoD 清单对应既有守卫 |
| 5 | `llms.txt` | 仓库根 | 站外/GitHub 上的 agent | 脚本生成 | 链接检查 |
| 6 | `llms.txt` + `.md` 副本 | `server/docs/site/` | 线上文档站点的 agent | 构建时生成 | `make docs-check` 扩展 |

放置理由:AGENTS.md 必须在根(约定路径,agent 只认根);三个 ai 文档收进 `docs/ai/`
避免根目录膨胀,由 AGENTS.md 第一屏链接;llms.txt 在根 + 站点产物各一份(规范允许子路径)。

## 5. 各文件内容设计

### 5.1 `AGENTS.md`(根,手写,目标 ≤120 行)

```markdown
# AGENTS.md — rapid-admin
> 一句话:Go (Hertz+GORM+Casbin) + React18+Antd5 的通用后台骨架,
> 51 个框架模块在 server/internal/pkg/,每个都有可跑示例。

## 仓库布局
server/  web/  js-sdk/  docs/  server/docs/(手册源+产物)

## 常用命令
- 后端测试: cd server && go test ./...
- 跑某模块示例: cd server && go run ./examples/<模块名>
- 前端: cd web && pnpm dev / pnpm lint / pnpm typecheck / pnpm test
- 手册重建: cd server && make docs(产物 site/ 与源必须同步,docs-check 会红)
- 配置样例: server/configs/config.example.yaml(每个键有注释)

## 硬约束(违反会让 CI 红,不是风格建议)
1. internal/pkg/ 不得 import internal/{service,model,api,cmd} —— examples_test 守
2. 新增框架模块必须同时有 examples/<模块名>/main.go —— examples_test 守
3. 手册章节里的代码段必须真实存在于示例 —— check_docs_links 守
4. 改了 server/docs 源必须 make docs 重建,产物漂移即红 —— docs-check 守
5. 改框架原语必须保持 internal/guard 可达性(写了没人调用会红)

## 代码风格
Go: 参照同目录既有文件;错误用框架统一响应体/错误码,不许裸写
Web: TS strict + ESLint max-warnings 0;权限码从数据库下发,前端不硬编码

## 加新模块?
先读 docs/ai/module-add.md,再动手。
```

设计原则:不复制手册,**只放约束和命令**,详情全部外链。AGENTS.md 说谎比没有更糟,
所以每条硬约束都标注"谁在守"。

### 5.2 `docs/ai/install.md`(手写,给 AI 执行的起步剧本)

结构:前置依赖(Go 1.22+ / Node+pnpm / 可选 Redis) → 后端五步(cp config.example.yaml →
`go run ./cmd/server` → seed 脚本 → 默认账号 → 访问 /swagger 与 /docs/index.html)→
前端三步(pnpm i → dev → 代理说明)→ 验证清单( curl 就绪探针、登录成功即完成)。
每个命令都可被 AI 原样复制执行,写明**预期输出**(agent 靠它判断成功/失败)。

### 5.3 `docs/ai/module-list.md`(脚本生成,永不手改)

每模块一条:

```markdown
| 模块 | 一句话 | 手册 | 示例 |
|---|---|---|---|
| cachex | 两级缓存(L1 LRU + L2 Redis) | [手册](../site 链接) | go run ./examples/cachex |
```

数据源与生成方式:`internal/pkg/` 目录名 × `server/docs/SUMMARY.md` 章节 ×
示例目录,三边对齐后由脚本(建议 `go run ./cmd/docs module-list` 或
`server/scripts/gen_module_list.py`)渲染;`-check` 模式对比产物,过期退出码 1 ——
和现有"文档不撒谎"守卫同一哲学。**这个文件永远不出现在手工编辑的 git diff 里。**

### 5.4 `docs/ai/module-add.md`(手写配方,9 步)

新增一个框架模块的机器可执行 checklist,每步带验收命令:

1. `mkdir server/internal/pkg/<name>`,对照同组最相近模块定接口形态
2. 配置:在 `config.example.yaml` 加键 + 注释(有安全默认值,或漏配显式报错 —— 手册前提 #1)
3. 示例:`examples/<name>/main.go`,可 `go run` 出可见输出
4. 登记:`examples_test.go` 的模块清单加一行,`go test ./examples` 必须绿
5. 手册:`server/docs/<组>/<name>.md` 三段式(功能/配置/使用方法),SUMMARY 收录
6. 可达性:确认 `internal/guard` 对新包不红(至少被一个示例/服务引用)
7. 重建手册:`make docs`,`docs-check` 绿
8. 全量:`go test ./...`
9. 完成定义(DoD):2/4/6/7/8 全绿,缺一不算完

### 5.5 根 `llms.txt`(脚本生成)

```markdown
# Rapid Admin
> Go + React 通用后台管理框架:RBAC/数据范围/字段级加密脱敏/双级缓存/审计,
> 51 个框架模块各配手册章节与可运行示例。

## 手册(纯 Markdown)
- [快速开始](https://raw.../server/docs/getting-started/README.md): ...
- [包速查表](...): 51 个模块一览
- ...(按 SUMMARY.md 八组生成,链接指向 GitHub raw 的 .md 源文件)

## 仓库文档
- [错误码表](...): 4xx 全量错误码与语义
- [AGENTS.md](...): 改仓库代码的 agent 指令

## Optional
- [设计文档](...) / [ROADMAP](...)
```

由脚本从 `SUMMARY.md` 渲染,链接统一指向 GitHub raw(站外 agent 无需 clone)。

### 5.6 `server/docs/site/llms.txt` + `.md` 副本(构建时生成)

- `cmd/docs build` 在 honkit 之后追加一步:把手册源 `.md` 按目录结构拷进 `site/`,
  并生成 `site/llms.txt`(链接指向站内相对路径的 `.md` 副本)。
- 对应 §2.2 v2 的 "同 URL 提供 markdown 版" 建议 —— 线上
  `https://f.3dman.cn/static/rapid-admin/docs/llms.txt` 即可用
  (上传 OSS 时把新增文件一并带上;目录 URL 解析问题见既有结论,与本次无关)。
- `docs-check` 同步校验 site/llms.txt 与 SUMMARY 一致。

## 6. 守卫与维护

延续仓库"文档不撒谎让 CI 红"的原则,新增守卫全部收进既有机制:

| 守卫 | 挂载点 |
|---|---|
| module-list 生成一致性 | 生成脚本 `-check` 模式,进 CI |
| llms.txt(根)链接可解析 | 扩展 `check_docs_links.py` |
| site/llms.txt + .md 副本同步 | `make docs-check` |
| AGENTS.md / module-add.md 中命令真实可跑 | 低频人工 CI 抽查(写进 pre-commit 注释说明) |

## 7. 实施顺序

| 阶段 | 内容 | 工作量 | 价值 |
|---|---|---|---|
| **P0** | AGENTS.md + docs/ai/install.md + docs/ai/module-add.md | 纯手写,半天 | 立即覆盖改代码场景(Claude Code/Cursor 进仓库就有入口) |
| **P1** | module-list.md 生成脚本 + 根 llms.txt 生成脚本 + 守卫 | 1 天 | 覆盖"用框架开发业务"场景,51 模块速查 |
| **P2** | site/llms.txt + site 内嵌 .md 副本 + docs-check 扩展 | 半天 | 线上站点(OSS)对 agent 可用 |

P3(暂不做,记录备查):MCP server(把手册/Swagger 做成 MCP 工具)。
收益 < 文档面,等 P0–P2 用起来有真实痛点再评估。

## 8. 取舍说明

- **只写 AGENTS.md,不再单写 CLAUDE.md**:Claude Code 已原生读 AGENTS.md,
  两份必然漂移,违反本仓库反漂移原则。
- **module-list 用生成不用手写**:51 个模块手写必漂移;数据源(目录名/SUMMARY/示例)
  三边机器对齐,天然和 examples_test、check_docs_links 互为印证。
- **AGENTS.md 控制在一屏内**:agent 每次会话都加载它,长文件等于给所有会话加税;
  细节外链让 agent 按需取,和 llms.txt 的"索引+按需"是同一个设计。
- **install.md 放 docs/ai/ 而非根**:根目录只留约定文件(README/AGENTS.md/LICENSE/llms.txt),
  场景文件收进子目录由 AGENTS.md 引路,降低人类读者的根目录噪音。
