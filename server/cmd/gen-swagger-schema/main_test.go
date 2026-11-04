package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerate_生成物不漂移 —— 这条网守的是"字段契约不会悄悄过期"
//
// 生成物 internal/api/handler/v1/swagger_dto_gen.go 是**生成出来的**,
// 不是手写的,所以它必须能被重跑出同样的结果:
//
//	改了 handler 绑定的 DTO,却没重跑生成器 ⇒ 这里红。
//
// ⚠️ 逐字比而不是"跑一遍不报错" —— 生成器静默跳过猜不准的 handler
// 也能跑通,但那会让覆盖率悄悄掉下去(那正是 x-schema-coverage 干的事)。
func TestGenerate_生成物不漂移(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("定位模块根失败: %v", err)
	}
	res, err := analyze(root)
	if err != nil {
		t.Fatalf("分析失败: %v", err)
	}
	got, err := render(res)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	wantPath := filepath.Join(root, handlerDir, outName)
	want, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("读生成物失败(%s): %v", wantPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("生成物漂移了:在 server/ 下跑 `go run ./cmd/gen-swagger-schema` 重生成 %s", wantPath)
	}
}

// TestGenerate_识别到关键handler —— 防"生成器整体失灵"这种静默退化
//
// 生成器一个 handler 都识别不到时,spec 里的请求体还是空 object,
// 而且**不报任何错** —— 覆盖率会掉到 0,但没人知道。
// 所以这里钉住几个关键 handler 必须被认出来。
func TestGenerate_识别到关键handler(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("定位模块根失败: %v", err)
	}
	res, err := analyze(root)
	if err != nil {
		t.Fatalf("分析失败: %v", err)
	}
	for _, key := range []string{
		"(*UserHandler).Create",
		"(*RoleHandler).CreateRole",
		"(*AuthHandler).Login",
		"(*AuthHandler).SwitchIdentity",
		"(*ApprovalHandler).CreateApproval",
	} {
		if _, ok := res.dtos[key]; !ok {
			t.Errorf("生成器没认出 %s 的请求体 DTO", key)
		}
	}
	// 识别到的量级:开源口径裁掉业务模块后,handler 包里仍有 59 个
	// 带请求体的 handler;只认出个位数说明分析器退化了
	if len(res.dtos) < 50 {
		t.Errorf("只识别出 %d 个 handler 的 DTO,预期 50+ —— 分析器可能退化了", len(res.dtos))
	}
}

// TestGenerate_字段说明取自行尾注释 —— 说明不能是空串凑数
//
// 生成物里的字段说明是 Swagger UI 里"这个字段是什么意思"的唯一来源。
// 如果它变成空串,UI 上还是一片空白,但没人会发现。
func TestGenerate_字段说明取自行尾注释(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("定位模块根失败: %v", err)
	}
	res, err := analyze(root)
	if err != nil {
		t.Fatalf("分析失败: %v", err)
	}
	if len(res.docs) < 20 {
		t.Errorf("只有 %d 个类型带字段说明,预期 20+ —— 注释搬运逻辑可能退化了", len(res.docs))
	}
	for comp, fields := range res.docs {
		for name, doc := range fields {
			if strings.TrimSpace(doc) == "" {
				t.Errorf("组件 %s 的字段 %s 说明是空串", comp, name)
			}
		}
	}
}

// TestFindModuleRoot_两种cwd都能定位 —— 生成器既可能被 `go run` 跑,
// 也可能被包内测试跑,两种 cwd 差两级
func TestFindModuleRoot_两种cwd都能定位(t *testing.T) {
	root, err := findModuleRoot()
	if err != nil {
		t.Fatalf("定位模块根失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("定位到的目录 %s 里没有 go.mod", root)
	}
	if _, err := os.Stat(filepath.Join(root, handlerDir)); err != nil {
		t.Fatalf("定位到的目录 %s 里没有 %s", root, handlerDir)
	}
}
