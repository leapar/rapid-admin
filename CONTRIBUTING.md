# 贡献指南

感谢关注 Rapid Admin!欢迎 Issue / PR。

## 开发环境

- Go 1.22+、Node 18+、pnpm;可选 MySQL 8 / Redis(SQLite 开发模式免装)
- 后端:`cd server && make dev-sqlite`;测试:`make test`
- 前端:`cd web && pnpm install && pnpm dev`;测试:`pnpm test`,lint:`pnpm lint`

## 提交约定

- commit message 用 `type(scope): 描述`(feat / fix / docs / refactor / test / chore)
- 改了 `server/docs/` 下任何 `.md` 之后必须 `make docs` 重建手册并一起提交 ——
  `go test ./docs/` 里有产物漂移检测,不同步会红
- 新增 `internal/pkg/<包>` 必须同时有:同名示例(`examples/<包>/`)、手册章节、
  以及 `internal/guard` 可达性(守卫会扫"零调用方的框架包")
- 涉及权限 / 鉴权的改动,请补可逆探针测试(拆掉实现,测试要能变红)

## 文档纪律

本仓的文档是"会说谎会变红"的:

- `server/internal/guard` 扫描文档里反引号包着的仓库路径,断链即红
- `scripts/check_backlog_consistency.py` 校验 `docs/ROADMAP.md` 里每个计数与表格一致
- `server/docs` 有 SUMMARY 孤儿页 / 相对链接 / 包引用三项检查(`scripts/check_docs_links.py`)

写文档时引用代码路径请用反引号并确保路径真实存在;引用计数请与表格对得上。
