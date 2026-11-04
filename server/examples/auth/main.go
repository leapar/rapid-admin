// auth —— JWT 签发/解析/吊销 + 密码哈希
//
// ## 这个包解决什么问题
//
// 三件事放在一个包里,因为它们共享同一份"信任链":
//
//   - JWT 签发与解析(三受众独立 secret:admin / owner / supplier)
//   - 令牌吊销(登出、改密码、切换身份时让令牌失效)
//   - 密码哈希与强度校验(bcrypt)
//
// ## 关键点
//
//   - **typ 声明**:access / refresh / short 三类令牌互不通用。
//     少了这一条,refresh token 能当 access token 用,有效期还长 90 倍。
//   - **sid 会话绑定**:一次登录生成一个 sid,access 和 refresh 共用。
//     登出时服务端只拿得到 access 的 jti,拿不到 refresh 的 ——
//     sid 让"登出"能一次结束整条会话(换了几轮 refresh 也不怕)。
//   - **吊销存储永不返回 nil**:没配 Redis 时给内存实现。
//     以前是"Redis 没配就整条链路静默短路",表现是登出后 token 照样能用。
//   - **失败方向是 fail-open**:存储故障时放行,不踢人。
//     反过来(故障即全员登出)是更糟的故障类别。
//
// ## 跑法
//
//	go run ./examples/auth
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/auth"
)

func main() {
	ctx := context.Background()

	// ── 一、密码哈希 ──────────────────────────────────────────────
	fmt.Println("=== 密码 ===")
	plain := "Abcd1234!@"
	fmt.Printf("强度校验 Abcd1234!@   = %v\n", auth.VerifyPasswordStrength(plain))
	fmt.Printf("强度校验 abc          = %v\n", auth.VerifyPasswordStrength("abc"))

	hash, err := auth.HashPassword(plain)
	if err != nil {
		fmt.Println("哈希失败:", err)
		return
	}
	fmt.Printf("哈希前 40 位          = %s...\n", hash[:40])
	fmt.Printf("校验正确密码          = %v\n", auth.CheckPassword(hash, plain))
	fmt.Printf("校验错误密码          = %v\n", auth.CheckPassword(hash, "Abcd1234!#"))
	fmt.Println("bcrypt 每次哈希的盐都不同 ⇒ 同一个密码两次哈希结果不一样")

	// ── 二、单受众的 JWT 管理器 ──────────────────────────────────
	fmt.Println()
	fmt.Println("=== 单受众 JWT ===")
	mgr := auth.NewManager(
		"admin-secret-at-least-32-bytes-long!!",
		"rapid-admin",
		8*time.Hour,    // access TTL
		720*time.Hour,  // refresh TTL(30 天)
		15*time.Minute, // short TTL(短令牌)
		auth.AudienceAdmin,
	)

	claims := &auth.Claims{
		UserID:     1001,
		IdentityID: 2001,
		Username:   "zhangwei",
		RoleCode:   "role_project_manager",
		ProjectIDs: []uint64{7, 9},
		SessionID:  auth.NewSessionID(),
	}
	fmt.Printf("会话 sid = %s\n", claims.SessionID)

	access, err := mgr.SignAccess(claims)
	if err != nil {
		fmt.Println("签发失败:", err)
		return
	}
	fmt.Printf("access token 前 40 位 = %s...\n", access[:40])

	parsed, err := mgr.ParseAccess(access)
	if err != nil {
		fmt.Println("解析失败:", err)
		return
	}
	fmt.Printf("解析回来: uid=%d iid=%d usr=%s role=%s pj=%v\n",
		parsed.UserID, parsed.IdentityID, parsed.Username, parsed.RoleCode, parsed.ProjectIDs)
	fmt.Printf("jti = %s\n", parsed.JTI())

	// ── 三、三类令牌互不通用 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 令牌类型互拒 ===")
	_, err = mgr.ParseRefresh(access)
	fmt.Printf("把 access 当 refresh 解析 = %v\n", err != nil)
	fmt.Printf("错误类型 = %v\n", errors.Is(err, auth.ErrTokenTypeMismatch))

	refresh, err := mgr.SignRefresh(claims)
	if err != nil {
		fmt.Println("签发 refresh 失败:", err)
		return
	}
	_, err = mgr.ParseAccess(refresh)
	fmt.Printf("把 refresh 当 access 解析 = %v\n", err != nil)

	// ── 四、三受众隔离 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 三受众隔离 ===")
	multi := auth.NewMulti(
		"admin-secret-at-least-32-bytes-long!!",
		"owner-secret-at-least-32-bytes-long!!",
		"supplier-secret-at-least-32-bytes-long!!!",
		"rapid-admin",
		8*time.Hour, 720*time.Hour, 15*time.Minute,
	)
	adminToken, err := multi.SignByAudience(auth.AudienceAdmin, claims)
	if err != nil {
		fmt.Println("签发失败:", err)
		return
	}
	ownerMgr, _ := multi.Get(auth.AudienceOwner)
	_, err = ownerMgr.ParseAccess(adminToken)
	fmt.Printf("admin 的 token 给 owner 的 manager 解析 = %v(secret 不同 ⇒ 验签失败)\n", err != nil)

	anyClaims, err := multi.ParseAnyAccess(adminToken)
	fmt.Printf("Multi.ParseAnyAccess 能认出来 = %v(aud=%s)\n", err == nil, anyClaims.Audience())

	// ── 五、吊销:登出一次,整条会话死 ──────────────────────────────
	fmt.Println()
	fmt.Println("=== 吊销 ===")
	store := auth.NewMemoryStore()
	auth.SetDefaultRevocation(store)

	fmt.Printf("吊销前 IsSessionRevoked = %v\n", mustBool(auth.IsSessionRevoked(ctx, claims.SessionID)))
	if err := auth.RevokeSession(ctx, claims.SessionID, 720*time.Hour); err != nil {
		fmt.Println("吊销失败:", err)
		return
	}
	fmt.Printf("吊销后 IsSessionRevoked = %v\n", mustBool(auth.IsSessionRevoked(ctx, claims.SessionID)))

	// 逐 jti 吊销
	_ = store.Revoke(ctx, parsed.JTI(), time.Hour)
	revoked, _ := store.IsRevoked(ctx, parsed.JTI())
	fmt.Printf("逐 jti 吊销后 IsRevoked = %v\n", revoked)

	// ── 六、强制下线作废线 ────────────────────────────────────────
	//
	// "把某个用户的全部在途令牌一次性作废"的做法:在存储里给他记一条
	// **作废线**(一个时间点),之后签发的令牌都在线之后。
	// 判定式是 `iat + skew < 作废线` —— 有 60s 宽限,是为了多实例时钟不同步时
	// 不至于让"刚改完密码、重新登录拿到的 token"当场被判失效。
	fmt.Println()
	fmt.Println("=== 强制下线作废线 ===")
	line := time.Now()
	if err := store.InvalidateFrom(ctx, claims.UserID, line); err != nil {
		fmt.Println("推作废线失败:", err)
		return
	}
	read, _ := store.InvalidateBefore(ctx, claims.UserID)
	fmt.Printf("作废线 = %s\n", read.Format(time.RFC3339))

	old := read.Add(-10 * time.Minute) // 改密码之前签发的
	fresh := read.Add(time.Minute)     // 改密码之后签发的
	fmt.Printf("作废线之前签发的令牌已失效 = %v\n",
		auth.TokenInvalidated(old, read, auth.ClockSkewGrace))
	fmt.Printf("作废线之后签发的令牌仍有效 = %v\n",
		auth.TokenInvalidated(fresh, read, auth.ClockSkewGrace))
	fmt.Printf("作废线前 30 秒签发的(在 60s 宽限内)= %v\n",
		auth.TokenInvalidated(read.Add(-30*time.Second), read, auth.ClockSkewGrace))
}

// mustBool 把 (bool, error) 压成一个 bool,示例里只关心结果
func mustBool(b bool, err error) bool {
	if err != nil {
		return false
	}
	return b
}
