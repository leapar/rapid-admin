// Gzip 中间件单测 —— 锁住"流式响应不被压缩"这条契约
//
// 背景:M80 加了 GET /biz/files/:id/content(SetBodyStream 读字节),
// 而 Gzip 是全局中间件,跑在 handler 之后会调 c.Response.Body()。
// hertz 的 Body() 是 BodyE():全量 copy 进堆 buffer + CloseBodyStream。
// 不挡的话 = 整个文件进内存,症状是 OOM 而不是报错,所以只能靠测试钉死。
package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/cloudwego/hertz/pkg/route"
)

// countingStream 记一笔读了多少字节 —— 只是为了让"流被谁读的"在出错时可见,
// 不参与断言(ut.PerformRequest 自己也会 drain 一次,时序在中间件之后)。
type countingStream struct {
	r    *strings.Reader
	read int
}

func (s *countingStream) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	s.read += n
	return n, err
}

func (s *countingStream) Close() error { return nil }

// newGzipEngine 注册三条路由,共用 minSize=1024 的 Gzip
func newGzipEngine() *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(Gzip(1024))

	// 流式 + 不在压缩跳过名单里的类型。text/plain 是故意的:换成
	// video/mp4 的话第 4 步的 content-type 判断就会顺手放过,测试假绿。
	engine.GET("/stream", func(ctx context.Context, c *app.RequestContext) {
		c.Response.Header.SetContentType("text/plain")
		c.SetStatusCode(consts.StatusOK)
		c.Response.SetBodyStream(&countingStream{r: strings.NewReader(strings.Repeat("a", 5000))}, 5000)
	})

	// 流式 + 在跳过名单里的类型。证明判据是"是不是流",不是 content-type。
	engine.GET("/stream-img", func(ctx context.Context, c *app.RequestContext) {
		c.Response.Header.SetContentType("image/jpeg")
		c.SetStatusCode(consts.StatusOK)
		c.Response.SetBodyStream(&countingStream{r: strings.NewReader(strings.Repeat("a", 5000))}, 5000)
	})

	// 缓冲响应,必须仍然被压缩(守卫不能顺手把普通响应也关掉)
	engine.GET("/plain", func(ctx context.Context, c *app.RequestContext) {
		c.Response.Header.SetContentType("text/plain")
		c.SetStatusCode(consts.StatusOK)
		c.Response.SetBody([]byte(strings.Repeat("a", 5000)))
	})
	return engine
}

var gzAccept = ut.Header{Key: "Accept-Encoding", Value: "gzip"}

func TestGzip_SkipsBodyStreamResponse(t *testing.T) {
	w := ut.PerformRequest(newGzipEngine(), consts.MethodGet, "/stream", nil, gzAccept)
	if w.Code != consts.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	// 契约 1:流式响应不压。删掉守卫时 text/plain 会被压 → 头出现。
	if enc := string(w.Header().Peek(consts.HeaderContentEncoding)); enc != "" {
		t.Errorf("Content-Encoding = %q, want empty(流式响应不该被压)", enc)
	}
	// 契约 2:字节原样,没被 gzip 改写。
	if got := w.Body.Len(); got != 5000 {
		t.Errorf("body len = %d, want 5000", got)
	}
}

func TestGzip_StreamSkippedRegardlessOfContentType(t *testing.T) {
	// image/jpeg 本来就在跳过名单里,所以这条用例在"没有守卫"时也会过。
	// 它防的是另一种改法:有人把守卫收窄成"只挡某些 content-type"。
	w := ut.PerformRequest(newGzipEngine(), consts.MethodGet, "/stream-img", nil, gzAccept)
	if enc := string(w.Header().Peek(consts.HeaderContentEncoding)); enc != "" {
		t.Errorf("Content-Encoding = %q, want empty", enc)
	}
	if got := w.Body.Len(); got != 5000 {
		t.Errorf("body len = %d, want 5000", got)
	}
}

func TestGzip_StillCompressesBufferedResponse(t *testing.T) {
	w := ut.PerformRequest(newGzipEngine(), consts.MethodGet, "/plain", nil, gzAccept)
	if w.Code != consts.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if enc := string(w.Header().Peek(consts.HeaderContentEncoding)); enc != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip(缓冲响应仍要压)", enc)
	}
	// 压完必须还能解开,否则是"压坏了"而不是"没压"
	r, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read gunzipped: %v", err)
	}
	if len(got) != 5000 {
		t.Errorf("gunzipped len = %d, want 5000", len(got))
	}
}