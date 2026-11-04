// M125 多级文件夹 —— parent_id 真正启用 + 封面必须是图片
//
// 背景:用户 2026-10-30 报「文件系统创建文件夹时要能创建多级文件夹,
// 点文件夹能进子文件夹;把文件设为封面必须图片;封面设置完在哪显示」。
//
// ⚠️ **这些测试断言的是"行为变了",不是"接口长了个字段"** ——
//
//	光断言 `child.ParentID == parent.ID` 证明不了"用户能真的建出多级",
//	因为唯一索引那一关(同名文件夹在**不同父级**下必须能共存)才是真正
//	卡住这件事的地方,而它在 DB 层,不在 service 层。
//
// ⚠️ **每一处"这里以前是反的"都写了为什么** —— 因为这些断言最容易被
//
//	"改代码顺手改测试"磨掉,而它们磨掉之后症状是"用户说功能没生效"。
package service

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/oss"
)

// ===== 建子文件夹 =====

// TestFolderCreate_ChildInheritsParentBiz 特征:子文件夹的**业务段继承上级**。
//
// ⚠️ **不继承的后果**:前端建子文件夹时通常不传 biz,不继承的话子文件夹会
//
//	落到默认段(default)里去,而上级在 asset 段 —— 于是用户在上级里**看不到
//	它**,而且不报错(它确实建出来了,只是在另一个目录)。
func TestFolderCreate_ChildInheritsParentBiz(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材", Biz: "asset"})
	require.NoError(t, err)

	child, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: root.ID})
	require.NoError(t, err)
	require.Equal(t, root.ID, child.ParentID, "子文件夹必须挂在上级下")
	require.Equal(t, "asset", child.Biz, "子文件夹必须继承上级的业务段")
}

// TestFolderCreate_ExplicitBizMismatchRejected 特征:显式传了**和上级不一致**
// 的业务段 → 报错,不静默改写。
//
// ⚠️ **静默改写比报错难查**:调用方明确说了"放这儿",程序偷偷放到别处,
// 表现是"建好了但不在那儿",而报错是"你没资格放这儿"。
func TestFolderCreate_ExplicitBizMismatchRejected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材", Biz: "asset"})
	require.NoError(t, err)

	_, err = folders.Create(context.Background(), caller,
		FolderCreateReq{Name: "图纸", Biz: "material", ParentID: root.ID})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
}

// TestFolderCreate_SameNameUnderDifferentParentsAllowed 特征:**不同父级下同名
// 允许**(A/图纸 和 B/图纸 能同时存在)。
//
// ⚠️ **这条是 M125 的核心断言** —— 它由**唯一索引**保证,不是应用层。
//
//	唯一索引从 (user_id, biz, name) 变成 (user_id, biz, parent_id, name);
//	索引不改,建第二个"图纸"就会撞 duplicate key,而报错是"同级下已有同名文件夹"
//	—— 用户看到的是"我明明建在另一个文件夹下面"。
//
// ⚠️ **测试库是用 AutoMigrate 建表的**,所以它建出来的就是新索引 ——
//
//	这条测试因此真的在验索引,不是在验应用层。
func TestFolderCreate_SameNameUnderDifferentParentsAllowed(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	a, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "A"})
	require.NoError(t, err)
	b, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "B"})
	require.NoError(t, err)

	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: a.ID})
	require.NoError(t, err, "A 下的『图纸』必须能建出来")
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: b.ID})
	require.NoError(t, err, "B 下的『图纸』也必须能建出来(同名但不同父级)")
}

// TestFolderCreate_SameNameUnderSameParentRejected 特征:**同一父级下同名 → Conflict**。
//
// ⚠️ 反面断言一起写:不传 ParentID 时同名要能建(根级之间互不冲突),
//
//	否则这条测试会被"根本不许重名"这种实现骗过去。
func TestFolderCreate_SameNameUnderSameParentRejected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: root.ID})
	require.NoError(t, err)

	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: root.ID})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.Conflict), "want Conflict, got %v", err)

	// 反面:**根级之间同名也要撞**(两个根级文件夹的 parent_id 都是 0,
	// 在新索引 (user_id, biz, parent_id, name) 下它们是"同一个父级下的同名")
	// —— 这和 M80 的老行为一致,不是 M125 改出来的。
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.Error(t, err, "根级同名也要撞")
	require.True(t, errCodeIs(err, errcode.Conflict), "want Conflict, got %v", err)
}

// TestFolderCreate_ChildOfOtherUserNotFound 特征:**不能把子文件夹挂到别人的
// 文件夹下** —— 返 404(不是 403)。
//
// ⚠️ 返 404 而不是 403:403 会告诉攻击者"这个 id 确实存在,只是你没权限"。
func TestFolderCreate_ChildOfOtherUserNotFound(t *testing.T) {
	db := newFileTestDB(t)
	uid1 := seedFileUser(t, db, 0)
	uid2 := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	other, err := folders.Create(context.Background(), mkFileCaller(uid2), FolderCreateReq{Name: "别人的"})
	require.NoError(t, err)

	_, err = folders.Create(context.Background(), mkFileCaller(uid1),
		FolderCreateReq{Name: "我的", ParentID: other.ID})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.NotFound), "want NotFound, got %v", err)
}

// TestFolderCreate_DepthLimited 特征:**层级有上限**(maxFolderDepth 层)。
//
// ⚠️ 为什么要有上限:路径字符串每层最多 128 字节,20 层就是 2.5KB ——
//
//	面包屑一行放不下,而且它会出现在每个下拉框的 label 里。
func TestFolderCreate_DepthLimited(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	var parent uint64
	for i := 1; i <= maxFolderDepth; i++ {
		v, err := folders.Create(context.Background(), caller,
			FolderCreateReq{Name: "层" + itoa(uint64(i)), ParentID: parent})
		require.NoErrorf(t, err, "第 %d 层必须能建", i)
		parent = v.ID
	}
	_, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "超了", ParentID: parent})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
}

// TestFolderCreate_NameWithSlashRejected 特征:**文件夹名里不能有 "/"**。
//
// ⚠️ 为什么这条重要:路径("素材/图纸")是用 "/" 拼的,名字里带 "/" 会让路径
//
//	有歧义 —— "a/b" 到底是两级还是一个名字。而前端要靠路径拆出"所在目录"
//	来区分同名的文件夹("A/图纸" 和 "B/图纸"),路径一歧义,界面上就会出现
//	两条一模一样的路径,而且**没有任何地方会报错**。
func TestFolderCreate_NameWithSlashRejected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	_, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "a/b"})
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)

	// 反面:普通名字不受影响
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "a-b_c"})
	require.NoError(t, err, "普通名字必须能建")
}

// ===== 列表 =====
// TestFolderList_ParentIDThreeStates 特征:parent_id 的**三态**。
//
// ⚠️ **三态不能合并成两态** ——
//
//	· nil(不传)= 不限层级(平铺全部)—— 老调用方(下拉框)依赖这个;
//	· 0 = 只列根级;
//	· id = 只列它的直接子级。
//	用 int 而不是 *uint64 的话,"不传"和"传 0"分不开 —— 于是"进入文件夹"
//	和"看全部"会变成同一个请求。
func TestFolderList_ParentIDThreeStates(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: root.ID})
	require.NoError(t, err)

	// 不传 → 全部 2 条
	all, err := folders.List(context.Background(), caller, FolderListQuery{Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(2), all.Total, "不传 parent_id = 不限层级")

	// 传 0 → 只列根级
	zero := uint64(0)
	roots, err := folders.List(context.Background(), caller, FolderListQuery{ParentID: &zero, Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), roots.Total, "parent_id=0 只列根级")
	require.Equal(t, "素材", roots.List[0].Name)

	// 传 id → 只列它的直接子级
	kids, err := folders.List(context.Background(), caller, FolderListQuery{ParentID: &root.ID, Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), kids.Total, "parent_id=id 只列直接子级")
	require.Equal(t, "图纸", kids.List[0].Name)
}

// TestFolderList_PathAndChildCount 特征:列表里带**祖先路径**和**直接子级数**。
//
// ⚠️ 为什么必须有 Path:多级之后同一个用户下可以有 "A/图纸" 和 "B/图纸",
//
//	下拉框里只显示 "图纸" 分不清是哪个。
func TestFolderList_PathAndChildCount(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	a, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "A"})
	require.NoError(t, err)
	b, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "B"})
	require.NoError(t, err)
	leaf, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: a.ID})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "照片", ParentID: leaf.ID})
	require.NoError(t, err)

	res, err := folders.List(context.Background(), caller, FolderListQuery{Page: 1, Size: 50})
	require.NoError(t, err)

	byID := map[uint64]FolderView{}
	for _, f := range res.List {
		byID[f.ID] = f
	}
	require.Equal(t, "A/图纸", byID[leaf.ID].Path, "路径含祖先也含自身")
	require.Equal(t, "A", byID[a.ID].Path, "根级的路径就是它自己的名字")
	require.Equal(t, 1, byID[a.ID].ChildCount, "A 有 1 个直接子级")
	require.Equal(t, 1, byID[leaf.ID].ChildCount)
	require.Equal(t, 0, byID[b.ID].ChildCount, "B 没有子级")
}

// ===== 详情 =====

// TestFolderDetail_AncestorsAndChildren 特征:详情一次返回**祖先链**和**直接子级**。
//
// ⚠️ **祖先链必须由后端给** —— 前端手里只有"当前文件夹 id",
//
//	而分页列表里未必有祖先,前端自己拼不出面包屑。
func TestFolderDetail_AncestorsAndChildren(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	mid, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: root.ID})
	require.NoError(t, err)
	leaf, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "户型", ParentID: mid.ID})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "其它", ParentID: leaf.ID})
	require.NoError(t, err)

	d, err := folders.Detail(context.Background(), caller, leaf.ID)
	require.NoError(t, err)
	require.Len(t, d.Ancestors, 2, "祖先链是 根→直接上级")
	require.Equal(t, "素材", d.Ancestors[0].Name)
	require.Equal(t, "图纸", d.Ancestors[1].Name)
	require.Len(t, d.Children, 1)
	require.Equal(t, "其它", d.Children[0].Name)
	// ⚠️ **空集合不是 nil** —— nil 序列化成 null,前端 .length 会炸
	require.NotNil(t, d.Ancestors)
	require.NotNil(t, d.Children)
}

// TestFolderDetail_EmptyAncestorsNotNil 特征:根级文件夹的祖先链是**空数组**。
func TestFolderDetail_EmptyAncestorsNotNil(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)

	d, err := folders.Detail(context.Background(), caller, root.ID)
	require.NoError(t, err)
	require.NotNil(t, d.Ancestors)
	require.Empty(t, d.Ancestors)
	require.NotNil(t, d.Children)
}

// ===== 删除(整棵子树) =====

// TestFolderDelete_DeletesWholeSubtree 特征:删一个文件夹 = **删掉它整棵子树**
// (行、文件、配额、物理对象一个都不少)。
//
// ⚠️ **只删自己那一层的后果**:子文件夹成了孤儿 —— 父没了、子还在,
//
//	而且再也点不进去(前端按 parent_id 列目录)。用户会以为文件凭空消失了,
//	而且没有任何地方会告诉他"你删的只是上一层"。
func TestFolderDelete_DeletesWholeSubtree(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	folders := NewFolderService(db, prov, opt)
	files := NewFileService(db, prov, opt)
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	child, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: root.ID})
	require.NoError(t, err)
	grand, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "户型", ParentID: child.ID})
	require.NoError(t, err)

	body := []byte("hello")
	for _, fid := range []uint64{root.ID, child.ID, grand.ID} {
		_, err := files.Receive(context.Background(), caller, FileReceiveReq{
			FolderID: fid, Biz: "default", FileName: "a.txt", ContentType: "text/plain",
			Size: int64(len(body)), Body: bytes.NewReader(body),
		})
		require.NoError(t, err)
	}
	require.Equal(t, int64(15), usedBytes(t, db, uid), "前置:三层各 5 字节")

	require.NoError(t, folders.Delete(context.Background(), caller, root.ID))

	// 文件行全没了
	var alive int64
	require.NoError(t, db.Model(&model.SysFile{}).
		Where("folder_id IN ? AND status = ?", []uint64{root.ID, child.ID, grand.ID}, model.FileStatusNormal).
		Count(&alive).Error)
	require.Equal(t, int64(0), alive, "子树的文件必须一起删")

	// 文件夹行全软删了
	var aliveFolders int64
	require.NoError(t, db.Model(&model.SysFolder{}).
		Where("id IN ? AND status = ?", []uint64{root.ID, child.ID, grand.ID}, model.FileStatusNormal).
		Count(&aliveFolders).Error)
	require.Equal(t, int64(0), aliveFolders, "子树的文件夹必须一起删")

	// 配额退回来了
	require.Equal(t, int64(0), usedBytes(t, db, uid), "配额必须全退回来")

	// 物理对象也没了 —— 这一条只能靠列存储看
	require.Empty(t, listAllKeys(t, prov, "res/"), "物理对象必须一起删")
}

// TestFolderDelete_ChildOnlyDoesNotTouchSiblings 特征:删子文件夹**不动兄弟**。
//
// ⚠️ 反面断言一起写:只删自己那一层(不删子树)也会让上面那条测试绿 ——
//
//	所以这里必须证明"没多删"。
func TestFolderDelete_ChildOnlyDoesNotTouchSiblings(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	folders := NewFolderService(db, prov, opt)
	files := NewFileService(db, prov, opt)
	caller := mkFileCaller(uid)

	root, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	child, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "图纸", ParentID: root.ID})
	require.NoError(t, err)
	sibling, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "照片", ParentID: root.ID})
	require.NoError(t, err)

	body := []byte("hi")
	for _, fid := range []uint64{child.ID, sibling.ID} {
		_, err := files.Receive(context.Background(), caller, FileReceiveReq{
			FolderID: fid, Biz: "default", FileName: "a.txt", ContentType: "text/plain",
			Size: int64(len(body)), Body: bytes.NewReader(body),
		})
		require.NoError(t, err)
	}

	require.NoError(t, folders.Delete(context.Background(), caller, child.ID))

	var alive int64
	require.NoError(t, db.Model(&model.SysFile{}).
		Where("folder_id = ? AND status = ?", sibling.ID, model.FileStatusNormal).Count(&alive).Error)
	require.Equal(t, int64(1), alive, "兄弟的文件不能被牵连")
	require.Equal(t, int64(2), usedBytes(t, db, uid), "配额只能退删掉那一份")
}

// ===== 封面必须是图片 =====

// TestFolderSetCover_RejectsNonImage 特征:**只有图片能当封面**。
//
// ⚠️ 以前这里什么都不查 —— 把一个 txt 设成封面也成功,而封面是**当图片渲染**
//
//	的,表现是"封面位置裂图",用户完全不知道是自己选错了文件。
func TestFolderSetCover_RejectsNonImage(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	folders := NewFolderService(db, prov, opt)
	files := NewFileService(db, prov, opt)
	caller := mkFileCaller(uid)

	f, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	txt, err := files.Receive(context.Background(), caller, FileReceiveReq{
		FolderID: f.ID, Biz: "default", FileName: "readme.txt", ContentType: "text/plain",
		Size: 3, Body: bytes.NewReader([]byte("abc")),
	})
	require.NoError(t, err)

	_, err = folders.SetCover(context.Background(), caller, f.ID, txt.ID)
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)

	// ⚠️ **拒了之后不能留下半截状态** —— 文件的 is_cover 必须还是 false
	var isCover bool
	require.NoError(t, db.Model(&model.SysFile{}).Where("id = ?", txt.ID).
		Select("is_cover").Scan(&isCover).Error)
	require.False(t, isCover, "被拒之后不该打上封面标记")
}

// TestFolderSetCover_AcceptsImage 特征:**图片能当封面,而且返回体带封面直链**。
//
// ⚠️ 为什么返回体必须带直链:前端拿到 cover_key 自己拼不出地址
//
//	(key 的形态在 local 和 OSS 下不一样),拼错就是一张裂图。
func TestFolderSetCover_AcceptsImage(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	folders := NewFolderService(db, prov, opt)
	files := NewFileService(db, prov, opt)
	caller := mkFileCaller(uid)

	f, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	png, err := files.Receive(context.Background(), caller, FileReceiveReq{
		FolderID: f.ID, Biz: "default", FileName: "cover.png", ContentType: "image/png",
		Size: 3, Body: bytes.NewReader([]byte("abc")),
	})
	require.NoError(t, err)

	v, err := folders.SetCover(context.Background(), caller, f.ID, png.ID)
	require.NoError(t, err)
	require.Equal(t, png.ID, v.CoverFileID)
	require.NotEmpty(t, v.CoverURL, "封面直链必须给出来")
	require.Contains(t, v.CoverURL, "/"+oss.DefaultRoot+"/", "直链指向静态挂载点, got %q", v.CoverURL)

	// 列表里也要能看到封面(用户问的"设置完在哪显示"就是这里)
	res, err := folders.List(context.Background(), caller, FolderListQuery{Page: 1, Size: 50})
	require.NoError(t, err)
	require.Len(t, res.List, 1)
	require.NotEmpty(t, res.List[0].CoverURL, "列表里也要带封面直链")
}

// TestFolderSetCover_RejectsSVG 特征:**SVG 不能当封面**。
//
// ⚠️ 理由和 handler 的 isInlineType 一样:SVG 里能塞 <script>,而封面会被
//
//	"点开看大图"(新标签页打开直链)—— 那时它是**同源文档**,脚本会跑在
//	本站 origin 下。
func TestFolderSetCover_RejectsSVG(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	folders := NewFolderService(db, prov, opt)
	files := NewFileService(db, prov, opt)
	caller := mkFileCaller(uid)

	f, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	svg, err := files.Receive(context.Background(), caller, FileReceiveReq{
		FolderID: f.ID, Biz: "default", FileName: "logo.svg", ContentType: "image/svg+xml",
		Size: 3, Body: bytes.NewReader([]byte("abc")),
	})
	require.NoError(t, err)

	_, err = folders.SetCover(context.Background(), caller, f.ID, svg.ID)
	require.Error(t, err)
	require.True(t, errCodeIs(err, errcode.InvalidParam), "want InvalidParam, got %v", err)
}
