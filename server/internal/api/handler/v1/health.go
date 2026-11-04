// 健康检查 handler — /healthz(liveness)+ /readyz(readiness)
// 文档 §1.3 #12 + §1.8 #9 P0
package v1

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// HealthHandler 健康检查
//   - Liveness(/healthz): 进程活就 OK,用于 k8s livenessProbe
//   - Readiness(/readyz): 探 DB + Redis,任一不可用 → 503,用于 k8s readinessProbe / LB 健康检查
//
// 设计取舍:
//   - /healthz 不查 DB,只返 200 — liveness probe 卡 DB 会导致 DB 短暂不可用时 pod 被全杀
//   - /readyz 超时 500ms/probe,总 1.2s — 比典型 LB 探活超时(3s)短
//   - Redis 为 nil(未配置)时该检查视为"disabled"而非 fail(本地 dev 模式常无 Redis)
type HealthHandler struct {
	db     *gorm.DB
	rdb    *redis.Client
}

// NewHealthHandler 构造
//   - db  / rdb 可为 nil:nil 时该 probe 报"disabled",不阻塞 readiness
func NewHealthHandler(db *gorm.DB, rdb *redis.Client) *HealthHandler {
	return &HealthHandler{db: db, rdb: rdb}
}

// Liveness 探活 — 仅判断进程是否响应 HTTP,不走任何依赖
// GET /healthz → 200 {"status":"ok"}
func (h *HealthHandler) Liveness(_ context.Context, c *app.RequestContext) {
	c.JSON(200, map[string]string{"status": "ok"})
}

// Readiness 探活 — DB SELECT 1 + Redis PING,任一失败/超时 → 503
// GET /readyz → 200/503 {"status":"ready|not_ready","checks":{"db":"ok|timeout|disabled|error:...","redis":"..."}}
func (h *HealthHandler) Readiness(ctx context.Context, c *app.RequestContext) {
	probeCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()

	checks := map[string]string{}
	overallOK := true

	// ---- DB ----
	if h.db == nil {
		checks["db"] = "disabled"
		// nil DB 视为不可用(readiness 应当 fail,防止带病服务被 LB 拉入)
		overallOK = false
	} else {
		probeDB, cancelDB := context.WithTimeout(probeCtx, 500*time.Millisecond)
		err := h.db.WithContext(probeDB).Raw("SELECT 1").Error
		cancelDB()
		switch {
		case err == nil:
			checks["db"] = "ok"
		case isTimeoutErr(err):
			checks["db"] = "timeout"
			overallOK = false
		default:
			checks["db"] = "error: " + err.Error()
			overallOK = false
		}
	}

	// ---- Redis ----
	if h.rdb == nil {
		// Redis 未配置(本地 dev 模式)→ 视为 OK,不阻塞 readiness
		checks["redis"] = "disabled"
	} else {
		probeRedis, cancelRedis := context.WithTimeout(probeCtx, 500*time.Millisecond)
		err := h.rdb.Ping(probeRedis).Err()
		cancelRedis()
		switch {
		case err == nil:
			checks["redis"] = "ok"
		case isTimeoutErr(err):
			checks["redis"] = "timeout"
			overallOK = false
		default:
			checks["redis"] = "error: " + err.Error()
			overallOK = false
		}
	}

	body := map[string]any{
		"status": "ready",
		"checks": checks,
	}
	code := 200
	if !overallOK {
		body["status"] = "not_ready"
		code = 503
	}
	c.JSON(code, body)
}

// isTimeoutErr 判断是否 context deadline / i/o timeout
//   - 跨驱动通用:context.DeadlineExceeded、net.Error.Timeout、redis.Nil 之外的超时
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if err == context.DeadlineExceeded {
		return true
	}
	// gorm MySQL/SQLite driver 都 wrap net.Error
	if t, ok := err.(interface{ Timeout() bool }); ok && t.Timeout() {
		return true
	}
	// gorm 自己的 ErrInvalidDB / driver.ErrBadConn 不算 timeout
	return false
}
