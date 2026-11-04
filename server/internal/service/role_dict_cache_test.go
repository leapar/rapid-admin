// 字典「读走缓存 / 写完失效」的业务路径回归网 —— M95
//
// ⚠️ **这里守的是 service 的接线**,不是缓存那一层:
//
//	`dict_cache_test.go` 守缓存本身(key 方案 / 失效范围 / nil 安全),
//	这一份守"读路径真的走了缓存"、"写路径真的做了失效"、
//	以及**失效早于广播**这个顺序。
//
// ⚠️ **为什么顺序要单测守着** —— M94 让前端在收到 `dict.changed` 后立刻重拉。
//
//	若此刻后端还吐着缓存里的旧字典,前端重拉回来的就是旧值:
//	症状是"改了不生效",正是 M94 要修的那个,而且这次还多了一层
//	"我明明让它刷新了"。这个 bug 不报错、不白屏,只能靠单测拦。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
)

// capturingPublisher 假 publisher —— 广播时**同步**读一次字典,
// 把"广播那一刻前端能读到什么"记下来。
//
// ⚠️ **必须同步读** —— roleService 是同步调 `PublishDictChanged` 的
//
//	(只有 push 实现自己起 goroutine),所以这里读到的就是
//	"广播发出那一刻"的状态,顺序断言才立得住。
type capturingPublisher struct {
	// list 读字典的入口(装配完 svc 之后回填)
	list func(ctx context.Context, groupCode string) ([]model.SysDict, error)
	// seen 每次广播时读到的 label
	seen []string
}

func (p *capturingPublisher) PublishDictChanged(ctx context.Context, groupCode string) {
	rows, err := p.list(ctx, groupCode)
	if err != nil {
		p.seen = append(p.seen, "ERR:"+err.Error())
		return
	}
	if len(rows) == 0 {
		p.seen = append(p.seen, "")
		return
	}
	p.seen = append(p.seen, rows[0].Label)
}

// ⚠️ **读路径必须真的走缓存** —— 直接改库(绕开 service,也就是绕开失效)
//
//	之后读出来的还是旧值,这才证明缓存生效了;
//	走 service 改(会触发失效)之后又能看到新值,证明写路径做了失效。
func TestM95_ListDict_CachesAndUpdateDict_Invalidates(t *testing.T) {
	db := newDictTestDB(t)
	seedDictItem(t, db, "user_status", "active", "启用")

	cache, _ := newTestDictCache()
	svc := NewRoleService(db, nil, cache)

	// 第一次读:回源
	rows, err := svc.ListDict(context.Background(), "user_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "启用", rows[0].Label)

	// ⚠️ **直接改库**(绕开 service,也就绕开了失效)
	require.NoError(t, db.Model(&model.SysDict{}).Where("id = ?", testDictID).
		Update("label", "库外改的").Error)

	// 再读:还是旧值 —— 证明走的是缓存
	rows, err = svc.ListDict(context.Background(), "user_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "启用", rows[0].Label, "读路径必须命中缓存(直接改库看不到变化)")

	// 走 service 改 → 失效 → 再读就是新值
	//
	// ⚠️ **is_active 要显式带上** —— service 的更新组装规则是
	//   "is_active 跟旧值不同才写"(见 UpdateDict),零值 false 会被判成
	//   "要下线这一项"。前端表单本来就 always 提交 is_active,这里跟它对齐。
	err = svc.UpdateDict(context.Background(), &Caller{UserID: 1}, testDictID, &model.SysDict{
		Label:    "已改名",
		IsActive: true,
	})
	require.NoError(t, err)

	rows, err = svc.ListDict(context.Background(), "user_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "已改名", rows[0].Label, "写路径必须让缓存失效")
}

// 🔴 **失效必须早于广播** —— 这是 M94「改了马上生效」的前置条件。
//
// 假 publisher 在广播那一刻同步读一次字典:读到新值说明失效先于广播发生了,
// 读到旧值说明顺序反了(前端会拿着旧值重拉,然后把旧值又写回缓存)。
func TestM95_UpdateDict_InvalidatesBeforePublish(t *testing.T) {
	db := newDictTestDB(t)
	seedDictItem(t, db, "user_status", "active", "启用")

	cache, _ := newTestDictCache()
	pub := &capturingPublisher{}
	svc := NewRoleService(db, pub, cache)
	pub.list = svc.ListDict

	// 先读一次,把旧值放进缓存
	rows, err := svc.ListDict(context.Background(), "user_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "启用", rows[0].Label)

	err = svc.UpdateDict(context.Background(), &Caller{UserID: 1}, testDictID, &model.SysDict{
		Label:    "已改名",
		IsActive: true,
	})
	require.NoError(t, err)

	require.Len(t, pub.seen, 1, "改完字典必须广播一次")
	assert.Equal(t, "已改名", pub.seen[0],
		"广播那一刻读到的必须是新值 —— 失效必须早于广播")
}

// ⚠️ **没装缓存 = 退化成每次回源**,不是报错。
//
//	单测 / 单机开发 / 没配 Redis 时都走这条路径。
func TestM95_NoCache_BehaviorUnchanged(t *testing.T) {
	db := newDictTestDB(t)
	seedDictItem(t, db, "user_status", "active", "启用")

	svc := NewRoleService(db, nil, nil)

	rows, err := svc.ListDict(context.Background(), "user_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// 直接改库 → 立刻可见(没有缓存这一层)
	require.NoError(t, db.Model(&model.SysDict{}).Where("id = ?", testDictID).
		Update("label", "库外改的").Error)
	rows, err = svc.ListDict(context.Background(), "user_status")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "库外改的", rows[0].Label, "没装缓存时必须每次回源")

	// 改完也不该炸(失效路径是空操作)
	require.NoError(t, svc.UpdateDict(context.Background(), &Caller{UserID: 1}, testDictID,
		&model.SysDict{Label: "已改名", IsActive: true}))
}

// 分组列表也走缓存 —— 它和字典项共用同一个失效前缀(`dict:`)。
func TestM95_ListDictGroups_CachesAndInvalidates(t *testing.T) {
	db := newDictTestDB(t)
	seedDictItem(t, db, "user_status", "active", "启用")

	cache, _ := newTestDictCache()
	svc := NewRoleService(db, nil, cache)

	groups, err := svc.ListDictGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, "user_status", groups[0].Code)

	// ⚠️ **直接加一条别的组**(绕开 service,也就绕开了失效)
	require.NoError(t, db.Create(&model.SysDict{
		BaseModel: model.BaseModel{ID: testDictID + 1},
		GroupCode: "role_type",
		ItemKey:   "platform",
		ItemValue: "platform",
		Label:     "平台级",
		Sort:      1,
		IsActive:  true,
	}).Error)

	groups, err = svc.ListDictGroups(context.Background())
	require.NoError(t, err)
	assert.Len(t, groups, 1, "分组列表必须命中缓存(直接改库看不到变化)")

	// 走 service 改 → 失效 → 分组列表重新算
	//
	// ⚠️ **is_active 必须显式带 true** —— 零值 false 会被 service 判成
	//   "要下线这一项",那一项就从分组列表里消失了(分组列表只列 active 的组),
	//   断言就会数出错的组数。前端表单本来就 always 提交 is_active。
	require.NoError(t, svc.UpdateDict(context.Background(), &Caller{UserID: 1}, testDictID,
		&model.SysDict{Label: "已改名", IsActive: true}))

	groups, err = svc.ListDictGroups(context.Background())
	require.NoError(t, err)
	assert.Len(t, groups, 2, "写路径必须让分组列表的缓存也失效")
}
