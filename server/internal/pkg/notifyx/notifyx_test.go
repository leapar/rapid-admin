package notifyx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/emailx"
	"rapid-admin/internal/pkg/inbox"
)

// ============== 测试替身 ==============

// fakeChannel 记录收到的消息,可指定行为
type fakeChannel struct {
	name string
	// err 返回这个错误(nil = 成功)
	err error
	// panicMsg 非空 = panic 这个值
	panicMsg string
	// skip = 返回 ErrSkip
	skip bool

	got []Message
}

func (c *fakeChannel) Name() string { return c.name }

func (c *fakeChannel) Send(ctx context.Context, r Recipient, m Message) error {
	if c.panicMsg != "" {
		panic(c.panicMsg)
	}
	if c.skip {
		return fmt.Errorf("channel %s: %w", c.name, ErrSkip)
	}
	c.got = append(c.got, m)
	if c.err != nil {
		return c.err
	}
	return nil
}

// fakeNotifier 站内信替身
type fakeNotifier struct {
	msgs    []inbox.Message
	batches [][]inbox.Message
	err     error
}

func (n *fakeNotifier) Notify(ctx context.Context, msg inbox.Message) error {
	if n.err != nil {
		return n.err
	}
	n.msgs = append(n.msgs, msg)
	return nil
}

func (n *fakeNotifier) NotifyBatch(ctx context.Context, msgs []inbox.Message) error {
	if n.err != nil {
		return n.err
	}
	n.batches = append(n.batches, msgs)
	n.msgs = append(n.msgs, msgs...)
	return nil
}

// fakeResolver 固定收件人
type fakeResolver struct {
	r   Recipient
	err error
}

func (r *fakeResolver) Resolve(ctx context.Context, userID uint64) (Recipient, error) {
	if r.err != nil {
		return Recipient{}, r.err
	}
	return r.r, nil
}

// ============== Dispatcher ==============

func TestDispatcher_DeliversToEveryChannel(t *testing.T) {
	a := &fakeChannel{name: "a"}
	b := &fakeChannel{name: "b"}
	d := NewDispatcher(a, b)

	res := d.Dispatch(context.Background(),
		Recipient{UserID: 1},
		Message{Title: "标题", Content: "正文"})

	if len(res.Delivered) != 2 {
		t.Fatalf("Delivered = %v, want 2 条", res.Delivered)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("Failed = %v, want 空", res.Failed)
	}
	if len(a.got) != 1 || len(b.got) != 1 {
		t.Fatalf("两条通道各应收到 1 条: a=%d b=%d", len(a.got), len(b.got))
	}
	if a.got[0].Title != "标题" {
		t.Errorf("Title = %q, want 标题", a.got[0].Title)
	}
}

// 🔴 **核心语义:一条通道失败,其它通道照发。**
//
// 这条断言守的是"因为 A 失败所以 B 也不发了"这个错误实现 ——
// 那会让一次 SMTP 超时把站内信之外的推送也一起吃掉。
func TestDispatcher_OneChannelFailureDoesNotStopOthers(t *testing.T) {
	boom := errors.New("smtp 超时")
	bad := &fakeChannel{name: "bad", err: boom}
	good := &fakeChannel{name: "good"}

	d := NewDispatcher(bad, good)
	res := d.Dispatch(context.Background(), Recipient{UserID: 1}, Message{Title: "t"})

	if len(good.got) != 1 {
		t.Fatalf("失败通道之后的通道也应收到消息,实际收到 %d 条", len(good.got))
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %v, want 1 条", res.Failed)
	}
	if !errors.Is(res.Failed["bad"], boom) {
		t.Errorf("Failed[bad] = %v, want %v", res.Failed["bad"], boom)
	}
	if !res.OK() {
		t.Errorf("OK() 应为 true(至少一条通道成功)")
	}
}

func TestDispatcher_SkipIsNotFailure(t *testing.T) {
	skipper := &fakeChannel{name: "skipper", skip: true}
	ok := &fakeChannel{name: "ok"}

	d := NewDispatcher(skipper, ok)
	res := d.Dispatch(context.Background(), Recipient{UserID: 1}, Message{Title: "t"})

	if len(res.Failed) != 0 {
		t.Errorf("skip 不该算失败: %v", res.Failed)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != "skipper" {
		t.Errorf("Skipped = %v, want [skipper]", res.Skipped)
	}
}

// 🔴 **通道 panic 必须被隔离** —— 通知是旁路,一条通知不能带走主流程。
func TestDispatcher_PanicIsolated(t *testing.T) {
	panicky := &fakeChannel{name: "panicky", panicMsg: "boom"}
	after := &fakeChannel{name: "after"}

	d := NewDispatcher(panicky, after)
	res := d.Dispatch(context.Background(), Recipient{UserID: 1}, Message{Title: "t"})

	if len(after.got) != 1 {
		t.Fatalf("panic 之后的通道仍应收到消息,实际 %d 条", len(after.got))
	}
	if len(res.Failed) != 1 {
		t.Fatalf("Failed = %v, want 1 条(panic 记成失败)", res.Failed)
	}
	if !strings.Contains(res.Failed["panicky"].Error(), "boom") {
		t.Errorf("panic 值应进错误信息: %v", res.Failed["panicky"])
	}
}

func TestDispatcher_RegisterSameNameReplaces(t *testing.T) {
	a1 := &fakeChannel{name: "x"}
	a2 := &fakeChannel{name: "x"}
	d := NewDispatcher(a1)
	d.Register(a2)

	if got := d.Channels(); len(got) != 1 || got[0] != "x" {
		t.Fatalf("Channels = %v, want [x]", got)
	}
	d.Dispatch(context.Background(), Recipient{UserID: 1}, Message{Title: "t"})
	if len(a1.got) != 0 {
		t.Errorf("被替换掉的通道不应收到消息")
	}
	if len(a2.got) != 1 {
		t.Errorf("新通道应收到消息")
	}
}

func TestResult_ErrJoinsAllFailures(t *testing.T) {
	res := Result{Failed: map[string]error{
		"a": errors.New("err-a"),
		"b": errors.New("err-b"),
	}}
	err := res.Err()
	if err == nil {
		t.Fatal("Err() 应返非 nil")
	}
	// 两个失败都要看得见 —— 只留最后一个是丢信息
	if !strings.Contains(err.Error(), "err-a") || !strings.Contains(err.Error(), "err-b") {
		t.Errorf("Err() = %v, 两个失败都应在里面", err)
	}
}

// ============== EmailChannel ==============

func TestEmailChannel_SendsToRecipient(t *testing.T) {
	mock := emailx.NewMock()
	ch := NewEmailChannel(mock, "no-reply@x.com", "平台", "https://admin.x.com")

	err := ch.Send(context.Background(),
		Recipient{UserID: 1, Email: "u@x.com"},
		Message{Title: "【待审批】单子", Content: "有一条待办", Link: "/approval/123"})

	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if mock.Count() != 1 {
		t.Fatalf("发了 %d 封, want 1", mock.Count())
	}
	msg, _ := mock.Last()
	if msg.From != "no-reply@x.com" {
		t.Errorf("From = %q", msg.From)
	}
	if len(msg.To) != 1 || msg.To[0] != "u@x.com" {
		t.Errorf("To = %v", msg.To)
	}
	if msg.Subject != "【待审批】单子" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	// ⚠️ 相对链接必须拼成绝对地址 —— 邮件客户端没有"当前站点"的概念
	if !strings.Contains(msg.HTML, "https://admin.x.com/approval/123") {
		t.Errorf("HTML 里的链接应拼成绝对地址: %s", msg.HTML)
	}
	if !strings.Contains(msg.Text, "https://admin.x.com/approval/123") {
		t.Errorf("Text 里也应有绝对链接: %s", msg.Text)
	}
}

func TestEmailChannel_SkipsWhenNoEmail(t *testing.T) {
	mock := emailx.NewMock()
	ch := NewEmailChannel(mock, "no-reply@x.com", "", "")

	err := ch.Send(context.Background(), Recipient{UserID: 1}, Message{Title: "t"})

	if !errors.Is(err, ErrSkip) {
		t.Fatalf("err = %v, want ErrSkip", err)
	}
	if mock.Count() != 0 {
		t.Errorf("没邮箱就不该发")
	}
}

// 🔴 **没有 baseURL 时,相对链接要丢掉** ——
// 给一个点不开的相对链接,比不给链接更糟(收件人会以为应用坏了)。
func TestEmailChannel_RelativeLinkDroppedWithoutBaseURL(t *testing.T) {
	mock := emailx.NewMock()
	ch := NewEmailChannel(mock, "no-reply@x.com", "", "")

	err := ch.Send(context.Background(),
		Recipient{UserID: 1, Email: "u@x.com"},
		Message{Title: "t", Link: "/approval/123"})

	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	msg, _ := mock.Last()
	if strings.Contains(msg.HTML, "/approval/123") {
		t.Errorf("没有 baseURL 时不该保留相对链接: %s", msg.HTML)
	}
}

func TestEmailChannel_EscapesHTML(t *testing.T) {
	mock := emailx.NewMock()
	ch := NewEmailChannel(mock, "no-reply@x.com", "", "")

	// ⚠️ **注入点放在 Content,不放在 Title** ——
	// 探针实测过:只测 Title 的话,把 Content 的转义去掉测试**照样绿**
	// (Title 的转义把 <script> 变成了 &lt;script&gt;,断言照样通过)。
	// 标题和正文是两条独立的转义路径,必须各测各的。
	err := ch.Send(context.Background(),
		Recipient{UserID: 1, Email: "u@x.com"},
		Message{Title: "标题", Content: "<script>alert(1)</script>"})

	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	msg, _ := mock.Last()
	if strings.Contains(msg.HTML, "<script>") {
		t.Errorf("正文里的 script 标签应被转义: %s", msg.HTML)
	}
	if !strings.Contains(msg.HTML, "&lt;script&gt;") {
		t.Errorf("转义后的内容不对: %s", msg.HTML)
	}
}

func TestEmailChannel_EscapesTitle(t *testing.T) {
	mock := emailx.NewMock()
	ch := NewEmailChannel(mock, "no-reply@x.com", "", "")

	err := ch.Send(context.Background(),
		Recipient{UserID: 1, Email: "u@x.com"},
		Message{Title: "<script>alert(1)</script>", Content: "正文"})

	if err != nil {
		t.Fatalf("Send err: %v", err)
	}
	msg, _ := mock.Last()
	if strings.Contains(msg.HTML, "<script>") {
		t.Errorf("标题里的 script 标签应被转义: %s", msg.HTML)
	}
}

func TestEmailChannel_MissingFromIsError(t *testing.T) {
	mock := emailx.NewMock()
	ch := NewEmailChannel(mock, "", "", "")

	err := ch.Send(context.Background(),
		Recipient{UserID: 1, Email: "u@x.com"}, Message{Title: "t"})

	if err == nil {
		t.Fatal("没配发件地址应报错")
	}
	if errors.Is(err, ErrSkip) {
		t.Errorf("配置问题不该是 skip —— skip 会让\"配错了却没人发现\"")
	}
}

// ============== 收件人解析 ==============

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"u@x.com", "u@x.com"},
		{"  u@x.com  ", "u@x.com"},
		{"", ""},
		{"not-an-email", ""},
		{"@x.com", ""},
		{"u@", ""},
		{"u@x", ""},
		{"u @x.com", ""},
		// 🔴 将来 email 会走 crypto 加密,密文是 Base64 —— 必须被判成"没有邮箱",
		//    否则会把密文当收件地址发出去
		{"Uk9TU2lFbmNWMDFCTE5PSIsImtleI4iOnsiY2lwaGVydGV4dCI6ImEifQ==", ""},
	}
	for _, c := range cases {
		if got := normalizeEmail(c.in); got != c.want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"13800138000", "13800138000"},
		{"", ""},
		{"1380013800", ""},   // 10 位
		{"138001380000", ""}, // 12 位
		{"23800138000", ""},  // 不是 1 开头
		{"1380013800a", ""},
		{"Uk9TU2lFbmNWMDFC", ""}, // 密文
	}
	for _, c := range cases {
		if got := normalizePhone(c.in); got != c.want {
			t.Errorf("normalizePhone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDBResolver_Resolve(t *testing.T) {
	db := openTestDB(t)
	if err := db.AutoMigrate(&model.SysUser{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SysUser{
		BaseModel: model.BaseModel{ID: 1001},
		Username:  "u1",
		Nickname:  "U1",
		Email:     "u1@x.com",
		Phone:     "13800138000",
	}).Error; err != nil {
		t.Fatal(err)
	}

	r := NewDBResolver(db)
	got, err := r.Resolve(context.Background(), 1001)
	if err != nil {
		t.Fatalf("Resolve err: %v", err)
	}
	if got.Email != "u1@x.com" {
		t.Errorf("Email = %q", got.Email)
	}
	if got.Phone != "13800138000" {
		t.Errorf("Phone = %q", got.Phone)
	}
	if got.UserID != 1001 {
		t.Errorf("UserID = %d", got.UserID)
	}

	// 查不到 → error
	if _, err := r.Resolve(context.Background(), 999999); err == nil {
		t.Error("查不到用户应返 error")
	}
	// userID = 0 → error(而不是查 id=0)
	if _, err := r.Resolve(context.Background(), 0); err == nil {
		t.Error("userID=0 应返 error")
	}
}

// ============== Fanout ==============

func TestFanout_NotifiesInboxAndExtraChannel(t *testing.T) {
	inner := &fakeNotifier{}
	mock := emailx.NewMock()
	d := NewDispatcher(NewEmailChannel(mock, "no-reply@x.com", "", ""))

	f := NewSyncFanout(inner, d, &fakeResolver{r: Recipient{UserID: 7, Email: "u@x.com"}})
	err := f.Notify(context.Background(), inbox.Message{UserID: 7, Title: "待办"})

	if err != nil {
		t.Fatalf("Notify err: %v", err)
	}
	if len(inner.msgs) != 1 {
		t.Fatalf("站内信应落 1 条,实际 %d", len(inner.msgs))
	}
	if mock.Count() != 1 {
		t.Fatalf("邮件应发 1 封,实际 %d", mock.Count())
	}
	msg, _ := mock.Last()
	if msg.Subject != "待办" {
		t.Errorf("Subject = %q", msg.Subject)
	}
}

// 🔴 **站内信失败 = 整个通知失败**(它是"记录")
func TestFanout_InboxFailurePropagates(t *testing.T) {
	inner := &fakeNotifier{err: errors.New("db 挂了")}
	mock := emailx.NewMock()
	d := NewDispatcher(NewEmailChannel(mock, "no-reply@x.com", "", ""))

	f := NewSyncFanout(inner, d, &fakeResolver{r: Recipient{UserID: 7, Email: "u@x.com"}})
	err := f.Notify(context.Background(), inbox.Message{UserID: 7, Title: "待办"})

	if err == nil {
		t.Fatal("站内信失败应返 error")
	}
	if mock.Count() != 0 {
		t.Errorf("站内信失败时不该发邮件(记录都没写,发出去没有对应的事)")
	}
}

// 🔴 **额外交付失败 = 不影响返回值**(它是"送达")
func TestFanout_ExtraChannelFailureDoesNotFail(t *testing.T) {
	inner := &fakeNotifier{}
	bad := &fakeChannel{name: "bad", err: errors.New("smtp 超时")}
	d := NewDispatcher(bad)

	f := NewSyncFanout(inner, d, &fakeResolver{r: Recipient{UserID: 7}})
	err := f.Notify(context.Background(), inbox.Message{UserID: 7, Title: "待办"})

	if err != nil {
		t.Fatalf("额外交付失败不该让 Notify 返错: %v", err)
	}
	if len(inner.msgs) != 1 {
		t.Errorf("站内信仍应落库")
	}
}

func TestFanout_ResolverFailureStillDeliversInbox(t *testing.T) {
	inner := &fakeNotifier{}
	mock := emailx.NewMock()
	d := NewDispatcher(NewEmailChannel(mock, "no-reply@x.com", "", ""))

	f := NewSyncFanout(inner, d, &fakeResolver{err: errors.New("查不到人")})
	err := f.Notify(context.Background(), inbox.Message{UserID: 7, Title: "待办"})

	if err != nil {
		t.Fatalf("解析收件人失败不该让 Notify 返错: %v", err)
	}
	if len(inner.msgs) != 1 {
		t.Errorf("站内信仍应落库")
	}
	if mock.Count() != 0 {
		t.Errorf("没解析出收件人就不该发邮件")
	}
}

func TestFanout_NotifyBatch(t *testing.T) {
	inner := &fakeNotifier{}
	mock := emailx.NewMock()
	d := NewDispatcher(NewEmailChannel(mock, "no-reply@x.com", "", ""))

	f := NewSyncFanout(inner, d, &fakeResolver{r: Recipient{UserID: 7, Email: "u@x.com"}})
	err := f.NotifyBatch(context.Background(), []inbox.Message{
		{UserID: 7, Title: "一"},
		{UserID: 7, Title: "二"},
	})

	if err != nil {
		t.Fatalf("NotifyBatch err: %v", err)
	}
	if len(inner.batches) != 1 {
		t.Fatalf("站内信应批量落 1 次,实际 %d", len(inner.batches))
	}
	if mock.Count() != 2 {
		t.Fatalf("邮件应发 2 封,实际 %d", mock.Count())
	}
}

// 🔴 **批量落库失败 = 整批都不发邮件** ——
// 站内信是"记录",记录没写成就没有对应的事,发出去的邮件会指向一个不存在的单子。
func TestFanout_NotifyBatchInboxFailureDoesNotDeliverExtra(t *testing.T) {
	inner := &fakeNotifier{err: errors.New("db 挂了")}
	mock := emailx.NewMock()
	d := NewDispatcher(NewEmailChannel(mock, "no-reply@x.com", "", ""))

	f := NewSyncFanout(inner, d, &fakeResolver{r: Recipient{UserID: 7, Email: "u@x.com"}})
	err := f.NotifyBatch(context.Background(), []inbox.Message{{UserID: 7, Title: "一"}})

	if err == nil {
		t.Fatal("站内信批量失败应返 error")
	}
	if mock.Count() != 0 {
		t.Errorf("站内信批量失败时不该发邮件,实际发了 %d 封", mock.Count())
	}
}

// 🔴 **异步投递也不能吞掉 panic** —— goroutine 里逃出去的 panic 会打死进程。
func TestFanout_AsyncDeliverySurvivesPanic(t *testing.T) {
	inner := &fakeNotifier{}
	panicky := &fakeChannel{name: "panicky", panicMsg: "boom"}
	d := NewDispatcher(panicky)

	f := NewFanout(inner, d, &fakeResolver{r: Recipient{UserID: 7}})
	if err := f.Notify(context.Background(), inbox.Message{UserID: 7, Title: "待办"}); err != nil {
		t.Fatalf("Notify err: %v", err)
	}
	f.Wait() // 等异步投递结束
}

func TestFanout_NoDispatcherIsInboxOnly(t *testing.T) {
	inner := &fakeNotifier{}
	f := NewSyncFanout(inner, nil, nil)
	if err := f.Notify(context.Background(), inbox.Message{UserID: 7, Title: "待办"}); err != nil {
		t.Fatalf("Notify err: %v", err)
	}
	if len(inner.msgs) != 1 {
		t.Errorf("没有分发器时站内信仍应落库")
	}
}

// ============== 夹具 ==============

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
