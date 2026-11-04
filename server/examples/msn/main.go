// msn —— 运行时可切换的短信 sender
//
// ## 这个包解决什么问题
//
// 平台配置中心里有一组 aliyun.sms.* 的配置项(AccessKey / 签名 / 模板 /
// provider),**运维在面板上改必须实时生效** —— 换密钥、换模板不应该要求
// 重启服务。
//
// 而 aliyun.AliyunSender 原来在 New() 里就把 client 和 cfg **定死**,
// 之后再没有任何地方读配置中心 ⇒ 面板成了摆设(改了没反应,且没有任何报错)。
//
// 这一层的三段拆分:
//
//	Resolver        每次发送前解析一次"当前生效的 config.SMSConfig"
//	DynamicSender   按解析出来的 Provider 选后端
//	AliyunBackend   把"这次生效的配置"交给阿里云后端
//
// ## 关键点
//
//   - **provider 是每次发送时解析的**,不是启动时定死的
//   - **未知 provider / 没配 provider ⇒ fallback 到 console**,
//     并打一条 Warn(不是静默降级)
//   - **模板守卫在真正下发之前**,而且用的是**这次解析出来的同一份 cfg** ——
//     否则会出现"按 A 号的审核状态放行、实际发出去的是 B 号"的错配
//   - **守卫的三档策略**(off / warn / block)全在 smstpl.Guard 里
//
// ## 跑法
//
//	go run ./examples/msn
package main

import (
	"context"
	"fmt"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/msn"
	"rapid-admin/internal/pkg/smstpl"
)

func main() {
	ctx := context.Background()

	// ── 一、场景常量 ────────────────────────────────────────────
	fmt.Println("=== 场景 ===")
	for _, s := range []string{"login", "reset_password", "change_phone", "view_full_phone", "不认识"} {
		fmt.Printf("  %-18q → %q\n", s, msn.AsScene(s))
	}
	fmt.Println("  ⇒ AsScene 是场景字符串到 Scene 的唯一转换入口")

	// ── 二、生效配置 ────────────────────────────────────────────
	//
	// 配置文件里那一份(打底)。配置中心的覆盖在 Resolver 里合进来。
	base := config.SMSConfig{
		Provider:  "console",
		SignName:  "RapidAdmin",
		Templates: config.SMSTemplate{Login: "SMS_100000000"},
	}

	// Resolver:每次发送前跑一次,返回"这次生效的配置"。
	//
	// ⚠️ 生产实现 = yaml 打底 + 配置中心覆盖(见 service.ResolveSMSConfig)。
	//    示例里用 panel 这个变量模拟"运维在面板上改了 provider"。
	panel := base
	resolve := func(ctx context.Context, b config.SMSConfig) (config.SMSConfig, error) {
		out := b                      // yaml 打底
		out.Provider = panel.Provider // 配置中心覆盖
		out.Templates = panel.Templates
		return out, nil
	}

	// ── 三、模板守卫 ────────────────────────────────────────────
	//
	// 登记册:这个场景的模板过没过审
	reg := smstpl.NewRegistry()
	if _, _, err := reg.Register(smstpl.SceneLogin, "SMS_100000000"); err != nil {
		fmt.Println("登记模板失败:", err)
		return
	}
	// 服务商同步回来的结论:已通过
	if _, _, err := reg.Apply(smstpl.SceneLogin, smstpl.StatusApproved, smstpl.SourceProvider, "阿里云审核通过", ""); err != nil {
		fmt.Println("更新状态失败:", err)
		return
	}
	guard := smstpl.NewGuard(reg, smstpl.PolicyBlock)

	// ── 四、发送 ────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 发送(console provider)===")
	sender := msn.NewDynamicSender(base, resolve, nil, nil, guard)
	err := sender.Send(ctx, "13800001234", "123456", smstpl.SceneLogin)
	fmt.Printf("  err = %v\n", err)
	fmt.Printf("  本次用的 provider = %s\n", sender.Provider())

	// ── 五、切换 provider(不重启)───────────────────────────────
	fmt.Println()
	fmt.Println("=== 运维在面板上把 provider 改成 aliyun ===")
	panel.Provider = "aliyun"
	err = sender.Send(ctx, "13800001234", "123456", smstpl.SceneLogin)
	fmt.Printf("  err = %v\n", err)
	fmt.Printf("  本次用的 provider = %s\n", sender.Provider())
	fmt.Println("  ⇒ provider=aliyun 但没装配阿里云后端 ⇒ 明确报错,")
	fmt.Println("    而不是把验证码悄悄打到日志里")

	// ── 六、未知 provider 的兜底 ────────────────────────────────
	fmt.Println()
	fmt.Println("=== 未知 provider ===")
	panel.Provider = "某个不存在的provider"
	err = sender.Send(ctx, "13800001234", "123456", smstpl.SceneLogin)
	fmt.Printf("  err = %v\n", err)
	fmt.Printf("  本次用的 provider = %s\n", sender.Provider())
	fmt.Println("  ⇒ 未知 provider ⇒ fallback 到 console + 一条 Warn(不静默)")

	// ── 七、守卫拦下 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 守卫拦下(模板被服务商驳回)===")
	panel.Provider = "console"
	if _, _, err := reg.Apply(smstpl.SceneLogin, smstpl.StatusRejected, smstpl.SourceProvider, "内容不合规", ""); err != nil {
		fmt.Println("更新状态失败:", err)
		return
	}
	err = sender.Send(ctx, "13800001234", "123456", smstpl.SceneLogin)
	fmt.Printf("  err = %v\n", err)
	fmt.Println("  ⇒ 拦下时必须让调用方拿到「为什么」——")
	fmt.Println("    上层会把错误原文拼进给用户的提示,运营看到的是")
	fmt.Println("    「模板被服务商驳回」而不是「发送失败」")
}
