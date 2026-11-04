// M117-4 —— 会话活动心跳(last_active_at)
//
// # 这一列此前到底缺什么
//
// `sys_login_log.last_active_at` 的文档定义是"最后一次**确认这条会话还活着**的时刻",
// 但它**只在续期时更新** —— 而续期每 8 小时才发生一次。
// ⇒ 会话建立后的整整 8 小时里,这一列是**空的**:
// 审计中心「最后活动」那列对当天所有的登录都是空白 ——
// 看上去像"登录之后就没动过",实际上人一直在用。
//
// 本文件锁的是"真的写进去了" + "该拦的拦住了",不是"代码里有没有这个函数"。
//
// # 为什么节流要测两层
//
// 只做进程内闸 ⇒ 多副本下每个副本各写一次;只做 DB 条件 ⇒ 每个请求一次 DB 往返。
// 两层都要,两层都要有测试。
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
)

// seedSessionRow 造一行"登录成功"的登记簿
//
// lastActiveAt 传 nil = 刚登录还没记过活动(= 这一列此前的常态)
func seedSessionRow(t *testing.T, db *gorm.DB, userID uint64, sid string, lastActiveAt *model.Time) {
	t.Helper()
	row := model.SysLoginLog{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		UserID:       userID,
		Username:     "u",
		SessionID:    sid,
		Status:       1,
		LoginAt:      model.Now(),
		LastActiveAt: lastActiveAt,
	}
	require.NoError(t, db.Create(&row).Error)
}

// lastActiveOf 取某一行当前的 last_active_at
func lastActiveOf(t *testing.T, db *gorm.DB, sid string) *model.Time {
	t.Helper()
	var row model.SysLoginLog
	require.NoError(t, db.Where("session_id = ?", sid).First(&row).Error)
	return row.LastActiveAt
}

// timePtr 取 model.Time 的指针
func timePtr(t time.Time) *model.Time { return &model.Time{Time: t} }

// ===== 1. 闸门(进程内)=====

// TestTouchGate_ZeroValueUsable 闸门零值必须可用
//
// authService 里它是**值字段**,没在构造函数里初始化 ——
// 如果零值不可用,漏初始化就会变成"每个请求一次 UPDATE"(热路径上打 DB)。
func TestTouchGate_ZeroValueUsable(t *testing.T) {
	var g touchGate
	require.True(t, g.allow("sid-a", time.Now()),
		"零值闸门必须放行第一次 —— 否则漏初始化就等于热路径上一直打 DB")
}

// TestTouchGate_ThrottlesThenAllowsAgain 间隔内拦、间隔后放
func TestTouchGate_ThrottlesThenAllowsAgain(t *testing.T) {
	var g touchGate
	now := time.Now()
	require.True(t, g.allow("sid-a", now))
	require.False(t, g.allow("sid-a", now.Add(time.Minute)),
		"间隔内的第二次必须被拦住 —— 否则每个请求一次 UPDATE")
	require.True(t, g.allow("sid-a", now.Add(10*time.Minute)),
		"过了间隔必须放行 —— 否则这一列永远停在第一次")
}

// TestTouchGate_PerSession 不同会话互不影响
func TestTouchGate_PerSession(t *testing.T) {
	var g touchGate
	now := time.Now()
	require.True(t, g.allow("sid-a", now))
	require.True(t, g.allow("sid-b", now),
		"会话 A 的节流不该连累会话 B —— 否则一个人狂点页面能把所有人挡在门外")
}

// ===== 2. 写库(服务层)=====

// updateCounter 数一数某张表被 UPDATE 了几次
type updateCounter struct {
	mu    sync.Mutex
	table string
	hits  int
}

// count 当前计数
func (c *updateCounter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

// watchUpdates 在这个 gorm 实例上挂一个 UPDATE 计数器
//
// ⚠️ 每个用例的 newAuthTestDB 都是**独立的 gorm.Open** ⇒ 回调注册表是
// 独立的一份,不会污染同包的其它用例。
func watchUpdates(t *testing.T, db *gorm.DB, table string) *updateCounter {
	t.Helper()
	c := &updateCounter{table: table}
	require.NoError(t, db.Callback().Update().Before("gorm:update").
		Register("test:count_updates", func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Table == c.table {
				c.mu.Lock()
				c.hits++
				c.mu.Unlock()
			}
		}))
	return c
}

// TestTouchSession_SkipsDBWithinInterval 热路径上不碰 DB
//
// 这一条锁的是**进程内闸存在的理由**:节流不只是"少写一次",
// 而是"连查询都不发" —— 否则每个请求一次 DB 往返
// (SQLite 下写锁串行,MySQL 下多一次 RTT)。
//
// ⚠️ **只断言 RowsAffected 是测不出这一层的**:
// DB 条件更新挡住的是"写",不是"查" —— 两层闸在 RowsAffected 上表现一模一样。
// 探针实测:把进程内闸整个短路掉,下面这条之外的用例**全部照样绿**。
// 所以这里必须数 UPDATE 语句条数。
func TestTouchSession_SkipsDBWithinInterval(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	seedSessionRow(t, db, 7, "sid-a", nil)
	ctx := context.Background()

	counter := watchUpdates(t, db, "sys_login_log")

	_, err := svc.touchSession(ctx, 7, "sid-a")
	require.NoError(t, err)
	require.Equal(t, 1, counter.count(),
		"第一次活动应该打一次 DB(顺带自证:计数器认得这张表)")

	// 间隔内的第二次:进程内闸就该把它拦在 DB 之前
	_, err = svc.touchSession(ctx, 7, "sid-a")
	require.NoError(t, err)
	require.Equal(t, 1, counter.count(),
		"间隔内的第二次活动还打了 DB —— 进程内闸没起作用,等于每个请求一次往返")
}

// TestTouchSession_FirstTouchFillsEmptyColumn 第一次活动必须写进去
//
// 这条是本改动的**主断言**:改之前,这一列在会话建立后 8 小时里恒空。
func TestTouchSession_FirstTouchFillsEmptyColumn(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	seedSessionRow(t, db, 7, "sid-a", nil)
	require.Nil(t, lastActiveOf(t, db, "sid-a"), "前置条件:这一列原本是空的")

	written, err := svc.touchSession(context.Background(), 7, "sid-a")
	require.NoError(t, err)
	require.True(t, written, "第一次活动必须写进去")

	got := lastActiveOf(t, db, "sid-a")
	require.NotNil(t, got, "last_active_at 还是空的 —— 审计中心「最后活动」仍然是空白")
}

// TestTouchSession_ThrottledWithinInterval 间隔内的重复活动不再写库
func TestTouchSession_ThrottledWithinInterval(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	seedSessionRow(t, db, 7, "sid-a", nil)
	ctx := context.Background()

	_, err := svc.touchSession(ctx, 7, "sid-a")
	require.NoError(t, err)
	first := *lastActiveOf(t, db, "sid-a")
	require.NotNil(t, first)

	written, err := svc.touchSession(ctx, 7, "sid-a")
	require.NoError(t, err)
	require.False(t, written, "间隔内的第二次活动不该再写库")

	second := lastActiveOf(t, db, "sid-a")
	require.NotNil(t, second)
	require.True(t, first.Time.Equal(second.Time), "值不该被刷新 —— 否则等于每个请求一次 UPDATE")
}

// TestTouchSession_WritesAgainAfterInterval 过了间隔要能再写
//
// 同时拨快两处时间:进程内闸的记忆 + DB 里那一行的 last_active_at。
func TestTouchSession_WritesAgainAfterInterval(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	old := time.Now().Add(-time.Hour)
	seedSessionRow(t, db, 7, "sid-a", timePtr(old))

	// 进程内闸:塞一个"1 小时前就该放行"的记忆
	cache, err := lru.New[string, time.Time](touchGateSize)
	require.NoError(t, err)
	svc.touch.cache = cache
	cache.Add("sid-a", old)

	written, err := svc.touchSession(context.Background(), 7, "sid-a")
	require.NoError(t, err)
	require.True(t, written, "过了节流间隔必须能再写 —— 否则这一列永远停在第一次")

	got := lastActiveOf(t, db, "sid-a")
	require.NotNil(t, got)
	require.True(t, old.Before(got.Time), "时间戳应该被推进")
}

// TestTouchSession_DBConditionBlocksFreshRow —— 第二层(DB 那道闸)
//
// 模拟"另一个副本":进程内闸是空的(它不知道 A 副本刚写过),
// 但 DB 里这一行刚被写过 ⇒ DB 那道闸必须把它挡住。
// 没有这一层,多副本部署时每个副本都会写一次。
func TestTouchSession_DBConditionBlocksFreshRow(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	seedSessionRow(t, db, 7, "sid-a", nil)
	ctx := context.Background()

	_, err := svc.touchSession(ctx, 7, "sid-a")
	require.NoError(t, err)
	first := *lastActiveOf(t, db, "sid-a")

	// 另一个副本:闸门是新的(空的)
	other := &authService{db: db}
	written, err := other.touchSession(ctx, 7, "sid-a")
	require.NoError(t, err)
	require.False(t, written,
		"DB 那道闸没挡住 —— 多副本下每个副本都会写一次,写放大 N 倍")

	second := lastActiveOf(t, db, "sid-a")
	require.NotNil(t, second)
	require.True(t, first.Time.Equal(second.Time))
}

// TestTouchSession_OnlyTouchesOwnSession 不能碰别的会话
func TestTouchSession_OnlyTouchesOwnSession(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	seedSessionRow(t, db, 7, "sid-a", nil)
	seedSessionRow(t, db, 7, "sid-b", nil)

	_, err := svc.touchSession(context.Background(), 7, "sid-a")
	require.NoError(t, err)

	require.NotNil(t, lastActiveOf(t, db, "sid-a"))
	require.Nil(t, lastActiveOf(t, db, "sid-b"),
		"记 A 的活动不该动到 B 那一行 —— 那会让「活跃设备」列表显示错的活跃时间")
}

// TestTouchSession_NoopCases 没有可定位的行时什么都不做
func TestTouchSession_NoopCases(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	seedSessionRow(t, db, 7, "sid-a", nil)
	ctx := context.Background()

	// sid 为空 = 本轮上线前签发的老 token(没有 sid)⇒ 没有可定位的行
	written, err := svc.touchSession(ctx, 7, "")
	require.NoError(t, err)
	require.False(t, written)

	// sid 不存在
	written, err = svc.touchSession(ctx, 7, "no-such-sid")
	require.NoError(t, err)
	require.False(t, written)

	// userID 为 0
	written, err = svc.touchSession(ctx, 0, "sid-a")
	require.NoError(t, err)
	require.False(t, written)

	require.Nil(t, lastActiveOf(t, db, "sid-a"), "上面三种情况都不该写库")
}

// TestTouchLoginSession_NilCaller nil 入参不 panic
func TestTouchLoginSession_NilCaller(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	require.NoError(t, svc.TouchLoginSession(context.Background(), nil))
}

// TestTouchLoginSession_PublicEntry 走公开入口(中间件调的那条)也要写
func TestTouchLoginSession_PublicEntry(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}
	seedSessionRow(t, db, 7, "sid-a", nil)

	require.NoError(t, svc.TouchLoginSession(context.Background(), &Caller{
		UserID: 7, SessionID: "sid-a",
	}))
	require.NotNil(t, lastActiveOf(t, db, "sid-a"))
}

// ===== 3. 登录本身也是一次活动 =====

// TestRecordLoginLog_WritesLastActiveAtOnSuccess 成功登录就该有活动时刻
//
// 登录**本身就是一次活动**。不写的话,"登录之后一直没再操作"的会话,
// 「最后活动」显示空白 —— 看上去像"登录之后就没动过",与事实不符。
func TestRecordLoginLog_WritesLastActiveAtOnSuccess(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}

	svc.recordLoginLog(context.Background(), 7, "u", LoginRequest{}, 1, "登录成功", "sid-a")

	var row model.SysLoginLog
	require.NoError(t, db.Where("session_id = ?", "sid-a").First(&row).Error)
	require.NotNil(t, row.LastActiveAt,
		"成功登录后 last_active_at 就该有值 —— 登录本身就是一次活动")
	require.True(t, row.LoginAt.Time.Equal(row.LastActiveAt.Time),
		"登录那一刻的活动时刻就是登录时刻")
}

// TestRecordLoginLog_FailedLoginHasNoLastActiveAt 失败登录不写活动时刻
//
// 失败时压根没有会话,给不存在的会话记活动是假的。
func TestRecordLoginLog_FailedLoginHasNoLastActiveAt(t *testing.T) {
	db := newAuthTestDB(t)
	svc := &authService{db: db}

	svc.recordLoginLog(context.Background(), 7, "u", LoginRequest{}, 0, "账号或密码错误", "")

	var row model.SysLoginLog
	require.NoError(t, db.Where("status = ?", 0).First(&row).Error)
	require.Nil(t, row.LastActiveAt,
		"登录失败时压根没有会话,不该有活动时刻")
}
