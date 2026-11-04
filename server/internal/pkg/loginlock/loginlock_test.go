// 登录锁定的行为契约 —— 见 loginlock.go 的注释。
//
// 本文件的存在理由:2026-10-21 之前,没有 Redis 时 loginlock 三个函数
// 开头就 `return nil`,于是**本机根本测不了锁定**——
// 想验"输错 10 次会被锁",必须先起一个 Redis。
// 讽刺的是这正是最需要它的场景:生产有 Redis 挡着,本地反而完全裸奔。
//
// 挂到 kvstore 的内存实现之后,这套东西第一次可以离线验。
package loginlock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/kvstore"
)

// useMemory 换一副干净的内存存储。锁定计数是进程级全局,
// 不隔离的话用例之间会互相污染(一个用例失败 10 次,下一个直接被锁)。
//
// ⚠️ cleanup 里**只能调 SetDefault,不能再调 useMemory** —— useMemory 自己
// 也注册 cleanup,那样会变成 Cleanup → useMemory → Cleanup → … 无限递归,
// 表现是 go test 直接卡死到超时(2026-10-21 踩过)。
func useMemory(t *testing.T) {
	t.Helper()
	setMemory()
	t.Cleanup(setMemory)
}

func setMemory() {
	kvstore.SetDefault(&kvstore.Store{
		Counter: kvstore.NewMemoryCounter(),
		Window:  kvstore.NewMemoryWindow(),
		Backend: "memory",
	})
}

func errIs(err error, code int) bool {
	var e *errcode.Error
	return errors.As(err, &e) && e.Code == code
}

// TestLoginLock_LocksAtThreshold 是这套东西的主断言:
// 连错 max 次之后,PreCheck 必须开始拒。
func TestLoginLock_LocksAtThreshold(t *testing.T) {
	useMemory(t)
	ctx := context.Background()
	const max = 10
	const lockDur = 15 * time.Minute

	require.NoError(t, PreCheck(ctx, "alice", "10.0.0.1", max), "没失败过不该锁")

	for i := 1; i < max; i++ {
		require.NoError(t, AfterFail(ctx, "alice", "10.0.0.1", lockDur))
		require.NoError(t, PreCheck(ctx, "alice", "10.0.0.1", max),
			"第 %d 次失败后不该锁(阈值是 %d)", i, max)
	}

	require.NoError(t, AfterFail(ctx, "alice", "10.0.0.1", lockDur))
	err := PreCheck(ctx, "alice", "10.0.0.1", max)
	assert.True(t, errIs(err, errcode.UserLocked), "第 %d 次失败后应锁,实际: %v", max, err)
}

// TestLoginLock_ClearOnSuccess 登录成功要能立刻解锁 ——
// 否则用户输错 10 次之后即使输对了密码也进不去,只能等锁过期。
func TestLoginLock_ClearOnSuccess(t *testing.T) {
	useMemory(t)
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		require.NoError(t, AfterFail(ctx, "bob", "10.0.0.2", 15*time.Minute))
	}
	require.True(t, errIs(PreCheck(ctx, "bob", "10.0.0.2", 10), errcode.UserLocked))

	require.NoError(t, Clear(ctx, "bob", "10.0.0.2"))
	require.NoError(t, PreCheck(ctx, "bob", "10.0.0.2", 10),
		"登录成功后 Clear 没把计数清零 —— 输对密码也进不去")
}

// TestLoginLock_ScopedByIPAndUsername 锁的范围必须同时含账号和 IP:
// 只按账号锁的话,同一办公室里另一个人正常输密码也会被锁;
// 只按 IP 锁的话,攻击者换账号就能无限试。
func TestLoginLock_ScopedByIPAndUsername(t *testing.T) {
	useMemory(t)
	ctx := context.Background()

	for i := 0; i < 15; i++ {
		require.NoError(t, AfterFail(ctx, "carol", "10.0.0.3", 15*time.Minute))
	}
	require.True(t, errIs(PreCheck(ctx, "carol", "10.0.0.3", 10), errcode.UserLocked))

	require.NoError(t, PreCheck(ctx, "dave", "10.0.0.3", 10), "同 IP 换个账号不该被锁")
	require.NoError(t, PreCheck(ctx, "carol", "10.0.0.4", 10), "同账号换个 IP 不该被锁")
}

// TestLoginLock_MaxZeroDisables 这是 PreCheck 里 max<=0 那道守卫的锚点。
//
// 去掉守卫的话:`n >= max` 在 max=0 时对 n=0 也成立,
// 于是只要有人把阈值改成 0(本来想"关闭锁定"),
// 实际效果是**所有账号一失败就永久锁死**。
func TestLoginLock_MaxZeroDisables(t *testing.T) {
	useMemory(t)
	ctx := context.Background()

	for i := 0; i < 30; i++ {
		require.NoError(t, AfterFail(ctx, "erin", "10.0.0.5", 15*time.Minute))
	}
	require.NoError(t, PreCheck(ctx, "erin", "10.0.0.5", 0),
		"max=0 应表示关闭锁定,而不是锁死所有人")
}