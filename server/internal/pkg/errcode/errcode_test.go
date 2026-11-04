package errcode

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ── M98(2026-10-26)可逆探针记录 ───────────────────────────────────────
//
//   拆法                                            必须变红
//   ──────────────────────────────────────────  ────────────────────────────────────
//   P1 HTTPStatus 里 401xx 桶挪到大桶之后          TestHTTPStatus_SpecialCodes
//   P2 HTTPStatus 里 413xx 桶删掉                  TestHTTPStatus_SpecialCodes
//   P3 HTTPStatus 里 NotImplemented 改成 500        TestHTTPStatus_SpecialCodes
//   P4 HTTPStatus 里 ServiceUnavailable 改成 500    TestHTTPStatus_SpecialCodes
//   P5 CodeOf 换回裸类型断言                       TestCodeOf_UnwrapsWrapped
//   P6 defaultMsg 里删掉 NotImplemented             TestEveryCodeHasDefaultMessage
//   P7 文档生成器换回本地 httpStatus 拷贝           TestEveryCodeInGeneratedDoc
//
//   7/7 全部变红。
//
// ⚠️ **P1 是最容易假绿的那个**:把 401xx 桶挪到大桶之后,`HTTPStatus(40100)`
// 变 400 —— 但如果测试只断言"40100 落在 40000-49999 里",就**抓不到**。
// 必须把期望值写死成 401(见 TestHTTPStatus_SpecialCodes 的表),不能反过来
// "按桶推导期望值"。
//
// ⚠️ **P7 的拆法**:不是把生成器改回去(那样要重跑生成器才看得到差异),
// 而是直接改 `docs/error-codes.md` 里 50100 那一行的 HTTP 列 —— 断言必须
// 能发现"文档里的状态码跟代码对不上"。
// ───────────────────────────────────────────────────────────────────────

// ── AST 辅助:把 errcode.go 里的 const 抠出来 ──────────────────────────
//
// 为什么不在测试里手写一份码表?
// 因为手写的表**不会跟着代码长** —— 加了新码,测试照样绿,新码根本没被覆盖。
// 这里用 go/ast 读同目录的 errcode.go,把 const 块里的整型常量全抠出来,
// 于是"新加的码"自动进入测试范围。
//
// 读的是**源码文件**而不是运行时反射 —— Go 没有运行时反射拿 const,
// 而 defaultMsg 这个 map 恰好是"所有码"的运行时视图,但它不区分
// "码在 const 块里但漏了文案"和"码压根不存在"。
type codeEntry struct {
	Name  string
	Value int
}

// parseCodes 从 errcode.go 里提取所有整型常量
func parseCodes(t *testing.T) []codeEntry {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "errcode.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse errcode.go: %v", err)
	}
	var out []codeEntry
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue // 继承上一行的常量(iota),本文件没有
				}
				bl, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || bl.Kind != token.INT {
					continue
				}
				v, err := strconv.Atoi(bl.Value)
				if err != nil {
					continue
				}
				out = append(out, codeEntry{Name: name.Name, Value: v})
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("errcode.go 里一个整型常量都没解析到 —— AST 解析本身坏了")
	}
	return out
}

// ── 1. 映射表:特殊码的期望状态码写死 ──────────────────────────────────

// TestHTTPStatus_SpecialCodes 特殊业务码 → HTTP 状态码
//
// # 期望值必须写死,不能按桶推导
//
// 如果写成"401xx 落在 40000-49999 里,所以期望 400",那这条测试就是在
// **复述实现** —— 实现改了它跟着改,永远绿。写死期望值,实现一改就红。
//
// 这张表就是 M98 修的那三份拷贝里的错的**对照表**。
func TestHTTPStatus_SpecialCodes(t *testing.T) {
	cases := []struct {
		code int
		want int
		why  string
	}{
		{Success, 200, "成功"},
		{BadRequest, 400, "请求参数错误"},
		{ParamMissing, 400, "必填参数缺失"},
		{ParamInvalid, 400, "参数格式错误"},
		{InvalidParam, 400, "参数无效"},
		{Unauthorized, 401, "未登录"},
		{TokenExpired, 401, "令牌过期"},
		{TokenInvalid, 401, "令牌无效"},
		{Forbidden, 403, "没有权限"},
		{NoActiveIdentity, 403, "身份已停用"},
		{OutOfScope, 403, "超出工作范围"},
		{IdorAttempt, 403, "越权访问"},
		{NotFound, 404, "资源不存在"},
		{Conflict, 409, "数据冲突"},
		{PayloadTooLarge, 413, "请求体过大"},
		{TooManyRequests, 429, "限流"},
		{Internal, 500, "内部错误"},
		{NotImplemented, 501, "功能未实现 —— 不是 500"},
		{ServiceUnavailable, 503, "熔断打开 —— 不是 500"},
		{GatewayTimeout, 504, "网关超时 —— 不是 500"},
		{VersionNotSupported, 500, "版本不支持落 5xxxx 兜底桶"},
		{SignatureMissing, 500, "缺签名落 5xxxx 兜底桶"},
		{PayloadMalformed, 500, "坏 body 落 5xxxx 兜底桶"},
		{UnsupportedMediaType, 500, "不支持的编码落 5xxxx 兜底桶"},
		// 业务段(2xxxx / 3xxxx)一律 200 —— 语义由 body 的 code + i18n 承载
		{LoginFailed, 200, "业务码走 200"},
		{CaptchaWrong, 200, "业务码走 200"},
		{ApprovalNoTemplate, 200, "业务码走 200"},
		{ApprovalNoAssignee, 200, "业务码走 200"},
		{ConfigNotFound, 200, "业务码走 200"},
		{ConfigReadOnly, 200, "业务码走 200"},
	}
	for _, tc := range cases {
		if got := HTTPStatus(tc.code); got != tc.want {
			t.Errorf("HTTPStatus(%d) = %d, want %d (%s)", tc.code, got, tc.want, tc.why)
		}
	}
}

// TestHTTPStatus_Buckets 桶边界
//
// 码段的边界值必须落在正确的桶里 —— 边界写错(比如 < 写成了 <=)会让
// 相邻两段互相吃码。
func TestHTTPStatus_Buckets(t *testing.T) {
	cases := []struct {
		code int
		want int
	}{
		{0, 200},
		{1, 200},     // 成功附近
		{20000, 200}, // 2xxxx 下界
		{39999, 200}, // 3xxxx 上界
		{40000, 400}, // 4xxxx 下界
		{40099, 400}, // 4xxxx 段内(大桶)
		{40100, 401}, // 401xx 下界
		{40199, 401}, // 401xx 上界
		{40200, 400}, // 401xx 之后回大桶
		{40300, 403},
		{40399, 403},
		{40400, 404},
		{40499, 404},
		{40900, 409},
		{40999, 409},
		{41000, 400}, // 409xx 之后回大桶
		{41299, 400},
		{41300, 413},
		{41399, 413},
		{41400, 400},
		{42899, 400},
		{42900, 429},
		{42999, 429},
		{43000, 400},
		{49999, 400},
		{50000, 500},
		{50099, 500},
		{59999, 500},
	}
	for _, tc := range cases {
		if got := HTTPStatus(tc.code); got != tc.want {
			t.Errorf("HTTPStatus(%d) = %d, want %d", tc.code, got, tc.want)
		}
	}
}

// TestHTTPStatus_NarrowBeforeWide 细分桶必须排在大桶前面
//
// 这是 M98 的核心断言。errcode.HTTPStatus 曾经把 "40000-41000 → 400"
// 放在细分桶**前面**,于是 401/403/404/409 全被大桶吃掉返 400。
func TestHTTPStatus_NarrowBeforeWide(t *testing.T) {
	// 每个细分段的码,都必须在它自己那一档 —— 任何一个大桶都不能吃掉它
	narrow := map[int]int{
		40100: 401, 40101: 401, 40102: 401,
		40300: 403, 40301: 403, 40302: 403, 40303: 403,
		40400: 404,
		40900: 409,
		41300: 413,
		42900: 429,
	}
	for code, want := range narrow {
		got := HTTPStatus(code)
		if got != want {
			t.Errorf("HTTPStatus(%d) = %d, want %d —— 细分桶被大桶吃掉了", code, got, want)
		}
		if got == 400 && want != 400 {
			t.Errorf("HTTPStatus(%d) 落进了 400 大桶", code)
		}
	}
}

// TestHTTPRawMatchesHTTPStatus 废弃的 HTTPRaw 必须与 HTTPStatus 等价
//
// HTTPRaw 曾经是**另一份实现**(503/504 精确、其余兜底 500),
// 同一个码在两个函数里两个状态码。M98 合并成一份。
func TestHTTPRawMatchesHTTPStatus(t *testing.T) {
	for _, code := range parseCodes(t) {
		a, b := HTTPStatus(code.Value), HTTPRaw(code.Value)
		if a != b {
			t.Errorf("码 %d(%s):HTTPStatus=%d 但 HTTPRaw=%d —— 两份实现又分叉了",
				code.Value, code.Name, a, b)
		}
	}
}

// ── 2. 文案完整性 ──────────────────────────────────────────────────────

// TestEveryCodeHasDefaultMessage 每个登记的码都要有默认文案
//
// 没有默认文案的码,`Msg()` 会返"未知错误" —— 前端 i18n 也查不到,
// 用户看到的就是一句莫名其妙的"未知错误"。
func TestEveryCodeHasDefaultMessage(t *testing.T) {
	for _, c := range parseCodes(t) {
		msg, ok := defaultMsg[c.Value]
		if !ok {
			t.Errorf("码 %d(%s) 在 defaultMsg 里没有默认文案", c.Value, c.Name)
			continue
		}
		if strings.TrimSpace(msg) == "" {
			t.Errorf("码 %d(%s) 的默认文案是空串", c.Value, c.Name)
		}
	}
}

// TestMsgUnknown 未登记的码 → "未知错误"
func TestMsgUnknown(t *testing.T) {
	if got := Msg(99999); got != "未知错误" {
		t.Errorf("Msg(99999) = %q, want %q", got, "未知错误")
	}
	if got := Msg(Success); got != "ok" {
		t.Errorf("Msg(Success) = %q, want %q", got, "ok")
	}
}

// ── 3. CodeOf ──────────────────────────────────────────────────────────

// TestCodeOf_Basic 基本取码
func TestCodeOf_Basic(t *testing.T) {
	if got := CodeOf(nil); got != Success {
		t.Errorf("CodeOf(nil) = %d, want %d", got, Success)
	}
	if got := CodeOf(New(Forbidden, "没有权限")); got != Forbidden {
		t.Errorf("CodeOf(*Error) = %d, want %d", got, Forbidden)
	}
	// 普通 error → Internal
	if got := CodeOf(errors.New("boom")); got != Internal {
		t.Errorf("CodeOf(普通 error) = %d, want %d", got, Internal)
	}
}

// TestCodeOf_UnwrapsWrapped 包装过的错误要能取到内层业务码
//
// # M98 修的第二个点
//
// 旧实现用裸类型断言 `err.(*Error)`,**只认最外层**。服务层普遍用
// `fmt.Errorf("...: %w", err)` 包装错误 —— 断言失败 → 一律返 Internal
// → 客户端拿到 500,真正的业务码(比如 20601)在包装层就被吃掉了。
//
// 而 `handler.BaseHandler.Fail` 一直用 errors.As —— 同一个错误,
// 走 Fail 是 403,走 CodeOf 是 500。
func TestCodeOf_UnwrapsWrapped(t *testing.T) {
	inner := New(ApprovalNoAssignee, "审批流程配置有误")
	wrapped := fmt.Errorf("解析第 %d 关审批人失败: %w", 1, inner)
	if got := CodeOf(wrapped); got != ApprovalNoAssignee {
		t.Errorf("CodeOf(被包装的 *Error) = %d, want %d —— 包装层把业务码吃掉了",
			got, ApprovalNoAssignee)
	}
	// 两层包装也要能取到
	double := fmt.Errorf("submit: %w", wrapped)
	if got := CodeOf(double); got != ApprovalNoAssignee {
		t.Errorf("CodeOf(两层包装) = %d, want %d", got, ApprovalNoAssignee)
	}
	// 包装的是普通 error → 仍然是 Internal
	if got := CodeOf(fmt.Errorf("ctx: %w", errors.New("boom"))); got != Internal {
		t.Errorf("CodeOf(包装普通 error) = %d, want %d", got, Internal)
	}
}

// ── 4. 生成文档不能过期 ────────────────────────────────────────────────

// docPath 生成文档的路径(从本包目录算起)
//
// server/internal/pkg/errcode → ../../../../ = 仓库根
const docPath = "../../../../docs/error-codes.md"

// TestEveryCodeInGeneratedDoc docs/error-codes.md 必须与代码同步
//
// # 这条防的是"加了码忘了重跑生成器"
//
// 2026-10-26 M98 发现:生成文档最后一次跑是 **2026-10-19**,而从那天到
// M98 之间又加了 **10 个码**(20600-20603 审批段 + 50500-50701 框架段),
// 文档里**一个都没有**。没有任何测试能发现 —— 因为文档就是生成器产的,
// 而生成器是手工跑的。
//
// 这条断言:每个 const 都要出现在文档里,而且**那一行的 HTTP 列必须等于
// 代码算出来的值**。
func TestEveryCodeInGeneratedDoc(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(docPath))
	if err != nil {
		// 文档不在(例如只拷贝了 server/ 的构建环境)→ 跳过,但要说清楚
		t.Skipf("读不到 %s: %v(在只含 server/ 的环境里属正常)", docPath, err)
	}
	// 解析 "| 码 | 常量名 | 默认文案 | HTTP | 说明 |" 表格
	docHTTP := map[int]int{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| ") {
			continue
		}
		cols := strings.Split(line, "|")
		// ["", " 50100 ", " `NotImplemented` ", " ... ", " 501 ", " ... ", ""]
		if len(cols) < 6 {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(cols[1]))
		if err != nil {
			continue // 表头 / 分隔行
		}
		http, err := strconv.Atoi(strings.TrimSpace(cols[4]))
		if err != nil {
			continue
		}
		docHTTP[code] = http
	}
	if len(docHTTP) == 0 {
		t.Fatalf("%s 里一行都没解析出来 —— 文档格式变了,或文档是空的", docPath)
	}

	for _, c := range parseCodes(t) {
		got, ok := docHTTP[c.Value]
		if !ok {
			t.Errorf("码 %d(%s) 没出现在 %s —— 加了码忘了重跑生成器?"+
				"(cd server && go run ./cmd/gen-errcode-doc)", c.Value, c.Name, docPath)
			continue
		}
		if want := HTTPStatus(c.Value); got != want {
			t.Errorf("码 %d(%s) 在 %s 里写的是 HTTP %d,代码算出来是 %d —— 文档在撒谎",
				c.Value, c.Name, docPath, got, want)
		}
	}
}
