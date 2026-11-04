// GET /files/:id/content 的响应头 —— M80
//
// ⚠️ 这里守的不是"能不能下载",而是**文件名能不能把响应头拆开**。
// FileName 是原样入库的用户输入,而 Content-Disposition 里必须原样带出它
// (不然用户下载到的全是 "download"),于是每一个转义漏掉的地方都是一个
// 响应头注入点 —— CRLF 能拼出第二个响应头;引号能提前闭合 quoted-string;
// 非 ASCII 进 header 会被部分代理按 latin-1 存成乱码。
//
// 不需要起服务:直接对 `app.RequestContext` 调一次,断言它 Header 里的值。
package v1

import (
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/require"
)

// hdr 取响应头。hertz 的 Peek 在没值时返 ""
func hdr(c *app.RequestContext, key string) string {
	return string(c.Response.Header.Peek(key))
}

// requireNoRawCRLF 断言响应头值里没有裸 CR / LF
func requireNoRawCRLF(t *testing.T, v string) {
	t.Helper()
	require.False(t, strings.ContainsAny(v, "\r\n"),
		"响应头值里出现了裸 CR/LF —— 能拼出第二个响应头: %q", v)
}

func TestSetContentDisposition_PlainName(t *testing.T) {
	var c app.RequestContext
	setContentDisposition(&c, "plan.pdf", "inline")

	got := hdr(&c, "Content-Disposition")
	require.NotEmpty(t, got)
	require.True(t, strings.HasPrefix(got, "inline; "), "disposition 必须打头, got %q", got)
	require.Contains(t, got, `filename="plan.pdf"`)
	require.Contains(t, got, "filename*=UTF-8''plan.pdf")
	requireNoRawCRLF(t, got)
}

// 特征:**CRLF 注入被打掉**。这是这一组里最锋利的一条。
func TestSetContentDisposition_CRLFInjection(t *testing.T) {
	var c app.RequestContext
	setContentDisposition(&c, "a.txt\r\nX-Injected: 1", "attachment")

	got := hdr(&c, "Content-Disposition")
	requireNoRawCRLF(t, got)
	require.Empty(t, hdr(&c, "X-Injected"), "不能拼出第二个响应头")
	require.Contains(t, got, `%0D%0A`, "filename* 里 CR/LF 要按 RFC 5987 转义, got %q", got)
	require.Contains(t, got, `filename="a.txt__X-Injected`, "ASCII 兜底位里 CR/LF 变 _, got %q", got)
}

func TestSetContentDisposition_QuoteInjection(t *testing.T) {
	var c app.RequestContext
	// 原名里的 " 能提前闭合 quoted-string,后面的 filename* 就会被当成
	// quoted-string 的一部分,老客户端读到的是 `x` 而不是真名。
	setContentDisposition(&c, `x"; evil="1.txt`, "attachment")

	got := hdr(&c, "Content-Disposition")
	require.Contains(t, got, `filename="x\"; evil=\"1.txt"`, `引号必须被转义, got %q`, got)
	requireNoRawCRLF(t, got)
}

// 特征:反斜杠也要转义。quoted-string 里的裸 `\` 会把下一个字符变成转义字符,
// `a\"b.txt` 会被客户端读成 `a"b.txt` —— 文件名就变了。
func TestSetContentDisposition_BackslashIsEscaped(t *testing.T) {
	var c app.RequestContext
	setContentDisposition(&c, `dir\file.txt`, "attachment")
	require.Contains(t, hdr(&c, "Content-Disposition"), `filename="dir\\file.txt"`)
}

// 特征:中文名走 filename*(RFC 5987),ASCII 兜底位非 ASCII 全变下划线。
// 两个都给是有原因的:老客户端只认 filename=,新客户端优先 filename*。
func TestSetContentDisposition_ChineseName(t *testing.T) {
	var c app.RequestContext
	setContentDisposition(&c, "户型图 v2.pdf", "inline")

	got := hdr(&c, "Content-Disposition")
	// ⚠️ 9 个下划线不是数错了:"户型图" 是 3 个汉字 = **9 个字节**,
	// 而兜底位是逐字节判的。
	require.Contains(t, got, `filename="_________ v2.pdf"`,
		"ASCII 兜底位非 ASCII 要变 _, got %q", got)
	require.Contains(t, got, "filename*=UTF-8''%E6%88%B7%E5%9E%8B%E5%9B%BE%20v2.pdf",
		"真名要按 RFC 5987 转义, got %q", got)
}

// 特征:空名兜底。空 filename= 在不同客户端下表现不一(有的当无名,有的报错)。
func TestSetContentDisposition_EmptyName(t *testing.T) {
	var c app.RequestContext
	setContentDisposition(&c, "", "attachment")
	require.Contains(t, hdr(&c, "Content-Disposition"), `filename="download"`)
}

// ====================== isInlineType ======================

// 特征:**SVG 必须被排除**。SVG 里能塞 <script>,而 /content 是**同源**响应
// —— 内联一个用户上传的 SVG 等于把本站的存储变成 XSS 投放位。
func TestIsInlineType_SVGIsNeverInline(t *testing.T) {
	require.False(t, isInlineType("image/svg+xml"))
	require.False(t, isInlineType("image/svg+xml; charset=utf-8"))
	require.False(t, isInlineType("IMAGE/SVG+XML"), "大小写归一化后再判")
	// 同一个前缀下的其它格式照常内联
	require.True(t, isInlineType("image/png"))
	require.True(t, isInlineType("image/webp"))
}

func TestIsInlineType(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/webp", true},
		{"video/mp4", true},
		{"audio/mpeg", true},
		{"application/pdf", true},
		{"text/plain", true},
		{"application/zip", false},
		{"application/octet-stream", false},
		{"application/x-msdownload", false},
		{"text/html", false}, // 内联 HTML = 同源 XSS
		{"  image/png  ", true},
		{"", false},
	}
	for _, tc := range cases {
		require.Equalf(t, tc.want, isInlineType(tc.ct), "content_type=%q", tc.ct)
	}
}
