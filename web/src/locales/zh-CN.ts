/**
 * 中文文案
 *
 * 约定:
 * 1. key 用点分路径(如 'common.ok'),便于嵌套
 * 2. 业务页面内文案应优先用本文件
 * 3. 不在此处的兜底用 dayjs locale
 * 4. 资源菜单 namespace `resource.*` 的 key 由 `code` 派生:
 *      `user:list`              → `resource.user_list`
 *      `audit:operation:list`   → `resource.audit_operation_list`
 *      `config:read`            → `resource.config_read`
 *    派生函数:`code.replace(/[:.\-]/g, '_')`
 *    后端只管 code,前端管 i18n(关注点分离)
 */
const zhCN = {
  common: {
    // 浏览器标签页标题 —— 运行时由 main.tsx 的 LangRoot 写进 document.title。
    // index.html 里那个写死的中文只是"水合前"的默认首屏;英文用户看到的是这个。
    // ⚠️ **这是兜底值,不是产品名** —— 后端 `branding.app_title` 有值时以它为准
    //   (开源通用化:换平台名改服务端 yaml,不用动这里)。后端没配才落回这一行。
    appTitle: '后台管理平台',
    ok: '确定',
    cancel: '取消',
    save: '保存',
    submit: '提交',
    reset: '重置',
    search: '查询',
    add: '新增',
    edit: '编辑',
    delete: '删除',
    detail: '详情',
    export: '导出',
    import: '导入',
    refresh: '刷新',
    revoke: '撤回',
    enable: '启用',
    disable: '停用',
    confirm: '确认',
    success: '操作成功',
    failed: '操作失败',
    loading: '加载中...',
    noData: '暂无数据',
    // 暂无用户(2026-10-25 Module B-5c)—— 部门页"负责人"下拉的空态
    noUsers: '暂无用户',
    all: '全部',
    yes: '是',
    no: '否',
    // 通用字段标签
    phone: '手机号',
    email: '邮箱',
    name: '姓名',
    remark: '备注',
    status: '状态',
    action: '操作',
    avatar: '头像',
    createTime: '创建时间',
    updateTime: '更新时间',
    // 创建人(2026-11-02 dyn 记录页)—— 显示的是**名字**,不是 owner_id
    creator: '创建人',
    // 权限码(动态业务集合的接口要勾哪一条)
    permCode: '权限码',
    // 说明(接口清单里那一列)
    desc: '说明',
    // 选填(与 field.required「必填」成对)
    optional: '选填',
    total: '共 {n} 条',
    // 批量操作(§2.8 #14,BatchActionBar 用)
    selected: '已选 {n} 项',
    clearSelection: '取消选择',
    // 列表错误态(M7:TableErrorState / FallbackPlaceholder 用)
    loadFailed: '加载失败',
    retry: '重试',
    // 保存失败(2026-10-25 Module B-5a)—— 用户管理页 + 角色分配抽屉两处共用
    saveFailed: '保存失败',
    // 删除失败(2026-10-25 Module B-5b)—— 资源管理页用
    deleteFailed: '删除失败',
    // 关闭(2026-10-25 Module B-5b)—— 资源分配抽屉的关闭按钮 + 报修列表的行操作
    close: '关闭',
    // 计数括号(2026-10-25 Module B-5b)—— "(3 项)" 这种写法在两处出现
    countItems: '({n} 项)',
    // 值占位 / 通用动作(2026-10-25 Module B)
    // ⚠️ 这几个字面量在**好几个页面**出现(审计日志、配置审计、任务、身份编辑…),
    //   各写一份必然漏翻译 —— 所以提到 common。
    emptyValue: '(空)',
    resetDefault: '恢复默认',
    switch: '切换',
    // 状态词(2026-10-25 Module B-2)
    // 同一份文案在个人中心 / 系统配置 / 定时任务三处各写了一份中文,
    // 英文环境下全都漏翻 —— 提到 common 一处,谁用都走 t('common.xxx')。
    enabled: '已启用',
    disabled: '已停用',
    // 结果徽标(标签上的两个字)。
    // ⚠️ 跟 common.success 区分:那个是 toast 的「操作成功」,
    //    这里是徽标上的状态值 —— 两个词长得像,含义不同,别合并。
    resultSuccess: '成功',
    resultFailed: '失败',
    // placeholder —— 输入框 / 下拉框 / 日期选择器的占位提示(2026-10-25)
    // 写死中文的话,英文界面下这些提示仍是中文 —— 全仓统一走 t()。
    placeholder: {
      searchUser: '搜索用户名/昵称/手机号',
      searchRole: '搜索编码/名称',
      searchPermission: '搜索编码/名称/路径',
      searchResource: '搜索 code / 名称 / 路径',
      searchDict: '搜索字典键/值/显示名',
      searchConfig: '搜索配置键 / 配置名',
      searchConfigAudit: '搜索配置键(config_key)',
      searchDept: '搜索部门名称(支持高亮匹配)',
      searchJob: '搜索任务标识/名称/备注',
      searchApproval: '搜索标题',
      searchAuditOp: '搜索用户/IP/描述',
      searchAuditLogin: '搜索用户名',
      searchAuditChange: '搜索表名',
      // 系统日志(2026-11-03)—— keyword 命中 path / message 两列
      searchAuditSystem: '搜索路径/摘要',
      filterLevel: '按级别过滤',
      filterSource: '按来源过滤',
      searchFile: '搜索文件名',
      searchFolder: '搜索文件夹名',
      searchFlow: '搜索编码 / 名称',
      searchIssue: '搜索标题/描述',
      searchMaterial: '搜索标题/分类',
      searchNotice: '搜索标题/内容',
      searchOwner: '搜索姓名/手机/身份证',
      filterStatus: '按状态过滤',
      filterOperation: '按操作类型过滤',
      filterModule: '按模块过滤',
      filterFolder: '按文件夹过滤',
      filterType: '按类型过滤',
      filterDictGroup: '选择字典分组(默认查全部)',
      allStatus: '全部状态',
      status: '状态',
      triggerType: '触发方式',
      parentDept: '选择父部门(不选或选根 = 顶级部门)',
      topLevelDefault: '不选则为顶级',
      nickname: '请输入昵称',
      // 2026-10-25 Module B-5b:资源管理页和角色管理页的编码/名称校验提示(两处共用)
      code: '请输入编码',
      name: '请输入名称',
      realName: '请输入真实姓名',
      email: '请输入邮箱',
      oldPassword: '请输入旧密码',
      newPassword: '请输入新密码(8 位以上,含字母+数字)',
      confirmPassword: '请再次输入新密码',
      approvalTitle: '标题',
      approvalReason: '事由 / 上下文(JSON 文本)',
      selectRole: '选择角色',
      optionalDept: '可选绑部门',
      platformAll: '0 = 全平台',
      effectiveNow: '立即生效',
      forever: '永久',
      cronExpr: '0 3 * * * 或 @daily / @every 1h',
      captcha: '请输入图形验证码',
      keepUnchanged: '留空 = 保持原值',
      currentlyEmpty: '当前为空',
      selectOwner: '请选择负责人(可输入姓名搜索)',
      approvalComment: '审批意见(驳回 / 打回必填,不少于 {n} 个字)',
      rejectReason: '请写明驳回理由(不少于 {n} 个字)',
      // 2026-10-30 审计中心三页共用的时间范围选择器
      rangeStart: '开始日期',
      rangeEnd: '结束日期',
    },
    auditApprove: '审核通过',
    auditReject: '审核驳回',
    publish: '发布',
    saveSuccess: '保存成功',
  },

  // field —— 字段标签(2026-10-25)
  // 同一份标签在多个页面复用("操作"就出现在 19 个表格里),
  // 所以抽成共享组,而不是每个 page.* 下各写一份。
  // 用法:①表格列标题 ②表单字段标签(EditField.label / ViewField.label)
  //     —— 两者是同一个概念(一个数据项的名字),所以共用一组。
  // 注:"操作/状态/创建时间/更新时间/邮箱/备注/手机号"复用 common.* ——
  //     那些本来就是"通用字段标签",不必再抄一份。
  field: {
    title: '标题',
    ip: 'IP',
    type: '类型',
    sort: '排序',
    // 2026-11-02 M152 业务表单设计器 —— 字段表的「必填」列
    required: '必填',
    approver: '审批人',
    operator: '操作人',
    time: '时间',
    bizType: '业务类型',
    biz: '业务',
    index: '序号',
    name: '名称',
    priority: '优先级',
    handleTime: '处理时间',
    logAction: '动作',
    comment: '意见',
    currentStage: '当前关卡',
    initiator: '发起人',
    rolledBack: '被打回',
    submitTime: '提交时间',
    user: '用户',
    module: '模块',
    // M164 字段可见性抽屉的字段表列标题(M173 补:列标题必须落在 field.*/common.* 共享组)
    fieldName: '字段',
    visibility: '可见性',
    maskType: '打码方式',
    request: '请求',
    durationMs: '耗时(ms)',
    folderName: '文件夹名',
    /** 用户层:这个用户名下有几个文件夹 */
    folderCount: '文件夹数',
    fileCount: '文件数',
    totalSize: '总大小',
    isDefault: '默认',
    cover: '封面',
    childFolders: '子文件夹',
    fileName: '文件名',
    size: '大小',
    storage: '存储',
    uploadTime: '上传时间',
    stageName: '关卡名',
    passType: '通过方式',
    approverRule: '审批人规则',
    code: '编码',
    stageCount: '关卡数',
    source: '来源',
    description: '描述',
    // 语音管理(2026-11-06)—— 表格列标题必须落共享组(见 table-column-i18n.test)
    voiceId: '音色 ID',
    ttsType: '合成平台',
    gender: '性别类别',
    targetModel: '目标模型',
    handler: '处理人',
    category: '分类',
    file: '文件',
    version: '版本',
    reviewer: '审核人',
    reviewComment: '审核意见',
    publisher: '发布人',
    publishTime: '发布时间',
    expireTime: '过期时间',
    readCount: '阅读数',
    realName: '真实姓名',
    idCard: '身份证',
    houseCount: '房屋数',
    realNameStatus: '实名状态',
    device: '设备',
    // ⚠️ 2026-10-30 新增:登录日志的 browser / os(后端从 UA 解析出来的产品名)
    browser: '浏览器',
    os: '操作系统',
    result: '结果',
    loginTime: '登录时间',
    lastActivity: '最后活动',
    offlineTime: '下线时间',
    opTime: '操作时间',
    configKey: '配置键',
    changedBy: '变更人',
    changeDiff: '变更前 → 变更后',
    group: '分组',
    dictKey: '字典键',
    dictValue: '字典值',
    displayName: '显示名',
    color: '颜色',
    dictDescription: '说明',
    startTime: '开始时间',
    trigger: '触发',
    duration: '耗时',
    /**
     * 音视频时长(M141)
     *
     * ⚠️ **跟 `duration`("耗时")不是一回事** —— 那个是"任务跑了多久",
     *   这个是"这个视频有多长"。共用一个键的话,两个意思挤在一个词上。
     */
    mediaDuration: '时长',
    jobKey: '任务标识',
    cronExpr: '调度表达式',
    nextRun: '下次触发',
    timeout: '超时',
    resourceCode: '资源编码',
    resourceName: '资源名称',
    path: '路径',
    method: '方法',
    roleCode: '角色编码',
    roleName: '角色名称',
    builtin: '内置',
    account: '账号',
    nickname: '昵称',
    department: '部门',
    lastLogin: '最后登录',
    departmentName: '部门名称',
    departmentPath: '部门路径',
    departmentCode: '部门编码',
    leader: '负责人',
    memberCount: '成员数',
    groupCode: '分组编码',
    oldPassword: '旧密码',
    newPassword: '新密码',
    confirmPassword: '确认新密码',
    role: '角色',
    // 短信模板审核状态(M115)—— 列标题统一挂在 field 组下,
    // 见 table-column-i18n.test.ts(列标题只准用 field.* / common.*)。
    scene: '场景',
    templateCode: '模板号',
    reviewStatus: '审核状态',
    usable: '可用性',
    reason: '原因',
    reviewedAt: '确认时间',
    // 2026-10-30 —— 状态列只说"未同步",这一列说**为什么**
    lastSyncError: '上次同步失败原因',
    statusChange: '状态变化',
    // 审计中心三页(2026-10-30)—— 登录日志 / 数据变更日志 / 操作日志共用
    userId: '用户 ID',
    tableName: '表名',
    recordId: '记录 ID',
    operation: '操作类型',
    sessionId: '会话 ID',
    userAgent: 'User-Agent',
    traceId: '链路 ID',
    // 系统日志(2026-11-03)—— source 复用上面已有的 field.source;
    // durationMs 复用上面 M164 加的 field.durationMs
    level: '级别',
    requestId: '请求 ID',
    message: '结果说明',
    beforeValue: '变更前',
    afterValue: '变更后',
    // 2026-10-30 M122:用户列表的「权限」列
    // ⚠️ **必须挂在 field 组下** —— 表格列标题只准用 field.* / common.*
    //   (见 table-column-i18n.test.ts)。挂 page.user.* 会被守门网判红。
    // ⚠️ **不是** field.role —— 那个是编辑抽屉里的表单 label「角色」,
    //   这一列列的是"这个人当前有哪些角色(=有什么权限)",不是单个角色字段。
    roles: '权限',
    // 2026-11-03 M177 dyn 导入结果弹窗的错误行表 —— 列标题按 M164 约定
    // 必须挂 field.*(挂 page.dyn.records.* 会被 table-column-i18n.test.ts 判红)。
    importErrorRow: '行号',
    importErrorField: '字段',
    importErrorMessage: '错误',
    // 2026-11-03 dyn 记录列表的审批状态列(列标题,须挂 field.*)
    approvalStatus: '审批状态',
  },
  // 共享组件文案(2026-10-25 Module B)
  // 这些组件被多个页面复用,文案也跟着复用 —— 挂到 page.* 下会各写一份,
  // 改一句得改 N 个页面,漏一处就有一处界面漏翻译。
  component: {
    captcha: {
      refreshAria: '刷新图形验证码',
      refreshTitle: '点击刷新',
    },
    columnSettings: {
      title: '列设置',
      done: '完成',
      width: '宽度',
      storageHint: '列设置保存在浏览器 localStorage(同浏览器同 key 自动恢复)',
    },
    configDiff: {
      title: '变更对比',
      sensitiveHint: '该字段标记为敏感,值已脱敏展示,实际值仅在审批通过后生效',
      old: '原',
      new: '新',
    },
    identityEditor: {
      title: '多身份编辑',
      readOnly: '只读',
      add: '新增身份',
      roleRequired: '每条身份必须选择角色',
      primaryRequired: '必须有且只有 1 条主身份',
      loading: '加载角色/部门...',
      emptyHint: "尚未配置任何身份,点击右上角 '新增身份' 开始",
      emptyReadOnly: '此用户暂无身份',
      primaryBadge: '⭐ 主身份',
      alreadyPrimary: '已是主身份',
      setPrimary: '设为主身份',
      copyRow: '复制此行',
      deleteRow: '删除此行',
      projectId: '项目 ID',
      platformAll: '全平台',
      validFrom: '生效时间',
      validTo: '失效时间',
      disableUnsupported: '后端暂不支持停用单条身份',
    },
    multiDept: {
      placeholder: '可选择多个部门,带 ⭐ 为主部门',
      empty: '未设置部门',
      alreadyPrimary: '已是主部门',
      setPrimary: '设为主部门',
      selectedHint: '已选 {n} 个,带 ⭐ 为主部门({name})',
      autoFirst: '自动第一项',
    },
    offline: {
      banner: '网络已断开,正在使用离线模式,部分功能不可用',
    },
    secretInput: {
      show: '显示明文',
      hide: '隐藏明文',
    },
    sensitiveField: {
      viewPlain: '查看明文',
      hide: '隐藏',
    },
    tablePro: {
      exportAria: '导出当前数据为 CSV',
      exportCsv: '导出 CSV',
      columnSettingsAria: '设置显示列与列宽',
      columnSettings: '列设置',
    },
    identitySwitcher: {
      switchedTo: '已切换到 {name}',
      highSensitive: '高敏',
      switchTitle: '切换高敏感身份',
      // 拆成前后两段:中间夹着要加粗高亮的角色名,整句搬进字典会丢掉那个样式
      switchToPrefix: '切换到',
      switchToSuffix: '需短信验证',
      // M146b:身份已切换、但权限清单没拉到(常见于新身份权限不足)。
      // 措辞要说清"切换本身成功了",否则用户会以为要重新登录。
      permissionRefreshFailed: '身份已切换,但权限清单加载失败,菜单可能不完整',
    },
  },
  // 资源类型(sys_resource.type 的三个取值:menu / button / api)
  // ⚠️ 这三个是**固定的枚举**,不是后端可配的字典 ——
  //   所以标签写死在前端字典里,英文界面才不会露出中文。
  resourceType: {
    menu: '菜单',
    button: '按钮',
    api: '接口',
  },
  // 资源图标词表(@/constants/resourceIcons 的 key → 中文名)
  // ⚠️ **key 必须与词表一一对应** —— 少登记一个,那个图标在下拉里露出英文 key;
  //   多登记一个,下拉里能选到一个后端不认的 key(侧边栏渲染成裸文字)。
  resourceIcon: {
    dashboard: '仪表盘',
    user: '用户',
    team: '团队',
    role: '角色',
    permission: '权限',
    dept: '部门',
    dict: '字典',
    config: '配置',
    job: '定时任务',
    file: '文件',
    audit: '审计',
    system: '系统',
    security: '安全',
    flow: '流程',
    approval: '审批',
    // 审批管理(2026-11-01 新增的父菜单图标)—— 必须与 constants/resourceIcons.ts
    // 的 approvalMgmt 键对上,少登记一个,资源编辑弹窗的下拉里会露出英文 key。
    approvalMgmt: '审批管理',
    // 短信模板(M115)—— 必须与 constants/resourceIcons.ts 的 sms 键对上,
    // 少登记一个,资源编辑弹窗的下拉里会露出英文 key。
    sms: '短信',
    // API 文档(§8 G5)—— 同上,constants/resourceIcons.ts 里的 api 键。
    api: 'API',
    // 业务表单设计器 / 动态业务集合(M151)—— 同上,constants/resourceIcons.ts
    skin: '调色板',
    voice: '语音',
    // 里的 table 键(动态集合菜单 menu.dyn.<集合码> 的默认图标也是它)。
    table: '业务表',
    // ── 图标选择器扩容(2026-11-02)—— 与 constants/resourceIcons.ts 一一对应 ──
    // 办公文书
    form: '表单',
    profile: '档案',
    reconciliation: '台账',
    schedule: '日程',
    search: '查询',
    chart: '报表',
    monitor: '监控',
    // 业务对象
    building: '楼宇',
    car: '车辆',
    tool: '维修',
    phone: '电话',
    mail: '邮件',
    contacts: '联系人',
    idcard: '证件',
    solution: '方案',
    customerService: '客服',
    shopping: '商城',
    cart: '购物车',
    rest: '餐饮',
    truck: '物流',
    medicineBox: '医疗',
    experiment: '检验',
    // 财务
    accountBook: '账本',
    wallet: '钱包',
    payCircle: '支付',
    creditCard: '银行卡',
    moneyCollect: '收款',
    transaction: '交易',
    fund: '资金',
    // 数据与文件
    database: '数据',
    cloud: '云',
    fileSearch: '查档',
    fileAdd: '建档',
    folderOpen: '文件夹',
    import: '导入',
    export: '导出',
    upload: '上传',
    download: '下载',
    print: '打印',
    save: '保存',
    paperClip: '附件',
    // 消息协同
    comment: '评论',
    notification: '广播',
    send: '发送',
    share: '分享',
    qrcode: '二维码',
    // 标记与状态
    star: '星标',
    heart: '收藏',
    trophy: '荣誉',
    tag: '标签',
    pushpin: '置顶',
    environment: '位置',
    rocket: '上线',
    alert: '提醒',
    insurance: '保障',
    safetyCertificate: '资质',
    lock: '锁定',
    // 媒体与设备
    camera: '拍照',
    picture: '图片',
    video: '视频',
    robot: '智能',
    mobile: '移动端',
  },
  // tagColor —— antd Tag 颜色白名单(字典管理页的颜色下拉)
  // ⚠️ 这些是 antd Tag 的固定色名,不是业务枚举 —— 写死在前端字典里,
  //   英文界面才不会露出"红/绿/蓝"这种中文色名。
  tagColor: {
    none: '无',
    default: '默认',
    red: '红',
    orange: '橙',
    yellow: '黄',
    green: '绿',
    blue: '蓝',
    purple: '紫',
    magenta: '品红',
    cyan: '青',
    gold: '金',
    lime: '青柠',
    volcano: '火山',
    geekblue: '极客蓝',
  },
  // 表单校验(共享给 login / forgot-password / 业务页)
  validation: {
    required: '此项必填',
    pleaseInput: '请输入{label}',
    pleaseSelect: '请选择{label}',
    usernameRequired: '请输入用户名 / 工号',
    passwordRequired: '请输入密码',
    phoneRequired: '请输入手机号',
    phoneInvalid: '请输入有效的 11 位手机号',
    smsCodeRequired: '请输入验证码',
    smsCodeLen: '{n} 位',
    smsCodePlaceholder: '{n} 位短信验证码',
    phonePlaceholder: '11 位手机号',
    newPasswordRequired: '请输入新密码',
    passwordStrength: '密码须 8-32 位且含字母和数字',
    newPasswordPlaceholder: '8-32 位, 含字母和数字',
    confirmPasswordRequired: '请再次输入新密码',
    confirmPasswordPlaceholder: '再次输入新密码',
    passwordMismatch: '两次输入的密码不一致',
    // 2026-10-25 Module B-2:个人中心改密 + 用户管理建号,两处同一句
    emailInvalid: '邮箱格式不正确',
    passwordMinLength: '至少 8 位',
    captchaRequired: '请输入图形验证码',
  },
  // 全局 toast
  message: {
    loginSuccess: '登录成功',
    resetPasswordSuccess: '密码重置成功, 请使用新密码登录',
    uploadFailed: '上传失败',
  },
  // 短信验证码相关
  sms: {
    send: '发送验证码',
    getCode: '获取验证码',
    codeSentSimple: '验证码已发送',
    codeSent: '验证码已发送({scene})',
    resendAfter: '{n}s 后重发',
    resendAfterRetry: '{n}s 后重试',
  },
  // 短信场景名(2026-10-28 M104 新增)
  //
  // 以前提示语里直接拼内部标识(bizType="login-sms"),用户看到的是
  // "验证码已发送 (login-sms)" —— 内部标识不该出现在界面上。
  // 现在 scene 是后端白名单里的 4 个值,各配一句人话。
  smsScene: {
    login: '短信登录',
    resetPassword: '重置密码',
    changePhone: '修改手机号',
    viewFullPhone: '查看完整手机号',
    mfa: '登录二次验证',
  },
  // 短信二次验证弹窗 (SmsCodeGuard)
  smsGuard: {
    defaultTitle: '安全验证',
    // 弹窗标题(身份切换 / 改密等场景显式指定)
    verifyTitle: '短信验证',
    phoneMissing: '请先完善手机号信息',
    codeInvalid: '请输入 6 位验证码',
    codeWrong: '验证码错误',
    sentTo: '验证码将发送至 {phone}({scene})',
  },
  // 上传组件
  upload: {
    defaultHint: '上传',
    fileTypeNotSupported: '不支持的文件类型: {type}',
    fileTooLarge: '文件超过限制 ({size}MB)',
  },
  // 搜索表单
  searchForm: {
    expand: '展开',
    collapse: '收起',
  },
  login: {
    // ⚠️ 兜底值 —— 后端 `branding.app_name` 有值时以它为准(开源通用化)
    title: '后台管理平台',
    subtitle: '后台管理系统',
    tabPassword: '账号密码',
    tabSms: '短信登录',
    username: '用户名',
    usernamePlaceholder: '用户名 / 工号',
    password: '密码',
    passwordPlaceholder: '密码',
    smsCode: '短信验证码',
    remember: '记住我',
    forgot: '忘记密码?',
    submit: '登录',
    captcha: '图形验证码',
    // M9 polish
    attemptsLeft: '登录失败,还剩 {n} 次尝试机会',
    locked: '账号已锁定,请 {n} 秒后重试',
    lockedButton: '已锁定 ({n}s)',
    lockedWarning: '账号已锁定,请 {n} 秒后重试',
    lockedByServer: '账号已被服务器锁定,请稍后再试或联系管理员',
    // ===== 登录二次验证 MFA(M150)=====
    mfaTitle: '二次验证',
    mfaHint: '验证码已发送至该账号绑定的手机号,请输入短信验证码完成登录。',
    mfaSubmit: '完成登录',
    mfaResend: '重新发送验证码',
    mfaResendIn: '{n}s 后可重发',
    mfaResent: '验证码已重新发送',
    mfaBack: '返回重新登录',
    idleTimeout: '因为长时间无操作,会话已自动结束,请重新登录',
    // 改密后踢回登录页时的提示(2026-10-25 Module B)
    passwordChanged: '密码已修改,请使用新密码重新登录',
  },
  forgotPassword: {
    title: '找回密码',
    subtitle: '输入注册手机号, 验证后即可设置新密码',
    successTitle: '密码重置成功',
    successSubtitle: '请使用新设置的密码登录. 出于安全考虑, 该短信验证码已失效.',
    goLogin: '前往登录',
    rememberBack: '想起密码了?',
    loginNow: '立即登录',
    submit: '提交重置',
    phonePlaceholder: '请输入注册时使用的手机号',
  },
  // 布局(品牌名 / 版权)
  layout: {
    // ⚠️ 三个都是**兜底值** —— 后端 `branding.app_name` 有值时以它为准
    //   (开源通用化:换平台名改服务端 yaml,不用动语言包)。
    //   brandShort / copyright 目前**零渲染**(2026-11-01 全仓无调用方),
    //   留着是因为它们是品牌信息的完整集合,将来接上组件即可用。
    brandShort: '后台',
    brandLong: '后台管理平台',
    copyright: '后台管理平台 ©{year}',
  },
  menu: {
    dashboard: '仪表盘',
    system: '系统管理',
    audit: '审计中心',
  },
  // 顶部栏
  header: {
    fullscreen: '全屏',
    exitFullscreen: '退出全屏',
    help: '帮助文档',
    notifications: '消息',
    profile: '个人中心',
    logout: '退出登录',
    // 退出登录确认框(2026-10-25 Module B)
    logoutTitle: '确认退出登录?',
    logoutDesc: '退出后需要重新输入账号密码',
    logoutOk: '退出',
    // 帮助下拉(2026-11-03)
    apiDocs: 'API 文档',
    userGuide: '使用教程',
  },
  // 站内信(M74)
  //
  // ⚠️ noPermission 不是"加载失败":inbox:list 只绑给超管和只读角色,
  // 多数人点开铃铛本来就是 403。说"没有权限"比让用户以为系统坏了诚实。
  inbox: {
    title: '站内信',
    empty: '暂无消息',
    noPermission: '没有查看站内信的权限',
    loadFailed: '加载失败',
    markAllRead: '全部已读',
    markAllReadConfirm: '将全部未读消息标记为已读?',
  },
  // 主题切换(§2.8 #19)
  theme: {
    // 2026-11-05 皮肤扁平化:明暗不再是独立轴,ThemeSwitcher 退役,
    // 只保留皮肤切换(皮肤名是皮肤属性,来自 registry,不进语言包)
    skin: '切换皮肤',
  },
  // 路由多 tab(§2.8)
  tab: {
    // 页面名 —— 给**没有菜单节点**的页面用(面包屑用;规则见 pageName.ts)
    // 2026-10-25 多 tab 下线后,这里只剩面包屑在用
    page: {
      configAudit: '审计日志',
      profile: '个人中心',
    },
  },
  // 语言切换(§2.6 #8)
  lang: {
    switch: '切换语言',
    'zh-CN': '简体中文',
    'en-US': 'English',
  },
  // 工作台(仪表盘)
  //
  // ⚠️ 页面名由面包屑给(见 components/layout/pageName.ts),
  //    页内再写一遍就是同一句话说两遍。
  dashboard: {
    description: '关键指标总览',
    kpi: {
      fileTotal: '文件总数',
      fileToday: '今日上传',
      fileStorageBytes: '存储占用',
      folderTotal: '文件夹总数',
      userOnline: '当前在线',
    },
    // ⚠️ 2026-11-01:服务端按业务域权限过滤看板内容 ——
    //   没权限的域**压根不出现在响应里**(字段缺省)⇒ 前端不画那张卡片。
    //   这个身份一个域都看不到时,给这句空态(区别于 common.loadFailed:
    //   那是"请求挂了",这里是"你没权限看大盘")。
    empty: {
      noVisibleData: '当前身份没有可查看的看板数据',
    },
    // ⚠️ month1..month12 / daySun..daySat 必须**字面量 key** ——
    //   组件把 12 个月名/7 个星期名当数组传给 echarts,
    //   动态拼 key(`heatmap.month${i}`)会让 i18n 守门网扫不到。
    heatmap: {
      title: '每日登录(近半年)',
      empty: '暂无登录数据',
      less: '少',
      more: '多',
      tooltip: '{date} 登录 {n} 次',
      month1: '1月',
      month2: '2月',
      month3: '3月',
      month4: '4月',
      month5: '5月',
      month6: '6月',
      month7: '7月',
      month8: '8月',
      month9: '9月',
      month10: '10月',
      month11: '11月',
      month12: '12月',
      daySun: '日',
      dayMon: '一',
      dayTue: '二',
      dayWed: '三',
      dayThu: '四',
      dayFri: '五',
      daySat: '六',
    },
    // 服务器状态(2026-11-03)—— dashboard:server 才显示(平台运营线)
    // 样式照用户拍的参考:hertz-antd-admin-saas 的四宫格(运行环境/磁盘/CPU/内存)
    server: {
      title: '服务器状态',
      refresh: '刷新',
      cpu: 'CPU',
      cores: '核心数',
      coreN: 'core {n}',
      memory: '内存',
      disk: '磁盘',
      env: '运行环境',
      os: '操作系统',
      cpuProcessors: 'cpu 逻辑处理器',
      compiler: '编译器',
      goVersion: 'go 版本',
      goroutines: 'goroutine 数',
      total: '总容量',
      used: '已使用',
      free: '剩余',
      dataDisk: '数据盘',
      systemDisks: '系统磁盘',
      noDisks: '未枚举到可用分区',
      redis: 'Redis',
      redisStatus: '状态',
      available: '可用',
      unavailable: '不可用',
      redisKeys: 'key 数',
      redisMemory: '内存占用',
    },
  },
  // 资源菜单(后端 sys_resource.code → 中文名)
  // key 派生规则见文件头注释
  resource: {
    // user
    user_list: '用户列表',
    user_read: '用户详情',
    user_create: '新增用户',
    user_update: '编辑用户',
    user_delete: '删除用户',
    user_unlock: '解锁用户',
    user_toggle: '启用停用',
    // role
    role_list: '角色列表',
    role_create: '新增角色',
    role_update: '编辑角色',
    role_delete: '删除角色',
    role_assign: '分配权限',
    // resource
    resource_list: '资源列表',
    resource_tree: '资源树',
    resource_create: '新增资源',
    resource_update: '编辑资源',
    resource_delete: '删除资源',
    // dept
    dept_list: '部门列表',
    dept_tree: '部门树',
    dept_read: '部门详情',
    dept_create: '新增部门',
    dept_update: '编辑部门',
    dept_delete: '删除部门',
    // dict
    dict_list: '字典列表',
    dict_create: '新增字典',
    dict_update: '编辑字典',
    dict_delete: '删除字典',
    // upload
    upload_list: '上传列表',
    upload_read: '上传详情',
    upload_create: '上传文件',
    upload_delete: '删除文件',
    // family
    family_list: '家庭组列表',
    family_read: '家庭组详情',
    family_create: '新建家庭组',
    family_update: '编辑家庭组',
    family_delete: '删除家庭组',
    // supplier
    supplier_list: '供应商列表',
    supplier_read: '供应商详情',
    supplier_create: '新增供应商',
    supplier_update: '编辑供应商',
    supplier_delete: '删除供应商',
    supplier_audit: '供应商审核',
    // config
    config_list: '配置列表',
    config_read: '配置详情',
    config_create: '新增配置',
    config_update: '编辑配置',
    config_delete: '删除配置',
    config_approve: '审批配置',
    config_cancel: '撤销配置',
    config_activate: '激活配置',
    config_audit: '配置审计',
    config_health: '配置健康',
    config_stats: '配置统计',
    config_watch: '配置监听',
    // audit
    audit_operation_list: '操作日志',
    audit_operation_read: '操作日志详情',
    audit_data_list: '数据变更日志',
    audit_data_read: '数据变更日志详情',
    audit_login_list: '登录日志',
    audit_login_read: '登录日志详情',
    audit_system_list: '系统日志',
    audit_system_read: '系统日志详情',
    audit_full_info_list: '敏感信息查看日志',
    audit_full_info_read: '敏感信息查看日志详情',
    audit_export_list: '导出日志',
    audit_export_read: '导出日志详情',
    audit_export_create: '创建导出任务',
    // contract
    contract_list: '合同列表',
    contract_read: '合同详情',
    contract_create: '新增合同',
    contract_update: '编辑合同',
    contract_delete: '删除合同',
    // device
    device_list: '设备列表',
    device_read: '设备详情',
    device_create: '新增设备',
    device_update: '编辑设备',
    device_delete: '删除设备',
    // feedback
    feedback_list: '反馈列表',
    feedback_read: '反馈详情',
    feedback_create: '提交反馈',
    feedback_reply: '回复反馈',
    // guide
    guide_list: '指南列表',
    guide_read: '指南详情',
    guide_create: '新增指南',
    guide_update: '编辑指南',
    guide_delete: '删除指南',
    // activity
    activity_list: '活动列表',
    activity_read: '活动详情',
    activity_create: '新增活动',
    activity_update: '编辑活动',
    activity_delete: '删除活动',
    // dashboard
    dashboard_read: '仪表盘',
    // wxtemplate
    wxtemplate_list: '微信模板列表',
    wxtemplate_read: '微信模板详情',
    wxtemplate_create: '新增微信模板',
    wxtemplate_update: '编辑微信模板',
    wxtemplate_delete: '删除微信模板',
    wxtemplate_send: '发送微信模板',
    // auth
    auth_switch_identity: '切换身份',
  },
  // 侧边栏菜单(后端 menu.* code → 中文名,Phase 5 M2)
  // key 派生:menu.system → menu_system_user(去掉 menu_ 前缀并替换 . → _)
  // 前端 Sidebar 优先查 resource.menu_<segment>,没有再回落到 code 自身
  resourceMenu: {
    menu_dashboard: '工作台',
    menu_system: '系统管理',
    menu_system_user: '用户管理',
    menu_system_role: '角色管理',
    menu_system_permission: '权限资源',
    menu_system_department: '部门管理',
    menu_system_dict: '数据字典',
    menu_system_config: '系统配置',
    menu_system_job: '定时任务',
    // 短信模板(M115)—— 键 = 后端资源 code 里的 '.' 换成 '_'
    menu_system_smsTemplate: '短信模板',
    // 业务表单设计器(M152)—— 后端 menu.system.dyn
    menu_system_dyn: '业务表单设计器',
    // 自定义业务(M151)—— 运行期生成的动态集合菜单的父节点
    //   (父节点本身没有 path,只是个分组;它的子菜单是 menu.dyn.<集合码>)
    menu_dyn: '自定义业务',
    menu_audit: '审计中心',
    menu_audit_operation: '操作日志',
    menu_audit_dataChange: '数据变更',
    menu_audit_login: '登录日志',
    menu_audit_system: '系统日志',
    // 审批(M83-d 种下菜单,M87 补路由,M88 补 i18n)
    // ⚠️ 2026-11-01:「我的审批」「审批流程」收进「审批管理」父菜单
    menu_approvalMgmt: '审批管理',
    menu_approval: '我的审批',
    menu_flow: '审批流程',
  },
  // API 文档入口页(2026-11-01 · §8 G5)
  //
  // ⚠️ **别把这几段文案写回页面里** —— 写死的中文在英文环境下不会被发现
  //   (t() 查不到 key 时原样返回 key,永不报错),唯一的兜底是扫源码的守门网
  //   (src/locales/__tests__/hardcoded-chinese-i18n.test.ts)。
  apiDocs: {
    description: '接口清单与在线调试入口(Swagger UI)',
    open: '打开 Swagger UI',
    uiPath: '页面地址',
    specPath: 'OpenAPI 描述文件',
    authTitle: '该端点有独立的访问口令',
    authDesc:
      'Swagger UI 由后端在 /swagger 下提供,默认带 BasicAuth 保护:用户名与密码由服务端配置(swagger.auth.*)决定,与后台登录账号无关。打开后浏览器会弹框要求输入。',
    disabledHint: '若提示 404,说明服务端没有启用该端点(swagger.enabled=false)。',
  },
  // 审批待办角标(M88)
  // 登录设备 / 会话(个人中心「登录设备」Tab)
  session: {
    revoked: '已下线 {n} 个设备',
    expired: '已过期',
    // 2026-10-25 Module B-2:登录设备面板(SessionPanel)的文案。
    // 整段搬进字典 —— 这个面板在英文环境下原来整块是中文。
    activeDevices: '活跃设备',
    reload: '重新拉取',
    noActive: '没有活跃设备',
    currentDevice: '本机',
    history: '历史登录记录',
    revoke: '下线',
    revokeOk: '确认下线',
    revokeAll: '下线其他所有设备',
    revokeAllTitle: '下线其他所有设备?',
    revokeAllDesc: '共 {n} 个设备会被强制下线,它们需要重新登录。',
    revokeOneTitle: '强制下线该设备?',
    revokeOneDesc: '该设备上的登录会立刻失效,需要重新登录。',
    revokeCurrentHint: '下线本机请用右上角「退出登录」',
    loggedInAt: '登录于',
  },
  //
  // ⚠️ **角标数是「待我审批」的条数,不是「我发起的」** ——
  //   侧边栏那个角标的作用是提醒我"有单子在等我",发起数不是等待数。
  approval: {
    pendingBadge: '待我审批',
    // 三个盒子(§9.1)—— 少一个参数不等于"给我看全部",所以只有这三盒
    box: {
      todo: '待我审批',
      mine: '我发起的',
      done: '我已办理',
    },
    // 审批单状态(后端 ApprovalStatus)
    status: {
      draft: '草稿(未送审)',
      running: '审批中',
      returned: '已打回',
      approved: '已通过',
      rejected: '已驳回',
      cancelled: '已作废',
    },
    // 轮次结果(一次提交 = 一轮)
    round: {
      running: '进行中',
      approved: '已通过',
      rejected: '已驳回',
      rolledBack: '已打回',
    },
    // 意见流水里的动作
    logAction: {
      submit: '提交',
      approve: '通过',
      reject: '驳回',
      rollback: '打回',
      cancel: '作废',
    },
    // 单个审批人的处理状态
    // 轮次标题与"谁提交的"
    roundTitle: '第 {n} 轮',
    actionReasonRequired: '{action}必须写明理由(不少于 {n} 个字)',
    submittedBy: '{name} 提交',
    assigneeStatus: {
      pending: '待审',
      approved: '已通过',
      rejected: '已驳回',
      stale: '已失效(上一轮)',
      skipped: '已跳过',
    },
    // 审批通过方式(2026-10-25 Module B-3)
    // 会签 = 全部通过,或签 = 一人即可。审批详情页和流程页都在用这两个词,
    // 提到一处共用 —— 各写一份必然漏翻。
    reviewType: {
      all: '会签',
      any: '或签',
    },
  },
  logout: '退出登录',
  profile: '个人中心',
  help: '帮助',
  // PagePlaceholder 内部文案
  placeholder: {
    defaultDescription: '{title}模块,用于 ...',
    skeletonTitle: '页面骨架阶段 —— 业务逻辑后续实现',
    skeletonDesc: '此页面已配置路由、权限、面包屑。具体列表/表单/审批流后续按业务实现。',
    modulePath: '模块路径',
    pageId: '页面标识',
    dependsOnPermissions: '依赖权限码',
    plannedFeatures: '计划功能点',
    placeholderDesc: '页面骨架占位,待业务实现',
  },
  // 业务页文案 (按 module_page camelCase 为 key)
  page: {
    // module=audit
    auditDataChange: {
      title: '数据变更日志',
      description: '关键业务表数据变更审计',
      f1: '数据变更列表(分页 + 表名/记录ID/操作类型)',
      f2: '详情(原值/新值对比)',
      f3: '导出',
    },
    auditLogin: {
      title: '登录日志',
      description: '用户登录行为审计',
      f1: '登录日志列表(分页 + 用户/IP/状态/时间筛选)',
      f2: '详情(IP/UA/时间/设备)',
      f3: '异常登录高亮',
    },
    // 系统日志(2026-11-03)—— 慢 SQL / 慢接口 / 5xx / panic
    auditSystem: {
      title: '系统日志',
      description: '系统运行日志(慢 SQL / 慢接口 / 5xx / panic)',
      sourceSlowSql: '慢 SQL',
      sourceSlowHttp: '慢接口',
      sourceHttpError: '接口 5xx',
      sourcePanic: 'panic',
    },
    auditOperation: {
      description: '用户操作审计(只读) - 用于事后追溯与合规检查',
      f1: '操作日志列表(分页 + 模块/用户/状态筛选)',
      f2: '详情(请求参数/响应结果/IP/UA/耗时)',
      f3: '导出',
      f4: '异常日志高亮',
      errorMsg: '错误信息',
      location: '位置',
      moduleConfig: '配置',
      requestParams: '请求参数',
      // ⚠️ 2026-10-30 M126 新增:脱敏后的请求体(后端现在会写这一列)
      requestBody: '请求体',
      // ⚠️ 2026-10-30 新增:完整请求 URI(路径 + query),与 module(只有路径)不同
      requestUrl: '请求 URL',
      // ⚠️ 2026-10-30 改名:wire 上是 `response_summary`(不是 responseResult)
      responseSummary: '响应摘要',
    },
    // module=system
    configChange: {
      description: '平台配置变更审批流',
      f1: '变更列表(待审批 / 已通过 / 已驳回)',
      f2: '变更详情(配置项原值 vs 新值 diff)',
      f3: '审批通过(立即生效或定时生效)',
      f4: '审批驳回(填写理由)',
      f5: '变更审计',
    },
    config: {
      description: '按配置组折叠管理 — 整组保存,敏感值默认脱敏(点眼睛切明文),每次保存自动写入 sys_config_audit',
      f1: '配置分组(树)',
      f2: '配置列表(支持搜索/过滤/标签)',
      f3: '查看配置详情(值/类型/描述/环境)',
      f4: '修改配置(触发审批流或直生效)',
      f5: '新增配置',
      f6: '版本历史 / 变更审计',
      f7: '回滚到指定版本',
      f8: '敏感字段脱敏展示 + 明文查看权限',
      auditTooltip: '该 group 的所有审计日志',
      emptyNoMatch: '无匹配配置',
      emptyNone: '暂无配置',
      itemCount: '{n} 项',
      lastChanged: ' · 最后改:',
      noChanges: '没有修改',
      resetDone: '已重置为初始值',
      saveGroup: '保存本组({n} 项)',
      savedCount: '已保存 {n} 项',
      savedPartial: '已保存 {n} 项,{m} 项失败',
      sensitive: '敏感',
      sensitiveHint: '敏感值 — 留空保持原值,点眼睛切明文',
      /**
       * 配置项名称 —— key = config_key 把 '.' / '-' 换成 '_'
       *
       * ⚠️ 必须与 `server/internal/seed/seed.go` 的 `mustSysConfigs` **一一对应**
       *   (齐全性由 `locales/__tests__/config-i18n.test.ts` 对着 seed 清单守着)。
       *   缺一条 ⇒ 英文环境回落成 DB 里的中文 config_name(而且是静默的)。
       */
      item: {
        aliyun_sms_provider: '短信服务提供方',
        aliyun_sms_access_key: '短信 AccessKey ID',
        aliyun_sms_access_secret: '短信 AccessKey Secret',
        aliyun_sms_sign_name: '短信签名',
        aliyun_sms_region: '短信 API Region',
        aliyun_sms_template_login: '登录验证码模板',
        aliyun_sms_template_view_full_phone: '查看完整手机号模板',
        aliyun_sms_template_reset_password: '重置密码模板',
        aliyun_sms_template_change_phone: '换绑手机模板',
        aliyun_oss_provider: 'OSS 服务提供方',
        aliyun_oss_endpoint: 'OSS Endpoint',
        aliyun_oss_access_key: 'OSS AccessKey ID',
        aliyun_oss_access_secret: 'OSS AccessKey Secret',
        aliyun_oss_bucket: '文件 bucket',
        aliyun_oss_cdn_domain: 'CDN 加速域名',
        aliyun_oss_upload_token_ttl: 'STS 凭证有效期(秒)',
        // voice(百炼语音,2026-11-06)—— 中文名与 seed 逐字一致(测试守着)
        voice_api_key: '百炼 API Key',
        voice_workspace_id: '百炼业务空间 ID',
        voice_region: '百炼端点地域',
        voice_default_target_model: '克隆目标音色模型',
        voice_synth_url: '百炼合成端点',
      },
      /**
       * 字典驱动的表单 label —— key = 字典 item_key 把 '.' 换成 '_'
       *
       * ⚠️ 这一组是**兜底**:字典(config_ui_label)存的是运营可改的中文,
       *   英文环境下仍然是中文 ⇒ 2026-10-29 起改成 i18n 优先、字典兜底。
       */
      dict: {
        config_value: '当前值',
      },
    },
    department: {
      description: '组织架构维护',
      f1: '部门树',
      f2: '新增/编辑部门(上级/排序)',
      f3: '启用/停用',
      f4: '部门负责人设置',
      f5: '删除(有用户时禁止)',
    },
    permission: {
      description: '系统所有可访问资源(菜单/按钮/接口)',
      // 2026-10-25 Module B-5b:资源(权限点)管理页的文案(原来整页写死中文)。
      addChild: '添加子级',
      codePattern: '小写字母+数字+_+.+*,以字母开头',
      parentResource: '父资源',
      topLevel: '— 顶级 —',
      pathLabel: '前端路径 / API 路径',
      pathPlaceholder: '菜单:/system/xxx  /  API:/rapid-admin/v1/xxx',
      httpMethod: 'HTTP 方法',
      iconLabel: '图标',
      iconPlaceholder: '选择侧边栏图标',
      // 2026-10-30 用户报:"图标是输入,用户根本不知道输入什么" ⇒ 改成下拉选择
      iconNone: '无图标',
      iconHelp: '仅菜单使用;图标会显示在左侧导航栏上。',
      // HTTP 方法「全部」
      methodHelp: '仅用于标注该权限点对应哪个接口,不参与鉴权;「全部」= 不限定。',
      // 父资源类型约束
      parentHelp: '菜单只能挂在菜单下;按钮可挂在菜单/按钮下;接口可挂在菜单/按钮/接口下。',
      deleteTip1: '删除后该资源点将不再出现在菜单/按钮/接口列表中。',
      deleteTip2: '若后端提示「存在子资源」或「资源已被角色绑定」,需先清理依赖再删。',
      f1: '资源(权限点)树',
      f2: '新增/编辑资源(菜单/按钮/接口)',
      f3: '资源类型:菜单 / 按钮 / API',
      f4: '资源排序、显示控制',
      f5: '删除(被引用时禁止)',
    },
    role: {
      description: '角色定义 + 资源授权',
      // 2026-10-25 Module B-5b:角色管理页 + 资源分配抽屉的文案。
      viewPermissions: '查看权限',
      builtinNotEditable: '内置角色不可编辑',
      builtinNotDeletable: '内置角色不可删除',
      superAdminNotDeletable: '超管角色不可删除',
      codePattern: '小写字母+数字+下划线,以字母开头',
      typeBusiness: '业务角色',
      typePlatform: '平台角色',
      deleteTip: '删除后该角色下用户将失去对应权限,角色-资源绑定关系一并清理',
      // ---- 资源分配抽屉 ----
      assign: {
        loadFailed: '加载失败: {msg}',
        success: '已为「{name}」分配 {n} 个权限点',
        viewTitle: '查看权限 — {name}',
        title: '分配权限 — {name}',
        readOnlyTag: '内置/超管,只读',
        selected: '已选 {n} / {total}',
        selectAllInView: '本视图全选',
        clearInView: '本视图清空',
        empty: '暂无资源',
      },
      // ---- 数据范围(行级权限)抽屉 —— M157(2026-11-02) ----
      scope: {
        button: '数据范围',
        title: '数据范围 — {name}',
        viewTitle: '查看数据范围 — {name}',
        menuTitle: '业务域',
        defaultModuleLabel: '默认(全部业务域)',
        defaultModuleTip: '这是「默认规则组」',
        defaultModuleDesc:
          '默认组只对**没有单独配规则**的业务域生效。一旦某个业务域自己配了规则,默认组在那个业务域上就不再参与(就近覆盖,两组不是叠加)。',
        moduleTip: '这是单个业务域',
        moduleDesc:
          '本业务域一旦配了规则,默认组在**本业务域**上就不再参与(就近覆盖)。其它没配规则的业务域仍然走默认组。',
        ruleHint: '多条规则之间是「或」的关系',
        ruleHintDesc:
          '满足任意一条规则的行就看得见。「全部数据」一票放行 —— 只要有它,其余规则都不会生效。限定了范围但调用方一条都匹配不到时,他看不到任何数据(不是看到全部)。',
        allWarning: '本组里有「全部数据」',
        allWarningDesc: '只要存在「全部数据」这条规则,其余规则就不会被解析 —— 它们配了也不起作用。',
        addRule: '添加规则',
        removeRule: '删除',
        customColumnPlaceholder: '列名(如 project_id)',
        customValuesPlaceholder: '允许取值,回车添加',
        remarkPlaceholder: '备注(选填)',
        emptyRules: '本业务域还没有配置规则',
        emptyModules: '后端暂未登记任何业务域,当前只能配默认规则组',
        noChange: '没有改动',
        saveSuccess: '已保存 {n} 个业务域的数据范围',
        loadFailed: '加载失败: {msg}',
        type: {
          all: '全部数据',
          project: '本项目',
          dept: '本部门',
          dept_and_child: '本部门及下级',
          self: '仅本人',
          custom: '自定义',
        },
        errors: {
          scopeTypeRequired: '请选择范围类型',
          scopeTypeInvalid: '范围类型不合法',
          customColumnRequired: '自定义范围必须指定列名',
          customColumnInvalid: '列名必须是裸标识符(字母/数字/下划线,不能以数字开头)',
          customValuesRequired: '自定义范围必须至少给一个取值',
        },
      },
      // ---- 字段可见性(列级权限)抽屉 —— M164(2026-11-02) ----
      //
      // ⚠️ 这一段与上面的 scope(数据范围)是**两根不同的轴**:
      //   scope 管"能看到哪些行",fieldPolicy 管"能看到那一行里的哪些字段"。
      fieldPolicy: {
        button: '字段可见性',
        title: '字段可见性 — {name}',
        viewTitle: '查看字段可见性 — {name}',
        menuTitle: '业务域',
        defaultModuleLabel: '默认(全部业务域)',
        defaultModuleTip: '这是「默认规则组」',
        defaultModuleDesc:
          '默认组对**所有**业务域生效。某个业务域上配了同名字段的规则时,只覆盖**那个字段**(逐字段叠加)—— 默认组对其它字段的限制在这个业务域上仍然有效。',
        moduleTip: '这是单个业务域',
        moduleDesc:
          '本业务域里配了规则的字段,覆盖默认组里**同一个字段**的规则;没配规则的字段仍然走默认组。',
        overlayHint: '默认组与业务域是「逐字段叠加」',
        overlayHintDesc:
          '把某个字段设成「明文可见」,会让它在**本业务域**上重新可见 —— 默认组对它的那条限制在本业务域上就不生效了。字段级可见性只能对**已经在响应里**的字段做收窄;想让某个角色看到某个字段,得业务侧先把值放进响应。',
        hiddenHint: '「隐藏」是从响应里删掉这个键',
        hiddenHintDesc:
          '不是把值置空 —— 响应里连字段名都不出现(比如 emergency_contact_phone 这个键本身就是信息)。',
        maskHint: '「打码」要选一种打码方式',
        maskHintDesc:
          '字段名猜得出来时可以选「自动」(按字段名推断);猜不出来时必须显式选一种,否则保存会被拒 —— 运行期的兜底是「整串打码」(等长星号),那会让管理员看到一堆意料之外的星号。',
        remarkPlaceholder: '备注(选填)',
        sensitive: '敏感',
        notCandidate: '候选已不存在',
        ruleCount: '本业务域已配 {n} 条字段规则',
        emptyFields: '这个业务域没有可配置的字段',
        emptyModules: '后端暂未登记任何业务域,当前只能配默认规则组',
        noChange: '没有改动',
        saveSuccess: '已保存 {n} 个业务域的字段可见性',
        saveRejected: '有 {n} 处配置不合法,已阻止保存(见下方标红处)',
        loadFailed: '加载失败: {msg}',
        mode: {
          inherit: '跟随默认组',
          defaultInherit: '未配置(明文可见)',
          visible: '明文可见',
          masked: '打码',
          hidden: '隐藏',
        },
        mask: {
          auto: '自动(按字段名推断)',
          phone: '手机号',
          id_card: '身份证',
          name: '姓名',
          email: '邮箱',
          bank_card: '银行卡',
          contract_no: '合同编号',
          full: '整串打码',
        },
        errors: {
          modeInvalid: '可见性模式不合法',
          maskTypeRequired: '打码方式必须显式指定(按字段名猜不出来)',
          fieldNotCandidate: '这个字段已经不在该业务域的候选里,请删除这条规则',
        },
      },
      f1: '角色列表',
      f2: '新增/编辑角色(类型 / 描述)',
      f3: '分配资源(权限树勾选)',
      f4: '查看角色已分配用户',
      f5: '删除(有用户时禁止)',
    },
    user: {
      description: '后台系统用户管理,涵盖账号/身份/部门',
      resetPasswordTitle: '重置 {name} 的密码?',
      // 2026-10-25 Module B-5a:用户管理页的文案(原来整页写死中文)。
      detailLoadFailed: '加载用户详情失败:{msg}',
      identityLoadFailed: '加载用户身份失败:{msg}',
      resetPassword: '重置密码',
      cannotDeleteSelf: '不能删除自己(超管)',
      accountRequired: '请输入账号',
      phoneUnchangedTip: '留空 = 不修改手机号',
      roleRequired: '请至少选择一个角色',
      roleMultiTip: '可多选,第一个为主身份',
      identityOverwriteTip: '改动会覆盖该用户的身份清单(已绑的项目 / 部门 / 有效期按角色保留)',
      deptOverwriteTip: '改动会覆盖该用户的部门清单(未勾选的部门会被移除)',
      lockedStatusTip: '「锁定」是系统自动派生的状态(登录失败锁定),不能在这里设置',
      initialPassword: '初始密码',
      initialPasswordRequired: '请输入初始密码',
      firstLoginForceChangeTip: '用户首次登录后会被强制改密',
      departmentsLabel: '所属部门',
      notSet: '未设置',
      identitiesLabel: '身份 / 角色',
      // ⚠️ 前面的空格是**文案的一部分** —— 原来写的是 ' (停用)',
      //   空格在名字和括号之间。放进键里,渲染出来才跟原来一模一样。
      inactiveSuffix: ' (停用)',
      batchDelete: '批量删除 ({n})',
      deptRequired: '请至少选择一个部门',
      preHashUnsupported: '当前环境不支持密码预哈希,请换用现代浏览器',
      identityUpdated: '身份已更新',
      manageIdentity: '管理身份',
      deleteTip: '删除后该用户将无法登录,关联的身份/审计记录保留(只读)',
      batchDeleteTitle: '批量删除 {n} 个用户?',
      batchDeleteTip: '此操作不可撤销,请谨慎。包含自己的勾选已自动排除。',
      batchDeleted: '已删除 {n} 个用户',
      batchDeleteFailed: '批量删除失败',
      resetPasswordTip: '重置后系统将生成初始密码,目标用户下次登录会被强制改密',
      resetDone: '已重置,新密码: {pwd}',
      resetFailed: '重置失败',
      f1: '用户列表(分页 + 多条件筛选)',
      f2: '新增用户(部门 / 多身份 / 初始密码)',
      f3: '编辑用户信息',
      f4: '启停用 / 解锁',
      f5: '批量导入 / 导出 Excel',
      f6: '重置密码(管理员)',
      f7: '操作日志关联',
    },
    skin: {
      description: '可视化调色,实时预览;导出的 TS 片段粘进 web/src/theme/skins/ 即成为构建内皮肤(docs/theming.md §5)',
      listTitle: '内置皮肤',
      clone: '副本',
      cloneSuffix: '副本',
      newSkin: '从主色新建',
      pickFirst: '从左侧「副本」或「从主色新建」开始',
      editTitle: '编辑',
      defaultName: '自定义皮肤',
      skinName: '皮肤名',
      appearanceLight: '浅色',
      appearanceDark: '深色',
      primary: '主色',
      hover: '悬浮色',
      active: '按下色',
      baseMode: '基础模式(界面色调 + 主色三态)',
      baseModeTip: '主色 = 品牌强调色(按钮 / 链接 / 选中态),不是界面底色 —— 底色由「界面色调」+ 浅色 / 深色参考系派生,高级模式「画布」组可逐键调整。',
      surface: '界面色调',
      surfaceTip: '界面中性面(画布 / 侧栏 / 菜单底色)的色相与浓度来源 —— 选纯灰就是白色 / 黑色皮肤那种无彩底;品牌主色只管按钮 / 链接 / 选中态,两者互不影响。',
      rederive: '按主色重算全部色值',
      rederiveTip: '重算全部建议值:中性面(画布/文字/边框/顶栏/侧栏)由「界面色调」决定色相与浓度,按钮/链接/选中态等强调键由「主色」决定;两者都保持参考皮肤(浅色=文博棕,深色=黑)的明度档位。高级模式里的手动调整会被覆盖。',
      rederiveConfirm: '会用当前主色重新生成所有建议值,覆盖你在高级模式里的手动调整。继续?',
      rederived: '已按主色重算',
      advancedPalette: '高级模式 · 语义色板(palette 25 键)',
      advancedComp: '高级模式 · 组件级(comp 33 键)',
      previewTitle: '实时预览',
      tryOn: '整页试穿',
      exitTryOn: '退出试穿',
      tryOnHint: '试穿注册的是草稿皮肤(custom-draft),离开本页自动恢复原皮肤;导出前记得改名',
      exportTsTitle: '导出 TS 片段',
      exportTsHint: '保存为 web/src/theme/skins/my-skin.ts,然后在 theme/registry.ts 的 SKINS 表挂一行(见 docs/theming.md §5 第 3 步)',
      exportJson: '导出 JSON',
      copied: '已复制到剪贴板',
      copy: '复制到剪贴板',
      previewInputPlaceholder: '输入框(聚焦看主色光圈)',
      tagDefault: '默认',
      tagPrimary: '主按钮',
      tagDashed: '虚线',
      tagDanger: '危险',
      tagInfo: '信息',
      tagSuccess: '成功',
      tagWarning: '警告',
      tagError: '错误',
      groupCanvas: '画布',
      groupText: '文字梯度',
      groupBorder: '边框',
      groupBrand: '品牌强调',
      groupLayout: '布局(顶栏 / 侧栏 / 画布)',
      groupMenu: '菜单',
      groupTable: '表格',
      groupMisc: '按钮阴影 / 分页 / 面包屑 / Tabs',
    },
    // 字典页 2026-10-17 实装(ManagementTable + inlineToolBar),顶掉了 placeholder 版 ——
    // 所以这一组只有 description,没有 f1..f5 功能清单(别再补回来,那是死键)
    dict: {
      description: '字典维护(业务方自由 enum 场景)',
      // 2026-10-25 Module B-5c:字典管理页的写死中文
      // ---- 编辑弹窗 ----
      editTitle: '编辑字典项',
      detailTitle: '字典项详情',
      displayNameRequired: '请输入显示名',
      descriptionPlaceholder: '纯文字说明(不带 color: 前缀)',
      // ---- 顶部 Alert ----
      repoNotice: '字典是代码 enum 的中央仓库,只能编辑展示属性,不能新增/删除',
      // ⚠️ 这 5 段是**一段富文本**被 <code> 打断后的碎片 ——
      //   本项目的 i18n 只做纯字符串占位替换,插不进标签,
      //   所以一句带 <code> 的长句必须拆成几段,渲染时按原顺序拼回去。
      //   中文侧不带空格(除「。 详见」那句本身带一个空格);
      //   英文侧**首尾都要留空格**,否则拼起来会粘在一起。
      repoDesc1: '新增字典请改 ',
      repoDesc2: ' 的 ',
      repoDesc3: ',然后跑 ',
      repoDesc4: ';下线字典项请改为 ',
      repoDesc5: '。 详见 ',
    },
    dept: {
      description: '组织架构维护 · 树形结构,父部门禁用/删除需先处理子部门',
      // 2026-10-25 Module B-5c:部门管理页的写死中文
      // ---- 左栏:组织树 ----
      orgTree: '组织架构',
      addRoot: '新增根部门',
      addChild: '新增子部门',
      empty: '暂无部门',
      searchHint: '请在左侧选择部门',
      // ---- 右栏:详情 ----
      subDepartments: '子部门',
      noSubDepartments: '无子部门',
      // 成员数单位(「3 人」)—— 单独一个键,英文里是 "3 people"
      memberUnit: '{n} 人',
      // ---- 编辑 / 删除 ----
      parentDept: '父部门',
      selectParentFirst: '请先选中一个父部门',
      // 有成员不能删(按钮 title 提示)
      deleteBlocked: '存在成员的部门不可删除(请先转移成员)',
      deleteTip: '删除后该部门及子部门将被移除,部门下用户需先转移至其他部门',
    },
    job: {
      description:
        '调度任务的启停 / 频率 / 备注覆盖与手动触发 — 任务本体只在代码里注册,后台只能改运行态',
      // 2026-10-25 Module B-2:执行记录抽屉里的运行状态。
      // success / failed 走 common.resultSuccess / common.resultFailed
      // (通用结果词),只有"执行中"是定时任务专有的。
      status: {
        running: '执行中',
      },
      // 2026-10-25 Module B-4:定时任务页(列表 + 执行记录抽屉)的文案。
      // 原来整页写死中文,英文环境下这块完全不可用。
      // ---- cron 快捷填入 ----
      // ⚠️ 这几个是**模块顶层常量**里的值 —— 顶层常量在 import 时求值,
      //   直接写 t('…') 会把语言冻结在 import 那一刻。所以源码里存的是**键**,
      //   渲染时再查字典。
      cronPreset: {
        everyMinute: '每分钟',
        everyHour: '每小时',
        daily3am: '每天 03:00',
        weeklyMonday3am: '每周一 03:00',
        monthly1st3am: '每月 1 号 03:00',
        every10Minutes: '每 10 分钟',
      },
      cronInvalid: '表达式非法',
      defaultExprLabel: '代码默认值:',
      nextRuns: '接下来 {n} 次:',
      // ---- cron 表达式翻译成人话(M120)----
      // 描述器:web/src/pages/system/job/cronDescribe.ts
      //
      // ⚠️ **星期名是"先翻译再拼接"的** —— 所以它必须是一个独立短语,
      //   不能做成 {dows} 参数(参数是字符串,参数里的东西没法再翻译)。
      //   于是 weekly 里的 {dows} 装的是**已经翻译好的**星期名列表。
      cronDesc: {
        // 列表分隔符:中文用顿号,英文用逗号
        listSep: '、',
        daily: '每天',
        // {dows} = 已经翻译好的星期名(用 listSep 连起来的)
        weekly: '每周{dows}',
        monthlyDay: '每月 {d} 日',
        everyNDays: '每 {n} 天',
        everyMinute: '每分钟',
        everyMinutes: '每 {n} 分钟',
        everyHours: '每 {n} 小时',
        everyHoursMinutes: '每 {h} 小时 {m} 分钟',
        everySeconds: '每 {n} 秒',
        everyHour: '每小时',
        // ---- "每小时的第 N 分钟"长句式 ----
        //
        // ⚠️ **四句都是完整的一句话**,不是"范围 + 分句"两块拼的:
        //   中文「每月每日每星期每小时的第 7 分钟各执行一次」中间不能有空格,
        //   英文又必须有 —— 拼接规则随语言变,不如让翻译直接看到整句。
        //
        // {mins} = 分钟数列表(用 listSep 连起来,**不补零**)
        minuteListAll: '每月每日每星期每小时的第 {mins} 分钟各执行一次',
        minuteListWeekly: '每周{dows}每小时的第 {mins} 分钟各执行一次',
        minuteListMonthlyDay: '每月 {d} 日每小时的第 {mins} 分钟各执行一次',
        minuteListEveryNDays: '每 {n} 天每小时的第 {mins} 分钟各执行一次',
        // 中文里"03:00"前面不需要介词,英文需要(at 03:00)
        atTime: '{time}',
        yearly: '每年 1 月 1 日',
        dow: {
          sun: '日',
          mon: '一',
          tue: '二',
          wed: '三',
          thu: '四',
          fri: '五',
          sat: '六',
        },
      },
      // ---- 触发方式(定时 / 手动)----
      triggerSchedule: '定时',
      triggerManual: '手动',
      // ---- 执行记录抽屉 ----
      runHistory: '执行记录',
      degraded: '降级',
      degradedOnly: '只看降级',
      lastFailed: '最近一次失败({time}):',
      noErrorInfo: '(无错误信息)',
      // ---- 任务列表 ----
      notScheduled: '未调度',
      overridden: '已覆盖',
      residualTip: '代码里已删除该任务,sys_job 里只剩没人认领的残留行',
      residual: '残留',
      codeSideOff: '代码侧关闭',
      minutes: '{n} 分钟',
      removedTip: '任务已从代码中移除,残留配置不可修改',
      removedTipShort: '任务已从代码中移除',
      disabledTip: '任务已停用,需先启用才能触发',
      noOverrideTip: '当前没有后台覆盖,本来就是代码默认值',
      manualTrigger: '手动触发',
      // ---- 手动触发的结果 ----
      triggerSuccess: '执行成功,耗时 {n}ms',
      triggerFailedTitle: '触发失败 · {key}',
      nonSuccessStatus: '任务返回了非成功状态:{status}',
      // ---- 降级横幅 ----
      schedulerDisabled: '调度器未启用',
      schedulerDisabledDesc: 'job.enabled=false,本页所有任务都不会自动触发。手动触发仍然可用。',
      lockDegraded: '分布式锁不可用(降级运行)',
      // ⚠️ 下面四个键是**一句话被 <code> / <strong> 切成的四段** ——
      //   这套 i18n 只支持字符串占位符替换,富文本没法整个塞进一个键。
      //   四段拼起来仍然还原原来那句话,一个 visible character 都没少。
      //   中文这边不带空格,英文那边靠前缀/后缀自带空格(见 en-US.ts 的注释)。
      lockDegradedPrefix: '锁模式',
      lockDegradedMiddle: '当前不可用,多副本部署下同一任务可能',
      lockDegradedHighlight: '在多个实例重复执行',
      lockDegradedSuffix: '。非幂等任务在降级时会被跳过(执行记录里标记「降级」)。',
      streamDegraded: '事件推送已降级,任务状态不会自动刷新',
      streamDegradedDesc: '事件通道(Redis)当前不可用,执行记录需要手动点「刷新」查看。定时任务本身仍在正常调度。',
      onlyEnabled: '仅已启用',
      runtimeInfo: '实例 {id} · 时区 {tz} · 已注册 {n} 个任务',
      // ---- 弹窗 ----
      editTitle: '编辑任务',
      manualTriggerTitle: '手动触发 {key}?',
      // 手动触发的确认文案 —— 同样是被 <b> 切开的几段
      triggerConfirmPrefix: '任务会',
      triggerConfirmHighlight: '同步',
      triggerConfirmSuffix: '执行到结束,最长可能挂起 {n} 分钟。非幂等任务请勿重复点击。',
      resetTitle: '恢复默认配置?',
      resetConfirmPrefix: '将清空 ',
      resetConfirmMiddle: ' 的后台覆盖:cron 表达式、名称、备注全部回到代码里的默认值。「已启用/停用」状态',
      resetConfirmHighlight: '不受影响',
      resetConfirmSuffix: '(想停用请用「停用」按钮)。',
      restored: '已恢复代码默认值',
    },
    // module=system(短信模板审核状态 M115)
    //
    // ⚠️ 这一页的文案有两条硬约束:
    //   ① **状态标签与颜色走字典**(sms_template_status 组),不在这里写死 ——
    //      字典是运营能改的,前端这份副本必然漂移。
    //   ② **"恢复"不等于"通过"** —— 人工无权判定通过,恢复后落到"待审核",
    //      最终结论要等服务商下一次同步。所以按钮文案不能写成"启用"。
    smsTemplate: {
      description: '四个短信场景的模板审核状态 —— 停用是止血手段,恢复后还要等服务商确认',
      // ---- 立即同步(与定时任务同一个入口)----
      syncNow: '立即同步',
      syncDone: '同步完成:更新 {updated} 个,新登记 {registered} 个,未变化 {unchanged} 个',
      syncPartial: '没能确定状态:{scenes}(状态保持原样)',
      syncNothing: '状态没有变化({unchanged} 个)',
      // ---- 停用 / 恢复 ----
      disable: '停用',
      enable: '恢复',
      disableTitle: '停用模板',
      enableTitle: '恢复模板',
      reasonLabel: '原因',
      reasonPlaceholder: '选填,例如:服务商驳回 / 模板内容有误',
      reasonHelp: '会写进流转历史(最多 500 字),排障时全靠它',
      disabled: '已停用',
      enabled: '已恢复,等待服务商确认',
      notConfiguredTip: '该场景没有配置模板号,没有可停用的模板',
      notDisabledTip: '该场景当前不是停用状态,无需恢复',
      // ---- 表格 ----
      codeMismatch: '模板号不一致',
      codeMismatchTip: '登记册里的审核结论是另一个模板号的,当前这个号还没同步过',
      unknown: '未知',
      // ---- 状态来源 ----
      source: {
        provider: '服务商',
        manual: '人工',
      },
      // ---- 守卫策略横幅 ----
      policyLabel: '守卫策略:{policy}',
      policyDesc: '守卫按模板的审核状态决定放行、告警还是拦截。这条策略是平台级开关,所有场景共用。',
      policyBlock: '拦截',
      policyWarn: '只告警',
      policyOff: '放行(不检查)',
      policyUnknown: '未知策略',
      // ---- 当前通道 ----
      providerUnsupported: '当前短信通道({provider})不支持查询模板审核状态',
      providerUnsupportedDesc: '这个通道没有可审的模板,状态不会自动更新,「立即同步」会失败。',
      // ---- 流转历史抽屉 ----
      history: '历史',
      historyTitle: '流转历史',
      noHistory: '暂无流转记录',
    },
    // module=voice(语音管理 2026-11-06:音色登记册 + 克隆台账 + 文本合成)
    voice: {
      description: '音色登记册、克隆台账与文本合成 —— 内置音色由后端 seed 自动登记;克隆走百炼 CosyVoice,合成可选百炼 / Edge 两条通道',
      tabVoices: '音色列表',
      tabClones: '克隆记录',
      tabSynth: '文本合成',
      errorMsg: '失败原因',
      // ---- 添加 / 编辑音色 ----
      addVoice: '添加音色',
      nameRequired: '请输入名称',
      editVoice: '编辑音色',
      voiceId: '音色 ID',
      voiceIdPlaceholder: '如 zh-CN-XiaoxiaoNeural 或 longxiaochun_v3',
      ttsType: '合成平台',
      gender: '性别类别',
      genderPh: '男声 / 女声 / 童声 / 方言 / 定制',
      sex: '性别',
      male: '男声',
      female: '女声',
      recommend: '推荐',
      voiceIdLocked: 'voice_id 与合成平台不可改 —— 换平台/换音色应删除后重建(就地改会让试听与新音色对不上)',
      created: '已创建,试听音频正在后台生成',
      updated: '已保存,试听音频正在后台重新生成',
      // ---- 克隆音色 ----
      cloneVoice: '克隆音色',
      audio: '参考音频',
      audioRequired: '请选择参考音频(wav/mp3/m4a/pcm/aac/flac/ogg)',
      audioHint: '10s 以上、内容清晰的音频克隆效果最好;文件经对象存储中转,由百炼侧拉取注册',
      targetModel: '目标模型',
      targetModelPh: '默认 cosyvoice-v3-plus(后端 voice.default_target_model)',
      prefix: 'voice_id 前缀',
      prefixPh: '默认 voice',
      languageHints: '语言提示',
      languageHintsPh: '逗号分隔,如 zh,en',
      cloning: '正在提交克隆(远端要拉取音频并进入审核,请稍候)…',
      cloned: '已提交克隆,进度看「克隆记录」(DEPLOYING → OK)',
      // ---- 克隆台账 ----
      refreshStatus: '刷新状态',
      refreshed: '状态已同步',
      statusDeploying: '审核中',
      statusOk: '已发布',
      statusUndeployed: '审核拒绝',
      statusFailed: '失败',
      refreshUnavailable: '这条是失败记录(没有 voice_id),无法刷新',
      // ---- 文本合成 ----
      selectVoice: '选择音色',
      selectVoiceFirst: '请先选择音色',
      textRequired: '请输入要合成的文本',
      textPh: '输入要朗读的文本…',
      format: '音频格式',
      provider: '合成通道',
      providerAuto: '按音色登记册',
      providerBailian: '百炼',
      providerEdge: 'Edge 朗读',
      volume: '音量',
      rate: '语速',
      play: '试听',
      download: '下载音频',
      streamPlay: '流式试听',
      streaming: '接收中… {n} 字节',
      synthDone: '合成完成',
      edgeWavHint: '当前音色走 Edge 通道,只输出 mp3 —— wav 选项已禁用',
      bailianHint: '百炼通道需要后端配置 voice.synth_url;未配置时合成会显式报错',
      demoMissing: '试听音频还没生成(创建后异步合成,失败会只少试听)',
    },
    // module=approval / module=flow(通用审批 M83/M87)
    approvalList: {
      description: '待我审批 / 我发起的 / 我已办理 —— 三个盒子',
      // 2026-10-25 Module B-3:「被打回 N 次」标签上的计数后缀
      returnCount: '{n} 次',
    },
    approvalDetail: {
      notFound: '审批单不存在或无权查看',
      title: '审批详情',
      description: '轮次切换 + 关卡进度 + 意见流水 + 审批动作',
      // 2026-10-25 Module B-3:详情页正文文案。
      // 原来整页写死中文,英文环境下这张卡片一个英文都没有。
      // 顶部
      back: '返回',
      bizId: '业务记录 ID',
      templateName: '流程模板',
      returnCount: '被打回次数',
      finishTime: '完成时间',
      // 轮次切换器
      roundSection: '轮次',
      roundCurrent: '(当前)',
      roundArchive: '这是第 {n} 轮的存档,当前生效的是第 {m} 轮',
      // 审批内容(可编辑区)
      contentSection: '审批内容',
      dirty: '有未保存的修改',
      sameAsSnapshot: '内容与本轮快照一致',
      saveAndResubmit: '保存并重提',
      submit: '送审',
      noExtra: '无附加内容',
      rawContentKey: '内容',
      // 切轮次前的确认框(有未保存修改时)
      discardAndSwitch: '丢掉并切换',
      continueEditing: '继续编辑',
      switchConfirm: '切到别的轮次会丢掉这些修改。确定要切吗?',
      // 送审 / 审批动作的结果提示
      resubmitMustChange: '重提必须先修改内容:与上一轮提交完全一致的单子会被退回',
      submitted: '已提交',
      submittedForReview: '已送审',
      resubmitted: '已重提',
      // 审批进度
      progressSection: '审批进度',
      noStage: '该单还没有进入任何关卡',
      assigneeProgress: '共 {n} 人,已批 {m} 人',
      stageTitle: '第 {n} 关 · {name}',
      // 意见流水
      logSection: '意见流水',
      noLog: '还没有任何意见记录',
      rollbackRequest: '打回补充材料',
    },
    flowList: {
      description: '审批模板 —— 关卡与审批人规则的配置入口',
      bizTypeRequired: '请输入业务类型(如 digital_material)',
      createTemplate: '新建模板',
      defaultStageName: '第 1 关',
      deleteTemplateDesc: '删除后该模板不能再用于新审批。已经在跑的审批单不受影响(它们用的是发起时的快照)。',
      deleteTemplateTitle: '删除模板 {name}?',
      stageConfig: '关卡配置',
      stages: '关卡',
      templateCode: '模板编码',
      templateCodePattern: '只允许小写字母、数字、下划线,且必须以字母开头',
      templateCodeRequired: '请输入模板编码',
      templateDetail: '模板详情',
      templateName: '模板名称',
      templateNameRequired: '请输入模板名称',
    },
    flowDetail: {
      description: '每一关是一个审批阶段;或签=一人通过即可,会签=全部通过',
      addStage: '新增关卡',
      backToTemplateList: '返回模板列表',
      deleteStageDesc: '该关卡下的审批人规则会一起删除。整棵模板会被重新写入,不是只删这一行。',
      deleteStageTitle: '删除关卡 {name}?',
      editStage: '编辑关卡',
      moveDown: '下移',
      moveUp: '上移',
      orderChanged: '顺序已调整',
      reviewTypeAllDesc: '会签(所有人都批才算通过)',
      reviewTypeAllTag: '会签(全部通过)',
      reviewTypeAnyDesc: '或签(第一个批的人定结果)',
      reviewTypeAnyTag: '或签(一人即可)',
      ruleDeptLeaderTag: '部门主管 +{n}',
      ruleRoleTag: '角色 {name}',
      ruleUserTag: '指定人 {name}',
      // 2026-11-03:值从裸 ID(#9729…)改成名字,所以 # 从模板里挪进了回退值
      rules: '规则',
      stageCountHint: '共 {n} 关(上限 10 关)· 规则在「规则」子页里配',
      stageDeleted: '关卡已删除',
      stageNameRequired: '请输入关卡名',
      newStageAutoRuleHint: '新关卡会自动带一条「部门主管 +1 级」的审批人规则(创建后可在「规则」子页里调整)',
    },
    // 以下是**页头描述**用的一组(只有 description,没有 f1..fn 功能清单)
    // 2026-10-25:这些页面的页头文案原来直接写死中文,英文环境下不跟着切 ——
    //   统一搬进字典,值 = 原来页面上显示的那句话(界面文案一个字没改)。
    profile: {
      description: '管理个人资料、安全设置、身份与登录记录',
      // 2026-10-25 Module B-2:个人中心整页的文案。
      // ⚠️ 挂在 page.profile 下而不是新建一个顶层 profile ——
      //   顶层已经有一个 profile: '个人中心'(路由标题用的字符串),同名会撞。
      // 左侧用户卡片
      notLoggedIn: '未登录',
      identityCount: '身份数',
      identityCountValue: '{n} 个',
      currentIdentity: '当前身份',
      // 头像上传(M124)
      avatarUpload: '更换头像',
      avatarSuccess: '头像已更新',
      avatarTooLarge: '头像不能超过 5 MB',
      avatarTypeError: '头像只支持 PNG / JPEG / GIF / WEBP / BMP 格式',
      // Tab 标题
      tabBasic: '基本信息',
      tabSecurity: '安全设置',
      tabIdentities: '我的身份',
      tabSessions: '登录设备',
      // 基本信息表单
      max32: '不超过 32 字',
      // ⚠️ M113:phoneChangeHint 已删 —— 手机号从「基本信息」页签移走了
      //   (手机号只有「安全设置 → 手机绑定」一个出处),这句提示没了归属。
      emailHint: '留空表示不修改邮箱',
      // 安全设置
      loginPassword: '登录密码',
      pwdTip: '定期更换密码可提高账号安全性',
      pwdReloginTip: '修改后需重新登录',
      changePassword: '修改密码',
      phoneBinding: '手机绑定',
      boundPrefix: '已绑定:',
      changePhone: '换绑手机',
      // 换绑手机号弹窗(C5,2026-10-29)
      bindPhoneTitle: '换绑手机号',
      bindPhoneNewLabel: '新手机号',
      bindPhoneNewPlaceholder: '请输入 11 位手机号',
      bindPhoneCodeLabel: '验证码',
      bindPhoneCodePlaceholder: '请输入 6 位验证码',
      bindPhoneCodeRequired: '请输入验证码',
      bindPhoneCodeLen: '验证码是 6 位数字',
      bindPhoneHint: '验证码会发到新手机号上,验证通过后旧手机号立即失效。',
      bindPhoneConfirm: '确认换绑',
      bindPhoneSuccess: '手机号已更新',
      mfa: '二次验证(MFA)',
      // ⚠️ M113(2026-10-29):MFA 以前只有一个"未开启"标签 + 一个禁用按钮,
      //   用户完全不知道它是什么。加了这三句把它讲清楚。
      mfaWhat: '二次验证(MFA)= 登录除了密码,还要再验一次身份',
      mfaWhy:
        '开启后,登录除了密码还要再验一条发到你绑定手机号上的短信验证码 —— 即使密码被别人知道了,对方没有你的手机也登不进你的账号。',
      statusPrefix: '状态:',
      notEnabled: '未开启',
      enabled: '已开启',
      enableMfa: '开启 MFA',
      disableMfa: '关闭 MFA',
      mfaNeedPhone: '开启二次验证前,请先在上方绑定手机号 —— 验证码要发到你绑定的手机上。',
      mfaEnableTitle: '开启二次验证',
      mfaEnableHint: '验证码会发到账号绑定的手机号上,验证通过后二次验证立即生效。',
      mfaEnableConfirm: '确认开启',
      mfaDisableTitle: '关闭二次验证',
      mfaDisableHint: '关闭后登录将只需密码。为确认是本人操作,请先验证一遍绑定手机号的短信验证码。',
      mfaDisableConfirm: '确认关闭',
      mfaCodeLabel: '短信验证码',
      mfaEnabled: '二次验证已开启',
      mfaDisabled: '二次验证已关闭',
      // 尚未实现的功能统一用这一句标 tooltip
      phase6: 'Phase 6 真实化',
      // 我的身份
      current: '当前',
      primaryIdentity: '主身份',
      validPeriod: '有效期',
      switchHint: '切换身份请用右上角身份切换器',
      // 改密弹窗
      confirmChange: '确认修改',
      pwdChanged: '密码修改成功,即将退出重新登录',
      pwdNeedLetter: '需含字母',
      pwdNeedDigit: '需含数字',
    },
    fileFolder: {
      description: '按业务段隔离的文件夹。文件夹里可以放文件,并指定一张封面(只有图片能当封面)。',
      bizSegment: '业务段',
      coverFileId: '封面文件 ID',
      coverKey: '封面 Key',
      createFolder: '新建文件夹',
      defaultFolder: '默认文件夹',
      defaultNotDeletable: '默认文件夹不能删(上传时会落到它)',
      deleteDescCount: ' 个文件(',
      deleteDescPrefix: '文件夹里的 ',
      deleteDescSub: '它下面的 {n} 个子文件夹(以及里面的文件)也会一起删掉。',
      deleteDescSuffix: ')会一起删掉,不可恢复。',
      deleteTitlePrefix: '删除文件夹 ',
      detailTitle: '文件夹详情',
      viewFiles: '查看文件',
      // ===== M125:多级文件夹 =====
      /** 面包屑的根节点 */
      rootDir: '根目录',
      /** 行操作:进入这个文件夹 */
      enter: '进入',
      /** 行操作:在这个文件夹下面建一个子文件夹 */
      createSubFolder: '新建子文件夹',
      /** 新建弹窗里提示"会建在哪" */
      createInPath: '将创建在「{path}」下面',
      /** 没有封面时的占位 */
      noCover: '无封面',
      /** 卡片视图里的文件数 —— M144 */
      fileCountValue: '{n} 个文件',
      /** 视图切换按钮的无障碍标签 —— M144 */
      viewMode: '视图',
      /** 表格视图 —— M144 */
      viewTable: '表格',
      /** 卡片视图 —— M144 */
      viewGrid: '卡片',
      /** 详情抽屉里的完整路径字段 */
      fullPath: '完整路径',
      /** 有关键字时列表跨层级搜,得说清楚 */
      searchAllLevels: '搜索:全部层级',
      // ===== M139:用户层 =====
      /** 用户层(面包屑的第一级,超管可见) */
      userLayer: '用户',
      /** 用户层的页面描述 */
      userLayerDescription: '按用户查看文件夹。点用户名进入该用户的文件夹空间(只读)。',
      /** 用户层的搜索框占位符 —— 搜的是"人",不是文件夹 */
      userLayerSearchPlaceholder: '搜索用户名 / 昵称 / 姓名',
      /** 行操作:进入这个用户的文件夹空间 */
      enterSpace: '进入空间',
      /** 归属人显示不出来时的兜底 */
      mySpace: '我的文件夹',
      /** 超管看自己的空间 */
      ownSpaceDescription: '{name} 的文件夹空间。',
      /** 超管看别人的空间(只读) */
      otherSpaceDescription: '{name} 的文件夹空间(只读)。',
      /** 别人的空间是只读的提示条 */
      readOnlySpace: '只读:这是别人的文件夹空间',
      // ===== 混合列表(2026-11-03 用户验收反馈:两个列表合成一个) =====
      /** 混合卡片体里"文件"分区的小标题 */
      filesTitle: '文件',
      /** 统计条:分页只管文件夹,文件总数常驻(翻页 / 搜索时也看得见) */
      mixedTotal: '文件夹 {folders} 个 · 文件 {files} 个',
      /** 文件超过一次拉取上限时的兜底入口(跳文件列表页,带 folder_id) */
      viewAllFiles: '查看全部 {n} 个文件',
    },
    fileList: {
      description: '当前账号上传的所有文件。目录按用户隔离,别人看不到这里的任何东西。',
      allFolders: '全部文件夹',
      clickOrDropUpload: '点击或拖拽上传',
      copyDirectLink: '复制直链',
      copyDirectLinkDone: '直链已复制到剪贴板',
      copyDirectLinkFailed: '复制失败,请手动复制地址',
      coverTag: '封面',
      coverOnlyImage: '只有图片才能设为封面',
      deleteDesc: '删除后存储里的对象一并清掉,配额同步释放,不可恢复。',
      detailTitle: '文件详情',
      download: '下载',
      /** 当前作用域里的文件个数(跟在文件夹筛选走) */
      fileCount: '{n} 个文件',
      folderId: '文件夹 ID',
      notInFolder: '该文件不在文件夹里',
      objectKey: '对象 Key',
      /** 缩略图视图:图片类文件拿不到直链(后端签不出来)或直链加载失败 */
      previewUnavailable: '无法预览',
      /* --- M145:浏览器能不能播(判据在后端 pkg/media,前端只搬运) --- */
      /** 徽标:这个文件浏览器播不了(悬浮显示后端给的原因) */
      mediaUnplayable: '浏览器不支持',
      /** 徽标:转码还没开始 */
      mediaTranscodePending: '待转码',
      /** 徽标:正在转码 */
      mediaTranscoding: '转码中',
      /** 徽标:已经有可播副本了 */
      mediaTranscoded: '已转码',
      /** 徽标:转码失败 */
      mediaTranscodeFailed: '转码失败',
      /** 配额环的悬浮说明:账号总用量 / 配额上限 */
      quotaTooltip: '总用量 {used} / 配额 {quota}',
      refreshQuota: '刷新配额',
      /** 选中文件夹时,搜索区那个用量标签的前缀 */
      scopeFolder: '本文件夹',
      setCover: '设为封面',
      setFolderCoverPrefix: '将 ',
      setFolderCoverSuffix: ' 设为所在文件夹的封面(同一文件夹只有一张,会被覆盖)。',
      setFolderCoverTitle: '设为文件夹封面?',
      storageLocal: '本地',
      storageOSS: 'OSS',
      /** 账号级总用量(不限配额时显示) */
      totalUsage: '总用量',
      uploadingPrefix: '正在上传 ',
      uploadingSuffix: ' 个文件',
      /** 视图切换(表格 / 缩略图)—— M134 */
      viewGrid: '缩略图',
      viewMode: '视图',
      viewTable: '表格',
    },
    flowStage: {
      description: '这一关的审批人从哪来:指定人 / 指定角色 / 申请人的部门主管',
      addRule: '新增规则',
      approverSource: '审批人来源',
      backToStage: '返回关卡',
      deleteRuleDesc: '整棵模板会被重新写入,不是只删这一行。',
      deleteRuleTitle: '删除这条规则?',
      deptLeaderApprover: '申请人的直属主管 +{n} 级',
      editRule: '编辑规则',
      levelsUp: '往上几级',
      multiRuleHint: '同一关里的多条规则是「或」关系 —— 命中任意一条就算有人能审',
      ruleDeleted: '规则已删除',
      ruleSourceDeptLeader: '部门主管(申请人的直属上级)',
      ruleSourceDeptLeaderTag: '部门主管',
      ruleSourceRole: '指定角色',
      ruleSourceRoleTag: '指定角色',
      ruleSourceUser: '指定人',
      ruleSourceUserTag: '指定人',
      stageTitleWithRules: '{name} · 审批人规则',
    },
    configAudit: {
      description: '只读审计记录 — super_admin 每次修改配置自动写入 sys_config_audit',
      descriptionFiltered: '当前分组:{group}(只显示该分组下配置项的变更记录)',
      afterValue: '变更后',
      beforeValue: '变更前',
      clearGroupFilter: '清除分组过滤',
      detailTitle: '审计详情',
      groupTag: '分组:{group}',
      userFallback: '用户#{id}',
    },
    // 动态业务集合(M152)—— 业务表单设计器 + 通用记录列表页
    //
    // ⚠️ **页面里不许写死中文** —— 见 locales/__tests__/hardcoded-chinese-i18n.test.ts。
    dyn: {
      designer: {
        description:
          '业务表单设计器 —— 设计好集合与字段后,菜单、增删改查接口、权限码都会自动生成',
        collections: '业务集合',
        newCollection: '新建集合',
        editCollection: '编辑集合',
        emptyCollections: '还没有集合 —— 点右上角「新建集合」开始',
        searchPlaceholder: '搜索集合名称 / 编码',
        fields: '字段',
        newField: '新建字段',
        editField: '编辑字段',
        emptyFields: '这个集合还没有字段',
        selectCollectionFirst: '请先在左侧选择一个集合',
        selectCollectionHint: '在左侧选择一个集合,即可在这里维护它的字段',
        code: '集合编码',
        codeHelp:
          '小写字母开头,只能含小写字母 / 数字 / 下划线。创建后不可修改 —— 它同时是数据表名和权限码的来源',
        codeInvalid: '集合编码只能是小写字母开头的小写字母 / 数字 / 下划线',
        name: '集合名称',
        descriptionField: '描述',
        ownerScoped: '仅创建人可见',
        ownerScopedHelp: '开启后,普通业务人员只能看到自己创建的记录(超管不受限)',
        // 公开只读(2026-11-03)—— 与「仅创建人可见」互斥
        publicRead: '公开只读(免登录)',
        publicReadHelp:
          '开启后,任何人无需登录即可通过公开接口读取这张表的表单结构和记录(只读,写入仍需登录)。与「仅创建人可见」互斥,只能选一个',
        icon: '菜单图标',
        sort: '排序',
        // 集合绑定审批模板(2026-11-03)
        approvalTemplate: '审批模板',
        approvalTemplateHelp:
          '绑定后,该集合的记录可以送进这条审批流(记录列表出现"送审"动作与审批状态列);不绑定则没有送审能力',
        approvalTemplateNone: '不绑定',
        fieldKey: '字段标识',
        fieldKeyHelp:
          '小写英文标识,创建后不可修改 —— 它同时是数据库列名和记录 JSON 的键',
        fieldKeyInvalid: '字段标识只能是小写字母开头的小写字母 / 数字 / 下划线',
        label: '显示名',
        type: '字段类型',
        typeHelp: '创建后不可修改 —— 改类型等于改列类型(要重建表、搬数据)',
        required: '必填',
        options: '选项集',
        optionsHelp: '单选 / 多选字段的候选项。值是存进数据库的东西,显示名是给人看的',
        constraints: '值约束',
        constraintsHelp:
          '录入时的限制,所有写入入口(新增/编辑/导入/批量)都会校验;不填 = 不启用。文本长度按字数算(中文算 1 个字)',
        minLength: '最小长度(字)',
        maxLength: '最大长度(字)',
        pattern: '格式约束(正则)',
        patternHelp: 'Go 正则语法,整串匹配。例:^010-\\d{8}(以 010- 开头加 8 位数字)',
        minValue: '最小值',
        maxValue: '最大值',
        // 跨集合关联字段(M5)
        relation: '关联配置',
        relationHelp: '引用其它业务集合的记录:存的是目标记录的 id,显示的是目标记录的某个字段;允许引用本集合自己(如"任务的父任务")',
        relationTargetPlaceholder: '选择目标集合',
        relationDisplayPlaceholder: '显示目标记录的哪个字段(不选 = 自动挑第一个文本字段)',
        relationDisplayHelp: '目标记录在表单下拉框和列表里显示成这个字段的值;显示字段被停用时会自动回落',
        relationTargetMissing: '目标集合 {code} 不存在或已停用 —— 保存前请修正',
        // 集合级索引(M2)
        indexes: '索引配置',
        indexesHelp:
          '为物理表建数据库索引(唯一索引可挡重复录入);引用的字段停用后,依赖它的索引会一并删除',
        indexesNoFields: '先添加字段,才能配置索引',
        indexName: '索引名(小写英文)',
        indexColumns: '索引列(可多选,顺序即复合索引顺序)',
        indexUnique: '唯一',
        addIndex: '添加索引',
        defaultValue: '默认值',
        defaultValueHelp: '新增记录时留空就填这个值',
        deleteCollectionTip:
          '删除后集合停用、菜单与权限码一并停用;物理表与已有数据保留,不会被物理删除',
        deleteFieldTip: '删除后字段停用,列表页与表单里不再出现;物理列与已有数据保留',
        fieldType: {
          text: '单行文本',
          longtext: '多行文本',
          int: '整数',
          number: '小数',
          bool: '开关',
          date: '日期',
          datetime: '日期时间',
          select: '单选',
          multiselect: '多选',
          json: 'JSON',
          file: '附件',
          relation: '关联记录',
        },
      },
      options: {
        empty: '还没有候选项',
        valuePlaceholder: '选项值(存进数据库的值)',
        labelPlaceholder: '显示名',
        remove: '删除这个候选项',
        add: '添加候选项',
        duplicate: '有候选项的值重复了 —— 一条记录到底存哪一个会说不清',
        // 「手工添加」vs「关联系统表」(2026-11-02)
        //   两种来源**互斥**:手工的候选项写死在字段定义里,
        //   关联表的候选项是读记录时现查的(表里多一行,表单里就多一个选项)。
        manual: '手工添加',
        table: '关联系统表',
        sourcePlaceholder: '选择一张系统表作为选项来源',
        sourceTable: '选项取自 {table} 表的 {column} 列,显示名取同一条记录的可读名称',
        sourceUnreadable: '当前账号没有读这张表的权限({perm})—— 配了也取不到选项',
      },
      records: {
        description: '通用业务记录 —— 列与表单都按设计器里定义的字段自动生成',
        searchPlaceholder: '在文本类字段里搜索',
        // 高级筛选(M4)
        filterPlaceholder: "高级筛选:status = 'active' && amount > 100",
        filterTitle:
          "筛选表达式语法:比较 = != > >= < <=、包含 ~ !~、多值 ?= ('a','b')、空值 = null,逻辑 && || ( )。字段名写字段标识,不存在的字段会报错",
        newRecord: '新增记录',
        editRecord: '编辑记录',
        detailTitle: '记录详情',
        deleteTip: '删除后这条记录不再出现在列表里(数据仍保留在库里)',
        jsonInvalid: '不是合法 JSON',
        schemaFailed: '取不到这个集合的表结构',
        noSchema: '这个集合还没有可用字段',
        // 附件字段(2026-11-02)
        //   字段里存的是**文件的 key**(不带域名的路径),不是 URL。
        file: {
          upload: '上传文件',
          reupload: '重新上传',
          clear: '清除',
          missing: '文件已失效(记录里的文件找不到了)',
          uploadFailed: '文件上传失败',
          // ⚠️ 没有 file:upload 时上传按钮不渲染(跟文件列表页同一口径)
          //   —— 所以得说清楚"为什么没有上传按钮",不然用户以为坏了。
          noUploadPerm: '没有上传文件的权限,不能传附件',
        },
        // 「API 接入」说明弹窗(2026-11-02)
        //   接口是设计器建集合那一刻自动生成的,但用户看不见 ⇒ 给他一份能抄的说明。
        apiGuide: {
          button: 'API 接入',
          title: 'API 接入说明',
          intro:
            '这个集合的增删改查接口在设计器建好集合的那一刻就自动生成了 —— 按下面的路径调用即可,不用再写后端代码。',
          bodyKey:
            '请求体的键是「字段标识」(设计器里填的那个小写英文标识),不是数据库列名;值的写法由字段类型决定。',
          collectionCode: '集合编码',
          basePath: '基础路径',
          auth: '认证方式',
          endpoints: '接口清单',
          fields: '字段一览',
          example: '调用示例',
          requestBody: '请求体',
          response: '响应',
          epSchema: '取表单结构(这个集合有哪些字段)',
          epList: '记录列表(分页 / 关键字 / 排序)',
          epRead: '记录详情',
          epCreate: '新建记录',
          epUpdate: '修改记录(只改请求体里出现的字段)',
          epDelete: '删除记录(软删,数据仍保留在库里)',
          sdkTitle: 'JS SDK(推荐)',
          sdkDesc:
            '不想手拼 HTTP 就用官方零依赖 SDK(@rapid-admin/dyn-sdk):自动挂 token、解析统一信封、抛带 trace_id 的错误;下面的片段可直接抄走。',
        },
        // 行级批量操作 + 导入导出(2026-11-03)
        trash: '回收站',
        trashOn: '返回列表',
        restore: '恢复',
        restoreTip: '恢复后这条记录回到普通列表',
        batchDelete: '批量删除',
        batchDeleteTip: '删除后这些记录进入回收站(数据仍保留在库里)',
        batchEdit: '批量修改',
        batchEditTip: '把选中的 {n} 条记录的同一个字段改成同一个值',
        batchEditSelectField: '选择要修改的字段',
        batchEditValuePlaceholder: '输入新值',
        batchEditValueJson: 'JSON 数组,如 ["a","b"]',
        export: '导出',
        import: '导入',
        importTitle: '导入记录(CSV)',
        importSelectFile: 'CSV 表头用字段标识或显示名都认;单次最多 5000 行 / 5MB',
        importNoFile: '未选择文件',
        importMode: '导入语义',
        importModeAll: '全有才进',
        importModeAllTip: '任何一行有错误,整个文件都不导入',
        importModeSkip: '跳过错误行',
        importModeSkipTip: '跳过有错误的行,只导入合法的行',
        importStart: '开始导入',
        importResultStat: '共 {total} 行,导入 {inserted} 行,跳过 {skipped} 行',
        // importErrorRow/Field/Message 2026-11-03 移到 field.* 组(列标题共享组)
        importNoErrors: '没有错误行',
        // 送审 + 审批状态(2026-11-03,集合绑定审批模板)
        submitApproval: '送审',
        approvalSubmitted: '已提交审批',
        approvalNone: '未送审',
      },
    },
  },
  // HTTP 客户端错误消息
  error: {
    networkError: '网络异常,请检查网络连接',
    unauthorized: '登录已过期,请重新登录',
    forbidden: '没有权限执行此操作',
    serverError: '服务器开了小差,请稍后重试',
    validateFailed: '参数校验失败',
    notFound: '资源不存在',
    timeout: '请求超时,请稍后重试',
    requestFailed: '请求失败',
    businessError: '业务错误',
    unknownError: '未知错误',
    operationFailed: '操作失败',
    requestFailedWithStatus: '请求失败 ({status})',
    networkException: '网络异常',
    // ErrorBoundary 的兜底 UI(不是 HTTP 错误页)
    boundaryTitle: '页面出错了',
    boundaryReload: '刷新页面',
    // M109 致命兜底 UI(不依赖 antd / ConfigProvider)
    fatalTitle: '应用遇到严重错误',
    fatalDesc: '页面无法继续显示,请刷新页面重试;若问题反复出现,请联系系统管理员。',
    // M10 错误页
    page403: {
      title: '403',
      subTitle: '抱歉,您无权访问此页面',
      backHome: '返回首页',
      hint: '如需访问请联系系统管理员申请权限',
    },
    page404: {
      title: '404',
      subTitle: '抱歉,您访问的页面不存在',
      backHome: '返回首页',
      hint: '请检查 URL 是否正确,或返回首页',
    },
    page500: {
      title: '500',
      subTitle: '抱歉,服务器开了小差',
      retry: '刷新重试',
      copy: '复制错误码',
      copied: '已复制到剪贴板',
      // 错误码按钮上的文案(2026-10-25 Module B)
      codeLabel: '错误码: {code}',
    },
  },
  // 后端业务码 → 翻译文案(M6)
  // 命名空间:bizCode.<后端码>
  //   - 与 server/internal/pkg/errcode/errcode.go 的 defaultMsg 一一对齐
  //   - 前端比后端更友好(更白话),允许改写
  //   - 新增业务码:同时在 types/api.ts::BIZ_CODE_KEYS 和这里登记
  bizCode: {
    // 4xxxx 基础
    '40000': '请求参数错误',
    '40001': '必填参数缺失',
    '40002': '参数格式错误',
    '40003': '参数无效',
    '40100': '未登录或登录已过期',
    '40101': '登录已过期,请重新登录',
    '40102': '登录凭证无效,请重新登录',
    '40300': '您没有权限执行此操作',
    '40301': '当前身份已停用',
    '40302': '此操作超出您的工作范围',
    '40303': '您无权访问该资源',
    '40400': '请求的资源不存在',
    '40900': '数据已变更,请刷新后重试',
    '41300': '请求体超过限制',
    '42900': '操作过于频繁,请稍后再试',
    // 50000 服务内部错误
    '50000': '服务内部错误,请稍后重试',
    // 2xxxx 业务
    '20100': '登录失败',
    '20101': '账号已锁定,请稍后重试或联系管理员',
    '20102': '图形验证码错误',
    '20103': '请先通过图形验证码',
    '20104': '短信发送过于频繁',
    '20105': '短信验证码错误或已过期',
    '20108': '短信发送过于频繁,请稍后再试',
    '20109': '该账号已开启二次验证,请输入短信验证码',
    '20106': '密码强度不足',
    '20107': '密码已过期,请修改',
    '20200': '提交人与审核人不能为同一人',
    '20201': '不能给自己授予更高权限',
    '20202': '不能授予超出自身范围的权限',
    '20203': '当前状态不允许此操作',
    '20601': '审批流程配置有误:解析不出审批人,请联系管理员检查流程配置',
    '20602': '部门架构数据异常(成环或上级不存在),审批无法定位主管,请联系管理员修复组织架构',
    '20603': '审批人规则配置有误,请联系管理员检查审批流程配置',
    // 3xxxx 平台配置
    '30100': '配置项不存在',
    '30101': '该配置项为只读,无法修改',
    '30200': '存在未审批的配置变更',
    '30201': '变更未审核',
    '30202': '申请人与审批人不能为同一人',
    '30203': '配置变更状态不允许此操作',
    // 5xxxx 框架
    '50100': '该功能尚未实现',
    '50300': '服务暂时不可用,请稍后重试',
    '50400': '请求处理超时,请稍后重试',
    '50500': '请求的 API 版本不被支持',
    '50600': '缺少接口签名',
    '50601': '接口签名已过期,请重试',
    '50602': '接口签名校验失败',
    '50700': '请求体格式错误',
    '50701': '不支持的 Content-Encoding(仅支持 gzip/identity)',
  },
  // 字典标签 (constants/dict.ts)
  dict: {
    status: { active: '启用', disabled: '禁用', locked: '锁定' },
    userStatus: { active: '正常', disabled: '禁用', locked: '锁定' },
    gender: { male: '男', female: '女', unknown: '未知' },
    configChangeStatus: { pending: '待复核', approved: '已通过', rejected: '已拒绝', expired: '已过期' },
  },
  // 角色名 (constants/roles.ts ROLE_NAME_MAP)
  // ⚠️ 这几个键与前端 `RoleCode` 的角色**一一对应**,与后端 seed 的 code 同名。
  role: {
    superAdmin: '超级管理员',
    readonly: '只读查看员',
    platformOps: '平台运营员',
    // 2026-10-25 Module B-2:个人中心用户卡片上的"没有特殊角色"标签
    normalUser: '普通用户',
  },
}

export default zhCN
export type Locale = typeof zhCN
