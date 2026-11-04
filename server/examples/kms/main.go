// kms —— 密钥管理服务(KMS)抽象层
//
// ## 这个包解决什么问题
//
// 业务模块只拿 Tier(pii / secret / normal),不直接读 env。
// 切换 KMS(aliyun / tencent / vault / local)只改启动配置。
//
// ## 关键点
//
//   - **启动期把 KMS 的 keys 灌到 crypto.InitKeys**,业务代码始终走
//     `crypto.EncryptFor(ctx, crypto.TierPII, ...)`
//   - **密钥配错不 panic**。M100 之前 crypto.InitKeys 在 key 非法时直接
//     panic(而它那个 `err != nil` 分支是死代码)⇒ 注释里承诺的"降级继续"
//     是假的,实际是**进程 panic 退出**。现在 InitKeys 真返 error,
//     NewManager 把错误交给调用方:main 打 Warn,服务照常起,只是分级
//     加密不可用
//   - **每个 tier 恰好一把 Active key**。ACTIVE 指向的版本不存在 ⇒
//     启动就报清楚,不是"悄悄不加密"
//   - **ACTIVE 接受 "V2" / "v2" / "2"**(老文档只写了 V<N>,运维漏个 V 就踩坑)
//   - **local provider 从 env 读**:`KEY_<TIER>_V<N>=<64 位 hex>` +
//     `KEY_<TIER>_ACTIVE=V<N>`。每个 tier 的 key 是 32 字节 hex(64 字符)
//
// ## 跑法
//
//	go run ./examples/kms
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"rapid-admin/internal/pkg/crypto"
	"rapid-admin/internal/pkg/kms"
)

func main() {
	ctx := context.Background()

	// ── 一、配置 KMS ────────────────────────────────────────────
	//
	// local provider 从 env 读密钥。生产上这些 env 由部署平台注入。
	fmt.Println("=== 装配 KMS(local provider)===")
	// 每个 tier 的 key 是 32 字节 hex(64 个字符)—— 这里用 Repeat 写死长度,
	// 免得手抄的时候多一个少一个字符(多一个就是"配错",启动期会报出来)
	setKeyEnv("PII", strings.Repeat("0123456789abcdef", 4), "V1")
	setKeyEnv("SECRET", strings.Repeat("fedcba9876543210", 4), "V1")
	setKeyEnv("NORMAL", strings.Repeat("a1b2c3d4e5f60718", 4), "V1")

	mgr, err := kms.NewManager(ctx, kms.Config{Provider: "local"})
	if err != nil {
		fmt.Println("KMS 初始化失败:", err)
		fmt.Println("  ⇒ 调用方(main)应该打 Warn 并继续起服务,而不是 panic")
		return
	}
	defer func() { _ = mgr.Close() }()
	fmt.Printf("provider = %s\n", mgr.ProviderName())

	// ── 二、key 灌进了 crypto ───────────────────────────────────
	fmt.Println()
	fmt.Println("=== crypto 侧看到的状态 ===")
	for _, tier := range []crypto.Tier{crypto.TierPII, crypto.TierSecret, crypto.TierNormal} {
		meta, err := crypto.ActiveKeyMeta(tier)
		if err != nil {
			fmt.Printf("  %-7s → 未配置: %v\n", tier, err)
			continue
		}
		fmt.Printf("  %-7s → key_id=%s version=%d\n", tier, meta.KeyID, meta.KeyVersion)
	}
	fmt.Println("  ⇒ 业务代码不关心这些 —— 它只调 crypto.EncryptFor(ctx, TierPII, …)")

	// ── 三、加密往返 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 加密往返 ===")
	ct, err := crypto.EncryptStringFor(ctx, crypto.TierPII, "13800001234")
	if err != nil {
		fmt.Println("加密失败:", err)
		return
	}
	fmt.Printf("  密文 = %s\n", ct)

	plain, err := crypto.DecryptStringFor(ctx, crypto.TierPII, ct)
	if err != nil {
		fmt.Println("解密失败:", err)
		return
	}
	fmt.Printf("  解回来 = %s\n", plain)

	// 不同 tier 用不同的 key
	ctSecret, err := crypto.EncryptStringFor(ctx, crypto.TierSecret, "third-party-api-key")
	if err != nil {
		fmt.Println("加密失败:", err)
		return
	}
	fmt.Printf("  secret tier 密文与 pii tier 不同 = %v\n", ct != ctSecret)

	// ── 四、轮转状态 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 轮转状态 ===")
	state, err := crypto.RotationState(crypto.TierPII)
	if err != nil {
		fmt.Println("取轮转状态失败:", err)
		return
	}
	fmt.Printf("  已配置       = %v\n", state.Configured)
	fmt.Printf("  active key   = %s (v%d)\n", state.ActiveKeyID, state.ActiveVersion)
	fmt.Printf("  历史版本     = %v\n", state.RetainedVersions)
	fmt.Printf("  active 是最高 = %v\n", state.ActiveIsHighest)
	fmt.Printf("  往返正常     = %v\n", state.RoundTripOK)

	// ── 五、密钥配错 → 降级继续 ─────────────────────────────────
	fmt.Println()
	fmt.Println("=== 密钥配错 ===")
	os.Setenv("KEY_PII_ACTIVE", "V9") // 指向不存在的版本
	_, err = kms.NewManager(ctx, kms.Config{Provider: "local"})
	fmt.Printf("  ACTIVE 指向不存在的版本 → err = %v\n", err)
	fmt.Println("  ⇒ 启动期报清楚,不 panic —— 业务侧照常起,只是这个 tier 不可用")

	// ── 六、provider 校验 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== provider 校验 ===")
	_, err = kms.NewManager(ctx, kms.Config{Provider: "aliyun"})
	fmt.Printf("  aliyun 但没给凭证 → err = %v\n", err)
	_, err = kms.NewManager(ctx, kms.Config{Provider: "不存在的provider"})
	fmt.Printf("  未知 provider    → err = %v\n", err)
}

// setKeyEnv 设某个 tier 的环境变量(示例里直接改进程 env)
func setKeyEnv(tier, hexKey, active string) {
	os.Setenv("KEY_"+tier+"_V1", hexKey)
	os.Setenv("KEY_"+tier+"_ACTIVE", active)
}
