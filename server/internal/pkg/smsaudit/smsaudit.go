// Package smsaudit 短信发送审计框架级 helper —— §1.9 #7 P1
//
// 异步批量写(模式与 piiaudit 一致):
//   - 100 条/5s flush
//   - 3 次重试
//   - db nil 时 no-op
//
// 用法:
//
//	// main.go
//	smsaudit.Init(repository.GetDB())
//	defer smsaudit.Shutdown()
//
//	// 业务侧(任何地方)
//	smsaudit.Record(ctx, smsaudit.SendLog{
//	    Phone: phone, Scene: scene, Code: code,
//	    Provider: "aliyun", Success: true, CostMs: 234,
//	})
//
// 也可包在 Sender 装饰器里自动接入(推荐):
//
//	// factory.go
//	return smsaudit.Wrap(realSender) // 装饰器,内部 Real 调用 + Record
package smsaudit

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/piiaudit"
	"rapid-admin/internal/pkg/sonyflake"
)

const (
	defaultBatchSize     = 100
	defaultFlushInterval = 5 * time.Second
	defaultRetryTimes    = 3
)

// SendLog 单次发送审计(payload,user/identity/request_id/trace_id/ip/ua 框架从 ctx 提)
type SendLog struct {
	Phone         string
	Scene         string
	Code          string
	Provider      string // aliyun / console
	Success       bool
	ErrorMsg      string
	CostMs        int
	UpstreamReqID string
}

var (
	global   *Recorder
	onceInit sync.Once
)

// Recorder 异步批量写
type Recorder struct {
	db     *gorm.DB
	batch  []model.SysSmsSendLog
	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
	// shutdownOnce 保证 Shutdown 幂等(每个 Recorder 各自一份)
	shutdownOnce sync.Once
}

// Init 启动全局 Recorder(只调一次)
func Init(db *gorm.DB) {
	onceInit.Do(func() {
		if db == nil {
			logger.L().Warn("smsaudit: db is nil, recorder disabled")
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

// Shutdown 优雅退出(等最后一批 flush 完)
//
// # 幂等
//
// 原来第二次调用会对已关闭的 channel 再 close 一次 → panic("close of closed
// channel")。退出路径上"`defer smsaudit.Shutdown()` + 显式调一次"是很容易
// 写出来的组合(先显式调一次把最后一批 flush 掉,再靠 defer 兜底)。
//
// 兄弟包 piiaudit / cryptaudit 在 M100 都已经改成幂等,本包之前漏了。
//
// Once 放在 Recorder 上(不是包级变量):每个 Recorder 各自只关一次,
// 换 global 也不会误伤别的实例。
func Shutdown() {
	if global == nil {
		return
	}
	global.shutdown()
}

// shutdown 停掉后台 flush 循环并等它收尾(幂等)
func (r *Recorder) shutdown() {
	r.shutdownOnce.Do(func() {
		close(r.stopCh)
		<-r.doneCh
	})
}

// Record 记一次发送(异步,非阻塞)
func Record(ctx context.Context, l SendLog) {
	if global == nil {
		return
	}
	if ctx == nil {
		// ctx 为 nil 时 ctx.Value 会 panic —— 审计路径不该把业务请求带崩
		// (与 piiaudit 的 M100 同款处理)。
		ctx = context.Background()
	}
	log := extractFromCtx(ctx)
	log.Phone = l.Phone
	log.Scene = l.Scene
	log.Code = l.Code
	log.Provider = l.Provider
	log.Success = l.Success
	log.ErrorMsg = l.ErrorMsg
	log.CostMs = l.CostMs
	log.UpstreamReqID = l.UpstreamReqID
	global.append(log)
}

// extractFromCtx 从 ctx 提 principal / request_id / trace_id / ip / user_agent
//
// # ⚠️ M100(2026-10-26):**主键必须自己填**
//
// 与 piiaudit / cryptaudit 同款缺陷:`BaseModel.ID` 是
// `primaryKey;autoIncrement:false`,GORM 把零值 0 原样入库 →
// id 撞主键 → 第二批往后的行全部插不进去 → **这张表永远只有 1 行**。
// 本包属 §1.9 #7(短信发送审计),不在 M100 的 §1.5 范围内,
// 但缺陷与 §1.5 #5 / #8 的两个兄弟包**完全同源**,且修复只有一行 ——
// 明知故犯地留着不管不可接受,故一并修掉。
//
// # ctx key 必须与中间件用同一套
//
// 原来本包读自己的私有 `ctxKeyRequestID{}` / `ctxKeyTraceID{}`,
// 而全仓唯一注入方(`internal/api/middleware/trace.go`)用的是
// **piiaudit** 的 key ⇒ request_id / trace_id 恒为空。
// ip / user_agent 则**从来没读过** —— 表里有那两列,兄弟包也读同一套 key。
// 现在统一走 piiaudit。
func extractFromCtx(ctx context.Context) model.SysSmsSendLog {
	log := model.SysSmsSendLog{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
	}
	if p, ok := authz.PrincipalFromContext(ctx); ok && p != nil {
		log.UserID = p.UserID
		log.IdentityID = p.IdentityID
	}
	log.RequestID = piiaudit.RequestIDFromContext(ctx)
	log.TraceID = piiaudit.TraceIDFromContext(ctx)
	log.IP = ctxString(ctx, piiaudit.CtxKeyIP{})
	log.UserAgent = ctxString(ctx, piiaudit.CtxKeyUA{})
	return log
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

/* ====================== 内部:batch flush ====================== */

func (r *Recorder) append(log model.SysSmsSendLog) {
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
			r.flush()
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
	// ⚠️ M100(2026-10-26):与 piiaudit 同款 —— 不能是 `r.batch[:0]`。
	// 那会复用同一块底层数组,flush 期间 append 写进去的新行会**就地改写**
	// 正在被 GORM 读的那批数据(数据竞争 + 落库行内容被串)。
	r.batch = nil
	r.mu.Unlock()

	for i := 0; i < defaultRetryTimes; i++ {
		// 整批一个事务:要么全进要么全不进。
		// 之前裸 CreateInBatches(batch, 50) 在批次中点断连时,重试会把
		// 已提交的前 50 条再写一遍(审计表重复行);3 次重试耗尽则已提交的
		// 一半留下、后一半丢失 —— 两种结局都是审计账不平。
		// 事务化后重试天然幂等(失败批 = 空批)。
		err := r.db.Transaction(func(tx *gorm.DB) error {
			return tx.CreateInBatches(batch, 50).Error
		})
		if err == nil {
			return
		}
		logger.L().Warn("smsaudit: flush failed, retrying",
			zap.Int("attempt", i+1),
			zap.Int("batch_size", len(batch)),
			zap.Error(err))
	}
	logger.L().Error("smsaudit: flush failed after retries, dropping",
		zap.Int("batch_size", len(batch)))
}
