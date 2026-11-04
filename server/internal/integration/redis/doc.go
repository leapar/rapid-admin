// Package redisint 是**真 Redis 集成测试**的落脚点(不是普通单测)。
//
// # 这份网为什么现在才有
//
// 框架层里所有依赖 Redis 的代码,在此之前**一次都没有在真 Redis 上跑过**。
// 原因很具体:
//
//   - cachex / idempotentx / smslimit / push / service(dict_cache) 用的是
//     **miniredis**(进程内假 Redis)。它只覆盖一部分语义:
//     SCAN 忽略 COUNT、游标恒为 0、Lua 由 gopher-lua 解释、Pub/Sub 的确认帧
//     处理与真 Redis 不同。
//   - kvstore(RedisCounter / RedisWindow)、auth.RedisStore(含 Lua 作废线)、
//     cronx 的 redisLocker(Lua CAS 锁)、revocache(跨实例广播)、
//     authz.CachedLoader、loginlock —— **连 miniredis 都没有**。
//     它们只有"内存版"被测过,Redis 版只有编译期接口断言。
//
// 后果不是"少测了一点",而是"**Redis 版和内存版可以不一致,而没人会发现**"。
// 这类不一致历史上已经出过两次(smslimit 的固定窗口、cachex 的 glob 前缀),
// 两次都是内存版测得绿、Redis 版从没跑过 ⇒ 上线才发现。
//
// # 这份网跑什么
//
// 一条一条走**框架真正会走的 Redis 路径**,用真的 RESP 协议:
//
//	kvstore      RedisCounter(INCR+EXPIRE) / RedisWindow(ZSET 滑动窗口)
//	cachex       L2 读写 / TTL 归一 / 前缀失效(真 SCAN 游标 + glob 转义)
//	auth         RedisStore 吊销往返 / Lua 作废线(只前进不后退)/ revocache 广播
//	cronx        redisLocker 的 Lua CAS 续期与释放
//	idempotentx  结果 + 锁两把 key 的往返
//	smslimit     windowScript 的 INCR+EXPIRE 原子性
//	push         Redis pub/sub 传输层(多 topic / 事件不丢)
//	loginlock    走 kvstore 的真实计数
//	service      字典缓存的前缀失效
//
// # 怎么跑
//
// 默认连 `127.0.0.1:6379` 的 **DB 15**(约定俗成的"草稿库",不碰业务用的 DB 0)。
//
//	cd server && go test ./internal/integration/redis/ -v
//
// 连不上就 **skip 并说清楚怎么装**(Windows 上装 Memurai:
// https://www.memurai.com/get-memurai)。
//
// 环境变量:
//
//	REDIS_TEST_ADDR     覆盖地址(默认 127.0.0.1:6379)
//	REDIS_TEST_DB       覆盖库号(默认 15)
//	REDIS_TEST_PASSWORD 密码(Memurai 默认没有 requirepass,一般留空)
//
// ⚠️ **DB 15 会被 FLUSHDB**。为防误伤:库非空且**没有本包的标记 key**时,
// 这套网会拒绝清库并 skip —— 宁可一条不跑,也不能把别人放在 db 15 的数据删了。
//
// # 已抓到的真问题(2026-11-01,本包第一次跑起来那天)
//
// 这套网一上来就抓到两个 —— 都在 kvstore,都是"Redis 版从没被跑过"造成的。
//
// ## P0:装配绕过了构造器 ⇒ 每一个限流接口直接 500
//
// `kvstore.redisStore()` 用结构体字面量造窗口:
//
//	return &Store{..., Window: &RedisWindow{rdb: rdb}, ...}
//	                                          ↑ 绕过了 NewRedisWindow
//
// 而 `NewRedisWindow` 才会把 `rand` 设成 `RandomID` ⇒ `rand` 是 nil
// ⇒ `RedisWindow.Allow` 里的 `w.rand(4)` 是**空函数指针调用** ⇒ panic。
//
// 后果不是"限流不准",是**每个带限流的接口直接 500**:登录 / 发短信 /
// 刷新 token / 验证码 / 切换身份 全在限流名单上。
// Recover 中间件把 panic 兜成 500,日志里只有一条 recovered panic,像"偶发"。
//
// 为什么没人发现:RedisWindow 的 Redis 版**连 miniredis 都没有**;
// 内存版根本没有 `rand` 字段,永远是对的;而 `Resolve` 是唯一的装配入口,
// main 走的就是它 ⇒ **一旦配了 redis.addr 就中招**。
//
// 修法两层:① `redisStore` 改走构造器;② `Allow` 内部对 `rand == nil` 兜底。
// ② 是为了"根因被改回来时线上不会全线 500";① 的回归网是白盒的
// (`internal/pkg/kvstore/redis_ctor_test.go`),因为 ② 会让黑盒的
// "不许 panic" 一直绿。
//
// ## P1:INCR 与 EXPIRE 之间留了"无 TTL"窗口 ⇒ 账号被永久锁定
//
// `RedisCounter.Incr` 原来是两条命令:
//
//	n, _ := rdb.Incr(ctx, key)              // 第 1 条
//	if n == 1 { rdb.Expire(ctx, key, ttl) }  // 第 2 条
//
// 中间那个窗口里 **key 已经存在、但没有 TTL**。此刻进程挂掉 / 网络断
// ⇒ key **永久不过期** ⇒ 登录失败计数永不清零 ⇒ 那个 (ip, 用户名)
// 被**永久锁定**。
//
// 内存版没这个洞(建条目时一次算好过期时刻),而**只有内存版被测过**。
//
// 修法:用一段 Lua 把 INCR 与 EXPIRE 合成一步(与 smslimit.windowScript 同款),
// 并且顺手**补设**"存在但没有 TTL"的孤儿 key(旧实现留下的)。
//
// # 没有抓到问题的那些
//
// 其余路径(cronx 的 Lua CAS 锁、auth 的 Lua 作废线、revocache 的跨实例广播、
// push 的 pub/sub、cachex 的真 SCAN 与 glob 转义、idempotentx 的事务)
// **第一次跑就全绿** —— 它们是对的,只是从来没被验证过。
//
// "没抓到"不等于"验过了":本包的每条断言都跑在真的 RESP 协议上,
// 而**基线是绿的**(修复后重跑仍绿),不是"改完变绿"。
package redisint
