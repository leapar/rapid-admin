// 密文信封(envelope) —— 带格式版本 + 密钥版本的密文包装
// 文档 §1.5 #6 P1 密钥轮转 + §1.5 #7 P1 密钥分级
//
// 目的:
//   - 把"哪把 key 加密的"信息嵌进密文本身(不需要 DB 列)
//   - 旧 key 还在用也能解密(双写期 / 平滑轮转期)
//   - 显式标注密钥版本(便于审计 §1.5 #8)
//
// 格式:
//
//	v2(当前):Base64( 0x02 || key_version[1] || nonce[12] || ciphertext[...] )
//	  第 1 字节 0x02 = envelope format version
//	  第 2 字节 = key version(1..255)
//	v1(历史):Base64( nonce[12] || ciphertext[...] )  ← 老代码生成,本包可读不可写
//
// 路由:
//   - 加密:走 active key(每个 tier 一个)
//   - 解密:按 envelope 标 key_version 找对应 key;未标(v1)→ 试 v1 key
//
// # ⚠️ M100(2026-10-26):v1 / v2 的**首字节歧义**
//
// v1 格式的第 1 字节是**随机 nonce 的第 1 个字节** —— 而 v2 格式的第 1 字节
// 必须**恰好**等于 0x02。于是:**每 256 份 v1 密文里就有 1 份**会被当成 v2
// envelope 解析,后面跟着的 nonce 字节被当成 key version → 查不到 key →
// 报 ErrUnknownKeyVersion → **那份数据永久解不开**(不是报错,是丢数据)。
//
// 单条看不出来,但 PII 表是批量加密的:1 万条历史密文里大约 39 条中招,
// 每一条都要人工去捞原始明文重新加密 —— 而 PII 恰恰是最不该"人工捞"的字段。
//
// 修法:**不靠猜,靠试**。v2 是**带认证**的(GCM tag),解不开就是解不开 ——
// 所以 v2 解释失败时,再按 v1 解释一次(用 legacy key)。反过来不成立:
// 真 v2 envelope 的首字节永远是 0x02,一定会先走 v2 分支。
// 唯一理论上的误判是"v1 密文的前 2 字节恰好是 0x02 + 某个已注册版本号,
// 且剩下的字节还能通过 GCM 认证" —— 概率约 2^-128,可以忽略。
package crypto

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// EnvelopeFormatVersion 当前 envelope 格式版本
const EnvelopeFormatVersion byte = 0x02

// minEnvelopeLen 最短合法密文长度:nonce(12B) + GCM tag(16B)
//
// v1 是 nonce+ct,v2 是 2 字节头+nonce+ct —— 两者的下界都是 nonce+tag。
const minEnvelopeLen = 12 + 16

// ErrUnknownKeyVersion 解密时找不到对应版本(可能历史 key 已被轮换删除)
var ErrUnknownKeyVersion = errors.New("crypto: ciphertext references unknown key version")

// ErrMalformedEnvelope 密文格式错(不是有效 envelope 或 v1 格式)
var ErrMalformedEnvelope = errors.New("crypto: malformed ciphertext")

// envelopeWrap 在密文前拼格式版本 + key version
func envelopeWrap(keyVersion byte, rawCT []byte) []byte {
	out := make([]byte, 0, 2+len(rawCT))
	out = append(out, EnvelopeFormatVersion, keyVersion)
	out = append(out, rawCT...)
	return out
}

// decodeRaw 解 base64 并做**长度**校验(不判断格式 —— 格式判断见 envelopeUnwrap)
//
// 解 base64 只做一次:v2 / v1 两种解释复用同一份原始字节,
// 回退逻辑才不必再解一遍。
func decodeRaw(b64ct string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64ct)
	if err != nil {
		return nil, fmt.Errorf("%w: base64 decode: %v", ErrMalformedEnvelope, err)
	}
	if len(raw) < minEnvelopeLen {
		return nil, ErrMalformedEnvelope
	}
	return raw, nil
}

// envelopeUnwrap 从**已解码**的原始字节里拆出 (envVersion, keyVersion, rawCT)
//   - v2 envelope:返 (EnvelopeFormatVersion, key version, nonce+ct, nil)
//   - v1 plain:返 (0, 0, 原始, nil) —— 让调用方走 v1 解密路径
//   - 长度不够:返 (_, _, _, ErrMalformedEnvelope)
//
// ⚠️ 返回 envVersion == EnvelopeFormatVersion **不等于**"这一定是 v2 密文"
// —— 见文件头"v1 / v2 的首字节歧义"。调用方必须在 v2 解不开时回退 v1。
func envelopeUnwrap(raw []byte) (envVersion, keyVersion byte, rawCT []byte, err error) {
	if len(raw) < minEnvelopeLen {
		return 0, 0, nil, ErrMalformedEnvelope
	}
	// v2 envelope
	if raw[0] == EnvelopeFormatVersion {
		return EnvelopeFormatVersion, raw[1], raw[2:], nil
	}
	// v1 格式(无 prefix)
	return 0, 0, raw, nil
}

// sealWithVersion 用指定 key version 包装密文
func sealWithVersion(keyVersion byte, nonce, ct []byte) string {
	wrapped := envelopeWrap(keyVersion, append(nonce, ct...))
	return base64.StdEncoding.EncodeToString(wrapped)
}
