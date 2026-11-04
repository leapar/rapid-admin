# 框架手册 —— 构建 / 内嵌 / 访问

> 这一本手册本身也是**可执行产物**:源在 `server/docs/*.md`,
> 构建产物在 `server/docs/site/`,用 `go:embed` 打进二进制,
> 由服务端挂在 `/docs` 上 —— 和 [Swagger UI](swaggerx.md) 同一套路子。

## 功能

三件事,缺一不可:

1. **构建** —— HonKit(GitBook 的维护分支)把 58 页 Markdown 编成一个静态站。
   源在 `server/docs/`,产物在 `server/docs/site/`。
2. **内嵌** —— `//go:embed all:site` 把产物打进二进制。
   部署时**不需要 node、不需要外网、不需要额外进程**:
   手册就是二进制的一部分,跟 `server/swagger/`(swagger-ui)一个待遇。
3. **访问** —— `GET /docs` 302 跳 `/docs/index.html`,静态资产由服务端直出。
   页面里的链接**全是相对的**,所以手册挂在任何前缀下都能跳。

外加两条纪律:

- **产物提交进仓库**。它不是"随时能重建的垃圾",是**发布物** ——
  二进制里嵌的那一份就是它。仓库里存着产物,clone 下来的人拿到的就是对的。
- **漂移有网**。源改了没重建 ⇒ 线上手册停在上一版,而这个坑**没有任何报错**
  (编译过、测试过、页面照开)。所以 `go test ./docs/` 会红,并告诉你跑哪条命令。

## 配置

手册本身**没有独立的配置块** —— 它跟着 Swagger 的文档端点一起开关:

| 键 | 默认 | 说明 |
|---|---|---|
| `swagger.enabled` | `true` | 关掉只影响 `/swagger`,**不影响 `/docs`** |
| `swagger.auth.mode` | `auto` | `auto` / `basic` / `none`,**手册与 Swagger 共用这套凭据** |

⚠️ **手册默认要鉴权,和 `/swagger` 同一套 BasicAuth 凭据**(`swagger.auth`)。
理由一样:手册里写了默认账号、配置键名、模块清单、示例命令 ——
跟 API 清单一样,裸奔等于把内部结构画好送出去。

- `auto`(默认):配了固定凭据就用固定的;没配就**本次启动随机生成**(密码打启动日志)。
- `basic`:必须有固定凭据,没有直接报错(拒绝启动)。
- `none`:不鉴权 —— 只允许本地开发用,启动会打 WARN。

## 使用方法

### 构建

```bash
cd server

# 源变了才重建(比对源文件指纹);没变直接跳过,连 node 都不需要
go run ./cmd/docs build

# 强制重建
go run ./cmd/docs force

# 只检查:产物落后于源 ⇒ 退出码 1(CI 用)
go run ./cmd/docs check
```

`make build` 会自动先跑一遍 `make docs` —— 手册产物是用 `go:embed` 嵌进二进制的,
所以"改了手册没重建"= "二进制里嵌的是旧手册"。

构建需要 Node.js(npm 装 honkit 与插件):

```bash
cd server/docs
npm ci          # 第一次要装;之后 node_modules 在就不用再装
npm run build   # = honkit build . site
npm run serve   # 本地预览 http://localhost:4000
```

### 访问

| 端点 | 说明 |
|---|---|
| `GET /docs` | 302 跳 `/docs/index.html` |
| `GET /docs/index.html` | 手册首页 |
| `GET /docs/<章节>/<页面>.html` | 某一页 |
| `GET /docs/gitbook/*` | 主题与插件的静态资源 |
| `GET /docs/buildinfo.json` | 构建元信息(源指纹 / 构建时间 / 页数) |

鉴权跟 `/swagger` 一样是 BasicAuth:

```bash
curl -u swagger:<启动日志里那个密码> http://127.0.0.1:8888/docs/index.html
```

## 注意事项

- **产物是发布物,不是垃圾**。改了 `server/docs/` 下的任何 `.md` / `book.json` /
  `package.json` 之后,必须 `make docs` 重建并把 `server/docs/site/` 一起提交,
  否则 `go test ./docs/` 会红(它比对源文件指纹与产物里 `buildinfo.json` 记的指纹)。
- **honkit 不自带 classic 插件**。`betterchinese` / `chapter-fold` /
  `back-to-top-button` / `hide-element` / `custom-favicon` / `addcssjs` /
  `highlight` / `search-pro` 都得 npm 装,否则报
  `Failed to load HonKit's plugin module`。
- **指纹跨平台要一致**。本仓 git 配了 `core.autocrlf=true`,同一个文件在
  Windows 工作区是 CRLF、在仓库里是 LF —— 所以算指纹时**先把 CRLF 归一成 LF**,
  否则同一份源在每个人的机器上算出不同指纹,漂移网在每台机器上都是红的。
- **指纹不含 `.go` 文件**。`server/docs/` 同时是一个 Go 包(`docs.go` / `docs_test.go`),
  那些文件的改动不影响手册产物;算进去会导致"每次提交都判定过期 ⇒ 永远在重建"。
- **构建目录必须在 module 之外或被忽略**。swag CLI 生成物落在 module 内部会让
  `go build ./...` 失败(它 import 的 `github.com/swaggo/swag` 不在本仓 go.mod 里)。
- **`node_modules` 在 module 内部**。`go list ./...` 会把它当包扫(flatted 带了
  `.go` 文件)—— 所以 `.golangci.yml` 里排掉了 `docs/node_modules`。

## 相关

- [swaggerx](swaggerx.md) —— API 文档端点(`/swagger`),与手册共用鉴权凭据
- [快速开始](../getting-started/README.md) —— 十分钟把框架跑起来
- [配置体系](../getting-started/configuration.md) —— yaml 配置项怎么分层
