// KMS Manager —— 启动期选择 provider,把 keys 灌到 crypto.InitKeys
// 文档 §1.5 #10 P1
//
// 用法(在 main.go):
//
//	mgr, err := kms.NewManager(ctx, kms.Config{Provider: "local"})
//	if err != nil { log.Warn("kms init failed; tiered encrypt disabled", zap.Error(err)) }
//	defer mgr.Close()
//
// 切到 aliyun KMS:
//
//	mgr, err := kms.NewManager(ctx, kms.Config{
//	    Provider: "aliyun", Endpoint: "...", Region: "cn-hangzhou",
//	    AccessKey: "...", SecretKey: "...",
//	})
package kms

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/crypto"
	"rapid-admin/internal/pkg/kms/local"
	"rapid-admin/internal/pkg/logger"
)

// Manager KMS 中心(持 Provider + 启动装载结果)
type Manager struct {
	provider Provider
	cfg      Config
	log      *zap.Logger
}

// NewManager 选 provider + ListKeys + 灌到 crypto.InitKeys
//
//   - provider 选型失败 → 返 error,业务不挂(本地 dev 可继续)
//
//   - ⚠️ M100(2026-10-26):**密钥配错也走这条路**。原来 `crypto.InitKeys`
//     在 key 非法 / active key 数不对时直接 panic,而它那个 `err != nil`
//     分支是死代码(InitKeys 永远返 nil)→ 注释里承诺的"降级继续"是假的,
//     实际是**进程 panic 退出**。现在 InitKeys 真返 error,这条降级路径
//     才第一次真的生效:main.go 打 Warn,服务照常起,只是分级加密不可用。
//
//   - 切换 KMS 时,旧 env 本地残留会被忽略(只取 provider 返回的)
//
//   - Legacy(v1 envelope 兼容)目前仅 local 支持;aliyun 接入时单独实现
func NewManager(ctx context.Context, cfg Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	log := logger.L()
	mgr := &Manager{cfg: cfg, log: log}

	// 选 provider
	prov, err := buildProvider(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	mgr.provider = prov

	// 取 keys
	keys, err := prov.ListKeys(ctx)
	if err != nil {
		_ = prov.Close()
		return nil, fmt.Errorf("kms %s ListKeys: %w", prov.Name(), err)
	}

	// 灌到 crypto.InitKeys
	cryptoCfg := crypto.KeysConfig{
		PII:    keys[crypto.TierPII],
		Secret: keys[crypto.TierSecret],
		Normal: keys[crypto.TierNormal],
	}
	// legacy 非法走与 ListKeys 同款口径启动期上抛(KEY_LEGACY 配了但解不开
	// = v1 存量密文将永远无法解密,不能静默降级成 Warn 让运维毫无知觉)
	legacy, err := local.LoadLegacyKey()
	if err != nil {
		_ = prov.Close()
		return nil, fmt.Errorf("kms %s LoadLegacyKey: %w", prov.Name(), err)
	}
	if legacy != nil {
		cryptoCfg.Legacy = legacy
	}
	if err := crypto.InitKeys(cryptoCfg); err != nil {
		_ = prov.Close()
		return nil, fmt.Errorf("kms %s: crypto.InitKeys: %w", prov.Name(), err)
	}

	// 日志:每个 tier 报告
	for _, t := range []crypto.Tier{crypto.TierPII, crypto.TierSecret, crypto.TierNormal} {
		if crypto.HasTier(t) {
			meta, _ := crypto.ActiveKeyMeta(t)
			log.Info("kms tier ready",
				zap.String("provider", prov.Name()),
				zap.String("tier", string(t)),
				zap.String("key_id", meta.KeyID),
				zap.Int("key_version", meta.KeyVersion),
				zap.Int("keys_count", len(keys[t])))
		}
	}
	return mgr, nil
}

// ProviderName 返回当前激活的 provider 名
func (m *Manager) ProviderName() string {
	if m == nil || m.provider == nil {
		return ""
	}
	return m.provider.Name()
}

// Close 关闭底层 provider(SDK 资源释放)
func (m *Manager) Close() error {
	if m == nil || m.provider == nil {
		return nil
	}
	return m.provider.Close()
}

// buildProvider 工厂:根据 cfg.Provider 选实现
func buildProvider(ctx context.Context, cfg Config, log *zap.Logger) (Provider, error) {
	switch cfg.Provider {
	case "", "local":
		log.Info("kms using local provider (env-based, dev mode)")
		return local.New(log), nil
	case "aliyun":
		return nil, fmt.Errorf("kms: aliyun provider 暂未实现(TODO:接 aliyun-sdk-managed-kms v2);本期请用 local")
	case "tencent", "vault":
		return nil, fmt.Errorf("kms: %s provider 暂未实现;本期请用 local", cfg.Provider)
	default:
		return nil, fmt.Errorf("kms: 未知 provider %q", cfg.Provider)
	}
}
