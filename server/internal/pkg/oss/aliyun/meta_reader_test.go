// M133 小元数据回读(oss.MetaReader)—— aliyun provider
//
// ⚠️ **真 OSS 的往返没法在单测里测**(要真 bucket、真 AK/SK),所以这里只测
// "**在联网之前就能判掉的那一段**" —— key 校验。
//
// 而这一段恰恰是最该测的:OSS 上 `../` 只是三个普通字符,key 校验是
// 唯一拦住"把 key 拼进 URL 时拼出不属于这个桶的路径"的东西。
// 少了它,一次 Get("../..") 就会变成一次针对别的路径的真实请求。
package aliyun

import (
	"context"
	"errors"
	"testing"

	"rapid-admin/internal/pkg/oss"
)

// 特征:**非法 key 必须在发请求之前就拒**。
//
// ⚠️ newTestProvider 造的是**不联网**的 Provider(见 m78_test.go 的注释),
// 所以这条用例能跑通就说明 Get 根本没走到 GetObject —— 如果哪天
// 有人把 SafeKey 检查挪到 GetObject 之后,这里会开始尝试联网并失败。
func TestGet_RejectsUnsafeKeyBeforeNetwork(t *testing.T) {
	p := newTestProvider("ak", "sk")

	for _, key := range []string{
		"",              // 空 key
		"../etc/passwd", // 目录穿越
		"res//a",        // 空段
		"res/a/",        // 尾斜杠
		"res\\a",        // 反斜杠
		"res/a/\x00b",   // 控制字符
	} {
		_, err := p.Get(context.Background(), key)
		if err == nil {
			t.Fatalf("非法 key %q 必须被拒", key)
		}
		var e *oss.ErrInvalidKey
		if !errors.As(err, &e) {
			t.Fatalf("key %q 必须报 ErrInvalidKey,got %v", key, err)
		}
	}
}
