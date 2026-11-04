// typed-nil 回归网 —— 见 kvstore.go Resolve 的注释。
//
// 与 auth.revocation_resolve_test.go 是同一个 bug 的两个副本:
// 两处 Resolve 都因为接收 redis.UniversalClient 而让 nil 判断失效,
// 结果是"显式要求 Redis 却没配 Redis"照样启动成功。
package kvstore

import (
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolve_TypedNilFromRepository 复现 main.go 的真实写法。
//
// ⚠️ 写成 Resolve(ModeRedis, nil) 是**测不到**的:无类型 nil 转成接口后
// 接口值确实为 nil,任何判空实现都能过。必须声明成 *redis.Client 再传,
// 那才是 repository.GetRedis() 的返回类型,也才是那个坑的入口。
func TestResolve_TypedNilFromRepository(t *testing.T) {
	var rdb *redis.Client // redis.addr 为空时 repository.GetRedis() 的返回值
	require.Nil(t, rdb)

	_, err := Resolve(ModeRedis, rdb)
	require.Error(t, err, "typed nil 穿透了判空:限流会挂到一台没初始化的 Redis 上")
	assert.Contains(t, err.Error(), "kv.mode=redis")

	st, err := Resolve(ModeAuto, rdb)
	require.NoError(t, err)
	assert.Equal(t, "memory", st.Backend,
		"auto + 无 Redis 应退内存,实际 %q —— 每个请求都会去打一台死 Redis", st.Backend)
}

// TestResolve_TypedNilMemoryMode 对照组:显式 memory 与非法值分支不受影响,
// 确保上一条不是因为"永远返回内存"才绿的。
func TestResolve_TypedNilMemoryMode(t *testing.T) {
	var rdb *redis.Client

	st, err := Resolve(ModeMemory, rdb)
	require.NoError(t, err)
	assert.Equal(t, "memory", st.Backend)

	_, err = Resolve("postgres", rdb)
	require.Error(t, err)
}

// TestResolve_RealClientPicksRedis 确认非 nil 时选 Redis,
// 否则上面两条可能只是恒返回内存。
func TestResolve_RealClientPicksRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}) // 不会真去连
	st, err := Resolve(ModeAuto, rdb)
	require.NoError(t, err)
	assert.Equal(t, "redis", st.Backend)
}