// HMAC-SHA256 盲索引
// 文档 §9.2 —— 用于 idCard/phone 精确查找(明文不暴露,索引可查)
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// BlindIndex HMAC-SHA256 盲索引器
type BlindIndex struct {
	key []byte
}

// NewBlindIndex 创建盲索引器,key 至少 16 字节
//
// ⚠️ M100(2026-10-26):**真的校验了**。原来这里只有一句注释
// ("key 至少 16 字节"),代码一行检查都没有 —— `NewBlindIndex(nil)`
// 会造出一个**空 key**的 HMAC-SHA256。而空 key 的 HMAC 是一个
// **任何人都能算出来的公开函数**:拿到库的人拿候选明文算一遍
// HMAC-SHA256(空 key) 就能把手机号 / 身份证反查出来 ——
// 盲索引"不落明文"的全部意义当场归零。
//
// 现在短 key / nil key 直接返 error,调用方拿不到一个"看着能用、
// 实则形同明文"的索引器。
func NewBlindIndex(key []byte) (*BlindIndex, error) {
	if len(key) < BlindIndexMinKeyLen {
		return nil, fmt.Errorf("%w: need >= %d bytes, got %d",
			ErrBlindIndexKeyTooShort, BlindIndexMinKeyLen, len(key))
	}
	return &BlindIndex{key: key}, nil
}

// Compute 计算明文的盲索引(hex)
func (b *BlindIndex) Compute(plaintext string) string {
	mac := hmac.New(sha256.New, b.key)
	mac.Write([]byte(plaintext))
	return hex.EncodeToString(mac.Sum(nil))
}

// ComputeNorm 计算前先规范化(去空格、统一大小写),用于"模糊匹配"
func (b *BlindIndex) ComputeNorm(s string) string {
	return b.Compute(normalize(s))
}

// normalize 简单规范化:去掉**所有** ASCII 空格 / 制表符 + ASCII 字母转小写
//
// ⚠️ 注释与实现曾经对不上:老注释写的是"去**首尾**空格",实际实现是
// **中间的空也一并去掉**(手机号 "138 0000 1234" → "13800001234")。
// 这是**有意的** —— 同一个号码带不带空格必须算出同一个索引,
// 否则建索引和查询两边对不上,查不到人。所以老注释是错的,代码是对的,
// M100 把注释改成了与实现一致的说法。
//
// ⚠️ 只处理 ASCII:非 ASCII 空白(全角空格 U+3000 等)不处理,
// 中文姓名里如果混了全角空格,索引与查询仍能对上(两边同样地不处理),
// 但"看起来一样"的两个字符串会算出不同索引 —— 与其猜,不如明写。
func normalize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		out = append(out, c)
	}
	return string(out)
}
