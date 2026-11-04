// 角色绑定 → casbin 策略行的**运行时同步**(M147-f)
//
// # 这张网守的是什么
//
// casbin_rule 是「角色-资源绑定矩阵」的**物化副本** —— 它只在
// seed / cmd/migrate 时被整体重建一次。运行时管理员在界面上:
//
//	· 建角色(CreateRole)
//	· 改角色的权限分配(AssignResources)
//	· 删角色(DeleteRole)
//
// 这三个动作改的是 sys_role / sys_role_resource,**casbin_rule 不会跟着动**。
//
// ⇒ 后端按 casbin 判、前端按绑定表显示,两套真相分叉:
//
//	管理员勾了权限 → 菜单出现了 → 点进去 403。
//	而且**没有任何报错**:casbin 只是"没匹配到策略行",正常返回 false。
//
// # 为什么同步要写进调用方的 tx
//
// 绑定改动与策略行改动必须**同一个事务**。反过来(先提交绑定、再单独写策略)
// 的话,中间崩了就是"绑定改了、策略没改" —— 失败方向是"放行",不能要。
//
// # 为什么写完还要 Reload
//
// enforcer 的内存模型**不会自动读库**。行写进 casbin_rule 了,内存里还是旧的
// ⇒ 强制执行层看到的还是旧权限。刷新必须在**事务提交之后**做
// (事务里刷到的是还没提交的库状态)。
package service

import (
	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/casbinx"
	"rapid-admin/internal/pkg/logger"
)

// syncRolePolicies 把角色当前的绑定同步到 casbin 强制执行层
//
// ⚠️ **必须在调用方的 tx 里调** —— 绑定改动与策略行改动同一事务。
func syncRolePolicies(tx *gorm.DB, roleCode string, roleID uint64) error {
	codes, err := roleResourceCodes(tx, roleID)
	if err != nil {
		return err
	}
	return casbinx.SyncRolePolicies(tx, roleCode, codes)
}

// roleResourceCodes 取某角色当前绑定的资源码(策略行的 obj)
//
// ⚠️ **口径必须与 seed.buildRolePolicies 一致** —— 否则"运行时同步"与
// "migrate 整体重建"会算出两套策略集合,跑一次 migrate 权限就变了。
// 一致的两条:
//
//	① 只取 is_active 的资源(停用的资源不该继续放行);
//	② 软删的行不算数(绑定表被硬删,资源表是软删)。
//
// ⚠️ 用模型查询(而不是 Table()+JOIN)是**为了拿到软删 scope**:
//
//	Table() 查询不带 scope,软删掉的资源会被算进来 ⇒ 停用的资源又活了。
func roleResourceCodes(tx *gorm.DB, roleID uint64) ([]string, error) {
	var bindings []model.SysRoleResource
	if err := tx.Where("role_id = ?", roleID).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, nil
	}
	resIDs := make([]uint64, 0, len(bindings))
	for _, b := range bindings {
		resIDs = append(resIDs, b.ResourceID)
	}
	var resources []model.SysResource
	if err := tx.Where("id IN ? AND is_active = ?", resIDs, true).Find(&resources).Error; err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(resources))
	for _, r := range resources {
		codes = append(codes, r.Code)
	}
	return codes, nil
}

// refreshCasbinPolicies 事务提交后刷新 enforcer 的内存副本
//
// ⚠️ **必须在事务提交之后调** —— 事务里刷的是还没提交的库状态。
//
// ⚠️ 刷新失败只 warn 不报错:库里的行已经是最新的(那是真相),
// 内存副本旧只是"这个进程还没看到",下个进程启动会 LoadPolicy 读到。
// 这里返回错误的话,管理员会看到"保存失败"但其实**保存成功了**。
func refreshCasbinPolicies() {
	if err := casbinx.ReloadIfInitialized(); err != nil {
		logger.L().Warn("casbin policy reload failed after role binding change",
			zap.Error(err))
	}
}
