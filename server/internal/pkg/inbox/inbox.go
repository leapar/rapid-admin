// Package inbox 站内信 + 通知中心框架(§1.9 #11+#12 P1)
//
// 设计:
//   - Notifier interface:业务模块调 Notify(ctx, msg) 即可投递站内信
//   - Manager:DB 持久化 + 列表/未读数/标已读/全部已读等查询
//   - 未读变更经 pkg/push 推 push:user:<id> 频道,前端 SSE 实时刷新未读数
//   - 失败处理:
//   - DB 写入失败 → 返回 err(业务层决定是否回滚主事务)
//   - Redis 不可用 → 推送静默丢弃,前端轮询 GET unread-count 兜底
//
// ⚠️ M71 变更:Manager **不再持有 redis.Client**,未读推送改走 pkg/push。
// 原来的 Subscribe(ctx, userID) 从未有过调用方(站内信前端 UI 直到 M72 才
// 出现),是个只有注释没有消费者的半成品,连同 channelPrefix / once 一起删掉。
//
// 依赖: pkg/logger, gorm.DB, pkg/push
package inbox

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/push"
	"rapid-admin/internal/pkg/sonyflake"
)

// Message 一条站内信(投递入参)
type Message struct {
	UserID   uint64
	MsgType  string // system / biz / notice
	Title    string
	Content  string
	Link     string
	RefType  string
	RefID    uint64
	SenderID uint64
	// ExpiredAt 留业务可选;nil = 不过期
	ExpiredAt *time.Time
}

// Validate 投递前基本校验
func (m Message) Validate() error {
	if m.UserID == 0 {
		return errors.New("inbox: UserID required")
	}
	if m.Title == "" {
		return errors.New("inbox: Title required")
	}
	// ⚠️ 不在这里给 MsgType 补默认值:m 是值接收者,赋值永远传不回调用方。
	// 默认值在 newRow(Notify/NotifyBatch 落库前唯一的行构造点)里补。
	return nil
}

// ListFilter 列表查询过滤
type ListFilter struct {
	UserID     uint64
	MsgType    string // 空 = 全部
	OnlyUnread bool
	Page       int
	Size       int
}

// Notifier 投递接口(业务模块依赖此 interface,不依赖 Manager)
type Notifier interface {
	Notify(ctx context.Context, msg Message) error
	// NotifyBatch 批量投递(同事务),用于"通知项目下全部业主"场景
	NotifyBatch(ctx context.Context, msgs []Message) error
}

// Manager inbox 中心(DB 持久化 + 列表/已读/未读数)
type Manager struct {
	db *gorm.DB
}

// New 构造 Manager
func New(db *gorm.DB) *Manager {
	return &Manager{db: db}
}

// === Notifier 实现 ===

// newRow 把一条 Message 摊成待插入的行。
//
// 🔴 **id 必须在这里生成,不能交给 DB 自增**(2026-10-24 修的真 bug):
//
// `model.InboxMessage` 继承 `BaseModel`,而 BaseModel.ID 声明的是
// `primaryKey;autoIncrement:false` —— **全仓的表都是应用侧雪花 ID**。
// 拿 MySQL 的 DDL 措辞说(`gorm.io/driver/mysql` 的 FullDataTypeOf 实测):
// 这一列是 `bigint unsigned`,**没有 AUTO_INCREMENT**。
// 于是 `INSERT INTO sys_inbox_message (user_id, ...) VALUES (...)` 在
// **MySQL 严格模式下直接报错 1364「Field 'id' doesn't have a default value」** ——
// 站内信一条都投不出去。非严格模式更糟:全部写成 id=0,而
// `MarkRead(userID, 0)` 会把**这个人的所有消息**一起标成已读。
//
// ⚠️ **为什么一直没被发现**:SQLite 的 `INTEGER PRIMARY KEY` 是 rowid 别名,
// 省略 id 时驱动会自己填(1、2、3……),单测夹具还是自己手写的
// `id INTEGER PRIMARY KEY AUTOINCREMENT` —— **两边都替我们把 bug 盖住了**。
// 夹具改成跟生产一致(不写 AUTOINCREMENT、且不留 id)才会暴露。
//
// ⚠️ **本函数是 Notify 与 NotifyBatch 唯一共用的地方** —— 两处各抄一份
// map 的写法,迟早会漂(这正是这个 bug 的形状:NotifyBatch 那份抄的时候
// 就没有 id)。收在这里,以后加字段只改一处。
func newRow(msg Message, now time.Time) map[string]any {
	// 🔴 MsgType 的默认值必须在这里补,不能在 Validate 里补:Validate 是值
	// 接收者,赋值传不回调用方,空 msg_type 曾直接入库。Notify 与 NotifyBatch
	// 的落库行都只经这一个构造点,在这里补两处同时生效。
	msgType := msg.MsgType
	if msgType == "" {
		msgType = "system"
	}
	row := map[string]any{
		// ⚠️ 雪花 ID 走应用侧(理由见上)。**别改成"让 DB 自增"** ——
		// 那样 ID 会变成 1/2/3,而全仓其它表都是 19 位雪花,
		// `MessageView.ID` 的 string 序列化也是按雪花准备的。
		"id":         sonyflake.Next(),
		"user_id":    msg.UserID,
		"msg_type":   msgType,
		"title":      msg.Title,
		"content":    msg.Content,
		"link":       msg.Link,
		"ref_type":   msg.RefType,
		"ref_id":     msg.RefID,
		"sender_id":  msg.SenderID,
		"created_at": now,
		"updated_at": now,
	}
	if msg.ExpiredAt != nil {
		row["expired_at"] = *msg.ExpiredAt
	}
	return row
}

// Notify 投递一条站内信
func (m *Manager) Notify(ctx context.Context, msg Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	row := newRow(msg, time.Now())
	if err := m.db.WithContext(ctx).Table("sys_inbox_message").Create(row).Error; err != nil {
		return fmt.Errorf("inbox: insert: %w", err)
	}
	m.publishUnread(ctx, msg.UserID)
	return nil
}

// NotifyBatch 批量投递(同事务)
//
//	msgs 中允许不同 UserID;失败整批回滚
func (m *Manager) NotifyBatch(ctx context.Context, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	for i := range msgs {
		if err := msgs[i].Validate(); err != nil {
			return fmt.Errorf("inbox: msgs[%d]: %w", i, err)
		}
	}
	rows := make([]map[string]any, 0, len(msgs))
	now := time.Now()
	affected := make(map[uint64]struct{}, len(msgs))
	for _, msg := range msgs {
		// ⚠️ 走 newRow —— id 在那里生成(见 newRow 的注释)。
		// 这里再抄一份 map 就是下一个 bug 的形状。
		rows = append(rows, newRow(msg, now))
		affected[msg.UserID] = struct{}{}
	}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Table("sys_inbox_message").Create(&rows).Error
	})
	if err != nil {
		return fmt.Errorf("inbox: batch insert: %w", err)
	}
	// 推送:按 user 去重一次,避免同一 user 多条消息多次 publish
	for uid := range affected {
		m.publishUnread(ctx, uid)
	}
	return nil
}

// === 列表 / 未读数 / 标已读 ===

// MessageView 站内信列表项(M74 的前端 wire 契约)
//
// ⚠️ **为什么这个类型存在**:List 原来返回 `[]map[string]any`,那是整个 pkg/ 树里
// 唯一一处这种写法(其余全仓都返 typed struct)。2026-10-22 实测出三个后果,
// 全都由"map 没有 json tag"这一条派生:
//
//  1. **ID 精度**:模型上写的是 `json:"id,string"`,但 map 直接被 json.Marshal,
//     tag 根本不生效 → wire 上是**裸数字**。Notify 现在**在应用侧生成雪花 ID**
//     (见 newRow),拿到的是 17 位数字,前端 JSON.parse 会静默截断,再回传
//     就点到**错误的那一条**。改成 typed 之后,这个字段重新被既有棘轮守住。
//     JobRunView 有同样一条注释,那边靠 `pkg/jsonid` 扫描测试守着;
//     这边 map 正是那个扫描的**盲区**(它扫的是 struct tag,map 上没有 tag 可扫)。
//     改成 typed 之后,这个字段重新被既有棘轮守住。
//
//  2. **时间格式两种**:map 直出 driver 原始值 —— GORM 写入的是本地时间带纳秒
//     ("2026-10-22T12:38:05.2690357+08:00"),别的写入路径给的是 UTC 秒级
//     ("2026-10-22T04:38:05Z")。同一列两种格式,前端得同时兼容两个。
//     改走 model.Time 后由它的 MarshalJSON 统一成 +08:00 秒级。
//
//  3. **NULL 与空串混用**:"没有值"一半是 null、一半是 ""/0(取决于写入方),
//     前端每个字段都得判两遍。出口统一成一个形状。
type MessageView struct {
	// ID / RefID / SenderID 必须是 string —— 理由见上,同 JobRunView
	ID       string `json:"id"`
	MsgType  string `json:"msg_type"` // system / biz / notice
	Title    string `json:"title"`
	Content  string `json:"content"`
	Link     string `json:"link"`     // 可空;前端据此决定要不要渲染"查看详情"
	RefType  string `json:"ref_type"` // 关联业务资源类型,框架只透传
	RefID    string `json:"ref_id"`
	SenderID string `json:"sender_id"` // 空 = 系统发的
	// ReadAt 未读为 null,已读是 ISO8601(+08:00)。前端判未读请比 `read_at === null`,
	// 不要比时间戳 —— 未读没有时间戳可比
	ReadAt    *model.Time `json:"read_at"`
	ExpiredAt *model.Time `json:"expired_at"`
	CreatedAt *model.Time `json:"created_at"`
	UpdatedAt *model.Time `json:"updated_at"`
}

// toView model → wire
//
// 空串 / 0 的归一化在这里做一次:库里存的是 NULL 还是零值由写入方决定
// (Notify 写零值、别的写入方可能写 NULL),但**出口只有一个形状**。
func toView(rows []model.InboxMessage) []MessageView {
	out := make([]MessageView, 0, len(rows))
	for i := range rows {
		r := rows[i]
		// ⚠️ BaseModel 的 CreatedAt/UpdatedAt 是**裸 time.Time**,不是 model.Time。
		// 裸 time.Time 的 MarshalJSON 走 RFC3339 原样输出,时间戳带着 driver 给的
		// 时区和纳秒 —— 这正是 MessageView 注释里"时间格式两种"的来源。
		// 这里显式转一层,让四个时间字段的输出格式一致。
		created := model.Time{Time: r.CreatedAt}
		updated := model.Time{Time: r.UpdatedAt}
		v := MessageView{
			ID:        strconv.FormatUint(r.ID, 10),
			MsgType:   r.MsgType,
			Title:     r.Title,
			Content:   r.Content,
			Link:      r.Link,
			RefType:   r.RefType,
			RefID:     strconv.FormatUint(r.RefID, 10),
			SenderID:  strconv.FormatUint(r.SenderID, 10),
			ReadAt:    r.ReadAt,
			ExpiredAt: r.ExpiredAt,
			CreatedAt: &created,
			UpdatedAt: &updated,
		}
		// RefID / SenderID 为 0 时输出 "" 而不是 "0":0 的含义是"无关联 / 系统发的",
		// 让前端判 `if (msg.ref_type)` 就行,不必每个调用点都记得 0 的特殊含义
		if r.RefID == 0 {
			v.RefID = ""
		}
		if r.SenderID == 0 {
			v.SenderID = ""
		}
		out = append(out, v)
	}
	return out
}

// List 取列表(返回总数,不分页拆)— 适配 BaseHandler.Page
//
// ⚠️ 扫 `[]model.InboxMessage` 而不是 map:GORM 的软删除作用域、model.Time 的
// 时间归一化、uint64 的 string tag 三件事都只在 typed 扫描下才生效。
func (m *Manager) List(ctx context.Context, f ListFilter) ([]MessageView, int64, error) {
	if f.UserID == 0 {
		return nil, 0, errors.New("inbox.List: UserID required")
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Size <= 0 {
		f.Size = 20
	}
	q := m.db.WithContext(ctx).Model(&model.InboxMessage{}).Where("user_id = ?", f.UserID)
	if f.MsgType != "" {
		q = q.Where("msg_type = ?", f.MsgType)
	}
	if f.OnlyUnread {
		q = q.Where("read_at IS NULL")
	}
	// 过滤已过期(但保留已读;前端一般按未读优先)
	q = q.Where("(expired_at IS NULL OR expired_at > ?)", time.Now())

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("inbox.List count: %w", err)
	}
	var rows []model.InboxMessage
	if err := q.Order("id DESC").
		Offset((f.Page - 1) * f.Size).
		Limit(f.Size).
		Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("inbox.List find: %w", err)
	}
	return toView(rows), total, nil
}

// UnreadCount 未读数
func (m *Manager) UnreadCount(ctx context.Context, userID uint64, msgType string) (int64, error) {
	if userID == 0 {
		return 0, errors.New("inbox.UnreadCount: userID required")
	}
	// ⚠️ 下面三处是裸 Table 查询,GORM 不会自动带软删作用域;Delete 改软删后
	// 必须显式排除已删行(否则已删消息仍计入未读数、仍可被标已读)。
	q := m.db.WithContext(ctx).Table("sys_inbox_message").
		Where("user_id = ? AND read_at IS NULL", userID).
		Where("deleted_at IS NULL").
		Where("(expired_at IS NULL OR expired_at > ?)", time.Now())
	if msgType != "" {
		q = q.Where("msg_type = ?", msgType)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, fmt.Errorf("inbox.UnreadCount: %w", err)
	}
	return n, nil
}

// MarkRead 标单条已读
//   - 已读的不报错(幂等)
//   - 不属于该 user 的也返 nil(防 IDOR)
func (m *Manager) MarkRead(ctx context.Context, userID, msgID uint64) error {
	if userID == 0 || msgID == 0 {
		return errors.New("inbox.MarkRead: userID and msgID required")
	}
	now := time.Now()
	res := m.db.WithContext(ctx).Table("sys_inbox_message").
		Where("id = ? AND user_id = ? AND read_at IS NULL", msgID, userID).
		Where("deleted_at IS NULL"). // 裸 Table 查询不带软删作用域,见 UnreadCount
		Updates(map[string]any{"read_at": now, "updated_at": now})
	if res.Error != nil {
		return fmt.Errorf("inbox.MarkRead: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		m.publishUnread(ctx, userID)
	}
	return nil
}

// MarkAllRead 全部已读(按 user,可选 msgType)
func (m *Manager) MarkAllRead(ctx context.Context, userID uint64, msgType string) (int64, error) {
	if userID == 0 {
		return 0, errors.New("inbox.MarkAllRead: userID required")
	}
	now := time.Now()
	q := m.db.WithContext(ctx).Table("sys_inbox_message").
		Where("user_id = ? AND read_at IS NULL", userID).
		Where("deleted_at IS NULL") // 裸 Table 查询不带软删作用域,见 UnreadCount
	if msgType != "" {
		q = q.Where("msg_type = ?", msgType)
	}
	res := q.Updates(map[string]any{"read_at": now, "updated_at": now})
	if res.Error != nil {
		return 0, fmt.Errorf("inbox.MarkAllRead: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		m.publishUnread(ctx, userID)
	}
	return res.RowsAffected, nil
}

// Delete 单条删除(框架接口;业务可选用,本批 handler 不挂)
//   - 防 IDOR:只能删自己的
//   - 🔴 必须走软删:Delete(&model.InboxMessage{}) 命中 gorm.DeletedAt 的软删
//     契约(行不消失,deleted_at 置位),也让挂在该模型上的 GORM 回调
//     (如 auditx 数据变更审计)有机会触发;原先 Table()+Delete(nil) 是
//     物理 DELETE,审计与恢复都无从谈起
func (m *Manager) Delete(ctx context.Context, userID, msgID uint64) error {
	if userID == 0 || msgID == 0 {
		return errors.New("inbox.Delete: userID and msgID required")
	}
	res := m.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", msgID, userID).
		Delete(&model.InboxMessage{})
	if res.Error != nil {
		return fmt.Errorf("inbox.Delete: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		m.publishUnread(ctx, userID)
	}
	return nil
}

// === pub/sub ===

// UnreadEvent 未读数变更事件的 payload。
//
// ⚠️ 不含 At —— 时间戳由 push.Event.At 承载。
//
// ⚠️ 只带 delta 不带绝对未读数:并发投递 / 标已读会让 delta 累加漂移,
// 前端拿 delta 只是为了让红点"动一下",真正的值以 GET /inbox/unread-count 为准。
type UnreadEvent struct {
	UserID uint64 `json:"user_id,string"`
	Delta  int64  `json:"delta"` // +N / -N(本次事件影响)
}

// publishUnread 推未读数变更信号。
//
// **永不返回错误**:投递方是"刚写完库的请求",不该因为推送失败就让主流程失败。
// push.Publish 内部已把失败记成 Warn 日志。
func (m *Manager) publishUnread(ctx context.Context, userID uint64) {
	topic := push.TopicUser(userID)
	push.Publish(ctx, []string{topic}, push.Event{
		Topic: topic,
		Event: push.EventInboxUnread,
		Payload: UnreadEvent{
			UserID: userID,
			Delta:  +1, // 新投递 / 标已读前都先 +1(实际未读数前端拉 unread-count 校准)
		},
	})
}
