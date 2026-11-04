package dynx

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"rapid-admin/internal/pkg/timeutil"
)

// dateLayout 日期部分
const dateLayout = "2006-01-02"

// dateTimeLayout 日期时间部分(归一化后的落库格式,统一 UTC)
const dateTimeLayout = "2006-01-02T15:04:05Z"

// Normalize 把前端提交的 JSON 值归一化成「能存进数据库」的值
//
// ⚠️ **归一化是唯一写进数据库的入口** ——
// 不做归一化直接存的话,int 字段可能存进 "123"(字符串),
// bool 字段可能存进 "true",日期可能存进 "2026/11/01"。
// 这些值**当场不报错**,等到按类型读出来做计算时才炸,
// 而那时候数据已经写进去了。
//
// ⚠️ **nil 原样返回(nil, nil)** ——
// "没填"和"填了非法值"是两件事:
// 前者由必填校验管,后者归一化管。混在一起会让"选填字段留空"变成报错。
func Normalize(t Type, raw any, opts Options) (any, error) {
	if raw == nil {
		return nil, nil
	}
	spec, ok := Lookup(t)
	if !ok {
		return nil, fmt.Errorf("未知的字段类型 %q", t)
	}
	switch spec.Type {
	case TypeText, TypeLongText:
		v, err := toText(raw)
		if err != nil {
			return nil, err
		}
		return checkTextConstraints(v, opts)
	case TypeInt:
		v, err := toInt64(raw)
		if err != nil {
			return nil, err
		}
		n, _ := v.(int64)
		return checkNumConstraints(float64(n), opts, true)
	case TypeNumber:
		v, err := toFloat64(raw)
		if err != nil {
			return nil, err
		}
		f, _ := v.(float64)
		return checkNumConstraints(f, opts, false)
	case TypeBool:
		return toBool(raw)
	case TypeDate:
		return normalizeDate(raw)
	case TypeDateTime:
		return normalizeDateTime(raw)
	case TypeSelect:
		return normalizeSelect(raw, opts)
	case TypeMultiSelect:
		return normalizeMultiSelect(raw, opts)
	case TypeJSON:
		return normalizeJSON(raw)
	case TypeFile:
		return normalizeFile(raw)
	case TypeRelation:
		return normalizeRelation(raw)
	default:
		return nil, fmt.Errorf("未实现的字段类型 %q", spec.Type)
	}
}

// ============================================================================
// 值约束(M1,2026-11-03)—— Normalize 是唯一写入口,约束在这里落
// ============================================================================

// checkTextConstraints 文本约束:MinLength / MaxLength / Pattern
//
// ⚠️ **长度按字符数(runes)不按字节** —— "张三"是 2 个字,不是 6 字节;
// 按字节算的话中文名 10 个字就超了,而用户数的是字数。
// ⚠️ **Pattern 整串匹配** —— 设计者可以自己写锚点(^…$),没写锚点的
// (如 "[0-9]{6}")由框架自动包成整串匹配:直接 MatchString 是子串匹配,
// "abc123456xyz" 会混过 "[0-9]{6}" 这条约束。
func checkTextConstraints(v any, opts Options) (any, error) {
	s, _ := v.(string)
	if s == "" {
		// 空串 = 没填,归必填校验管;长度约束对空串无意义(否则"选填"没法用)
		return s, nil
	}
	if opts.MinLength != nil && utf8.RuneCountInString(s) < *opts.MinLength {
		return nil, fmt.Errorf("长度不能少于 %d 个字(当前 %d 个)", *opts.MinLength, utf8.RuneCountInString(s))
	}
	if opts.MaxLength != nil && utf8.RuneCountInString(s) > *opts.MaxLength {
		return nil, fmt.Errorf("长度不能超过 %d 个字(当前 %d 个)", *opts.MaxLength, utf8.RuneCountInString(s))
	}
	if p := strings.TrimSpace(opts.Pattern); p != "" {
		// 未锚定的 pattern 包上 ^(?:...)$;设计者自己写了 ^ 或 $ 的按写的来,
		// 不双重包裹(Go regexp 默认 ^ $ 只匹配整串首尾,包了虽然语义等价,
		// 但没必要让"设计者写了什么"变得不可辨认)。
		re, err := regexp.Compile(anchorPattern(p))
		if err != nil {
			// 设计期校验过;走到这说明历史数据里有坏正则 —— 拒绝写入比静默忽略强
			return nil, fmt.Errorf("字段配置的格式约束无效,请联系管理员修正字段设置")
		}
		if !re.MatchString(s) {
			return nil, fmt.Errorf("格式不符合要求(约束:%s)", p)
		}
	}
	return s, nil
}

// anchorPattern 给未锚定的正则补整串锚点。已带 ^ 或 $ 其一的视为设计者
// 自己管锚点,原样返回。
func anchorPattern(p string) string {
	if strings.HasPrefix(p, "^") || strings.HasSuffix(p, "$") {
		return p
	}
	return "^(?:" + p + ")$"
}

// checkNumConstraints 数值约束:Min / Max(isInt=true 时非整数已在 toInt64 拦过)
func checkNumConstraints(v float64, opts Options, isInt bool) (any, error) {
	if opts.Min != nil && v < *opts.Min {
		return nil, fmt.Errorf("不能小于 %v", *opts.Min)
	}
	if opts.Max != nil && v > *opts.Max {
		return nil, fmt.Errorf("不能大于 %v", *opts.Max)
	}
	if isInt {
		return int64(v), nil
	}
	return v, nil
}

// maxFilePathBytes 文件字段值的字节上限//
// ⚠️ **与 sys_file.file_key 的列宽(512)对齐** ——
// 短一截就是"存不下"(数据库报错),长一截则是"存进去了但 key 被截断"
// (截断后的 key 查不到文件 ⇒ 记录里的附件永远打不开)。
const maxFilePathBytes = 512

// normalizeFile 归一化文件字段的值
//
// ⚠️ **只做形状校验,不碰文件系统** ——
// dynx 是纯原语层,拿不到文件服务 ⇒
// "这个文件真的存在吗"由 service 层答(见 internal/service/dyn_records.go)。
//
// ⚠️ **拒带域名的值** ——
// 字段存的是"不带域名的路径"(设计文档 §2.9)。
// 存进 `https://cdn.x.com/rapid-admin/...` 会让"换域名 / 换 CDN"变成一次数据迁移。
//
// ⚠️ **拒 `..`** ——
// key 是要拼进静态路由与对象存储路径的,`..` 能爬出目录。
//
// ⚠️ **拒开头的 `/`** ——
// 存的是**对象存储里的 key**(不带挂载点、不带前导斜杠),
// 前导斜杠会让"按 key 查文件"永远查不到 ⇒ 附件永远打不开。
func normalizeFile(raw any) (any, error) {
	text, err := toText(raw)
	if err != nil {
		return nil, err
	}
	s, _ := text.(string)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if strings.Contains(s, "://") {
		return nil, fmt.Errorf("文件路径不能带域名(只存不带域名的相对路径)")
	}
	if strings.Contains(s, "..") {
		return nil, fmt.Errorf("文件路径不能包含 ..")
	}
	if strings.Contains(s, "\\") {
		return nil, fmt.Errorf("文件路径不能包含反斜杠")
	}
	if strings.HasPrefix(s, "/") {
		return nil, fmt.Errorf("文件路径必须是相对路径(不能以 / 开头)")
	}
	if len(s) > maxFilePathBytes {
		return nil, fmt.Errorf("文件路径过长(最多 %d 字节)", maxFilePathBytes)
	}
	return s, nil
}

// normalizeRelation 归一化关联记录字段的值(目标记录的 id)
//
// ⚠️ "没填"的两种形态都认:nil 原样;空串落成 nil(= 没填),不是 id 0 ——
// id 0 是"指向主键为 0 的记录",而没有任何记录的 id 是 0,
// 存 0 会让列表页渲染出一行永远查不到的引用。
//
// ⚠️ 归一化只做形状校验(正整数 id);存在性由 service 层答(checkRelationTarget)。
// dynx 是纯原语层,拿不到 DB —— 同 normalizeFile 的分工。
func normalizeRelation(raw any) (any, error) {
	// ⚠️ **Go 内部的 id 是 uint64(sonyflake),toText / toInt64 都不认这个形态** ——
	// 服务层内部调用(播种 / 测试 / 未来的内部装配)传 uint64 进来是合法输入,
	// 拒掉会逼着每个调用方先转字符串;HTTP 路径上到不了这里(JSON 没有 uint64)。
	switch v := raw.(type) {
	case uint64:
		if v == 0 {
			return nil, fmt.Errorf("关联记录的 id 必须是正整数")
		}
		return int64(v), nil
	case uint:
		if v == 0 {
			return nil, fmt.Errorf("关联记录的 id 必须是正整数")
		}
		return int64(v), nil
	}
	text, err := toText(raw)
	if err != nil {
		return nil, err
	}
	s, _ := text.(string)
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, err := toInt64(s)
	if err != nil {
		return nil, err
	}
	id, _ := n.(int64)
	if id <= 0 {
		return nil, fmt.Errorf("关联记录的 id 必须是正整数")
	}
	return id, nil
}

// toText 把任意标量转成文本
//
// ⚠️ **数字一律用 'f' 格式 + 最短精度** ——
// strconv.FormatFloat(v, 'f', -1, 64) 给 "1" 而不是 "1.000000",
// 也不会给 "1e+06" 这种科学计数法(用 'g' 就会)。
func toText(raw any) (any, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case int:
		return strconv.Itoa(v), nil
	case json.Number:
		return v.String(), nil
	default:
		return nil, fmt.Errorf("需要文本,收到 %T", raw)
	}
}

// toInt64 把任意标量转成整数
func toInt64(raw any) (any, error) {
	switch v := raw.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case float64:
		if v != math.Trunc(v) {
			return nil, fmt.Errorf("需要整数,收到 %v", v)
		}
		return int64(v), nil
	case bool:
		return boolToInt64(v), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return nil, fmt.Errorf("需要整数,收到 %q", v.String())
		}
		return n, nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("需要整数,收到 %q", v)
		}
		return n, nil
	default:
		return nil, fmt.Errorf("需要整数,收到 %T", raw)
	}
}

// toFloat64 把任意标量转成小数
func toFloat64(raw any) (any, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int:
		return float64(v), nil
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil, fmt.Errorf("需要数字,收到 %q", v.String())
		}
		return f, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return nil, fmt.Errorf("需要数字,收到 %q", v)
		}
		return f, nil
	default:
		return nil, fmt.Errorf("需要数字,收到 %T", raw)
	}
}

// boolToInt64 开关落库用 0 / 1
//
// ⚠️ **不用 bool 直接存** —— MySQL 的 TINYINT(1) 与 SQLite 的 INTEGER
// 都能接 bool,但 go-sql-driver 在某些列类型下把它读成 []byte,
// 读回来还得再转一次。统一落 0/1,读的时候统一转回 bool,只转一次。
func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// toBool 把任意标量转成开关(落库仍是 0/1)
//
// ⚠️ **认不出来的字符串一律报错,不静默当 false** ——
// "开" / "是" / "on" 这类值如果被静默当成 false,用户存进去的是"关",
// 而且当场不报错。宁可报错让人改对。
func toBool(raw any) (any, error) {
	switch v := raw.(type) {
	case bool:
		return boolToInt64(v), nil
	case int64:
		return boolToInt64(v != 0), nil
	case int:
		return boolToInt64(v != 0), nil
	case float64:
		return boolToInt64(v != 0), nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return nil, fmt.Errorf("需要开关,收到 %q", v.String())
		}
		return boolToInt64(n != 0), nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "t", "yes", "y", "on":
			return int64(1), nil
		case "0", "false", "f", "no", "n", "off":
			return int64(0), nil
		default:
			return nil, fmt.Errorf("需要开关(true/false),收到 %q", v)
		}
	default:
		return nil, fmt.Errorf("需要开关,收到 %T", raw)
	}
}

// normalizeDate 归一化日期
//
// ⚠️ **带时间的输入按东八区折算成日期** ——
// "2026-11-01T23:00:00Z" 在东八区是 11-02 早上 7 点,日期是 11-02。
// 直接截字符串会得到 11-01(UTC 的那天),比本仓其它业务时间口径差 8 小时。
func normalizeDate(raw any) (any, error) {
	t, ok, err := parseTime(raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		return "", nil
	}
	return t.In(timeutil.AsiaShanghai).Format(dateLayout), nil
}

// normalizeDateTime 归一化日期时间(统一 UTC)
func normalizeDateTime(raw any) (any, error) {
	t, ok, err := parseTime(raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		return "", nil
	}
	return t.UTC().Format(dateTimeLayout), nil
}

// parseTime 解析时间输入
//
// 返回 (时间点, 是否解析成功, 错误):
//   - 空串 → (零值, false, nil) —— "没填"
//   - 解析不了 → (零值, false, 错误)
//
// ⚠️ **不带时区的输入按东八区解释** ——
// 用户在表单里填 "2026-11-01 08:30:00",指的是公司里的 8 点半。
// 带时区的(RFC3339 / Z)按字面解析。
func parseTime(raw any) (time.Time, bool, error) {
	switch v := raw.(type) {
	case time.Time:
		return v, true, nil
	case string:
		return parseTimeString(v)
	default:
		return time.Time{}, false, fmt.Errorf("需要日期,收到 %T", raw)
	}
}

// parseTimeString 解析时间字符串(多种格式)
func parseTimeString(s string) (time.Time, bool, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return time.Time{}, false, nil
	}
	// 带时区的先试 RFC3339 —— offset / Z 都在这两种格式里
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", dateLayout} {
		if t, err := time.ParseInLocation(layout, trimmed, timeutil.AsiaShanghai); err == nil {
			return t, true, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("需要日期或日期时间,收到 %q", s)
}

// normalizeSelect 归一化单选
//
// ⚠️ **配了选项集就必须是其中之一** ——
// 不校验的话,下拉框里选不到的值也能存进来(改个请求就写进去了),
// 列表页按选项集翻译标签时会显示成一个空白的标签。
func normalizeSelect(raw any, opts Options) (any, error) {
	text, err := toText(raw)
	if err != nil {
		return nil, err
	}
	s, _ := text.(string)
	if opts.HasChoices() && s != "" && !opts.Contains(s) {
		return nil, fmt.Errorf("%q 不是合法选项(可选:%s)", s, strings.Join(opts.Values(), ", "))
	}
	return s, nil
}

// normalizeMultiSelect 归一化多选(落库是 JSON 数组文本)
func normalizeMultiSelect(raw any, opts Options) (any, error) {
	values, err := toStringList(raw)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if opts.HasChoices() && !opts.Contains(v) {
			return nil, fmt.Errorf("%q 不是合法选项(可选:%s)", v, strings.Join(opts.Values(), ", "))
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("序列化多选值失败: %w", err)
	}
	return string(b), nil
}

// toStringList 把输入转成字符串列表
//
// 接受三种形态:字符串数组 / 单个字符串 / JSON 数组文本。
// ⚠️ **单个字符串按"一个值"处理,不是按逗号切分** ——
// 用户自己填的 "a,b,c" 是一个字面量,不是三个值。
func toStringList(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text, err := toText(item)
			if err != nil {
				return nil, err
			}
			s, _ := text.(string)
			out = append(out, s)
		}
		return out, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, nil
		}
		if strings.HasPrefix(trimmed, "[") {
			var decoded []string
			if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
				return decoded, nil
			}
		}
		return []string{v}, nil
	default:
		return nil, fmt.Errorf("需要字符串列表,收到 %T", raw)
	}
}

// normalizeJSON 归一化 JSON(落库是 JSON 文本)
//
// ⚠️ **字符串输入要校验是不是合法 JSON** ——
// 用户在表单里粘贴半截 JSON,当场报错比存进去之后查不出来强得多。
func normalizeJSON(raw any) (any, error) {
	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", nil
		}
		var probe any
		if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
			return nil, fmt.Errorf("不是合法 JSON: %w", err)
		}
		return trimmed, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("序列化 JSON 失败: %w", err)
		}
		return string(b), nil
	}
}
