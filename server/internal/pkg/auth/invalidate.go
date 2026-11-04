// 强制下线原语 —— 让某个用户的**全部**在途令牌一次性作废。
//
// # 要解决的问题
//
// 逐 jti 吊销只能处理"眼前这一张":登出、改密码都只 revoke caller.JTI。
// 于是同一个用户的其它会话(另一台设备、另一个浏览器、以及**盗号者**)
// 手里的 token 照旧有效,最长还能活 refresh_ttl(720h = 30 天)。
//
// 改密码 / 重置密码是最要命的那个:用户发现自己被盗后做的第一件事就是改密码,
// 而改密码只踢掉"当前这张",盗号者的会话纹丝不动。事故响应形同虚设。
//
// # 为什么不在 JWT 里放一个 token_epoch 计数器
//
// 想过,而且是对的方向(见本轮设计过程),但最后选了"签发时间 vs 作废线",理由是
// **失败方向**。用计数器 + `Claims.Epoch` 时:
//
//	MemoryStore 重启 → epoch 归零 → 所有存量令牌的 ep=3 ≠ 0
//	→ **全部合法用户被踢下线**,而且每次重启都重来一遍。
//
// 用时间戳线:
//
//	MemoryStore 重启 → 标记丢失 → 判定为"从未作废" → 放行
//	→ 退化方向是 fail-open(作废记录丢了一次),**不是 fail-closed(全员被踢)。
//
// 前者是一个此前**不存在**的故障类别(重启踢人),而且会立刻被开发发现、变成阻塞;
// 后者与吊销表本身的既有降级完全一致(revocation.go 里写着的"重启会复活所有
// 本该失效的 token"),不是新引入的行为。选已知失败方向,不选新的。
//
// 附带好处:**JWT 一个字节都不用改**。`iat` 是 sign() 本来就写的标准 claim,
// 于是没有 W3 那种"老 token 全部作废、全员重登"的破坏性变更,也没有
// Claims.Fork() 漏拷字段的风险(那个坑 W7 踩过一次)。
package auth

import (
	"context"
	"time"
)

// ClockSkewGrace 时钟偏移宽限。
//
// 判定式是 `iat + skew < 作废线`,而不是 `iat < 作废线`。差别在于:多实例部署时
// 各机器时钟不完全同步,而"改密码"和"重新登录拿新 token"可能分别落到两台机器上。
// 若偏移 2 秒,严格比较会让用户**刚改完密码、重新登录拿到的 token 当场被判失效**,
// 进而在客户端表现成"改完密码就再也登不进去"的死循环。
//
// 60s 宽限对这个机制是无害的:它只影响"作废线前后 60 秒内签发的 token",
// 而真正需要被作废的存量 token 早就远超这个窗口。
const ClockSkewGrace = 60 * time.Second

// TokenInvalidated 判断"这张令牌是否落在该用户的作废线之前"。
//
// 参数:
//   - issuedAt: 令牌的 iat(签发时刻)
//   - before:   该用户的作废线(零值 = 从未作废过 → 全部有效)
//   - skew:     时钟偏移宽限,见 ClockSkewGrace
//
// 纯函数、不碰存储:边界判定(正好等于 / 差 1ns / 差 1 秒)必须能直接测,
// 不用去构造真实的 token 再等时间。
func TokenInvalidated(issuedAt, before time.Time, skew time.Duration) bool {
	// 从未作废过 —— 最常见的情况,先短路。
	if before.IsZero() {
		return false
	}
	return issuedAt.Add(skew).Before(before)
}

// SessionInvalidated 判断这张令牌是否已被"强制下线"作废 —— 两个校验点的唯一入口。
//
// 之所以包一层而不是让两处调用点各写两行:取线 + 判定 + 降级取向是一组**必须
// 绑在一起**的决定。中间件和 RefreshToken 各写一遍,迟早会有一份把"存储出错"
// 写成 fail-closed,那时候的表现是 Redis 一抖全站人被踢,且日志里没有任何线索。
//
// err 的语义是"没能判定",不是"没被作废"。调用点必须自己决定怎么办,本函数
// 不替它做这个决定 —— 与 RevocationStore 上那两个方法的约定一致。
func SessionInvalidated(ctx context.Context, c *Claims) (bool, error) {
	// 拿不到签发时刻就无从判定。放行(fail-open):这道闸的用途是**踢人**,
	// 而踢人判不出来时,漏过一次的后果是"一次没踢干净",误判的后果是
	// "用户被随机踢下线、且完全无法复现"。后者更糟。
	//
	// 注意这里的 nil 检查是必须的,不能省:Claims.IssuedAt 是 *jwt.NumericDate,
	// 而 NumericDate.IsZero 是**值接收者**方法,对 nil 指针调用会 panic。
	// 本仓库所有令牌都由 sign() 签发、必定带 iat,所以这条实际不可达 ——
	// 留着是因为"不可达的解引用崩溃"是最不该省的那类判断。
	if c == nil || c.IssuedAt == nil || c.IssuedAt.IsZero() {
		return false, nil
	}
	line, err := DefaultRevocation().InvalidateBefore(ctx, c.UserID)
	if err != nil {
		return false, err
	}
	return TokenInvalidated(c.IssuedAt.Time, line, ClockSkewGrace), nil
}
