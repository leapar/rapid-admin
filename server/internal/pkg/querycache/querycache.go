// Package querycache —— GORM 查询结果缓存(借鉴 GoFrame gdb 的 Model.Cache 机制)。
//
// # 解决什么
//
// cachex 是"按点缓存"(GetOrLoad 显式 loader),列表 / 树 / 报表类 SELECT
// 每次都打库。本包把"这条查询的结果缓存下来、写到相关表就自动失效"收敛成
// 显式两行:
//
//	querycache.Install(db)                              // 启动装一次
//	rows, err := querycache.On(db, 5*time.Minute, "sys_resource").Find(ctx, &rows)
//
// # 与 gdb Model.Cache 的差别
//
// gdb 的失效是"写 Model 自己挂 Cache{Duration:-1, Name}"显式清名下缓存;
// 本包是**表标签 + GORM 写回调自动失效**(每张 tag 表维护一个 key 索引,
// 写回调反查索引删 key)。失效语义更强,但只在 Install 过的 db 上生效。
//
// # 纪律
//
//   - **opt-in**:没有 Install / 没有 On 的查询行为完全不变;
//   - **事务内不缓存**(gorm.TxCommitter 探测)—— 照抄 gdb 的 tx 短路,
//     "缓存配置不改变业务语义";
//   - **只缓存 Find 形状的 SELECT**(整个结果集按 JSON 存);Count / 聚合
//     / ScanList 关联预加载不在此列,别硬套;
//   - **TTL 兜底**:索引反查是尽力而为(并发写索引有丢更新窗口),失效
//     万一漏了也有 TTL 顶着 —— 所以 TTL 要按"脏几分钟可接受"来定;
//   - **缓存配置不改变语义**:cachex 未启用 / Safe 无后端(ModeL1、无
//     Redis)/ 建键失败,一律退化为直查,绝不把查询变成失败;
//   - **裸 SQL 写路径打不着 GORM 回调**(与 auditx 同款坑):db.Exec 的
//     UPDATE 不会触发失效 —— 要么显式调 Invalidate,要么把 TTL 定短。
//
// # 失效的边界(如实说)
//
//   - 多实例部署:写回调只清**本进程**写的那份 Redis L2(共享,各实例都
//     能看到失效);若只配了进程内 memory 后端,其他实例的缓存要等 TTL;
//   - 跨表查询:tag 全部传进来(On(db, ttl, "a", "b")),写到任何一张
//     tag 表都会失效这条查询。
package querycache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"rapid-admin/internal/pkg/cachex"
)

const (
	keyPrefix = "qc:"     // 结果缓存 key 前缀
	idxPrefix = "qcidx:"  // 表标签 → key 索引前缀
	maxIdxLen = 512       // 单表索引最多登记的 key 数(超出丢最旧,TTL 兜底)
)

// safe 包级缓存客户端,由 main 注入(Use)。
// nil / 无后端 = 全部退化直查。
var (
	mu   sync.RWMutex
	safe *cachex.Safe
)

// Use 注入缓存客户端(启动装一次;cachePool.Safe())。
// Safe 无后端时(ModeL1 / 无 Redis)所有查询自动退化直查。
func Use(s *cachex.Safe) {
	mu.Lock()
	defer mu.Unlock()
	safe = s
}

// currentSafe 取当前客户端(快照,避免持锁做 IO)。
func currentSafe() *cachex.Safe {
	mu.RLock()
	defer mu.RUnlock()
	return safe
}

// On 开启一次可缓存的查询。tables 是**失效标签**:写到任何一张 tag 表
// (经 Install 的 GORM 回调)都会让这条查询的缓存失效。不传表 = 不缓存
// (没有失效手段的缓存是裸奔,不给)。
//
// db 必须是**还没执行**的查询链(Find 由本包收尾):
//
//	q := querycache.On(s.db.WithContext(ctx).Where("is_active = ?", true), 5*time.Minute, "sys_resource")
//	q.Find(ctx, &rows)
func On(db *gorm.DB, ttl time.Duration, tables ...string) *Scope {
	return &Scope{db: db, ttl: ttl, tables: tables}
}

// Scope 一次可缓存的查询。
type Scope struct {
	db     *gorm.DB
	ttl    time.Duration
	tables []string
	hit    bool // 最近一次 Find 是否命中缓存(观测用)
}

// Hit 最近一次 Find 是否命中缓存(未缓存 / 直查 / 失败均返回 false)。
func (s *Scope) Hit() bool { return s.hit }

// Find 执行 SELECT,结果写进 dest;缓存命中时 dest 从缓存解码,不打库。
func (s *Scope) Find(ctx context.Context, dest any) error {
	s.hit = false
	sc := currentSafe()
	if !s.cacheable(sc) {
		return s.exec(ctx, dest)
	}
	key, err := s.fingerprint(dest)
	if err != nil {
		// 建键失败不该把查询变成失败 —— 直查。
		return s.exec(ctx, dest)
	}
	if ok, _ := sc.Get(ctx, key, dest); ok {
		s.hit = true
		return nil
	}
	if err := s.exec(ctx, dest); err != nil {
		return err
	}
	// 缓存写失败不影响查询结果(与 cachex 的 fail-open 一致)。
	_ = sc.Set(ctx, key, dest, s.jitteredTTL())
	s.registerIndex(ctx, sc, key)
	return nil
}

// cacheable 这条查询是否走缓存:有客户端、有失效标签、不在事务里。
func (s *Scope) cacheable(sc *cachex.Safe) bool {
	if sc == nil || len(s.tables) == 0 {
		return false
	}
	// 事务内不缓存(gdb 同款短路):事务还没提交的东西不能被别人看见。
	if _, tx := s.db.Statement.ConnPool.(gorm.TxCommitter); tx {
		return false
	}
	return true
}

// exec 直查(缓存不可用 / 未命中后的真实执行)。
func (s *Scope) exec(ctx context.Context, dest any) error {
	return s.db.WithContext(ctx).Find(dest).Error
}

// fingerprint 用干跑建键:同一链路 DryRun 跑一遍 Find,把 SQL 文本与参数
// 指纹化。任何条件变化都会得到不同的 key,不存在"改了参数拿到旧结果"。
func (s *Scope) fingerprint(dest any) (string, error) {
	// DryRun 会走回调链但不执行 SQL;Logger 覆盖成 Discard,
	// 否则 gorm logger 会把"建键"也当成一条 SELECT 打出来,观测全是噪音。
	dry := s.db.Session(&gorm.Session{DryRun: true, Logger: logger.Discard})
	if err := dry.Find(dest).Error; err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.WriteString(h, dry.Statement.SQL.String()); err != nil {
		return "", err
	}
	for _, v := range dry.Statement.Vars {
		b, err := json.Marshal(v)
		if err != nil {
			b = []byte(fmt.Sprintf("%v", v))
		}
		if _, err := h.Write(b); err != nil {
			return "", err
		}
	}
	return keyPrefix + s.tables[0] + ":" + hex.EncodeToString(h.Sum(nil)), nil
}

// jitteredTTL TTL 带 ±10% 抖动(同 key 同时过期 = 雪崩,与 cachex 同纪律)。
func (s *Scope) jitteredTTL() time.Duration {
	j := 1 + (rand.Float64()-0.5)*0.2 //nolint:gosec // 非安全场景,只做抖动
	return time.Duration(float64(s.ttl) * j)
}

// registerIndex 把 key 登记进每张 tag 表的索引,写回调按索引反查删除。
//
// ⚠️ 读改写不是原子的(并发登记有丢更新窗口)—— 失效尽力而为,TTL 兜底,
// 这个取舍写在包注释里。
func (s *Scope) registerIndex(ctx context.Context, sc *cachex.Safe, key string) {
	for _, t := range s.tables {
		idxKey := idxPrefix + t
		var keys []string
		if ok, _ := sc.Get(ctx, idxKey, &keys); ok {
			keys = append(keys, key)
			if len(keys) > maxIdxLen {
				keys = keys[len(keys)-maxIdxLen:]
			}
		} else {
			keys = []string{key}
		}
		// 索引比值活得久一点(50%),保证能覆盖到即将过期的 key
		_ = sc.Set(ctx, idxKey, keys, s.ttl+s.ttl/2)
	}
}

// Install 注册失效回调(Create / Update / Delete 之后按 Statement.Table
// 反查索引删 key)。只应对**服务运行时**的 db 调,与 auditx.Install 同款纪律。
func Install(db *gorm.DB) error {
	invalidate := func(tx *gorm.DB) {
		table := tx.Statement.Table
		if table == "" {
			return
		}
		if sc := currentSafe(); sc != nil {
			invalidateTable(tx.Statement.Context, sc, table)
		}
	}
	if err := db.Callback().Create().After("gorm:create").Register("querycache:invalidate", invalidate); err != nil {
		return err
	}
	if err := db.Callback().Update().After("gorm:update").Register("querycache:invalidate", invalidate); err != nil {
		return err
	}
	return db.Callback().Delete().After("gorm:delete").Register("querycache:invalidate", invalidate)
}

// Invalidate 显式失效(给裸 SQL 写路径用 —— 回调打不着的地方)。
func Invalidate(ctx context.Context, tables ...string) {
	sc := currentSafe()
	if sc == nil {
		return
	}
	for _, t := range tables {
		invalidateTable(ctx, sc, t)
	}
}

func invalidateTable(ctx context.Context, sc *cachex.Safe, table string) {
	idxKey := idxPrefix + table
	var keys []string
	if ok, _ := sc.Get(ctx, idxKey, &keys); !ok {
		return
	}
	_ = sc.Del(ctx, keys...)
	_ = sc.Del(ctx, idxKey)
}
