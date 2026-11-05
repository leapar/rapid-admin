# permnotify —— 权限失效广播

> 管理员改了角色权限,已经登录的浏览器**手里的权限是旧的** ——
> 菜单还是老的,按钮还是该显示就显示。
> permnotify 就是那个"喊一嗓子"的动作:让所有在线前端**重拉权限**。

## 功能

permnotify 只做一件事:

> `PublishInvalidateAll(ctx, actorID, reason)` —— 往 `push.TopicPerm` 频道
> 发一条 `perm_changed` 事件,前端收到后重拉 `GET /auth/accessible` 并**重建 SSE 连接**。

它是一个**广播**,不是精确推送:

- 角色变了,所有持该角色的用户都要刷 ⇒ 广播给所有人;
- 精细化(按 role 找 userID 列表再 publish)本期不做 —— 收益小,复杂度大。

它**不携带任何权限内容**。payload 只有两个字段:

| 字段 | 类型 | 含义 |
|---|---|---|
| `reason` | string | 什么变了:`role_resource_changed` / `role_active_changed` / `user_role_changed` / `manual` |
| `actor_id` | string | 谁触发的(审计用)。string 化是因为 JS 的安全整数上限 |

**广播本身不带权限数据**,只发"你该重拉了"这个信号 —— 收到的人自己去重拉。
理由与整个 [push](push.md) 包一致:**事件是信号,不是数据**。

**不解决什么**:

- 不吊销会话(见「注意事项」第 1 条 —— 这是本包最容易被误解的地方);
- 不做权限判定本身(RBAC 在 [casbinx](../security/casbinx.md) 那条线上);
- 不推给 App 客户端。

## 配置

**该包无 yaml 配置。** 没有 `permnotify:` 段,也没有任何配置项。

它连 Redis 都不自己持有 —— **依赖方向是 `permnotify → push`**(反过来不成立:
push 不知道权限是什么)。发布走 `push.Publish`,而 push 用的是 `redis:` 块。

### 代码侧

```go
permnotify.PublishInvalidateAll(ctx, caller.UserID, "role_resource_changed")
```

没有构造函数、没有 `Init`、没有配置项 —— 就这一个函数。
(M71 把原来那套 `Init` / `InitRedis` / `Subscribe` / `rdb` / `ChannelAll` 全删了:
`Init` 从来没人调用,`Subscribe` 的唯一调用方是 `/auth/perm-stream` handler,
而它后来被 `pkg/push` 接管 —— 订阅逻辑跟着 handler 一起搬走了。)

## 使用方法

### 装配推送通道(启动期一次)

```go
push.InitRedis(repository.GetRedis())
```

### 广播

```go
// 在改权限的事务提交之后调
permnotify.PublishInvalidateAll(ctx, caller.UserID, "role_resource_changed")
```

生产上的两个调用点都在 `internal/service/role_impl.go`:
改角色的启用状态 → `role_active_changed`;改角色的资源绑定 → `role_resource_changed`。

### 订阅(前端 SSE 端点干的事)

```go
ch, cancel := push.Subscribe(ctx, []string{push.TopicPerm})
defer cancel()
for ev := range ch {
    // ev.Event == push.EventPermChanged
    // payload: {reason, actor_id}
}
```

服务端按 RBAC 决定订阅范围(`push.TopicPerm` 是**登录即可**,不限角色)。

## 注意事项

- ⚠️ **权限变了以后,旧的 token 仍按旧角色放行**。
  这是**已知缺口**,不是本包的 bug:广播只是让前端**重拉权限**,
  它不动服务端 RBAC 里那个已经签进 token 的角色。
  降职(撤掉某个资源码)之后,用户手里的旧 access token 在**最长 `jwt.access_ttl`(默认 8h)**
  内依然带着旧角色,照样能访问那些接口。
  要真正掐断得用 [auth](../security/auth.md) 的**强制下线**原语
  (`InvalidateBefore` 作废线),但它目前只在改密码 / 重置密码这类路径上用,
  **权限变更没有调它**。
  换句话说:**前端会立刻变成"新的样子",但接口权限在 token 失效前还是"老的样子"** ——
  这两件事的时间窗不一致,不要以为广播等于权限立即生效。
- ⚠️ **订阅范围是建连那一刻的快照**。老连接上仍挂着已经不属于自己的 topic
  (比如刚被撤销了 `job:read` 的用户还能继续收 job 事件)。
  所以前端收到 `perm_changed` 后**必须重建连接**,只重拉权限不够 ——
  只重拉权限会让"能收到自己已无权访问的事件信号"这件事持续下去。
  代价是"变更到重建之间有个窗口"(前端延迟 500ms 重连),窗口里那点信号仍然会到;
  信号本身不含业务数据,可接受。
- ⚠️ **`PublishInvalidateAll` 永不返回错误**。推送是旁路 ——
  权限改成功了但推送失败,不能变成"改失败"。`push.Publish` 内部已经把投递失败记成
  Warn 日志,这里不必也不该重复处理。
- ⚠️ **频道名 `perm:invalidate:all` 故意不带 `push:` 前缀**。
  它是本包迁到 `pkg/push` **之前**就存在的频道名,保留它意味着发布端一行都不用改,
  没有"双写 / 双订阅"的迁移窗口。别"顺手统一前缀" —— 那会让在跑的发布端和订阅端对不上。
- ⚠️ **`reason` 的词表比注释里写的多一个**。包注释和示例里列的是
  `role_resource_changed` / `user_role_changed` / `manual`,
  但生产实际发出的是 `role_resource_changed` 和 **`role_active_changed`**。
  前端要按 `reason` 分支处理的话,得把这四个都认了 —— 别只按注释里那三个写。
- ⚠️ **payload 不带权限内容,前端拿到后必须自己重拉**。
  别在前端拿 `payload` 拼权限 —— 那等于在前端造第二份权限契约,两边必然漂。
- ⚠️ **Redis 不可用时广播静默丢弃**(只记 Debug 日志)。
  这意味着"改了权限但别人的页面没刷新"在 Redis 挂了的时候是**完全静默**的 ——
  前端要靠轮询或手动刷新兜底,别指望广播一定到。

## 相关

- 源码:`internal/pkg/permnotify/`
- 可运行示例:`examples/permnotify/main.go` —— `go run ./examples/permnotify`
- 传输层(本包只是它的一个生产者)→ [push](push.md)
- 会话吊销 / 强制下线(本包**不做**的那件事)→ [auth](../security/auth.md)
- RBAC 判定本身 → [casbinx](../security/casbinx.md)
