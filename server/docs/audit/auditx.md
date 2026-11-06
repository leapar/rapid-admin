# auditx —— 数据变更审计(sys_data_change_log 自动写入器)

> 「谁在什么时候把 X 改成了 Y」要当证据用,但让每个业务 service 手写
> before/after 快照必然漏。auditx 对**白名单表**注册 GORM 回调
> (create / update / delete),操作人从 ctx 取,before/after 快照落
> `sys_data_change_log` —— 业务写与审计写**同一个连接/事务**,
> 业务回滚审计行也不存在(证据一致性)。
> 与 [piiaudit](piiaudit.md)(看了什么)/ [cryptaudit](cryptaudit.md)(密钥用了没)
> 同属审计族,它回答的是**改了什么**。

## 功能

- **白名单制,不是所有表都记**:流水类表(日志 / 会话 / 验证码)再记一层是套娃;
  新增表要"当证据用"才进白名单(默认首批 8 张:`biz_project` / `biz_building` /
  `biz_house` / `biz_user` / `biz_supplier` / `contract` / `sys_user` / `sys_role`);
- **update 只记 diff**:before/after 只含**变化的列**(`{col: {old, new}}`),
  没变不记,`RowsAffected == 0` 不记;
- **凭据材料绝不进快照**:`password` / `password_hash` / `secret` / `token` 全局跳过,
  `_nonce` / `_aad` / `_hash` 后缀跳过,`created_at` / `updated_at` / `deleted_at` 跳过;
- **审计写失败不炸业务**:写 `sys_data_change_log` 失败只打 warn;
- **显式通道**:裸 SQL 写路径(GORM 回调打不着)走 `RecordChange` 手动记。

## 数据落点

表 `sys_data_change_log`(model.SysDataChangeLog):
`table_name` / `record_id` / `operation`(1 新增 / 2 修改 / 3 删除)/
`before` / `after`(JSON 文本,单值超长按 `MaxValueLen` 截断)/
`user_id` / `username`。

## 使用方法

装配一次(DB 初始化后,只对**服务运行时**的 db 调 —— 片段出自
[cmd/server/main.go](../../cmd/server/main.go)):

```go
	if err := auditx.Install(db, auditx.Config{
		LookupCaller: func(ctx context.Context) (uint64, string) {
			if c, ok := service.FromContext(ctx); ok {
				return c.UserID, c.Username
			}
			return 0, ""
		},
		Logger: log,
	}); err != nil {
		log.Warn("auditx install failed; 数据变更审计未启用", zap.Error(err))
	}
```

业务 service **一行不用改** —— 回调挂在白名单表上自动生效。
操作人来自 Auth 中间件 `WithCaller` 注入的 service ctx,取不到记 `system`
(seed / migrate / cron 的后台写记 system 是"诚实的")。

显式通道(裸 SQL / 钩子打不着的写路径 —— 片段出自
[examples/auditx/main.go](../../examples/auditx/main.go)):

```go
	// 二、RecordChange:显式通道,给裸 SQL 写路径用(GORM 回调打不着的地方)
	err = auditx.RecordChange(context.Background(), db, "demo_user", auditx.OpUpdate, 42,
		map[string]any{"status": "pending"},
		map[string]any{"status": "approved"})
```

跑最小示例(内存 sqlite,不真连):

```bash
go run ./examples/auditx
```

## 装配项(Config)

| 字段 | 说明 |
|---|---|
| `LookupCaller` | 从 ctx 取操作人;nil 或查不到 ⇒ 记 `system` |
| `Logger` | 审计写入失败 warn 用的 zap;nil = Nop |
| `Rules` | 白名单 `[]TableRule{Table / Prefix / Skip}`;nil/空 = 默认 8 张;`Prefix` 给 dyn_ 运行时建的物理表用 |
| `MaxValueLen` | 快照单值截断长度,默认 1000,截断后追加 `"…(截断)"` |

## 关键纪律

- **同事务证据一致性是底线**:审计行与业务写同一连接/事务,业务回滚
  审计行一起消失;不追求"回滚了也留痕";
- **Install 只装服务运行时的 db**:`cmd/migrate` 用自己的 db 实例不 Install,
  建表灌种子天然不被审计;
- **取证不了就放过,不硬凑**:批量写 / 取不到主键 / before 没拿到 ⇒ 不审计
  (操作日志那层有记录);审计写失败只 warn,绝不影响业务写;
- **dyn_* 裸 SQL 路径钩子打不着**,必须显式调 `RecordChange` —— 且它对
  table 空 / id=0 静默返回 nil,不会提醒你漏了;
- **防递归靠白名单天然截止**:`sys_data_change_log` 自己不在白名单里;
- **凭据 / 密钥材料绝不进快照** —— 落进 5 年期证据表的东西没有读者、只有风险。
