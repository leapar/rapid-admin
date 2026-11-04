// Package swagger 内嵌 swagger-ui 静态资源。
//
// # 为什么把 UI 资产放进仓库
//
// 之前 UI 来自 Go module github.com/swaggo/files —— 也就是说
// **页面长什么样不由本仓库决定**,而是 transitively 由 go.mod 决定。
// 后果有三个:
//
//   - UI 的版本/内容不由本仓库说了算,go.mod 一升级就可能悄悄变掉
//   - 想改个标题/加个 logo 都得去 fork 一个依赖
//   - 离线部署时,页面里任何外链都会挂住(渲染被拖到超时)
//
// 放进仓库之后:版本是我们定的(见 ui/README.md)、改起来是改一个静态文件、
// 离线部署不依赖任何外网、也不再多一个第三方 Go 依赖。
//
// # 许可
//
// swagger-ui 是 Apache-2.0。出处、版本、为什么丢掉了 .map 文件,
// 都写在 ui/README.md 里。
package swagger

import (
	"embed"
	"io/fs"
)

// assets 内嵌 ui/ 目录下的全部静态资源。
//
//go:embed ui
var assets embed.FS

// Assets 返回 swagger-ui 静态资源的 fs.FS(根就是 index.html 那一层)。
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "ui")
	if err != nil {
		// embed 的目录在编译期就定死了,Sub 失败只可能是"目录名写错" ——
		// 那时编译根本过不去。这里返回原始 FS 而不是 panic:
		// swagger 挂不上不该把整个服务带崩。
		return assets
	}
	return sub
}
