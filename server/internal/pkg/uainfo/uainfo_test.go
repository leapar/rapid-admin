// uainfo 单测 —— UA 解析
//
// # 这些用例的 UA 都是**真实抓的**(不是编的)
//
// 编出来的 UA 只能验证"代码按我写的跑",验证不了"规则表对不对"。
// 下面每一条都对应一个真实存在的浏览器/客户端。
package uainfo

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		name        string
		ua          string
		wantBrowser string
		wantOS      string
	}{
		{
			name:        "Chrome on Windows 10",
			ua:          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			wantBrowser: "Chrome 120",
			wantOS:      "Windows 10/11",
		},
		{
			// ⚠️ 这条 UA 里**含 "Chrome"** —— 规则顺序错了就会认成 Chrome
			name:        "Edge on Windows 10",
			ua:          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
			wantBrowser: "Edge 120",
			wantOS:      "Windows 10/11",
		},
		{
			// ⚠️ Opera 的 UA 里**也含 "Chrome"**
			name:        "Opera on Windows 10",
			ua:          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36 OPR/92.0.0.0",
			wantBrowser: "Opera 92",
			wantOS:      "Windows 10/11",
		},
		{
			// ⚠️ Safari 的版本号在 "Version/" 里,"Safari/" 后面是 WebKit 构建号
			name:        "Safari on macOS",
			ua:          "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
			wantBrowser: "Safari 17.4",
			wantOS:      "macOS 10.15",
		},
		{
			name:        "Safari on macOS 14",
			ua:          "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4_1) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
			wantBrowser: "Safari 17.4",
			wantOS:      "macOS 14.4",
		},
		{
			// ⚠️ iOS 版 Chrome 的标记是 "CriOS/",UA 里**没有 "Chrome/"**
			name:        "Chrome on iOS",
			ua:          "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0.6099.119 Mobile/15E148 Safari/604.1",
			wantBrowser: "Chrome 120",
			wantOS:      "iOS 16",
		},
		{
			name:        "Firefox on Linux",
			ua:          "Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0",
			wantBrowser: "Firefox 121",
			wantOS:      "Linux",
		},
		{
			name:        "Firefox on Windows 7",
			ua:          "Mozilla/5.0 (Windows NT 6.1; WOW64; rv:109.0) Gecko/20100101 Firefox/115.0",
			wantBrowser: "Firefox 115",
			wantOS:      "Windows 7",
		},
		{
			// ⚠️ 微信的 UA 里**含 Chrome / Version / Safari 三重伪装**
			name:        "WeChat on Android",
			ua:          "Mozilla/5.0 (Linux; Android 10; V2049A Build/QP1A.190711.020; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/78.0.3904.108 Mobile Safari/537.36 MMWEBID/1234 MicroMessenger/8.0.30.2260(0x28001E51) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN",
			wantBrowser: "微信 8.0.30",
			wantOS:      "Android 10",
		},
		{
			// ⚠️ 企业微信的 UA 里**含 "Chrome"**
			name:        "WeCom on Windows",
			ua:          "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/104.0.0.0 Safari/537.36 wxwork/4.1.36",
			wantBrowser: "企业微信 4.1.36",
			wantOS:      "Windows 10/11",
		},
		{
			name:        "DingTalk on Android",
			ua:          "Mozilla/5.0 (Linux; Android 13; SM-S918B Build/TP1A.220624.014; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/108.0.0.0 Mobile Safari/537.36 AliApp(DingTalk/7.0.0/11002024)",
			wantBrowser: "钉钉 7.0.0",
			wantOS:      "Android 13",
		},
		{
			// ⚠️ 小米浏览器的 UA 里**含 "Chrome"**
			name:        "MiuiBrowser on Android",
			ua:          "Mozilla/5.0 (Linux; U; Android 9; zh-CN; MI 8 Build/PKQ1.180904.001) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/71.0.3578.141 Mobile Safari/537.36 XiaoMi/MiuiBrowser/12.5.3",
			wantBrowser: "小米浏览器 12.5",
			wantOS:      "Android 9",
		},
		{
			name:        "Safari on iPhone",
			ua:          "Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Mobile/15E148 Safari/604.1",
			wantBrowser: "Safari 17.1",
			wantOS:      "iOS 17",
		},
		{
			// ⚠️ iPad 的 UA 里**含 "Mac OS X"** —— 先判 Mac 就认错了
			name:        "Safari on iPad",
			ua:          "Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1",
			wantBrowser: "Safari 16.6",
			wantOS:      "iPadOS 16",
		},
		{
			name:        "Chrome on Android",
			ua:          "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36",
			wantBrowser: "Chrome 120",
			wantOS:      "Android 13",
		},
		{
			// ⚠️ HarmonyOS 的 UA 里**含 "Android"**
			name:        "HarmonyOS with Huawei browser",
			ua:          "Mozilla/5.0 (Linux; HarmonyOS 4.0; ALN-AL00; HMSCore 6.11.0.312) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.88 HuaweiBrowser/13.0.5.303 Mobile Safari/537.36",
			wantBrowser: "华为浏览器 13.0",
			wantOS:      "HarmonyOS",
		},
		{
			name:        "curl",
			ua:          "curl/8.4.0",
			wantBrowser: "curl 8.4",
			wantOS:      "",
		},
		{
			name:        "Postman",
			ua:          "PostmanRuntime/7.35.0",
			wantBrowser: "Postman 7",
			wantOS:      "",
		},
		{
			name:        "Internet Explorer 11",
			ua:          "Mozilla/5.0 (Windows NT 6.1; Trident/7.0; rv:11.0) like Gecko",
			wantBrowser: "Internet Explorer",
			wantOS:      "Windows 7",
		},
		{
			// 认不出来 ⇒ **空串**,不猜
			name:        "认不出来的 UA",
			ua:          "some-random-agent/1.0",
			wantBrowser: "",
			wantOS:      "",
		},
		{
			name:        "空 UA",
			ua:          "",
			wantBrowser: "",
			wantOS:      "",
		},
		{
			name:        "纯空白 UA",
			ua:          "   \t\n ",
			wantBrowser: "",
			wantOS:      "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Parse(c.ua)
			if got.Browser != c.wantBrowser {
				t.Errorf("Browser = %q, want %q", got.Browser, c.wantBrowser)
			}
			if got.OS != c.wantOS {
				t.Errorf("OS = %q, want %q", got.OS, c.wantOS)
			}
		})
	}
}

// TestParse_不会把别的浏览器认成 Chrome
//
// 这条单独拎出来:上面那张表里 UA 里含 "Chrome" 的有好几条,
// 但只要有一条被认成 Chrome,就是**规则顺序错了** —— 那是最容易犯、也最要命的错。
func TestParse_不会把别的浏览器认成Chrome(t *testing.T) {
	others := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36 OPR/92.0.0.0",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/104.0.0.0 Safari/537.36 wxwork/4.1.36",
		"Mozilla/5.0 (Linux; Android 10; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/78.0.3904.108 Mobile Safari/537.36 MicroMessenger/8.0.30",
		"Mozilla/5.0 (Linux; U; Android 9; zh-CN) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/71.0.3578.141 Mobile Safari/537.36 XiaoMi/MiuiBrowser/12.5.3",
	}
	for _, ua := range others {
		got := Parse(ua)
		if got.Browser == "Chrome" || len(got.Browser) >= 6 && got.Browser[:6] == "Chrome" {
			t.Errorf("UA %q 被认成了 %q(应当是别的浏览器)", ua, got.Browser)
		}
	}
}

func TestClip(t *testing.T) {
	if got := Clip("short", 128); got != "short" {
		t.Errorf("Clip 不该改短串: got %q", got)
	}
	long := ""
	for i := 0; i < 300; i++ {
		long += "a"
	}
	if got := Clip(long, 128); len(got) != 128 {
		t.Errorf("Clip 长度 = %d, want 128", len(got))
	}
	// ⚠️ 按**字符**截,不是按字节 —— 中文 3 字节,按字节切会劈开汉字
	cn := ""
	for i := 0; i < 100; i++ {
		cn += "客"
	}
	got := Clip(cn, 10)
	if len([]rune(got)) != 10 {
		t.Errorf("Clip 字符数 = %d, want 10", len([]rune(got)))
	}
	if got != "客客客客客客客客客客" {
		t.Errorf("Clip 中文结果不对: %q", got)
	}
	if got := Clip("x", 0); got != "" {
		t.Errorf("Clip(0) = %q, want 空", got)
	}
}
