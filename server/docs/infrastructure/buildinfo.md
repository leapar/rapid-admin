# buildinfo —— 编译期构建信息注入

> `/version` 端点要返 `{app_name, version, commit, build_time, go_version, os, arch}`。
> 这些值只有编译时才知道 ⇒ 用 ldflags 注入。

## 功能

buildinfo 提供:

1. **四个可被 ldflags 覆盖的变量** —— `AppName` / `Version` / `Commit` / `BuildTime`。
2. **`Snapshot()`** —— 一站式只读快照,顺带带上 `runtime` 的 Go 版本与平台。

配套端点在 `internal/api/router/router.go` 注册:

```go
h.GET("/version", func(ctx context.Context, c *app.RequestContext) {
    c.JSON(200, buildinfo.Snapshot())
})
```

**不解决**:版本号从哪来(git tag / CI 流水线是外部的事)。

## 配置

**本包无 yaml 配置** —— 值由编译参数注入。

| 变量 | 默认值 | 说明 |
|---|---|---|
| `buildinfo.AppName` | `rapid-admin` | 应用名 |
| `buildinfo.Version` | `dev` | 版本号 |
| `buildinfo.Commit` | `unknown` | git 短 hash |
| `buildinfo.BuildTime` | `unknown` | 构建时间(UTC,ISO 8601) |

未注入时是 `dev` / `unknown` / `unknown` —— **不会 panic,也不会返空**。

## 使用方法

### 注入(编译时)

包路径必须是**完整 import path**(模块名 `rapid-admin` + 包路径):

```bash
go build -ldflags "\
  -X rapid-admin/internal/pkg/buildinfo.Version=v1.2.3 \
  -X rapid-admin/internal/pkg/buildinfo.Commit=$(git rev-parse --short HEAD) \
  -X rapid-admin/internal/pkg/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/rapid-admin ./cmd/server
```

`-X` 只对**包级字符串变量**生效。`buildinfo` 里的四个都是 `string` 类型 ⇒ 可以注入。

### 读取(运行期)

```go
info := buildinfo.Snapshot()

info.AppName    // rapid-admin
info.Version    // dev
info.Commit     // unknown
info.BuildTime  // unknown
info.GoVersion  // go1.22.x(runtime.Version())
info.OS         // linux / windows / darwin
info.Arch       // amd64 / arm64
```

### 运行时改 AppName

```go
buildinfo.AppName = "示例平台"
```

`AppName` 是可变的包级变量,运行时可改(平台名可配那个需求走的就是这条)。
`Snapshot()` 返回的是**值拷贝**,调用方拿到的 struct 改不动全局变量。

完整示例见 `examples/buildinfo/main.go`。

## 注意事项

- ⚠️ **`make build` 没有注入 ldflags**。仓库 `Makefile` 的 build 目标是:

  ```make
  CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN_DIR)/$(BIN_NAME) $(MAIN_PKG)
  ```

  `-s -w` 只是去符号表和调试信息,**没有 `-X`** ⇒ 走 `make build` 出来的二进制,
  `/version` 永远返 `version=dev` / `commit=unknown` / `build_time=unknown`。
  ⇒ 上线前要么改 Makefile 加上 `-X`,要么在 CI 里直接用上面的 `go build` 命令。
- ⚠️ **`-X` 的包路径写错不会报错,只会静默不生效**。
  必须是 `rapid-admin/internal/pkg/buildinfo.Version`
  —— 少了模块名前缀(`internal/pkg/buildinfo.Version`)或写成了目录路径,
  编译照过,变量还是默认值。
- ⚠️ **`buildinfo.AppName` 没有从 `system.app_name` 读**。
  `cfg.System.AppName`(yaml `system.app_name`,示例值 `rapid-admin`)
  只用在 OTel service name 和 Swagger 标题派生上;
  `/version` 的 `app_name` 走的是 `buildinfo.AppName` 这个**独立的包级变量**,
  默认值是 `rapid-admin` —— 和 yaml 里的 `rapid-admin` **不是一个值**。
  包注释里写的 `buildinfo.AppName = cfg.System.AppName` 是建议用法,
  `cmd/server/main.go` 里**目前没有这一行**。
- ⚠️ **`BuildTime` 的格式要自己定**。包注释推荐的 `date -u +%Y-%m-%dT%H:%M:%SZ`
  是 UTC 的 ISO 8601;注入什么就返什么,不校验。
- ⚠️ **变量是全局可变状态**。`AppName` 在 `main` 装配阶段改一次即可;
  运行时并发改它属于数据竞争(没有加锁)—— 不要这么用。
- ⚠️ **`Snapshot()` 是快照,不是活视图**。它把四个变量的当前值拷进 struct;
  之后全局变量变了,手里这份 `Info` 不变。

## 相关

- 源码:`internal/pkg/buildinfo/`(`buildinfo.go`)
- 可运行示例:`examples/buildinfo/main.go` —— `go run ./examples/buildinfo`
- 端点注册 → `internal/api/router/router.go`
- 构建产物与配置 → [配置体系](../getting-started/configuration.md)
