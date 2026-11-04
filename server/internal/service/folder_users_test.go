// M139 用户层 —— 文件夹管理的"外层"
//
// 背景:用户 2026-10-31 报「文件夹管理列表的外层显示不对 ——
// 按文件系统目录格式(`rapid-admin/{uid}/{biz}/{fid}/`),第一层应该是
// 某个用户下面的文件夹,再进去才是这个用户的子文件夹」。
//
// 查下来是**两处**叠在一起:
//
// 1. `FolderService.List` 写死 `Where("user_id = ?", 调用者)` ——
// 超管在文件夹管理页**根本列不出别人的文件夹**;
// 2. 没有"用户层"这个概念 —— 界面上也没有任何地方说明"这一行属于谁"。
//
// ⚠️ **这两种失效都不会报错**:列表照常返回 200,只是内容永远是自己的。
// 所以断言必须落在**"别人的文件夹到底有没有出现在结果里"**上。
//
// ⚠️ **本文件的注释一律不缩进续行** —— Go 1.19 的 gofmt 会把缩进的续行
// 当成"代码块"重排,段落被拦腰截断。新增文件要过 gofmt。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// mkSuperCaller 造一个超管 caller。
//
// ⚠️ **角色码要用 model.RoleSuperAdmin,不能手写字符串** ——
// 手写一遍就多一个"写错了也没人知道"的地方(前端那边的 role code
// 就这么对不上过,见 M103 审计)。
func mkSuperCaller(userID uint64) *Caller {
	return &Caller{
		UserID:     userID,
		IdentityID: userID,
		RoleCode:   model.RoleSuperAdmin,
		Audience:   "admin",
		Username:   "super",
	}
}

// TestFolderList_SuperAdminCanListOtherUsersFolders 特征:
// 超管带 user_id 列别人的文件夹时,**列出来的是那个人的文件夹**。
//
// ⚠️ 反面断言也要有:不传 user_id 时列的还是自己的 ——
// 不然"永远返回全部"也能骗过这条测试。
func TestFolderList_SuperAdminCanListOtherUsersFolders(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	// A 建两个,B 建一个
	_, err := folders.Create(context.Background(), mkFileCaller(uidA), FolderCreateReq{Name: "A的图纸"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), mkFileCaller(uidA), FolderCreateReq{Name: "A的合同"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), mkFileCaller(uidB), FolderCreateReq{Name: "B的资料"})
	require.NoError(t, err)

	super := mkSuperCaller(uidA)

	// 不传 user_id → 自己的(A 的两个)
	mine, err := folders.List(context.Background(), super, FolderListQuery{Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(2), mine.Total, "不传 user_id 应列自己的")
	require.ElementsMatch(t, []string{"A的图纸", "A的合同"}, folderNames(mine))

	// 传 B 的 id → B 的那一个
	other := uint64(uidB)
	theirs, err := folders.List(context.Background(), super,
		FolderListQuery{UserID: &other, Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), theirs.Total, "应只列 B 的文件夹")
	require.Equal(t, []string{"B的资料"}, folderNames(theirs))
}

// TestFolderList_NonSuperAdminCannotListOtherUsers 特征:
// 普通用户带别人的 user_id → **403**,不是"静默列自己的"。
//
// ⚠️ 静默改回自己是最坏的一种:接口返 200,列表也"看起来正常",
// 只是内容不是用户要的 —— 数字和预期对不上,而且没有任何报错。
func TestFolderList_NonSuperAdminCannotListOtherUsers(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	_, err := folders.Create(context.Background(), mkFileCaller(uidB), FolderCreateReq{Name: "B的资料"})
	require.NoError(t, err)

	other := uint64(uidB)
	_, err = folders.List(context.Background(), mkFileCaller(uidA),
		FolderListQuery{UserID: &other, Page: 1, Size: 50})
	require.Error(t, err, "普通用户列别人的文件夹必须被拒")
	assert.Equal(t, errcode.Forbidden, errcode.CodeOf(err))
}

// TestFolderList_CrossUserPathsUseThatUsersTree 特征:
// 跨用户列出的文件夹,**路径按那个用户的祖先链算**,不是空路径。
//
// ⚠️ 这条守的是 loadFolderTree 的 owner —— 骨架如果按调用方读,
// 跨用户时 path 会算成空串(界面上"所在目录"那一列整列消失)。
func TestFolderList_CrossUserPathsUseThatUsersTree(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	parent, err := folders.Create(context.Background(), mkFileCaller(uidB), FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), mkFileCaller(uidB),
		FolderCreateReq{Name: "图纸", ParentID: parent.ID})
	require.NoError(t, err)

	super := mkSuperCaller(uidA)
	other := uint64(uidB)
	res, err := folders.List(context.Background(), super,
		FolderListQuery{UserID: &other, ParentID: &parent.ID, Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total)
	// 根级文件夹的 path 就是它自己的名字;这里查的是子级
	require.Equal(t, "素材/图纸", res.List[0].Path,
		"跨用户时路径必须按那个用户的祖先链算")
}

// TestFolderListUsers_OnlyUsersWithFolders 特征:
// 用户层**只列有文件夹的用户**。
//
// ⚠️ 反面断言:没建过文件夹的用户**不能**出现在列表里 ——
// 把他列出来,点进去是个空空间,用户会以为"文件丢了"。
func TestFolderListUsers_OnlyUsersWithFolders(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	uidC := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	_, err := folders.Create(context.Background(), mkFileCaller(uidA), FolderCreateReq{Name: "A"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), mkFileCaller(uidB), FolderCreateReq{Name: "B"})
	require.NoError(t, err)
	// uidC 一个文件夹都没有

	res, err := folders.ListUsers(context.Background(), mkSuperCaller(uidA),
		FolderUserListQuery{Page: 1, Size: 50})
	require.NoError(t, err)
	require.True(t, res.CrossUser, "超管应能跨用户")
	require.Equal(t, uint64(uidA), res.CurrentUserID)

	got := map[uint64]bool{}
	for _, u := range res.List {
		got[u.UserID] = true
	}
	assert.True(t, got[uint64(uidA)], "有文件夹的用户应在列表里")
	assert.True(t, got[uint64(uidB)], "有文件夹的用户应在列表里")
	assert.False(t, got[uint64(uidC)], "没有文件夹的用户不该出现")
}

// TestFolderListUsers_NonSuperAdminSeesOnlySelf 特征:
// 普通用户的用户层**只有自己一行**,cross_user = false。
//
// ⚠️ 这一条是前端"要不要显示用户层"的判据 —— 普通用户多一层"用户"
// 是纯噪音(而且点进去也是自己)。
func TestFolderListUsers_NonSuperAdminSeesOnlySelf(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	_, err := folders.Create(context.Background(), mkFileCaller(uidA), FolderCreateReq{Name: "A"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), mkFileCaller(uidB), FolderCreateReq{Name: "B"})
	require.NoError(t, err)

	res, err := folders.ListUsers(context.Background(), mkFileCaller(uidA),
		FolderUserListQuery{Page: 1, Size: 50})
	require.NoError(t, err)
	assert.False(t, res.CrossUser, "普通用户不能跨用户")
	require.Len(t, res.List, 1, "普通用户只应看到自己")
	assert.Equal(t, uint64(uidA), res.List[0].UserID)
	assert.Equal(t, 1, res.List[0].FolderCount)
}

// TestFolderListUsers_CountsAndDisplayName 特征:
// 用户层那一行的**文件夹数 / 文件数 / 展示名**都对。
//
// ⚠️ 文件数是从 sys_file 现算的(不是 sys_folder 的冗余列)——
// 冗余列会漂移,而这里是"看全局"的页面,数字必须准。
func TestFolderListUsers_CountsAndDisplayName(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	// 补上昵称和真实姓名,验证展示名的优先级
	require.NoError(t, db.Model(&model.SysUser{}).Where("id = ?", uid).
		Updates(map[string]any{"nickname": "小李", "real_name": "李四"}).Error)

	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	for _, n := range []string{"甲", "乙", "丙"} {
		_, err := folders.Create(context.Background(), mkFileCaller(uid), FolderCreateReq{Name: n})
		require.NoError(t, err)
	}

	res, err := folders.ListUsers(context.Background(), mkFileCaller(uid),
		FolderUserListQuery{Page: 1, Size: 50})
	require.NoError(t, err)
	require.Len(t, res.List, 1)
	assert.Equal(t, 3, res.List[0].FolderCount)
	assert.Equal(t, "李四", res.List[0].DisplayName, "真实姓名优先")
	assert.Equal(t, "小李", res.List[0].Nickname)
}
