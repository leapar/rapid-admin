// 中间件包入口 —— 暴露全局依赖 + 通用工具
// 文档 §10
// 各职责拆分为独立文件:
//   trace.go     请求 ID / TraceID
//   recover.go   panic 恢复
//   access_log.go 访问日志
//   auth.go      JWT 鉴权 + caller 注入
//   principal.go L0.5 Principal 加载(grant/override/feature flag)
//   rbac.go      Casbin 权限 + 项目域 + IDOR
//   ratelimit.go 滑动窗口限流 + 登录失败锁定
//   audit.go     操作审计
//   csrf.go      CSRF token 校验
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/casbin/casbin/v3"
	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/service"
)

// 全局依赖(由 main 注入)
var (
	Cfg             *config.Server
	JWTs            *auth.Multi
	Enforcer        *casbin.Enforcer
	AuditSvc        service.AuditService
	PrincipalLoader authz.Loader // L0.5 — nil 时跳过 principal 加载(降级为纯 role/user)
	// AuthSvc 会话活动心跳(M117-4)—— 用来更新 sys_login_log.last_active_at
	//
	// ⚠️ 类型是**窄接口**而不是 service.AuthService:
	//   中间件只用到 TouchLoginSession 一个方法,窄接口让单测能塞个假实现进来,
	//   不必把 AuthService 那二十来个方法都抄一遍。
	AuthSvc SessionToucher
	// SystemLogSvc 系统运行日志写入方(慢接口 / 5xx / panic)—— 2026-11-03
	//
	// ⚠️ 类型是**窄接口**而不是 service.AuditService:中间件只用到
	//   RecordSystemLog 一个方法,窄接口让单测能塞假实现,不必抄二十个方法。
	//   未装配(nil)时中间件**静默跳过** —— 系统日志是可观测性辅助,
	//   不能因为它没装配就把请求判失败。
	SystemLogSvc SystemLogWriter
)

// SystemLogWriter 系统运行日志写入方(窄接口)
type SystemLogWriter interface {
	RecordSystemLog(entry *service.SystemLogEntry)
}

// SetSystemLogWriter 注入系统运行日志写入方(与 SetAuditService 同一装配点:
// router.Register 单一装配 —— main 不需要记得传)
//
// ⚠️ nil 时只 Warn 不 Error —— 后果是"慢接口 / 5xx / panic 不进 sys_system_log",
// zap 文件日志照旧,属于降级不属于事故(SetAuditService 那是审计 DB 轨,量级不同)。
func SetSystemLogWriter(w SystemLogWriter) {
	SystemLogSvc = w
	if w == nil {
		L().Warn("系统日志写入方未装配 —— sys_system_log 将为空(慢接口 / 5xx / panic 只进 zap 文件)",
			zap.String("修法", "router.Register 里的 middleware.SetSystemLogWriter(svc.Audit)"))
	}
}

// SessionToucher 会话活动心跳(M117-4)
//
// 只声明中间件真正用到的那一个方法。
type SessionToucher interface {
	// TouchLoginSession 记一次"这条会话刚刚有活动"
	TouchLoginSession(ctx context.Context, caller *service.Caller) error
}

// SetGlobal 注入全局(由 main.go 启动后调用)
//
// ⚠️ **2026-10-30:audit 参数被删了** ——
//
// 它原来在第 4 位,main.go 传的是 `nil`,而且**全仓再没有第二处赋值**:
// 于是 `AuditSvc` 永远是 nil,audit 中间件的 DB 轨
// (`if AuditSvc == nil { return }`)每次都提前返回 ——
//
//	**sys_operation_log 一行都没被中间件写过。**
//
// 表现是"页面正常、功能正常,只是操作日志永远是空的",而且**零报错**。
//(dev 库里那 11 行全是 service 层直写的,不是中间件写的。)
//
// 删参数不是为了少一个参数,是为了**让"忘了装配"变成编译错误** ——
// 参数还在,编译器不会提醒你传了 nil;参数没了,`SetGlobal(...)` 调用点
// 必须改,改的时候就会看见 audit 该从哪来。
//
// 新的装配入口是 `SetAuditService`,由 `router.Register` 调用(它手里有
// `*service.Registry`),见下面那条函数的注释。
func SetGlobal(cfg *config.Server, jwt *auth.Multi, enf *casbin.Enforcer) {
	Cfg = cfg
	JWTs = jwt
	Enforcer = enf
}

// SetAuditService 注入 DB 审计服务(由 router.Register 调用)
//
// # 为什么放在 Register 而不是 main
//
// `Register(h, cfg, svc *service.Registry, db, rdb)` **本来就拿到了 registry**,
// 而 `registry.Audit` 就在里面。在 Register 里装配 = **单一装配点**:
// main 不需要记得传,中间件也不需要"等谁注入"。
//
// ⚠️ 这跟 `SetPrincipalLoader` 的做法不一样(那个是 main 里单独调一次),
//   原因是 Principal loader 需要在 Register **之前**换实现(Redis 缓存版),
//   而 audit 服务没有这种"运行期替换"的需求。
//
// # nil 的两种来源,处理方式不同
//
//   - `svc == nil`(单测场景:Register 只挂 handler 引用,不调 service 方法)
//     → 跳过装配,不报错。单测不装配审计是合理的。
//   - `svc != nil` 但 `svc.Audit == nil`(registry 漏填字段)
//     → **打 Error 日志**。这是真事故:整个平台的写操作都不会进操作日志。
//     不 panic —— 平台不能因为审计没装配而起不来(那是更大的事故),
//     但必须喊到日志里,不能默默吞掉。
func SetAuditService(audit service.AuditService) {
	AuditSvc = audit
	if audit == nil {
		L().Error("审计服务未装配 —— 操作日志(sys_operation_log)将全部丢失",
			zap.String("影响", "所有 POST/PUT/PATCH/DELETE 请求不留 DB 审计痕迹"),
			zap.String("修法", "router.Register 里的 middleware.SetAuditService(svc.Audit)"))
	}
}

// SetPrincipalLoader 单独注入 L0.5 loader(与 SetGlobal 分开调 ——
// 主链路的依赖要在 Register 之前换实现)
func SetPrincipalLoader(l authz.Loader) { PrincipalLoader = l }

// SetAuthService 注入会话活动心跳的写入方(由 router.Register 调用)
//
// # 与 SetAuditService 同一装配点
//
// Register 本来就拿着 registry,`registry.Auth` 就在里面 ——
// 放这里是**单一装配点**:main 不需要记得传,也没有"忘了注入"这种可能。
//
// # 为什么 nil 时只 Warn 不 Error
//
// SetAuditService 在 nil 时打 Error,理由是"审计 DB 轨缺失 = 所有写操作没有痕迹",
// 那是另一个量级的事。鉴权服务为 nil 的后果只是"会话活动不再更新"
// (活跃设备列表的精度),**不影响任何放行判定** ⇒ Warn 足够。
//
// ⚠️ nil 时中间件会**静默跳过心跳**,不报错、不影响请求 ——
// 心跳是可选的记账,不能因为它没装配就把请求判失败。
func SetAuthService(a SessionToucher) {
	AuthSvc = a
	if a == nil {
		L().Warn("鉴权服务未装配 —— 会话活动心跳(last_active_at)不再更新",
			zap.String("影响", "活跃设备列表的「最后活动」精度下降,不影响任何放行判定"),
			zap.String("修法", "router.Register 里的 middleware.SetAuthService(svc.Auth)"))
	}
}

// L 全局 logger
func L() *zap.Logger { return logger.L() }

// ===== 通用工具 =====

// RandomID 生成 n 字节十六进制随机串
func RandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 退化方案
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now & 0xff)
			now >>= 4
		}
	}
	return hex.EncodeToString(b)
}
