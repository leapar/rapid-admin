// M140 —— 文件夹自愈(头像文件夹改名 + 每个文件夹都有 info.json)
//
// 背景:用户 2026-10-31 报「用户的头像文件夹名称也叫默认文件夹,而且用户头像
// 文件夹里面没有 info.json 这个文件,按道理每个文件夹里面应该都有这个
// info.json 文件」。
//
// ⚠️ **两条失效都不会报错**:文件夹照样建得出来、文件照样传得进去、
// 列表照样有数据。区别只在**存储里的物理布局**和**界面上的名字**。
// 所以断言必须落在"存储里到底有没有那一层 / 那一行叫什么"上。
//
// ⚠️ **本文件的注释一律不缩进续行** —— Go 1.19 的 gofmt 会把缩进的续行
// 当成"代码块"重排,段落被拦腰截断。新增文件要过 gofmt。
package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/oss"
	"rapid-admin/internal/pkg/oss/local"
	"rapid-admin/internal/pkg/sonyflake"
)

// folderInfoPresent 这个文件夹在存储里有没有 info.json。
//
// ⚠️ **只看"有没有"** —— info.json 的**内容**是生成物,内容对不对由
// writeFolderInfo 负责(它按 DB 现状重建)。这里守的是"这一份文件在不在"。
func folderInfoPresent(t *testing.T, p *local.Provider, opt FileOptions, uid uint64, biz string, fid uint64) bool {
	t.Helper()
	key, err := infoKey(opt.Root, uid, biz, fid)
	require.NoError(t, err)
	_, err = p.Stat(context.Background(), key)
	if err == nil {
		return true
	}
	require.True(t, oss.IsNotFound(err), "stat 的错必须是 not found,实际:%v", err)
	return false
}

// seedLegacyAvatarFolder 造一个 M140 之前的历史行:头像段、名字叫"默认文件夹"。
//
// ⚠️ **必须照抄老代码写出来的样子** —— 老代码是
// `IsDefault: name == defaultFolderName` ⇒ 头像文件夹当时**也**被标成了
// "默认文件夹"。照抄才不会写出一个"历史上根本不存在"的探针。
func seedLegacyAvatarFolder(t *testing.T, db *gorm.DB, uid uint64) model.SysFolder {
	t.Helper()
	row := model.SysFolder{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    uid,
		Biz:       avatarBiz,
		Name:      defaultFolderName,
		ParentID:  0,
		IsDefault: true,
	}
	require.NoError(t, db.Create(&row).Error)
	return row
}

// TestEnsureFolder_AvatarSegmentHasDedicatedName 特征:
// 头像段懒建出来的文件夹叫"头像",不叫"默认文件夹"。
//
// ⚠️ 反面断言也要有:默认段**仍然是**"默认文件夹" ——
// 两段共用一个名字的话,用户在文件夹管理页看到两个"默认文件夹",
// 分不出哪个是头像(这正是用户报的那件事)。
func TestEnsureFolder_AvatarSegmentHasDedicatedName(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt).(*folderService)
	caller := mkFileCaller(uid)

	av, err := folders.ensureFolder(context.Background(), caller, 0, avatarBiz, "")
	require.NoError(t, err)
	assert.Equal(t, avatarFolderName, av.Name, "头像文件夹必须有自己的名字")
	assert.NotEqual(t, defaultFolderName, av.Name)

	def, err := folders.ensureFolder(context.Background(), caller, 0, "", "")
	require.NoError(t, err)
	assert.Equal(t, defaultFolderName, def.Name, "默认段的名字不能被头像段带跑")
	assert.NotEqual(t, av.ID, def.ID, "头像文件夹和默认文件夹必须是两个")
}

// TestEnsureFolder_AvatarSegmentReusesLegacyFolder 特征:
// 历史上叫"默认文件夹"的头像文件夹**被复用**,不会另建一个"头像"。
//
// ⚠️ **这是最危险的一种失效**:按名字找会找不到历史那一个 ⇒ 建出第二个
// 头像文件夹 ⇒ 头像文件散落在两处,而两处都显示得出来、都不报错。
// 用户换了两次头像,一半头像在 A 文件夹、一半在 B 文件夹。
func TestEnsureFolder_AvatarSegmentReusesLegacyFolder(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt).(*folderService)
	legacy := seedLegacyAvatarFolder(t, db, uid)

	got, err := folders.ensureFolder(context.Background(), mkFileCaller(uid), 0, avatarBiz, "")
	require.NoError(t, err)
	assert.Equal(t, legacy.ID, got.ID, "必须复用历史上那一个头像文件夹")

	var cnt int64
	require.NoError(t, db.Model(&model.SysFolder{}).
		Where("biz = ?", avatarBiz).Count(&cnt).Error)
	assert.Equal(t, int64(1), cnt, "一个用户只能有一个头像文件夹")
}

// TestEnsureFolder_AvatarSegmentNameGetsCorrected 特征:
// 碰到历史那一个头像文件夹时,顺手把名字纠正成"头像"。
//
// ⚠️ 启动期的批量改名是"尽力而为"(可能没跑过、可能跑失败),
// 这里是最后一道收敛 —— 只要有人动了头像段,名字就一定是对的。
func TestEnsureFolder_AvatarSegmentNameGetsCorrected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt).(*folderService)
	legacy := seedLegacyAvatarFolder(t, db, uid)

	_, err := folders.ensureFolder(context.Background(), mkFileCaller(uid), 0, avatarBiz, "")
	require.NoError(t, err)

	var row model.SysFolder
	require.NoError(t, db.Where("id = ?", legacy.ID).First(&row).Error)
	assert.Equal(t, avatarFolderName, row.Name, "名字必须被纠正")
	assert.False(t, row.IsDefault,
		"头像文件夹不能标成默认 —— 前端按 is_default 定位上传目标")
}

// TestNormalizeAvatarFolders_RenamesLegacyRows 特征:
// 启动期的批量改名:历史那行被改成"头像",且**幂等**。
func TestNormalizeAvatarFolders_RenamesLegacyRows(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	legacy := seedLegacyAvatarFolder(t, db, uid)

	n, err := NormalizeAvatarFolders(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "应改一行")

	var row model.SysFolder
	require.NoError(t, db.Where("id = ?", legacy.ID).First(&row).Error)
	assert.Equal(t, avatarFolderName, row.Name)
	assert.False(t, row.IsDefault)

	// 幂等:再跑一次没有改动
	n2, err := NormalizeAvatarFolders(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 0, n2, "已经改过的行不该再被算成改动")
}

// TestNormalizeAvatarFolders_LeavesOtherBizAlone 特征:
// 批量改名**只碰头像段** —— 普通业务段的"默认文件夹"必须原样保留。
func TestNormalizeAvatarFolders_LeavesOtherBizAlone(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	other := model.SysFolder{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    uid,
		Biz:       defaultBiz,
		Name:      defaultFolderName,
		IsDefault: true,
	}
	require.NoError(t, db.Create(&other).Error)

	n, err := NormalizeAvatarFolders(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "默认段不该被改")

	var row model.SysFolder
	require.NoError(t, db.Where("id = ?", other.ID).First(&row).Error)
	assert.Equal(t, defaultFolderName, row.Name)
	assert.True(t, row.IsDefault)
}

// TestCreate_FolderGetsInfoJsonImmediately 特征:
// 新建的文件夹**立刻**有一份 info.json。
//
// ⚠️ **这是用户那句话的字面要求** ——「按道理每个文件夹里面应该都有这个
// info.json 文件」。以前只有"文件事件"之后才生成 ⇒ 空文件夹里没有。
func TestCreate_FolderGetsInfoJsonImmediately(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt)

	fv, err := folders.Create(context.Background(), mkFileCaller(uid), FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	require.True(t, folderInfoPresent(t, p, opt, uid, fv.Biz, fv.ID),
		"新建文件夹必须立刻有一份 info.json")
}

// TestEnsureFolder_LazyFolderGetsInfoJson 特征:
// 懒建出来的文件夹(上传时 folder_id=0 落到的那个)也立刻有 info.json。
func TestEnsureFolder_LazyFolderGetsInfoJson(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt).(*folderService)

	fv, err := folders.ensureFolder(context.Background(), mkFileCaller(uid), 0, "", "")
	require.NoError(t, err)
	require.True(t, folderInfoPresent(t, p, opt, uid, fv.Biz, fv.ID),
		"懒建的文件夹也必须有 info.json")
}

// TestEnsureFolder_AvatarFolderGetsInfoJson 特征:
// **头像文件夹里也必须有 info.json** —— 用户报的就是这一条。
func TestEnsureFolder_AvatarFolderGetsInfoJson(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt).(*folderService)

	fv, err := folders.ensureFolder(context.Background(), mkFileCaller(uid), 0, avatarBiz, "")
	require.NoError(t, err)
	require.True(t, folderInfoPresent(t, p, opt, uid, avatarBiz, fv.ID),
		"头像文件夹里必须有 info.json")
}

// TestDelete_AvatarFolderIsRejected 特征:
// **头像文件夹删不掉** —— 它是系统托管的空间,不是用户该管的东西。
//
// ⚠️ **这条断言守的是 M140 的一个副作用** —— 头像文件夹以前删不掉,
// 是因为它被**误标成了 is_default**;M140 把 is_default 归零(它的语义是
// "上传落点",不是"受保护")⇒ 就必须**显式**挡住删除,否则用户删掉它 ⇒
// 头像文件被连带删掉,而 sys_user.avatar 还指着那个死 URL。
func TestDelete_AvatarFolderIsRejected(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt).(*folderService)
	caller := mkFileCaller(uid)

	av, err := folders.ensureFolder(context.Background(), caller, 0, avatarBiz, "")
	require.NoError(t, err)

	err = folders.Delete(context.Background(), caller, av.ID)
	require.Error(t, err, "头像文件夹必须删不掉")
	require.True(t, errCodeIs(err, errcode.Forbidden), "want Forbidden, got %v", err)

	// 行还在、而且没被软删(名字也没被改成 #del: 形式)
	var row model.SysFolder
	require.NoError(t, db.Where("id = ?", av.ID).First(&row).Error)
	assert.Equal(t, model.FileStatusNormal, row.Status)
	assert.Equal(t, avatarFolderName, row.Name)
}

// TestDelete_NormalFolderStillDeletable 特征:
// 加了"头像段不可删"的闸,**普通文件夹照旧能删** ——
// 闸不能顺手把整条删除路径堵死。
func TestDelete_NormalFolderStillDeletable(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt)
	caller := mkFileCaller(uid)

	fv, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "素材"})
	require.NoError(t, err)

	require.NoError(t, folders.Delete(context.Background(), caller, fv.ID))

	var row model.SysFolder
	require.NoError(t, db.Unscoped().Where("id = ?", fv.ID).First(&row).Error)
	assert.Equal(t, model.FileStatusDeleted, row.Status)
}

// TestBackfillFolderInfo_WritesMissingInfoJson 特征:
// 缺 info.json 的文件夹被补上。
//
// ⚠️ **回填的输入是"历史上就没有"** —— 这里先建文件夹(它会写一份),
// 再把那份删掉,模拟 M133 之前的状态。回填必须能把它补回来。
func TestBackfillFolderInfo_WritesMissingInfoJson(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt)

	fv, err := folders.Create(context.Background(), mkFileCaller(uid), FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	key, err := infoKey(opt.Root, uid, fv.Biz, fv.ID)
	require.NoError(t, err)
	require.NoError(t, p.Delete(context.Background(), key))
	require.False(t, folderInfoPresent(t, p, opt, uid, fv.Biz, fv.ID))

	n, err := BackfillFolderInfo(context.Background(), db, p, opt, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "应补一份")
	require.True(t, folderInfoPresent(t, p, opt, uid, fv.Biz, fv.ID),
		"缺的 info.json 必须被补上")
}

// TestBackfillFolderInfo_KeepsExisting 特征:
// **已有的 info.json 不被覆盖**。
//
// ⚠️ 反面断言:回填是"缺了才补",不是"每次启动重写一遍"。
// 无脑重写的话存储侧凭空多 N 次 PUT,而内容一样,写了也是白写。
func TestBackfillFolderInfo_KeepsExisting(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt)

	fv, err := folders.Create(context.Background(), mkFileCaller(uid), FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	key, err := infoKey(opt.Root, uid, fv.Biz, fv.ID)
	require.NoError(t, err)
	// 塞一个标记,验证回填没有覆盖已有的
	_, err = p.Put(context.Background(), key,
		strings.NewReader(`{"marker":1}`), "application/json")
	require.NoError(t, err)

	n, err := BackfillFolderInfo(context.Background(), db, p, opt, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "已经有的 info.json 不该被重写")
	body := mustRead(t, p, key)
	assert.Contains(t, string(body), "marker", "已有的 info.json 不能被覆盖")
}

// TestBackfillFolderInfo_SkipsDeletedFolders 特征:
// 软删的文件夹**不补** —— 它的 info.json 早就跟着前缀一起删了。
func TestBackfillFolderInfo_SkipsDeletedFolders(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	p, opt := newFileTestProv(t)
	folders := NewFolderService(db, p, opt)

	fv, err := folders.Create(context.Background(), mkFileCaller(uid), FolderCreateReq{Name: "素材"})
	require.NoError(t, err)
	key, err := infoKey(opt.Root, uid, fv.Biz, fv.ID)
	require.NoError(t, err)
	require.NoError(t, p.Delete(context.Background(), key))
	// 软删这个文件夹
	require.NoError(t, db.Model(&model.SysFolder{}).Where("id = ?", fv.ID).
		Updates(map[string]any{"status": model.FileStatusDeleted}).Error)

	n, err := BackfillFolderInfo(context.Background(), db, p, opt, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "软删的文件夹不该被补")
	require.False(t, folderInfoPresent(t, p, opt, uid, fv.Biz, fv.ID))
}
