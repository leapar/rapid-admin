package kvstore

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// RandomID 生成 n 字节十六进制随机串。
//
// 与 middleware.RandomID 同实现,分开是因为 middleware 依赖 kvstore
// (限流要用它),反向 import 会成环。两份都只有 10 行,不值得为它抽一个
// 只装这一个函数的公共包;如果第三个地方需要,再合并。
func RandomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 退化方案:用纳秒时钟拼,精度足够(同毫秒两次调用靠它区分 ZSET member)
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now & 0xff)
			now >>= 4
		}
	}
	return hex.EncodeToString(b)
}