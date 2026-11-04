package v1

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/service"
)

// realIdentityID 是 2026-10-21 真机复现身份切换 404 时抓到的原始身份 ID。
//
// 用真实值而不是"看起来够大"的数:这个 bug 的表现取决于具体数值,换个量级
// 说不定就不触发了,而测试还绿着 —— 那就等于没测。
const realIdentityID = uint64(94883951113533126)

// TestJS_TruncatesBareID_这是整轮改造的根因
//
// 这条测试**不测我们的代码**,它测的是 JS 语言本身,作用是把"为什么必须改"
// 钉死成可执行的事实,而不是注释里的一句话。
//
// JSON.parse 把所有数字变成 float64,超过 2^53 的部分静默丢失 —— 不报错、
// 不告警,只在"把这个 id 发回后端"的那一刻变成 404。
func TestJS_TruncatesBareID_这是整轮改造的根因(t *testing.T) {
	const maxSafeInteger = uint64(9007199254740991) // 2^53-1,JS Number 的精确上限

	assert.Less(t, maxSafeInteger, realIdentityID, "真实 ID 超出 JS 安全整数上限")

	// JS 的做法:JSON.parse 之后这个值是个 float64,拿它再变回整数就变了
	roundTripped := uint64(float64(realIdentityID))
	assert.NotEqual(t, realIdentityID, roundTripped,
		"裸整数上 wire 必然被截断 —— 如果这条断言失败了,说明 ID 生成策略变了,本轮改造的前提需要重估")

	// 具体丢多少:末 6 位
	assert.Equal(t, uint64(6), realIdentityID-roundTripped, "末 6 位被吃掉")

	// 字符串形式则一个字节都不丢 —— 这就是改成 ,string 的全部理由
	var asString string
	require.NoError(t, json.Unmarshal([]byte(`"`+itoa(realIdentityID)+`"`), &asString))
	assert.Equal(t, itoa(realIdentityID), asString)
}

// TestAuthTokens_AllIDsAreStringsOnTheWire 响应侧:
// 登录/续期/切换三个端点共用 service.AuthTokens,ID 必须是字符串。
func TestAuthTokens_AllIDsAreStringsOnTheWire(t *testing.T) {
	tokens := &service.AuthTokens{
		Token:            "access.jwt.value",
		RefreshToken:     "refresh.jwt.value",
		ExpiresIn:        28800,
		RefreshExpiresIn: 2592000,
		User: &service.CurrentUserView{
			ID:                realIdentityID,
			CurrentIdentityID: realIdentityID,
			Identities: []service.IdentityView{
				{ID: realIdentityID, RoleCode: "role_super_admin", IsPrimary: true},
			},
		},
	}

	raw, err := json.Marshal(tokens)
	require.NoError(t, err)

	// UseNumber 保住字面量 —— 默认解成 float64 就把我们要验的东西毁掉了,
	// 那正是浏览器里发生的事。
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var m map[string]any
	require.NoError(t, dec.Decode(&m))

	// 断言"是字符串":解出来是 json.Number 就说明 wire 上是裸数字,JS 会截断
	assertIDIsString := func(path string, v any) {
		t.Helper()
		if n, bad := v.(json.Number); bad {
			assert.Fail(t, path+" 在 wire 上是裸数字,JS 会截断", "字面量 %s", n.String())
			return
		}
		s, ok := v.(string)
		if !ok {
			assert.Fail(t, path+" 类型不对", "实际 %T,值 %v", v, v)
			return
		}
		assert.Equal(t, itoa(realIdentityID), s, path+" 精度丢失")
	}

	user := m["user"].(map[string]any)
	assertIDIsString("user.id", user["id"])
	assertIDIsString("user.current_identity_id", user["current_identity_id"])

	identities := user["identities"].([]any)
	for i, idt := range identities {
		assertIDIsString("user.identities["+itoa(uint64(i))+"].id", idt.(map[string]any)["id"])
	}

	// 正面确认:字符串字面量原样保留,精度一点没丢
	assert.Contains(t, string(raw), `"id":"`+itoa(realIdentityID)+`"`,
		"19 位 ID 必须原样出现在 JSON 文本里")
}

// TestSwitchIdentityRequest_BindsStringIDLosslessly 请求侧:
// 前端把字符串 id 发回来,必须一位不差地解成原值。
//
// 这是用户报的 bug 的**直接现场**:之前 identity_id 是裸 uint64,前端拿到的
// 是被截断的值,回传必然 404(40400 身份不存在)。
func TestSwitchIdentityRequest_BindsStringIDLosslessly(t *testing.T) {
	body := `{"identity_id":"` + itoa(realIdentityID) + `","sms_code":"123456"}`

	var req SwitchIdentityRequest
	require.NoError(t, json.Unmarshal([]byte(body), &req),
		"字符串 identity_id 必须能绑定 —— 绑不上就是身份切换必然 404")

	assert.Equal(t, realIdentityID, req.IdentityID,
		"19 位 ID 经一次 JSON 往返后必须逐位相等")
	assert.Equal(t, "123456", req.SmsCode)
}

// TestSwitchIdentityRequest_RejectsBareNumber 把契约的另一面钉死:
// 迁移完成后 wire 上只认字符串,裸数字会被拒。
//
// 为什么"拒"比"兼容"好:静默接受裸数字看起来更宽容,但它会让"前端忘了
// String()"这类错误在开发环境完全看不出来 —— 等哪天 ID 量级或客户端变了
// 又突然截断。响亮失败在这里是对的。
func TestSwitchIdentityRequest_RejectsBareNumber(t *testing.T) {
	body := `{"identity_id":` + itoa(realIdentityID) + `}`
	var req SwitchIdentityRequest
	err := json.Unmarshal([]byte(body), &req)
	require.Error(t, err, "裸数字 identity_id 必须被拒 —— 兼容它等于把截断 bug 藏起来")
	assert.Contains(t, err.Error(), "string")
}

// itoa 是本文件的小工具,避免为一处 strconv 而 import
func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
