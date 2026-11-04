// RequestGzip 中间件 —— 请求体 gzip 解压(§1.3 补 P2)
//
// 目的:
//   - 客户端可用 Content-Encoding: gzip 提交 body,服务端自动解压
//   - 业务 handler 拿到的就是解压后的明文(透明)
//   - 配套 M9a Gzip 响应压缩 → 双向压缩
//
// 用法(挂在 router.go BodyLimit 之后,业务 handler 之前):
//
//	h.Use(middleware.RequestGzip())
//
// 行为:
//   - 缺 Content-Encoding 或 "identity" → 透传
//   - Content-Encoding: gzip → 解压,改写 Content-Encoding 为 identity,SetBody 写回
//   - 解压失败 → 400 errcode.PayloadMalformed(不是 5xx,客户端问题)
//   - 解压后超 10MB → 415 errcode.PayloadTooLarge(已被 BodyLimit 拦,这里再保险一次)
//
// 设计要点:
//   - 不读 body 时(c.Request.Body() 长度==0)→ 透传
//   - peek body 走 Request.Body()(同 sign.go 的稳妥模式)
package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/response"
)

const reqGzipHeader = "Content-Encoding"

const reqGzipMaxDecompressed = 10 << 20 // 10MB(防止 zip bomb)

// RequestGzip 请求体 gzip 解压中间件
func RequestGzip() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// 1. 无 body 透传
		body := c.Request.Body()
		if len(body) == 0 {
			c.Next(ctx)
			return
		}
		// 2. 读 Content-Encoding
		enc := strings.ToLower(strings.TrimSpace(string(c.Request.Header.Peek(reqGzipHeader))))
		if enc == "" || enc == "identity" {
			c.Next(ctx)
			return
		}
		if enc != "gzip" {
			// 仅支持 gzip,其他编码(brotli/deflate)暂不接
			response.Fail(c, errcode.UnsupportedMediaType, errcode.Msg(errcode.UnsupportedMediaType))
			c.Abort()
			return
		}
		// 3. 解压
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			response.Fail(c, errcode.PayloadMalformed, "gzip header 无效或 body 损坏")
			c.Abort()
			return
		}
		defer gr.Close()
		// 限制读出字节数(防 zip bomb)
		dec, err := io.ReadAll(io.LimitReader(gr, reqGzipMaxDecompressed+1))
		if err != nil {
			response.Fail(c, errcode.PayloadMalformed, "gzip 解压失败: "+err.Error())
			c.Abort()
			return
		}
		if int64(len(dec)) > reqGzipMaxDecompressed {
			response.Fail(c, errcode.PayloadTooLarge, "解压后 body 超过限制")
			c.Abort()
			return
		}
		// 4. 写回明文 + 清 Content-Encoding(下游无感)
		c.Request.SetBody(dec)
		c.Request.Header.Del(reqGzipHeader)
		c.Next(ctx)
	}
}
