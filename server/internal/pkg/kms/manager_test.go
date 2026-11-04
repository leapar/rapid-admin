package kms

import (
	"context"
	"encoding/hex"
	"strconv"
	"testing"
)

// clearEnv 把所有 KEY_* env 清干净(t.Setenv 会自动还原)
func clearEnv(t *testing.T) {
	t.Helper()
	for _, tier := range []string{"PII", "SECRET", "NORMAL"} {
		prefix := "KEY_" + tier + "_"
		for v := 1; v <= 8; v++ {
			t.Setenv(prefix+"V"+strconv.Itoa(v), "")
		}
		t.Setenv(prefix+"ACTIVE", "")
	}
	t.Setenv("KEY_LEGACY", "")
}

func key32(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return hex.EncodeToString(k)
}

// TestNewManager_KeyMisconfig_ReturnsError 密钥配错必须返 error,**不能 panic**
//
// # M100 修的那个 bug
//
// 老实现 `crypto.InitKeys` 在 key 非法 / active key 数不对时**直接 panic**,
// 而 `NewManager` 里那个 `if err := crypto.InitKeys(...); err != nil`
// 是**死代码**(InitKeys 永远返 nil)—— 于是本文件顶部注释里承诺的
//
//	defer mgr.Close()
//	// 配错 → log.Warn("kms init failed; tiered encrypt disabled") → 服务照常起
//
// 是**假的**:密钥配错 = **进程 panic 退出**,不是降级。
//
// 现在 InitKeys 真返 error → 这条降级路径第一次真的生效。
func TestNewManager_KeyMisconfig_ReturnsError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("密钥配错不该 panic(应该返 error 让调用方降级): %v", r)
		}
	}()
	clearEnv(t)
	// ACTIVE 指向没配的版本
	t.Setenv("KEY_PII_V1", key32(t))
	t.Setenv("KEY_PII_ACTIVE", "V2")

	mgr, err := NewManager(context.Background(), Config{Provider: "local"})
	if err == nil {
		_ = mgr.Close()
		t.Fatal("密钥配错时 NewManager 必须返 error")
	}
}

// TestNewManager_UnknownProvider 未知 provider 必须返 error
func TestNewManager_UnknownProvider(t *testing.T) {
	clearEnv(t)
	if _, err := NewManager(context.Background(), Config{Provider: "not-exist"}); err == nil {
		t.Fatal("未知 provider 必须返 error")
	}
}

// TestNewManager_ValidLocalConfig 正常配置必须能装上
func TestNewManager_ValidLocalConfig(t *testing.T) {
	clearEnv(t)
	t.Setenv("KEY_PII_V1", key32(t))
	t.Setenv("KEY_PII_ACTIVE", "V1")

	mgr, err := NewManager(context.Background(), Config{Provider: "local"})
	if err != nil {
		t.Fatalf("正常配置不该报错: %v", err)
	}
	defer mgr.Close()
	if mgr.ProviderName() != "local" {
		t.Fatalf("provider = %q", mgr.ProviderName())
	}
}
