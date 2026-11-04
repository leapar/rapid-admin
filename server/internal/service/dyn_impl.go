// DynService 真实实现 —— 动态业务集合(设计面)
//
// # 为什么不为每个集合生成一条路由
//
// PocketBase 的做法是**一条带 `{collection}` 路径参数的通用路由**,
// 请求进来再按名字查集合。本仓照抄这一条,理由有三:
//
//  1. **路由表是静态的** —— 路由在启动时注册,集合是运行时建的。
//     如果每个集合注册一条路由,就得在运行时改 Hertz 的路由树 ——
//     而"运行时改路由表"意味着并发读路由表这件事要加锁。
//  2. **权限码在注册期是未知的** —— 资源码 `dyn:<code>:list` 里的
//     `<code>` 要等请求进来才知道,`rbac()` 闭包(启动期记账 +
//     启动期一致性检查)用不了。
//  3. **启动期一致性检查不用改** —— 一条静态路由 = 一次 rbac 记账,
//     而动态集合走的是 `middleware.CanAccess`(见 handler 层)。
//
// # 物理表长什么样
//
//	dyn_<code>(
//	  id         BIGINT     NOT NULL,          -- sonyflake
//	  owner_id   BIGINT     NOT NULL DEFAULT 0,-- 行级权限用
//	  created_at VARCHAR(32) NOT NULL DEFAULT '',
//	  updated_at VARCHAR(32) NOT NULL DEFAULT '',
//	  deleted_at VARCHAR(32) NULL,
//	  f_<key>    <类型列>   NOT NULL DEFAULT <零值>,
//	  PRIMARY KEY (id)
//	)
//
// ⚠️ **时间列存字符串** —— 见 internal/pkg/dynx 的包注释:
// MySQL 与 SQLite 的 time.Time 存取行为不一样,统一存 ISO-8601 字符串。
//
// ⚠️ **用户字段一律带 `f_` 前缀** —— 保证不可能跟系统列撞名。
// (撞名的后果不是"报错",而是用户的字段悄悄写进了系统列。)
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

// 物理表上的系统列名
const (
	dynColID        = "id"
	dynColOwnerID   = "owner_id"
	dynColCreatedAt = "created_at"
	dynColUpdatedAt = "updated_at"
	dynColDeletedAt = "deleted_at"
)

// dynSysCols 系统列(顺序即建表顺序)
var dynSysCols = []string{dynColID, dynColOwnerID, dynColCreatedAt, dynColUpdatedAt, dynColDeletedAt}

// dynColTypes 系统列的 DDL 类型(按方言)
//
// ⚠️ MySQL 的 BIGINT 是**有符号**的 —— sonyflake 的 ID 是 uint64,
// 但它的量级(毫秒时间戳左移 16 位 ≈ 1.2e15)远小于 int64 上限(9.2e18),
// 有符号完全够用。有符号的好处是 SQLite 的 INTEGER(有符号 64 位)
// 跟它**完全对齐** —— 无符号 BIGINT 在 SQLite 里会溢出成浮点数,精度就没了。
var dynColTypes = map[string]map[string]string{
	"mysql": {
		dynColID:        "BIGINT",
		dynColOwnerID:   "BIGINT",
		dynColCreatedAt: "VARCHAR(32)",
		dynColUpdatedAt: "VARCHAR(32)",
		dynColDeletedAt: "VARCHAR(32)",
	},
	"sqlite": {
		dynColID:        "INTEGER",
		dynColOwnerID:   "INTEGER",
		dynColCreatedAt: "TEXT",
		dynColUpdatedAt: "TEXT",
		dynColDeletedAt: "TEXT",
	},
}

// dynService 真实实现
type dynService struct {
	db *gorm.DB
	// scopeSvc 数据范围(行级权限)服务 —— M160
	//
	// ⚠️ **允许为 nil** —— 没装配 scope 服务时不过滤,
	// 与 scope_impl.go 的 applyScope helper 同一套兜底(单测 / 精简装配)。
	scopeSvc ScopeService
	// files 文件服务(附件字段用)—— M169
	//
	// ⚠️ **允许为 nil** —— 没装文件服务时附件字段的值照常读写,
	// 只是没有"原文件名 + 直链"的展示信息(记录视图里没有 `_files`)。
	files FileResolver
	// engine 通用审批引擎(集合送审用)—— 2026-11-03
	//
	// ⚠️ **允许为 nil**(单测 / 精简装配)——
	// 送审入口发现 nil 时报"未装配审批引擎",其余功能全部照常。
	engine ApprovalEngine
}

// FileResolver 文件解析能力(附件字段要的那一小块)
//
// ⚠️ **窄接口,不是整个 FileService** ——
// dyn 只需要"按 key 解析出名字与直链"这一件事;
// 声明成窄接口,单测就能塞个假实现,不必把整个文件系统装起来。
type FileResolver interface {
	// ResolveKeys 批量把文件 key 解析成「原文件名 + 直链」
	ResolveKeys(ctx context.Context, keys []string) (map[string]FileRef, error)
}

// NewDynService 构造
//
// ⚠️ **scopeSvc 传 nil 是合法的**(没有装数据范围服务)——
// 退化成"不过滤",**不改变函数的成功/失败语义**(与 cachex 全体一致)。
//
// ⚠️ **files 传 nil 也是合法的** ——
// 附件字段的值照常读写,只是记录视图里没有 `_files`(没有展示信息)。
//
// ⚠️ **engine 传 nil 也是合法的** —— 送审入口会报"未装配审批引擎"。
func NewDynService(db *gorm.DB, scopeSvc ScopeService, files FileResolver, engine ApprovalEngine) DynService {
	return &dynService{db: db, scopeSvc: scopeSvc, files: files, engine: engine}
}

// dialect 当前 DB 方言
func (s *dynService) dialect() string {
	if s.db == nil || s.db.Dialector == nil {
		return "sqlite"
	}
	return s.db.Dialector.Name()
}

// ---------------------------------------------------------------------------
// 集合
// ---------------------------------------------------------------------------

// ListCollections 集合列表
func (s *dynService) ListCollections(ctx context.Context, q DynCollectionListQuery) (*model.PageResponse, error) {
	page, size := normPage(q.Page, q.Size, 200)
	tx := s.db.WithContext(ctx).Model(&model.SysDynCollection{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("name LIKE ? OR code LIKE ? OR description LIKE ?", like, like, like)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询集合数失败").WithDetail(err.Error())
	}
	var rows []model.SysDynCollection
	if err := tx.Order("sort ASC, id ASC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询集合失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

// GetCollection 集合详情
func (s *dynService) GetCollection(ctx context.Context, id uint64) (*model.SysDynCollection, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var c model.SysDynCollection
	if err := s.db.WithContext(ctx).First(&c, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "集合不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询集合失败").WithDetail(err.Error())
	}
	return &c, nil
}

// GetCollectionByCode 按编码取集合
func (s *dynService) GetCollectionByCode(ctx context.Context, code string) (*model.SysDynCollection, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errcode.New(errcode.BadRequest, "collection 不能为空")
	}
	var c model.SysDynCollection
	if err := s.db.WithContext(ctx).Where("code = ?", code).First(&c).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errcode.New(errcode.NotFound, "集合不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询集合失败").WithDetail(err.Error())
	}
	return &c, nil
}

// PublicReadCollection 公开只读闸门(公开 schema 端点用):
// 集合存在(软删被默认 scope 排除)+ 在用 + public_read + 非 owner_scoped。
// 不满足一律按未登录(401)处理 —— 不区分"集合不存在"和"不能公开读",
// 不给枚举探测留信号。
func (s *dynService) PublicReadCollection(ctx context.Context, code string) (*model.SysDynCollection, error) {
	col, err := s.GetCollectionByCode(ctx, code)
	if err != nil {
		// ⚠️ 不存在的集合也按未登录(401)处理,不透传 404 ——
		// 404 vs 401 的区别会让探测者区分出"编码存在但没公开"。
		var ec *errcode.Error
		if errors.As(err, &ec) && ec.Code == errcode.NotFound {
			return nil, errcode.New(errcode.Unauthorized, "未登录")
		}
		return nil, err
	}
	if !col.IsActive || !col.PublicRead || col.OwnerScoped {
		return nil, errcode.New(errcode.Unauthorized, "未登录")
	}
	return col, nil
}

// CreateCollection 新建集合
//
// ⚠️ **建表与登记不在一个事务里** —— MySQL 的 DDL 会触发隐式提交,
// 把 CREATE TABLE 放进 GORM 事务等于"事务其实已经提交了",
// 后面登记失败时回滚不了,留下一张没人认领的野表。
//
// ⇒ 手工补偿:登记失败就把刚建的表删掉。
func (s *dynService) CreateCollection(ctx context.Context, caller *Caller, in *DynCollectionInput) (uint64, error) {
	if _, err := requireUser(caller); err != nil {
		return 0, err
	}
	return s.createCollectionCore(ctx, caller.RoleCode, in)
}

// createCollectionCore 建集合的主体(鉴权之后的部分)
//
// ⚠️ **拆出来是给内置集合播种用的**(dyn_builtin.go)——
// 播种发生在启动期,没有登录用户,不能走 requireUser;
// 而建表 → 落集合行 → 登记资源 → 失败逐级补偿这套顺序**只有一份**,
// 播种自己再抄一遍必然漂移(哪天补偿顺序改了,播种路径漏改)。
// bindRole = 资源绑给哪个角色(在线创建时是创建者当前角色,播种时是业务角色)。
func (s *dynService) createCollectionCore(ctx context.Context, bindRole string, in *DynCollectionInput) (uint64, error) {
	if in == nil {
		return 0, errcode.New(errcode.BadRequest, "请求体不能为空")
	}
	code := strings.TrimSpace(in.Code)
	if err := dynx.ValidateCode(code); err != nil {
		return 0, errcode.New(errcode.BadRequest, err.Error())
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return 0, errcode.New(errcode.BadRequest, "name 不能为空")
	}
	// 公开只读与拥有者隔离互斥(2026-11-03)—— 匿名调用者没有 owner,
	// 两个都开要么全裸要么全空,语义就是错的;当场报错,别存进去。
	if in.OwnerScoped && in.PublicRead {
		return 0, errcode.New(errcode.BadRequest, "公开只读与「只看自己创建的记录」互斥,只能选一个")
	}
	var dup int64
	if err := s.db.WithContext(ctx).Model(&model.SysDynCollection{}).
		Where("code = ?", code).Count(&dup).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "查询集合失败").WithDetail(err.Error())
	}
	if dup > 0 {
		return 0, errcode.New(errcode.Conflict, "集合编码已存在")
	}

	phys := dynx.PhysTableName(code)
	if err := s.createTable(ctx, phys, nil); err != nil {
		return 0, errcode.New(errcode.Internal, "创建物理表失败").WithDetail(err.Error())
	}

	// 审批模板绑定(2026-11-03)—— 建表之后、落集合行之前校验,
	// 不合法直接报错,别把坏绑定存进去
	if err := s.validateApprovalTemplate(ctx, in.ApprovalTemplateID); err != nil {
		s.dropTable(ctx, phys)
		return 0, err
	}

	col := &model.SysDynCollection{
		BaseModel:   model.BaseModel{ID: sonyflake.Next()},
		Code:        code,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		PhysTable:   phys,
		OwnerScoped: in.OwnerScoped,
		PublicRead:  in.PublicRead,
		Icon:        strings.TrimSpace(in.Icon),
		Sort:        in.Sort,
		IsActive:    true,

		ApprovalTemplateID: in.ApprovalTemplateID,
	}
	if err := s.db.WithContext(ctx).Create(col).Error; err != nil {
		s.dropTable(ctx, phys)
		return 0, errcode.New(errcode.Internal, "创建集合失败").WithDetail(err.Error())
	}
	// 资源 + 授权失败也要把刚建的东西撤掉 —— 否则留下一张没有权限的表
	if err := s.registerCollectionResources(ctx, bindRole, col); err != nil {
		s.dropTable(ctx, phys)
		s.db.WithContext(ctx).Delete(col)
		return 0, err
	}
	return col.ID, nil
}

// UpdateCollection 改集合
func (s *dynService) UpdateCollection(ctx context.Context, caller *Caller, id uint64, in *DynCollectionUpdate) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	cur, err := s.GetCollection(ctx, id)
	if err != nil {
		return err
	}
	if in == nil {
		return errcode.New(errcode.BadRequest, "请求体不能为空")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errcode.New(errcode.BadRequest, "name 不能为空")
	}
	updates := map[string]any{
		"name":        name,
		"description": strings.TrimSpace(in.Description),
		"icon":        strings.TrimSpace(in.Icon),
		"sort":        in.Sort,
	}
	if in.OwnerScoped != nil {
		updates["owner_scoped"] = *in.OwnerScoped
	}
	if in.PublicRead != nil {
		updates["public_read"] = *in.PublicRead
	}
	// 公开只读与拥有者隔离互斥 —— 用**合并后的值**判
	// (只传一个开关时,另一个要取当前值,不能只看本次请求体)
	effectiveOwner := cur.OwnerScoped
	if in.OwnerScoped != nil {
		effectiveOwner = *in.OwnerScoped
	}
	effectivePublic := cur.PublicRead
	if in.PublicRead != nil {
		effectivePublic = *in.PublicRead
	}
	if effectiveOwner && effectivePublic {
		return errcode.New(errcode.BadRequest, "公开只读与「只看自己创建的记录」互斥,只能选一个")
	}
	if in.IsActive != nil {
		updates["is_active"] = *in.IsActive
	}
	// 审批模板绑定(2026-11-03)—— 指针语义:没传不动,传了(含 0 解绑)才改
	if in.ApprovalTemplateID != nil {
		if err := s.validateApprovalTemplate(ctx, *in.ApprovalTemplateID); err != nil {
			return err
		}
		updates["approval_template_id"] = *in.ApprovalTemplateID
	}
	// 索引配置(M2)—— 指针语义:没传不动,传了(含空串 = 全部清掉)才整体重建。
	// DDL 先行、行后写:DDL 失败时集合行里存的还是旧定义,两边不会出现
	// "行说有索引、库里没有"的静默漂移;DDL 成功而行写入失败的话,
	// 下一次保存会按行里的旧定义重新对账(先 create 后 drop),自愈。
	// ⚠️ 行里存的是**归一化后的 JSON**(解析再序列化),不是调用方的原文 ——
	// 否则同一份定义换种空格写法就算"改了",下次对账白跑一遍 DDL。
	if in.Indexes != nil {
		stored, err := s.syncCollectionIndexes(ctx, cur, *in.Indexes)
		if err != nil {
			return err
		}
		updates["indexes"] = stored
	}
	// ⚠️ **停用前先查引用** —— 放在写入之后查的话,拒绝时 is_active 已经翻了,
	// "报错但集合停用了"比"该停的停不了"更让人糊涂。
	if in.IsActive != nil && !*in.IsActive {
		refs, err := s.relationReferrers(ctx, cur.Code)
		if err != nil {
			return err
		}
		if len(refs) > 0 {
			return errcode.New(errcode.BadRequest,
				"集合被其它集合的关联字段引用,不能停用:"+dynReferrerSummary(refs))
		}
	}
	if err := s.db.WithContext(ctx).Model(&model.SysDynCollection{}).
		Where("id = ?", cur.ID).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新集合失败").WithDetail(err.Error())
	}
	// ⚠️ **改名 / 换图标后必须把菜单资源一起改** ——
	// 菜单资源(sys_resource)里存的是**建集合那一刻**的 name / icon / sort,
	// 不跟着改的话侧边栏显示的还是旧名字。
	cur.Name = name
	cur.Description = strings.TrimSpace(in.Description)
	cur.Icon = strings.TrimSpace(in.Icon)
	cur.Sort = in.Sort
	if in.OwnerScoped != nil {
		cur.OwnerScoped = *in.OwnerScoped
	}
	if in.PublicRead != nil {
		cur.PublicRead = *in.PublicRead
	}
	if in.IsActive != nil {
		cur.IsActive = *in.IsActive
	}
	// 停用集合 ⇒ 菜单也一起停用(否则侧边栏还点得进去,进去报 404)
	if in.IsActive != nil && !*in.IsActive {
		return s.disableCollectionResources(ctx, cur)
	}
	// 其余情况(改名 / 换图标 / 重新启用)⇒ 重新登记资源
	//
	// ⚠️ **启用走的是"重新登记"而不是"把 is_active 翻回 true"** ——
	// 登记顺带做了三件事:upsert 资源(名字/图标/排序跟上),
	// 把资源绑给操作者当前的角色(没绑 ⇒ 菜单不出现),同步 casbin 策略行。
	// 只翻 is_active 的话,菜单回来了但没人有权限点它。
	return s.registerCollectionResources(ctx, caller.RoleCode, cur)
}

// DeleteCollection 删集合
//
// ⚠️ **物理表保留** —— 删集合只是"把这个业务下线":
// 菜单消失、资源停用、集合记录软删,但 dyn_<code> 这张表和数据还在。
//
// 为什么不 DROP TABLE:数据是用户录入的业务数据,不是设计期的草稿 ——
// "删错了还能救回来"比"删干净"重要。
// (真要清空间,让运维手工 DROP TABLE dyn_xxx。)
//
// ⚠️ **被其它集合的关联字段引用中的集合不可删** ——
// 删了目标集合,引用方的下拉框整个变空、存量记录变成悬空引用。
// 挡下来并报出引用方,让管理员先处理引用字段(见 relationReferrers)。
func (s *dynService) DeleteCollection(ctx context.Context, caller *Caller, id uint64) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	cur, err := s.GetCollection(ctx, id)
	if err != nil {
		return err
	}
	refs, err := s.relationReferrers(ctx, cur.Code)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		return errcode.New(errcode.BadRequest,
			"集合被其它集合的关联字段引用,不能删除:"+dynReferrerSummary(refs))
	}
	if err := s.db.WithContext(ctx).Model(&model.SysDynCollection{}).
		Where("id = ?", cur.ID).Updates(map[string]any{"is_active": false}).Error; err != nil {
		return errcode.New(errcode.Internal, "删除集合失败").WithDetail(err.Error())
	}
	return s.disableCollectionResources(ctx, cur)
}

// dynRelationReferrer 一个引用了指定集合的 relation 字段
type dynRelationReferrer struct {
	// Code 引用方集合编码
	Code string
	// Name 引用方集合名(报错文案用)
	Name string
	// FieldLabel 字段显示名
	FieldLabel string
	// Column 物理列名
	Column string
}

// relationReferrers 扫出引用目标集合的全部在用 relation 字段
//
// ⚠️ **只看字段的 is_active,不看引用方集合的 is_active** ——
// 引用方集合被停用了,它的记录还躺着,重新启用后悬空引用立刻现形;
// 保护性检查宁严勿松。
//
// ⚠️ **选项集解析失败的字段跳过,不报错** ——
// 配置坏了应该在字段保存时报,不能反过来把删除/停用一起卡死。
func (s *dynService) relationReferrers(ctx context.Context, code string) ([]dynRelationReferrer, error) {
	var fields []model.SysDynField
	if err := s.db.WithContext(ctx).
		Where("type = ? AND is_active = ?", string(dynx.TypeRelation), true).
		Find(&fields).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询关联字段失败").WithDetail(err.Error())
	}
	ids := make([]uint64, 0, 2)
	matched := make([]dynRelationReferrer, 0, 2)
	for i := range fields {
		opts, err := dynx.ParseOptions(fields[i].Options)
		if err != nil || opts.Relation == nil || strings.TrimSpace(opts.Relation.Target) != code {
			continue
		}
		matched = append(matched, dynRelationReferrer{
			FieldLabel: fields[i].Label,
			Column:     fields[i].Column,
		})
		ids = append(ids, fields[i].CollectionID)
	}
	if len(matched) == 0 {
		return nil, nil
	}
	var cols []model.SysDynCollection
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&cols).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询引用方集合失败").WithDetail(err.Error())
	}
	byID := make(map[uint64]model.SysDynCollection, len(cols))
	for _, c := range cols {
		byID[c.ID] = c
	}
	// 集合行查不到的字段剔除(理论到不了:字段必然属于某个集合)
	out := make([]dynRelationReferrer, 0, len(matched))
	for i := range matched {
		if c, ok := byID[ids[i]]; ok {
			matched[i].Code = c.Code
			matched[i].Name = c.Name
			out = append(out, matched[i])
		}
	}
	return out, nil
}

// dynReferrerSummary 引用方清单 → 报错文案("《设备台账》所属设备、《工单》关联设备")
func dynReferrerSummary(refs []dynRelationReferrer) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		name := r.Name
		if name == "" {
			name = r.Code
		}
		parts = append(parts, "《"+name+"》"+r.FieldLabel)
	}
	return strings.Join(parts, "、")
}

// ---------------------------------------------------------------------------
// 字段
// ---------------------------------------------------------------------------

// ListFields 字段列表(按 sort 排序)
func (s *dynService) ListFields(ctx context.Context, collectionID uint64) ([]model.SysDynField, error) {
	if collectionID == 0 {
		return nil, errcode.New(errcode.BadRequest, "collectionId 不能为空")
	}
	var rows []model.SysDynField
	if err := s.db.WithContext(ctx).Where("collection_id = ?", collectionID).
		Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询字段失败").WithDetail(err.Error())
	}
	return rows, nil
}

// activeFields 取在用的字段
func (s *dynService) activeFields(ctx context.Context, collectionID uint64) ([]model.SysDynField, error) {
	var rows []model.SysDynField
	if err := s.db.WithContext(ctx).
		Where("collection_id = ? AND is_active = ?", collectionID, true).
		Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询字段失败").WithDetail(err.Error())
	}
	return rows, nil
}

// CreateField 新建字段(加列)
//
// ⚠️ **同标识的软删字段会被"复活",而不是新建一行** ——
// 唯一索引 (collection_id, field_key) 覆盖软删行,
// 直接插第二行会撞唯一键。
// 复活的前提是**类型一致**:类型不一致说明当初那张列的存储格式不同,
// 硬复用会把老数据读成新格式(比如把存过 "abc" 的 int 列当成 text 读)。
func (s *dynService) CreateField(ctx context.Context, caller *Caller, collectionID uint64, in *DynFieldInput) (uint64, error) {
	if _, err := requireUser(caller); err != nil {
		return 0, err
	}
	if in == nil {
		return 0, errcode.New(errcode.BadRequest, "请求体不能为空")
	}
	col, err := s.GetCollection(ctx, collectionID)
	if err != nil {
		return 0, err
	}
	return s.createFieldCore(ctx, col, in)
}

// createFieldCore 加字段的主体(鉴权与集合加载之后的部分)
//
// ⚠️ **拆出来是给内置集合播种用的**(dyn_builtin.go)——
// 启动期播种没有登录用户,走不了 requireUser;
// 而"先加列再登记字段、失败不补偿"这套顺序只有一份,别抄两遍。
func (s *dynService) createFieldCore(ctx context.Context, col *model.SysDynCollection, in *DynFieldInput) (uint64, error) {
	if in == nil {
		return 0, errcode.New(errcode.BadRequest, "请求体不能为空")
	}
	key, label, fieldType, err := validateFieldInput(in)
	if err != nil {
		return 0, err
	}
	// relation 字段的设计期配置校验(目标集合 + 显示字段)——
	// 放在 revive 分支之前,新建与复活两条路都过这一关
	if err := s.validateRelationConfig(ctx, fieldType, in.Options); err != nil {
		return 0, err
	}

	// 复用软删行?
	var existing model.SysDynField
	err = s.db.WithContext(ctx).
		Where("collection_id = ? AND field_key = ?", col.ID, key).First(&existing).Error
	if err == nil {
		return s.reviveField(ctx, &existing, in)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, errcode.New(errcode.Internal, "查询字段失败").WithDetail(err.Error())
	}

	field := &model.SysDynField{
		BaseModel:    model.BaseModel{ID: sonyflake.Next()},
		CollectionID: col.ID,
		FieldKey:     key,
		Label:        label,
		Column:       dynx.ColumnName(key),
		Type:         string(fieldType),
		Required:     in.Required,
		Options:      in.Options,
		DefaultValue: in.DefaultValue,
		Sort:         in.Sort,
		IsActive:     true,
	}
	// ⚠️ **先加列再登记字段** —— 反过来的话,登记成功但加列失败,
	// 就留下一条"字段存在但列不存在"的记录:
	// 之后的每次写入都会因为列缺失而报错,而错误信息指向的是数据不是设计。
	if err := s.addColumn(ctx, col.PhysTable, *field); err != nil {
		return 0, errcode.New(errcode.Internal, "添加物理列失败").WithDetail(err.Error())
	}
	if err := s.db.WithContext(ctx).Create(field).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建字段失败").WithDetail(err.Error())
	}
	return field.ID, nil
}

// validateFieldInput 校验字段定义(标识 / 显示名 / 类型 / 选项集 / 默认值)
//
// ⚠️ **选项集必须是合法 JSON 且选填/多选字段必须有选项** ——
// 选项集坏了的话,写入时 `dynx.Normalize` 会拿不到候选值,
// 表现是"新建字段成功,但存这个字段时报错",而错的是数据不是设计。
func validateFieldInput(in *DynFieldInput) (string, string, dynx.Type, error) {
	key := strings.TrimSpace(in.FieldKey)
	if err := dynx.ValidateFieldKey(key); err != nil {
		return "", "", "", errcode.New(errcode.BadRequest, err.Error())
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		return "", "", "", errcode.New(errcode.BadRequest, "label 不能为空")
	}
	fieldType := dynx.Type(strings.TrimSpace(in.Type))
	if err := dynx.ValidateType(fieldType); err != nil {
		return "", "", "", errcode.New(errcode.BadRequest, err.Error())
	}
	if err := validateFieldShape(fieldType, in.Options, in.DefaultValue); err != nil {
		return "", "", "", err
	}
	return key, label, fieldType, nil
}

// validateFieldShape 校验「选项集 + 默认值」这一对
//
// ⚠️ **单选/多选字段必须配置选项** ——
// 没选项的单选字段 = 任何字符串都能存进去,而列表页按选项集翻译显示名时
// 会显示成一个空白的标签(数据已经写进去了,查不出来)。
//
// ⚠️ **默认值必须在这里校验,不能等到写记录时才炸** ——
// 默认值是在"新建记录"时被填进去的,那时候用户已经填完一整张表了。
// 一个填错的默认值要等到某个人新建记录时才暴露,那时候已经没人记得
// 是谁填的、也不记得当时想填什么了。
//
// ⚠️ **校验走的是 dynx.Normalize,与写记录是同一套规则** ——
// 自己再写一套判断必然与写入侧漂移(写入侧后来加了一个类型,这边忘了加)。
func validateFieldShape(fieldType dynx.Type, optionsRaw, defaultValue string) error {
	opts, err := dynx.ParseOptions(optionsRaw)
	if err != nil {
		return errcode.New(errcode.BadRequest, err.Error())
	}
	spec, ok := dynx.Lookup(fieldType)
	if !ok {
		return errcode.New(errcode.BadRequest, "未知字段类型 "+string(fieldType))
	}
	if spec.HasOptions && !opts.HasChoices() && !opts.HasSource() {
		return errcode.New(errcode.BadRequest, "单选/多选字段必须配置选项(或关联一张系统表)")
	}
	// ⚠️ **来源的 key 必须在注册表里** ——
	// 不校验的话,设计器能存下一个后端不认识的来源 ⇒
	// 字段建好了,读的时候选项永远是空的,而**没有任何报错**。
	if opts.HasSource() {
		if err := dynx.ValidateSourceKey(opts.Source.Key); err != nil {
			return errcode.New(errcode.BadRequest, err.Error())
		}
	}
	// 值约束(M1):自洽性 + 类型适配。**默认值也要过约束** ——
	// 否则"默认值 200 + 上限 100"能存进去,约束当场就是摆设。
	if err := dynx.ValidateForType(fieldType, opts); err != nil {
		return errcode.New(errcode.BadRequest, err.Error())
	}
	if err := validateDefaultValue(fieldType, defaultValue, opts); err != nil {
		return err
	}
	return nil
}

// validateDefaultValue 校验默认值能不能被该类型接受
//
// ⚠️ **空串不算错** —— 空串就是"没有默认值"。
// 而 dynx.Normalize 对空串的处理按类型不同(int 会报"需要整数,收到 \"\""),
// 所以必须在这里先挡掉,不能直接丢给 Normalize。
func validateDefaultValue(fieldType dynx.Type, defaultValue string, opts dynx.Options) error {
	if strings.TrimSpace(defaultValue) == "" {
		return nil
	}
	if _, err := dynx.Normalize(fieldType, defaultValue, opts); err != nil {
		return errcode.New(errcode.BadRequest, "默认值不合法: "+err.Error())
	}
	return nil
}

// validateRelationConfig 设计期校验 relation 字段的配置(目标集合 + 显示字段)
//
// ⚠️ **目标集合必须存在且在用** ——
// 不校验的话设计器能存下一个不存在的目标 ⇒ 字段建好了,
// 候选下拉框永远是空的,而且**没有任何报错**(与来源 key 同一个坑)。
//
// ⚠️ **显示字段必须是目标集合的在用字段且"拿得出显示名"** ——
// json / multiselect / file 存的是序列化文本、JSON 数组、对象存储 key,
// 拿来做显示名没有意义;不校验的话列表页的"关联名"显示的是一串 key。
//
// ⚠️ **允许自引用**(集合引用自己,比如"任务的父任务")—— 不拦。
// 运行期(relationFieldOptions / dynRels)只做宽容回退,这里是唯一严格的地方。
func (s *dynService) validateRelationConfig(ctx context.Context, fieldType dynx.Type, optionsRaw string) error {
	if fieldType != dynx.TypeRelation {
		return nil
	}
	opts, err := dynx.ParseOptions(optionsRaw)
	if err != nil {
		return errcode.New(errcode.BadRequest, err.Error())
	}
	if opts.Relation == nil {
		return errcode.New(errcode.BadRequest, "关联字段必须配置目标集合")
	}
	target := strings.TrimSpace(opts.Relation.Target)
	var col model.SysDynCollection
	err = s.db.WithContext(ctx).Where("code = ? AND is_active = ?", target, true).First(&col).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errcode.New(errcode.BadRequest, "目标集合 "+target+" 不存在或已停用")
	}
	if err != nil {
		return errcode.New(errcode.Internal, "查询目标集合失败").WithDetail(err.Error())
	}
	display := strings.TrimSpace(opts.Relation.DisplayField)
	if display == "" {
		return nil
	}
	fields, err := s.activeFields(ctx, col.ID)
	if err != nil {
		return err
	}
	for i := range fields {
		if fields[i].FieldKey != display {
			continue
		}
		switch dynx.Type(fields[i].Type) {
		case dynx.TypeText, dynx.TypeLongText, dynx.TypeSelect,
			dynx.TypeInt, dynx.TypeNumber, dynx.TypeBool,
			dynx.TypeDate, dynx.TypeDateTime:
			return nil
		default:
			return errcode.New(errcode.BadRequest,
				"显示字段 "+display+" 的类型("+fields[i].Type+")不适合做显示名")
		}
	}
	return errcode.New(errcode.BadRequest, "显示字段 "+display+" 不存在或已停用")
}

// reviveField 复活同标识的软删字段(列还在,不重复 ADD COLUMN)
//
// ⚠️ **类型不一致直接拒绝** —— 列的存储格式是建列那一刻定死的,
// 换个类型复用同一列,老数据会被读成新格式(把存过 "abc" 的 int 列当 text 读)。
func (s *dynService) reviveField(ctx context.Context, existing *model.SysDynField, in *DynFieldInput) (uint64, error) {
	fieldType := dynx.Type(strings.TrimSpace(in.Type))
	if existing.Type != string(fieldType) {
		return 0, errcode.New(errcode.Conflict,
			fmt.Sprintf("字段 %q 之前是 %s 类型,列还在表里,不能改成 %s",
				existing.FieldKey, existing.Type, fieldType))
	}
	updates := map[string]any{
		"label":         strings.TrimSpace(in.Label),
		"required":      in.Required,
		"options":       in.Options,
		"default_value": in.DefaultValue,
		"sort":          in.Sort,
		"is_active":     true,
	}
	if err := s.db.WithContext(ctx).Model(&model.SysDynField{}).
		Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "更新字段失败").WithDetail(err.Error())
	}
	return existing.ID, nil
}

// UpdateField 改字段
func (s *dynService) UpdateField(ctx context.Context, caller *Caller, fieldID uint64, in *DynFieldUpdate) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	var field model.SysDynField
	if err := s.db.WithContext(ctx).First(&field, fieldID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "字段不存在")
		}
		return errcode.New(errcode.Internal, "查询字段失败").WithDetail(err.Error())
	}
	if in == nil {
		return errcode.New(errcode.BadRequest, "请求体不能为空")
	}
	// ⚠️ **改字段也要校验选项集与默认值** ——
	// 类型不可改,但选项集和默认值都可以改 ⇒ 这里是唯一能挡住
	// "把单选字段的选项全删了"(之后任何字符串都能存进去)的关口。
	if err := validateFieldShape(dynx.Type(field.Type), in.Options, in.DefaultValue); err != nil {
		return err
	}
	// relation 字段的设计期配置校验(类型不可改,但目标集合/显示字段可改)
	if err := s.validateRelationConfig(ctx, dynx.Type(field.Type), in.Options); err != nil {
		return err
	}
	updates := map[string]any{
		"label":         strings.TrimSpace(in.Label),
		"required":      in.Required,
		"options":       in.Options,
		"default_value": in.DefaultValue,
		"sort":          in.Sort,
	}
	if in.IsActive != nil {
		updates["is_active"] = *in.IsActive
	}
	if err := s.db.WithContext(ctx).Model(&model.SysDynField{}).
		Where("id = ?", field.ID).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新字段失败").WithDetail(err.Error())
	}
	return nil
}

// DeleteField 删字段(软删,列保留)
//
// ⚠️ **不 DROP COLUMN** —— 见 model.SysDynField.IsActive 的注释:
// 删字段是"这个字段不再用了",不是"把这段历史数据抹掉"。
// 留着列 ⇒ 老的记录还能读出历史值,数据不丢。
//
// ⚠️ **列留着,但读写时按"在用字段"过滤** ——
// 否则新记录会把已删字段写成默认值,老记录里那个字段的值还会冒出来。
//
// ⚠️ **引用该字段的用户索引一并退役(M2)** ——
// 索引定义引用的字段必须在「在用字段」里(见 syncCollectionIndexes),
// 字段停用后还留着引用它的定义,下一次改集合(哪怕只是改个名)就会校验失败,
// 用户被一个自己没碰过的索引卡死。列本身不删(数据不丢),
// 但"字段在设计器里消失"= "依赖它的索引一起消失",两个口径必须一致。
func (s *dynService) DeleteField(ctx context.Context, caller *Caller, fieldID uint64) error {
	if _, err := requireUser(caller); err != nil {
		return err
	}
	var field model.SysDynField
	if err := s.db.WithContext(ctx).First(&field, fieldID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return errcode.New(errcode.NotFound, "字段不存在")
		}
		return errcode.New(errcode.Internal, "查询字段失败").WithDetail(err.Error())
	}
	if err := s.db.WithContext(ctx).Model(&model.SysDynField{}).
		Where("id = ?", field.ID).
		Updates(map[string]any{"is_active": false}).Error; err != nil {
		return errcode.New(errcode.Internal, "删除字段失败").WithDetail(err.Error())
	}
	// 索引清理:解析集合行里的定义,引用了该字段的整条退役
	col, err := s.GetCollection(ctx, field.CollectionID)
	if err != nil {
		return err
	}
	if col.Indexes != "" {
		if defs, perr := dynx.ParseIndexes(col.Indexes); perr == nil {
			kept := make([]dynx.IndexDef, 0, len(defs))
			for _, d := range defs {
				references := false
				for _, key := range d.Columns {
					if key == field.FieldKey {
						references = true
						break
					}
				}
				if !references {
					kept = append(kept, d)
				}
			}
			if len(kept) != len(defs) {
				raw, merr := dynx.MarshalIndexes(kept)
				if merr != nil {
					return errcode.New(errcode.Internal, "序列化索引定义失败").WithDetail(merr.Error())
				}
				stored, serr := s.syncCollectionIndexes(ctx, col, raw)
				if serr != nil {
					return serr
				}
				if uerr := s.db.WithContext(ctx).Model(&model.SysDynCollection{}).
					Where("id = ?", col.ID).
					Updates(map[string]any{"indexes": stored}).Error; uerr != nil {
					return errcode.New(errcode.Internal, "更新索引定义失败").WithDetail(uerr.Error())
				}
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// DDL
// ---------------------------------------------------------------------------

// createTable 建物理表
func (s *dynService) createTable(ctx context.Context, phys string, fields []model.SysDynField) error {
	d := s.dialect()
	parts := make([]string, 0, len(fields)+len(dynSysCols)+1)
	for _, name := range dynSysCols {
		parts = append(parts, fmt.Sprintf("`%s` %s %s", name, dynColTypes[d][name], dynSysColDefault(name)))
	}
	for _, f := range fields {
		spec, ok := dynx.Lookup(dynx.Type(f.Type))
		if !ok {
			return fmt.Errorf("未知字段类型 %q", f.Type)
		}
		// ⚠️ **字段列必须可空(NOT NULL 是错的)** ——
		//
		//   dynx 的读 / 写两侧都是按「NULL = 没填」设计的:
		//     写:Normalize(nil) → (nil, nil)      —— 见 normalize.go 的说明
		//     读:Denormalize(nil) → nil           —— 见 denormalize.go
		//   而列是 NOT NULL 时,"没填"这一路直接撞约束:
		//     NOT NULL constraint failed: dyn_xxx.f_amount
		//   ⇒ 用户**清空一个选填的整数框就存不进去**(前端发的就是 null)。
		//
		//   这里**不写 NOT NULL、也不写 DEFAULT** ——
		//   两个方言下都等价于「可空 + 隐式默认值 NULL」,
		//   而且 MySQL 的 TEXT/BLOB 列**不允许带 DEFAULT**,
		//   写 DEFAULT NULL 会被 1101 拒掉。
		parts = append(parts, fmt.Sprintf("`%s` %s", f.Column, spec.ColumnType(d)))
	}
	parts = append(parts, "PRIMARY KEY (`"+dynColID+"`)")
	sql := fmt.Sprintf("CREATE TABLE `%s` (%s)", phys, strings.Join(parts, ", "))
	if d == "mysql" {
		sql += " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	}
	if err := s.db.WithContext(ctx).Exec(sql).Error; err != nil {
		return err
	}
	for _, idx := range []struct{ name, col string }{
		{"idx_" + phys + "_owner", dynColOwnerID},
		{"idx_" + phys + "_deleted", dynColDeletedAt},
	} {
		stmt := fmt.Sprintf("CREATE INDEX `%s` ON `%s` (`%s`)", idx.name, phys, idx.col)
		if err := s.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

// addColumn 加列
//
// ⚠️ **新列必须可空** —— 理由与 createTable 里那条一样(见那里的注释)。
// 加在一个已有数据的表上时,老行的这一列就是 NULL( = 没填),
// 而不是被填成 0 或空串那类"看起来像填过"的零值。
//
// ⚠️ **列已在就跳过(幂等)** —— 正常路径列不可能先于字段行存在,
// 但内置集合播种的半截状态会:字段行建失败/进程被杀,物理列却已经加上了。
// 这时补建字段行不能再 ADD COLUMN(必撞 duplicate column,
// 症状是服务器起不来);列在 = 结构已就位,跳过即可。
func (s *dynService) addColumn(ctx context.Context, phys string, field model.SysDynField) error {
	spec, ok := dynx.Lookup(dynx.Type(field.Type))
	if !ok {
		return fmt.Errorf("未知字段类型 %q", field.Type)
	}
	if s.db.WithContext(ctx).Migrator().HasColumn(phys, field.Column) {
		return nil
	}
	sql := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s",
		phys, field.Column, spec.ColumnType(s.dialect()))
	return s.db.WithContext(ctx).Exec(sql).Error
}

// dropTable 删物理表(补偿用)
func (s *dynService) dropTable(ctx context.Context, phys string) {
	_ = s.db.WithContext(ctx).Exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", phys)).Error
}

// syncCollectionIndexes 把集合行上的索引定义同步到物理表
//
// ⚠️ **先 create 新的、再 drop 退役的**(顺序反了会留窗口)——
// 先 drop 再 create 的话,create 撞存量重复值(唯一索引)失败时,
// 旧索引已经没了,而集合行里存的还是"有索引"的定义 ⇒ 两边漂移。
// create 先行的话,失败时旧的都还在,下次保存重试即可;多出来的索引无害,
// 下一次保存会按定义重新对账(只 drop 不在目标集里的)。
//
// ⚠️ **只动 dix_ 前缀的索引,系统索引(idx_owner / idx_deleted)一个不碰** ——
// 没有这个边界,一次"索引没配好"的保存就能把查询性能打回解放前,
// 而且是静默的,没有任何报错。
//
// ⚠️ **列引用的是字段标识,必须先换成物理列名** ——
// 定义里存的是 f_<key> 的 key 部分;字段停用(软删)后列还在物理表上,
// 但"在用字段"查不到它 ⇒ 校验会拒绝引用已停用字段的索引
// (防"字段删了索引忘删"整类 bug;老索引不受影响,列没被 DROP)。
func (s *dynService) syncCollectionIndexes(ctx context.Context, col *model.SysDynCollection, raw string) (string, error) {
	defs, err := dynx.ParseIndexes(raw)
	if err != nil {
		return "", errcode.New(errcode.BadRequest, err.Error())
	}
	// ⚠️ **行里存归一化后的 JSON** —— 调用方原文可能带任意空格,
	// 存原文的话"没改也会被当成改了",下次对账白跑一遍 DDL。
	stored, err := dynx.MarshalIndexes(defs)
	if err != nil {
		return "", errcode.New(errcode.Internal, err.Error())
	}
	// 列存在性:引用的字段必须在「在用字段」里
	var fields []model.SysDynField
	if err := s.db.WithContext(ctx).
		Where("collection_id = ? AND is_active = ?", col.ID, true).
		Find(&fields).Error; err != nil {
		return "", errcode.New(errcode.Internal, "查询字段失败").WithDetail(err.Error())
	}
	keyToCol := make(map[string]string, len(fields))
	for _, f := range fields {
		keyToCol[f.FieldKey] = f.Column
	}
	type built struct {
		name string
		def  dynx.IndexDef
		cols []string
	}
	targets := make([]built, 0, len(defs))
	for _, d := range defs {
		cols := make([]string, 0, len(d.Columns))
		for _, key := range d.Columns {
			colName, ok := keyToCol[key]
			if !ok {
				return "", errcode.New(errcode.BadRequest,
					fmt.Sprintf("索引 %q 引用了不存在或已停用的字段 %q", d.Name, key))
			}
			cols = append(cols, colName)
		}
		name, err := dynx.UserIndexName(col.PhysTable, d.Name)
		if err != nil {
			return "", errcode.New(errcode.BadRequest, err.Error())
		}
		targets = append(targets, built{name: name, def: d, cols: cols})
	}

	// 现存的用户索引(按前缀认领,系统索引不在认领范围)
	existing, err := s.listUserIndexNames(ctx, col.PhysTable)
	if err != nil {
		return "", errcode.New(errcode.Internal, "查询索引失败").WithDetail(err.Error())
	}
	targetSet := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		targetSet[t.name] = struct{}{}
	}
	// 先 create 缺的(已存在的跳过 ⇒ 重复保存幂等)
	existingSet := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		existingSet[name] = struct{}{}
	}
	for _, t := range targets {
		if _, done := existingSet[t.name]; done {
			continue
		}
		sql := dynx.CreateIndexSQL(col.PhysTable, t.name, t.def, t.cols)
		if err := s.db.WithContext(ctx).Exec(sql).Error; err != nil {
			return "", errcode.New(errcode.Internal,
				fmt.Sprintf("建索引 %s 失败(唯一索引可能撞存量重复值)", t.def.Name)).WithDetail(err.Error())
		}
	}
	// 再 drop 退役的(定义里已经没有了的)
	for _, name := range existing {
		if _, keep := targetSet[name]; keep {
			continue
		}
		if err := s.dropIndex(ctx, col.PhysTable, name); err != nil {
			return "", errcode.New(errcode.Internal, "删除旧索引失败").WithDetail(err.Error())
		}
	}
	return stored, nil
}

// listUserIndexNames 列出物理表上 dix_ 前缀的用户索引名(不含系统索引)
//
// ⚠️ **按前缀认领,而不是按集合行里存的定义认领** ——
// 上一次保存"DDL 成功但行写入失败"会留下定义外的孤儿索引,
// 按定义 drop 的话孤儿永远清不掉;按前缀全量认领,下一次保存自然对齐。
func (s *dynService) listUserIndexNames(ctx context.Context, phys string) ([]string, error) {
	prefix := dynx.UserIndexPrefix(phys)
	var names []string
	switch s.dialect() {
	case "mysql":
		err := s.db.WithContext(ctx).Raw(
			"SELECT DISTINCT INDEX_NAME FROM information_schema.STATISTICS "+
				"WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME LIKE ?",
			phys, prefix+"%").Scan(&names).Error
		return names, err
	default: // sqlite
		err := s.db.WithContext(ctx).Raw(
			"SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name LIKE ?",
			phys, prefix+"%").Scan(&names).Error
		return names, err
	}
}

// dropIndex 删一个索引(方言分支:MySQL 要带表名,SQLite 不用)
func (s *dynService) dropIndex(ctx context.Context, phys, name string) error {
	if s.dialect() == "mysql" {
		return s.db.WithContext(ctx).Exec(
			fmt.Sprintf("DROP INDEX `%s` ON `%s`", name, phys)).Error
	}
	return s.db.WithContext(ctx).Exec(fmt.Sprintf("DROP INDEX IF EXISTS `%s`", name)).Error
}

// dynSysColDefault 系统列的默认值(SQL 字面量)
//
// ⚠️ **系统列是 NOT NULL,用户列可空** ——
// 系统列(id / owner_id / created_at / updated_at)永远有值,
// 而用户列允许 NULL(= 没填),两者语义不同,不能一视同仁。
func dynSysColDefault(name string) string {
	switch name {
	case dynColID, dynColOwnerID:
		return "NOT NULL DEFAULT 0"
	case dynColDeletedAt:
		return "NULL DEFAULT NULL"
	default:
		return "NOT NULL DEFAULT ''"
	}
}
