// role_lookup.go —— 角色 id ⇄ 角色 code 的互查 —— M162(2026-11-02)
//
// # 为什么单独抽出来
//
// 数据范围(scope_impl.go)与字段级可见性(field_policy_impl.go)都要按角色查规则,
// 两边的查法必须**一模一样** ——
// 一边按 id 一边按 code,或者一边查得到一边查不到,
// 后果都是"配了规则但运行时没生效",而且没有任何报错。
//
// ⇒ 收口成两个包级函数,两边共用。

package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/errcode"
)

// lookupRoleID 按角色 code 查 id(不存在返 0)
func lookupRoleID(ctx context.Context, db *gorm.DB, code string) (uint64, error) {
	var role model.SysRole
	err := db.WithContext(ctx).Select("id").First(&role, "code = ?", code).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	return role.ID, nil
}

// lookupRoleCode 按角色 id 查 code(不存在返空串)
func lookupRoleCode(ctx context.Context, db *gorm.DB, roleID uint64) (string, error) {
	var role model.SysRole
	err := db.WithContext(ctx).Select("code").First(&role, "id = ?", roleID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	return role.Code, nil
}
