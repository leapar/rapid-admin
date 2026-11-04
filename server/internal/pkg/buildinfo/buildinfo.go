// Package buildinfo 编译期构建信息(§1.8 补 P2)
//
// 目的:
//   - 提供 /version 端点返 {version, commit, buildTime, goVersion, appName}
//   - 通过 ldflags 注入(server/build 时):
//     go build -ldflags "-X rapid-admin/internal/pkg/buildinfo.Version=v1.2.3 \
//                        -X rapid-admin/internal/pkg/buildinfo.Commit=$(git rev-parse --short HEAD) \
//                        -X rapid-admin/internal/pkg/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//   - 未注入时:Version="dev" / Commit="unknown" / BuildTime="unknown"
//
// 用法:
//
//	// main.go
//	buildinfo.AppName = cfg.System.AppName
//	buildinfo.Version = "1.0.0"  // 或 ldflags 注入
//
//	// handler
//	c.JSON(200, buildinfo.Info())
package buildinfo

import "runtime"

// 编译期可被 ldflags 覆盖的变量
var (
	AppName   = "rapid-admin"
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Info 一站式构建信息快照
type Info struct {
	AppName   string `json:"app_name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Snapshot 当前构建信息(只读)
func Snapshot() Info {
	return Info{
		AppName:   AppName,
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}
