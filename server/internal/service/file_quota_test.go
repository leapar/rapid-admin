// M118 —— 文件配额记账的 SQL 参数个数回归网
//
// ⚠️ **这一组是为一个线上 bug 立的**:用户上传文件报
//
//	{"code":50000,"message":"预扣配额失败","data":"sql: expected 4 arguments, got 5"}
//
// # 根因
//
// `holdQuota` 的 Where 传了 3 个实参(`userID, size, size`),
// 而 SQL 里只有 2 个 `?`(`id = ?` + `quotaWhere` 自带的一个)。
//
// GORM 的 `clause.Expr.Build` 在"占位符比参数少"时,把多出来的参数当成
// `sql.NamedArg` 塞进 `stmt.Vars` —— **却不给它写对应的 `?`**。
// 于是最终 SQL 有 4 个 ?(含 GORM 自动加的 `updated_at=?`)、5 个参数。
//
// # 为什么单测查不出来
//
// `database/sql` 只在驱动报告 `Stmt.NumInput() >= 0` 时才核对参数个数:
//
//   - 单测用的 glebarez/sqlite(底层 modernc.org/sqlite)报 **-1**(=不核对)
//     ⇒ **单测恒绿**,这个 bug 能在仓库里活很久;
//   - MySQL 驱动报真实个数 ⇒ 直接炸 "expected N arguments, got M"。
//
// ⇒ 所以本文件**自己装一个会核对个数的连接池**,把生产环境的行为搬进单测。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	把 holdQuota 的实参改回 3 个(userID, size, size)
//	  → TestHoldQuota_ArgCountMatchesPlaceholders   RED
//	  → TestReceive_QuotaArgCountMatches           RED
//	  → TestHoldQuota_QuotaGateStillBindsRight      RED(闸门被绑歪了)
package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ── 会核对参数个数的连接池 ───────────────────────────────────────────

// argCheckPool 包一层 gorm.ConnPool:每次执行前核对
// "SQL 里的 ? 个数 == 实参个数"。
//
// ⚠️ **报错文案和 database/sql 一模一样**("sql: expected N arguments, got M"),
// 这样用例失败时看到的红字就是用户看到的红字 —— 一眼能认出是不是同一个 bug。
type argCheckPool struct {
	inner   gorm.ConnPool
	counter *argCounter
}

// argCounter 记录每一条被执行过的 SQL
type argCounter struct {
	mu      sync.Mutex
	checked []argCheck
}

type argCheck struct {
	SQL          string
	Placeholders int
	Args         int
	Err          error
}

func (c *argCounter) record(sql string, args int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checked = append(c.checked, argCheck{
		SQL:          sql,
		Placeholders: strings.Count(sql, "?"),
		Args:         args,
		Err:          err,
	})
}

func (c *argCounter) all() []argCheck {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]argCheck, len(c.checked))
	copy(out, c.checked)
	return out
}

// checkArgCount 核对占位符个数。
//
// ⚠️ 这里用 `strings.Count(sql, "?")` 数 —— 它**不区分字符串字面量里的 ?**。
// 本仓的 SQL 片段里没有字面量内的 ?,所以够用;真出现了再说。
func checkArgCount(sql string, args []interface{}) error {
	n := strings.Count(sql, "?")
	if n != len(args) {
		return fmt.Errorf("sql: expected %d arguments, got %d", n, len(args))
	}
	return nil
}

func (p *argCheckPool) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return p.inner.PrepareContext(ctx, query)
}

func (p *argCheckPool) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	err := checkArgCount(query, args)
	p.counter.record(query, len(args), err)
	if err != nil {
		return nil, err
	}
	return p.inner.ExecContext(ctx, query, args...)
}

func (p *argCheckPool) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	err := checkArgCount(query, args)
	p.counter.record(query, len(args), err)
	if err != nil {
		return nil, err
	}
	return p.inner.QueryContext(ctx, query, args...)
}

// QueryRowContext 不能提前返错(sql.Row 的错误字段是私有的),
// 所以只记录不拦截 —— 计数断言照样能抓到它。
func (p *argCheckPool) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	p.counter.record(query, len(args), checkArgCount(query, args))
	return p.inner.QueryRowContext(ctx, query, args...)
}

// BeginTx 事务里也要继续核对 —— 事务用同一个 counter。
//
// ⚠️ **只能实现 gorm.ConnPoolBeginner,不能实现 gorm.TxBeginner**:
// GORM 的 `db.Begin()` 先判 `TxBeginner`(签名是 BeginTx → *sql.Tx),
// 再判 `ConnPoolBeginner`(签名是 BeginTx → gorm.ConnPool)。
// 实现了前者的话,GORM 会拿返回的裸 *sql.Tx 当连接池 ——
// **事务里的 SQL 就绕过了本层,核对不到**。
func (p *argCheckPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	var (
		tx  gorm.ConnPool
		err error
	)
	switch b := p.inner.(type) {
	case gorm.ConnPoolBeginner:
		tx, err = b.BeginTx(ctx, opts)
	case gorm.TxBeginner:
		sqlTx, terr := b.BeginTx(ctx, opts)
		if terr != nil {
			return nil, terr
		}
		tx = sqlTx
	default:
		// ⚠️ **必须原样返回 gorm.ErrInvalidTransaction**,不能自己编一个错。
		//
		// 走到这里说明内层已经是事务(*sql.Tx 在 Go 1.26 里**没有** BeginTx 方法,
		// 起不了嵌套事务)。GORM 的 `callbacks.BeginTransaction` 正是靠
		// `tx.Error == gorm.ErrInvalidTransaction` 来判断"已经在事务里了" ——
		// 它会把这个错**清掉**继续往下走。
		// 自己编错 ⇒ GORM 认为这是真故障 ⇒ Create 直接失败
		// (症状:事务里的 insert 报一个莫名其妙的错)。
		return nil, gorm.ErrInvalidTransaction
	}
	if err != nil {
		return nil, err
	}
	// ⚠️ 事务池**必须**带 Commit/Rollback —— GORM 靠
	// `db.Statement.ConnPool.(TxCommitter)` 判断"是不是已经在事务里"。
	// 普通池**不能**带这两个方法,否则 GORM 会以为最外层也在事务里,
	// 于是对 *sql.DB 直接 SAVEPOINT(会炸)。
	return &argCheckTxPool{argCheckPool{inner: tx, counter: p.counter}}, nil
}

// argCheckTxPool 事务用的池:比普通池多两个 Commit/Rollback。
type argCheckTxPool struct {
	argCheckPool
}

func (p *argCheckTxPool) Commit() error {
	c, ok := p.inner.(gorm.TxCommitter)
	if !ok {
		return fmt.Errorf("argCheckTxPool: 内层不支持提交")
	}
	return c.Commit()
}

func (p *argCheckTxPool) Rollback() error {
	c, ok := p.inner.(gorm.TxCommitter)
	if !ok {
		return fmt.Errorf("argCheckTxPool: 内层不支持回滚")
	}
	return c.Rollback()
}

// installArgCountCheck 给 db 装上会核对参数个数的连接池。
func installArgCountCheck(t *testing.T, db *gorm.DB) *argCounter {
	t.Helper()
	c := &argCounter{}
	require.NotNil(t, db.Statement.ConnPool, "连接池没装上,这条用例等于没跑")
	db.Statement.ConnPool = &argCheckPool{inner: db.Statement.ConnPool, counter: c}
	return c
}

// assertNoArgMismatch 断言这批 SQL 的占位符个数都等于实参个数。
func assertNoArgMismatch(t *testing.T, c *argCounter) {
	t.Helper()
	checked := c.all()
	require.NotEmpty(t, checked, "一条 SQL 都没记录到 —— 连接池没接上,用例等于没跑")
	for _, q := range checked {
		require.NoError(t, q.Err,
			"SQL 的占位符(%d 个)与实参(%d 个)个数不一致:\n%s", q.Placeholders, q.Args, q.SQL)
	}
}

// ── 用例 ─────────────────────────────────────────────────────────────

// 特征:**预扣配额这条 SQL 的占位符个数 == 实参个数**。
//
// 修复前:多传了一个 size ⇒ 4 个 ? 带 5 个参数 ⇒
// holdQuota 直接返 "预扣配额失败: sql: expected 4 arguments, got 5"。
func TestHoldQuota_ArgCountMatchesPlaceholders(t *testing.T) {
	db := newFileTestDB(t)
	counter := installArgCountCheck(t, db)
	uid := seedFileUser(t, db, 1000)

	ok, err := holdQuota(db, int64(uid), 100)
	require.NoError(t, err, "预扣配额不该报 SQL 参数错误")
	require.True(t, ok, "配额充足,预扣必须成功")
	require.Equal(t, int64(100), usedBytes(t, db, uid), "配额要真的扣掉")

	assertNoArgMismatch(t, counter)
}

// 特征:**上传链路整体不炸** —— 走真实 provider + 真实 sqlite。
//
// 这一条是用户报的那个 bug 的**端到端复现**:修复前
// svc.Receive 会返 "预扣配额失败"。
func TestReceive_QuotaArgCountMatches(t *testing.T) {
	db := newFileTestDB(t)
	counter := installArgCountCheck(t, db)
	uid := seedFileUser(t, db, 0)
	svc, _ := newFileSvc(t, db)

	body := []byte("quota arg count payload")
	_, err := svc.Receive(context.Background(), mkFileCaller(uid), FileReceiveReq{
		Biz: "asset", FileName: "a.txt", ContentType: "text/plain",
		Size: int64(len(body)), Body: bytes.NewReader(body),
	})
	require.NoError(t, err, "上传失败(修复前这里是 '预扣配额失败: sql: expected 4 arguments, got 5')")
	require.Equal(t, int64(len(body)), usedBytes(t, db, uid))

	assertNoArgMismatch(t, counter)
}

// 特征:**改了实参个数之后,闸门还绑在对的列上**。
//
// ⚠️ 这条守的是"少传一个 size"可能引入的**另一种错**:如果哪天把
// `Where("id = ? AND "+quotaWhere, userID, size)` 写成了
// `Where("id = ? AND "+quotaWhere, size, userID)`(占位符个数对、顺序错),
// 那条 SQL 会变成"拿 size 去比 id" —— **不报错,但闸门永远开着**,
// 超额用户能无限传。所以必须显式断言边界行为。
func TestHoldQuota_QuotaGateStillBindsRight(t *testing.T) {
	db := newFileTestDB(t)
	counter := installArgCountCheck(t, db)
	uid := seedFileUser(t, db, 100) // 只给 100 字节

	// 1) 装得下
	ok, err := holdQuota(db, int64(uid), 50)
	require.NoError(t, err)
	require.True(t, ok, "50 < 100,必须放行")
	require.Equal(t, int64(50), usedBytes(t, db, uid))

	// 2) 再装 60 就超了(50+60=110 > 100)⇒ 拒,且**账不动**
	ok, err = holdQuota(db, int64(uid), 60)
	require.NoError(t, err, "超配额是'空间不足',不是 SQL 炸了")
	require.False(t, ok, "超配额必须被闸门挡住")
	require.Equal(t, int64(50), usedBytes(t, db, uid), "被拒的请求不能改账")

	// 3) 恰好装满(50+50=100)⇒ 放行
	ok, err = holdQuota(db, int64(uid), 50)
	require.NoError(t, err)
	require.True(t, ok, "恰好等于上限必须放行(比较是 <=)")
	require.Equal(t, int64(100), usedBytes(t, db, uid))

	assertNoArgMismatch(t, counter)
}

// 特征:**不限额(q quota=0)的人不受闸门限制**。
//
// ⚠️ quotaWhere 把 0 当"不限",这个语义和 `model.SysUser.QuotaExceeded`
// 必须一致(两处都把 0 当不限)。绑定错了的话,不限额的人会被当成
// "used + size <= 0" 判死 —— 表现是"所有人都传不了文件"。
func TestHoldQuota_UnlimitedUserIsNotBlocked(t *testing.T) {
	db := newFileTestDB(t)
	counter := installArgCountCheck(t, db)
	uid := seedFileUser(t, db, 0) // quota=0 = 不限

	ok, err := holdQuota(db, int64(uid), 1<<30)
	require.NoError(t, err)
	require.True(t, ok, "不限额的人不能被闸门挡住")
	require.Equal(t, int64(1<<30), usedBytes(t, db, uid))

	assertNoArgMismatch(t, counter)
}
