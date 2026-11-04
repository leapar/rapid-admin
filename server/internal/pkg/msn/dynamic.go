// 运行时可切换的 SMS sender —— 平台配置中心接线(C6,2026-10-29)
//
// ⚠️ **为什么要有这一层**
//
// 平台配置中心里有一组 aliyun.sms.* 的配置项(AccessKey / 签名 / 模板 / provider),
// **运维在面板上改必须实时生效** —— 换密钥、换模板不应该要求重启服务。
// 而 aliyun.AliyunSender 原来在 New() 里就把 client 和 cfg **定死**,
// 之后再没有任何地方读配置中心 ⇒ 面板成了摆设(改了没反应,且没有任何报错)。
//
// 这一层的三段拆分:
//
//	Resolver        每次发送前解析一次"当前生效的 config.SMSConfig"
//	                (yaml 打底 + 配置中心覆盖,规则见 service.ResolveSMSConfig)
//	DynamicSender   按解析出来的 Provider 选后端
//	AliyunBackend   把"这次生效的配置"交给阿里云后端
//	                (凭证变了重建 client,没变就复用)
//
// # ⚠️ 发送前的模板守卫(M115)
//
// 模板在阿里云那边没过审时,阿里云会拒发,平台**只能在失败之后**知道:
// 服务端日志只有一句 isv.SMS_TEMPLATE_ILLEGAL,用户看到的是"发送失败"。
//
// 守卫把这件事提前:发送之前先问登记册"这张模板能不能用",
// 确定不能用就**当场说清楚是哪张模板、为什么**。
//
// ⇒ 守卫的判定逻辑全在 pkg/smstpl 里,本包只负责"在正确的位置问一次"。
package msn

import (
	"context"
	"fmt"
	"sync/atomic"

	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/logger"
	consolepkg "rapid-admin/internal/pkg/msn/console"
	"rapid-admin/internal/pkg/smstpl"
)

// Resolver 解析"当前生效"的短信配置
//
//	base = 配置文件里那一份(打底);返回值 = 覆盖后的生效配置
//
// 签名与 service.ConfigCenterService.ResolveSMSConfig 一致 ⇒ 方法值可直接当 Resolver 用。
//
// 约定:**返回错误时调用方应当放弃这次发送**(而不是退回默认值)——
// 配置读不出来说明环境有问题,静默用旧配置发短信会掩盖故障。
type Resolver func(ctx context.Context, base config.SMSConfig) (config.SMSConfig, error)

// AliyunBackend 阿里云下发后端
//
// SendWith 每次调用都带上生效配置 —— 凭证/签名的热替换靠它;
// 后端内部按凭证指纹决定是复用 client 还是重建。
type AliyunBackend interface {
	SendWith(ctx context.Context, cfg config.SMSConfig, phone, code, scene string) error
}

// TemplateGuard 发送前的模板守卫(窄接口)
//
// ⚠️ **窄在行为上,不窄在类型上** —— 返回 smstpl.Decision 而不是本包自定义的结构,
//
//	是为了让 *smstpl.Guard **直接**满足它(main.go 里零适配器)。
//	代价是本包依赖 smstpl —— 这个方向是允许的:
//	**smstpl 刻意不依赖 msn**(见 smstpl.go 的场景常量注释),所以不会成环。
//
//	依赖面只有两个:
//	  - Decision  —— 判定结果(拦不拦 + 为什么)
//	  - TemplateCode —— 场景 → 模板号的**唯一**映射(在 smstpl 里)
//
// ⚠️ **模板号必须由 smstpl 换算,不能在本包再写一份** ——
//
//	"场景对应哪个模板"曾经有过三份真相(aliyun 包一份、msn 一份、配置中心一份),
//	合并成 smstpl.TemplateCode 之后,再写一份就等于把刚拆掉的问题装回去。
type TemplateGuard interface {
	// Evaluate 判定"某场景 + 某模板号"现在能不能发
	//
	// templateCode 是**这次要发的模板号**(可能为空 —— 空表示"调用方没带",
	// 此时只按登记册里的状态判)。
	Evaluate(scene, templateCode string) smstpl.Decision
}

// DynamicSender 按"当前生效配置"选 provider 的 sender
type DynamicSender struct {
	base    config.SMSConfig // 配置文件里那一份(打底)
	resolve Resolver
	aliyun  AliyunBackend
	console Sender

	// guard 发送前的模板守卫(M115);为 nil 时不做任何检查
	guard TemplateGuard

	// provider 记录最近一次解析出来的 provider —— Sender.Provider() 没有 ctx,
	// 而 smsaudit 审计行要写它(见 internal/pkg/smsaudit/wrap.go)。
	provider atomic.Value // string
}

// NewDynamicSender 构造
//
//   - base    配置文件里那一份短信配置(打底)
//   - resolve 解析生效配置;为 nil 时发送直接报错(不会静默用旧配置)
//   - aliyun  阿里云后端;为 nil 且生效配置要求 aliyun 时,发送返回明确错误
//   - console 控制台后端;为 nil 时退化为内置的 console 实现
//   - guard   发送前的模板守卫;为 nil 时不做检查
//
// ⚠️ **guard 是必填位置参数,不是可选的 setter** ——
//
//	守卫是安全开关,做成"忘了调就不生效"的 setter,等于默认失效。
//	不想检查就传 nil(显式表达"这条链路不做模板检查")。
func NewDynamicSender(
	base config.SMSConfig,
	resolve Resolver,
	aliyun AliyunBackend,
	console Sender,
	guard TemplateGuard,
) *DynamicSender {
	if console == nil {
		console = consolepkg.New()
	}
	d := &DynamicSender{base: base, resolve: resolve, aliyun: aliyun, console: console, guard: guard}
	d.provider.Store("unknown")
	return d
}

// Send 发送验证码
func (d *DynamicSender) Send(ctx context.Context, phone, code, scene string) error {
	cfg, err := d.resolveCfg(ctx)
	if err != nil {
		return err
	}
	// ⚠️ **守卫必须在真正下发之前**,而且用的是**这次解析出来的同一份 cfg** ——
	//
	//	模板号从 cfg 里取,而不是另找一份配置再解析一次。
	//	否则会出现"按 A 号的审核状态放行、实际发出去的是 B 号"这种
	//	**拦错对象**的错配(守卫第 3 条口径就是为了防这个才写的)。
	if err := d.checkTemplate(smstpl.TemplateCode(cfg, scene), scene); err != nil {
		return err
	}
	switch cfg.Provider {
	case "aliyun":
		d.provider.Store("aliyun")
		if d.aliyun == nil {
			return fmt.Errorf("sms: provider=aliyun 但没有可用的阿里云后端")
		}
		return d.aliyun.SendWith(ctx, cfg, phone, code, scene)
	case "console":
		d.provider.Store("console")
		return d.console.Send(ctx, phone, code, scene)
	case "":
		d.provider.Store("console")
		logger.L().Warn("sms.provider 未配置, fallback 到 console (验证码只打到日志)")
		return d.console.Send(ctx, phone, code, scene)
	default:
		d.provider.Store("console")
		logger.L().Warn("未知的 sms.provider, fallback 到 console",
			zap.String("provider", cfg.Provider))
		return d.console.Send(ctx, phone, code, scene)
	}
}

// Provider 最近一次解析出来的 provider(审计用)
func (d *DynamicSender) Provider() string {
	p, _ := d.provider.Load().(string)
	return p
}

// checkTemplate 发送前的模板守卫 —— 确定发不出去就拦下,并说清是哪张模板、为什么
//
// ⚠️ **三档策略全在 smstpl.Guard 里**(off / warn / block),本方法只负责
//
//	"把结论变成动作 + 记日志":
//	  - Blocked ⇒ 返回错误,这次短信**根本不发**
//	  - 没拦但有 Reason ⇒ 记一条 Warn(warn 策略,或"模板号已变更、状态待同步")
//
// ⚠️ **拦下时必须让调用方拿到"为什么"** —— 上层(service/auth)会把错误原文
//
//	拼进给用户的提示里,运营看到的是"模板被服务商驳回"而不是"发送失败"。
//
// ⚠️ **守卫放在 smsaudit 装饰器的里面** —— 拦下的这次发送同样要落
//
//	sys_sms_send_log(带错误原因),否则"发不出去"这件事在审计表里查不到。
//
// ⚠️ **只收模板号,不收整份 cfg** —— 判定只需要"这次要发的号";
//
//	整份 config.SMSConfig 有 192 字节,按值传进来会被 gocritic 的 hugeParam 报,
//	而且传进来之后**容易被误用**(拿它读别的东西,读到的就不是"这次生效"的那份了)。
func (d *DynamicSender) checkTemplate(tplCode, scene string) error {
	if d.guard == nil {
		return nil
	}
	dec := d.guard.Evaluate(scene, tplCode)

	if dec.Blocked {
		if dec.Err == nil {
			// 防御:守卫实现若只填了 Blocked 没填 Err,这里补一个能读懂的错
			dec.Err = fmt.Errorf("sms: 场景 %q 的短信模板不可用(%s)", scene, dec.Reason)
		}
		logger.L().Error("短信模板守卫:模板确定发不出去,已拦下本次发送",
			zap.String("scene", scene),
			zap.String("template_code", tplCode),
			zap.String("status", dec.Status.String()),
			zap.String("reason", dec.Reason))
		return dec.Err
	}
	if dec.Reason != "" {
		logger.L().Warn("短信模板守卫:告警(未拦下)",
			zap.String("scene", scene),
			zap.String("template_code", tplCode),
			zap.String("status", dec.Status.String()),
			zap.String("reason", dec.Reason))
	}
	return nil
}

// resolveCfg 取生效配置
func (d *DynamicSender) resolveCfg(ctx context.Context) (config.SMSConfig, error) {
	if d.resolve == nil {
		return config.SMSConfig{}, fmt.Errorf("sms: 没有配置解析器")
	}
	return d.resolve(ctx, d.base)
}
