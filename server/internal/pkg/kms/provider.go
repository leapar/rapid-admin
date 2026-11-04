// Package kms 密钥管理服务(KMS)抽象层(§1.5 #10 P1)
//
// 目的:
//   - 业务模块拿 Tier 即可,不直接读 env
//   - 切换 KMS(aliyun / tencent / vault / local)只改启动配置
//   - 密钥轮转 / 审计 / 灰度发布由 KMS 后端保证
//
// 用法:
//   - main 启动时:kms.Init(ctx, kms.Config{Provider: "aliyun", ...}) 或 "local"
//   - 业务模块不直接调 kms;仍是 crypto.EncryptFor(ctx, crypto.TierPII, ...)
//   - 本包负责"启动期把 KMS 的 keys 灌到 crypto.InitKeys"
//
// 实现:
//   - Provider interface:ListKeys(ctx) → map[Tier][]KeyEntry
//   - local provider:从 env 读(KEY_<TIER>_V<N> / KEY_<TIER>_ACTIVE)
//   - aliyun provider:TODO(本期 stub)— 接 aliyun-sdk-managed-kms
//   - 切换逻辑由 Config.Provider 决定("local" / "aliyun" / "tencent" / "vault")
package kms

import (
	"context"
	"errors"

	"rapid-admin/internal/pkg/crypto"
)

// Config KMS 启动配置
type Config struct {
	// Provider 实现选择:local / aliyun / tencent / vault
	Provider string
	// Endpoint KMS 服务地址(aliyun / tencent 专用)
	Endpoint string
	// AccessKey 凭证
	AccessKey string
	// SecretKey 凭证
	SecretKey string
	// Region 区域(aliyun:cn-hangzhou 等)
	Region string
	// KeyPrefix 业务前缀(隔离多业务 — 同 KMS 实例可挂多套)
	KeyPrefix string
	// CacheTTL 启动期 ListKeys 缓存(KMS 端 key 列表变化不频繁,本地缓存几小时)
	CacheTTL int // 秒,0 = 用默认 3600
}

// Validate 必填校验
func (c Config) Validate() error {
	switch c.Provider {
	case "", "local":
		return nil // local 不需要凭证
	case "aliyun", "tencent", "vault":
		if c.AccessKey == "" || c.SecretKey == "" {
			return errors.New("kms: " + c.Provider + " 需要 AccessKey + SecretKey")
		}
	default:
		return errors.New("kms: 未知 provider " + c.Provider)
	}
	return nil
}

// Provider 密钥源接口(每种实现各自一种 KMS 后端)
type Provider interface {
	// Name 自报家门
	Name() string
	// ListKeys 列出所有 tier 的所有 key version
	//   - 返回 map[tier]→entries(按 version 升序或乱序都行)
	//   - tier 名称必须用 crypto.Tier{PII,Secret,Normal}
	//   - 每个 tier 必至少 1 active
	ListKeys(ctx context.Context) (map[crypto.Tier][]crypto.KeyEntry, error)
	// Close 释放资源(SDK client 等)
	Close() error
}

// Tier 字符串到 crypto.Tier 的转换由实现内部做(避免本包 import crypto.Tier 引用)
