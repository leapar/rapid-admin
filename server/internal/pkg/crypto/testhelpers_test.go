package crypto

import (
	"encoding/base64"
	"testing"
)

// b64Decode 帮手
func b64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// mustSeal 测试助手:用任意 version 构造 envelope(不真加密,只标头)
//
//	仅用于 unit test "未知 version 解密" 场景
func mustSeal(version byte, _ string) string {
	c, _ := NewCipher(make([]byte, 32)) // dummy 32B key
	nonce := make([]byte, c.gcm.NonceSize())
	ct := c.gcm.Seal(nil, nonce, []byte("placeholder"), nil)
	wrapped := envelopeWrap(version, append(nonce, ct...))
	return base64.StdEncoding.EncodeToString(wrapped)
}

// 静默 unused import
var _ = testing.Short
