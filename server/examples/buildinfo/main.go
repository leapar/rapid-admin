// buildinfo —— 编译期构建信息
//
// ## 这个包解决什么问题
//
// `/version` 端点要返 {version, commit, buildTime, goVersion, appName}。
// 这些值只有编译时才知道 ⇒ 用 ldflags 注入。
//
// ## 注入方式
//
//	go build -ldflags "\
//	  -X rapid-admin/internal/pkg/buildinfo.Version=v1.2.3 \
//	  -X rapid-admin/internal/pkg/buildinfo.Commit=$(git rev-parse --short HEAD) \
//	  -X rapid-admin/internal/pkg/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// ## 关键点
//
//   - 未注入时是 "dev" / "unknown" / "unknown" —— 不会 panic,也不会返空
//   - AppName 可以在运行时由配置覆盖(平台名可配置那个需求就靠它)
//   - Snapshot() 是**只读快照**:调用方拿到的 struct 改不动全局变量
//
// ## 跑法
//
//	go run ./examples/buildinfo
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/buildinfo"
)

func main() {
	info := buildinfo.Snapshot()

	fmt.Println("构建信息:")
	fmt.Printf("  应用名   = %s\n", info.AppName)
	fmt.Printf("  版本     = %s\n", info.Version)
	fmt.Printf("  commit   = %s\n", info.Commit)
	fmt.Printf("  构建时间 = %s\n", info.BuildTime)
	fmt.Printf("  Go 版本  = %s\n", info.GoVersion)
	fmt.Printf("  平台     = %s/%s\n", info.OS, info.Arch)

	fmt.Println()
	if info.Version == "dev" {
		fmt.Println("提示:当前是未注入的 dev 构建 —— 用上面的 ldflags 命令编译才会带上真实版本号。")
	}

	// AppName 由配置覆盖 —— 平台名可配置那个需求走的就是这条
	buildinfo.AppName = "示例平台"
	fmt.Println()
	fmt.Println("运行时改 AppName 后:", buildinfo.Snapshot().AppName)
}
