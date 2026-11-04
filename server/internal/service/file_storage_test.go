// 文件「存储后端」标记 —— sys_file.storage 记的是"这个文件实际躺在哪"
//
// 守的是**两处各自独立、都把事情做反了**的 bug:
//
//  1. 后端 storageOf() 比的是 provider 名 "aliyun",而 aliyun provider 的
//     Name() 返回的是 "oss" ⇒ 这条分支**恒不成立** ⇒ 配了 OSS 的部署里,
//     每一行文件都被记成"本地磁盘"。而逐行记 storage 的意义正是迁移期
//     两种 provider 共存(先复制再切)—— 记错了这个字段就白留。
//
//  2. 前端把两个枚举值搞反(`v === 1 ? 'OSS' : '本地'`),本地磁盘上的文件
//     在页面上显示成 "OSS"(用户 2026-10-30 报的)。
//     前端那半由 web/src/pages/file/__tests__/list.test.tsx 守。
//
// ⚠️ 这里**用真实 provider + 真实 sqlite**,不 mock 存储:
// 这一层的 bug 大多藏在"provider 的真实返回值"和"GORM 的真实行为"之间。
package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/oss"
)

// namedProv 给 provider 套一个自定义名字 —— 用来模拟"配了 OSS 的部署"。
//
// ⚠️ 底层还是 local provider(照样往磁盘写):这里要验的是
// **provider 名 → storage 枚举** 的映射,不是 OSS 的字节读写。
type namedProv struct {
	oss.Provider
	name string
}

func (p *namedProv) Name() string { return p.name }

// TestStorageOf_RecordsTheActualBackend 逐行记的 storage 必须等于实际后端。
func TestStorageOf_RecordsTheActualBackend(t *testing.T) {
	db := newFileTestDB(t)
	uid := seedFileUser(t, db, 0)
	caller := mkFileCaller(uid)

	base, opt := newFileTestProv(t)

	// ① 本地磁盘
	localSvc := NewFileService(db, base, opt)
	vLocal := put(t, localSvc, caller, "plan.txt", []byte("hello"), 5)
	require.Equal(t, int(model.FileStorageLocal), vLocal.Storage,
		"本地 provider 记的必须是 FileStorageLocal(=1)")

	// ② 名字是 "oss" 的 provider
	ossSvc := NewFileService(db, &namedProv{Provider: base, name: oss.ProviderNameOSS}, opt)
	vOSS := put(t, ossSvc, caller, "plan2.txt", []byte("hello"), 5)
	require.Equal(t, int(model.FileStorageOSS), vOSS.Storage,
		"OSS provider 记的必须是 FileStorageOSS(=2) —— storageOf 早先比的是 "+
			`"aliyun",与 aliyun provider 的 Name()("oss")永远不相等`)
}

// TestStorageEnumValues 锁住枚举取值。
//
// ⚠️ 前端按这两个数字判"本地 / OSS",改了一边就得出相反的结论。
// 前端那半的对照表在 web/src/constants/file.ts。
func TestStorageEnumValues(t *testing.T) {
	require.Equal(t, model.FileStorage(1), model.FileStorageLocal)
	require.Equal(t, model.FileStorage(2), model.FileStorageOSS)
}

// TestProviderNames_AreTheOnesServiceCompares provider 的名字必须与
// service 层拿来比较的那个一致。
//
// ⚠️ 这条守的是**跨包的名字面量**:provider 在 pkg/oss/{local,aliyun,mock},
// service 在 internal/service —— 两边各写一个字面量,改了一边另一边就对不上,
// 而且**不报错**(只是永远不相等,于是那个分支永远不成立)。
//
// aliyun provider 的 Name() 要真连 OSS 才能构造,那边由它自己的包测;
// 这里锁住 local / mock 两个能构造的,以及三个常量本身。
func TestProviderNames_AreTheOnesServiceCompares(t *testing.T) {
	p, _ := newFileTestProv(t)
	require.Equal(t, oss.ProviderNameLocal, p.Name())
	require.Equal(t, oss.ProviderNameMock, oss.NewMockProvider().Name())

	require.Equal(t, "oss", oss.ProviderNameOSS)
	require.Equal(t, "local", oss.ProviderNameLocal)
	require.Equal(t, "mock", oss.ProviderNameMock)
}
