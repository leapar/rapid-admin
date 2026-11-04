// Package notifyx 多通道通知分发(§1.9 #9+#10 P2)
//
// 目的:把"要通知某人"这件事,从"用哪个通道发"里拆开。
//
//	调用方只描述 **给谁、说什么**
//	分发器决定 **走哪几条通道**、以及 **某条通道挂了怎么办**
//
// 现状(本包落地前):
//   - emailx(15 个单测)、pushx(12 个单测)都**零生产调用方** ——
//     清单里写着"✅ P2",但那只是"代码完备",不是"能力已通"
//   - 唯一真正在跑的通道是站内信(pkg/inbox,M34 起)
//
// 本包补的就是那条缺口:**让通道有装配点、有真实调用方、有降级语义**。
//
// 三条纪律:
//
//  1. **通知是旁路,永不阻断主流程。**
//     一条通道失败只记进 Result,调用方的业务事务照常提交。
//     反过来就不对了 —— 审批单已经落库,不该因为"邮件没发出去"让审批失败。
//
//  2. **单通道失败不牵连其它通道。**
//     邮件挂了不影响站内信,站内信挂了不影响推送。
//     "因为 A 失败所以 B 也不发了"是分发器最常见的错误实现。
//
//  3. **通道的 panic 必须被隔离。**
//     通知是主流程的旁支,一条通知把整个进程带走是不可接受的失败方向。
//     每个通道的调用都 recover,panic 记成该通道 failed。
//
// ⚠️ **本包不认识审批 / 站内信的业务语义** —— 它只认 Recipient + Message。
//
//	"新待办投给审批人、结果投给发起人"那套规则在
//	internal/service/approval_inbox.go,与本包平行。
package notifyx

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Recipient 收件人
//
// ⚠️ **只有 UserID 是必填的**,Email / Phone 都是可选:
//
//	一个用户可能没填邮箱(后台账号很常见),那就不该发邮件 ——
//	但**站内信照发**。所以"没有某个联系方式"是 **skipped**,不是 failed。
type Recipient struct {
	// UserID 平台内用户 id(必填,站内信靠它落库)
	UserID uint64
	// Email 邮箱(可选)
	Email string
	// Phone 手机号(可选)
	Phone string
}

// Message 通知内容
//
// ⚠️ **不重复定义站内信的字段** —— Title / Content / Link / RefType / RefID
//
//	这几个的语义跟 pkg/inbox.Message 完全一致,重复一份就是漂移的起点。
//	Fanout 的做法是**直接复用 inbox.Message**,本结构只服务于
//	"没有站内信落库需求的纯通道投递"(比如只发邮件的告警)。
type Message struct {
	// Title 标题(邮件的 Subject / 推送的 Title)
	Title string
	// Content 正文(纯文本)
	Content string
	// Link 详情链接(相对路径,通道自己拼成绝对地址)
	Link string
	// RefType / RefID 关联资源(供通道决定深链 / 统计)
	RefType string
	RefID   uint64
	// Data 附加数据(推送的 Data 字段)
	Data map[string]string
}

// Channel 一条投递通道
//
// ⚠️ **Send 返回 error = 这条通道失败了**,但**不代表整条通知失败** ——
//
//	分发器会把失败收进 Result,其它通道照发。
type Channel interface {
	// Name 通道名(日志 / 监控用)。全仓唯一,别重名。
	Name() string
	// Send 投递一条。返回 ErrSkip 表示"这个收件人不适用这条通道"
	// (比如没邮箱),**不算失败**。
	Send(ctx context.Context, r Recipient, m Message) error
}

// ErrSkip 通道主动跳过(不是失败)
//
// ⚠️ **用 errors.Is 判,不要用 ==** —— 通道实现可能在 ErrSkip 上再包一层
//
//	(带上下文),直接 == 会漏判,把"跳过"记成"失败"。
var ErrSkip = errors.New("notifyx: channel skipped")

// Result 一次分发的结果
//
// ⚠️ **失败是 map 而不是 slice** —— 多个通道可能同时失败,
//
//	map 才能一个通道对一条错误;slice 只能记"有失败",查不出是哪条。
type Result struct {
	// Delivered 投递成功的通道名
	Delivered []string
	// Skipped 主动跳过的通道名(收件人不适用)
	Skipped []string
	// Failed 失败的通道名 → 错误
	Failed map[string]error
}

// OK 是否至少有一条通道投递成功
func (r Result) OK() bool { return len(r.Delivered) > 0 }

// Err 把所有失败汇总成一个 error(没有失败则返 nil)
//
// ⚠️ **用 errors.Join** —— 多个失败都要看得见,只留最后一个是丢信息。
func (r Result) Err() error {
	if len(r.Failed) == 0 {
		return nil
	}
	errs := make([]error, 0, len(r.Failed))
	for name, err := range r.Failed {
		errs = append(errs, fmt.Errorf("%s: %w", name, err))
	}
	return errors.Join(errs...)
}

// Dispatcher 多通道分发器
//
// ⚠️ **线程安全** —— 注册在启动期做、派发在运行期做,两者可能并发
//
//	(热重载配置时重新注册通道)。
type Dispatcher struct {
	mu       sync.RWMutex
	channels []Channel
}

// NewDispatcher 构造(可预置通道)
func NewDispatcher(channels ...Channel) *Dispatcher {
	d := &Dispatcher{}
	for _, c := range channels {
		d.Register(c)
	}
	return d
}

// Register 注册一条通道
//
// ⚠️ **同名通道直接覆盖** —— 重复注册同一个名字会让 Result 里的通道名
//
//	出现两次,排查时根本分不清是哪一次投递。覆盖比并存可查。
func (d *Dispatcher) Register(c Channel) {
	if c == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, existing := range d.channels {
		if existing.Name() == c.Name() {
			d.channels[i] = c
			return
		}
	}
	d.channels = append(d.channels, c)
}

// Channels 已注册的通道名
func (d *Dispatcher) Channels() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	names := make([]string, 0, len(d.channels))
	for _, c := range d.channels {
		names = append(names, c.Name())
	}
	return names
}

// Dispatch 按注册顺序逐条投递
//
// ⚠️ **串行,不并发** —— 理由三条:
//  1. 通知不是热路径,串行的耗时可以接受
//  2. 串行时 Result 的通道顺序 = 注册顺序,**可复现**;
//     并发时日志和 Result 的顺序每次都不一样,排查时对不上
//  3. 邮件(SMTP)本身就有连接成本,并发发反而更容易被对方限流
//
// 🔴 **每条通道都 recover** —— 一条通知的 panic 不能带走主流程。
//
//	panic 记成该通道 failed,其余通道照发。
func (d *Dispatcher) Dispatch(ctx context.Context, r Recipient, m Message) Result {
	d.mu.RLock()
	channels := make([]Channel, len(d.channels))
	copy(channels, d.channels)
	d.mu.RUnlock()

	res := Result{Failed: map[string]error{}}
	for _, c := range channels {
		err := safeSend(ctx, c, r, m)
		switch {
		case err == nil:
			res.Delivered = append(res.Delivered, c.Name())
		case errors.Is(err, ErrSkip):
			res.Skipped = append(res.Skipped, c.Name())
		default:
			res.Failed[c.Name()] = err
		}
	}
	return res
}

// safeSend 调一条通道,把 panic 变成 error
func safeSend(ctx context.Context, c Channel, r Recipient, m Message) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("notifyx: channel %s panicked: %v", c.Name(), rec)
		}
	}()
	return c.Send(ctx, r, m)
}
