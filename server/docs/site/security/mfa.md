# mfa —— 登录二次验证(MFA)

> 密码之外再加一条"持有物"证明:一条发到**绑定手机号**的 6 位短信码。
> 它不是 `internal/pkg/` 下的独立包 —— 是登录链路(service 层)上的一段编排,
> 复用了 [smslimit](../messaging/smslimit.md) 的三道闸、[loginlock](loginlock.md) 的锁定、
> [smstpl](../messaging/smstpl.md) 的模板守卫和 `sys_sms_code` 表。

## 功能(逻辑原理)

### 双因子与两步登录

开启 MFA 的账号,登录从一步变两步 —— **挑战-应答**,挑战无状态:

```
第一步(挑战):  POST /public/auth/login { username, password }
                密码验对 → 自动发码(scene=mfa)→ 返回 code=20109 MFA_REQUIRED
                ⚠️ 这不是失败 —— 账号没锁、次数不计、HTTP 是 200

第二步(应答):  POST /public/auth/login { username, password, mfa_code }
                消费码 → 签发 token
```

**挑战无状态**的意思:服务端不为"第一步"保存任何会话或挑战记录 —— 第二步把
username + password + mfa_code **完整重发**即可。没有第三个凭据,没有
"挑战 id",第一步返回什么前端都不必记住(除了提示用户输码)。代价是第二步
要多传一次密码,收益是服务端零状态、重发/重试天然幂等。

### 为什么短信码是独立 scene=mfa

验证码按 scene 隔离:`scene=login` 的码给短信验证码登录用,`scene=mfa` 的码
只给二次验证用,**互不通用**。这不是洁癖 —— 如果 MFA 复用 login scene:

> 用户 A 给**自己的**手机发一条 scene=login 的码(公开接口就能发),
> 拿去填 MFA 挑战框 → 消费成功 → 二次验证形同虚设。

scene 隔离之后,MFA 码在 `LoginBySms` 路径上根本查不到 —— 短信登录和密码
+MFA 登录两条路各用各的码,谁也绕不过谁。

### 防暴力破解

MFA 码错和密码错**同罪**,走同一套闸:

| 闸 | 行为 |
|---|---|
| DB `failed_count` | +1,达 **10** 次锁 **15 分钟**(Redis 降级时也有这道闸) |
| [loginlock](loginlock.md) | 同 IP+账号维度计数(有 Redis 时) |
| 重发限流 | `POST /public/auth/mfa/send` **5 次/分钟/IP** |
| 发码三道闸 | 同 phone+scene 冷却 60s / 每分钟 1 条 / 每天 10 条(20108) |

### 图形验证码的例外

带 `mfa_code` 的第二次 Login **跳过图形码校验**(`mfa/send` 同理)。取舍:

- 图形码是一次性的,第一条登录请求已经消费掉了,挑战页重放会永远 20102;
- 这次请求已经自带密码(知识因子)+ 手机持有(持有因子)两个因子,
  且错码会计入锁定、重发有 IP 限流 —— 撞库面与密码登录等价。

### 端点一览

| 端点 | 权限码 | 限流 | 作用 |
|---|---|---|---|
| `POST /public/auth/mfa/send` | 公开(**密码先验**,锁定照算) | 5/min/IP | 挑战页重发验证码 |
| `POST /auth/mfa/enable` | `auth:account-sms` | 10/min/user | 自助开启 |
| `POST /auth/mfa/disable` | `auth:account-sms` | 10/min/user | 自助关闭(**也要验码**) |
| `POST /system/users/:id/mfa/disable` | `user:update` | — | 管理员恢复(手机丢了) |

权限码全部**复用现有资源**(`auth:account-sms` / `user:update`)——
M147 之后新增资源码要走 seed + 绑定矩阵,能复用就不新增。

错误码:`20109 MFA_REQUIRED`(要码,非失败)/ `20108` 发送过频 /
`20105` 码无效或已过期 / `20203` 状态不允许(重复开启、没开却关、没绑手机)。

## 开启后会发生什么(效果)

| 场景 | 行为 |
|---|---|
| 每次密码登录 | 多一步"输短信码",挑战页有 60s 重发倒计时 |
| 没绑手机号 | **开不了**(EnableMfa 直接拒)—— 不存在"开了却收不到码" |
| 开着但手机号后来被清 | 登录 **fail-closed**:宁可登不进也不降级成单因子,只能管理员恢复 |
| 关闭 MFA | 同样要验一条 scene=mfa 的码 —— 偷到会话的人**不能悄悄关掉**二次验证 |
| 管理员恢复 | `mfa_enabled=false` + **该用户所有会话强制下线** |
| 短信验证码登录 | 不需要 MFA(它本身就有持有因子),也消费不到 MFA 码(scene 隔离) |
| 5 个端点 | login / refresh / switch-identity / auth/me / mfa 开关都下发最新 `mfa_enabled`,前端整份替换即可 |

"关闭也要验码"不是仪式感:偷到会话的人第一件事就是把 MFA 关掉,之后再用
偷来的密码就能无阻登录。让"改安全设置"和"用安全设置"付出同样的代价。

## 配置

**零新增 yaml 配置。** 复用的既有配置:

```yaml
sms:
  rate_limit:
    window_seconds: 60    # 重发冷却(20108)
    per_minute: 1
    per_day: 10
    code_ttl: 600         # 码有效期 10 分钟(跟着阿里云模板文案走的)
```

⚠️ `templates` 里**没有也不需要 `mfa` 键** —— `resolveTemplate` 把
`mfa` 映射到 `login` 的模板(短信文案一样是"验证码,10分钟内有效")。
单独报备一张模板要过一轮阿里云审核,对部署是纯负担。

验证码存储:`sys_sms_code` 表(code + scene + phone 三者全等才可消费,
一次性 `used_at`)。**消费必须带 phone** —— M104 的教训:不带 phone 的
校验等于"任何人给自己发码就能替别人过验证"。

## 使用方法

### 后端:两步登录(curl)

```bash
# 第一步:挑战 —— 返回 {"code":20109,"message":"..."},同时手机收到码
curl -X POST /api/public/auth/login -d '{"username":"u1","password":"...","captcha_id":"...","captcha_code":"..."}'

# 第二步:应答 —— 密码原样重发,追加 mfa_code;图形码传空(见上)
curl -X POST /api/public/auth/login -d '{"username":"u1","password":"...","mfa_code":"123456","captcha_id":"","captcha_code":""}'
```

### 自助开 / 关(登录态)

```bash
# 个人中心:先发码(scene=mfa),再带码调用开关
curl -X POST /api/auth/sms/send -d '{"phone":"...","scene":"mfa","captcha_id":"...","captcha_code":"..."}'
curl -X POST /api/auth/mfa/enable  -H "Authorization: Bearer ..." -d '{"sms_code":"123456"}'
curl -X POST /api/auth/mfa/disable -H "Authorization: Bearer ..." -d '{"sms_code":"123456"}'
```

开关的状态检查(`enable == 当前 mfa_enabled` → 20203)发生在**消费码之前**
—— 重复点击不会白白烧掉一条码。

### 管理员恢复

```bash
curl -X POST /api/system/users/:id/mfa/disable -H "Authorization: Bearer ..." 
# mfa_enabled=false + invalidateUserSessions(该用户全部下线)
```

### 前端落点

| 位置 | 行为 |
|---|---|
| `web/src/pages/login/index.tsx` | 收到 20109 → 切 MFA 面板(60s 倒计时);**不进失败计数** |
| `web/src/pages/profile/index.tsx` | MFA 卡片:状态 Tag 读 `userInfo.mfa_enabled`,开/关弹窗复用绑手机的发码倒计时模式 |
| `web/src/constants/sms.ts` | `SmsScene.MFA = 'mfa'`;契约测试自动比对后端源码(scene 白名单 / 模板映射改了会红) |
| `web/src/types/api.ts` | `BizCode.MFA_REQUIRED = 20109`,标注"不是失败" |

## 注意事项

1. **20109 必须被当作"流程中间态"** —— 前端把它拦在失败计数(软锁定)
   之前,测试 `web/src/pages/login/__tests__/mfa.test.tsx` 专门断言
   `login_fail_count` 不增长。
2. **错码计入 DB failed_count** —— 别只依赖 loginlock:无 Redis 部署时
   loginlock 是内存计数(重启清零),DB 那道闸永远在。
3. **关 MFA 不吊销已有会话** —— 和改角色一样,吊销整条会话依赖
   per-user JTI 索引(已知缺口)。管理员恢复路径是显式吊销的,没有这个缺口。
4. **scene 改动要前后端同 commit** —— `web/src/constants/sms.ts` 的契约
   测试会解析后端 Go 源码比对,单边改动必红;这是设计出来的漂移网。
5. **公开集合 / 多端接入** —— js-sdk 与小程序走同样的两步登录契约;
   `POST /public/auth/mfa/send` 是限流的公开端点,不要在别的 scene 上
   叠加"免密重发"。

## 相关

- [loginlock](loginlock.md) —— 错码与错密码共用同一套锁定
- [smslimit](../messaging/smslimit.md) —— 发码三道闸
- [smstpl](../messaging/smstpl.md) —— 模板审核守卫(mfa 复用 login 模板)
- [msn](../messaging/msn.md) —— 短信通道(provider 切换)
- [auth](auth.md) —— JWT 签发 / 吊销(MFA 通过后的 token 生命周期)
