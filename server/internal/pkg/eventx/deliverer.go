package eventx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/logger"
)

// ErrTransportUnavailable 投递通道暂时不可用(如 Redis 没连上)。
//
// ⚠️ **投递器对它特殊处理:退避重试,但不烧尝试次数**。
// "通道死了"不是"这条消息有问题" —— Redis 宕 10 分钟不该把积压的事件
// 全部送进死信。业务侧投递函数在通道不可用时应返回本错误
// (而不是把通道错误混在普通错误里)。
var ErrTransportUnavailable = errors.New("eventx: 投递通道暂不可用")

// Message 投递器交给 DeliverFunc 的一条消息。
type Message struct {
	// ID outbox 行 id(事件的唯一标识,消费侧幂等可以用它做去重键)
	ID uint64
	// EventName 事件名
	EventName string
	// Topic 投递目标通道名(落表时原样存,原样交回)
	Topic string
	// Payload 载荷 JSON 原文(落表时是什么字节,这里就是什么字节)
	Payload []byte
	// Attempts 本次投递之前已经失败的次数(0 = 第一次投)
	Attempts int
}

// DeliverFunc 真正的投递实现(装配期注入,通常是"推一条 pkg/push 事件")。
//
// 返回 nil = 投递成功;返回 ErrTransportUnavailable = 通道暂不可用
// (退避但不烧次数);返回其他错误 = 本次投递失败(退避且烧次数)。
type DeliverFunc func(ctx context.Context, m Message) error

// DelivererConfig 投递器配置。零值字段全部走默认值,生产装配通常传空结构体。
type DelivererConfig struct {
	// Interval 轮询间隔。默认 1s(事件从落表到推出的最大延迟量级)
	Interval time.Duration
	// SweepTimeout 单次轮询的整体 deadline(防止一次卡死的 DB 查询
	// 把整个轮询循环拖住)。默认 30s
	SweepTimeout time.Duration
	// BatchSize 单次轮询最多取多少条到期行。默认 100
	BatchSize int
	// DeliverTimeout 单条投递的兜底 deadline。默认 5s
	DeliverTimeout time.Duration
	// MaxAttempts 最大尝试次数,达到即进死信(status=dead)。默认 8
	MaxAttempts int
	// BaseDelay 首次重试的退避基数。默认 5s
	BaseDelay time.Duration
	// MaxDelay 退避上限。默认 5m
	MaxDelay time.Duration
	// DeliveredRetention 已投递行的保留时长,超期由投递器清理。
	// 0 = 用默认(24h);负值 = 不清理(表会一直涨,知道你在做什么再用)
	DeliveredRetention time.Duration
}

// withDefaults 补默认值。
func (c DelivererConfig) withDefaults() DelivererConfig {
	if c.Interval <= 0 {
		c.Interval = time.Second
	}
	if c.SweepTimeout <= 0 {
		c.SweepTimeout = 30 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.DeliverTimeout <= 0 {
		c.DeliverTimeout = 5 * time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 8
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = 5 * time.Second
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = 5 * time.Minute
	}
	if c.DeliveredRetention == 0 {
		c.DeliveredRetention = 24 * time.Hour
	}
	return c
}

// backoff 第 attempts 次(1 起)失败后的重试退避:base * 2^(attempts-1),封顶 max。
func backoff(base, max time.Duration, attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := base
	// 2^(attempts-1),用移位;封顶防溢出:一旦 d 已经 ≥ max 就不再翻倍
	for i := 1; i < attempts && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	return d
}

// Deliverer outbox 投递器:轮询 sys_event_outbox 的到期行,交给
// DeliverFunc 投递;失败按指数退避重试,重试耗尽置死信。
//
// # 投递语义:at-least-once
//
// "投出去"和"标记成功"是两步,进程在这两步之间死掉的话,重启后这一行
// 会被再投一次。**消费方必须容忍重复**(信号类事件重收一次的代价是
// 多刷一次列表;要精确一次用 Deduper / 幂等键)。
//
// # 多实例
//
// 轮询没有分布式锁:两个实例同时扫到同一行会各投一次。信号类事件
// 重复无害;要收窄的话,把 DeployFunc 换成带锁实现或用 cronx 的锁跑 Tick。
type Deliverer struct {
	db  *gorm.DB
	fn  DeliverFunc
	cfg DelivererConfig
}

// NewDeliverer 构造投递器。db 为 nil 时 Tick 直接返回错误(不 panic)。
func NewDeliverer(db *gorm.DB, fn DeliverFunc, cfg DelivererConfig) *Deliverer {
	return &Deliverer{db: db, fn: fn, cfg: cfg.withDefaults()}
}

// Run 轮询循环,直到 ctx 被取消(生产装配在 main 里 go d.Run(ctx))。
func (d *Deliverer) Run(ctx context.Context) {
	ticker := time.NewTicker(d.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, d.cfg.SweepTimeout)
			_, err := d.Tick(sweepCtx)
			cancel()
			// 停机窗口的 context canceled 不是故障,别刷日志
			if err != nil && ctx.Err() == nil {
				logger.L().Warn("eventx: outbox 轮询失败", zap.Error(err))
			}
		}
	}
}

// Tick 单次轮询:取一批到期行逐条投递,顺手清理过期的已投递行。
// 返回本次成功投递的条数。
//
// 单测直接调 Tick(不用 sleep 等轮询)。
func (d *Deliverer) Tick(ctx context.Context) (int, error) {
	if d.db == nil {
		return 0, ErrNoDB
	}
	if d.fn == nil {
		return 0, errors.New("eventx: DeliverFunc 为 nil")
	}
	now := time.Now()

	var rows []model.SysEventOutbox
	err := d.db.WithContext(ctx).
		Where("status = ? AND (next_retry_at IS NULL OR next_retry_at <= ?)",
			model.OutboxStatusPending, now).
		Order("id").Limit(d.cfg.BatchSize).Find(&rows).Error
	if err != nil {
		return 0, fmt.Errorf("eventx: 查询到期事件失败: %w", err)
	}

	delivered := 0
	for _, row := range rows {
		if d.deliverOne(ctx, row, now) {
			delivered++
		}
	}
	d.cleanup(ctx, now)
	return delivered, nil
}

// deliverOne 投递一行并按结果更新状态。返回是否投递成功。
func (d *Deliverer) deliverOne(ctx context.Context, row model.SysEventOutbox, now time.Time) bool {
	mCtx, cancel := context.WithTimeout(ctx, d.cfg.DeliverTimeout)
	defer cancel()

	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				// 投递实现 panic 不能打死轮询循环 —— 当作一次失败处理
				err = fmt.Errorf("deliver panic: %v", r)
			}
		}()
		return d.fn(mCtx, Message{
			ID:        row.ID,
			EventName: row.EventName,
			Topic:     row.Topic,
			Payload:   []byte(row.Payload),
			Attempts:  row.Attempts,
		})
	}()
	if err == nil {
		d.mark(ctx, row, model.OutboxStatusDelivered, row.Attempts, nil, nil)
		return true
	}

	// ⚠️ 外层 ctx 已经死了(进程在停机)= 不是消息的问题,不烧次数、
	// 不推退避 —— 下一轮接着投。单条 deadline 到期不算(mCtx 只影响这一条)。
	if ctx.Err() != nil {
		return false
	}

	attempts := row.Attempts + 1
	if errors.Is(err, ErrTransportUnavailable) {
		// 通道不可用:退避,但不烧次数(attempts 不增长)
		next := now.Add(backoff(d.cfg.BaseDelay, d.cfg.MaxDelay, row.Attempts+1))
		d.mark(ctx, row, model.OutboxStatusPending, row.Attempts, &next, err)
		return false
	}

	if attempts >= d.cfg.MaxAttempts {
		logger.L().Error("eventx: 事件重试耗尽,进死信",
			zap.Uint64("id", row.ID),
			zap.String("event", row.EventName),
			zap.String("topic", row.Topic),
			zap.Int("attempts", attempts),
			zap.Error(err))
		d.mark(ctx, row, model.OutboxStatusDead, attempts, nil, err)
		return false
	}

	next := now.Add(backoff(d.cfg.BaseDelay, d.cfg.MaxDelay, attempts))
	logger.L().Warn("eventx: 事件投递失败,将重试",
		zap.Uint64("id", row.ID),
		zap.String("event", row.EventName),
		zap.Int("attempts", attempts),
		zap.Time("next_retry_at", next),
		zap.Error(err))
	d.mark(ctx, row, model.OutboxStatusPending, attempts, &next, err)
	return false
}

// mark 更新一行的投递结果。失败只记日志 —— 投递已经发生/没发生,
// 这里的写失败只影响状态准确性,不该往上抛打断整批。
func (d *Deliverer) mark(ctx context.Context, row model.SysEventOutbox,
	status string, attempts int, next *time.Time, lastErr error) {
	updates := map[string]any{
		"status":   status,
		"attempts": attempts,
	}
	if next != nil {
		updates["next_retry_at"] = next
	} else {
		updates["next_retry_at"] = nil
	}
	if lastErr != nil {
		updates["last_error"] = truncateErr(lastErr)
	}
	if err := d.db.WithContext(ctx).Model(&model.SysEventOutbox{}).
		Where("id = ?", row.ID).Updates(updates).Error; err != nil {
		logger.L().Warn("eventx: outbox 状态更新失败",
			zap.Uint64("id", row.ID), zap.String("to_status", status), zap.Error(err))
	}
}

// cleanup 清理超过保留期的已投递行。死信行**不**清,留给人排查。
func (d *Deliverer) cleanup(ctx context.Context, now time.Time) {
	if d.cfg.DeliveredRetention < 0 {
		return
	}
	cut := now.Add(-d.cfg.DeliveredRetention)
	if err := d.db.WithContext(ctx).
		Where("status = ? AND updated_at < ?", model.OutboxStatusDelivered, cut).
		Delete(&model.SysEventOutbox{}).Error; err != nil {
		logger.L().Warn("eventx: 已投递事件清理失败", zap.Error(err))
	}
}

// truncateErr 错误摘要截断到 512(last_error 列的容量)。
func truncateErr(err error) string {
	s := err.Error()
	if len(s) > 512 {
		s = s[:512]
	}
	return s
}
