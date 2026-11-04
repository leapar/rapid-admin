// Package push 服务端事件推送通道(SSE 的传输层地基)—— M71
//
// 设计三条:
//
//  1. **事件是信号,不是数据。** payload 里没有业务正文,前端收到后
//     重新调 REST 拿最新状态。这样推送不扩大数据可见范围(能收事件的人
//     一定也能通过对应 REST 接口看到同样的数据),也不会产生"REST DTO 一份、
//     推送 payload 一份"的双份契约漂移。与既有的 perm-stream 一致。
//
//  2. **单路径投递。** Redis pub/sub 是唯一通道,没有"进程内 Hub + Redis"双投递。
//     双投递会让**同实例**的连接收到两份事件(Hub 一次、Redis 一次),而事件
//     是 refetch 信号 —— 长列表页会因此反复重拉,而且这个 bug 只在单实例部署
//     下出现,多实例部署恰好正常。去重要引事件 ID + LRU 集合,不划算。
//
//  3. **不静默。** Redis 不可用时 /events 会发 degraded 事件让前端切轮询。
//     permnotify 现在是 rdb == nil 直接 return,推送消失但没人知道 ——
//     对任务状态来说那意味着"页面永远不更新",比报错更难查,不学这个。
//
// 依赖方向:业务包(cronx / inbox / 未来的 approval)依赖本包,反之不依赖。
// 本包不知道 job、inbox 是什么,只认 topic 字符串。
//
// ⚠️ **别和 pkg/pushx 搞混**:pushx 是阿里云 EMNS 的封装(APNs / Android 厂商
// 通道 / Web Push),推的是**给 App 客户端**的离线通知;本包推的是**服务端到浏览器**
// 的实时事件(SSE)。两个都叫"推送",但通道、投递对象、失败语义完全不同 ——
// 看到 pushx 别以为"推送框架已经有了",那是给手机用的。
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

// topicPrefix 所有 Redis 频道的公共前缀。
//
// ⚠️ `perm:invalidate:all` 是**迁移前就存在的频道名**,故意不带本前缀:
// 本模块接管订阅端时,发布端(PermissionService 那条线)一行都不用改。
// 没有"双写 / 双订阅"的迁移窗口。
const topicPrefix = "push:"

// Topic 命名空间
const (
	// TopicPerm 权限失效广播。任何人登录后都会收到。
	// 频道名沿用迁移前的 perm:invalidate:all,保证发布端零改动。
	TopicPerm = "perm:invalidate:all"

	// TopicJob 定时任务状态广播。**需要 job:read 权限**(由 /events handler 判)
	TopicJob = "job"

	// TopicApproval 审批 / 待办广播。**需要 approval:read 权限**。
	//
	// ⚠️ **M88 起有生产者了** —— 审批引擎的 `ApprovalPublisher`
	// (internal/service/approval_publisher.go)往这个 topic 发
	// `approval.todo` / `approval.done`。**在此之前这条 topic 是空的**,
	// 订阅它的前端只会收到一个永远不来的监听器。
	TopicApproval = "approval"

	// TopicDict 字典变更广播。**需要 dict:list 权限**。
	//
	// ⚠️ **M94 起** —— 此前字典改了没有任何广播,前端 `stores/dict.ts` 的
	// 30 分钟缓存让"管理员改了标签,别人最长半小时看不到"成了常态。
	// 门禁是 `dict:list`:M93 之后**每个登录角色都有**这个码,
	// 所以实际上等于"登录即可",但仍然显式判 —— 将来若把字典改成
	// "只对部分角色可见",门禁已经在那儿了。
	//
	// ⚠️ **这条广播不携带字典内容**,只带 `group_code`(见 DictChangedPayload)——
	// 收到的人自己去重拉。理由是 pkg/push 包注释第 1 条:
	// **事件是信号,不是数据**。
	TopicDict = "dict"
)

// TopicUser 返回某用户的专属频道名(站内信未读、审批指派给我这类"只给本人"的事件)
func TopicUser(userID uint64) string {
	return fmt.Sprintf("%suser:%d", topicPrefix, userID)
}

// Event 一条推送事件。
//
// ⚠️ Payload **不放业务正文**(error 详情、消息内容、审批单据都不放),
// 只放"够前端判断要不要去拉"的最小信息。见包注释第 1 条。
type Event struct {
	// Topic 频道标识,原样回传给前端便于排查
	Topic string `json:"topic"`
	// Event 事件名,前端按这个名分发,如 "job.finished" / "inbox.unread"
	Event string `json:"event"`
	// Payload 小而稳定,不含业务数据正文
	Payload any `json:"payload,omitempty"`
	// At 事件产生时刻(ms)。用于前端丢弃乱序到达的旧事件
	At int64 `json:"at"`
}

// 事件名常量(前端 useEventStream 按这些名注册回调)
const (
	// EventConnected 连接建立。payload = { topics: [...], degraded: bool }
	EventConnected = "connected"
	// EventDegraded Redis 不可用,前端应切到轮询
	EventDegraded = "degraded"
	// EventLag 本连接被丢过事件(慢客户端),payload = { dropped: n }
	// 前端收到应立刻补拉一次列表
	EventLag = "lag"
)

// 常用事件名
const (
	EventJobFinished = "job.finished"
	EventJobFailed   = "job.failed"
	EventInboxUnread = "inbox.unread"
	EventPermChanged = "perm_changed"

	// ── 审批 / 待办(M73 契约,M88 落地生产端)──
	//
	// ⚠️ **事件名从 M73 起就没改过,也不许改** —— 前端 `useEventStream.ts`
	//   的 `EVENT` 常量按名字订阅,改名等于让所有已发版的前端收不到事件。
	//
	// ⚠️ **生产端是审批引擎(M84 发事件)→ service 层 publisher(M88 投递)**。
	//   引擎只认 `service.ApprovalEvent`,不认本包 —— 依赖方向是
	//   service → push,反过来不成立(push 不知道审批是什么)。
	//
	// ⚠️ **`Action` 的词表在 M88 加了 `rolled_back`** —— 打回也是一种"办完了",
	//   但它跟通过/驳回不同:单子回到申请人手里,不是终局。
	//   ⚠️ **这个词表在同一个 commit 里改了本文件 + web/src/types/approval.ts
	//   + 设计文档 §9.2** —— 事件是广播契约,少改一边就是两套词表。
	EventApprovalTodo = "approval.todo" // 新待办 / 待我审批
	EventApprovalDone = "approval.done" // 我这条处理完了(通过或驳回)

	// ── 字典(M94)──
	//
	// ⚠️ **事件名用点号,不用下划线** —— 与 M73 之后的新事件
	// (`job.finished` / `inbox.unread` / `approval.todo`)保持一致。
	// `perm_changed` 是 M73 之前的老名字,不改它(已发版前端在用)。
	EventDictChanged = "dict.changed" // 某个字典组被改了 → 前端把该组缓存作废
)

// DictChangedPayload 字典变更事件的 payload(M94 契约)
//
// ⚠️ **只带 group_code,不带字典内容** —— 收到的人自己去重拉
// `GET /system/dicts?groupCode=xxx`。理由同 ApprovalPayload:
// 广播出去的东西不构成第二份数据契约,也不扩大可见范围。
//
// ⚠️ **group_code 不敏感**:M93 之后任何登录角色都能拉任意字典组
// (`GET /system/dicts` 挂 `rbac("dict","list")`),所以广播它不越权。
type DictChangedPayload struct {
	// GroupCode 被改动的字典组(例如 "issue_status")
	GroupCode string `json:"group_code"`
}

// ApprovalPayload 审批 / 待办事件的 payload(M73 契约)
//
// ⚠️ 白名单,和 JobPayload 同一条纪律:事件会广播给所有订阅了 TopicApproval
// 的浏览器连接,**只带"够前端判断要不要重拉"的字段**。审批单正文
// (事由 / 金额 / 明细 / 附件)一律由前端自己回查,那才是按权限过滤过的地方。
//
// ⚠️ **没有 reason / 驳回意见**:审批意见常含内部口径,而"能看列表"和
// "该看这条的意见"未必是同一个权限粒度 —— 广播出去等于越权。
type ApprovalPayload struct {
	// TodoID 审批单 / 待办 ID。string 不是 uint64:雪花 ID 是 19 位数,
	// 超出 JS 的 2^53,裸数字会被 JSON.parse 静默截断(见 types/job.ts 文件头)
	TodoID string `json:"todo_id"`
	// Kind todo / approval —— 同一通道上区分两类,便于前端分别角标 / 路由
	Kind string `json:"kind"`
	// Action assigned / approved / rejected / rolled_back / cancelled
	//
	// ⚠️ **`rolled_back` 是 M88 加的** —— 之前没有"打回"这个动作,
	//   打回的语义(回到发起人、不是终局)跟 approved/rejected 都不一样,
	//   混进 approved 会让前端的角标减错。
	//
	// ⚠️ **词表两侧必须一起改**:本文件 + `web/src/types/approval.ts` 的
	//   `ApprovalEventAction` + 设计文档 §9.2。同一个 commit。
	Action string `json:"action"`
	// AssigneeID 当前处理人(user_id,string)
	AssigneeID string `json:"assignee_id,omitempty"`
}

// JobPayload 定时任务状态事件的 payload。
//
// ⚠️ 刻意**不含** Error 正文:能收这个事件的人确实有 job:read,也能通过
// GET /jobs/runs 看到 error,但推送经过 Redis 且会落在任何订阅者的内存里,
// 让前端自己回查更省事也更安全。
type JobPayload struct {
	JobKey     string `json:"job_key"`
	Status     string `json:"status"` // success / failed
	DurationMs int64  `json:"duration_ms"`
	// TriggerType schedule / manual
	TriggerType string `json:"trigger_type"`
}

// ErrNoRedis Redis 未注入 —— 已 Init 但没拿到 client
var ErrNoRedis = errors.New("push: redis 未初始化")

// Transport 传输层抽象。
//
// ⚠️ 本期只有 redisTransport 一个实现。留这个接口的唯一理由是文档 §2.2
// 登记的那三条"SSE 会不够"的场景 —— 将来真要换 WebSocket,新增一个实现
// 即可,**业务侧的 Publish 调用一行都不用改**。
//
// 代价是多约 15% 代码量。现在只有 0 个业务侧在用,是抽这个接口最便宜的时候;
// 等业务侧铺开了再加,就要改所有调用点。
type Transport interface {
	// Publish 投递一条事件。**不能阻塞在网络 IO 上**
	Publish(ctx context.Context, topics []string, ev Event) error
	// Subscribe 订阅若干 topic,返回事件流。调用方负责关 cancel
	Subscribe(ctx context.Context, topics []string) (<-chan Event, func(), error)
	// Available 传输层当前是否可用
	Available() bool
}

// defaultTransport 全局单例,由 Init 注入
var defaultTransport Transport

// Init 注入传输层(main 启动期调一次)
func Init(t Transport) { defaultTransport = t }

// InitRedis 用 redis.Client 构造默认传输层
func InitRedis(cli *redis.Client) {
	if cli == nil {
		return
	}
	defaultTransport = NewRedisTransport(cli)
}

// Available 当前是否可推送。Redis 没注入时返回 false
func Available() bool {
	return defaultTransport != nil && defaultTransport.Available()
}

// Publish 往一组 topic 投递事件。
//
// **永不返回错误给调用方当"业务失败"用**:推送是尽力而为的旁路,
// 投递方是 cronx 刚跑完任务的 goroutine 或业务写完库的请求 ——
// 不该因为"推送没发出去"就让主流程失败。失败只记日志。
func Publish(ctx context.Context, topics []string, ev Event) {
	if len(topics) == 0 {
		return
	}
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	if defaultTransport == nil {
		// 开发 / 单测里没 Init Redis 是常态,记 Debug 不记 Warn,免得刷屏
		logger.L().Debug("push: 传输层未初始化,事件丢弃",
			zap.String("event", ev.Event), zap.Strings("topics", topics))
		return
	}
	if err := defaultTransport.Publish(ctx, topics, ev); err != nil {
		// ⚠️ 不静默。这是"看起来在工作但其实没推"的典型来源,
		// 排查页面为什么不刷新时,这条日志是唯一的线索
		logger.L().Warn("push: 事件投递失败",
			zap.String("event", ev.Event),
			zap.Strings("topics", topics),
			zap.Error(err))
	}
}

// Subscribe 订阅一组 topic。
//
// rdb == nil 时**不返回 error**,而是返回 nil channel:
// 调用方(/events handler)据此发 degraded 事件让前端切轮询。
// 返回 error 会让前端看到"连接失败",然后无限重连 —— 而那永远也不会成功。
func Subscribe(ctx context.Context, topics []string) (<-chan Event, func()) {
	if len(topics) == 0 || defaultTransport == nil {
		return nil, func() {}
	}
	ch, cancel, err := defaultTransport.Subscribe(ctx, topics)
	if err != nil {
		logger.L().Warn("push: 订阅失败,连接将降级为轮询",
			zap.Strings("topics", topics), zap.Error(err))
		return nil, func() {}
	}
	return ch, cancel
}

// marshalEvent 事件序列化(push 包内部与测试共用)
func marshalEvent(ev Event) (string, error) {
	b, err := json.Marshal(ev)
	return string(b), err
}
