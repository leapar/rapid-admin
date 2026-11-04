// 字典缓存这一层的回归网 —— M95
//
// ⚠️ **这里守的是"缓存"本身**:key 方案、失效范围、nil 安全、uint64 精度、
//
//	"查全部"不与真的组名撞车。
//
//	业务接线(读走缓存 / 写完失效 / 失效早于广播)在
//	`role_dict_cache_test.go` 里守。两份网分工明确:
//	这一份换掉缓存实现就红,那一份换掉 service 接线就红。
//
// ⚠️ **用内存后端而不是 mock** —— 内存后端实现了 `cachex.Scanner`,
//
//	所以**前缀失效这条真路径**在单测里是真跑过的。
//	把 Scan mock 掉的话,失效会变成"永远成功"的假绿。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/seed"
)

// newTestDictCache 装一个内存后端的字典缓存 —— 单测不连 Redis。
func newTestDictCache() (*DictCache, *cachex.MemoryBackend) {
	backend := cachex.NewMemoryBackend()
	return NewDictCache(cachex.NewWithBackend(backend)), backend
}

// ⚠️ **key 方案是"写"和"删"之间唯一的约定** —— 改 key 不改失效前缀,
//
//	症状是"写进去的永远删不掉"(陈旧数据直到 TTL 过期),而且**没有任何报错**。
func TestM95_DictCache_KeyScheme(t *testing.T) {
	c, _ := newTestDictCache()

	assert.Equal(t, "dict:items:issue_status", c.itemsKey("issue_status"))
	// ⚠️ **"查全部"是空后缀,不是 `dict:items:all`** ——
	//   后者会和一个真叫 "all" 的 group_code 撞车。
	assert.Equal(t, "dict:items:", c.itemsKey(""))
	assert.Equal(t, "dict:groups", c.groupsKey())

	// 所有 key 都得落在失效前缀底下,否则失效扫不到它
	for _, k := range []string{c.itemsKey("issue_status"), c.itemsKey(""), c.groupsKey()} {
		assert.True(t, strings.HasPrefix(k, dictCacheKeyPrefix),
			"key %q 必须落在失效前缀 %q 底下", k, dictCacheKeyPrefix)
	}
}

// ⚠️ **nil 缓存 = 没有缓存,不是错误** —— 单测里不装缓存是常态。
func TestM95_DictCache_NilCache_Passthrough(t *testing.T) {
	var c *DictCache
	calls := 0
	loader := func(ctx context.Context) ([]model.SysDict, error) {
		calls++
		return []model.SysDict{{ItemKey: "open"}}, nil
	}

	rows, err := c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// 再来一次还是回源 —— 没有缓存可命中
	_, err = c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)

	// 失效也不能炸
	require.NoError(t, c.Invalidate(context.Background()))
}

// 读路径:第二次命中缓存,loader 不再被调
func TestM95_DictCache_Items_CachesSecondCall(t *testing.T) {
	c, _ := newTestDictCache()
	calls := 0
	loader := func(ctx context.Context) ([]model.SysDict, error) {
		calls++
		return []model.SysDict{{
			BaseModel: model.BaseModel{ID: testDictID},
			GroupCode: "issue_status",
			ItemKey:   "open",
			Label:     "待处理",
		}}, nil
	}

	first, err := c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)
	second, err := c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)

	assert.Equal(t, 1, calls, "第二次必须命中缓存")
	assert.Equal(t, first, second)
}

// ⚠️ **uint64 精度不能丢** —— `SysDict.ID` 是 uint64 且 json tag 是 `id,string`。
//
//	2^53 以上的 ID 一旦掉精度,前端拿到的就是另一个字典项。
//	(这也是"字典走 L2-only 而不是 `GetOrLoadTyped`"的理由:any 往返会掉。)
func TestM95_DictCache_Items_PreservesUint64ID(t *testing.T) {
	c, _ := newTestDictCache()
	big := uint64(96413000000000001) // > 2^53
	require.True(t, big > uint64(1)<<53, "这条测试的前提:ID 超过 2^53")

	loader := func(ctx context.Context) ([]model.SysDict, error) {
		return []model.SysDict{{
			BaseModel: model.BaseModel{ID: big},
			GroupCode: "issue_status",
			ItemKey:   "open",
		}}, nil
	}

	// 第一次回源
	_, err := c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)
	// 第二次命中缓存 —— 精度要是掉在这一步
	cached, err := c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)

	require.Len(t, cached, 1)
	assert.Equal(t, big, cached[0].ID, "缓存往返不能掉 uint64 精度")
}

// ⚠️ **"查全部"的 key 不能和真的组名撞车** ——
//
//	若"查全部"用 `dict:items:all`,一个真叫 "all" 的组就会跟它抢同一个 key,
//	症状是"下拉里混进了别的东西"。
func TestM95_DictCache_AllKey_DoesNotCollideWithGroupNamedAll(t *testing.T) {
	c, _ := newTestDictCache()
	allCalls, groupCalls := 0, 0

	_, err := c.Items(context.Background(), "", func(ctx context.Context) ([]model.SysDict, error) {
		allCalls++
		return []model.SysDict{
			{GroupCode: "aaa", ItemKey: "one"},
			{GroupCode: "all", ItemKey: "two"},
		}, nil
	})
	require.NoError(t, err)

	_, err = c.Items(context.Background(), "all", func(ctx context.Context) ([]model.SysDict, error) {
		groupCalls++
		return []model.SysDict{{GroupCode: "all", ItemKey: "two"}}, nil
	})
	require.NoError(t, err)

	// 两次都真的回源了 —— 说明是两个不同的 key
	assert.Equal(t, 1, allCalls)
	assert.Equal(t, 1, groupCalls)

	// 再读一次:各自的缓存都得是自己的内容
	all, err := c.Items(context.Background(), "", func(ctx context.Context) ([]model.SysDict, error) {
		allCalls++
		return nil, nil
	})
	require.NoError(t, err)
	group, err := c.Items(context.Background(), "all", func(ctx context.Context) ([]model.SysDict, error) {
		groupCalls++
		return nil, nil
	})
	require.NoError(t, err)

	require.Len(t, all, 2, "查全部的缓存不能被 'all' 组覆盖")
	require.Len(t, group, 1, "'all' 组的缓存不能被查全部覆盖")
	assert.Equal(t, 1, allCalls, "命中缓存就不该再回源")
	assert.Equal(t, 1, groupCalls)
}

// ⚠️ **超长 group_code 不进缓存** —— `sys_dict.group_code` 列宽 64,
//
//	超过这个长度必然查不到任何东西,给它建缓存条目是白占 Redis 内存。
func TestM95_DictCache_OversizedGroupCode_BypassesCache(t *testing.T) {
	c, backend := newTestDictCache()
	long := strings.Repeat("x", dictCacheMaxGroupLen+1)
	calls := 0
	loader := func(ctx context.Context) ([]model.SysDict, error) {
		calls++
		return nil, nil
	}

	for i := 0; i < 3; i++ {
		_, err := c.Items(context.Background(), long, loader)
		require.NoError(t, err)
	}
	assert.Equal(t, 3, calls, "超长 group_code 必须每次回源")
	assert.Equal(t, 0, backend.Len(), "超长 group_code 不该产生缓存条目")
}

// 失效:一次把"字典"这一族全清掉(字典项 + 查全部 + 分组列表)
func TestM95_DictCache_Invalidate_ClearsDictFamily(t *testing.T) {
	c, backend := newTestDictCache()
	calls := 0
	itemsLoader := func(ctx context.Context) ([]model.SysDict, error) {
		calls++
		return []model.SysDict{{GroupCode: "issue_status"}}, nil
	}
	groupsLoader := func(ctx context.Context) ([]seed.DictGroupKV, error) {
		calls++
		return []seed.DictGroupKV{{Code: "issue_status", Label: "问题状态"}}, nil
	}

	_, err := c.Items(context.Background(), "issue_status", itemsLoader)
	require.NoError(t, err)
	_, err = c.Items(context.Background(), "", itemsLoader)
	require.NoError(t, err)
	_, err = c.Groups(context.Background(), groupsLoader)
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	require.Greater(t, backend.Len(), 0)

	require.NoError(t, c.Invalidate(context.Background()))

	// 失效之后再读,三个都得重新回源
	_, err = c.Items(context.Background(), "issue_status", itemsLoader)
	require.NoError(t, err)
	_, err = c.Items(context.Background(), "", itemsLoader)
	require.NoError(t, err)
	_, err = c.Groups(context.Background(), groupsLoader)
	require.NoError(t, err)
	assert.Equal(t, 6, calls, "失效之后必须重新回源")
}

// ⚠️ **失效只删"字典"这一族** —— 别的前缀不能被牵连。
func TestM95_DictCache_Invalidate_OnlyTouchesDictPrefix(t *testing.T) {
	c, backend := newTestDictCache()
	require.NoError(t, backend.Set(context.Background(), "other:key", "v", time.Minute))

	_, err := c.Items(context.Background(), "issue_status", func(ctx context.Context) ([]model.SysDict, error) {
		return nil, nil
	})
	require.NoError(t, err)

	require.NoError(t, c.Invalidate(context.Background()))

	// 别的业务的 key 还在
	v, err := backend.Get(context.Background(), "other:key")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

// ⚠️ **没有 L2 时失效不是错误** —— 没配 Redis / `cache.mode=off` 时
//
//	本来就没有缓存,没有东西可失效。
func TestM95_DictCache_NoBackend_Invalidate_NoError(t *testing.T) {
	c := NewDictCache(nil)
	require.NoError(t, c.Invalidate(context.Background()))

	// 读路径也退化成每次回源
	calls := 0
	loader := func(ctx context.Context) ([]model.SysDict, error) {
		calls++
		return nil, nil
	}
	_, err := c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)
	_, err = c.Items(context.Background(), "issue_status", loader)
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

// ⚠️ **缓存不改变函数的成败语义** —— loader 的错误一律透传。
func TestM95_DictCache_Items_PropagatesLoaderError(t *testing.T) {
	c, _ := newTestDictCache()
	boom := errors.New("db down")

	_, err := c.Items(context.Background(), "issue_status", func(ctx context.Context) ([]model.SysDict, error) {
		return nil, boom
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom, "缓存不能吞掉业务错误")
}
