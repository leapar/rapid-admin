// ============================================================================
// 菜单↔接口绑定一致性守门网 测试(M170,2026-11-02)
//
// 核心是**自验证**:守门网跑在真实 seed 出来的 13 角色 × 全资源矩阵上 ——
//   - 绿 = 当前 bindingRules 没有任何一个「菜单给了、接口没给」的洞;
//   - 若 menu_api_map.go 里登记的码**要求过头**(页面其实不调),
//     这里会以「角色 X 菜单 Y 缺 Z」点名红 —— 误报自己长脚来报。
package seed

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"rapid-admin/internal/model"
)

// 全量 seed 后守门网必须绿:当前矩阵无死菜单
func TestCheckMenuAPIWiring_GreenOnSeededMatrix(t *testing.T) {
	db := newSeededMatrixDB(t)
	require.NoError(t, CheckMenuAPIWiring(db))
}

// 可逆探针:拆掉 M169 给平台运营员补的 audit 码 → 守门网必须红,
// 且报错要点名角色 / 菜单 / 缺的码 —— 它红,就证明网真能接住 #18 这类洞
func TestCheckMenuAPIWiring_RedWhenAuditApiRevoked(t *testing.T) {
	db := newSeededMatrixDB(t)

	role := model.RolePlatformOps
	var r model.SysRole
	require.NoError(t, db.Where("code = ?", role).First(&r).Error)

	var probeTargets []model.SysResource
	require.NoError(t, db.Where("code IN ?", []string{
		"audit:operation:list", "audit:data:list", "audit:login:list",
	}).Find(&probeTargets).Error)
	require.Len(t, probeTargets, 3)

	for _, res := range probeTargets {
		require.NoError(t, db.
			Where("role_id = ? AND resource_id = ?", r.ID, res.ID).
			Delete(&model.SysRoleResource{}).Error)
	}

	err := CheckMenuAPIWiring(db)
	require.Error(t, err, "拆掉 audit 三码后守门网必须红(否则网是假的)")
	msg := err.Error()
	assert.Contains(t, msg, string(role))
	assert.Contains(t, msg, "menu.audit.operation")
	assert.Contains(t, msg, "audit:operation:list")
}

// 可逆探针:把 map 里某菜单的必调码人为抽走 → 红(证明检查逻辑真的在比对,
// 不是永远绿)。用 dict:list 最尖锐 —— 它在 authBase 里全员都绑,
// 若守门网退化成"不比对",这条探针测不出来;若比对在,抽走必红。
func TestCheckMenuAPIWiring_RedWhenRequiredCodeMissing(t *testing.T) {
	db := newSeededMatrixDB(t)

	var dictCode model.SysResource
	require.NoError(t, db.Where("code = ?", "dict:list").First(&dictCode).Error)
	require.NoError(t, db.Where("resource_id = ?", dictCode.ID).
		Delete(&model.SysRoleResource{}).Error)

	err := CheckMenuAPIWiring(db)
	require.Error(t, err)
	// 至少 2 个带菜单的角色受影响(系统字典页绑定方);报错里要出现缺的码
	assert.Contains(t, err.Error(), "dict:list")
	assert.Contains(t, err.Error(), "menu.system.dict")
}

// 报错文案必须带修法指引(新页面要回 menu_api_map.go 登记)
func TestCheckMenuAPIWiring_ErrorMessageHasFixHint(t *testing.T) {
	db := newSeededMatrixDB(t)

	var dictCode model.SysResource
	require.NoError(t, db.Where("code = ?", "dict:list").First(&dictCode).Error)
	require.NoError(t, db.Where("resource_id = ?", dictCode.ID).
		Delete(&model.SysRoleResource{}).Error)

	err := CheckMenuAPIWiring(db)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "menu_api_map.go"),
		"报错要指引到登记表,实际:%v", err)
}
