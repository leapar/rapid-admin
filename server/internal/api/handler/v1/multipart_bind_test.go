// 分片上传的 body 绑定 —— M80 收尾
//
// ⚠️ 这里守的不是"handler 调得通",而是 **`CompleteReq` 能不能吃下前端真的发过来的
// 那份 body**。这两件事在 Go 里是会分开的:
//
//   - 前端 `completeMultipart()` POST 的是 `{ session_id: sessionId }`
//   - `session_id` 的值本身还是**字符串** —— 它来自 `MultipartInitResp.SessionID`,
//     那个字段带 `,string`,序列化出来就是 `"123"`,TS 侧也声明成了 `string`
//
// 而 encoding/json 的字段匹配是「大小写不敏感、**但忽略不了下划线**」,
// `session_id` 因此**永远匹配不上** `SessionID`,解出来是 0 —— 不报错、不 400,
// 只是拿着会话 id 0 去 Complete,于是"分片传完了但合并总是失败"。
//
// 这就是为什么这个测试直接拿 `service.CompleteReq` 走一遍真实的 `BindJSON`,
// 而不是 mock service 断言"传进去什么":**绑定对了但没人调**是另一回事,
// **绑定错了但测试全绿**是这里真正要防的那一种。
package v1

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/service"
)

// bindCompleteReq 用真实 BindJSON 路径解析 body
func bindCompleteReq(t *testing.T, body string) (service.CompleteReq, error) {
	t.Helper()
	var c app.RequestContext
	c.Request.SetHeader("Content-Type", "application/json")
	c.Request.SetBodyString(body)
	h := NewMultipartHandler(&service.Registry{})
	var req service.CompleteReq
	err := h.BindJSON(&c, &req)
	return req, err
}

// 特征:前端真实发的那份 body 必须能绑出 session id。
//
// ⚠️ **回滚这个测试就会红**:去掉 `,string` → 报 "cannot unmarshal string into
// uint64";去掉字段名 → 静默拿到 0(不报错!这是它当初没被发现的原因)。
func TestCompleteReq_BindsFrontendBody(t *testing.T) {
	req, err := bindCompleteReq(t, `{"session_id":"1234567890"}`)
	require.NoError(t, err)
	require.Equal(t, uint64(1234567890), req.SessionID,
		"前端 POST 的 session_id 绑不上 —— Complete 会拿着 0 去合并")
}

// 特征:**只收字符串形态,且数字形态要报错而不是静默当 0**。
//
// ⚠️ 这条断言的是实际契约,不是我的设想:`,string` 在 hertz 的 decoder 上是
// 严格模式(裸数字会被判为类型不匹配)。前端 POST 的确实是字符串
// (`MultipartInitResp.session_id` 就是 string),而 `/files/mp/part` 那一路
// 走的是 header —— `X-Session-Id` 两种形态都收(`headerUint` 自己 ParseUint),
// 所以两路形态不一致是既有设计,不是 bug。留这条测试是为了让"以后有人嫌
// `,string` 麻烦想删掉"这个动作立刻变红:删了之后数字请求从 400 变成 200 +
// 拿 0 去查会话,比现在难查得多。
func TestCompleteReq_RejectsNumericSessionID(t *testing.T) {
	_, err := bindCompleteReq(t, `{"session_id":42}`)
	require.Error(t, err, "数字形态必须显式报错,不能静默解成 0")
}

// 特征:Init 的 folder_id 走的是同一条路(前端传字符串,后端带 `,string`),
// 一起锁住 —— 这两个字段是同一个坑,只修一个的话下一个 commit 又会写回去。
//
// ⚠️ 注意 `file_size` / `part_size` 前端传的是**数字**(`File.size` 就是 number),
// 所以它们**不能**加 `,string` —— 加了会让数字形态自己绑不上。
func TestMultipartInitReq_BindsFrontendBody(t *testing.T) {
	var c app.RequestContext
	c.Request.SetHeader("Content-Type", "application/json")
	c.Request.SetBodyString(`{"folder_id":"77","biz":"house","file_name":"a.mp4","file_size":12,"part_size":0}`)
	h := NewMultipartHandler(&service.Registry{})
	var req service.MultipartInitReq
	require.NoError(t, h.BindJSON(&c, &req))
	require.Equal(t, uint64(77), req.FolderID)
	require.Equal(t, "house", req.Biz)
	require.Equal(t, int64(12), req.FileSize)
}
