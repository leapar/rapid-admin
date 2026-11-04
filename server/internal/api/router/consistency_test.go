// 启动一致性检查的看护 —— M115-4
//
// # 这张网守的是什么
//
// `CheckResourceConsistency` 在 main.go 里是**启动前最后一道关**:
//
//	rbac() 引用的 code 不在 sys_resource 里 → **直接 Fatal,整个服务起不来**
//
// 后果比"那条路由 403"严重得多 —— 403 只废掉一个接口,起不来是**整个平台没了**。
//
// ⚠️ **此前零覆盖** —— 这个 fail-fast 逻辑写完就没人再动过,
//	所以"加一条路由忘了在 seed.resourceList() 里登记码"这件事,
//	只有等到**部署到生产的那一次启动**才会暴露。
//
// ⇒ 加一条 rbac 路由 = 同时改两处(router.go + seed.go 的 resourceList()),
//	这条测试就是那两处的**对照表**:少改一处,这里立刻红。
//
// # 可逆探针(拆掉实现看测试会不会红)
//
//	· seed.go 的 resourceList() 里删掉任意一个 rbac 用到的 code
//	  → TestCheckResourceConsistency_AllRBACCodesSeeded 变红
//	· consistency.go 的检查 (1) 改成 return nil
//	  → 同样变红
package router

import (
	"path/filepath"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/seed"
)

// newConsistencyTestDB 造一个装了 seed 的内存库
func newConsistencyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.SysRole{},
		&model.SysResource{},
		&model.SysRoleResource{},
		&model.SysUser{},
		&model.SysUserIdentity{},
		&model.SysLoginLog{},
		&model.SysSmsCode{},
		&model.SysDict{},
		&model.SysConfig{},
		&model.SysDepartment{},
		&model.SysApprovalTemplate{},
		&model.SysApprovalStage{},
		&model.SysApprovalAssigneeRule{},
	))
	seed.SetDriver("sqlite")
	// ⚠️ **M146a 起 sqlite 也要真的同步 casbin 策略**,所以模型文件路径**不能空**
	// (空路径 → casbin.NewEnforcer("") → "open : The system cannot find the file specified."
	//  → seed.Run 直接报错 → 本用例在 M146a 之后一直是红的,直到 M147 顺手修掉)。
	require.NoError(t, seed.Run(db, consistencyCasbinModelPath(t), ""))
	return db
}

// consistencyCasbinModelPath 定位仓库里的 casbin 模型文件
//
// internal/api/router 往上三级才是 server(server/configs)
func consistencyCasbinModelPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "casbin_rbac.conf"))
	require.NoError(t, err)
	require.FileExists(t, p, "casbin model 文件必须存在")
	return p
}

// TestCheckResourceConsistency_AllRBACCodesSeeded
//
// rbac() 引用的每个 code 都必须在 sys_resource 里(否则生产启动直接 Fatal)
func TestCheckResourceConsistency_AllRBACCodesSeeded(t *testing.T) {
	db := newConsistencyTestDB(t)

	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, db, nil)

	require.NoError(t, CheckResourceConsistency(h, db),
		"rbac() 引用的 code 必须在 seed.resourceList() 里登记 —— 少登记就是生产启动 Fatal")
}

// TestCheckResourceConsistency_MissingCodeIsDetected
//
// ⚠️ **反向测试** —— 证明上面那条不是"恒绿"。
//
// 从 sys_resource 里删掉一个 rbac 用的 code,一致性检查必须报错。
// 没有这一条,上面那条可能是"检查逻辑自己坏了"也照样绿。
func TestCheckResourceConsistency_MissingCodeIsDetected(t *testing.T) {
	db := newConsistencyTestDB(t)

	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, db, nil)

	// 删掉一个 rbac 用到的 code(sms_template:list 是 M115 新加的)
	require.NoError(t, db.Where("code = ?", "sms_template:list").
		Delete(&model.SysResource{}).Error)

	err := CheckResourceConsistency(h, db)
	require.Error(t, err, "少了 code 必须报错")
	require.Contains(t, err.Error(), "sms_template:list",
		"错误信息里要点名是哪个 code 缺了")
}

// TestCheckResourceConsistency_DashboardDomainCodesSeeded
//
// 看板的**域权限码**也必须在资源池里(2026-11-01 检查 (4))
//
// ⚠️ 这些码**没经过 rbac() 闭包** —— 它们不是路由守卫,是"这块内容能不能看"
// (owner:list / house:list / …)。检查 (1) 扫不到它们。
// 缺一个 ⇒ 那个域**永远不显示**,而且**不报错**:服务照常起、页面照常开。
func TestCheckResourceConsistency_DashboardDomainCodesSeeded(t *testing.T) {
	db := newConsistencyTestDB(t)

	h := server.Default(server.WithHostPorts(":0"))
	Register(h, minimalCfg(), nil, db, nil)

	require.NoError(t, CheckResourceConsistency(h, db),
		"看板的域权限码必须在 seed.resourceList() 里登记 —— 少登记就是那个域永远不显示")
}

// TestCheckDashboardDomainCodes_MissingIsDetected
//
// ⚠️ **反向测试** —— 证明上面那条不是"恒绿"。
//
// 单独测 checkDashboardDomainCodes(而不是走整条 CheckResourceConsistency):
// 看板用的那几个码(file:list 等)**同时也被 rbac() 路由引用**,
// 走整条链路的话检查 (1) 会先炸 ⇒ 测到的不是这一条。
func TestCheckDashboardDomainCodes_MissingIsDetected(t *testing.T) {
	db := newConsistencyTestDB(t)

	var codes []string
	require.NoError(t, db.Model(&model.SysResource{}).
		Where("is_active = ?", true).Pluck("code", &codes).Error)
	known := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		known[c] = struct{}{}
	}
	require.NoError(t, checkDashboardDomainCodes(known), "资源池齐全时不该报错")

	// 删掉一个看板域用到的码
	delete(known, "file:list")
	err := checkDashboardDomainCodes(known)
	require.Error(t, err, "少了看板域的权限码必须报错")
	require.Contains(t, err.Error(), "file:list", "错误信息里要点名是哪个码缺了")
}
