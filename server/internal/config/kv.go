// 键值存储(计数 / 滑动窗口)配置 —— 限流与登录锁定的后端选择
package config

// KVConfig 限流 / 登录锁定用的键值存储配置。
//
// 与 AuthConfig.RevocationMode 分成两个开关,不是重复,是因为**后果等级不同**:
//
//   吊销降级 = 令牌不失效 = 安全事故    → 生产漏配应该直接启动失败
//   限流降级 = 阈值不准 = 可用性问题    → 本地/预发用内存完全合理
//
// 合成一个开关的话,本地想用内存限流调试就必须连 Redis,或者为了调试把
// 吊销一起降级 —— 后者不可接受。
//
// 但两者**共用同一个决策点**:kvstore.Resolve 在 main 启动时调用一次,
// ratelimit / loginlock 之后只问 kvstore.DefaultStore(),自己不碰
// repository.RDB。"有没有 Redis" 因此只在一个地方被判断。
type KVConfig struct {
	// Mode 存储实现:auto | redis | memory
	//
	//   auto   有 Redis 用 Redis,没有退内存(默认)。限流**存在**,
	//          但多副本下每个副本各数各的,实际阈值 = 配置值 × 副本数。
	//   redis  强制 Redis,没配 redis.addr 直接启动失败。**生产多副本必须用这个**。
	//   memory 强制内存,即使配了 Redis 也不连。仅调试用。
	Mode string `mapstructure:"mode"`
}