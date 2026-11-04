// RequestGzip 中间件单测(§1.3 补 P2)
package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
)

func gz(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newReqGzipEngine() *route.Engine {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(RequestGzip())
	engine.POST("/x", func(ctx context.Context, c *app.RequestContext) {
		// handler 读到的应是解压后明文
		c.String(http.StatusOK, "got:"+string(c.Request.Body()))
	})
	return engine
}

func TestRequestGzip_NoEncoding_PassThrough(t *testing.T) {
	engine := newReqGzipEngine()
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader([]byte(`{"k":"v"}`)), Len: 9},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !containsStr(w.Body.String(), `{"k":"v"}`) {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestRequestGzip_Identity_PassThrough(t *testing.T) {
	engine := newReqGzipEngine()
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader([]byte(`plain`)), Len: 5},
		ut.Header{Key: "Content-Encoding", Value: "identity"},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (identity pass-through)", w.Code)
	}
	if !containsStr(w.Body.String(), "plain") {
		t.Fatalf("body = %q", w.Body.String())
	}
}

func TestRequestGzip_GzipEncoded_Decoded(t *testing.T) {
	engine := newReqGzipEngine()
	plain := []byte(`{"name":"张三丰","phone":"13800001234"}`)
	gzipped := gz(t, plain)
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader(gzipped), Len: len(gzipped)},
		ut.Header{Key: "Content-Encoding", Value: "gzip"},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "张三丰") {
		t.Fatalf("body = %q, want containing 解压后明文", w.Body.String())
	}
}

func TestRequestGzip_GzipCaseInsensitive(t *testing.T) {
	engine := newReqGzipEngine()
	plain := []byte("hello")
	gzipped := gz(t, plain)
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader(gzipped), Len: len(gzipped)},
		ut.Header{Key: "Content-Encoding", Value: "GZIP"},
	)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (case-insensitive)", w.Code)
	}
}

func TestRequestGzip_CorruptBody_Rejected(t *testing.T) {
	engine := newReqGzipEngine()
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader([]byte("not-gzip-data")), Len: 13},
		ut.Header{Key: "Content-Encoding", Value: "gzip"},
	)
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (corrupt gzip)", w.Code)
	}
	if !containsStr(w.Body.String(), "gzip") {
		t.Fatalf("body = %q, want containing 'gzip'", w.Body.String())
	}
}

func TestRequestGzip_UnsupportedEncoding_Rejected(t *testing.T) {
	engine := newReqGzipEngine()
	w := ut.PerformRequest(engine, "POST", "/x",
		&ut.Body{Body: bytes.NewReader([]byte("xx")), Len: 2},
		ut.Header{Key: "Content-Encoding", Value: "br"},
	)
	if w.Code < 400 {
		t.Fatalf("status = %d, want >=400 (brotli unsupported)", w.Code)
	}
	if !containsStr(w.Body.String(), "Content-Encoding") {
		t.Fatalf("body = %q, want containing 'Content-Encoding'", w.Body.String())
	}
}

func TestRequestGzip_EmptyBody_PassThrough(t *testing.T) {
	engine := newReqGzipEngine()
	w := ut.PerformRequest(engine, "POST", "/x", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty pass)", w.Code)
	}
}
