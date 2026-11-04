// Package searchidx 加密字段全文搜索原语(§1.5 #9 P2)
//
// 问题:
//   - 加密字段(手机号/姓名/身份证)无法走 DB LIKE 查询
//   - 盲索引(blindindex)只支持"完全相等"查询
//   - 需要"模糊/前缀/包含"查询时无路可走
//
// 方案:n-gram 盲索引(也叫 "deterministic encryption with substring search")
//   - 把文本切成重叠的 n-gram 片段(默认 4 字符)
//   - 每个 n-gram 用 HMAC-SHA256 加密存到 side table
//   - 搜索时:把 query 也切成 n-gram,查"至少 k 个 token 命中"的记录
//
// 字段 schema 示例(以 phone 字段为例):
//
//	主表:sys_user(id, phone_cipher)              -- 加密存
//	索引表:sys_user_phone_idx(user_id, gram)     -- n-gram 盲索引
//	  PRIMARY KEY (user_id, gram)
//	  INDEX (gram)
//
// 写入流程:
//   1. cipher = crypto.Encrypt(phone)
//   2. grams = searchidx.BlindNgrams(secret, phone, 4)
//   3. 主表写 cipher;索引表 bulk insert grams
//
// 查询流程("138 开头"):
//   1. query = "138"  → 切成 grams = ["138", "138 ", "138 1"...] (不足 n 时取原串)
//   2. SELECT user_id FROM sys_user_phone_idx WHERE gram IN (...grams)
//      GROUP BY user_id HAVING COUNT(DISTINCT gram) >= k   (k=全部 grams 命中数,或 ≥ 1)
//   3. 再 join 主表拿 cipher 解密
//
// 性能与安全 trade-off:
//   - n 越小 → 索引越大 / 召回越高 / 误报可能
//   - n 越大 → 索引越小 / 召回越低 / 误报少
//   - 推荐 n=4 中文姓名/手机号;n=3 长查询更友好
//
// 用法:
//
//	// 索引
//	grams := searchidx.BlindNgrams(secret, plain, 4)
//	db.Create(&[]PhoneIdx{...})  // 批量插入
//
//	// 查询
//	tokens := searchidx.SearchTokens(secret, query, 4)
//	db.Where("gram IN ?", tokens).Group("user_id").Having("COUNT >= ?", len(tokens))
package searchidx

import (
	"crypto/hmac"
	"crypto/sha256"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultN 默认 n-gram 大小(中文姓名/手机号场景 4 较平衡)
	DefaultN = 4
)

// Ngrams 把 s 切成重叠的 n-gram 列表
//   - 短串(< n):整体作为一个 token
//   - 长串:滑动窗口产出 s[:n], s[1:n+1], ... , s[len-n:]
//   - 跳过空白段(连续空白只切一次)
//   - unicode 字符按 rune 算
func Ngrams(s string, n int) []string {
	if n < 1 {
		n = DefaultN
	}
	// 预 trim + collapse 内部连续空白(避免 "abc   def" 切成 "c  " 之类碎片)
	cleaned := collapseSpaces(s)
	if cleaned == "" {
		return nil
	}
	runes := []rune(cleaned)
	if len(runes) <= n {
		// 短串整体作为一个 token
		return []string{string(runes)}
	}
	out := make([]string, 0, len(runes)-n+1)
	for i := 0; i+n <= len(runes); i++ {
		out = append(out, string(runes[i:i+n]))
	}
	return out
}

// BlindNgrams 把 n-gram 用 HMAC-SHA256(secret) 加密成不可逆 token
//   - 同 secret + 同 gram → 同 token(用于索引匹配)
//   - 不同 secret → 不同 token(防跨库攻击)
//   - 同 secret + 不同 gram → 不同 token(防碰撞)
func BlindNgrams(secret []byte, s string, n int) []string {
	grams := Ngrams(s, n)
	if len(grams) == 0 {
		return nil
	}
	out := make([]string, len(grams))
	for i, g := range grams {
		out[i] = blindOne(secret, g)
	}
	return out
}

// SearchTokens 把 query 切成 n-gram 并加密(返索引表能直接 IN 的 token 列表)
//
//	query 比 n 短时仍可查(整串当 1 个 token,匹配模式:精确包含 query)
func SearchTokens(secret []byte, query string, n int) []string {
	return BlindNgrams(secret, query, n)
}

// MatchAtLeast 候选 hits 至少满足 k 个 query token 命中
//
//	hits: 每条候选记录"实际索引命中"的 token 数
//	k:    最少需要几个 token 命中(常用 len(queryTokens) 即"全部命中")
//
// 用法:
//
//	tokens := searchidx.SearchTokens(secret, "13800001234", 4)
//	hits := db.Raw("SELECT user_id, COUNT(DISTINCT gram) AS hit FROM ... WHERE gram IN ? GROUP BY user_id", tokens)
//	// hits 是 [{user_id, hit}, ...]
//	for _, h := range hits {
//	    if searchidx.MatchAtLeast(h.hit, len(tokens), 0) { ... }  // k=0 表示"至少 1 个命中"
//	}
func MatchAtLeast(actualHits, queryTokenCount int, k int) bool {
	if k == 0 {
		return actualHits >= 1
	}
	if k < 0 {
		k = queryTokenCount // 默认全部命中
	}
	return actualHits >= k
}

// MinMatchTokens 计算"至少需要的命中 token 数"默认值
//   - k 比例 < 1 → 至少 N*ratio 个命中
//   - k 比例 = 1 → 全部命中(默认)
//   - k 比例 = 0 → 至少 1 个命中
func MinMatchTokens(queryTokenCount int, ratio float64) int {
	if ratio <= 0 {
		return 1
	}
	if ratio >= 1 {
		return queryTokenCount
	}
	return int(float64(queryTokenCount) * ratio)
}

/* ====================== 内部 ====================== */

func blindOne(secret []byte, s string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(s))
	return hexEncode(mac.Sum(nil))
}

func hexEncode(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hex[c>>4]
		out[i*2+1] = hex[c&0x0f]
	}
	return string(out)
}

// collapseSpaces 压缩连续空白为单空格 + trim(只切 ASCII 空格)
//   - 保留 utf8 rune 完整性
func collapseSpaces(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if r == ' ' {
			if !lastSpace {
				b.WriteRune(r)
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	_ = utf8.RuneError // 占位:保留 utf8 语义(实际 Ngrams 按 rune 处理)
	return b.String()
}
