package dynx

import (
	"fmt"
	"regexp"
)

// fieldKeyRe 字段标识(用户自己起的英文名)
//
// ⚠️ **必须小写开头、只允许小写字母/数字/下划线** ——
// 这个名字会拼进物理列名和资源码:
//
//	f_<key>              物理列
//	dyn:<code>:list      资源码
//
// 一旦允许大写或短横线,SQL 标识符在不同方言下的引号规则就开始咬人
// (MySQL 的大小写不敏感 vs SQLite 的大小写敏感)。
var fieldKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// reservedFieldKeys 保留的字段标识
//
// ⚠️ **物理列不会撞(有 `f_` 前缀),但 JSON 键会撞** ——
// 记录的 JSON 形状是 `{"id":..., "owner_id":..., "created_at":..., "updated_at":...,
// "<字段标识>": ...}`。字段标识叫 `id` 的话,用户字段的值会**盖掉**系统的 id
// (服务层是"先写系统列,再遍历字段覆盖",后写的赢)。
// ⇒ 前端拿到的 id 就变成了用户填的值 —— 之后所有按 id 的操作全错位。
//
// 所以这里挡的是**JSON 键**这一层,不是物理列那一层。
var reservedFieldKeys = map[string]bool{
	"id":         true,
	"owner_id":   true,
	"created_at": true,
	"updated_at": true,
	"deleted_at": true,
}

// ValidateFieldKey 校验字段标识
func ValidateFieldKey(key string) error {
	if key == "" {
		return fmt.Errorf("字段标识不能为空")
	}
	if !fieldKeyRe.MatchString(key) {
		return fmt.Errorf("字段标识 %q 非法:必须小写字母开头,只含小写字母/数字/下划线,最长 64 位", key)
	}
	if reservedFieldKeys[key] {
		return fmt.Errorf("字段标识 %q 是系统保留名,换一个", key)
	}
	return nil
}

// ColumnName 物理列名
//
// ⚠️ **统一加 `f_` 前缀** ——
// 物理表上还有一批系统列(id / owner_id / created_at / updated_at / deleted_at),
// 前缀保证用户字段**不可能**跟系统列撞名。
// (撞名的后果不是"报错",而是用户的字段悄悄写进了系统列。)
func ColumnName(key string) string {
	return "f_" + key
}

// ValidateCode 校验集合编码
//
// ⚠️ **与字段标识同一套规则** —— 集合编码同样会拼进物理表名和资源码。
func ValidateCode(code string) error {
	if code == "" {
		return fmt.Errorf("集合编码不能为空")
	}
	if !fieldKeyRe.MatchString(code) {
		return fmt.Errorf("集合编码 %q 非法:必须小写字母开头,只含小写字母/数字/下划线,最长 64 位", code)
	}
	return nil
}

// PhysTableName 物理表名
//
// ⚠️ **集合改名不改表名** ——
// 物理表名在建表那一刻定死,之后集合改名只改 sys_dyn_collection.name。
// 理由:表名进了 DDL,改名意味着一次数据搬迁(RENAME TABLE + 改所有外键引用),
// 而这套动态表的收益恰恰是"改个名不影响数据"。
// ⇒ 表名由**编码**决定,编码本身不可改。
func PhysTableName(code string) string {
	return "dyn_" + code
}
