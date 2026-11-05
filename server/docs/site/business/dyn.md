# dynx —— 动态业务集合(用户自己设计业务表)

> 让**不懂代码的人**在后台里设计一张业务表(比如"门店巡检记录"),
> 设计完就自动有:物理表、增删改查接口、菜单、权限码、列表管理页。
> 以后再要一张表,还是点几下 —— 不用开发改代码 + 建表 + 写接口 + 加菜单 + 加权限。
>
> ⚠️ **这是本仓唯一一处"业务代码不写也能上线新业务"的机制** ——
> 其余模块都是框架原语,这一条是**业务能力本身**。

## 功能

### 两个面

| 面 | 谁在用 | 路径前缀 | 资源码形状 |
|---|---|---|---|
| **设计面** | 管理员(设计业务表) | `/rapid-admin/v1/system/dyn/**` | `dyn:design:collection:*` / `dyn:design:field:*` / `dyn:design:type:list`(四段) |
| **数据面** | 业务人员(录数据) | `/rapid-admin/v1/biz/dyn/:collection/**` | `dyn:<集合编码>:<动作>`(三段) |

⚠️ **两套码形状不同,撞不了** —— 集合编码里不可能出现 `:`(规则见 `dynx.ValidateCode`),
所以数据面永远三段。
(若设计面也用三段,用户建一个叫 `collection` 的集合,
它的 `dyn:collection:list` 与设计面的"列出所有集合"就是同一个字符串 ⇒ 越权。)

### 设计完之后自动有什么

建一个集合(比如编码 `inspection`)会自动产生:

| 产物 | 形态 |
|---|---|
| 物理表 | `dyn_inspection`(列名统一带 `f_` 前缀,如 `f_device_name`) |
| 5 条 api 资源码 | `dyn:inspection:list` / `:read` / `:create` / `:update` / `:delete` |
| 1 个菜单 | `menu.dyn.inspection`,路径 `/dyn/inspection`,父节点 `menu.dyn`("自定义业务") |
| 1 个前端页面 | `/dyn/:code` —— 列表、搜索、新增、编辑、详情、删除**全自动** |

加一个字段会自动产生:一个物理列 + 一列表格 + 一个表单控件 + 一条校验规则。

### 记录页上的两样东西

| 东西 | 在哪 | 说明 |
|---|---|---|
| 「创建人」列 | 列表页最后一组固定列 | 显示名由后端读的时候从 `sys_user` 补(`owner_name`),取不到时前端回落到 `owner_id` |
| 「API 接入」按钮 | 列表页搜索区右侧 | 弹窗里写清这个集合的 6 条接口:路径 / 权限码 / 请求体的键 / 一条能跑的 curl |

⚠️ **`owner_name` 不是物理列** —— 物理表上只有 `owner_id`(创建人,建记录时写入,之后不覆盖)。
显示名是读的时候补的,所以**加这一列不需要改任何物理表**,
已经在跑的集合不需要迁移。

⚠️ **查名字失败不报错** —— 装饰性的东西取不到就让列表少一个名字,
不让整页失败(降级成空串,前端显示 `owner_id`)。

### 字段类型注册表(`internal/pkg/dynx`)

一个字段的**三种行为挂在同一张表上**:落成什么列类型(DDL)、前端提交的值怎么变成能存的值
(归一化)、存进去的值怎么变回前端能显示的值(反归一化)。

⚠️ **这三个问题必须由同一张表回答** —— 各写各的就会出现
"DDL 说它是 VARCHAR、归一化却塞了个 int64"这种分叉,
表现是"存进去看着对、查出来类型变了",而且**不报错**。
所以新增类型只加一行(`specs` 切片)。

| 类型 | 存进去的样子 | 读出来 |
|---|---|---|
| `text` / `longtext` | 文本 | 文本 |
| `int` | 整数 | 整数 |
| `number` | 小数 | 小数 |
| `bool` | `0` / `1` | 布尔 |
| `date` | `"2026-11-01"` | `"2026-11-01"` |
| `datetime` | `"2026-11-01T00:30:00Z"`(统一 UTC) | 同左 |
| `select` | 选项值文本 | 选项值文本 |
| `multiselect` | JSON 数组文本 | `string[]` |
| `json` | JSON 文本 | **已解析的 JSON 值** |
| `file` | **文件的 key**(不带域名的路径) | 同左(展示信息另放在 `_files`) |

⚠️ **`select` / `multiselect` 的选项有两种来源** ——
手工逐条填,或**关联一张系统表**(见下面的「选项来源:关联系统表」)。

### 附件字段(`file`)

字段里存的是**文件在存储树里的 key**:

```
rapid-admin/1844/dyn/0/1712345678-a1b2c3d4-户型图.pdf
└ 路径根     └ uid  └ biz └ fid └ 对象名
```

⚠️ **存 key,不存 URL** —— URL 里带域名(本地部署 `/rapid-admin/...`,
CDN 部署 `https://cdn.../rapid-admin/...`),域名会变 ⇒
存 URL 等于把"换 CDN"变成一次数据迁移。
key 是**两种 provider 下形状一致**的那一个(local 直链 = `/` + key,
OSS 直链 = `<CDN 前缀>` + `/` + key)。

⚠️ **key 的第一段是路径根词**(`file.path_root`,默认 `rapid-admin`)——
改它等于换了一棵树,已存的文件字段值会全部失效。

**写的时候**:

| 输入 | 结果 |
|---|---|
| `""` / `null` | 空(没填) |
| 带域名的 URL / 含 `..` / 含反斜杠 / 前导斜杠 / 超 512 字节 | **400** |
| 合法的 key | 原样(去首尾空白) |

⚠️ **存在性也查** —— key 查不到 ⇒ 400「文件不存在(请重新上传)」。
⚠️ **但文件服务查不动时降级放行** —— 只做形状校验
(旁路纪律:"附件服务暂时查不动"不该让整个表单存不进去)。

⚠️ **上传走文件服务(`POST /biz/files`,权限码 `file:upload`)** ——
不新造一条上传通道,也不让用户手填路径(手填 = 让用户自己声明"这个文件存在")。
前端按这个权限码控「上传」按钮的可见性(没有它点了就是 403);
⚠️ **但读是另一件事** —— 没有上传权的人照样看得见已有的文件名。

**读的时候**:列里只有 key,原文件名与直链不在列里 ⇒
后端另外解析成一个 `_files` 映射(**键是字段标识**,不是文件 key):

```json
{
  "attachment": "rapid-admin/1844/dyn/0/1712345678-户型图.pdf",
  "_files": { "attachment": { "name": "户型图.pdf", "url": "/rapid-admin/1844/dyn/0/…" } }
}
```

⚠️ **`_files` 不是字段** —— 字段标识必须以小写字母开头 ⇒ 不可能与用户字段撞名。
⚠️ **写入时忽略 `_` 开头的键** —— 「读出来 → 改两个字段 → 原样提交」是最常见的用法,
回传 `_files` 不该被当成"未知字段"拒掉。
⚠️ **解析失败要降级** —— 文件被删了那一项**不出现**在 `_files` 里,
前端显示原始 key 并标成「文件已失效」,**不让整页记录失败**。

🔴 **隐私口径(必读)** —— 字段的值就是文件的直链 ⇒
**能读这条记录的人就能拿到这个文件**。M80 的隐私模型是「私密靠 key 不可猜」,
把 key 写进一条业务记录,等于把这份不可猜性转发给这条记录的每一个读者。
⇒ 需要强私密性的场景(身份证、工资单等)不要用 `file` 字段。

### 选项来源:关联系统表

单选 / 多选的选项可以**关联一张系统表**,而不是一条一条手工填:

| key | 表 | 取值列 | 显示名列 | 需要的资源码 |
|---|---|---|---|---|
| `user` | `sys_user` | `id` | `real_name → nickname → username` | `user:list` |
| `role` | `sys_role` | `id` | `name` | `role:list` |
| `dept` | `sys_department` | `id` | `name` | `dept:list` |

⚠️ **注册表是白名单,不是"任意表"** —— 让用户填表名等于把 SQL 的表名交给用户拼。
要加表 = 在 `internal/pkg/dynx/relation.go` 的 `relationSources` 里加一行。

⚠️ **选项不落库** —— 落库的只是"从哪张表取"这一条配置
(`{"choices":[],"source":{"kind":"table","key":"user"}}`),
选项是**读表单结构的时候现查**的 ⇒ 系统表里新增一行,表单里立刻多一个选项。

⚠️ **三层闸门,顺序不能换**:

1. **资源码闸门** —— 读那张表要哪个权限(`user:list` …)。
   判定走 `middleware.CanAccess`(执行层的唯一一份实现),
   ⚠️ **判定在 handler 层做**:`internal/api/middleware` 依赖 `internal/service`,
   反过来 import 就是循环依赖 ⇒ handler 判完把结果传下去。
   ⚠️ **判定的对象是"当前调用方",不是"设计者"** ——
   设计师配了 `user` 来源,不代表录数据的业务员有 `user:list`。
2. **数据范围闸门** —— `user` 来源按 `user` 业务域套行级范围
   (与「用户管理」页同一套口径);`role` / `dept` 是平台级配置表,不套。
3. **降级闸门** —— 读不到就返回**空选项 + 一个"来源不可读"的标记**,
   **绝不让整个表单结构失败**(一个字段的选项读不到,不该让整页列表白屏)。

⇒ 反过来(先查表再判权限)等于"先偷看再问能不能看"。

⚠️ **关联表来源不校验成员资格** —— 手工选项集在写入时校验成员资格
(值必须在那份选项集里);关联表是"活的"(行会被删),
若也校验,**一条历史记录只要它引用的那一行被删了,这条记录就再也存不进去任何修改**。
⇒ 关联表来源只校验值的形状。代价:可以存进表里不存在的 id,
那一格会显示空白标签(要真的挡住得做外键 + 级联,本模块明确不做)。

## 配置

### 没有 yaml 配置块

这一套**不需要任何配置** —— 建表、登记资源、登记菜单都在运行期完成。
唯一的"配置"是设计器菜单本身(种子里的一条):

```go
// server/internal/seed/seed.go
{Code: "menu.system.dyn", Name: "业务表单设计器", Path: "/system/dyn", Icon: "table", Sort: 19, ParentCode: "menu.system"},
```

### 命名规则(硬约束,前后端都必须遵守)

| 东西 | 规则 | 来源 |
|---|---|---|
| 集合编码 `code` | `^[a-z][a-z0-9_]{0,63}$` | `dynx.ValidateCode` |
| 字段标识 `field_key` | 同上,且**不能是** `id` / `owner_id` / `created_at` / `updated_at` / `deleted_at` | `dynx.ValidateFieldKey` |
| 物理表名 | `dyn_<集合编码>` | `dynx.PhysTableName` |
| 物理列名 | `f_<字段标识>` | `dynx.ColumnName` |
| 数据面资源码 | `dyn:<集合编码>:<动作>` | `dynx.RecordCode` |

⚠️ **字段标识不能叫 `id`** —— 记录的 JSON 形状是
`{"id":…, "owner_id":…, "owner_name":…, "created_at":…, "updated_at":…, "<字段标识>": …}`,
服务层"先写系统列、再遍历字段覆盖",后写的赢 ⇒
用户字段叫 `id` 会把系统 id 盖掉,之后所有按 id 的操作全错位。

### 动作清单是唯一真相

`internal/pkg/dynx/actions.go` 里的 `Actions()` 同时被两处消费:

- service 侧建集合时按它登记资源码(`internal/service/dyn_resource.go`)
- handler 侧守卫时按它拼资源码(`internal/api/handler/v1/dyn.go`)

⚠️ **两边必须同源,否则会静默失权** —— 2026-11-01 M151 真踩过:
handler 多写了一个 `schema` 动作(想给表单结构单开一个码),service 只登记 5 个 ⇒
`dyn:<集合码>:schema` 这条资源码**永远不存在** ⇒ 角色配置页里压根没有这一行可勾 ⇒
谁也绑不上 ⇒ 表单结构接口对所有非超管一律 403 —— **而日志里一条错误都没有**。

⇒ 清单 + 拼码函数都放在原语层,两边引用同一份。

## 使用方法

### 业务人员:设计一张表

1. 后台 →「系统管理 → 业务表单设计器」(`/system/dyn`)
2. 左栏「新建集合」:填**集合编码**(小写英文,创建后不可改)、名称、图标、排序
3. 右栏「新建字段」:填**字段标识**(小写英文,创建后不可改)、显示名、类型、是否必填
4. 单选 / 多选再配**选项集**:
   **手工添加**(值 = 存进数据库的,显示名 = 给人看的)或
   **关联系统表**(选一张表当来源,选项在读的时候现查)
5. 存完 → 左侧菜单出现「自定义业务 → <集合名>」,点进去就是列表管理页

### 业务人员:录数据

「自定义业务 → <集合名>」:搜索 / 新增 / 编辑 / 详情 / 删除,列与表单全部按字段定义生成。

要把它接进自己的系统?点搜索区右侧的**「API 接入」** ——
弹窗里是这个集合的 6 条接口(路径 / 各要哪个权限码 / 请求体的键 / 一条能直接跑的 curl),
照着接就行,不用回来问开发。

### 开发:新增一种字段类型

只改一处 —— `internal/pkg/dynx/dynx.go` 的 `specs` 切片加一行:

```go
{Type: "decimal", Label: "定点小数", MySQLType: "DECIMAL(10,2)", SQLiteType: "TEXT", HasOptions: false},
```

⚠️ **MySQL 的 `number` 用 DOUBLE 而不是 DECIMAL** ——
DECIMAL 列经 go-sql-driver 读出来是 `[]byte("123.450000")`,
前端拿到的是字符串而不是数字;DOUBLE 直接给 float64。
⚠️ **json 列在两个方言里都是 TEXT** —— MySQL 的 JSON 类型要求插入的是
**合法 JSON 文档**,用户随手输入的半截 JSON 会直接报错;
TEXT 只存字符串,校验挪到归一化那一层(统一口径)。

⚠️ **新增类型还要顺手改前端的类型映射** ——
`web/src/pages/dyn/recordForm.ts` 的 `editTypeOf()`:
**未知类型一律当单行文本**(不是当"不支持"而是当文本)——
后端加了新类型而前端还不知道时,这一列至少还能显示、还能编辑字符串,
而不是整个页面炸掉(`TypeError: unknown type`)。

### 开发:接一条业务线到动态集合

不需要写代码 —— 直接在设计器里建表即可。
真要写代码(比如给某个集合加特殊校验),走的是普通 service 层,
集合数据在 `sys_dyn_collection` / `sys_dyn_field` 里,
物理表名由 `dynx.PhysTableName(code)` 算。

### 开发:用 @rapid-admin/dyn-sdk 集成自定义业务集合

设计器把表建好之后,**后端接口、权限码、菜单、列表页**都是现成的 ——
把数据接到自己的系统(小程序 / Node 脚本 / 另一个前端)不需要手写 HTTP,
用官方 SDK:`js-sdk/` 目录,发布名 **`@rapid-admin/dyn-sdk`**,零运行时依赖,
浏览器 / Node 18+ / 微信小程序(带 fetch 适配)都能跑。

#### 安装与构造

```ts
import { DynClient } from '@rapid-admin/dyn-sdk'

// ⚠️ baseURL 是 API 根路径(含 /rapid-admin/v1),不是站点根 ——
//    拼错一步,所有 404 都会指向"路由不存在",跟真正原因(baseURL)对不上。
const client = new DynClient('https://your-host/rapid-admin/v1')
```

可选构造项:`{ fetch }`(自定义 fetch 实现,老环境必备)、
`{ beforeSend }`(每个请求发出前的钩子 —— 加签名头、改路径都在这里做,
返回 `RequestInit` 表示替换,返回 `void` 表示不改)。

#### 登录:写操作之前调一次就够

```ts
// 验证码开关开启时:先拿图给用户看,输码后一起传
const cap = await client.getCaptcha()          // { id, image(base64), length, expires_in }
// ...用户看完图输入 code...

const tokens = await client.authWithPassword('<账号>', '<明文密码>', {
  captchaId: cap.id,
  captchaCode: '<用户输入>',
})
// access token 已自动挂到后续所有请求(client.authStore)
```

⚠️ **密码传明文** —— SDK 内部做前置摘要(sha256 后再发),自己先 hash 过
再传 = 双重摘要 = 永远"账号或密码错误"。

⚠️ **refresh_token 要集成方自己持久化**(`tokens.refresh_token`)——
它是**单次使用**的,access token 过期(40101)时换新:

```ts
const fresh = await client.refreshAuth(savedRefreshToken)
// fresh.refresh_token 是新的一张,**必须覆盖保存**,重放旧值直接被拒
```

登出 `client.logout()` —— 服务端吊销 + 清本地 token,**永不抛错**
(服务端拒绝对调用方没有可处理的动作,本地清掉就是登出的全部意义)。

token 存放默认**只在内存**(`client.authStore`)—— 集成环境五花八门,
SDK 不替你选 localStorage。浏览器端要跨刷新保持就监听 `authStore.onChange`
把 token 存起来、启动时 `authStore.save(saved)` 回填。

#### 集合数据面:一个实例对应一个集合

```ts
const posts = client.collection('notice')   // 集合编码 = 设计器里起的那个
// 同参数重复调用返回同一个实例
```

`RecordService` 的方法与后端路由一一对应:

| 方法 | 后端 | 说明 |
|---|---|---|
| `getSchema()` | GET `…/schema` | 集合结构(字段标识/类型/选项)。**表单要动态渲染就先调这个** |
| `getList(opts)` | GET `…/records` | 分页列表,查询参数见下 |
| `getFullList(opts)` | 同上循环 | 自动翻页拉全量(**只用于导数据/对账**,列表页请用 getList) |
| `getOne(id)` | GET `…/records/:id` | 不存在 = 404 |
| `create(body)` | POST `…/records` | 返回 `{ id }` |
| `update(id, body)` | PATCH `…/records/:id` | 只改请求里出现的字段 |
| `delete(id)` | DELETE | **软删,进回收站** |
| `restore(id)` | PUT `…/restore` | 从回收站恢复(没删过的记录恢复 = 404) |
| `submitApproval(id)` | POST `…/approval` | 送审(集合绑了审批模板才有意义) |
| `batchDelete(ids)` | POST `…/batch-delete` | 返回 `{ count }` |
| `batchUpdate(ids, field, value)` | POST `…/batch-update` | **一次只改一个字段**,改多个字段请逐条 update |

#### 查询:getList 的参数

```ts
const page = await posts.getList({
  page: 1,
  size: 20,
  keyword: '验收',                  // 模糊匹配文本字段
  filter: "status = 'active' && amount > 1000",   // 表达式筛选
  filters: { status: ['active', 'pending'] },     // 简单等值:f_status=active,pending
  orderBy: 'created_at',
  orderDir: 'desc',
  fields: ['id', 'title', 'status'],   // 只取这些字段,减少传输
  skipTotal: true,                   // 大表翻页加速(total 为 0)
  trash: false,                      // true = 看回收站
})
page.list    // 记录数组,键是字段标识
page.total
```

`filter` 表达式是与 PocketBase 同形状的子集:运算符
`= != > >= < <= ~ !~ ?=`,逻辑 `&& || ( )`,`status ?= ('a','b')` 是 IN,
`amount = null` 判空。**字段不存在会报错**(不是静默忽略)。
`filter` 与 `filters` 可以同时用。

#### 写记录:键是字段标识,值的形态有讲究

```ts
const { id } = await posts.create({
  title: '系统通知',        // 字段标识(field_key),发显示名 = 400「字段不存在」
  amount: null,             // ⚠️ 空数字是 null,不是 ""
  content: '',              // 空文本是 ""(语义是"清空")
})
await posts.update(id, { status: 'published' })   // 只发改了的字段
```

数字 / 文本 / 开关 / JSON / 多选 / 单选 / 附件各发什么 ——
**必背对应关系见本章「注意事项」的《值的形态》表**(附件存文件的 key,
不是 URL)。存完要不要走审批,看集合绑没绑审批模板,绑了就 `submitApproval(id)`。

#### 错误处理:只认 ClientResponseError

所有失败(网络错 / 非 2xx / **HTTP 200 但业务 code ≠ 0**)统一抛
`ClientResponseError`,四个字段:

```ts
import { ClientResponseError } from '@rapid-admin/dyn-sdk'

try {
  await posts.create({ title: '' })
} catch (err) {
  if (err instanceof ClientResponseError) {
    err.status   // HTTP 状态码(网络层失败 = 0)
    err.code     // 业务码(信封里的 code;非信封响应 = 0)
    err.message  // 后端 message / 网络层描述
    err.response // 原始 body(排查用)
    err.isAbort  // 请求被 AbortSignal 取消
  }
}
```

⚠️ **HTTP 403 空 body 的经典长相**是 CORS 未放行(响应体为空),
SDK 的 message 里已内置这条提示;顺带把集成方的 Origin 加进后端白名单。

#### 公开只读集合(public_read)

集合开了 `public_read` 时,**未登录状态下读动作(list / getOne / getSchema)
自动切到公开镜像路径**,登录后自动切回正常数据面 —— 调用方无感。
⚠️ 只切 GET,写动作永远走正常数据面;集合没开 public_read 时匿名读 = 401
(不区分"不存在"和"没公开",是服务端口径)。

#### 一个能跑通的完整例子

```ts
import { DynClient } from '@rapid-admin/dyn-sdk'

const client = new DynClient('https://your-host/rapid-admin/v1')
await client.authWithPassword('dev', 'your-password')

const schema = await client.collection('notice').getSchema()
console.log(schema.fields.map(f => `${f.field_key}:${f.type}`))

const { id } = await client.collection('notice').create({
  title: '停水通知',
  content: '明早 9 点至 12 点',
})
await client.collection('notice').submitApproval(id)

const { list } = await client.collection('notice')
  .getList({ filter: "status = 'published'", orderBy: 'created_at', orderDir: 'desc' })

await client.logout()
```

⚠️ **权限码是服务端判的,SDK 不吞不猜** —— 每个动作要哪个权限码
(`dyn:<集合码>:list / create / update / delete / …`)见后台该集合的
「API 接入」弹窗,403 会把业务信息原样抛出来,照着补绑定即可。

## 注意事项

### 三样东西**不可改**(前端必须锁死输入框)

| 东西 | 为什么不能改 | 想改怎么办 |
|---|---|---|
| 集合编码 `code` | 它同时是物理表名和资源码的来源,改了等于换了一张表 + 换了一套权限 | 新建一个集合,把数据搬过去 |
| 字段标识 `field_key` | 它同时是物理列名和记录 JSON 的键 | 新建一个字段,把数据搬过去,再停用旧字段 |
| 字段类型 `type` | 改类型意味着改列类型(重建表 = 全量数据搬迁 + 途中失败留半张表) | 同上 |

⚠️ **后端 DTO 里根本没有这三个字段** —— 改了会被**静默忽略**。
前端必须自己锁住输入框,否则用户填了个新编码、以为改成功了,
下次进页面又变回旧值,数据还对不上。

### 删除是软删,物理结构保留

- 删集合:集合停用、菜单与权限码一并停用;**物理表与已有数据保留**
- 删字段:字段停用,列表页与表单里不再出现;**物理列与已有数据保留**

⇒ 停用之后可以重新启用,数据还在。

### 默认值必须在**设计时**就合法

默认值是在"新建记录"时被填进去的,那时候用户已经填完一整张表了 ⇒
一个填错的默认值要等到某个人新建记录时才暴露,那时候已经没人记得是谁填的。

⇒ 建字段 / 改字段时,后端拿**写记录同一套规则**(`dynx.Normalize`)校验默认值,
不合法直接 400(前端弹 toast)。空串 = 没有默认值,不校验。

### 时间口径:不带时区的输入按**东八区**解释

- `datetime` 存成 UTC 的 ISO-8601(`"2026-11-01T00:30:00Z"`)
- 用户在表单里填 `"2026-11-01 08:30:00"` ⇒ 按**东八区**解释 ⇒ 存成 UTC

⚠️ **前端不能自己换算** —— 一旦前端把本地时间转成 UTC 再发,
遇上"浏览器时区 ≠ 东八区"就整整差 8 小时,而且**当场不报错**
(存进去了,只是时间不对)。⇒ 前端的 `datetime` 字段用**文本框**,
让用户按后端认得的格式直接填;`date` 没有时区问题,可以用日期选择器。

### 值的形态:几个必背的对应关系

| 场景 | 前端发什么 | 后端怎么接 |
|---|---|---|
| 空数字 | `null`(**不是 `""`**) | `""` 会被 `toInt64` 拒:"需要整数,收到 \"\"" |
| 空文本 | `""` | 存空串(这是"清空"而不是"不填") |
| 开关 | 布尔 | 认不出来的字符串**报错**(不静默当 false) |
| JSON | 文本 | 文本必须**是合法 JSON**,否则 400 |
| 多选 | `string[]` | 落库是 JSON 数组文本 |
| 单选 | 选项值 | 手工来源:必须在选项集里,否则 400;关联表来源:只校验形状 |
| 附件 | **文件的 key** | 带域名 / `..` / 反斜杠 ⇒ 400;查不到文件 ⇒ 400「文件不存在」 |

### "没填"是 **NULL**,不是零值

⚠️ **字段列必须是可空的** —— 这是整个模块的一条硬约定,别改成 `NOT NULL`。

`dynx` 的读侧与写侧都按「NULL = 没填」设计:

- 写:`Normalize(nil)` → `(nil, nil)` —— "没填"与"填了非法值"是两件事
- 读:`Denormalize(nil)` → `nil` ⇒ 界面显示"未填"

⇒ **字段列必须可空**,否则"没填"这一路直接撞数据库约束:

```
NOT NULL constraint failed: dyn_inspection.f_count
```

症状是**用户清空一个选填的整数框就存不进去**(前端发的就是 `null`),
而后端返回的是 500 "更新记录失败" —— 报错说的是"更新失败",
跟"这个框不能清空"八竿子打不着。

⇒ 业务上的理由:**金额 0 元 ≠ 没填金额**。
零值顶替 NULL 的话,列表里那条"没填金额"的记录看起来就是"金额 0 元"。

⚠️ **NULL 文本列是会真实出现的** —— 给一个**已有数据**的集合
**新加字段**时,老行的这一列就是 NULL(新列可空 ⇒ 加列时老行填 NULL)。

⇒ 关键字搜索那一组因此必须用 `COALESCE(col, '')`(见
`server/internal/service/dyn_records.go` 的 `recordWhere`),
否则 `col LIKE ?` 在 NULL 上求值是 NULL 而不是 false(SQL 三值逻辑)。

⚠️ **老表改不动** —— 本模块上线**之前**建的物理表,列是 `NOT NULL`,
而 SQLite **不支持** `ALTER COLUMN` 改可空性(MySQL 可以
`ALTER TABLE dyn_xxx MODIFY COLUMN f_yyy <type> NULL`),
SQLite 只能重建表。

⇒ 而"删集合再重建"这条路也走不通:删集合是**软删**
(`sys_dyn_collection` 的行还在,物理表也还在),
同一个编码再建会撞唯一约束返 409 "集合编码已存在"。
⇒ **本模块还没上线 ⇒ 老的动态集合直接换编码重建一个**(旧表是野表,
没有别的手段能删 —— `dropTable` 只在"建表失败要补偿"时才会被调用)。

### 权限:数据面的码是**运行期**拼出来的

`dyn:<集合编码>:<动作>` 的集合编码是用户起名、运行期才有的 ⇒ **编译期枚举不了**。

- 后端:守卫按集合判定(`dynx.RecordModule(code)` + `middleware.CanAccess`)
- 前端:权限 store 里装的就是后端下发的 api 资源码,**含运行期生成的那几条**
  ⇒ `has('dyn:' + code + ':create')` 在运行期是真的能判的
  (见 `web/src/api/dyn.ts` 的 `dynRecordPerm()`)

⇒ 前端的 `/dyn/:code` 路由**刻意不加** `RoutePermissionGuard` ——
写死的 `anyOf` 表达不了"这个集合能不能看"。闸门在后端,前端只控按钮可见性。

### 其它几条容易踩的

- **关键字搜索只搜文本类字段** —— 数字 / 日期 / 开关不参与 LIKE 匹配
- **排序字段必须在已知集合里** —— 白名单校验,排序字段会拼进 SQL
- **owner_scoped 的集合**:普通业务人员只看得见自己创建的记录(超管不受限)
- **同标识的软删字段会被"复活"而不是新建一行** —— 唯一索引 `(collection_id, field_key)`
  覆盖软删行;复活的前提是**类型一致**

## 相关

| 位置 | 内容 |
|---|---|
| `server/internal/pkg/dynx/` | 字段类型注册表 + 归一化 / 反归一化 + 命名规则 + 动作清单 |
| `server/internal/pkg/dynx/relation.go` | **选项来源注册表**(关联系统表白名单) |
| `server/internal/service/dyn.go` | DTO 与 service 接口 |
| `server/internal/service/dyn_impl.go` | 集合 / 字段的增删改查(含默认值与选项集校验) |
| `server/internal/service/dyn_records.go` | 记录的数据面 CRUD(含 `_files` 附件解析) |
| `server/internal/service/dyn_relation.go` | **关联表来源的选项解析**(权限 → 数据范围 → 查表) |
| `server/internal/service/dyn_resource.go` | 运行期登记资源码与菜单 |
| `server/internal/api/handler/v1/dyn.go` | HTTP handler + 守卫 + `RelationSources` |
| `server/examples/dynx/main.go` | 可运行示例 |
| `web/src/pages/system/dyn/` | 业务表单设计器(前端) |
| `web/src/pages/system/dyn/OptionsEditor.tsx` | 选项集编辑器(手工 / 关联表二选一) |
| `web/src/pages/dyn/` | 通用记录列表页(前端) |
| `web/src/pages/dyn/recordForm.ts` | 值 ⇄ 表单 的纯规则(有单测) |
| `web/src/pages/dyn/FileFieldControl.tsx` | 附件控件(上传 / 显示 / 清除) |
| `web/src/pages/dyn/apiGuide.ts` | 「API 接入」说明的**内容生成**(纯函数,有单测) |
| `web/src/pages/dyn/ApiGuideModal.tsx` | 「API 接入」弹窗(组件层) |
| `web/src/api/dyn.ts` | 前端 API + 选项集翻译 + 运行期权限码 |
| `js-sdk/`(`@rapid-admin/dyn-sdk`) | **二次开发集成的官方 SDK**(零依赖;用法见上文「用 @rapid-admin/dyn-sdk 集成自定义业务集合」) |
| [包速查表](../reference/packages.md) | 56 个 Go 包一句话索引 |
