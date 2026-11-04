/**
 * 资源图标词表 —— **单一真源**
 *
 * # 为什么单独抽一个文件
 *
 * 以前这张表是 Sidebar.tsx 里的一个私有 `iconMap`,只有侧边栏用得到。
 * 2026-10-30 用户报「图标是让人手填的,根本不知道填什么」⇒ 资源编辑弹窗
 * 要给一个**下拉选择**。于是同一张表就有两个消费者了 ——
 * 如果下拉那边再抄一份,两份词表迟早对不上(加了一个图标,侧边栏有、
 * 下拉里没有,或者反过来),而这种不一致**不报错**,只是菜单图标时有时无。
 *
 * ⇒ 一张表,两个地方 import。
 *
 * # 词表里存的是「key」,不是图标本身
 *
 * 后端 sys_resource.icon 存的是**字符串 key**(如 'system' / 'job'),
 * 前端拿到 key 再换算成图标 —— 见 resolveResourceIcon。
 * 所以这张表是「后端字段值 → 前端图标」的映射表,**后端不存图标本身**。
 *
 * ⚠️ 词表与 seed 里的 Icon 字段值必须对得上:
 *   server/internal/seed/seed.go 的 menuList() 里 Icon: "xxx" 用的就是这里的 key。
 *   写 seed 时新增一个 Icon,记得回来在这里登记一行(不然菜单图标是空的)。
 *
 * # 为什么是 .ts 而不是 .tsx
 *
 * 表里存的是**图标组件**(不是渲染好的元素),渲染时才 createElement。
 * 这样文件里没有 JSX ⇒ 保持 .ts ⇒ eslint 的 react-refresh 规则**不扫 .ts**。
 *
 * ⚠️ 如果写成 .tsx(表里直接放 `<DashboardOutlined />`):
 *   该规则会报 "Fast refresh only works when a file only exports components" ——
 *   因为这个文件"定义了组件"(JSX 里的 `<DashboardOutlined />` 算)又导出非组件
 *   (词表 + resolve 函数)。本仓 lint 是 `--max-warnings 0`,警告也算失败。
 *   (实测:写成 .tsx 就是 1 error + 2 warning;改成 .ts 全部消失。)
 */
import { createElement, type ReactNode } from 'react'
import {
  AccountBookOutlined,
  AlertOutlined,
  ApartmentOutlined,
  ApiOutlined,
  AuditOutlined,
  BankOutlined,
  BarChartOutlined,
  BookOutlined,
  CameraOutlined,
  CarOutlined,
  ClockCircleOutlined,
  CloudOutlined,
  CommentOutlined,
  ContactsOutlined,
  ContainerOutlined,
  CreditCardOutlined,
  CustomerServiceOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DownloadOutlined,
  EnvironmentOutlined,
  ExperimentOutlined,
  ExportOutlined,
  FileAddOutlined,
  FileDoneOutlined,
  FileSearchOutlined,
  FolderOpenOutlined,
  FolderOutlined,
  FormOutlined,
  FundOutlined,
  HeartOutlined,
  IdcardOutlined,
  ImportOutlined,
  InsuranceOutlined,
  KeyOutlined,
  LockOutlined,
  MailOutlined,
  MedicineBoxOutlined,
  MessageOutlined,
  MobileOutlined,
  MoneyCollectOutlined,
  MonitorOutlined,
  NotificationOutlined,
  PaperClipOutlined,
  PayCircleOutlined,
  PhoneOutlined,
  PictureOutlined,
  PrinterOutlined,
  ProfileOutlined,
  PushpinOutlined,
  QrcodeOutlined,
  ReconciliationOutlined,
  RestOutlined,
  RobotOutlined,
  RocketOutlined,
  SafetyCertificateOutlined,
  SafetyOutlined,
  SaveOutlined,
  ScheduleOutlined,
  SearchOutlined,
  SendOutlined,
  SettingOutlined,
  ShareAltOutlined,
  ShoppingCartOutlined,
  ShoppingOutlined,
  SolutionOutlined,
  StarOutlined,
  TableOutlined,
  TagOutlined,
  TeamOutlined,
  ToolOutlined,
  TransactionOutlined,
  TrophyOutlined,
  TruckOutlined,
  UploadOutlined,
  UserOutlined,
  VideoCameraOutlined,
  WalletOutlined,
} from '@ant-design/icons'

/** antd 图标的组件类型(所有 @ant-design/icons 导出都是这一个类型) */
type AntdIcon = typeof DashboardOutlined

/**
 * 图标 key → 图标组件
 *
 * ⚠️ key 名不许改 —— 它同时是:
 *   · 后端 sys_resource.icon 存的值
 *   · seed 里 menuList() 的 Icon 字段值
 *   · i18n key `resourceIcon.<key>` 的后缀(见 locales/zh-CN.ts)
 */
export const RESOURCE_ICON_MAP: Record<string, AntdIcon> = {
  dashboard: DashboardOutlined,
  user: UserOutlined,
  team: TeamOutlined,
  role: TeamOutlined,
  permission: KeyOutlined,
  dept: ApartmentOutlined,
  // 字典(dictionary / lookup tables)—— 书本图标最贴合「字典」语义
  // 之前复用 SettingOutlined 跟系统设置撞, 不易区分
  dict: BookOutlined,
  config: SettingOutlined,
  // 定时任务 —— 钟表图标。seed 里 menu.system.job 的 Icon 字段是 'job',
  // Sidebar 走 r.icon 分支命中这里(而不是走 code 的 resolveIcon)
  job: ClockCircleOutlined,
  // 文件(M82)—— 种子里 icon 字段填 'file',子菜单共用父级图标所以只登记一个
  file: FolderOutlined,
  audit: AuditOutlined,
  system: SettingOutlined,
  security: SafetyOutlined,
  // 审批(M83)—— 后端 menu.flow / menu.approval 的 Icon 字段填 'flow' / 'approval',
  // 这里必须登记对应条目,否则 resolveIcon 返回 null,菜单条目渲染成没有图标的裸文字。
  // 审批流程(配模板)= 分支图标;我的审批(单据)= 单据图标。
  flow: ApartmentOutlined,
  approval: FileDoneOutlined,
  // 审批管理(2026-11-01 新增的父菜单,后端 menu.approvalMgmt 的 Icon 字段填
  // 'approvalMgmt')—— 容器图标,和下面两个子菜单的图标区分开,
  // 否则父节点和「我的审批」长得一模一样,侧边栏里分不清层级。
  approvalMgmt: ContainerOutlined,
  // 短信模板(M115)—— 后端 menu.system.smsTemplate 的 Icon 字段填 'sms',
  // 这里必须登记对应条目,否则 resolveIcon 返回 null,菜单条目渲染成没有图标的裸文字。
  // 用 MessageOutlined(信封气泡)而不是手机图标:antd 的 MobileOutlined 在
  // 侧边栏小尺寸下糊成一团,信封更清楚。
  sms: MessageOutlined,
  // 'api' 条目曾给 menu.apiDocs(2026-11-01 §8 G5)用;该菜单 2026-11-03 已摘除
  // (入口移到帮助按钮下拉),条目保留 —— 词表多一个成员无害,真要删先确认
  // sys_resource 里没有存量行还带着 'api'。
  api: ApiOutlined,
  // 业务表单设计器 / 动态业务集合(M151)—— 后端 menu.system.dyn 的 Icon 字段
  // 填 'table',动态集合菜单(menu.dyn.<集合码>)的默认图标也是 'table'。
  // 这里必须登记对应条目,否则 resolveIcon 返回 null,菜单条目渲染成没有
  // 图标的裸文字。
  // 用 TableOutlined 而不是 AppstoreOutlined:两个域在侧边栏里
  // 长得一样就分不出来了。
  table: TableOutlined,

  // ── 下拉可选的富余图标(2026-11-02 · 图标选择器扩容)────────────────
  //
  // 以下条目**后端 seed 不一定用到** —— 它们是图标选择器(IconSelect)
  // 的可选项:用户给动态集合 / 自建菜单挑图标用。
  // (词表 ⇒ seed 方向是富余,测试只守 seed ⇒ 词表方向。)
  //
  // ⚠️ **每个 key 都要在 zh-CN / en-US 的 resourceIcon 段登记标签**,
  //   constants/__tests__/resourceIcons.test.ts 有一条用例核对,
  //   漏一个 ⇒ 下拉里露出裸 key。

  // 办公文书
  form: FormOutlined,
  profile: ProfileOutlined,
  reconciliation: ReconciliationOutlined,
  schedule: ScheduleOutlined,
  search: SearchOutlined,
  chart: BarChartOutlined,
  monitor: MonitorOutlined,
  // 业务对象
  building: BankOutlined,
  car: CarOutlined,
  tool: ToolOutlined,
  phone: PhoneOutlined,
  mail: MailOutlined,
  contacts: ContactsOutlined,
  idcard: IdcardOutlined,
  solution: SolutionOutlined,
  customerService: CustomerServiceOutlined,
  shopping: ShoppingOutlined,
  cart: ShoppingCartOutlined,
  rest: RestOutlined,
  truck: TruckOutlined,
  medicineBox: MedicineBoxOutlined,
  experiment: ExperimentOutlined,
  // 财务
  accountBook: AccountBookOutlined,
  wallet: WalletOutlined,
  payCircle: PayCircleOutlined,
  creditCard: CreditCardOutlined,
  moneyCollect: MoneyCollectOutlined,
  transaction: TransactionOutlined,
  fund: FundOutlined,
  // 数据与文件
  database: DatabaseOutlined,
  cloud: CloudOutlined,
  fileSearch: FileSearchOutlined,
  fileAdd: FileAddOutlined,
  folderOpen: FolderOpenOutlined,
  import: ImportOutlined,
  export: ExportOutlined,
  upload: UploadOutlined,
  download: DownloadOutlined,
  print: PrinterOutlined,
  save: SaveOutlined,
  paperClip: PaperClipOutlined,
  // 消息协同
  comment: CommentOutlined,
  notification: NotificationOutlined,
  send: SendOutlined,
  share: ShareAltOutlined,
  qrcode: QrcodeOutlined,
  // 标记与状态
  star: StarOutlined,
  heart: HeartOutlined,
  trophy: TrophyOutlined,
  tag: TagOutlined,
  pushpin: PushpinOutlined,
  environment: EnvironmentOutlined,
  rocket: RocketOutlined,
  alert: AlertOutlined,
  insurance: InsuranceOutlined,
  safetyCertificate: SafetyCertificateOutlined,
  lock: LockOutlined,
  // 媒体与设备
  camera: CameraOutlined,
  picture: PictureOutlined,
  video: VideoCameraOutlined,
  robot: RobotOutlined,
  mobile: MobileOutlined,
}

/** 词表里的全部 key(下拉框的选项来源) */
export const RESOURCE_ICON_KEYS: string[] = Object.keys(RESOURCE_ICON_MAP)

/**
 * resolveResourceIcon 把 icon key 换算成图标
 *
 * 找不到就返回 null —— 侧边栏据此渲染成"没有图标的裸文字",
 * 而不是抛错(一个没登记的 key 不该让整个侧边栏挂掉)。
 */
export function resolveResourceIcon(key?: string | null): ReactNode {
  if (!key) return null
  const Comp = RESOURCE_ICON_MAP[key]
  return Comp ? createElement(Comp) : null
}
