// searchidx —— 加密字段的全文搜索
//
// ## 这个包解决什么问题
//
// 加密字段(手机号/姓名/身份证)没法走 DB 的 LIKE 查询;
// 盲索引(blindindex)只支持"完全相等"。
// 需要"前缀/包含/模糊"查询时,就得有 n-gram 盲索引:
//
//	主表:  sys_user(id, phone_cipher)          -- 密文
//	索引表:sys_user_phone_idx(user_id, gram)   -- n-gram 盲索引
//
// ## 关键点
//
//   - 同 secret + 同 gram → 同 token(索引能匹配上)
//   - 不同 secret → 不同 token(防跨库攻击)
//   - n 越小 → 索引越大 / 召回越高 / 误报变多;n 越大 → 反之
//   - 推荐 n=4(中文姓名/手机号);n=3 对长查询更友好
//
// ## 跑法
//
//	go run ./examples/searchidx
package main

import (
	"fmt"
	"strings"

	"rapid-admin/internal/pkg/searchidx"
)

func main() {
	secret := []byte("blind-index-secret-key-32-bytes!!")
	const n = 4

	// 一、切成 n-gram
	fmt.Println("n-gram 切分(n=4):")
	for _, s := range []string{"13800001234", "张伟", "RapidAdmin"} {
		fmt.Printf("  %-12q → %v\n", s, searchidx.Ngrams(s, n))
	}

	// 二、加密成不可逆 token
	fmt.Println()
	fmt.Println("盲索引 token(同 secret + 同 gram → 同 token):")
	grams := searchidx.BlindNgrams(secret, "13800001234", n)
	for i, g := range grams {
		fmt.Printf("  %-12s → %s\n", searchidx.Ngrams("13800001234", n)[i], g)
	}

	// 三、模拟索引表 + 查询
	//
	// 真实场景:grams 批量写索引表;查询时把 query 切好加密,
	// 用 `WHERE gram IN (...) GROUP BY user_id HAVING COUNT(DISTINCT gram) >= k`
	// 聚合出候选,再回主表拿密文解密。
	fmt.Println()
	index := map[string][]string{} // gram → user_id 列表
	records := map[string]string{} // user_id → 明文
	add := func(userID, plain string) {
		records[userID] = plain
		for _, g := range searchidx.BlindNgrams(secret, plain, n) {
			index[g] = append(index[g], userID)
		}
	}
	add("u1", "13800001234")
	add("u2", "13812345678")
	add("u3", "13900002222")
	add("u4", "张伟")

	query := func(q string, ratio float64) {
		tokens := searchidx.SearchTokens(secret, q, n)
		hits := map[string]int{}
		for _, t := range tokens {
			for _, uid := range index[t] {
				hits[uid]++
			}
		}
		k := searchidx.MinMatchTokens(len(tokens), ratio)
		fmt.Printf("  查询 %-14q tokens=%d 需要命中≥%d →", q, len(tokens), k)
		found := false
		for uid, h := range hits {
			if searchidx.MatchAtLeast(h, len(tokens), k) {
				fmt.Printf("  %s(%s)", uid, records[uid])
				found = true
			}
		}
		if !found {
			fmt.Printf("  (无结果)")
		}
		fmt.Println()
	}

	fmt.Println("查询:")
	query("13800001234", 1.0) // 全命中
	query("1380", 0.0)        // 至少 1 个命中 → 前缀查询
	query("1390", 0.0)        // 前缀不匹配任何记录
	query("张伟", 1.0)

	// ⚠️ **query 的长度必须 ≥ n**
	//
	// 比 n 短时,SearchTokens 会把整串当成 1 个 token,
	// 而索引里存的是 n-gram —— 两边对不上,查不到。
	// 想支持更短的 query,建索引时就要用更小的 n(代价是索引变大)。
	fmt.Println()
	short := searchidx.SearchTokens(secret, "138", n)
	fmt.Printf("query \"138\"(3 字符 < n=%d)→ token 数 %d,是整串而不是 n-gram\n", n, len(short))

	// 四、换 secret ⇒ token 全变 ⇒ 查不到(这就是"防跨库攻击")
	fmt.Println()
	other := []byte("another-secret-key-32-bytes-xxxx")
	fmt.Println("换 secret 后:")
	fmt.Printf("  同 gram 不同 secret → %s vs %s\n",
		searchidx.BlindNgrams(secret, "1380", n)[0],
		searchidx.BlindNgrams(other, "1380", n)[0])
	fmt.Println("  ", strings.Repeat("·", 40), "索引表对不上 ⇒ 查不到")
}
