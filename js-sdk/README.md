# @rapid-admin/dyn-sdk

业务表单(dyn)记录接口的 **JS SDK** —— 零运行时依赖、零 UI 依赖,对标 PocketBase js-sdk 的用法,但只覆盖 `/rapid-admin/v1/biz/dyn/:collection` 数据面。

- **零依赖**:只用 `fetch`(浏览器 / Node 18+ / 小程序适配层自带),不发 UI 组件、不发状态库,集成方不会有任何"框架冲突"问题。
- **TypeScript 优先**:自带 `.d.ts`,字段名与后端 wire 格式(snake_case)逐字对齐。
- **错误可排查**:统一抛 `ClientResponseError`,带 HTTP 状态码、业务码、`trace_id`。

## 安装

```bash
npm install @rapid-admin/dyn-sdk
```

(私有源未发布时,可以把本目录 `dist/` 拷走,或 `npm run build` 后引用。)

## 快速开始

```ts
import { DynClient } from '@rapid-admin/dyn-sdk'

// ⚠️ baseURL 是 API 根路径(含 /rapid-admin/v1),不是站点根
const client = new DynClient('https://your-host/rapid-admin/v1')

// 登录:传明文,SDK 内部做前置摘要(sha256,后端 bcrypt 比对)。
// token 自动挂到后续所有请求上
await client.authWithPassword('<账号>', '<密码>')

// 1. 先取结构(字段标识 / 类型 / 选项)—— 动态渲染表单要用
const schema = await client.collection('inspection').getSchema()

// 2. 分页列表
const page = await client.collection('inspection').getList({
  page: 1,
  size: 20,
  keyword: '消防',
  filter: "status = 'active' && amount > 1000", // 筛选表达式(与 PocketBase 同形状的子集)
})

// 3. 新建(键是字段标识 field_key,不是显示名!)
const { id } = await client.collection('inspection').create({
  title: '3 号楼巡检',
  level: 'high',
})

// 4. 更新 / 删除 / 恢复 / 送审
await client.collection('inspection').update(id, { level: 'low' })
await client.collection('inspection').delete(id)          // 软删,进回收站
await client.collection('inspection').restore(id)         // 从回收站恢复
await client.collection('inspection').submitApproval(id)  // 集合绑定审批流才有意义
```

## 登录与 token

**写操作(create / update / delete / 批量 / 送审)必须先登录**;读接口(schema / list / get)默认也要求一个有 `dyn:<code>:list` 权限的 token —— **例外见下一节「公开只读」**。集成方在第一次写调用前完成登录即可:

```ts
// 服务端开了图形验证码(captcha.login_enabled)时,先取图给用户看:
const captcha = await client.getCaptcha()
// <img src={captcha.image} /> 让用户输入后:
await client.authWithPassword('ops', '明文密码', {
  captchaId: captcha.id,
  captchaCode: '<用户输入>',
})

// 服务端没开验证码时,两步并一步:
await client.authWithPassword('ops', '明文密码')
```

- **密码只传明文**:SDK 内部做 `sha256("rapid-admin::" + 明文)` 前置摘要(与后台前端同一条契约),再不会把明文落进请求体;自己 hash 过再传 = 永远"账号或密码错误"。
- **token 自动管理**:`authWithPassword` / `refreshAuth` 成功后 token 自动入 `authStore` 并挂到后续请求;`authStore.onChange` 可以订阅到变化,要跨刷新保持就自己存一份。
- **token 过期**(`err.code === 40101`)时用登录返回的 `refresh_token` 静默换新,**不要重新要用户输密码**:

```ts
await client.refreshAuth(refreshToken) // 换出的新 refresh_token 要覆盖保存
```

  ⚠️ refresh token 是**单次使用**的,换完旧值立即作废。
- **登出**:`await client.logout()` 吊销服务端会话并清本地 token;服务端拒绝(比如 token 已过期)也不会抛错,本地照清。
- **没有做注册 / 短信登录 / 改密**:集成方账号由管理员在后台建;这些流程涉及短信 UI,不适合进零依赖 SDK。

## 公开只读(public_read)

管理员在「业务表单设计器」里给某个集合勾选**「公开只读(免登录)」**后,任何人无需登录即可读它的表单结构和记录(只读;写入仍必须登录)。SDK 会**自动**适配,不需要任何配置:

- 未登录时,`getSchema` / `getList` / `getOne` / `getFullList` 自动走公开镜像路径(`/rapid-admin/v1/public/dyn/:collection/**`);
- 登录后自动切回正常数据面(`/rapid-admin/v1/biz/dyn/:collection/**`),登出又切回来;
- 写动作无论登录与否都走正常数据面 —— 公开镜像没有写。

集合没开公开只读时,匿名读公开镜像会得到 `401`(服务端刻意不区分"集合不存在"和"没公开",不给枚举探测留信号)。

```ts
const client = new DynClient('https://your-host/rapid-admin/v1')
// notices 集合开了公开只读 —— 不登录直接读:
const list = await client.collection('notices').getList({ page: 1, size: 20 })
// 要写数据?先登录,之后的读写都走正常数据面:
await client.authWithPassword('ops', '明文密码')
```

## 方法一览

鉴权方法(路径相对 baseURL):

| 方法 | 接口 | 需要登录 |
| --- | --- | --- |
| `authWithPassword(username, password, options?)` | `POST /public/auth/login` | 否 |
| `getCaptcha()` | `GET /public/captcha` | 否 |
| `refreshAuth(refreshToken)` | `POST /public/auth/refresh` | 否 |
| `logout()` | `POST /auth/logout` | 是 |

`client.collection(code)` 返回 `RecordService`,方法与权限码(在角色配置页勾选)一一对应:

| 方法 | 接口 | 权限码 |
| --- | --- | --- |
| `getSchema()` | `GET /biz/dyn/:code/schema` | `dyn:<code>:list` |
| `getList(options?)` | `GET /biz/dyn/:code/records` | `dyn:<code>:list` |
| `getFullList(options?)` | 自动翻页拉全量(导数据用) | `dyn:<code>:list` |
| `getOne(id)` | `GET /biz/dyn/:code/records/:id` | `dyn:<code>:read` |
| `create(body)` | `POST /biz/dyn/:code/records` | `dyn:<code>:create` |
| `update(id, body)` | `PATCH /biz/dyn/:code/records/:id` | `dyn:<code>:update` |
| `delete(id)` | `DELETE /biz/dyn/:code/records/:id` | `dyn:<code>:delete` |
| `restore(id)` | `PUT /biz/dyn/:code/records/:id/restore` | `dyn:<code>:restore` |
| `submitApproval(id)` | `POST /biz/dyn/:code/records/:id/approval` | `dyn:<code>:update` |
| `batchDelete(ids)` | `POST /biz/dyn/:code/records/batch-delete` | `dyn:<code>:batch_delete` |
| `batchUpdate(ids, field, value)` | `POST /biz/dyn/:code/records/batch-update` | `dyn:<code>:batch_update` |

## 查询参数(getList)

| 参数 | 说明 |
| --- | --- |
| `page` / `size` | 分页 |
| `keyword` | 关键字,模糊匹配文本字段 |
| `filter` | 筛选表达式:`status = 'active' && amount > 1000`、`status ?= ('a','b')`、`title ~ '验收'`、`amount = null`;运算符 `= != > >= < <= ~ !~ ?=`,逻辑 `&& \|\| ( )`。**字段写错会报错,不是静默忽略** |
| `filters` | 简单等值筛选:`{ status: ['active','pending'] }` → `f_status=active,pending` |
| `orderBy` / `orderDir` | 排序字段 / `asc`·`desc` |
| `fields` | 只取这些字段,减少传输 |
| `skipTotal` | 不算总数,大表翻页更快(total 会是 0) |
| `trash` | 看回收站(软删的行) |
| `owner` | 按创建人过滤 |

## 错误处理

```ts
import { ClientResponseError } from '@rapid-admin/dyn-sdk'

try {
  await client.collection('inspection').getList()
} catch (err) {
  if (err instanceof ClientResponseError) {
    if (err.isAbort) return // 被取消的请求不是错误,别弹提示
    if (err.code === 40101) { /* token 过期 */ }
    console.error(err.status, err.code, err.message, err.traceId) // trace_id 给后端查日志
  }
}
```

一条失败有两层码:**`status`** 是 HTTP 状态码,**`code`** 是响应体业务码。两层都可能为 0(网络层失败),判错一律 `instanceof`。

## 集成注意事项

- **附件(file 字段)的值是文件 key,不是 URL** —— key 由文件服务(`POST /biz/files`)给出,直接塞进记录即可。
- **跨域(CORS)**:浏览器集成要求本站域名在服务端 CORS 白名单里。CORS 被拦的典型长相是 `HTTP 403 + 空 body`,SDK 会提示这一点,但放行白名单要在服务端改。
- **权限**:SDK 不做本地权限判定,403 时把业务码和 message 原样抛出 —— 需要哪个权限码,见方法一览表。
- **集合编码 / 字段标识都是用户在设计器里起的**,接入前先调 `getSchema()` 拿真实清单(管理后台的「API 接入」弹窗也会印出来)。

## 开发

```bash
npm install
npm test        # vitest,44 条用例(sha256 与 node:crypto 逐长度对照)
npm run build   # ESM(dist/esm)+ CJS(dist/cjs)双产物
```
