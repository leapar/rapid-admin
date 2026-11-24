# querycache —— GORM 查询结果缓存(SELECT 缓存 + 写回调自动失效)

> [cachex](cachex.md) 是"按点缓存"(`GetOrLoad` 显式 loader),列表 / 树 /
> 报表类 SELECT 每次都打库。querycache 借鉴 GoFrame gdb 的 `Model.Cache`,
> 把"这条查询的结果缓存下来、写到相关表就自动失效"收敛成显式两行。
> 与 gdb 的差别:gdb 的失效是"写 Model 自己挂 `Cache{Duration:-1}`"显式清,
> 这里是**表标签 + GORM 写回调自动失效**,语义更强。

## 功能

- **干跑建键**:同一查询链 `DryRun` 构建出 SQL + 参数再指纹化 —— 条件一变
  key 就变,不存在"改了参数拿到旧结果";干跑静音(logger.Discard),不污染观测;
- **表标签失效**:tag 表上的 Create/Update/Delete 回调按索引反查删 key;
  跨表查询把 tag 全传进来,写到任何一张都失效;
- **事务短路**:事务内(`gorm.TxCommitter` 探测)一律直查 —— 照抄 gdb,
  "缓存配置不改变业务语义";
- **空结果也缓存**(防穿透,空集就是这条查询的真实值);
- **无后端退化直查**:cachex 未启用 / `Use(nil)` / 建键失败,查询照常,
  绝不把查询变成失败;
- **命中观测**:`q.Hit()` 返回最近一次 Find 是否命中。

## 使用方法

装配一次(后端选择 + 失效回调,片段出自 [cmd/server/main.go](../../cmd/server/main.go)):

```go
	qcSafe := cachex.NewWithBackend(cachex.NewMemoryBackend())
	if cachePool != nil {
		if s := cachePool.Safe(); s != nil {
			qcSafe = s
		}
	}
	querycache.Use(qcSafe)
	if err := querycache.Install(db); err != nil {
		log.Warn("querycache install failed; 查询缓存不生效(查询仍直查)", zap.Error(err))
	}
```

有 Redis 用共享 L2(写失效跨实例可见);没有退进程内 memory —— 与 cachex
l1 模式同哲学,**多实例失效收敛上界 = TTL**。

消费(片段出自 [service/role_impl.go](../../internal/service/role_impl.go),
资源授权树是第一个接入点 —— 读多写极少):

```go
	q := querycache.On(s.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("sort ASC, id ASC"), resourceTreeCacheTTL, "sys_resource")
	if err := q.Find(ctx, &rows); err != nil {
```

跑最小示例(内存 sqlite,SELECT 可见:第二次查询不打库,UPDATE 后重新打库):

```bash
go run ./examples/querycache
```

平台内已接入(读多写少 + 写路径全走 GORM,失效闭环成立):

- 资源授权树([role_impl.go](../../internal/service/role_impl.go) ResourceTree,tag `sys_resource`)
- 角色可见资源([auth_impl.go](../../internal/service/auth_impl.go) resourcesForRole —— 全后台最热的查询,
  每次页面加载的菜单 / 按钮都走它;JOIN 查询挂双 tag `sys_resource` + `sys_role_resource`)
- 部门树(role_impl.go DepartmentTree,tag `sys_department`;leader_name /
  member_count 是 Find 后新鲜回查,不在缓存里,不会脏)
- 字典已有 DictCache(按点缓存),不再叠加查询缓存

## 失效的边界(如实说)

- **裸 SQL 写路径打不着回调**(`db.Exec` 没有 `Statement.Table`)—— 与
  [auditx](../audit/auditx.md) 的 dyn_* 同款坑,补救通道是显式
  `querycache.Invalidate(ctx, "表")`;dyn 的记录写路径(增删改 / 导入 /
  批量)已预接了 `Invalidate` —— 现在没有 dyn 查询接缓存所以是 no-op,
  以后哪条 dyn 查询挂了缓存标签,失效天然闭环;
- **索引登记是读改写,并发有丢更新窗口** —— 失效尽力而为,TTL 兜底,
  所以 TTL 要按"脏几分钟可接受"来定;
- **只支持 Find 形状的 SELECT**(整个结果集按 JSON 存);Count / 聚合 /
  关联预加载不在此列。

## 关键纪律

- **opt-in**:没有 `Install` / 没有 `On` 的查询行为完全不变 —— 缓存不当
  造成的"改了数据没生效"以低概率出现,而 opt-in 让每个用缓存的人显式背责任;
- **事务内不缓存**:事务还没提交的东西不能被别人看见;
- **TTL 按"脏几分钟可接受"定**(不是越大越好):TTL 是所有失效漏洞的兜底;
- **多表查询 tag 全传**:`On(db, ttl, "a", "b")` —— 漏一个 tag 就是一条
  失效盲区;
- **db 必须是未执行的查询链**,Find 由本包收尾;链上先 `Find` 再传进来
  拿到的是已执行完的 statement,指纹就是错的。
