// Package smstpl 短信模板审核流 —— M115(§4 P1 #13,框架层最后一个 P1 欠账)
//
// # 这个包解决什么
//
// 模板在阿里云那边过没过审,平台**此前完全不知道**:
//
//   - 运营在配置中心填了一个模板号,面板上看着一切正常;
//   - 用户点「获取验证码」,短信没到;
//   - 服务端日志只有一句 `isv.SMS_TEMPLATE_ILLEGAL`(阿里云的通用业务错误码,
//     它不告诉你"是模板没过审"还是"模板号写错了"还是"签名没报备");
//   - 而模板其实**早就被驳回了**,驳回理由就躺在阿里云控制台里没人看。
//
// 审核流要做的事只有三件:
//  1. **知道状态** —— 把服务商侧的真实审核态拉回来(approved / rejected / reviewing);
//  2. **提前失败** —— 模板确定不能用时,在发出去之前就拦住,并说清楚是哪张模板、为什么;
//  3. **留痕** —— 状态每次变化都记一条,回答"它是什么时候变成这样的"。
//
// # 三条硬规则(写在最前面,因为后面每个决定都是它们推出来的)
//
//  1. **只有服务商能判"通过/驳回"**。人工只能"停用"或"标记为待审核"。
//     反过来的话,运营在面板上点一下"已通过"就绕过了阿里云的审核 ——
//     而模板没过审时阿里云会拒发,于是面板显示"正常"、用户永远收不到验证码。
//  2. **人工停用的模板,服务商同步不能自动复活**。停用是止血手段,
//     同步任务每 10 分钟跑一次,不能因为服务商说"通过"就把运维刚停用的东西又打开。
//  3. **换了模板号,原来的审核状态立即作废**。审核的是**模板号**,不是场景;
//     换了号就是另一张模板,拿旧号的通过状态去担保新号 = 假保证。
//
// # 边界
//
// 这个包**只管模板的审核状态**,不碰:
//   - 验证码的生成 / 校验 / 限流(那是 pkg/smslimit + service/auth 的事)
//   - 模板正文与参数(那是阿里云侧的事,平台只认模板号)
//   - 谁在用哪个场景(那是配置中心 aliyun.sms.template.* 的事 ——
//     **模板号的唯一真源在配置中心**,本包只记"这张模板过没过审")
package smstpl

import (
	"fmt"
	"sort"

	"rapid-admin/internal/config"
)

// Status 模板审核状态
//
// ⚠️ **不要用 int 存** —— 这个值要进 DB、要进 JSON、要给人看。
// 字符串可读、可排序、跨版本不会因为枚举插值而错位。
type Status string

const (
	// StatusUnknown 不知道(没登记 / 还没同步过)
	StatusUnknown Status = "unknown"
	// StatusPending 待审核(已登记,还没送审或送审结果未知)
	StatusPending Status = "pending"
	// StatusReviewing 审核中(服务商侧正在审)
	StatusReviewing Status = "reviewing"
	// StatusApproved 审核通过(可以发)
	StatusApproved Status = "approved"
	// StatusRejected 审核驳回(发不出去)
	StatusRejected Status = "rejected"
	// StatusCancelled 审核取消(服务商撤回了这次审核;发不出去)
	//
	// ⚠️ **它跟 rejected 不是一回事,别合并** ——
	//   服务商侧确实有一个"审核取消"(阿里云 TemplateStatus=10):
	//   既不是"内容被驳回",也不是"还在审"。运维看到"已驳回"会去改模板内容;
	//   看到"审核取消"要办的事是另一件(多半是重新送审,或者模板已经被删了)。
	//   合并成一个状态,历史里就分不清这两种事了。
	StatusCancelled Status = "cancelled"
	// StatusDisabled 人工停用(运维主动关掉,服务商说通过也不复活)
	StatusDisabled Status = "disabled"
)

// allStatuses 全部合法状态(ParseStatus 的白名单)
var allStatuses = []Status{
	StatusUnknown, StatusPending, StatusReviewing,
	StatusApproved, StatusRejected, StatusCancelled, StatusDisabled,
}

// String 实现 fmt.Stringer
func (s Status) String() string { return string(s) }

// Known 是否是合法状态值
func (s Status) Known() bool {
	for _, v := range allStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// Usable 这张模板现在能不能用来发短信
//
// ⚠️ **只有 approved 算能用** —— pending / reviewing 都还没拿到服务商的通过结论,
// 拿"还没被拒绝"当"可以用"是本末倒置。
//
// ⚠️ **这个方法只用于"展示"**,不是守卫判据 —— 守卫判的是"确定发不出去",
//
//	见 Blocked()。两者口径不同是**故意的**:页面要如实显示"还在审",
//	而守卫不能因为"还在审"就把验证码通道锁死。
func (s Status) Usable() bool { return s == StatusApproved }

// Blocked 这张模板现在**确定**发不出去
//
// ⚠️ **只有 rejected / cancelled / disabled 算"确定"** ——
//
//	unknown   : 平台还没同步过(或同步失败)。拿"不知道"当"不能用",
//	            等于服务商一抽风就把全平台的登录验证码锁死。
//	pending / reviewing : 还在审。阿里云侧确实暂时发不出去,但那是"暂时";
//	            更要命的是同步任务一挂,状态就永远停在 pending ——
//	            此时拦掉 = 平台自己把通道锁了,而没有人知道为什么。
//	cancelled : 服务商撤回了这次审核(模板不是通过状态)。要恢复得重新送审,
//	            重新送审期间也一样发不出去 —— 与 rejected 同一条理。
//
// ⇒ 守卫只拦**确定不行**的三种;其余放行(但会记日志 / 打指标)。
//
// ⚠️ **判据只有一处** —— blockedInfo(见 guard.go)。
//
//	以前这里是"一个 switch 判三个状态",守卫里又有"另一个 switch 决定拦不拦、
//	说什么话"。两份表 ⇒ 加一个新状态时漏改一处,新状态就"算确定发不出去"
//	却因为查不到文案而落到放行分支 ⇒ **一张确定不可用的模板照发不误**。
//	两份表合成一份之后,这个漂移就**不可能**发生了。
func (s Status) Blocked() bool {
	err, _ := blockedInfo(s)
	return err != nil
}

// ParseStatus 解析状态字符串
//
// ⚠️ **未知值报错,不静默归到 unknown** ——
// 静默归一的话,DB 里存进一个手写错的 "approvd",页面上会显示"未知",
// 而守卫会**放行**(unknown 在宽松模式下不拦)⇒ 一个拼写错误就绕过了审核态检查。
// 宁可这里报错,让写入方立刻发现。
func ParseStatus(raw string) (Status, error) {
	s := Status(raw)
	if s == "" {
		return StatusUnknown, nil
	}
	if !s.Known() {
		return "", fmt.Errorf("smstpl: 未知状态 %q", raw)
	}
	return s, nil
}

// Source 状态来源
type Source string

const (
	// SourceManual 人工(运营在管理端 / 配置中心操作)
	SourceManual Source = "manual"
	// SourceProvider 服务商(阿里云同步回来的真实审核态)
	SourceProvider Source = "provider"
)

// String 实现 fmt.Stringer
func (s Source) String() string { return string(s) }

// 业务场景(与 msn.Scene 同值 —— 这里定义一份是因为 smstpl 不能反向依赖 msn)
//
// ⚠️ **场景 → 模板号的映射只有这一处** ——
// aliyun 包以前自己有一份 resolveTemplate,msn 包又要一份,
// 再加上配置中心那 9 个 key 的映射,一共三份"场景对应哪个模板"的真相。
// 现在统一到 TemplateCode()。
const (
	SceneLogin         = "login"
	SceneResetPassword = "reset_password"
	SceneChangePhone   = "change_phone"
	SceneViewFullPhone = "view_full_phone"
	// SceneMFA 登录二次验证(M150)。
	//
	// ⚠️ **没有专属阿里云模板,复用登录那张** ——
	// MFA 发生在登录流程里,"您正在登录"的文案本来就是对的;
	// 单独报备一张模板要过阿里云审核,对部署是纯负担(配置里不出现 mfa 键)。
	// 哪天真要换文案,再在 config.SMSConfig.Templates 加 Mfa 字段 + 配置中心 key。
	SceneMFA = "mfa"
)

// Scenes 全部场景(登记册初始化 / 同步遍历用)
var Scenes = []string{
	SceneLogin, SceneResetPassword, SceneChangePhone, SceneViewFullPhone, SceneMFA,
}

// TemplateCode 从生效配置里取某个场景的模板号
//
// 取不到返回空串(而不是编一个默认模板号 —— 编出来的号会带着
// "没配模板"的假状态进登记册,页面上显示成一个像模像样的模板)。
func TemplateCode(cfg config.SMSConfig, scene string) string {
	switch scene {
	case SceneLogin:
		return cfg.Templates.Login
	case SceneResetPassword:
		return cfg.Templates.ResetPassword
	case SceneChangePhone:
		return cfg.Templates.ChangePhone
	case SceneViewFullPhone:
		return cfg.Templates.ViewFullPhone
	case SceneMFA:
		// MFA 复用登录模板(见 SceneMFA 常量注释)—— 返回**实际要发的那张号**,
		// 守卫/登记册看到的和真正下发的必须是同一张,否则登记册对不上账。
		return cfg.Templates.Login
	default:
		return ""
	}
}

// SceneCodes 生效配置 → 场景到模板号的映射
//
// ⚠️ **没配模板号的场景不出现** —— 空模板号进登记册没有意义,
// 而且守卫拿空号去比对会产生"配置换了 ⇒ 登记册过期"的假报警。
func SceneCodes(cfg config.SMSConfig) map[string]string {
	out := make(map[string]string, len(Scenes))
	for _, sc := range Scenes {
		if code := TemplateCode(cfg, sc); code != "" {
			out[sc] = code
		}
	}
	return out
}

// SceneLabel 场景中文名(页面 / 日志用)
func SceneLabel(scene string) string {
	switch scene {
	case SceneLogin:
		return "登录验证码"
	case SceneResetPassword:
		return "重置密码"
	case SceneChangePhone:
		return "换绑手机"
	case SceneViewFullPhone:
		return "查看完整手机号"
	case SceneMFA:
		return "登录二次验证"
	default:
		return scene
	}
}

// SortedScenes 排序后的场景列表(登记册输出用,保证顺序稳定)
func SortedScenes(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
