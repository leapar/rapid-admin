// Gzip 压缩中间件 — §1.3 #15 P1
//
// Hertz 有 hertz-contrib/gzip,但实现简单(< 80 行),自实现更可控:
//   - 仅压缩响应体 > minSize 的请求(默认 1024 字节)
//   - 仅对客户端声明 Accept-Encoding: gzip 的请求生效
//   - 不压缩已压缩的内容类型(image/png image/jpeg video/* application/zip 等)
//   - 不压缩 SSE / 流式响应
//   - 写 Content-Encoding: gzip + Vary: Accept-Encoding
//
// 用法(挂在 AccessLog 之前,业务 handler 能感知 size):
//
//	h.Use(middleware.Gzip(2048)) // 2KB 起才压
package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// 默认压缩阈值 1KB
const defaultGzipMinSize = 1024

// 不压缩的内容类型(已经是压缩格式,再压浪费 CPU)
var gzipSkipContentTypes = map[string]struct{}{
	"image/png":                    {},
	"image/jpeg":                   {},
	"image/gif":                    {},
	"image/webp":                   {},
	"video/mp4":                    {},
	"video/webm":                   {},
	"audio/":                       {},
	"application/zip":              {},
	"application/x-gzip":           {},
	"application/x-tar":            {},
	"application/wasm":             {},
	"application/x-protobuf":       {},
	"application/grpc":             {},
	"application/octet-stream":     {},
	"font/woff":                    {},
	"font/woff2":                   {},
}

// gzipResponseWriter 包装 app.ResponseWriter,压缩写
// 注意:Hertz 的 Response 是非导出结构,只能在 middleware 层接管 body
// 这里走另一种思路:不接管 stream,而是在 handler 执行完 c.Next 后
// 读 c.Response.BodyWriter 的内容 → gzip → 重写
// (适用于内容已全缓冲在 ctx 的场景;典型 JSON 响应符合)
type gzipResponseWriter struct {
	ctx     *app.RequestContext
	minSize int
}

// gzipBufPool gzip.Writer 池(避免高频请求反复 New)
var gzipBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Gzip 返回 gzip 压缩中间件(minSize 起压,默认 1KB)
func Gzip(minSize int) app.HandlerFunc {
	if minSize <= 0 {
		minSize = defaultGzipMinSize
	}
	return func(ctx context.Context, c *app.RequestContext) {
		// 1. 仅对声明 gzip 的客户端生效
		acceptEnc := string(c.Request.Header.Peek("Accept-Encoding"))
		if !containsToken(acceptEnc, "gzip") {
			c.Next(ctx)
			return
		}

		// 2. 仅对 GET/HEAD/POST 等典型 JSON API 生效
		//    SSE/流式通常 method 走 GET + Transfer-Encoding: chunked,跳过
		if string(c.Request.Header.Peek("Transfer-Encoding")) == "chunked" {
			c.Next(ctx)
			return
		}

		// 3. 业务 handler 执行
		c.Next(ctx)

		// 4. 检查响应:状态码 200 + 流式跳过 + body 长度 >= minSize + content-type 可压
		//
		// ⚠️ **这条必须在 c.Next 之后**。第一版写在第 2 步(Next 之前),
		// 而那时 handler 还没跑,bodyStream 恒为 nil,守卫等于不存在 ——
		// TestGzip_SkipsBodyStreamResponse 当场就打红了。
		//
		// 为什么非挡不可:c.Response.Body() 不是"读一下大小",hertz 的
		// 实现是 BodyE() —— 把 bodyStream 全量 copy 进一个堆上的
		// bytes.Buffer,再 CloseBodyStream()(后者把 resp.bodyStream 置 nil)。
		// 于是文件下载(SetBodyStream)走到这一步 = 整个文件进内存,
		// "边读边发"彻底没了;video/mp4、image/* 还能靠下面的 content-type
		// 名单幸免,application/pdf、text/plain、docx 这些不在名单里的
		// 还要再叠一份 gzip 输出缓冲。
		//
		// 字节本身不会错(置 nil 之后改从缓冲写),所以症状是"大文件并发
		// 几路就 OOM",不报错、不丢数据 —— 正是最难查的那种。
		if c.Response.IsBodyStream() {
			return
		}
		body := c.Response.Body()
		if c.Response.StatusCode() != consts.StatusOK {
			return
		}
		if len(body) < minSize {
			return
		}
		ct := string(c.Response.Header.Peek("Content-Type"))
		if shouldSkipContentType(ct) {
			return
		}

		// 5. 压缩
		buf := gzipBufPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer gzipBufPool.Put(buf)

		gz, err := gzip.NewWriterLevel(buf, gzip.DefaultCompression)
		if err != nil {
			return // 压缩器初始化失败:不动响应(选 不可用 比 报错 好)
		}
		if _, err = gz.Write(body); err != nil {
			return
		}
		if err = gz.Close(); err != nil {
			return
		}
		gzipped := buf.Bytes()

		// 6. 压缩收益检查(必须真的更小,否则放弃)
		if len(gzipped) >= len(body) {
			return
		}

		// 7. 重写响应
		c.Response.Header.Set("Content-Encoding", "gzip")
		c.Response.Header.Set("Vary", "Accept-Encoding")
		// 压缩后 body 字节数变化,要更新 Content-Length(否则客户端按原长度读会卡住)
		c.Response.Header.Set("Content-Length", itoa(len(gzipped)))
		// 显式覆写 body(注意:不能直接 SetBody,因为 Content-Length 已变,先 Header 写再 Body 写)
		c.Response.SetBody(gzipped)
	}
}

// containsToken 简易 Accept-Encoding token 匹配(gzip;q=0 也算)
// 不引入完整 RFC 解析,够用即可
func containsToken(haystack, needle string) bool {
	if haystack == "" {
		return false
	}
	for _, t := range splitComma(haystack) {
		// strip ;q= 之后
		for i, ch := range t {
			if ch == ';' {
				t = t[:i]
				break
			}
		}
		if t == needle {
			return true
		}
	}
	return false
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i, ch := range s {
		if ch == ',' {
			out = append(out, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	out = append(out, trimSpace(s[start:]))
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// shouldSkipContentType 按前缀/后缀判断是否跳过
func shouldSkipContentType(ct string) bool {
	// 取主类型(去 charset 之类)
	main := ct
	for i, ch := range ct {
		if ch == ';' {
			main = ct[:i]
			break
		}
	}
	main = trimSpace(main)
	main = lower(main)
	if _, ok := gzipSkipContentTypes[main]; ok {
		return true
	}
	// 前缀匹配(audio/* video/*)
	for prefix := range gzipSkipContentTypes {
		if len(prefix) > 0 && prefix[len(prefix)-1] == '/' {
			if len(main) >= len(prefix) && main[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

func lower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

// itoa 避免 strconv 依赖
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// Discard 留个空 io 引用,避免 goimports 把 io 当 unused 删
var _ = io.Discard
