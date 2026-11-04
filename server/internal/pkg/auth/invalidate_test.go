// 强制下线原语的测试。
//
// # 这个文件最该被记住的是这两条
//
//	TestInvalidate_TokenIssuedBeforeLineIsRejected
//	TestInvalidate_FreshTokenAfterChangeSurvives
//
// 它们是一对。只写前一条,一个"比较式写反了"的实现照样能绿(反正断言的是 true);
// 只写后一条,一个"线永远读成非零"的实现也能绿。只有两条同时存在,判定式才被
// 夹住。它们都走完整链路 —— 真的签一张 token、真的写一次作废线、真的解析回来
// 判定 —— 而不是只调纯函数。只测纯函数会漏掉"线写进去了但没人读"这类接线错误。
//
// 注:MemoryStore 原先没有独立的测试文件。本文件覆盖它的作废线部分;
// 吊销部分(jti → 过期时刻)的往返见 TestMemoryStore_InvalidateIndependentOfSweep。
//
// # 本文件的可逆探针清单(写完当场逐条验过,全都能把对应实现改红)
//
// 将来动这个机制的判定式或存储实现,先照着这张表把对应那条确认会红,再谈绿:
//
//	| # | 改坏什么                            | 谁会红                                      |
//	|---|------------------------------------|--------------------------------------------|
//	| 1 | 判定式 < 写成 <=                     | TestTokenInvalidated_Boundaries(两个边界格)   |
//	| 2 | 判定式丢掉时钟宽限                    | TestTokenInvalidated_Boundaries(窗口内/右端)   |
//	| 3 | sign() 不再写 iat                   | TestInvalidate_TokenIssuedBeforeLineIsRejected|
//	| 4 | 作废线允许后退                       | TestMemoryStore_InvalidateNeverGoesBackward   |
//	| 5 | sweep 顺手清掉过期作废线              | TestMemoryStore_InvalidateIndependentOfSweep   |
//	| 6 | InvalidateBefore 串到别的用户          | TestMemoryStore_InvalidateOtherUserDoesNotLeak |
//	| 7 | InvalidateFrom 首次不落盘            | TestMemoryStore_InvalidateRoundTrip           |
//	| 8 | 判定式整体取反                       | TestInvalidate_FreshTokenAfterChangeSurvives  |
//
// 第 3 条尤其重要:它证明端到端那两条**真的走了 sign→parse→判定**的链路,
// 而不是把纯函数调了两遍 —— 只测纯函数的版本在改动这里时不会有任何反应。
package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/* ==================== TokenInvalidated 纯判定 ==================== */

// TestTokenInvalidated_Boundaries 把边界全部钉死。
//
// 每个用例都带 why —— 这些期望值不是"算出来的",每一个都对应一个具体的线上故障。
// 将来有人改判定式,失败信息里要能直接读到"你踩的是哪个坑"。
func TestTokenInvalidated_Boundaries(t *testing.T) {
	base := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		issuedAt time.Time
		before   time.Time
		skew     time.Duration
		want     bool
		why      string
	}{
		{
			name: "从未作废(零值线)→ 一律有效", issuedAt: base, before: time.Time{},
			want: false,
			why:  "绝大多数用户没有作废线。这里若返回 true 就是全员被踢 —— 绝大多数用户根本没改过密码",
		},
		{
			name: "零宽限 + 签发时刻 == 作废线 → 有效", issuedAt: base, before: base, skew: 0,
			want: false,
			why:  "判定是严格早于。写成 >= 会把「恰好在改密码那一刻签发的 token」判死",
		},
		{
			name: "零宽限 + 早 1ns → 作废", issuedAt: base.Add(-time.Nanosecond), before: base, skew: 0,
			want: true,
			why:  "边界内侧一格。这一格决定了「早于线」而不是「不晚于线」",
		},
		{
			name: "零宽限 + 晚 1s → 有效", issuedAt: base.Add(time.Second), before: base, skew: 0,
			want: false,
			why:  "改完密码之后重新登录拿到的 token 必须能用",
		},
		{
			name: "窗口内(早 1s,不足一个宽限)→ 有效", issuedAt: base.Add(-ClockSkewGrace + time.Second),
			before: base, skew: ClockSkewGrace,
			want: false,
			why:  "落在 ClockSkewGrace 窗口内。跨实例时钟偏移就在这个量级,把它判死会造成「改完密码就再也登不进去」的死循环",
		},
		{
			name: "窗口右端(正好早一个宽限)→ 有效", issuedAt: base.Add(-ClockSkewGrace),
			before: base, skew: ClockSkewGrace,
			want: false,
			why:  "窗口开闭的判别格:iat+skew 恰好等于作废线,严格早于是 false",
		},
		{
			name: "刚出窗口(多早 1s)→ 作废", issuedAt: base.Add(-ClockSkewGrace - time.Second),
			before: base, skew: ClockSkewGrace,
			want: true,
			why:  "存量令牌的 iat 早就远超一个宽限,这一格才是真实存量令牌的代表",
		},
		{
			name: "晚于作废线 1h → 有效", issuedAt: base.Add(time.Hour),
			before: base, skew: ClockSkewGrace,
			want: false,
			why:  "改完密码之后重新登录的正常路径",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, TokenInvalidated(c.issuedAt, c.before, c.skew), c.why)
		})
	}
}

/* ==================== MemoryStore:作废线往返 ==================== */

func TestMemoryStore_InvalidateRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	const uid = uint64(42)

	// 从未作废 → 零值 + 无错。map 取缺失键天然给零值,正是"从未作废"的语义。
	before, err := s.InvalidateBefore(ctx, uid)
	require.NoError(t, err)
	assert.True(t, before.IsZero(), "新用户应返回零值线")

	at := time.Date(2026, 10, 21, 12, 0, 0, 123456789, time.UTC) // 带纳秒,验证不截断
	require.NoError(t, s.InvalidateFrom(ctx, uid, at))

	before, err = s.InvalidateBefore(ctx, uid)
	require.NoError(t, err)
	assert.Equal(t, at.UnixNano(), before.UnixNano(), "写进去的线要能原样读出来")
}

// TestMemoryStore_InvalidateNeverGoesBackward 是这个原语最容易写错的地方。
//
// 作废线回退 = 已经踢掉的 token 整批复活。所以即便后到的那个 at 更早,也必须被忽略。
func TestMemoryStore_InvalidateNeverGoesBackward(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	const uid = uint64(7)
	base := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.InvalidateFrom(ctx, uid, base.Add(2*time.Hour)))
	require.NoError(t, s.InvalidateFrom(ctx, uid, base)) // 更早 —— 必须被忽略

	got, err := s.InvalidateBefore(ctx, uid)
	require.NoError(t, err)
	assert.Equal(t, base.Add(2*time.Hour).UnixNano(), got.UnixNano(),
		"作废线被后退了 —— [T1,T2) 之间签发的 token 会整批复活。一次本该更严格的收紧被放松了")

	// 零值写入是 no-op,不能把线抹掉
	require.NoError(t, s.InvalidateFrom(ctx, uid, time.Time{}))
	got, err = s.InvalidateBefore(ctx, uid)
	require.NoError(t, err)
	assert.Equal(t, base.Add(2*time.Hour).UnixNano(), got.UnixNano(), "零值不应清空作废线")
}

// TestMemoryStore_InvalidateConcurrentMonotonic 并发下仍然单调。
//
// 模拟两个实例同时改密码、后到的请求带着更早的时刻到达。go test -race 下跑。
func TestMemoryStore_InvalidateConcurrentMonotonic(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	const uid = uint64(99)
	base := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for h := 1; h <= 200; h++ {
		wg.Add(1)
		go func(h int) {
			defer wg.Done()
			_ = s.InvalidateFrom(ctx, uid, base.Add(time.Duration(h)*time.Hour))
		}(h)
	}
	wg.Wait()

	got, err := s.InvalidateBefore(ctx, uid)
	require.NoError(t, err)
	assert.Equal(t, base.Add(200*time.Hour).UnixNano(), got.UnixNano(),
		"并发乱序写入后应停在最大值,而不是最后写入的那个")
}

// TestMemoryStore_InvalidateIndependentOfSweep 两套状态互不干扰。
//
// 作废线和 jti 吊销共用一把锁、同一个接口,但它们是两件事。若 sweepLocked 顺手
// 把过期作废线清了,一个用户改完密码后跑几次吊销写入就"自动复活" ——
// 而作废线本来是**不过期**的,这正是 MemoryStore.invalid 上那条注释的由来。
func TestMemoryStore_InvalidateIndependentOfSweep(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	// at 刻意放在 now **之前**:这样"按过期时间顺手清掉作废线"这个错误实现一定会触发,
	// 而不是因为作废线太新而侥幸躲过 —— 那样这条测试就是恒真的。
	at := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	require.NoError(t, s.InvalidateFrom(ctx, 5, at))
	require.NoError(t, s.Revoke(ctx, "jti-short", time.Second))

	// 推时钟让 jti-short 过期,再触发一次写入驱动 sweepLocked
	now = now.Add(2 * time.Second)
	require.NoError(t, s.Revoke(ctx, "jti-trigger", time.Hour))

	require.Equal(t, 1, s.Len(),
		"sweep 应当清掉 jti-short 只留下 jti-trigger —— 这条断言是必要的:它证明 sweep 真的跑过了,"+
			"否则下面那条「作废线没被影响」就是个恒真的空断言")

	got, err := s.InvalidateBefore(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, at.UnixNano(), got.UnixNano(),
		"清理过期吊销记录时把作废线一起清了 —— sweepLocked 越界了")
}

func TestMemoryStore_InvalidateOtherUserDoesNotLeak(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	require.NoError(t, s.InvalidateFrom(ctx, 1, time.Now()))

	got, err := s.InvalidateBefore(ctx, 2)
	require.NoError(t, err)
	assert.True(t, got.IsZero(), "别人的作废线不该泄漏到本用户")

	// 反向:本用户自己写过线之后,别人的写入也不该影响它
	require.NoError(t, s.InvalidateFrom(ctx, 2, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, s.InvalidateFrom(ctx, 1, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)))

	got, err = s.InvalidateBefore(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(1767225600), got.Unix(), "2026-01-01 的线不该被别人的 2030 写入顶掉")
}

/* ==================== 端到端:签发 → 作废 → 判定 ==================== */

// TestInvalidate_TokenIssuedBeforeLineIsRejected 本文件最重要的一条。
//
// 走真实链路:真的签一张 access token、真的写一次作废线、真的解析回来判定。
func TestInvalidate_TokenIssuedBeforeLineIsRejected(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	m := newTestMulti()

	tok, err := m.SignByAudience(AudienceAdmin, testClaims(42))
	require.NoError(t, err)

	claims, err := m.ParseAnyAccess(tok)
	require.NoError(t, err)
	require.NotNil(t, claims.IssuedAt, "sign() 权威写 iat —— 没有它整个机制无从谈起")

	// 前置:作废之前,这张 token 是好的
	before, err := s.InvalidateBefore(ctx, claims.UserID)
	require.NoError(t, err)
	require.False(t, TokenInvalidated(claims.IssuedAt.Time, before, ClockSkewGrace),
		"前置条件不成立,后面的断言没有意义")

	// 改密码:作废线推到宽限窗口之外
	//
	// ⚠️ 这里必须推到 ClockSkewGrace **之外**。iat 的精度是秒(jwt.NumericDate
	// 默认 TimePrecision),而 ClockSkewGrace=60s,只推 1s 的话 iat+60s 仍然晚于
	// 作废线 —— 断言会红,但红的原因是测试写错而不是实现写错。
	require.NoError(t, s.InvalidateFrom(ctx, claims.UserID,
		time.Now().Add(ClockSkewGrace+time.Minute)))

	// 重新解析(模拟下一次请求):JWT 本身没坏,解析仍应成功 —— 作废是**另一层**判断,
	// 混进解析层会让"过期"和"被作废"变成同一个错误码,排查时无从区分。
	claims, err = m.ParseAnyAccess(tok)
	require.NoError(t, err, "JWT 解析不负责作废判断")
	before, err = s.InvalidateBefore(ctx, claims.UserID)
	require.NoError(t, err)

	assert.True(t, TokenInvalidated(claims.IssuedAt.Time, before, ClockSkewGrace),
		"改密码之前签发的 token 在改密码之后必须被判失效 —— "+
			"否则盗号者的会话在事故响应后依然有效,改密码这个动作形同虚设")
}

// TestInvalidate_FreshTokenAfterChangeSurvives 反向守卫。
//
// 与上一条配对。上一条若因"比较式写反 / 线永远读成非零"而绿,这条会红。
func TestInvalidate_FreshTokenAfterChangeSurvives(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	m := newTestMulti()
	const uid = uint64(43)

	// 用户改密码 → 作废线推到此刻
	require.NoError(t, s.InvalidateFrom(ctx, uid, time.Now()))

	// 然后**重新登录**,拿到一张全新 token
	tok, err := m.SignByAudience(AudienceAdmin, testClaims(uid))
	require.NoError(t, err)

	claims, err := m.ParseAnyAccess(tok)
	require.NoError(t, err)
	before, err := s.InvalidateBefore(ctx, uid)
	require.NoError(t, err)

	assert.False(t, TokenInvalidated(claims.IssuedAt.Time, before, ClockSkewGrace),
		"改密码**之后**签发的 token 必须有效 —— "+
			"否则用户改完密码当场登不进去,而且是再也登不进去的死循环")
}

// TestInvalidate_ManySessionsAllDieTogether 覆盖本机制相对逐 jti 吊销的全部意义。
//
// 一个用户同时有 5 台设备在线,改密码之后必须**全部**下线 —— 逐 jti 吊销做不到这件事,
// 因为服务端根本没有"这个用户还有哪些 jti"的索引。
func TestInvalidate_ManySessionsAllDieTogether(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	m := newTestMulti()
	const uid = uint64(50)

	var toks []string
	for i := 0; i < 5; i++ {
		tok, err := m.SignByAudience(AudienceAdmin, testClaims(uid))
		require.NoError(t, err)
		toks = append(toks, tok)
	}
	require.NoError(t, s.InvalidateFrom(ctx, uid, time.Now().Add(ClockSkewGrace+time.Minute)))

	for i, tok := range toks {
		c, err := m.ParseAnyAccess(tok)
		require.NoError(t, err)
		before, err := s.InvalidateBefore(ctx, uid)
		require.NoError(t, err)
		assert.True(t, TokenInvalidated(c.IssuedAt.Time, before, ClockSkewGrace),
			"第 %d 个会话应当一并失效", i)
	}

	// 对照:另一个用户毫发无损
	other, err := m.SignByAudience(AudienceAdmin, testClaims(51))
	require.NoError(t, err)
	oc, err := m.ParseAnyAccess(other)
	require.NoError(t, err)
	ob, err := s.InvalidateBefore(ctx, oc.UserID)
	require.NoError(t, err)
	assert.False(t, TokenInvalidated(oc.IssuedAt.Time, ob, ClockSkewGrace),
		"作废是按用户的,不能波及他人")
}
