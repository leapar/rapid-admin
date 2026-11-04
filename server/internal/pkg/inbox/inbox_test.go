package inbox

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/push"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 每个 test 用独立 DSN(:memory:)避免共享 cache
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// ⚠️ deleted_at 必须在这张表里:List 现在扫 typed 模型(Model(&model.InboxMessage{})),
	// GORM 会自动带上软删除作用域 `deleted_at IS NULL`。生产表由 migrate.go 的
	// AutoMigrate 建,本来就有这列 —— 夹具缺列会让软删除作用域直接报
	// "no such column",看着像代码坏了,其实是夹具跟生产不一致。
	// (List 原先返 map,作用域不生效,反而把这个不一致盖住了)
	//
	// 🔴 **id 列的写法必须跟生产(MySQL)一致,不能图省事写 AUTOINCREMENT**
	// (2026-10-24 修的真 bug 就是被这一行盖住的):
	//   · 生产 DDL:`id bigint unsigned NOT NULL` + `PRIMARY KEY (id)`,
	//     **没有 AUTO_INCREMENT**(BaseModel.ID 声明了 autoIncrement:false)。
	//     MySQL 严格模式下,INSERT 不带 id 直接报 1364。
	//   · 这里用 `BIGINT` 而不是 `INTEGER`:SQLite 只在列类型**恰好是 INTEGER**
	//     时才把 PRIMARY KEY 当 rowid 别名(自动填值);写成 BIGINT 就不会,
	//     于是"漏填 id"在夹具里也会炸 —— 与生产同一种病。
	// 夹具里原本写的是 `id INTEGER PRIMARY KEY AUTOINCREMENT`,那两处自动填值
	// 叠在一起,把 bug 完美地藏了起来。
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS sys_inbox_message (
		id BIGINT NOT NULL,
		user_id INTEGER NOT NULL,
		msg_type TEXT,
		title TEXT NOT NULL,
		content TEXT,
		link TEXT,
		ref_type TEXT,
		ref_id INTEGER,
		sender_id INTEGER,
		read_at DATETIME,
		expired_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME,
		PRIMARY KEY (id)
	)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestManager_Notify_List_UnreadCount_MarkRead(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	// 投递 3 条
	for i := 0; i < 3; i++ {
		if err := mgr.Notify(ctx, Message{
			UserID:  100,
			MsgType: "system",
			Title:   "test",
			Content: "hello",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 另一 user 一条(不能混)
	if err := mgr.Notify(ctx, Message{UserID: 200, Title: "x"}); err != nil {
		t.Fatal(err)
	}

	// 列表(只 user 100)
	rows, total, err := mgr.List(ctx, ListFilter{UserID: 100})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("expected 3, got total=%d len=%d", total, len(rows))
	}

	// 未读
	n, err := mgr.UnreadCount(ctx, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("unread = %d, want 3", n)
	}

	// 取第一条 ID(wire 上是字符串,前端拿到的也是同一个字符串)
	firstID, err := strconv.ParseUint(rows[0].ID, 10, 64)
	if err != nil {
		t.Fatalf("解析首条 ID %q 失败: %v", rows[0].ID, err)
	}

	// 标已读
	if err := mgr.MarkRead(ctx, 100, firstID); err != nil {
		t.Fatal(err)
	}
	n, _ = mgr.UnreadCount(ctx, 100, "")
	if n != 2 {
		t.Fatalf("after mark read, unread = %d, want 2", n)
	}

	// IDOR 防护:user 200 标 user 100 的 → 应 0 affected,not error
	// (我们的实现:where user_id = ? → affected=0,err=nil)
	if err := mgr.MarkRead(ctx, 200, firstID); err != nil {
		t.Fatal(err)
	}
	n, _ = mgr.UnreadCount(ctx, 100, "")
	if n != 2 {
		t.Fatalf("IDOR 防护:200 标 100 的应无效,unread 仍 = %d", n)
	}

	// 全部已读
	affected, err := mgr.MarkAllRead(ctx, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if affected != 2 {
		t.Fatalf("MarkAllRead affected = %d, want 2", affected)
	}
	n, _ = mgr.UnreadCount(ctx, 100, "")
	if n != 0 {
		t.Fatalf("after all read, unread = %d, want 0", n)
	}
}

func TestManager_NotifyBatch(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()
	msgs := []Message{
		{UserID: 1, Title: "a"},
		{UserID: 1, Title: "b"},
		{UserID: 2, Title: "c"},
	}
	if err := mgr.NotifyBatch(ctx, msgs); err != nil {
		t.Fatal(err)
	}
	n1, _ := mgr.UnreadCount(ctx, 1, "")
	n2, _ := mgr.UnreadCount(ctx, 2, "")
	if n1 != 2 || n2 != 1 {
		t.Fatalf("expected 2/1, got %d/%d", n1, n2)
	}
}

func TestManager_NotifyBatch_Empty(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	if err := mgr.NotifyBatch(context.Background(), nil); err != nil {
		t.Fatalf("empty batch should be no-op, got %v", err)
	}
}

func TestMessage_Validate(t *testing.T) {
	cases := []struct {
		name string
		m    Message
		ok   bool
	}{
		{"ok", Message{UserID: 1, Title: "t"}, true},
		{"no user", Message{Title: "t"}, false},
		{"no title", Message{UserID: 1}, false},
	}
	for _, c := range cases {
		err := c.m.Validate()
		if c.ok && err != nil {
			t.Errorf("%s: want ok, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: want err, got nil", c.name)
		}
	}
}

func TestManager_OnlyUnread_Filter(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		mgr.Notify(ctx, Message{UserID: 1, Title: "x"})
	}
	rows, total, _ := mgr.List(ctx, ListFilter{UserID: 1, OnlyUnread: true})
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	// 标第一已读
	id, err := strconv.ParseUint(rows[0].ID, 10, 64)
	if err != nil {
		t.Fatalf("解析首条 ID %q 失败: %v", rows[0].ID, err)
	}
	mgr.MarkRead(ctx, 1, id)
	// 再查 onlyUnread
	_, total2, _ := mgr.List(ctx, ListFilter{UserID: 1, OnlyUnread: true})
	if total2 != 4 {
		t.Fatalf("after mark read, onlyUnread = %d, want 4", total2)
	}
}

func TestManager_ExpiredAt_Filtered(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()
	// 一条过期
	past := time.Now().Add(-1 * time.Hour)
	mgr.Notify(ctx, Message{UserID: 1, Title: "old", ExpiredAt: &past})
	// 一条没过期
	mgr.Notify(ctx, Message{UserID: 1, Title: "new"})
	_, total, _ := mgr.List(ctx, ListFilter{UserID: 1})
	if total != 1 {
		t.Fatalf("expected 1 (expired filtered), got %d", total)
	}
}

// TestManager_PushUninitialized_NoPanic 推送通道没装配时也不能 panic
//
// 推送是旁路:Redis 没起来不该让"投递站内信"这个主流程失败,更不该崩进程。
func TestManager_PushUninitialized_NoPanic(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	push.Init(nil) // 未装配 Redis

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("publish 不该 panic: %v", r)
		}
	}()
	for i := 0; i < 3; i++ {
		if err := mgr.Notify(context.Background(), Message{UserID: 1, Title: "x"}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestManager_PublishUnread_ViaPush 未读事件确实投到了本人专属频道
//
// ⚠️ 这条比"不 panic"有价值得多:原来 Manager 自持 rdb 时,"没 panic"
// 完全可能是**事件压根没投出去**(rdb == nil 直接 return)。M71 改成走 push
// 之后,必须有一条测试真的盯住"投到哪个频道、什么事件名、payload 是什么"。
func TestManager_PublishUnread_ViaPush(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)

	rec := &recordingTransport{}
	push.Init(rec)
	t.Cleanup(func() { push.Init(nil) })

	if err := mgr.Notify(context.Background(), Message{UserID: 42, Title: "hi"}); err != nil {
		t.Fatal(err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("应该只投 1 条事件,实际 %d 条", len(rec.events))
	}
	got := rec.events[0]
	if len(got.topics) != 1 || got.topics[0] != push.TopicUser(42) {
		t.Fatalf("投错频道了: %v,期望 [%s]", got.topics, push.TopicUser(42))
	}
	if got.ev.Event != push.EventInboxUnread {
		t.Fatalf("事件名不对: %q", got.ev.Event)
	}
	if got.ev.At == 0 {
		t.Fatal("Event.At 应由 push.Publish 自动补时间戳,不能是 0")
	}
	payload, ok := got.ev.Payload.(UnreadEvent)
	if !ok {
		t.Fatalf("payload 类型不对: %T", got.ev.Payload)
	}
	if payload.UserID != 42 || payload.Delta != 1 {
		t.Fatalf("payload 内容不对: %+v", payload)
	}
}

// TestManager_PublishUnread_IsolatesUsers 消息不会投到别人的频道
func TestManager_PublishUnread_IsolatesUsers(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)

	rec := &recordingTransport{}
	push.Init(rec)
	t.Cleanup(func() { push.Init(nil) })

	ctx := context.Background()
	_ = mgr.Notify(ctx, Message{UserID: 1, Title: "a"})
	_ = mgr.Notify(ctx, Message{UserID: 2, Title: "b"})

	if len(rec.events) != 2 {
		t.Fatalf("应该投 2 条,实际 %d 条", len(rec.events))
	}
	if rec.events[0].topics[0] == rec.events[1].topics[0] {
		t.Fatalf("两个用户的频道撞了: %s", rec.events[0].topics[0])
	}
	if rec.events[0].topics[0] != push.TopicUser(1) || rec.events[1].topics[0] != push.TopicUser(2) {
		t.Fatalf("频道与 userID 不匹配: %v / %v", rec.events[0].topics, rec.events[1].topics)
	}
}

// recordingTransport 记录 Publish 调用,不连 Redis
type recordingTransport struct {
	events []recordedPublish
}

type recordedPublish struct {
	topics []string
	ev     push.Event
}

func (r *recordingTransport) Available() bool { return true }

func (r *recordingTransport) Publish(_ context.Context, topics []string, ev push.Event) error {
	r.events = append(r.events, recordedPublish{topics: topics, ev: ev})
	return nil
}

func (r *recordingTransport) Subscribe(context.Context, []string) (<-chan push.Event, func(), error) {
	return nil, func() {}, nil
}

// ─────────── M74:MessageView wire 契约 ───────────
//
// 这组测试盯的是 List **序列化之后**的样子,不是内存里的字段值 ——
// "字段是 string"和"wire 上是带引号的字符串"是两回事,后者才是前端看到的。
// 三个缺陷(ID 裸数字 / 时间两种格式 / null 与空串混用)当初全都只在
// json.Marshal 之后才看得见,内存断言一个都发现不了。

// TestM74_List_IDOnWireIsString 守住 jsonid 那条铁律
//
// 探针来源:List 原来返 []map[string]any,模型上的 `json:"id,string"` 完全不生效,
// 19 位雪花 ID 序列化成裸数字 → 前端 JSON.parse 静默截断 → 回传的 ID 打到别的记录。
// 今天 Notify 不写 id、靠 DB 自增,所以必须**手工塞一个 19 位的**才能复现。
func TestM74_List_IDOnWireIsString(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	const snowflake uint64 = 192837465019283746 // 18 位,远超 JS 的 2^53
	if err := db.Exec(
		`INSERT INTO sys_inbox_message (id,user_id,msg_type,title,created_at,updated_at)
		 VALUES (?,?,?,?,datetime('now'),datetime('now'))`,
		snowflake, 100, "system", "雪花 ID",
	).Error; err != nil {
		t.Fatal(err)
	}

	rows, _, err := mgr.List(ctx, ListFilter{UserID: 100, Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].ID != "192837465019283746" {
		t.Fatalf("ID = %q, want 完整 19 位字符串", rows[0].ID)
	}

	// 真正的前端视角:JSON.parse
	b, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	raw, ok := wire["id"].(string)
	if !ok {
		t.Fatalf(`wire 上 id 是 %T 不是 string —— JSON.parse 会截断。前端拿到 %v`,
			wire["id"], wire["id"])
	}
	if raw != "192837465019283746" {
		t.Fatalf("wire id = %q, want %q", raw, "192837465019283746")
	}
}

// TestM74_List_TimestampsOneFormat 守住"同一列不能两种格式"
//
// 探针来源:List 原来直出 driver 原始值 —— GORM 写入的是本地时间带纳秒
// ("…T12:38:05.2690357+08:00"),手写 SQL 写入的是 UTC 秒级("…T04:38:05Z"),
// 同一个响应里两种格式并存。
func TestM74_List_TimestampsOneFormat(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	// 两条走不同写入路径,构造出当初那两种时间
	if err := mgr.Notify(ctx, Message{UserID: 100, Title: "GORM 写入"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		`INSERT INTO sys_inbox_message (id,user_id,msg_type,title,created_at,updated_at)
		 VALUES (?,?,?,?,datetime('now'),datetime('now'))`,
		uint64(999), 100, "system", "原生 SQL 写入",
	).Error; err != nil {
		t.Fatal(err)
	}

	rows, _, err := mgr.List(ctx, ListFilter{UserID: 100, Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	var shapes []string
	for _, r := range rows {
		if r.CreatedAt == nil {
			t.Fatalf("%q 的 created_at 是 nil", r.Title)
		}
		b, err := r.CreatedAt.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		shapes = append(shapes, string(b))
	}
	if shapes[0] != shapes[1] {
		t.Fatalf("两条 created_at 格式不同:\n  %s\n  %s", shapes[0], shapes[1])
	}
	// 且必须是 model.Time 的统一口径:+08:00 秒级
	if !strings.HasSuffix(shapes[0], `+08:00"`) || strings.Contains(shapes[0], ".") {
		t.Fatalf("created_at = %s,期望 +08:00 秒级(model.Time 的统一口径)", shapes[0])
	}
}

// TestM74_List_NullAndEmptyAreOneShape 守住"出口只有一个形状"
//
// 探针来源:Notify 写零值(空串 / 0),别的写入方写 NULL,同一语义两种 wire 形状,
// 前端每个字段都得判两遍。
func TestM74_List_NullAndEmptyAreOneShape(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	// 一条走 Notify(零值),一条只填必填列(其余留 NULL)
	if err := mgr.Notify(ctx, Message{UserID: 100, Title: "零值行"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		`INSERT INTO sys_inbox_message (id,user_id,msg_type,title,content,link,ref_type,
		 ref_id,sender_id,read_at,expired_at,created_at,updated_at)
		 VALUES (?,?,?,?,NULL,NULL,NULL,NULL,NULL,NULL,NULL,datetime('now'),datetime('now'))`,
		uint64(999), 100, "system", "NULL 行",
	).Error; err != nil {
		t.Fatal(err)
	}

	rows, _, err := mgr.List(ctx, ListFilter{UserID: 100, Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for _, r := range rows {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(b, &wire); err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"id", "msg_type", "title", "content", "link", "ref_type", "ref_id", "sender_id"} {
			if _, ok := wire[f].(string); !ok {
				t.Fatalf("%q 的 %q 是 %v(%T),期望 string —— 前端不做 null 判断",
					r.Title, f, wire[f], wire[f])
			}
		}
		// 0 值归一化成空串而不是 "0"
		if wire["sender_id"] != "" {
			t.Fatalf("%q 的 sender_id = %v,期望空串(0 = 系统发的)", r.Title, wire["sender_id"])
		}
		if wire["ref_id"] != "" {
			t.Fatalf("%q 的 ref_id = %v,期望空串(0 = 无关联)", r.Title, wire["ref_id"])
		}
	}
}

// ─────────── ② id 必须由应用侧生成(2026-10-24 修的真 bug 的回归守卫)───────────

// 🔴 **id 必须由应用侧生成**
//
// 生产是 MySQL:`id bigint unsigned NOT NULL` + `PRIMARY KEY (id)`,**没有
// AUTO_INCREMENT**(BaseModel.ID 声明了 autoIncrement:false)。Notify 早先
// 不写 id,于是 INSERT 在 MySQL 严格模式下直接报 1364「Field 'id' doesn't
// have a default value」—— **站内信一条都投不出去**;非严格模式下更糟:
// 全部写成 id=0,而 `MarkRead(userID, 0)` 会把这个人的**所有**消息一起
// 标成已读。
//
// ⚠️ **夹具已经改成 `id BIGINT NOT NULL`**(SQLite 只在列类型**恰好是 INTEGER**
// 时才把 PK 当 rowid 别名自动填值),所以"漏填 id"在夹具里也会炸 ——
// 与生产同一种病,而不是被 SQLite 悄悄兜住。
func TestManager_Notify_GeneratesIDInApp(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	if err := mgr.Notify(ctx, Message{UserID: 100, Title: "a"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if err := mgr.Notify(ctx, Message{UserID: 100, Title: "b"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	rows, _, err := mgr.List(ctx, ListFilter{UserID: 100, Page: 1, Size: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		id, err := strconv.ParseUint(r.ID, 10, 64)
		if err != nil {
			t.Fatalf("解析 ID %q 失败: %v", r.ID, err)
		}
		if id == 0 {
			t.Fatalf("ID = 0 —— 漏填 id 的老 bug 回来了")
		}
		// ⚠️ 雪花是 17 位(1e16 量级);DB 自增会是 1/2/3。
		if id < 1e16 {
			t.Fatalf("ID %d 不是应用侧生成的雪花(小于 1e16)—— 又退回 DB 自增了?", id)
		}
		if seen[r.ID] {
			t.Fatalf("两条消息的 ID 撞了: %s", r.ID)
		}
		seen[r.ID] = true
	}
}

// NotifyBatch 走的是另一条写入路径(事务 + 批量),**必须同样生成 id**。
//
// ⚠️ 这条单独写是因为老 bug 的形状正是"两份 map 抄岔了":
// Notify 那份后来补了 id,NotifyBatch 那份没补,于是批量投递照样炸。
func TestManager_NotifyBatch_GeneratesIDInApp(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	msgs := []Message{
		{UserID: 100, Title: "a"},
		{UserID: 200, Title: "b"},
	}
	if err := mgr.NotifyBatch(ctx, msgs); err != nil {
		t.Fatalf("NotifyBatch: %v", err)
	}
	for _, uid := range []uint64{100, 200} {
		rows, _, err := mgr.List(ctx, ListFilter{UserID: uid, Page: 1, Size: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("user %d 的 rows = %d, want 1", uid, len(rows))
		}
		id, err := strconv.ParseUint(rows[0].ID, 10, 64)
		if err != nil {
			t.Fatalf("解析 ID %q 失败: %v", rows[0].ID, err)
		}
		if id < 1e16 {
			t.Fatalf("user %d 的 ID %d 不是应用侧生成的雪花(小于 1e16)", uid, id)
		}
	}
}

// ─────────── ③ 落库默认值 / 软删(2026-11-03 审计修复的回归守卫)───────────

// TestManager_Notify_DefaultMsgType 不传 MsgType,入库行必须是 "system"
//
// 探针来源:Message.Validate 是值接收者,在里面给 m.MsgType 赋默认值永远传
// 不回调用方 ⇒ 空 msg_type 直接入库。默认值已挪到 newRow(落库前唯一构造点),
// 这条用原始 SQL 查库断言,不经过 Manager 的读路径(防止读路径兜底把它盖住)。
func TestManager_Notify_DefaultMsgType(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	// 故意不传 MsgType
	if err := mgr.Notify(ctx, Message{UserID: 1, Title: "t"}); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.Raw(`SELECT msg_type FROM sys_inbox_message WHERE user_id = ?`, 1).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != "system" {
		t.Fatalf("入库 msg_type = %q, want %q(默认值没在落库前补上)", got, "system")
	}
}

// TestManager_Delete_SoftDelete 删除必须走软删,不是物理 DELETE
//
// 探针来源:Delete 原先 Table()+Delete(nil) 物理 DELETE,行直接消失 ——
// 绕开软删契约与挂在该模型上的 GORM 审计回调。现在:
//  1. List(typed 扫描,自带软删作用域)里不再出现;
//  2. UnreadCount(裸 Table 查询,作用域是显式补的)不再计入;
//  3. 库里行还在,deleted_at 已置位 —— 软删而非物理删;
//  4. IDOR:别人的消息删不到。
func TestManager_Delete_SoftDelete(t *testing.T) {
	db := openTestDB(t)
	mgr := New(db)
	ctx := context.Background()

	// user 1 两条,user 2 一条
	if err := mgr.Notify(ctx, Message{UserID: 1, Title: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Notify(ctx, Message{UserID: 1, Title: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Notify(ctx, Message{UserID: 2, Title: "c"}); err != nil {
		t.Fatal(err)
	}
	rows, _, err := mgr.List(ctx, ListFilter{UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	idA, err := strconv.ParseUint(rows[0].ID, 10, 64)
	if err != nil {
		t.Fatalf("解析 ID %q 失败: %v", rows[0].ID, err)
	}

	// 先验 IDOR:user 2 删 user 1 的 idA → 不报错,也不动
	if err := mgr.Delete(ctx, 2, idA); err != nil {
		t.Fatal(err)
	}
	var alive int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_inbox_message WHERE id = ? AND deleted_at IS NULL`, idA).Scan(&alive).Error; err != nil {
		t.Fatal(err)
	}
	if alive != 1 {
		t.Fatal("IDOR:user 2 竟然删掉了 user 1 的消息")
	}

	// 本人删 → 软删:行还在,deleted_at 已置位
	if err := mgr.Delete(ctx, 1, idA); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var deleted int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_inbox_message WHERE id = ? AND deleted_at IS NOT NULL`, idA).Scan(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted_at 置位的行 = %d, want 1 —— 被物理删了?", deleted)
	}

	// 列表(typed 扫描,软删作用域自动生效)不再出现
	_, total, err := mgr.List(ctx, ListFilter{UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("删除后 List total = %d, want 1", total)
	}
	// 未读数(裸 Table 查询,软删作用域是显式补的)不再计入
	n, err := mgr.UnreadCount(ctx, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("删除后 unread = %d, want 1(裸 Table 查询漏了软删作用域会把已删行也算进来)", n)
	}
}
