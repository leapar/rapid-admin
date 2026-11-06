/**
 * English copy
 *
 * Key naming follows zh-CN.ts. See that file's header for the
 * `resource.*` key derivation rule from backend `sys_resource.code`.
 */
import type { Locale } from './zh-CN'

const enUS: Locale = {
  common: {
    // Browser tab title — written into document.title at runtime by
    // LangRoot (main.tsx). See zh-CN.ts for the matching note.
    // ⚠️ **Fallback only** — the server's `branding.app_title` wins when set
    //   (open-source generalisation: rename the platform in server yaml).
    appTitle: 'Admin Console',
    ok: 'OK',
    cancel: 'Cancel',
    save: 'Save',
    submit: 'Submit',
    reset: 'Reset',
    search: 'Search',
    add: 'Add',
    edit: 'Edit',
    delete: 'Delete',
    detail: 'Detail',
    export: 'Export',
    import: 'Import',
    refresh: 'Refresh',
    revoke: 'Revoke',
    enable: 'Enable',
    disable: 'Disable',
    confirm: 'Confirm',
    success: 'Success',
    failed: 'Failed',
    loading: 'Loading...',
    noData: 'No Data',
    // No users (2026-10-25 Module B-5c) — empty state of the department page's
    // "leader" dropdown.
    noUsers: 'No users',
    all: 'All',
    yes: 'Yes',
    no: 'No',
    phone: 'Phone',
    email: 'Email',
    name: 'Name',
    remark: 'Remark',
    status: 'Status',
    action: 'Action',
    avatar: 'Avatar',
    createTime: 'Created At',
    updateTime: 'Updated At',
    // Creator (2026-11-02 dyn record page) — shows the **name**, not owner_id
    creator: 'Created By',
    // Permission code (which row to tick for a dynamic collection endpoint)
    permCode: 'Permission Code',
    // Description (the column in the endpoint list)
    desc: 'Description',
    // Optional (pairs with field.required "Required")
    optional: 'Optional',
    total: 'Total {n} items',
    // 批量操作(§2.8 #14,BatchActionBar 用)
    selected: '{n} selected',
    clearSelection: 'Clear selection',
    // 列表错误态(M7:TableErrorState / FallbackPlaceholder 用)
    loadFailed: 'Failed to load',
    retry: 'Retry',
    // 保存失败(2026-10-25 Module B-5a)—— 用户管理页 + 角色分配抽屉两处共用
    saveFailed: 'Save failed',
    // 删除失败(2026-10-25 Module B-5b)—— 资源管理页用
    deleteFailed: 'Delete failed',
    // 关闭(2026-10-25 Module B-5b)—— 资源分配抽屉的关闭按钮 + 报修列表的行操作
    close: 'Close',
    // 计数括号(2026-10-25 Module B-5b)—— "(3 items)" 这种写法在两处出现
    countItems: '({n} items)',
    // 值占位 / 通用动作(2026-10-25 Module B)
    // ⚠️ 这几个字面量在**好几个页面**出现(审计日志、配置审计、任务、身份编辑…),
    //   各写一份必然漏翻译 —— 所以提到 common。
    emptyValue: '(empty)',
    resetDefault: 'Reset to default',
    switch: 'Switch',
    // Status words (2026-10-25 Module B-2)
    enabled: 'Enabled',
    disabled: 'Disabled',
    // Result badges (the two words on a status tag).
    // NOTE: distinct from common.success, which is the "operation succeeded"
    // toast message - different words, different meaning, don't merge them.
    resultSuccess: 'Success',
    resultFailed: 'Failed',
    // placeholder —— 输入框 / 下拉框 / 日期选择器的占位提示(2026-10-25)
    // 写死中文的话,英文界面下这些提示仍是中文 —— 全仓统一走 t()。
    placeholder: {
      searchUser: 'Search username / nickname / phone',
      searchRole: 'Search code / name',
      searchPermission: 'Search code / name / path',
      searchResource: 'Search code / name / path',
      searchDict: 'Search dict key / value / label',
      searchConfig: 'Search config key / name',
      searchConfigAudit: 'Search config key (config_key)',
      searchDept: 'Search department name (fuzzy match supported)',
      searchJob: 'Search task key / name / remark',
      searchApproval: 'Search title',
      searchAuditOp: 'Search user / IP / description',
      searchAuditLogin: 'Search username',
      searchAuditChange: 'Search table name',
      searchFile: 'Search file name',
      searchFolder: 'Search folder name',
      searchFlow: 'Search code / name',
      searchIssue: 'Search title / description',
      searchMaterial: 'Search title / category',
      searchNotice: 'Search title / content',
      searchOwner: 'Search name / phone / ID number',
      // 系统日志(2026-11-03)—— keyword hits path / message columns
      searchAuditSystem: 'Search path / summary',
      filterLevel: 'Filter by level',
      filterSource: 'Filter by source',
      filterStatus: 'Filter by status',
      filterOperation: 'Filter by operation',
      filterModule: 'Filter by module',
      filterFolder: 'Filter by folder',
      filterType: 'Filter by type',
      filterDictGroup: 'Select dict group (all by default)',
      allStatus: 'All statuses',
      status: 'Status',
      triggerType: 'Trigger type',
      parentDept: 'Select parent department (blank or root = top level)',
      topLevelDefault: 'Leave blank for top level',
      nickname: 'Enter nickname',
      // 2026-10-25 Module B-5b: code/name validation prompts of the resource
      // management page and the role management page (shared by both).
      code: 'Enter the code',
      name: 'Enter the name',
      realName: 'Enter real name',
      email: 'Enter email',
      oldPassword: 'Enter current password',
      newPassword: 'Enter new password (8+ chars, letters and digits)',
      confirmPassword: 'Re-enter the new password',
      approvalTitle: 'Title',
      approvalReason: 'Reason / context (JSON text)',
      selectRole: 'Select role',
      optionalDept: 'Bind a department (optional)',
      platformAll: '0 = whole platform',
      effectiveNow: 'Effective immediately',
      forever: 'Never expires',
      cronExpr: '0 3 * * * or @daily / @every 1h',
      captcha: 'Enter the captcha',
      keepUnchanged: 'Leave blank to keep the current value',
      currentlyEmpty: 'Currently empty',
      selectOwner: 'Select an owner (search by name)',
      approvalComment: 'Approval comment (required for reject / rollback, at least {n} characters)',
      rejectReason: 'Please explain the rejection reason (at least {n} characters)',
      // 2026-10-30 shared time-range picker of the three audit pages
      rangeStart: 'Start date',
      rangeEnd: 'End date',
    },
    auditApprove: 'Approve',
    auditReject: 'Reject',
    publish: 'Publish',
    saveSuccess: 'Saved successfully',
  },

  // field —— field labels (2026-10-25)
  // Shared on purpose: the same label shows up in many pages.
  // Used for both table column headers and form field labels.
  field: {
    title: 'Title',
    ip: 'IP',
    type: 'Type',
    sort: 'Sort',
    // 2026-11-02 M152 business form designer -- "Required" column
    required: 'Required',
    approver: 'Approver',
    operator: 'Operator',
    time: 'Time',
    bizType: 'Business Type',
    biz: 'Business',
    index: 'No.',
    name: 'Name',
    priority: 'Priority',
    handleTime: 'Handled At',
    logAction: 'Action',
    comment: 'Comment',
    currentStage: 'Current Stage',
    initiator: 'Initiator',
    rolledBack: 'Rolled Back',
    submitTime: 'Submitted At',
    user: 'User',
    module: 'Module',
    // M164 字段可见性抽屉的字段表列标题(M173 补:列标题必须落在 field.*/common.* 共享组)
    fieldName: 'Field',
    visibility: 'Visibility',
    maskType: 'Mask type',
    request: 'Request',
    durationMs: 'Duration (ms)',
    folderName: 'Folder Name',
    /** User layer: how many folders this user owns */
    folderCount: 'Folder Count',
    fileCount: 'File Count',
    totalSize: 'Total Size',
    isDefault: 'Default',
    cover: 'Cover',
    childFolders: 'Subfolders',
    fileName: 'File Name',
    size: 'Size',
    storage: 'Storage',
    uploadTime: 'Uploaded At',
    stageName: 'Stage Name',
    passType: 'Pass Method',
    approverRule: 'Approver Rule',
    code: 'Code',
    stageCount: 'Stage Count',
    source: 'Source',
    description: 'Description',
    // Voice management (2026-11-06) -- column titles must live in the shared groups
    voiceId: 'Voice ID',
    ttsType: 'Platform',
    gender: 'Gender category',
    targetModel: 'Target model',
    handler: 'Handler',
    category: 'Category',
    file: 'File',
    version: 'Version',
    reviewer: 'Reviewer',
    reviewComment: 'Review Comment',
    publisher: 'Publisher',
    publishTime: 'Published At',
    expireTime: 'Expires At',
    readCount: 'Read Count',
    realName: 'Real Name',
    idCard: 'ID Card',
    houseCount: 'Houses',
    realNameStatus: 'Verification',
    device: 'Device',
    // ⚠️ 2026-10-30 added: browser / os in the login log (product names parsed from the UA)
    browser: 'Browser',
    os: 'Operating System',
    result: 'Result',
    loginTime: 'Login Time',
    lastActivity: 'Last Activity',
    offlineTime: 'Offline At',
    opTime: 'Operated At',
    configKey: 'Config Key',
    changedBy: 'Changed By',
    changeDiff: 'Before → After',
    group: 'Group',
    dictKey: 'Dict Key',
    dictValue: 'Dict Value',
    displayName: 'Display Name',
    color: 'Color',
    dictDescription: 'Description',
    startTime: 'Start Time',
    trigger: 'Trigger',
    duration: 'Duration',
    /**
     * Media length (M141)
     *
     * ⚠️ **Not the same as `duration`** — that one is "how long the job took",
     *   this one is "how long this video is".
     */
    mediaDuration: 'Length',
    jobKey: 'Job Key',
    cronExpr: 'Cron Expression',
    nextRun: 'Next Run',
    timeout: 'Timeout',
    resourceCode: 'Resource Code',
    resourceName: 'Resource Name',
    path: 'Path',
    method: 'Method',
    roleCode: 'Role Code',
    roleName: 'Role Name',
    builtin: 'Built-in',
    account: 'Account',
    nickname: 'Nickname',
    department: 'Department',
    lastLogin: 'Last Login',
    departmentName: 'Department Name',
    departmentPath: 'Department Path',
    departmentCode: 'Department Code',
    leader: 'Leader',
    memberCount: 'Members',
    groupCode: 'Group Code',
    oldPassword: 'Current Password',
    newPassword: 'New Password',
    confirmPassword: 'Confirm New Password',
    role: 'Role',
    // SMS template review status (M115) -- column titles live under the
    // shared field group, see table-column-i18n.test.ts.
    scene: 'Scene',
    templateCode: 'Template Code',
    reviewStatus: 'Review Status',
    usable: 'Usability',
    reason: 'Reason',
    reviewedAt: 'Reviewed At',
    // 2026-10-30 -- the status column only says "not synced";
    // this column says *why*.
    lastSyncError: 'Last Sync Error',
    statusChange: 'Status Change',
    // Audit center pages (2026-10-30) -- login log / data change log /
    // operation log share these field labels.
    userId: 'User ID',
    tableName: 'Table Name',
    recordId: 'Record ID',
    operation: 'Operation',
    sessionId: 'Session ID',
    userAgent: 'User-Agent',
    traceId: 'Trace ID',
    // 系统日志(2026-11-03)—— source reuses existing field.source;
    // durationMs reuses field.durationMs
    level: 'Level',
    requestId: 'Request ID',
    message: 'Message',
    beforeValue: 'Before',
    afterValue: 'After',
    // 2026-10-30 M122: the "Permissions" column in the user list.
    // MUST live under the `field` group — table column titles may only use
    // field.* / common.* (see table-column-i18n.test.ts).
    // NOT field.role (that one is the edit drawer's form label for a single
    // role); this column lists every role the person currently holds.
    roles: 'Permissions',
    // 2026-11-03 M177 dyn import-result modal's error-rows table — column
    // titles must live under field.* per the M164 convention (page.dyn.records.*
    // gets flagged by table-column-i18n.test.ts).
    importErrorRow: 'Row',
    importErrorField: 'Field',
    importErrorMessage: 'Error',
    // 2026-11-03 approval-status column title on the dyn record list (must live under field.*)
    approvalStatus: 'Approval',
  },
  // Shared component copy (2026-10-25 Module B)
  // These components are reused across pages, so their copy is reused too --
  // keeping it under page.* would duplicate it per page and drift.
  component: {
    captcha: {
      refreshAria: 'Refresh captcha',
      refreshTitle: 'Click to refresh',
    },
    columnSettings: {
      title: 'Column Settings',
      done: 'Done',
      width: 'Width',
      storageHint: 'Column settings are stored in the browser localStorage (restored automatically on the same browser and key)',
    },
    configDiff: {
      title: 'Change Comparison',
      sensitiveHint:
        'This field is marked sensitive; the value is masked here and only takes effect after the change is approved',
      old: 'Old',
      new: 'New',
    },
    identityEditor: {
      title: 'Multi-identity Editor',
      readOnly: 'Read-only',
      add: 'Add Identity',
      roleRequired: 'Every identity must have a role',
      primaryRequired: 'There must be exactly one primary identity',
      loading: 'Loading roles/departments...',
      emptyHint: "No identity configured yet. Click 'Add Identity' in the top right to start",
      emptyReadOnly: 'This user has no identity',
      primaryBadge: '⭐ Primary',
      alreadyPrimary: 'Already the primary identity',
      setPrimary: 'Set as primary identity',
      copyRow: 'Copy this row',
      deleteRow: 'Delete this row',
      projectId: 'Project ID',
      platformAll: 'Whole platform',
      validFrom: 'Valid from',
      validTo: 'Valid to',
      disableUnsupported: 'The backend cannot disable a single identity yet',
    },
    multiDept: {
      placeholder: 'Pick multiple departments, ⭐ marks the primary one',
      empty: 'No department set',
      alreadyPrimary: 'Already the primary department',
      setPrimary: 'Set as primary department',
      selectedHint: '{n} selected, ⭐ marks the primary one ({name})',
      autoFirst: 'the first one by default',
    },
    offline: {
      banner: 'The network is down. Offline mode is active and some features are unavailable',
    },
    secretInput: {
      show: 'Show plaintext',
      hide: 'Hide plaintext',
    },
    sensitiveField: {
      viewPlain: 'View plaintext',
      hide: 'Hide',
    },
    tablePro: {
      exportAria: 'Export the current data as CSV',
      exportCsv: 'Export CSV',
      columnSettingsAria: 'Choose visible columns and widths',
      columnSettings: 'Column Settings',
    },
    identitySwitcher: {
      switchedTo: 'Switched to {name}',
      highSensitive: 'High risk',
      switchTitle: 'Switch to a high-risk identity',
      // Split in two: the role name in between is bold and highlighted
      switchToPrefix: 'Switch to',
      switchToSuffix: 'requires SMS verification',
      // M146b: the identity switch itself succeeded, but the permission list
      // could not be loaded (usually: the new identity has too few rights)
      permissionRefreshFailed: 'Identity switched, but the permission list failed to load; the menu may be incomplete',
    },
  },
  // Resource types (the three fixed sys_resource.type values: menu / button / api)
  resourceType: {
    menu: 'Menu',
    button: 'Button',
    api: 'API',
  },
  // Resource icon vocabulary (@/constants/resourceIcons key -> English label).
  // ⚠️ The keys must match the vocabulary one by one -- a missing key shows the
  //   raw English key in the dropdown, an extra key is a value the backend
  //   does not know (the sidebar then renders it as bare text).
  resourceIcon: {
    dashboard: 'Dashboard',
    user: 'User',
    team: 'Team',
    role: 'Role',
    permission: 'Permission',
    dept: 'Department',
    dict: 'Dictionary',
    config: 'Configuration',
    job: 'Scheduled job',
    file: 'File',
    audit: 'Audit',
    system: 'System',
    security: 'Security',
    flow: 'Flow',
    approval: 'Approval',
    // Approval management (parent menu added 2026-11-01) -- must match the
    // approvalMgmt key in constants/resourceIcons.ts.
    approvalMgmt: 'Approval Management',
    // SMS template (M115) -- must match the sms key in constants/resourceIcons.ts,
    // otherwise the resource-edit dropdown shows a raw English key.
    sms: 'SMS',
    // API docs (§8 G5) -- must match the api key in constants/resourceIcons.ts.
    api: 'API',
    // Business form designer / dynamic collections (M151) -- must match the
    // table key in constants/resourceIcons.ts (it is also the default icon
    // of the per-collection menus menu.dyn.<code>).
    table: 'Business table',
    skin: 'Skin editor',
    voice: 'Voice',
    // -- Icon picker expansion (2026-11-02) -- one-to-one with constants/resourceIcons.ts
    // Office
    form: 'Form',
    profile: 'Profile',
    reconciliation: 'Ledger',
    schedule: 'Schedule',
    search: 'Search',
    chart: 'Report',
    monitor: 'Monitor',
    // Business objects
    building: 'Building',
    car: 'Vehicle',
    tool: 'Repair',
    phone: 'Phone',
    mail: 'Mail',
    contacts: 'Contacts',
    idcard: 'ID card',
    solution: 'Solution',
    customerService: 'Support',
    shopping: 'Mall',
    cart: 'Cart',
    rest: 'Dining',
    truck: 'Logistics',
    medicineBox: 'Medical',
    experiment: 'Inspection',
    // Finance
    accountBook: 'Book',
    wallet: 'Wallet',
    payCircle: 'Payment',
    creditCard: 'Bank card',
    moneyCollect: 'Collection',
    transaction: 'Transaction',
    fund: 'Funds',
    // Data & files
    database: 'Data',
    cloud: 'Cloud',
    fileSearch: 'File search',
    fileAdd: 'New file',
    folderOpen: 'Folder',
    import: 'Import',
    export: 'Export',
    upload: 'Upload',
    download: 'Download',
    print: 'Print',
    save: 'Save',
    paperClip: 'Attachment',
    // Messaging
    comment: 'Comment',
    notification: 'Broadcast',
    send: 'Send',
    share: 'Share',
    qrcode: 'QR code',
    // Marks & states
    star: 'Star',
    heart: 'Favorite',
    trophy: 'Trophy',
    tag: 'Tag',
    pushpin: 'Pinned',
    environment: 'Location',
    rocket: 'Launch',
    alert: 'Alert',
    insurance: 'Insurance',
    safetyCertificate: 'Certificate',
    lock: 'Locked',
    // Media & devices
    camera: 'Camera',
    picture: 'Image',
    video: 'Video',
    robot: 'AI',
    mobile: 'Mobile',
  },
  // tagColor -- antd Tag color whitelist (color dropdown on the dict page)
  tagColor: {
    none: 'None',
    default: 'Default',
    red: 'Red',
    orange: 'Orange',
    yellow: 'Yellow',
    green: 'Green',
    blue: 'Blue',
    purple: 'Purple',
    magenta: 'Magenta',
    cyan: 'Cyan',
    gold: 'Gold',
    lime: 'Lime',
    volcano: 'Volcano',
    geekblue: 'Geekblue',
  },
  validation: {
    required: 'This field is required',
    pleaseInput: 'Please enter {label}',
    pleaseSelect: 'Please select {label}',
    usernameRequired: 'Please enter username / job number',
    passwordRequired: 'Please enter password',
    phoneRequired: 'Please enter phone number',
    phoneInvalid: 'Please enter a valid 11-digit phone number',
    smsCodeRequired: 'Please enter the verification code',
    smsCodeLen: '{n} digits',
    smsCodePlaceholder: '{n}-digit SMS code',
    phonePlaceholder: '11-digit phone number',
    newPasswordRequired: 'Please enter a new password',
    passwordStrength: 'Password must be 8-32 chars and contain letters and digits',
    newPasswordPlaceholder: '8-32 chars, with letters and digits',
    confirmPasswordRequired: 'Please re-enter the new password',
    confirmPasswordPlaceholder: 'Re-enter new password',
    passwordMismatch: 'The two passwords do not match',
    // 2026-10-25 Module B-2: same wording in the profile password form and
    // in the admin "create user" form
    emailInvalid: 'Invalid email format',
    passwordMinLength: 'At least 8 characters',
    captchaRequired: 'Please enter the captcha',
  },
  message: {
    loginSuccess: 'Login successful',
    resetPasswordSuccess: 'Password reset successful, please log in with the new password',
    uploadFailed: 'Upload failed',
  },
  sms: {
    send: 'Send Code',
    getCode: 'Get Code',
    codeSentSimple: 'Code sent',
    codeSent: 'Code sent ({scene})',
    resendAfter: 'Resend in {n}s',
    resendAfterRetry: 'Retry in {n}s',
  },
  // SMS scene names (2026-10-28 M104)
  //
  // The toast used to interpolate the internal identifier
  // (bizType="login-sms"), so users literally saw
  // "Code sent (login-sms)" — internal identifiers belong in logs, not UI.
  smsScene: {
    login: 'SMS login',
    resetPassword: 'Password reset',
    changePhone: 'Change phone number',
    viewFullPhone: 'View full phone number',
    mfa: 'Login two-factor',
  },
  smsGuard: {
    defaultTitle: 'Security Verification',
    // Modal title (explicitly set by the identity-switch / password-change flows)
    verifyTitle: 'SMS verification',
    phoneMissing: 'Please provide a phone number first',
    codeInvalid: 'Please enter the 6-digit code',
    codeWrong: 'Incorrect code',
    sentTo: 'Code will be sent to {phone} ({scene})',
  },
  upload: {
    defaultHint: 'Upload',
    fileTypeNotSupported: 'Unsupported file type: {type}',
    fileTooLarge: 'File exceeds limit ({size}MB)',
  },
  searchForm: {
    expand: 'Expand',
    collapse: 'Collapse',
  },
  login: {
    // ⚠️ Fallback — the server's `branding.app_name` wins when set
    title: 'Admin Console',
    subtitle: 'Admin Console',
    tabPassword: 'Account',
    tabSms: 'SMS',
    username: 'Username',
    usernamePlaceholder: 'Username / Job Number',
    password: 'Password',
    passwordPlaceholder: 'Password',
    smsCode: 'SMS Code',
    remember: 'Remember me',
    forgot: 'Forgot password?',
    submit: 'Login',
    captcha: 'Captcha',
    // M9 polish
    attemptsLeft: 'Login failed, {n} attempts remaining',
    locked: 'Account locked, please retry in {n}s',
    lockedButton: 'Locked ({n}s)',
    lockedWarning: 'Account locked, please retry in {n}s',
    lockedByServer: 'Account locked by server, please retry later or contact admin',
    // ===== Login two-factor (M150) =====
    mfaTitle: 'Two-factor verification',
    mfaHint: 'A verification code has been sent to the phone number bound to this account. Enter it to finish signing in.',
    mfaSubmit: 'Finish signing in',
    mfaResend: 'Resend code',
    mfaResendIn: 'Resend in {n}s',
    mfaResent: 'Code resent',
    mfaBack: 'Back to sign in',
    idleTimeout: 'Signed out after 30 minutes of inactivity. Please sign in again.',
    // Shown when a password change bounces the user back to the login page
    passwordChanged: 'Your password has been changed. Please sign in again with the new password',
  },
  forgotPassword: {
    title: 'Reset Password',
    subtitle: 'Enter your registered phone number to set a new password',
    successTitle: 'Password Reset Successful',
    successSubtitle: 'Please log in with your new password. For security, the SMS code is no longer valid.',
    goLogin: 'Go to Login',
    rememberBack: 'Remember your password?',
    loginNow: 'Log in now',
    submit: 'Submit',
    phonePlaceholder: 'Phone number used at registration',
  },
  layout: {
    // ⚠️ Fallbacks — the server's `branding.app_name` wins when set.
    //   brandShort / copyright currently have **no render site** (2026-11-01:
    //   no callers repo-wide); kept so the brand set stays complete.
    brandShort: 'Admin',
    brandLong: 'Admin Console',
    copyright: 'Admin Console ©{year}',
  },
  menu: {
    dashboard: 'Dashboard',
    system: 'System',
    audit: 'Audit',
  },
  header: {
    fullscreen: 'Fullscreen',
    exitFullscreen: 'Exit Fullscreen',
    help: 'Help',
    notifications: 'Notifications',
    profile: 'Profile',
    logout: 'Logout',
    // Logout confirmation dialog (2026-10-25 Module B)
    logoutTitle: 'Log out?',
    logoutDesc: 'You will need to enter your account and password again',
    logoutOk: 'Log out',
    // Help dropdown (2026-11-03)
    apiDocs: 'API Docs',
    userGuide: 'User Guide',
  },
  // Inbox (M74)
  //
  // ⚠️ noPermission is not "load failed": inbox:list is bound to super_admin
  // and readonly roles only, so most users legitimately get 403 here.
  inbox: {
    title: 'Messages',
    empty: 'No messages',
    noPermission: 'You do not have permission to view messages',
    loadFailed: 'Failed to load',
    markAllRead: 'Mark all read',
    markAllReadConfirm: 'Mark all unread messages as read?',
  },
  // Theme switch (§2.8 #19)
  theme: {
    // 2026-11-05 skins flattened: no standalone light/dark axis any more,
    // ThemeSwitcher retired; skin names come from the registry, not i18n
    skin: 'Switch skin',
  },
  // Route tabs (§2.8)
  tab: {
    // Page names — for pages that have **no menu node** (used by the breadcrumb;
    // see SEGMENT_LABEL_KEYS in components/layout/pageName.ts).
    // Since the multi-tab bar was removed (2026-10-25), only the breadcrumb uses these.
    page: {
      configAudit: 'Audit Log',
      profile: 'Profile',
    },
  },
  // Language switch (§2.6 #8)
  lang: {
    switch: 'Switch language',
    'zh-CN': '简体中文',
    'en-US': 'English',
  },
  // 工作台(仪表盘) —— 与 zh-CN 同步
  dashboard: {
    description: 'Key metrics overview',
    kpi: {
      fileTotal: 'Total Files',
      fileToday: 'Uploaded Today',
      fileStorageBytes: 'Storage Used',
      folderTotal: 'Total Folders',
      userOnline: 'Online Now',
    },
    // ⚠️ 2026-11-01:服务端按业务域权限过滤看板内容 ——
    //   没权限的域**压根不出现在响应里**(字段缺省)⇒ 前端不画那张卡片。
    //   这个身份一个域都看不到时,给这句空态(区别于 common.loadFailed:
    //   那是"请求挂了",这里是"你没权限看大盘")。
    empty: {
      noVisibleData: 'No dashboard data available for the current role',
    },
    // ⚠️ month1..month12 / daySun..daySat must stay **literal keys** —
    //   the component passes the 12 month / 7 weekday names as arrays to echarts;
    //   dynamically built keys (`heatmap.month${i}`) are invisible to the i18n guard.
    heatmap: {
      title: 'Daily Logins (Last 6 Months)',
      empty: 'No login data',
      less: 'Less',
      more: 'More',
      tooltip: '{n} logins on {date}',
      month1: 'Jan',
      month2: 'Feb',
      month3: 'Mar',
      month4: 'Apr',
      month5: 'May',
      month6: 'Jun',
      month7: 'Jul',
      month8: 'Aug',
      month9: 'Sep',
      month10: 'Oct',
      month11: 'Nov',
      month12: 'Dec',
      daySun: 'Sun',
      dayMon: 'Mon',
      dayTue: 'Tue',
      dayWed: 'Wed',
      dayThu: 'Thu',
      dayFri: 'Fri',
      daySat: 'Sat',
    },
    // Server status (2026-11-03) — visible with dashboard:server only (ops line)
    // Layout follows the user's reference: hertz-antd-admin-saas four-quadrant grid
    server: {
      title: 'Server Status',
      refresh: 'Refresh',
      cpu: 'CPU',
      cores: 'Cores',
      coreN: 'core {n}',
      memory: 'Memory',
      disk: 'Disk',
      env: 'Runtime',
      os: 'OS',
      cpuProcessors: 'CPU processors',
      compiler: 'Compiler',
      goVersion: 'Go Version',
      goroutines: 'Goroutines',
      total: 'Total',
      used: 'Used',
      free: 'Free',
      dataDisk: 'Data disk',
      systemDisks: 'System Disks',
      noDisks: 'No usable partitions found',
      redis: 'Redis',
      redisStatus: 'Status',
      available: 'Available',
      unavailable: 'Unavailable',
      redisKeys: 'Keys',
      redisMemory: 'Memory Used',
    },
  },
  resource: {
    // user
    user_list: 'User List',
    user_read: 'User Detail',
    user_create: 'New User',
    user_update: 'Edit User',
    user_delete: 'Delete User',
    user_unlock: 'Unlock User',
    user_toggle: 'Enable/Disable User',
    // role
    role_list: 'Role List',
    role_create: 'New Role',
    role_update: 'Edit Role',
    role_delete: 'Delete Role',
    role_assign: 'Assign Permissions',
    // resource
    resource_list: 'Resource List',
    resource_tree: 'Resource Tree',
    resource_create: 'New Resource',
    resource_update: 'Edit Resource',
    resource_delete: 'Delete Resource',
    // dept
    dept_list: 'Department List',
    dept_tree: 'Department Tree',
    dept_read: 'Department Details',
    dept_create: 'New Department',
    dept_update: 'Edit Department',
    dept_delete: 'Delete Department',
    // dict
    dict_list: 'Dictionary List',
    dict_create: 'New Dictionary',
    dict_update: 'Edit Dictionary',
    dict_delete: 'Delete Dictionary',
    // upload
    upload_list: 'Upload List',
    upload_read: 'Upload Detail',
    upload_create: 'Upload File',
    upload_delete: 'Delete File',
    // family
    family_list: 'Family Group List',
    family_read: 'Family Group Detail',
    family_create: 'New Family Group',
    family_update: 'Edit Family Group',
    family_delete: 'Delete Family Group',
    // supplier
    supplier_list: 'Supplier List',
    supplier_read: 'Supplier Detail',
    supplier_create: 'New Supplier',
    supplier_update: 'Edit Supplier',
    supplier_delete: 'Delete Supplier',
    supplier_audit: 'Supplier Audit',
    // config
    config_list: 'Config List',
    config_read: 'Config Detail',
    config_create: 'New Config',
    config_update: 'Edit Config',
    config_delete: 'Delete Config',
    config_approve: 'Approve Config',
    config_cancel: 'Cancel Config',
    config_activate: 'Activate Config',
    config_audit: 'Config Audit',
    config_health: 'Config Health',
    config_stats: 'Config Stats',
    config_watch: 'Config Watch',
    // audit
    audit_operation_list: 'Operation Log',
    audit_operation_read: 'Operation Log Detail',
    audit_data_list: 'Data Change Log',
    audit_data_read: 'Data Change Log Detail',
    audit_login_list: 'Login Log',
    audit_login_read: 'Login Log Detail',
    audit_system_list: 'System Log',
    audit_system_read: 'System Log Detail',
    audit_full_info_list: 'Sensitive View Log',
    audit_full_info_read: 'Sensitive View Log Detail',
    audit_export_list: 'Export Log',
    audit_export_read: 'Export Log Detail',
    audit_export_create: 'Create Export Task',
    // contract
    contract_list: 'Contract List',
    contract_read: 'Contract Detail',
    contract_create: 'New Contract',
    contract_update: 'Edit Contract',
    contract_delete: 'Delete Contract',
    // device
    device_list: 'Device List',
    device_read: 'Device Detail',
    device_create: 'New Device',
    device_update: 'Edit Device',
    device_delete: 'Delete Device',
    // feedback
    feedback_list: 'Feedback List',
    feedback_read: 'Feedback Detail',
    feedback_create: 'Submit Feedback',
    feedback_reply: 'Reply',
    // guide
    guide_list: 'Guide List',
    guide_read: 'Guide Detail',
    guide_create: 'New Guide',
    guide_update: 'Edit Guide',
    guide_delete: 'Delete Guide',
    // activity
    activity_list: 'Activity List',
    activity_read: 'Activity Detail',
    activity_create: 'New Activity',
    activity_update: 'Edit Activity',
    activity_delete: 'Delete Activity',
    // dashboard
    dashboard_read: 'Dashboard',
    // wxtemplate
    wxtemplate_list: 'WeChat Template List',
    wxtemplate_read: 'WeChat Template Detail',
    wxtemplate_create: 'New WeChat Template',
    wxtemplate_update: 'Edit WeChat Template',
    wxtemplate_delete: 'Delete WeChat Template',
    wxtemplate_send: 'Send WeChat Template',
    // auth
    auth_switch_identity: 'Switch Identity',
  },
  // Sidebar menu (backend menu.* code → English, Phase 5 M2)
  resourceMenu: {
    menu_dashboard: 'Dashboard',
    menu_system: 'System',
    menu_system_user: 'Users',
    menu_system_role: 'Roles',
    menu_system_permission: 'Permissions',
    menu_system_department: 'Departments',
    menu_system_dict: 'Dictionaries',
    menu_system_config: 'System Config',
    menu_system_job: 'Scheduled Jobs',
    // SMS template (M115) -- key = backend resource code with '.' replaced by '_'
    menu_system_smsTemplate: 'SMS Templates',
    // Business form designer (M152) -- backend menu.system.dyn
    menu_system_dyn: 'Form Designer',
    // Custom business (M151) -- parent of the runtime-generated collection menus
    //   (the parent has no path of its own, it is only a group)
    menu_dyn: 'Custom Business',
    menu_audit: 'Audit Center',
    menu_audit_operation: 'Operation Log',
    menu_audit_dataChange: 'Data Change',
    menu_audit_login: 'Login Log',
    menu_audit_system: 'System Log',
    // Approval (menus seeded in M83-d, routes in M87, i18n in M88)
    // 2026-11-01: "My Approvals" and "Approval Flow" moved under "Approval Management"
    menu_approvalMgmt: 'Approval Management',
    menu_approval: 'My Approvals',
    menu_flow: 'Approval Flow',
  },
  // API docs entry page (2026-11-01, open-source generalisation G5)
  apiDocs: {
    description: 'API reference and live debugging (Swagger UI)',
    open: 'Open Swagger UI',
    uiPath: 'UI URL',
    specPath: 'OpenAPI spec',
    authTitle: 'This endpoint has its own credential',
    authDesc:
      'Swagger UI is served by the backend under /swagger and is protected by BasicAuth by default: the username and password come from the server config (swagger.auth.*) and are unrelated to the admin login. The browser will prompt for them.',
    disabledHint: 'A 404 means the endpoint is disabled on the server (swagger.enabled=false).',
  },
  // Approval pending badge (M88)
  // Login devices / sessions (the "Login Devices" tab on the profile page)
  session: {
    revoked: 'Revoked {n} device(s)',
    expired: 'Expired',
    // 2026-10-25 Module B-2: copy of the login-devices panel (SessionPanel).
    // The whole panel used to be hard-coded Chinese, so it never switched
    // with the UI language.
    activeDevices: 'Active devices',
    reload: 'Reload',
    noActive: 'No active devices',
    currentDevice: 'This device',
    history: 'Login history',
    revoke: 'Log out',
    revokeOk: 'Log out',
    revokeAll: 'Log out all other devices',
    revokeAllTitle: 'Log out all other devices?',
    revokeAllDesc: '{n} device(s) will be forced to log out and will need to sign in again.',
    revokeOneTitle: 'Force this device to log out?',
    revokeOneDesc: 'The session on this device will be invalidated immediately and will need to sign in again.',
    revokeCurrentHint: 'Use "Log out" in the top right to log out this device',
    loggedInAt: 'Signed in at',
  },
  //
  // ⚠️ The badge counts items **waiting for me to approve**, not the ones
  //   I initiated — a pending count is a "someone is waiting on you" signal.
  approval: {
    pendingBadge: 'To Approve',
    box: {
      todo: 'To Approve',
      mine: 'Requested by Me',
      done: 'Handled by Me',
    },
    status: {
      draft: 'Draft (not submitted)',
      running: 'In Review',
      returned: 'Rolled Back',
      approved: 'Approved',
      rejected: 'Rejected',
      cancelled: 'Cancelled',
    },
    round: {
      running: 'In Progress',
      approved: 'Approved',
      rejected: 'Rejected',
      rolledBack: 'Rolled Back',
    },
    logAction: {
      submit: 'Submitted',
      approve: 'Approved',
      reject: 'Rejected',
      rollback: 'Rolled Back',
      cancel: 'Cancelled',
    },
    roundTitle: 'Round {n}',
    actionReasonRequired: '{action} requires a reason (at least {n} characters)',
    submittedBy: 'Submitted by {name}',
    assigneeStatus: {
      pending: 'Pending',
      approved: 'Approved',
      rejected: 'Rejected',
      stale: 'Stale (previous round)',
      skipped: 'Skipped',
    },
    // Pass method (2026-10-25 Module B-3)
    // all = every approver must approve, any = one approver is enough.
    // Both the approval detail page and the flow pages use these two words,
    // so they live here instead of being written out twice.
    reviewType: {
      all: 'All approvers',
      any: 'Any one approver',
    },
  },
  logout: 'Logout',
  profile: 'Profile',
  help: 'Help',
  // PagePlaceholder internal copy
  placeholder: {
    defaultDescription: '{title} module, for ...',
    skeletonTitle: 'Page skeleton stage — business logic to be implemented',
    skeletonDesc: 'This page has been configured with routing, permissions, and breadcrumbs. Lists/forms/approval flows will be implemented per business.',
    modulePath: 'Module',
    pageId: 'Page ID',
    dependsOnPermissions: 'Required Permissions',
    plannedFeatures: 'Planned Features',
    placeholderDesc: 'Page skeleton placeholder, pending business implementation',
  },
  // Business page copy (key format: module_page in camelCase)
  page: {
    // module=audit
    auditDataChange: {
      title: 'Data Change Log',
      description: 'Critical business table data change audit',
      f1: 'Data change list (paginated + table/record ID/op type filter)',
      f2: 'Detail (old vs new value diff)',
      f3: 'Export',
    },
    auditLogin: {
      title: 'Login Log',
      description: 'User login behavior audit',
      f1: 'Login log list (paginated + user/IP/status/time filter)',
      f2: 'Detail (IP/UA/time/device)',
      f3: 'Anomalous login highlight',
    },
    // 系统日志(2026-11-03)—— slow SQL / slow HTTP / 5xx / panic
    auditSystem: {
      title: 'System Log',
      description: 'Runtime logs (slow SQL / slow HTTP / 5xx / panic)',
      sourceSlowSql: 'Slow SQL',
      sourceSlowHttp: 'Slow HTTP',
      sourceHttpError: 'HTTP 5xx',
      sourcePanic: 'Panic',
      sourceAsrError: 'ASR failure',
      sourceTtsError: 'TTS failure',
    },
    auditOperation: {
      description: 'User operation audit (read-only) - for after-the-fact tracing and compliance checks',
      f1: 'Operation log list (paginated + module/user/status filter)',
      f2: 'Detail (request params/response/IP/UA/duration)',
      f3: 'Export',
      f4: 'Abnormal log highlight',
      errorMsg: 'Error Message',
      location: 'Location',
      moduleConfig: 'Config',
      requestParams: 'Request Params',
      // ⚠️ 2026-10-30 M126 added: redacted request body (the backend now writes this column)
      requestBody: 'Request Body',
      // ⚠️ 2026-10-30 added: full request URI (path + query), differs from module (path only)
      requestUrl: 'Request URL',
      // ⚠️ 2026-10-30 renamed: the wire field is `response_summary`
      responseSummary: 'Response Summary',
    },
    // module=system
    configChange: {
      description: 'Platform config change approval flow',
      f1: 'Change list (pending / approved / rejected)',
      f2: 'Change detail (old vs new diff)',
      f3: 'Approve (immediate or scheduled)',
      f4: 'Reject (with reason)',
      f5: 'Change audit',
    },
    config: {
      description: 'Managed in collapsed groups - save a whole group at a time; sensitive values are masked by default (click the eye to reveal), and every save is written to sys_config_audit',
      f1: 'Config grouping (tree)',
      f2: 'Config list (search/filter/tag)',
      f3: 'View config detail (value/type/description/env)',
      f4: 'Edit config (trigger approval or immediate)',
      f5: 'Create config',
      f6: 'Version history / change audit',
      f7: 'Rollback to specific version',
      f8: 'Sensitive field masking + plain view permission',
      auditTooltip: 'All audit logs for this group',
      emptyNoMatch: 'No matching config',
      emptyNone: 'No config yet',
      itemCount: '{n} items',
      lastChanged: ' · Last changed:',
      noChanges: 'No changes',
      resetDone: 'Reset to initial values',
      saveGroup: 'Save this group ({n} items)',
      savedCount: '{n} saved',
      savedPartial: '{n} saved, {m} failed',
      sensitive: 'Sensitive',
      sensitiveHint: 'Sensitive value — leave blank to keep the current value, click the eye to reveal',
      /**
       * Config item names — key = config_key with '.' / '-' replaced by '_'
       *
       * ⚠️ Must match `mustSysConfigs` in `server/internal/seed/seed.go` one by one
       *   (guarded by `locales/__tests__/config-i18n.test.ts`, which reads the seed).
       *   A missing entry silently falls back to the Chinese `config_name` in the DB.
       */
      item: {
        aliyun_sms_provider: 'SMS Provider',
        aliyun_sms_access_key: 'SMS AccessKey ID',
        aliyun_sms_access_secret: 'SMS AccessKey Secret',
        aliyun_sms_sign_name: 'SMS Signature',
        aliyun_sms_region: 'SMS API Region',
        aliyun_sms_template_login: 'Login Verification Template',
        aliyun_sms_template_view_full_phone: 'Full Phone Number Template',
        aliyun_sms_template_reset_password: 'Password Reset Template',
        aliyun_sms_template_change_phone: 'Phone Change Template',
        aliyun_oss_provider: 'OSS Provider',
        aliyun_oss_endpoint: 'OSS Endpoint',
        aliyun_oss_access_key: 'OSS AccessKey ID',
        aliyun_oss_access_secret: 'OSS AccessKey Secret',
        aliyun_oss_bucket: 'File Bucket',
        aliyun_oss_cdn_domain: 'CDN Domain',
        aliyun_oss_upload_token_ttl: 'STS Token TTL (seconds)',
        // voice (Bailian speech, 2026-11-06) — zh-CN names must mirror seed.go verbatim
        voice_api_key: 'Bailian API Key',
        voice_workspace_id: 'Bailian Workspace ID',
        voice_region: 'Bailian Region',
        voice_default_target_model: 'Voice Clone Target Model',
        voice_synth_url: 'Bailian Synthesis Endpoint',
      },
      /**
       * Dict-driven form labels — key = dict item_key with '.' replaced by '_'
       *
       * ⚠️ This group is the **fallback**: the dictionary (`config_ui_label`) stores
       *   operator-editable Chinese, which stays Chinese in an English UI.
       *   Since 2026-10-29 the lookup order is i18n first, dictionary second.
       */
      dict: {
        config_value: 'Current Value',
      },
    },
    department: {
      description: 'Organization structure maintenance',
      f1: 'Department tree',
      f2: 'Create/edit department (parent/sort)',
      f3: 'Enable/disable',
      f4: 'Department head settings',
      f5: 'Delete (forbidden when users exist)',
    },
    permission: {
      description: 'All accessible resources (menu/button/API)',
      // 2026-10-25 Module B-5b: copy of the resource (permission point)
      // management page (the whole page used to be hard-coded Chinese).
      addChild: 'Add child',
      codePattern: 'Lowercase letters + digits + _ . + *, starting with a letter',
      parentResource: 'Parent resource',
      topLevel: '— Top level —',
      pathLabel: 'Frontend path / API path',
      pathPlaceholder: 'Menu: /system/xxx  /  API: /rapid-admin/v1/xxx',
      httpMethod: 'HTTP method',
      iconLabel: 'Icon',
      iconPlaceholder: 'Pick a sidebar icon',
      // 2026-10-30: the icon field was a free-text input; it is a dropdown now
      iconNone: 'No icon',
      iconHelp: 'Menus only — the icon shows up in the left navigation bar.',
      // HTTP method "All"
      methodHelp:
        'Documentation only: it marks which endpoint the permission point maps to and does not take part in authorization. "All" means no restriction.',
      // Parent resource type rules
      parentHelp:
        'A menu can only sit under a menu; a button under a menu or a button; an API under a menu, a button or an API.',
      deleteTip1: 'After the deletion this resource point no longer shows up in the menu/button/API lists.',
      deleteTip2:
        'If the backend reports "child resources exist" or "the resource is bound to a role", clear the dependencies first.',
      f1: 'Resource (permission point) tree',
      f2: 'Create/edit resource (menu/button/API)',
      f3: 'Resource type: menu / button / API',
      f4: 'Resource sort and display control',
      f5: 'Delete (forbidden when referenced)',
    },
    role: {
      description: 'Role definition + resource authorization',
      // 2026-10-25 Module B-5b: copy of the role management page and of the
      // resource-assignment drawer.
      viewPermissions: 'View permissions',
      builtinNotEditable: 'Built-in roles cannot be edited',
      builtinNotDeletable: 'Built-in roles cannot be deleted',
      superAdminNotDeletable: 'The super admin role cannot be deleted',
      codePattern: 'Lowercase letters + digits + underscore, starting with a letter',
      typeBusiness: 'Business role',
      typePlatform: 'Platform role',
      deleteTip:
        'After the deletion the users of this role lose the corresponding permissions; the role-resource bindings are cleaned up as well',
      // ---- resource assignment drawer ----
      assign: {
        loadFailed: 'Failed to load: {msg}',
        success: 'Assigned {n} permission point(s) to "{name}"',
        viewTitle: 'View permissions — {name}',
        title: 'Assign permissions — {name}',
        readOnlyTag: 'Built-in / super admin, read-only',
        selected: '{n} selected / {total}',
        selectAllInView: 'Select all in this view',
        clearInView: 'Clear this view',
        empty: 'No resources',
      },
      // ---- data scope (row-level permission) drawer — M157 (2026-11-02) ----
      scope: {
        button: 'Data scope',
        title: 'Data scope — {name}',
        viewTitle: 'View data scope — {name}',
        menuTitle: 'Domain',
        defaultModuleLabel: 'Default (all domains)',
        defaultModuleTip: 'This is the "default rule group"',
        defaultModuleDesc:
          'The default group only applies to domains that have **no rules of their own**. Once a domain has its own rules, the default group no longer participates for that domain (nearest-wins; the two groups are not merged).',
        moduleTip: 'This is a single domain',
        moduleDesc:
          'Once this domain has its own rules, the default group no longer participates **for this domain** (nearest-wins). Other domains without their own rules still fall back to the default group.',
        ruleHint: 'Multiple rules are combined with OR',
        ruleHintDesc:
          'A row is visible when it matches any one of the rules. "All data" wins outright — if it is present, the other rules are never evaluated. When a restriction matches nothing for the caller, they see no rows at all (not every row).',
        allWarning: 'This group contains "All data"',
        allWarningDesc:
          'As long as an "All data" rule exists, the other rules are never parsed — they have no effect.',
        addRule: 'Add rule',
        removeRule: 'Remove',
        customColumnPlaceholder: 'Column (e.g. project_id)',
        customValuesPlaceholder: 'Allowed values, press Enter to add',
        remarkPlaceholder: 'Remark (optional)',
        emptyRules: 'No rules configured for this domain',
        emptyModules:
          'The backend has not registered any domain yet — only the default group can be configured',
        noChange: 'Nothing changed',
        saveSuccess: 'Saved the data scope of {n} domain(s)',
        loadFailed: 'Failed to load: {msg}',
        type: {
          all: 'All data',
          project: 'Own project',
          dept: 'Own department',
          dept_and_child: 'Own department and sub-departments',
          self: 'Own records only',
          custom: 'Custom',
        },
        errors: {
          scopeTypeRequired: 'Please pick a scope type',
          scopeTypeInvalid: 'Invalid scope type',
          customColumnRequired: 'A custom scope requires a column name',
          customColumnInvalid:
            'The column must be a bare identifier (letters/digits/underscore, not starting with a digit)',
          customValuesRequired: 'A custom scope requires at least one allowed value',
        },
      },
      // ---- field visibility (column-level permission) drawer — M164 (2026-11-02) ----
      //
      // ⚠️ This is a **different axis** from `scope` (data scope) above:
      //   scope = "which rows", fieldPolicy = "which columns of that row".
      fieldPolicy: {
        button: 'Field visibility',
        title: 'Field visibility — {name}',
        viewTitle: 'View field visibility — {name}',
        menuTitle: 'Domain',
        defaultModuleLabel: 'Default (all domains)',
        defaultModuleTip: 'This is the "default rule group"',
        defaultModuleDesc:
          'The default group applies to **every** domain. When a domain has its own rule for a field, it overrides **that field only** (per-field overlay) — the default group keeps restricting the other fields in that domain.',
        moduleTip: 'This is a single domain',
        moduleDesc:
          'The fields you configure here override the default group for **the same field**. Fields you do not configure still fall back to the default group.',
        overlayHint: 'The default group and a domain are combined per field',
        overlayHintDesc:
          'Setting a field to "Plain visible" makes it visible again **in this domain** — the default group rule for that field no longer applies here. Field visibility can only narrow fields that are **already in the response**; to let a role see a field, the business side has to put the value into the response first.',
        hiddenHint: '"Hidden" removes the key from the response',
        hiddenHintDesc:
          'It does not null the value — the field name disappears from the response too (the key itself may be information, e.g. emergency_contact_phone).',
        maskHint: '"Masked" needs a mask type',
        maskHintDesc:
          'When the field name gives it away, "Auto" infers the mask type. When it cannot be inferred you must pick one explicitly, otherwise the save is rejected — the runtime fallback is "Full mask" (equal-length asterisks), which would surprise the administrator.',
        remarkPlaceholder: 'Remark (optional)',
        sensitive: 'Sensitive',
        notCandidate: 'No longer a candidate',
        ruleCount: '{n} field rule(s) configured for this domain',
        emptyFields: 'This domain has no configurable field',
        emptyModules:
          'The backend has not registered any domain yet — only the default group can be configured',
        noChange: 'Nothing changed',
        saveSuccess: 'Saved the field visibility of {n} domain(s)',
        saveRejected: '{n} invalid configuration(s) — the save was blocked (see the highlighted rows)',
        loadFailed: 'Failed to load: {msg}',
        mode: {
          inherit: 'Follow the default group',
          defaultInherit: 'Not configured (plain visible)',
          visible: 'Plain visible',
          masked: 'Masked',
          hidden: 'Hidden',
        },
        mask: {
          auto: 'Auto (infer from the field name)',
          phone: 'Phone',
          id_card: 'ID card',
          name: 'Name',
          email: 'Email',
          bank_card: 'Bank card',
          contract_no: 'Contract no.',
          full: 'Full mask',
        },
        errors: {
          modeInvalid: 'Invalid visibility mode',
          maskTypeRequired: 'The mask type must be picked explicitly (it cannot be inferred)',
          fieldNotCandidate:
            'This field is no longer a candidate of this domain — please delete the rule',
        },
      },
      f1: 'Role list',
      f2: 'Create/edit role (type / description)',
      f3: 'Assign resources (permission tree)',
      f4: 'View role-assigned users',
      f5: 'Delete (forbidden when users exist)',
    },
    user: {
      description: 'Backend system user management, covering account/identity/department',
      resetPasswordTitle: 'Reset the password of {name}?',
      // 2026-10-25 Module B-5a: copy of the user-management page
      // (the whole page used to be hard-coded Chinese).
      detailLoadFailed: 'Failed to load the user details: {msg}',
      identityLoadFailed: 'Failed to load the user identities: {msg}',
      resetPassword: 'Reset password',
      cannotDeleteSelf: 'You cannot delete yourself (super admin)',
      accountRequired: 'Please enter the account',
      phoneUnchangedTip: 'Leave empty = the phone number is not changed',
      roleRequired: 'Please select at least one role',
      roleMultiTip: 'Multiple selection allowed, the first one is the primary identity',
      identityOverwriteTip:
        'This overwrites the identity list of the user (bound projects / departments / validity periods are kept per role)',
      deptOverwriteTip: 'This overwrites the department list of the user (unchecked departments are removed)',
      lockedStatusTip:
        '"Locked" is a state derived by the system (locked after failed logins); it cannot be set here',
      initialPassword: 'Initial password',
      initialPasswordRequired: 'Please enter the initial password',
      firstLoginForceChangeTip: 'The user is forced to change the password after the first login',
      departmentsLabel: 'Departments',
      notSet: 'Not set',
      identitiesLabel: 'Identities / roles',
      // NOTE: the leading space is part of the copy — the original literal
      // was ' (停用)', with the space between the name and the bracket.
      inactiveSuffix: ' (disabled)',
      batchDelete: 'Batch delete ({n})',
      deptRequired: 'Please select at least one department',
      preHashUnsupported:
        'This environment does not support password pre-hashing, please use a modern browser',
      identityUpdated: 'Identities updated',
      manageIdentity: 'Manage identities',
      deleteTip: 'After the deletion the user can no longer log in; related identity/audit records are kept (read-only)',
      batchDeleteTitle: 'Batch delete {n} user(s)?',
      batchDeleteTip:
        'This cannot be undone, please be careful. The selection containing yourself is excluded automatically.',
      batchDeleted: '{n} user(s) deleted',
      batchDeleteFailed: 'Batch delete failed',
      resetPasswordTip:
        'The system generates an initial password after the reset; the target user is forced to change it on the next login',
      resetDone: 'Reset, new password: {pwd}',
      resetFailed: 'Reset failed',
      f1: 'User list (paginated + multi-condition filter)',
      f2: 'Create user (department / multi-identity / initial password)',
      f3: 'Edit user info',
      f4: 'Enable/disable / unlock',
      f5: 'Batch import / export Excel',
      f6: 'Reset password (admin)',
      f7: 'Operation log association',
    },
    // 字典页 2026-10-17 实装(ManagementTable + inlineToolBar),顶掉了 placeholder 版 ——
    // 所以这一组只有 description,没有 f1..f5 功能清单(别再补回来,那是死键)
    skin: {
      description: 'Visual color editing with live preview; the exported TS snippet drops into web/src/theme/skins/ to become a built-in skin (docs/theming.md §5)',
      listTitle: 'Built-in skins',
      clone: 'Copy',
      cloneSuffix: 'copy',
      newSkin: 'New from primary',
      pickFirst: 'Start from "Copy" or "New from primary" on the left',
      editTitle: 'Edit',
      defaultName: 'Custom skin',
      skinName: 'Skin name',
      appearanceLight: 'Light',
      appearanceDark: 'Dark',
      primary: 'Primary',
      hover: 'Hover',
      active: 'Active',
      baseMode: 'Basic mode (surface tone + brand trio)',
      baseModeTip: 'Primary = the brand accent (buttons / links / selected states), not the interface background — surfaces come from the surface tone + light/dark reference frame, adjustable per-key in Advanced → Canvas.',
      surface: 'Surface tone',
      surfaceTip: 'Drives the hue and intensity of neutral surfaces (canvas / sider / menu). Pick a pure gray for a colorless base like the White / Black skins; the brand primary only controls buttons / links / selected states — the two are independent.',
      rederive: 'Rederive all values from primary',
      rederiveTip: 'Recalculates every suggested value: neutral surfaces (canvas/text/borders/header/sider) take hue and intensity from the surface tone, accent keys (buttons/links/selected states) from the primary; both keep the reference lightness ladder (light = museum, dark = black). Manual edits in advanced mode are overwritten.',
      rederiveConfirm: 'This regenerates all suggestion values from the current primary and overwrites manual edits made in advanced mode. Continue?',
      rederived: 'Rederived from primary',
      advancedPalette: 'Advanced · palette (25 keys)',
      advancedComp: 'Advanced · comp (33 keys)',
      previewTitle: 'Live preview',
      tryOn: 'Try on full page',
      exitTryOn: 'Exit try-on',
      tryOnHint: 'Try-on registers the draft skin (custom-draft); the previous skin is restored when you leave this page',
      exportTsTitle: 'Export TS snippet',
      exportTsHint: 'Save as web/src/theme/skins/my-skin.ts, then add one line to the SKINS table in theme/registry.ts (see docs/theming.md §5 step 3)',
      exportJson: 'Export JSON',
      copied: 'Copied to clipboard',
      copy: 'Copy to clipboard',
      previewInputPlaceholder: 'Input (focus to see the primary glow)',
      tagDefault: 'Default',
      tagPrimary: 'Primary',
      tagDashed: 'Dashed',
      tagDanger: 'Danger',
      tagInfo: 'Info',
      tagSuccess: 'Success',
      tagWarning: 'Warning',
      tagError: 'Error',
      groupCanvas: 'Canvas',
      groupText: 'Text ramp',
      groupBorder: 'Borders',
      groupBrand: 'Brand accents',
      groupLayout: 'Layout (header / sider / canvas)',
      groupMenu: 'Menu',
      groupTable: 'Table',
      groupMisc: 'Button shadows / pagination / breadcrumb / Tabs',
    },
    dict: {
      description: 'Dictionary maintenance (free-form business enum scenarios)',
      // 2026-10-25 Module B-5c: copy of the dictionary management page.
      // ---- edit modal ----
      editTitle: 'Edit dictionary item',
      detailTitle: 'Dictionary item details',
      displayNameRequired: 'Enter the display name',
      descriptionPlaceholder: 'Plain text only (without the color: prefix)',
      // ---- top alert ----
      repoNotice:
        'Dictionaries are the central registry of code enums — you can only edit display attributes, not add or remove items',
      // ⚠️ These 5 fragments are **one rich-text sentence broken up by <code>** —
      //   this i18n layer only does plain-string placeholder substitution and
      //   cannot inject tags, so a sentence containing <code> has to be split
      //   and stitched back together in order at render time.
      //   The English side carries **leading and trailing spaces** so the
      //   fragments don't run into each other.
      repoDesc1: 'To add a dictionary item, edit ',
      repoDesc2: ' in ',
      repoDesc3: ', then run ',
      repoDesc4: '; to retire a dictionary item, set ',
      repoDesc5: '. See ',
    },
    dept: {
      description: 'Organization structure maintenance - tree view; a parent department must have its child departments handled before it can be disabled or deleted',
      // 2026-10-25 Module B-5c: copy of the department management page.
      // ---- left pane: org tree ----
      orgTree: 'Organization',
      addRoot: 'Add root department',
      addChild: 'Add sub-department',
      empty: 'No departments',
      searchHint: 'Select a department on the left',
      // ---- right pane: details ----
      subDepartments: 'Sub-departments',
      noSubDepartments: 'No sub-departments',
      // member-count unit ("3 people")
      memberUnit: '{n} people',
      // ---- edit / delete ----
      parentDept: 'Parent department',
      selectParentFirst: 'Select a parent department first',
      // cannot delete a department that still has members (button title)
      deleteBlocked: 'Departments with members cannot be deleted (transfer the members first)',
      deleteTip:
        'The department and its sub-departments will be removed; members must be transferred to another department first',
    },
    job: {
      description:
        'Enable/disable, reschedule, annotate and manually trigger jobs — tasks are registered in code, this page only overrides runtime state',
      // 2026-10-25 Module B-2: run status in the execution-history drawer.
      // success / failed reuse common.resultSuccess / common.resultFailed
      // (generic result words); only "running" is specific to scheduled jobs.
      status: {
        running: 'Running',
      },
      // 2026-10-25 Module B-4: copy of the scheduled-job page (list + run
      // history drawer). The whole page used to be hard-coded Chinese, so it
      // was unusable in the English UI.
      // ---- cron presets ----
      // NOTE: these live in a module-level constant in the source, which is
      // evaluated at import time — so the source stores the KEYS, not the
      // copy, and looks them up while rendering.
      cronPreset: {
        everyMinute: 'Every minute',
        everyHour: 'Every hour',
        daily3am: 'Every day at 03:00',
        weeklyMonday3am: 'Every Monday at 03:00',
        monthly1st3am: 'On the 1st of every month at 03:00',
        every10Minutes: 'Every 10 minutes',
      },
      cronInvalid: 'Invalid expression',
      defaultExprLabel: 'Default in code:',
      nextRuns: 'Next {n} runs:',
      // ---- cron expression → plain English (M120) ----
      // Describer: web/src/pages/system/job/cronDescribe.ts
      //
      // NOTE: the weekday names are translated BEFORE they are joined, so
      // they have to be separate phrases rather than a {dows} parameter
      // (a parameter is a string — you cannot translate inside a string).
      // {dows} therefore carries the ALREADY-translated weekday names.
      cronDesc: {
        // list separator: Chinese uses the ideographic comma, English a comma
        listSep: ', ',
        daily: 'Every day',
        // {dows} = already-translated weekday names joined by listSep
        weekly: 'Every {dows}',
        monthlyDay: 'Day {d} of every month',
        everyNDays: 'Every {n} days',
        everyMinute: 'Every minute',
        everyMinutes: 'Every {n} minutes',
        everyHours: 'Every {n} hours',
        everyHoursMinutes: 'Every {h} hours {m} minutes',
        everySeconds: 'Every {n} seconds',
        everyHour: 'Every hour',
        // ---- "at minutes N of every hour" long-form sentences ----
        //
        // NOTE: these are four COMPLETE sentences, not a scope + a clause
        // glued together: Chinese must not have a space inside
        // 「每月每日每星期每小时的第 7 分钟各执行一次」 while English must.
        // The joining rule would then differ per language, so the
        // translator gets to see (and reorder) a whole sentence instead.
        //
        // {mins} = the minute list joined by listSep (NOT zero-padded)
        minuteListAll: 'Every hour at minutes {mins} on every day of every week of every month',
        minuteListWeekly: 'Every hour at minutes {mins} on {dows}',
        minuteListMonthlyDay: 'Every hour at minutes {mins} on day {d} of every month',
        minuteListEveryNDays: 'Every hour at minutes {mins} every {n} days',
        // English needs a preposition before a clock time (at 03:00),
        // Chinese does not.
        atTime: 'at {time}',
        yearly: 'On January 1st every year',
        dow: {
          sun: 'Sunday',
          mon: 'Monday',
          tue: 'Tuesday',
          wed: 'Wednesday',
          thu: 'Thursday',
          fri: 'Friday',
          sat: 'Saturday',
        },
      },
      // ---- trigger type (schedule / manual) ----
      triggerSchedule: 'Scheduled',
      triggerManual: 'Manual',
      // ---- run history drawer ----
      runHistory: 'Run history',
      degraded: 'Degraded',
      degradedOnly: 'Degraded only',
      lastFailed: 'Last failure ({time}):',
      noErrorInfo: '(no error message)',
      // ---- job list ----
      notScheduled: 'Not scheduled',
      overridden: 'Overridden',
      residualTip: 'This job was removed from the code; the sys_job row is a leftover nobody claims',
      residual: 'Leftover',
      codeSideOff: 'Disabled in code',
      minutes: '{n} min',
      removedTip: 'This job was removed from the code, its leftover config cannot be changed',
      removedTipShort: 'This job was removed from the code',
      disabledTip: 'This job is disabled; enable it before triggering',
      noOverrideTip: 'There is no backend override, the code default is already in effect',
      manualTrigger: 'Trigger manually',
      // ---- result of a manual trigger ----
      triggerSuccess: 'Ran successfully in {n}ms',
      triggerFailedTitle: 'Trigger failed · {key}',
      nonSuccessStatus: 'The job returned a non-success status: {status}',
      // ---- degradation banners ----
      schedulerDisabled: 'The scheduler is disabled',
      schedulerDisabledDesc: 'job.enabled=false, so no job on this page will run automatically. Manual triggering still works.',
      lockDegraded: 'The distributed lock is unavailable (running degraded)',
      // NOTE: these four keys are ONE sentence cut into four pieces by
      // <code> / <strong>. This i18n only does string placeholder
      // substitution, so rich text cannot live in a single key. The four
      // pieces still render the original sentence, character for character.
      // English needs the spaces around the inline elements, so the
      // leading/trailing spaces are part of the key values here.
      lockDegradedPrefix: 'Lock mode ',
      lockDegradedMiddle: ' is currently unavailable; the same task may ',
      lockDegradedHighlight: 'run on multiple instances at once',
      lockDegradedSuffix: '. Non-idempotent tasks are skipped while degraded (marked "Degraded" in the run history).',
      streamDegraded: 'Event streaming is degraded, job status will not refresh automatically',
      streamDegradedDesc: 'The event channel (Redis) is unavailable, so run history has to be refreshed manually. The jobs themselves are still scheduled normally.',
      onlyEnabled: 'Enabled only',
      runtimeInfo: 'Instance {id} · Time zone {tz} · {n} jobs registered',
      // ---- dialogs ----
      editTitle: 'Edit job',
      manualTriggerTitle: 'Trigger {key} manually?',
      // Manual-trigger confirmation — also split by <b>
      triggerConfirmPrefix: 'The task will run ',
      triggerConfirmHighlight: 'synchronously',
      triggerConfirmSuffix: ' to completion, taking up to {n} minutes. Do not click twice — non-idempotent tasks must not be triggered repeatedly.',
      resetTitle: 'Restore the default configuration?',
      resetConfirmPrefix: 'This will clear the backend override for ',
      resetConfirmMiddle: ': the cron expression, name and remark all go back to the defaults in code. The "Enabled/Disabled" state is ',
      resetConfirmHighlight: 'not affected',
      resetConfirmSuffix: ' (use the "Disable" button to turn it off).',
      restored: 'Restored the defaults from code',
    },
    // module=system (SMS template review status, M115)
    //
    // Two hard rules for this page:
    //   1. Status labels and colors come from the dictionary group
    //      (sms_template_status) -- never hardcoded here, because the
    //      dictionary is operator-editable and this copy would drift.
    //   2. "Restore" is NOT "Approved" -- a human may not decide that a
    //      template passed review; restoring moves it back to "pending" and
    //      the provider's verdict comes back on the next sync.
    smsTemplate: {
      description:
        'Review status of the four SMS scenes -- disabling is a stop-gap; a restored template still waits for the provider to confirm it',
      // ---- Sync now (same entry point as the scheduled job) ----
      syncNow: 'Sync now',
      syncDone: 'Sync done: {updated} updated, {registered} newly registered, {unchanged} unchanged',
      syncPartial: 'Could not determine the status of: {scenes} (status left as is)',
      syncNothing: 'No status changed ({unchanged})',
      // ---- Disable / restore ----
      disable: 'Disable',
      enable: 'Restore',
      disableTitle: 'Disable template',
      enableTitle: 'Restore template',
      reasonLabel: 'Reason',
      reasonPlaceholder: 'Optional, e.g. rejected by the provider / content error',
      reasonHelp: 'Stored in the transition history (up to 500 chars) -- this is what you read when troubleshooting',
      disabled: 'Disabled',
      enabled: 'Restored, waiting for the provider to confirm',
      notConfiguredTip: 'No template code is configured for this scene, there is nothing to disable',
      notDisabledTip: 'This scene is not disabled, there is nothing to restore',
      // ---- Table ----
      codeMismatch: 'Template code mismatch',
      codeMismatchTip:
        'The registered review verdict belongs to a different template code -- the current one has not been synced yet',
      unknown: 'Unknown',
      // ---- Status source ----
      source: {
        provider: 'Provider',
        manual: 'Manual',
      },
      // ---- Guard policy banner ----
      policyLabel: 'Guard policy: {policy}',
      policyDesc:
        'The guard uses the review status to decide whether to let the SMS through, warn, or block. This policy is a platform-wide switch shared by every scene.',
      policyBlock: 'Block',
      policyWarn: 'Warn only',
      policyOff: 'Allow (no check)',
      policyUnknown: 'Unknown policy',
      // ---- Current provider ----
      providerUnsupported: 'The current SMS channel ({provider}) cannot query template review status',
      providerUnsupportedDesc:
        'This channel has no templates to review, so the status never updates on its own and "Sync now" will fail.',
      // ---- Transition history drawer ----
      history: 'History',
      historyTitle: 'Transition History',
      noHistory: 'No transitions recorded yet',
    },
    // module=voice (Voice management 2026-11-06: voice registry + clone ledger + TTS)
    voice: {
      description: 'Voice registry, clone ledger and text-to-speech — built-in voices are seeded by the backend; cloning goes through Bailian CosyVoice, synthesis supports both Bailian and Edge channels',
      tabVoices: 'Voices',
      tabClones: 'Clone records',
      tabSynth: 'Text to speech',
      errorMsg: 'Failure reason',
      // ---- Add / edit voice ----
      addVoice: 'Add voice',
      nameRequired: 'Please enter a name',
      editVoice: 'Edit voice',
      voiceId: 'Voice ID',
      voiceIdPlaceholder: 'e.g. zh-CN-XiaoxiaoNeural or longxiaochun_v3',
      ttsType: 'Synthesis platform',
      gender: 'Gender category',
      genderPh: 'Male / Female / Child / Dialect / Custom',
      sex: 'Gender',
      male: 'Male',
      female: 'Female',
      recommend: 'Recommended',
      voiceIdLocked: 'voice_id and platform cannot be edited — delete and re-create instead (editing in place would leave the demo audio pointing at the wrong voice)',
      created: 'Created. Demo audio is being generated in the background',
      updated: 'Saved. Demo audio is being regenerated in the background',
      // ---- Clone voice ----
      cloneVoice: 'Clone voice',
      audio: 'Reference audio',
      audioRequired: 'Please choose a reference audio file (wav/mp3/m4a/pcm/aac/flac/ogg)',
      audioHint: 'Clear audio longer than 10s clones best; the file is relayed through object storage and fetched by Bailian for registration',
      targetModel: 'Target model',
      targetModelPh: 'Default cosyvoice-v3-plus (backend voice.default_target_model)',
      prefix: 'voice_id prefix',
      prefixPh: 'Default: voice',
      languageHints: 'Language hints',
      languageHintsPh: 'Comma separated, e.g. zh,en',
      cloning: 'Submitting clone (the remote fetches the audio and starts review, please wait)…',
      cloned: 'Clone submitted. Track progress in "Clone records" (DEPLOYING → OK)',
      // ---- Clone ledger ----
      refreshStatus: 'Refresh status',
      refreshed: 'Status synced',
      statusDeploying: 'Reviewing',
      statusOk: 'Published',
      statusUndeployed: 'Rejected',
      statusFailed: 'Failed',
      refreshUnavailable: 'This is a failed record (no voice_id) and cannot be refreshed',
      // ---- Text to speech ----
      selectVoice: 'Voice',
      selectVoiceFirst: 'Please select a voice first',
      textRequired: 'Please enter the text to synthesize',
      textPh: 'Enter the text to read aloud…',
      format: 'Audio format',
      provider: 'Channel',
      providerAuto: 'Follow voice registry',
      providerBailian: 'Bailian',
      providerEdge: 'Edge TTS',
      volume: 'Volume',
      rate: 'Rate',
      play: 'Preview',
      download: 'Download audio',
      streamPlay: 'Streamed preview',
      streaming: 'Receiving… {n} bytes',
      synthDone: 'Synthesis finished',
      edgeWavHint: 'This voice uses the Edge channel which only outputs mp3 — the wav option is disabled',
      bailianHint: 'The Bailian channel requires backend voice.synth_url; without it synthesis fails with an explicit error',
      demoMissing: 'Demo audio not generated yet (async after creation; failures only cost the demo)',
    },
    // module=approval / module=flow (Generic Approval M83/M87)
    approvalList: {
      description: 'To approve / initiated by me / handled by me — three boxes',
      // 2026-10-25 Module B-3: the count suffix on the "rolled back N times" tag
      returnCount: '{n} times',
    },
    approvalDetail: {
      notFound: 'The request does not exist, or you are not allowed to view it',
      title: 'Approval Detail',
      description: 'Round switcher + stage progress + comment history + approval actions',
      // 2026-10-25 Module B-3: the body copy of the detail page.
      // It used to be hard-coded Chinese throughout, so this card showed
      // not a single English word in the English UI.
      // Top
      back: 'Back',
      bizId: 'Business record ID',
      templateName: 'Flow template',
      returnCount: 'Times rolled back',
      finishTime: 'Finished at',
      // Round switcher
      roundSection: 'Round',
      roundCurrent: '(current)',
      roundArchive: 'This is the archive of round {n}; the effective round is {m}',
      // Approval content (editable area)
      contentSection: 'Approval content',
      dirty: 'Unsaved changes',
      sameAsSnapshot: 'Same as the snapshot of this round',
      saveAndResubmit: 'Save and resubmit',
      submit: 'Submit for review',
      noExtra: 'No extra content',
      rawContentKey: 'Content',
      // Confirm dialog before switching rounds (when there are unsaved changes)
      discardAndSwitch: 'Discard and switch',
      continueEditing: 'Keep editing',
      switchConfirm: 'Switching to another round will discard these changes. Are you sure?',
      // Result messages of submit / approval actions
      resubmitMustChange: 'You must change the content before resubmitting: a request identical to the previous round will be rejected',
      submitted: 'Submitted',
      submittedForReview: 'Submitted for review',
      resubmitted: 'Resubmitted',
      // Approval progress
      progressSection: 'Approval progress',
      noStage: 'This request has not entered any stage yet',
      assigneeProgress: '{n} approver(s) in total, {m} approved',
      stageTitle: 'Stage {n} · {name}',
      // Comment history
      logSection: 'Comment history',
      noLog: 'No comment records yet',
      rollbackRequest: 'Roll back for more material',
    },
    flowList: {
      description: 'Approval templates — entry point for stages and approver rules',
      bizTypeRequired: 'Please enter the business type (e.g. digital_material)',
      createTemplate: 'New template',
      defaultStageName: 'Stage 1',
      deleteTemplateDesc: 'After deletion this template can no longer be used for new approvals. Approvals already in progress are not affected (they use the snapshot taken when they were started).',
      deleteTemplateTitle: 'Delete template {name}?',
      stageConfig: 'Stage configuration',
      stages: 'Stages',
      templateCode: 'Template code',
      templateCodePattern: 'Only lowercase letters, digits and underscores are allowed, and it must start with a letter',
      templateCodeRequired: 'Please enter the template code',
      templateDetail: 'Template details',
      templateName: 'Template name',
      templateNameRequired: 'Please enter the template name',
    },
    flowDetail: {
      description: 'Each stage is an approval step; any = one approver, all = every approver',
      addStage: 'Add stage',
      backToTemplateList: 'Back to template list',
      deleteStageDesc: 'The approver rules under this stage will be deleted as well. The whole template is written back, not just this row.',
      deleteStageTitle: 'Delete stage {name}?',
      editStage: 'Edit stage',
      moveDown: 'Move down',
      moveUp: 'Move up',
      orderChanged: 'Order updated',
      reviewTypeAllDesc: 'All approvers (everyone must approve to pass)',
      reviewTypeAllTag: 'All approvers (all must approve)',
      reviewTypeAnyDesc: 'Any one (the first approver decides the result)',
      reviewTypeAnyTag: 'Any one (one is enough)',
      ruleDeptLeaderTag: 'Dept leader +{n}',
      ruleRoleTag: 'Role {name}',
      ruleUserTag: 'User {name}',
      rules: 'Rules',
      stageCountHint: '{n} stage(s) in total (up to 10) · rules are configured on the "Rules" sub-page',
      stageDeleted: 'Stage deleted',
      stageNameRequired: 'Please enter the stage name',
      newStageAutoRuleHint: 'A new stage starts with a default "dept leader +1" approver rule (adjust it on the "Rules" sub-page after creation)',
    },
    // Page-header descriptions (description only, no f1..fn feature list).
    // 2026-10-25: these pages had their header copy hard-coded in Chinese, so it
    //   never switched with the UI language. Moved into the dictionary with the
    //   exact text the page used to show (not one visible character changed).
    profile: {
      description: 'Manage your profile, security settings, identities, and login history',
      // 2026-10-25 Module B-2: the whole profile page copy.
      // ⚠️ Lives under `page.profile` instead of a new top-level `profile` ——
      //   a top-level `profile: 'Profile'` already exists (route title), same name collides.
      // User card on the left
      notLoggedIn: 'Not signed in',
      identityCount: 'Identities',
      identityCountValue: '{n}',
      currentIdentity: 'Current identity',
      // Avatar upload (M124)
      avatarUpload: 'Change avatar',
      avatarSuccess: 'Avatar updated',
      avatarTooLarge: 'The avatar must not exceed 5 MB',
      avatarTypeError: 'Only PNG / JPEG / GIF / WEBP / BMP images are supported',
      // Tab titles
      tabBasic: 'Basic info',
      tabSecurity: 'Security',
      tabIdentities: 'My identities',
      tabSessions: 'Login devices',
      // Basic info form
      max32: 'No more than 32 characters',
      // ⚠️ M113:phoneChangeHint removed — the phone number now lives only in
      //   Security → Phone binding, so this hint had nowhere left to point.
      emailHint: 'Leave empty to keep the current email address',
      // Security settings
      loginPassword: 'Login password',
      pwdTip: 'Changing your password regularly improves account security',
      pwdReloginTip: 'You will need to sign in again after changing it',
      changePassword: 'Change password',
      phoneBinding: 'Phone binding',
      boundPrefix: 'Bound:',
      changePhone: 'Change phone number',
      // Change-phone dialog (C5, 2026-10-29)
      bindPhoneTitle: 'Change phone number',
      bindPhoneNewLabel: 'New phone number',
      bindPhoneNewPlaceholder: 'Enter the 11-digit phone number',
      bindPhoneCodeLabel: 'Verification code',
      bindPhoneCodePlaceholder: 'Enter the 6-digit code',
      bindPhoneCodeRequired: 'Please enter the verification code',
      bindPhoneCodeLen: 'The verification code is 6 digits',
      bindPhoneHint:
        'The code is sent to the new phone number. Once verified, the old number stops working immediately.',
      bindPhoneConfirm: 'Confirm',
      bindPhoneSuccess: 'Phone number updated',
      mfa: 'Two-factor authentication (MFA)',
      // ⚠️ M113(2026-10-29): MFA used to be a bare "not enabled" tag plus a
      //   disabled button — users had no idea what it was or why they'd want it.
      //   These three lines explain it in place.
      mfaWhat: 'Two-factor authentication (MFA) = sign-in needs one more proof of identity besides the password',
      mfaWhy:
        'Once enabled, sign-in requires an SMS code sent to your bound phone number in addition to the password — even if someone else learns your password, they cannot sign in without your phone.',
      statusPrefix: 'Status:',
      notEnabled: 'Not enabled',
      enabled: 'Enabled',
      enableMfa: 'Enable MFA',
      disableMfa: 'Disable MFA',
      mfaNeedPhone: 'Bind a phone number first (above) — verification codes are sent to it.',
      mfaEnableTitle: 'Enable two-factor authentication',
      mfaEnableHint: 'The code will be sent to the phone number bound to this account. Once verified, two-factor authentication takes effect immediately.',
      mfaEnableConfirm: 'Enable',
      mfaDisableTitle: 'Disable two-factor authentication',
      mfaDisableHint: 'After this, signing in only needs the password. To confirm it is really you, verify an SMS code sent to your bound phone number first.',
      mfaDisableConfirm: 'Disable',
      mfaCodeLabel: 'SMS verification code',
      mfaEnabled: 'Two-factor authentication enabled',
      mfaDisabled: 'Two-factor authentication disabled',
      // One tooltip for every feature that is not implemented yet
      phase6: 'To be implemented in Phase 6',
      // My identities
      current: 'Current',
      primaryIdentity: 'Primary',
      validPeriod: 'Valid',
      switchHint: 'Use the identity switcher in the top right to switch',
      // Change-password dialog
      confirmChange: 'Confirm',
      pwdChanged: 'Password changed. You will be signed out and asked to sign in again.',
      pwdNeedLetter: 'Must contain letters',
      pwdNeedDigit: 'Must contain digits',
    },
    fileFolder: {
      description: 'Folders isolated per business segment. You can put files in a folder and pick a cover (only images can be used as a cover).',
      bizSegment: 'Business segment',
      coverFileId: 'Cover file ID',
      coverKey: 'Cover Key',
      createFolder: 'New folder',
      defaultFolder: 'Default folder',
      defaultNotDeletable: 'The default folder cannot be deleted (uploads fall back to it)',
      deleteDescCount: ' file(s) in it (',
      deleteDescPrefix: 'The ',
      deleteDescSub: 'Its {n} subfolder(s) (and the files inside them) will be deleted as well.',
      deleteDescSuffix: ') will be deleted as well, and this cannot be undone.',
      deleteTitlePrefix: 'Delete folder ',
      detailTitle: 'Folder Details',
      viewFiles: 'View files',
      // ===== M125: nested folders =====
      /** The root node of the breadcrumb */
      rootDir: 'Root',
      /** Row action: enter this folder */
      enter: 'Enter',
      /** Row action: create a subfolder under this folder */
      createSubFolder: 'New subfolder',
      /** Tell the user where the new folder will land */
      createInPath: 'Will be created under "{path}"',
      /** Placeholder when there is no cover */
      noCover: 'No cover',
      /** File count shown on the folder card — M144 */
      fileCountValue: '{n} files',
      /** Accessible label of the view switcher — M144 */
      viewMode: 'View',
      /** Table view — M144 */
      viewTable: 'Table',
      /** Card view — M144 */
      viewGrid: 'Cards',
      /** Full path field in the detail drawer */
      fullPath: 'Full path',
      /** The list searches across all levels while a keyword is set */
      searchAllLevels: 'Search: all levels',
      // ===== M139: user layer =====
      /** The user layer (first breadcrumb level, visible to super admins) */
      userLayer: 'Users',
      /** Page description of the user layer */
      userLayerDescription: 'Browse folders per user. Click a name to open the folder space of that user (read-only).',
      /** Search placeholder of the user layer — it searches people, not folders */
      userLayerSearchPlaceholder: 'Search username / nickname / name',
      /** Row action: open this user's folder space */
      enterSpace: 'Open space',
      /** Fallback when the owner name cannot be resolved */
      mySpace: 'My folders',
      /** Super admin looking at their own space */
      ownSpaceDescription: 'Folder space of {name}.',
      /** Super admin looking at the space of another user (read-only) */
      otherSpaceDescription: 'Folder space of {name} (read-only).',
      /** Banner shown in a read-only space of another user */
      readOnlySpace: 'Read-only: this folder space belongs to another user',
      // ===== Mixed list (2026-11-03 acceptance feedback: merge the two lists into one) =====
      /** Section heading for the "Files" block inside the mixed card view */
      filesTitle: 'Files',
      /** Summary line: pagination covers folders only, the file count stays visible on every page */
      mixedTotal: '{folders} folders · {files} files',
      /** Fallback entry when files exceed one batch (jumps to the file list page with folder_id) */
      viewAllFiles: 'View all {n} files',
    },
    fileList: {
      description: 'All files uploaded by the current account. Folders are isolated per user - nobody else can see anything here.',
      allFolders: 'All folders',
      clickOrDropUpload: 'Click or drag to upload',
      copyDirectLink: 'Copy link',
      copyDirectLinkDone: 'Link copied to clipboard',
      copyDirectLinkFailed: 'Copy failed, please copy the URL manually',
      coverTag: 'Cover',
      coverOnlyImage: 'Only images can be used as a cover',
      deleteDesc: 'The stored object will be deleted as well, the quota is released accordingly, and this cannot be undone.',
      detailTitle: 'File Details',
      download: 'Download',
      /** Number of files in the current scope (follows the folder filter) */
      fileCount: '{n} files',
      folderId: 'Folder ID',
      notInFolder: 'This file is not in a folder',
      objectKey: 'Object Key',
      /** Thumbnail view: the file is an image but its direct link is missing or failed to load */
      previewUnavailable: 'Preview unavailable',
      /* --- M145: can the browser play it (the criterion lives server-side in pkg/media) --- */
      /** Badge: the browser cannot play this file (hover shows the server-side reason) */
      mediaUnplayable: 'Not browser-playable',
      /** Badge: the transcode has not started yet */
      mediaTranscodePending: 'Transcode pending',
      /** Badge: transcoding right now */
      mediaTranscoding: 'Transcoding',
      /** Badge: a playable copy is ready */
      mediaTranscoded: 'Transcoded',
      /** Badge: the transcode failed */
      mediaTranscodeFailed: 'Transcode failed',
      /** Quota ring tooltip: account total usage / quota limit */
      quotaTooltip: 'Total used {used} / quota {quota}',
      refreshQuota: 'Refresh quota',
      /** Prefix of the scope usage tag shown next to the folder filter */
      scopeFolder: 'This folder',
      setCover: 'Set as cover',
      setFolderCoverPrefix: 'Set ',
      setFolderCoverSuffix: ' as the cover of its folder (only one cover per folder, it will be overwritten).',
      setFolderCoverTitle: 'Set as folder cover?',
      storageLocal: 'Local',
      storageOSS: 'OSS',
      /** Account-level total usage (shown when the quota is unlimited) */
      totalUsage: 'Total used',
      uploadingPrefix: 'Uploading ',
      uploadingSuffix: ' files',
      /** View switch (table / thumbnails) — M134 */
      viewGrid: 'Thumbnails',
      viewMode: 'View',
      viewTable: 'Table',
    },
    flowStage: {
      description: 'Where the approvers of this stage come from: specific users / specific roles / the department supervisor of the applicant',
      addRule: 'Add rule',
      approverSource: 'Approver source',
      backToStage: 'Back to stages',
      deleteRuleDesc: 'The whole template is written back, not just this row.',
      deleteRuleTitle: 'Delete this rule?',
      deptLeaderApprover: 'The applicant\'s direct supervisor, {n} level(s) up',
      editRule: 'Edit rule',
      levelsUp: 'Levels up',
      multiRuleHint: 'Multiple rules within the same stage are OR relations — any one of them matching means someone can approve',
      ruleDeleted: 'Rule deleted',
      ruleSourceDeptLeader: 'Dept leader (the applicant\'s direct supervisor)',
      ruleSourceDeptLeaderTag: 'Dept leader',
      ruleSourceRole: 'Specific role',
      ruleSourceRoleTag: 'Specific role',
      ruleSourceUser: 'Specific user',
      ruleSourceUserTag: 'Specific user',
      stageTitleWithRules: '{name} · Approver rules',
    },
    configAudit: {
      description: 'Read-only audit records - every configuration change made by a super_admin is written to sys_config_audit',
      descriptionFiltered: 'Current group: {group} (only changes of the configs in this group)',
      afterValue: 'After change',
      beforeValue: 'Before change',
      clearGroupFilter: 'Clear group filter',
      detailTitle: 'Audit details',
      groupTag: 'Group: {group}',
      userFallback: 'User #{id}',
    },
    // Dynamic business collections (M152) -- form designer + generic record list
    //
    // NOTE: no hard-coded Chinese in the pages -- see
    // locales/__tests__/hardcoded-chinese-i18n.test.ts
    dyn: {
      designer: {
        description:
          'Business form designer -- once collections and fields are defined, the menu, CRUD endpoints and permission codes are generated automatically',
        collections: 'Collections',
        newCollection: 'New collection',
        editCollection: 'Edit collection',
        emptyCollections: 'No collection yet -- click "New collection" to start',
        searchPlaceholder: 'Search by collection name / code',
        fields: 'Fields',
        newField: 'New field',
        editField: 'Edit field',
        emptyFields: 'This collection has no field yet',
        selectCollectionFirst: 'Select a collection on the left first',
        selectCollectionHint: 'Select a collection on the left to maintain its fields',
        code: 'Collection code',
        codeHelp:
          'Starts with a lowercase letter; only lowercase letters, digits and underscores. Cannot be changed later -- it is the source of the table name and the permission codes',
        codeInvalid: 'Collection code must start with a lowercase letter and contain only lowercase letters, digits and underscores',
        name: 'Collection name',
        descriptionField: 'Description',
        ownerScoped: 'Visible to creator only',
        ownerScopedHelp: 'When on, regular staff only see the records they created (super admins are not restricted)',
        // public read (2026-11-03) -- mutually exclusive with "visible to creator only"
        publicRead: 'Public read (no login)',
        publicReadHelp:
          'When on, anyone can read the schema and records of this collection through the public endpoints without logging in (read-only; writes still require login). Mutually exclusive with "Visible to creator only"',
        icon: 'Menu icon',
        sort: 'Sort',
        // collection binds an approval template (2026-11-03)
        approvalTemplate: 'Approval template',
        approvalTemplateHelp:
          'When bound, records of this collection can be submitted into this approval flow (the record list gets a "Submit for Approval" action and an approval-status column); leave unbound for no approval',
        approvalTemplateNone: 'Not bound',
        fieldKey: 'Field key',
        fieldKeyHelp:
          'Lowercase identifier, cannot be changed later -- it is the source of the database column name and the record JSON key',
        fieldKeyInvalid: 'Field key must start with a lowercase letter and contain only lowercase letters, digits and underscores',
        label: 'Display name',
        type: 'Field type',
        typeHelp: 'Cannot be changed later -- changing the type means altering the column type (rebuild the table and migrate the data)',
        required: 'Required',
        options: 'Options',
        optionsHelp: 'Candidates for single/multi select fields. The value is what gets stored; the label is what people read',
        constraints: 'Value constraints',
        constraintsHelp:
          'Enforced on every write path (create / edit / import / batch); leave empty = disabled. Text length counts characters (a Chinese char counts as 1)',
        minLength: 'Min length (chars)',
        maxLength: 'Max length (chars)',
        pattern: 'Format pattern (regex)',
        patternHelp: 'Go regex, full-string match. Example: ^010-\\d{8} (010- prefix plus 8 digits)',
        minValue: 'Min value',
        maxValue: 'Max value',
        // Cross-collection relation field (M5)
        relation: 'Relation',
        relationHelp: 'References records in another business collection: the value stored is the target record id, displayed via one of its fields. Self-reference is allowed (e.g. "parent task")',
        relationTargetPlaceholder: 'Select target collection',
        relationDisplayPlaceholder: 'Field to display (empty = first text field automatically)',
        relationDisplayHelp: 'Target records appear as this field value in dropdowns and lists; falls back automatically if the display field is deactivated',
        relationTargetMissing: 'Target collection {code} does not exist or is inactive — fix before saving',
        // Collection-level indexes (M2)
        indexes: 'Indexes',
        indexesHelp:
          'Creates database indexes on the physical table (unique indexes also block duplicate values); when a referenced field is deactivated, its indexes are dropped with it',
        indexesNoFields: 'Add fields first to configure indexes',
        indexName: 'Index name (lowercase)',
        indexColumns: 'Index columns (multi-select, order = composite order)',
        indexUnique: 'Unique',
        addIndex: 'Add index',
        defaultValue: 'Default value',
        defaultValueHelp: 'Filled in when a new record leaves this field empty',
        deleteCollectionTip:
          'The collection is deactivated and its menu and permission codes are disabled; the physical table and its data are kept, nothing is physically deleted',
        deleteFieldTip:
          'The field is deactivated and disappears from the list and the form; the physical column and its data are kept',
        fieldType: {
          text: 'Single-line text',
          longtext: 'Multi-line text',
          int: 'Integer',
          number: 'Decimal',
          bool: 'Switch',
          date: 'Date',
          datetime: 'Date & time',
          select: 'Single select',
          multiselect: 'Multi select',
          json: 'JSON',
          file: 'Attachment',
          relation: 'Record relation',
        },
      },
      options: {
        empty: 'No candidate yet',
        valuePlaceholder: 'Option value (what gets stored)',
        labelPlaceholder: 'Display name',
        remove: 'Remove this candidate',
        add: 'Add candidate',
        duplicate: 'Two candidates share the same value -- it becomes ambiguous which one a record holds',
        // "Manual" vs "Linked system table" (2026-11-02)
        //   The two sources are mutually exclusive: manual candidates live in
        //   the field definition, table candidates are queried when a record is
        //   read (one more row in the table = one more option in the form).
        manual: 'Manual',
        table: 'Linked system table',
        sourcePlaceholder: 'Pick a system table as the option source',
        sourceTable: 'Options come from column {column} of table {table}; the label is the readable name of the same row',
        sourceUnreadable:
          'Your account cannot read this table ({perm}) -- configuring it still yields no options',
      },
      records: {
        description: 'Generic business records -- columns and form are generated from the fields defined in the designer',
        searchPlaceholder: 'Search text fields',
        // Advanced filter (M4)
        filterPlaceholder: "Advanced filter: status = 'active' && amount > 100",
        filterTitle:
          "Filter syntax: comparison = != > >= < <=, contains ~ !~, any-of ?= ('a','b'), null = null, logic && || ( ). Use field keys; unknown fields are rejected",
        newRecord: 'New record',
        editRecord: 'Edit record',
        detailTitle: 'Record details',
        deleteTip: 'The record disappears from the list (the row is still kept in the database)',
        jsonInvalid: 'Not valid JSON',
        schemaFailed: 'Cannot load the schema of this collection',
        noSchema: 'This collection has no usable field yet',
        // Attachment field (2026-11-02)
        //   The field holds the file **key** (the path without the domain),
        //   not a URL.
        file: {
          upload: 'Upload file',
          reupload: 'Upload again',
          clear: 'Clear',
          missing: 'The file is gone (the file referenced by this record cannot be found)',
          uploadFailed: 'File upload failed',
          // No file:upload permission ⇒ the upload button is not rendered
          //   (same rule as the file list page) — so say why.
          noUploadPerm: 'You do not have permission to upload files, so no attachment can be added',
        },
        // "API Integration" guide modal (2026-11-02)
        //   The endpoints are generated when the collection is created in the
        //   designer, but the user cannot see that -- so hand them a copyable guide.
        apiGuide: {
          button: 'API Integration',
          title: 'API Integration Guide',
          intro:
            'The CRUD endpoints of this collection were generated automatically when the collection was created in the designer -- call them as below, no backend code needed.',
          bodyKey:
            'Request body keys are the field identifiers (the lowercase keys you entered in the designer), not the database column names; the field type decides the value format.',
          collectionCode: 'Collection Code',
          basePath: 'Base Path',
          auth: 'Authentication',
          endpoints: 'Endpoints',
          fields: 'Fields',
          example: 'Example',
          requestBody: 'Request Body',
          response: 'Response',
          epSchema: 'Get the form schema (which fields this collection has)',
          epList: 'List records (paging / keyword / sorting)',
          epRead: 'Get one record',
          epCreate: 'Create a record',
          epUpdate: 'Update a record (only the fields present in the body)',
          epDelete: 'Delete a record (soft delete, the row is still kept)',
          sdkTitle: 'JS SDK (recommended)',
          sdkDesc:
            'Skip raw HTTP calls with the official zero-dependency SDK (@rapid-admin/dyn-sdk): it attaches the token, unwraps the response envelope and throws errors carrying trace_id; the snippet below is ready to copy.',
        },
        // Row-level batch ops + CSV import/export (2026-11-03)
        trash: 'Trash',
        trashOn: 'Back to list',
        restore: 'Restore',
        restoreTip: 'The record will return to the normal list',
        batchDelete: 'Delete selected',
        batchDeleteTip: 'These records will move to the trash (data is still kept)',
        batchEdit: 'Edit selected',
        batchEditTip: 'Set one field of the {n} selected rows to the same value',
        batchEditSelectField: 'Select a field to update',
        batchEditValuePlaceholder: 'Enter the new value',
        batchEditValueJson: 'JSON array, e.g. ["a","b"]',
        export: 'Export',
        import: 'Import',
        importTitle: 'Import records (CSV)',
        importSelectFile:
          'CSV header accepts field keys or labels; up to 5000 rows / 5MB per import',
        importNoFile: 'No file selected',
        importMode: 'Import mode',
        importModeAll: 'All or nothing',
        importModeAllTip: 'If any row has an error, nothing is imported',
        importModeSkip: 'Skip bad rows',
        importModeSkipTip: 'Rows with errors are skipped, only valid rows are imported',
        importStart: 'Start import',
        importResultStat: '{total} rows read, {inserted} imported, {skipped} skipped',
        // importErrorRow/Field/Message moved to field.* group on 2026-11-03 (shared column-title group)
        importNoErrors: 'No bad rows',
        // submit-for-approval + approval status (2026-11-03, collection bound to a template)
        submitApproval: 'Submit for Approval',
        approvalSubmitted: 'Submitted for approval',
        approvalNone: 'Not submitted',
      },
    },
  },
  // HTTP client error messages
  error: {
    networkError: 'Network error, please check your connection',
    unauthorized: 'Session expired, please log in again',
    forbidden: 'You do not have permission for this action',
    serverError: 'Server error, please try again later',
    validateFailed: 'Parameter validation failed',
    notFound: 'Resource not found',
    timeout: 'Request timeout, please try again',
    requestFailed: 'Request failed',
    businessError: 'Business error',
    unknownError: 'Unknown error',
    operationFailed: 'Operation failed',
    requestFailedWithStatus: 'Request failed ({status})',
    networkException: 'Network exception',
    // ErrorBoundary fallback UI (not an HTTP error page)
    boundaryTitle: 'Something went wrong',
    boundaryReload: 'Reload page',
    // M109 fatal fallback UI (no antd / ConfigProvider dependency)
    fatalTitle: 'The application hit a fatal error',
    fatalDesc:
      'The page cannot keep rendering. Please reload and try again; if it keeps happening, contact your administrator.',
    // M10 error pages
    page403: {
      title: '403',
      subTitle: 'Sorry, you do not have access to this page',
      backHome: 'Back to Home',
      hint: 'Please contact the system administrator to request access',
    },
    page404: {
      title: '404',
      subTitle: 'Sorry, the page you visited does not exist',
      backHome: 'Back to Home',
      hint: 'Please check the URL or return to the home page',
    },
    page500: {
      title: '500',
      subTitle: 'Sorry, the server encountered an error',
      retry: 'Retry',
      copy: 'Copy Error Code',
      copied: 'Copied to clipboard',
      // Label on the error-code button
      codeLabel: 'Error code: {code}',
    },
  },
  // Business code translations (M6) — aligned with server errcode.go
  // Add a new code? Register in types/api.ts::BIZ_CODE_KEYS AND here.
  bizCode: {
    // 4xxxx Basic
    '40000': 'Invalid request parameters',
    '40001': 'Required parameter missing',
    '40002': 'Invalid parameter format',
    '40003': 'Invalid parameter',
    '40100': 'Not logged in or session expired',
    '40101': 'Session expired, please log in again',
    '40102': 'Invalid auth token, please log in again',
    '40300': 'You do not have permission for this action',
    '40301': 'Current identity is disabled',
    '40302': 'This action is outside your working scope',
    '40303': 'You do not have access to this resource',
    '40400': 'The requested resource does not exist',
    '40900': 'Data has changed, please refresh and try again',
    '41300': 'Request payload is too large',
    '42900': 'Too many requests, please try again later',
    // 50000 Internal server error
    '50000': 'Internal server error, please try again later',
    // 2xxxx Business
    '20100': 'Login failed',
    '20101': 'Account locked, please try later or contact admin',
    '20102': 'Incorrect captcha',
    '20103': 'Please complete the captcha first',
    '20104': 'SMS sent too frequently',
    '20105': 'SMS code is incorrect or expired',
    '20108': 'SMS send is rate-limited, please try later',
    '20109': 'Two-factor verification is enabled for this account. Enter the SMS code.',
    '20106': 'Password is too weak',
    '20107': 'Password expired, please change it',
    '20200': 'The submitter and the reviewer must be different people',
    '20201': 'You cannot grant higher permissions to yourself',
    '20202': 'You cannot grant permissions beyond your own scope',
    '20203': 'Current state does not allow this action',
    '20601': 'Approval workflow is misconfigured: the approver could not be resolved, please contact an administrator',
    '20602': 'Department hierarchy is corrupted (cycle or missing parent), the approver cannot be located, please contact an administrator',
    '20603': 'The approver rule is misconfigured, please contact an administrator',
    // 3xxxx Platform config
    '30100': 'Configuration key not found',
    '30101': 'This configuration is read-only',
    '30200': 'There is a pending configuration change',
    '30201': 'The change has not been reviewed',
    '30202': 'The requester and the approver must be different people',
    '30203': 'The configuration change state does not allow this action',
    // 5xxxx Framework
    '50100': 'This feature is not implemented yet',
    '50300': 'Service temporarily unavailable, please try later',
    '50400': 'Request timed out, please try later',
    '50500': 'The requested API version is not supported',
    '50600': 'Request signature is missing',
    '50601': 'Request signature has expired, please retry',
    '50602': 'Request signature verification failed',
    '50700': 'Malformed request payload',
    '50701': 'Unsupported Content-Encoding (only gzip/identity)',
  },
  // Dictionary labels (constants/dict.ts)
  dict: {
    status: { active: 'Active', disabled: 'Disabled', locked: 'Locked' },
    userStatus: { active: 'Normal', disabled: 'Disabled', locked: 'Locked' },
    gender: { male: 'Male', female: 'Female', unknown: 'Unknown' },
    configChangeStatus: { pending: 'Pending Review', approved: 'Approved', rejected: 'Rejected', expired: 'Expired' },
  },
  // Owner source (frontend-only enum; the backend has no matching sys_dict group)
  // Role names (constants/roles.ts ROLE_NAME_MAP)
  // ⚠️ These keys map 1:1 onto the frontend `RoleCode` values,
  //   which share the backend seed's codes.
  role: {
    superAdmin: 'Super Admin',
    readonly: 'Read-only Viewer',
    platformOps: 'Platform Ops',
    // 2026-10-25 Module B-2: the "no special role" tag on the profile user card
    normalUser: 'Normal user',
  },
}

export default enUS
