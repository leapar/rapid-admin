// Package cryptaudit 密钥使用审计框架级 helper —— §1.5 #8 P1
//
// 设计:与 piiaudit/smsaudit 同一模式(异步批量 + 失败重试 + nil-safe)
//   - Init 阶段 main.go 调一次,Shutdown 退出前调
//   - 业务代码解密/加密成功后调 Record(...) 一行即可
//   - 失败也落库(便于排查"哪个 key 解不开"——直接刷到 grep error_msg)
//
// 与 piiaudit 的区别:
//   - piiaudit 专盯 PII 字段,本包是"所有加密/解密"的全量记录
//   - 单条信息更精简(只 key/op/资源/字节数,不含 PII 字段名)
//
// 数据来源(同 piiaudit):
//   - principal:authz.PrincipalFromContext(由 principal middleware 注入)
//   - request_id / trace_id:ctx(由 access_log + otel middleware 注入)
package cryptaudit

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/piiaudit" // 复用 request_id/trace_id ctx key
	"rapid-admin/internal/pkg/sonyflake"
)

// Event 单条审计事件
type Event struct {
	KeyID        string // 密钥标识(env var 名 / KMS key id)
	KeyVersion   int    // 密钥版本(轮转用)
	Algorithm    string // "aes-256-gcm" / "hmac-sha256" / ...
	Operation    string // "encrypt" / "decrypt" / "blind-index"
	ResourceType string // "owner" / "house" / ...
	ResourceID   uint64
	ByteCount    int
	Success      bool
	ErrorMsg     string
}

var (
	once        sync.Once
	shutdownOne sync.Once
	db          *gorm.DB
	batch       []model.SysCryptoAuditLog
	mu          sync.Mutex
	stopCh      chan struct{}
	doneCh      chan struct{}
	tick        = 5 * time.Second
	maxTry      = 3
)

// Init 启动期调一次,db 可为 nil(测试/无 DB 模式)
//
// ⚠️ M100(2026-10-26):db 为 nil 时打一条 **warn** ——
// 密钥使用审计是 §1.5 #8 的合规项,"静默关掉"必须留痕。
// (piiaudit 一直有这条 warn,本包之前没有 —— 同一件事两种口径。)
func Init(d *gorm.DB) {
	once.Do(func() {
		db = d
		if db == nil {
			logger.L().Warn("cryptaudit: db is nil, recorder disabled")
		}
		stopCh = make(chan struct{})
		doneCh = make(chan struct{})
		go run()
	})
}

// Shutdown 退出前调,等最后一批 flush 完
//
// ⚠️ M100(2026-10-26):改成**幂等**(重复调用会 panic:close of closed channel)
func Shutdown() {
	if stopCh == nil {
		return
	}
	shutdownOne.Do(func() {
		close(stopCh)
		<-doneCh
	})
}

func run() {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stopCh:
			flush()
			close(doneCh)
			return
		case <-t.C:
			flush()
		}
	}
}

func flush() {
	mu.Lock()
	if len(batch) == 0 {
		mu.Unlock()
		return
	}
	cur := batch
	batch = nil
	mu.Unlock()
	if db == nil {
		return
	}
	for i := 0; i < maxTry; i++ {
		if err := db.CreateInBatches(cur, 50).Error; err != nil {
			if i == maxTry-1 {
				logger.L().Warn("cryptaudit flush failed",
					zap.Int("count", len(cur)),
					zap.Error(err))
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return
	}
}

/**
 * Record 落一条审计事件
 *  - ctx: 携带 principal / request_id / trace_id 的请求上下文
 *  - e:   业务事件
 */
func Record(ctx context.Context, e Event) {
	if db == nil {
		return
	}
	var p *authz.Principal
	if ctx != nil {
		p, _ = authz.PrincipalFromContext(ctx)
	}
	var reqID, traceID string
	var userID, identityID uint64
	var role string
	if p != nil {
		userID = p.UserID
		identityID = p.IdentityID
		role = p.RoleCode
	}
	if ctx != nil {
		reqID = piiaudit.RequestIDFromContext(ctx)
		traceID = piiaudit.TraceIDFromContext(ctx)
	}
	// 截断 error_msg 避免长串
	//
	// ⚠️ M100(2026-10-26):按 **rune** 截,不是按字节。
	// 原来 `errMsg[:200]` 是**字节**切片 —— 一个中文错误信息(3 字节/字)
	// 正好在第 200 字节被劈开,落库就是一个**半个汉字** + 乱码尾巴。
	// 排障时看到这种 error_msg,第一反应是"程序写错了",而不是"被截断了"。
	errMsg := truncateRunes(e.ErrorMsg, 200)
	row := model.SysCryptoAuditLog{
		// ⚠️ M100(2026-10-26):主键必须自己填 ——
		// `BaseModel.ID` 是 `primaryKey;autoIncrement:false`,GORM 不会让库自增,
		// 零值 0 会原样入库 → id 撞主键 → 第二批往后全部插不进去
		// (整批重试 3 次后丢弃)→ **这张表永远只有 1 行**。
		// 详见 piiaudit/piiaudit.go extractFromCtx 的同款说明。
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		KeyID:        e.KeyID,
		KeyVersion:   e.KeyVersion,
		Algorithm:    e.Algorithm,
		Operation:    e.Operation,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		ByteCount:    e.ByteCount,
		Success:      e.Success,
		ErrorMsg:     errMsg,
		RequestID:    reqID,
		TraceID:      traceID,
		UserID:       userID,
		IdentityID:   identityID,
		RoleCode:     role,
	}
	// 尝试从 ctx 拿 ip/ua(如果有 — 中间件后续会注入,目前留空不报错)
	if ctx != nil {
		row.IP = ctxString(ctx, piiaudit.CtxKeyIP{})
		row.UserAgent = ctxString(ctx, piiaudit.CtxKeyUA{})
	}
	mu.Lock()
	batch = append(batch, row)
	if len(batch) >= 100 {
		go flush()
	}
	mu.Unlock()
}

// ctxString 从 ctx 取字符串值(不存在 / 类型不符 → "")
func ctxString(ctx context.Context, key any) string {
	if ctx == nil {
		return ""
	}
	v := ctx.Value(key)
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// truncateRunes 按 rune 截断(不劈开 UTF-8 多字节字符)
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
