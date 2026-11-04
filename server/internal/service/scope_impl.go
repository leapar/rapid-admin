// 数据范围(行级权限)规则 —— 真实实现 —— M155(2026-11-02)
//
// # 这一层做四件事
//
//  1. 存:规则落 model.SysDataScope(全量替换,硬删)
//  2. 算:调用 pkg/scope.Resolve(纯函数)
//  3. 认人:把 Caller 翻译成 pkg/scope.Actor(项目 / 部门从哪来)
//  4. 落:调用 pkg/scope.Apply(翻译成 WHERE)
//
// # 为什么规则按 roleCode 缓存,而不是 roleID
//
// 解析路径上拿到的是 **Caller.RoleCode**(字符串),不是 roleID ——
// 拿 roleID 得多查一次 sys_role。所以缓存键用 roleCode。
//
// ⚠️ **而且必须用 roleCode**:roleID 是 uint64,而 cachex 的 TwoLevel 路径
// 走 any 中转,uint64 超过 2^53 会掉精度(见 pkg/cachex/typed.go 的注释)。
// roleCode 是字符串,没这个问题。
//
// # 认人的三条来源链(与仓库既有约定一致)
//
// 项目:
//
//	当前身份 sys_user_identity.project_id > 0 → 就它
//	否则 → token 里的 Caller.ProjectIDs(与 RBAC 项目域校验同一个来源)
//	都没有 → 空(该调用方不属于任何项目)⇒"本项目"规则看不到任何数据
//
// 部门:
//
//	当前身份 sys_user_identity.dept_id > 0 → 就它
//	否则 → 用户所属部门 sys_user_department(主部门优先)
//
// ⚠️ **身份优先不是新发明** —— 与 approval_resolver_impl.go 的
// applicantDeptID 同一条链(身份部门 → 主部门 → 第一条)。
// 两处各定一套 = 同一个问题两个答案,那是最难查的一类不一致。

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/cachex"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/pkg/sonyflake"
)

// scopeCacheTTL 数据范围规则的缓存有效期
//
// ⚠️ **5 分钟不是"随便定的"** —— 它是**兜底**,不是生效路径:
// 改规则时显式失效(ReplaceRoleScopes 里删前缀),
// 所以真实陈旧窗口是"失效失败到 TTL 到期"这段时间,通常 0。
//
// 之所以还要留一个 TTL:失效万一失败(后端不支持 SCAN 等),
// 至少 5 分钟后自愈,而不是永远读到旧规则。
const scopeCacheTTL = 5 * time.Minute

// scopeCachePrefix 数据范围规则缓存的键前缀
//
// ⚠️ **失效时按 "前缀 + roleCode + ':'" 删** ——
// 用 roleCode 而不是 roleID 的理由见文件头。
const scopeCachePrefix = "scope:rules:"

// scopeService 真实实现
type scopeService struct {
	db *gorm.DB
	// safe L2 缓存客户端(nil = 没有缓存)
	//
	// ⚠️ **允许为 nil** —— 单测里不装缓存是常态,
	// 所有方法都对 nil receiver 安全(退化成每次回源)。
	safe *cachex.Safe
}

// NewScopeService 构造
//
// ⚠️ **safe 传 nil 是合法的**(没有 Redis / cache.mode=off)——
// 退化成每次回源,**不改变函数的成功/失败语义**(与 cachex 全体一致)。
//
// 生产装配期传 `cachePool.Safe()`(见 cmd/server/main.go):
// **刻意只取 L2**,不取全局池本身 —— 全局池带 L1,
// L1 只能被本实例的失效清掉,多副本下"改完立刻生效"会退化成"最多 10 秒后生效"。
func NewScopeService(db *gorm.DB, safe *cachex.Safe) ScopeService {
	return &scopeService{db: db, safe: safe}
}

// ===== 读 =====

// ListRoleScopes 列出某角色配的全部规则(所有业务域)
func (s *scopeService) ListRoleScopes(ctx context.Context, roleID uint64) ([]ScopeRuleView, error) {
	var rows []model.SysDataScope
	if err := s.db.WithContext(ctx).
		Where("role_id = ?", roleID).
		Order("module ASC, scope_type ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询数据范围规则失败").WithDetail(err.Error())
	}
	out := make([]ScopeRuleView, 0, len(rows))
	// ⚠️ **下标遍历而不是 `for _, r := range rows`** ——
	// model.SysDataScope 有 100 多字节,按值拷贝进循环变量每个元素都拷一遍
	// (gocritic rangeValCopy)。
	for i := range rows {
		r := &rows[i]
		v := ScopeRuleView{
			Module:       r.Module,
			ScopeType:    r.ScopeType,
			CustomColumn: r.CustomColumn,
			Remark:       r.Remark,
		}
		if r.CustomValues != "" {
			vals, err := decodeCustomValues(r.CustomValues)
			if err != nil {
				// ⚠️ **库里存坏了不许静默吞掉** —— 自定义取值读不出来,
				// 管理员在页面上看到的是"这条规则没有取值",会以为是没配,
				// 于是重配一次 —— 而库里那条坏数据还在,永远读不对。
				return nil, errcode.New(errcode.Internal, "数据范围规则的自定义取值已损坏").WithDetail(err.Error())
			}
			v.CustomValues = vals
		}
		out = append(out, v)
	}
	return out, nil
}

// ListModules 列出可配置的业务域候选
//
// ⚠️ **拆成三段(default / api / dyn + 合并排序)** —— 一开始是个函数,
// 圈复杂度 18,越过仓库上限(15)。拆开之后每段只管一件事,
// 而且三段的取数方式完全不同,混在一个函数里读不清。
func (s *scopeService) ListModules(ctx context.Context) ([]ScopeModuleView, error) {
	apiMods, err := s.apiScopeModules(ctx)
	if err != nil {
		return nil, err
	}
	dynMods, err := s.dynScopeModules(ctx)
	if err != nil {
		return nil, err
	}
	// ⚠️ **默认规则组(module="")排在最前** ——
	// 它是"这个角色在所有业务域上的默认行级权限",
	// 是最常用的一项,不该藏在列表最后面。
	out := make([]ScopeModuleView, 0, len(apiMods)+len(dynMods)+1)
	out = append(out, ScopeModuleView{
		Module: "",
		Label:  "默认(适用于所有业务域)",
		Source: "default",
		// ⚠️ **默认组的可用维度 = 所有已知业务域的交集** ——
		// 默认组的规则会在**每一个**没单独配规则的业务域上各自求值,
		// 而各张表能表达的维度不一样(楼栋/户型/房屋/项目/业主/家庭没有归属人列;
		// 动态集合只有归属人一维)。
		// ⇒ 默认组里配了某个业务域表达不出来的维度时,
		// **那个业务域的查询会直接报错**(见 biz_scope.go 的 checkDims)。
		// ⇒ 前端按这个交集过滤下拉框,管理员配不出"必然在某些域炸"的规则。
		SupportedTypes: defaultGroupScopeTypes(apiMods, dynMods),
	})
	out = append(out, apiMods...)
	out = append(out, dynMods...)
	sort.Slice(out[1:], func(i, j int) bool {
		a, b := out[1+i], out[1+j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Module < b.Module
	})
	return out, nil
}

// apiScopeModules 从**路由层登记的业务域**出候选
//
// ⚠️ **业务域集合来自 `scope.RegisteredModules()`,不是从资源码反推** ——
// 资源码是 module + ":" + action,而两者都可能含冒号
// (rbac("audit:operation", "list") / rbac("job", "run:read")),
// 反推出来的 module 与运行时注入的对不上 ⇒ 规则静默失配。
// 详见 internal/pkg/scope/module.go。
//
// ⚠️ **标签仍然从 sys_resource 取** —— 那是给人看的,
// 取不到就退回业务域本身(总比空串强)。
func (s *scopeService) apiScopeModules(ctx context.Context) ([]ScopeModuleView, error) {
	registered := scope.RegisteredModules()
	if len(registered) == 0 {
		return nil, nil
	}
	var res []model.SysResource
	if err := s.db.WithContext(ctx).
		Select("code", "name", "type").
		Where("is_active = ?", true).
		Find(&res).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}

	labels := scopeModuleLabels(res, registered)

	// ⚠️ **只列"接了行级数据范围"的业务域** —— M161
	//
	// 路由层登记的业务域有 40 多个,大部分是平台域(角色 / 字典 / 配置 / 短信模板……),
	// 它们**没有业务行**,配数据范围**永远不生效**。
	//
	// ⇒ 以前把它们列进候选,就是给管理员一个"配了也不生效"的选项 ——
	// 与"业务域拼错一个字符"是同一类失败,只是更隐蔽(拼错会报错,这个不报错)。
	//
	// ⇒ 现在只列真接了的(bizScopeSpecs / bizScopeCtxWired / dyn:<编码>)。
	// 没接的那些在 bizScopeUnwired 里有账,启动期 CheckScopeWiring 会核对。
	out := make([]ScopeModuleView, 0, len(registered))
	for _, m := range registered {
		if _, ok := scopeModuleSupported(m); !ok {
			continue
		}
		label := labels[m]
		if label == "" {
			label = m
		}
		types, _ := scopeModuleSupported(m)
		out = append(out, ScopeModuleView{
			Module:         m,
			Label:          label,
			Source:         "api",
			SupportedTypes: types,
		})
	}
	return out, nil
}

// defaultGroupScopeTypes 默认规则组能用的维度 = 所有已知业务域的交集
//
// ⚠️ **为什么取交集而不是并集** ——
// 默认组的规则会在**每一个**没单独配规则的业务域上各自求值。
// 取并集 ⇒ 默认组里出现一个"某些表表达不出来"的维度
// ⇒ 那些业务域的查询**直接报错**(checkDims 的响亮失败)。
// ⇒ 前端按交集过滤,管理员配不出这种规则。
//
// ⚠️ **一个业务域都没有时 ⇒ 全部类型都可用** ——
// 没有具体业务域时,默认组是唯一能配的东西,
// 此时把下拉框清空就变成"一个都配不了"(与 dyn_scope 那边同一条教训)。
//
// ⚠️ **交集是"逐个求交",不是"按固定顺序取"** ——
// 各业务域支持的维度集合大小不同,交集要真的算一遍。
func defaultGroupScopeTypes(mods ...[]ScopeModuleView) []string {
	all := make([]string, 0, len(scope.AllTypes()))
	for _, t := range scope.AllTypes() {
		all = append(all, string(t))
	}
	var acc []string
	for _, list := range mods {
		for _, m := range list {
			if len(m.SupportedTypes) == 0 {
				// 这个业务域没报支持的维度(老数据/异常)⇒ 交集算不出来,
				// 保守起见当成"支持全部",不因为它把交集缩成空。
				continue
			}
			acc = intersectScopeTypes(acc, m.SupportedTypes)
			if len(acc) == 0 {
				return nil
			}
		}
	}
	if acc == nil {
		return all
	}
	return acc
}

// intersectScopeTypes 两个维度集合的交集(保持 allTypes 的顺序)
func intersectScopeTypes(a, b []string) []string {
	if a == nil {
		return b
	}
	inB := make(map[string]bool, len(b))
	for _, v := range b {
		inB[v] = true
	}
	out := make([]string, 0, len(a))
	for _, v := range a {
		if inB[v] {
			out = append(out, v)
		}
	}
	return out
}

// scopeModuleLabels 给业务域配中文标签
//
// ⚠️ **菜单名优先于接口名** —— "用户列表"(接口)不如"用户管理"(菜单)像业务域。
// 菜单码形如 "menu.system.user",后缀 ".<module>" 对得上就是它。
//
// ⚠️ **对不上就取第一条接口名** —— 接口码形如 "<module>:<action>",
// 前缀对得上就用它的名字;再不济用业务域字符串本身。
func scopeModuleLabels(res []model.SysResource, modules []string) map[string]string {
	labels := make(map[string]string, len(modules))

	for i := range res {
		r := &res[i]
		if r.Type != "menu" || r.Name == "" {
			continue
		}
		for _, m := range modules {
			if _, ok := labels[m]; ok {
				continue
			}
			if strings.HasSuffix(r.Code, "."+m) {
				labels[m] = r.Name
			}
		}
	}
	for i := range res {
		r := &res[i]
		if r.Type != "api" || r.Name == "" {
			continue
		}
		for _, m := range modules {
			if _, ok := labels[m]; ok {
				continue
			}
			if r.Code == m || strings.HasPrefix(r.Code, m+":") {
				labels[m] = r.Name
			}
		}
	}
	return labels
}

// dynScopeModules 从动态业务集合推导业务域候选
//
// ⚠️ **动态集合的数据面模块是 "dyn:<集合编码>"** ——
// 与接口权限码的模块段("dyn")不是一回事:
// 设计面(rbac("dyn", ...))的模块段是 "dyn",
// 数据面(集合编码来自 URL 路径参数)的模块是 "dyn:<编码>",
// 两者是不同的数据域,规则也要分开配。
func (s *scopeService) dynScopeModules(ctx context.Context) ([]ScopeModuleView, error) {
	var cols []model.SysDynCollection
	if err := s.db.WithContext(ctx).
		Select("code", "name").
		Where("is_active = ?", true).
		Find(&cols).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询动态集合失败").WithDetail(err.Error())
	}
	out := make([]ScopeModuleView, 0, len(cols))
	for i := range cols {
		c := &cols[i]
		out = append(out, ScopeModuleView{
			Module: "dyn:" + c.Code,
			Label:  c.Name,
			Source: "dyn",
			// ⚠️ **动态集合的物理表只有归属人一维** ——
			// 列是 id / owner_id / created_at / updated_at / deleted_at + f_<字段>,
			// **没有 project_id、也没有 dept_id** ⇒ 只能按 owner_id 过滤。
			SupportedTypes: []string{
				string(scope.TypeAll), string(scope.TypeSelf), string(scope.TypeCustom),
			},
		})
	}
	return out, nil
}

// ===== 写 =====

// ReplaceRoleScopes 全量替换某角色在某业务域上的规则
func (s *scopeService) ReplaceRoleScopes(
	ctx context.Context, roleID uint64, module string, rules []ScopeRuleInput,
) error {
	module = strings.TrimSpace(module)
	if len(module) > 64 {
		return errcode.New(errcode.BadRequest, "业务域标识过长(最多 64 字符)")
	}

	// 角色必须存在 —— 否则会给一个不存在的角色写下规则,
	// 而那行规则永远不会被读到(还占着唯一键)。
	roleCode, err := s.roleCode(ctx, roleID)
	if err != nil {
		return err
	}
	if roleCode == "" {
		return errcode.New(errcode.NotFound, "角色不存在")
	}

	// 业务域必须是**已存在的**业务域
	//
	// ⚠️ **为什么在这里就要拒** ——
	// 配错一个字符 ⇒ 规则静默不生效(退化成"全部数据")，
	// 而"看起来配好了"比"配失败"危险得多。
	// ⇒ 配置时就拒,并把已知业务域列出来。
	known, err := s.knownModules(ctx)
	if err != nil {
		return err
	}
	if len(known) > 0 {
		if _, ok := known[module]; !ok {
			return errcode.New(errcode.BadRequest,
				"业务域 "+module+" 不存在或不支持数据范围(规则配了也不会生效)").
				WithDetail("可配的业务域:" + knownModuleList(known))
		}
	}

	// 逐条校验 + 归一
	parsed, err := parseScopeRuleInputs(rules)
	if err != nil {
		return err
	}

	// ⚠️ **这一维在这张表上落不下去 ⇒ 配置时就拒** —— M161
	//
	// 以前只在查询时报错(那时管理员已经走了,而且错的是**用这条规则的人**)。
	// ⇒ 现在配的时候就说清楚:这个业务域的表没有对应的归属列。
	//
	// ⚠️ **默认规则组(module 为空串)不校验** ——
	// 它同时管着所有"没单独配规则"的业务域,而各张表能表达的维度不一样
	// (楼栋/户型/房屋/项目/业主/家庭**没有归属人列**;动态集合只有归属人一维)。
	// ⇒ 在这里按"所有表的交集"校验会把默认规则组几乎废掉
	// (能剩下的只有「全部数据」和「自定义」)。
	//
	// ⇒ 默认规则组里配了某个业务域表达不出来的维度时,
	// **那个业务域的查询会响亮报错**(见 biz_scope.go 的 checkDims),
	// 消息里点名是哪个业务域、哪张表、哪一维。
	if module != "" {
		for _, p := range parsed {
			if !scopeModuleSupportsType(module, string(p.typ)) {
				return scopeModuleTypeError(module, p.typ)
			}
		}
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// ⚠️ **硬删** —— 见 binding_replace.go 的注释:
		// 软删的墓碑会挡住唯一索引,第二次保存同一个组合就撞 UNIQUE。
		if err := clearBindings(tx, &model.SysDataScope{}, "role_id = ? AND module = ?", roleID, module); err != nil {
			return errcode.New(errcode.Internal, "清理旧的数据范围规则失败").WithDetail(err.Error())
		}
		for _, p := range parsed {
			row := model.SysDataScope{
				BaseModel:    model.BaseModel{ID: sonyflake.Next()},
				RoleID:       roleID,
				Module:       module,
				ScopeType:    string(p.typ),
				CustomColumn: p.column,
				CustomValues: encodeCustomValues(p.values),
				Remark:       p.remark,
			}
			if err := tx.Create(&row).Error; err != nil {
				return errcode.New(errcode.Internal, "保存数据范围规则失败").WithDetail(err.Error())
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.invalidateRole(ctx, roleCode)
}

// scopeRule 一条已校验、已归一的数据范围规则
type scopeRule struct {
	typ    scope.Type
	column string
	values []string
	remark string
}

// parseScopeRuleInputs 校验并归一前端发来的规则组
//
// ⚠️ **拆出来是为了圈复杂度** —— ReplaceRoleScopes 里同时塞着
// "查角色 / 查业务域 / 校验规则 / 落库 / 失效缓存"五件事,
// 每加一种范围类型就 +2,迟早越过仓库上限(15)。
//
// ⚠️ **校验在这里一次做完,不在落库时才发现问题** ——
// 事务里插到一半才发现某条规则非法,会留下"删了旧的、新的没插全"的空档,
// 而管理员看到的只是"保存失败",不知道该改哪一条。
func parseScopeRuleInputs(rules []ScopeRuleInput) ([]scopeRule, error) {
	out := make([]scopeRule, 0, len(rules))

	seen := map[scope.Type]bool{}
	for _, r := range rules {
		typ, err := scope.ParseType(r.ScopeType)
		if err != nil {
			return nil, errcode.New(errcode.BadRequest, err.Error())
		}
		if seen[typ] {
			// 同一角色的同一业务域上,同一种类型只能有一条
			// (唯一键 role_id + module + scope_type)⇒ 这里直接拒,
			// 不靠数据库报唯一索引冲突(那个错误信息对管理员没用)。
			return nil, errcode.New(errcode.BadRequest, "数据范围类型 "+string(typ)+" 重复了")
		}
		seen[typ] = true

		p := scopeRule{typ: typ, remark: strings.TrimSpace(r.Remark)}
		if typ == scope.TypeCustom {
			col := strings.TrimSpace(r.CustomColumn)
			if err := scope.ValidateCustomColumn(col); err != nil {
				return nil, errcode.New(errcode.BadRequest, err.Error())
			}
			p.column = col
			for _, v := range r.CustomValues {
				if v != "" {
					p.values = append(p.values, v)
				}
			}
			if len(p.values) == 0 {
				return nil, errcode.New(errcode.BadRequest, "自定义数据范围必须至少给一个取值")
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// invalidateRole 清掉某角色的规则缓存
//
// ⚠️ **按 roleCode 删前缀,不是 roleID** ——
// 缓存键里是 roleCode(见文件头的理由),按 roleID 删会删不到任何东西,
// 于是"改了规则但旧规则还在生效",而且**没有任何报错**。
func (s *scopeService) invalidateRole(ctx context.Context, roleCode string) error {
	if s.safe == nil || !s.safe.L2Available() {
		return nil // 没有缓存 → 没有东西要失效
	}
	if roleCode == "" {
		return nil
	}
	if !s.safe.SupportsScan() {
		// ⚠️ **后端不支持扫描 ⇒ 失效一定没做成** ——
		// 静默返回 nil 就是"改了规则但旧规则还在生效",而且没人知道。
		// 与 DictCache.Invalidate 同一判断。
		return errcode.New(errcode.Internal,
			"缓存后端不支持前缀扫描,数据范围规则缓存无法失效(期间会读到旧规则)")
	}
	if _, err := s.safe.DeletePrefix(ctx, scopeCachePrefix+roleCode+":"); err != nil {
		return errcode.New(errcode.Internal, "失效数据范围规则缓存失败").WithDetail(err.Error())
	}
	return nil
}

// ===== 解析 =====

// EffectiveScope 解析出某调用方在某业务域上的有效作用域
func (s *scopeService) EffectiveScope(
	ctx context.Context, caller *Caller, module string,
) (scope.Scope, error) {
	if caller == nil {
		return scope.Scope{}, nil
	}
	rules, err := s.rulesFor(ctx, caller.RoleCode, module)
	if err != nil {
		return scope.Scope{}, err
	}
	if len(rules) == 0 {
		// ⚠️ **没配规则 = 不过滤 = 全部数据** ——
		// 这是规则层上线前的行为,保持不变。
		// 反过来(没配 = 看不到任何数据)会让上线当天全站空列表。
		return scope.Scope{}, nil
	}
	actor, err := s.actorFor(ctx, caller)
	if err != nil {
		return scope.Scope{}, err
	}
	return scope.Resolve(rules, actor)
}

// Apply 把作用域落到查询上
func (s *scopeService) Apply(ctx context.Context, tx *gorm.DB, cols scope.Columns) (*gorm.DB, error) {
	if tx == nil {
		return nil, errcode.New(errcode.Internal, "数据范围过滤:查询为空")
	}
	caller, ok := FromContext(ctx)
	if !ok || caller == nil {
		// 内部调用(定时任务 / 跨服务)没有调用者 ⇒ 没有"这个人能看哪些行"
		// 这件事 ⇒ 不过滤。**这不是放行漏洞**:内部调用不走用户身份。
		return tx, nil
	}
	// 业务域从 ctx 读 —— 判定权限的那一层写进去的
	module, _ := scope.ModuleFromContext(ctx)
	sc, err := s.EffectiveScope(ctx, caller, module)
	if err != nil {
		return nil, err
	}
	return scope.Apply(tx, &sc, cols)
}

// rulesFor 取某角色在某业务域上的有效规则(带缓存)
func (s *scopeService) rulesFor(ctx context.Context, roleCode, module string) ([]scope.Rule, error) {
	if roleCode == "" {
		return nil, nil
	}
	loader := func(ctx context.Context) ([]scope.Rule, error) {
		return s.loadRules(ctx, roleCode, module)
	}
	v, _, err := cachex.GetOrLoadTypedL2(ctx, s.safe, s.rulesKey(roleCode, module), scopeCacheTTL, loader)
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询数据范围规则失败").WithDetail(err.Error())
	}
	return v, nil
}

// rulesKey 缓存键
func (s *scopeService) rulesKey(roleCode, module string) string {
	return scopeCachePrefix + roleCode + ":" + module
}

// loadRules 从库里读某角色在某业务域上的规则
//
// ⚠️ **"某业务域单独配了就完全用这一组,默认组不参与"** ——
// 两组是"就近覆盖",不是叠加。
// 叠加的话,默认组里的 "all" 会一票放行,业务域上的规则永远不生效。
func (s *scopeService) loadRules(ctx context.Context, roleCode, module string) ([]scope.Rule, error) {
	roleID, err := s.roleID(ctx, roleCode)
	if err != nil {
		return nil, err
	}
	if roleID == 0 {
		return nil, nil
	}
	rows, err := s.rawRules(ctx, roleID, module)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 && module != "" {
		// 本业务域没单独配 ⇒ 回落默认组(module 为空的那一组)
		rows, err = s.rawRules(ctx, roleID, "")
		if err != nil {
			return nil, err
		}
	}
	return parseScopeRules(rows)
}

// rawRules 读某角色某业务域上的原始行
func (s *scopeService) rawRules(ctx context.Context, roleID uint64, module string) ([]model.SysDataScope, error) {
	var rows []model.SysDataScope
	if err := s.db.WithContext(ctx).
		Where("role_id = ? AND module = ?", roleID, module).
		Order("scope_type ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询数据范围规则失败").WithDetail(err.Error())
	}
	return rows, nil
}

// parseScopeRules 把原始行解析成规则
func parseScopeRules(rows []model.SysDataScope) ([]scope.Rule, error) {
	out := make([]scope.Rule, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		typ, err := scope.ParseType(r.ScopeType)
		if err != nil {
			return nil, errcode.New(errcode.Internal, "数据范围规则的类型已损坏").WithDetail(err.Error())
		}
		rule := scope.Rule{Type: typ, CustomColumn: r.CustomColumn}
		if typ == scope.TypeCustom && r.CustomValues != "" {
			vals, err := decodeCustomValues(r.CustomValues)
			if err != nil {
				return nil, errcode.New(errcode.Internal, "数据范围规则的自定义取值已损坏").WithDetail(err.Error())
			}
			rule.CustomValues = vals
		}
		out = append(out, rule)
	}
	return out, nil
}

// actorFor 把 Caller 翻译成 Actor
func (s *scopeService) actorFor(ctx context.Context, caller *Caller) (scope.Actor, error) {
	actor := scope.Actor{UserID: caller.UserID}

	var ident model.SysUserIdentity
	hasIdent := false
	if caller.IdentityID != 0 {
		err := s.db.WithContext(ctx).
			Select("project_id", "dept_id").
			First(&ident, "id = ?", caller.IdentityID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return scope.Actor{}, errcode.New(errcode.Internal, "查询当前身份失败").WithDetail(err.Error())
		}
		hasIdent = err == nil
	}

	// 项目:身份优先,否则用 token 里的工作范围
	switch {
	case hasIdent && ident.ProjectID > 0:
		actor.ProjectIDs = []uint64{ident.ProjectID}
	default:
		actor.ProjectIDs = caller.ProjectIDs
	}

	// 部门:身份优先,否则用用户所属部门
	switch {
	case hasIdent && ident.DeptID > 0:
		actor.DeptIDs = []uint64{ident.DeptID}
	default:
		depts, err := s.userDeptIDs(ctx, caller.UserID)
		if err != nil {
			return scope.Actor{}, err
		}
		actor.DeptIDs = depts
	}

	actor.DeptSubtree = func(ids []uint64) ([]uint64, error) {
		return s.deptSubtree(ctx, ids)
	}
	return actor, nil
}

// userDeptIDs 用户所属的部门 id 列表(主部门优先)
func (s *scopeService) userDeptIDs(ctx context.Context, userID uint64) ([]uint64, error) {
	if userID == 0 {
		return nil, nil
	}
	var rows []model.SysUserDepartment
	if err := s.db.WithContext(ctx).
		Select("dept_id").
		Where("user_id = ?", userID).
		Order("is_primary DESC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户所属部门失败").WithDetail(err.Error())
	}
	out := make([]uint64, 0, len(rows))
	for _, r := range rows {
		if r.DeptID != 0 {
			out = append(out, r.DeptID)
		}
	}
	return out, nil
}

// deptSubtree 给一组部门 id 返回「自己 + 所有下级」的并集
func (s *scopeService) deptSubtree(ctx context.Context, ids []uint64) ([]uint64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.SysDepartment
	if err := s.db.WithContext(ctx).
		Select("id", "parent_id").
		Where("is_active = ?", true).
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询部门树失败").WithDetail(err.Error())
	}
	children := map[uint64][]uint64{}
	for i := range rows {
		r := &rows[i]
		children[r.ParentID] = append(children[r.ParentID], r.ID)
	}
	out := make([]uint64, 0, len(rows))
	seen := map[uint64]bool{}
	var walk func(id uint64)
	walk = func(id uint64) {
		// ⚠️ seen 同时防环和防重复 —— 部门树里出现环(数据被改坏)
		// 时,没有它就是无限递归。
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
		for _, c := range children[id] {
			walk(c)
		}
	}
	for _, id := range ids {
		walk(id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// ===== 辅助 =====

// applyScope 把角色上的数据范围规则落到这条查询上
//
// ⚠️ **共享 helper,不每个 service 各写一遍** ——
// 每个 service 各写一遍,"哪个 service 漏了"就变成只有那个人知道的事。
// 这里把"scopeSvc 为 nil ⇒ 不过滤"这条兜底统一收口。
//
// ⚠️ **nil 检查必须在** —— scopeSvc 是接口,接口的零值是 nil,
// 对 nil 接口调方法直接 panic。而"没装配 scope 服务"是合法场景
// (单测、某些精简装配),不该让整个列表接口崩掉。
func applyScope(ctx context.Context, scopeSvc ScopeService, tx *gorm.DB, cols scope.Columns) (*gorm.DB, error) {
	if scopeSvc == nil {
		return tx, nil
	}
	return scopeSvc.Apply(ctx, tx, cols)
}

// roleID 按角色 code 查 id(不存在返 0)
//
// ⚠️ **M162:实现挪到 role_lookup.go 的包级函数** ——
// 字段级可见性(field_policy_impl.go)也要做同一件事,
// 两边各写一份 ⇒ 迟早有一份的判空/错误处理不一样。
func (s *scopeService) roleID(ctx context.Context, code string) (uint64, error) {
	return lookupRoleID(ctx, s.db, code)
}

// roleCode 按角色 id 查 code
//
// ⚠️ **M162:实现挪到 role_lookup.go 的包级函数** —— 同上。
func (s *scopeService) roleCode(ctx context.Context, roleID uint64) (string, error) {
	return lookupRoleCode(ctx, s.db, roleID)
}

// knownModules 已存在的业务域集合
//
// ⚠️ **必须包含空串** —— 空串是"默认规则组":
// 某业务域没单独配规则时回落到它(见 loadRules)。
// 它不是一个业务域,但它是**唯一能配的入口**
// —— 不然管理员想给一个角色配"全局默认只看本部门",
// 就得挨个业务域配一遍。
func (s *scopeService) knownModules(ctx context.Context) (map[string]bool, error) {
	mods, err := s.ListModules(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(mods))
	for _, m := range mods {
		out[m.Module] = true
	}
	out[""] = true
	return out, nil
}

// knownModuleList 把已知业务域列成一行(给错误信息看)
func knownModuleList(known map[string]bool) string {
	keys := make([]string, 0, len(known))
	for k := range known {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 30 {
		keys = keys[:30]
	}
	return strings.Join(keys, ", ")
}

// encodeCustomValues 自定义取值 → JSON 数组字符串
//
// ⚠️ **用 JSON 不用逗号分隔** —— 取值本身可能带逗号
// (比如"杭州,南京"这种标签),逗号分隔会被切错。
func encodeCustomValues(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	b, err := json.Marshal(vals)
	if err != nil {
		return ""
	}
	return string(b)
}

// decodeCustomValues JSON 数组字符串 → 取值
func decodeCustomValues(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var vals []string
	if err := json.Unmarshal([]byte(s), &vals); err != nil {
		return nil, err
	}
	return vals, nil
}
