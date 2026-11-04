package jsonid

import (
	"database/sql/driver"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// devAdminIdentityID 是实测到的真实身份 ID(2026-10-21 从 /auth/login 响应里
// 取的原始字面量)。这个值 > 2^53,是整个问题的物证 —— 用它做断言,而不是用
// 一个"看起来够大"的数,否则哪天 ID 生成策略变了测试还绿着。
const devAdminIdentityID = uint64(94883951113533126)

func TestID_MarshalProducesString(t *testing.T) {
	b, err := json.Marshal(struct {
		ID ID `json:"id"`
	}{ID: ID(devAdminIdentityID)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"94883951113533126"}`, string(b),
		"ID 必须是字符串。裸数字会被 JS 截断 —— %d 超出 2^53", devAdminIdentityID)
}

// TestID_SurvivesJSRoundTrip 是本包存在的全部理由。
//
// 复刻 JS 的行为:JSON.parse 之后再回传。若序列化出来是数字,这一步就会
// 丢掉末 6 位,回传后端必然 404。
func TestID_SurvivesJSRoundTrip(t *testing.T) {
	type dto struct {
		ID ID `json:"id"`
	}
	b, err := json.Marshal(dto{ID: ID(devAdminIdentityID)})
	require.NoError(t, err)

	// 模拟 JS 端的 JSON.parse:只有当 wire 上是字符串时才能原样保留
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	wire := string(raw["id"])
	assert.Equal(t, `"94883951113533126"`, wire)

	// 模拟把解析结果回传给后端,再被后端解回来
	var got dto
	require.NoError(t, json.Unmarshal([]byte(`{"id":"94883951113533126"}`), &got))
	assert.Equal(t, uint64(devAdminIdentityID), got.ID.Uint64(),
		"回传后必须还是同一个 ID")
}

func TestID_UnmarshalAcceptsBothStringAndNumber(t *testing.T) {
	var fromString ID
	require.NoError(t, json.Unmarshal([]byte(`"94883951113533126"`), &fromString))
	assert.Equal(t, devAdminIdentityID, fromString.Uint64())

	// 裸数字也要能收:改造是分模块推进的,中途某个兄弟接口还没改时前端不能整片挂掉
	var fromNumber ID
	require.NoError(t, json.Unmarshal([]byte(`94883951113533126`), &fromNumber))
	assert.Equal(t, devAdminIdentityID, fromNumber.Uint64())
}

func TestID_UnmarshalRejectsGarbage(t *testing.T) {
	for _, bad := range []string{`"abc"`, `"12x"`, `"-"`, `""`} {
		var id ID
		err := json.Unmarshal([]byte(bad), &id)
		if bad == `""` {
			assert.NoError(t, err, "空串应等价于 0(ID 未设置)")
			assert.Equal(t, ID(0), id)
			continue
		}
		assert.Error(t, err, "%s 必须被拒,否则会静默变成一个不存在的 ID", bad)
	}
}

// TestID_NeverZeroMeansNeverEmitStringZero 不成立,反过来才要测:
// ID=0 必须序列化成 "0" 而不是省略 / 变成 null,否则前端 `Number(id)` 拿到 NaN。
func TestID_ZeroMarshalsAsStringZero(t *testing.T) {
	b, err := json.Marshal(struct {
		ID ID `json:"id"`
	}{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"0"}`, string(b))
}

func TestIDList_MarshalIsStringArray(t *testing.T) {
	l := FromUints([]uint64{94883951113533126, 1, 2})
	b, err := json.Marshal(l)
	require.NoError(t, err)
	assert.JSONEq(t, `["94883951113533126","1","2"]`, string(b))
}

// TestIDList_InStructWithBuiltinStringTagWouldNotWork 解释为什么需要 IDList。
//
// 内置的 `,string` tag 选项对切片**无效**(标准库只支持 string/float/int/bool
// 标量)。这个测试用真实行为钉住该事实:万一哪天有人"优化"成
// []uint64 + `,string`,这里会红。
func TestIDList_InStructWithBuiltinStringTagWouldNotWork(t *testing.T) {
	type badDTO struct {
		IDs []uint64 `json:"ids,string"`
	}
	b, err := json.Marshal(badDTO{IDs: []uint64{94883951113533126}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"ids":[94883951113533126]}`, string(b),
		"内置 ,string 对切片无效 —— 这正是 IDList 存在的理由。若此断言失败说明标准库行为变了,需重新评估")

	type goodDTO struct {
		IDs IDList `json:"ids"`
	}
	gb, err := json.Marshal(goodDTO{IDs: FromUints([]uint64{94883951113533126})})
	require.NoError(t, err)
	assert.JSONEq(t, `{"ids":["94883951113533126"]}`, string(gb))
}

func TestIDList_NullStaysNull(t *testing.T) {
	// null → nil,空数组 → 空 slice。前端据此区分"不过滤"与"过滤到空"。
	var nullList IDList
	require.NoError(t, json.Unmarshal([]byte(`null`), &nullList))
	assert.Nil(t, nullList)

	var emptyList IDList
	require.NoError(t, json.Unmarshal([]byte(`[]`), &emptyList))
	assert.NotNil(t, emptyList)
	assert.Len(t, emptyList, 0)

	b1, _ := json.Marshal(nullList)
	b2, _ := json.Marshal(emptyList)
	assert.Equal(t, "null", string(b1))
	assert.Equal(t, "[]", string(b2))
}

func TestIDList_UnmarshalRejectsNonArray(t *testing.T) {
	var l IDList
	assert.Error(t, json.Unmarshal([]byte(`"abc"`), &l))
}

func TestConversions(t *testing.T) {
	assert.Equal(t, devAdminIdentityID, FromUint(devAdminIdentityID).Uint64())
	assert.Equal(t, []uint64{1, 2, 3}, FromUints([]uint64{1, 2, 3}).Uints())
	// nil list 转换后是空 slice(不是 nil),给 GORM 的 WHERE IN 用
	assert.Empty(t, IDList(nil).Uints())
	assert.Equal(t, IDList{1, 2}, FromUints([]uint64{1, 2}))
}

// TestStringTagOption_RoundTripsAndKeepsOmitEmpty 锁住内置 `,string` 方案。
//
// 结论:对**标量** ID,`json:"xxx_id,string"` 完全可以替代 jsonid.ID ——
//   - 出参是字符串(JS 不会被截断)
//   - 入参能收字符串(前端回传不会 400)
//   - omitempty 仍然生效(0 值照样省略)
//   - **不碰 Go 类型** → GORM / service 层一行都不用改
//
// 这也是本次改造的主力手段:235 个字段里绝大多数是标量,改 tag 是纯机械操作,
// 换成 jsonid.ID 会把整条 service→model→GORM 链路全拖下水。
//
// 反过来,切片(`[]uint64`)打 `,string` 是**无效**的,那才是 IDList 的用武之地,
// 见 TestIDList_InStructWithBuiltinStringTagWouldNotWork。
func TestStringTagOption_RoundTripsAndKeepsOmitEmpty(t *testing.T) {
	type dto struct {
		IdentityID uint64 `json:"identity_id,string"`
		ProjectID  uint64 `json:"project_id,string,omitempty"`
	}

	// 出参:大 ID 必须是字符串
	b, err := json.Marshal(dto{IdentityID: devAdminIdentityID, ProjectID: 42})
	require.NoError(t, err)
	assert.JSONEq(t, `{"identity_id":"94883951113533126","project_id":"42"}`, string(b))

	// 入参:字符串要能解回 uint64
	var got dto
	require.NoError(t, json.Unmarshal([]byte(`{"identity_id":"94883951113533126"}`), &got))
	assert.Equal(t, devAdminIdentityID, got.IdentityID)
	assert.Equal(t, uint64(0), got.ProjectID)

	// omitempty 没被 ,string 破坏 —— 这一条不测就会有人担心而不敢用
	b2, err := json.Marshal(dto{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"identity_id":"0"}`, string(b2),
		"omitempty 字段应被省略;identity_id 没有 omitempty,零值应序列化成 \"0\"")
}

// TestID_IsUsableAsSQLParameter 证明 jsonid.ID 能直接喂给 GORM。
//
// 为什么需要这条:UserListQuery.ProjectIDs 换成了 jsonid.IDList,用法是
// Where("project_id IN ?", q.ProjectIDs)。GORM 遇到 IN ? 会把切片**逐元素**
// 当 driver.Value 传给 database/sql,而自定义类型能否过这一关是运行时的事,
// 编译期完全看不出来 —— 类型能赋值 != 能落库。
//
// 结论(实测):能过。database/sql 的 defaultConverter 对"底层是整数的具名
// 类型"有反射兜底,所以 ID 不必实现 driver.Valuer。这条测试守住这个事实 ——
// 免得哪天有人看到"自定义类型"就条件反射地加一个 driver.Valuer。
func TestID_IsUsableAsSQLParameter(t *testing.T) {
	conv := driver.DefaultParameterConverter

	v, err := conv.ConvertValue(ID(devAdminIdentityID))
	require.NoError(t, err)
	assert.Equal(t, int64(devAdminIdentityID), v)

	list := FromUints([]uint64{devAdminIdentityID, 2})
	require.Len(t, list, 2)
	for i, id := range list {
		v, err := conv.ConvertValue(id)
		require.NoError(t, err, "IDList[%d] 不能作为 SQL 参数", i)
		assert.Equal(t, int64(id.Uint64()), v)
	}

	// 反射兜底的证据:另一个同样"底层是整数"的具名类型也一样能过
	type bareID uint64
	v2, err := conv.ConvertValue(bareID(7))
	require.NoError(t, err, "database/sql 不再对整型具名类型做反射兜底了")
	assert.Equal(t, int64(7), v2)
}
