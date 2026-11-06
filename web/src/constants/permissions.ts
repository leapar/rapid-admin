/**
 * 权限码常量 —— 与后端 server/internal/seed/seed.go resourceList() 一致
 *
 * 格式: <module>:<action>  (两段式,与后端保持一致)
 * 前端用 usePermission(perm) / <Permission perm="..."> 判断
 *
 * 重要:
 *   1. 前端不要直接判断角色,统一判断权限码
 *   2. 新增/删除时务必同步后端 seed.go resourceList()
 *   3. 按钮级 vs 路由级共用同一份 code(不再区分 *_LIST / *_DETAIL 命名)
 *
 * 历史:Phase 5 M2 把三段式 system:user:list / owner:err_correction / system:config:view
 *   等旧命名统一改为后端实际 code;删 system: / data_change 等自创前缀。
 */

export const PermSystem = {
  // user
  USER_LIST: 'user:list',
  USER_READ: 'user:read',
  USER_CREATE: 'user:create',
  USER_UPDATE: 'user:update',
  USER_DELETE: 'user:delete',
  USER_UNLOCK: 'user:unlock',
  USER_TOGGLE: 'user:toggle',
  // role
  ROLE_LIST: 'role:list',
  ROLE_CREATE: 'role:create',
  ROLE_UPDATE: 'role:update',
  ROLE_DELETE: 'role:delete',
  ROLE_ASSIGN: 'role:assign',
  // resource / permission
  RESOURCE_LIST: 'resource:list',
  RESOURCE_TREE: 'resource:tree',
  RESOURCE_ACCESSIBLE: 'resource:accessible',
  RESOURCE_CREATE: 'resource:create',
  RESOURCE_UPDATE: 'resource:update',
  RESOURCE_DELETE: 'resource:delete',
  // dept
  DEPT_LIST: 'dept:list',
  DEPT_TREE: 'dept:tree',
  DEPT_CREATE: 'dept:create',
  DEPT_UPDATE: 'dept:update',
  DEPT_DELETE: 'dept:delete',
  // dict
  DICT_LIST: 'dict:list',
  DICT_CREATE: 'dict:create',
  DICT_UPDATE: 'dict:update',
  DICT_DELETE: 'dict:delete',
  // config (按后端实际 code,view→read, change→create+update, review→approve, rollback→cancel)
  CONFIG_LIST: 'config:list',
  CONFIG_READ: 'config:read',
  CONFIG_CREATE: 'config:create',
  CONFIG_UPDATE: 'config:update',
  CONFIG_DELETE: 'config:delete',
  CONFIG_APPROVE: 'config:approve',
  CONFIG_CANCEL: 'config:cancel',
  CONFIG_ACTIVATE: 'config:activate',
  CONFIG_AUDIT: 'config:audit',
  CONFIG_HEALTH: 'config:health',
  CONFIG_STATS: 'config:stats',
  CONFIG_WATCH: 'config:watch',
  // job(M68 定时任务)。与后端 seed resourceList 一一对应,改一边必须同步另一边。
  //   没有 create / delete —— 任务本体只在代码里 cronx.Register,DB 只有运行态覆盖。
  //   RUN_READ 独立于 READ:sys_job_run 里有 trace_id / 错误堆栈,
  //   将来要放开给运维时可以只给「看执行记录」而不给「手动触发」。
  JOB_LIST: 'job:list',
  JOB_READ: 'job:read',
  JOB_UPDATE: 'job:update',
  JOB_TRIGGER: 'job:trigger',
  JOB_RUN_READ: 'job:run:read',
  // auth(自助权限,所有登录用户都该有)
  AUTH_UPDATE_PROFILE: 'auth:update-profile',
  AUTH_SWITCH_IDENTITY: 'auth:switch-identity',
} as const

/**
 * 文件 / 文件夹(M80 文件系统)。与后端 seed resourceList 一一对应。
 *
 * ⚠️ 没有 `folder:update`:改文件夹名/移动不在首期范围(文件夹是扁平的,
 * 不支持嵌套 —— 见设计文档 §2)。
 *
 * ⚠️ `FOLDER_UPLOAD` 不是"往文件夹里传文件"(那是 FILE_UPLOAD),而是
 * "把文件夹里的某个文件设为封面"。封面按**文件夹一张**存,所以它得是
 * 写文件夹的动作,权限归 folder 组。
 */
export const PermFile = {
  LIST: 'file:list',
  READ: 'file:read',
  UPLOAD: 'file:upload',
  DELETE: 'file:delete',
  FOLDER_LIST: 'folder:list',
  FOLDER_CREATE: 'folder:create',
  FOLDER_DELETE: 'folder:delete',
  FOLDER_UPLOAD: 'folder:upload',
} as const

/** 审计(后端 data_change→data) */
export const PermAudit = {
  OPERATION_LIST: 'audit:operation:list',
  OPERATION_READ: 'audit:operation:read',
  DATA_LIST: 'audit:data:list',
  DATA_READ: 'audit:data:read',
  LOGIN_LIST: 'audit:login:list',
  LOGIN_READ: 'audit:login:read',
  /** 系统运行日志(2026-11-03):慢 SQL / 慢接口 / 5xx / panic */
  SYSTEM_LIST: 'audit:system:list',
  SYSTEM_READ: 'audit:system:read',
  FULL_INFO_LIST: 'audit:full-info:list',
  FULL_INFO_READ: 'audit:full-info:read',
  EXPORT_LIST: 'audit:export:list',
  EXPORT_READ: 'audit:export:read',
  EXPORT_CREATE: 'audit:export:create',
} as const

/** 供应商 */
export const PermSupplier = {
  LIST: 'supplier:list',
  READ: 'supplier:read',
  CREATE: 'supplier:create',
  UPDATE: 'supplier:update',
  DELETE: 'supplier:delete',
  AUDIT: 'supplier:audit',
} as const

/** 家庭授权(数字家园) */
export const PermFamily = {
  LIST: 'family:list',
  READ: 'family:read',
  CREATE: 'family:create',
  UPDATE: 'family:update',
  DELETE: 'family:delete',
} as const

/** 微信模板 */
export const PermWxTemplate = {
  LIST: 'wxtemplate:list',
  READ: 'wxtemplate:read',
  CREATE: 'wxtemplate:create',
  UPDATE: 'wxtemplate:update',
  DELETE: 'wxtemplate:delete',
  SEND: 'wxtemplate:send',
} as const

/** 通用审批(M83)—— 流程模板配置
 *  设计文档: docs/通用审批系统设计方案V1.0.md §8.1
 *
 *  ⚠️ **写权限只有 role_super_admin 一个角色**(§8.4 D 组)。配错一个审批模板 =
 *  全平台每一条业务记录都走错审批链,危险度与改定时任务配置同一量级。
 *
 *  ⚠️ 审批单上的 6 个动作(通过/驳回/打回/取消/提交/重提)**不在这两个常量组里,
 *  也不该有** —— 它们的按钮可见性由「我手里有没有 pending task」决定,不是 RBAC。
 */
export const PermFlow = {
  LIST: 'flow:list',
  READ: 'flow:read',
  CREATE: 'flow:create',
  UPDATE: 'flow:update',
  DELETE: 'flow:delete',
} as const

/** 通用审批(M83)—— 审批单本身
 *
 *  ⚠️ **广发是安全的,因为没有 box=all**(§8.2)。列表只有 todo / mine / done
 *  三个盒子,每个都由「我和这条单的关系」定义 —— 拿到这三个码的人也只看得到
 *  自己的三盒。⚠️ 一旦前端加了「全部」列表,广发立刻变成越权。
 */
export const PermApproval = {
  LIST: 'approval:list',
  READ: 'approval:read',
  CREATE: 'approval:create',
} as const

/**
 * 短信模板审核状态(M115)—— 与后端 sms_template:* 一一对应
 *
 * ⚠️ **只绑 super_admin** —— "停用某个场景的模板" = 全平台该场景的验证码
 * **立刻发不出去**,这是平台级的止血开关,不是业务域操作。
 *
 * ⚠️ **没有 create / delete** —— 模板本体不在平台里(那是服务商的模板号),
 * 平台这边只有「这张模板现在是什么状态」。
 *
 * ⚠️ **停用与恢复共用 UPDATE 这一个码** —— 后端两个路由都挂 `update`
 * (见 server/internal/api/router/router.go 的说明:没有"能停不能恢复"的角色)。
 * 真正的**动作**粒度在 `PermButton.SMS_TEMPLATE_*` 上。
 */
export const PermSmsTemplate = {
  LIST: 'sms_template:list',
  READ: 'sms_template:read',
  UPDATE: 'sms_template:update',
  SYNC: 'sms_template:sync',
} as const

/**
 * 语音(2026-11-06)—— 与后端 tts_voice:* / tts:* / voice_clone:* 一一对应
 *
 * ⚠️ **只绑 super_admin** —— 音色登记册是平台级资产:改一条音色 = 所有
 * 用它的合成请求都换声音;克隆花钱(DashScope 计费),不该广发。
 *
 * ⚠️ **三个域、三组前缀,别混**:
 *   - tts_voice:*   音色登记册 CRUD(页面主表)
 *   - tts:convert   文本合成(试听 / 下载音频都走它)
 *   - voice_clone:* 克隆台账(上传参考音频 → DashScope 注册)
 */
export const PermVoice = {
  LIST: 'tts_voice:list',
  READ: 'tts_voice:read',
  CREATE: 'tts_voice:create',
  UPDATE: 'tts_voice:update',
  DELETE: 'tts_voice:delete',
  CONVERT: 'tts:convert',
  CLONE_LIST: 'voice_clone:list',
  CLONE_CREATE: 'voice_clone:create',
  CLONE_REFRESH: 'voice_clone:refresh',
  CLONE_DELETE: 'voice_clone:delete',
} as const

/**
 * 动态业务集合(M151)—— 与后端 seed resourceList 一一对应
 *
 * ⚠️ **这里是"设计面"的码(四段)** —— 业务表单设计器那一页用的。
 *
 * ⚠️ **数据面(某个集合的记录 CRUD)的码不在这里** ——
 * 数据面的资源码是 `dyn:<集合编码>:<动作>`,**逐集合不同**,
 * 集合编码是用户起名、运行期才存在 ⇒ 编译期没法枚举成常量。
 * ⇒ 数据面的权限由后端按集合判定,前端**不预置任何码**;
 * 通用列表页要判权限时,问后端要这个集合的实际权限集合
 * (见 server/internal/service/dyn_resource.go 的资源登记)。
 *
 * ⚠️ **为什么设计面是四段、数据面是三段** ——
 * 设计面 `dyn:design:collection:list` vs 数据面 `dyn:<集合编码>:list`。
 * 集合编码里不可能出现 ":"(规则见 dynx.ValidateCode)⇒ 数据面永远三段
 * ⇒ 两套码**形状不同,撞不了**。
 * (若设计面也用三段,用户建一个叫 "collection" 的集合,
 * 它的 "dyn:collection:list" 与设计面的"列出所有集合"就是同一个字符串 ⇒ 越权。)
 */
export const PermDyn = {
  // 集合
  COLLECTION_LIST: 'dyn:design:collection:list',
  COLLECTION_READ: 'dyn:design:collection:read',
  COLLECTION_CREATE: 'dyn:design:collection:create',
  COLLECTION_UPDATE: 'dyn:design:collection:update',
  COLLECTION_DELETE: 'dyn:design:collection:delete',
  // 字段
  FIELD_LIST: 'dyn:design:field:list',
  FIELD_CREATE: 'dyn:design:field:create',
  FIELD_UPDATE: 'dyn:design:field:update',
  FIELD_DELETE: 'dyn:design:field:delete',
  // 字段类型词表
  TYPE_LIST: 'dyn:design:type:list',
} as const

/** 按钮级权限(2026-10-18 12/15 上线对齐)
 *  命名: button.<module>.<action>
 *  与 server/internal/seed/seed.go resourceList() 中 button.* 一一对应
 *  用法: 业务页 <Button perm={PermButton.USER_EXPORT}>导出</Button>
 *       缺权限时按钮置灰 + Tooltip "无权限"
 */
export const PermButton = {
  // user
  USER_ADD: 'button.user.add',
  USER_EDIT: 'button.user.edit',
  USER_DELETE: 'button.user.delete',
  USER_EXPORT: 'button.user.export',
  USER_IMPORT: 'button.user.import',
  USER_RESET_PASSWORD: 'button.user.reset-password',
  USER_UNLOCK: 'button.user.unlock',
  USER_TOGGLE: 'button.user.toggle',
  // role
  ROLE_ADD: 'button.role.add',
  ROLE_EDIT: 'button.role.edit',
  ROLE_DELETE: 'button.role.delete',
  ROLE_ASSIGN: 'button.role.assign',
  ROLE_EXPORT: 'button.role.export',
  // resource
  RESOURCE_ADD: 'button.resource.add',
  RESOURCE_EDIT: 'button.resource.edit',
  RESOURCE_DELETE: 'button.resource.delete',
  RESOURCE_ADD_CHILD: 'button.resource.add-child',
  // dept
  DEPT_ADD: 'button.dept.add',
  DEPT_EDIT: 'button.dept.edit',
  DEPT_DELETE: 'button.dept.delete',
  DEPT_IMPORT: 'button.dept.import',
  DEPT_EXPORT: 'button.dept.export',
  // dict
  DICT_ADD: 'button.dict.add',
  DICT_EDIT: 'button.dict.edit',
  DICT_DELETE: 'button.dict.delete',
  DICT_IMPORT: 'button.dict.import',
  DICT_EXPORT: 'button.dict.export',
  // config
  //   2026-10-18 收尾:系统配置由 super_admin 独占,前端不做按钮级权限
  //   所有 CONFIG_* 常量删除(button.config.* 资源池也已删)
  // job(M68)。toggle 与 edit 分开:改频率影响生产调度,停用只是止血,
  // 授权上不该混。trigger 再单独拆 —— 能改配置不代表有权立刻跑一遍。
  JOB_EDIT: 'button.job.edit',
  JOB_TOGGLE: 'button.job.toggle',
  JOB_TRIGGER_BTN: 'button.job.trigger',
  JOB_RESET_DEFAULT: 'button.job.reset-default',
  JOB_VIEW_RUN: 'button.job.view-run',
  // sms_template(M115)。**没有 edit** —— 这张表上没有"编辑",只有四个动作。
  //   disable / enable —— 止血与恢复,分开授权:能停不能恢复的角色不成立
  //                     (一次误停就再也回不来)。
  //   sync  —— 手动拉一轮服务商的真实审核状态(与定时任务同一个入口)。
  //   history —— 看流转历史(谁在什么时候停用的)。
  SMS_TEMPLATE_DISABLE: 'button.sms_template.disable',
  SMS_TEMPLATE_ENABLE: 'button.sms_template.enable',
  SMS_TEMPLATE_SYNC: 'button.sms_template.sync',
  SMS_TEMPLATE_HISTORY: 'button.sms_template.history',
  // audit
  AUDIT_OPERATION_EXPORT: 'button.audit.operation.export',
  AUDIT_DATA_EXPORT: 'button.audit.data.export',
  AUDIT_LOGIN_EXPORT: 'button.audit.login.export',
  // flow(M83)。⚠️ **只有配置页这三个** —— 审批单上的动作按钮不设权限码,
  // 由「我手里有没有 pending task」决定(设计文档 §8.1)。
  FLOW_ADD: 'button.flow.add',
  FLOW_EDIT: 'button.flow.edit',
  FLOW_DELETE: 'button.flow.delete',
} as const

/** 全部权限码(用于前端类型推导) */
export type PermissionCode =
  | (typeof PermSystem)[keyof typeof PermSystem]
  | (typeof PermFile)[keyof typeof PermFile]
  | (typeof PermAudit)[keyof typeof PermAudit]
  | (typeof PermSupplier)[keyof typeof PermSupplier]
  | (typeof PermFamily)[keyof typeof PermFamily]
  | (typeof PermWxTemplate)[keyof typeof PermWxTemplate]
  | (typeof PermFlow)[keyof typeof PermFlow]
  | (typeof PermApproval)[keyof typeof PermApproval]
  | (typeof PermSmsTemplate)[keyof typeof PermSmsTemplate]
  | (typeof PermVoice)[keyof typeof PermVoice]
  | (typeof PermDyn)[keyof typeof PermDyn]
  | (typeof PermButton)[keyof typeof PermButton]
