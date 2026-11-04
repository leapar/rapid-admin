// uainfo —— 解析 User-Agent
//
// ## 这个包解决什么问题
//
// 登录日志要记"什么浏览器、什么系统"。本仓不引 uasurfer / uaparser ——
// UA 解析本来就不是"有标准"的问题,主流库说到底也是一堆 if/else,
// 再加一层依赖不划算。自己写一份可控、可测、零依赖的。
//
// ## 关键点
//
//   - 输出的是**产品名**而不是 code:"Chrome 120" / "微信 8.0.30" / "Windows 10/11"
//     —— 前端直接渲染,不必再维护一张 code → 标签 的表(那种表后端前端要同步改两处)
//   - **认不出来就是空字符串,不猜**。审计里最怕"看起来确定、其实错"的信息
//     (把爬虫认成 Chrome 比空着更容易误导人)
//   - Clip 按**字符**截断:sys_login_log.device 是 varchar(128),
//     而 UA 经常超 128 字符(微信那种带 token 的能到 200+)。
//     MySQL 严格模式下超长 ⇒ **整条登录日志写不进去** ⇒ 审计链断一条;
//     SQLite 不校验长度 ⇒ 本地开发期看不出来,上生产才炸
//
// ## 跑法
//
//	go run ./examples/uainfo
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/uainfo"
)

func main() {
	uas := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.30",
		"Mozilla/5.0 (Linux; Android 13; SM-S908B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Mobile Safari/537.36",
		"PostmanRuntime/7.36.0",
		"curl/8.4.0",
		"",
	}

	fmt.Println("User-Agent → 浏览器 / 操作系统")
	for _, ua := range uas {
		info := uainfo.Parse(ua)
		label := ua
		if len(label) > 56 {
			label = label[:56] + "..."
		}
		if label == "" {
			label = "(空)"
		}
		fmt.Printf("  %-59s → 浏览器=%-18q 系统=%q\n", label, info.Browser, info.OS)
	}

	// 认不出来就是空串,不猜
	fmt.Println()
	fmt.Println("空 UA:", uainfo.Parse(""))

	// 截断:按字符,不按字节
	long := "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) MicroMessenger/8.0.30(0x18002935) NetType/WIFI Language/zh_CN"
	clipped := uainfo.Clip(long, 128)
	fmt.Println()
	fmt.Printf("原始长度 %d 字符 → 截断后 %d 字符\n", len([]rune(long)), len([]rune(clipped)))
	fmt.Println("截断结果:", clipped)
}
