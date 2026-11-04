// Package uainfo 解析 User-Agent,取出**浏览器**和**操作系统**
//
// # 为什么不引第三方库
//
// 仓库里没有 uasurfer / uaparser 之类的依赖,也不打算为此加一个:
// UA 解析本来就不是"有标准"的问题 —— 各家 UA 格式千奇百怪,
// 主流库说到底也是一堆 if/else,再加一层依赖不划算。
// 自己写一份**可控、可测、零依赖**的,想改一条规则就改一条。
//
// # 输出的是**产品名**,不是代码
//
//	Browser = "Chrome 120" / "Edge 120" / "微信 8.0.30" / "Postman"
//	OS      = "Windows 10/11" / "macOS 14" / "iOS 16" / "Android 13"
//
// 为什么不让前端拿 code 再做 i18n 映射:
// 这些**绝大多数是产品名**(Chrome / Safari / Windows / macOS),
// 中文界面里也是这么叫的;真正需要中文的只有国产 App(微信 / 企业微信 / 钉钉),
// 它们的产品名本身就是中文。
// ⇒ 存显示名,前端直接渲染,不必再维护一张 code → 标签 的表
// (那种表一旦加了,后端和前端就得同步改两处,还得防漏 —— 字典页就是这么长出来的)。
//
// # ⚠️ 认不出来就是**空字符串**,不猜
//
//	空 = "这个 UA 没认出来" 或 "压根没有 UA"。
//	**不猜**很重要:审计里最怕的是**看起来确定、其实错**的信息
//	(比如把一个爬虫认成 Chrome),那比空着更容易误导人。
//	前端把空串渲染成 "-"。
package uainfo

import "strings"

// Info UA 解析结果
type Info struct {
	// Browser 浏览器产品名 + 版本(如 "Chrome 120");认不出时为空串
	Browser string
	// OS 操作系统名 + 版本(如 "Windows 10/11");认不出时为空串
	OS string
}

// Parse 解析 UA
//
// 空 UA / 纯空白 → 两个字段都是空串。
func Parse(ua string) Info {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return Info{}
	}
	return Info{
		Browser: parseBrowser(ua),
		OS:      parseOS(ua),
	}
}

// browserRule 浏览器识别规则
//
// ⚠️ **规则表是从上往下匹配的,命中即停** —— 所以**顺序就是优先级**。
//
//	这不是随便排的,每一对都有真实的坑:
//	  - Edge 的 UA 里**含 "Chrome"** —— 先判 Chrome 就把 Edge 认错了
//	  - Opera 的 UA 里**含 "Chrome"** —— 同上
//	  - 微信(iOS)的 UA 里**含 "Safari"** —— 同上
//	  - 企业微信的 UA 里**含 "Chrome"** —— 同上
//	  - UC / 夸克 / QQ 的 UA 里**含 "Chrome"** —— 同上(它们是 Chromium 内核)
//	  - Chrome 的 UA 里**含 "Safari"** —— Safari 必须在 Chrome 之后
type browserRule struct {
	// markers 任一命中即算这条规则
	markers []string
	// name 产品名
	name string
	// versionMarkers 版本号从这些标记后面取(按顺序,取到第一个非空为止);
	// 空 = 这条规则不带版本
	versionMarkers []string
	// versionParts 取几段版本号(1 = "120",2 = "17.4",3 = "8.0.30")
	versionParts int
}

// browserRules 浏览器规则表(**顺序敏感**)
var browserRules = []browserRule{
	// —— 国产 App(它们的 UA 里都含 Chrome/Safari,必须排在引擎前面)——
	{[]string{"wxwork"}, "企业微信", []string{"wxwork/"}, 3},
	{[]string{"MicroMessenger"}, "微信", []string{"MicroMessenger/"}, 3},
	{[]string{"DingTalk", "AliApp(DingTalk"}, "钉钉", []string{"DingTalk/"}, 3},
	{[]string{"Lark", "Feishu"}, "飞书", []string{"Lark/", "Feishu/"}, 3},

	// —— 国产浏览器(同样是 Chromium 内核,排在 Chrome 前面才不会被认成 Chrome)——
	{[]string{"HuaweiBrowser/"}, "华为浏览器", []string{"HuaweiBrowser/"}, 2},
	{[]string{"MiuiBrowser/", "XiaoMi/MiuiBrowser/"}, "小米浏览器", []string{"MiuiBrowser/"}, 2},
	{[]string{"UCBrowser/", "UCWEB"}, "UC 浏览器", []string{"UCBrowser/", "UCWEB/"}, 2},
	{[]string{"QQBrowser/", "MQQBrowser/"}, "QQ 浏览器", []string{"QQBrowser/", "MQQBrowser/"}, 2},
	{[]string{"Quark/"}, "夸克浏览器", []string{"Quark/"}, 2},
	{[]string{"360SE", "360EE"}, "360 浏览器", nil, 0},
	{[]string{"SogouMobileBrowser/"}, "搜狗浏览器", []string{"SogouMobileBrowser/"}, 2},

	// —— Android WebView(它伪装成 Chrome,但**不是**浏览器)——
	{[]string{"; wv)"}, "Android WebView", nil, 0},

	// —— 内核(彼此会互相"冒名")——
	{[]string{"Edg/", "EdgA/", "EdgiOS/", "Edge/"}, "Edge", []string{"Edg/", "EdgiOS/", "EdgA/", "Edge/"}, 1},
	{[]string{"OPR/", "OPiOS/", "Opera"}, "Opera", []string{"OPR/", "OPiOS/"}, 1},
	{[]string{"Firefox/", "FxiOS/"}, "Firefox", []string{"Firefox/", "FxiOS/"}, 1},
	{[]string{"Chrome/", "CriOS/"}, "Chrome", []string{"Chrome/", "CriOS/"}, 1},
	// ⚠️ Safari 的版本号在 "Version/" 里,**不在 "Safari/" 里** ——
	//   "Safari/605.1.15" 是 WebKit 的构建号,拿它当浏览器版本就是**报错信息**。
	//   所以这里**只**从 "Version/" 取,取不到就光秃秃一个 "Safari"。
	{[]string{"Version/", "Safari/"}, "Safari", []string{"Version/"}, 2},
	{[]string{"MSIE ", "Trident/"}, "Internet Explorer", []string{"MSIE "}, 1},

	// —— 非浏览器客户端(审计里同样是关键信息:谁在调接口)——
	{[]string{"PostmanRuntime/"}, "Postman", []string{"PostmanRuntime/"}, 1},
	{[]string{"python-requests/"}, "python-requests", []string{"python-requests/"}, 2},
	{[]string{"python-urllib/"}, "python-urllib", []string{"python-urllib/"}, 2},
	{[]string{"Go-http-client/"}, "Go-http-client", []string{"Go-http-client/"}, 1},
	{[]string{"okhttp/"}, "okhttp", []string{"okhttp/"}, 2},
	{[]string{"axios/"}, "axios", []string{"axios/"}, 2},
	{[]string{"node-fetch/"}, "node-fetch", []string{"node-fetch/"}, 2},
	{[]string{"Apache-HttpClient/"}, "Apache HttpClient", []string{"Apache-HttpClient/"}, 2},
	{[]string{"WinHttp/"}, "WinHTTP", []string{"WinHttp/"}, 1},
	{[]string{"Java/"}, "Java", []string{"Java/"}, 1},
	{[]string{"Electron/"}, "Electron", []string{"Electron/"}, 1},
	{[]string{"Dalvik/"}, "Android App", nil, 0},
	{[]string{"curl/"}, "curl", []string{"curl/"}, 2},
}

// parseBrowser 认浏览器
func parseBrowser(ua string) string {
	for _, r := range browserRules {
		if !hasAny(ua, r.markers) {
			continue
		}
		name := r.name
		if r.versionMarkers != nil {
			for _, m := range r.versionMarkers {
				if v := extractVersion(ua, m, r.versionParts); v != "" {
					name += " " + v
					break
				}
			}
		}
		return name
	}
	return ""
}

// parseOS 认操作系统
//
// ⚠️ **顺序敏感**,理由同 browserRules:
//   - iPad 的 UA 里**含 "Mac OS X"** —— 先判 Mac 会把 iPad 认成 Mac
//   - HarmonyOS 的 UA 里**含 "Android"** —— 先判 Android 会把它认成 Android
func parseOS(ua string) string {
	switch {
	// —— Windows ——
	case strings.Contains(ua, "Windows Phone"):
		return "Windows Phone"
	case strings.Contains(ua, "Windows NT 10.0"):
		// ⚠️ Windows 11 的 UA **也是 "Windows NT 10.0"** —— 跟 10 无法区分。
		//   写死 "Windows 10" 就是**谎报**;写 "10/11" 是实话。
		return "Windows 10/11"
	case strings.Contains(ua, "Windows NT 6.3"):
		return "Windows 8.1"
	case strings.Contains(ua, "Windows NT 6.2"):
		return "Windows 8"
	case strings.Contains(ua, "Windows NT 6.1"):
		return "Windows 7"
	case strings.Contains(ua, "Windows NT 6.0"):
		return "Windows Vista"
	case strings.Contains(ua, "Windows NT 5.1"):
		return "Windows XP"
	case strings.Contains(ua, "Windows"):
		return "Windows"

	// —— 国产 OS(HarmonyOS 的 UA 里含 "Android")——
	case strings.Contains(ua, "HarmonyOS"), strings.Contains(ua, "OpenHarmony"):
		return "HarmonyOS"

	// —— Apple(必须排在 Mac 前面)——
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPod"):
		return withVersion("iOS", osVersion(ua, "OS ", 1))
	case strings.Contains(ua, "iPad"):
		// iPadOS 13+ 的**桌面模式 UA** 跟 macOS 一模一样("Macintosh; Intel Mac OS X"),
		// 那种情况下认成 macOS 也**不算错**(它确实在伪装成 Mac)。
		// 这里判到的 iPad 是移动模式 UA,带 "CPU OS x_y"。
		return withVersion("iPadOS", osVersion(ua, "OS ", 1))

	// —— Android ——
	case strings.Contains(ua, "Android"):
		return withVersion("Android", osVersion(ua, "Android ", 1))

	// —— 桌面 OS ——
	case strings.Contains(ua, "CrOS"):
		return "ChromeOS"
	case strings.Contains(ua, "Mac OS X"):
		return withVersion("macOS", osVersion(ua, "Mac OS X ", 2))
	case strings.Contains(ua, "X11"), strings.Contains(ua, "Linux"):
		return "Linux"
	}
	return ""
}

// withVersion 拼 "名称 版本";版本为空就不拼
func withVersion(name, ver string) string {
	if ver == "" {
		return name
	}
	return name + " " + ver
}

// hasAny ua 里是否含其中任意一个
func hasAny(ua string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(ua, m) {
			return true
		}
	}
	return false
}

// extractVersion 取 marker 之后的版本号(最多 parts 段数字)
//
// 例:ua="...Chrome/120.0.0.0 Safari/537.36", marker="Chrome/", parts=1 → "120"
//
// ⚠️ **只收数字和点** —— 版本号后面紧跟的是 ";" 或 " " 或 ")",收进去就变成 "120.0.0.0;" 这种没法用的东西。
func extractVersion(ua, marker string, parts int) string {
	i := strings.Index(ua, marker)
	if i < 0 {
		return ""
	}
	s := ua[i+len(marker):]
	var segs []string
	cur := ""
	for j := 0; j < len(s); j++ {
		c := s[j]
		if c >= '0' && c <= '9' {
			cur += string(c)
			continue
		}
		if cur != "" {
			segs = append(segs, cur)
			cur = ""
		}
		if c == '.' && len(segs) < parts {
			continue
		}
		break
	}
	if cur != "" {
		segs = append(segs, cur)
	}
	if len(segs) > parts {
		segs = segs[:parts]
	}
	return strings.Join(segs, ".")
}

// osVersion 取 marker 之后的第一个 token 里的版本号
//
// 与 extractVersion 的区别:**OS 的版本号用 "_" 分隔**
// ("iPhone OS 16_0"、"Mac OS X 10_15_7"),所以这里按 "_" 切,再把段数截到 parts。
func osVersion(ua, marker string, parts int) string {
	i := strings.Index(ua, marker)
	if i < 0 {
		return ""
	}
	s := ua[i+len(marker):]
	if end := strings.IndexAny(s, " );"); end >= 0 {
		s = s[:end]
	}
	var out []string
	for _, seg := range strings.Split(s, "_") {
		if len(out) >= parts {
			break
		}
		n := 0
		for n < len(seg) && seg[n] >= '0' && seg[n] <= '9' {
			n++
		}
		if n == 0 {
			break
		}
		out = append(out, seg[:n])
	}
	return strings.Join(out, ".")
}

// Clip 按**字符**(不是字节)把 UA 截到 max 个字符
//
// ⚠️ **为什么必须有这个** ——
//
//	sys_login_log.device 是 `varchar(128)`,而 UA 原文**经常超过 128 字符**
//	(微信 / 企业微信那种带一长串 token 的能到 200+)。
//	MySQL 严格模式下超长直接报 "Data too long for column" ⇒
//	**整条登录日志写不进去**(只在日志里留一条 warn)⇒ 审计链断了一条。
//	SQLite 不校验长度,所以本地开发期**看不出这个问题** ——
//	上生产才炸,那时候查历史登录记录已经查不到了。
//
//	按**字符**截而不是按字节:中文是 3 字节 UTF-8,按字节切会把汉字劈成两半,
//	落库就是乱码。
func Clip(ua string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(ua)
	if len(r) <= max {
		return ua
	}
	return string(r[:max])
}
