// Local KMS provider —— 从环境变量装载密钥
// 文档 §1.5 #10 P1
//
// 设计:
//   - 沿用 M32 的 env 格式:KEY_<TIER>_V<N>=hex32B / KEY_<TIER>_ACTIVE=V<N>
//   - 完全等价于 main.go 原来的 initCryptoKeys,只是被包成 Provider
//   - 业务可平滑切换到 aliyun/tencent/vault,只改 KMS_PROVIDER
//
// env 格式:
//
//	KEY_PII_V1=hex64   # 32B AES-256 key
//	KEY_PII_V2=hex64   # 轮转时新增
//	KEY_PII_ACTIVE=V2  # 当前 active 版本
//	KEY_SECRET_V1=...
//	KEY_NORMAL_V1=...
//	KEY_LEGACY=hex64   # 旧 v1 envelope 兜底(可选)
package local

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/crypto"
	"rapid-admin/internal/pkg/logger"
)

// Provider Local KMS provider(env 装载)
type Provider struct {
	log *zap.Logger
}

// New 构造
func New(log *zap.Logger) *Provider {
	if log == nil {
		log = logger.L()
	}
	return &Provider{log: log}
}

// Name 自报家门
func (p *Provider) Name() string { return "local" }

// ListKeys 扫 env 装载所有 tier 的 keys
//
// ⚠️ M100(2026-10-26):**任何一个 tier 配错就整体返 error**。
// 理由:配错意味着"以为有 key、其实没有" —— 半装(一个 tier 好了、另一个
// 悄悄禁用)比整体不装更危险:业务会以为 PII 已经加密了。
// 错误由 kms.NewManager 传出去,main.go 打 Warn 降级(服务照常起)。
func (p *Provider) ListKeys(ctx context.Context) (map[crypto.Tier][]crypto.KeyEntry, error) {
	out := map[crypto.Tier][]crypto.KeyEntry{}
	var errs []error
	for _, t := range []struct {
		tier crypto.Tier
		env  string
	}{
		{crypto.TierPII, "PII"},
		{crypto.TierSecret, "SECRET"},
		{crypto.TierNormal, "NORMAL"},
	} {
		entries, err := loadTier(t.env, p.log)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out[t.tier] = entries
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// Close no-op
func (p *Provider) Close() error { return nil }

// loadTier 扫 env,组装一个 tier 的 keys(active=1,其他仅解)
//
// ⚠️ M100(2026-10-26):ACTIVE 配了却找不到可用 key 时**返 error**。
// 原来这两种情况的结果完全相反:
//   - ACTIVE=V2 但 V2 没配(而 V1 配了)→ 出来的 entries 全是 Active=false
//     → crypto 侧 panic("must have exactly 1 active key")→ **进程退出**
//   - ACTIVE=V2 但 V1/V2 都没配 → entries 为空 → 该 tier 被**静默禁用**
//
// 同一个错因(ACTIVE 写错),两种结局:一个崩进程,一个悄悄不加密。
// 现在统一成"启动时报清楚",不再有 panic 路径。
//
// ACTIVE 的值接受 "V2" / "v2" / "2"(老文档只写了 V<N>,运维漏个 V 就踩坑)。
func loadTier(tierName string, log *zap.Logger) ([]crypto.KeyEntry, error) {
	prefix := "KEY_" + tierName + "_"
	activeRaw := os.Getenv(prefix + "ACTIVE")
	if activeRaw == "" {
		log.Debug("tier has no active key, encrypt disabled",
			zap.String("tier", tierName),
			zap.String("env", prefix+"ACTIVE"))
		return nil, nil
	}
	activeVer, err := parseVersion(activeRaw)
	if err != nil {
		return nil, fmt.Errorf("kms(local): %sACTIVE=%q 不是合法版本号(应为 V<N> 或 <N>,1..255)", prefix, activeRaw)
	}

	var out []crypto.KeyEntry
	activeFound := false
	for v := 1; v <= 255; v++ {
		ver := "V" + strconv.Itoa(v)
		hexKey := os.Getenv(prefix + ver)
		if hexKey == "" {
			continue
		}
		k, err := hex.DecodeString(hexKey)
		if err != nil || len(k) != 32 {
			log.Warn("skip invalid key",
				zap.String("tier", tierName),
				zap.String("version", ver))
			continue
		}
		isActive := v == activeVer
		if isActive {
			activeFound = true
		}
		out = append(out, crypto.KeyEntry{
			KeyID:      prefix + ver,
			KeyVersion: byte(v),
			Key:        k,
			Active:     isActive,
		})
	}
	if !activeFound {
		return nil, fmt.Errorf(
			"kms(local): %sACTIVE=%q 没有对应的可用密钥(%sV%d 缺失,或不是 32 字节 hex)",
			prefix, activeRaw, prefix, activeVer)
	}
	return out, nil
}

// parseVersion 解析 ACTIVE 版本号:接受 "V2" / "v2" / "2"(1..255)
func parseVersion(s string) (int, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, errors.New("empty version")
	}
	if t[0] == 'V' || t[0] == 'v' {
		t = t[1:]
	}
	n, err := strconv.Atoi(t)
	if err != nil {
		return 0, err
	}
	if n < 1 || n > 255 {
		return 0, fmt.Errorf("version out of range: %d", n)
	}
	return n, nil
}

// LoadLegacyKey 单独取出 KEY_LEGACY(供 KMS manager 走 v1 envelope 兼容路径)
//
// ⚠️ M100(2026-10-26):函数名原来叫 `LoadLegacyLegacyKey`(Legacy 写了两遍)。
// 改名会破坏调用方 —— 全仓只有一个调用方(kms.NewManager),同 commit 一起改。
//
// ⚠️ 2026-11-03 审计 P1:原来对非法值(hex 解码失败 / 长度≠32)**静默返 nil**
// —— 和"没配置"完全同形。存量 v1 密文等着这把钥匙解密,静默丢掉意味着
// 那些数据永远解不开,而运维毫无知觉。现在:未配置返 (nil, nil);
// 配置了但非法返带明文原因的 error,由 manager 沿 ListKeys 同款口径启动期上抛。
func LoadLegacyKey() (*crypto.KeyEntry, error) {
	hexKey := os.Getenv("KEY_LEGACY")
	if hexKey == "" {
		return nil, nil
	}
	k, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("kms(local): KEY_LEGACY 不是合法 hex: %v —— v1 存量密文将无法解密", err)
	}
	if len(k) != 32 {
		return nil, fmt.Errorf("kms(local): KEY_LEGACY 长度 %d 字节,应为 32 字节 hex(64 字符)—— v1 存量密文将无法解密", len(k))
	}
	return &crypto.KeyEntry{KeyID: "legacy-v1", KeyVersion: 1, Key: k}, nil
}
