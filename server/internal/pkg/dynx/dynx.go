// Package dynx —— 动态业务集合的「字段类型注册表」
//
// # 这个包解决什么问题
//
// 后台的「业务表单设计器」让用户自己设计业务表(比如"设备巡检记录")。
// 每设计一个字段,系统要同时回答三个问题:
//
//  1. 这个字段在数据库里落成什么列?(DDL 类型)
//  2. 前端提交的 JSON 值怎么变成能存进去的值?(归一化)
//  3. 存进去的值怎么变成前端能显示的值?(反归一化)
//
// ⚠️ **这三个问题必须由同一张表回答。**
//
// 各写各的就会出现"DDL 说它是 VARCHAR、归一化却塞了个 int64"这种分叉 ——
// 表现是"存进去看着对,查出来类型变了",而且**不报错**。
// 所以本包把三种行为挂在同一张 `Spec` 表上,新增类型只加一行。
//
// # 为什么日期/时间存字符串,不用 time.Time
//
// 本仓的 DB 有两个方言(MySQL 8 / SQLite),两个驱动的 time.Time
// 存取行为不一样(MySQL 走 DATETIME 列,SQLite 走 TEXT 列 + 自己的字符串格式),
// 同一份代码在开发库和生产库读出来的可能不是一个类型。
//
// ⇒ 统一存 **ISO-8601 字符串**:
//
//	date     → "2026-11-01"
//	datetime → "2026-11-01T00:30:00Z"(统一 UTC)
//
// 顺带拿到一个好处:**字典序 == 时间序** ——
// 用 VARCHAR 排序时不必关心列类型,跨方言行为一致。
//
// ⚠️ **代价:带时区偏移的输入必须先转成 UTC。**
// 否则 "…+08:00" 和 "…Z" 混排时字典序不等于时间序(归一化里已经统一转 UTC)。
//
// # 不带时区的输入按哪个时区解释
//
// 按 **东八区**(timeutil.AsiaShanghai)—— 与本仓其它业务时间同一口径。
// 用户在表单里填 "2026-11-01 08:30:00",指的是公司里的 8 点半。
package dynx

import (
	"fmt"
	"strings"
)

// Type 字段类型
type Type string

const (
	// TypeText 单行文本
	TypeText Type = "text"
	// TypeLongText 多行文本
	TypeLongText Type = "longtext"
	// TypeInt 整数
	TypeInt Type = "int"
	// TypeNumber 小数
	TypeNumber Type = "number"
	// TypeBool 开关
	TypeBool Type = "bool"
	// TypeDate 日期(yyyy-mm-dd)
	TypeDate Type = "date"
	// TypeDateTime 日期时间(ISO-8601 UTC)
	TypeDateTime Type = "datetime"
	// TypeSelect 单选
	TypeSelect Type = "select"
	// TypeMultiSelect 多选
	TypeMultiSelect Type = "multiselect"
	// TypeJSON JSON
	TypeJSON Type = "json"
	// TypeFile 附件(文件的 key,不带域名)
	//
	// ⚠️ **存的是 key,不是 URL** —— 见 docs/业务表单设计器设计方案V1.0.md §2.9。
	// URL 里带域名(本地部署 / CDN 部署形状不同),域名会变;
	// key 是两种 provider 下形状一致的那一个。
	TypeFile Type = "file"
	// TypeRelation 关联记录(目标集合里一条记录的 id)
	//
	// ⚠️ **存的是目标记录的 id,不是显示文本** ——
	// 显示文本(display_field)是目标集合的**可变数据**:改名后旧值全错;
	// id 是不变的主键。显示名在读的时候现查(记录视图的 `_rels`,
	// 口径同 owner_name —— 显示名从不落库)。
	//
	// ⚠️ **一期只做单值** —— 多值(PB 的 MaxSelect>1)要 JSON 文本落库 +
	// 跨方言 JSON_EACH 才能筛选,与 M3 已拍板"multiselect 不开放 IN 筛选"
	// 是同一类语义陷阱(见升级路线 §6 忽略清单)。
	TypeRelation Type = "relation"
)

// Spec 一个字段类型的规格
//
// ⚠️ **DDL 类型按方言分列,不是一张通用表** ——
// MySQL 有真正的 JSON / DECIMAL 类型,SQLite 没有(它靠"类型亲和性"模拟)。
// 强行统一成一种列类型,后果是某个方言下存不进去或读出来类型变了。
type Spec struct {
	// Type 类型标识
	Type Type `json:"type"`
	// Label 中文名(前端下拉框用)
	Label string `json:"label"`
	// MySQLType MySQL 的 DDL 列类型
	MySQLType string `json:"mysql_type"`
	// SQLiteType SQLite 的 DDL 列类型
	SQLiteType string `json:"sqlite_type"`
	// HasOptions 该类型是否需要选项集(select / multiselect)
	HasOptions bool `json:"has_options"`
}

// ColumnType 取该类型在指定方言下的 DDL 列类型
//
// ⚠️ **未知方言回落 SQLite 类型** —— SQLite 的类型亲和性更宽松
// (TEXT 亲和的列什么都能装),拿它兜底不会写出一个"存不进去"的表。
// 真正的方言校验在 service 层做(建表前先确认驱动是 mysql / sqlite)。
func (s Spec) ColumnType(dialect string) string {
	if strings.EqualFold(dialect, "mysql") {
		return s.MySQLType
	}
	return s.SQLiteType
}

// specs 全部字段类型(顺序即前端下拉框顺序)
//
// ⚠️ **MySQL 的 number 用 DOUBLE 而不是 DECIMAL** ——
// DECIMAL 列经 go-sql-driver 读出来是 []byte("123.450000"),
// 前端拿到的是字符串而不是数字;DOUBLE 直接给 float64。
// ⚠️ **json 列在两个方言里都是 TEXT** ——
// MySQL 的 JSON 类型要求插入的是**合法 JSON 文档**,用户随手输入的
// 半截 JSON 会直接报错;TEXT 只存字符串,校验挪到归一化那一层(统一口径)。
var specs = []Spec{
	{Type: TypeText, Label: "单行文本", MySQLType: "VARCHAR(255)", SQLiteType: "TEXT"},
	{Type: TypeLongText, Label: "多行文本", MySQLType: "TEXT", SQLiteType: "TEXT"},
	{Type: TypeInt, Label: "整数", MySQLType: "BIGINT", SQLiteType: "INTEGER"},
	{Type: TypeNumber, Label: "小数", MySQLType: "DOUBLE", SQLiteType: "REAL"},
	{Type: TypeBool, Label: "开关", MySQLType: "TINYINT(1)", SQLiteType: "INTEGER"},
	{Type: TypeDate, Label: "日期", MySQLType: "VARCHAR(10)", SQLiteType: "TEXT"},
	{Type: TypeDateTime, Label: "日期时间", MySQLType: "VARCHAR(32)", SQLiteType: "TEXT"},
	{Type: TypeSelect, Label: "单选", MySQLType: "VARCHAR(255)", SQLiteType: "TEXT", HasOptions: true},
	{Type: TypeMultiSelect, Label: "多选", MySQLType: "TEXT", SQLiteType: "TEXT", HasOptions: true},
	{Type: TypeJSON, Label: "JSON", MySQLType: "TEXT", SQLiteType: "TEXT"},
	// ⚠️ **VARCHAR(512) 对齐 sys_file.file_key 的长度** ——
	// 那边是 varchar(512),这里短一截就**存不下**(不是截断),
	// 表现是"选了文件却存不进去",而报错说的是数据库。
	{Type: TypeFile, Label: "附件", MySQLType: "VARCHAR(512)", SQLiteType: "TEXT"},
	// ⚠️ **BIGINT 与系统列 id 同型** ——
	// 存的就是另一张 dyn_ 表的主键 id(sonyflake,量级 < int64 上限);
	// 与 dynColTypes 的 id 列同一种 DDL,跨表 JOIN / IN 的类型才对得齐。
	{Type: TypeRelation, Label: "关联记录", MySQLType: "BIGINT", SQLiteType: "INTEGER"},
}

// Lookup 查一个类型的规格
func Lookup(t Type) (Spec, bool) {
	for _, s := range specs {
		if s.Type == t {
			return s, true
		}
	}
	return Spec{}, false
}

// Types 列出全部字段类型(顺序固定)
func Types() []Spec {
	out := make([]Spec, len(specs))
	copy(out, specs)
	return out
}

// TypeNames 列出全部字段类型名(顺序固定)
func TypeNames() []string {
	out := make([]string, 0, len(specs))
	for _, s := range specs {
		out = append(out, string(s.Type))
	}
	return out
}

// ValidateType 校验类型名是否受支持
func ValidateType(t Type) error {
	if _, ok := Lookup(t); !ok {
		return fmt.Errorf("未知的字段类型 %q(可选:%s)", t, strings.Join(TypeNames(), " / "))
	}
	return nil
}
