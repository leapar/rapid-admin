// 用户层 —— 文件夹管理的"外层"
//
// ⚠️ **为什么文件夹管理要有"用户层"**
//
// 存储侧的目录形态是 `rapid-admin/{uid}/{biz}/{fid}/`(设计文档 §3.4),
// **第一段就是用户**。而文件夹管理页原来直接把"调用者自己的根级文件夹"
// 当成最外层 —— 于是:
//
// · 超管看到的是**他自己的**文件夹,别人的文件夹根本列不出来
// (service 层写死了 `user_id = 调用者`);
// · 界面上没有任何地方说明"这一行属于谁"。
//
// 用户层就是把那第一段显式化:**先选用户 → 看这个用户的文件夹 → 再进子文件夹**。
//
// ⚠️ **用户层只对 super_admin 可见** —— 设计文档 §10.2 规定超管可跨用户
// **读**,普通用户只能看自己的。判定在后端(`callerCanCrossUserRead`),
// **前端不自己猜角色**(前端那份 role code 和后端对不上过,见 M103 审计)。
//
// ⚠️ **用户层是"只读"的** —— 它列的是"谁有文件夹",不提供任何写动作。
// 别人的空间里建文件夹 / 设封面 / 删除都**必须**被拒(§10.2:只能读不能改),
// 而写路径的拒绝是在 service 层做的(loadFolderForWrite → callerOwns),
// 不是靠前端把按钮藏起来。
//
// ⚠️ **本文件的注释一律不缩进续行** —— Go 1.19 的 gofmt 会把缩进的续行
// 当成"代码块"重排(多一行空行 + 一个 tab),段落被拦腰截断。
// 仓库里另外 150 多个文件还是老写法(基线不管),但**新增文件要过 gofmt**。
package service

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// FolderUserView 用户层的一行 —— 一个"有文件夹的用户"。
type FolderUserView struct {
	// UserID 用户 id
	UserID uint64 `json:"user_id,string"`
	// Username 登录名
	Username string `json:"username"`
	// Nickname 昵称(可能为空)
	Nickname string `json:"nickname"`
	// RealName 真实姓名(可能为空)
	RealName string `json:"real_name"`
	// DisplayName 展示名:real_name → nickname → username
	//
	// ⚠️ **后端算好,前端别自己拼** —— 三个字段的优先级一旦在前端写一遍,
	// 后端再改优先级就会出现"两边显示不同的名字"。
	DisplayName string `json:"display_name"`
	// FolderCount 这个用户名下的文件夹数
	FolderCount int `json:"folder_count"`
	// FileCount 这个用户名下的文件数
	FileCount int `json:"file_count"`
	// TotalSize 这个用户名下的文件总字节
	TotalSize int64 `json:"total_size"`
}

// FolderUserListQuery 用户层查询
type FolderUserListQuery struct {
	// Keyword 按用户名 / 昵称 / 真实姓名模糊搜
	//
	// ⚠️ **搜的是"人",不是"文件夹"** —— 用户层搜文件夹名是搞混了两层的语义
	// (文件夹名要进到某个用户空间里才搜得到)。
	Keyword string
	Page    int
	Size    int
}

// FolderUserPageResult 用户层分页结果
type FolderUserPageResult struct {
	List  []FolderUserView `json:"list"`
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Size  int              `json:"size"`

	// CrossUser 调用者能不能跨用户读(super_admin = true)
	//
	// ⚠️ **前端据此决定要不要显示用户层** —— 普通用户直接进自己的文件夹,
	// 多一层"用户"对他是纯噪音。
	CrossUser bool `json:"cross_user"`

	// CurrentUserID 调用者自己的 id
	//
	// ⚠️ **前端据此判断"当前看的空间是不是我的"** —— 别人的空间**不能写**
	// (建文件夹 / 设封面 / 删除)。前端把按钮藏起来只是体验,
	// 真正的拦截在后端(loadFolderForWrite → callerOwns)。
	CurrentUserID uint64 `json:"current_user_id,string"`
}

// ListUsers 用户层:列出"有文件夹的用户"。
//
// ⚠️ **超管看的是"有文件夹的所有人",不是"所有注册用户"** —— 用户层是
// 文件系统的那一层,列出几千个没有文件的账号只是噪音。
func (s *folderService) ListUsers(ctx context.Context, caller *Caller, q FolderUserListQuery) (*FolderUserPageResult, error) {
	uid, err := requireUser(caller)
	if err != nil {
		return nil, err
	}
	page, size := normPage(q.Page, q.Size, 200)
	cross := callerCanCrossUserRead(caller)
	kw := strings.TrimSpace(q.Keyword)

	// ⚠️ **每次都重建查询** —— Count 和 Find 共用同一个 *gorm.DB 时,
	// Count 会把语句改掉(去掉 ORDER BY 之类),第二次用就变了。
	build := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Model(&model.SysUser{})
		if !cross {
			// 普通用户:只有自己
			tx = tx.Where("id = ?", uid)
		} else {
			// 超管:只列**有文件夹**的人
			// ⚠️ 子查询里的 status 过滤是必须的 —— 软删掉的文件夹不算,
			// 否则删干净的用户还会挂在列表里(点进去是空的)。
			tx = tx.Where("id IN (?)", s.db.Model(&model.SysFolder{}).
				Select("DISTINCT user_id").
				Where("status = ?", model.FileStatusNormal))
		}
		if kw != "" {
			like := "%" + kw + "%"
			tx = tx.Where("username LIKE ? OR nickname LIKE ? OR real_name LIKE ?", like, like, like)
		}
		return tx
	}

	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户数失败").WithDetail(err.Error())
	}
	var users []model.SysUser
	if err := build().Order("id ASC").
		Offset((page - 1) * size).Limit(size).Find(&users).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}

	ids := make([]uint64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	folderCounts, fileCounts, totalSizes, err := s.aggregateUserFiles(ctx, ids)
	if err != nil {
		return nil, err
	}

	out := make([]FolderUserView, 0, len(users))
	for _, u := range users {
		out = append(out, FolderUserView{
			UserID:      u.ID,
			Username:    u.Username,
			Nickname:    u.Nickname,
			RealName:    u.RealName,
			DisplayName: folderUserDisplayName(u),
			FolderCount: folderCounts[u.ID],
			FileCount:   fileCounts[u.ID],
			TotalSize:   totalSizes[u.ID],
		})
	}
	return &FolderUserPageResult{
		List:          out,
		Total:         total,
		Page:          page,
		Size:          size,
		CrossUser:     cross,
		CurrentUserID: uid,
	}, nil
}

// aggregateUserFiles 统计一批用户的文件夹数 / 文件数 / 总字节。
//
// ⚠️ **文件数和字节数从 sys_file 现算,不用 sys_folder 上的冗余列** ——
// 那两列是会漂移的(设计文档 §4.1 明说"冗余就意味着会漂移"),
// 用户层是"看全局"的页面,数字必须准。
//
// ⚠️ **统计失败就返错,不吞** —— 吞掉的话界面上会显示"0 个文件",
// 而用户点进去明明有文件。**错值比报错难查**(用户会以为文件丢了)。
//
// ⚠️ **返回的 map 里没有的 key = 0** —— 用户层不能因为"这个人一个文件都没有"
// 就少一行(文件夹是有的,只是空的)。
func (s *folderService) aggregateUserFiles(ctx context.Context, ids []uint64) (folderCounts, fileCounts map[uint64]int, totalSizes map[uint64]int64, err error) {
	folderCounts = map[uint64]int{}
	fileCounts = map[uint64]int{}
	totalSizes = map[uint64]int64{}
	if len(ids) == 0 {
		return folderCounts, fileCounts, totalSizes, nil
	}

	type folderAgg struct {
		UserID      uint64
		FolderCount int
	}
	var fAggs []folderAgg
	if err := s.db.WithContext(ctx).Model(&model.SysFolder{}).
		Select("user_id, COUNT(*) AS folder_count").
		Where("status = ?", model.FileStatusNormal).
		Where("user_id IN ?", ids).
		Group("user_id").Scan(&fAggs).Error; err != nil {
		return nil, nil, nil, errcode.New(errcode.Internal, "统计文件夹数失败").WithDetail(err.Error())
	}
	for _, a := range fAggs {
		folderCounts[a.UserID] = a.FolderCount
	}

	type fileAgg struct {
		UserID    uint64
		FileCount int
		TotalSize int64
	}
	var fileAggs []fileAgg
	if err := s.db.WithContext(ctx).Model(&model.SysFile{}).
		Select("user_id, COUNT(*) AS file_count, COALESCE(SUM(file_size), 0) AS total_size").
		Where("status = ?", model.FileStatusNormal).
		Where("user_id IN ?", ids).
		Group("user_id").Scan(&fileAggs).Error; err != nil {
		return nil, nil, nil, errcode.New(errcode.Internal, "统计文件数失败").WithDetail(err.Error())
	}
	for _, a := range fileAggs {
		fileCounts[a.UserID] = a.FileCount
		totalSizes[a.UserID] = a.TotalSize
	}
	return folderCounts, fileCounts, totalSizes, nil
}

// folderUserDisplayName 展示名:real_name → nickname → username。
//
// ⚠️ **三级兜底** —— 真实姓名和昵称都可能为空(注册时只填了登录名),
// 直接用 real_name 会显示成空白格。
func folderUserDisplayName(u model.SysUser) string {
	if s := strings.TrimSpace(u.RealName); s != "" {
		return s
	}
	if s := strings.TrimSpace(u.Nickname); s != "" {
		return s
	}
	return u.Username
}
