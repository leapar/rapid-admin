// ============================================================================
// 菜单 ↔ 接口 绑定一致性守门网(§1.1 #17 第 ② 步,M170,2026-11-02)
//
// # 守的洞
//
// `sys_resource.type ∈ {menu, button, api}` 是三份**独立**码表,角色↔资源绑定
// (`bindingRules`)是手写的 —— 「菜单给了、接口没给」在两个方向上都**不报错**:
//
//   - 菜单给了、接口没给 ⇒ 侧边栏点得进去、一进去就 403(像功能坏了)
//   - 接口给了、菜单没给 ⇒ 界面少一项(像"这角色就该这么少")
//
// 本守门网管**第一个方向**(有 403 症状的那个):角色绑了某个菜单,就必须绑
// 该菜单页面**加载时必调**的 api 码。反方向(接口给了菜单没给)是显示层语义,
// 不在本网范围。
//
// # 这一关堵住过的真实洞(全是靠人偶然发现的,守门网之后机器堵)
//
//   - M93:`dict:list` 只绑 2 个角色 ⇒ 10 个角色下拉全空
//   - 2026-11-01:`dashboard:read` 只有 4 个角色有 ⇒ 9/13 点开工作台 4 接口全 403
//   - M154:用户管理员三个死菜单(config / job / smsTemplate)
//   - M169(§1.1 #18):平台运营负责人四个审计菜单零接口权
//
// # 数据从哪来(menuRequiredAPIs,menu_api_map.go)
//
// 叶子菜单 → 页面加载时必调的 api 码**最小集**,逐页对着前端代码核出来的
// (useTable 的 fetch + 挂载即发的请求;按钮后面的动作不算 —— 那是 button 码的事)。
//
// ⚠️ **它是手写的第四份表,这是有意的妥协** ——
// 彻底的解法是 §1.1 #17 第 ③ 步「能力清单」(menu/button/api 从一份能力定义投影),
// 那会改授权粒度、要重推整个 13 角色 × 377 资源的绑定矩阵,是另一轮的事。
// 在那之前,这张表就是「菜单↔接口」唯一一份机器可验的关系;
// 新增页面必须在这里登记(见 CheckMenuAPIWiring 的报错指引)。
//
// ⚠️ **只要求"最小集"** —— 页面用 usePermission 按码隐藏按钮,缺某个动作码
// 页面照样能开;只有"加载即调、不调就没数据"的码才算必调。
// 判宽不判严:宁可漏报,不许误报(误报 = 合法配置启动失败,守门网就没人信了)。
package seed

import (
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// CheckMenuAPIWiring 菜单↔接口绑定一致性校验。
//
//   - 任何 caller(Run / cmd/migrate / 测试)收到 error 应阻断
//   - 与 SeedDictGuard 同一挂点模式:seed 末尾调用,缺账即失败
func CheckMenuAPIWiring(db *gorm.DB) error {
	// 1. 角色 → 绑定码集合(三步查,与 seed_test.go 的 roleResourceCodes 同一口径:
	//    全程走 model 查询,让 GORM 的软删除过滤在每张表上都生效)
	var roles []model.SysRole
	if err := db.Find(&roles).Error; err != nil {
		return fmt.Errorf("menu api guard: query roles: %w", err)
	}
	bound := make(map[string]map[string]struct{}, len(roles))
	for _, r := range roles {
		var bindings []model.SysRoleResource
		if err := db.Where("role_id = ?", r.ID).Find(&bindings).Error; err != nil {
			return fmt.Errorf("menu api guard: query bindings of %s: %w", r.Code, err)
		}
		if len(bindings) == 0 {
			continue
		}
		ids := make([]uint64, 0, len(bindings))
		for _, b := range bindings {
			ids = append(ids, b.ResourceID)
		}
		var resources []model.SysResource
		if err := db.Where("id IN ?", ids).Find(&resources).Error; err != nil {
			return fmt.Errorf("menu api guard: query resources of %s: %w", r.Code, err)
		}
		codes := make(map[string]struct{}, len(resources))
		for _, res := range resources {
			codes[res.Code] = struct{}{}
		}
		bound[r.Code] = codes
	}

	// 2. 逐角色 × 逐菜单核对(角色名排序,报错顺序稳定,diff 友好)
	roleCodes := make([]string, 0, len(bound))
	for rc := range bound {
		roleCodes = append(roleCodes, rc)
	}
	sort.Strings(roleCodes)

	var problems []string
	for _, rc := range roleCodes {
		codes := bound[rc]
		menuCodes := make([]string, 0, len(codes))
		for c := range codes {
			if strings.HasPrefix(c, "menu.") {
				menuCodes = append(menuCodes, c)
			}
		}
		sort.Strings(menuCodes)
		for _, mc := range menuCodes {
			req, ok := menuRequiredAPIs[mc]
			if !ok {
				continue // 父级菜单 / 未登记的页面 —— 不猜
			}
			var missing []string
			for _, code := range req {
				if _, has := codes[code]; !has {
					missing = append(missing, code)
				}
			}
			if len(missing) > 0 {
				problems = append(problems,
					fmt.Sprintf("   · 角色 %s 绑了菜单 %s,但缺页面必调的接口码: %s", rc, mc, strings.Join(missing, ", ")))
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf(
			"❌ 菜单↔接口绑定不一致(%d 处)——「菜单给了、接口没给」= 点进去 403:\n%s\n"+
				"   → 修法二选一:给角色补码(seed.go bindingRules),或撤掉菜单(revokeRoleResources)\n"+
				"   → 若是新页面:先在 seed/menu_api_map.go 登记该菜单必调的接口码,再配绑定",
			len(problems), strings.Join(problems, "\n"))
	}
	return nil
}
