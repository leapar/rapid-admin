// FolderService —— 文件夹
// 设计见 docs/文件系统模块设计方案V1.0.md §8.3 / §3.4
package service

import (
	"context"
	"time"
)

// FolderView 文件夹视图
type FolderView struct {
	ID          uint64    `json:"id,string"`
	UserID      uint64    `json:"user_id,string"`
	Biz         string    `json:"biz"`
	Name        string    `json:"name"`
	ParentID    uint64    `json:"parent_id,string"`
	CoverKey    string    `json:"cover_key"`
	CoverFileID uint64    `json:"cover_file_id,string"`
	FileCount   int       `json:"file_count"`
	TotalSize   int64     `json:"total_size"`
	IsDefault   bool      `json:"is_default"`
	Status      int       `json:"status"`
	CreatedAt   time.Time `json:"created_at"`

	// Path 这个文件夹的**完整路径**,形如 "素材/图纸"(**含自身**)。
	//
	// ⚠️ **多级文件夹(M125)之后光有 name 是不够的** —— 同一个用户下
	// 可以有 "A/图纸" 和 "B/图纸",下拉框里只显示 "图纸" 分不清是哪个。
	// 这段路径给下拉框当标签用。
	//
	// ⚠️ **根级文件夹的 Path 就是它自己的名字**(没有前导 "/")——
	//   前端判"是不是根级"要看 ParentID,不要看 Path。
	//
	// ⚠️ **面包屑另用 Ancestors**(根 → 直接上级,不含自己)——
	//   Path 只有一条线,拼不出中间那几级。
	Path string `json:"path"`

	// ChildCount 直接子文件夹数量
	//
	// ⚠️ **只算直接子级**,不含更深层。用途是"进入"按钮旁边显示
	// "有 3 个子文件夹",不是全树的节点数。
	ChildCount int `json:"child_count"`

	// CoverURL 封面直链(能直接塞进 `<img src>`)。空 = 没封面。
	//
	// ⚠️ **这是稳定直链,不是签名 URL** —— 单桶单树模型下每个文件都有
	// 稳定直链(local 是 `/rapid-admin/...`,OSS 是 `https://{bucket}.{endpoint}/{key}`),
	// 签名 URL 过期封面就裂了。
	//
	// ⚠️ **封面文件被删 / 存储没初始化时是空串**,不是报错 ——
	//   封面是锦上添花,不能因为它坏掉整个列表页。
	CoverURL string `json:"cover_url"`
}

// FolderCreateReq 建文件夹
type FolderCreateReq struct {
	// Biz 业务英文名。空 = 用调用方的默认业务段(见 defaultBiz)
	Biz string `json:"biz"`
	// Name 文件夹名
	Name string `json:"name"`
	// ParentID 上级文件夹 id。0 = 建在根级。
	//
	// ⚠️ **必须与调用方同一用户、同一 biz** —— 否则会出现
	// "别人的文件夹里长出自己的子目录"这种跨用户挂靠。
	ParentID uint64 `json:"parent_id,string"`
}

// FolderListQuery 文件夹列表查询
type FolderListQuery struct {
	// Biz 业务段。空 = 不限(跨业务列全部)
	Biz string
	// Keyword 文件夹名模糊搜索。空 = 不限
	//
	// ⚠️ 与文件列表的 Keyword 同一套口径(只匹配 name),别一个查 name
	// 一个查 name+biz —— 同一页两个搜索框,口径不一样会让人以为搜漏了。
	Keyword string
	// ParentID 只列这一级的子文件夹。
	//
	// ⚠️ **三态,别搞混**:
	//   · nil(不传)= **不限层级**(平铺列全部)—— 兼容老调用方(下拉框);
	//   · 指向 0 = **只列根级**;
	//   · 指向某个 id = **只列它的直接子级**。
	//
	// 之所以用指针而不是 int:0 是一个**有意义的取值**(根级),
	// 用 int 分不清"没传"和"传了 0"。
	ParentID *uint64

	// UserID 只列某个用户的文件夹。
	//
	// ⚠️ **nil = 用调用方自己**,不是"所有用户" —— 列表接口的默认口径
	//   永远是"我的",跨用户必须显式点名(设计文档 §10.2)。
	//
	// ⚠️ **只有 super_admin 能指定别人的 id** —— 非超管传了别人的 id 会
	//   **返 403**,不静默改回自己。静默的话前端会以为"筛选生效了",
	//   而返回的其实是自己的数据(界面上的数字和用户预期对不上,而且没报错)。
	UserID *uint64

	Page int
	Size int
}

// FolderDetail 文件夹详情 = 视图 + 文件列表 + info.json 原文
type FolderDetail struct {
	FolderView
	// Files 该文件夹下的文件
	Files []FileView `json:"files"`
	// InfoJSON info.json 的原文。⚠️ 它是**生成物**(§2.3),DB 才是真相源,
	// 所以这里是"顺带返回一份给排障看",前端不该依赖它的字段。
	InfoJSON string `json:"info_json,omitempty"`

	// Ancestors 祖先链,**从根到直接上级**(不含自己)。
	//
	// ⚠️ **前端的面包屑靠它** —— 前端只有"当前文件夹 id",
	// 要显示"素材 / 图纸 / 户型"这条链就得问后端要。
	// 前端自己拼是不可行的:分页列表里未必有祖先。
	Ancestors []FolderView `json:"ancestors"`

	// Children 直接子文件夹。
	//
	// ⚠️ **一次请求就够**:进入一个文件夹时,前端要同时知道
	// "我的上级是谁"(面包屑)和"我下面有什么"(子文件夹列表)。
	// 分成两个接口就得两次往返,而且两次之间数据可能已经变了。
	Children []FolderView `json:"children"`
}

// FolderService 文件夹服务
type FolderService interface {
	// List 文件夹列表
	List(ctx context.Context, caller *Caller, q FolderListQuery) (*FolderPageResult, error)
	// ListUsers 用户层:列出"有文件夹的用户"(见 folder_users.go 的文件头)
	ListUsers(ctx context.Context, caller *Caller, q FolderUserListQuery) (*FolderUserPageResult, error)
	// Create 建文件夹。同名返 Conflict(唯一索引也会兜一层,但这里先判能给出人话错误)
	Create(ctx context.Context, caller *Caller, req FolderCreateReq) (*FolderView, error)
	// Detail 文件夹详情(含文件列表)
	Detail(ctx context.Context, caller *Caller, id uint64) (*FolderDetail, error)
	// Delete 删文件夹(含全部文件与物理对象)
	Delete(ctx context.Context, caller *Caller, id uint64) error
	// SetCover 关联封面:把 folder 里的某个文件设为封面
	SetCover(ctx context.Context, caller *Caller, id, fileID uint64) (*FolderView, error)
	// ensureFolder 取出(或懒创建)目标文件夹 —— UploadToken / Register 内部用。
	//
	// ⚠️ **暴露在接口上是因为 UploadService 也要用**(分片上传要预扣配额、
	// 要把 key 落到同一个文件夹下)。它不是给 handler 调的。
	ensureFolder(ctx context.Context, caller *Caller, folderID uint64, biz, name string) (*FolderView, error)
}

// FolderPageResult 文件夹分页结果
type FolderPageResult struct {
	List  []FolderView `json:"list"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`

	// OwnerName 归属人的展示名(real_name → nickname → username)
	//
	// ⚠️ **放在这一层,不放进 FolderView** —— 同一个列表里所有行的归属人
	//   都是同一个,每行带一遍是冗余(而且行数一多就是 N 倍的传输)。
	//
	// ⚠️ **空串 = 查不到**(用户被删了 / 三个名字字段都是空的)——
	//   前端退回显示 user_id,不该让整个列表 500。
	OwnerName string `json:"owner_name"`
}
