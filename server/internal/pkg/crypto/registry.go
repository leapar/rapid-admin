// 密钥分级 + 轮转 registry
// 文档 §1.5 #6 P1 密钥轮转 + §1.5 #7 P1 密钥分级
//
// 用法:
//   - main 启动时调 InitKeys(KeysConfig{...})
//   - 业务用 EncryptFor(ctx, TierPII, plaintext) / DecryptFor(ctx, TierPII, ct)
//   - 轮转时:加新 key(KeyVersion+1,Active=true),老 key 保留(Active=false)用于解旧数据
//   - 后台 cron 扫描老 ciphertext → re-encrypt 用新 key → 持久化后下次不再走老 key
//
// # ⚠️ M100(2026-10-26)的三处改动
//
//  1. **InitKeys 不再 panic,改为返 error**。
//     原来 `mustBuildTier` 在 key 非法 / active key 数 != 1 时直接 panic ——
//     而 `kms.NewManager` 里那个 `if err := crypto.InitKeys(...); err != nil`
//     是**死代码**(InitKeys 永远返 nil)。结果是:main.go 里那句
//     "kms init failed; tiered encrypt/decrypt disabled" 的**降级承诺是假的** ——
//     配置写错(比如 `KEY_PII_ACTIVE=V2` 但 `KEY_PII_V2` 没配)不是降级,
//     是**进程 panic 退出**。
//     现在:配错 → InitKeys 返 error → NewManager 返 error → main.go 打 Warn
//     → 服务照常起,只是分级加密不可用(与注释承诺一致)。
//
//  2. **InitKeys 改成"要么全装上,要么一点不改"**。
//     原来三个 tier 逐个赋值:`tierPII` 装好了、`tierSecret` panic 了 →
//     PII 用新 key、Secret 用旧 key、Normal 还是老 registry ——
//     **半更新状态**,而轮转正是最需要原子性的时刻。
//
//  3. **tiered 路径补上密钥使用审计**(§1.5 #8 P1)。
//     原来只有 `crypto.Field()` 那条路(字段级 Cipher)落 cryptaudit,
//     而 KMS 装配好的 tiered 这条主路径**一条审计都不写** ——
//     等于"审计覆盖的是没人用的那条路,漏掉的是真在用的那条"。
package crypto

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"

	"rapid-admin/internal/pkg/cryptaudit"
)

// Tier 密钥分级(数据敏感度)
type Tier string

const (
	// TierPII 个人信息:手机/邮箱/身份证/银行卡
	TierPII Tier = "pii"
	// TierSecret 系统级密钥:API key、第三方凭证、数据库密码等
	TierSecret Tier = "secret"
	// TierNormal 普通:非敏感业务数据
	TierNormal Tier = "normal"
)

// 算法名 / 操作名(写进 sys_crypto_audit_log)
const (
	algorithmAES256GCM = "aes-256-gcm"
	opEncrypt          = "encrypt"
	opDecrypt          = "decrypt"
)

// KeyEntry 单把 key(一个 version 一个)
type KeyEntry struct {
	KeyID      string // KMS key id / env var 名,审计用
	KeyVersion byte   // 1..255,信封里就标这个
	Key        []byte // 32B AES-256
	Active     bool   // true:用作新加密的 default;false:仅用于解旧数据
}

// tieredCipher 一个 tier 的全套 keys(按 version 索引)
type tieredCipher struct {
	active  *KeyEntry
	byVer   map[byte]*Cipher
	entries map[byte]KeyEntry
}

// KeysConfig InitKeys 接收的配置
type KeysConfig struct {
	PII    []KeyEntry // 至少 1 个;轮转期可放多个
	Secret []KeyEntry
	Normal []KeyEntry
	// 旧版本 cipher(无 tier 概念)兼容入口;v1 格式密文会走这里
	Legacy *KeyEntry
}

var (
	keyMu       sync.RWMutex
	tierPII     *tieredCipher
	tierSecret  *tieredCipher
	tierNormal  *tieredCipher
	legacyEntry *KeyEntry // v1 格式兜底
)

// InitKeys 注入分级密钥(由 main 启动时调一次,轮转时再调覆盖)
//
// ⚠️ 失败**不改任何全局状态** —— 三个 tier + legacy 全部先在局部建好,
// 成功才一次性换上去(轮转中途失败不会留下"一半新一半旧"的 registry)。
func InitKeys(cfg KeysConfig) error {
	pii, err := buildTier(TierPII, cfg.PII)
	if err != nil {
		return err
	}
	secret, err := buildTier(TierSecret, cfg.Secret)
	if err != nil {
		return err
	}
	normal, err := buildTier(TierNormal, cfg.Normal)
	if err != nil {
		return err
	}
	var legacy *KeyEntry
	if cfg.Legacy != nil {
		cp := *cfg.Legacy
		if _, err := NewCipher(cp.Key); err != nil {
			return fmt.Errorf("crypto: legacy key %q invalid: %w", cp.KeyID, err)
		}
		legacy = &cp
	}

	keyMu.Lock()
	tierPII = pii
	tierSecret = secret
	tierNormal = normal
	legacyEntry = legacy
	keyMu.Unlock()
	return nil
}

// buildTier 组装一个 tier 的 keys
//
//   - entries 为空 = 该 tier 未配置(**不是错误**):返回一个空 registry,
//     加密/解密时返 "tier not initialized"
//   - key 长度非法 / active key 数 != 1 → **返 error**(不再 panic)
func buildTier(t Tier, entries []KeyEntry) (*tieredCipher, error) {
	if len(entries) == 0 {
		// tier 没配 = 不允许这个 tier 加密/解密
		// 但仍保留对象,避免 nil deref
		return &tieredCipher{byVer: map[byte]*Cipher{}, entries: map[byte]KeyEntry{}}, nil
	}
	tc := &tieredCipher{
		byVer:   make(map[byte]*Cipher, len(entries)),
		entries: make(map[byte]KeyEntry, len(entries)),
	}
	activeCount := 0
	for _, e := range entries {
		c, err := NewCipher(e.Key)
		if err != nil {
			return nil, fmt.Errorf("crypto: tier %s key v%d invalid: %w", t, e.KeyVersion, err)
		}
		if _, dup := tc.byVer[e.KeyVersion]; dup {
			return nil, fmt.Errorf("crypto: tier %s has duplicate key version %d", t, e.KeyVersion)
		}
		tc.entries[e.KeyVersion] = e
		tc.byVer[e.KeyVersion] = c
		if e.Active {
			activeCount++
			tc.active = &KeyEntry{
				KeyID:      e.KeyID,
				KeyVersion: e.KeyVersion,
				Key:        e.Key,
				Active:     true,
			}
		}
	}
	if activeCount != 1 {
		return nil, fmt.Errorf("crypto: tier %s must have exactly 1 active key, got %d", t, activeCount)
	}
	return tc, nil
}

func lookupTier(t Tier) *tieredCipher {
	keyMu.RLock()
	defer keyMu.RUnlock()
	switch t {
	case TierPII:
		return tierPII
	case TierSecret:
		return tierSecret
	case TierNormal:
		return tierNormal
	}
	return nil
}

// lookupLegacy 取 v1 兼容 key
//
// ⚠️ **必须带读锁** —— InitKeys 会替换 legacyEntry(M100 修)。
// 原来 DecryptFor 是**裸读**这个包级变量,与 InitKeys 的写构成 data race。
func lookupLegacy() *KeyEntry {
	keyMu.RLock()
	defer keyMu.RUnlock()
	return legacyEntry
}

// EncryptFor 用 tier 的 active key 加密并打 envelope
//
// §1.5 #8:成功后落一条密钥使用审计(cryptaudit)。
func EncryptFor(ctx context.Context, t Tier, plaintext []byte) (string, error) {
	return encryptFor(ctx, t, plaintext, true)
}

// encryptFor 加密实现。audit=false 用于**自检探针**(RotationState):
// 探针故意加解密一次固定明文,那是"体检"不是"业务用密钥",
// 不该混进 sys_crypto_audit_log 让人误以为有人在读数据。
func encryptFor(ctx context.Context, t Tier, plaintext []byte, audit bool) (string, error) {
	tc := lookupTier(t)
	if tc == nil || tc.active == nil {
		return "", fmt.Errorf("crypto: tier %q not initialized or no active key", t)
	}
	c := tc.byVer[tc.active.KeyVersion]
	if c == nil {
		return "", fmt.Errorf("crypto: tier %q active key v%d missing cipher", t, tc.active.KeyVersion)
	}
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := readFull(randReader, nonce); err != nil {
		if audit {
			recordKeyUsage(ctx, tc.active.KeyID, int(tc.active.KeyVersion), opEncrypt, len(plaintext), err)
		}
		return "", err
	}
	ct := c.gcm.Seal(nil, nonce, plaintext, nil)
	out := sealWithVersion(tc.active.KeyVersion, nonce, ct)
	if audit {
		recordKeyUsage(ctx, tc.active.KeyID, int(tc.active.KeyVersion), opEncrypt, len(plaintext), nil)
	}
	return out, nil
}

// DecryptFor 解密 envelope 格式密文;按 tier + envelope 标版本找 key
//
// §1.5 #8:成功/失败都落一条密钥使用审计(cryptaudit)。
//
// ⚠️ **v2 解不开时会回退按 v1 再试一次** —— v1 密文的首字节是随机 nonce 的
// 第一字节,1/256 的概率恰好等于 envelope 标记 0x02 → 会被误判成 v2。
// 详见 envelope.go 文件头的"v1 / v2 的首字节歧义"。
func DecryptFor(ctx context.Context, t Tier, b64ct string) ([]byte, error) {
	return decryptFor(ctx, t, b64ct, true)
}

func decryptFor(ctx context.Context, t Tier, b64ct string, audit bool) ([]byte, error) {
	raw, err := decodeRaw(b64ct)
	if err != nil {
		// 密文连格式都读不出来 → 没有任何 key 上下文,不值得记审计
		return nil, err
	}
	envVer, keyVer, rawCT, err := envelopeUnwrap(raw)
	if err != nil {
		return nil, err
	}
	if envVer == EnvelopeFormatVersion {
		pt, keyID, verr := decryptEnvelopeV2(t, keyVer, rawCT)
		if verr == nil {
			if audit {
				recordKeyUsage(ctx, keyID, int(keyVer), opDecrypt, len(pt), nil)
			}
			return pt, nil
		}
		// ⚠️ 可能是 v1 密文被误判成 v2(首字节恰好 0x02)→ 回退 v1 再试
		if pt, legacyID, lerr := decryptLegacyV1(raw); lerr == nil {
			if audit {
				recordKeyUsage(ctx, legacyID, 1, opDecrypt, len(pt), nil)
			}
			return pt, nil
		}
		if audit {
			recordKeyUsage(ctx, keyID, int(keyVer), opDecrypt, len(b64ct), verr)
		}
		return nil, verr
	}
	// v1 裸格式
	pt, legacyID, lerr := decryptLegacyV1(raw)
	if lerr != nil {
		if audit {
			recordKeyUsage(ctx, legacyID, 1, opDecrypt, len(b64ct), lerr)
		}
		return nil, lerr
	}
	if audit {
		recordKeyUsage(ctx, legacyID, 1, opDecrypt, len(pt), nil)
	}
	return pt, nil
}

// decryptEnvelopeV2 按 envelope 标版本解密;返回 (明文, 用的 key id, 错误)
func decryptEnvelopeV2(t Tier, keyVer byte, rawCT []byte) ([]byte, string, error) {
	tc := lookupTier(t)
	if tc == nil {
		return nil, "", fmt.Errorf("crypto: tier %q not initialized", t)
	}
	c := tc.byVer[keyVer]
	if c == nil {
		return nil, "", fmt.Errorf("%w: tier=%s version=%d", ErrUnknownKeyVersion, t, keyVer)
	}
	ns := c.gcm.NonceSize()
	if len(rawCT) < ns {
		return nil, "", ErrMalformedEnvelope
	}
	pt, err := c.gcm.Open(nil, rawCT[:ns], rawCT[ns:], nil)
	return pt, tc.entries[keyVer].KeyID, err
}

// decryptLegacyV1 按 v1 格式(nonce || ct)用 legacy key 解密
func decryptLegacyV1(raw []byte) ([]byte, string, error) {
	legacy := lookupLegacy()
	if legacy == nil {
		return nil, "", fmt.Errorf("%w: no legacy key registered for v1 ciphertext", ErrUnknownKeyVersion)
	}
	c, err := NewCipher(legacy.Key)
	if err != nil {
		return nil, legacy.KeyID, err
	}
	ns := c.gcm.NonceSize()
	if len(raw) < ns {
		return nil, legacy.KeyID, ErrMalformedEnvelope
	}
	pt, err := c.gcm.Open(nil, raw[:ns], raw[ns:], nil)
	return pt, legacy.KeyID, err
}

// recordKeyUsage 落一条密钥使用审计(§1.5 #8 P1)
//
// ctx 为 nil 是允许的(cryptaudit.Record 内部 nil-safe)。
func recordKeyUsage(ctx context.Context, keyID string, keyVersion int, op string, byteCount int, err error) {
	ev := cryptaudit.Event{
		KeyID:      keyID,
		KeyVersion: keyVersion,
		Algorithm:  algorithmAES256GCM,
		Operation:  op,
		ByteCount:  byteCount,
		Success:    err == nil,
	}
	if err != nil {
		ev.ErrorMsg = err.Error()
	}
	cryptaudit.Record(ctx, ev)
}

// EncryptStringFor / DecryptStringFor 字符串便捷方法
func EncryptStringFor(ctx context.Context, t Tier, s string) (string, error) {
	return EncryptFor(ctx, t, []byte(s))
}
func DecryptStringFor(ctx context.Context, t Tier, b64ct string) (string, error) {
	b, err := DecryptFor(ctx, t, b64ct)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ActiveKeyMeta 拿到 tier 当前 active key 的元信息(给 cryptaudit 用)
func ActiveKeyMeta(t Tier) (KeyMeta, error) {
	tc := lookupTier(t)
	if tc == nil || tc.active == nil {
		return KeyMeta{}, fmt.Errorf("crypto: tier %q not initialized", t)
	}
	return KeyMeta{
		KeyID:      tc.active.KeyID,
		KeyVersion: int(tc.active.KeyVersion),
		Algorithm:  algorithmAES256GCM,
	}, nil
}

// HasTier 是否该 tier 已配置
func HasTier(t Tier) bool {
	tc := lookupTier(t)
	return tc != nil && tc.active != nil
}

// KeyRotationState 单个 tier 的**轮转就绪状态**
//
// ⚠️ **诚实的边界说明 —— 这个结构能测什么、不能测什么:**
//
// 能测(纯看 registry 就能判,状态是显式的):
//   - 哪些 tier 配了 key
//   - active 是哪一把、版本号多少
//   - 历史版本堆了几个、active 是不是最高的那个
//   - 加解密往返通不通(见 RoundTripOK)
//
// **不能测:「历史上有没有数据是用已经退役的 key 加密的」。** 那需要扫描
// 业务表的密文列,每张表在哪、怎么批量重写只有业务自己知道。
// 本类型不假装能做这件事 —— 一个天天报"一切正常"却看不见真正问题的检查,
// 比没有检查更坏:它会让人以为轮转没问题。
type KeyRotationState struct {
	Tier Tier
	// Configured 该 tier 装了 key。false = 没配(不是错误,见 jobs 包的说明)
	Configured bool

	// ActiveKeyID 当前 active key 的标识(审计用,不打印 key 本身)
	ActiveKeyID string
	// ActiveVersion 当前 active key 的版本号
	ActiveVersion int

	// RetainedVersions 仍保留在 registry 里的历史版本号(Active=false),升序
	RetainedVersions []byte
	// MaxRetained 保留版本里的最大号
	MaxRetained int

	// RoundTripOK 用 active key 做了一次真实加解密往返,密文能解回原文
	RoundTripOK bool

	// ActiveIsHighest active 版本号是否就是最高版本。
	// false = 加了新 key 但没把 Active 翻过去,新数据还在用老 key 加密
	ActiveIsHighest bool

	// Problem RoundTripOK=false 时的原因(给调用方日志用)
	Problem string
}

// RotationState 探测单个 tier 的轮转就绪状态。
//
// **会真的加解密一次**(用固定探针明文)。这是一次纯内存操作,不落库、不写文件,
// 也不碰任何业务数据 —— 目的只是确认「加密 → 解密」这条路当前走得通。
//
// ⚠️ 探针**不写密钥使用审计**:它是体检,不是业务读数据 ——
// 混进 sys_crypto_audit_log 会让人误以为有人在读数据。
//
// ⚠️ 不要高估这个探针:buildTier 已经保证了「active 版本在 byVer 里一定
// 有 cipher」「key 长度一定合法」,所以**在今天的不变式下它几乎不会失败**。
// 这正是它该有的样子 —— 它是一只在不变式被破坏时立刻响起的哨兵
// (目前能实际抓到的一例:探测的两次调用之间有并发 InitKeys 换掉了整个 registry,
// 密文会用旧 key 加、新 key 解,直接解不开),不是一个天天汇报"一切正常"的假绿灯。
//
// 返回的 error 只表示「这个 tier 压根没初始化」,那是调用方要区分的状态
// (没配 ≠ 配坏了),不是失败。真正的失败信号在返回值的 RoundTripOK 上。
func RotationState(t Tier) (KeyRotationState, error) {
	st := KeyRotationState{Tier: t}

	tc := lookupTier(t)
	if tc == nil || tc.active == nil || len(tc.entries) == 0 {
		return st, fmt.Errorf("crypto: tier %q not initialized", t)
	}

	st.Configured = true
	st.ActiveKeyID = tc.active.KeyID
	st.ActiveVersion = int(tc.active.KeyVersion)
	for v := range tc.entries {
		if v == tc.active.KeyVersion {
			continue
		}
		if int(v) > st.MaxRetained {
			st.MaxRetained = int(v)
		}
		st.RetainedVersions = append(st.RetainedVersions, v)
	}
	sort.Slice(st.RetainedVersions, func(i, j int) bool {
		return st.RetainedVersions[i] < st.RetainedVersions[j]
	})
	st.ActiveIsHighest = st.ActiveVersion >= st.MaxRetained

	// 往返探测:探针明文固定,结果只用于「能不能解回来」,不依赖随机性
	probe := []byte("crypto-rotation-probe")
	ct, err := encryptFor(context.Background(), t, probe, false)
	if err != nil {
		st.Problem = "加密失败:" + err.Error()
		return st, nil
	}
	pt, err := decryptFor(context.Background(), t, ct, false)
	if err != nil {
		st.Problem = "解密失败:" + err.Error()
		return st, nil
	}
	if !bytes.Equal(pt, probe) {
		// 解得开但内容不对 —— 比解不开更危险,静默返回会污染数据
		st.Problem = "解密内容与明文不符"
		return st, nil
	}
	st.RoundTripOK = true
	return st, nil
}

// HasLegacyKey 是否注册了 v1 格式的 legacy key
//
// 用途:没注册 legacy key 时,**任何 v1 格式密文都解不开**
// (DecryptFor 的 v1 分支会返 "no legacy key registered for v1 ciphertext")。
// 新部署从没写过 v1 密文,这是正常的;老部署丢了 legacy key 才是事故。
// 调用方靠这个把两者区分开。
func HasLegacyKey() bool {
	return lookupLegacy() != nil
}
