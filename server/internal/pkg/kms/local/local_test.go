package local

import (
	"context"
	"encoding/hex"
	"os"
	"testing"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/crypto"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestProvider_Name(t *testing.T) {
	p := New(zap.NewNop())
	if p.Name() != "local" {
		t.Fatalf("name = %s", p.Name())
	}
}

func TestProvider_ListKeys_AllTiers(t *testing.T) {
	// 清干净再设
	os.Unsetenv("KEY_PII_ACTIVE")
	os.Unsetenv("KEY_PII_V1")
	os.Unsetenv("KEY_PII_V2")
	os.Unsetenv("KEY_SECRET_ACTIVE")
	os.Unsetenv("KEY_SECRET_V1")
	os.Unsetenv("KEY_NORMAL_ACTIVE")
	os.Unsetenv("KEY_NORMAL_V1")
	os.Unsetenv("KEY_NORMAL_V2")
	defer func() {
		os.Unsetenv("KEY_PII_ACTIVE")
		os.Unsetenv("KEY_PII_V1")
		os.Unsetenv("KEY_PII_V2")
		os.Unsetenv("KEY_SECRET_ACTIVE")
		os.Unsetenv("KEY_SECRET_V1")
		os.Unsetenv("KEY_NORMAL_ACTIVE")
		os.Unsetenv("KEY_NORMAL_V1")
		os.Unsetenv("KEY_NORMAL_V2")
	}()

	// PII: V1 老 + V2 active
	os.Setenv("KEY_PII_V1", hex.EncodeToString(newKey(t)))
	os.Setenv("KEY_PII_V2", hex.EncodeToString(newKey(t)))
	os.Setenv("KEY_PII_ACTIVE", "V2")
	// SECRET: V1 active
	os.Setenv("KEY_SECRET_V1", hex.EncodeToString(newKey(t)))
	os.Setenv("KEY_SECRET_ACTIVE", "V1")
	// NORMAL: V1 + V2 active
	os.Setenv("KEY_NORMAL_V1", hex.EncodeToString(newKey(t)))
	os.Setenv("KEY_NORMAL_V2", hex.EncodeToString(newKey(t)))
	os.Setenv("KEY_NORMAL_ACTIVE", "V2")

	p := New(zap.NewNop())
	keys, err := p.ListKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// PII
	if len(keys[crypto.TierPII]) != 2 {
		t.Fatalf("PII keys = %d, want 2", len(keys[crypto.TierPII]))
	}
	activeCount := 0
	for _, k := range keys[crypto.TierPII] {
		if k.Active {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("PII active count = %d, want 1", activeCount)
	}
	// SECRET
	if len(keys[crypto.TierSecret]) != 1 {
		t.Fatalf("SECRET keys = %d", len(keys[crypto.TierSecret]))
	}
	// NORMAL
	if len(keys[crypto.TierNormal]) != 2 {
		t.Fatalf("NORMAL keys = %d", len(keys[crypto.TierNormal]))
	}
}

func TestProvider_ListKeys_NoActive(t *testing.T) {
	os.Unsetenv("KEY_PII_ACTIVE")
	os.Unsetenv("KEY_PII_V1")
	defer os.Unsetenv("KEY_PII_V1")
	os.Setenv("KEY_PII_V1", hex.EncodeToString(newKey(t)))
	// 不设 ACTIVE → tier 返 nil
	p := New(zap.NewNop())
	keys, err := p.ListKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys[crypto.TierPII]) != 0 {
		t.Fatalf("expected 0 PII keys, got %d", len(keys[crypto.TierPII]))
	}
}

func TestProvider_ListKeys_SkipInvalidHex(t *testing.T) {
	os.Unsetenv("KEY_PII_ACTIVE")
	os.Unsetenv("KEY_PII_V1")
	os.Unsetenv("KEY_PII_V2")
	defer func() {
		os.Unsetenv("KEY_PII_ACTIVE")
		os.Unsetenv("KEY_PII_V1")
		os.Unsetenv("KEY_PII_V2")
	}()
	// V1: 非法 hex(短)→ 跳过
	os.Setenv("KEY_PII_V1", "deadbeef")
	// V2: 合法
	os.Setenv("KEY_PII_V2", hex.EncodeToString(newKey(t)))
	os.Setenv("KEY_PII_ACTIVE", "V2")
	p := New(zap.NewNop())
	keys, _ := p.ListKeys(context.Background())
	if len(keys[crypto.TierPII]) != 1 {
		t.Fatalf("expected 1 valid PII key (V1 skipped), got %d", len(keys[crypto.TierPII]))
	}
	if keys[crypto.TierPII][0].KeyVersion != 2 {
		t.Fatalf("expected V2, got V%d", keys[crypto.TierPII][0].KeyVersion)
	}
}

func TestProvider_Close(t *testing.T) {
	p := New(zap.NewNop())
	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestLoadLegacyKey KEY_LEGACY 的装载
//
// ⚠️ M100(2026-10-26):函数名原来叫 `LoadLegacyLegacyKey`(Legacy 写了两遍),
// 改名成 `LoadLegacyKey` —— 全仓只有一个调用方(kms.NewManager),同 commit 一起改。
func TestLoadLegacyKey(t *testing.T) {
	os.Unsetenv("KEY_LEGACY")
	defer os.Unsetenv("KEY_LEGACY")
	// 空 → (nil, nil):没配置是合法状态
	k, err := LoadLegacyKey()
	if err != nil || k != nil {
		t.Fatalf("empty env should be (nil, nil), got (%v, %v)", k, err)
	}
	// 非法 hex → error(2026-11-03 修:原来静默返 nil,v1 密文解不开却没人知道)
	os.Setenv("KEY_LEGACY", "xyz")
	k, err = LoadLegacyKey()
	if err == nil {
		t.Fatal("invalid hex should return error")
	}
	if k != nil {
		t.Fatal("invalid hex should return nil entry")
	}
	// 长度≠32 → error
	os.Setenv("KEY_LEGACY", "deadbeef")
	if _, err = LoadLegacyKey(); err == nil {
		t.Fatal("non-32-byte key should return error")
	}
	// 合法
	os.Setenv("KEY_LEGACY", hex.EncodeToString(newKey(t)))
	k, err = LoadLegacyKey()
	if err != nil {
		t.Fatalf("valid env should load: %v", err)
	}
	if k == nil {
		t.Fatal("valid env should be non-nil")
	}
	if k.KeyVersion != 1 || k.KeyID != "legacy-v1" {
		t.Fatalf("legacy: %+v", k)
	}
}
