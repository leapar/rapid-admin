# module-add.md —— 新增一个框架模块的机器可执行配方

> 受众:AI agent(或人)要在 `server/internal/pkg/` 下新增一个框架模块。
> 本配方每一步都带**验收命令**;§DoD 的检查全绿才算完成。
> 为什么是这九步:仓库的守卫(`examples_test.go` / `internal/guard` /
> `check_docs_links.py`)会强制其中 5 步,跳步 = CI 红。

## 第 1 步:建包

```bash
mkdir server/internal/pkg/<模块名>
```

- 模块名小写、一个词、不带后缀(对照同目录:`cachex`、`idempotentx`、`grayx`)。
- **先找同组最相近的模块抄接口形态**(存储类抄 `kvstore`,安全类抄 `signx`,
  消息类抄 `pushx`),不要从零发明 API 风格。
- 硬约束(会红):本包**不得** import `internal/{service,model,api,cmd}`
  —— `examples/examples_test.go` 的 `forbiddenImports` 在守。

## 第 2 步:配置

在 `server/configs/config.example.yaml` 加本模块的配置键 + 注释。

- **每个键必须有安全默认值;漏配显式报错或显式降级,禁止静默走默认路径**
  (例外:影响时间语义的键如 `job.timezone` 反过来 —— 必须显式配,配错会红)。
- 结构体落在 `internal/config/`,与 yaml 键一一对应。

## 第 3 步:最小可运行示例

```bash
# server/examples/<模块名>/main.go
go run ./examples/<模块名>
```

- 预期:打印**可见的演示输出**(如命中/未命中、拦截前后),不是"没报错就是成功"。
- 手册「使用方法」小节将直接引用这份代码 —— 示例即文档素材。
- 硬约束(会红):`examples_test.go` 自动扫描 `internal/pkg/`,新模块目录出现
  而 `examples/<模块名>/main.go` 不存在 ⇒ 测试红。**不需要手工登记模块清单**。

## 第 4 步:手册章节

建 `server/docs/<组>/<模块名>.md`(组 = SUMMARY 八组之一,对号入座):

- 三段式结构:**功能 / 配置 / 使用方法**(抄任意一章的骨架,如 `security/auth.md`);
- 「使用方法」小节的代码段**就是**第 3 步示例的片段 —— `check_docs_links.py`
  会核对文档引用的示例路径与包名真实存在;
- 注册进 `server/docs/SUMMARY.md`(格式:`* [<模块名>(一句话)](<组>/<模块名>.md) — 补充说明`)。

## 第 5 步:可达性

```bash
cd server && go test ./internal/guard/...
```

- guard 算的是**从 `cmd/` 入口出发的可达性**,"写了但没人调用"的包会红。
- 新模块至少要被一个示例(第 3 步)引用;要进生产路径就再被 service 层装配。
- 扫出的"不可达"分两类处理(见 `internal/guard/guard.go` 头注释),别机械加 import 了事。

## 第 6 步:重建手册

```bash
cd server && make docs && make docs-check
```

- `make docs`:honkit 重建 `site/` + 刷新 buildinfo 指纹;
- `make docs-check`:产物与源不同步 ⇒ 退出码 1。**改手册源不重建 = 红**。

## 第 7 步:全量回归

```bash
cd server && go test ./...
```

预期全绿。红了先看是不是把业务包拉进了框架层(第 1 步约束)。

## DoD(完成定义,缺一不算完)

- [ ] `go build ./...` 过(新包在 `internal/pkg/<模块名>`)
- [ ] `go run ./examples/<模块名>` 有可见演示输出
- [ ] `config.example.yaml` 有本模块键 + 注释,默认值安全
- [ ] 手册章节三段式齐全,`SUMMARY.md` 已收录
- [ ] `go test ./internal/guard/...` 绿(可达)
- [ ] `make docs-check` 绿(产物同步)
- [ ] `go test ./...` 全绿
- [ ] 新增的品牌/UI 值(若有前端部分)进了 `web/src/theme/skins/`,没写进 LangRoot token
      (见 [docs/theming.md](../theming.md))

## 反模式(别这么干)

- ❌ 在框架包里写业务字段/业务表名 —— 业务通过 dynx / 自建模块接入,框架层零业务。
- ❌ 示例只写"初始化成功"就 return —— 示例要演示行为,手册引用它,空转示例 = 假文档。
- ❌ 配置键漏注释 —— config.example.yaml 的注释就是这份配置的文档。
