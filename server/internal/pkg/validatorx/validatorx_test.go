package validatorx

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/go-playground/validator/v10"
)

// ── 本文件的来历 ───────────────────────────────────────────────────────
//
// 这个包之前**一张测试都没有**,而它是 §1.11 #6「统一参数校验」的落点。
//
// 它有 2 条真 bug(2026-10-29 M112 修的):
//
//	P1 Explain 用类型断言而不是 errors.As ⇒ 任何一层 %w 包装都会让
//	   **字段名整个丢掉**,用户只看到"参数校验失败",不知道是哪个字段错了
//	P1 RegisterBuiltins 把注册错误直接丢掉(`_ = v.RegisterCustom(...)`)
//	   而且注释写的是"重复注册会 panic" —— 实际 go-playground 返回的是
//	   **错误**,不会 panic ⇒ 那条防护从来不存在,而失败是**静默**的:
//	   `validate:"cn_phone"` 会变成"未知规则",校验时报
//	   "Undefined validation function",没人知道为什么。
//
// ── M112 可逆探针记录 ───────────────────────────────────────────────────
//
//	拆法                                       必须变红
//	──────────────────────────────────────  ─────────────────────────────────
//	Explain 退回类型断言                        TestM112_Explain_UnwrapsWrappedError
//	Explain 认不出字段时也拼 ": "               TestM112_Explain_NoField_NoColon
//	RegisterBuiltins 吞掉注册错误               TestM112_RegisterBuiltins_ReportsError
//	RegisterBuiltins 重复调用 panic              TestM112_RegisterBuiltins_RepeatNoPanic
//	内置规则没真正注册                          TestM112_Builtins_CnPhoneWorks
// ───────────────────────────────────────────────────────────────────────

type validDTO struct {
	Username string `json:"username" validate:"required,min=3,max=32"`
	Email    string `json:"email" validate:"omitempty,email"`
	Age      int    `json:"age" validate:"omitempty,gte=0,lte=150"`
	Role     string `json:"role" validate:"required,oneof=admin operator"`
}

type noTagDTO struct {
	Plain string `validate:"required"`
}

type dashTagDTO struct {
	Secret string `json:"-" validate:"required"`
}

type omitemptyTagDTO struct {
	Nick string `json:"nick,omitempty" validate:"omitempty,min=2"`
}

// ── 单例 ────────────────────────────────────────────────────────────────

func TestM112_Get_Singleton(t *testing.T) {
	a := Get()
	b := Get()
	if a != b {
		t.Fatal("Get 必须返回单例")
	}
}

// ── 基本校验 ────────────────────────────────────────────────────────────

func TestM112_Struct_Pass(t *testing.T) {
	dto := validDTO{Username: "zhangsan", Email: "a@b.com", Age: 18, Role: "admin"}
	if err := Get().Struct(dto); err != nil {
		t.Fatalf("合法 DTO 不该报错,实际 %v", err)
	}
}

func TestM112_Struct_Fail(t *testing.T) {
	dto := validDTO{Username: "ab", Role: "root"}
	if err := Get().Struct(dto); err == nil {
		t.Fatal("非法 DTO 应该报错")
	}
}

// ── Explain ─────────────────────────────────────────────────────────────

// TestM112_Explain_UsesJSONTag 字段名要用 json tag
func TestM112_Explain_UsesJSONTag(t *testing.T) {
	dto := validDTO{Username: "ab", Role: "admin"}
	err := Get().Struct(dto)
	field, msg := Get().Explain(err)
	if field != "username" {
		t.Fatalf("字段名应为 json tag 的 username,实际 %q", field)
	}
	if msg == "" {
		t.Fatal("消息不该为空")
	}
}

// TestM112_Explain_UnwrapsWrappedError ⚠️ 核心回归:穿透 %w 包装
//
// 旧实现用 `err.(validator.ValidationErrors)` 类型断言 ——
// 只要中间隔了一层 fmt.Errorf("...: %w", err),断言就失配,
// **字段名整个丢掉**,用户只看到"参数校验失败"。
func TestM112_Explain_UnwrapsWrappedError(t *testing.T) {
	dto := validDTO{Username: "ab", Role: "admin"}
	raw := Get().Struct(dto)
	if raw == nil {
		t.Fatal("前置条件不满足:DTO 应该校验失败")
	}

	// 模拟真实链路:service 层包一层 %w 之后再交给 handler
	wrapped := fmt.Errorf("校验 DTO 失败: %w", raw)

	field, msg := Get().Explain(wrapped)
	if field != "username" {
		t.Fatalf("穿透包装后仍应拿到字段名 username,实际 %q", field)
	}
	if msg == "" {
		t.Fatal("穿透包装后消息不该为空")
	}

	// 再包两层
	wrapped2 := fmt.Errorf("外层: %w", wrapped)
	field2, _ := Get().Explain(wrapped2)
	if field2 != "username" {
		t.Fatalf("穿透两层包装后字段名丢了,实际 %q", field2)
	}
}

// TestM112_Explain_NoField_NoColon 认不出字段时不要拼 ": "
//
// ⚠️ handler 侧已经配套改成"field 为空就不拼冒号"
//
//	(旧实现无条件 `field + ": " + msg` ⇒ 消息变成 "": 参数校验失败""),
//	这里锁住"field 为空 ⇒ message 是干净的一句"。
func TestM112_Explain_NoField_NoColon(t *testing.T) {
	field, msg := Get().Explain(errors.New("完全不是校验错误"))
	if field != "" {
		t.Fatalf("非校验错误不该有字段名,实际 %q", field)
	}
	if msg != "参数校验失败" {
		t.Fatalf("消息应为「参数校验失败」,实际 %q", msg)
	}
	if msg == ": " || len(msg) < 4 {
		t.Fatalf("消息不该带孤零零的冒号,实际 %q", msg)
	}
}

// TestM112_Explain_EmptyValidationErrors 空列表也不能 panic
func TestM112_Explain_EmptyValidationErrors(t *testing.T) {
	field, msg := Get().Explain(validator.ValidationErrors{})
	if field != "" || msg != "参数校验失败" {
		t.Fatalf("空校验错误应返回兜底文案,实际 %q / %q", field, msg)
	}
}

// ── 字段名规则 ──────────────────────────────────────────────────────────

func TestM112_Explain_NoJSONTag_UsesGoName(t *testing.T) {
	err := Get().Struct(noTagDTO{})
	field, _ := Get().Explain(err)
	if field != "Plain" {
		t.Fatalf("没有 json tag 时应回落到 Go 字段名 Plain,实际 %q", field)
	}
}

func TestM112_Explain_DashJSONTag_UsesGoName(t *testing.T) {
	err := Get().Struct(dashTagDTO{})
	field, _ := Get().Explain(err)
	if field != "Secret" {
		t.Fatalf(`json:"-" 时应回落到 Go 字段名 Secret,实际 %q`, field)
	}
}

func TestM112_Explain_OmitemptyJSONTag(t *testing.T) {
	err := Get().Struct(omitemptyTagDTO{Nick: "a"})
	field, _ := Get().Explain(err)
	if field != "nick" {
		t.Fatalf("带 omitempty 的 json tag 也应取 tag 名,实际 %q", field)
	}
}

// ── 规则文案 ────────────────────────────────────────────────────────────

func TestM112_RuleLabel(t *testing.T) {
	cases := []struct {
		rule, param, want string
	}{
		{"required", "", "不能为空"},
		{"min", "3", "长度不能小于 3"},
		{"max", "32", "长度不能大于 32"},
		{"len", "10", "长度必须为 10"},
		{"gte", "0", "不能小于 0"},
		{"gt", "0", "必须大于 0"},
		{"lte", "150", "不能大于 150"},
		{"lt", "150", "必须小于 150"},
		{"email", "", "邮箱格式不正确"},
		{"url", "", "URL 格式不正确"},
		{"uuid", "", "UUID 格式不正确"},
		{"oneof", "admin operator", "必须是 admin / operator 之一"},
		{"numeric", "", "必须是数字"},
		{"alpha", "", "只能是字母"},
		{"alphanum", "", "只能是字母和数字"},
		{"cn_phone", "", "手机号格式不正确(11 位,1 开头)"},
		{"phone", "", "手机号格式不正确(11 位,1 开头)"},
		{"idcard", "", "身份证号格式不正确"},
	}
	for _, c := range cases {
		if got := ruleLabel(c.rule, c.param); got != c.want {
			t.Fatalf("ruleLabel(%q,%q) = %q,期望 %q", c.rule, c.param, got, c.want)
		}
	}
}

func TestM112_RuleLabel_Unknown(t *testing.T) {
	if got := ruleLabel("myrule", ""); got != "不符合 myrule 规则" {
		t.Fatalf("未知规则(无参)文案不对,实际 %q", got)
	}
	if got := ruleLabel("myrule", "x"); got != "不符合 myrule(x) 规则" {
		t.Fatalf("未知规则(有参)文案不对,实际 %q", got)
	}
}

// ── 内置规则注册 ────────────────────────────────────────────────────────

type phoneDTO struct {
	Phone string `json:"phone" validate:"cn_phone"`
}

type idcardDTO struct {
	ID string `json:"id" validate:"idcard"`
}

// TestM112_Builtins_CnPhoneWorks 内置规则必须真的生效
func TestM112_Builtins_CnPhoneWorks(t *testing.T) {
	if err := RegisterBuiltins(); err != nil {
		t.Fatalf("内置规则注册不该失败:%v", err)
	}

	if err := Get().Struct(phoneDTO{Phone: "13812345678"}); err != nil {
		t.Fatalf("合法手机号不该报错:%v", err)
	}
	if err := Get().Struct(phoneDTO{Phone: "12345"}); err == nil {
		t.Fatal("非法手机号应该报错")
	}
	if err := Get().Struct(idcardDTO{ID: "110101199001011234"}); err != nil {
		t.Fatalf("合法身份证不该报错:%v", err)
	}
	if err := Get().Struct(idcardDTO{ID: "1101011990010112"}); err == nil {
		t.Fatal("非法身份证应该报错")
	}
}

// TestM112_RegisterBuiltins_RepeatNoPanic ⚠️ 核心回归:重复注册不 panic
//
// ⚠️ 老注释写"重复注册同名 tag 会 panic("tag already exists")" ——
//
//	实测(v10.30.5)**既不 panic 也不报错**,只是直接覆盖。
//	真正会 panic 的是注册**受限 tag**(见下面那条测试)。
//	这条测试锁的是"重复调用是安全的" —— main.go 与业务模块的 init()
//	都有可能各调一次,不该炸。
func TestM112_RegisterBuiltins_RepeatNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("重复注册不该 panic,实际 panic:%v", r)
		}
	}()
	for i := 0; i < 3; i++ {
		if err := RegisterBuiltins(); err != nil {
			t.Fatalf("第 %d 次注册不该报错:%v", i+1, err)
		}
	}
	// 注册完之后规则依然可用(没被覆盖坏)
	if err := Get().Struct(phoneDTO{Phone: "13812345678"}); err != nil {
		t.Fatalf("重复注册后规则失效了:%v", err)
	}
}

// TestM112_RegisterCustom_RestrictedTagPanics 受限 tag 会 panic(真实存在的护栏)
//
// 这条不是"我们希望它 panic",而是**记录实测行为**:
// 业务模块如果拿 "required" / "dive" / "omitempty" 这种内置保留字当自定义
// tag 名,库会直接 panic。写进测试是为了让下一个人踩到时知道这是库的规则,
// 而不是以为自己写错了宇宙。
func TestM112_RegisterCustom_RestrictedTagPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("注册受限 tag(required)应该 panic")
		}
	}()
	_ = Get().RegisterCustom("required", func(fl validator.FieldLevel) bool { return true })
}

// TestM112_RegisterCustom_SurfacesError 注册错误必须能被调用方看到
//
// ⚠️ 旧实现是 `_ = v.RegisterCustom(...)` —— 错误直接丢掉。
//
//	业务模块传错 tag 名时就是**静默失败**:规则没注册 ⇒
//	DTO 上的 `validate:"xxx"` 变成"未知规则" ⇒
//	校验时报 "Undefined validation function",没人知道为什么。
func TestM112_RegisterCustom_SurfacesError(t *testing.T) {
	// 空 tag 名 ⇒ 库返错
	err := Get().RegisterCustom("", func(fl validator.FieldLevel) bool { return true })
	if err == nil {
		t.Fatal("空 tag 名必须返回错误(而不是被静默丢掉)")
	}
	// 空函数 ⇒ 库返错
	err = Get().RegisterCustom("m112_empty_fn", nil)
	if err == nil {
		t.Fatal("空函数必须返回错误(而不是被静默丢掉)")
	}
}

// TestM112_RegisterCustom_CustomRule 自定义规则能注册并生效
func TestM112_RegisterCustom_CustomRule(t *testing.T) {
	type customDTO struct {
		Code string `json:"code" validate:"m112_test_rule"`
	}
	if err := Get().RegisterCustom("m112_test_rule", func(fl validator.FieldLevel) bool {
		return fl.Field().String() == "OK"
	}); err != nil {
		t.Fatalf("注册自定义规则失败:%v", err)
	}
	if err := Get().Struct(customDTO{Code: "OK"}); err != nil {
		t.Fatalf("合法值不该报错:%v", err)
	}
	if err := Get().Struct(customDTO{Code: "NO"}); err == nil {
		t.Fatal("非法值应该报错")
	}
}

// TestRegisterCustom_ConcurrentWithStruct 运行期并发注册 + 并发校验不得数据竞争。
//
// v10.30.5 的 validations map 读写都没有锁(cache.go:289 读 / 注册处裸写),
// 并发 RegisterCustom + Struct 会撞 "concurrent map read and map write"
// fatal 整个进程 —— 本条由 `go test -race` 守护(审计报告 validatorx #1)。
// 读者/写者都拿同一个已注册 tag(写者重复注册覆盖),返回值不关心。
func TestRegisterCustom_ConcurrentWithStruct(t *testing.T) {
	v := Get()
	type raceDTO struct {
		Code string `json:"code" validate:"vx_concurrent_rule"`
	}
	// 先注册一次:validator 在构建字段缓存时对"未注册 tag"会直接 panic
	// (cache.go:311),所以并发段必须是"重复注册(库语义:覆盖)"而不是首次注册
	if err := v.RegisterCustom("vx_concurrent_rule", func(fl validator.FieldLevel) bool { return true }); err != nil {
		t.Fatalf("前置注册失败:%v", err)
	}
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = v.Struct(raceDTO{Code: "x"})
				}
			}
		}()
	}
	var writers sync.WaitGroup
	for i := 0; i < 32; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 20; j++ {
				_ = v.RegisterCustom("vx_concurrent_rule", func(fl validator.FieldLevel) bool { return true })
			}
		}()
	}
	writers.Wait()
	close(stop)
	readers.Wait()

	// 并发注册结束后规则必须真实可用
	if err := v.Struct(raceDTO{Code: "OK"}); err != nil {
		t.Fatalf("并发注册后规则应可用:%v", err)
	}
}
