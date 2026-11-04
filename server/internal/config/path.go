// 配置路径解析 —— 相对路径 → 绝对路径
// 文档 §4.3
//
// 为什么需要这一层:
//
//	config 里的 logger.dir / casbin.model_path / database.sqlite.path 都是相对路径,
//	而 viper.SetConfigFile + os.Open 都是相对【进程 cwd】解析的。
//	同一个 server 从不同目录启动(mkdir -p data && go run ./cmd/server /
//	VSCode F5 / systemd WorkingDirectory / 直接双击二进制),落盘位置就全变了 ——
//	日志散落、SQLite 建到意料之外的目录、casbin 模型找不到直接 Fatal。
//
//	这里统一在 Load() 阶段把相对路径钉死成绝对路径,让"从哪启动都一样"。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// rootDir 根目录 —— 所有相对路径的解析基准
	rootDir string
	// configFile 实际加载的配置文件绝对路径
	configFile string
)

// Root 返回配置根目录(所有相对路径的解析基准)
func Root() string { return rootDir }

// ConfigFile 返回实际加载的配置文件绝对路径
func ConfigFile() string { return configFile }

// initRoot 确定根目录
//
//	优先级 1:环境变量 APP_ROOT
//	  —— VSCode launch.json 的 env、systemd 的 EnvironmentFile 显式指定,
//	     这样即使 cwd 飘了,落盘位置仍然固定
//	优先级 2:进程 cwd
//	  —— make dev-sqlite / make run 等在 server/ 目录下执行的场景,行为与改造前一致
//
//	刻意不使用 os.Executable() 反推:main.go 通常以 `go run` 启动,
//	此时可执行文件位于系统临时目录,反推出来的路径没有意义。
func initRoot() error {
	base := os.Getenv("APP_ROOT")
	if base == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		base = wd
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return fmt.Errorf("abs root %s: %w", base, err)
	}
	rootDir = normalizeDrive(abs)
	return nil
}

// normalizeDrive Windows 盘符统一大写
//
// APP_ROOT 传 `d:/x` 和传 `D:/x` 在 Windows 上是同一个目录,
// 但打印到日志里是两种字面量,排查时按路径比对很容易误以为是两个目录。
func normalizeDrive(p string) string {
	if len(p) >= 2 && p[1] == ':' {
		return strings.ToUpper(p[:1]) + p[1:]
	}
	return p
}

// absPath 相对路径 → 绝对路径(rootDir 为基准)
//
//	空串     → 空串(保持"未配置"语义,由各消费方决定默认值)
//	已是绝对 → filepath.Clean 后原样返回(配置里写绝对路径也支持)
//	相对     → filepath.Join(rootDir, p),顺带清掉 ./ 前缀
func absPath(p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(rootDir, p)
}

// resolvePaths 把配置里所有"相对路径"字段转成绝对路径
//
//	注意基准是 rootDir(通常 = server/),**不是配置文件所在目录**。
//	因为 casbin.model_path 写的是 `configs/casbin_rbac.conf` 而不是 `casbin_rbac.conf`,
//	它本来就是相对 server/ 根的;若改成相对 configs/ 解析会变成 configs/configs/…。
//
//	必须在 Unmarshal 之后、logger.New / NewDB / casbinx.Init 之前调用,
//	否则消费方拿到的还是相对路径。
func (c *Server) resolvePaths() {
	// logger.New 在 Dir 为空时会兜底成 "logs"(相对路径),这里先补默认值再转绝对,
	// 避免绕过 absPath 又落回相对路径。
	if c.Logger.Dir == "" {
		c.Logger.Dir = "logs"
	}
	c.Logger.Dir = absPath(c.Logger.Dir)
	c.Casbin.ModelPath = absPath(c.Casbin.ModelPath)
	c.Database.SQLite.Path = absPath(c.Database.SQLite.Path)
	// 文件存储根目录。⚠️ **只在 local provider 下有意义**,但照样转绝对 ——
	// aliyun 模式下这个值根本没人读,而"没人读的字段保持相对路径"会让
	// 之后有人切回 local 时拿到一个相对 cwd 的目录(启动目录一变,文件就没了)。
	c.File.Root = absPath(c.File.Root)
}
