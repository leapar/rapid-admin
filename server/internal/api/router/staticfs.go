// 文件树的静态挂载(M80)—— 设计文档 §7
//
// 这一层就是"本地磁盘也能像 CDN 一样直接出图"的那一层:浏览器拿到
// `/rapid-admin/1/house/2/a.jpg` 就能直连,不经业务接口、不查库、不鉴权。
//
// ⚠️ **挂的是整棵树,不是某个子树** —— 存储侧只有一棵树(见 oss/path.go
// 的文件头),没有"公开/私有"两套布局。私密性靠**key 不可猜**(uid 是雪花
// id、对象名带 8 位随机后缀),不靠"存在另一个别人访问不到的目录"。
//
// 用 hertz v0.9.6 **自带的** app.FS,不引 hertz-contrib/static —— 本仓已经
// 因为它引入过一个 AGPL 依赖,不再加。
package router

import (
	"path/filepath"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
)

// mountFileStatic 把整棵文件树挂到静态路由上。
func mountFileStatic(h *server.Hertz, cfg *config.Server) {
	// ⚠️ **aliyun 模式下不挂**。文件在 OSS 上,本机磁盘上没有对应对象 ——
	// 挂出来的是切 provider 之前残留的旧树,而 URL 形状跟 OSS 直链一模一样
	// ⇒ 同一个 `/rapid-admin/...` 有两个来源,一个 200(过期内容)一个 404。
	// 详见 staticFSTarget 的注释。
	//
	// 这里显式打一条日志:**"没挂"本身是个需要被看见的状态**,否则排查时
	// 只能靠"访问 404 但日志里啥都没有"来反推。
	if !config.IsLocalProvider(cfg.File.Provider) {
		logger.L().Info("file static fs 未挂载(文件在 OSS 上,直链走 CDN / bucket host)",
			zap.String("provider", cfg.File.Provider))
		return
	}

	mount, root, strip, ok := staticFSTarget(cfg.File)
	if !ok {
		return
	}

	h.StaticFS(mount, &app.FS{
		Root:        root,
		PathRewrite: app.NewPathSlashesStripper(strip),
		// ⚠️ **GenerateIndexPages 必须显式 false**。app.FS 结构体零值是 false
		// (安全),但 hertz 的**全局默认 rootFS 是 true**(fs.go:81)—— 任何不走
		// 结构体字面量的调用方式都会开出目录列表,而那等于把所有文件名公开
		// 枚举给任何拿到 URL 的人。
		GenerateIndexPages: false,
		// ⚠️ **Compress 必须显式 false**。开启后 hertz 会把压缩缓存**写回 Root
		// 目录**(fs.go:141 注释:需要 write access)。文件存储目录通常该只读挂载,
		// 而且会在存储目录里凭空多出一批 `.gz`,`DeletePrefix` 和配额对账都会被
		// 这些没进过 sys_file 的文件干扰。
		Compress: false,
		// Range 要它:视频拖进度条、下载断点续传都靠 206。
		AcceptByteRange: true,
	})

	logger.L().Info("file static fs mounted",
		zap.String("mount", mount),
		zap.String("root", root),
		zap.Int("strip_segments", strip))
}

// staticFSTarget 算出静态挂载的参数。**提成纯函数是为了让测试能直接断言
// root / strip 的值** —— 只靠 HTTP 往返反推的话,"Root 挂高了"这种错会被
// 路由模式挡掉,测试假绿。
//
// ⚠️ **只有 local provider 才挂**。文件在 OSS 上时,对象不在本机磁盘上,
// 挂出来的是一棵**残留的本地旧树**(切 provider 之前存的文件),而 URL 形状
// 跟 OSS 直链一模一样(`/rapid-admin/...`)⇒ 同一个 URL 有两个来源:
//   - 本机磁盘上有的旧文件 → 200(但 OSS 上没有,内容是过期的)
//   - 只有 OSS 上有的新文件 → 404(本机没有)
// 症状是"切到 OSS 之后有的文件能开有的不能,而且是随机的那几个",
// 而日志里只有一条 "file static fs mounted"。
//
// ⚠️ 而且**挂载点跟 OSS 直链的前缀同名**:local 的直链是 `/rapid-admin/...`
// (同源),OSS 的直链是 `https://cdn/.../rapid-admin/...` —— 第一段相同是
// 单桶单树模型的要求,不是巧合。所以"要不要挂本机这棵树"必须由 provider
// 决定,不能靠"文件名不冲突"蒙混过去。
func staticFSTarget(cfg config.FileConfig) (mount, root string, strip int, ok bool) {
	// ⚠️ **空 = local**(见 config.FileConfig.Provider 的注释),所以判定是
	// "不是 local 就不挂",而不是"等于 local 才挂" —— 后者会在 yaml 漏写
	// provider 时把本机磁盘树挂出去(而文件其实在 OSS 上)。
	if !config.IsLocalProvider(cfg.Provider) {
		return "", "", 0, false
	}

	pathRoot := strings.Trim(strings.TrimSpace(cfg.PathRoot), "/")
	if pathRoot == "" {
		return "", "", 0, false
	}

	// ⚠️ **挂载点 = key 的第一段**。磁盘上是 `<file.root>/rapid-admin/...`,
	// URL 是 `/rapid-admin/...` —— 同一个 path_root 推出两处,不可能对不齐。
	mount = "/" + pathRoot

	// Root = <file.root>/<path_root>。
	//
	// ⚠️ **必须带 path_root**:local provider 是 `filepath.Join(自己的Root, key)`
	// 原样落盘 —— 所以磁盘路径是 `<file.root>/rapid-admin/...`。只写
	// `Join(root, "")` 的话挂载点指向 `<root>`,而文件在 `<root>/rapid-admin`,
	// **请求会命中路由、然后 404**,而且没有任何报错。
	root = filepath.Join(cfg.Root, pathRoot)

	// PathRewrite 的剥除段数 = **挂载路径的 URL 段数** = 1(剥掉 "rapid-admin")。
	//
	// ⚠️ **绝对不能用磁盘路径的段数代替**:`<root>` 在开发机上是
	// `D:/data/rapid-admin/server/data/files`,7 段;URL 只有 1 段。
	// 段数算大了会把 URL 的后几段一起砍掉(文件永远找不到),算小了会把
	// "rapid-admin" 当成磁盘子目录(更隐蔽:路径存在性取决于 root 下恰好
	// 有没有 rapid-admin/)。
	return mount, root, urlSegments(mount), true
}

// urlSegments 挂载路径的 URL 段数。"/rapid-admin" → 1,"/a/b" → 2。
func urlSegments(mount string) int {
	s := strings.Trim(mount, "/")
	if s == "" {
		return 0
	}
	return strings.Count(s, "/") + 1
}
