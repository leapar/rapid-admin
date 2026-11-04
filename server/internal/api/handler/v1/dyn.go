// 动态业务集合 handler —— 设计面(集合/字段)+ 数据面(通用记录 CRUD)
//
// # 两面各管什么
//
//	设计面  /rapid-admin/v1/system/dyn/**      —— 集合与字段的增删改
//	数据面  /rapid-admin/v1/biz/dyn/:collection/** —— 某个集合的记录 CRUD
//
// # 数据面的权限在 handler 里判,不在路由上挂 rbac()
//
// 资源码是 `dyn:<集合编码>:<动作>`,集合编码来自 **URL 路径参数**,
// 注册期根本不存在 ⇒ rbac() 闭包(编译期常量)钉不出来。
// ⇒ 走 middleware.CanAccess —— 与 SSE 的 topic 判定**同一套 decideAccess**,
// 不是第二套实现(两套实现分叉是本仓明确记过的失败模式)。
//
// ⚠️ **不要为了"看起来统一"改成固定码** ——
// 固定码(如 `dyn:record:list`)会变成运营看不懂的粗粒度闸门:
// 管理员得同时配"通用动态记录"和"某个业务表"两套码,少配一套就 403,
// 而且报错说的是"没有 dyn:record:list",跟他要配的东西完全对不上。
//
// # 设计面与数据面的资源码形状不同(故意的)
//
//	设计面  dyn:design:collection:list   ← 4 段
//	数据面  dyn:<集合编码>:list           ← 3 段(集合编码由用户起)
//
// 集合编码里不可能出现 ":"(规则见 dynx.ValidateCode)⇒ 数据面永远 3 段。
// ⇒ 两套码**形状不同,撞不了**。
//
// ⚠️ **如果设计面也用 3 段(`dyn:collection:list`)会怎样** ——
// 用户建一个叫 "collection" 的集合,它的 "dyn:collection:list"
// 与设计面的"列出所有集合"是**同一个字符串** ⇒ 建表的人顺手拿到了
// 读全部集合定义的能力(越权)。所以这里是 4 段,不是偷懒。
//
// # 动作清单只有一份(在 internal/pkg/dynx/actions.go)
//
// service 登记 `dyn:<集合编码>:<动作>` 资源码、handler 拼同一条码 ——
// 两边引用同一份清单。
//
// ⚠️ **两边各抄一份的后果真的发生过**(2026-11-01):
// handler 多写了一个 "schema" 动作(想给表单结构单开一个码),
// 登记侧还是 5 个 ⇒ `dyn:<集合编码>:schema` **永远不存在** ⇒
// 「角色管理」页压根没有这一行可勾 ⇒ 谁也绑不上 ⇒
// 表单结构接口对所有非超管一律 403 —— **而日志里一条错误都没有**
// (权限判定返 false 是完全正常的一条路径)。
// ⇒ 清单收进原语层;守卫还会在动作不在清单时当场 500。
package v1

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/api/middleware"
	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/authz"
	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/service"
)

// DynHandler 动态业务集合
type DynHandler struct{ *handler.BaseHandler }

// dynImportMaxUploadBytes 导入 CSV 的上传体积上限(5MB)
//
// ⚠️ service 层的行数上限(5000 行)挡的是"行多",
// 这里挡的是"行少但字节巨大"的怪文件;两头都设,内存才有界。
const dynImportMaxUploadBytes = 5 << 20

// NewDynHandler 构造
func NewDynHandler(r *service.Registry) *DynHandler {
	return &DynHandler{BaseHandler: handler.New(r)}
}

// ============================================================================
// 设计面:集合
// ============================================================================

// ListCollections GET /rapid-admin/v1/system/dyn/collections?keyword=&page=&size=
func (h *DynHandler) ListCollections(ctx context.Context, c *app.RequestContext) {
	q := service.DynCollectionListQuery{Keyword: c.Query("keyword")}
	q.Page, q.Size = rawPage(c)
	res, err := h.Services.Dyn.ListCollections(ctx, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.dynPage(c, res)
}

// GetCollection GET /rapid-admin/v1/system/dyn/collections/:id
func (h *DynHandler) GetCollection(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	col, err := h.Services.Dyn.GetCollection(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, col)
}

// CreateCollection POST /rapid-admin/v1/system/dyn/collections
//
//	body: { code, name, description, owner_scoped, icon, sort }
func (h *DynHandler) CreateCollection(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body service.DynCollectionInput
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.Dyn.CreateCollection(ctx, caller, &body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// UpdateCollection PATCH /rapid-admin/v1/system/dyn/collections/:id
//
//	body: { name, description, owner_scoped, icon, sort, is_active }
func (h *DynHandler) UpdateCollection(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body service.DynCollectionUpdate
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Dyn.UpdateCollection(ctx, caller, id, &body); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// DeleteCollection DELETE /rapid-admin/v1/system/dyn/collections/:id
func (h *DynHandler) DeleteCollection(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Dyn.DeleteCollection(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// ============================================================================
// 设计面:字段
// ============================================================================

// ListFields GET /rapid-admin/v1/system/dyn/collections/:id/fields
//
// ⚠️ **返回全部字段(含已停用的)** ——
// 设计器要看得见"这个字段被停用了"才能恢复它;只返回在用的字段
// 的话,停用等于消失,用户根本不知道还能不能救。
func (h *DynHandler) ListFields(ctx context.Context, c *app.RequestContext) {
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	fields, err := h.Services.Dyn.ListFields(ctx, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, fields)
}

// CreateField POST /rapid-admin/v1/system/dyn/collections/:id/fields
//
//	body: { field_key, label, type, required, options, default_value, sort }
func (h *DynHandler) CreateField(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body service.DynFieldInput
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	fieldID, err := h.Services.Dyn.CreateField(ctx, caller, id, &body)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, fieldID)
}

// UpdateField PATCH /rapid-admin/v1/system/dyn/fields/:id
//
//	body: { label, required, options, default_value, sort, is_active }
func (h *DynHandler) UpdateField(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var body service.DynFieldUpdate
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Dyn.UpdateField(ctx, caller, id, &body); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// DeleteField DELETE /rapid-admin/v1/system/dyn/fields/:id
func (h *DynHandler) DeleteField(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Dyn.DeleteField(ctx, caller, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// FieldTypes GET /rapid-admin/v1/system/dyn/field-types
//
// ⚠️ **类型清单由后端下发,前端不许硬编码** ——
// 硬编码一份类型表,后端加一个类型(比如"日期区间")时,
// 前端的下拉框里没有它 ⇒ 用户选了那个类型,前端按不认识处理,
// 页面直接坏掉,而且**两边对不上没人知道**。
func (h *DynHandler) FieldTypes(ctx context.Context, c *app.RequestContext) {
	h.OK(c, dynx.Types())
}

// ============================================================================
// 数据面:记录
// ============================================================================

// dynFieldView 字段定义的前端视图
//
// ⚠️ **不把物理列名(column)给前端** ——
// 前端只用字段标识当 JSON 键,知道列名没有用处,
// 反而会诱导前端去依赖它(改字段标识就崩)。
type dynFieldView struct {
	// FieldKey 字段标识(前端 JSON 的键)
	FieldKey string `json:"field_key"`
	// Label 中文显示名
	Label string `json:"label"`
	// Type 字段类型
	Type string `json:"type"`
	// Required 是否必填
	Required bool `json:"required"`
	// Options 选项集(仅 select / multiselect 非空)
	//
	// ⚠️ **两种来源共用这一个字段** ——
	// 手工配置的选项集与关联系统表现查出来的选项,前端拿到的形状完全一样,
	// 前端不需要分支处理"这个选项是哪来的"。
	Options []dynx.Choice `json:"options"`
	// OptionSource 选项来源 key(空串 = 手工配置的选项集)
	OptionSource string `json:"option_source,omitempty"`
	// OptionReadable 关联表来源时:当前账号能不能读那张表
	//
	// ⚠️ **手工选项集恒为 true** —— 它是设计者写死在字段定义里的,
	// 不存在"读不到"这回事。
	OptionReadable bool `json:"option_readable"`
	// OptionRequiredPerm 读不到时缺哪个权限码(前端提示用)
	OptionRequiredPerm string `json:"option_required_perm,omitempty"`
	// OptionTruncated 候选被截断了(关联表来源的行数超过上限)
	OptionTruncated bool `json:"option_truncated,omitempty"`
	// DefaultValue 默认值(文本形态)
	DefaultValue string `json:"default_value"`
	// Sort 排序
	Sort int `json:"sort"`
}

// dynRelationSourceView 一个可关联的来源(设计面下拉框用)
//
// ⚠️ **不下发 LabelSQL / WhereSQL** ——
// 那是后端注册表里的实现细节(怎么拼 SELECT、过滤什么),
// 给了前端也没有用,还多一份"前端可以照着拼 SQL"的错觉。
type dynRelationSourceView struct {
	// Key 来源标识(存进 options.source.key)
	Key string `json:"key"`
	// Label 中文名
	Label string `json:"label"`
	// Table 来源表名(给用户看清楚"选项是从哪张表来的")
	Table string `json:"table"`
	// ValueColumn 取值列(存进记录的就是这一列)
	ValueColumn string `json:"value_column"`
	// ResourceCode 需要的资源码
	ResourceCode string `json:"resource_code"`
	// Readable 当前账号能不能读那张表
	//
	// ⚠️ **判定的对象是"当前调用方",不是"设计者"** ——
	// 设计师配了 `user` 来源,不代表录数据的业务员有 `user:list`。
	Readable bool `json:"readable"`
}

// RecordSchema GET /rapid-admin/v1/biz/dyn/:collection/schema
//
// 通用列表页要渲染表格列、要渲染录入表单,都得先知道"这个表长什么样"。
//
// ⚠️ **这个接口必须存在,不能靠"前端自己去设计面拿字段"** ——
// 设计面要 `dyn:design:field:list` 权限,那是管理员的权限;
// 业务人员只有 `dyn:<集合编码>:list` 却也得渲染表格。
// ⇒ 表结构跟着数据面走,一个集合一份。
//
// ⚠️ **权限归 `list`,不另开一个 `schema` 码** ——
// 表单结构是"渲染列表页"的前提:字段定义对能看记录的人没有信息增量,
// 而拆成两个码会让"只读查看"这个最常见的授权要多勾一个框,
// 漏勾就是**空表格 + 一个 403 弹窗**(页面半残,且看不出是权限问题)。
// ⇒ 清单见 internal/pkg/dynx/actions.go(唯一真相)。
func (h *DynHandler) RecordSchema(ctx context.Context, c *app.RequestContext) {
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionList)
	if !ok {
		return
	}
	col, err := h.Services.Dyn.GetCollectionByCode(ctx, code)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.recordSchemaInto(ctx, code, col, c)
}

// recordSchemaInto schema 视图装配(登录 / 公开两条路径共用)
func (h *DynHandler) recordSchemaInto(ctx context.Context, code string, col *model.SysDynCollection, c *app.RequestContext) {
	if !col.IsActive {
		h.FailCode(c, errcode.NotFound, "集合不存在")
		return
	}
	all, err := h.Services.Dyn.ListFields(ctx, col.ID)
	if err != nil {
		h.Fail(c, err)
		return
	}
	// ⚠️ **选项集在这里现查** ——
	// 关联表来源的选项不是落库的,是读的时候从系统表现查的
	// (设计文档 §2.10)⇒ 表里新增一行,这个表单立刻多一个选项。
	//
	// ⚠️ **权限判定在 handler 层做** ——
	// 判定走 middleware.CanAccessQuiet(执行层的唯一一份实现)。
	// service 层不能自己判:`internal/api/middleware` 依赖 `internal/service`,
	// 反过来 import 就是循环依赖 ⇒ handler 判完把结果传下去。
	caller, _ := service.FromContext(ctx)
	readable := h.dynRelationReadable(ctx, caller, dynUsedSources(all))
	optionSets, err := h.Services.Dyn.ResolveFieldOptions(ctx, caller, all, readable)
	if err != nil {
		h.Fail(c, err)
		return
	}
	fields := make([]dynFieldView, 0, len(all))
	// ⚠️ **按索引取,不用 `for _, f := range all`** ——
	// SysDynField 一行 216 字节,值拷贝会让 gocritic 的 rangeValCopy 报警
	// (也确实是真拷贝:每个字段都白搬一次)。
	for i := range all {
		f := &all[i]
		// ⚠️ **只给在用的字段** —— 停用的字段列还留在物理表里(不丢数据),
		// 但表单不该再出现它(出现了用户填了也存不进去)。
		if !f.IsActive {
			continue
		}
		view := dynFieldView{
			FieldKey:       f.FieldKey,
			Label:          f.Label,
			Type:           f.Type,
			Required:       f.Required,
			Options:        []dynx.Choice{},
			OptionReadable: true,
			DefaultValue:   f.DefaultValue,
			Sort:           f.Sort,
		}
		if set, ok := optionSets[f.FieldKey]; ok {
			view.Options = set.Choices
			view.OptionSource = set.Source
			view.OptionReadable = set.Readable
			view.OptionRequiredPerm = set.RequiredPerm
			view.OptionTruncated = set.Truncated
		} else if opts, err := dynx.ParseOptions(f.Options); err == nil {
			// ⚠️ **兜底:解析器整个失败时,退回到"手工选项集"的老行为** ——
			// 拿不到选项不该让整页表单打不开。
			view.Options = opts.Choices
		}
		fields = append(fields, view)
	}
	h.OK(c, map[string]any{
		"code":         col.Code,
		"name":         col.Name,
		"owner_scoped": col.OwnerScoped,
		// 公开只读(2026-11-03)—— 集成方据此判断这条 schema 能不能匿名拿
		"public_read": col.PublicRead,
		// 审批模板绑定(2026-11-03)—— 记录页据此决定「送审」动作与状态列要不要出现
		"approval_template_id": col.ApprovalTemplateID,
		"fields":               fields,
	})
}

// RelationSources GET /rapid-admin/v1/system/dyn/relation-sources
//
// 列出**可以关联的系统表** + 当前账号能不能读。
//
// ⚠️ **权限码 `dyn:design:type:list`** ——
// 与字段类型词表同一档:能看"有哪些类型"的人,也能看"有哪些来源表"。
// 两件事都是"填字段时的元信息",没有理由分成两档。
//
// ⚠️ **设计上不读表,只报"能不能读"** ——
// 这个接口只回答"这张表我配不配得上",不把表里的行倒出来。
// 真正的选项在读表单结构时现查(见 RecordSchema)。
func (h *DynHandler) RelationSources(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	principal, _ := authz.PrincipalFromContext(ctx)
	sources := dynx.RelationSources()
	out := make([]dynRelationSourceView, 0, len(sources))
	for _, s := range sources {
		out = append(out, dynRelationSourceView{
			Key:          s.Key,
			Label:        s.Label,
			Table:        s.Table,
			ValueColumn:  s.ValueColumn,
			ResourceCode: s.ResourceCode,
			// ⚠️ **判定的对象是"当前调用方"** ——
			// 设计师能配 ≠ 录数据的人能读。
			Readable: middleware.CanAccessQuiet(caller, principal, s.Module, s.Action, 0),
		})
	}
	h.OK(c, out)
}

// dynUsedSources 收集这批字段用到的来源 key
//
// ⚠️ **只判真正用到的来源** ——
// 每个来源判一次都是一次 casbin 强制,
// 集合里有三个来源但只有两个字段用 ⇒ 少判一次是一次。
//
// ⚠️ **relation 字段的目标集合也是一种"来源"** ——
// 统一编成 `collection:<目标集合码>` 这个形态的 key 进 readable 表,
// 与系统表来源(user:list 那一族)共用同一条下传通道。
// 前缀让 service 层分得清"这个 key 是集合还是注册表来源"。
func dynUsedSources(fields []model.SysDynField) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 2)
	for i := range fields {
		opts, err := dynx.ParseOptions(fields[i].Options)
		if err != nil {
			continue
		}
		if opts.HasSource() {
			if !seen[opts.Source.Key] {
				seen[opts.Source.Key] = true
				out = append(out, opts.Source.Key)
			}
			continue
		}
		if dynx.Type(fields[i].Type) == dynx.TypeRelation && opts.Relation != nil {
			target := strings.TrimSpace(opts.Relation.Target)
			if target == "" {
				continue
			}
			key := "collection:" + target
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	return out
}

// dynRelationReadable 算出这些来源里,当前调用方读得了哪几个
//
// ⚠️ **用 CanAccessQuiet(不记 WARN)** ——
// 「没权限读某张表」是**正常**的一等情况(业务员就是没有 user:list),
// 每次读表单都打一条 WARN ⇒ 日志被刷爆,真出事时反而看不见。
func (h *DynHandler) dynRelationReadable(
	ctx context.Context,
	caller *service.Caller,
	keys []string,
) map[string]bool {
	out := make(map[string]bool, len(keys))
	if len(keys) == 0 {
		return out
	}
	principal, _ := authz.PrincipalFromContext(ctx)
	for _, k := range keys {
		// ⚠️ **relation 字段的目标集合:判 `dyn:<目标>:list`** ——
		// 与记录列表接口同一对 module / action:能进列表页的人,
		// 下拉框里才看得到这个集合的候选。
		if target, ok := strings.CutPrefix(k, "collection:"); ok {
			out[k] = middleware.CanAccessQuiet(caller, principal, dynx.RecordModule(target), dynx.ActionList, 0)
			continue
		}
		src, ok := dynx.LookupRelationSource(k)
		if !ok {
			// ⚠️ **来源不在注册表里(老配置被删)⇒ 判成读不到** ——
			// service 层会按"不可读"降级成空选项。
			out[k] = false
			continue
		}
		out[k] = middleware.CanAccessQuiet(caller, principal, src.Module, src.Action, 0)
	}
	return out
}

// dynRecordQueryFromQuery 从 GET 查询串组装记录查询(M3 起 list / export 共用一份解析)
//
// ⚠️ **筛选参数是 `f_<字段标识>=v1,v2`** ——
// 普通名字会跟 keyword / owner / page 这些系统参数撞,
// 前缀让"哪个参数是筛选"一眼认得出;逗号分隔多值(值里本身带逗号的场景不存在:
// 字段标识的值是选项 code / 数字 / 日期,逗号不是合法字符)。
// 同名参数可重复出现(与逗号分隔等效,合并处理)。
func dynRecordQueryFromQuery(c *app.RequestContext) service.DynRecordQuery {
	q := service.DynRecordQuery{
		Keyword: c.Query("keyword"),
		Owner:   c.Query("owner"),
		Trash:   c.Query("trash") == "1",
	}
	q.Page, q.Size = rawPage(c)
	q.OrderBy = c.Query("order_by")
	q.OrderDir = c.Query("order_dir")
	q.SkipTotal = c.Query("skip_total") == "1"
	q.Fields = c.Query("fields")
	q.Filter = c.Query("filter")
	filters := make(map[string][]string)
	c.QueryArgs().VisitAll(func(key, value []byte) {
		k := string(key)
		if !strings.HasPrefix(k, "f_") {
			return
		}
		fk := strings.TrimPrefix(k, "f_")
		if fk == "" {
			return
		}
		for _, part := range strings.Split(string(value), ",") {
			if strings.TrimSpace(part) != "" {
				filters[fk] = append(filters[fk], part)
			}
		}
	})
	if len(filters) > 0 {
		q.Filters = filters
	}
	return q
}

// ListRecords GET /rapid-admin/v1/biz/dyn/:collection/records?keyword=&owner=&order_by=&order_dir=&page=&size=
// &skip_total=1&fields=a,b&f_<字段>=v1,v2
func (h *DynHandler) ListRecords(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionList)
	if !ok {
		return
	}
	q := dynRecordQueryFromQuery(c)
	res, err := h.Services.Dyn.ListRecords(ctx, caller, code, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.dynPage(c, res)
}

// GetRecord GET /rapid-admin/v1/biz/dyn/:collection/records/:id
func (h *DynHandler) GetRecord(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionRead)
	if !ok {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	rec, err := h.Services.Dyn.GetRecord(ctx, caller, code, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, rec)
}

// ── 公开只读(2026-11-03)──────────────────────────────────────────
//
// 路由在 /rapid-admin/v1/public/dyn/**(pub 组:IP 限流 60/min,无 Auth/RBAC/CSRF/Audit)。
// 集合 public_read = true 才放行 —— 闸门在 service(PublicReadCollection /
// publicRecordCtx),handler 不重复判;**写路径没有公开版**,
// 匿名可写是事故,不是特性。
// 合成调用方 RoleCode = "public" 什么权限都没有 ⇒ 行级 / relation 选项
// 全部走既有降级路径,不需要为新 caller 写一行特判。

// publicDynCaller 公开只读请求的合成调用方(与 service 侧 publicCaller 同形状)
func publicDynCaller() *service.Caller {
	return &service.Caller{UserID: 0, RoleCode: "public", Username: "public"}
}

// PublicRecordSchema GET /rapid-admin/v1/public/dyn/:collection/schema —— 公开只读
func (h *DynHandler) PublicRecordSchema(ctx context.Context, c *app.RequestContext) {
	code := strings.TrimSpace(c.Param("collection"))
	if err := dynx.ValidateCode(code); err != nil {
		h.FailCode(c, errcode.BadRequest, err.Error())
		return
	}
	col, err := h.Services.Dyn.PublicReadCollection(ctx, code)
	if err != nil {
		h.Fail(c, err)
		return
	}
	ctx = service.WithCaller(ctx, publicDynCaller())
	module := dynx.RecordModule(code)
	c.Set(scope.ModuleKey, module)
	ctx = scope.WithModule(ctx, module)
	h.recordSchemaInto(ctx, code, col, c)
}

// PublicListRecords GET /rapid-admin/v1/public/dyn/:collection/records —— 公开只读
func (h *DynHandler) PublicListRecords(ctx context.Context, c *app.RequestContext) {
	code := strings.TrimSpace(c.Param("collection"))
	if err := dynx.ValidateCode(code); err != nil {
		h.FailCode(c, errcode.BadRequest, err.Error())
		return
	}
	q := dynRecordQueryFromQuery(c)
	res, err := h.Services.Dyn.PublicListRecords(ctx, code, q)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.dynPage(c, res)
}

// PublicGetRecord GET /rapid-admin/v1/public/dyn/:collection/records/:id —— 公开只读
func (h *DynHandler) PublicGetRecord(ctx context.Context, c *app.RequestContext) {
	code := strings.TrimSpace(c.Param("collection"))
	if err := dynx.ValidateCode(code); err != nil {
		h.FailCode(c, errcode.BadRequest, err.Error())
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	rec, err := h.Services.Dyn.PublicGetRecord(ctx, code, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, rec)
}

// CreateRecord POST /rapid-admin/v1/biz/dyn/:collection/records
//
//	body: { "<字段标识>": <值>, ... } —— 键是字段标识,不是物理列名
func (h *DynHandler) CreateRecord(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionCreate)
	if !ok {
		return
	}
	var payload map[string]any
	if err := h.BindJSON(c, &payload); err != nil {
		h.Fail(c, err)
		return
	}
	id, err := h.Services.Dyn.CreateRecord(ctx, caller, code, payload)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, id)
}

// UpdateRecord PATCH /rapid-admin/v1/biz/dyn/:collection/records/:id
//
//	body: { "<字段标识>": <值>, ... } —— 只改请求里出现的字段
func (h *DynHandler) UpdateRecord(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionUpdate)
	if !ok {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	var payload map[string]any
	if err := h.BindJSON(c, &payload); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Dyn.UpdateRecord(ctx, caller, code, id, payload); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// DeleteRecord DELETE /rapid-admin/v1/biz/dyn/:collection/records/:id
func (h *DynHandler) DeleteRecord(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionDelete)
	if !ok {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Dyn.DeleteRecord(ctx, caller, code, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// RestoreRecord PUT /rapid-admin/v1/biz/dyn/:collection/records/:id/restore
//
// ⚠️ **恢复的是"回收站里"的行** ——
// 没删过的记录调恢复是 404,不是"无事发生"(点按钮没反应 ≠ 这条不存在)。
func (h *DynHandler) RestoreRecord(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionRestore)
	if !ok {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Dyn.RestoreRecord(ctx, caller, code, id); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// SubmitRecordApproval POST /rapid-admin/v1/biz/dyn/:collection/records/:id/approval
//
// 把记录送进集合绑定的审批流(2026-11-03)。
// 权限沿用 dyn:<code>:update —— 送审是"改这条记录的状态"的一种,
// 不新开资源码(同 restore 的口径)。
func (h *DynHandler) SubmitRecordApproval(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionUpdate)
	if !ok {
		return
	}
	id, err := h.GetID(c, "id")
	if err != nil {
		h.Fail(c, err)
		return
	}
	instID, err := h.Services.Dyn.SubmitRecordApproval(ctx, caller, code, id)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OKID(c, instID)
}

// dynBatchIDsRequest 批量操作的请求体
//
// ⚠️ **ids 是字符串数组** —— 前端手里的 id 本来就是 string(19 位 uint64),
// 转成 number 会丢精度;解析与校验在后端做。
type dynBatchIDsRequest struct {
	IDs []string `json:"ids"`
}

// BatchDeleteRecords POST /rapid-admin/v1/biz/dyn/:collection/records/batch-delete
//
//	body: { ids: ["...", ...] }
func (h *DynHandler) BatchDeleteRecords(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionBatchDelete)
	if !ok {
		return
	}
	var body dynBatchIDsRequest
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	count, err := h.Services.Dyn.BatchDeleteRecords(ctx, caller, code, body.IDs)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, map[string]any{"count": count})
}

// BatchUpdateRecords POST /rapid-admin/v1/biz/dyn/:collection/records/batch-update
//
//	body: { ids, field, value } —— field 是字段标识;值走与单条 update 同一套归一化
func (h *DynHandler) BatchUpdateRecords(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionBatchUpdate)
	if !ok {
		return
	}
	var body service.DynBatchUpdateRequest
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	count, err := h.Services.Dyn.BatchUpdateRecords(ctx, caller, code, body.IDs, body.Field, body.Value)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, map[string]any{"count": count})
}

// ExportRecords GET /rapid-admin/v1/biz/dyn/:collection/records/export?keyword=&owner=&order_by=&order_dir=&trash=
//
// ⚠️ **不走 h.OK 信封** —— 响应体是 CSV 字节流,不是 JSON 信封;
// Content-Type / Content-Disposition 直接写响应头。
func (h *DynHandler) ExportRecords(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionExport)
	if !ok {
		return
	}
	// M3 起 list / export 共用同一份查询解析 —— 导出与列表看到的是同一批数据
	//(筛选口径分叉的话,"导出的跟看到的不一样"比"导不出"更伤信任)。
	q := dynRecordQueryFromQuery(c)
	var buf bytes.Buffer
	if err := h.Services.Dyn.ExportRecords(ctx, caller, code, q, &buf); err != nil {
		// ⚠️ **先 build 后发** —— 出错时响应头还没写,错误能走正常的 JSON 信封。
		// 先写 200 + Content-Disposition 再流式写的话,报错时前端会把
		// 错误 JSON 当 CSV 存下来(裂文件,且没有报错提示)。
		h.Fail(c, err)
		return
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="dyn-`+code+`-`+
		strconv.FormatInt(time.Now().Unix(), 10)+`.csv"`)
	c.Data(200, "text/csv; charset=utf-8", buf.Bytes())
}

// ImportRecords POST /rapid-admin/v1/biz/dyn/:collection/records/import?mode=all|skip
//
// 表头认字段标识或显示名(与导出同形状);mode=all(默认)整批全有才进,skip 跳错行。
func (h *DynHandler) ImportRecords(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	ctx, code, ok := h.dynGuard(ctx, c, dynx.ActionImport)
	if !ok {
		return
	}
	fh, err := c.FormFile("file")
	if err != nil {
		h.FailCode(c, errcode.ParamMissing, "缺少文件字段 file")
		return
	}
	f, err := fh.Open()
	if err != nil {
		h.FailCode(c, errcode.BadRequest, "读取上传内容失败")
		return
	}
	defer func() { _ = f.Close() }()
	// ⚠️ **读取上限** —— 超上限直接拒,不把超大文件整块读进内存
	// (行数上限 5000 在 service 层,这里挡的是"行短但字节巨大"的怪文件)。
	data, err := io.ReadAll(io.LimitReader(f, dynImportMaxUploadBytes+1))
	if err != nil {
		h.FailCode(c, errcode.BadRequest, "读取上传内容失败")
		return
	}
	if len(data) > dynImportMaxUploadBytes {
		h.FailCode(c, errcode.BadRequest, "CSV 文件过大(上限 5MB)")
		return
	}
	res, err := h.Services.Dyn.ImportRecords(ctx, caller, code, data, c.Query("mode"))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// dynGuard 数据面的权限守卫
//
// 返回集合编码;不通过时已经把错误响应写出去了。
//
// ⚠️ **顺序是"先判权限,后查集合"** ——
// 反过来(先查集合再判权限)会把"这个集合存不存在"泄漏给没权限的人。
// 现在没权限的人拿到的是 403,不是 404。
//
// ⚠️ **集合编码要校验** ——
// 它拼进资源码,不校验就等于让请求方决定"判哪个权限"。
// dynx.ValidateCode 把它挡在合法形状外(小写字母开头、只含小写字母/数字/下划线)。
//
// ⚠️ **动作要在清单里** ——
// 资源码 `dyn:<集合编码>:<动作>` 只有在「权限资源」里存在一行才绑得动。
// 动作写错(比如手滑写了 "schema" 而清单里没有)⇒ 那条码永远不存在 ⇒
// 谁也绑不上 ⇒ **该接口对所有非超管 403,且日志里一条错误都没有**
// (权限判定返 false 是完全正常的一条路径)。
// ⇒ 这里当场 500 + 记日志,让"配置漏了"当场暴露,而不是变成一个安静坏掉的接口。
//
// ⚠️ **projectID 传 0** ——
// 动态表没有 project_id 列,项目域对它没有意义;
// 传 0 是"不做项目域校验"(middleware 里 projectID==0 会跳过那一步)。
//
// ⚠️ **返回新的 ctx** —— 数据范围(行级权限,M155)按业务域配,
// 而动态集合的数据面业务域是 `dyn:<集合编码>` ——
// 那个字符串**注册期拼不出来**(集合是用户建的,路由是静态的),
// 只有在这一层(拿到了路径参数)才知道。
// ⇒ 这里把业务域写进 ctx,service 只管读。
//
// ⚠️ **M163:同一份业务域再写进 Hertz 的 KV** ——
// 字段级可见性在**响应出口**生效,而那个出口(BaseHandler.OK/Page)
// 只有 RequestContext,读不到这里返回的派生 ctx。
func (h *DynHandler) dynGuard(ctx context.Context, c *app.RequestContext, action string) (context.Context, string, bool) {
	if !dynx.IsAction(action) {
		zap.L().Error("dyn: 未登记的动作,资源码在权限资源池里不存在",
			zap.String("action", action))
		h.FailCode(c, errcode.Internal, errcode.Msg(errcode.Internal))
		return ctx, "", false
	}
	caller, ok := service.FromContext(ctx)
	if !ok || caller == nil || caller.UserID == 0 {
		h.FailCode(c, errcode.Unauthorized, errcode.Msg(errcode.Unauthorized))
		return ctx, "", false
	}
	code := strings.TrimSpace(c.Param("collection"))
	if err := dynx.ValidateCode(code); err != nil {
		h.FailCode(c, errcode.BadRequest, err.Error())
		return ctx, "", false
	}
	principal, _ := authz.PrincipalFromContext(ctx)
	if !middleware.CanAccess(caller, principal, dynx.RecordModule(code), action, 0) {
		h.FailCode(c, errcode.Forbidden, errcode.Msg(errcode.Forbidden))
		return ctx, "", false
	}
	module := dynx.RecordModule(code)
	c.Set(scope.ModuleKey, module)
	return scope.WithModule(ctx, module), code, true
}

// rawPage 读**未经归一化**的分页参数
//
// ⚠️ **不复用 h.GetPage** ——
// h.GetPage 走 pagination.Normalize(size 上限 100),
// service 侧走 normPage(size 上限 200),两套规则不同
// (size=150 时一个给 100 一个给 200)
// ⇒ 响应里的 size 与实际返回的行数对不上,前端分页器会算错总页数。
// ⇒ 这里传原始值,让 service 归一化,再用 service 的结果回响应。
//
// ⚠️ **返回值起名** —— 两个 int 摆在一起,调用方看不出哪个是页码哪个是每页条数
// (gocritic 的 unnamedResult 也是这么说的:同类型多返回值必须起名)。
func rawPage(c *app.RequestContext) (page, size int) {
	page, _ = strconv.Atoi(c.Query("page"))
	size, _ = strconv.Atoi(c.Query("size"))
	return page, size
}

// dynPage 用 service 归一化后的页码回响应
//
// ⚠️ **必须用 res.Page / res.Size** —— 见 rawPage 的说明:
// 那是实际取数据的那个窗口,不是重新算一遍的另一个窗口。
//
// ⚠️ **M163:走 h.PageWith 而不是 response.Page** ——
// h.PageWith 是**带字段级可见性**的那个分页出口;
// 直接调 response.Page 就把这一层绕过去了(动态集合的字段是用户自己定的,
// 里面出现手机号/身份证是常事)。
func (h *DynHandler) dynPage(c *app.RequestContext, res *model.PageResponse) {
	if res == nil {
		h.PageWith(c, []any{}, 0, 1, 20)
		return
	}
	h.PageWith(c, res.List, res.Total, res.Page, res.Size)
}
