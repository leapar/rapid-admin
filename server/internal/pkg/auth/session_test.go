// 会话绑定(sid)的行为契约。
//
// 这些断言针对的是一个**具体**失效:登出只吊销了 access 的 jti,那张 refresh
// 纹丝不动 —— 用户点了"退出登录",refresh 还能继续换发新 access,最长 720h。
//
// 修法是给一次登录生成一个会话 id(sid),access 与 refresh 两张 token 共用它,
// 登出时吊销 sid,两张一起死。见 session.go。
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClaims_ForkCarriesSessionID 钉住 Fork 必须带上 sid。
//
// ⚠️ 这条**必须**是绿的,否则会话绑定会在第一次 refresh 之后断掉 ——
// 而它**不会报错**:签发照样成功,只是 quietly 少一个字段,于是登出又变回
// 只吊销一半。Fork 的注释里点名了这条测试,别让它悄悄失效。
func TestClaims_ForkCarriesSessionID(t *testing.T) {
	c := &Claims{
		UserID:     1,
		IdentityID: 2,
		Username:   "u",
		RoleCode:   "r",
		SessionID:  "sid-abc",
	}
	f := c.Fork()
	assert.Equal(t, "sid-abc", f.SessionID, "Fork 丢了 sid —— 轮换后会话绑定就断了")
	// Fork 产出的必须是**新结构**:sign() 会覆写整个 RegisteredClaims,
	// 复用同一个结构签两次,内存里那份 claims 会停留在第二张 token 上。
	assert.NotSame(t, c, f)
	assert.Empty(t, f.JTI(), "Fork 不该带上任何标准 claim")
}

// TestSessionRevokeKey_IsNamespaced 钉住会话键与 jti 键不撞。
//
// 吊销存储是一张扁平的字符串键集合,jti 与 sid 住在同一张表里。
// 没有前缀的话,"现在两种键不撞"只是巧合,不是保证。
func TestSessionRevokeKey_IsNamespaced(t *testing.T) {
	sid := NewSessionID()
	key := SessionRevokeKey(sid)
	assert.NotEqual(t, sid, key, "会话键必须带前缀,否则与 jti 键空间混在一起")
	assert.Contains(t, key, sid, "前缀之外必须保留原 sid,否则定位不到")
	// 两个不同会话的键必须不同
	assert.NotEqual(t, key, SessionRevokeKey(NewSessionID()))
}

// TestRevokeSession_RoundTrip 吊销 → 查得到。
func TestRevokeSession_RoundTrip(t *testing.T) {
	old := globalRevocation
	globalRevocation = NewMemoryStore()
	t.Cleanup(func() { globalRevocation = old })

	ctx := context.Background()
	sid := NewSessionID()

	revoked, err := IsSessionRevoked(ctx, sid)
	require.NoError(t, err)
	assert.False(t, revoked, "新会话不该是吊销态")

	require.NoError(t, RevokeSession(ctx, sid, time.Hour))

	revoked, err = IsSessionRevoked(ctx, sid)
	require.NoError(t, err)
	assert.True(t, revoked, "吊销后必须查得到 —— 这正是登出能杀掉 refresh 的机制")
}

// TestRevokeSession_EmptySIDIsNoop 空 sid 是 no-op,不是"吊销了空字符串"。
//
// 老 token 没有 sid(本轮上线前签发的)。这时登出退回到只吊销 access 的 jti,
// 不能反过来把**所有**空 sid 的会话一起吊了 —— 那是"一条空键吊销一切"。
func TestRevokeSession_EmptySIDIsNoop(t *testing.T) {
	old := globalRevocation
	globalRevocation = NewMemoryStore()
	t.Cleanup(func() { globalRevocation = old })

	ctx := context.Background()
	require.NoError(t, RevokeSession(ctx, "", time.Hour))

	revoked, err := IsSessionRevoked(ctx, "")
	require.NoError(t, err)
	assert.False(t, revoked, "空 sid 不该是吊销态")
}

// TestSignAccessAndRefresh_ShareSessionID 两张 token 必须共用同一个 sid。
//
// 这是整个机制的根:两张 token 靠 sid 认亲,登出时才能一次把两张都干掉。
// 如果签发时 sid 掉了(比如 Fork 漏抄),这里就红。
func TestSignAccessAndRefresh_ShareSessionID(t *testing.T) {
	m := NewManager("secret-for-session-test", "iss",
		time.Hour, 24*time.Hour, 5*time.Minute, AudienceAdmin)

	sid := NewSessionID()
	base := (&Claims{UserID: 1, IdentityID: 2, SessionID: sid}).Fork()

	accessTok, err := m.SignAccess(base.Fork())
	require.NoError(t, err)
	refreshTok, err := m.SignRefresh(base.Fork())
	require.NoError(t, err)

	ac, err := m.ParseAccess(accessTok)
	require.NoError(t, err)
	rc, err := m.ParseRefresh(refreshTok)
	require.NoError(t, err)

	assert.Equal(t, sid, ac.SessionID, "access token 丢了 sid")
	assert.Equal(t, sid, rc.SessionID, "refresh token 丢了 sid")
	assert.Equal(t, ac.SessionID, rc.SessionID, "两张 token 的 sid 必须一致")
}

// TestSessionID_IsRandomAndNonEmpty sid 必须随机且非空 ——
// 它是"把两张 token 绑在一起"的那根线,重复了就等于两个用户的会话混成一条
// (A 能登出 B 的会话)。
func TestSessionID_IsRandomAndNonEmpty(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		sid := NewSessionID()
		require.NotEmpty(t, sid)
		_, dup := seen[sid]
		require.False(t, dup, "sid 重复了")
		seen[sid] = struct{}{}
	}
}
