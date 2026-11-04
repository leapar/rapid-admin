// M80 文件系统 —— 文件的访问直链
//
// ## 这条网守的是什么
//
//	`GET /files/:id/url` 的契约(设计文档 §8.1)是:**每个文件都拿得到一条直链**。
//
//	存储侧只有一棵树、一个桶 —— 没有"公开/私有"两套布局(见 oss/path.go 的
//	文件头)。"私密"不是靠目录隔离实现的,而是靠 **key 不可猜**:uid 是雪花
//	id、对象名带 8 位随机后缀。所以直链是**唯一的主路径**,`/files/:id/content`
//	只是兜底。
//
// ## 曾经踩过的坑
//
//	早先 local 的"私有文件"签不出直链,而实现对这种情况**返 500**
//	(`生成访问地址失败`),于是:
//
//	1. 前端 `resolveFileUrl` 写的是"先试 /url,拿不到 url 就走 /content 中转",
//	   而 axios 拦截器遇到 500 直接 reject ⇒ **那条 fallback 永远执行不到**;
//	2. 默认部署(local)下文件打开是裂图。
//
//	现在没有"签不出来"这个状态了:签不出来 = 对象真的不在 = 真故障,必须报错。
//
// ## 可逆探针(拆掉实现看会不会红)
//
//	· 把 local 的 `SignDownloadURL` 改成对任何 key 返 "" → 「直链非空」红;
//	· 把 `URLBase` 置空 → 「直链非空」红(而且不会静默给一条废 URL);
//	· 把 `SignDownloadURL` 的错误吞掉(返 "", nil)→ 「文件不存在必须是错」红。
//
//	⚠️ **探针抓出来的一条假绿**:上面第一个探针跑下来,「直链稳定」那条
//	**没红** —— 因为"两次都返空串"也满足"两次一样"。已经补上非空断言
//	(见 TestFileURL_DirectURLIsStable 的注释)。**用例自己也会假绿**。
package service

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/oss"

	"rapid-admin/internal/pkg/errcode"
)

// putOne 传一个文件(单次上传)
func putOne(t *testing.T, svc FileService, caller *Caller, name string, content []byte) *FileView {
	t.Helper()
	v, err := svc.Receive(context.Background(), caller, FileReceiveReq{
		Biz: "asset", FileName: name, ContentType: "application/octet-stream",
		Size: int64(len(content)), Body: bytes.NewReader(content),
	})
	require.NoError(t, err)
	return v
}

// TestFileURL_LocalFile_ReturnsDirectURL 特征:
// **每个文件都拿得到直链** —— 没有"公开/私有"之分。
//
// ⚠️ 断言的是"err == nil" —— 只断言 url 非空的话,"报 500 同时 url 为空"
// 也能骗过测试(错误被吞掉时 url 本来就是空串)。
func TestFileURL_LocalFile_ReturnsDirectURL(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	f := putOne(t, svc, caller, "户型图1.jpg", []byte("bytes"))

	u, err := svc.URL(context.Background(), caller, f.ID, 0)
	require.NoError(t, err, "签不出直链是真故障,不该报 500 —— 更不该返空串")
	require.NotEmpty(t, u, "每个文件都要有直链")
	require.True(t, strings.HasPrefix(u, "/"+oss.DefaultRoot+"/"),
		"直链要指向静态挂载点(/"+oss.DefaultRoot+"/), got %q", u)
	require.NotContains(t, u, "/public/",
		"没有公开段这回事了 —— key 恒为 root/uid/biz/folder/name")
}

// TestFileURL_DirectURLIsStable 特征:
// **同一条直链反复取必须一模一样**。
//
// ⚠️ 这是"直链能不能直接塞进 <img src>"的前提。签名 URL 一过期,页面上已经
// 渲染出来的图就裂 —— 而前端拿到的 FileView 早就用上去了,不会重新去签。
// 存储侧只有一棵树,直链是拼出来的,所以它必须是稳定的。
func TestFileURL_DirectURLIsStable(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	f := putOne(t, svc, caller, "a.txt", []byte("x"))

	u1, err := svc.URL(context.Background(), caller, f.ID, 0)
	require.NoError(t, err)
	u2, err := svc.URL(context.Background(), caller, f.ID, 0)
	require.NoError(t, err)
	// ⚠️ **必须同时断言非空** —— 只断言"两次一样"的话,"两次都是空串"也算稳定。
	//   (探针实测:把 local 的 SignDownloadURL 改成恒返 "" 时,这条会假绿。)
	require.NotEmpty(t, u1, "直链不能是空串")
	require.Equal(t, u1, u2, "直链必须是稳定的 —— 带签名的话第二次就变了")
}

// TestFileURL_UnknownFile_StillErrors 特征:
// **真故障仍然是错** —— "签不出来"不能被当成正常结论吞成空串。
func TestFileURL_UnknownFile_StillErrors(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	_, err := svc.URL(context.Background(), caller, 999999, 0)
	require.Error(t, err, "文件不存在必须是错,不能返空串")
}

// TestFileURL_CrossUserIsNotFound 特征:
// **跨用户一律 404**,不是 403。
//
// ⚠️ 403 等于告诉 A"这个 id 确实存在,只是不是你的",扫一遍 id 就能把别人的
// 文件清单摸出来。
func TestFileURL_CrossUserIsNotFound(t *testing.T) {
	db := newFileTestDB(t)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)

	f := putOne(t, svc, mkFileCaller(uidA), "secret.txt", []byte("s3cret"))

	_, err := svc.URL(context.Background(), mkFileCaller(uidB), f.ID, 0)
	require.True(t, errCodeIs(err, errcode.NotFound), "want NotFound, got %v", err)
}

// TestReceive_ReturnedViewCarriesDirectURL 特征:
// **上传出口就带直链** —— 前端拿 FileView 就能直接渲染,不用再打一次 /url。
//
// ⚠️ 早先这条只在"公开文件"上成立,前端于是对每个文件都要多发一次请求;
// 现在每个文件都有直链,这条对所有文件成立。
func TestReceive_ReturnedViewCarriesDirectURL(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)
	caller := mkFileCaller(uid)

	v := putOne(t, svc, caller, "cover.jpg", []byte("jpegbytes"))

	require.NotEmpty(t, v.URL, "上传完就该能直接显示")
	require.True(t, strings.HasPrefix(v.URL, "/"+oss.DefaultRoot+"/"),
		"直链要指向静态挂载点(/"+oss.DefaultRoot+"/), got %q", v.URL)
}
