// logger —— 结构化日志
//
// ## 这个包解决什么问题
//
// zap 之上的一层薄封装,解决三件事:
//
//   - 三个 logger 分工:主日志 / 访问日志 / 审计日志
//   - 字段化日志(zap.Field)而不是拼字符串
//   - 落盘 + 按大小滚动 + 压缩
//
// ## 关键点
//
//   - **audit / access 恒为 JSON** —— 这两份是给机器读的(审计检索、访问日志分析),
//     人读的 console 格式只给 dev/test
//   - **每个 core 一份自己的 encoder**:encoder 内部持有 buffer
//     (`With` 追加的字段就存在那儿),共享一个实例 = 共享可变状态
//   - dev/test 环境下额外写一份到 stdout,便于本地看;
//     **access / audit 两份只落文件,不打 stdout** —— 它们是给机器读的
//   - Logger.Must(l, err) —— 初始化失败时不想处理错误时的快捷写法
//
// ## 跑法
//
//	go run ./examples/logger
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

func main() {
	// 日志落盘到临时目录,避免在仓库里留下 logs/
	dir := filepath.Join(os.TempDir(), "logger-example")

	l, err := logger.New(logger.Config{
		Level:      "debug",
		Format:     "console",
		Dir:        dir,
		MaxSizeMB:  10,
		MaxBackups: 3,
		MaxAgeDays: 7,
		Env:        "dev",
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = logger.Close() }()

	// 一、主日志
	l.Info("服务启动",
		zap.String("app", "示例"),
		zap.Int("port", 8080),
		zap.Duration("uptime", 3*1e9))

	// 二、访问日志(给机器读)
	logger.Access().Info("GET /rapid-admin/v1/system/users",
		zap.String("method", "GET"),
		zap.String("path", "/rapid-admin/v1/system/users"),
		zap.Int("status", 200),
		zap.Int64("cost_ms", 12))

	// 三、审计日志(给机器读,谁在什么时候动了什么)
	logger.Audit().Info("删除用户",
		zap.Uint64("actor_id", 1001),
		zap.String("actor_name", "zhangwei"),
		zap.String("target", "sys_user"),
		zap.Uint64("target_id", 2002),
		zap.String("ip", "10.0.0.7"))

	// 四、结构化错误:Error 级别会自动带堆栈
	l.Error("数据库写入失败",
		zap.String("table", "sys_user"),
		zap.Error(os.ErrPermission))

	// 五、Must:不想处理 error 时的快捷写法
	logger.Must(l, nil).Warn("这是一条 warn")

	// 刷盘 + 关文件
	_ = logger.SyncFlush()
	fmt.Println("日志已写到:", dir)
}
