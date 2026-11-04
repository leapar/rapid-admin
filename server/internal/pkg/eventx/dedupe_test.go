package eventx

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────── MemoryDeduper ───────────

func TestM173_MemoryDeduper_Once(t *testing.T) {
	d := NewMemoryDeduper()
	calls := 0
	fn := func() error {
		calls++
		return nil
	}

	require.NoError(t, d.Once(context.Background(), "k", time.Minute, fn))
	require.ErrorIs(t, d.Once(context.Background(), "k", time.Minute, fn), ErrDuplicate,
		"同一 key 第二次必须被挡住")
	assert.Equal(t, 1, calls, "fn 只执行了一次")

	// 不同 key 互不影响
	require.NoError(t, d.Once(context.Background(), "k2", time.Minute, fn))
	assert.Equal(t, 2, calls)
}

// 🔴 fn 失败不记录 —— 下次同 key 会再执行(重试语义)。
// 拆掉"失败不记录"这条分支(失败也记),本用例转红。
func TestM173_MemoryDeduper_FailureNotRecorded(t *testing.T) {
	d := NewMemoryDeduper()
	calls := 0
	fn := func() error {
		calls++
		return errors.New("处理失败")
	}
	require.Error(t, d.Once(context.Background(), "k", time.Minute, fn))
	require.Error(t, d.Once(context.Background(), "k", time.Minute, fn))
	assert.Equal(t, 2, calls, "失败不该被记成已消费 —— 否则事件就静默丢了")

	ok := func() error { calls++; return nil }
	require.NoError(t, d.Once(context.Background(), "k", time.Minute, ok))
	require.ErrorIs(t, d.Once(context.Background(), "k", time.Minute, ok), ErrDuplicate)
	assert.Equal(t, 3, calls)
}

func TestM173_MemoryDeduper_TTLExpiry(t *testing.T) {
	d := NewMemoryDeduper()
	calls := 0
	fn := func() error { calls++; return nil }
	require.NoError(t, d.Once(context.Background(), "k", 10*time.Millisecond, fn))
	time.Sleep(30 * time.Millisecond)
	require.NoError(t, d.Once(context.Background(), "k", 10*time.Millisecond, fn),
		"TTL 过了之后允许再执行")
	assert.Equal(t, 2, calls)
}

// 🔴 满员兜底:超限清理不 panic,也不影响新 key 记录(代价只是漏去重)。
func TestM173_MemoryDeduper_OverflowSurvives(t *testing.T) {
	d := NewMemoryDeduper()
	for i := 0; i < d.maxKeys+10; i++ {
		require.NoError(t, d.Once(context.Background(), "k"+strconv.Itoa(i), time.Minute, func() error { return nil }))
	}
	// 全是活 key 时 record 的兜底分支不炸即可;断言进程没死 + 还能用
	var err error
	require.NotPanics(t, func() {
		err = d.Once(context.Background(), "after-overflow", time.Minute, func() error { return nil })
	})
	require.NoError(t, err)
}

// ─────────── RedisDeduper(miniredis)───

func newMiniRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	require.NotNil(t, mr)
	cli := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func TestM173_RedisDeduper_Once(t *testing.T) {
	cli := newMiniRedis(t)
	d := NewRedisDeduper(cli, "")
	calls := 0
	fn := func() error { calls++; return nil }

	require.NoError(t, d.Once(context.Background(), "k", time.Minute, fn))
	require.ErrorIs(t, d.Once(context.Background(), "k", time.Minute, fn), ErrDuplicate)
	assert.Equal(t, 1, calls)
}

func TestM173_RedisDeduper_FailureNotRecorded(t *testing.T) {
	cli := newMiniRedis(t)
	d := NewRedisDeduper(cli, "test:")
	calls := 0
	fn := func() error {
		calls++
		return errors.New("失败")
	}
	require.Error(t, d.Once(context.Background(), "k", time.Minute, fn))
	require.Error(t, d.Once(context.Background(), "k", time.Minute, fn))
	assert.Equal(t, 2, calls)

	ok := func() error { calls++; return nil }
	require.NoError(t, d.Once(context.Background(), "k", time.Minute, ok))
	require.ErrorIs(t, d.Once(context.Background(), "k", time.Minute, ok), ErrDuplicate)
}

func TestM173_RedisDeduper_NilClientRunsUnconditionally(t *testing.T) {
	// Redis 挂/没配 = 去重退化成"不去重",事件照常执行,不因去重层故障丢事件
	d := NewRedisDeduper(nil, "")
	calls := 0
	require.NoError(t, d.Once(context.Background(), "k", time.Minute, func() error {
		calls++
		return nil
	}))
	assert.Equal(t, 1, calls)
}
