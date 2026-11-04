package redisint

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// defaultAddr 本机 Memurai 的默认地址(Windows 上唯一的 Redis 兼容实现)。
	defaultAddr = "127.0.0.1:6379"

	// defaultDB 草稿库号。**刻意用 15 而不是 0** ——
	// DB 0 是业务服务(config.dev.yaml 里 redis.db=0)在用的库,
	// 这套网要是 FLUSHDB 就会把正在跑的服务的缓存 / 吊销记录全清掉。
	defaultDB = 15

	// markerKey 本包的标记 key。
	//
	// 用途:**防误伤**。清库之前先看这个 key 在不在 ——
	// 在 ⇒ 这个库是本包的草稿库,随便清;
	// 不在且库非空 ⇒ 里面是**别人的数据**,拒绝清、skip 整个套件。
	//
	// 宁可一条不跑,也不能把别人放在 db 15 的东西删掉。
	markerKey = "redisint:marker"
)

// realRedis 连真 Redis;连不上就 skip(但要说清楚怎么装)。
//
// 每个用例进来时这个库是干净的(FLUSHDB 过),用例之间不互相污染。
func realRedis(t *testing.T) *redis.Client {
	t.Helper()

	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	db := defaultDB
	if v := os.Getenv("REDIS_TEST_DB"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("REDIS_TEST_DB 不是数字:%q", v)
		}
		db = n
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_TEST_PASSWORD"),
		DB:       db,
		// 连接池给足:并发用例要真的并发。
		PoolSize:     20,
		MinIdleConns: 2,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		t.Skipf("跳过:连不上真 Redis(%s db=%d):%v\n"+
			"  Windows 上装 Memurai(Redis 兼容实现):https://www.memurai.com/get-memurai\n"+
			"  装完确认服务在跑:`memurai-cli.exe -p 6379 ping`\n"+
			"  换个地址跑:REDIS_TEST_ADDR=host:6379 go test ./internal/integration/redis/ -v",
			addr, db, err)
	}

	t.Cleanup(func() { _ = rdb.Close() })

	// 清库前先验"这个库是不是我们的"
	n, err := rdb.DBSize(ctx).Result()
	if err != nil {
		t.Fatalf("DBSIZE 失败:%v", err)
	}
	if n > 0 {
		if _, err := rdb.Get(ctx, markerKey).Result(); err != nil {
			t.Skipf("跳过:库 %s(db=%d)里有 %d 个 key 且没有本包的标记 key %q —— "+
				"那可能是别人的数据,拒绝 FLUSHDB。\n"+
				"  换个库号跑:REDIS_TEST_DB=14 go test ./internal/integration/redis/ -v",
				addr, db, n, markerKey)
		}
	}
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FLUSHDB 失败:%v", err)
	}
	// 打上标记(下次进来就知道这个库是草稿库)
	if err := rdb.Set(ctx, markerKey, "1", 0).Err(); err != nil {
		t.Fatalf("写标记 key 失败:%v", err)
	}

	return rdb
}

// ctx5 一个够用的超时上下文。
func ctx5(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ctx30 稍长的超时(给 Pub/Sub 订阅确认、广播等待用)。
func ctx30(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// waitFor 轮询等某个条件成立;超时返回 false。
//
// Pub/Sub 这类**没有同步信号**的地方只能这样等。
// 用固定 sleep 是"等运气",这里给的是"最多等这么久"。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}
