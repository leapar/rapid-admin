package errcode

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// 本文件测的都是 stack.go:创建/包装点拍栈、跨层去重、过滤、
// errors.As/Is 穿透与 CodeOf 兼容。

// assertStackShape 栈文本的基本形状:非空、有编号层、有帧行。
func assertStackShape(t *testing.T, s string) {
	t.Helper()
	if s == "" {
		t.Fatal("栈文本为空")
	}
	if !strings.Contains(s, "1. ") {
		t.Fatalf("缺编号层头,输出:\n%s", s)
	}
	if !strings.Contains(s, ")") || !strings.Contains(s, ".go:") {
		t.Fatalf("缺帧行(func + file:line),输出:\n%s", s)
	}
}

// selfFile 本测试文件路径片段 —— 栈里必须能看到它(帧来自业务调用点)。
func selfFile(t *testing.T, s string) {
	t.Helper()
	if !strings.Contains(s, "stack_test.go") {
		t.Fatalf("栈里找不到本测试文件,输出:\n%s", s)
	}
}

func TestNew_CapturesCallSiteStack(t *testing.T) {
	e := New(Internal, "测试错误") // 这一行的调用点必须出现在栈里
	s := e.Stack()
	assertStackShape(t, s)
	selfFile(t, s)
	if !strings.Contains(s, "TestNew_CapturesCallSiteStack") {
		t.Fatalf("栈里找不到测试函数名,输出:\n%s", s)
	}
	// 框架机器帧(stack.go / error.go)不应出现;测试帧在 errcode 包里
	// 是合法的,按包断言会误杀(见 stackFilterKeys 注释)。
	if strings.Contains(s, "errcode/stack.go") || strings.Contains(s, "errcode/error.go") {
		t.Fatalf("栈里混入了 errcode 机器帧,输出:\n%s", s)
	}
}

func TestNewf_CapturesCallSiteStack(t *testing.T) {
	e := Newf(Conflict, "冲突 %d", 7)
	s := e.Stack()
	assertStackShape(t, s)
	selfFile(t, s)
}

func TestWrap_ShowsWrapSite(t *testing.T) {
	err := Wrap(errors.New("boom"), "第 1 步失败")
	s := StackOf(err)
	assertStackShape(t, s)
	selfFile(t, s)
	if !strings.Contains(s, "第 1 步失败") {
		t.Fatalf("栈文本缺本层消息,输出:\n%s", s)
	}
}

func TestWrap_NilError_ReturnsNil(t *testing.T) {
	if got := Wrap(nil, "msg"); got != nil {
		t.Fatalf("Wrap(nil) 应为 nil,得到 %v", got)
	}
	if got := Wrapf(nil, "msg %d", 1); got != nil {
		t.Fatalf("Wrapf(nil) 应为 nil,得到 %v", got)
	}
}

func TestWrap_PreservesCodeOfAndAs(t *testing.T) {
	biz := New(Forbidden, "无权")
	err := Wrapf(biz, "查 %d 关审批人失败", 2)

	if got := CodeOf(err); got != Forbidden {
		t.Fatalf("CodeOf 穿透失败:got=%d want=%d", got, Forbidden)
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatal("errors.As 穿透失败")
	}
	if !errors.Is(err, biz) {
		t.Fatal("errors.Is 穿透失败")
	}
}

func TestStackOf_PlainError_ReturnsEmpty(t *testing.T) {
	// 纯 fmt.Errorf 链没有栈可给 —— 空串是"诚实",不是故障。
	plain := fmt.Errorf("outer: %w", errors.New("inner"))
	if got := StackOf(plain); got != "" {
		t.Fatalf("纯 error 链 StackOf 应为空串,得到:\n%s", got)
	}
}

func TestStackOf_Nil_ReturnsEmpty(t *testing.T) {
	if got := StackOf(nil); got != "" {
		t.Fatalf("StackOf(nil) 应为空串,得到:%q", got)
	}
}

func TestStack_MultiLayer_MessagesNumbered(t *testing.T) {
	// fmt.Errorf(无栈)→ Wrap(有栈)→ 哨兵(有栈):三层消息都要出现,
	// 且各层只出现自己的消息,不重复深层内容。
	sentinel := New(Conflict, "底层数据冲突")
	mid := Wrap(sentinel, "处理失败")
	err := fmt.Errorf("outer: %w", mid)

	s := StackOf(err)
	for _, want := range []string{"outer", "处理失败", "底层数据冲突"} {
		if !strings.Contains(s, want) {
			t.Fatalf("栈文本缺层消息 %q,输出:\n%s", want, s)
		}
	}
	// "底层数据冲突"只应出现一次(作为最后一层),不被 "outer: ..." 重复携带
	if strings.Count(s, "底层数据冲突") != 1 {
		t.Fatalf("深层消息重复出现,输出:\n%s", s)
	}
}

func TestStack_DedupAcrossLayers(t *testing.T) {
	// 两层共享同一份帧(同包手工构造):帧只允许出现在最深层,上层去重。
	st := capture()
	inner := &wrapError{msg: "inner", st: st}
	outer := &wrapError{msg: "outer", err: inner, st: st}

	s := StackOf(outer)
	if strings.Count(s, "1).") > len(st) {
		t.Fatalf("去重失败,帧数超出单层,输出:\n%s", s)
	}
	// 每条 file:line 只出现一次
	lines := strings.Split(s, "\n")
	seen := map[string]int{}
	for _, ln := range lines {
		if strings.Contains(ln, ".go:") {
			seen[ln]++
		}
	}
	for ln, n := range seen {
		if n > 1 {
			t.Fatalf("file:line 重复出现 %d 次:%q", n, ln)
		}
	}
}

func TestStack_LiteralErrorWithoutStack_RendersNoFrames(t *testing.T) {
	// 字面量构造的 Error 没有 st:渲染消息但诚实的不给帧。
	e := &Error{Code: Internal, Message: "手工构造"}
	s := e.Stack()
	if !strings.Contains(s, "手工构造") {
		t.Fatalf("缺本层消息,输出:\n%s", s)
	}
	if strings.Contains(s, ".go:") {
		t.Fatalf("无栈错误不应渲染帧,输出:\n%s", s)
	}
}

func TestErrorMethod_MessagesJoined(t *testing.T) {
	err := Wrapf(New(Internal), "上下文 %d", 9)
	want := "上下文 9: [50000] 服务内部错误"
	if got := err.Error(); got != want {
		t.Fatalf("Error() 形状变了:got=%q want=%q", got, want)
	}
}
