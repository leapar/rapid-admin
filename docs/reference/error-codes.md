# 业务错误码清单

> 本文档由 `server/cmd/gen-errcode-doc` 自动生成(2026-10-26)。
> 改码/改文案请直接编辑 `server/internal/pkg/errcode/errcode.go`,然后跑:
> ```
> cd server && go run ./cmd/gen-errcode-doc
> ```

## 概述

业务码 6 位整数,按段位表达语义:

| 段位 | 类别 | HTTP |
|---|---|---|
| 0 | 成功 | 200 |
| 40000-40099 / 40500-41299 / 41400-42899 / 43000-49999 | 请求参数 / 业务参数错 | 400 |
| 401xx | 未登录 / 令牌过期 | 401 |
| 403xx | 无权限 / 越权 | 403 |
| 404xx | 资源不存在 | 404 |
| 409xx | 数据冲突 | 409 |
| 413xx | 请求体过大 | 413 |
| 429xx | 限流 | 429 |
| 50000-59999(默认) | 框架 / 服务端错 | 500 |
| 501xx | 业务未实现(该换条路走,不是故障) | 501 |
| 503xx | 服务不可用(熔断 open) | 503 |
| 504xx | 网关超时 | 504 |
| 其他业务段(2xxxx / 3xxxx) | 业务语义错 | 200(由 i18n 文案呈现) |

## 状态码对照

| 业务码 | HTTP | 说明 |
|---|---|---|
| 0 | 200 | 成功 |
| 4xxxx 业务参数 | 400 | 请求参数 / 业务参数错 |
| 401xx | 401 | 未登录 / 令牌过期 |
| 403xx | 403 | 无权限 / 越权 |
| 404xx | 404 | 资源不存在 |
| 409xx | 409 | 数据冲突 |
| 413xx | 413 | 请求体过大(BodyLimit middleware) |
| 429xx | 429 | 限流(框架级 + 业务级) |
| 50000(默认) | 500 | 框架内部错 |
| 501xx | 501 | 业务未实现(该换条路走,不是故障) |
| 50300 | 503 | ServiceUnavailable(熔断 open) |
| 50400 | 504 | GatewayTimeout(per-route timeout) |

## 0 成功

| 码 | 常量名 | 默认文案 | HTTP | 说明 |
|---|---|---|---|---|
| 0 | `Success` | "ok" | 200 | - |

## 2xxxx 业务(登录 / 业主 / 资料 / 房屋 / 状态机)

| 码 | 常量名 | 默认文案 | HTTP | 说明 |
|---|---|---|---|---|
| 20100 | `LoginFailed` | "登录失败" | 200 | - |
| 20101 | `UserLocked` | "账号已锁定" | 200 | - |
| 20102 | `CaptchaWrong` | "图形验证码错误" | 200 | - |
| 20103 | `CaptchaRequired` | "请先通过图形验证码" | 200 | - |
| 20104 | `SmsTooFrequent` | "短信发送过于频繁" | 200 | - |
| 20105 | `SmsCodeInvalid` | "短信验证码错误或已过期" | 200 | - |
| 20106 | `PasswordWeak` | "密码强度不足" | 200 | - |
| 20107 | `PasswordExpired` | "密码已过期,请修改" | 200 | - |
| 20108 | `SmsSendTooFrequent` | "短信发送过于频繁,请稍后再试" | 200 | 同一 phone+scene 冷却期内重复发送 |
| 20109 | `MfaRequired` | "该账号已开启二次验证,请输入短信验证码" | 200 | MfaRequired 登录二次验证(M150):密码已验对,还要一条发到绑定手机的验证码。   ⚠️ 它**不是失败**—— 密码已经对了,流程停在"等验证码"这一步。  前端收到它要切到 MFA 输入码面板,而不是按"登录失败"计数/报错。  登录页的软锁定(fail count)不许对 20109 计数,否则开着 MFA 的用户  每次登录都被当成失败一次,五次之后被前端软锁在门外。 |
| 20200 | `SameAuditor` | "提交人与审核人不能为同一人" | 200 | 提交人与审核人相同 |
| 20201 | `SelfPromotion` | "不能给自己授予更高权限" | 200 | 给自己提权 |
| 20202 | `OverScope` | "不能授予超出自身范围的权限" | 200 | 越权授予 |
| 20203 | `StateInvalid` | "当前状态不允许此操作" | 200 | 状态机不允许 |
| 20210 | `CoOwnerCodeRequired` | "此操作需要全部共有人人脸验证" | 200 | 操作需要全部共有人人脸验证 |
| 20300 | `AssetNotReady` | "资产未就绪" | 200 | - |
| 20400 | `MaterialNotAudited` | "资料未通过审核" | 200 | - |
| 20500 | `HouseNotExists` | "房屋不存在" | 200 | - |
| 20501 | `ProjectArchive` | "项目已归档,只读" | 200 | 项目已归档 |
| 20600 | `ApprovalNoTemplate` | "该业务类型没有配置可用的审批流程" | 200 | 该 biz_type 没有可用的启用模板 |
| 20601 | `ApprovalNoAssignee` | "审批流程配置有误:解析不出审批人,请联系管理员检查流程配置" | 200 | 审批人解析为空 / 解析报错 |
| 20602 | `ApprovalDeptTreeBroken` | "部门架构数据异常(成环或上级不存在),审批无法定位主管,请联系管理员修复组织架构" | 200 | 部门树成环 / parent_id 悬空(§5.2 第 2 条) |
| 20603 | `ApprovalRuleInvalid` | "审批人规则配置有误,请联系管理员检查审批流程配置" | 200 | 规则本身配错:未知 rule_type / 角色不存在(§5.1) |

## 3xxxx 平台配置中心

| 码 | 常量名 | 默认文案 | HTTP | 说明 |
|---|---|---|---|---|
| 30100 | `ConfigNotFound` | "配置项不存在" | 200 | - |
| 30101 | `ConfigReadOnly` | "配置项为只读" | 200 | - |
| 30200 | `ConfigChangePending` | "存在未审批的变更" | 200 | - |
| 30201 | `ConfigNotAudited` | "变更未审核" | 200 | - |
| 30202 | `ConfigSelfReview` | "申请人与审批人不能为同一人" | 200 | 配置变更自审(申请人与审批人相同) |
| 30203 | `ConfigInvalidState` | "配置变更状态不允许此操作" | 200 | 配置变更状态机不允许 |

## 4xxxx 请求 / 客户端错(参数 / 认证 / 限流)

| 码 | 常量名 | 默认文案 | HTTP | 说明 |
|---|---|---|---|---|
| 40000 | `BadRequest` | "请求参数错误" | 400 | - |
| 40001 | `ParamMissing` | "必填参数缺失" | 400 | 必填参数缺失 |
| 40002 | `ParamInvalid` | "参数格式错误" | 400 | 参数格式错误 |
| 40003 | `InvalidParam` | "参数无效" | 400 | 参数无效(语义与 ParamInvalid 区分:此处是"我拒绝接受",后者是"格式不对") |
| 40100 | `Unauthorized` | "未登录或登录已过期" | 401 | - |
| 40101 | `TokenExpired` | "登录已过期" | 401 | - |
| 40102 | `TokenInvalid` | "令牌无效" | 401 | - |
| 40300 | `Forbidden` | "没有权限" | 403 | - |
| 40301 | `NoActiveIdentity` | "当前身份已停用" | 403 | 身份已停用 |
| 40302 | `OutOfScope` | "超出工作范围" | 403 | 超出工作范围 |
| 40303 | `IdorAttempt` | "无权访问该资源" | 403 | 越权访问他人资源 |
| 40400 | `NotFound` | "资源不存在" | 404 | - |
| 40900 | `Conflict` | "数据冲突" | 409 | - |
| 41300 | `PayloadTooLarge` | "请求体超过限制" | 413 | 请求体超过限制(§1.3 #16 BodyLimit) |
| 42900 | `TooManyRequests` | "请求过于频繁" | 429 | - |

## 5xxxx 框架(未实现 / 熔断 / 网关超时)

| 码 | 常量名 | 默认文案 | HTTP | 说明 |
|---|---|---|---|---|
| 50000 | `Internal` | "服务内部错误" | 500 | - |
| 50100 | `NotImplemented` | "功能未实现" | 501 | 业务尚未实现 |
| 50300 | `ServiceUnavailable` | "服务暂时不可用,请稍后重试" | 503 | 服务暂时不可用(熔断 open / 超时) |
| 50400 | `GatewayTimeout` | "请求处理超时" | 504 | 网关超时(handler 超时未写入响应) |
| 50500 | `VersionNotSupported` | "请求的 API 版本不被支持" | 500 | 请求的 API 版本不被支持(X-API-Version 超出 MaxMajor) |
| 50600 | `SignatureMissing` | "缺少接口签名" | 500 | 缺少 X-Signature header |
| 50601 | `SignatureExpired` | "接口签名已过期,请重试" | 500 | 签名 ts 超出允许的时钟漂移(防重放) |
| 50602 | `SignatureInvalid` | "接口签名校验失败" | 500 | 签名 sig 不匹配(secret 错 / 请求被篡改) |
| 50700 | `PayloadMalformed` | "请求体格式错误" | 500 | 请求体格式错误(gzip 解压失败 / JSON 损坏 / 编码不支持) |
| 50701 | `UnsupportedMediaType` | "不支持的 Content-Encoding(仅支持 gzip/identity)" | 500 | Content-Encoding 不支持(仅支持 gzip/identity) |

## 命名约定

- 段位:1xxxx=基础(成功/参数/认证/限流/框架),2xxxx=业务,3xxxx=平台配置,5xxxx=框架
- 后两位:同一语义组内递增(如 20100 系列都属登录相关)
- 文案:i18n 优先(`BIZ_CODE_KEYS` map,web/src/types/api.ts),拿不到走后端 `defaultMsg`
- 错误对象:`errcode.New(code, msg...)`,带详情用 `.WithDetail(...)`

## 添加新错误码 checklist

1. `errcode.go` 加 const(放对段位)
2. `defaultMsg` 加默认中文文案
3. 业务调用 `errcode.New(errcode.YourCode, ...).WithDetail(...)`
4. 跑 `cd server && go run ./cmd/gen-errcode-doc` 刷新本文档
5. 涉及前端 i18n 翻译,同步加 `web/src/locales/{zh-CN,en-US}.ts::bizCode` 块
