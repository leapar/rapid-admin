// smstpl —— 短信模板审核流
//
// ## 这个包解决什么问题
//
// 模板在阿里云那边过没过审,平台此前**完全不知道**:
//
//   - 运营在配置中心填了模板号,面板上看着一切正常
//   - 用户点「获取验证码」,短信没到
//   - 服务端日志只有一句 `isv.SMS_TEMPLATE_ILLEGAL`(阿里云的通用业务错误码,
//     它不告诉你"是模板没过审"还是"模板号写错了")
//   - 而模板其实早就被驳回了,驳回理由躺在阿里云控制台里没人看
//
// 这个包做三件事:知道状态、提前失败、留痕。
//
// ## 三条硬规则
//
//  1. **只有服务商能判「通过 / 驳回」**。人工只能"停用"或"标记为待审核"。
//     反过来的话,运营点一下"已通过"就绕过了阿里云的审核 —— 而模板没过审时
//     阿里云会拒发,于是面板显示"正常"、用户永远收不到验证码
//  2. **人工停用的模板,服务商同步不能自动复活**。停用是止血手段
//  3. **换了模板号,原来的审核状态立即作废**。审核的是**模板号**,不是场景;
//     拿旧号的通过状态去担保新号 = 假保证
//
// ## 关键点
//
//   - **"确定发不出去"只有三种**:rejected / cancelled / disabled。
//     unknown / pending / reviewing **一律放行** —— 同步任务一挂状态就停在
//     pending,此时拦掉 = 平台自己把登录验证码通道锁死,而且没人知道为什么
//   - **守卫策略三档 off / warn / block,默认 block**。未知策略值也落 block
//     (配置写错时,"没检查"意味着整套功能静默失效 —— 失效比误拦危险)
//   - **登记册里的号 ≠ 这次要发的号 ⇒ 放行**。拿旧号的"已驳回"去拦新号
//     = 拦错对象
//   - **状态没变且理由没变 ⇒ 不记历史**。服务商每次同步都报同一个状态,
//     每次都记一条的话历史会被"已通过 → 已通过"刷满
//
// ## 跑法
//
//	go run ./examples/smstpl
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/smstpl"
)

func main() {
	// ── 一、登记册 ──────────────────────────────────────────────
	fmt.Println("=== 登记模板 ===")
	reg := smstpl.NewRegistry()

	changed, tr, err := reg.Register(smstpl.SceneLogin, "SMS_100000000")
	fmt.Printf("  登记 login → changed=%v err=%v\n", changed, err)
	fmt.Printf("  新建不记历史(At.IsZero=%v):\"从没有到 unknown\"不是状态变化\n", tr.At.IsZero())

	// 再登记同一个号 ⇒ 没变化
	changed, _, err = reg.Register(smstpl.SceneLogin, "SMS_100000000")
	fmt.Printf("  再登记同一个号 → changed=%v err=%v\n", changed, err)

	// 换一个号 ⇒ 审核状态立即作废,并记一条历史
	changed, tr, err = reg.Register(smstpl.SceneLogin, "SMS_999")
	fmt.Printf("  换成 SMS_999 → changed=%v err=%v\n", changed, err)
	fmt.Printf("  历史:%s → %s(%s)\n", tr.From, tr.To, tr.Reason)
	fmt.Println("  ⇒ 硬规则 3:审核的是模板号,新号从没审过,旧状态不能担保它")

	// ── 二、状态机:来源权限 ────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 状态机(来源权限)===")
	// 人工想标"已通过" ⇒ 拒绝
	_, _, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved, smstpl.SourceManual, "", "zhangsan")
	fmt.Printf("  人工标 approved → err = %v\n", err)
	fmt.Println("  ⇒ 硬规则 1:人工不能替服务商判通过")

	// 人工停用 ⇒ 允许
	_, tr, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusDisabled, smstpl.SourceManual, "模板内容涉嫌违规,先止血", "zhangsan")
	fmt.Printf("  人工停用      → err = %v\n", err)
	fmt.Printf("  历史:%s → %s,操作人=%s\n", tr.From, tr.To, tr.Operator)
	fmt.Println("  ⇒ Source 说「是人还是服务商」,Operator 说「是哪个运维」——两个都要")

	// 服务商同步想复活 ⇒ 拒绝
	_, _, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved, smstpl.SourceProvider, "阿里云说通过了", "")
	fmt.Printf("  服务商复活已停用 → err = %v\n", err)
	fmt.Println("  ⇒ 硬规则 2:停用是止血手段,同步任务每 10 分钟跑一次,不能把运维刚关的又打开")

	// 人工恢复 ⇒ 允许(回到 pending,不是 approved)
	_, _, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusPending, smstpl.SourceManual, "已改好模板内容,重新送审", "zhangsan")
	fmt.Printf("  人工恢复(→ pending)→ err = %v\n", err)
	fmt.Println("  ⇒ 人工能做的只有两件:停用、标记为待审核")

	// ── 三、服务商同步 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 服务商同步 ===")
	_, tr, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved, smstpl.SourceProvider, "阿里云审核通过", "")
	fmt.Printf("  provider → approved → err = %v\n", err)
	fmt.Printf("  历史:%s → %s,来源=%s\n", tr.From, tr.To, tr.Source)

	// 同一个状态再同步一次 ⇒ 不记历史
	changed, tr, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved, smstpl.SourceProvider, "阿里云审核通过", "")
	fmt.Printf("  再同步一次同状态 → changed=%v,记历史=%v\n", changed, !tr.At.IsZero())
	fmt.Println("  ⇒ 状态没变且理由没变 ⇒ 不记历史,否则历史会被「已通过 → 已通过」刷满")

	// 理由变了 ⇒ 要记(那是新信息)
	_, tr, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved, smstpl.SourceProvider, "阿里云审核通过(已补充说明签名变更)", "")
	fmt.Printf("  理由变了 → 记历史=%v,reason=%q\n", !tr.At.IsZero(), tr.Reason)

	// ── 四、守卫 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 守卫:block 策略 ===")
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)
	d := guard.Evaluate(smstpl.SceneLogin, "SMS_999")
	fmt.Printf("  approved  → blocked=%v reason=%q\n", d.Blocked, d.Reason)
	fmt.Println("  ⇒ approved 是唯一「能用」的状态")

	// 让它变成 rejected
	_, _, err = reg.Apply(smstpl.SceneLogin, smstpl.StatusRejected, smstpl.SourceProvider, "内容不合规", "")
	if err != nil {
		fmt.Println("  改成 rejected 出错:", err)
		return
	}
	d = guard.Evaluate(smstpl.SceneLogin, "SMS_999")
	fmt.Printf("  rejected  → blocked=%v\n", d.Blocked)
	fmt.Printf("  err = %v\n", d.Err)
	fmt.Println("  ⇒ 拦下时必须让调用方拿到「为什么」——上层会把原文拼给用户")

	// ── 五、"不确定"一律放行 ────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 不确定的状态:一律放行 ===")
	for _, st := range []smstpl.Status{
		smstpl.StatusUnknown,
		smstpl.StatusPending,
		smstpl.StatusReviewing,
		smstpl.StatusApproved,
		smstpl.StatusRejected,
		smstpl.StatusCancelled,
		smstpl.StatusDisabled,
	} {
		fmt.Printf("  %-10s blocked=%-5v usable=%v\n", st, st.Blocked(), st.Usable())
	}
	fmt.Println("  ⇒ blocked 只对 rejected / cancelled / disabled 为真")
	fmt.Println("    pending / reviewing 是「暂时」,同步任务一挂就永远停在那儿,")
	fmt.Println("    此时拦掉 = 平台自己把验证码通道锁死,而且没人知道为什么")

	// ── 六、模板号对不上 ⇒ 放行 ────────────────────────────────
	fmt.Println()
	fmt.Println("=== 登记册的号 ≠ 这次要发的号 ===")
	d = guard.Evaluate(smstpl.SceneLogin, "SMS_OTHER")
	fmt.Printf("  blocked=%v reason=%q\n", d.Blocked, d.Reason)
	fmt.Println("  ⇒ 登记册的结论只对它登记的那个号有效。配置中心刚换了号、")
	fmt.Println("    同步还没跑到,此时拿旧号的「已驳回」去拦新号 = 拦错对象")

	// ── 七、三档策略 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 三档策略 ===")
	fmt.Printf("  ParsePolicy(\"off\")   = %s\n", smstpl.ParsePolicy("off"))
	fmt.Printf("  ParsePolicy(\"warn\")  = %s\n", smstpl.ParsePolicy("warn"))
	fmt.Printf("  ParsePolicy(\"block\") = %s\n", smstpl.ParsePolicy("block"))
	fmt.Printf("  ParsePolicy(\"blcok\") = %s ← 配置写错\n", smstpl.ParsePolicy("blcok"))
	fmt.Println("  ⇒ 未知值落 block:「没检查」意味着整套功能静默失效,失效比误拦危险")

	warnGuard := smstpl.NewGuard(reg, smstpl.PolicyWarn)
	d = warnGuard.Evaluate(smstpl.SceneLogin, "SMS_999")
	fmt.Printf("  warn 策略 → blocked=%v,但 reason 仍有值=%v\n", d.Blocked, d.Reason != "")
	fmt.Println("  ⇒ warn 下不拦但要记日志,Reason 在两种策略下都有值")

	offGuard := smstpl.NewGuard(reg, smstpl.PolicyOff)
	d = offGuard.Evaluate(smstpl.SceneLogin, "SMS_999")
	fmt.Printf("  off 策略  → blocked=%v status=%q\n", d.Blocked, d.Status)
	fmt.Println("  ⇒ off 完全不检查")

	// ── 八、登记册快照 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 登记册快照 ===")
	for _, t := range reg.List() {
		fmt.Printf("  %-16s code=%-16s status=%-10s source=%s\n", t.Scene, t.Code, t.Status, t.Source)
	}
	fmt.Println()
	fmt.Println("  流转历史(最新在前):")
	for _, h := range reg.History(smstpl.SceneLogin) {
		fmt.Printf("    %s → %s  来源=%-8s 操作人=%-8s %s\n", h.From, h.To, h.Source, h.Operator, h.Reason)
	}
}
