// slowquery —— 慢 SQL 报警
//
// ## 这个包解决什么问题
//
// "这个接口最近慢了 3 秒,SQL 到底慢在哪一条"—— 慢查询报警。
// 不用自己写 GORM callback(要克隆 Statement、还要挑注册位置,坑很多),
// 包一层 logger 就完事。
//
// ## 关键点
//
//   - **两个入口**:
//     `Install(db, cfg)` 给已有 db 包一层(原 logger 照跑,只是 Trace 被接管);
//     `NewLogger(orig, cfg)` 直接造一个 logger.Interface,塞进
//     `gorm.Config{Logger: ...}`
//   - **原 logger 不会被顶掉**。Info / Warn / Error / LogMode 全是透传,
//     只有 Trace 多走一步:比对耗时,超阈值才回调
//   - **OnSlow 里的 panic 会被吞掉**。慢查询报警是旁路,不该把业务请求带崩
//   - **Entry.SQL 永远有值**,但 `LogRawSQL` 只决定**默认回调**(zap warn 日志)
//     要不要把 SQL 打出来 —— 生产期 SQL 长,默认不打
//   - **指标 `app_slow_sql_total`** 按 table / op / is_error 三个 label 计数,
//     这是 Grafana 上"哪张表最慢"的面板数据源
//   - **Threshold <= 0 会退回 500ms**,不是"不限" —— 与 smslimit 的
//     `Config<=0 用默认值` 是同一条口径
//   - **表名是从 SQL 里正则抠出来的**,拿不到时是 "unknown";
//     操作类型看 SQL 开头(select / insert / update / delete,其它算 other)
//
// ## 跑法
//
//	go run ./examples/slowquery
package main

import (
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"rapid-admin/internal/pkg/slowquery"
)

// demoThreshold 演示用的阈值。生产按业务定(本仓默认 500ms)。
//
// ⚠️ 阈值必须**大于时钟粒度**:一条跑得极快的 SQL,实测耗时可能正好是 0,
// 那它当然不算慢查询(0 < 阈值)。想"什么 SQL 都报"不能靠调阈值到 0 ——
// 0 会被当成"没配",直接退回 500ms。
const demoThreshold = 10 * time.Millisecond

// heavyRows 递归 CTE 的行数。SQLite 里大概 80~100ms,足够越过阈值。
const heavyRows = 300000

// widgetDDL 演示用的表(生产上的表由 AutoMigrate 建)
const widgetDDL = `
CREATE TABLE IF NOT EXISTS widgets (
	id    INTEGER PRIMARY KEY,
	name  TEXT,
	price REAL
)`

func main() {
	// ── 一、Install:给已有 db 包一层 ───────────────────────────
	fmt.Println("=== 一、Install(db, cfg) ===")
	db := openDB("file:slowquery_demo1?mode=memory&cache=shared")
	seedWidgets(db)

	var hits []slowquery.Entry
	if err := slowquery.Install(db, slowquery.Config{
		Threshold: demoThreshold,
		OnSlow: func(e slowquery.Entry) {
			hits = append(hits, e)
		},
	}); err != nil {
		fmt.Println("装 slowquery 失败:", err)
		return
	}
	fmt.Println("  装了(阈值 10ms)")

	// 快的查询:不越过阈值
	seedWidgets(db)
	fmt.Printf("  插了 3 条、查了 1 次 —— 回调触发 %d 次(快查询不报警)\n", len(hits))

	// 慢的查询:越过阈值
	runHeavyQuery(db)
	fmt.Printf("  跑完一条重查询 —— 回调触发 %d 次\n", len(hits))
	for _, e := range hits {
		printEntry(e)
	}
	fmt.Println("  ⇒ Install 是**幂等**的:同一个 db 装两次,第二次直接返回 nil")

	// ── 二、NewLogger:直接造一个 logger ────────────────────────
	fmt.Println()
	fmt.Println("=== 二、NewLogger(orig, cfg) ===")
	// 这条路适合 gorm.Open 的时候就把 logger 定好:
	//
	//	db, _ := gorm.Open(dialector, &gorm.Config{
	//	    Logger: slowquery.NewLogger(logger.Default, cfg),
	//	})
	gl := slowquery.NewLogger(gormlogger.Default.LogMode(gormlogger.Silent), slowquery.Config{
		Threshold: demoThreshold,
		OnSlow: func(e slowquery.Entry) {
			fmt.Printf("  命中:表=%s 操作=%s 耗时=%v\n", e.Table, e.Op, e.Duration.Round(time.Millisecond))
		},
	})
	db2 := openDB("file:slowquery_demo2?mode=memory&cache=shared", gl)
	runHeavyQuery(db2)
	fmt.Println("  ⇒ 原 logger 照跑(Info/Warn/Error 全透传),只是 Trace 多走一步")

	// ── 三、慢 **且** 失败 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 三、慢且失败的 SQL ===")
	db3 := openDB("file:slowquery_demo3?mode=memory&cache=shared")
	if err := slowquery.Install(db3, slowquery.Config{
		Threshold: demoThreshold,
		OnSlow: func(e slowquery.Entry) {
			if e.Error != "" {
				fmt.Printf("  慢 SQL 且失败:表=%s 错误=%q\n", e.Table, e.Error)
			}
		},
	}); err != nil {
		fmt.Println("装 slowquery 失败:", err)
		return
	}
	runFailingHeavyQuery(db3)
	fmt.Println("  ⇒ 失败的 SQL 同样会进回调,Error 字段有值 ——")
	fmt.Println("    「慢」和「错」是两个维度,指标上分别落在 is_error=true/false")

	// ── 四、默认回调(不传 OnSlow)长什么样 ─────────────────────
	fmt.Println()
	fmt.Println("=== 四、默认回调(不传 OnSlow) ===")
	core, logs := observer.New(zapcore.WarnLevel)
	db4 := openDB("file:slowquery_demo4?mode=memory&cache=shared")
	if err := slowquery.Install(db4, slowquery.Config{
		Threshold: demoThreshold,
		Logger:    zap.New(core),
		// 不传 OnSlow ⇒ 用默认的:一条 zap warn 日志
	}); err != nil {
		fmt.Println("装 slowquery 失败:", err)
		return
	}
	runHeavyQuery(db4)
	for _, l := range logs.All() {
		fmt.Printf("  日志: %s\n", l.Message)
		fields := l.ContextMap()
		fmt.Printf("    table=%v op=%v duration=%v rows=%v\n",
			fields["table"], fields["op"], fields["duration"], fields["rows"])
		_, hasSQL := fields["sql"]
		fmt.Printf("    带 sql 字段吗 = %v(LogRawSQL 默认 false,不打 SQL)\n", hasSQL)
	}

	// ── 五、指标 ──────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 五、指标 app_slow_sql_total ===")
	printSlowMetric()
	fmt.Println("  ⇒ Grafana 上「哪张表最慢」的面板就是这张表")
	fmt.Println("    label: table(哪张表) / op(query|create|update|delete) / is_error")
}

// openDB 开一个内存库(可指定自定义 logger)
func openDB(dsn string, l ...gormlogger.Interface) *gorm.DB {
	cfg := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	if len(l) > 0 {
		cfg.Logger = l[0]
	}
	db, err := gorm.Open(sqlite.Open(dsn), cfg)
	if err != nil {
		fmt.Println("开库失败:", err)
		return nil
	}
	if err := db.Exec(widgetDDL).Error; err != nil {
		fmt.Println("建表失败:", err)
		return nil
	}
	return db
}

// seedWidgets 插几条 + 查一次(都是快查询)
func seedWidgets(db *gorm.DB) {
	if db == nil {
		return
	}
	for _, w := range []struct {
		name  string
		price float64
	}{
		{"户型图 A", 120.5},
		{"户型图 B", 98},
		{"车位", 350},
	} {
		db.Exec("INSERT INTO widgets (name, price) VALUES (?, ?)", w.name, w.price)
	}
	var n int64
	db.Raw("SELECT COUNT(*) FROM widgets").Scan(&n)
}

// runHeavyQuery 跑一条真的慢的查询(递归 CTE,几十万行)
func runHeavyQuery(db *gorm.DB) {
	if db == nil {
		return
	}
	var n int64
	db.Raw(fmt.Sprintf(`WITH RECURSIVE seq(x) AS
		(SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x < %d)
		SELECT COUNT(*) FROM seq`, heavyRows)).Scan(&n)
}

// runFailingHeavyQuery 跑一条**又慢又失败**的查询
//
// 手法:CHECK 约束写成"不等于最后一行",于是前面几十万行都插得进去,
// 直到最后一行才违反约束 → 错误发生在重活干完之后,而不是一上来就报错。
func runFailingHeavyQuery(db *gorm.DB) {
	if db == nil {
		return
	}
	db.Exec(fmt.Sprintf("CREATE TABLE guard (x INTEGER CHECK (x <> %d))", heavyRows))
	err := db.Exec(fmt.Sprintf(`INSERT INTO guard (x) SELECT x FROM
		(WITH RECURSIVE seq(x) AS
		 (SELECT 1 UNION ALL SELECT x+1 FROM seq WHERE x < %d)
		 SELECT x FROM seq)`, heavyRows)).Error
	if err != nil {
		fmt.Printf("  (这条 SQL 的错误 = %v)\n", err)
	}
}

// printEntry 打印一条慢查询记录
func printEntry(e slowquery.Entry) {
	fmt.Printf("  表=%s 操作=%s 耗时=%v 影响行数=%d\n",
		e.Table, e.Op, e.Duration.Round(time.Millisecond), e.Rows)
	fmt.Printf("    SQL = %s\n", e.SQL)
}

// printSlowMetric 从默认 registry 里把慢 SQL 指标读出来
func printSlowMetric() {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		fmt.Println("  读指标失败:", err)
		return
	}
	found := false
	for _, mf := range families {
		if mf.GetName() != "app_slow_sql_total" {
			continue
		}
		found = true
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			fmt.Printf("    table=%s op=%s is_error=%s 次数=%v\n",
				labels["table"], labels["op"], labels["is_error"], m.GetCounter().GetValue())
		}
	}
	if !found {
		fmt.Println("    (没有采集到 —— 说明本次运行没有一条 SQL 超阈值)")
	}
}
