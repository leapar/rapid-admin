// 前缀失效 —— 按 key 前缀批量删,而不是逐个 Del
package cachex

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// ErrEmptyPrefix 空前缀 —— 拒绝执行
//
// 不做"那我就删光本实例的全部 key"这种好心办坏事:调用方写 DeletePrefix(ctx, "")
// 几乎一定是变量拼错了(比如 fmt.Sprintf 的格式串少了个 %s),而它的后果是
// **把整个命名空间的缓存一次清空**。宁可报错让人看见,也不要静默执行一个
// 看起来成功的全量删除。
var ErrEmptyPrefix = errors.New("cachex: DeletePrefix 前缀不能为空")

// scanBatchSize SCAN 每次游标步进量
//
// 太小 → 往返多;太大 → 阻塞 Redis 主线程更久。500 是社区常用的折中值。
const scanBatchSize = 500

// DeletePrefix 失效所有以 prefix 开头的 key,返回删掉的条数。
//
// **这是"缓存一份实体"能不能安全落地的前提。** Del 只能删已知的 key,但改一条字典
// 的时候你并不知道有多少个 key 缓存了它(可能是 dict:12:items、dict:12:group、
// role:3:perms……)。没有前缀失效,唯一的办法就是"别缓存"或者"等 TTL 自然过期",
// 后者在配置中心这种"改了必须马上生效"的数据上直接不可接受。
//
// ⚠️ SCAN 是**渐进式**的,不保证一致性:扫描期间新写入的 key 可能被漏掉。
//
//	对失效来说可以接受(漏掉的那个会按自己的 TTL 过期),但如果调用方在
//	DeletePrefix 之前就已经把数据改了,漏掉的 key 就是**陈旧数据**。
//	所以正确姿势是:先改库 → 再 DeletePrefix,而不是反过来。
func (s *Safe) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	if strings.TrimSpace(prefix) == "" {
		return 0, ErrEmptyPrefix
	}
	if s == nil || s.backend == nil {
		// 没有 L2 —— 没有东西可删,不是错误(fail-open,和读路径一致)
		return 0, nil
	}
	sc, ok := s.backend.(Scanner)
	if !ok {
		// 后端不支持扫描 —— 没有可枚举的 key,同样不是错误(fail-open)。
		//
		// ⚠️ **内存后端是支持扫描的**(MemoryBackend.Scan,见 memory_backend.go):
		//   单测里的前缀失效走的就是这条路,不是被跳过的。
		//   (2026-10-26 M95 修正:这段注释原先写着"内存后端等",是错的。)
		//
		// ⚠️ **依赖"确实删掉了"的调用方必须先问一句 SupportsScan()** ——
		//   这里的 (0, nil) 对"清理一下不用的缓存"没问题,
		//   对"改完必须马上生效"是灾难:什么都没删 = 数据没更新,而且没有任何报错。
		//   见 Safe.SupportsScan 的注释。
		return 0, nil
	}

	// ⚠️ **传字面前缀,不传 pattern**(2026-10-26 M96 改)
	//
	// 旧实现传的是 `fkey(prefix) + "*"`,而 Redis 的 SCAN MATCH 走 glob 语法 ——
	// 前缀里自带的 `*` `?` `[` 会变成通配符,于是 DeletePrefix("a*b") 会把
	// "axb:2" 这种**别的业务的 key** 一起删掉(症状:删多了,且没有任何报错)。
	//
	// 现在契约是"字面前缀",由会 glob 的那个后端(Redis)自己转义。
	scanPrefix := s.fkey(prefix)
	var (
		cursor  uint64
		deleted int64
	)
	for {
		keys, next, err := sc.Scan(ctx, cursor, scanPrefix, scanBatchSize)
		if err != nil {
			// 删了一半失败要说明白:调用方可能需要重试
			return deleted, fmt.Errorf("cachex: DeletePrefix scan %s: %w", scanPrefix, err)
		}
		if len(keys) > 0 {
			if derr := s.backend.Del(ctx, keys...); derr != nil {
				return deleted, fmt.Errorf("cachex: DeletePrefix del: %w", derr)
			}
			deleted += int64(len(keys))
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	recordOp("l2", "delscan", "ok", 0)
	logger.L().Debug("cachex: prefix invalidated",
		zap.String("prefix", scanPrefix), zap.Int64("deleted", deleted))
	return deleted, nil
}

// DeletePrefix L1 + L2 一起按前缀删,返回 L2 删掉的条数
//
// L1 是进程内的,不删的话本实例在 L1TTL 内还会继续吐旧值 —— 表现为"我明明清了缓存,
// 这台机器上的接口还是老数据,重启就好了",是这个 bug 最常见的形态。
func (c *TwoLevel) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	if strings.TrimSpace(prefix) == "" {
		return 0, ErrEmptyPrefix
	}
	if c == nil {
		return 0, nil
	}
	c.purgeL1Prefix(c.l1PrefixOf(prefix))
	if c.safe == nil {
		return 0, nil
	}
	return c.safe.DeletePrefix(ctx, prefix)
}

// purgeL1Prefix 删掉 L1 里所有匹配前缀的条目
func (c *TwoLevel) purgeL1Prefix(match string) {
	if c == nil || c.l1 == nil {
		return
	}
	// Keys() 会把整个 L1 快照出来;容量上限 DefaultL1Cap=5000,这个开销可接受。
	// 真要换成 O(1) 失效得改 LRU 的数据结构(加一层 prefix 索引),不值得。
	for _, k := range c.l1.Keys() {
		if strings.HasPrefix(k, match) {
			c.l1.Remove(k)
		}
	}
	setL1Size(c.l1.Len())
	recordOp("l1", "delscan", "ok", 0)
}

// l1PrefixOf 给定业务 key 前缀,算出它在 L1 里对应的前缀
//
// 走 fkey 而不是手拼 "l1:" —— 无 L2 时 fkey 是恒等映射,手拼会拼出一个
// 永远匹配不到的 "l1:xxx",失效静默不生效。这正是"改了没生效"的来源。
func (c *TwoLevel) l1PrefixOf(prefix string) string {
	return c.fkey(prefix)
}

// SupportsScan 报告后端是否支持按前缀扫描 —— 决定 DeletePrefix 能不能真的删。
//
// ⚠️ **这是给"安全关键的前缀失效"准备的**。DeletePrefix 在后端不支持扫描时
// 会 fail-open 返回 (0, nil)(语义是"没有可枚举的 key,不是错误")—— 那个语义对
// "清理一下不用的缓存"没问题,但对"撤销授权后清掉权限画像"是**灾难**:
// 静默什么都没删 = 权限没撤销,而且**没有任何报错**。
//
// 所以依赖"确实删掉了"的调用方,必须先问一句这个。
func (s *Safe) SupportsScan() bool {
	if s == nil || s.backend == nil {
		return false
	}
	_, ok := s.backend.(Scanner)
	return ok
}

// L2Available 报告 L2 后端是否已配置(诊断 / 启动日志用)
//
// 2026-10-25 M90:后端由 *redis.Client 变成接口之后,这个名字里的 "Redis"
// 已经不是事实(可以是内存后端),所以改叫 L2Available。
func (s *Safe) L2Available() bool { return s != nil && s.backend != nil }

// PingL2 连通性探测。err 非 nil 时调用方应按"无 L2"降级,而不是当启动失败
func (s *Safe) PingL2(ctx context.Context) error {
	if s == nil || s.backend == nil {
		return errors.New("cachex: L2 未配置")
	}
	p, ok := s.backend.(Pinger)
	if !ok {
		// 后端没有探活概念 —— 视为"探不了",而不是"探不通"
		return errors.New("cachex: L2 后端不支持探活")
	}
	if err := p.Ping(ctx); err != nil {
		return fmt.Errorf("cachex: L2 ping: %w", err)
	}
	return nil
}
