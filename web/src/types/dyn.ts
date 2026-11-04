/**
 * 动态业务集合(M151/M152)—— 与后端 internal/service/dyn.go 的 DTO 对齐
 *
 * ⚠️ **字段名一律 snake_case** —— 后端 wire 上就是 snake_case
 *   (见 server/internal/service/dyn.go 的 json tag)。
 *   驼峰写法在 Go 的 encoding/json 里**不认下划线**(只做大小写不敏感匹配),
 *   所以前端写 camelCase ⇒ 绑定失败 ⇒ 字段全空。本仓 2026-10-18 专门
 *   为这件事做过一次全仓迁移,别改回去。
 *
 * # 两面
 *
 *   · **设计面**(集合 / 字段)—— 管理员在「业务表单设计器」里维护
 *   · **数据面**(记录)—— 业务人员用「通用列表页」增删改查
 */

/**
 * 集合(≈ 一张业务表的"表头")
 *
 * ⚠️ **code 不可改** —— 它同时是物理表名(`dyn_<code>`)和资源码
 *   (`dyn:<code>:list`)的来源,改了等于换了一张表 + 换了一套权限。
 */
export interface DynCollection {
  /** 本仓 uint64 在 wire 上是字符串(19 位,转 Number 会丢精度) */
  id: string
  /** 集合编码(小写英文标识符) */
  code: string
  /** 集合名称(中文) */
  name: string
  /** 描述 */
  description: string
  /** 物理表名 —— 排障用,业务不用管 */
  phys_table: string
  /** 行级权限:只看得见自己创建的记录 */
  owner_scoped: boolean
  /**
   * 公开只读(2026-11-03)
   *
   * ⚠️ **与 owner_scoped 互斥** —— 匿名调用者没有 owner,
   *   后端建 / 改集合时校验,前端弹窗里两个开关同步禁掉组合。
   *   开着的时候未登录可经 /rapid-admin/v1/public/dyn/:collection/** 读
   *   schema / 列表 / 详情;写路径永远要登录。
   */
  public_read: boolean
  /** 菜单图标 key(见 @/constants/resourceIcons) */
  icon: string
  /** 菜单排序 */
  sort: number
  /** 是否启用 */
  is_active: boolean
  /** 绑定的审批模板 id(字符串形态的 19 位 uint64;'0'/缺省 = 未绑定)(2026-11-03) */
  approval_template_id?: string
  /**
   * 用户自定义索引定义(JSON 文本,`[]DynIndexDef`;空串 = 没配)(M2)
   *
   * ⚠️ **与后端同步发生:保存时后端真的建/删物理索引** ——
   *   唯一索引撞存量重复值会保存失败并报可读的错。
   *   解析用 @/api/dyn 的 parseDynIndexes(解析失败返空数组,不炸页面)。
   */
  indexes?: string
  created_at?: string
  updated_at?: string
}

/**
 * 字段定义(设计面)
 *
 * ⚠️ **field_key / type 都不可改** ——
 *   field_key 是物理列名(`f_<key>`)和前端 JSON 键的来源;
 *   type 变了意味着改列类型(重建表 = 全量数据搬迁 + 途中失败留半张表)。
 *   想换类型:新建一个字段,把数据搬过去,再停用旧字段。
 */
export interface DynField {
  id: string
  /** 所属集合 */
  collection_id: string
  /** 字段标识(小写英文) */
  field_key: string
  /** 中文显示名(可改) */
  label: string
  /** 物理列名 —— 排障用 */
  column: string
  /** 字段类型(见 DynFieldType) */
  type: string
  /** 是否必填 */
  required: boolean
  /**
   * 选项集(JSON 文本)
   *
   * ⚠️ **这里是 JSON 文本,不是数组** —— 后端存的是 TEXT
   *   (`{"choices":[{"value":"a","label":"甲"}]}`)。
   *   前端要显示成数组时用 @/api/dyn 的 parseDynOptions 转一道。
   */
  options: string
  /** 默认值(文本形态) */
  default_value: string
  /** 排序 */
  sort: number
  /** 是否在用 */
  is_active: boolean
}

/**
 * 字段类型(后端下发)
 *
 * ⚠️ **前端不许硬编码这张表** —— 后端加一个类型时,
 *   硬编码的前端会"选了那个类型但自己不认识" ⇒ 页面坏掉,而且两边对不上没人知道。
 */
export interface DynFieldType {
  /** 类型标识 */
  type: string
  /** 中文名(下拉框显示) */
  label: string
  /** MySQL 的 DDL 列类型 */
  mysql_type: string
  /** SQLite 的 DDL 列类型 */
  sqlite_type: string
  /** 该类型是否需要选项集 */
  has_options: boolean
}

/** 下拉选项里的一项 */
export interface DynChoice {
  /** 存进数据库的值 */
  value: string
  /** 展示给用户看的名字 */
  label: string
}

/**
 * 一条用户自定义索引(设计面,M2)
 *
 * ⚠️ **columns 是字段标识,不是物理列名** ——
 *   界面上选的是字段;拼物理列(f_<key>)是后端的事。
 */
export interface DynIndexDef {
  /** 索引短名(小写英文标识符;物理名 = dix_<表名>_<短名>) */
  name: string
  /** 索引列(字段标识,顺序即复合索引顺序) */
  columns: string[]
  /** 是否唯一索引 */
  unique: boolean
}

/**
 * 一个可关联的系统表(设计面下拉框用)
 *
 * ⚠️ **前端不许硬编码这张表** —— 后端注册表是唯一真相
 *   (server/internal/pkg/dynx/relation.go)。
 *   硬编码一份的话,后端加一张表时前端下拉框里没有它 ⇒ 用户配不了。
 */
export interface DynRelationSource {
  /** 来源标识(存进 options.source.key) */
  key: string
  /** 中文名 */
  label: string
  /** 来源表名(给用户看清楚"选项是从哪张表来的") */
  table: string
  /** 取值列(存进记录的就是这一列) */
  value_column: string
  /** 需要的资源码(读不到时提示用) */
  resource_code: string
  /**
   * 当前账号能不能读那张表
   *
   * ⚠️ **判定的对象是"当前调用方",不是"设计者"** ——
   *   设计师配了 `user` 来源,不代表录数据的业务员有 `user:list`。
   */
  readable: boolean
}

/**
 * 设计器里的选项集草稿(表单里的值)
 *
 * ⚠️ **与后端 dynx.Options 的形状不一样** ——
 *   后端是 `{choices:[...], source:{kind,key}}`,
 *   而表单里只需要"选哪个来源"(kind 恒为 table)⇒ 这里只存 key。
 *   转换在 @/api/dyn 的 serializeDynOptionsDraft / parseDynOptionsDraft。
 */
export interface DynOptionsDraft {
  /** 手工配置的候选项 */
  choices: DynChoice[]
  /** 关联来源 key(空串 = 手工添加) */
  source: string
  /**
   * relation 字段的目标集合编码(空串 = 不是 relation)
   *
   * ⚠️ **存的是集合编码,不是集合 id** —— 编码不可改(M5),id 在
   *   "集合删了重建"之后会变;两边都以 code 为稳定标识。
   */
  relation_target?: string
  /**
   * relation 字段在目标集合里显示哪个字段(空 = 后端自动挑第一个文本字段)
   */
  relation_display_field?: string
  /** 数值下限(int/number;undefined = 不启用)—— 后端 dynx.Options.Min */
  min?: number
  /** 数值上限(int/number;undefined = 不启用) */
  max?: number
  /** 文本最小长度(text/longtext,按字数;undefined = 不启用) */
  min_length?: number
  /** 文本最大长度(text/longtext,按字数;undefined = 不启用) */
  max_length?: number
  /** 文本格式约束(text,Go 正则整串匹配;空 = 不启用) */
  pattern?: string
}

/**
 * 附件的展示信息(原文件名 + 直链)
 *
 * ⚠️ **它是后端在读记录时另外解析出来的** ——
 *   字段列里只有文件 key,看不出原文件名,也拼不出 CDN 前缀。
 */
export interface DynFileRef {
  /** 原始文件名(用户认的是这个名字) */
  name: string
  /** 直链(可能为空 —— 签不出来时是空串) */
  url: string
}

/** 表单结构里的一个字段(数据面) */
export interface DynSchemaField {
  /** 字段标识(前端 JSON 的键) */
  field_key: string
  /** 中文显示名 */
  label: string
  /** 字段类型 */
  type: string
  /** 是否必填 */
  required: boolean
  /**
   * 选项集(仅 select / multiselect / relation 非空)
   *
   * ⚠️ **三种来源共用这一个字段** ——
   *   手工配置的候选项、关联系统表现查出来的候选、relation 字段现查的
   *   目标集合记录(value = 目标记录 id 文本,label = 显示字段值),
   *   形状完全一样,前端不需要分支处理"这个候选是哪来的"。
   */
  options: DynChoice[]
  /** 选项来源 key(空串 = 手工配置的选项集) */
  option_source?: string
  /**
   * 关联表来源时:当前账号能不能读那张表
   *
   * ⚠️ **手工选项集恒为 true** —— 它是设计者写死在字段定义里的,
   *   不存在"读不到"这回事。
   */
  option_readable?: boolean
  /** 读不到时缺哪个权限码(前端提示用) */
  option_required_perm?: string
  /** 候选被截断了(关联表来源的行数超过上限) */
  option_truncated?: boolean
  /** 默认值(文本形态) */
  default_value: string
  /** 排序 */
  sort: number
}

/**
 * 表单结构(数据面)
 *
 * ⚠️ **业务人员只有 `dyn:<集合码>:list` 权限也得能拿到它** ——
 *   渲染表格列、渲染录入表单都要靠它,而设计面的
 *   `dyn:design:field:list` 是管理员权限。所以结构跟着数据面走。
 */
export interface DynSchema {
  /** 集合编码 */
  code: string
  /** 集合名称 */
  name: string
  /** 行级权限 */
  owner_scoped: boolean
  /** 公开只读(2026-11-03;true 时这条 schema 也能匿名拿到) */
  public_read?: boolean
  /** 绑定的审批模板 id('0'/缺省 = 未绑定;记录页据此渲染送审动作与状态列) */
  approval_template_id?: string
  /** 字段定义(只含在用的) */
  fields: DynSchemaField[]
}

/**
 * 一条记录
 *
 * ⚠️ **键是用户起的字段标识,不是物理列名** ——
 *   所以类型只能是 `Record<string, unknown>`:编译期不可能知道有哪些字段。
 *   渲染时按 schema.fields 逐个取。
 */
export type DynRecord = {
  id: string
  owner_id: string
  /**
   * 创建人显示名
   *
   * ⚠️ **不是物理列** —— 物理表上只有 owner_id,这个显示名是后端读的时候
   *   从 sys_user 补的(口径同配置中心的"操作人":真实姓名 → 昵称 → 账号)。
   *   取不到时是空串,界面回落到显示 owner_id。
   */
  owner_name: string
  created_at: string
  updated_at: string
  /**
   * 附件的展示信息(**键是字段标识**,不是文件 key)
   *
   * ⚠️ **没有附件的记录里这个键不存在** —— 后端只在真有附件时才放它。
   */
  _files?: Record<string, DynFileRef>
  /**
   * relation 字段的显示标签(**键是字段标识**)(M5)
   *
   * ⚠️ **关联字段值本身是目标记录的 id(字符串形态)** ——
   *   界面上显示一串数字没人认得;后端读的时候批量现查显示字段补到 `_rels`。
   *   没有关联值 / 标签取不到(目标记录被删)时这个键不给 ⇒ 界面回落显示 id。
   */
  _rels?: Record<string, string>
  /** 审批概要(集合绑定了审批模板且这条记录送审过时才有;2026-11-03) */
  _approval?: { instance_id: string; status: string }
} & Record<string, unknown>

/** 记录列表的查询条件 */
export interface DynRecordQuery {
  /** 关键字(在文本类字段上模糊匹配) */
  keyword?: string
  /** 归属人过滤 */
  owner?: string
  /** 排序字段(字段标识 / id / created_at / updated_at) */
  order_by?: string
  /** 排序方向 */
  order_dir?: 'asc' | 'desc'
  /**
   * 回收站视图:只看软删的记录
   *
   * ⚠️ **恢复动作的入口** —— 软删的记录从普通列表里消失了,
   *   没有这个开关,恢复按钮就永远没有落点。
   */
  trash?: boolean
  /**
   * 跳过 COUNT —— total 返 -1(M3,对标 PocketBase)
   *
   * ⚠️ **大表上 COUNT 是列表接口最贵的一条** ——
   *   不需要渲染页码(无限滚动)时跳过它,首屏快一倍。
   *   total = -1 表示"没统计",**不要**拿它渲染页码。
   */
  skip_total?: boolean
  /** 响应裁剪:只返回这些用户字段(逗号分隔的字段标识;空 = 全部)(M3) */
  fields?: string
  /**
   * 按字段多值筛选(M3):键 = 字段标识,值 = 候选清单(命中任意一个即入选)
   *
   * ⚠️ **白名单类型才开放**(text/longtext/select/int/number/bool/date/datetime/relation)——
   *   multiselect 落库是 JSON 文本,IN 精确匹配语义是错的,后端会拒。
   */
  filters?: Record<string, string[]>
  /**
   * 筛选表达式(M4,对标 PocketBase fexpr)
   *
   * ⚠️ **与 filters 是 AND 关系,不是二选一** ——
   *   语法:`status = 'active' && amount > 1000`、`no ?= ('a','b')`、
   *   `title ~ '验收'`、`due = null`。
   *   字段不存在 / 类型不匹配后端直接报错(静默忽略 = 返回全表,更危险)。
   */
  filter?: string
}

/** CSV 导入错误报告里的一条(后端 DynImportError) */
export interface DynImportErrorItem {
  /** 数据行号(第 1 行数据 = 1;表头的问题记 0) */
  row: number
  /** 出错的字段标识(表头问题 = 表头原文) */
  field: string
  /** 错误说明 */
  message: string
}

/** CSV 导入结果(后端 DynImportResult) */
export interface DynImportResult {
  total: number
  inserted: number
  skipped: number
  errors: DynImportErrorItem[]
}
