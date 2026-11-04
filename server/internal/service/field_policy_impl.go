// 字段级可见性(列级权限)规则 —— 真实实现 —— M162(2026-11-02)
//
// # 这一层做四件事
//
//  1. 存:规则落 model.SysFieldPolicy(全量替换,硬删)
//  2. 认候选:反射出每个业务域真实存在的响应字段
//  3. 算:调用 pkg/fieldpolicy(纯函数)
//  4. 生效:把策略交给响应出口(internal/api/handler 的 OK / Page)
//
// # ⚠️ **候选字段从响应模型上反射,不从数据库列上查**
//
// 理由见 biz_field_domain.go 的文件头:`json:"-"` 的列不在响应里,
// 按列出候选会让管理员配一个"作用不到任何键"的规则。
//
// # ⚠️ **配置期就拒"这个域没有这个字段"**
//
// 配一个不存在的字段名 ⇒ 规则作用不到任何键上 ⇒ **静默不生效**。
// 那正是这个机制要治的病 —— 与行级数据范围"维度落不下去 ⇒ 响亮报错"同一条纪律。
//
// # ⚠️ **缓存按 roleCode,不按 roleID**
//
// 与 scope_impl.go 同一个理由:解析路径上拿到的是 Caller.RoleCode,
// 而且 roleID 是 uint64,走 cachex 的 any 中转会掉精度(超过 2^53)。

package service

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/fieldpolicy"
	"rapid-admin/internal/pkg/sonyflake"
)

// fieldPolicyCacheTTL 字段规则缓存有效期
//
// ⚠️ **与 scopeCacheTTL 同一个理由** —— 它是**兜底**,不是生效路径:
// 改规则时显式失效,真实陈旧窗口通常为 0。
const fieldPolicyCacheTTL = 5 * time.Minute

// fieldPolicyCachePrefix 字段规则缓存的键前缀
const fieldPolicyCachePrefix = "fieldpolicy:rules:"

// fieldPolicyService 真实实现
type fieldPolicyService struct {
	db *gorm.DB
	// safe L2 缓存客户端(nil = 没有缓存)
	//
	// ⚠️ **允许为 nil** —— 单测里不装缓存是常态,
	// 所有方法都对 nil receiver 安全(退化成每次回源)。
	safe *cachex.Safe
}

// NewFieldPolicyService 构造
//
// ⚠️ **safe 传 nil 是合法的**(没有 Redis / cache.mode=off)。
func NewFieldPolicyService(db *gorm.DB, safe *cachex.Safe) FieldPolicyService {
	return &fieldPolicyService{db: db, safe: safe}
}

// ===== 读 =====

// ListRoleFieldPolicies 列出某角色配的全部字段规则(所有业务域)
func (s *fieldPolicyService) ListRoleFieldPolicies(
	ctx context.Context, roleID uint64,
) ([]FieldPolicyView, error) {
	var rows []model.SysFieldPolicy
	if err := s.db.WithContext(ctx).
		Where("role_id = ?", roleID).
		Order("module ASC, field ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询字段可见性规则失败").WithDetail(err.Error())
	}
	out := make([]FieldPolicyView, 0, len(rows))
	// ⚠️ **下标遍历而不是 `for _, r := range rows`** ——
	// model.SysFieldPolicy 有 100 多字节,按值拷贝进循环变量每个元素都拷一遍
	// (gocritic rangeValCopy)。
	for i := range rows {
		r := &rows[i]
		out = append(out, FieldPolicyView{
			Module:   r.Module,
			Field:    r.Field,
			Mode:     r.Mode,
			MaskType: r.MaskType,
			Remark:   r.Remark,
		})
	}
	return out, nil
}

// ListFieldPolicyModules 列出可配置的业务域候选(含每个域的字段候选)
//
// ⚠️ **默认规则组排在最前** —— 它是"这个角色在所有业务域上的默认字段规则",
// 是最常用的一项,不该藏在列表最后面(与 scope_impl 的 ListModules 同一条)。
func (s *fieldPolicyService) ListFieldPolicyModules(
	ctx context.Context,
) ([]FieldPolicyModuleView, error) {
	labels, err := s.moduleLabels(ctx)
	if err != nil {
		return nil, err
	}
	apiMods := s.apiFieldModules(labels)
	dynMods, err := s.dynFieldModules(ctx)
	if err != nil {
		return nil, err
	}
	all := make([]FieldPolicyModuleView, 0, len(apiMods)+len(dynMods)+1)
	all = append(all, defaultGroupFieldModule(apiMods, dynMods))
	all = append(all, apiMods...)
	all = append(all, dynMods...)
	sort.Slice(all[1:], func(i, j int) bool {
		a, b := all[1+i], all[1+j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Module < b.Module
	})
	return all, nil
}

// apiFieldModules 业务表域的候选(字段来自响应模型反射)
func (s *fieldPolicyService) apiFieldModules(labels map[string]string) []FieldPolicyModuleView {
	out := make([]FieldPolicyModuleView, 0, len(bizFieldDomains))
	for _, d := range bizFieldDomains {
		label := labels[d.module]
		if label == "" {
			label = d.module
		}
		out = append(out, FieldPolicyModuleView{
			Module: d.module,
			Label:  label,
			Source: "api",
			Fields: toFieldViews(wireFieldCandidates(d.models...)),
		})
	}
	return out
}

// dynFieldModules 动态业务集合的候选(字段来自 sys_dyn_field)
//
// ⚠️ **动态集合的字段是运行期建的** —— 候选取自 `sys_dyn_field` 的**在用**字段,
// 与 dyn_records.go 的 activeFields 同一套口径(停用的字段列还留在物理表里,
// 但记录视图里已经没有了)。
func (s *fieldPolicyService) dynFieldModules(ctx context.Context) ([]FieldPolicyModuleView, error) {
	var cols []model.SysDynCollection
	if err := s.db.WithContext(ctx).
		Select("id", "code", "name").
		Where("is_active = ?", true).
		Order("code ASC").
		Find(&cols).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询动态集合失败").WithDetail(err.Error())
	}
	out := make([]FieldPolicyModuleView, 0, len(cols))
	for i := range cols {
		c := &cols[i]
		fields, err := s.dynFields(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, FieldPolicyModuleView{
			Module: "dyn:" + c.Code,
			Label:  c.Name,
			Source: "dyn",
			Fields: toFieldViews(dynFieldCandidates(fields)),
		})
	}
	return out, nil
}

// dynFields 取某个集合的在用字段
func (s *fieldPolicyService) dynFields(ctx context.Context, collectionID uint64) ([]model.SysDynField, error) {
	var rows []model.SysDynField
	if err := s.db.WithContext(ctx).
		Where("collection_id = ? AND is_active = ?", collectionID, true).
		Order("sort ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询动态集合字段失败").WithDetail(err.Error())
	}
	return rows, nil
}

// defaultGroupFieldModule 默认规则组(候选 = 所有业务域候选的并集)
//
// ⚠️ **候选取并集** —— 默认组的规则会作用在**每一个**业务域上,
// 而各域的字段不一样(house 有 address,owner 有 real_name)。
// ⇒ 并集才是"这个角色在所有业务域上的默认字段规则"能表达的完整集合。
func defaultGroupFieldModule(mods ...[]FieldPolicyModuleView) FieldPolicyModuleView {
	seen := map[string]bool{}
	var fields []FieldPolicyFieldView
	for _, list := range mods {
		for _, m := range list {
			for _, f := range m.Fields {
				if seen[f.Field] {
					continue
				}
				seen[f.Field] = true
				fields = append(fields, f)
			}
		}
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Field < fields[j].Field })
	return FieldPolicyModuleView{
		Module: "",
		Label:  "默认(适用于所有业务域)",
		Source: "default",
		Fields: fields,
	}
}

// ===== 写 =====

// ReplaceRoleFieldPolicies 全量替换某角色在某业务域上的字段规则
func (s *fieldPolicyService) ReplaceRoleFieldPolicies(
	ctx context.Context, roleID uint64, module string, policies []FieldPolicyInput,
) error {
	module = strings.TrimSpace(module)
	if len(module) > 64 {
		return errcode.New(errcode.BadRequest, "业务域标识过长(最多 64 字符)")
	}

	// 角色必须存在 —— 否则会给一个不存在的角色写下规则,
	// 而那行规则永远不会被读到(还占着唯一键)。
	roleCode, err := lookupRoleCode(ctx, s.db, roleID)
	if err != nil {
		return err
	}
	if roleCode == "" {
		return errcode.New(errcode.NotFound, "角色不存在")
	}

	// 业务域必须存在
	known, err := s.knownFieldModules(ctx)
	if err != nil {
		return err
	}
	if !known[module] {
		return errcode.New(errcode.BadRequest,
			"业务域 "+module+" 不存在或不支持字段级可见性(规则配了也不会生效)").
			WithDetail("可配的业务域:" + knownModuleList(known))
	}

	// 候选字段 —— 用来在配置期就拒"这个域没有这个字段"
	candidates, err := s.moduleCandidates(ctx, module)
	if err != nil {
		return err
	}
	candidateSet := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		candidateSet[c.field] = true
	}

	parsed, err := parseFieldPolicyInputs(policies, candidateSet)
	if err != nil {
		return err
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// ⚠️ **硬删** —— 软删的墓碑会挡住唯一索引,
		// 第二次保存同一个组合就撞 UNIQUE。
		if err := clearBindings(tx, &model.SysFieldPolicy{}, "role_id = ? AND module = ?", roleID, module); err != nil {
			return errcode.New(errcode.Internal, "清理旧的字段可见性规则失败").WithDetail(err.Error())
		}
		for _, p := range parsed {
			row := model.SysFieldPolicy{
				BaseModel: model.BaseModel{ID: sonyflake.Next()},
				RoleID:    roleID,
				Module:    module,
				Field:     p.field,
				Mode:      string(p.mode),
				MaskType:  string(p.maskType),
				Remark:    p.remark,
			}
			if err := tx.Create(&row).Error; err != nil {
				return errcode.New(errcode.Internal, "保存字段可见性规则失败").WithDetail(err.Error())
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.invalidateRole(ctx, roleCode)
}

// fieldPolicy 一条已校验、已归一的字段规则
type fieldPolicy struct {
	field    string
	mode     fieldpolicy.Mode
	maskType fieldpolicy.MaskType
	remark   string
}

// parseFieldPolicyInputs 校验并归一前端发来的规则组
//
// ⚠️ **三条校验都在这里一次做完** ——
// ① 模式/打码方式合法 ② 打码方式猜得出来 ③ 字段在这个业务域的候选里。
// 事务里插到一半才发现非法,会留下"删了旧的、新的没插全"的空档,
// 而管理员看到的只是"保存失败",不知道该改哪一条。
func parseFieldPolicyInputs(
	policies []FieldPolicyInput, candidates map[string]bool,
) ([]fieldPolicy, error) {
	out := make([]fieldPolicy, 0, len(policies))
	seen := map[string]bool{}
	for _, p := range policies {
		field := strings.TrimSpace(p.Field)
		if field == "" {
			return nil, errcode.New(errcode.BadRequest, "字段名不能为空")
		}
		if len(field) > 64 {
			return nil, errcode.New(errcode.BadRequest, "字段名过长(最多 64 字符)")
		}
		if seen[field] {
			return nil, errcode.New(errcode.BadRequest, "字段 "+field+" 重复了")
		}
		seen[field] = true

		mode, err := fieldpolicy.ParseMode(p.Mode)
		if err != nil {
			return nil, errcode.New(errcode.BadRequest, err.Error())
		}
		maskType, err := fieldpolicy.ParseMaskType(p.MaskType)
		if err != nil {
			return nil, errcode.New(errcode.BadRequest, err.Error())
		}
		// ⚠️ **"打码"但猜不出打码方式 ⇒ 配置期就拒** ——
		// 运行期的兜底是"整串打码",那会让管理员看到一堆意料之外的星号,
		// 而他以为自己配的是"手机号打码"。
		if mode == fieldpolicy.ModeMasked && maskType == fieldpolicy.MaskAuto {
			maskType = fieldpolicy.AutoMaskType(field)
			if maskType == fieldpolicy.MaskAuto {
				return nil, errcode.New(errcode.BadRequest,
					"字段 "+field+" 猜不出打码方式,请显式指定 mask_type").
					WithDetail("可用的 mask_type:" + strings.Join(maskTypeNames(), "、"))
			}
		}
		// ⚠️ **这个域没有这个字段 ⇒ 配置期就拒** ——
		// 配一个不存在的字段名 ⇒ 规则作用不到任何键上 ⇒ **静默不生效**。
		if len(candidates) > 0 && !candidates[field] {
			return nil, errcode.New(errcode.BadRequest,
				"这个业务域没有字段 "+field+"(规则配了也不会生效)").
				WithDetail("该业务域可配的字段:" + candidateFieldList(candidates))
		}
		out = append(out, fieldPolicy{
			field:    field,
			mode:     mode,
			maskType: maskType,
			remark:   strings.TrimSpace(p.Remark),
		})
	}
	return out, nil
}

// maskTypeNames 合法的打码方式名(给错误信息看)
func maskTypeNames() []string {
	types := fieldpolicy.AllMaskTypes()
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}

// candidateFieldList 把候选字段列成一行(给错误信息看)
func candidateFieldList(candidates map[string]bool) string {
	keys := make([]string, 0, len(candidates))
	for k := range candidates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 30 {
		keys = keys[:30]
	}
	return strings.Join(keys, ", ")
}

// invalidateRole 清掉某角色的字段规则缓存
//
// ⚠️ **按 roleCode 删前缀,不是 roleID** —— 缓存键里是 roleCode,
// 按 roleID 删会删不到任何东西,于是"改了规则但旧规则还在生效",而且**没有任何报错**。
func (s *fieldPolicyService) invalidateRole(ctx context.Context, roleCode string) error {
	if s.safe == nil || !s.safe.L2Available() {
		return nil // 没有缓存 → 没有东西要失效
	}
	if roleCode == "" {
		return nil
	}
	if !s.safe.SupportsScan() {
		return errcode.New(errcode.Internal,
			"缓存后端不支持前缀扫描,字段可见性规则缓存无法失效(期间会读到旧规则)")
	}
	if _, err := s.safe.DeletePrefix(ctx, fieldPolicyCachePrefix+roleCode+":"); err != nil {
		return errcode.New(errcode.Internal, "失效字段可见性规则缓存失败").WithDetail(err.Error())
	}
	return nil
}

// ===== 解析 =====

// EffectiveFieldPolicy 解析出某调用方在某业务域上的有效字段策略
//
// ⚠️ **默认规则组与业务域规则是"逐字段叠加",不是"整组覆盖"** ——
// 默认组的限制不会被某个业务域的其它规则悄悄顶掉(见 pkg/fieldpolicy.Overlay)。
func (s *fieldPolicyService) EffectiveFieldPolicy(
	ctx context.Context, caller *Caller, module string,
) (fieldpolicy.Policy, error) {
	if caller == nil {
		return fieldpolicy.Policy{}, nil
	}
	// ⚠️ **不支持的业务域直接返回空策略,连缓存都不查** ——
	//
	// "auth" / "config" / "dict" / "role" 这些域**没有业务行**,
	// 它们的响应也不是"业务行的一个投影" ⇒ 字段级可见性对它们没有意义。
	//
	// 而**默认规则组(module="")只作用于业务域** ——
	// 它是"这个角色在所有业务域上的默认字段规则",
	// 不是"所有接口的默认规则"。
	// (若把它撒到 auth/config 上,一条"隐藏 name"的默认规则
	//  会把配置项的名字也一起藏掉。)
	//
	// ⇒ 这一层早退 ⇒ 每个请求省掉两次缓存查表。
	if !fieldPolicyDomainSupported(module) {
		return fieldpolicy.Policy{}, nil
	}
	modRules, err := s.rulesFor(ctx, caller.RoleCode, module)
	if err != nil {
		return fieldpolicy.Policy{}, err
	}
	if module == "" {
		return fieldpolicy.NewPolicy(modRules), nil
	}
	defRules, err := s.rulesFor(ctx, caller.RoleCode, "")
	if err != nil {
		return fieldpolicy.Policy{}, err
	}
	return fieldpolicy.NewPolicy(defRules).Overlay(fieldpolicy.NewPolicy(modRules)), nil
}

// fieldPolicyDomainSupported 这个业务域是否受字段级可见性管
//
// ⚠️ **判据是"有没有业务行"** ——
// 只有业务域的响应才是"业务行的一个投影",字段级可见性才有意义。
//
// ⚠️ **动态集合的数据面模块是 "dyn:<集合编码>"** ——
// 它不在静态登记表里(字段是运行期建的,候选从 sys_dyn_field 现算),
// 但同样受字段级可见性管 ⇒ 用前缀认。
func fieldPolicyDomainSupported(module string) bool {
	if module == "" {
		// 默认规则组本身
		return true
	}
	if _, ok := bizFieldDomainFor(module); ok {
		return true
	}
	return strings.HasPrefix(module, "dyn:")
}

// rulesFor 取某角色在某业务域上的规则(带缓存)
func (s *fieldPolicyService) rulesFor(
	ctx context.Context, roleCode, module string,
) ([]fieldpolicy.Rule, error) {
	if roleCode == "" {
		return nil, nil
	}
	loader := func(ctx context.Context) ([]fieldpolicy.Rule, error) {
		return s.loadRules(ctx, roleCode, module)
	}
	key := fieldPolicyCachePrefix + roleCode + ":" + module
	v, _, err := cachex.GetOrLoadTypedL2(ctx, s.safe, key, fieldPolicyCacheTTL, loader)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询字段可见性规则失败").WithDetail(err.Error())
	}
	return v, nil
}

// loadRules 从库里读某角色在某业务域上的规则
//
// ⚠️ **只读这一组,不回落默认组** ——
// 回落与叠加的差别在 EffectiveFieldPolicy 里做(逐字段叠加)。
// 这里混着做会让"默认组 + 业务域组"变成不可分辨的一坨。
func (s *fieldPolicyService) loadRules(
	ctx context.Context, roleCode, module string,
) ([]fieldpolicy.Rule, error) {
	roleID, err := lookupRoleID(ctx, s.db, roleCode)
	if err != nil {
		return nil, err
	}
	if roleID == 0 {
		return nil, nil
	}
	var rows []model.SysFieldPolicy
	if err := s.db.WithContext(ctx).
		Where("role_id = ? AND module = ?", roleID, module).
		Order("field ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询字段可见性规则失败").WithDetail(err.Error())
	}
	out := make([]fieldpolicy.Rule, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		mode, err := fieldpolicy.ParseMode(r.Mode)
		if err != nil {
			return nil, errcode.New(errcode.Internal, "字段可见性规则的模式已损坏").WithDetail(err.Error())
		}
		maskType, err := fieldpolicy.ParseMaskType(r.MaskType)
		if err != nil {
			return nil, errcode.New(errcode.Internal, "字段可见性规则的打码方式已损坏").WithDetail(err.Error())
		}
		out = append(out, fieldpolicy.Rule{Field: r.Field, Mode: mode, MaskType: maskType})
	}
	return out, nil
}

// ===== 候选 =====

// moduleCandidates 某个业务域的字段候选
//
// ⚠️ **没登记的业务域返回空** —— 调用方据此知道"这一域没有可配的字段"。
func (s *fieldPolicyService) moduleCandidates(
	ctx context.Context, module string,
) ([]fieldCandidate, error) {
	if module == "" {
		all, err := s.allCandidates(ctx)
		if err != nil {
			return nil, err
		}
		return all, nil
	}
	if d, ok := bizFieldDomainFor(module); ok {
		return wireFieldCandidates(d.models...), nil
	}
	if strings.HasPrefix(module, "dyn:") {
		code := strings.TrimPrefix(module, "dyn:")
		return s.dynCandidatesByCode(ctx, code)
	}
	return nil, nil
}

// allCandidates 默认规则组的候选 = 所有业务域候选的并集
func (s *fieldPolicyService) allCandidates(ctx context.Context) ([]fieldCandidate, error) {
	seen := map[string]bool{}
	var out []fieldCandidate
	for _, d := range bizFieldDomains {
		for _, c := range wireFieldCandidates(d.models...) {
			if seen[c.field] {
				continue
			}
			seen[c.field] = true
			out = append(out, c)
		}
	}
	dynMods, err := s.dynFieldModules(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range dynMods {
		for _, f := range m.Fields {
			if seen[f.Field] {
				continue
			}
			seen[f.Field] = true
			out = append(out, fieldCandidate{
				field:     f.Field,
				label:     f.Label,
				maskType:  fieldpolicy.MaskType(f.MaskType),
				sensitive: f.Sensitive,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].field < out[j].field })
	return out, nil
}

// dynCandidatesByCode 某个动态集合的字段候选
func (s *fieldPolicyService) dynCandidatesByCode(ctx context.Context, code string) ([]fieldCandidate, error) {
	var col model.SysDynCollection
	err := s.db.WithContext(ctx).Select("id").First(&col, "code = ?", code).Error
	if err != nil {
		return nil, nil
	}
	fields, err := s.dynFields(ctx, col.ID)
	if err != nil {
		return nil, err
	}
	return dynFieldCandidates(fields), nil
}

// knownFieldModules 已存在的业务域集合(含空串 = 默认规则组)
func (s *fieldPolicyService) knownFieldModules(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{"": true}
	for _, d := range bizFieldDomains {
		out[d.module] = true
	}
	var cols []model.SysDynCollection
	if err := s.db.WithContext(ctx).
		Select("code").
		Where("is_active = ?", true).
		Find(&cols).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询动态集合失败").WithDetail(err.Error())
	}
	for i := range cols {
		out["dyn:"+cols[i].Code] = true
	}
	return out, nil
}

// moduleLabels 给业务域配中文标签(与数据范围那一层同一套来源)
func (s *fieldPolicyService) moduleLabels(ctx context.Context) (map[string]string, error) {
	var res []model.SysResource
	if err := s.db.WithContext(ctx).
		Select("code", "name", "type").
		Where("is_active = ?", true).
		Find(&res).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	modules := make([]string, 0, len(bizFieldDomains))
	for _, d := range bizFieldDomains {
		modules = append(modules, d.module)
	}
	return scopeModuleLabels(res, modules), nil
}

// ===== 反射 =====

// fieldCandidate 一个字段候选项
type fieldCandidate struct {
	field     string
	label     string
	maskType  fieldpolicy.MaskType
	sensitive bool
}

// wireFieldCandidates 从响应模型上反射出候选字段
//
// ⚠️ **取并集并按字段名排序** —— 同一个字段在多个模型里出现时只留一条。
func wireFieldCandidates(models ...any) []fieldCandidate {
	seen := map[string]bool{}
	var out []fieldCandidate
	for _, m := range models {
		for _, c := range reflectWireFields(m) {
			if seen[c.field] {
				continue
			}
			seen[c.field] = true
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].field < out[j].field })
	return out
}

// reflectWireFields 反射一个模型上的响应字段
func reflectWireFields(v any) []fieldCandidate {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var out []fieldCandidate
	collectWireFields(t, &out)
	return out
}

// collectWireFields 递归收集结构体上的响应字段
//
// ⚠️ **嵌入的 BaseModel 不进候选** ——
// id 是主键(前端靠它跳转/查详情),created_at/updated_at/deleted_at 是框架基础设施列。
// 把它们摆进下拉框,管理员勾一个"隐藏 id",页面直接废掉。
//
// ⚠️ **其它嵌入结构(AuditFields 等)要展开** ——
// created_by / updated_by 是"谁建的",正是要管的那类字段。
func collectWireFields(t reflect.Type, out *[]fieldCandidate) {
	baseType := reflect.TypeOf(model.BaseModel{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue // 非导出字段
		}
		if f.Anonymous {
			if f.Type == baseType {
				continue
			}
			ft := f.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectWireFields(ft, out)
			}
			continue
		}
		name, ok := wireFieldName(f)
		if !ok {
			continue
		}
		mt := fieldpolicy.AutoMaskType(name)
		*out = append(*out, fieldCandidate{
			field:     name,
			label:     name,
			maskType:  mt,
			sensitive: mt != fieldpolicy.MaskAuto,
		})
	}
}

// wireFieldName 取一个结构体字段的响应键名
//
// ⚠️ **`json:"-"` 的字段不在响应里** —— 它不是候选。
// ⚠️ **没有 json tag 时用 Go 字段名** —— 与 encoding/json 的默认行为一致。
func wireFieldName(f reflect.StructField) (string, bool) {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return f.Name, true
	}
	name := strings.Split(tag, ",")[0]
	if name == "-" {
		return "", false
	}
	if name == "" {
		return f.Name, true
	}
	return name, true
}

// dynFieldCandidates 动态集合的字段候选
func dynFieldCandidates(fields []model.SysDynField) []fieldCandidate {
	out := make([]fieldCandidate, 0, len(fields))
	for i := range fields {
		f := &fields[i]
		mt := fieldpolicy.AutoMaskType(f.FieldKey)
		out = append(out, fieldCandidate{
			field:     f.FieldKey,
			label:     f.Label,
			maskType:  mt,
			sensitive: mt != fieldpolicy.MaskAuto,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].field < out[j].field })
	return out
}

// toFieldCandidates → DTO
func toFieldViews(cs []fieldCandidate) []FieldPolicyFieldView {
	out := make([]FieldPolicyFieldView, 0, len(cs))
	for _, c := range cs {
		out = append(out, FieldPolicyFieldView{
			Field:     c.field,
			Label:     c.label,
			MaskType:  string(c.maskType),
			Sensitive: c.sensitive,
		})
	}
	return out
}
