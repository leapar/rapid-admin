package local

import (
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/crypto"
)

// clearTierEnv 把一个 tier 的 env 清干净(t.Setenv 会自动还原)
//
// 只清 V1..V8 —— 实际用到的版本号在这个范围内,再多只是拖慢测试。
func clearTierEnv(t *testing.T, tier string) {
	t.Helper()
	prefix := "KEY_" + tier + "_"
	for v := 1; v <= 8; v++ {
		t.Setenv(prefix+"V"+strconv.Itoa(v), "")
	}
	t.Setenv(prefix+"ACTIVE", "")
}

// TestProvider_ListKeys_ActivePointsToMissingKey_ReturnsError
//
// # M100 修的那个 bug
//
// 原来两种"ACTIVE 写错"的结局**完全相反**:
//   - ACTIVE=V2 但 V2 没配(而 V1 配了)→ entries 全是 Active=false
//     → crypto 侧 panic("must have exactly 1 active key")→ **进程退出**
//   - ACTIVE=V2 但 V1/V2 都没配 → entries 为空 → 该 tier 被**静默禁用**
//
// 同一个错因(ACTIVE 写错),一个崩进程,一个悄悄不加密。
// 现在统一成"启动时报清楚"。
func TestProvider_ListKeys_ActivePointsToMissingKey_ReturnsError(t *testing.T) {
	clearTierEnv(t, "PII")
	clearTierEnv(t, "SECRET")
	clearTierEnv(t, "NORMAL")

	// 只配了 V1,ACTIVE 却指向 V2
	t.Setenv("KEY_PII_V1", hex.EncodeToString(newKey(t)))
	t.Setenv("KEY_PII_ACTIVE", "V2")

	p := New(zap.NewNop())
	keys, err := p.ListKeys(context.Background())
	if err == nil {
		t.Fatalf("ACTIVE 指向没配的版本时必须返 error(拿到 %d 个 PII key)", len(keys[crypto.TierPII]))
	}
	if !strings.Contains(err.Error(), "ACTIVE") {
		t.Fatalf("错误信息里要点明是 ACTIVE 的问题: %v", err)
	}
}

// TestProvider_ListKeys_ActiveUnparseable_ReturnsError ACTIVE 写错格式必须报错
func TestProvider_ListKeys_ActiveUnparseable_ReturnsError(t *testing.T) {
	for _, bad := range []string{"XYZ", "V0", "V256", "-1", "v"} {
		t.Run(bad, func(t *testing.T) {
			clearTierEnv(t, "PII")
			clearTierEnv(t, "SECRET")
			clearTierEnv(t, "NORMAL")

			t.Setenv("KEY_PII_V1", hex.EncodeToString(newKey(t)))
			t.Setenv("KEY_PII_ACTIVE", bad)

			p := New(zap.NewNop())
			if _, err := p.ListKeys(context.Background()); err == nil {
				t.Fatalf("ACTIVE=%q 非法时必须返 error", bad)
			}
		})
	}
}

// TestProvider_ListKeys_ActiveAcceptsPlainNumber ACTIVE 的写法容错
//
// 老文档只写了 `V<N>`,运维漏个 V 就踩坑(而且踩的是崩进程那条路)。
// 现在 "V2" / "v2" / "2" 都认。
func TestProvider_ListKeys_ActiveAcceptsPlainNumber(t *testing.T) {
	for _, ok := range []string{"V2", "v2", "2", " V2 "} {
		t.Run(ok, func(t *testing.T) {
			clearTierEnv(t, "PII")
			clearTierEnv(t, "SECRET")
			clearTierEnv(t, "NORMAL")

			t.Setenv("KEY_PII_V1", hex.EncodeToString(newKey(t)))
			t.Setenv("KEY_PII_V2", hex.EncodeToString(newKey(t)))
			t.Setenv("KEY_PII_ACTIVE", ok)

			p := New(zap.NewNop())
			keys, err := p.ListKeys(context.Background())
			if err != nil {
				t.Fatalf("ACTIVE=%q 应该被接受: %v", ok, err)
			}
			if len(keys[crypto.TierPII]) != 2 {
				t.Fatalf("PII keys = %d, want 2", len(keys[crypto.TierPII]))
			}
			active := 0
			for _, k := range keys[crypto.TierPII] {
				if k.Active {
					active++
					if k.KeyVersion != 2 {
						t.Fatalf("active 版本 = %d, want 2", k.KeyVersion)
					}
				}
			}
			if active != 1 {
				t.Fatalf("active 数 = %d, want 1", active)
			}
		})
	}
}

// TestProvider_ListKeys_OneBadTier_FailsWholeList 一个 tier 配错就整体报错
//
// 理由:配错意味着"以为有 key、其实没有" —— 半装(一个 tier 好了、另一个
// 悄悄禁用)比整体不装更危险:业务会以为 PII 已经加密了。
func TestProvider_ListKeys_OneBadTier_FailsWholeList(t *testing.T) {
	clearTierEnv(t, "PII")
	clearTierEnv(t, "SECRET")
	clearTierEnv(t, "NORMAL")

	t.Setenv("KEY_PII_V1", hex.EncodeToString(newKey(t)))
	t.Setenv("KEY_PII_ACTIVE", "V1")
	t.Setenv("KEY_SECRET_V1", hex.EncodeToString(newKey(t)))
	t.Setenv("KEY_SECRET_ACTIVE", "V9") // 指向没配的版本

	p := New(zap.NewNop())
	if _, err := p.ListKeys(context.Background()); err == nil {
		t.Fatal("SECRET tier 配错时必须整体返 error")
	}
}
