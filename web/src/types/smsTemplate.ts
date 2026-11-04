/**
 * 短信模板审核状态 类型 —— M115 配套
 * 2026-10-30 新建,对齐后端 server/internal/service/sms_template.go 的 View DTO
 *
 * # 这个页面在看什么
 *
 *   模板在短信服务商那边**过没过审**,平台只有主动去问才知道。
 *   阿里云拒发时只给一句 isv.SMS_TEMPLATE_ILLEGAL,用户看到的是"发送失败" ——
 *   所以平台侧维护一本"登记册"(pkg/smstpl),由后台页面看、由守卫用。
 *
 * # ⚠️ 状态字面量是**后端定的**,前端不重新定义一套
 *
 *   后端 pkg/smstpl.Status 有 7 个字面量,与 seed 的字典组 `sms_template_status`
 *   一一对应(unknown / pending / reviewing / approved / rejected / cancelled / disabled)。
 *   ⚠️ 所以状态**标签与颜色走字典**(useDict('sms_template_status')),
 *   前端**不许**自己写一张 status → 文案/颜色的映射表 ——
 *   那会跟字典漂移,而字典是运营能改的(下线一个状态、加一个新状态)。
 *
 *   类型上仍然用 `| string` 兜底:后端加一个状态而前端没跟上时,
 *   页面显示成字典里查不到的值(灰标签),**而不是白屏**。
 */

/**
 * 审核状态(后端 pkg/smstpl.Status 的字面量)
 *
 * ⚠️ 末尾的 `| string` 不是"随便写写" —— 是**故意**的:
 * 后端加状态时前端不该编译失败(那会逼着人乱改类型),
 * 页面显示成字典查不到的灰标签即可(状态仍然看得见,只是没有专门配色)。
 */
export type SmsTemplateStatus =
  'unknown' | 'pending' | 'reviewing' | 'approved' | 'rejected' | 'cancelled' | 'disabled' | string

/** 状态来源:manual = 人工点的;provider = 服务商同步回来的 */
export type SmsTemplateSource = 'manual' | 'provider' | string

/**
 * 守卫策略(off / warn / block)
 *
 * ⚠️ 这是**进程级**的开关,不是每个场景一份 —— 挡的是"这张模板确定发不出去"。
 * 后端 smstpl.ParsePolicy 解析,配置项是 sms.template_policy。
 */
export type SmsTemplatePolicy = 'off' | 'warn' | 'block' | string

/** 单个场景的模板状态(后端 SmsTemplateView) */
export interface SmsTemplateItem {
  /** 场景标识(login / reset_password / ...) */
  scene: string
  /**
   * 场景中文名(后端 smstpl.SceneLabel 返的)。
   *
   * ⚠️ **页面不用它** —— 那是写死的中文,英文环境下必然露馅。
   * 前端走 `@/constants/sms` 的 SMS_SCENE_LABEL_KEY + i18n(见 smsScene 组)。
   * 这个字段留着是为了调试时能对照,以及后端其它调用方需要。
   */
  scene_label: string

  /** 当前生效的模板号(配置中心那份) */
  template_code: string
  /** 登记册里登记的模板号(上一次同步时的快照) */
  registered_code: string
  /**
   * 两个号不一致 —— 登记册里的审核结论**不适用于当前这个号**。
   *
   * ⚠️ 这时候守卫按"不确定"放行(见 pkg/smstpl.Guard.Evaluate 第 3 条口径),
   * 所以这一列必须显眼:它意味着"你现在看到的审核结论其实是旧号的"。
   */
  code_mismatch: boolean
  /** 配置中心里配了模板号 */
  configured: boolean
  /** 登记册里有这一行(同步过一次) */
  registered: boolean

  /** 审核状态 —— 标签与颜色走字典 sms_template_status */
  status: SmsTemplateStatus
  /** 服务商给的备注 / 驳回理由,或人工填的原因 */
  reason: string
  /** 当前状态是谁给的 */
  source: SmsTemplateSource

  /** 确定发不出去(守卫会拦:rejected / cancelled / disabled) */
  blocked: boolean
  /** 审核通过(这张模板可以用) */
  usable: boolean

  /** 服务商最后一次确认这个状态的时间(RFC3339,空串 = 从未确认过) */
  reviewed_at: string
  /** 登记册这一行最后变动的时间(RFC3339) */
  updated_at: string

  /**
   * 上一轮同步这个场景**失败的原因**(空串 = 上一轮没失败)
   *
   * ⚠️ **为什么要有这个字段** —— 页面上只显示状态("未同步")的话,
   *   运维看到的是一片"未同步",**却不知道为什么**。
   *   有了它,页面能直接说"上次同步失败:模板号在阿里云不存在"这种话。
   *
   * ⚠️ **只保留最近一轮** —— 不做历史累积(见 smstpl.Runtime.LastFailure)。
   */
  last_error: string
}

/**
 * 列表响应(后端 SmsTemplateListView)
 *
 * ⚠️ **不是裸数组** —— 策略与通道是进程级状态,每个场景一样,
 * 放数组里就得让前端去 items[0] 里取(空数组时取不到)。
 */
export interface SmsTemplateListView {
  items: SmsTemplateItem[]
  /** 守卫策略 off / warn / block */
  policy: SmsTemplatePolicy
  /** 当前生效的短信通道(console 表示这个通道压根没有模板可审) */
  provider: string
}

/** 一条流转历史(后端 SmsTemplateHistoryView) */
export interface SmsTemplateHistoryItem {
  scene: string
  scene_label: string
  /** 这次流转对应的模板号(换号之后老记录上的号是不同的) */
  template_code: string
  /** 流转前的状态 */
  from: string
  /** 流转后的状态 */
  to: string
  /** manual / provider */
  source: SmsTemplateSource
  /** 操作人(人工流转时 = 运维的登录名;服务商流转时为空) */
  operator: string
  /** 原因 / 备注 */
  reason: string
  /** 发生时间(RFC3339) */
  at: string
}

/**
 * 一轮同步的结果(后端 SmsTemplateSyncView)
 *
 * ⚠️ `failed` 里混着两种东西:服务商查询**失败**,和状态机按规则**拒绝流转**
 * (人工停用的模板,服务商说通过 —— 硬规则 2 在起作用,不是故障)。
 * 所以前端提示不能写成"失败",要说"没能确定状态"。
 */
export interface SmsTemplateSyncResult {
  /** 模板号变了(旧状态被作废)的场景 */
  registered: string[]
  /** 状态变了(拿到新结论)的场景 */
  updated: string[]
  /** 状态没变的场景数 */
  unchanged: number
  /** 拉取失败 / 状态被拒绝的场景(状态保持原样) */
  failed: string[]
  /**
   * 每个失败场景的**原因**
   *
   * ⚠️ **以前只有场景名,没有原因** —— 前端只能弹一句
   *   "没能确定状态:login,reset_password",运维还是不知道为什么。
   *   加上原因之后,提示里能带上一句人话。
   */
  failures?: SmsTemplateSyncFailure[]
}

/** 一个场景的同步失败 */
export interface SmsTemplateSyncFailure {
  /** 场景码 */
  scene: string
  /** 失败原因(人话) */
  reason: string
}

/** 停用 / 恢复弹窗的表单(只有"原因"一个字段) */
export interface SmsTemplateActionForm {
  /** 原因(可选)—— 会写进流转历史,排障时全靠它 */
  reason?: string
}
