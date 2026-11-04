// pagination —— 分页参数
//
// ## 这个包解决什么问题
//
// 手写分页有三处容易踩的坑,本包把它们收在一起:
//
//   - page/size 越界:page=0、size=100000
//   - Offset 算成负数:`(page-1)*size`,page=0 ⇒ OFFSET -20 ⇒ SQL 直接报错
//     (而且报错信息跟"分页参数非法"八竿子打不着)
//   - 忘了调 Normalize ⇒ 负 Offset ⇒ 在 repository 层炸
//
// ## 关键点
//
// MaxSize = 100 —— 防止一次拉太多把 DB 拖垮。
// ⚠️ 后端静默截断时**前端必须知道**:被截到 100 却仍请求 1000,
// 前端会以为"还有更多",一直翻页翻到底。
//
// ## 跑法
//
//	go run ./examples/pagination
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/pagination"
)

func main() {
	// 一、规整:page/size 越界都会被拉回合法区间
	cases := []pagination.PageRequest{
		{Page: 1, Size: 20},
		{Page: 0, Size: 20},   // page 越界
		{Page: -3, Size: 20},  // page 是负数
		{Page: 1, Size: 0},    // size 没填
		{Page: 1, Size: 5000}, // size 超上限
	}
	fmt.Println("规整前 → 规整后:")
	for _, c := range cases {
		orig := c
		c.Normalize()
		fmt.Printf("  page=%-4d size=%-5d  →  page=%-4d size=%-5d offset=%d\n",
			orig.Page, orig.Size, c.Page, c.Size, c.Offset())
	}

	// 二、Offset 自己兜底 —— 忘了调 Normalize 也不会算出负数
	//
	// ⚠️ Offset 对副本做 Normalize,**不改调用方的 PageRequest** ——
	// 一个"取值"方法不该有副作用。
	p := pagination.PageRequest{Page: 0, Size: 20}
	fmt.Println()
	fmt.Printf("没调 Normalize 的 PageRequest{0,20}: Offset = %d(不是 -20)\n", p.Offset())
	fmt.Printf("调用方手里的结构体没被改: page=%d size=%d\n", p.Page, p.Size)

	// 三、响应结构:列表 + 总数 + 当前页
	//
	// ⚠️ 实际 HTTP 响应体用的是 response.PageData(包了一层 code/message),
	// 这里的 PageResponse 只是通用 result 类型。
	resp := pagination.PageResponse{
		List:  []string{"第一条", "第二条"},
		Total: 137,
		Page:  2,
		Size:  20,
	}
	fmt.Println()
	fmt.Printf("响应: %+v\n", resp)
}
