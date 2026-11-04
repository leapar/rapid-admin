// 文件系统 —— ResolveKeys(批量把 key 解析成「原文件名 + 直链」)
//
// ⚠️ **这一层是动态集合「附件字段」的底座** ——
// 字段列里只有文件 key,原文件名与直链不在列里 ⇒ 读记录时要靠它解析。
//
// ⚠️ **它刻意不做归属校验** ——
// 一条记录里的附件 key 可能是别人上传的(管理员替人录数据)。
// 按"只有上传者能读"的规则去解析,附件就永远显示不出来。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveKeys_解析出原文件名与直链 ——
// 对照组:下面两条"查不到"的用例要有这条做参照,否则它们恒真。
func TestResolveKeys_解析出原文件名与直链(t *testing.T) {
	db := newFileTestDB(t)
	svc, _ := newFileSvc(t, db)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	f := put(t, svc, caller, "户型图.pdf", []byte("hello"), 5)
	require.NotEmpty(t, f.FileKey)

	refs, err := svc.ResolveKeys(context.Background(), []string{f.FileKey})
	require.NoError(t, err)
	ref, ok := refs[f.FileKey]
	require.True(t, ok, "刚传的文件必须能解析出来")
	assert.Equal(t, "户型图.pdf", ref.Name, "原文件名要原样给出(用户认的是这个名字)")
	assert.NotEmpty(t, ref.URL, "直链不能为空")
}

// TestResolveKeys_查不到就不在结果里 ——
// ⚠️ **查不到不是错误** ——
// 文件被删了 / key 写错了 / 存储还没同步,都是"这一项没有解析出来",
// 不是"整个请求失败"(旁路纪律)。
func TestResolveKeys_查不到就不在结果里(t *testing.T) {
	db := newFileTestDB(t)
	svc, _ := newFileSvc(t, db)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	f := put(t, svc, caller, "a.pdf", []byte("hello"), 5)

	refs, err := svc.ResolveKeys(context.Background(),
		[]string{f.FileKey, "rapid-admin/1/dyn/0/nope.pdf"})
	require.NoError(t, err)
	assert.Len(t, refs, 1, "只有存在的那一个")
	_, ok := refs["rapid-admin/1/dyn/0/nope.pdf"]
	assert.False(t, ok, "不存在的 key 不该在结果里")
}

// TestResolveKeys_空输入 ——
// 一页记录里没有附件字段时,这个调用根本不该发生。
func TestResolveKeys_空输入(t *testing.T) {
	db := newFileTestDB(t)
	svc, _ := newFileSvc(t, db)

	refs, err := svc.ResolveKeys(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, refs)
}

// TestResolveKeys_重复key只算一次 ——
// 一页 20 条记录指向同一个文件时,不该查 20 次。
func TestResolveKeys_重复key只算一次(t *testing.T) {
	db := newFileTestDB(t)
	svc, _ := newFileSvc(t, db)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	f := put(t, svc, caller, "a.pdf", []byte("hello"), 5)

	refs, err := svc.ResolveKeys(context.Background(),
		[]string{f.FileKey, f.FileKey, f.FileKey, ""})
	require.NoError(t, err)
	assert.Len(t, refs, 1)
}

// TestResolveKeys_别人的文件也解析得出 ——
// ⚠️ **这条钉的是"不做归属校验"这个决定** ——
// 管理员替人录数据时挂一个别人传的文件是合法的;
// 若这里按"只有上传者能读"过滤,管理员根本录不进去。
func TestResolveKeys_别人的文件也解析得出(t *testing.T) {
	db := newFileTestDB(t)
	svc, _ := newFileSvc(t, db)
	uidA := seedFileUser(t, db, 0)
	uidB := seedFileUser(t, db, 0)

	f := put(t, svc, mkFileCaller(uidA), "a.pdf", []byte("hello"), 5)

	refs, err := svc.ResolveKeys(context.Background(), []string{f.FileKey})
	require.NoError(t, err)
	_, ok := refs[f.FileKey]
	assert.True(t, ok, "归属不参与解析(uidA 传的,uidB 也要能解析出来)")
	assert.NotEqual(t, uidA, uidB)
}
