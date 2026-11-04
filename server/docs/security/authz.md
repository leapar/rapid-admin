# authz —— 资源级鉴权原语:principal / grant

> 接口级 RBAC 回答"这个角色能不能调这个接口"。
> 还有一类问题它答不了:"这个用户能不能看**这一条**订单的详情" —— 细到具体资源、具体人。
> authz 做的是后者。

## 功能

authz 提供三样东西:

1. **`Principal`** —— 当前用户的鉴权画像:基础身份(uid / iid / role / 项目范围)
   \+ 应用层附加(有效授权 / deny 覆盖 / 个人功能开关)。
2. **`Resource`** —— 被访问资源的最小信息三元组:`ResourceType` / `ResourceID` /
   `OwnerUserID`。
3. **`GenericAssertor`** —— 断言"caller 能不能对 resource 做 permissionCode"。

外加:

- **`Loader` / `DBLoader` / `CachedLoader`** —— 装配"从哪加载 principal"
- **`MatchPermissionCode`** —— 权限码匹配(`*` / `house:*` / `house:view`)

**不解决什么**:接口级权限判定归 [casbinx](casbinx.md) 与 `middleware.RBAC`;
本包只管"**这一条数据**能不能碰"。两件事必须都在,漏一层就是 IDOR。

## 配置

### yaml 侧(`auth:` 块里的一个键)

```yaml
auth:
  principal_cache_ttl: 5m   # 权限主体缓存 TTL,0 → 5min
```

⚠️ **这个 TTL 就是权限撤销的延迟上界**。grant / revoke 走 `InvalidateUser`
主动删 key,正常路径 0 延迟;但**任何没走那两处 Invalidate 的改权限路径**,
都要在这里挂满 TTL 才生效。调小换收敛速度,调大换 Redis 压力。

它放在 `auth:` 段而不是 `cache:` 段,是因为它是**数据**的配置(缓存哪份数据、
多久),不是缓存层的配置。

### 代码侧

```go
authz.NewDBLoader(db *gorm.DB) *authz.DBLoader
authz.NewCachedLoader(inner authz.Loader, cache *cachex.Safe, ttl time.Duration) *authz.CachedLoader
```

`NewCachedLoader` 的 `ttl <= 0` 会兜底成 5 分钟;`cache == nil` 表示不缓存
(直查 DB,适合单实例 dev)。

## 使用方法

### Principal:构造与流转

```go
p := authz.FromCallerBase(authz.CallerFields{
    UserID:     1001,
    IdentityID: 2001,
    RoleCode:   "role_project_manager",
    ProjectIDs: []uint64{7, 9},
})
p.ActiveGrants = []authz.GrantRef{{
    GrantID:      5001,
    GrantType:    "order_manage",
    ResourceType: "house",
    ResourceID:   8801, // 0 = 该类型全部
    Permissions:  []string{"house:view"},
}}
p.Overrides   = []authz.Override{{PermissionCode: "house:delete", Effect: "deny"}}
p.FeatureFlags = map[string]bool{"new_dashboard": true}

// 中间件加载一次,下游随便读
ctx := authz.WithPrincipal(ctx, p)
got, ok := authz.PrincipalFromContext(ctx)
```

### Resource 与断言

```go
assertor := authz.NewGenericAssertor()
caller := &authz.CallerSummary{UserID: 1001, RoleCode: "role_project_manager"}

house := authz.HouseResource(8801, ownerUserID)
err := assertor.AssertCan(caller, p, house, "house:view") // nil = 允许
```

`Resource` 有四个快捷构造:`HouseResource` / `OwnerResource` / `AssetResource` /
`DeliveryResource`。

### Loader 装配

```go
dbLoader := authz.NewDBLoader(db)
cached := authz.NewCachedLoader(dbLoader, cachexSafe, 5*time.Minute)
middleware.SetPrincipalLoader(cached)

// 改了授权之后
cached.InvalidateUser(ctx, userID)
```

### 权限码匹配

```go
authz.MatchPermissionCode("house:*", "house:view") // true
authz.MatchPermissionCode("*", "user:delete")      // true
authz.MatchPermissionCode("house:view", "house:delete") // false
```

## 注意事项

- ⚠️ **这是 IDOR 的最后一道闸**。`middleware.RBAC` 只管"能不能进 handler",不保证
  "能不能看这一条"。service 里取到资源之后第一件事就该调 `AssertCan`。
- ⚠️ **决策顺序是按优先级短路,不是逐条累加**:
  `caller 缺失 → 超管 → 资源所有人直通 → 显式 deny → grant → 拒绝`。
  **资源所有人直通排在 deny 检查之前** ⇒ 显式 deny 对资源本人**无效**。
- ⚠️ **失效粒度是"用户",不是"身份"**。`InvalidateUser(userID)` 一次清掉这个人
  所有身份的缓存 —— grant 挂在 `grantee_user_id` 上、override 与 feature flag
  挂在 `user_id` 上,只有 roleCode / projectIDs 是身份级的。只清一个身份 ⇒ 这个人
  的其它身份继续拿着旧授权(最长一个 TTL)。
- ⚠️ **`userCachePrefix` 末尾的冒号不能省**。少了它,`"principal:12:"` 会匹配上
  `"principal:123:456"`(用户 123 的缓存),把不相干的人的授权画像一起删了 ——
  失效删多了和没删一样危险。
- ⚠️ **缓存 key 格式 `principal:{userID}:{identityID}` 是隐式契约**。改了之后线上
  存量条目全部对不上,上线瞬间会有一波全量回源。
- ⚠️ **principal 缓存只走 L2,故意不加本地 L1**。L1 是进程内的,grant/revoke 触发的
  Invalidate 只能删掉**本实例**的 L1,别的副本仍持有旧 principal ⇒ "改完权限立刻
  生效"退化成"最多 L1 TTL 后生效"。
- ⚠️ **前缀失效依赖后端支持 SCAN**。不支持时 `cachex.DeletePrefix` 会 fail-open
  返回 `(0, nil)` —— 对"撤销授权"来说就是**静默什么都没删**。所以
  `InvalidateUser` 先问 `SupportsScan()`,拿不到能力就**报错**,绝不假装失效成功。
- ⚠️ **`CachedLoader.Load` 返回 `(nil, nil)` 表示"确认过没有这个身份"**(null 占位
  命中),与"加载失败"(返回 error)是**两回事**:上层对两者处理一样(降级到
  role/user 判定),但前者是确定的事实,不是故障。
- ⚠️ **`DBLoader.Load` 只把 `gorm.ErrRecordNotFound` 映射成 `cachex.ErrNotFound`**。
  连接失败 / 超时 / 权限不足**绝不能**被当成"查无此数据" —— 那样一次 DB 抖动会被
  缓存成 null 占位,把瞬时故障固化成持续故障。
- ⚠️ **grant 的 `permissions` 是 JSON 数组字符串,解析失败不报错**,降级为"无权限"。
  脏数据的表现是"授权静默失效",不是"启动报错"。
- ⚠️ **`CachedLoader.Load` 必须拷贝一份再改 `CacheHit`**。singleflight 路径下同一个
  `*Principal` 指针会被多个 goroutine 同时拿到,直接写字段就是 data race(-race 会
  当场报)。
- ⚠️ **本包不 import service 包**。最小信息用 `CallerFields` / `CallerSummary`
  承载(避免 `cmd/migrate → repository → authz → service → loginlock → repository`
  的 import cycle)。所以 `RoleCode` 得由调用方从 `service.Caller` 拆出来传 ——
  **忘了传就等于没有角色**,断言会一路落到 grant 路径上。

## 相关

- 源码:`internal/pkg/authz/`
- 可运行示例:`examples/authz/main.go` —— `go run ./examples/authz`
- 接口级 RBAC(另一件事)→ [casbinx](casbinx.md)
- principal 缓存走 cachex 的三防 → [../storage/cachex.md](../storage/cachex.md)
