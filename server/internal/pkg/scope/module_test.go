// module.go 的单测 —— M155(2026-11-02)
//
// # 这套用例守的是什么
//
// 业务域登记表是"数据范围规则能不能生效"的**唯一真相**:
// 运行时注入的 module 与库里存的 module 必须逐字相同,
// 否则规则静默失配 ⇒ 退化成不过滤 ⇒ 越权。
//
// 所以这里守三件事:
//  1. 登记的 module **原样**保留(不切分、不截断、不规范化)
//  2. 含冒号的 module 完整保留("audit:operation" 不能变成 "audit")
//  3. 读出来的是拷贝,改它不影响登记表

package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterModule_原样保留(t *testing.T) {
	const module = "user"
	ResetModules()
	t.Cleanup(ResetModules)

	RegisterModule(module)
	assert.Equal(t, []string{module}, RegisteredModules())
}

// TestRegisterModule_含冒号的业务域不被切分
//
// ⚠️ **这是 M155 改过一次的语义** ——
// 业务域来自 rbac(module, action) 的第一个参数,而它**可以含冒号**:
// rbac("audit:operation", "list") ⇒ 业务域是 "audit:operation"。
//
// 从资源码("audit:operation:list")反推会得到 "audit" ——
// 那个 module 与运行时注入的对不上 ⇒ 规则静默失配。
func TestRegisterModule_含冒号的业务域不被切分(t *testing.T) {
	ResetModules()
	t.Cleanup(ResetModules)

	RegisterModule("audit:operation")
	RegisterModule("job")
	RegisterModule("dyn")

	assert.Equal(t, []string{"audit:operation", "dyn", "job"}, RegisteredModules())
}

func TestRegisterModule_重复登记只算一次(t *testing.T) {
	ResetModules()
	t.Cleanup(ResetModules)

	RegisterModule("user")
	RegisterModule("user")
	RegisterModule("user")

	assert.Equal(t, []string{"user"}, RegisteredModules())
}

func TestRegisterModule_空串不登记(t *testing.T) {
	ResetModules()
	t.Cleanup(ResetModules)

	// 空串是"默认规则组",它不是一个业务域
	RegisterModule("")
	assert.Empty(t, RegisteredModules())
}

func TestRegisteredModules_返拷贝不是内部map(t *testing.T) {
	ResetModules()
	t.Cleanup(ResetModules)

	RegisterModule("user")
	got := RegisteredModules()
	require.Len(t, got, 1)

	got[0] = "被改坏了"
	assert.Equal(t, []string{"user"}, RegisteredModules(), "改返回值不能影响登记表")
}

func TestResetModules_清空(t *testing.T) {
	RegisterModule("user")
	RegisterModule("role")
	ResetModules()
	assert.Empty(t, RegisteredModules())
}
