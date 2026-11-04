// Package idempotentx 幂等键框架级处理 — §1.3 #17 P1
//
// 设计目标:
//   - 写操作(POST/PUT/PATCH/DELETE)重复请求不重复执行业务逻辑
//   - 用客户端在 Idempotency-Key 头里带的 key(前端 axios 拦截器已自动注入)
//   - 同 key 在 TTL 内重放 → 直接返首次结果;TTL 外 → 重新执行
//
// # 实际的数据结构:两个 key,不是一个状态机
//
// 旧注释写的是"notexist ─SETNX→ running ─业务完成→ done(json)"这样一个
// 单 key 状态机 —— **实现从来不是这样**,而且照着那个状态机去改代码一定会错:
// "running" 这个状态**只活在锁 key 的存在性里**,结果 key 里永远只有
// done / failed。2026-10-26 M97 把这个假状态机从注释里删掉,改成描述真正
// 实现的两把 key:
//
//	idem:res:<key>   结果(TTL 24h)—— 业务跑完才有,内容是 {state,http_status,body}
//	idem:lock:<key>  锁  (TTL 30s)—— 有人在跑,跑完就删
//
// 为什么要分两个 key:锁要短(30s,免得业务卡住把 key 锁死),结果要长
// (24h,免得用户重试时业务又跑一遍)。一个 key 做不到"两套 TTL"。
//
// # 键空间必须是单射
//
// 旧实现是 `idem:<k>`(结果)与 `idem:<k>:lock`(锁)—— **两个命名空间重叠**:
// 客户端 key "foo:lock" 的结果 key 正好是客户端 key "foo" 的锁 key。于是:
//   - "foo:lock" 的 Save 会 SET "idem:foo:lock"(覆盖 "foo" 的锁)
//   - "foo" 的 Save 会 DEL "idem:foo:lock"(删掉 "foo:lock" 的结果)
//
// 两个互不相干的请求能互相把对方的幂等状态搞坏,而且**没有任何报错**。
// 今天没炸过(前端 key 是 UUID,不含冒号),但这是**契约级的错**:键空间不是
// 单射,下一个用带冒号的 key 的调用方(网关重写、客户端自定义 key)就会踩。
// M97 改成 `idem:res:` / `idem:lock:` 两个前缀 —— 前缀不同,任何客户端 key
// 都撞不上另一个 key 的锁。
//
// 注意:
//   - 业务 handler 失败也缓存(返业务错误码)——避免 5 次重试每次都报错
//   - panic 不缓存,且**锁会被释放**(让客户端重试能再跑一次)
//   - 超时(504)按业务失败缓存 —— 写操作宁可少跑一次,不能重跑
//   - **"业务压根没跑成"的响应不缓存**(401/403/429 等)—— 见 middleware/idempotency.go
//   - 单接口不强制:仅 method ∈ {POST,PUT,PATCH,DELETE} 才生效
//
// 用法(在 main.go Init 一次,中间件自动用):
//
//	idempotentx.Init(rdb, 30*time.Second, 24*time.Hour)
//
//	// router
//	h.Use(middleware.Idempotency()) // 默认 5min TTL
package idempotentx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// 默认锁 TTL(运行中状态 30s)与结果 TTL(24h)
const (
	DefaultLockTTL   = 30 * time.Second
	DefaultResultTTL = 24 * time.Hour
)

// Redis key 前缀 —— 结果与锁分属两个命名空间
//
// ⚠️ **不能用"一个前缀 + 拼接后缀"**(旧的 `idem:<k>` / `idem:<k>:lock`):
// 那样两个命名空间会重叠,客户端 key "foo:lock" 的结果 key 正好是客户端 key
// "foo" 的锁 key。详见包注释的"键空间必须是单射"。
const (
	resultKeyPrefix = "idem:res:"
	lockKeyPrefix   = "idem:lock:"
)

// State 结果状态
//
// ⚠️ 这里**只有"已出结果"的两种状态**。旧版本还有一个 StateRunning("running"),
// 但实现从来不会写它 —— "正在跑"这个状态活在**锁 key 的存在性**里
// (idem:lock:<k> 存在 = 有人在跑),结果 key 里永远只有 done / failed。
//
// 留着那个常量是有害的:它让人以为"可以靠读结果的 State 区分在不在跑",
// 于是会写出"结果里 State==running 就当未命中、继续往下走"这种错代码 ——
// 而结果 key 里压根不会出现 running,那段判断永远不成立,于是"在跑"被当成
// "已跑完"重放出去。M97 删掉了它。
type State string

const (
	StateDone   State = "done"   // 业务已完成(成功或失败结果都在 ResultJSON)
	StateFailed State = "failed" // 业务失败(结果里是 errcode + message)
)

// Result 缓存的内容(JSON 序列化)
type Result struct {
	State      State     `json:"state"`
	HTTPStatus int       `json:"http_status"` // 原始响应状态(200/201/...)
	Body       []byte    `json:"body"`        // 原始响应 body(包含 code/message/data/request_id)
	WrittenAt  time.Time `json:"written_at"`
}

var (
	manager *Manager
)

// Manager 幂等管理
type Manager struct {
	rdb       *redis.Client
	lockTTL   time.Duration
	resultTTL time.Duration
}

// New 构造一个幂等管理器(不碰全局)
//
// main.go 用 Init 装全局;测试用 New 造独立实例 —— 免得测试之间共用一把锁。
func New(rdb *redis.Client, lockTTL, resultTTL time.Duration) *Manager {
	if lockTTL <= 0 {
		lockTTL = DefaultLockTTL
	}
	if resultTTL <= 0 {
		resultTTL = DefaultResultTTL
	}
	return &Manager{rdb: rdb, lockTTL: lockTTL, resultTTL: resultTTL}
}

// Init 全局初始化(只调一次)
func Init(rdb *redis.Client, lockTTL, resultTTL time.Duration) {
	if rdb == nil {
		logger.L().Warn("idempotentx: rdb is nil, manager disabled")
	}
	manager = New(rdb, lockTTL, resultTTL)
}

// M 取全局 manager(未 Init 返 nil,中间件 no-op)
func M() *Manager { return manager }

// resultKey 结果 key
func resultKey(idemKey string) string { return resultKeyPrefix + idemKey }

// lockKey 锁 key
func lockKey(idemKey string) string { return lockKeyPrefix + idemKey }

// AcquireOrGet 尝试拿到锁;拿不到就看现在是什么状态
//
// 返回值:
//   - acquired=true:本次拿到锁,可执行业务(首次请求)
//   - acquired=false,err=nil:已有结果,result 为缓存内容(命中)
//   - acquired=false,err=ErrInFlight:有其他请求正在跑(返 409 给客户端)
//   - acquired=false,err=其他:Redis 出错
func (m *Manager) AcquireOrGet(ctx context.Context, idemKey string) (acquired bool, result *Result, err error) {
	if m == nil || m.rdb == nil {
		return true, nil, nil // 未 Init / Redis 未配 → 放行
	}
	// ⚠️ **空 key 必须在这里挡住**,不能只靠 Save/Release 挡。
	// 空 key 会让所有人共用同一把锁(`idem:lock:`),而 Save/Release 因为
	// `idemKey == ""` 直接返回 —— 那把锁**永远不会被释放**,之后每个空 key 的
	// 请求都拿到 ErrInFlight(409) 直到 TTL 到期。
	// Save/Release 的空 key 判断本来就在,只是这里漏了,两边不对称。
	if idemKey == "" {
		return true, nil, nil
	}
	// 1. 先看结果(命中:返)
	raw, rerr := m.rdb.Get(ctx, resultKey(idemKey)).Result()
	if rerr == nil {
		var r Result
		if jerr := json.Unmarshal([]byte(raw), &r); jerr == nil {
			return false, &r, nil
		}
		logger.L().Warn("idempotentx: unmarshal result failed, treat as miss",
			zap.String("key", idemKey))
	} else if !errors.Is(rerr, redis.Nil) {
		// Redis 错:降级放行(避免 Redis 挂了所有写操作都被挡)
		logger.L().Warn("idempotentx: get failed, fallthrough",
			zap.String("key", idemKey), zap.String("err", rerr.Error()))
		return true, nil, nil
	}

	// 2. 没结果 → 抢锁
	ok, serr := m.rdb.SetNX(ctx, lockKey(idemKey), "1", m.lockTTL).Result()
	if serr != nil {
		// Redis 错:放行
		logger.L().Warn("idempotentx: setnx lock failed, fallthrough",
			zap.String("key", idemKey), zap.String("err", serr.Error()))
		return true, nil, nil
	}
	if ok {
		// 拿到锁
		return true, nil, nil
	}
	// 锁被占:表示有请求正在跑
	return false, nil, ErrInFlight
}

// Save 业务完成后写结果(由 middleware 在 c.Next 后调用)
//   - httpStatus: 业务 handler 实际返回的 HTTP 状态码
//   - body:       业务 handler 实际写入的 body 字节
//   - 失败结果(4xx/5xx)也写,客户端重试不会重复跑业务
//
// ⚠️ **只写"业务真的跑过"的结果** —— 判据在 middleware 那边
// (middleware/idempotency.go 的 cacheableResult)。"业务没跑成"的响应
// (401/403/429)不能写进来:写进来等于告诉客户端"这个 key 已经用掉了",
// 客户端拿同一个 key 重试(401 刷新 token 后重发)会被重放成同一个 401 ——
// 写操作永远不执行,而且用户被踢回登录页。
func (m *Manager) Save(ctx context.Context, idemKey string, httpStatus int, body []byte) {
	if m == nil || m.rdb == nil || idemKey == "" {
		return
	}
	r := Result{
		State:      StateDone,
		HTTPStatus: httpStatus,
		Body:       body,
		WrittenAt:  time.Now(),
	}
	if httpStatus >= 400 {
		r.State = StateFailed
	}
	b, err := json.Marshal(r)
	if err != nil {
		logger.L().Warn("idempotentx: marshal result failed",
			zap.String("key", idemKey), zap.String("err", err.Error()))
		return
	}
	// 写结果 + 删锁(锁删了不影响结果 TTL)
	pipe := m.rdb.TxPipeline()
	pipe.Set(ctx, resultKey(idemKey), b, m.resultTTL)
	pipe.Del(ctx, lockKey(idemKey))
	if _, err := pipe.Exec(ctx); err != nil {
		logger.L().Warn("idempotentx: save result failed",
			zap.String("key", idemKey), zap.String("err", err.Error()))
	}
}

// Release 业务异常(panic / ctx cancel)时主动释放锁,允许客户端重试
func (m *Manager) Release(ctx context.Context, idemKey string) {
	if m == nil || m.rdb == nil || idemKey == "" {
		return
	}
	if err := m.rdb.Del(ctx, lockKey(idemKey)).Err(); err != nil {
		logger.L().Warn("idempotentx: release lock failed",
			zap.String("key", idemKey), zap.String("err", err.Error()))
	}
}

// ErrInFlight 业务正在执行中
var ErrInFlight = errors.New("idempotentx: request in flight")

// String 调试用
func (m *Manager) String() string {
	if m == nil {
		return "idempotentx: not initialized"
	}
	return fmt.Sprintf("idempotentx: lock=%s result=%s", m.lockTTL, m.resultTTL)
}
