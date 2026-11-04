// jsonid —— 以字符串形式上 wire 的主键
//
// ## 这个包解决什么问题
//
// 本仓的主键是 sonyflake 生成的 19 位整数。JS 的 Number 只能安全表示
// 2^53(16 位)—— 19 位整数到了前端会**丢精度**。
//
// 于是约定:**所有 ID 在 JSON 上都是字符串**。
//
//	type ID uint64
//	func (id ID) MarshalJSON() ([]byte, error)   // → "97013624807423348"
//	func (id *ID) UnmarshalJSON(b []byte) error  // 只接受字符串
//
// ## 关键点
//
//   - **规范格式是字符串**。UnmarshalJSON 也接受裸数字,那是"改造尚未完成的
//     兄弟接口"的兼容通道 —— 新代码别依赖它
//   - IDList 同样整体字符串化
//   - ScanDir / RenderAllowlist:扫描代码里"该打 ,string 却是 integer"的
//     json tag 违规,并生成 allowlist —— 这是一条**自动化的守卫**
//
// ## 跑法
//
//	go run ./examples/jsonid
package main

import (
	"encoding/json"
	"fmt"

	"rapid-admin/internal/pkg/jsonid"
)

func main() {
	// ── 一、序列化:整数 → 字符串 ────────────────────────────────
	id := jsonid.FromUint(97013624807423348)
	b, _ := json.Marshal(id)
	fmt.Println("=== 序列化 ===")
	fmt.Printf("ID %d → %s\n", id.Uint64(), b)

	// ── 二、反序列化 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 反序列化 ===")
	var got jsonid.ID
	err := json.Unmarshal([]byte(`"97013624807423348"`), &got)
	fmt.Printf("从字符串解 = %d (err=%v)\n", got.Uint64(), err)

	// 裸数字是"改造尚未完成的兄弟接口"的兼容通道,新代码别依赖它
	err = json.Unmarshal([]byte(`97013624807423348`), &got)
	fmt.Printf("从裸数字解 = %d (err=%v)← 兼容通道,规范格式是字符串\n", got.Uint64(), err)

	err = json.Unmarshal([]byte(`"不是数字"`), &got)
	fmt.Printf("从非法串解 = err=%v\n", err)

	// ── 三、IDList ─────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== IDList ===")
	list := jsonid.FromUints([]uint64{1, 2, 3})
	lb, _ := json.Marshal(list)
	fmt.Printf("列表 → %s\n", lb)
	fmt.Printf("转回 uint64 = %v\n", list.Uints())

	// ── 四、在结构体里用 ────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 结构体里用 ===")
	type user struct {
		ID     jsonid.ID     `json:"id"`
		RoleID jsonid.ID     `json:"role_id"`
		Name   string        `json:"name"`
		Tags   jsonid.IDList `json:"tag_ids"`
	}
	u := user{
		ID:     jsonid.FromUint(97013624807423348),
		RoleID: jsonid.FromUint(42),
		Name:   "张伟",
		Tags:   jsonid.FromUints([]uint64{7, 9}),
	}
	ub, _ := json.Marshal(u)
	fmt.Printf("%s\n", ub)

	// ── 五、扫描 json tag 违规 ──────────────────────────────────
	//
	// 这条命令是 CI 里跑的:
	//
	//	go run ./cmd/scan-jsonid -root . -out id_tag_allowlist.txt
	//
	// 扫出"该打 ,string 却是 integer"的地方,生成 allowlist 供人工确认。
	fmt.Println()
	fmt.Println("=== 违规扫描 ===")
	fmt.Println("违规记录的 Key 形如:", jsonid.Violation{
		File: "internal/model/user.go", Line: 42,
		Struct: "SysUser", Field: "ID", JSONName: "id",
		GoType: "uint64", Tagged: false,
	}.Key())
}
