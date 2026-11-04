// 通用 DTO —— request/response
// 文档 §7
package model

// PageRequest 通用分页请求
type PageRequest struct {
	Page    int `json:"page" form:"page"`
	Size    int `json:"size" form:"size"`
	OrderBy string `json:"order_by" form:"orderBy"`
	OrderDir string `json:"order_dir" form:"orderDir"`
}

// PageResponse 通用分页响应
type PageResponse struct {
	List  interface{} `json:"list"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Size  int         `json:"size"`
}
