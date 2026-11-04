// 发送前守卫 —— M115
//
// # 这个文件解决什么
//
// 模板没过审时,阿里云会拒发,平台**只能在失败之后**知道:
// 服务端日志只有一句 `isv.SMS_TEMPLATE_ILLEGAL`,用户看到的是"发送失败"。
//
// 守卫把这件事提前:发送之前先问登记册"这张模板能不能用",
// 确定不能用就**当场说清楚是哪张模板、为什么**。
//
// # ⚠️ 三条口径,别混
//
//  1. **"确定不能用"只有三种**:rejected(服务商驳回)、cancelled(服务商撤回了审核)、
//     disabled(人工停用)。见 `Status.Blocked()`。
//  2. **"不确定"一律放行**:unknown / pending / reviewing 都不拦。
//     理由见 `Status.Blocked()` 的注释 —— 同步任务一挂,状态就停在 pending,
//     此时拦掉 = 平台自己把验证码通道锁死,而且没人知道为什么。
//  3. **登记册里的号跟这次要发的号不是同一个 ⇒ 放行**。
//     登记册的结论只对它登记的那个号有效;配置中心刚换了号、同步还没跑到,
//     此时拿旧号的"已驳回"去拦新号 = 拦错对象。
//
// # ⚠️ `Evaluate` 里那个 switch 是**真正的闸门**,不是装饰
//
// `Status.Blocked()` 决定"哪些状态算确定不行",而 `blockedInfo` 决定"怎么拦、说什么话"。
// 两者现在**同源**(Blocked() 从 blockedInfo 派生),所以不会再出现
// "新状态算确定发不出去、却因为查不到文案而落到放行分支"这种漂移。
// 加新状态时**只改 blockedInfo 一处**就够了。
package smstpl

import (
	"errors"
	"fmt"
)

// 守卫错误
var (
	// ErrTemplateRejected 模板被服务商驳回
	ErrTemplateRejected = errors.New("smstpl: 短信模板未通过服务商审核")
	// ErrTemplateCancelled 模板的审核被服务商取消
	//
	// ⚠️ **跟 ErrTemplateRejected 分开** —— 运维看到"未通过审核"会去改模板内容,
	// 看到"审核被取消"要办的是另一件事(重新送审 / 确认模板是不是被删了)。
	ErrTemplateCancelled = errors.New("smstpl: 短信模板的审核已被服务商取消")
	// ErrTemplateDisabled 模板被人工停用
	ErrTemplateDisabled = errors.New("smstpl: 短信模板已被人工停用")
)

// Policy 守卫策略
//
// ⚠️ **三档,默认 block** —— 默认值的选择理由:
//
//	驳回 / 停用是**确定**的状态,拦住只是把"阿里云拒发"换成"平台提前说清楚",
//	不会凭空少发能发的短信;而放行的话,用户永远只看到一句含糊的"发送失败"。
//	但确实存在"状态陈旧导致误拦"的风险(比如刚被驳回又立刻改好了,
//	同步还没跑到),所以留了 warn / off 两档给运维兜底。
type Policy string

const (
	// PolicyOff 不检查(完全放行)
	PolicyOff Policy = "off"
	// PolicyWarn 检查但只告警(不拦)
	PolicyWarn Policy = "warn"
	// PolicyBlock 检查并拦下(默认)
	PolicyBlock Policy = "block"
)

// ParsePolicy 解析策略字符串
//
// ⚠️ **未知值落到 block,而不是 off** ——
// 配置写错(比如 "blcok")时,"没检查"意味着模板状态这一整套功能静默失效,
// 而"检查"最坏结果是拦掉确定不可用的模板。**失效比误拦危险**。
func ParsePolicy(raw string) Policy {
	switch Policy(raw) {
	case PolicyOff:
		return PolicyOff
	case PolicyWarn:
		return PolicyWarn
	case PolicyBlock:
		return PolicyBlock
	default:
		return PolicyBlock
	}
}

// Decision 守卫结论
type Decision struct {
	// Blocked 是否拦下
	Blocked bool
	// Status 判定时登记册里的状态(未登记时是 unknown)
	Status Status
	// Reason 人类可读的原因 —— **非空就该记一条日志**
	// (warn 策略下不拦,但原因照样返回,调用方要记)
	Reason string
	// Err 拦下时的错误(blocked=false 时为 nil)
	Err error
}

// Guard 发送前守卫
type Guard struct {
	reg    *Registry
	policy Policy
}

// NewGuard 构造守卫
func NewGuard(reg *Registry, policy Policy) *Guard {
	if reg == nil {
		reg = NewRegistry()
	}
	return &Guard{reg: reg, policy: ParsePolicy(string(policy))}
}

// Policy 当前策略
func (g *Guard) Policy() Policy {
	if g == nil {
		return PolicyBlock
	}
	return g.policy
}

// Evaluate 判定某个场景的短信能不能发
//
// 参数 templateCode 是**这次要发的模板号**(可能为空 —— 空表示"调用方没带",
// 此时只按登记册的状态判)。
func (g *Guard) Evaluate(scene, templateCode string) Decision {
	if g == nil {
		return Decision{}
	}
	if g.policy == PolicyOff {
		return Decision{}
	}
	tpl, ok := g.reg.Get(scene)
	if !ok {
		// 没登记 = 不知道 = 放行(同步还没跑过 / 这个场景没配模板)
		return Decision{Status: StatusUnknown}
	}
	if templateCode != "" && tpl.Code != "" && tpl.Code != templateCode {
		// 登记册的结论只对它登记的那个号有效
		return Decision{
			Status: tpl.Status,
			Reason: fmt.Sprintf("模板号已变更(登记册=%s,本次=%s),审核状态待同步",
				tpl.Code, templateCode),
		}
	}
	// 状态判定:**唯一真源** blockedInfo(见下)
	//
	// ⚠️ **不要在这里再写一遍"哪些状态要拦"** ——
	// 判据只有 blockedInfo 一处(Status.Blocked() 也是从它派生的)。
	// 这里如果再抄一份,加新状态时漏改这一处 ⇒ 新状态"算确定发不出去"
	// 却悄悄落到放行分支 ⇒ 一张确定不可用的模板照发不误。
	sentinel, defaultReason := blockedInfo(tpl.Status)
	if sentinel == nil {
		// 不算"确定发不出去"(unknown / pending / reviewing)⇒ 放行
		return Decision{Status: tpl.Status}
	}

	reason := tpl.Reason
	if reason == "" {
		reason = defaultReason
	}
	err := fmt.Errorf("%w:%s(%s)", sentinel, scene, reason)
	if g.policy == PolicyWarn {
		return Decision{Status: tpl.Status, Reason: err.Error()}
	}
	return Decision{Blocked: true, Status: tpl.Status, Reason: err.Error(), Err: err}
}

// blockedInfo 状态 → (哨兵错误, 默认理由)
//
// ⚠️ **这是"哪些状态确定发不出去"的唯一真源** ——
//
//	`Status.Blocked()`(smstpl.go)也从这里派生,
//	守卫拦截时的文案也用这里的 Reason。
//
// 以前这是两份表(`Blocked()` 一个 switch、拦截文案另一个 switch),
// 加新状态时漏改一处 ⇒ 新状态"算确定发不出去"却拦不下来,
// 轻则消息不具体,重则落到放行分支 ⇒ 一张确定不可用的模板照发不误。
// **合成一份之后这个漂移就不可能发生。**
func blockedInfo(s Status) (error, string) {
	switch s {
	case StatusRejected:
		return ErrTemplateRejected, "服务商驳回"
	case StatusCancelled:
		return ErrTemplateCancelled, "服务商撤回了这次审核(需重新送审)"
	case StatusDisabled:
		return ErrTemplateDisabled, "人工停用"
	default:
		return nil, ""
	}
}

// Check 判定能不能发 —— 拦下时返回错误,否则返回 nil
//
// ⚠️ **调用方在 warn 策略下拿不到错误** —— 想记日志就用 Evaluate,
//
//	它的 Reason 在两种策略下都有值。
func (g *Guard) Check(scene, templateCode string) error {
	return g.Evaluate(scene, templateCode).Err
}
