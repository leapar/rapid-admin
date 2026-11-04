// BodyLimit 中间件单测
//
// 这个中间件的价值全在 PathLimits 上 —— 默认 1MB 挡得住 JSON 业务,但挡不住
// 上传。历史实现里 PathLimits 一次都没生效过(见 BodyLimit 内的注释),而症状
// (传 3MB 的图必 413)和原因(中间件里明明配了 20MB)对不上号,所以这几条用例
// 锁的就是"到底哪条路径拿到哪个上限"。
package middleware

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/route"
)

// blRun 跑一次请求,返回 status 和 handler 是否被调用过。
//
// ⚠️ 断言要**同时**看 status 和 ran:命中阈值时中间件直接写 413 并**不调
// c.Next**,而 handler 永远返 200 —— 只看 status 的话,两种情况的信号是相反的,
// 漏看 ran 会把"没跑到 handler"误判成"阈值算错了"。
func blRun(t *testing.T, cfg BodyLimitConfig, path string, size int) (int, bool) {
	t.Helper()
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(BodyLimit(cfg))
	ran := false
	engine.POST(path, func(ctx context.Context, c *app.RequestContext) {
		ran = true
		c.String(http.StatusOK, "ok")
	})

	// ⚠️ **必须显式带 Content-Length**:hertz 的 `Request.SetBody()` 只写
	// bodyRaw,不动 header(见 protocol/request.go:443),而 BodyLimit 判的是
	// `c.Request.Header.ContentLength()`。不补这个头的话所有用例都会"放行",
	// 看起来像阈值配错了,其实是测试没把线上一定会有的东西搭出来。
	w := ut.PerformRequest(engine, "POST", path,
		&ut.Body{Body: bytes.NewReader(make([]byte, size)), Len: size},
		ut.Header{Key: "Content-Length", Value: strconv.Itoa(size)},
	)
	return w.Code, ran
}

func blCfg() BodyLimitConfig {
	return BodyLimitConfig{
		Default: 1 << 20,
		PathLimits: map[string]int64{
			"/rapid-admin/v1/biz/files/*":    20 << 20,
			"/rapid-admin/v1/biz/files/mp/*": 4 << 20,
		},
	}
}

// TestBodyLimit_PathLimitLargerThanDefault 是本文件的核心一条:
// 上传路由的 20MB 比全局 1MB **大**,而旧实现写的是 `l < limit`,于是永远匹配不上 ——
// 全局 1MB 反而把上传拦死了。这是 PathLimits 存在的唯一理由被自己否掉。
func TestBodyLimit_PathLimitLargerThanDefault(t *testing.T) {
	code, ran := blRun(t, blCfg(), "/rapid-admin/v1/biz/files", 5<<20)
	if code != http.StatusOK || !ran {
		t.Fatalf("上传路径应放行 5MB,got code=%d ran=%v", code, ran)
	}

	code, ran = blRun(t, blCfg(), "/rapid-admin/v1/biz/files", 21<<20)
	if code != http.StatusRequestEntityTooLarge || ran {
		t.Fatalf("超路径上限应 413 且不调 handler,got code=%d ran=%v", code, ran)
	}
}

// TestBodyLimit_UnlistedPathFallsBackToDefault 锁住白名单的另一侧:
// 没命中的路径必须落回 Default,不能被白名单里别人的宽松值"传染"。
func TestBodyLimit_UnlistedPathFallsBackToDefault(t *testing.T) {
	code, ran := blRun(t, blCfg(), "/rapid-admin/v1/biz/folders", 2<<20)
	if code != http.StatusRequestEntityTooLarge || ran {
		t.Fatalf("未命中路径应落回 Default(1MB),got code=%d ran=%v", code, ran)
	}
}

// TestBodyLimit_LongestPrefixWins 两条都命中时取更长的那条。
// 旧实现里这个结果取决于 map 的遍历顺序(随机),所以"稳定复现"本身就是断言。
func TestBodyLimit_LongestPrefixWins(t *testing.T) {
	// /rapid-admin/v1/biz/files/mp/part 同时命中两条 → 取 mp/* 的 4MB
	code, ran := blRun(t, blCfg(), "/rapid-admin/v1/biz/files/mp/part", 5<<20)
	if code != http.StatusRequestEntityTooLarge || ran {
		t.Fatalf("最长前缀应胜出(4MB),got code=%d ran=%v", code, ran)
	}
	// 而 4MB 仍然宽于全局 1MB → 2MB 应放行,证明没退回 Default
	code, ran = blRun(t, blCfg(), "/rapid-admin/v1/biz/files/mp/part", 2<<20)
	if code != http.StatusOK || !ran {
		t.Fatalf("最长前缀仍应放宽到 4MB,got code=%d ran=%v", code, ran)
	}
}

// TestBodyLimit_ZeroDefaultMeansUnlimited 0 = 不限,且白名单里 <=0 的条目视为未配置
func TestBodyLimit_ZeroDefaultMeansUnlimited(t *testing.T) {
	cfg := BodyLimitConfig{Default: 0, PathLimits: map[string]int64{"/x/*": 0}}
	code, ran := blRun(t, cfg, "/x/y", 8<<20)
	if code != http.StatusOK || !ran {
		t.Fatalf("Default=0 且白名单值也 0 应完全不限,got code=%d ran=%v", code, ran)
	}
	// 白名单给了正值时,Default=0 也要能被它收紧
	cfg2 := BodyLimitConfig{Default: 0, PathLimits: map[string]int64{"/x/*": 1 << 20}}
	code, ran = blRun(t, cfg2, "/x/y", 2<<20)
	if code != http.StatusRequestEntityTooLarge || ran {
		t.Fatalf("Default=0 时白名单应生效,got code=%d ran=%v", code, ran)
	}
}

// TestBodyLimit_ResponseShape 413 的响应体要带业务码 41300,否则前端拿到一个
// 没有 code 的裸 413,没法和别的错误区分。
func TestBodyLimit_ResponseShape(t *testing.T) {
	engine := route.NewEngine(config.NewOptions([]config.Option{}))
	engine.Use(BodyLimit(blCfg()))
	engine.POST("/rapid-admin/v1/biz/files", func(ctx context.Context, c *app.RequestContext) {
		c.String(http.StatusOK, "ok")
	})
	const size = 21 << 20
	w := ut.PerformRequest(engine, "POST", "/rapid-admin/v1/biz/files",
		&ut.Body{Body: bytes.NewReader(make([]byte, size)), Len: size},
		ut.Header{Key: "Content-Length", Value: strconv.Itoa(size)},
	)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d, want 413", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"code":41300`) {
		t.Fatalf("body = %q, want 包含 \"code\":41300", w.Body.String())
	}
}

func TestMatchPath(t *testing.T) {
	cases := []struct {
		pattern, actual string
		want            bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b", "/a/bc", false},
		{"/a/b", "/a/b/c", false},
		{"/a/b/*", "/a/b", true}, // 前缀去掉 /* 之后正好等于裸路径
		{"/a/b/*", "/a/b/c", true},
		{"/a/b/*", "/a/bc", false},
		{"/a/b/*", "/x/a/b/c", false},
	}
	for _, c := range cases {
		if got := matchPath(c.pattern, c.actual); got != c.want {
			t.Errorf("matchPath(%q,%q)=%v want %v", c.pattern, c.actual, got, c.want)
		}
	}
}
