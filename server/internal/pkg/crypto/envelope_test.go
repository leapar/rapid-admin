package crypto

import (
	"context"
	"encoding/base64"
	"testing"
)

// makeV1Ciphertext 造一份 **v1 格式**密文(nonce||ct,没有 envelope 头)
//
// nonce 的第 1 字节可指定 —— 这是本文件所有用例的关键:
// v1 的首字节是**随机 nonce 的第 1 字节**,而 v2 envelope 的首字节必须
// **恰好**等于 0x02 → 每 256 份 v1 密文里就有 1 份会被误判成 v2。
//
// ⚠️ M100 修的就是这条:老实现"只看首字节就下结论",那份密文**永久解不开**
// (报 ErrUnknownKeyVersion,不是报错 —— 是数据丢了)。
func makeV1Ciphertext(t *testing.T, key []byte, nonceFirstByte byte, plaintext string) string {
	t.Helper()
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, c.gcm.NonceSize())
	nonce[0] = nonceFirstByte
	for i := 1; i < len(nonce); i++ {
		nonce[i] = byte(i * 7)
	}
	ct := c.gcm.Seal(nil, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, ct...))
}

// TestDecryptFor_V1CiphertextStartingWith0x02_StillDecrypts
//
// # M100 修的那个数据丢失 bug
//
// v1 密文的 nonce 第 1 字节恰好等于 0x02(= envelope format version)时,
// 老实现会把它当成 v2 envelope 解析 → 拿 nonce 第 2 字节当 key version →
// 查不到 key → ErrUnknownKeyVersion → **那份 PII 永久解不开**。
//
// 修法:不靠猜靠试 —— v2 解不开就按 v1 再试一次(GCM 带认证,解不开就是解不开)。
func TestDecryptFor_V1CiphertextStartingWith0x02_StillDecrypts(t *testing.T) {
	legacy := testKey32(0x99)
	ct := makeV1Ciphertext(t, legacy, EnvelopeFormatVersion, "v1-ambiguous")

	if err := InitKeys(KeysConfig{
		PII:    []KeyEntry{{KeyID: "pii-v2", KeyVersion: 2, Key: testKey32(0x22), Active: true}},
		Legacy: &KeyEntry{KeyID: "legacy-v1", KeyVersion: 1, Key: legacy},
	}); err != nil {
		t.Fatal(err)
	}

	pt, err := DecryptStringFor(context.Background(), TierPII, ct)
	if err != nil {
		t.Fatalf("v1 密文(首字节 0x02)必须能解开,实际报错: %v", err)
	}
	if pt != "v1-ambiguous" {
		t.Fatalf("解密结果 = %q, want %q", pt, "v1-ambiguous")
	}
}

// TestDecryptFor_V1AllPossibleFirstBytes_Decrypt
//
// 把首字节的**全部 256 种取值**都跑一遍 —— 不是抽样,是穷举。
// 首字节 = 0x02 的那一种是"看起来像 v2"的最坏情况,其余 255 种是普通情况。
//
// ⚠️ 这条用例是**确定性**的(不靠随机撞 1/256),所以它不会时红时绿。
func TestDecryptFor_V1AllPossibleFirstBytes_Decrypt(t *testing.T) {
	legacy := testKey32(0x99)
	if err := InitKeys(KeysConfig{
		PII:    []KeyEntry{{KeyID: "pii-v2", KeyVersion: 2, Key: testKey32(0x22), Active: true}},
		Legacy: &KeyEntry{KeyID: "legacy-v1", KeyVersion: 1, Key: legacy},
	}); err != nil {
		t.Fatal(err)
	}

	for first := 0; first < 256; first++ {
		ct := makeV1Ciphertext(t, legacy, byte(first), "payload")
		pt, err := DecryptStringFor(context.Background(), TierPII, ct)
		if err != nil {
			t.Fatalf("首字节 0x%02x 的 v1 密文解不开: %v", first, err)
		}
		if pt != "payload" {
			t.Fatalf("首字节 0x%02x 解密结果 = %q", first, pt)
		}
	}
}

// TestDecryptFor_V2EnvelopeStillDecrypts 回退逻辑不能把真 v2 弄坏
//
// v2 envelope 的首字节永远是 0x02 → 一定先走 v2 分支 → v2 分支成功就不回退。
func TestDecryptFor_V2EnvelopeStillDecrypts(t *testing.T) {
	if err := InitKeys(KeysConfig{
		PII:    []KeyEntry{{KeyID: "pii-v2", KeyVersion: 2, Key: testKey32(0x22), Active: true}},
		Legacy: &KeyEntry{KeyID: "legacy-v1", KeyVersion: 1, Key: testKey32(0x99)},
	}); err != nil {
		t.Fatal(err)
	}
	ct, err := EncryptStringFor(context.Background(), TierPII, "v2-data")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		t.Fatal(err)
	}
	if raw[0] != EnvelopeFormatVersion {
		t.Fatalf("v2 首字节 = 0x%02x, want 0x02", raw[0])
	}
	pt, err := DecryptStringFor(context.Background(), TierPII, ct)
	if err != nil {
		t.Fatalf("v2 密文解不开: %v", err)
	}
	if pt != "v2-data" {
		t.Fatalf("解密结果 = %q", pt)
	}
}

// TestDecryptFor_NoLegacyKey_V1CiphertextFails v1 密文没有 legacy key 时必须报错
//
// 回退逻辑不能变成"没有 legacy key 也硬解" —— 那会拿 tier 的 key 去试 v1,
// 报错信息会误导排障(以为是 key 坏了,其实是 legacy key 没配)。
func TestDecryptFor_NoLegacyKey_V1CiphertextFails(t *testing.T) {
	if err := InitKeys(KeysConfig{
		PII: []KeyEntry{{KeyID: "pii-v2", KeyVersion: 2, Key: testKey32(0x22), Active: true}},
	}); err != nil {
		t.Fatal(err)
	}
	ct := makeV1Ciphertext(t, testKey32(0x99), 0x11, "no-legacy")
	if _, err := DecryptStringFor(context.Background(), TierPII, ct); err == nil {
		t.Fatal("没有 legacy key 时 v1 密文必须解不开")
	}
	if HasLegacyKey() {
		t.Fatal("HasLegacyKey 应为 false")
	}
}

// TestDecodeRaw_RejectsMalformed 密文格式错的几种情况
func TestDecodeRaw_RejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"不是 base64", "!!!!not-base64!!!!"},
		{"解出来太短", base64.StdEncoding.EncodeToString([]byte("short"))},
		{"空串", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := decodeRaw(c.in); err == nil {
				t.Fatal("应该报 ErrMalformedEnvelope")
			}
		})
	}
}

// TestDecodeRaw_AcceptsMinimumLength 最短合法密文(nonce 12 + tag 16)必须能过
func TestDecodeRaw_AcceptsMinimumLength(t *testing.T) {
	raw := make([]byte, minEnvelopeLen)
	if _, err := decodeRaw(base64.StdEncoding.EncodeToString(raw)); err != nil {
		t.Fatalf("最短长度应该合法: %v", err)
	}
}

// TestEnvelopeUnwrap_RoutesByFormatVersion envelope 头的路由
func TestEnvelopeUnwrap_RoutesByFormatVersion(t *testing.T) {
	// 造够长的原始字节(nonce 12 + tag 16 = 28 起)
	raw := make([]byte, minEnvelopeLen+4)
	for i := range raw {
		raw[i] = byte(i + 1)
	}

	// v2:首字节 0x02
	raw[0] = EnvelopeFormatVersion
	raw[1] = 0x07
	envVer, keyVer, rawCT, err := envelopeUnwrap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if envVer != EnvelopeFormatVersion || keyVer != 0x07 {
		t.Fatalf("envVer=%d keyVer=%d", envVer, keyVer)
	}
	if len(rawCT) != len(raw)-2 {
		t.Fatalf("rawCT 长度 = %d, want %d", len(rawCT), len(raw)-2)
	}

	// v1:首字节不是 0x02 → 原样返回
	raw[0] = 0x03
	envVer, keyVer, rawCT, err = envelopeUnwrap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if envVer != 0 || keyVer != 0 {
		t.Fatalf("v1 应该返 (0,0),得到 (%d,%d)", envVer, keyVer)
	}
	if len(rawCT) != len(raw) {
		t.Fatalf("v1 rawCT 应该原样返回 %d 字节,得到 %d", len(raw), len(rawCT))
	}

	// 太短
	if _, _, _, err := envelopeUnwrap([]byte{0x02, 0x07}); err == nil {
		t.Fatal("太短应该报错")
	}
}
