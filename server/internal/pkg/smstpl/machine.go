// 短信模板审核状态机 —— M115
//
// 状态流转的合法性由**两张表**决定:状态之间的可达关系 × 来源的权限。
//
// ⚠️ **两张表都要过,任何一张不满足都拒绝** ——
// 只查"状态可达"会让运营能把模板标成"已通过"(从 pending 到 approved 是可达的),
// 那就绕过了阿里云的审核;只查"来源权限"会让状态机失去意义
// (审核中 → 已通过 不该一步到位)。
package smstpl

import (
	"errors"
	"fmt"
)

// 错误
var (
	// ErrIllegalTransition 非法状态流转(这两者之间没有边)
	ErrIllegalTransition = errors.New("smstpl: 非法状态流转")
	// ErrManualCannotJudge 人工不能给模板判"通过/驳回"
	ErrManualCannotJudge = errors.New("smstpl: 人工不能把模板标成已通过/已驳回(只有服务商说了算)")
	// ErrProviderCannotRevive 服务商同步不能复活人工停用的模板
	ErrProviderCannotRevive = errors.New("smstpl: 人工已停用的模板,服务商同步不能自动恢复")
	// ErrEmptyScene 场景为空
	ErrEmptyScene = errors.New("smstpl: 场景不能为空")
)

// legalTransitions 状态可达表
//
// ⚠️ **approved → reviewing 是允许的**(复审:阿里云可能因为签名变更等原因重新审一遍),
// **approved → rejected 也是允许的**(审核结论被撤销)。
//
// ⚠️ **pending → approved 必须允许** —— 同步任务是按分钟跑的,
// 模板送审到出结果可能只有几分钟,同步任务完全可能**第一次看到它就已经通过了**。
// 只允许 pending → reviewing 会让真实存在的"直接通过"卡在 pending,永远过不去。
// ⚠️ **每个状态都能回到 unknown** ——
// 换模板号时旧模板的审核结论**立即作废**(硬规则 3),新模板号过没过审平台不知道,
// 于是状态必须回到 unknown。这条边只在"模板号变了"时被走到。
//
// ⚠️ **每个状态都能被人工标成 pending** ——
// 运营在阿里云控制台改了模板内容重新送审,平台这边要能立刻标"待审核",
// 而不是干等着下一轮同步(同步最快也要 10 分钟)。缺这条边的话,
// "标记为待审核"这个操作在 approved / reviewing 状态下会直接报非法流转。
//
// ⚠️ **表是对称的(除了自己→自己)** ——
// 看着"可达性"这张表好像没约束力,其实约束在**来源**那张表上。
// 但对称是必须的:同步任务 10 分钟才跑一轮,服务商两次状态之间可能整个跳过去
// (cancelled → approved:重新送审后几分钟就通过,中间的"审核中"我们根本没看见)。
// 少一条边,模板就永远卡在旧状态。
var legalTransitions = map[Status]map[Status]bool{
	StatusUnknown: {
		StatusUnknown: true, StatusPending: true, StatusReviewing: true,
		StatusApproved: true, StatusRejected: true, StatusCancelled: true,
		StatusDisabled: true,
	},
	StatusPending: {
		StatusUnknown: true, StatusPending: true, StatusReviewing: true,
		StatusApproved: true, StatusRejected: true, StatusCancelled: true,
		StatusDisabled: true,
	},
	StatusReviewing: {
		StatusUnknown: true, StatusPending: true, StatusReviewing: true,
		StatusApproved: true, StatusRejected: true, StatusCancelled: true,
		StatusDisabled: true,
	},
	StatusApproved: {
		StatusUnknown: true, StatusPending: true, StatusReviewing: true,
		StatusApproved: true, StatusRejected: true, StatusCancelled: true,
		StatusDisabled: true,
	},
	StatusRejected: {
		StatusUnknown: true, StatusPending: true, StatusReviewing: true,
		StatusApproved: true, StatusRejected: true, StatusCancelled: true,
		StatusDisabled: true,
	},
	StatusCancelled: {
		StatusUnknown: true, StatusPending: true, StatusReviewing: true,
		StatusApproved: true, StatusRejected: true, StatusCancelled: true,
		StatusDisabled: true,
	},
	StatusDisabled: {
		StatusUnknown: true, StatusPending: true, StatusReviewing: true,
		StatusApproved: true, StatusRejected: true, StatusCancelled: true,
		StatusDisabled: true,
	},
}

// manualTargets 人工允许的目标状态
//
// ⚠️ **人工不能标 approved / rejected / cancelled** —— 见包注释「硬规则 1」。
// 人工能做的是:停用(disabled)、标记为待审核(pending)、恢复(pending)。
var manualTargets = map[Status]bool{
	StatusPending:  true,
	StatusDisabled: true,
}

// providerTargets 服务商允许的目标状态
var providerTargets = map[Status]bool{
	StatusReviewing: true,
	StatusApproved:  true,
	StatusRejected:  true,
	StatusCancelled: true,
}

// CanTransition 判断一次状态流转是否合法
//
// 参数:
//   - from 当前状态
//   - to   目标状态
//   - src  来源(人工 / 服务商)
//
// ⚠️ **from == to 一律合法**(幂等):服务商每次同步都会报同一个状态,
// 如果把它判成非法,同步任务每次都会记一条错误,日志会被刷屏。
// "没变化"不是"非法",调用方自己判断要不要记流转。
func CanTransition(from, to Status, src Source) error {
	if !to.Known() {
		return fmt.Errorf("%w: 未知目标状态 %q", ErrIllegalTransition, to)
	}
	if !from.Known() {
		return fmt.Errorf("%w: 未知当前状态 %q", ErrIllegalTransition, from)
	}
	if from == to {
		return nil
	}

	// 规则 1:来源权限
	switch src {
	case SourceManual:
		if !manualTargets[to] {
			return fmt.Errorf("%w: 人工不能置为 %q", ErrManualCannotJudge, to)
		}
	case SourceProvider:
		if !providerTargets[to] {
			return fmt.Errorf("%w: 服务商不能置为 %q", ErrIllegalTransition, to)
		}
		// 规则 2:人工停用的模板,服务商不能复活
		if from == StatusDisabled {
			return fmt.Errorf("%w: 当前状态 %q", ErrProviderCannotRevive, from)
		}
	default:
		return fmt.Errorf("%w: 未知来源 %q", ErrIllegalTransition, src)
	}

	// 规则 3:可达性
	if !legalTransitions[from][to] {
		return fmt.Errorf("%w: %s → %s", ErrIllegalTransition, from, to)
	}
	return nil
}
