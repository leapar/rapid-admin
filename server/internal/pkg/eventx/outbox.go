package eventx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// ErrDuplicate 同一幂等键的事件已落过表(Enqueue 去重命中)。
// 对发布方来说这不是错误:事件已经在库里了,照常返回。
var ErrDuplicate = errors.New("eventx: 幂等键重复,事件已存在")

// ErrNoDB 没有可用的数据库句柄。
var ErrNoDB = errors.New("eventx: db 为 nil,无法落 outbox")

// OutboxMessage 一条待落表的事件。
type OutboxMessage struct {
	// EventName 事件名(如 approval.todo)
	EventName string
	// Topic 投递目标通道名(投递器把它原样交给 DeliverFunc)
	Topic string
	// Payload 事件载荷。json.RawMessage / []byte 原样存储;
	// 其余类型 json.Marshal 后存储(marshal 失败 = 落表失败 = 返回错误)
	Payload any
	// IdemKey 生产侧幂等键(可空)。给了的话同一 key 只落一行,
	// 重复 Enqueue 返回 ErrDuplicate(不算失败)
	IdemKey string
}

// Enqueue 把一条事件写进 outbox 表。
//
// # 事务性
//
// ⚠️ **想跟业务同生共死,就把业务的事务句柄传进来** —— db 参数是
// *gorm.DB,传 tx 就是事务内落表(业务回滚,事件一起回滚);
// 传普通 db 就是独立小事务落表(业务已 commit 后的补记)。
// 两种用法都是"commit 即发布成功",Redis 死活不再影响这件事。
//
// # 返回
//
//	id:落表成功时的行 id(投递/去重都没有失败);
//	ErrDuplicate:同幂等键的行已存在 —— 事件已经在库里,**不算失败**;
//	其他错误:落表失败,调用方自行兜底(见 publisher 的直推 fallback)。
func Enqueue(ctx context.Context, db *gorm.DB, msg OutboxMessage) (uint64, error) {
	if db == nil {
		return 0, ErrNoDB
	}
	payload, err := marshalPayload(msg.Payload)
	if err != nil {
		return 0, fmt.Errorf("eventx: payload 序列化失败 (event=%s): %w", msg.EventName, err)
	}
	row := model.SysEventOutbox{
		EventName: msg.EventName,
		Topic:     msg.Topic,
		Payload:   payload,
		Status:    model.OutboxStatusPending,
	}
	if msg.IdemKey != "" {
		k := msg.IdemKey
		row.IdemKey = &k
	}
	// ⚠️ **ID 必须自己发** —— BaseModel 主键是 autoIncrement:false(GORM 不写
	// AUTOINCREMENT),不赋值的话每行都是 0,第二行就撞唯一键
	// (cronx/store_test.go 的 seeder 注释记着同一件事)。
	row.ID = sonyflake.Next()
	res := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return 0, fmt.Errorf("eventx: outbox 落表失败 (event=%s): %w", msg.EventName, res.Error)
	}
	if res.RowsAffected == 0 {
		// 唯一键冲突被 OnConflict 吞成 0 行 —— 幂等键重复,事件已经在库里
		return 0, ErrDuplicate
	}
	return row.ID, nil
}

// marshalPayload 载荷序列化:RawMessage / []byte 原样,其余 json.Marshal。
//
// ⚠️ RawMessage 原样透传是**线字节兼容**的关键:发布方先把 typed struct
// marshal 成 JSON 存进 outbox,投递时作为 RawMessage 塞回 push.Event,
// transport 再 marshal 一遍得到的字节与"直推 struct"逐字节相同
// (struct 的字段顺序由定义固定,marshal 幂等)。
func marshalPayload(p any) (string, error) {
	switch v := p.(type) {
	case nil:
		return "", nil
	case json.RawMessage:
		return string(v), nil
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
