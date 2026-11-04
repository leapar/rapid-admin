// DynService 动态业务集合(用户设计的业务表)
//
// # 这个 service 覆盖两件事
//
//	① **设计面**:集合与字段的增删改(建表 / 加列)
//	② **数据面**:某个集合的记录的增删改查(通用 CRUD)
//
// ⚠️ **两件事在同一个 service 里,是因为它们共用同一份"字段定义"** ——
// 数据面的校验、归一化、列映射全都要读字段定义,
// 拆成两个 service 就得让其中一个去查另一个的私有方法。
//
// # 资源码
//
// 每个集合自带五个资源码:
//
//	dyn:<code>:list / read / create / update / delete
//
// 外加一个菜单资源 `menu.dyn.<code>`(路径 `/dyn/<code>`)。
// ⚠️ **路由是静态的** —— 不为每个集合生成一条路由,
// 而是一条带 `:collection` 路径参数的通用路由,请求进来再按 code 查集合。
// 理由见 dyn_impl.go 的「为什么不为每个集合生成路由」。
package service

import (
	"context"
	"io"

	"rapid-admin/internal/model"
)

// DynCollectionListQuery 集合列表查询
type DynCollectionListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"`
}

// DynCollectionInput 新建集合
type DynCollectionInput struct {
	// Code 集合编码(小写英文标识符,不可改)
	Code string `json:"code"`
	// Name 集合名称(中文)
	Name string `json:"name"`
	// Description 描述
	Description string `json:"description"`
	// OwnerScoped 行级权限:只看得见自己创建的记录
	OwnerScoped bool `json:"owner_scoped"`
	// PublicRead 公开只读:未登录可读(与 OwnerScoped 互斥,见 model 注释)
	PublicRead bool `json:"public_read"`
	// Icon 菜单图标
	Icon string `json:"icon"`
	// Sort 菜单排序
	Sort int `json:"sort"`
	// ApprovalTemplateID 绑定的审批模板 id(0 = 不绑定;2026-11-03)
	ApprovalTemplateID uint64 `json:"approval_template_id,string"`
}

// DynCollectionUpdate 改集合
//
// ⚠️ **没有 Code 字段** —— 编码不可改(改了等于换表 + 换权限)。
type DynCollectionUpdate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// OwnerScoped 用指针:分不清"没传"和"传了 false"
	OwnerScoped *bool `json:"owner_scoped"`
	// PublicRead 用指针:同上(公开只读,与 owner_scoped 互斥)
	PublicRead *bool  `json:"public_read"`
	Icon       string `json:"icon"`
	Sort       int    `json:"sort"`
	IsActive   *bool  `json:"is_active"`
	// ApprovalTemplateID 用指针:分不清"没传"和"传 0(解绑)"(2026-11-03)
	ApprovalTemplateID *uint64 `json:"approval_template_id,string"`
	// Indexes 索引定义(JSON 文本)—— 指针:分不清"没传"和"传空串(清空)"。
	//
	// ⚠️ **只在改集合的入口生效,新建集合不收** —— 新集合还没有任何字段,
	// 索引引用的列必然不存在。改动会同步物理 DDL(先 drop 后 create),
	// 见 dyn_impl.go 的 syncCollectionIndexes。
	Indexes *string `json:"indexes"`
}

// DynFieldInput 新建字段
//
// ⚠️ **没有 FieldKey 的修改入口** —— 字段标识不可改(它是物理列名)。
type DynFieldInput struct {
	// FieldKey 字段标识(小写英文)
	FieldKey string `json:"field_key"`
	// Label 中文显示名
	Label string `json:"label"`
	// Type 字段类型(见 internal/pkg/dynx)
	Type string `json:"type"`
	// Required 是否必填
	Required bool `json:"required"`
	// Options select / multiselect 的选项集(JSON 文本)
	Options string `json:"options"`
	// DefaultValue 默认值(文本形态)
	DefaultValue string `json:"default_value"`
	// Sort 排序
	Sort int `json:"sort"`
}

// DynFieldUpdate 改字段
//
// ⚠️ **没有 Type 字段** —— 字段类型不可改。
// 改类型意味着改列类型(MySQL 要 MODIFY / SQLite 要重建表),
// 而重建表 = 全量数据搬迁 + 途中失败留半张表。
// ⇒ 想换类型:新建一个字段,把数据搬过去,再停用旧字段。
type DynFieldUpdate struct {
	Label        string `json:"label"`
	Required     bool   `json:"required"`
	Options      string `json:"options"`
	DefaultValue string `json:"default_value"`
	Sort         int    `json:"sort"`
	IsActive     *bool  `json:"is_active"`
}

// DynRecordQuery 记录列表查询
type DynRecordQuery struct {
	model.PageRequest
	// Keyword 关键字(在文本类字段上模糊匹配)
	Keyword string `json:"keyword"`
	// Owner 归属人过滤(owner_id)
	Owner string `json:"owner"`
	// Trash 回收站视图:只看软删的记录
	//
	// ⚠️ **恢复动作的入口** —— 软删的记录从普通列表里消失了,
	// 不给一个"只看已删"的查询开关,恢复按钮就永远没有落点。
	Trash bool `json:"trash"`
	// SkipTotal 跳过 COUNT(M3,对标 PocketBase)
	//
	// ⚠️ **total 返 -1 表示"没统计"** ——
	// COUNT 在大表上是列表接口最贵的一条,而"总共多少条"只有第一页有用;
	// 调用方明确不需要总数时(如无限滚动),跳过它首屏能快一倍。
	// 前端拿到 -1 时**不要**拿它渲染页码(antd Pagination 传 -1 会算出负页数)。
	SkipTotal bool `json:"skip_total"`
	// Fields 响应裁剪:只返回这些用户字段(逗号分隔的字段标识;空 = 全部)(M3)
	//
	// ⚠️ **是响应裁剪,不是 SELECT 裁剪** —— 序列化后按字段摘键,零 SQL 侵入;
	// 引用了不存在的字段直接报错(同"不认识的字段名直接报错"的仓口径,
	// 静默忽略的话调用方拿到的列比它以为的少,还不报错)。
	Fields string `json:"fields"`
	// Filters 按字段多值筛选(M3):键 = 字段标识,值 = 多个候选(命中任意一个即入选)
	//
	// ⚠️ **白名单之外的类型不开放**(json / file / multiselect)——
	// multiselect 落库是 JSON 文本,IN 精确匹配的语义是错的
	// (要的是"包含",不是"等于"),错误的语义比没有语义危险。
	Filters map[string][]string `json:"filters"`
	// Filter 筛选表达式(M4,对标 PocketBase fexpr)
	//
	// ⚠️ **与 Filters 是 AND 关系,不是二选一** ——
	// 表达式管复杂条件(逻辑组合/比较/包含),f_ 参数管界面上的点选多值;
	// 两个通道都开着时各自成为一条 AND 条件。
	// 语法与安全边界见 internal/pkg/dynfilter。
	Filter string `json:"filter"`
}

// DynBatchUpdateRequest 批量修改单字段的请求体
//
// ⚠️ **ids 是字符串数组**(同批量删除);Value 用 any —— 前端按字段类型发
// number / boolean / string,归一化统一在后端 normalizeField 链做。
type DynBatchUpdateRequest struct {
	// IDs 记录 id(字符串形态,19 位 uint64)
	IDs []string `json:"ids"`
	// Field 字段标识
	Field string `json:"field"`
	// Value 统一改成的新值
	Value any `json:"value"`
}

// DynService 动态业务集合
type DynService interface {
	// ---- 设计面 ----

	// ListCollections 集合列表
	ListCollections(ctx context.Context, q DynCollectionListQuery) (*model.PageResponse, error)
	// GetCollection 集合详情
	GetCollection(ctx context.Context, id uint64) (*model.SysDynCollection, error)
	// GetCollectionByCode 按编码取集合
	GetCollectionByCode(ctx context.Context, code string) (*model.SysDynCollection, error)
	// CreateCollection 新建集合(建表 + 建资源 + 授权)
	CreateCollection(ctx context.Context, caller *Caller, in *DynCollectionInput) (uint64, error)
	// UpdateCollection 改集合
	UpdateCollection(ctx context.Context, caller *Caller, id uint64, in *DynCollectionUpdate) error
	// DeleteCollection 删集合(软删 + 停用资源,物理表保留)
	DeleteCollection(ctx context.Context, caller *Caller, id uint64) error

	// ListFields 字段列表
	ListFields(ctx context.Context, collectionID uint64) ([]model.SysDynField, error)
	// CreateField 新建字段(加列)
	CreateField(ctx context.Context, caller *Caller, collectionID uint64, in *DynFieldInput) (uint64, error)
	// UpdateField 改字段
	UpdateField(ctx context.Context, caller *Caller, fieldID uint64, in *DynFieldUpdate) error
	// DeleteField 删字段(软删,列保留)
	DeleteField(ctx context.Context, caller *Caller, fieldID uint64) error

	// ---- 数据面 ----

	// ListRecords 记录列表
	ListRecords(ctx context.Context, caller *Caller, code string, q DynRecordQuery) (*model.PageResponse, error)
	// GetRecord 记录详情
	GetRecord(ctx context.Context, caller *Caller, code string, id uint64) (map[string]any, error)

	// ---- 数据面 · 公开只读(2026-11-03)----
	//
	// 集合 public_read = true 且非 owner_scoped 时,未登录可读;
	// 写路径没有公开版 —— 匿名可写是事故,不是特性。

	// PublicReadCollection 公开只读闸门:集合在用 + public_read + 非 owner_scoped。
	// 不满足一律按未登录(401)处理 —— 不区分"集合不存在"和"不能公开读",
	// 不给枚举探测留信号。
	PublicReadCollection(ctx context.Context, code string) (*model.SysDynCollection, error)
	// PublicListRecords 公开只读列表(参数与 ListRecords 同一套,含筛选 DSL)
	PublicListRecords(ctx context.Context, code string, q DynRecordQuery) (*model.PageResponse, error)
	// PublicGetRecord 公开只读详情
	PublicGetRecord(ctx context.Context, code string, id uint64) (map[string]any, error)

	// SubmitRecordApproval 把一条记录送进审批流(2026-11-03)
	//
	// 前置:集合绑定了审批模板(approval_template_id)、记录存在且 caller 看得见。
	// 建草稿 + 提交一步走(见 dyn_approval.go 文件头 ③);返回审批单 id。
	SubmitRecordApproval(ctx context.Context, caller *Caller, code string, recordID uint64) (uint64, error)
	// CreateRecord 新建记录
	CreateRecord(ctx context.Context, caller *Caller, code string, payload map[string]any) (uint64, error)
	// UpdateRecord 改记录
	UpdateRecord(ctx context.Context, caller *Caller, code string, id uint64, payload map[string]any) error
	// DeleteRecord 删记录(软删)
	DeleteRecord(ctx context.Context, caller *Caller, code string, id uint64) error

	// RestoreRecord 恢复一条软删的记录(回收站)
	RestoreRecord(ctx context.Context, caller *Caller, code string, id uint64) error
	// BatchDeleteRecords 批量软删,返回实际删掉的行数
	//
	// ⚠️ ids 是**字符串数组** —— 前端手里的 id 本来就是 string(19 位 uint64),
	// 后端解析 + 校验,不为图省事让前端先转数字(会丢精度)。
	BatchDeleteRecords(ctx context.Context, caller *Caller, code string, ids []string) (int64, error)
	// BatchUpdateRecords 批量修改单字段,返回实际改掉的行数
	BatchUpdateRecords(ctx context.Context, caller *Caller, code string, ids []string, field string, value any) (int64, error)
	// ExportRecords 按当前筛选导出全量 CSV(写入 w)
	ExportRecords(ctx context.Context, caller *Caller, code string, q DynRecordQuery, w io.Writer) error
	// ImportRecords 导入 CSV,返回错误行报告
	ImportRecords(ctx context.Context, caller *Caller, code string, data []byte, mode string) (*DynImportResult, error)

	// EnsureBuiltinCollections 启动期幂等播种内置业务集合(Phase 2a:房屋台账 / 公告管理)
	//
	// ⚠️ **只播种缺的,不覆盖已有的** —— 集合编码撞上(运营自己建过/上次播过)
	// 就整个定义跳过;字段按 (collection, field_key) 逐个补,防半截播种。
	// 设计决策与"明确不做"清单见 dyn_builtin.go 文件头。
	EnsureBuiltinCollections(ctx context.Context) error

	// ResolveFieldOptions 解析一组字段的选项集(手工选项集 or 关联系统表)
	//
	// ⚠️ **readable 是调用方(handler)给的权限判定结果** ——
	// key 是来源 key(user / role / dept),值是"当前调用方能不能读那张表"。
	// 判定走 middleware.CanAccess,在 handler 层做:
	// `internal/api/middleware` 依赖 `internal/service`,反过来 import 就是循环依赖。
	//
	// ⚠️ **任何一个字段解析失败都不拖垮其它字段** ——
	// 一个字段的选项读不出来,那个字段的选项是空的,其余字段照常返回。
	ResolveFieldOptions(
		ctx context.Context,
		caller *Caller,
		fields []model.SysDynField,
		readable map[string]bool,
	) (map[string]DynOptionSet, error)
}
