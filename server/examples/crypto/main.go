// crypto —— 字段级加密 + 盲索引
//
// ## 这个包解决什么问题
//
// 敏感字段(手机号、身份证、银行卡)不能明文落库,但又不能丢:
// 需要能查、能比对、能改。
//
// 两套原语:
//
//   - Cipher:AES-256-GCM 信封加密 —— 存的是密文,可解密还原
//   - BlindIndex:HMAC-SHA256 盲索引 —— 存的是不可逆摘要,可做"完全相等"查询
//
// ## 密钥分级(tier)
//
//   - pii:身份证 / 银行卡 / 手机号 —— 最敏感
//   - secret:密码哈希、API 密钥
//   - normal:一般业务字段
//
// 分级不是装饰:**不同 tier 用不同 key**,一把 key 泄漏不会波及全部。
//
// ## 轮转
//
// 每个 tier 可以挂多把 key(KeyVersion 递增),只有 Active=true 的那把用于新加密,
// 老的保留用于解旧密文。后台任务扫老 ciphertext → 用新 key 重加密 → 落库。
//
// ## 关键点
//
//   - InitKeys **不再 panic,改为返 error**。以前 key 配错直接 panic,
//     现在启动阶段拿到 error,该 fail 的地方才 fail。
//   - 每个 tier 必须恰好一把 Active 的 key。
//   - 密文是信封格式:首字节是格式版本,后面带 key version ⇒ 能自己认出用哪把 key 解。
//
// ## 跑法
//
//	go run ./examples/crypto
package main

import (
	"context"
	"fmt"

	"rapid-admin/internal/pkg/crypto"
)

func main() {
	ctx := context.Background()

	// ── 一、装配密钥 ────────────────────────────────────────────
	//
	// 真实环境从 KMS / 环境变量拿,示例用固定 key。
	piiKey := []byte("0123456789abcdef0123456789abcdef") // 32 字节 = AES-256
	secretKey := []byte("abcdef0123456789abcdef0123456789")
	normalKey := []byte("6789abcdef0123456789abcdef012345")

	err := crypto.InitKeys(crypto.KeysConfig{
		PII: []crypto.KeyEntry{
			{KeyID: "env:PII_KEY", KeyVersion: 1, Key: piiKey, Active: true},
		},
		Secret: []crypto.KeyEntry{
			{KeyID: "env:SECRET_KEY", KeyVersion: 1, Key: secretKey, Active: true},
		},
		Normal: []crypto.KeyEntry{
			{KeyID: "env:NORMAL_KEY", KeyVersion: 1, Key: normalKey, Active: true},
		},
	})
	if err != nil {
		fmt.Println("密钥初始化失败:", err)
		return
	}

	// ── 二、密钥元信息(审计用)──────────────────────────────────
	fmt.Println("=== 密钥元信息 ===")
	meta, err := crypto.ActiveKeyMeta(crypto.TierPII)
	if err != nil {
		fmt.Println("读元信息失败:", err)
		return
	}
	fmt.Printf("PII tier: key_id=%s version=%d algorithm=%s\n", meta.KeyID, meta.KeyVersion, meta.Algorithm)

	// ── 三、字段加密 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 字段加密 ===")
	plain := "110101199001011234"
	ct, err := crypto.EncryptStringFor(ctx, crypto.TierPII, plain)
	if err != nil {
		fmt.Println("加密失败:", err)
		return
	}
	fmt.Printf("明文 = %s\n", plain)
	fmt.Printf("密文 = %s\n", ct)

	back, err := crypto.DecryptStringFor(ctx, crypto.TierPII, ct)
	if err != nil {
		fmt.Println("解密失败:", err)
		return
	}
	fmt.Printf("解密 = %s (一致=%v)\n", back, back == plain)

	// 不同 tier 用不同 key ⇒ 密文不同
	other, _ := crypto.EncryptStringFor(ctx, crypto.TierNormal, plain)
	fmt.Printf("换 tier 加密 = %s\n", other)
	fmt.Println("不同 tier 用不同 key ⇒ 同明文的密文不一样")

	// ── 四、盲索引:不可逆,但能"完全相等"查询 ───────────────────
	fmt.Println()
	fmt.Println("=== 盲索引 ===")
	bi, err := crypto.NewBlindIndex([]byte("blind-index-key-32-bytes-long!!"))
	if err != nil {
		fmt.Println("盲索引初始化失败:", err)
		return
	}
	h1 := bi.Compute("13800001234")
	h2 := bi.Compute("13800001234")
	h3 := bi.Compute("13800009999")
	fmt.Printf("同一个手机号两次盲索引一致 = %v\n", h1 == h2)
	fmt.Printf("不同手机号盲索引不同     = %v\n", h1 != h3)
	fmt.Printf("盲索引值 = %s\n", h1)
	fmt.Println("盲索引**不能解密** —— 只能拿同样的明文算出同样的值去比对")

	// ComputeNorm:归一化后再算(去掉空格 / 大小写),避免"同号不同格式"查不到
	fmt.Println()
	fmt.Println("=== 归一化 ===")
	fmt.Printf("138 0000 1234 与 13800001234 的归一化盲索引一致 = %v\n",
		bi.ComputeNorm("138 0000 1234") == bi.ComputeNorm("13800001234"))

	// ── 五、密钥轮转就绪状态 ────────────────────────────────────
	//
	// ⚠️ 这个结构能测什么、不能测什么,分清楚:
	//
	// 能测:哪些 tier 配了 key、active 是哪一把、历史堆了几把、
	//       active 是不是最高版本、加解密往返通不通。
	//
	// **不能测**:"历史上有没有数据是用已退役的 key 加密的" ——
	// 那要扫业务表的密文列,每张表在哪、怎么批量重写只有业务自己知道。
	// 一个天天报"一切正常"却看不见真问题的检查,比没有检查更坏。
	fmt.Println()
	fmt.Println("=== 轮转就绪状态 ===")
	state, err := crypto.RotationState(crypto.TierPII)
	if err != nil {
		fmt.Println("读轮转状态失败:", err)
		return
	}
	fmt.Printf("已配置     = %v\n", state.Configured)
	fmt.Printf("active key = %s (v%d)\n", state.ActiveKeyID, state.ActiveVersion)
	fmt.Printf("历史版本   = %v\n", state.RetainedVersions)
	fmt.Printf("active 是最高版本 = %v\n", state.ActiveIsHighest)
	fmt.Printf("加解密往返正常     = %v\n", state.RoundTripOK)
	if state.Problem != "" {
		fmt.Printf("问题: %s\n", state.Problem)
	}
}
