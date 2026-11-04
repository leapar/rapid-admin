// AES-256-GCM 字段级加密
// 文档 §9.1 —— 用于 phone/email/idCard/bankCard 等敏感字段
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"

	"rapid-admin/internal/pkg/cryptaudit"
	"rapid-admin/internal/pkg/piiaudit"
)

// Cipher AES-256-GCM 加密器
type Cipher struct {
	gcm cipher.AEAD
}

// NewCipher key 必须 32 字节(AES-256)
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("aes-256-gcm: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{gcm: gcm}, nil
}

// Encrypt 加密明文,返回 Base64(nonce || ciphertext)
func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := c.gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt 解密 Base64(nonce || ciphertext) 形式的密文
func (c *Cipher) Decrypt(b64ct string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64ct)
	if err != nil {
		return nil, err
	}
	ns := c.gcm.NonceSize()
	if len(raw) < ns {
		return nil, errors.New("aes-256-gcm: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	return c.gcm.Open(nil, nonce, ct, nil)
}

// EncryptString 加密字符串便捷方法
func (c *Cipher) EncryptString(s string) (string, error) { return c.Encrypt([]byte(s)) }

// DecryptString 解密字符串便捷方法
func (c *Cipher) DecryptString(b64ct string) (string, error) {
	b, err := c.Decrypt(b64ct)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecryptStringAudited §1.5 #5 P0 + §1.5 #8 P1 —— 解密字符串并自动记 PII + 密钥审计
//
// 业务代码用这个一行替代 DecryptString + piiaudit.Record 两次调用:
//
//	phone, err := cipher.DecryptStringAudited(ctx, owner.PhoneCipher, "owner", owner.ID, "phone")
//
// 注:
//   - 解密失败也落 cryptaudit(便于"哪个 key 解不开"排查)
//   - 成功落 cryptaudit(谁用 key 解了什么)+ piiaudit(PII 字段访问)
//   - 异步批量写,不阻塞业务请求
func (c *Cipher) DecryptStringAudited(
	ctx context.Context,
	b64ct string,
	resourceType string,
	resourceID uint64,
	field string,
) (string, error) {
	s, err := c.DecryptString(b64ct)
	meta := FieldKeyMeta()
	if err != nil {
		cryptaudit.Record(ctx, cryptaudit.Event{
			KeyID: meta.KeyID, KeyVersion: meta.KeyVersion, Algorithm: meta.Algorithm,
			Operation: "decrypt", ResourceType: resourceType, ResourceID: resourceID,
			ByteCount: len(b64ct), Success: false, ErrorMsg: err.Error(),
		})
		return "", err
	}
	cryptaudit.Record(ctx, cryptaudit.Event{
		KeyID: meta.KeyID, KeyVersion: meta.KeyVersion, Algorithm: meta.Algorithm,
		Operation: "decrypt", ResourceType: resourceType, ResourceID: resourceID,
		ByteCount: len(s), Success: true,
	})
	piiaudit.Record(ctx, piiaudit.Access{
		Action:       "read",
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Field:        field,
	})
	return s, nil
}

// EncryptStringAudited §1.5 #8 P1 —— 加密字符串并自动记密钥审计
//
// 业务代码新增/编辑敏感字段时一行调:
//
//	cipher, err := crypto.Field().EncryptStringAudited(ctx, phone, "owner", owner.ID, "phone")
//
// 注:
//   - 加密失败也落 cryptaudit(看哪个 key 加密失败,大概率是 rand reader 问题,极少)
//   - 不写 piiaudit(写操作不算 PII 访问)
func (c *Cipher) EncryptStringAudited(
	ctx context.Context,
	s string,
	resourceType string,
	resourceID uint64,
	field string,
) (string, error) {
	ct, err := c.EncryptString(s)
	meta := FieldKeyMeta()
	if err != nil {
		cryptaudit.Record(ctx, cryptaudit.Event{
			KeyID: meta.KeyID, KeyVersion: meta.KeyVersion, Algorithm: meta.Algorithm,
			Operation: "encrypt", ResourceType: resourceType, ResourceID: resourceID,
			ByteCount: len(s), Success: false, ErrorMsg: err.Error(),
		})
		return "", err
	}
	cryptaudit.Record(ctx, cryptaudit.Event{
		KeyID: meta.KeyID, KeyVersion: meta.KeyVersion, Algorithm: meta.Algorithm,
		Operation: "encrypt", ResourceType: resourceType, ResourceID: resourceID,
		ByteCount: len(s), Success: true,
	})
	return ct, nil
}
