/**
 * 通用 API 类型 —— 与后端响应格式对齐
 * 后端统一响应格式: { code: number, message: string, data: T, request_id: string, timestamp: number }
 */

/** 后端统一响应体 */
export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data: T
  request_id: string
  timestamp: number
}

/**
 * 业务错误码(code != 0 时)
 *
 * ⚠️ **这里的值就是后端 wire 上的 `code` 字段** ——
 *   与 `server/internal/pkg/errcode/errcode.go` 的常量**逐个对齐**。
 *
 * ⚠️ **2026-10-26 修掉的一个真 bug(别改回去)**:
 *   以前这个枚举用的是**前端自己另起的一套编号**(USER_LOCKED = 20002、
 *   STATE_INVALID = 20100 ……),后端发的却是 20101、20203 ……
 *   而 `BIZ_CODE_KEYS` 用枚举值做键、`tError()` 收到的却是**后端码** ——
 *   于是**每一次查表都落空**,所有业务错误都回落成**后端那句中文**。
 *   英文用户在界面上看到的是中文,而这一切**完全静默**:不报错、不空白、
 *   测试全绿 —— 只是"翻译做完了,从来没被接上"。
 *
 *   顺带还有一个更危险的坑:前端那套编号和后端**撞号** ——
 *     前端 STATE_INVALID = 20100,后端 20100 是 LoginFailed;
 *   两套编号塞进同一张表,谁覆盖谁都说不清。
 *   现在只保留**一套**编号:后端那一套。
 *
 * ⚠️ 下面删掉的 4 个成员是**前端凭空造的码,后端从来没有发过**:
 *   · IDEMPOTENT_REPLAY(1002)—— 后端的幂等中间件用 **HTTP 响应头**
 *     `Idempotent-Replay: true` 标记重放,不是 body 里的 code;
 *   · DOUBLE_SIGN_REQUIRED(2001)—— 双签工作流已下线(配置中心的双签已移除);
 *   · CONFIG_NEED_REVIEW(2002)—— 配置中心的"待审批"是 30200 一族;
 *   · RATE_LIMIT(3001)—— 后端的限流码是 42900。
 *   它们零调用方,留着只会让人以为"后端会发这个码"。
 */
export enum BizCode {
  SUCCESS = 0,

  // 4xxxx 基础(对齐 server/internal/pkg/errcode/errcode.go)
  BAD_REQUEST = 40000,       // 请求参数错误
  PARAM_MISSING = 40001,     // 必填参数缺失
  PARAM_INVALID = 40002,     // 参数格式错误
  INVALID_PARAM = 40003,     // 参数无效(语义与 PARAM_INVALID 区分:我拒绝接受 vs 格式不对)
  UNAUTHORIZED = 40100,      // 未登录或登录已过期
  TOKEN_EXPIRED = 40101,     // 登录已过期
  TOKEN_INVALID = 40102,     // 令牌无效
  FORBIDDEN = 40300,         // 没有权限
  NO_ACTIVE_IDENTITY = 40301,// 当前身份已停用
  OUT_OF_SCOPE = 40302,      // 超出工作范围
  IDOR_ATTEMPT = 40303,      // 越权访问他人资源
  NOT_FOUND = 40400,         // 资源不存在
  PAYLOAD_TOO_LARGE = 41300, // 请求体超过限制
  CONFLICT = 40900,          // 数据冲突
  TOO_MANY_REQUESTS = 42900, // 请求过于频繁
  SERVER_ERROR = 50000,      // 服务内部错误

  // 2xxxx 业务
  LOGIN_FAILED = 20100,      // 登录失败
  USER_LOCKED = 20101,       // 账号已锁定
  CAPTCHA_WRONG = 20102,     // 图形验证码错误
  CAPTCHA_REQUIRED = 20103,  // 请先通过图形验证码
  SMS_TOO_FREQUENT = 20104,  // 短信发送过于频繁
  SMS_CODE_INVALID = 20105,  // 短信验证码错误或已过期
  PASSWORD_WEAK = 20106,     // 密码强度不足
  PASSWORD_EXPIRED = 20107,  // 密码已过期
  SMS_SEND_TOO_FREQUENT = 20108, // 同一 phone+scene 冷却期内重复发送
  MFA_REQUIRED = 20109,     // 密码已验对,还要一条发到绑定手机的短信码(⚠️ 不是失败)
  SAME_AUDITOR = 20200,      // 提交人与审核人不能为同一人
  SELF_PROMOTION = 20201,    // 不能给自己授予更高权限
  OVER_SCOPE = 20202,        // 不能授予超出自身范围的权限
  STATE_INVALID = 20203,     // 状态机不允许
  APPROVAL_NO_ASSIGNEE = 20601,    // 审批人解析为空 / 解析报错
  APPROVAL_DEPT_TREE_BROKEN = 20602, // 部门树成环 / parent_id 悬空
  APPROVAL_RULE_INVALID = 20603,   // 规则本身配错

  // 3xxxx 平台配置
  CONFIG_NOT_FOUND = 30100,     // 配置项不存在
  CONFIG_READ_ONLY = 30101,     // 配置项为只读
  CONFIG_PENDING = 30200,       // 存在未审批的变更
  CONFIG_NOT_AUDITED = 30201,   // 变更未审核
  CONFIG_SELF_REVIEW = 30202,   // 申请人与审批人不能为同一人
  CONFIG_INVALID_STATE = 30203, // 配置变更状态机不允许

  // 5xxxx 框架
  NOT_IMPLEMENTED = 50100,        // 功能未实现
  SERVICE_UNAVAILABLE = 50300,    // 服务暂时不可用,熔断 open
  GATEWAY_TIMEOUT = 50400,        // 网关超时
  VERSION_NOT_SUPPORTED = 50500,  // 请求的 API 版本不被支持
  SIGNATURE_MISSING = 50600,      // 缺少 X-Signature header
  SIGNATURE_EXPIRED = 50601,      // 签名 ts 超出允许的时钟漂移
  SIGNATURE_INVALID = 50602,      // 签名 sig 不匹配
  PAYLOAD_MALFORMED = 50700,      // 请求体格式错误(gzip 解压失败 / JSON 损坏)
  UNSUPPORTED_MEDIA_TYPE = 50701, // Content-Encoding 不支持
}

/**
 * 业务码 → i18n key 映射表
 *   - 前端只关心 user-facing 文案,内部调试用 code 数字
 *   - key 路径全部在 locales/{zh-CN,en-US}.ts 的 bizCode.* 命名空间下
 *   - 查不到时,tError() 走 fallback(后端 message)再走 key 原样
 *
 * ⚠️ **这张表的键就是后端发来的 code**(2026-10-26):
 *   `tError()` 收到的是响应 body 里的 `code` 字段,也就是后端 errcode 的值。
 *   以前这张表用前端自己那套编号做键(见 BizCode 的注释),于是**一次都查不中**。
 *   现在 BizCode 的值 = 后端码,这张表才真正对得上。
 *
 * 增删约定:
 *   - 后端 errcode.go 新增 1 个 → 这里加 1 行 + locales 加 1 条翻译
 *   - 不允许只加 code 不加翻译(会 fallback 到 key 字符串,UI 体验差)
 *   - BizCode.SUCCESS 不进这张表(它不是错误)
 */
export const BIZ_CODE_KEYS: Record<number, string> = {
  [BizCode.BAD_REQUEST]: 'bizCode.40000',
  [BizCode.PARAM_MISSING]: 'bizCode.40001',
  [BizCode.PARAM_INVALID]: 'bizCode.40002',
  [BizCode.INVALID_PARAM]: 'bizCode.40003',
  [BizCode.UNAUTHORIZED]: 'bizCode.40100',
  [BizCode.TOKEN_EXPIRED]: 'bizCode.40101',
  [BizCode.TOKEN_INVALID]: 'bizCode.40102',
  [BizCode.FORBIDDEN]: 'bizCode.40300',
  [BizCode.NO_ACTIVE_IDENTITY]: 'bizCode.40301',
  [BizCode.OUT_OF_SCOPE]: 'bizCode.40302',
  [BizCode.IDOR_ATTEMPT]: 'bizCode.40303',
  [BizCode.NOT_FOUND]: 'bizCode.40400',
  [BizCode.PAYLOAD_TOO_LARGE]: 'bizCode.41300',
  [BizCode.CONFLICT]: 'bizCode.40900',
  [BizCode.TOO_MANY_REQUESTS]: 'bizCode.42900',
  [BizCode.SERVER_ERROR]: 'bizCode.50000',

  [BizCode.LOGIN_FAILED]: 'bizCode.20100',
  [BizCode.USER_LOCKED]: 'bizCode.20101',
  [BizCode.CAPTCHA_WRONG]: 'bizCode.20102',
  [BizCode.CAPTCHA_REQUIRED]: 'bizCode.20103',
  [BizCode.SMS_TOO_FREQUENT]: 'bizCode.20104',
  [BizCode.SMS_CODE_INVALID]: 'bizCode.20105',
  [BizCode.PASSWORD_WEAK]: 'bizCode.20106',
  [BizCode.PASSWORD_EXPIRED]: 'bizCode.20107',
  [BizCode.SMS_SEND_TOO_FREQUENT]: 'bizCode.20108',
  [BizCode.MFA_REQUIRED]: 'bizCode.20109',
  [BizCode.SAME_AUDITOR]: 'bizCode.20200',
  [BizCode.SELF_PROMOTION]: 'bizCode.20201',
  [BizCode.OVER_SCOPE]: 'bizCode.20202',
  [BizCode.STATE_INVALID]: 'bizCode.20203',
  [BizCode.APPROVAL_NO_ASSIGNEE]: 'bizCode.20601',
  [BizCode.APPROVAL_DEPT_TREE_BROKEN]: 'bizCode.20602',
  [BizCode.APPROVAL_RULE_INVALID]: 'bizCode.20603',

  [BizCode.CONFIG_NOT_FOUND]: 'bizCode.30100',
  [BizCode.CONFIG_READ_ONLY]: 'bizCode.30101',
  [BizCode.CONFIG_PENDING]: 'bizCode.30200',
  [BizCode.CONFIG_NOT_AUDITED]: 'bizCode.30201',
  [BizCode.CONFIG_SELF_REVIEW]: 'bizCode.30202',
  [BizCode.CONFIG_INVALID_STATE]: 'bizCode.30203',

  [BizCode.NOT_IMPLEMENTED]: 'bizCode.50100',
  [BizCode.SERVICE_UNAVAILABLE]: 'bizCode.50300',
  [BizCode.GATEWAY_TIMEOUT]: 'bizCode.50400',
  [BizCode.VERSION_NOT_SUPPORTED]: 'bizCode.50500',
  [BizCode.SIGNATURE_MISSING]: 'bizCode.50600',
  [BizCode.SIGNATURE_EXPIRED]: 'bizCode.50601',
  [BizCode.SIGNATURE_INVALID]: 'bizCode.50602',
  [BizCode.PAYLOAD_MALFORMED]: 'bizCode.50700',
  [BizCode.UNSUPPORTED_MEDIA_TYPE]: 'bizCode.50701',
}

/**
 * 分页请求
 *
 * ⚠️ **wire 上的分页参数名是 `page` + `size`**(后端 `BaseHandler.GetPage`,
 *    server/internal/api/handler/handler.go:86 —— `c.Query("page")` /
 *    `c.Query("size")`)。**后端全仓从来不读 `page_size`**。
 *
 *    2026-10-25 统一:以前前端三处写法不一(useTable 写 `pageSize` /
 *    这里声明 `page_size` / approval 自己翻成 `size`),
 *    结果**全仓 useTable 页面的"每页条数"选择器都是失效的**
 *    (选了 50 条,后端按默认 20 返回)。现在:
 *      · `size` 是**规范名**,新代码一律用它;
 *      · `page_size` / `pageSize` 保留为**可选旧名**,出口会翻成 `size`
 *        (见 api/client.ts 的 normalizeQueryParams),老代码不用改也能跑对。
 */
export interface PageRequest {
  page: number
  /** 每页条数(wire 名)—— 后端默认 20、上限 100 */
  size?: number
  /** @deprecated 旧名,出口翻成 `size`;新代码用 `size` */
  page_size?: number
  /** @deprecated 旧名,出口翻成 `size`;新代码用 `size` */
  pageSize?: number
  keyword?: string
  sort_field?: string
  sort_order?: 'asc' | 'desc'
}

/**
 * 分页响应
 *
 * ⚠️ 后端 `response.PageData` 的字段是 **`list` / `total` / `page` / `size`**
 *    (server/internal/pkg/response/response.go:23)——
 *    以前这里声明的是 `page_size`,**后端从来没返过这个字段**。
 */
export interface PageResponse<T> {
  list: T[]
  total: number
  page: number
  size: number
}

/** 通用键值对 */
export interface KV<K extends string | number = string, V = unknown> {
  key: K
  value: V
}

/** 通用 ID + 名称(下拉框常用) */
export interface IdName {
  id: number | string
  name: string
}

/** 树节点 */
export interface TreeNode<T = unknown> {
  id: number
  parent_id: number
  name: string
  children?: TreeNode<T>[]
  data?: T
}

/** 时间范围(查询条件) */
export interface TimeRange {
  start: string // ISO
  end: string
}

/** 软删除标记 */
export interface SoftDelete {
  is_deleted: 0 | 1
  deleted_at?: string
  deleted_by?: number
}
