package crypto

import (
	"context"
	"errors"
	"testing"
)

// testKey32 生成确定性的 32B 测试 key(version 1..N 给不同值便于辨别)
func testKey32(seed byte) []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = seed
	}
	return k
}

func TestEncryptDecryptFor_PII(t *testing.T) {
	_ = InitKeys(KeysConfig{
		PII: []KeyEntry{
			{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true},
		},
	})
	ct, err := EncryptStringFor(context.Background(), TierPII, "13800001234")
	if err != nil {
		t.Fatal(err)
	}
	if ct == "" {
		t.Fatal("ciphertext empty")
	}
	// 验 envelope 前缀:Base64 解码后第 1 字节必须是 0x02
	raw, _ := b64Decode(ct)
	if len(raw) < 2 || raw[0] != EnvelopeFormatVersion {
		t.Fatalf("envelope format version missing, got %x", raw[:2])
	}
	if raw[1] != 1 {
		t.Fatalf("key version wrong, got %d", raw[1])
	}
	// 解密回原文
	pt, err := DecryptStringFor(context.Background(), TierPII, ct)
	if err != nil {
		t.Fatal(err)
	}
	if pt != "13800001234" {
		t.Fatalf("decrypt mismatch: %s", pt)
	}
}

func TestKeyRotation_OldKeyStillDecrypts(t *testing.T) {
	// 模拟 V1 时期
	_ = InitKeys(KeysConfig{
		PII: []KeyEntry{
			{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true},
		},
	})
	oldCT, err := EncryptStringFor(context.Background(), TierPII, "old-data")
	if err != nil {
		t.Fatal(err)
	}
	// 切到 V2 active,V1 保留仅解
	_ = InitKeys(KeysConfig{
		PII: []KeyEntry{
			{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: false},
			{KeyID: "pii-v2", KeyVersion: 2, Key: testKey32(0x22), Active: true},
		},
	})
	// 新加密 → 走 V2
	newCT, _ := EncryptStringFor(context.Background(), TierPII, "new-data")
	raw, _ := b64Decode(newCT)
	if raw[1] != 2 {
		t.Fatalf("new ct should be v2, got v%d", raw[1])
	}
	// 旧密文 → 还能解(走 V1)
	pt, err := DecryptStringFor(context.Background(), TierPII, oldCT)
	if err != nil {
		t.Fatalf("old ct should still decrypt: %v", err)
	}
	if pt != "old-data" {
		t.Fatalf("old decrypt mismatch: %s", pt)
	}
	// 新密文 → 也能解
	pt2, _ := DecryptStringFor(context.Background(), TierPII, newCT)
	if pt2 != "new-data" {
		t.Fatalf("new decrypt mismatch: %s", pt2)
	}
}

func TestKeyTiering_DifferentTiersDifferentKeys(t *testing.T) {
	_ = InitKeys(KeysConfig{
		PII:    []KeyEntry{{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true}},
		Secret: []KeyEntry{{KeyID: "sec-v1", KeyVersion: 1, Key: testKey32(0x77), Active: true}},
		Normal: []KeyEntry{{KeyID: "nrm-v1", KeyVersion: 1, Key: testKey32(0xAA), Active: true}},
	})
	// 同 plaintext 不同 tier 出来的密文不同
	piiCT, _ := EncryptStringFor(context.Background(), TierPII, "same")
	secCT, _ := EncryptStringFor(context.Background(), TierSecret, "same")
	nrmCT, _ := EncryptStringFor(context.Background(), TierNormal, "same")
	if piiCT == secCT || secCT == nrmCT || piiCT == nrmCT {
		t.Fatal("different tier keys should produce different ciphertexts")
	}
	// 互相解不开
	if _, err := DecryptStringFor(context.Background(), TierPII, secCT); err == nil {
		t.Fatal("PII decrypt of Secret ciphertext should fail")
	}
}

func TestUnregisteredTier(t *testing.T) {
	_ = InitKeys(KeysConfig{
		PII: []KeyEntry{{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true}},
	})
	_, err := EncryptStringFor(context.Background(), TierSecret, "x")
	if err == nil {
		t.Fatal("encrypt on unregistered tier should fail")
	}
	if !HasTier(TierSecret) {
		// 期望:HasTier 返 false
	}
}

func TestLegacyV1Decrypt(t *testing.T) {
	// 用 legacy key 加密一份 v1 格式
	legacy := testKey32(0x99)
	c, _ := NewCipher(legacy)
	v1CT, err := c.EncryptString("legacy-data")
	if err != nil {
		t.Fatal(err)
	}
	// 注册 v1 legacy + v2 PII active
	_ = InitKeys(KeysConfig{
		PII:    []KeyEntry{{KeyID: "pii-v2", KeyVersion: 2, Key: testKey32(0x22), Active: true}},
		Legacy: &KeyEntry{KeyID: "legacy-v1", KeyVersion: 1, Key: legacy},
	})
	pt, err := DecryptStringFor(context.Background(), TierPII, v1CT)
	if err != nil {
		t.Fatalf("legacy v1 should still decrypt: %v", err)
	}
	if pt != "legacy-data" {
		t.Fatalf("legacy decrypt mismatch: %s", pt)
	}
}

func TestUnknownKeyVersion(t *testing.T) {
	_ = InitKeys(KeysConfig{
		PII: []KeyEntry{{KeyID: "pii-v1", KeyVersion: 1, Key: testKey32(0x11), Active: true}},
	})
	// 手工造一个 envelope 标 v99
	rawCT := mustSeal(99, "test-data")
	if _, err := DecryptStringFor(context.Background(), TierPII, rawCT); !errors.Is(err, ErrUnknownKeyVersion) {
		t.Fatalf("expected ErrUnknownKeyVersion, got %v", err)
	}
}
