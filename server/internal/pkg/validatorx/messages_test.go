// RegisterMessages 文案覆盖表的测试(借鉴 gvalid 自定义消息层)。
package validatorx

import (
	"sync"
	"testing"
)

func TestRegisterMessages_OverrideApplies(t *testing.T) {
	v := Get()
	t.Cleanup(func() { v.RegisterMessages(nil) })

	v.RegisterMessages(map[string]string{"required": "必填哦"})
	if got := v.ruleLabel("required", ""); got != "必填哦" {
		t.Fatalf("覆盖未生效:got=%q", got)
	}
	// 没覆盖的规则回落默认表
	if got := v.ruleLabel("email", ""); got != "邮箱格式不正确" {
		t.Fatalf("默认表被污染:got=%q", got)
	}
}

func TestRegisterMessages_ParamTemplate(t *testing.T) {
	v := Get()
	t.Cleanup(func() { v.RegisterMessages(nil) })

	v.RegisterMessages(map[string]string{"min": "至少 %s 个字"})
	if got := v.ruleLabel("min", "5"); got != "至少 5 个字" {
		t.Fatalf("模板填参失败:got=%q", got)
	}
	// 值不含 %s 时原样使用(比如把 min 的提示改成定长话术)
	v.RegisterMessages(map[string]string{"min": "太短了,请加长"})
	if got := v.ruleLabel("min", "5"); got != "太短了,请加长" {
		t.Fatalf("无占位符模板应原样输出:got=%q", got)
	}
}

func TestRegisterMessages_OneofParamNormalized(t *testing.T) {
	v := Get()
	t.Cleanup(func() { v.RegisterMessages(nil) })

	v.RegisterMessages(map[string]string{"oneof": "必须是 %s 之一"})
	if got := v.ruleLabel("oneof", "a b c"); got != "必须是 a / b / c 之一" {
		t.Fatalf("oneof 参数未归一化:got=%q", got)
	}
}

func TestRegisterMessages_NilResets(t *testing.T) {
	v := Get()
	t.Cleanup(func() { v.RegisterMessages(nil) })

	v.RegisterMessages(map[string]string{"required": "必填哦"})
	v.RegisterMessages(nil)
	if got := v.ruleLabel("required", ""); got != "不能为空" {
		t.Fatalf("nil 未清空覆盖:got=%q", got)
	}
}

func TestRegisterMessages_ConcurrentSafe(t *testing.T) {
	// 与 -race 一起跑:覆盖写并发 Explain 读路径(ruleLabel 的 RLock)。
	v := Get()
	t.Cleanup(func() { v.RegisterMessages(nil) })

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			v.RegisterMessages(map[string]string{"min": "至少 %s 个字"})
			v.RegisterMessages(nil)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = v.ruleLabel("min", "5")
			_ = v.ruleLabel("required", "")
		}
	}()
	wg.Wait()
}
