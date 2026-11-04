// signx —— HMAC 接口签名
//
// ## 这个包解决什么问题
//
// Bearer token 只能证明"你是谁",证明不了"这次请求没被改"。
// 签名把 method/path/时间戳/请求体绑在一起:中间件在**写操作**上校验,
// 篡改任何一项都会失配。
//
// ## 签名构造
//
//	拼接串 = METHOD + "\n" + PATH + "\n" + TS + "\n" + sha256(BODY)
//	sig    = hex(HMAC-SHA256(secret, 拼接串))
//
// 客户端 header:
//
//	X-Signature: t=<unix_seconds>,sig=<hex>
//
// ## 关键点
//
//   - body 取 sha256:防"空 body 替换"和"大 body 拼接攻击"
//   - 比较用 subtle.ConstantTimeCompare:防计时攻击
//   - ts 校验(默认 5 分钟窗口)是**防重放**,由中间件做,signx 只验签
//
// ## 跑法
//
//	go run ./examples/signx
package main

import (
	"fmt"
	"strconv"
	"time"

	"rapid-admin/internal/pkg/signx"
)

func main() {
	secret := []byte("s3cr3t-shared-key")
	now := time.Now().Unix()
	ts := strconv.FormatInt(now, 10)

	body := []byte(`{"phone":"13800001234","scene":"login"}`)

	// 一、客户端:算签名
	sig := signx.Sign(secret, "POST", "/rapid-admin/v1/auth/sms-code", ts, body)
	header := "t=" + ts + ",sig=" + sig
	fmt.Println("请求头 X-Signature:", header)

	// 二、中间件:拆头 + 验签
	gotTS, gotSig, ok := signx.ParseHeader(header)
	if !ok {
		fmt.Println("header 格式不合法")
		return
	}
	fmt.Println("解析出的 ts =", gotTS, " sig =", gotSig[:16], "...")

	// 三、时间戳窗口:超出 5 分钟就拒(防重放)
	const skew = 300 // 秒
	fmt.Println()
	fmt.Println("时间戳有效性:")
	fmt.Println("  当前时间      =", signx.ValidTS(gotTS, now, skew))
	fmt.Println("  5 分钟前      =", signx.ValidTS(strconv.FormatInt(now-301, 10), now, skew))
	fmt.Println("  5 分钟后(未来)= ", signx.ValidTS(strconv.FormatInt(now+301, 10), now, skew))

	// 四、验签:改任何一个字段都失配
	fmt.Println()
	fmt.Println("验签(全部应通过):")
	fmt.Println("  原样          =", signx.Verify(secret, "POST", "/rapid-admin/v1/auth/sms-code", gotTS, body, gotSig))
	fmt.Println("  改 body       =", signx.Verify(secret, "POST", "/rapid-admin/v1/auth/sms-code", gotTS, []byte(`{"phone":"13800009999"}`), gotSig))
	fmt.Println("  改 path       =", signx.Verify(secret, "POST", "/rapid-admin/v1/auth/login", gotTS, body, gotSig))
	fmt.Println("  改 method     =", signx.Verify(secret, "GET", "/rapid-admin/v1/auth/sms-code", gotTS, body, gotSig))
	fmt.Println("  改 secret     =", signx.Verify([]byte("wrong-key"), "POST", "/rapid-admin/v1/auth/sms-code", gotTS, body, gotSig))
}
