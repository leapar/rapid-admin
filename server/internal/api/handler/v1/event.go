// 通用事件推送端点 GET /rapid-admin/v1/events —— M72
//
// 一条 SSE 连接承载全部 topic,取代原来的 /auth/perm-stream(后者已删)。
// 一次连接 vs 每个主题一条连接:100 个运维开 100 个标签页,前者是 100 条
// 连接 + 100 个 Redis 订阅,后者按主题翻倍 —— 而浏览器对同域并发连接数有上限
// (HTTP/1.1 是 6 条),主题一多必然触发排队甚至直接连不上。
//
// ⚠️ **服务端决定订阅范围,客户端不能自选** —— 这是本端点最重要的安全边界。
// 如果允许 GET /events?topics=job,任何登录用户都能订阅到定时任务事件,
// 完全绕过 RBAC(事件本身只是信号,但它会暴露"有什么任务在跑、什么时候跑完",
// 而且能让前端去拉那些本来无权访问的接口)。所以请求里**不带** topics 参数,
// 即便带了也不读。
package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/push"
	"rapid-admin/internal/service"
)

type EventHandler struct{ *handler.BaseHandler }

func NewEventHandler(r *service.Registry) *EventHandler {
	return &EventHandler{BaseHandler: handler.New(r)}
}

// sseHeartbeat 心跳间隔。
//
// ⚠️ 25s 不是随手取的:nginx / 各种企业网关的默认空闲超时是 60s,心跳必须
// 显著小于它,否则连接会被中间设备静默掐掉,而客户端毫不知情(表现为"页面
// 半小时后就不更新了",且服务端日志一切正常 —— 极难排查)。
const sseHeartbeat = 25 * time.Second

// degradedRetry 降级后客户端的重连间隔(SSE `retry:` 字段,毫秒)。
//
// Redis 挂着的时候我们**主动关闭**连接,让 EventSource 按这个间隔重试,
// 而不是默认的 3 秒 —— 后者会把一个已经挂掉的 Redis 打成风暴。
// 1 分钟的重试频率对"Redis 恢复后自动接上"完全够用。
const degradedRetry = 60_000

// Stream GET /rapid-admin/v1/events
//
// 鉴权:走 middleware.SSEAuth()(见 router.go 豁免清单)—— EventSource 是
// W3C 标准 API,不能自定义 Header,只能走 ?access_token= / Cookie。
func (h *EventHandler) Stream(ctx context.Context, c *app.RequestContext) {
	caller := h.BaseHandler.RequireCaller(c)
	if caller == nil {
		return
	}

	topics := h.subscribableTopics(ctx, caller)

	writeSSEHeaders(c)
	c.Flush()

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	evs, unsub := push.Subscribe(streamCtx, topics)
	defer unsub()

	if evs == nil {
		// Redis 不可用 → 明确告诉前端"此页非实时",并主动断开让 EventSource
		// 按 retry 间隔重试。**不静默**:静默的表现是"页面就是不刷新",
		// 运维查一圈前端查不到任何线索。
		logger.L().Warn("events 订阅失败,连接降级为轮询",
			zap.Uint64("user_id", caller.UserID), zap.Strings("topics", topics))
		writeSSE(c, push.EventDegraded, map[string]any{
			"reason":   "subscribe_failed",
			"retry_ms": degradedRetry,
		})
		writeSSERetry(c, degradedRetry)
		return
	}

	writeSSE(c, push.EventConnected, map[string]any{
		"topics":      topics,
		"degraded":    false,
		"user_id":     caller.UserID,
		"heartbeat_s": int(sseHeartbeat / time.Second),
	})
	c.Flush()

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(c, ":heartbeat\n\n"); err != nil {
				return
			}
			c.Flush()
		case ev, ok := <-evs:
			if !ok {
				// Redis 订阅流断了(通常是连接被服务端断开)。主动收尾:
				// EventSource 会自动重连,前端无需参与。
				return
			}
			if err := writeSSE(c, ev.Event, ev.Payload); err != nil {
				return
			}
			c.Flush()
		}
	}
}

// subscribableTopics 按 caller 的权限算出该订阅哪些 topic。
//
// ⚠️ 权限判定走 middleware.CanAccess —— 它是 RBAC 中间件里 decideAccess 的
// 薄包装,不是第二套实现。SSE 走不了 RBAC 中间件(一条长连接会占着一次
// 鉴权链),但权限**必须**判,不能因为"没挂中间件"就默认放行。
//
// ⚠️ 这里是**连接建立时判一次**,连接存续期间权限变了不会重新判。
// 缓解:权限变更会发 perm_changed 事件(§TopicPerm),前端收到后重拉权限并
// 主动重建连接 —— 见 useEventStream.ts。代价是"变更到重建之间有个窗口,
// 老用户还能收到他已无权访问的事件信号"。信号本身不含数据,且窗口只有
// 一次重连的时间,可接受;真要严就得做连接级的权限失效,本期不做。
func (h *EventHandler) subscribableTopics(ctx context.Context, caller *service.Caller) []string {
	principal, _ := authz.PrincipalFromContext(ctx)

	topics := []string{
		push.TopicPerm,                // 权限失效广播:登录即可,不限角色
		push.TopicUser(caller.UserID), // 站内信未读:只看自己的
	}

	// 定时任务状态:需要 job:read(与 GET /jobs 用的是同一套判定)
	if middleware.CanAccess(caller, principal, "job", "read", 0) {
		topics = append(topics, push.TopicJob)
	}

	// 审批 / 待办:需要 approval:read。
	//
	// ⚠️ **M83-d 起 seed 里就有 approval:* 资源码了**(11 个权限码),
	// 所以这个判定**现在真的会过** —— 绑了 `approval:read` 的角色会订上
	// 这个 topic,M88-2 的 publisher 也真的会往这里发事件。
	// (M83 刚落地时这段注释写的是"审批模块还不存在,这里对所有非超管都是
	// false" —— 那时属实,现在过期了。)
	if middleware.CanAccess(caller, principal, "approval", "read", 0) {
		topics = append(topics, push.TopicApproval)
	}

	// 字典变更广播:需要 dict:list。
	//
	// ⚠️ **M93 之后每个登录角色都有 `dict:list`** —— 所以这道门禁
	// 实际上等于"登录即可"。仍然显式判,理由同上(将来字典可能改成
	// 只对部分角色可见,门禁已经在那儿了)。
	//
	// ⚠️ **事件不带字典内容**,只带 group_code,收到的人自己去重拉
	// `GET /system/dicts?groupCode=xxx` —— 推的是信号,不是数据。
	if middleware.CanAccess(caller, principal, "dict", "list", 0) {
		topics = append(topics, push.TopicDict)
	}

	return topics
}

// === SSE 写入小工具 ===
//
// 三个头一个都不能少:
//   - X-Accel-Buffering: no + 立即 Flush → 不然 nginx 会缓冲,前端永远收不到
//   - Cache-Control: no-cache → 不然代理会把整条流缓存住
//   - Transfer-Encoding: chunked → 没有 Content-Length,靠分块维持长连接

func writeSSEHeaders(c *app.RequestContext) {
	c.SetStatusCode(200)
	c.Response.Header.Set("Content-Type", "text/event-stream")
	c.Response.Header.Set("Cache-Control", "no-cache")
	c.Response.Header.Set("Connection", "keep-alive")
	c.Response.Header.Set("X-Accel-Buffering", "no")
	c.Response.Header.Set("Transfer-Encoding", "chunked")
}

// writeSSE 写一条命名事件。
//
// ⚠️ data 一律走 JSON —— 即使 payload 是字符串也要写成 `"foo"` 而不是裸
// foo,否则客户端 JSON.parse 会炸。map 的 key 顺序由 encoding/json 按字典序
// 排,不影响解析。
func writeSSE(c *app.RequestContext, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c, "event: %s\ndata: %s\n\n", event, data)
	return err
}

// writeSSERetry 设置客户端重连间隔
func writeSSERetry(c *app.RequestContext, ms int) {
	fmt.Fprintf(c, "retry: %d\n\n", ms)
}
