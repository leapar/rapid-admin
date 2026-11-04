// 动态业务集合 —— 用户在「业务表单设计器」里设计出来的业务表
//
// # 这两张表存的是什么
//
//	sys_dyn_collection —— 集合(≈ 一张业务表的"表头"):编码 / 名称 / 物理表名 / 行级权限
//	sys_dyn_field     —— 字段(≈ "表头"下面的一行):字段标识 / 中文名 / 物理列名 / 类型
//
// ⚠️ **字段定义存成行,不是塞进集合那一行的 JSON 字段。**
// 理由:字段要单独增删改、要单独排序、要单独建索引 ——
// 塞进 JSON 就得每次读写都解一遍 JSON 才能知道有哪些列,
// 而"加一个字段"变成"改一行 JSON",并发写还会互相覆盖。
//
// # 与 PocketBase 的差别(有意为之)
//
// PocketBase 把字段定义放在 `_collections` 表的一个 `fields` JSON 列里。
// 本仓不这么做的理由是**字段是独立实体**:它有自己的软删除标记
// (字段删了列还在,数据不丢)、自己的排序、自己的唯一键。
// 详见 internal/service/dyn_impl.go 里"字段删除 = 软删"的注释。
package model

// SysDynCollection 动态业务集合
type SysDynCollection struct {
	BaseModel
	// Code 集合编码(小写英文标识符)
	//
	// ⚠️ **不可修改** —— 它同时是物理表名(`dyn_<code>`)和资源码
	// (`dyn:<code>:list`)的来源,改了等于换了一张表 + 换了一套权限。
	Code string `gorm:"size:64;uniqueIndex;not null" json:"code"`
	// Name 集合名称(中文,可改)
	Name        string `gorm:"size:128;not null" json:"name"`
	Description string `gorm:"size:255" json:"description"`
	// PhysTable 物理表名
	//
	// ⚠️ 建表那一刻定死,之后改集合名**不动表** ——
	// 改表名意味着一次数据搬迁,而这套动态表的收益恰恰是"改个名不影响数据"。
	PhysTable string `gorm:"size:64;not null" json:"phys_table"`
	// OwnerScoped 行级权限:只看得见自己创建的记录
	//
	// ⚠️ **超管不受此限制** —— 超管在其它模块里本来就绕过一切,
	// 这里保持一致(否则超管建的表自己都看不到数据)。
	OwnerScoped bool `gorm:"default:false" json:"owner_scoped"`
	// PublicRead 公开只读(2026-11-03):未登录可经 /rapid-admin/v1/public/dyn/:collection/**
	// 读 schema / list / getOne;写路径永远要登录。
	//
	// ⚠️ **与 owner_scoped 互斥** —— 匿名调用者没有 owner,
	// 公开了要么全裸要么全空,语义就是错的;
	// service 建 / 改集合时校验,设计器 UI 同步禁掉组合。
	PublicRead bool   `gorm:"default:false" json:"public_read"`
	Icon       string `gorm:"size:64" json:"icon"`
	Sort       int    `gorm:"default:0" json:"sort"`
	IsActive   bool   `gorm:"default:true;index" json:"is_active"`
	// ApprovalTemplateID 绑定的审批模板(2026-11-03;0 = 未绑定)
	//
	// ⚠️ **绑定的是模板 id,不是 code** —— code 改名(软删让坑)后 id 不变,
	// 引用不断。送审时按 id 取模板再拿它的 code 显式传引擎
	// (同一 biz_type 有多条启用模板时引擎拒绝猜,见 approval_engine_impl 的
	// pickTemplate)。
	//
	// ⚠️ **审批结果不回写业务表** —— dyn 记录的数据本来就已经在表里,
	// 审批状态以 sys_approval_instance 为准(记录列表按 (biz_type,biz_id)
	// 连出来)。要"通过后把某字段写成某值"的回写语义,留给后续版本设计
	// 字段级契约,不拍脑袋定。
	ApprovalTemplateID uint64 `gorm:"default:0" json:"approval_template_id,string"`
	// Indexes 用户自定义索引定义(JSON 文本,`[]IndexDef`;空 = 没配)
	//
	// ⚠️ **存的是定义,不是 DDL 原文**(对标 PocketBase 的 Indexes []string 时有意改掉)——
	// PB 存 CREATE INDEX 原文等于把 SQL 拼接面交给管理员;这里存结构化定义,
	// 列必须是「在用字段的标识」,由 dynx.ParseIndexes + 服务层双重校验。
	// DDL 的重建(先 drop 后 create)见 dyn_impl.go 的 syncCollectionIndexes。
	Indexes string `gorm:"type:text" json:"indexes"`
}

// TableName 表名
func (SysDynCollection) TableName() string { return "sys_dyn_collection" }

// SysDynField 动态集合的字段定义
type SysDynField struct {
	BaseModel
	// CollectionID 所属集合
	//
	// ⚠️ 与 FieldKey 组成唯一索引 —— 同一个集合里字段标识不能重复。
	CollectionID uint64 `gorm:"index;uniqueIndex:uk_dyn_field;not null" json:"collection_id,string"`
	// FieldKey 字段标识(小写英文,用户起的名字)
	//
	// ⚠️ **不可修改** —— 它是物理列名(`f_<key>`)和前端 JSON 键的来源。
	// 想改显示名改 Label,想改存储格式只能新建字段 + 迁移数据。
	FieldKey string `gorm:"size:64;uniqueIndex:uk_dyn_field;not null" json:"field_key"`
	// Label 中文显示名(可改)
	Label string `gorm:"size:128;not null" json:"label"`
	// Column 物理列名(`f_<field_key>`)
	Column string `gorm:"size:64;not null" json:"column"`
	// Type 字段类型(见 internal/pkg/dynx)
	Type string `gorm:"size:32;not null" json:"type"`
	// Required 是否必填
	Required bool `gorm:"default:false" json:"required"`
	// Options select / multiselect 的选项集(JSON 文本)
	Options string `gorm:"type:text" json:"options"`
	// DefaultValue 默认值(文本形态,写入时按类型归一化)
	DefaultValue string `gorm:"size:255" json:"default_value"`
	Sort         int    `gorm:"default:0" json:"sort"`
	// IsActive 字段是否在用
	//
	// ⚠️ **停用的字段列还留在物理表里** —— 见 dyn_impl.go 的
	// 「字段删除 = 软删」注释:不 DROP COLUMN 是为了不丢数据。
	IsActive bool `gorm:"default:true;index" json:"is_active"`
}

// TableName 表名
func (SysDynField) TableName() string { return "sys_dyn_field" }
