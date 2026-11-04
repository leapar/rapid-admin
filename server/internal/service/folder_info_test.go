// M133 info.json 读回链路 —— service 层
//
// ⚠️ **两个 bug 叠在一起,症状是同一个:`info_json` 恒为空串。**
//
//	① **读不回来** —— readFolderInfo type-assert 的是
//	   `Get(ctx, key) ([]byte, error)`,而 M133 之前**三家 provider 一个都没实现**
//	   (local 只有 Open/Stat,aliyun 连读的方法都没有,mock 的 Get 签名还不一样)
//	   ⇒ 断言失败 ⇒ 直接返回 ""。
//	② **写不进去** —— Receive(服务端中转)/ Register(OSS 直传)这两条注册路径
//	   **从来不调 syncFolderInfo**(只有 SetCover / 删除 / 分片合并调)
//	   ⇒ 文件夹里压根就没有 info.json 可读。
//
// 只修①,Detail 依然返空串(没文件可读);只修②,Detail 依然返空串(读不回来)。
// ⇒ 下面每条用例都同时盯住"存储里真的有"和"读回来真的有"。
//
// ⚠️ **这层用真的 local provider,不用 mock** —— info.json 的 key 是
// `res/{uid}/{biz}/{fid}/info.json`,而 local 的 key→磁盘路径映射
// (含剥掉第一段 urlBase)只有真 provider 才会算对。用 mock 的话,
// "写进去的 key"和"读出来的 key"永远是同一个字符串,路径错位测不出来。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/oss"
)

// 特征:**上传完 info.json 就必须存在,且读得回来、内容对得上**。
//
// 这条同时盯住 M133 的两个 bug:
// - 存储里没有 info.json ⇒ 注册路径漏了 syncFolderInfo(②)
// - 读回来是空串 ⇒ provider 没实现 MetaReader(①)
func TestFolderInfo_ReadableAfterUpload(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	svc := NewFileService(db, prov, opt)
	folders := NewFolderService(db, prov, opt)
	caller := mkFileCaller(uid)

	body := []byte("hello rapid-admin")
	v := put(t, svc, caller, "plan.txt", body, int64(len(body)))

	// ① 存储里必须真的有 info.json
	key, err := infoKey(opt.Root, uid, "asset", v.FolderID)
	require.NoError(t, err)
	_, err = prov.Stat(context.Background(), key)
	require.NoError(t, err,
		"上传完必须在存储里重建 info.json —— 找不到说明注册路径漏了 syncFolderInfo")

	// ② Detail 必须把它读回来,而且内容对得上
	d, err := folders.Detail(context.Background(), caller, v.FolderID)
	require.NoError(t, err)
	require.NotEmpty(t, d.InfoJSON,
		"info_json 恒为空 —— provider 没实现 oss.MetaReader,读回链路不通")

	var info folderInfo
	require.NoError(t, json.Unmarshal([]byte(d.InfoJSON), &info))
	require.Equal(t, 1, info.FileCount, "清单里的文件数要跟实际一致")
	require.Len(t, info.Files, 1)
	require.Equal(t, "plan.txt", info.Files[0].Name)
	require.Equal(t, int64(len(body)), info.Files[0].Size)
	require.Equal(t, "asset", info.Biz)
}

// 特征:**清单是"重建"出来的,不是"追加"出来的** —— 传第二个文件后
// file_count 要变成 2,而且第一个文件还在里面。
//
// ⚠️ 这条盯的是"只在第一次建、之后不更新"那种半吊子实现:那样
// info.json 会一直停在第一个文件上,而接口返回 200、字段看着也有值。
func TestFolderInfo_RebuiltNotAppended(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	svc := NewFileService(db, prov, opt)
	folders := NewFolderService(db, prov, opt)
	caller := mkFileCaller(uid)

	v := put(t, svc, caller, "a.txt", []byte("aaa"), 3)
	put(t, svc, caller, "b.txt", []byte("bbb"), 3)

	d, err := folders.Detail(context.Background(), caller, v.FolderID)
	require.NoError(t, err)
	var info folderInfo
	require.NoError(t, json.Unmarshal([]byte(d.InfoJSON), &info))
	require.Equal(t, 2, info.FileCount, "第二个文件传完清单要重建,不是停在 1")
	require.Equal(t, int64(6), info.TotalSize)
	names := []string{info.Files[0].Name, info.Files[1].Name}
	require.ElementsMatch(t, []string{"a.txt", "b.txt"}, names)
}

// 特征:**OSS 直传登记完也要重建清单** —— 直传是"文件真的落地了"的那一刻。
//
// ⚠️ 这条是 Register 那处漏调 syncFolderInfo 的专属守卫。直传路径在
// M133 之前是"文件在、清单不更新",而列表页看的是 DB(对),详情页看的
// 是 info.json(错)—— 两处对不上,排查的人会先去怀疑列表页。
func TestFolderInfo_RebuiltAfterDirectRegister(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	svc := NewFileService(db, prov, opt)
	folders := NewFolderService(db, prov, opt)
	caller := mkFileCaller(uid)

	f, err := folders.Create(context.Background(), caller, FolderCreateReq{Biz: "asset", Name: "f"})
	require.NoError(t, err)

	// 模拟客户端自己直传上去的对象
	prefix, err := oss.FolderPrefix(opt.Root, uid, "asset", f.ID)
	require.NoError(t, err)
	key := prefix + "1-eeeeeeee-d.png"
	body := []byte("png-bytes")
	_, err = prov.Put(context.Background(), key, bytes.NewReader(body), "image/png")
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), caller, FileRegisterReq{
		Key: key, FileName: "d.png", ContentType: "image/png",
	})
	require.NoError(t, err)

	d, err := folders.Detail(context.Background(), caller, f.ID)
	require.NoError(t, err)
	require.NotEmpty(t, d.InfoJSON, "直传登记完也要重建 info.json")
	var info folderInfo
	require.NoError(t, json.Unmarshal([]byte(d.InfoJSON), &info))
	require.Equal(t, 1, info.FileCount)
	require.Equal(t, "d.png", info.Files[0].Name)
}

// 特征:**删掉文件后清单要跟着缩** —— 清单是快照,不是流水账。
//
// ⚠️ 这条在 M133 之前是"碰巧对的"(删除路径本来就有 syncFolderInfo),
// 但它是唯一一个"清单和 DB 会漂"的场景,值得钉住 ——
// 万一以后有人把删除路径的 syncFolderInfo 删了(看着像冗余调用),
// 这条会立刻红。
func TestFolderInfo_ShrinksAfterDelete(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	svc := NewFileService(db, prov, opt)
	folders := NewFolderService(db, prov, opt)
	caller := mkFileCaller(uid)

	v := put(t, svc, caller, "gone.txt", []byte("xyz"), 3)
	require.NoError(t, svc.Delete(context.Background(), caller, v.ID))

	d, err := folders.Detail(context.Background(), caller, v.FolderID)
	require.NoError(t, err)
	var info folderInfo
	require.NoError(t, json.Unmarshal([]byte(d.InfoJSON), &info))
	require.Equal(t, 0, info.FileCount, "文件删了清单要跟着缩")
	require.Equal(t, int64(0), info.TotalSize)
}

// 特征:**provider 没实现 MetaReader 时,详情不能整个失败** ——
// info_json 就是空串。
//
// ⚠️ 新接一个 provider 忘了实现 MetaReader 是很现实的事(它是可选接口)。
// 那时候的症状必须是"这个字段空着",而不是"GET /folders/:id 500"
// —— 后者会把整个文件夹功能打挂,排障的人根本想不到是少实现了一个
// 可选接口。
func TestFolderDetail_ProviderWithoutMetaReaderDoesNotFail(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	prov, opt := newFileTestProv(t)
	// noContentProvider 只嵌 oss.Provider(8 个方法),刻意不带 MetaReader
	folders := NewFolderService(db, noContentProvider{Provider: prov}, opt)
	caller := mkFileCaller(uid)

	f, err := folders.Create(context.Background(), caller, FolderCreateReq{Biz: "asset", Name: "f"})
	require.NoError(t, err)

	d, err := folders.Detail(context.Background(), caller, f.ID)
	require.NoError(t, err, "provider 没实现 MetaReader 时详情不能整个失败")
	require.Empty(t, d.InfoJSON, "读不到就是空串,不是报错")
}
