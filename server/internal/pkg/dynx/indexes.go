package dynx

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ============================================================================
// 集合级索引配置(2026-11-03 M2,对标 PocketBase collection.Indexes)
// ============================================================================

// IndexDef 一条用户自定义索引(结构化,不是 PB 那种整句 DDL)
//
// ⚠️ **存列的「字段标识」,不是物理列名** ——
// 用户界面上看到和选的是字段标识,定义里存的也是它;
// 拼物理列(f_<key>)是服务层的事(见 dyn_impl.go 的 syncCollectionIndexes)。
// 存物理列名的话,设计器和数据库两套名字混在一个 JSON 里,没人认得哪个是哪个。
//
// ⚠️ **不用 PB 的「整句 DDL」形态** ——
// PocketBase 的 Indexes 是 `[]string`,存的就是 CREATE INDEX 原文,
// 灵活但等于把 SQL 拼接面交给了管理员;结构化定义 + 白名单校验
// (列必须是在用字段)才防得住"字段删了索引忘删"整类 bug。
type IndexDef struct {
	// Name 索引名(短名,拼物理索引名时会加前缀)
	Name string `json:"name"`
	// Columns 索引列(字段标识,顺序即复合索引顺序)
	Columns []string `json:"columns"`
	// Unique 是否唯一索引
	Unique bool `json:"unique"`
}

// maxIndexColumns 单条索引的最大列数
//
// ⚠️ **MySQL 的硬上限是 16 列** —— SQLite 没有限制,但取两边较小的那个,
// 同一份定义才在两个方言下行为一致。
const maxIndexColumns = 16

// UserIndexPrefix 用户自建索引的命名前缀
//
// ⚠️ **必须有一眼认得出的前缀** ——
// 物理表上还有系统自建的 idx_<phys>_owner / _deleted,
// "重建用户索引"(先 drop 后 create)只许动自己的,系统索引一个都不能碰;
// 没有前缀就没法把两类索引分开,而"误删系统索引"是静默的性能退化,查不出来。
const userIndexPrefix = "dix_"

// UserIndexPrefix(phys) 该物理表上用户索引的完整前缀(`dix_<phys>_`)
func UserIndexPrefix(phys string) string {
	return userIndexPrefix + phys + "_"
}

// UserIndexName 短名 → 物理索引名(`dix_<phys>_<name>`)
//
// ⚠️ **MySQL 的标识符上限是 64 字符** ——
// 超长在 MySQL 直接报错、在 SQLite 却能建成功,同一份定义跨方言行为分裂;
// 在这里统一挡住,报错信息里给清楚"谁太长了"。
func UserIndexName(phys, name string) (string, error) {
	full := userIndexPrefix + phys + "_" + name
	if len(full) > 64 {
		return "", fmt.Errorf("索引名过长(%s,共 %d 字符,上限 64;换个短名)", full, len(full))
	}
	return full, nil
}

// ValidateIndex 校验单条索引定义(结构层:名字合法 + 列数与重复)
//
// ⚠️ **只查结构,不查列存在性** ——
// "引用的字段在不在用"需要查集合的字段行,那是服务层的职责
// (dynx 是纯函数包,不碰数据库);见 dyn_impl.go 的 syncCollectionIndexes。
func ValidateIndex(d IndexDef) error {
	if err := ValidateFieldKey(d.Name); err != nil {
		return fmt.Errorf("索引名非法:%w", err)
	}
	if len(d.Columns) == 0 {
		return fmt.Errorf("索引 %s 至少要有一列", d.Name)
	}
	if len(d.Columns) > maxIndexColumns {
		return fmt.Errorf("索引 %s 最多 %d 列(MySQL 的硬上限)", d.Name, maxIndexColumns)
	}
	seen := make(map[string]struct{}, len(d.Columns))
	for _, c := range d.Columns {
		c = strings.TrimSpace(c)
		if c == "" {
			return fmt.Errorf("索引 %s 的列名不能为空", d.Name)
		}
		if _, dup := seen[c]; dup {
			return fmt.Errorf("索引 %s 的列 %q 重复", d.Name, c)
		}
		seen[c] = struct{}{}
	}
	return nil
}

// ParseIndexes 解析集合行里存的索引定义 JSON 文本
//
// ⚠️ **空串不算错** —— 集合没配索引时存的就是空串。
// ⚠️ **定义之间不许重名** —— 重名的话"重建"时第二条会撞第一条。
func ParseIndexes(raw string) ([]IndexDef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var defs []IndexDef
	if err := json.Unmarshal([]byte(trimmed), &defs); err != nil {
		return nil, fmt.Errorf("索引定义不是合法 JSON: %w", err)
	}
	seen := make(map[string]struct{}, len(defs))
	for i := range defs {
		if err := ValidateIndex(defs[i]); err != nil {
			return nil, err
		}
		if _, dup := seen[defs[i].Name]; dup {
			return nil, fmt.Errorf("索引名 %q 重复", defs[i].Name)
		}
		seen[defs[i].Name] = struct{}{}
	}
	return defs, nil
}

// MarshalIndexes 把索引定义序列化成 JSON 文本(空集返空串)
func MarshalIndexes(defs []IndexDef) (string, error) {
	if len(defs) == 0 {
		return "", nil
	}
	b, err := json.Marshal(defs)
	if err != nil {
		return "", fmt.Errorf("序列化索引定义失败: %w", err)
	}
	return string(b), nil
}

// CreateIndexSQL 拼建索引的 DDL(MySQL / SQLite 语法一致,反引号两边都认)
//
// ⚠️ **cols 是物理列名,顺序即复合索引顺序** —— 由服务层按字段标识映射好再传进来。
func CreateIndexSQL(phys, idxName string, d IndexDef, cols []string) string {
	quoted := make([]string, 0, len(cols))
	for _, c := range cols {
		quoted = append(quoted, "`"+c+"`")
	}
	unique := ""
	if d.Unique {
		unique = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX `%s` ON `%s` (%s)", unique, idxName, phys, strings.Join(quoted, ", "))
}
