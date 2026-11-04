// M80 文件夹 —— 文件夹名搜索
//
// 背景:用户 2026-10-30 报「文件夹管理界面的 searchsection 里没有搜索输入框」。
// 前端那一层已经修好(补了 onSearch),但**后端本来就没有 Keyword 这个入参** ——
// 前端就算把关键字传过去,service 层也只是原样忽略,列表还是全量返回。
//
// ⚠️ **这类 bug 不会报错**:搜索框能打字、回车不报错、列表原样不动。
// 唯一能证明"搜索真的生效"的断言是"传了关键字之后**命中数变少**"。
//
// 所以这个文件只测一件事:**List 的 Keyword 真的把行过滤掉了**。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// mkFolders 建 n 个同名文件夹之外的若干文件夹,返回按名字排序后的视图
func mkFolders(t *testing.T, folders FolderService, caller *Caller, names ...string) {
	t.Helper()
	for _, n := range names {
		_, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: n})
		require.NoErrorf(t, err, "建文件夹 %q 失败", n)
	}
}

// folderNames 抽出列表里的文件夹名
func folderNames(res *FolderPageResult) []string {
	out := make([]string, 0, len(res.List))
	for _, f := range res.List {
		out = append(out, f.Name)
	}
	return out
}

// TestFolderList_KeywordFiltersByName 特征:传了关键字之后,
// **列表只留名字里含这个关键字的**,不是原样返回全量。
//
// ⚠️ 反面断言也要有:不传关键字时全量都在(不然"永远返回 0 条"也能骗过测试)。
func TestFolderList_KeywordFiltersByName(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	mkFolders(t, folders, caller, "户型图", "业主照片", "户型图归档", "合同扫描件")

	// 前置:不传关键字 → 全量 4 条
	all, err := folders.List(context.Background(), caller, FolderListQuery{Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(4), all.Total, "前置:不传关键字应返回全部 4 个")

	// 命中 2 个:"户型图" 和 "户型图归档"
	hit, err := folders.List(context.Background(), caller, FolderListQuery{Keyword: "户型图", Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(2), hit.Total, "关键字应真的过滤掉不匹配的")
	require.ElementsMatch(t, []string{"户型图", "户型图归档"}, folderNames(hit))

	// 命中 1 个
	one, err := folders.List(context.Background(), caller, FolderListQuery{Keyword: "业主", Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), one.Total)
	require.Equal(t, []string{"业主照片"}, folderNames(one))

	// 命中 0 个
	none, err := folders.List(context.Background(), caller, FolderListQuery{Keyword: "不存在的名字", Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(0), none.Total, "搜不到就该是空列表,不是全量")
	require.Empty(t, none.List)
}

// TestFolderList_KeywordDoesNotCrossUsers 特征:关键字不能把别人的文件夹搜出来。
//
// ⚠️ 关键词是**每个用户各自**的搜索,不是全站搜索。
func TestFolderList_KeywordDoesNotCrossUsers(t *testing.T) {
	db := newFileTestDB(t)
	uid1 := seedFileUser(t, db, 0)
	uid2 := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())

	mkFolders(t, folders, mkFileCaller(uid1), "户型图")
	mkFolders(t, folders, mkFileCaller(uid2), "户型图")

	res, err := folders.List(context.Background(), mkFileCaller(uid1), FolderListQuery{Keyword: "户型图", Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total, "只能搜到自己的")
	require.Equal(t, uid1, res.List[0].UserID)
}

// TestFolderList_KeywordAndBizAreAnded 特征:关键字和业务段是 **AND** 关系。
//
// ⚠️ 业务段和关键字都作用在同一个 tx 上,两个条件叠加才是"在这个业务段里
// 搜这个名字"。这里锁住 AND —— 如果哪天改成 OR,搜出来的会多出一堆别的段。
func TestFolderList_KeywordAndBizAreAnded(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	folders := NewFolderService(db, mustProv(t), DefaultFileOptions())
	caller := mkFileCaller(uid)

	_, err := folders.Create(context.Background(), caller, FolderCreateReq{Name: "户型图", Biz: "house"})
	require.NoError(t, err)
	_, err = folders.Create(context.Background(), caller, FolderCreateReq{Name: "户型图", Biz: "material"})
	require.NoError(t, err)

	res, err := folders.List(context.Background(), caller, FolderListQuery{Biz: "house", Keyword: "户型图", Page: 1, Size: 50})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total, "biz + keyword 必须是 AND")
	require.Equal(t, "house", res.List[0].Biz)
}
