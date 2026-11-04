// typed-nil 回归网 —— 见 revocation.go ResolveRevocation 的注释。
//
// 这个 bug 是 2026-10-21 真机启动时当场发现的,不是想出来的:
// config.dev.yaml 里 redis.addr 是空的,日志明写 "redis skipped (empty addr)",
// 同一屏却打出 kvstore backend=redis —— 一台根本没初始化过的 Redis。
package auth

import (
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveRevocation_TypedNilFromRepository 是本文件的核心断言。
//
// 关键在**怎么把 nil 传进来**:必须是
//
//	var rdb *redis.Client          // repository.GetRedis() 的返回类型
//	ResolveRevocation(mode, rdb)   // 隐式转换成接口参数
//
// 而不是直接写 ResolveRevocation(mode, nil) —— 后者传的是**无类型 nil**,
// 接口值真的是 nil,任何判空实现都能过。写成 nil 的测试**恰好绕过了**
// 要防的那个 bug。
func TestResolveRevocation_TypedNilFromRepository(t *testing.T) {
	// 这就是 main.go 里的写法:repository.GetRedis() 返回 *redis.Client,
	// redis.addr 为空时它是 nil,传进接口参数就成了 typed nil。
	var rdb *redis.Client
	require.Nil(t, rdb)

	// 显式要求 Redis,却没配 redis.addr → 必须**拒绝启动**
	_, err := ResolveRevocation(RevocationRedis, rdb)
	require.Error(t, err, "typed nil 穿透了判空:显式配 redis 模式却没配 addr 竟然能启动成功")
	assert.Contains(t, err.Error(), "revocation_mode=redis")

	// auto 模式必须退回内存,而不是造一个连不上的 RedisStore
	st, err := ResolveRevocation(RevocationAuto, rdb)
	require.NoError(t, err)
	_, isMem := st.(*MemoryStore)
	assert.True(t, isMem, "auto + 无 Redis 应退内存,实际拿到 %T —— 会一直往死 Redis 上打", st)
}

// TestResolveRevocation_TypedNilOnHealthyPath 与上一条互为对照:
// 模式是 memory 时,有没有 Redis 都该给内存实现 —— 用来确认上面那条
// 不是因为 memory 分支碰巧返回了内存才绿的。
func TestResolveRevocation_TypedNilOnHealthyPath(t *testing.T) {
	var rdb *redis.Client

	st, err := ResolveRevocation(RevocationMemory, rdb)
	require.NoError(t, err)
	_, isMem := st.(*MemoryStore)
	assert.True(t, isMem)

	_, err = ResolveRevocation("mongo", rdb)
	require.Error(t, err, "非法模式必须报错")
}

// TestResolveRevocation_RealClientPicksRedis 确认非 nil 时确实选 Redis,
// 否则上面两条可能只是"永远返回内存"而已。
func TestResolveRevocation_RealClientPicksRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}) // 不会去连,只是拿个非 nil
	st, err := ResolveRevocation(RevocationAuto, rdb)
	require.NoError(t, err)
	_, isRedis := st.(*RedisStore)
	assert.True(t, isRedis, "非 nil client 应选 RedisStore,实际 %T", st)
}