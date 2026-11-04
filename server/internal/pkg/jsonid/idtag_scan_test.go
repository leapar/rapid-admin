package jsonid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serverRoot 从本包位置回溯到 server/
func serverRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("解析 server 根目录失败: %v", err)
	}
	return root
}

// TestNoBareIntegerIDTags 是本轮 ID 改造的**棘轮**。
//
// 背景:sonyflake 的主键是 19 位数(~9.5e16),超出 JS 的 2^53 安全整数上限,
// JSON.parse 会静默截断,回传后端必然 404(身份切换就死在这里)。所以 wire 上
// 所有 ID 必须是字符串。
//
// 2026-10-21:259 个字段已全部改完,清单随之删除 —— 现在是**零容忍**模式,
// 任何新增的裸整数 ID 直接让测试红。allowlist 机制保留,但只在
// "确实需要临时豁免"时才重建(见下面的 UPDATE_ID_ALLOWLIST)。
//
// 记账规则不变:
//   - 新增违规 → 红
//   - 已改完却还挂在 allowlist 上 → 红(清单不许只增不减)
//   - allowlist 清空后删掉文件,测试继续守着"不许新增"
func TestNoBareIntegerIDTags(t *testing.T) {
	root := serverRoot(t)
	violations, err := ScanDir(filepath.Join(root, "internal"))
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}

	allowPath := filepath.Join(root, "internal", "pkg", "jsonid", AllowlistFile)

	// 重建清单的唯一入口(只在真需要临时豁免时用):
	//   UPDATE_ID_ALLOWLIST=1 go test ./internal/pkg/jsonid/ -run TestNoBareIntegerIDTags
	// 用途是把"当前所有违规"一次性登记下来,让棘轮从"全红"变成"只拦新增"。
	// 之后再改造就**手工删对应行**,不要重新生成 —— 重新生成会让清单永远
	// 只反映当下,失去"不许新增"的约束力。
	if os.Getenv("UPDATE_ID_ALLOWLIST") == "1" {
		if err := os.WriteFile(allowPath, []byte(RenderAllowlist(violations)), 0o644); err != nil {
			t.Fatalf("写 %s 失败: %v", allowPath, err)
		}
		t.Logf("已重新生成 %s,共 %d 条", AllowlistFile, len(violations))
		t.Skip("清单已重新生成,跳过校验")
	}

	allowed, err := LoadAllowlist(allowPath)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", allowPath, err)
	}

	var unlisted, stale []string
	seen := map[string]bool{}

	for _, v := range violations {
		key := v.Key()
		if seen[key] {
			continue // 同一字段在多个文件里出现,只报一次
		}
		seen[key] = true
		if _, ok := allowed[key]; !ok {
			unlisted = append(unlisted, v.String())
		}
	}

	// 反向:清单里已经不成立的行(字段改完了 / 文件被删了 / 名字改了)
	for key := range allowed {
		if !seen[key] {
			stale = append(stale, key)
		}
	}

	if len(stale) > 0 {
		t.Errorf("%s 里有 %d 条已经不成立了 —— 改造完的字段请删掉对应行(清单只减不增):\n  %s",
			AllowlistFile, len(stale), strings.Join(stale, "\n  "))
	}
	if len(unlisted) > 0 {
		t.Errorf("发现 %d 个 ID 以裸整数上 wire(超出 JS 2^53,前端会截断):\n  %s\n\n"+
			"修法:标量在 json tag 后加 ,string(Go 类型不动,service/GORM 零影响);\n"+
			"      切片内置 ,string 无效,类型要换成 jsonid.IDList。\n"+
			"确实需要临时豁免的,用 UPDATE_ID_ALLOWLIST=1 重建 %s 并写清原因。",
			len(unlisted), strings.Join(unlisted, "\n  "), AllowlistFile)
	}
}

// TestScanDirFindsKnownBadTags 证明扫描器本身有效 —— 输入是 testdata/scanfixture,
// 里面把"该抓的"和"不该抓的"都摆好了,逐条断言。
//
// 没有这条,一个永远返回空列表的扫描器也能让 TestNoBareIntegerIDTags 一直绿。
func TestScanDirFindsKnownBadTags(t *testing.T) {
	vs, err := ScanDir("testdata/scanfixture")
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}

	got := map[string]Violation{}
	for _, v := range vs {
		got[v.JSONName+"/"+v.Field] = v
	}

	mustCatch := []string{
		"id/ID",                    // 裸 uint64 主键
		"user_id/UserID",           // 裸 uint64 外键
		"identity_ids/IdentityIDs", // 批量 ID
		"parent_id/ParentID",       // 指针类型
		"created_by/CreatedBy",     // "_by" 后缀也是 ID(操作人)
		"review_by/ReviewBy",
	}
	for _, want := range mustCatch {
		if _, ok := got[want]; !ok {
			t.Errorf("扫描器漏了 %s —— 扫描器失效,棘轮就是假的", want)
		}
	}

	mustNotCatch := map[string]string{
		"name/Name":                    "非 id 字段名",
		"sort/Sort":                    "非 id 字段名",
		"typed_id/Typed":               "已经是 jsonid.ID",
		"typed_ids/TypedIDs":           "已经是 jsonid.IDList",
		"with_string_id/WithStringTag": "标量 ,string 是合法解法",
		"captcha_id/CaptchaID":         "令牌类字段,本来就是字符串",
		"trace_id/TraceID":             "令牌类字段",
		"request_id/RequestID":         "令牌类字段",
		"open_id/OpenID":               "微信类令牌",
		"order_by/OrderBy":             "排序列名,不是 ID",
		"last_changed_by_name/LastChangedName": "展示名,不是 ID",
	}
	for notWant, why := range mustNotCatch {
		if v, ok := got[notWant]; ok {
			t.Errorf("误报 %s(%s),GoType=%s", notWant, why, v.GoType)
		}
	}

	// 切片打了 ,string 也不算数 —— 内置选项对数组无效
	if v := got["identity_ids/IdentityIDs"]; v.Tagged {
		t.Error("切片的 ,string 应被判为无效(标准库只支持标量)")
	}
	if v := got["id/ID"]; v.Tagged {
		t.Error("裸 uint64 不该被判为已打 string")
	}
	// 结构体名要能解出来,否则清单行定位不到人
	for _, key := range mustCatch {
		if got[key].Struct == "(匿名)" {
			t.Errorf("%s 没能解析出所属结构体名,allowlist 行会难用", key)
		}
	}
}

// TestScanDirSkipsTestdata 确认生产扫描不会把测试夹具算成违规
func TestScanDirSkipsTestdata(t *testing.T) {
	vs, err := ScanDir(".")
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	for _, v := range vs {
		if strings.Contains(v.File, "testdata") {
			t.Errorf("testdata 被算成违规了:%s", v.Key())
		}
	}
}
