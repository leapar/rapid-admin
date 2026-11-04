// 鉴权配置(吊销存储模式等)—— 与 JWT 配置分开
package config

import "time"

// AuthConfig 鉴权运行时行为配置。
//
// 独立于 JWTConfig:JWTConfig 管的是"token 长什么样、多长",
// 这里管的是"token 失效了怎么才算真失效"。两件事的生命周期不同,
// 合成一块会让"改 TTL 要动吊销配置"这种无关耦合。
type AuthConfig struct {
	// RevocationMode 吊销存储实现:auto | redis | memory
	//
	//   auto   有 Redis 用 Redis,没有退内存(默认)。只保证"能吊销",
	//          不保证多副本一致 —— 单副本部署够用。
	//   redis  强制 Redis,没配 redis.addr 直接启动失败。
	//          **生产多副本必须用这个**:内存吊销只在单进程内有效,
	//          用户换个负载均衡节点,被吊销的 token 又活了。
	//   memory 强制内存,即使配了 Redis 也不连。仅调试用。
	//
	// 显式存在的意义:把"我现在到底是不是安全的"变成一个配置项,
	// 而不是隐式继承"有没有 Redis"。之前它是后者,而且不配也没人知道。
	RevocationMode string `mapstructure:"revocation_mode"`

	// PrincipalCacheTTL 权限主体缓存 TTL,0 → 5min
	//
	// **这也是权限撤销的延迟上界**:改完授权关系后,别的副本最迟这么久才看到。
	// CachedLoader 在 grant/revoke 时会主动删 key,所以正常路径是 0 延迟;
	// 但**任何没走那两处 Invalidate 的改权限路径**都会在这里挂满 TTL。
	// 调小它换收敛速度,调大换 Redis 压力 —— 默认 5min 是"多数后台操作感知不到"的折中。
	//
	// ⚠️ 它是**数据**的配置(缓存哪份数据、多久),不是缓存层的配置,所以放这儿
	//    而不是 cache 段 —— 和 cachex.Config 不收 TTL 是同一个理由。
	PrincipalCacheTTL time.Duration `mapstructure:"principal_cache_ttl"`

	// Anomaly 异常登录检测配置(M128)
	//
	// 这段配置存在的理由:pkg/anomalylogin 的检测一旦接进登录链路,
	// 就必须有**关掉它的开关**。
	// 一个会误报的检测比没有检测更糟 —— 用户被骚扰 → 关掉通知 → 真异常也没人看。
	// 而"能不能关"必须是配置项,不能是改代码。
	Anomaly AnomalyConfig `mapstructure:"anomaly"`
}

// AnomalyConfig 异常登录检测的配置。
//
// ⚠️ 这几项**每一个都有非零默认值**,且默认值都由
// `pkg/anomalylogin` 的字段注释语义推导而来 —— 不是随手填的:
//
//   - MaxConcurrent=3    ⇒ "允许同时在线 3 个会话,第 4 个起报"
//   - ActiveWindow=30m   ⇒ "30 分钟内有过活动"才算"同时在线"
//   - ImpossibleTravel=0 ⇒ 关(见下面的说明,这是**故意**的默认)
type AnomalyConfig struct {
	// Enabled 是否启用异常登录检测。默认 true。
	//
	// 关掉之后登录链路**完全不做检测**(连查库都不查),不是"检测但不报警"。
	Enabled bool `mapstructure:"enabled"`

	// MaxConcurrent 允许的同时活跃会话数(含本次登录),超过才报异常。默认 3。
	//
	// ⚠️ 这个旋钮在 M128 之前是**死的** —— `pkg/anomalylogin.Detect` 硬编码
	// `len(active) > 1`,这个字段从来没被读过。清单里"MaxConcurrent=3 默认"
	// 描述的是字段注释,不是行为。修好之后它才真的是个旋钮。
	MaxConcurrent int `mapstructure:"max_concurrent"`

	// ActiveWindow "多久之内有过活动"才算"同时在线"。默认 30m。
	//
	// ⚠️ **不能用 refresh_ttl(30 天)当这个口径**。
	// 那条 TTL 回答的是"这条会话还算不算数",不是"这个人现在在不在用"。
	// 拿它当"同时在线"⇒ 一个 30 天里登录过 4 次的人,**每次登录都算 4 端在线**
	// ⇒ 每次都报异常,阈值形同虚设。
	//
	// 活动时刻取 `COALESCE(last_active_at, login_at)` —— 与"活跃设备"列表同源。
	ActiveWindow time.Duration `mapstructure:"active_window"`

	// ImpossibleTravelWindow "多短时间之内换了 IP"算疑似账号共享/盗号。默认 0(关)。
	//
	// ⚠️ **默认关掉是刻意的**:这条规则只看 IP,不看地理位置。
	// 用户从家里 WiFi 切到手机流量(换 IP)只要几秒 ⇒ 开着必然误报。
	// 真正的"不可能旅行"需要 IP 地理库(两地相距多远),本仓没有那个库。
	// ⇒ 框架层把旋钮留好,要不要开由运维按自己的误报容忍度决定。
	ImpossibleTravelWindow time.Duration `mapstructure:"impossible_travel_window"`
}
