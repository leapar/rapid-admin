// 头像上传(M124)—— service 层特征测试
//
// ⚠️ **这份用例清单是"拆掉实现看测试会不会红"的清单,不是覆盖率清单。**
// 每一行对应一处能被无声改坏的逻辑:
//   · 只按扩展名放行 → 一个 HTML 改名成 .png 就能存进你的域下
//   · 只按浏览器上报的 Content-Type 放行 → 上报什么就是什么
//   · 存进 sys_user.avatar 的是签名 URL → 15 分钟后头像裂图
//   · 旧头像不回收 → 每换一次头像永久多占一份配额
//   · 落点不在自己 id 底下 → 别人的头像能覆盖我的
package service

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/oss"
)

// pngBytes 造一张**最小可识别**的 PNG(只到魔数,不做完整编码)
//
// ⚠️ 服务端只做**魔数**判定(不做图片解码)—— 见 user_avatar.go 的 isImageBytes。
// 所以测试也只需要魔数对。
func pngBytes() []byte {
	return []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32))
}

// jpegBytes 同上,JPEG 魔数
func jpegBytes() []byte {
	return []byte("\xFF\xD8\xFF\xE0" + strings.Repeat("\x00", 32))
}

func newAvatarSvc(t *testing.T) (UserService, *gorm.DB) {
	t.Helper()
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	return NewUserService(db, files, nil), db
}

// ===== 1. 落点 + 写库 =====

// TestSetAvatar_WritesUserAvatarAndLandsUnderOwnUserID
//
// 断言三件事:
//  1. sys_user.avatar 被写成**稳定直链**(不是签名 URL)
//  2. key 落在**自己 id 底下**,biz 段是 avatar
//  3. 文件夹是懒建的(biz=avatar 的那个文件夹)
func TestSetAvatar_WritesUserAvatarAndLandsUnderOwnUserID(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	body := pngBytes()
	view, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "me.png", ContentType: "image/png",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.NoError(t, err)
	require.NotEmpty(t, view.URL)

	// ① 写库
	var u model.SysUser
	require.NoError(t, db.First(&u, uid).Error)
	assert.Equal(t, view.URL, u.Avatar)

	// ② 稳定直链:**不带签名参数**(签名 URL 过期就裂图)
	assert.NotContains(t, view.URL, "?", "头像必须是稳定直链,签名 URL 过期就裂图")
	assert.True(t, strings.HasPrefix(view.URL, "/"+oss.DefaultRoot+"/"), "直链形态变了: %s", view.URL)

	// ③ 落点:key = {root}/{user_id}/avatar/{folder_id}/{name}
	var f model.SysFile
	require.NoError(t, db.Where("biz = ?", "avatar").First(&f).Error)
	assert.Equal(t, uid, f.UserID)
	assert.Equal(t, "avatar", f.Biz)
	assert.Contains(t, f.FileKey, "/avatar/")
	assert.Contains(t, f.FileKey, "/"+itoa(uid)+"/")

	// ④ 文件夹是懒建出来的
	var folders int64
	require.NoError(t, db.Model(&model.SysFolder{}).Where("user_id = ? AND biz = ?", uid, "avatar").
		Count(&folders).Error)
	assert.Equal(t, int64(1), folders)
}

// TestSetAvatar_StoresRealBytes —— 存进去的**是那张图**,不是 0 字节
//
// ⚠️ 魔数判定用 bufio.Peek(不消费流)。如果哪天改成读了流没还回去,
// 存进去的就是空文件 —— 而这个 bug **不报错**,只是头像裂图。
func TestSetAvatar_StoresRealBytes(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	body := pngBytes()
	_, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "me.png", ContentType: "image/png",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.NoError(t, err)

	var f model.SysFile
	require.NoError(t, db.Where("biz = ?", "avatar").First(&f).Error)
	assert.Equal(t, int64(len(body)), f.FileSize)

	// 从存储里把字节读回来 —— 证明落盘的是完整内容
	rc, _, err := files.Content(context.Background(), caller, f.ID)
	require.NoError(t, err)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

// ===== 2. 类型闸 =====

// TestSetAvatar_RejectsNonImageExtension —— 扩展名不是图片 ⇒ 拒
//
// ⚠️ **扩展名决定的是"发出去的 Content-Type"** —— 服务端按扩展名探测
// (probeContentType),探测结果会写进 sys_file.content_type,
// 浏览器取这个文件时按它渲染。
//
// 所以一个叫 `evil.svg` 的文件会被判成 `image/svg+xml` ——
// **SVG 能内嵌脚本**,而浏览器拿到 image/svg+xml 会**当文档渲染并执行**。
// 那就是存 XSS(挂在本站域名下,能偷 cookie)。
//
// ⚠️ 这里的**内容是真 PNG**:这条用例要隔离的是"**扩展名这一关**"。
// 如果内容也是 HTML,它会被后面那道魔数校验拦掉 ⇒ 这条用例就分不清
// "是扩展名这一关拦的"还是"是魔数那一关拦的",拆掉任意一关它都照样绿。
func TestSetAvatar_RejectsNonImageExtension(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	body := pngBytes()
	_, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "evil.svg", ContentType: "image/png", // ← 谎报自己是图
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.Error(t, err)
	assert.True(t, errCodeIs(err, errcode.InvalidParam), "错误码不对: %v", err)

	// ⚠️ **一个字节都不许落盘**
	var files0 int64
	require.NoError(t, db.Model(&model.SysFile{}).Count(&files0).Error)
	assert.Equal(t, int64(0), files0, "被拒的文件不该落盘")

	var u model.SysUser
	require.NoError(t, db.First(&u, uid).Error)
	assert.Empty(t, u.Avatar)
}

// TestSetAvatar_RejectsNonImageBytes —— 扩展名是 .png 但**内容不是图** ⇒ 拒
//
// 这一条是"只做扩展名校验"的解药:改名成 .png 的 HTML 会被扩展名放行。
func TestSetAvatar_RejectsNonImageBytes(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	body := []byte("<html><body>not an image at all</body></html>")
	_, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "sneaky.png", ContentType: "image/png",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.Error(t, err)
	assert.True(t, errCodeIs(err, errcode.InvalidParam), "错误码不对: %v", err)

	var files0 int64
	require.NoError(t, db.Model(&model.SysFile{}).Count(&files0).Error)
	assert.Equal(t, int64(0), files0)
}

// TestSetAvatar_RejectsOversize —— 超过 5MB ⇒ 拒
func TestSetAvatar_RejectsOversize(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	big := make([]byte, maxAvatarBytes+1)
	copy(big, pngBytes())
	_, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "big.png", ContentType: "image/png",
		Size: int64(len(big)), Body: bytes.NewReader(big),
	})
	require.Error(t, err)
	assert.True(t, errCodeIs(err, errcode.InvalidParam), "错误码不对: %v", err)

	var files0 int64
	require.NoError(t, db.Model(&model.SysFile{}).Count(&files0).Error)
	assert.Equal(t, int64(0), files0)
}

// TestSetAvatar_AcceptsJpeg —— 别把 JPEG 误伤
//
// ⚠️ 这条是"白名单只写了 png"的解药。
func TestSetAvatar_AcceptsJpeg(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	body := jpegBytes()
	view, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "me.jpg", ContentType: "image/jpeg",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, view.URL)
}

// ===== 3. 换头像:旧文件要回收 =====

// TestSetAvatar_ReplacesAndRecyclesOldFile
//
// 换一次头像之后:
//   - sys_user.avatar 指向**新**文件
//   - **旧**文件行被软删(status=deleted)
//   - 配额只算**一份**(旧的退掉了)
//
// ⚠️ 不回收的话,用户每换一次头像就永久多占一份配额 —— 配额几十 MB,
// 换十几次就传不上了,而且文件管理页里躺着一堆"同一张脸的历史版本"。
func TestSetAvatar_ReplacesAndRecyclesOldFile(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	first := pngBytes()
	v1, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "a.png", ContentType: "image/png",
		Size: int64(len(first)), Body: bytes.NewReader(first),
	})
	require.NoError(t, err)
	require.Equal(t, int64(len(first)), usedBytes(t, db, uid))

	// 记下旧文件 id(第二行断言要用)
	var oldFile model.SysFile
	require.NoError(t, db.Where("biz = ?", "avatar").First(&oldFile).Error)

	second := jpegBytes()
	v2, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "b.jpg", ContentType: "image/jpeg",
		Size: int64(len(second)), Body: bytes.NewReader(second),
	})
	require.NoError(t, err)
	assert.NotEqual(t, v1.URL, v2.URL)

	// 头像指向新文件
	var u model.SysUser
	require.NoError(t, db.First(&u, uid).Error)
	assert.Equal(t, v2.URL, u.Avatar)

	// 旧文件行被软删
	//
	// ⚠️ **必须 Unscoped** —— 删除走的是软删(status=deleted + deleted_at),
	// GORM 默认查询会带上 `deleted_at IS NULL` ⇒ 普通 First 查不到,
	// 而"查不到"会被误读成"文件没了"(其实是想确认它被标记成已删)。
	var old model.SysFile
	require.NoError(t, db.Unscoped().First(&old, oldFile.ID).Error)
	assert.Equal(t, int(model.FileStatusDeleted), int(old.Status), "旧头像文件没被回收")

	// 配额只算一份(旧的那份退掉了)
	assert.Equal(t, int64(len(second)), usedBytes(t, db, uid))
}

// ===== 4. 装配缺件 =====

// TestSetAvatar_WithoutFileSystem —— 没装文件系统 ⇒ 明确报错,不是 panic
func TestSetAvatar_WithoutFileSystem(t *testing.T) {
	db := newFileTestDB(t)
	svc := NewUserService(db, nil, nil)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	body := pngBytes()
	_, err := svc.SetAvatar(context.Background(), caller, AvatarUploadReq{
		FileName: "me.png", ContentType: "image/png",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.Error(t, err)
	assert.True(t, errCodeIs(err, errcode.Internal), "错误码不对: %v", err)
}

// TestSetAvatar_Unauthenticated —— 没登录 ⇒ 401
func TestSetAvatar_Unauthenticated(t *testing.T) {
	db := newFileTestDB(t)
	files, _ := newFileSvc(t, db)
	svc := NewUserService(db, files, nil)

	body := pngBytes()
	_, err := svc.SetAvatar(context.Background(), nil, AvatarUploadReq{
		FileName: "me.png", ContentType: "image/png",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.Error(t, err)
	assert.True(t, errCodeIs(err, errcode.Unauthorized), "错误码不对: %v", err)
}
