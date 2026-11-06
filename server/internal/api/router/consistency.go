// 启动检查 — sys_resource.code 与 rbac() 调用一致性保障
// 文档 §1.1 #15 P0
package router

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/service"
)

// rbac 闭包触发的注册期记账状态
//
// 设计取舍:为什么不用反射 / 读 Hertz radix tree?
//   - Hertz 路由表只存 handler 函数指针,无法反查"这条路由当初是用哪个 rbac 注册的"
//   - 反射 handler 链侵入性强,且 private 类型在 cloudwego 内部,易碎
//   - 注册期包级记账最简单,O(1) 无锁(只主线程跑),够用
//
// 线程模型:仅在 Register() 内由主线程写入,CheckResourceConsistency() 由 main 协程在
// 路由注册完后、h.Spin() 前读;无并发。
var (
	rbacTrackingMu sync.Mutex
	rbacCodeSet    = make(map[string]struct{}) // unique code → 已用
	rbacCallCount  int                         // rbac() 调用次数(含重复 code)
	rbacCallSites  = make(map[string][]string) // code → 调用次数(便于报告)
)

// resetRBACTracking 每次 Register 入口重置 — 防止单测多次 Register 累积脏数据
func resetRBACTracking() {
	rbacTrackingMu.Lock()
	defer rbacTrackingMu.Unlock()
	rbacCodeSet = make(map[string]struct{})
	rbacCallCount = 0
	rbacCallSites = make(map[string][]string)
}

// trackRBAC rbac 闭包侧记账 — 每次 rbac("X","Y") 调用一次
func trackRBAC(module, action string) string {
	code := module + ":" + action
	rbacTrackingMu.Lock()
	rbacCodeSet[code] = struct{}{}
	rbacCallCount++
	rbacCallSites[code] = append(rbacCallSites[code], code)
	rbacTrackingMu.Unlock()
	return code
}

// RBACCodesProvider 给 swaggerx 等非 router 包的访问入口
// 返回 map[code]bool(已去重),读 rbacTracking 不持锁(只注册期写、读时可重复读)
func RBACCodesProvider() map[string]bool {
	rbacTrackingMu.Lock()
	defer rbacTrackingMu.Unlock()
	out := make(map[string]bool, len(rbacCodeSet))
	for c := range rbacCodeSet {
		out[c] = true
	}
	return out
}

// rbacCodesSnapshot 给 CheckResourceConsistency 用的一致性快照
type rbacCodesSnapshot struct {
	UniqueCodes []string
	CallCount   int
}

// snapshotRBACCodes 拷一份当前状态(不持锁传给 DB 查询阶段)
func snapshotRBACCodes() rbacCodesSnapshot {
	rbacTrackingMu.Lock()
	defer rbacTrackingMu.Unlock()
	out := make([]string, 0, len(rbacCodeSet))
	for c := range rbacCodeSet {
		out = append(out, c)
	}
	sort.Strings(out)
	return rbacCodesSnapshot{UniqueCodes: out, CallCount: rbacCallCount}
}

// CheckResourceConsistency 启动时校验
//   - 1. rbac() 调用的所有 code 必须存在于 sys_resource.code
//     → 漏 seed / typo / 复制粘贴失误 都会让生产路由直接 403
//   - 2. /rapid-admin/v1/* 下受保护路由数 == rbac() 调用次数
//     → 漏挂 rbac 立刻可见(几乎所有受保护 route 都恰好 1 次 rbac)
//   - 3. sys_resource 中 type='api' 但无任何 route 引用的 code → 仅 info 日志
//     → seed 里 button.* 不进此检查,只查 type=api
//
// 失败策略:missing code (1) 返回 error,启动 fail-fast;
// 计数不匹配 (2) 返回 error;
func CheckResourceConsistency(h *server.Hertz, db *gorm.DB) error {
	if h == nil {
		return fmt.Errorf("consistency: nil hertz")
	}
	if db == nil {
		return fmt.Errorf("consistency: nil db")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// ---- 1. DB 端 known codes ----
	var knownCodes []string
	if err := db.WithContext(ctx).
		Model(&model.SysResource{}).
		Where("is_active = ?", true).
		Pluck("code", &knownCodes).Error; err != nil {
		return fmt.Errorf("consistency: load sys_resource.code: %w", err)
	}
	knownSet := make(map[string]struct{}, len(knownCodes))
	for _, c := range knownCodes {
		knownSet[c] = struct{}{}
	}

	// ---- 2. 路由端 registered codes ----
	snap := snapshotRBACCodes()

	// 检查 (1):rbac 用的 code 必须在 seed
	var missing []string
	for _, c := range snap.UniqueCodes {
		if _, ok := knownSet[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("consistency: rbac() 引用了 sys_resource 中不存在的 code: %s",
			strings.Join(missing, ", "))
	}

	// rbacExemptRoutes —— 受保护路径但**故意不挂 rbac()** 的路由(唯一合法出口)
	//
	// 收窄判据,两条都要满足才允许加进来:
	//  1. 该路由已有等价的鉴权手段(SSEAuth / 自定义中间件 / handler 内判),不是裸奔;
	//  2. "有没有某个资源权限"对它没有业务含义 —— 强行绑一个 code 只会制造
	//     运营看不懂、也排障困难的角色配置。
	//
	// 判据 2 的**第三种情形**(2026-11-01 M151 补):权限码**在请求期才确定**。
	// 典型是动态业务集合 —— 资源码 `dyn:<集合编码>:<动作>` 里的集合编码来自
	// URL 路径参数,注册期不存在 ⇒ rbac() 闭包(编译期常量)钉不出来。
	// 这时"钉一个固定码"正是判据 2 要防的那种运营看不懂的粗粒度闸门,
	// 而真正的权限码(逐集合那套)是**存在**的、只是没法在注册期写死。
	// ⇒ 这种路由进豁免清单,但必须在条目里写清"替代鉴权方式"。
	//
	// 每加一条必须写清替代鉴权方式。往这张表里塞路由 == 主动放弃该接口的权限校验,
	// 检查会同步校验路径有效性(路由改名/删除时报错),防止豁免静默失效或变成死条目。
	var rbacExemptRoutes = []string{
		// GET /rapid-admin/v1/events —— 通用事件推送 SSE 通道(权限实时刷新的加强版)
		//
		//	替代鉴权:middleware.SSEAuth()(解析 JWT + 查 token 黑名单 + 注入 Caller,
		//	          见 internal/api/middleware/sseauth.go);不能用普通 Auth(),
		//	          因为 EventSource 无法自定义 Header,只能走 ?access_token= / Cookie。
		//	          ⚠️ 三级兜底里的 Cookie 那一级目前是**不可达**的:全仓前后端都没有
		//	          任何一处 SetCookie / document.cookie,Cookie 永远不存在,
		//	          实际只有 ?access_token= 这一条路走得通(前端见 useEventStream.ts)。
		//	为何不需要 rbac 闭包:一条长连接不该占着鉴权链(鉴权语义是"每次请求",
		//	          SSE 是"一次鉴权、长期持有")。⚠️ 但权限**不是不判** ——
		//	          handler 内部用 middleware.CanAccess 逐 topic 判定订阅范围,
		//	          见 handler/v1/event.go subscribableTopics:
		//	          定时任务要 job:read、审批要 approval:read,
		//	          客户端不能自选 topic(请求里传了也不读)。
		//	          "有没有某个资源权限"对这条路由本身没有意义,所以不绑单一 code。
		"/rapid-admin/v1/events",

		// ---- 实时语音识别 WS 通道(2026-11-06)----
		//
		//	GET /rapid-admin/v1/biz/asr/stream
		//
		//	替代鉴权:middleware.SSEAuth()(Authorization / ?access_token= / cookie
		//	          三级兜底,完整三道吊销闸)+ handler 内 super_admin 短路
		//	          —— 识别是花钱的外呼,与克隆同档;浏览器 WebSocket 握手带不了
		//	          Authorization 头,普通 Auth() 会先 401(同 /events 的教训)。
		//	不能用 rbac():长连接不该占鉴权链;asr:stream 码等菜单/矩阵轮次再配。
		"/rapid-admin/v1/biz/asr/stream",

		// ---- 动态业务集合的数据面(M151)----
		//
		//	GET    /rapid-admin/v1/biz/dyn/:collection/schema
		//	GET    /rapid-admin/v1/biz/dyn/:collection/records
		//	GET    /rapid-admin/v1/biz/dyn/:collection/records/:id
		//	POST   /rapid-admin/v1/biz/dyn/:collection/records
		//	PATCH  /rapid-admin/v1/biz/dyn/:collection/records/:id
		//	DELETE /rapid-admin/v1/biz/dyn/:collection/records/:id
		//
		//	替代鉴权:handler 内 middleware.CanAccess(资源码 `dyn:<集合编码>:<动作>`)
		//	          —— 集合编码来自 **URL 路径参数**,注册期不存在 ⇒ rbac() 闭包
		//	          (编译期常量)钉不出"每个集合一条码"。
		//	          见 handler/v1/dyn.go 的 dynGuard。
		//
		//	⚠️ **这里是"判据 2"的第三种情形**(前两种是"没有等价的资源权限含义"
		//	和"长连接不该占着鉴权链"):权限码**逐集合不同**。
		//	钉一个固定码(如 `dyn:record:list`)会变成运营看不懂的粗粒度闸门 ——
		//	管理员得同时配"通用动态记录"和"某个业务表"两套码,少配一套就 403,
		//	而且报错说的码跟他在角色配置页看到的对不上。
		//	⇒ 真正的权限码就是 `dyn:<集合编码>:<动作>`,一个集合一套,
		//	在「权限资源」页里看得见、勾得动。
		//
		//	⚠️ **豁免的是"计数",不是"鉴权"** —— 这 6 条路由照样过 Auth / Principal /
		//	CSRF / Audit,权限由 handler 判(见上)。豁免的只是"受保护路由数 ==
		//	rbac() 调用次数"这条计数:它们没有 rbac() 调用,计进去就对不上。
		//	⇒ 将来往 /biz/dyn/** 下加第 7 条路由却忘了改这张表 ⇒ 启动失败(响亮)。
		//
		//	2026-11-03 扩到 11 条(行级批量 + 导入导出):export / batch-delete /
		//	batch-update / import / records/:id/restore —— 鉴权方式不变(handler 内
		//	dynGuard 按集合码判 `dyn:<码>:<动作>`),只是路径多了 5 条。
		//
		//	2026-11-03 再扩到 12 条:records/:id/approval(送审)——
		//	权限沿用 dyn:<码>:update,不加新码。
		"/rapid-admin/v1/biz/dyn/:collection/schema",
		"/rapid-admin/v1/biz/dyn/:collection/records",
		"/rapid-admin/v1/biz/dyn/:collection/records/:id",
		"/rapid-admin/v1/biz/dyn/:collection/records/:id/restore",
		"/rapid-admin/v1/biz/dyn/:collection/records/:id/approval",
		"/rapid-admin/v1/biz/dyn/:collection/records/export",
		"/rapid-admin/v1/biz/dyn/:collection/records/batch-delete",
		"/rapid-admin/v1/biz/dyn/:collection/records/batch-update",
		"/rapid-admin/v1/biz/dyn/:collection/records/import",
	}

	// 检查 (2):/rapid-admin/v1/* 受保护路由数 == rbac() 调用次数
	routes := h.Routes()
	var protectedCount int
	publicPrefixes := []string{"/healthz", "/readyz", "/rapid-admin/v1/public"}
	protectedPrefixes := []string{
		"/rapid-admin/v1/system", "/rapid-admin/v1/biz", "/rapid-admin/v1/platform", "/rapid-admin/v1/auth",
	}
	exempt := make(map[string]struct{}, len(rbacExemptRoutes))
	for _, p := range rbacExemptRoutes {
		exempt[p] = struct{}{}
	}
	registered := make(map[string]struct{}, len(routes))
	for _, r := range routes {
		registered[r.Path] = struct{}{}
	}
	// 豁免清单自身的卫生检查:路径写错(路由改名/删除)会让豁免静默失效,
	// 而失效方向是"重新开始计数报错",不至于漏检;但死条目会误导后来人,
	// 所以这里显式报错,逼着更新。
	//
	// ⚠️ 这条检查(而不是 protectedPrefixes)是"豁免路由不许悄悄消失"的唯一保证:
	// 下面统计循环里豁免项在 isProtectedRoute **之前**就被 continue 掉了,
	// 所以往 protectedPrefixes 里加豁免路由是死代码,加了不会报错、也不会被读到。
	for _, p := range rbacExemptRoutes {
		if _, ok := registered[p]; !ok {
			return fmt.Errorf("consistency: 豁免清单里的 %s 已不是有效路由(改名或删除了?),请同步 rbacExemptRoutes", p)
		}
	}
	for _, r := range routes {
		if isPublicRoute(r.Path, publicPrefixes) {
			continue
		}
		if _, ok := exempt[r.Path]; ok {
			continue
		}
		if isProtectedRoute(r.Path, protectedPrefixes) {
			protectedCount++
		}
	}
	if protectedCount != snap.CallCount {
		return fmt.Errorf("consistency: 受保护路由数 %d ≠ rbac() 调用次数 %d "+
			"(可能漏挂 rbac 中间件;当前豁免清单: %v)",
			protectedCount, snap.CallCount, rbacExemptRoutes)
	}

	// 检查 (3):DB 里 type=api 但无 route 引用 — 仅 info
	var apiCodes []string
	if err := db.WithContext(ctx).
		Model(&model.SysResource{}).
		Where("is_active = ? AND type = ?", true, "api").
		Pluck("code", &apiCodes).Error; err != nil {
		// 不 fail,只 warn — 这是 informational
		zap.L().Warn("consistency: load api-type codes failed", zap.Error(err))
	} else {
		registered := make(map[string]struct{}, len(snap.UniqueCodes))
		for _, c := range snap.UniqueCodes {
			registered[c] = struct{}{}
		}
		var orphan int
		for _, c := range apiCodes {
			if _, ok := registered[c]; !ok {
				orphan++
			}
		}
		if orphan > 0 {
			zap.L().Info("consistency: api-type code 未被任何 route 引用(seed 预留/可清理)",
				zap.Int("orphan_count", orphan),
				zap.Int("api_total", len(apiCodes)),
				zap.Int("registered", len(snap.UniqueCodes)),
			)
		}
	}

	// 检查 (4):看板的域权限码必须在资源池里(2026-11-01)
	//
	//	看板的可见性靠 middleware.CanAccess(资源码)判 —— 这些码**没经过 rbac() 闭包**
	//	(它们不是路由守卫,是"这块**内容**能不能看")⇒ 检查 (1) 扫不到它们。
	//	码不在池里 ⇒ 那个域**永远不显示**,而且**不报错**:
	//	服务照常起、页面照常开,只是少了一块 —— 这正是检查 (1) 要拦的那类静默洞。
	if err := checkDashboardDomainCodes(knownSet); err != nil {
		return err
	}

	zap.L().Info("consistency check passed",
		zap.Int("registered_routes", len(routes)),
		zap.Int("protected_routes", protectedCount),
		zap.Int("rbac_unique_codes", len(snap.UniqueCodes)),
		zap.Int("rbac_call_count", snap.CallCount),
		zap.Int("seed_codes_total", len(knownCodes)),
	)
	return nil
}

// checkDashboardDomainCodes 看板的域权限码必须在资源池里
//
// ⚠️ 单独拆成函数(而不是内联在 CheckResourceConsistency 里)——
// 反向测试要能**只**喂一个"缺了某个码"的集合进来:
// 走整条 CheckResourceConsistency 的话,缺的那个码多半同时被某条
// rbac() 路由引用 ⇒ 检查 (1) 先炸 ⇒ 测到的不是这一条。
func checkDashboardDomainCodes(knownSet map[string]struct{}) error {
	for _, d := range service.DashboardDomains() {
		if _, ok := knownSet[d.Code]; !ok {
			return fmt.Errorf("consistency: 看板域权限码 %s 不在 sys_resource 里 —— 该域永远不显示(且不报错)", d.Code)
		}
	}
	return nil
}

func isPublicRoute(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func isProtectedRoute(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
