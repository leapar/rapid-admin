# casbinx —— Casbin 初始化 + 策略行格式

> 后台权限的**执行层**:一个角色能不能对某个资源做某个动作。
> 显示层(前端菜单 / 按钮)和执行层(后端 403)读的是同一份绑定矩阵。

## 功能

casbinx 负责四件事:

1. **初始化全局 enforcer** —— `Init(db, conf)`,走 gorm-adapter 读写 `casbin_rule` 表
2. **策略行格式的唯一来源** —— `PolicyRow(roleCode, resCode)` 拼出
   `[sub, dom, obj, act, eft]` 五段行
3. **策略增删** —— `AddPolicy` / `AddPolicyWithEft` / `RemovePolicy` /
   `AddRoleForUser` / `RemoveRoleForUser`
4. **运行时同步** —— `SyncRolePolicies` / `RemoveRolePolicies` 把绑定矩阵的改动
   同步进 `casbin_rule`,并 `ReloadIfInitialized` 刷新内存副本

**不解决什么**:不存绑定矩阵本身(那是 `sys_role_resource` 等业务表);
不判"这一条数据能不能看"(那是 [authz](authz.md));不负责把资源码翻译成
HTTP 路径(模型里的 `obj` 装的就是资源码)。

## 配置

### yaml 侧(`casbin:` 块)

```yaml
casbin:
  model_path: configs/casbin_rbac.conf
  auto_load: true
```

⚠️ **`auto_load` 是个死配置**:`config.CasbinConfig` 里有这个字段,但**全仓没有
任何代码读它**。真正的加载发生在 `casbinx.Init` 内部的 `enforcer.LoadPolicy()`
——启动时加载一次,之后靠 `ReloadIfInitialized()` 手动刷。

### 模型文件(`configs/casbin_rbac.conf`)

```
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act, eft

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub, p.sub, r.dom) && (r.dom == p.dom || p.dom == "global") && keyMatch(r.obj, p.obj) && (r.act == p.act || p.act == "*")
```

### 代码侧

```go
casbinx.Init(db *gorm.DB, conf string) error
casbinx.Enforce(sub, dom, obj, act string) (bool, error)
```

`Init` 用 `sync.Once` 保护 —— **第二次调用是静默 no-op**(返回 nil)。单测要重置
用 `casbinx.ResetForTest()`。

## 使用方法

### 装配与鉴权

```go
if err := casbinx.Init(db, "configs/casbin_rbac.conf"); err != nil {
    return err
}

// sub = 用户码(user_10001) 或 角色码(role_order_admin)
// dom = 域(global)
// obj = **资源码**(order:view),不是 URL 路径
// act = 动作(list / *) 
ok, err := casbinx.Enforce("user_10001", "global", "order:view", "list")
```

### 装策略

```go
casbinx.AddRoleForUser("user_10001", "role:order_admin", "global")
casbinx.AddPolicy("role:order_admin", "global", "order:view", "*")           // eft 默认 allow
casbinx.AddPolicyWithEft("role:order_admin", "global", "order:delete", "*", "deny")

casbinx.ReloadIfInitialized()  // 内存副本不会自动读库,写完必须刷
```

### 策略行格式

```go
row := casbinx.PolicyRow("order_admin", "order:view")
// → ["role:order_admin", "global", "order:view", "*", "allow"]
```

### 运行时同步(建角色 / 分配权限 / 删角色)

```go
// 必须和绑定改动走**同一个事务**
if err := tx.Transaction(func(tx *gorm.DB) error {
    // ... 改 sys_role_resource ...
    return casbinx.SyncRolePolicies(tx, roleCode, resCodes)
}); err != nil { ... }

// 事务提交之后再刷内存
casbinx.ReloadIfInitialized()
```

## 注意事项

- ⚠️ **策略行段数不对会让整个 enforcer 加载失败,不是只坏那一行**。模型是
  `p = sub, dom, obj, act, eft`(**5 段**),而 casbin 的 `AddPolicy` **不校验段数** ——
  传 4 段会写出一行 4 段的脏策略,那行在库里看着好好的,但下一次 `LoadPolicy`
  读它时报 `invalid policy rule size: expected 5, got 4` ⇒ **整个 enforcer 加载
  失败**。而 `LoadPolicy` 在进程启动时必跑一次(`Init` 里)⇒ 后果是**服务起不来**。
  用 `casbinx.AddPolicy`(自动补 `eft="allow"`)或 `AddPolicyWithEft`。
- ⚠️ **`AddPolicyWithEft` 的 `eft` 只收 `allow` / `deny`**,别的值直接报错。
  脏行会让 `LoadPolicy` 整个失败,而失败点在**进程启动**,不在写策略的那一刻 ——
  写的时候不报错,启动时才炸,现场很难还原。
- ⚠️ **`obj` 装的是资源码,不是 URL 路径**。模型用 `keyMatch` 不是 `keyMatch2`:
  keyMatch2 把 `":xxx"` 当路径参数,`"user:list"` 被编译成 `^user[^/]+$` ⇒ 绑了
  `user:list` 的角色**也能过 `user:delete` 的检查**;而 `"order:*"` 这种 deny 规则
  **永不生效**(`keyMatch2("order","order:*") = false`)。资源码里的 `:` 是字面量
  分隔符,要的是"精确匹配 + `*` 后缀通配"。
- ⚠️ **`casbin_rule` 是绑定矩阵的物化副本**,只在 seed / `cmd/migrate` 时整体重建。
  运行时「建角色 / 分配权限 / 删角色」改了 `sys_role_resource` 而 `casbin_rule`
  不动 ⇒ 后端按 casbin 判、前端按绑定显示 ⇒ **管理员在界面上勾了权限,接口照样
  403**,而且**没有任何报错**。改完绑定必须调 `SyncRolePolicies`。
- ⚠️ **`SyncRolePolicies` 必须和绑定改动走同一个事务**。绑定改动与策略行改动不同
  步 ⇒ 事务回滚了、casbin 里却留着放行行 —— 失败方向是"放行",不能要。
- ⚠️ **写完策略行必须刷新内存副本**(`ReloadIfInitialized`)—— casbin 的内存模型
  不会自动读库。而且 `ReloadIfInitialized` **必须在事务提交之后调**:事务里刷的是
  "还没提交"的库状态。
- ⚠️ **`RemovePolicy` 把 allow / deny 两行都删**。只删一半会留下另一半,而 deny 行
  的优先级高于 grant ⇒ 留着 deny 行等于这个资源永远被拒,而且**不报错**。
- ⚠️ **`PolicyRow` 是策略行格式的唯一来源**。seed 的整体重建与 service 层的运行时
  同步都调它 —— 两处各抄一份格式的话,迟早有一处改了另一处没改:格式变了而老行还在
  库里 ⇒ 老角色突然失权 / 新角色莫名有权,而且**没有任何报错**(casbin 只按行里的
  字符串匹配)。
- ⚠️ **角色删了,策略行必须一起删**(`RemoveRolePolicies`)—— 否则同 code 的新角色
  **继承旧角色的全部权限**;库里还会看着像"这角色还有几百条权限",排障时看不明白。
- ⚠️ **显式 deny 永远赢**:`e = some(where (p.eft == allow)) && !some(where (p.eft == deny))`。
  并且 **`p.dom == "global"` 时跨任意域生效**。
- ⚠️ **未初始化时 `Enforce` 返 error 而不是 panic**(`"casbin not initialized"`),
  `AddPolicy` / `RemovePolicy` 同理。调用方要么忽略要么报错 —— 别把 error 当 false
  咽下去。

## 相关

- 源码:`internal/pkg/casbinx/`
- 模型文件:`configs/casbin_rbac.conf`
- 可运行示例:`examples/casbinx/main.go` —— `go run ./examples/casbinx`
- 资源级(IDOR 那一层)是另一件事 → [authz](authz.md)
- 三层权限的分工 → [../getting-started/conventions.md](../getting-started/conventions.md)
