# AGENTS.md — rapid-admin

> Go (Hertz + GORM + Casbin) + React 18 + Ant Design 5 的通用后台管理框架。
> 53 个框架模块在 `server/internal/pkg/`,每个模块都有 `go run` 即跑的示例;
> 70 页内嵌手册在 `server/docs/`。本文件是给 AI coding agent 的指令 ——
> 人读的导览在 [README.md](README.md)。

## 仓库布局

```
server/    Go 后端(Hertz + GORM + Casbin),internal/pkg/ 是框架模块本体
web/       React 18 + Antd 5 管理端(Vite + TS strict + Zustand)
js-sdk/    @rapid-admin/dyn-sdk —— 动态表单数据面 SDK(零运行时依赖)
docs/      仓库级文档(设计 / ROADMAP / AI 接入规划 / 皮肤规划)
server/docs/  框架手册源(HonKit)+ site/ 产物(go:embed 进二进制)
scripts/   check_docs_links.py 等仓库守卫脚本
```

## 常用命令

```bash
# 后端(在 server/ 下)
go test ./...                       # 全量测试
go run ./examples/<模块名>           # 跑某模块的最小示例(53 个,见 docs/ai/module-list.md)
go run ./cmd/migrate -config configs/config.yaml   # 建库 + seed(默认账号 admin / Admin@2026)
go run ./cmd/server -c configs/config.yaml         # 启动(注意是 -c 不是 -config)
make docs                           # 手册重建(honkit + buildinfo;产物 site/ 必须与源同步)
make docs-check                     # 产物与源不同步时退出码 1

# 前端(在 web/ 下,pnpm)
pnpm dev                            # Vite dev server
pnpm lint                           # ESLint(--max-warnings 0,零容忍)
pnpm typecheck                      # tsc --noEmit
pnpm test                           # Vitest
pnpm build                          # typecheck + vite build

# 仓库守卫(根目录)
python scripts/check_docs_links.py  # 手册结构/断链/引用真实性检查
```

配置样例:`server/configs/config.example.yaml`,每个键都有注释;复制为 `config.yaml` 使用。

## 硬约束(违反会让 CI 红 —— 不是风格建议)

1. **框架模块不依赖业务**:`internal/pkg/` 下任何包不得 import
   `internal/{service,model,api,cmd}` —— `examples/examples_test.go` 在守。
2. **新增框架模块必须同时有示例**:`server/examples/<模块名>/main.go`,可
   `go run` 出可见输出 —— 同上,`examples_test.go` 在守。流程见 [docs/ai/module-add.md](docs/ai/module-add.md)。
3. **手册里的代码段必须真实存在**:手册引用的示例路径、包名、错误码,
   `scripts/check_docs_links.py` 会逐一核对。
4. **改了 `server/docs/` 源必须 `make docs` 重建**:产物漂移 `make docs-check` 即红。
5. **框架原语必须可达**:`internal/guard` 守着"写了但没人调用"的包,会红。
6. **前端品牌色不写进 token**:新增/修改品牌色值必须进 `web/src/theme/skins/`,
   不得直接写进 `LangRoot` 的 token(见 [docs/theming.md](docs/theming.md))。

## 代码风格

- **Go**:参照同目录既有文件;接口返回一律走统一响应体与错误码
  (`internal/pkg/response` / `errcode`),不许裸写 `c.JSON`。
- **Web**:TS strict + ESLint `--max-warnings 0`;权限码从数据库下发
  (`useAuthStore`),前端不硬编码按钮权限;颜色 token 见约束 6。
- 提交信息用 `type(scope): 描述`(feat / fix / docs / refactor / test / chore)。

## 起步 / 加新模块?

- 首次上手(装依赖、起服务、打第一个接口):[docs/ai/install.md](docs/ai/install.md)
- 新增一个框架模块:[docs/ai/module-add.md](docs/ai/module-add.md)
- 换 UI 皮肤:[docs/theming.md](docs/theming.md) §5
