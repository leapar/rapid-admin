// 动态集合的资源登记 —— 集合建好之后,菜单与权限码自动就位
//
// # 这个文件解决什么
//
// 用户设计一个业务表之后,应该"菜单自动有、接口自动有、权限自动有",
// 而不是还要他去「权限资源」页手工登记五条码、去「角色管理」页勾五个框。
//
// ⇒ 集合创建时同步做三件事:
//  1. 建五个 api 资源码:`dyn:<code>:list / read / create / update / delete`
//  2. 建一个菜单资源:`menu.dyn.<code>`(路径 `/dyn/<code>`)
//  3. 把这些资源绑给**创建者当前的角色**,并同步 casbin 策略行
//
// # 为什么绑给创建者的角色
//
// ⚠️ **不绑的话,菜单不会出现** ——
// 菜单是数据驱动的(前端按 sys_resource 里 type=menu 的行渲染),
// 资源没绑给任何角色 ⇒ 角色的可访问集合里没有它 ⇒ 侧边栏不显示。
// 而"设计了业务表却找不到入口"是最难排查的一类问题(不报错,就是没有)。
//
// ⇒ 绑给**创建这个集合的人当时所处的角色** ——
// 这正是"我设计了一个业务表,我要能用它"的语义。
// (超管本来就绕过绑定,绑不绑都看得见。)
//
// # 为什么资源码是运行时生成的,而不是 seed 里写死
//
// seed 是编译期的东西,而集合是运行期才有的。
// seed 里写死就等于"业务表必须提前设计好"—— 那就不是设计器了。
//
// ⚠️ **代价:启动期一致性检查(consistency.go)扫不到这些码** ——
// 它扫的是 `rbac()` 闭包在注册期记的账,而动态路由走的是
// `middleware.CanAccess`(资源码要等请求进来才知道)。
// 这不是漏洞:一致性检查保证的是"启动时写死的码与 seed 对得上",
// 运行时生成的码不在那个范畴里。
package service

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/dynx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/sonyflake"
)

const (
	// dynMenuPrefix 动态集合的菜单资源码前缀
	dynMenuPrefix = "menu.dyn."
	// dynMenuParentCode 动态集合菜单的父节点
	dynMenuParentCode = "menu.dyn"
	// dynMenuParentName 父节点显示名
	dynMenuParentName = "自定义业务"
	// dynMenuParentSort 父节点排序(夹在「审计中心」90 与「审批管理」100 之间)
	dynMenuParentSort = 95
	// dynDefaultIcon 默认图标
	dynDefaultIcon = "table"
)

// dynActions 动态集合的动作
//
// ⚠️ **清单的原身在 internal/pkg/dynx/actions.go** ——
// handler 侧的守卫拼资源码用的是同一份。
// 曾经两边各写一份,结果 handler 多了一个 "schema" 动作而这边没登记,
// 那条资源码永远不存在 ⇒ 谁也绑不上 ⇒ 接口对所有非超管 403 且无日志。
// ⇒ 这里只做引用,不再自己抄一份。
func dynActions() []string { return dynx.Actions() }

// dynActionLabel 动作的中文名(资源管理页显示用)
func dynActionLabel(action string) string { return dynx.ActionLabel(action) }

// dynCollectionCodes 某个集合的全部资源码
//
// ⚠️ **精确列出,不用 LIKE** —— 集合编码里允许下划线(`my_code`),
// 而 SQL 的 `_` 在 LIKE 里是"任意单字符" ⇒ `dyn:my_code:%` 会连
// `dyn:myXcode:list` 一起命中,误伤别的集合。
func dynCollectionCodes(code string) []string {
	acts := dynActions()
	out := make([]string, 0, len(acts)+1)
	for _, a := range acts {
		out = append(out, dynx.RecordCode(code, a))
	}
	out = append(out, dynMenuPrefix+code)
	return out
}

// registerCollectionResources 建资源 + 授权
//
// ⚠️ **参数是 bindRole 而不是 *Caller** —— 资源绑给"哪个角色"由调用方决定:
// 在线创建时是创建者当前的角色,启动期播种内置集合时是各域的业务角色
// (播种没有登录用户,见 dyn_builtin.go)。
func (s *dynService) registerCollectionResources(ctx context.Context, bindRole string, col *model.SysDynCollection) error {
	parentID, err := s.ensureDynParentMenu(ctx)
	if err != nil {
		return err
	}
	ids := make([]uint64, 0, len(dynActions())+1)
	for _, action := range dynActions() {
		id, err := s.upsertResource(ctx, &model.SysResource{
			Code: dynx.RecordCode(col.Code, action),
			Name: col.Name + " · " + dynActionLabel(action),
			Type: "api",
		})
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	icon := strings.TrimSpace(col.Icon)
	if icon == "" {
		icon = dynDefaultIcon
	}
	menuID, err := s.upsertResource(ctx, &model.SysResource{
		Code:     dynMenuPrefix + col.Code,
		Name:     col.Name,
		Type:     "menu",
		Path:     "/dyn/" + col.Code,
		Icon:     icon,
		Sort:     col.Sort,
		ParentID: parentID,
	})
	if err != nil {
		return err
	}
	ids = append(ids, menuID)
	// 父菜单也要绑 —— 否则子菜单找不到父节点,前端会把它当顶级菜单浮出去
	parent, err := s.getResourceByCode(ctx, dynMenuParentCode)
	if err == nil && parent != nil {
		ids = append(ids, parent.ID)
	}
	return s.bindResourcesToRole(ctx, bindRole, ids)
}

// disableCollectionResources 停用集合的资源(删集合 / 停用集合时调)
//
// ⚠️ **停用资源必须同步 casbin 策略行,否则执行层和显示层分叉** ——
// sys_resource.is_active=false 只让**菜单消失**(前端按 is_active 过滤),
// casbin_rule 里的 `dyn:<code>:list` 还在 ⇒ 直接打接口仍然放行。
// 那就变成"菜单没了但接口还能调"的反向不一致(和 M149 修的是同一类病)。
//
// ⇒ 找出绑了这些资源的角色,逐个重算策略行。
func (s *dynService) disableCollectionResources(ctx context.Context, col *model.SysDynCollection) error {
	codes := dynCollectionCodes(col.Code)
	var resources []model.SysResource
	if err := s.db.WithContext(ctx).Where("code IN ?", codes).Find(&resources).Error; err != nil {
		return errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	if err := s.db.WithContext(ctx).Model(&model.SysResource{}).
		Where("code IN ?", codes).
		Updates(map[string]any{"is_active": false}).Error; err != nil {
		return errcode.New(errcode.Internal, "停用资源失败").WithDetail(err.Error())
	}
	if len(resources) == 0 {
		return nil
	}
	resIDs := make([]uint64, 0, len(resources))
	for _, r := range resources {
		resIDs = append(resIDs, r.ID)
	}
	var bindings []model.SysRoleResource
	if err := s.db.WithContext(ctx).Where("resource_id IN ?", resIDs).Find(&bindings).Error; err != nil {
		return errcode.New(errcode.Internal, "查询角色绑定失败").WithDetail(err.Error())
	}
	roleIDs := make([]uint64, 0, len(bindings))
	for _, b := range bindings {
		roleIDs = append(roleIDs, b.RoleID)
	}
	if len(roleIDs) == 0 {
		return nil
	}
	var roles []model.SysRole
	if err := s.db.WithContext(ctx).Where("id IN ?", roleIDs).Find(&roles).Error; err != nil {
		return errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	for _, role := range roles {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return syncRolePolicies(tx, role.Code, role.ID)
		}); err != nil {
			return errcode.New(errcode.Internal, "同步权限策略失败").WithDetail(err.Error())
		}
	}
	refreshCasbinPolicies()
	return nil
}

// ensureDynParentMenu 保证父菜单存在,返回它的 id
func (s *dynService) ensureDynParentMenu(ctx context.Context) (uint64, error) {
	existing, err := s.getResourceByCode(ctx, dynMenuParentCode)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.ID, nil
	}
	parent := &model.SysResource{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		Code:      dynMenuParentCode,
		Name:      dynMenuParentName,
		Type:      "menu",
		Icon:      dynDefaultIcon,
		Sort:      dynMenuParentSort,
		IsActive:  true,
	}
	if err := s.db.WithContext(ctx).Create(parent).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建菜单失败").WithDetail(err.Error())
	}
	return parent.ID, nil
}

// getResourceByCode 按 code 取资源(软删的当没有)
func (s *dynService) getResourceByCode(ctx context.Context, code string) (*model.SysResource, error) {
	var r model.SysResource
	err := s.db.WithContext(ctx).Where("code = ?", code).First(&r).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	return &r, nil
}

// upsertResource 建资源(已存在则更新并重新启用)
func (s *dynService) upsertResource(ctx context.Context, r *model.SysResource) (uint64, error) {
	existing, err := s.getResourceByCode(ctx, r.Code)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		updates := map[string]any{
			"name":      r.Name,
			"type":      r.Type,
			"path":      r.Path,
			"icon":      r.Icon,
			"sort":      r.Sort,
			"parent_id": r.ParentID,
			"is_active": true,
		}
		if err := s.db.WithContext(ctx).Model(&model.SysResource{}).
			Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return 0, errcode.New(errcode.Internal, "更新资源失败").WithDetail(err.Error())
		}
		return existing.ID, nil
	}
	r.BaseModel = model.BaseModel{ID: sonyflake.Next()}
	r.IsActive = true
	if err := s.db.WithContext(ctx).Create(r).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建资源失败").WithDetail(err.Error())
	}
	return r.ID, nil
}

// bindResourcesToRole 把资源绑给某个角色,并同步 casbin 策略行
//
// ⚠️ **绑完必须 syncRolePolicies** ——
// 绑定表(sys_role_resource)只是"给界面看的",
// 真正放行的是 casbin 策略行。不同步的话:
// 管理员勾了权限 → 菜单出现了 → 点进去 403。
// 而且**没有任何报错** —— casbin 只是"没匹配到策略行",正常返回 false。
func (s *dynService) bindResourcesToRole(ctx context.Context, roleCode string, resourceIDs []uint64) error {
	if roleCode == "" || len(resourceIDs) == 0 {
		return nil
	}
	var role model.SysRole
	if err := s.db.WithContext(ctx).Where("code = ?", roleCode).First(&role).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 角色不存在(创建者的身份已经被删了)⇒ 不绑,但不算失败
			return nil
		}
		return errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, rid := range resourceIDs {
			rr := &model.SysRoleResource{
				BaseModel:  model.BaseModel{ID: sonyflake.Next()},
				RoleID:     role.ID,
				ResourceID: rid,
			}
			// ⚠️ DoNothing 而不是报错 —— 重复绑定是幂等的,
			// 而唯一索引 uk_rr(role_id, resource_id, project_id) 会让第二次插入失败。
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(rr).Error; err != nil {
				return fmt.Errorf("bind resource %d: %w", rid, err)
			}
		}
		return syncRolePolicies(tx, role.Code, role.ID)
	})
	if err != nil {
		return errcode.New(errcode.Internal, "绑定资源失败").WithDetail(err.Error())
	}
	// 事务提交后再刷内存副本(事务里刷到的是还没提交的库状态)
	refreshCasbinPolicies()
	return nil
}
