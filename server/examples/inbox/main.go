// inbox —— 站内信 + 通知中心
//
// ## 这个包解决什么问题
//
// 业务模块(审批、报修、交付通知……)要给某个用户发一条站内信时,
// 只依赖一个接口:`Notifier.Notify(ctx, Message)` —— 不依赖具体实现、
// 不依赖 DB、不依赖 Redis。
//
// ## 关键点
//
//   - **业务模块依赖 Notifier 接口,不是 Manager**。这样换存储(比如
//     换成 MQ)时业务代码一行不用改
//   - **雪花 ID 在应用侧生成,不能交给 DB 自增**。全仓的表都是应用侧雪花;
//     `BaseModel.ID` 的 tag 是 `primaryKey;autoIncrement:false`,
//     MySQL 严格模式下直接报 1364「Field 'id' doesn't have a default value」
//   - **未读数变更经 pkg/push 推到 `push:user:<id>` 频道**,前端 SSE 实时刷红点。
//     推的只是"信号"(delta),**不含绝对未读数** —— 并发投递会让 delta 漂移,
//     真正的值以 `UnreadCount` 为准
//   - **Redis 不可用 ⇒ 推送静默丢弃**,前端轮询 `GET /inbox/unread-count` 兜底。
//     DB 写失败 ⇒ **返回错误**(业务层决定是否回滚主事务)—— 两个方向相反
//     是刻意的
//   - **列表过滤掉已过期,但保留已读**;未读判定是 `read_at IS NULL`,
//     不是"比时间戳"(未读没有时间戳可比)
//   - **MarkRead / Delete 都带 user_id 条件** —— 防 IDOR:
//     不属于自己的那条照样返 nil(不报错,也不动)
//
// ## 表从哪来
//
// 生产上表由 GORM AutoMigrate 建。示例里手写 DDL,是因为框架示例
// **不许 import internal/model**(那是业务模型,开源时不在范围内)——
// 见 examples/examples_test.go 的 forbiddenImports。
//
// ## 跑法
//
//	go run ./examples/inbox
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/pkg/inbox"
)

// inboxDDL 站内信表(生产上由 AutoMigrate 建,这里手写是为了不 import 业务模型)
const inboxDDL = `
CREATE TABLE IF NOT EXISTS sys_inbox_message (
	id          INTEGER PRIMARY KEY,
	created_at  DATETIME,
	updated_at  DATETIME,
	deleted_at  DATETIME,
	user_id     INTEGER,
	msg_type    TEXT,
	title       TEXT,
	content     TEXT,
	link        TEXT,
	ref_type    TEXT,
	ref_id      INTEGER,
	sender_id   INTEGER,
	read_at     DATETIME,
	expired_at  DATETIME
)`

func main() {
	ctx := context.Background()

	// ── 一、装配 ────────────────────────────────────────────────
	fmt.Println("=== 装配 ===")
	db, err := gorm.Open(sqlite.Open("file:inbox_demo?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		fmt.Println("开库失败:", err)
		return
	}
	if err := db.Exec(inboxDDL).Error; err != nil {
		fmt.Println("建表失败:", err)
		return
	}
	mgr := inbox.New(db)
	fmt.Println("inbox.Manager 已就绪(db = 内存 sqlite)")

	// ── 二、投递 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 投递 ===")
	//
	// 业务模块依赖的是 Notifier **接口**,不是 Manager:
	var notifier inbox.Notifier = mgr
	const userID = 8801

	err = notifier.Notify(ctx, inbox.Message{
		UserID:   userID,
		MsgType:  "biz",
		Title:    "交付确认单待签署",
		Content:  "《Rapid Admin 使用手册 V1.0》已生成,请在 7 天内签署。",
		Link:     "/delivery/confirm/20260301",
		RefType:  "delivery",
		RefID:    20260301,
		SenderID: 1,
	})
	fmt.Printf("  投递一条 → err = %v\n", err)

	// 批量投递(同事务;失败整批回滚)—— "通知项目下全部业主" 场景
	err = notifier.NotifyBatch(ctx, []inbox.Message{
		{UserID: userID, MsgType: "system", Title: "系统公告", Content: "本周六 02:00-04:00 系统维护。"},
		{UserID: userID, MsgType: "notice", Title: "物业费提醒", Content: "第三季度物业费将于 10 月 15 日开始计收。"},
	})
	fmt.Printf("  批量投递 2 条 → err = %v\n", err)

	// ── 三、校验 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 校验 ===")
	fmt.Printf("  UserID 为空   → %v\n", inbox.Message{}.Validate())
	fmt.Printf("  Title 为空    → %v\n", inbox.Message{UserID: 1}.Validate())
	fmt.Printf("  MsgType 为空  → %v(校验通过时默认 system)\n", inbox.Message{UserID: 1, Title: "x"}.Validate())

	// ── 四、未读数 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 未读数 ===")
	n, err := mgr.UnreadCount(ctx, userID, "")
	fmt.Printf("  全部未读     = %d (err=%v)\n", n, err)
	n, err = mgr.UnreadCount(ctx, userID, "biz")
	fmt.Printf("  只看 biz     = %d (err=%v)\n", n, err)
	fmt.Println("  ⇒ 角标数字以这个接口为准,推送里只带 delta(信号)")

	// ── 五、列表 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 列表 ===")
	views, total, err := mgr.List(ctx, inbox.ListFilter{UserID: userID})
	fmt.Printf("  total = %d, err = %v\n", total, err)
	for _, v := range views {
		fmt.Printf("    [%s] %s\n", v.MsgType, v.Title)
		fmt.Printf("        id=%s ref=%s/%s read_at=%v\n", v.ID, v.RefType, v.RefID, v.ReadAt)
	}
	fmt.Println("  ⇒ ID 是 string(雪花 19 位,裸数字会被 JS 的 2^53 静默截断)")
	fmt.Println("  ⇒ RefID / SenderID 为 0 时输出空串,不是 \"0\"")
	fmt.Println("  ⇒ ReadAt 未读是 null —— 前端判未读请比 null,别比时间戳")

	// 只看未读
	_, total, _ = mgr.List(ctx, inbox.ListFilter{UserID: userID, OnlyUnread: true})
	fmt.Printf("  只看未读 total = %d\n", total)

	// ── 六、标已读 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 标已读 ===")
	first := views[0]
	var id uint64
	fmt.Sscan(first.ID, &id)
	err = mgr.MarkRead(ctx, userID, id)
	fmt.Printf("  标已读 → err = %v\n", err)
	n, _ = mgr.UnreadCount(ctx, userID, "")
	fmt.Printf("  未读数 = %d\n", n)

	// 幂等:再标一次不报错
	err = mgr.MarkRead(ctx, userID, id)
	fmt.Printf("  再标一次(幂等)→ err = %v\n", err)

	// 防 IDOR:别人的消息标不到
	err = mgr.MarkRead(ctx, 9999, id)
	fmt.Printf("  标别人的消息 → err = %v\n", err)
	fmt.Println("  ⇒ 不属于该 user 的返 nil(不报错,也不动)")

	// 全部已读
	affected, err := mgr.MarkAllRead(ctx, userID, "")
	fmt.Printf("  全部已读 → 影响 %d 条 (err=%v)\n", affected, err)
	n, _ = mgr.UnreadCount(ctx, userID, "")
	fmt.Printf("  未读数 = %d\n", n)

	// ── 七、删除 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 删除 ===")
	err = mgr.Delete(ctx, userID, id)
	fmt.Printf("  删除 → err = %v\n", err)
	_, total, _ = mgr.List(ctx, inbox.ListFilter{UserID: userID})
	fmt.Printf("  剩余 %d 条\n", total)

	// ── 八、过期消息不显示 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 过期 ===")
	past := time.Now().Add(-time.Hour)
	_ = notifier.Notify(ctx, inbox.Message{
		UserID:    userID,
		MsgType:   "system",
		Title:     "已过期的通知",
		Content:   "这条已经过期了",
		ExpiredAt: &past,
	})
	_, total, _ = mgr.List(ctx, inbox.ListFilter{UserID: userID})
	fmt.Printf("  列表里剩 %d 条(过期的被过滤掉了)\n", total)
	n, _ = mgr.UnreadCount(ctx, userID, "")
	fmt.Printf("  未读数 = %d(过期的也不计入)\n", n)
	fmt.Println("  ⇒ 过滤掉已过期,但保留已读 —— 前端一般按未读优先")
}
