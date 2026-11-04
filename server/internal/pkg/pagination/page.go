// Package pagination 分页参数
// 文档 §7.4
package pagination

// 限制
const (
	DefaultPage = 1
	DefaultSize = 20
	MaxSize     = 100 // 防止一次拉太多
)

// PageRequest 分页请求
type PageRequest struct {
	Page int `json:"page" form:"page" query:"page"`
	Size int `json:"size" form:"size" query:"size"`
}

// Normalize 规整 page/size(避免越界)
func (p *PageRequest) Normalize() {
	if p.Page <= 0 {
		p.Page = DefaultPage
	}
	if p.Size <= 0 {
		p.Size = DefaultSize
	}
	if p.Size > MaxSize {
		p.Size = MaxSize
	}
}

// Offset 跳过记录数
//
// ⚠️ **M112(2026-10-29)修正:以前忘了 Normalize 就是负数**
//
//	老实现直接 `(p.Page - 1) * p.Size` —— `Normalize()` 是**另一个方法**,
//	调用方忘了调 ⇒ Page=0 ⇒ Offset = -Size ⇒ SQL `OFFSET -20` 直接报错
//	(而且是在 repository 层炸,报错信息跟"分页参数非法"八竿子打不着)。
//
//	⇒ 现在自己兜底:对副本做一次 Normalize 再算。
//	  (用副本 ⇒ 不改调用方的 PageRequest —— Offset 不该有副作用。)
func (p *PageRequest) Offset() int {
	q := *p
	q.Normalize()
	return (q.Page - 1) * q.Size
}

// PageResponse 分页响应(注:实际响应在 response.PageData 中)
// 这里只定义通用 result
type PageResponse struct {
	List  interface{} `json:"list"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Size  int         `json:"size"`
}
