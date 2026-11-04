// Package cachex 通用缓存层 —— 防穿透 / 击穿 / 雪崩(§1.7 #5 P0)
//
// 设计目标:
//  1. 击穿(hot key expire 后大量并发打 DB)→ singleflight 同 key 只查一次
//  2. 雪崩(大量 key 同时 expire)→ TTL 随机抖动 ±20%,错峰过期
//  3. 穿透(查询永远不存在的 key)→ null sentinel 短 TTL 占位,挡住无效流量
//
// 三防的实现只有一份:core.go 的 getOrLoadJSON(泛型)。本文件里的
// GetOrLoad(any 版)与 typed.go 的 GetOrLoadTypedL2 都调它。
//
// 与 authz.CachedLoader 的关系(2026-10-25 M90 之后):
//   - authz 的 principal 缓存**已经迁进来**,不再自己实现一套 get/set+singleflight
//   - 存储后端由 Backend 接口决定(默认 Redis),任何后端都能复用三防
//
// 用法:
//
//	var userCache = cachex.New(rdb)
//
//	// 业务取值
//	user, err := userCache.GetOrLoad(ctx, "user:"+id, 5*time.Minute,
//	    func(ctx context.Context) (any, error) {
//	        return userRepo.GetByID(ctx, id)
//	    })
package cachex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// DefaultJitterPct 雪崩防御:TTL 随机抖动比例(±20%)
// 60s base → 实际 [48s, 72s]
const DefaultJitterPct = 0.2

// DefaultNullTTL 穿透防御:null sentinel 占位 TTL(短,2 分钟)
// 真实数据 + 短 null,既不污染业务也不放过"暂时没数据"的合理场景
const DefaultNullTTL = 2 * time.Minute

// NullSentinel 标记"key 不存在"的占位值(JSON null)
// 业务侧反序列化拿到 string("null") 或 nil interface{} → 视为 miss(已查过 DB)
// 不直接用空字符串(防止业务合法空值与 null 冲突)
const NullSentinel = "\x00__cachex_null__\x00"

// ErrNotFound 业务侧 loader 明确"查无此数据"——会写 null sentinel
// 与 ErrMiss 区分:ErrMiss 是缓存没命中,ErrNotFound 是 DB 也没命中
var ErrNotFound = errors.New("cachex: not found")

// Safe 通用缓存客户端(线程安全,全局可复用)
type Safe struct {
	backend   Backend // 存储后端(2026-10-25 M90:由 *redis.Client 改为接口)
	flight    singleflight.Group
	jitterPct float64       // 雪崩抖动比例(0~0.5)
	nullTTL   time.Duration // 穿透占位 TTL
	keyPrefix string        // 可选 key 前缀(隔离多业务)
}

// Option 构造选项
type Option func(*Safe)

// WithJitter 自定义雪崩抖动(0~0.5,默认 0.2)
func WithJitter(pct float64) Option {
	if pct < 0 || pct > 0.5 {
		pct = DefaultJitterPct
	}
	return func(s *Safe) { s.jitterPct = pct }
}

// WithNullTTL 自定义穿透占位 TTL(0 表示禁用,默认 2 分钟)
func WithNullTTL(ttl time.Duration) Option {
	return func(s *Safe) { s.nullTTL = ttl }
}

// WithPrefix 加 key 前缀(同 rdb 跑多业务隔离)
func WithPrefix(prefix string) Option {
	return func(s *Safe) { s.keyPrefix = prefix }
}

// New 构造通用缓存客户端(Redis 后端)。
//
// rdb 为 nil → 得到的 Safe 没有后端,所有入口退化成"每次都调 loader"
// (缓存不该改变函数的成功/失败语义)。
func New(rdb *redis.Client, opts ...Option) *Safe {
	var b Backend
	if rdb != nil {
		b = NewRedisBackend(rdb)
	}
	return NewWithBackend(b, opts...)
}

// NewWithBackend 用任意后端构造(2026-10-25 M90)。
//
// 这是三防能脱离 Redis 的关键入口:接内存后端 = 进程内缓存,
// 接别的缓存服务 = 换存储不改业务代码,接测试 fake = 把真路径测起来。
func NewWithBackend(b Backend, opts ...Option) *Safe {
	s := &Safe{
		backend:   b,
		jitterPct: DefaultJitterPct,
		nullTTL:   DefaultNullTTL,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// fkey 加前缀(避免后端 key 冲突)
func (s *Safe) fkey(key string) string {
	if s.keyPrefix == "" {
		return key
	}
	return s.keyPrefix + ":" + key
}

// jitteredTTL 给 base 加 ±jitterPct 抖动
// 例 base=60s jitter=0.2 → [48s, 72s] 均匀分布
func (s *Safe) jitteredTTL(base time.Duration) time.Duration {
	if s.jitterPct <= 0 || base <= 0 {
		return base
	}
	delta := float64(base) * s.jitterPct
	// rand.Float64() [0, 1) → [-delta, +delta]
	offset := (rand.Float64()*2 - 1) * delta
	return base + time.Duration(offset)
}

/* ====================== GetOrLoad 核心(穿透+击穿+雪崩全开) ====================== */

// LoadFunc 业务侧加载函数 —— 返 nil + ErrNotFound 表示"真没数据"(会写 null sentinel)
// 返其他 error 表示"查询出错",不写缓存
type LoadFunc func(ctx context.Context) (any, error)

// GetOrLoad 通用三防入口(any 版)。
//
// 实现见 core.go 的 getOrLoadJSON —— 本方法只做类型适配。
//
// 返回值:
//   - 业务值(可能是 nil,表示"查无此数据")
//   - bool(true=cache 命中;false=走的 loader)
//   - error
//
// 注意:loader 必须返回可 JSON 序列化的值(struct/ptr/slice/map/string/int/float/bool)
func (s *Safe) GetOrLoad(ctx context.Context, key string, ttl time.Duration, loader LoadFunc) (any, bool, error) {
	return getOrLoadJSON(ctx, s, key, ttl, func(ctx context.Context) (any, error) {
		return loader(ctx)
	})
}

/* ====================== 便捷方法(无 loader,纯 KV) ====================== */

// Get 读缓存值(无 loader,miss 返 nil + false)
// 命中时反序列化到 dest(必须是指针);反序列化失败当 miss
func (s *Safe) Get(ctx context.Context, key string, dest any) (bool, error) {
	if s == nil || s.backend == nil {
		return false, nil
	}
	start := time.Now()
	raw, err := s.backend.Get(ctx, s.fkey(key))
	if errors.Is(err, ErrMiss) {
		recordOp("l2", "get", "miss", time.Since(start))
		return false, nil
	}
	if err != nil {
		recordOp("l2", "get", "error", time.Since(start))
		return false, err
	}
	if raw == NullSentinel {
		recordOp("l2", "get", "null", time.Since(start))
		return false, nil // null 占位视同 miss
	}
	if jerr := json.Unmarshal([]byte(raw), dest); jerr != nil {
		recordOp("l2", "get", "error", time.Since(start))
		return false, jerr
	}
	recordOp("l2", "get", "hit", time.Since(start))
	return true, nil
}

// Set 写缓存(带随机抖动,雪崩防御)
func (s *Safe) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if s == nil || s.backend == nil {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cachex: marshal: %w", err)
	}
	start := time.Now()
	if rerr := s.backend.Set(ctx, s.fkey(key), string(b), s.jitteredTTL(ttl)); rerr != nil {
		recordOp("l2", "set", "error", time.Since(start))
		return rerr
	}
	recordOp("l2", "set", "ok", time.Since(start))
	return nil
}

// SetNX 仅当 key 不存在时写(分布式锁场景,ttl 不抖动)
func (s *Safe) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	if s == nil || s.backend == nil {
		return false, nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("cachex: marshal: %w", err)
	}
	return s.backend.SetNX(ctx, s.fkey(key), string(b), ttl)
}

// Del 失效缓存
func (s *Safe) Del(ctx context.Context, keys ...string) error {
	if s == nil || s.backend == nil || len(keys) == 0 {
		return nil
	}
	fkeys := make([]string, len(keys))
	for i, k := range keys {
		fkeys[i] = s.fkey(k)
	}
	start := time.Now()
	if rerr := s.backend.Del(ctx, fkeys...); rerr != nil {
		recordOp("l2", "del", "error", time.Since(start))
		return rerr
	}
	recordOp("l2", "del", "ok", time.Since(start))
	return nil
}

// Expire 重设 TTL(雪崩防御:把即将过期的 key 续期时,顺便抖一下)
func (s *Safe) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if s == nil || s.backend == nil {
		return nil
	}
	return s.backend.Expire(ctx, s.fkey(key), s.jitteredTTL(ttl))
}

// Stats 缓存统计(用于打点/调试)
type Stats struct {
	JitterPct float64       `json:"jitter_pct"`
	NullTTL   time.Duration `json:"null_ttl"`
	KeyPrefix string        `json:"key_prefix"`
}

// Stats 取当前配置
func (s *Safe) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	return Stats{
		JitterPct: s.jitterPct,
		NullTTL:   s.nullTTL,
		KeyPrefix: s.keyPrefix,
	}
}
