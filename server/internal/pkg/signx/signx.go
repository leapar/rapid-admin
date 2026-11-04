// Package signx HMAC 接口签名原语(§1.3 #20 P2)
//
// 目的:
//   - 提供 "防止请求被重放/篡改" 的签名 + 校验能力
//   - 与 middleware/sign 配合:中间件在写操作上自动校验 header
//   - 算法固定为 HMAC-SHA256(与 pkg/crypto/blindindex 一致,复用 hmac/sha256 习惯)
//
// 签名构造:
//   - 拼接串 = METHOD + "\n" + PATH + "\n" + TS + "\n" + sha256(BODY)
//   - sig = hex(HMAC-SHA256(secret, 拼接串))
//
// 客户端 header 格式:
//
//	X-Signature: t=<unix_seconds>,sig=<hex>
//
// 校验规则(中间件统一处理,signx 只验签):
//   - 头格式不合法 → 拒绝
//   - 当前时间 - ts > 5 分钟 → 拒绝(防重放)
//   - sig 字节串不匹配 → 拒绝
//
// 设计要点:
//   - 拼接串用 "\n" 分隔(路径不出现 \n,合规)
//   - BODY 用 sha256 防"空 body 替换" + 防"大 body 拼接攻击"
//   - 全部字节比较用 subtle.ConstantTimeCompare 防计时攻击
package signx

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"strconv"
	"strings"
)

// Sign 计算签名(返回 hex 字符串)
//
//	method: "GET" / "POST" / ...
//	path:   "/rapid-admin/v1/biz/owner" (含前导 /,不含 query)
//	ts:     unix seconds(string,中间件从 header 拿)
//	body:   raw request body(可能为 nil)
func Sign(secret []byte, method, path, ts string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(method))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(path))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(ts))
	mac.Write([]byte{'\n'})
	mac.Write(bodyHash(body))
	return hexEncode(mac.Sum(nil))
}

// Verify 校验签名(常量时间比较)
//
//	secret: 服务端共享密钥
//	method/path/ts/body: 同 Sign 入参
//	got:    客户端传入的 hex sig
//
// 返 true 表示签名匹配(不含 ts 校验、ts 校验由中间件做)
func Verify(secret []byte, method, path, ts string, body []byte, got string) bool {
	want := Sign(secret, method, path, ts, body)
	if len(want) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// ParseHeader 解析 X-Signature: t=<ts>,sig=<hex>
//
//	返 false → header 格式不合法
func ParseHeader(h string) (ts string, sig string, ok bool) {
	if h == "" {
		return "", "", false
	}
	// 形式 1: "t=123,sig=abc..."
	// 形式 2: "ts=123,signature=abc..."
	for _, part := range strings.Split(h, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		switch strings.ToLower(k) {
		case "t", "ts", "timestamp":
			ts = v
		case "sig", "signature":
			sig = v
		}
	}
	if ts == "" || sig == "" {
		return "", "", false
	}
	return ts, sig, true
}

// ValidTS 校验 ts 是否在 [now - skew, now + skew] 区间内(unix seconds)
//
//	skew 单位:秒,推荐 300(5 分钟)
func ValidTS(ts string, nowUnix int64, skewSec int64) bool {
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	diff := nowUnix - n
	if diff < 0 {
		diff = -diff
	}
	return diff <= skewSec
}

/* ====================== 内部 helpers ====================== */

func bodyHash(body []byte) []byte {
	h := sha256.Sum256(body)
	return h[:]
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
