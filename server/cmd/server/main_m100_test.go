package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/crypto"
)

// TestInitFieldCrypto_EmptyConfig_NoError 没配 key 不是错误
//
// 配置为空 = 本期业务线还没接 PII 加密,保持"未启用"即可。
func TestInitFieldCrypto_EmptyConfig_NoError(t *testing.T) {
	beforeField, beforeBlind := crypto.TryField(), crypto.TryBlind()
	if err := initFieldCrypto(config.CryptoConfig{}); err != nil {
		t.Fatalf("没配 key 不该报错: %v", err)
	}
	if crypto.TryField() != beforeField || crypto.TryBlind() != beforeBlind {
		t.Fatal("没配 key 时不该动全局状态")
	}
}

// TestInitFieldCrypto_ValidKey_Initializes 配了就必须真的装载
//
// # M100 修的那个 bug
//
// `crypto.field_key` / `crypto.blind_key` 在配置里存在(`config.CryptoConfig`
// 也解出来了),但**全仓没有任何代码读它们** —— `crypto.Field()` /
// `crypto.Blind()` 从来没被初始化过,业务代码一旦调就是
// panic("not initialized")。
//
// 清单上 §1.5 #3「全局 cipher + 盲索引注入」标着 ✅,
// 实际上**从来没注入过** —— 一个"写完了但从没跑过"的 ✅。
func TestInitFieldCrypto_ValidKey_Initializes(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	err := initFieldCrypto(config.CryptoConfig{
		FieldKey: base64.StdEncoding.EncodeToString(key),
		BlindKey: "0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("合法配置不该报错: %v", err)
	}
	if crypto.TryField() == nil {
		t.Fatal("字段加密器没装载")
	}
	if crypto.TryBlind() == nil {
		t.Fatal("盲索引器没装载")
	}
	// 真能用:加解密往返
	c := crypto.TryField()
	ct, err := c.EncryptString("13800001234")
	if err != nil {
		t.Fatal(err)
	}
	pt, err := c.DecryptString(ct)
	if err != nil {
		t.Fatal(err)
	}
	if pt != "13800001234" {
		t.Fatalf("解密结果 = %q", pt)
	}
}

// TestInitFieldCrypto_InvalidKey_ReturnsError 配错了必须报错且**不初始化**
//
// 配错时如果还拿错误的 key 去初始化,那 PII 会被加密成**解不开的东西** ——
// 比"没启用"糟得多。
func TestInitFieldCrypto_InvalidKey_ReturnsError(t *testing.T) {
	beforeField := crypto.TryField()
	err := initFieldCrypto(config.CryptoConfig{FieldKey: "too-short"})
	if err == nil {
		t.Fatal("key 长度不对必须报错")
	}
	if !strings.Contains(err.Error(), "field_key") {
		t.Fatalf("错误信息要点明是哪个 key: %v", err)
	}
	if crypto.TryField() != beforeField {
		t.Fatal("失败的初始化不该改变全局加密器")
	}
}

// TestDecodeFieldKey 字段密钥的几种写法都要认
//
// 生产环境由 K8s Secret 注入 base64;本地开发可能直接写 32 字节原文 ——
// 两种都认,省得运维为了"必须是 base64"再写一层转换。
func TestDecodeFieldKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	cases := []struct {
		name string
		in   string
	}{
		{"标准 base64", base64.StdEncoding.EncodeToString(raw)},
		{"无 padding base64", base64.RawStdEncoding.EncodeToString(raw)},
		{"32 字节原文", string(raw)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := decodeFieldKey(c.in)
			if err != nil {
				t.Fatalf("不该报错: %v", err)
			}
			if len(got) != 32 {
				t.Fatalf("长度 = %d, want 32", len(got))
			}
			for i := range got {
				if got[i] != raw[i] {
					t.Fatalf("字节 %d = %d, want %d", i, got[i], raw[i])
				}
			}
		})
	}
	// 长度不对
	for _, bad := range []string{"", "short", strings.Repeat("x", 31), strings.Repeat("x", 33)} {
		if _, err := decodeFieldKey(bad); err == nil {
			t.Fatalf("%q 应该报错", bad)
		}
	}
}
