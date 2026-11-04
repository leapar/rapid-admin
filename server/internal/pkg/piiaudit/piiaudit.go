// Package piiaudit PII 解密审计框架级 helper —— §1.5 #5 P0
//
// 设计:
//   - 异步批量写:每批 100 条 / 5s flush,业务请求不阻塞
//   - 失败重试 3 次,仍失败打 error 丢日志
//   - Init 在 main.go 启动期调一次,Shutdown 在退出前调
//   - 任何业务代码解密 PII 后调 Record(...) 一行即可
//
// 用法:
//
//	// main.go
//	piiaudit.Init(db)
//	defer piiaudit.Shutdown()
//
//	// 业务代码(任何地方,有 ctx 即可)
//	phone, err := cipher.DecryptString(owner.PhoneCipher)
//	if err == nil {
//	    piiaudit.Record(ctx, piiaudit.Access{
//	        Action: model.PIIActionRead,
//	        ResourceType: "owner",
//	        ResourceID: owner.ID,
//	        Field: model.PIIFieldPhone,
//	    })
//	}
//
// 数据来源:
//   - principal (user/identity/role): authz.WithPrincipal()(由 principal middleware 注入)
//   - request_id: ctx(由 access_log middleware 注入)
//   - trace_id:   ctx(由 otel middleware 注入,无 otel 则为空)
package piiaudit

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/sonyflake"
)

// 默认批参数
const (
	defaultBatchSize     = 100
	defaultFlushInterval = 5 * time.Second
	defaultRetryTimes    = 3
)

// Access 单次访问记录(payload,不含 user/identity/ctx,框架从 ctx 提)
type Access struct {
	Action       string // model.PIIAction*
	ResourceType string // owner / asset / ...
	ResourceID   uint64
	Field        string // model.PIIField*
	Reason       string // 业务侧备注(可选)
}

var (
	global      *Recorder
	onceInit    sync.Once
	shutdownOne sync.Once
)

// Recorder 异步批量写
type Recorder struct {
	db     *gorm.DB
	batch  []model.SysPIIAccessLog
	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
}

// Init 启动全局 Recorder(只调一次)
//
// db 传 nil 时 recorder 关闭(部署形态支持关停 PII 审计)
func Init(db *gorm.DB) {
	onceInit.Do(func() {
		if db == nil {
			logger.L().Warn("piiaudit: db is nil, recorder disabled")
			return
		}
		global = &Recorder{
			db:     db,
			stopCh: make(chan struct{}),
			doneCh: make(chan struct{}),
		}
		go global.loop()
	})
}

// Shutdown 优雅退出(等最后一批 flush)
//
// ⚠️ M100(2026-10-26):改成**幂等**。原来第二次调用会对已关闭的 channel
// 再 close 一次 → panic("close of closed channel")。退出路径上
// defer piiaudit.Shutdown() + 显式调一次,是很容易写出来的组合。
func Shutdown() {
	if global == nil {
		return
	}
	shutdownOne.Do(func() {
		close(global.stopCh)
		<-global.doneCh
	})
}

// Record 记一次 PII 访问(异步,非阻塞)
func Record(ctx context.Context, a Access) {
	if global == nil {
		return // Init 未调:no-op
	}
	if ctx == nil {
		// ⚠️ M100:ctx 为 nil 时 ctx.Value 会 panic —— 审计路径不该把
		// 业务请求带崩(而且是后台 goroutine 里 panic = 进程退出)。
		ctx = context.Background()
	}
	if a.Action == "" {
		a.Action = model.PIIActionRead
	}
	log := extractFromCtx(ctx)
	log.Action = a.Action
	log.ResourceType = a.ResourceType
	log.ResourceID = a.ResourceID
	log.Field = a.Field
	log.Reason = a.Reason
	global.append(log)
}

// RecordMany 批量记(列表 / 导出场景)
//
// 注意:会按 len(as) 写入独立行(列表场景"每行算 1 条"是产品意图)
func RecordMany(ctx context.Context, as []Access) {
	if global == nil || len(as) == 0 {
		return
	}
	for _, a := range as {
		Record(ctx, a)
	}
}

// extractFromCtx 从 ctx 提 principal / request_id / trace_id / ip / user_agent
//
// ctx 拿不到的部分给零值
//
// # ⚠️ M100(2026-10-26):**主键必须自己填**
//
// `model.BaseModel.ID` 的 tag 是 `primaryKey;autoIncrement:false` ——
// GORM 因此**不会**让数据库自增,会把零值 0 原样写进去。
// 于是每一行的 id 都是 0,而 id 是主键 → **第二批往后的插入全部撞
// "UNIQUE constraint failed: sys_pii_access_log.id"** → 整批重试 3 次
// 之后被丢掉(只留一条 Warn 日志)。
//
// 也就是说:**这张 PII 审计表永远只能存下 1 行** —— 而它是 §1.5 #5 的
// P0 等保项("谁在什么时候看了手机号")。表在、行有,但**查不出东西**。
//
// 全仓其它写 `BaseModel` 的地方(cronx / inbox / seed)都显式写
// `BaseModel{ID: sonyflake.Next()}` —— 这里以前是漏的,现在补上。
func extractFromCtx(ctx context.Context) model.SysPIIAccessLog {
	log := model.SysPIIAccessLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
	}
	// principal(由 principal middleware 注入)
	if p, ok := authz.PrincipalFromContext(ctx); ok && p != nil {
		log.UserID = p.UserID
		log.IdentityID = p.IdentityID
		log.RoleCode = p.RoleCode
	}
	// request_id(由 access_log middleware 注入)
	log.RequestID = requestIDFromContext(ctx)
	// trace_id(由 otel middleware 注入,无 otel 则为空)
	log.TraceID = traceIDFromContext(ctx)
	// ⚠️ M100(2026-10-26):ip / user_agent **以前从不填** ——
	// 表里有这两列、cryptaudit 也读同一套 ctx key,唯独本包没读。
	// "谁在什么时候看了手机号"缺了"从哪台机器来"这一维。
	log.IP = stringFromContext(ctx, CtxKeyIP{})
	log.UserAgent = stringFromContext(ctx, CtxKeyUA{})
	return log
}

// stringFromContext 从 ctx 取字符串值(类型不符 / 不存在 → "")
func stringFromContext(ctx context.Context, key any) string {
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

/* ====================== 内部:batch flush ====================== */

func (r *Recorder) append(log model.SysPIIAccessLog) {
	r.mu.Lock()
	r.batch = append(r.batch, log)
	full := len(r.batch) >= defaultBatchSize
	r.mu.Unlock()
	if full {
		r.flush()
	}
}

func (r *Recorder) loop() {
	t := time.NewTicker(defaultFlushInterval)
	defer t.Stop()
	defer close(r.doneCh)
	for {
		select {
		case <-t.C:
			r.flush()
		case <-r.stopCh:
			r.flush() // 最后一批
			return
		}
	}
}

func (r *Recorder) flush() {
	r.mu.Lock()
	if len(r.batch) == 0 {
		r.mu.Unlock()
		return
	}
	batch := r.batch
	// ⚠️ M100(2026-10-26):这里必须是 `nil`,**不能**是 `r.batch[:0]`。
	//
	// `r.batch[:0]` 复用的是**同一块底层数组** —— flush 释放锁之后
	// 才去 CreateInBatches,而这期间 append() 会往同一块数组里写新行,
	// **正在被 GORM 读的那批数据被就地改写**。
	// 后果有两个:
	//   - 数据竞争(-race 一抓一个准)
	//   - 落库的行**内容可能是别的行的**(字段被串了),比丢行更坏
	//
	// 换一块新数组(让 append 重新分配)就没有这个问题。
	// (cryptaudit 一直写的是 `batch = nil`,是对的 —— 这里跟它对齐。)
	r.batch = nil
	r.mu.Unlock()

	for i := 0; i < defaultRetryTimes; i++ {
		err := r.db.CreateInBatches(batch, 50).Error
		if err == nil {
			return
		}
		logger.L().Warn("piiaudit: flush failed, retrying",
			zap.Int("attempt", i+1),
			zap.Int("batch_size", len(batch)),
			zap.Error(err))
	}
	logger.L().Error("piiaudit: flush failed after retries, dropping",
		zap.Int("batch_size", len(batch)))
}
