// 绑定表的"全量替换"语义 —— 框架层,所有绑多对多的地方都必须走这里。
package service

import "gorm.io/gorm"

// clearBindings 清空一张绑定表里匹配 cond 的行,**硬删**。
//
// # 为什么必须硬删(2026-10-21,实测复现)
//
// BaseModel 带 gorm.DeletedAt,所以 tx.Delete() 默认是**软删**
// (UPDATE ... SET deleted_at = now())。而绑定表的唯一索引只覆盖业务列,
// 不含 deleted_at —— 于是同一个绑定关系第二次保存时撞 UNIQUE 约束,
// 整个事务回滚、接口返 500。
//
// 实测两条路径,第一次 0、第二三次 50000:
//
//	PATCH /system/users/:id {"department_ids":[...]}   部门:uk_ud(user_id,dept_id)
//	POST  /system/roles/:id/resources                  权限:uk_rr(role_id,resource_id,project_id)
//
// 有意思的是这个 500 是被上一个 commit 修好字段名之后才露出来的:
// 在此之前前端发的 key 后端根本不认,收不到任何 ID,反而不会 INSERT —
// 所以它一直是"每次保存都清空权限"而不是报错。两个 bug 叠在一起,
// 修掉上面那个才看得见下面这个。
//
// # 为什么绑定表就该硬删,而业务主表不该
//
// 绑定行没有独立生命:它的存在完全由两端的"全量替换"操作决定,
// 不存在"恢复某条绑定"这种需求。留 deleted_at 墓碑有三个坏处:
//   - 挡唯一索引(上面的 500)
//   - 每次保存只增不减,绑定表无限膨胀
//   - 读的时候还得记得 GORM 默认过滤,漏一处就查出已解绑的数据
//
// 业务主表(House / User / Role / Owner...)的删除**仍然要软删** ——
// 那个是真业务事件,要能查能恢复。别把这个函数用到主表上。
//
// # 用法
//
//	tx clearBindings(tx, &model.SysUserDepartment{}, "user_id = ?", userID)
//
// cond/args 就是原 Where 的参数,原样搬过来。
func clearBindings(tx *gorm.DB, binding any, cond string, args ...any) error {
	return tx.Unscoped().Where(cond, args...).Delete(binding).Error
}