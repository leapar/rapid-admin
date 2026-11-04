package dynx

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Denormalize 把数据库里的值变回「前端能显示」的值
//
// ⚠️ **反归一化必须容错** ——
// 数据库里同一个列,不同方言、不同驱动读出来的 Go 类型可能不一样:
// MySQL 的 VARCHAR 给 string,SQLite 的 TEXT 给 string,
// MySQL 的 DECIMAL 给 []byte,MySQL 的 BIGINT 给 int64,
// SQLite 的 INTEGER 给 int64,BOOLEAN 某些驱动给 bool。
//
// ⇒ 这一层做的是**「宽松地读」**:能转就转,转不了就原样返回。
// 严格校验放在写入那一侧(Normalize)—— 写进去的就是合法的,
// 读出来只需要能显示。
func Denormalize(t Type, raw any) any {
	if raw == nil {
		return nil
	}
	switch t {
	// ⚠️ relation 出线也是文本(id 的字符串形态)——
	// id 可能是 19 位的 uint64 量级(sonyflake),当 JSON number 走
	// JS 的 double 会丢精度(本仓惯例:大 id 一律字符串上 wire)。
	case TypeText, TypeLongText, TypeDate, TypeDateTime, TypeSelect, TypeFile, TypeRelation:
		return denormText(raw)
	case TypeInt:
		return denormInt(raw)
	case TypeNumber:
		return denormFloat(raw)
	case TypeBool:
		return denormBool(raw)
	case TypeMultiSelect:
		return denormStringList(raw)
	case TypeJSON:
		return denormJSON(raw)
	default:
		return raw
	}
}

// denormText 值 → 文本
func denormText(raw any) any {
	switch v := raw.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(v, 10)
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(raw)
	}
}

// denormInt 值 → 整数
func denormInt(raw any) any {
	switch v := raw.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case bool:
		return boolToInt64(v)
	case []byte:
		return parseIntText(string(v))
	case string:
		return parseIntText(v)
	default:
		return raw
	}
}

// parseIntText 文本 → 整数(失败就原样返回)
func parseIntText(s string) any {
	trimmed := strings.TrimSpace(s)
	if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return int64(f)
	}
	return s
}

// denormFloat 值 → 小数
func denormFloat(raw any) any {
	switch v := raw.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case []byte:
		return parseFloatText(string(v))
	case string:
		return parseFloatText(v)
	default:
		return raw
	}
}

// parseFloatText 文本 → 小数(失败就原样返回)
func parseFloatText(s string) any {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return f
	}
	return s
}

// denormBool 值 → 开关
func denormBool(raw any) any {
	switch v := raw.(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case int:
		return v != 0
	case float64:
		return v != 0
	case []byte:
		return parseBoolText(string(v))
	case string:
		return parseBoolText(v)
	default:
		return false
	}
}

// parseBoolText 文本 → 开关
func parseBoolText(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "t", "yes", "y":
		return true
	default:
		return false
	}
}

// denormStringList 值 → 字符串列表
func denormStringList(raw any) any {
	text, ok := raw.(string)
	if !ok {
		if b, isBytes := raw.([]byte); isBytes {
			text = string(b)
		} else {
			return []string{}
		}
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return []string{}
	}
	return out
}

// denormJSON 值 → 任意 JSON 值
func denormJSON(raw any) any {
	text, ok := raw.(string)
	if !ok {
		if b, isBytes := raw.([]byte); isBytes {
			text = string(b)
		} else {
			return raw
		}
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		// 存进去的时候校验过,这里解析不了只可能是历史脏数据 —— 原样返回,不吞
		return text
	}
	return out
}
