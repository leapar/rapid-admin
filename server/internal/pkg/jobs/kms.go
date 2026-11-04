package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/crypto"
	"rapid-admin/internal/pkg/logger"
)

// retainedWarnThreshold 保留版本数超过这个值就告警。
//
// 2 是有依据的:一次轮转留下 1 把旧 key 用来解历史数据是**正常且必要**的
// (信封里标着 key_version,老 key 删了就解不开)。留到 3 把意味着:
//   - 要么轮转做完很久了没人回收(旧 key 长期在册,轮转等于没做)
//   - 要么数据从来没被 re-encrypt 过,新 key 只是个摆设
//
// 两种都是"轮转流程没走完",值得看一眼,但不该让任务失败。
const retainedWarnThreshold = 2

// rotationStateFn / hasLegacyKeyFn 是给单测留的替换点。
//
// 为什么需要:crypto.RotationState 的 RoundTripOK 在**今天的不变式下几乎不可能为
// false**(mustBuildTier 保证 active 版本一定有 cipher、key 长度一定合法),
// 所以 kmsRotateCheck 的硬失败分支没法用真实 registry 触发。没有替换点的话,
// 那条分支就只是一段从没被执行过的代码 —— 恰恰是最容易写错、又最该被测的分支。
//
// 只在 _test.go 里被覆写,生产路径恒等于 crypto.RotationState / crypto.HasLegacyKey。
var (
	rotationStateFn = crypto.RotationState
	hasLegacyKeyFn  = crypto.HasLegacyKey
)

// kmsRotateCheck 密钥轮转就绪检查。
//
// ⚠️ **它检查的是 key registry,不是业务数据。** 它不扫描任何业务表,
// 不 re-encrypt 任何字段 —— 那些是各业务模块自己的事(每张表的密文列在哪、
// 怎么批量重写,只有业务自己知道)。这个任务只回答两个问题:
//  1. 「现在加密 / 解密这条路走得通吗?」
//  2. 「轮转流程有没有停在半路上?」(新 key 没切 Active / 旧 key 一直不回收)
//
// 为什么要定时跑而不是启动时跑一次:
// 「历史版本堆积」「加了新 key 却忘了切 Active」这两种状态启动时是看不出来的
// (都合法、都跑得通),只有等几个月后有人加第三把 key 才发现就晚了。
//
// 返回 error 的只有一种情况:**某个已配置的 tier 加解密往返失败**。
// 「tier 没配」不算失败(见下方 configuredAny 的注释)。
func kmsRotateCheck(ctx context.Context) error {
	tiers := []crypto.Tier{crypto.TierPII, crypto.TierSecret, crypto.TierNormal}

	var (
		configuredAny bool
		hardErrs      []string
		warns         []string
	)

	for _, t := range tiers {
		// ctx 检查放在循环里:这个函数理论上很快,但它拿的是全局锁,
		// 不检查 ctx 会让一个已经超时的任务继续占着锁
		if err := ctx.Err(); err != nil {
			return err
		}

		st, err := rotationStateFn(t)
		if err != nil {
			// 没配这个 tier。**不是错误** —— 只用 PII 不加密 Secret 的部署
			// 完全合法。三个都没配更是常态(没启 KMS 的极简部署),
			// 那就只记一条 Info,别天天报 WARN
			logger.L().Debug("framework: 密钥 tier 未配置,跳过检查",
				zap.String("tier", string(t)))
			continue
		}
		configuredAny = true

		if !st.RoundTripOK {
			// 硬失败:这条 tier 现在加密或解密是坏的
			hardErrs = append(hardErrs, fmt.Sprintf("tier=%s %s", t, st.Problem))
			logger.L().Error("framework: 密钥加解密往返失败",
				zap.String("tier", string(t)),
				zap.String("problem", st.Problem),
				zap.Int("active_version", st.ActiveVersion))
			continue
		}

		if len(st.RetainedVersions) > retainedWarnThreshold {
			warns = append(warns, fmt.Sprintf("tier=%s 保留了 %d 个历史版本(v%d 起)",
				t, len(st.RetainedVersions), st.RetainedVersions[0]))
		}
		if !st.ActiveIsHighest {
			// 不是硬失败:有人加了 v3 但没把 Active 翻过去,新数据还在用 v2 加密。
			// 这在"先把新 key 灌进去、观察几天再切"的轮转流程里可能是**故意的**,
			// 所以只告警不失败 —— 但必须有人看见
			warns = append(warns, fmt.Sprintf("tier=%s active 是 v%d,最高版本却是 v%d(新数据仍在用旧 key)",
				t, st.ActiveVersion, st.MaxRetained))
		}

		logger.L().Debug("framework: 密钥 tier 检查通过",
			zap.String("tier", string(t)),
			zap.String("active_key_id", st.ActiveKeyID),
			zap.Int("active_version", st.ActiveVersion),
			zap.Int("retained", len(st.RetainedVersions)))
	}

	if len(warns) > 0 {
		// 汇总成一条 WARN 而不是每个 tier 一条:一行能看完比翻 10 行日志有用
		logger.L().Warn("framework: 密钥轮转状态需要关注",
			zap.Int("warnings", len(warns)),
			zap.String("details", strings.Join(warns, " | ")))
	}

	if !configuredAny {
		logger.L().Info("framework: 未配置任何密钥 tier,轮转检查跳过(未启用 KMS 的部署)")
		return nil
	}

	// legacy key 检查只在「至少配了一个 tier」时才做 —— 没配 KMS 的时候
	// 提"缺 legacy key"是纯噪音。
	//
	// 这条也**不返 error**:新部署从没写过 v1 格式密文,没有 legacy key 完全正常。
	// 老部署丢了它才是事故,但那种情况下业务侧解密的报错会先炸出来。
	// 这里只负责在它还来得及的时候把话说出来。
	if !hasLegacyKeyFn() {
		logger.L().Warn("framework: 未注册 v1 legacy key",
			zap.String("impact", "本部署写出的密文一律是 v2 信封;若历史上存在未迁移的 v1 密文,那些数据已无法解密"),
			zap.String("note", "新部署无 legacy key 属正常,仅老部署需关注"))
	}

	if len(hardErrs) > 0 {
		return errors.New("密钥加解密往返失败:" + strings.Join(hardErrs, " | "))
	}
	return nil
}
