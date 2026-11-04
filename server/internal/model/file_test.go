// 文件系统模块 model 单测(M79)
//
// ⚠️ **这里的索引测试一律断言"gorm 解析出来的索引",不断言 tag 字符串**。
// 这不是风格偏好,是 M79 真踩过的坑:索引名字叫
// `uk_folder_owner_biz_name`("owner" = user_id),tag 写得好好的,但 UserID 上
// **少了一个 `uniqueIndex:`**,gorm 就只拿另外两段建索引 —— 实际是
// `UNIQUE(biz, name)`。用 `assert.Contains(tag, "uniqueIndex:uk_...")` 去测的
// 话**照样绿**,因为 Biz/Name 两列的 tag 里确实有那个名字。
//
// 断言 tag 只能证明"我写了什么",证明不了"gorm 建了什么";而唯一索引这件事,
// 错的正好是后者。
package model

import (
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

// uniqueIndexes 返回 gorm 实际会建的 UNIQUE 索引:名字 → 列名列表
//
// ⚠️ `ParseIndexes` 就是 AutoMigrate 建索引时读的那份数据(`schema/index.go`
// 里 Migrator 走的是同一个函数),所以这里断言的就是真实产物。
func uniqueIndexes(t *testing.T, m any) map[string][]string {
	t.Helper()
	s, err := schema.Parse(m, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	out := map[string][]string{}
	for _, idx := range s.ParseIndexes() {
		if idx.Class != "UNIQUE" {
			continue
		}
		cols := make([]string, 0, len(idx.Fields))
		for _, f := range idx.Fields {
			cols = append(cols, f.DBName)
		}
		out[idx.Name] = cols
	}
	return out
}

// TestSysFolder_UniqueIndexIncludesOwner 防 M79 那个 bug 复发:
// 索引少一段 → 从"同用户内唯一"退化成"全平台唯一",用户 2 建同名文件夹必炸。
//
// ⚠️ **M125 起索引多了 parent_id 一段** —— 文件夹支持多级以后,
//
//	"同一个父级下不能有同名文件夹"才是正确的约束,而不是"这个用户下
//	全局不能有同名文件夹"(那会让 A/图纸 和 B/图纸 建不出来)。
//	**owner(user_id)那一段仍然必须在** —— 少它就退回全平台唯一。
func TestSysFolder_UniqueIndexIncludesOwner(t *testing.T) {
	idx := uniqueIndexes(t, &SysFolder{})

	got, ok := idx["uk_folder_owner_biz_parent_name"]
	require.True(t, ok, "应有 uk_folder_owner_biz_parent_name 唯一索引,实际索引=%v", idx)
	assert.Equal(t, []string{"user_id", "biz", "parent_id", "name"}, got,
		"联合唯一索引必须包含 user_id,否则不同用户的同名文件夹互相撞")

	// ⚠️ 这一条单独写出来,是因为它是最容易被"顺手加一列 index"改坏的:
	// user_id 除了进唯一索引,还要保留单独的普通索引给"某用户的全部文件夹"查。
	plain := map[string][]string{}
	s, err := schema.Parse(&SysFolder{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	for _, i := range s.ParseIndexes() {
		if i.Class != "UNIQUE" {
			plain[i.Name] = nil
		}
	}
	assert.Contains(t, plain, "idx_sys_folder_user_id",
		"user_id 还需要一个普通索引(列表按归属人查,不能靠唯一索引的前缀)")
}

// TestSysFolder_UniqueIndexHasNoDeletedAt 锁住"唯一索引不含 deleted_at"。
//
// 原因见 SysFolder 类型注释:SQL 里 UNIQUE 认为 NULL 互不相等,而活跃行的
// deleted_at 恰好是 NULL,加了它唯一性就完全不生效,且失败得毫无声响。
// 真要"软删后释放同名",靠 M80 软删时改名,不是靠索引。
func TestSysFolder_UniqueIndexHasNoDeletedAt(t *testing.T) {
	for name, cols := range uniqueIndexes(t, &SysFolder{}) {
		for _, c := range cols {
			assert.NotEqual(t, "deleted_at", c,
				"索引 %s 含 deleted_at → 活跃行之间不再互斥(实测连插两条都成功)", name)
		}
	}
}

// TestSysFile_UniqueKey file_key 必须是唯一的 —— 它是唯一指向存储字节的把手。
func TestSysFile_UniqueKey(t *testing.T) {
	idx := uniqueIndexes(t, &SysFile{})
	got, ok := idx["uk_sys_file_key"]
	require.True(t, ok, "file_key 应有唯一索引,实际索引=%v", idx)
	assert.Equal(t, []string{"file_key"}, got)
}

// TestSysFile_ColumnWidths 锁住两个"曾经被按别的表抄错"的宽度。
func TestSysFile_ColumnWidths(t *testing.T) {
	s, err := schema.Parse(&SysFile{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	width := map[string]int{}
	for _, f := range s.Fields {
		if f.Size > 0 {
			width[f.DBName] = f.Size
		}
	}
	// 设计文档 §4.2:key ≈ 260 字节,255 装不下。biz_digital_asset.file_key 是
	// 255,别拿那个的长度来套。
	assert.Equal(t, 512, width["file_key"], "file_key 必须 512,否则长路径被截断/报错")
	assert.Equal(t, 255, width["file_name"], "file_name 255")
}

// TestSysUploadSession_Columns 关键列存在(防重构漏字段)
func TestSysUploadSession_Columns(t *testing.T) {
	// ⚠️ 这里列的是**列名(snake_case)不是 Go 字段名** —— `f.DBName` 给的是
	// 落到 DB 的名字。列成 Go 名的话这个测试会因为"找不到 UploadID"而红,
	// 而列真的一个都不少,红得毫无意义(这条已经踩过一次)。
	required := []string{
		"upload_id", "file_key", "user_id", "folder_id", "file_name", "file_size",
		"content_type", "part_size", "status", "quota_held", "expires_at",
	}
	s, err := schema.Parse(&SysUploadSession{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	have := map[string]bool{}
	for _, f := range s.Fields {
		have[f.DBName] = true
	}
	for _, r := range required {
		assert.True(t, have[r], "SysUploadSession 应有列 %s", r)
	}
}

func TestSysUser_Quota(t *testing.T) {
	const gb = int64(1) << 30

	t.Run("0 配额 = 不限", func(t *testing.T) {
		// ⚠️ 这一支最容易漏:`quota=0` 不是"配额为零",是"不限额"。
		// 漏判的表现是所有默认账号一律传不了任何东西,报"超出配额"。
		u := &SysUser{StorageQuotaBytes: 0}
		assert.False(t, u.QuotaExceeded(0, 1<<62), "不限额时再大的写入也不该超")
		assert.Equal(t, int64(math.MaxInt64), u.QuotaRemaining(1<<62))

		// 负配额同理当"不限"处理(配置写错不该把所有人锁死)
		neg := &SysUser{StorageQuotaBytes: -1}
		assert.False(t, neg.QuotaExceeded(0, 100))
	})

	t.Run("有配额时按边界判", func(t *testing.T) {
		u := &SysUser{StorageQuotaBytes: 10 * gb}
		assert.False(t, u.QuotaExceeded(0, 10*gb), "正好等于配额 = 没超")
		assert.True(t, u.QuotaExceeded(1, 10*gb), "超 1 字节就要拒")
		assert.Equal(t, 10*gb-1, u.QuotaRemaining(1))
	})

	t.Run("已用量超了 → remaining 为负", func(t *testing.T) {
		// 配额被调小的场景。remaining 为负不是 bug,但调用方拿去当"还能传多少"
		// 用就会写出负数的上限,所以方法注释里写明了别拿它做展示。
		u := &SysUser{StorageQuotaBytes: 1 * gb}
		assert.True(t, u.QuotaRemaining(2*gb) < 0)
		assert.True(t, u.QuotaExceeded(2*gb, 1), "已经超了,再写必然超")
	})
}

func TestSysUser_QuotaColumnsRegistered(t *testing.T) {
	s, err := schema.Parse(&SysUser{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	names := map[string]schema.Field{}
	for _, f := range s.Fields {
		names[f.DBName] = *f
	}
	for _, c := range []string{"storage_used_bytes", "storage_quota_bytes"} {
		f, ok := names[c]
		require.True(t, ok, "sys_user 应有列 %s", c)
		// ⚠️ 断言的是 **Size 而不是 DataType**:gorm 对 int32/int64 的
		// DataType 都报 "int",宽度只体现在 Size 上。断 DataType 的话
		// int64 写成 int 一样绿 —— 而那正是会让 2GB 以上的用量溢出成负数的错。
		assert.Equal(t, int64(64), int64(f.Size), "%s 宽度必须是 64 位(字节数会超 32 位)", c)
		assert.Equal(t, "0", f.DefaultValue, "%s 默认 0", c)
	}
}

func TestUploadSession_IsExpired(t *testing.T) {
	base := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

	t.Run("零值永不判过期", func(t *testing.T) {
		// 没填 ExpiresAt 的行(老数据 / 建表前的行)如果被当成"已过期",
		// 清理任务会把它连同配额预扣一起清掉 —— 而 quota_held 也是 0 时
		// 恰好无害,但这个分支不该靠下游的巧合兜底。
		s := &SysUploadSession{}
		assert.False(t, s.IsExpired(base))
		assert.False(t, s.IsExpired(base.Add(9999*time.Hour)))
	})

	t.Run("边界是闭区间", func(t *testing.T) {
		s := &SysUploadSession{ExpiresAt: base}
		assert.True(t, s.IsExpired(base), "正好到点就算过期")
		assert.True(t, s.IsExpired(base.Add(time.Second)))
		assert.False(t, s.IsExpired(base.Add(-time.Second)))
	})
}

func TestUploadSession_Done(t *testing.T) {
	// 终结态的会话不该再被清理任务碰 —— 对它调 Abort 会报 NoSuchUpload,
	// 那种无意义的错误日志量大了会把真错误淹掉。
	assert.False(t, (&SysUploadSession{Status: UploadSessionActive}).Done())
	for _, st := range []UploadSessionStatus{
		UploadSessionDone, UploadSessionCanceled, UploadSessionExpired,
	} {
		assert.True(t, (&SysUploadSession{Status: st}).Done(), "状态 %d 应算已终结", st)
	}
}

// TestEnumZeroValues 零值必须落在"正常/进行中"上。
//
// AutoMigrate 只在**新表**上应用 struct tag 的 default;若哪天改成给已有表加
// 列,MySQL 走的是列级 DEFAULT 0,老行补的也是 0。所以 0 必须是合法状态,
// 不能是"未初始化"。
func TestEnumZeroValues(t *testing.T) {
	assert.Equal(t, FileStatus(0), FileStatusNormal, "0 必须是正常,否则存量行全是异常态")
	assert.Equal(t, FileStorage(1), FileStorageLocal)
	assert.Equal(t, FileStorage(2), FileStorageOSS)
	assert.Equal(t, UploadSessionStatus(0), UploadSessionActive)
	assert.Equal(t, UploadSessionStatus(1), UploadSessionDone)
	assert.Equal(t, UploadSessionStatus(2), UploadSessionCanceled)
	assert.Equal(t, UploadSessionStatus(3), UploadSessionExpired)
}

// TestStorageDefaultIsLocal SysFile.Storage 的 tag default 是 1(local)。
//
// 忘了写 default 会落成 0,而 0 不是任何合法存储后端 —— 读文件时 switch 落空,
// 只能返回"未知存储"错误,而且老数据全是这个状态。
func TestStorageDefaultIsLocal(t *testing.T) {
	s, err := schema.Parse(&SysFile{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	for _, f := range s.Fields {
		if f.DBName == "storage" {
			assert.Equal(t, "1", f.DefaultValue,
				"storage 默认必须是 1(local),0 不是合法枚举值")
			return
		}
	}
	t.Fatal("SysFile 应有 storage 列")
}

func TestTableNames(t *testing.T) {
	assert.Equal(t, "sys_folder", SysFolder{}.TableName())
	assert.Equal(t, "sys_file", SysFile{}.TableName())
	assert.Equal(t, "sys_upload_session", SysUploadSession{}.TableName())
}

/*
================================================================================
M79 探针记录(2026-10-22)
================================================================================
逐条"拆掉实现看测试会不会红"。三条全部实测 RED 后**无条件还原**,还原后跑全包绿。

| # | 探针改动                                    | 期望红的测试                        | 实测                                       |
|---|---------------------------------------------|-------------------------------------|--------------------------------------------|
| 1 | SysFolder.UserID 去掉 `uniqueIndex:...priority:1` | TestSysFolder_UniqueIndexIncludesOwner | RED "联合唯一索引必须包含 user_id..."      |
| 2 | SysFile.FileKey 去掉 `uniqueIndex:uk_sys_file_key`  | TestSysFile_UniqueKey                | RED                                        |
| 3 | QuotaExceeded 去掉 `if quota <= 0 { return false }` | TestSysUser_Quota/0_配额_=_不限     | RED "不限额时再大的写入也不该超"           |

⚠️ **探针 1 就是 M79 开发过程中真踩到的 bug 本体**,不是假想:索引名字叫
`uk_folder_owner_biz_name`,tag 看着齐全,实际 gorm 建出来的是 `UNIQUE(biz, name)`
—— UserID 上压根没有 `uniqueIndex:` tag。不拆开看解析结果,这个错能一路带到线上,
表现为"第二个用户建同名文件夹报 duplicate key",而报错里没有任何线索指向用户。

⚠️ **另一个非探针的实测**:`UNIQUE(a, b, deleted_at)` 在 SQLite 上对
`deleted_at IS NULL` 的行**不互斥** —— 同一组 (a,b) 连插两条都成功,rows=2。
这否掉了设计文档 §4.1 原写的索引方案,所以 §4.1 也同步改了。

已知盲区:`storage` 列的 tag default 断言读的是 gorm 解析值,**没有真建表**。
AutoMigrate 在真 DB 上把 default 写成什么,本测试证不到。
================================================================================
*/
