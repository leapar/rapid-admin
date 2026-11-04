// 会话绑定(sid)—— 让"登出"能同时结束这条会话的 access + refresh 两张 token。
//
// # 要解决的问题
//
// `Logout` 只拿得到 **access token** 的 jti(它是从 Authorization 头里那张
// access token 解析出来的),拿不到同一会话里那张 **refresh token** 的 jti。
// 于是登出只 revoke 了 access 的 jti,refresh 纹丝不动 —— 用户点了"退出登录",
// 那张 refresh 还能继续换发新 access,最长活 720h(30 天)。
//
// 表现为:点了退出,过一会儿(或换个浏览器)又登进去了;更糟的是,攻击者手里
// 那张 refresh 在受害者"已登出"之后照样能用。
//
// 这不是"登出没做",是"登出只做了一半",而另一半没人看得见。
//
// # 为什么用 sid 而不是别的办法
//
// 试过几种,都不成立:
//
//   - **登出时把用户的 refresh 全吊了**:做不到 —— 不知道哪些 jti 是他的,
//     要知道就得有索引(见下面"会话登记簿")。
//   - **登出时推作废线**:能杀掉所有会话,但那是**全有或全无**的
//     (见 invalidate.go)—— 用户在手机上退出,电脑上的会话也一起没了。
//     service 层有两条否定断言(TestLogout_DoesNotPushInvalidationLine)
//     就是专门钉住这件事的,不能拿它来顶数。
//   - **把 refresh 的 jti 塞进 access token**:轮换一次就断一次
//     (新 access 对应新 refresh),而且 access token 里带 refresh 的标识
//     没有额外收益。sid 是同一件事的稳定版本。
//
// sid 是**会话 id**:一次登录生成一次,该会话的 access / refresh 两张 token
// 共用同一个;refresh 轮换不换(还是同一条会话)。
//
// ⚠️ **M148(2026-11-01):切换身份要换 sid** —— 换身份时旧 sid 被吊销,
// 新 sid 随新 token 一起发出去。
//
// 以前是"切换身份也不换",理由是"换的是同一台设备上的身份,不是新会话"。
// 那个理由**只考虑了登出**:切换时服务端只拿得到 **access** 的 jti,
// 拿不到切换**之前**那张 **refresh** 的 jti ⇒ 吊销不了它 ⇒
// 它还能换发一份**旧身份**的 access(jti 没吊 / sid 没变 / 作废线没推,
// 三道闸全过)。
//
// ⇒ 危害:用户从高危身份切到低危身份(超管 → 普通用户)之后,手里那张旧
// refresh **还能继续拿超管**,最长 720h(30 天)。
// "切身份"这个动作在安全边界上等于什么也没做。
//
// 修法就是**换 sid**:sid 是会话级判据,refresh 续期时必查
// (RefreshToken 第 2.6 步)⇒ 旧 sid 一吊,那条会话的 access + refresh
// 一起死,**不管中间轮换过几轮**(轮换只换 jti,sid 不变)。
//
// 换 sid 之后,登记簿(sys_login_log)那一行的 session_id 也跟着换 ——
// 否则"活跃设备列表 / 强制下线 / 登出写 logout_at"三处会一起失灵
// (见 service 层 SwitchIdentity 的 rotateSession 第 3 步)。
//
// # 关键性质:一张老 token 也能把整条会话结束掉
//
// 轮换会让 access 的 jti 一换再换。如果登出时只能按"当前 access 的 jti"去
// 找 refresh,那用户在 A 标签页登出、B 标签页(拿着轮换前的旧 access)继续
// 续期,就绕过去了。
//
// sid 是**会话级**的:不管 access 换了几轮,那条会话的 sid 始终不变。
// 任何一张属于这条会话的 token 都能定位到它,于是登出能一次把整条会话结束掉。
//
// # 降级:没有 sid 的老 token
//
// sid 是本轮新增的 claim,本轮上线前签发的 token 没有它(omitempty)。
// 那种 token 登出时只能退回到"只吊销 access 的 jti" —— 也就是本轮之前的
// 行为,**不是新引入的缺陷**。
//
// 残留窗口:本轮上线前签发、且在登录前被截获的 refresh token,在受害者登出后
// 仍可换发一次(它随后会被轮换掉,不会变成长期有效的后门)。
// 这个窗口随"上线前签发的 token 全部自然过期"而关闭(最长 720h),
// 不需要为它做强制下线 —— 强制下线会把该用户的所有会话一起踢掉,
// 那正是 invalidate.go 明确反对的做法。
//
// 缓解:refresh 时**绑定 sid**(见 service 层 RefreshToken)—— 没有 sid 的
// 老 refresh 在被用的那一刻就会拿到一个 sid,从此这条会话就有了绑定。
// 所以窗口只覆盖"上线前签发、且一次都没被用过"的那些 token。
package auth

import (
	"context"
	"time"

	"rapid-admin/internal/pkg/randomx"
)

// sessionIDBytes 会话 id 的随机字节数。16 字节 = 128 bit。
//
// 与 jti 同量级:会话 id 的作用是"把两张 token 绑在一起",猜中它并不能
// 伪造任何 token(签名还在),但撞了就会把两个用户的会话混成一条 ——
// 那等于让 A 能登出 B 的会话。128 bit 下这个概率可以忽略。
const sessionIDBytes = 16

// NewSessionID 生成一个新的会话 id。
func NewSessionID() string {
	return randomx.Token(sessionIDBytes)
}

// sessionRevokePrefix 会话吊销键的前缀。
const sessionRevokePrefix = "sid:"

// SessionRevokeKey 返回会话在吊销存储里的键。
//
// # 为什么加前缀
//
// 吊销存储是一张**扁平的字符串键集合**(Redis 那边是 `auth:bl:<key>`),
// jti 和会话 id 住在同一张表里。jti 来自 randomx.Token(URL-safe base64,
// 不含冒号),sid 同理 —— 但"当前两种键不撞"只是巧合,不是保证。
// 一旦将来有第三套键空间也往这张表里塞东西,就没有任何东西能保证不撞。
// 前缀把两套键空间隔开,撞不了。
func SessionRevokeKey(sid string) string {
	return sessionRevokePrefix + sid
}

// RevokeSession 吊销整条会话 —— 该会话的 access 与 refresh 一起失效。
//
// ttl 用**会话里寿命最长的那张 token** 的剩余寿命(通常是 refresh 的 720h):
// 短了就会出现"吊销记录先过期、token 还在" —— 那等于没吊。
func RevokeSession(ctx context.Context, sid string, ttl time.Duration) error {
	if sid == "" {
		return nil
	}
	return DefaultRevocation().Revoke(ctx, SessionRevokeKey(sid), ttl)
}

// IsSessionRevoked 查询会话是否已被吊销。
//
// 出错时返回 (false, err):调用点必须自己决定降级取向,不要把 err 当成
// "已吊销"(那会把所有存储故障变成全员登出),也不要把 err 当成"没被吊销"
// (那会放行已登出的会话)。与 RevocationStore 上那两个方法的约定一致。
func IsSessionRevoked(ctx context.Context, sid string) (bool, error) {
	if sid == "" {
		return false, nil
	}
	return DefaultRevocation().IsRevoked(ctx, SessionRevokeKey(sid))
}
