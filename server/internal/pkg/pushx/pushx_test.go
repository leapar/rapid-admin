// pushx 单测(§1.9 #10 P2)
package pushx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func validNotif() Notification {
	return Notification{
		Title:   "新消息",
		Body:    "您有一条新工单",
		Targets: []Target{{Type: "account", Value: "user_123"}},
	}
}

func TestNotification_IsValid(t *testing.T) {
	cases := []struct {
		name    string
		mut     func(n *Notification)
		wantErr bool
	}{
		{"valid", func(n *Notification) {}, false},
		{"no_title", func(n *Notification) { n.Title = "" }, true},
		{"no_body", func(n *Notification) { n.Body = "" }, true},
		{"no_target", func(n *Notification) { n.Targets = nil }, true},
		{"target_no_type", func(n *Notification) { n.Targets = []Target{{Value: "x"}} }, true},
		{"target_no_value", func(n *Notification) { n.Targets = []Target{{Type: "account"}} }, true},
		{"silent_no_title_ok", func(n *Notification) { n.Title = ""; n.Body = ""; n.Silent = true }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := validNotif()
			c.mut(&n)
			err := n.IsValid()
			if (err != nil) != c.wantErr {
				t.Errorf("IsValid err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

func TestMockSender_Record(t *testing.T) {
	m := NewMock()
	for i := 0; i < 3; i++ {
		if err := m.Push(context.Background(), validNotif()); err != nil {
			t.Fatalf("Push err: %v", err)
		}
	}
	if m.Count() != 3 {
		t.Errorf("Count = %d, want 3", m.Count())
	}
	last, ok := m.Last()
	if !ok || last.Title != "新消息" {
		t.Errorf("Last Title = %q", last.Title)
	}
	m.Reset()
	if m.Count() != 0 {
		t.Errorf("after Reset Count = %d", m.Count())
	}
}

func TestMockSender_FailOn(t *testing.T) {
	m := NewMock()
	m.FailOn = errors.New("forced fail")
	if err := m.Push(context.Background(), validNotif()); err == nil {
		t.Error("expected err, got nil")
	}
	if m.Count() != 0 {
		t.Errorf("Count after fail = %d", m.Count())
	}
}

func TestMockSender_InvalidMsg(t *testing.T) {
	m := NewMock()
	if err := m.Push(context.Background(), Notification{Title: "x"}); err == nil {
		t.Error("expected invalid err, got nil")
	}
}

func TestNewAliyunEMNS_Validation(t *testing.T) {
	if _, err := NewAliyunEMNS(AliyunConfig{}); err == nil {
		t.Error("expected access_key required")
	}
	if _, err := NewAliyunEMNS(AliyunConfig{AccessKey: "k", AccessSecret: "s"}); err == nil {
		t.Error("expected app_key required")
	}
	s, err := NewAliyunEMNS(AliyunConfig{
		AccessKey: "k", AccessSecret: "s", AppKey: "app",
	})
	if err != nil {
		t.Fatalf("NewAliyunEMNS err: %v", err)
	}
	if s.cfg.Region != "cn-hangzhou" {
		t.Errorf("Region = %q, want cn-hangzhou", s.cfg.Region)
	}
	if s.cfg.DefaultAPNSEnv != "PROD" {
		t.Errorf("DefaultAPNSEnv = %q, want PROD", s.cfg.DefaultAPNSEnv)
	}
	if s.cfg.TargetMax != 100 {
		t.Errorf("TargetMax = %d, want 100", s.cfg.TargetMax)
	}
}

func TestAliyunEMNS_InvalidMsg(t *testing.T) {
	s, _ := NewAliyunEMNS(AliyunConfig{AccessKey: "k", AccessSecret: "s", AppKey: "app"})
	if err := s.Push(context.Background(), Notification{Title: "x"}); err == nil {
		t.Error("expected invalid err, got nil")
	}
}

func TestChunkTargets(t *testing.T) {
	targets := make([]Target, 250)
	for i := range targets {
		targets[i] = Target{Type: "device", Value: "d" + string(rune('a'+i%26))}
	}
	chunks := chunkTargets(targets, 100)
	if len(chunks) != 3 {
		t.Errorf("len = %d, want 3", len(chunks))
	}
	if len(chunks[0]) != 100 || len(chunks[1]) != 100 || len(chunks[2]) != 50 {
		t.Errorf("chunk sizes = %d,%d,%d, want 100,100,50",
			len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
}

func TestJoinTargets(t *testing.T) {
	ts := []Target{
		{Type: "account", Value: "u1"},
		{Type: "device", Value: "d1"},
	}
	if got := joinTargetTypes(ts); got != "account,device" {
		t.Errorf("types = %q", got)
	}
	if got := joinTargetValues(ts); got != "u1,d1" {
		t.Errorf("values = %q", got)
	}
}

func TestPercentEncodeEMNS(t *testing.T) {
	cases := []struct{ in, out string }{
		{"abc", "abc"},
		{"a b", "a%20b"},
		{"中", "%E4%B8%AD"},
		{"+", "%2B"},
	}
	for _, c := range cases {
		got := percentEncodeEMNS(c.in)
		if got != c.out {
			t.Errorf("percentEncodeEMNS(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestRandNonceEMNS(t *testing.T) {
	for i := 0; i < 50; i++ {
		if len(randNonceEMNS()) != 32 {
			t.Errorf("nonce length != 32")
			return
		}
	}
}

func TestConsoleSender_Send(t *testing.T) {
	// ⚠️ **Out 要单独断言** —— 2026-10-25 修的正是"输出被吞"那个 bug:
	//   当时 Out 是个空的内存 buffer,内容只进了内部留档,
	//   而老测试**只读内部留档**,所以 bug 在场测试照样绿。
	//   只断言 Output() 的话,"根本没往 Out 写"这件事永远抓不到。
	var out bytes.Buffer
	s := NewConsolePush()
	s.Out = &out

	err := s.Push(context.Background(), validNotif())
	if err != nil {
		t.Fatalf("Push err: %v", err)
	}
	for _, want := range []string{"新消息", "您有一条新工单", "account=user_123"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Out 里没有 %q(内容没真的打印出去): %s", want, out.String())
		}
		if !strings.Contains(s.Output(), want) {
			t.Errorf("Output() 里没有 %q: %s", want, s.Output())
		}
	}
}

// TestConsoleSender_ConcurrentPush 守住内部留档的锁
//
// ⚠️ **通知分发器的额外交付是异步的** —— 两条通知可能同时打进同一个
//
//	sender。留档 buffer 是共享可变状态,不加锁就是数据竞争
//	(-race 会报)。这个用例本身就是探针:去掉锁,go test -race 变红。
func TestConsoleSender_ConcurrentPush(t *testing.T) {
	s := NewConsolePush()
	s.Out = io.Discard

	const n = 32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			notif := validNotif()
			notif.Title = fmt.Sprintf("标题-%d", i)
			if err := s.Push(context.Background(), notif); err != nil {
				t.Errorf("Push err: %v", err)
			}
		}(i)
	}
	wg.Wait()

	out := s.Output()
	for i := 0; i < n; i++ {
		if !strings.Contains(out, fmt.Sprintf("标题-%d", i)) {
			t.Errorf("留档里少了 标题-%d", i)
		}
	}
}

// fakeEMNS 拦掉真实 HTTP,按用例给 body
type fakeEMNS struct{ body string }

func (f fakeEMNS) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

// TestAliyunEMNS_NonJSONBody HTTP 200 但 body 是 HTML(LB/WAF 注入)时
// 必须报错,不能当成推送成功 —— 以前这里 return nil,推送静默丢失,
// 连一条日志都没有。
func TestAliyunEMNS_NonJSONBody(t *testing.T) {
	s, _ := NewAliyunEMNS(AliyunConfig{AccessKey: "k", AccessSecret: "s", AppKey: "app"})
	s.cli = &http.Client{Transport: fakeEMNS{body: "<html><body>Blocked by WAF</body></html>"}}

	err := s.Push(context.Background(), validNotif())
	if err == nil {
		t.Fatal("非 JSON body 不该被当成推送成功")
	}
	var pe *PushError
	if !errors.As(err, &pe) {
		t.Fatalf("错误类型 = %T,应该是 *PushError", err)
	}
	if pe.Code != "unparseable_response" {
		t.Errorf("Code = %q, want unparseable_response", pe.Code)
	}
	if pe.Retry {
		t.Errorf("网关策略性拦截不是瞬时故障,不该标可重试")
	}
	if !strings.Contains(pe.Message, "Blocked by WAF") {
		t.Errorf("错误信息该带 body 摘要,方便定位是谁拦的: %q", pe.Message)
	}
}

// TestAliyunEMNS_ResponseCodes Code 的成功/失败判定:数字 0/200 成功,
// 其他数字与字符串错误码都是业务失败。
func TestAliyunEMNS_ResponseCodes(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantErr  bool
		wantCode string
	}{
		{"成功 Code=0", `{"RequestId":"r","Code":0,"Message":"ok","MessageId":"m"}`, false, ""},
		{"成功 Code=200", `{"Code":200,"Message":"ok"}`, false, ""},
		{"业务错误数字码", `{"Code":10005,"Message":"target invalid"}`, true, "biz_10005"},
		{"业务错误字符串码", `{"Code":"Throttling","Message":"too fast"}`, true, "biz_Throttling"},
		{"缺 Code 字段", `{"RequestId":"r"}`, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := NewAliyunEMNS(AliyunConfig{AccessKey: "k", AccessSecret: "s", AppKey: "app"})
			s.cli = &http.Client{Transport: fakeEMNS{body: c.body}}
			err := s.Push(context.Background(), validNotif())
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if c.wantErr {
				var pe *PushError
				if !errors.As(err, &pe) || pe.Code != c.wantCode {
					t.Fatalf("Code = %v, want %q", err, c.wantCode)
				}
			}
		})
	}
}

func TestPushError_Error(t *testing.T) {
	e := &PushError{Provider: "aliyun-emns", Code: "biz_400", Message: "Target invalid"}
	if e.Error() != "aliyun-emns: biz_400: Target invalid" {
		t.Errorf("Error() = %q", e.Error())
	}
}
