// 加密/盲索引 全局访问入口(由 main 注入,业务方直接调)
package crypto

import (
	"errors"
	"sync"
)

/**
 * KeyMeta 用于密钥使用审计(§1.5 #8 P1)
 *   - KeyID:密钥标识(env var 名 / KMS key id),入库 sys_crypto_audit_log.key_id
 *   - KeyVersion:密钥版本(轮转用,审计能区分"老 key 还在被用")
 *   - Algorithm:算法名(aes-256-gcm / hmac-sha256 / ...)
 *
 * 由 main.go 在 InitFieldCipher/InitBlindIndex 时设置,业务包通过 KeyMeta() 读
 */
type KeyMeta struct {
	KeyID      string
	KeyVersion int
	Algorithm  string
}

var (
	mu        sync.RWMutex
	field     *Cipher
	blind     *BlindIndex
	fieldMeta = KeyMeta{KeyID: "field-cipher-default", KeyVersion: 1, Algorithm: "aes-256-gcm"}
	blindMeta = KeyMeta{KeyID: "blind-index-default", KeyVersion: 1, Algorithm: "hmac-sha256"}
)

// InitFieldCipher 注入 AES-256-GCM 字段加密器(key 32B)
func InitFieldCipher(key []byte) error {
	return InitFieldCipherWithMeta(key, fieldMeta)
}

// InitFieldCipherWithMeta 带 key 元信息初始化(密钥轮转时使用)
func InitFieldCipherWithMeta(key []byte, meta KeyMeta) error {
	c, err := NewCipher(key)
	if err != nil {
		return err
	}
	mu.Lock()
	field = c
	if meta.KeyID != "" {
		fieldMeta = meta
	}
	mu.Unlock()
	return nil
}

// InitBlindIndex 注入 HMAC-SHA256 盲索引器(key >=16B)
//
// ⚠️ M100(2026-10-26):现在**返 error**。原来 key 长度完全不校验 ——
// `InitBlindIndex(nil)` 会得到一个**空 key 的 HMAC**,而空 key 的 HMAC-SHA256
// 等价于"任何人都能算出来的公开哈希" —— 盲索引的全部意义(不暴露明文、
// 只在库里存不可逆的派生值)当场归零:拿到库的人直接对候选明文算一遍
// HMAC-SHA256 就能把手机号反查出来。
//
// 之前那句"key 至少 16 字节"只写在注释里,**没有任何代码兑现**。
func InitBlindIndex(key []byte) error {
	mu.RLock()
	meta := blindMeta
	mu.RUnlock()
	return InitBlindIndexWithMeta(key, meta)
}

// InitBlindIndexWithMeta 带 key 元信息初始化
func InitBlindIndexWithMeta(key []byte, meta KeyMeta) error {
	b, err := NewBlindIndex(key)
	if err != nil {
		return err
	}
	mu.Lock()
	blind = b
	if meta.KeyID != "" {
		blindMeta = meta
	}
	mu.Unlock()
	return nil
}

// Field 取得字段加密器(未注入则 panic —— 应在 main 启动时已初始化)
func Field() *Cipher {
	mu.RLock()
	defer mu.RUnlock()
	if field == nil {
		panic("crypto.Field: not initialized, call InitFieldCipher in main")
	}
	return field
}

// Blind 取得盲索引器
func Blind() *BlindIndex {
	mu.RLock()
	defer mu.RUnlock()
	if blind == nil {
		panic("crypto.Blind: not initialized, call InitBlindIndex in main")
	}
	return blind
}

// FieldKeyMeta 取字段加密器当前 key 元信息(给 cryptaudit 用)
func FieldKeyMeta() KeyMeta {
	mu.RLock()
	defer mu.RUnlock()
	return fieldMeta
}

// BlindKeyMeta 取盲索引当前 key 元信息
func BlindKeyMeta() KeyMeta {
	mu.RLock()
	defer mu.RUnlock()
	return blindMeta
}

// TryField 安全获取,未初始化时返回 nil(用于单元测试/降级)
func TryField() *Cipher {
	mu.RLock()
	defer mu.RUnlock()
	return field
}

// TryBlind 安全获取盲索引器
func TryBlind() *BlindIndex {
	mu.RLock()
	defer mu.RUnlock()
	return blind
}

// BlindIndexMinKeyLen 盲索引 key 最小字节数(HMAC-SHA256 的下限)
const BlindIndexMinKeyLen = 16

// ErrBlindIndexKeyTooShort 盲索引 key 长度不足
var ErrBlindIndexKeyTooShort = errors.New("crypto: blind index key too short")
