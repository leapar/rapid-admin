package voicex

import (
	"strings"
	"testing"
)

func TestNormalizeEdgeVoice(t *testing.T) {
	cases := []struct{ in, want string }{
		{"zh-CN-XiaoxiaoNeural", "Microsoft Server Speech Text to Speech Voice (zh-CN, XiaoxiaoNeural)"},
		{"zh-CN-liaoning-XiaobeiNeural", "Microsoft Server Speech Text to Speech Voice (zh-CN-liaoning, XiaobeiNeural)"},
		// 已是全名 → 原样
		{"Microsoft Server Speech Text to Speech Voice (zh-CN, XiaoxiaoNeural)",
			"Microsoft Server Speech Text to Speech Voice (zh-CN, XiaoxiaoNeural)"},
		// 认不出的形态原样返回(校验交给服务端)
		{"", ""},
		{"custom-voice-123", "custom-voice-123"},
	}
	for _, c := range cases {
		if got := NormalizeEdgeVoice(c.in); got != c.want {
			t.Errorf("NormalizeEdgeVoice(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEdgeSecMSGEC(t *testing.T) {
	g := edgeSecMSGEC(1700000000)
	if len(g) != 64 || strings.ToUpper(g) != g || strings.Trim(g, "0123456789ABCDEF") != "" {
		t.Fatalf("edgeSecMSGEC 形态非法: %q", g)
	}
	if g != edgeSecMSGEC(1700000000) {
		t.Fatal("同一时间戳两次计算结果不一致")
	}
	if g == edgeSecMSGEC(1700000300) {
		t.Fatal("5 分钟窗口外的值不应相同")
	}
}

func TestEdgeSynthParamsFill(t *testing.T) {
	var p EdgeSynthParams
	if err := p.fill(); err == nil {
		t.Fatal("空参数应报错(text 必填)")
	}
	p = EdgeSynthParams{Text: " hi ", Voice: "zh-CN-XiaoxiaoNeural", Volume: "+0%", Rate: "-10%", Pitch: "+5Hz"}
	if err := p.fill(); err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if p.Text != "hi" {
		t.Fatalf("text 应去首尾空白, got %q", p.Text)
	}
	for _, bad := range []EdgeSynthParams{
		{Text: "x", Volume: "louder"},
		{Text: "x", Rate: "fast"},
		{Text: "x", Pitch: "high"},
	} {
		if err := bad.fill(); err == nil {
			t.Errorf("非法参数 %+v 应报错", bad)
		}
	}
}

func TestEdgeHeadersAndData(t *testing.T) {
	raw := []byte("X-RequestId:abc\r\nPath:turn.start\r\n\r\n{\"context\":{}}")
	h, data, err := edgeHeadersAndData(raw)
	if err != nil {
		t.Fatal(err)
	}
	if h["Path"] != "turn.start" || h["X-RequestId"] != "abc" {
		t.Fatalf("headers 解析错误: %v", h)
	}
	if string(data) != `{"context":{}}` {
		t.Fatalf("payload 解析错误: %q", string(data))
	}
	if _, _, err := edgeHeadersAndData([]byte("no-separator")); err == nil {
		t.Fatal("缺分隔符应报错")
	}
}
