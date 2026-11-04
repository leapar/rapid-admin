// randomx —— 安全随机 token 生成
//
// ## 这个包解决什么问题
//
// 统一替代散落在业务代码里的 `fmt.Sprintf("%d", rand.Int())` 和
// `uuid.New().String()`。两个都要换掉:
//
//   - `math/rand` 不是加密安全的,seed 可见 ⇒ 能被预测
//   - `uuid` 固定 36 字符且含连字符,做文件名/URL 参数不好看
//
// ## 关键点
//
//   - 全部走 crypto/rand(熵源来自 OS,线程安全)
//   - 字符集都是 URL-safe(无 + / = 等需要转义的字符)
//   - 长度按**字符数**算,不是字节数(内部按 6 bit/字符编码)
//
// ## 跑法
//
//	go run ./examples/randomx
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/randomx"
)

func main() {
	// 一、通用 token:URL-safe,适合 header / cookie / 上传 ID
	fmt.Println("Token(32)      =", randomx.Token(32))
	fmt.Println("String()       =", randomx.String())

	// 二、短信验证码:纯数字,允许前导 0
	fmt.Println("NumericCode(6) =", randomx.NumericCode(6))

	// 三、邀请码:排除易混字符(0/O/Q、1/I/L),人眼能读
	fmt.Println("InviteCode(8)  =", randomx.InviteCode(8))

	// 四、密钥/盐:hex 编码,长度按字节
	fmt.Println("Hex(16)        =", randomx.Hex(16), "(16 字节 = 32 字符 = 128 bit)")

	// 五、熵估算 —— 决定"该发多长的验证码"时用这个
	fmt.Println()
	for _, n := range []int{6, 8, 16, 32} {
		e := randomx.Entropy(n, len(randomx.CharsetURLSafe))
		fmt.Printf("%2d 字符 URL-safe ≈ %.1f bit 熵\n", n, e)
	}
	fmt.Printf("%2d 字符纯数字   ≈ %.1f bit 熵\n", 6, randomx.Entropy(6, len(randomx.CharsetNumeric)))
}
