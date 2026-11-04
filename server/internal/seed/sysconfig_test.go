// 平台配置 seed 的特征测试 —— 单桶模型(2026-10-30)
//
// ## 这条网守的是什么
//
//	文件存储从"admin/public 双桶"改成"单桶"之后,`mustSysConfigs` 里的
//	`aliyun.oss.bucket_admin` / `aliyun.oss.bucket_public` 退役了,换成
//	`aliyun.oss.bucket` 一个键。
//
//	但 **seed 的策略是"建缺失的、不动已存在的"** —— 所以老库里那两行
//	会**永远留着**,配置中心照常把它们显示出来,读配置的人会以为
//	"还在生效",而服务端早就不认了(而且这两个键一改就会让人以为改了桶)。
//
//	症状是零的:没有任何报错,只是配置中心里多两行"僵尸配置"。
//
// ## 可逆探针(拆掉实现看会不会红)
//
//	· 把 pruneRetiredSysConfigs 的调用去掉 → 「退役键被清掉」红;
//	· 把 retiredSysConfigKeys 清空 → 同上红。
package seed

import (
	"testing"

	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
)

// mustSysConfigKeys 当前必须存在的配置键集合
func mustSysConfigKeys() map[string]bool {
	m := make(map[string]bool, len(mustSysConfigs))
	for _, c := range mustSysConfigs {
		m[c.Key] = true
	}
	return m
}

// TestMustSysConfigs_SingleBucket 特征:OSS 只有**一个**桶。
//
// ⚠️ 这一条钉的是"契约本身" —— 双桶模型那两个键必须不在 seed 清单里,
// 否则就等于把已经退役的配置又请回来。
func TestMustSysConfigs_SingleBucket(t *testing.T) {
	keys := mustSysConfigKeys()

	if !keys["aliyun.oss.bucket"] {
		t.Error("mustSysConfigs 里没有 aliyun.oss.bucket —— 单桶模型下这个键必须在")
	}
	for _, retired := range []string{"aliyun.oss.bucket_admin", "aliyun.oss.bucket_public"} {
		if keys[retired] {
			t.Errorf("mustSysConfigs 里还有 %s —— 双桶已经退役了", retired)
		}
	}
}

// TestRetiredSysConfigKeys_AreNotInMustList 退役清单和 seed 清单不能有交集。
//
// ⚠️ 否则 prune 会**先把刚建好的行删掉**(顺序是 prune 在建之前),于是
// `aliyun.oss.bucket` 建了又被删 —— 症状是"配置中心里没有桶这一项",
// 而 seed 的日志还打着 "upserted: N"。
func TestRetiredSysConfigKeys_AreNotInMustList(t *testing.T) {
	keys := mustSysConfigKeys()
	for _, k := range retiredSysConfigKeys {
		if keys[k] {
			t.Errorf("%s 同时在 mustSysConfigs 和 retiredSysConfigKeys 里 —— prune 会把它建完又删掉", k)
		}
	}
}

// TestSeedAllSysConfigs_PrunesRetiredKeys 特征:
// **老库里那两行僵尸配置会被清掉**。
func TestSeedAllSysConfigs_PrunesRetiredKeys(t *testing.T) {
	db := newSeedTestDB(t)

	// 造一个"老库"的样子:退役键已经在库里了
	for i, k := range retiredSysConfigKeys {
		require.NoError(t, db.Create(&model.SysConfig{
			BaseModel:    model.BaseModel{ID: uint64(9001 + i)},
			ConfigKey:    k,
			ConfigGroup:  "aliyun.oss",
			ConfigName:   "老配置",
			ValueType:    model.ConfigValueType("string"),
			ValuePreview: "old",
			IsActive:     true,
		}).Error)
	}

	require.NoError(t, seedAllSysConfigs(db))

	for _, k := range retiredSysConfigKeys {
		var n int64
		require.NoError(t, db.Unscoped().Model(&model.SysConfig{}).
			Where("config_key = ?", k).Count(&n).Error)
		require.Equal(t, int64(0), n, "%s 已经退役,不该还留在库里", k)
	}

	// 新的单桶键必须建出来
	var n int64
	require.NoError(t, db.Model(&model.SysConfig{}).
		Where("config_key = ?", "aliyun.oss.bucket").Count(&n).Error)
	require.Equal(t, int64(1), n, "单桶模型下 aliyun.oss.bucket 必须被 seed 出来")
}

// TestSeedAllSysConfigs_DoesNotOverwriteExisting 幂等 + 不覆盖运营改过的值。
//
// ⚠️ 这条是 prune 的**副作用守卫**:prune 只能删"退役清单里点名的键",
// 别的键一律不许动 —— 运营自己加的配置项、或者改过值的键,都不能被冲掉。
func TestSeedAllSysConfigs_DoesNotOverwriteExisting(t *testing.T) {
	db := newSeedTestDB(t)

	// 一个"运营改过的"键
	require.NoError(t, db.Create(&model.SysConfig{
		BaseModel:    model.BaseModel{ID: 9002},
		ConfigKey:    "aliyun.oss.bucket",
		ConfigGroup:  "aliyun.oss",
		ConfigName:   "桶",
		ValueType:    model.ConfigValueType("string"),
		ValuePreview: "运营改过的值",
		IsActive:     true,
	}).Error)
	// 一个跟 seed 无关的自定义键
	require.NoError(t, db.Create(&model.SysConfig{
		BaseModel:    model.BaseModel{ID: 9003},
		ConfigKey:    "custom.ops.note",
		ConfigGroup:  "custom",
		ConfigName:   "自定义",
		ValueType:    model.ConfigValueType("string"),
		ValuePreview: "别动我",
		IsActive:     true,
	}).Error)

	require.NoError(t, seedAllSysConfigs(db))

	var cfg model.SysConfig
	require.NoError(t, db.Where("config_key = ?", "aliyun.oss.bucket").First(&cfg).Error)
	require.Equal(t, "运营改过的值", cfg.ValuePreview, "seed 不许覆盖已存在的值")

	var custom model.SysConfig
	require.NoError(t, db.Where("config_key = ?", "custom.ops.note").First(&custom).Error)
	require.Equal(t, "别动我", custom.ValuePreview, "不在退役清单里的键一律不许动")
}

// TestPruneRetiredSysConfigs_Idempotent 连跑两次不报错、也不重复删。
func TestPruneRetiredSysConfigs_Idempotent(t *testing.T) {
	db := newSeedTestDB(t)
	require.NoError(t, pruneRetiredSysConfigs(db))
	require.NoError(t, pruneRetiredSysConfigs(db))

	var n int64
	require.NoError(t, db.Unscoped().Model(&model.SysConfig{}).Count(&n).Error)
	require.Equal(t, int64(0), n)
}
