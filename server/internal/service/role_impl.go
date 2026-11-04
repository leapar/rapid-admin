// RoleService 真实实现 —— 角色 / 资源 / 部门 / 字典
// 文档 §6.1 #3
//
// 设计要点:
//  1. 角色与资源: 多对多,经 sys_role_resource 关联
//  2. 角色: code 全局唯一;is_builtin=true 不可删
//  3. 资源: code 全局唯一;支持层级(parent_id);type=menu/button/api
//  4. 部门: 支持层级(parent_id);code 唯一
//  5. 字典: groupCode+itemKey 联合唯一(实际唯一性走 uniqueIndex)
package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/casbinx"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/permnotify"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/seed"
)

// roleService 真实实现
type roleService struct {
	db *gorm.DB
	// dictPublisher 字典变更广播(M94)。
	//
	// ⚠️ **允许为 nil**(单测里不装)—— 但 nil 意味着「字典改了没人知道」,
	// 生产装配期**必须给真实现**(见 cmd/server/main.go)。
	// 判 nil 的是这里,不是调用方:调用方只管发。
	dictPublisher DictPublisher
	// dictCache 字典缓存(M95)。
	//
	// ⚠️ **允许为 nil**(单测里不装)—— 退化成每次回源,不影响正确性。
	// 生产装配期给真实现(见 cmd/server/main.go)。
	dictCache *DictCache
}

// NewRoleService 构造
//
// ⚠️ **dictPublisher 必须给真实现**(M94)—— 传 nil 的话字典改了没有任何
// 广播,前端 `stores/dict.ts` 的 30 分钟缓存会让「改了不生效」成为默认行为,
// 而且**没有任何报错**(跟 M88 审批 publisher 那条教训一模一样)。
//
// ⚠️ **dictCache 传 nil 只是"没有缓存"**(M95)—— 不影响正确性,
// 但生产装配期该给真实现:字典是每个页面下拉都要读的数据,
// 不缓存就是每次都打库。
func NewRoleService(db *gorm.DB, dictPublisher DictPublisher, dictCache *DictCache) RoleService {
	return &roleService{db: db, dictPublisher: dictPublisher, dictCache: dictCache}
}

// 公共分页规整
func (s *roleService) normPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// ===== 角色 =====

func (s *roleService) ListRoles(ctx context.Context, q RoleListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysRole{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("code LIKE ? OR name LIKE ? OR description LIKE ?", like, like, like)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询角色总数失败").WithDetail(err.Error())
	}
	var rows []model.SysRole
	if err := tx.Order("sort ASC, id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	// 投影 DTO(2026-10-17):snake_case + 派生 enabled + 透传 type
	items := make([]RoleListItem, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		typ := r.Type
		if typ == "" {
			typ = model.RoleTypeBusiness
		}
		items = append(items, RoleListItem{
			ID:          r.ID,
			Code:        r.Code,
			Name:        r.Name,
			Description: r.Description,
			Type:        typ,
			Sort:        r.Sort,
			IsBuiltin:   r.IsBuiltin,
			Enabled:     r.IsActive,
			CreatedAt:   r.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return &model.PageResponse{List: items, Total: total, Page: page, Size: size}, nil
}

func (s *roleService) GetRole(ctx context.Context, id uint64) (*model.SysRole, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var r model.SysRole
	if err := s.db.WithContext(ctx).First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "角色不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	return &r, nil
}

func (s *roleService) CreateRole(ctx context.Context, caller *Caller, role *model.SysRole) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if err := MustNoEmpty("code", role.Code); err != nil {
		return 0, err
	}
	if err := MustNoEmpty("name", role.Name); err != nil {
		return 0, err
	}
	// 唯一性
	//
	// ⚠️ **必须用 Unscoped 查** —— 软删的角色**仍然占着 code**
	// (uniqueIndex 不看 deleted_at)。用默认 scope 查的话:
	//   · 软删的同 code 角色查不到 → 预检放行 → INSERT 撞 DB 约束;
	//   · 报出来的是裸 SQL 错误 "UNIQUE constraint failed: sys_role.code"
	//     (500 + 把 SQL 漏给前端);
	//   · 而且这个 code **再也建不了** —— "删掉 → 再建同 code"是死路。
	//
	// 处置:活的同 code → 409(与 user 服务的"用户名已存在"同一口径);
	// 软删的同 code → **复活**它(删除时绑定/身份已硬删,等于一个全新角色)。
	var dup model.SysRole
	dupErr := s.db.WithContext(ctx).Unscoped().Where("code = ?", role.Code).First(&dup).Error
	revive := false
	switch {
	case dupErr == nil && dup.DeletedAt.Valid:
		revive = true
	case dupErr == nil:
		return 0, errcode.New(errcode.Conflict, "角色 code 已存在")
	case !errors.Is(dupErr, gorm.ErrRecordNotFound):
		return 0, errcode.New(errcode.Internal, "校验角色失败").WithDetail(dupErr.Error())
	}
	role.ID = sonyflake.Next()
	role.IsActive = true
	if role.Sort == 0 {
		role.Sort = 999
	}
	if role.Type == "" {
		role.Type = "business"
	}
	// ⚠️ M147:建角色时把"自服务"资源码一起绑上(同一个事务)。
	//
	// 自服务能力(auth:me / resource:accessible / dict:list …)是**每个
	// 登录角色**都必须有的:少了 auth:me,这个身份切过去之后前端连"我是谁"
	// 都拿不到(整个后台是空的);少了 resource:accessible,菜单和按钮全空。
	//
	// ⚠️ 以前只有 seed 给 13 个**内置**角色绑 —— 业务上自建的角色拿不到。
	// M147 把通配策略行删掉之后,那些角色**整个不可用**(M147 之前靠通配行
	// 兜着,所以没人发现)。
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if revive {
			// 复活:清 deleted_at + 覆盖字段。
			//
			// ⚠️ 它等于一个**全新**角色 —— 删除时绑定(sys_role_resource)
			// 与身份(sys_user_identity)都已硬删,casbin 策略行也删了。
			// ⚠️ 唯一还留着的是审批规则里的 role_id(sysc_approval_assignee_rule)
			// —— 角色 ID 复用 ⇒ 那些规则会重新指向这个"新"角色。
			// 比留一条指向已删角色的孤儿规则更合理。
			updates := map[string]any{
				"name":        role.Name,
				"description": role.Description,
				"type":        role.Type,
				"sort":        role.Sort,
				"is_active":   true,
				"is_builtin":  false,
				"deleted_at":  nil,
			}
			if err := tx.Unscoped().Model(&model.SysRole{}).
				Where("id = ?", dup.ID).Updates(updates).Error; err != nil {
				return err
			}
			role.ID = dup.ID
		} else if err := tx.Create(role).Error; err != nil {
			return err
		}
		if err := bindAuthBaseResources(tx, role.ID); err != nil {
			return err
		}
		// ⚠️ M147-f:绑定改了,casbin 的策略行必须**同一个事务**里跟上 ——
		//
		// 不跟上的话,这个角色在强制执行层是"零权限":界面上菜单能看见
		// (前端按绑定表显示),接口却一律 403(后端按 casbin 判),
		// 而且**没有任何报错** —— casbin 只是"没匹配到策略行"。
		return syncRolePolicies(tx, role.Code, role.ID)
	}); err != nil {
		return 0, errcode.New(errcode.Internal, "创建角色失败").WithDetail(err.Error())
	}
	// 事务提交后再刷内存副本(事务里刷到的是还没提交的状态)
	refreshCasbinPolicies()
	return role.ID, nil
}

func (s *roleService) UpdateRole(ctx context.Context, caller *Caller, id uint64, role *model.SysRole) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var old model.SysRole
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "角色不存在")
		}
		return errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	updates := map[string]any{}
	if role.Name != "" {
		updates["name"] = role.Name
	}
	if role.Description != "" {
		updates["description"] = role.Description
	}
	if role.Sort > 0 {
		updates["sort"] = role.Sort
	}
	// §1.1 #16 P1 —— 角色停用级联停用所有该角色身份
	//   - 角色被关,所有挂此角色 code 的 identity 也要关(否则后端通,前端看不到)
	//   - 重启用(false → true)不级联:避免把已被人为关停的 identity 自动恢复
	//   - 重启用场景由 admin 在用户管理页逐个启用 identity
	identityCascade := false
	if role.IsActive != old.IsActive {
		updates["is_active"] = role.IsActive
		// 只在"启用 → 停用"时级联
		if old.IsActive && !role.IsActive {
			identityCascade = true
		}
	}
	if len(updates) == 0 {
		return nil
	}
	// 事务:更新角色 + 级联停用 identity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&old).Updates(updates).Error; err != nil {
			return err
		}
		if identityCascade {
			res := tx.Model(&model.SysUserIdentity{}).
				Where("role_code = ? AND is_active = ?", old.Code, true).
				Update("is_active", false)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				logger.L().Info("role deactivation cascaded to identities",
					zap.String("role_code", old.Code),
					zap.Int64("identity_count", res.RowsAffected))
			}
		}
		return nil
	})
	if err != nil {
		return errcode.New(errcode.Internal, "更新角色失败").WithDetail(err.Error())
	}
	// 角色状态变化 → 广播"权限失效"
	//   - 停用:被停用 identity 的用户在线的话立即 re-fetch 会发现没权限
	//   - 重启用:虽然不级联恢复 identity,但若有用户主动开关 principal 缓存可感知
	if role.IsActive != old.IsActive {
		permnotify.PublishInvalidateAll(ctx, caller.UserID, "role_active_changed")
	}
	return nil
}

func (s *roleService) DeleteRole(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var r model.SysRole
	if err := s.db.WithContext(ctx).First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "角色不存在")
		}
		return errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	if r.IsBuiltin {
		return errcode.New(errcode.Forbidden, "内置角色不可删除")
	}
	// 同事务:删角色 + 删关联
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := clearBindings(tx, &model.SysRoleResource{}, "role_id = ?", id); err != nil {
			return err
		}
		if err := clearBindings(tx, &model.SysUserIdentity{}, "role_code = ?", r.Code); err != nil {
			return err
		}
		if err := tx.Delete(&r).Error; err != nil {
			return err
		}
		// ⚠️ M147-f:角色删了,casbin 的策略行必须一起删 ——
		//
		// 不删的话,同 code 的新角色会**继承旧角色的全部权限**
		// (策略行是按 role:<code> 存的,跟角色行没关系)。
		if err := casbinx.RemoveRolePolicies(tx, r.Code); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return errcode.New(errcode.Internal, "删除角色失败").WithDetail(err.Error())
	}
	// 事务提交后再刷内存副本
	refreshCasbinPolicies()
	return nil
}

func (s *roleService) AssignResources(ctx context.Context, caller *Caller, roleID uint64, resourceIDs []uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if roleID == 0 {
		return errcode.New(errcode.BadRequest, "roleId 不能为空")
	}
	// 角色存在性
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.SysRole{}).Where("id = ?", roleID).Count(&cnt).Error; err != nil {
		return errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	if cnt == 0 {
		return errcode.New(errcode.NotFound, "角色不存在")
	}
	// 策略行的 sub 是 role:<code> —— 同步时要用它,先取出来
	var targetRole model.SysRole
	if err := s.db.WithContext(ctx).Select("id, code").First(&targetRole, roleID).Error; err != nil {
		return errcode.New(errcode.Internal, "查询角色失败").WithDetail(err.Error())
	}
	roleCode := targetRole.Code
	// 资源存在性
	if len(resourceIDs) > 0 {
		var rcnt int64
		if err := s.db.WithContext(ctx).Model(&model.SysResource{}).
			Where("id IN ?", resourceIDs).Count(&rcnt).Error; err != nil {
			return errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
		}
		if int(rcnt) != len(resourceIDs) {
			return errcode.New(errcode.BadRequest, "部分 resourceId 不存在")
		}
	}
	// ⚠️ M147:自服务资源码**并进**最终绑定集 —— 不许被「分配权限」抹掉。
	//
	// AssignResources 是**覆盖式**(事务里先删光旧绑定再写新的),而自服务
	// 能力是每个登录角色的必备项 ⇒ 被抹掉 = 那个角色的身份整个不可用
	// (切过去 /auth/me 就 403),而管理员在界面上看不出任何异常。
	//
	// ⚠️ 界面上的表现:这几个码取消勾选后保存,**保存完又回来了**。
	// 这是有意的 —— 它们是必备项,不是可选项。
	resourceIDs = mergeUint64(resourceIDs, authBaseResourceIDs(s.db.WithContext(ctx)))

	// 事务:清旧 + 写新
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 硬删而不是 tx.Delete() —— 见 clearBindings 的注释:软删会撞 uk_rr,
		// 表现为"分配权限保存第一次成功、第二次 500"
		if err := clearBindings(tx, &model.SysRoleResource{}, "role_id = ?", roleID); err != nil {
			return err
		}
		for _, rid := range resourceIDs {
			rr := &model.SysRoleResource{
				BaseModel:  model.BaseModel{ID: sonyflake.Next()},
				RoleID:     roleID,
				ResourceID: rid,
			}
			if err := tx.Create(rr).Error; err != nil {
				return err
			}
		}
		// ⚠️ M147-f:绑定是**覆盖式**的 ⇒ 策略行也要整体重写。
		//
		// 不重写的话:管理员在这里**取消勾选**的权限,策略行还在 ⇒
		// 界面上的权限已经收回,接口还能调(前端按绑定表显示、后端按 casbin 判)。
		return syncRolePolicies(tx, roleCode, roleID)
	})
	if err != nil {
		return errcode.New(errcode.Internal, "分配资源失败").WithDetail(err.Error())
	}
	// 事务提交后再刷内存副本
	refreshCasbinPolicies()
	// §2.2 #9 P1 —— 角色资源变化,广播"权限失效",所有在线前端收到后 re-fetch
	permnotify.PublishInvalidateAll(ctx, caller.UserID, "role_resource_changed")
	return nil
}

// authBaseResourceIDs 把"自服务"资源码解析成 sys_resource.id(只取库里真有的)
//
// ⚠️ **清单的单一来源在 seed 包**(`seed.AuthBaseResourceCodes()`)——
//
//	service 层不自己抄一份:抄的那份和真的那份**一起**少一个码时,
//	谁都不会发现(测试照抄的是同一份 ⇒ 恒绿)。
//
// ⚠️ 用 `Table()` 不带软删除 scope —— 与 seed.assignRoleResources 一致。
func authBaseResourceIDs(tx *gorm.DB) []uint64 {
	codes := seed.AuthBaseResourceCodes()
	if len(codes) == 0 {
		return nil
	}
	var ids []uint64
	if err := tx.Table("sys_resource").Where("code IN ?", codes).Pluck("id", &ids).Error; err != nil {
		// ⚠️ 查不到就当没有 —— 建角色不能因为"自服务码没登记"就失败
		// (资源池里少一个码 = 那一个能力没有,不是整个建角色流程该报错)
		return nil
	}
	return ids
}

// bindAuthBaseResources 给角色绑上"自服务"资源码(只补缺的)
//
// ⚠️ **为什么建角色时就要绑**:角色的资源绑定只有两个写入口 ——
//
//	「建角色」和「分配权限」。两个入口都得保证这份清单在,
//	否则自建的角色一建出来就是"没有 auth:me"的空壳。
func bindAuthBaseResources(tx *gorm.DB, roleID uint64) error {
	ids := authBaseResourceIDs(tx)
	if len(ids) == 0 {
		return nil
	}
	rows := make([]model.SysRoleResource, 0, len(ids))
	for _, rid := range ids {
		rows = append(rows, model.SysRoleResource{
			BaseModel:  model.BaseModel{ID: sonyflake.Next()},
			RoleID:     roleID,
			ResourceID: rid,
		})
	}
	return tx.Create(&rows).Error
}

// mergeUint64 合并两个 id 列表并去重(保序)
func mergeUint64(a []uint64, b []uint64) []uint64 {
	if len(b) == 0 {
		return a
	}
	seen := make(map[uint64]struct{}, len(a)+len(b))
	out := make([]uint64, 0, len(a)+len(b))
	for _, list := range [][]uint64{a, b} {
		for _, id := range list {
			if id == 0 {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// GetRoleResourceIDs 2026-10-18 分配权限 UI 真实化
//   - 返某角色已绑定的 sys_role_resource.resource_id 集合
//   - 前端 Drawer 打开时拉这个,作为 Tree checkedKeys 初值
//   - 缺这步,前端把 checkedKeys 当空数组(覆盖式保存) → 误清空所有绑定
func (s *roleService) GetRoleResourceIDs(ctx context.Context, roleID uint64) ([]uint64, error) {
	if roleID == 0 {
		return nil, errcode.New(errcode.BadRequest, "roleId 不能为空")
	}
	var ids []uint64
	if err := s.db.WithContext(ctx).
		Model(&model.SysRoleResource{}).
		Where("role_id = ?", roleID).
		Pluck("resource_id", &ids).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询角色资源失败").WithDetail(err.Error())
	}
	return ids, nil
}

// ===== 资源 =====

func (s *roleService) ListResources(ctx context.Context, q ResourceListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysResource{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("code LIKE ? OR name LIKE ? OR path LIKE ?", like, like, like)
	}
	if q.Type != "" {
		tx = tx.Where("type = ?", q.Type)
	}
	if q.ParentID > 0 {
		tx = tx.Where("parent_id = ?", q.ParentID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询资源总数失败").WithDetail(err.Error())
	}
	var rows []model.SysResource
	if err := tx.Order("sort ASC, id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *roleService) ResourceTree(ctx context.Context) ([]model.SysResource, error) {
	var rows []model.SysResource
	if err := s.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("sort ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询资源树失败").WithDetail(err.Error())
	}
	return rows, nil
}

func (s *roleService) GetResource(ctx context.Context, id uint64) (*model.SysResource, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var r model.SysResource
	if err := s.db.WithContext(ctx).First(&r, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "资源不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	return &r, nil
}

func (s *roleService) CreateResource(ctx context.Context, caller *Caller, res *model.SysResource) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if err := MustNoEmpty("code", res.Code); err != nil {
		return 0, err
	}
	if err := MustNoEmpty("name", res.Name); err != nil {
		return 0, err
	}
	if res.Type == "" {
		res.Type = "menu"
	}
	if !oneOf(res.Type, "menu", "button", "api") {
		return 0, errcode.New(errcode.InvalidParam, "type 必须是 menu/button/api")
	}
	if err := validateMethod(res.Method); err != nil {
		return 0, err
	}
	// 父资源:存在 + 类型组合合法 + 不成环(见 validateParent 的注释)
	if res.ParentID > 0 {
		if err := s.validateParent(ctx, res.Type, res.ID, res.ParentID); err != nil {
			return 0, err
		}
	}
	// 唯一性
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.SysResource{}).
		Where("code = ?", res.Code).Count(&cnt).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "校验资源失败").WithDetail(err.Error())
	}
	if cnt > 0 {
		return 0, errcode.New(errcode.Conflict, "资源 code 已存在")
	}
	res.ID = sonyflake.Next()
	res.IsActive = true
	if err := s.db.WithContext(ctx).Create(res).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建资源失败").WithDetail(err.Error())
	}
	return res.ID, nil
}

// allowedParentTypes 「子资源类型 → 允许的父资源类型」白名单
//
// ⚠️ **与前端 web/src/pages/system/permission/resourceRules.ts 的
// ALLOWED_PARENT_TYPES 一一镜像** —— 改一边必须改另一边,
// 否则会出现"前端不让选、后端却收"的割裂(或者反过来)。
//
// 为什么 menu 只能挂在 menu 下面:
//   菜单树(buildMenuTree / 侧边栏)只认菜单节点。把菜单挂到按钮/接口下面,
//   这个菜单在侧边栏里**永远不出现** —— 它不在任何菜单的子树里,
//   界面不报错、也不像"权限丢了",就是菜单凭空没了。
//
// ⚠️ **2026-11-03 用户拍板:button / api 不允许有父级**(原来允许挂
//   menu/button/api)。parent_id 唯一的消费者是菜单树;按钮和接口在
//   所有消费方里(seed / 分配 Drawer / casbin)都是平铺的,父级是
//   没有读者的死数据 ⇒ 前端编辑弹窗不再渲染父级字段,这里同步拒绝。
var allowedParentTypes = map[string]map[string]bool{
	"menu":   {"menu": true},
	"button": {},
	"api":    {},
}

// validateMethod 校验 HTTP 方法 —— 空串合法(表示"不限")
func validateMethod(m string) error {
	m = strings.ToUpper(strings.TrimSpace(m))
	if m == "" {
		return nil
	}
	if !oneOf(m, "GET", "POST", "PATCH", "PUT", "DELETE") {
		return errcode.New(errcode.InvalidParam, "HTTP 方法必须是 GET/POST/PATCH/PUT/DELETE 之一,留空表示不限")
	}
	return nil
}

// parseParentID 解析 parent_id(wire 上是带引号的数字串,"0" = 顶级)
func parseParentID(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, errcode.New(errcode.InvalidParam, "parent_id 必须是数字串").WithDetail(err.Error())
	}
	return v, nil
}

// validateParent 校验父资源:存在 + 类型组合合法 + 不成环
//
// ⚠️ **第 3 条(不成环)为什么必须在后端拦**:
//   前端已经在下拉里排除了自己和自己的后代,但前端是**建议**,不是**约束**
//   —— 直接打 API 就能绕过。父子成环之后,环上的节点都不是根,
//   在 buildMenuTree(前端按 parent_id 建树)里**凭空消失**:
//   菜单连同整棵子树静默不见,而且**不报任何错**。
func (s *roleService) validateParent(ctx context.Context, childType string, childID, parentID uint64) error {
	var parent model.SysResource
	if err := s.db.WithContext(ctx).First(&parent, parentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.BadRequest, "父资源不存在")
		}
		return errcode.New(errcode.Internal, "查询父资源失败").WithDetail(err.Error())
	}
	if !allowedParentTypes[childType][parent.Type] {
		return errcode.New(errcode.InvalidParam,
			"父资源类型不合法:"+childType+" 不能挂在 "+parent.Type+" 下面")
	}
	// 成环检查:沿 parent_id 一路向上,碰到自己(或自己的祖先链里有自己)就是环
	seen := map[uint64]bool{}
	cur := parent
	for cur.ParentID != 0 {
		if cur.ParentID == childID {
			return errcode.New(errcode.InvalidParam, "父资源不能是自己的后代(会形成环)")
		}
		if seen[cur.ID] {
			// 库里本来就有环(历史脏数据)—— 别把这条查询搞成死循环
			break
		}
		seen[cur.ID] = true
		var next model.SysResource
		if err := s.db.WithContext(ctx).
			Select("id", "parent_id").
			Where("id = ?", cur.ParentID).
			First(&next).Error; err != nil {
			break
		}
		cur = next
	}
	return nil
}

func (s *roleService) UpdateResource(ctx context.Context, caller *Caller, id uint64, in *ResourceUpdateInput) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	if in == nil {
		return errcode.New(errcode.BadRequest, "请求体不能为空")
	}
	var old model.SysResource
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "资源不存在")
		}
		return errcode.New(errcode.Internal, "查询资源失败").WithDetail(err.Error())
	}
	// ⚠️ **指针字段 = 区分"没传"和"传了但是空"** —— 见 ResourceUpdateInput 的注释。
	//   以前这里是 `if res.Method != ""` 这类写法,清空类操作全是死的。
	updates := map[string]any{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return errcode.New(errcode.BadRequest, "资源名称不能为空")
		}
		updates["name"] = name
	}
	if in.Path != nil {
		updates["path"] = strings.TrimSpace(*in.Path)
	}
	if in.Method != nil {
		if err := validateMethod(*in.Method); err != nil {
			return err
		}
		updates["method"] = strings.ToUpper(strings.TrimSpace(*in.Method))
	}
	if in.Icon != nil {
		updates["icon"] = strings.TrimSpace(*in.Icon)
	}
	if in.Sort != nil {
		updates["sort"] = *in.Sort
	}
	if in.IsActive != nil {
		updates["is_active"] = *in.IsActive
	}
	if in.ParentID != nil {
		pid, err := parseParentID(*in.ParentID)
		if err != nil {
			return err
		}
		if pid != 0 {
			if pid == id {
				return errcode.New(errcode.InvalidParam, "父资源不能是自己")
			}
			if err := s.validateParent(ctx, old.Type, id, pid); err != nil {
				return err
			}
		}
		// "0" 也要写进去 —— 提到顶级是合法操作(以前 `if res.ParentID > 0` 把它跳过了)
		updates["parent_id"] = pid
	}
	if len(updates) == 0 {
		return nil
	}
	// ⚠️ M147-f:资源的**启用状态**变了 → 绑着它的角色的策略行要重算。
	//
	// 不重算的话,"停用资源"就是个**死开关**:界面上的菜单消失了
	// (AccessibleResources 过滤 is_active),接口却还能调
	// (casbin 里那条策略行还在)—— 前后端两套真相。
	//
	// 重新启用也要重算(把权限还回去)—— 停用只是临时收回,不是解绑。
	activeChanged := in.IsActive != nil && *in.IsActive != old.IsActive
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&old).Updates(updates).Error; err != nil {
			return err
		}
		if !activeChanged {
			return nil
		}
		// 绑着这个资源的角色(通常 1~3 个,最多 13 个内置角色)
		var roleIDs []uint64
		if err := tx.Model(&model.SysRoleResource{}).
			Where("resource_id = ?", id).Pluck("role_id", &roleIDs).Error; err != nil {
			return err
		}
		for _, rid := range roleIDs {
			var rc model.SysRole
			if err := tx.Select("id, code").First(&rc, rid).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					// 角色已被软删 —— 绑定理应已硬删(clearBindings),
					// 这里碰到的是孤儿行:跳过,别把"更新资源"拖成 500。
					// (该角色的 casbin 行在 DeleteRole 时已删干净,跳过不丢东西)
					continue
				}
				return err
			}
			if err := syncRolePolicies(tx, rc.Code, rid); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errcode.New(errcode.Internal, "更新资源失败").WithDetail(err.Error())
	}
	if activeChanged {
		// 事务提交后再刷内存副本
		refreshCasbinPolicies()
	}
	return nil
}

func (s *roleService) DeleteResource(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	// 子资源检查
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.SysResource{}).
		Where("parent_id = ?", id).Count(&cnt).Error; err != nil {
		return errcode.New(errcode.Internal, "校验子资源失败").WithDetail(err.Error())
	}
	if cnt > 0 {
		return errcode.New(errcode.BadRequest, "存在子资源,不可删除")
	}
	// 角色绑定检查
	if err := s.db.WithContext(ctx).Model(&model.SysRoleResource{}).
		Where("resource_id = ?", id).Count(&cnt).Error; err != nil {
		return errcode.New(errcode.Internal, "校验资源绑定失败").WithDetail(err.Error())
	}
	if cnt > 0 {
		return errcode.New(errcode.Conflict, "资源已被角色绑定,不可删除")
	}
	if err := s.db.WithContext(ctx).Delete(&model.SysResource{}, id).Error; err != nil {
		return errcode.New(errcode.Internal, "删除资源失败").WithDetail(err.Error())
	}
	return nil
}

// ===== 部门 =====

func (s *roleService) ListDepartments(ctx context.Context, q DepartmentListQuery) (*model.PageResponse, error) {
	page, size := s.normPage(q.Page, q.Size)
	tx := s.db.WithContext(ctx).Model(&model.SysDepartment{})
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		tx = tx.Where("name LIKE ? OR code LIKE ?", like, like)
	}
	if q.ParentID > 0 {
		tx = tx.Where("parent_id = ?", q.ParentID)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询部门总数失败").WithDetail(err.Error())
	}
	var rows []model.SysDepartment
	if err := tx.Order("sort ASC, id ASC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询部门失败").WithDetail(err.Error())
	}
	// 2026-10-18:补 Path。分页场景只查当前页,parent 链可能跨页,
	//   多走一次只查 id+parent_id 轻量查询建映射,17 节点成本可接受
	if len(rows) > 0 {
		var all []model.SysDepartment
		if err := s.db.WithContext(ctx).
			Select("id, parent_id, name").
			Find(&all).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询部门路径失败").WithDetail(err.Error())
		}
		fillDeptPath(rows, buildParentMap(all), buildNameMap(all))
	}
	// 2026-10-18:补 leader_name(分页)
	if err := s.fillLeaderName(ctx, rows); err != nil {
		return nil, err
	}
	// 2026-10-18:补 member_count(分页)
	if err := s.fillMemberCount(ctx, rows); err != nil {
		return nil, err
	}
	return &model.PageResponse{List: rows, Total: total, Page: page, Size: size}, nil
}

func (s *roleService) DepartmentTree(ctx context.Context) ([]model.SysDepartment, error) {
	var rows []model.SysDepartment
	if err := s.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("sort ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询部门树失败").WithDetail(err.Error())
	}
	// 2026-10-18:真正建树 — 按 parent_id 分组递归挂 children
	//   之前版本直接返扁平 rows,前端 Antd Tree 全部展平成根的兄弟,17 节点没层级
	//   现在递归挂 children,前端直接消费 (避免再多一次 group by / 二次查询)
	//   顺便给每个节点算 Path(根→当前 的中文名链路,'/' 分隔),详情页 '部门路径' 字段用
	parentByID := buildParentMap(rows)
	fillDeptPath(rows, parentByID, buildNameMap(rows))
	// 2026-10-18:补 leader_name(join sys_user.nickname),详情页 '负责人' 字段直接展示
	if err := s.fillLeaderName(ctx, rows); err != nil {
		return nil, err
	}
	// 2026-10-18:补 member_count(详情 / 树每节点),前端 ViewDrawer '成员数' 字段用
	if err := s.fillMemberCount(ctx, rows); err != nil {
		return nil, err
	}
	return buildDeptTree(rows, 0), nil
}

// buildDeptTree 按 parent_id 递归组装部门树
//   - all:扁平全部行(已按 sort ASC, id ASC 排好序)
//   - parentID:本次要找的父 ID(0 = 根节点)
//   返回:挂好 Children 的根节点列表
func buildDeptTree(all []model.SysDepartment, parentID uint64) []model.SysDepartment {
	out := make([]model.SysDepartment, 0)
	for _, n := range all {
		if n.ParentID == parentID {
			// 递归:先填 Children 再 append,保证 Children 字段已被赋值
			n.Children = buildDeptTree(all, n.ID)
			out = append(out, n)
		}
	}
	return out
}

// buildParentMap 把 rows 转成 id→parent_id 查询表(给 path 算链路用)
func buildParentMap(rows []model.SysDepartment) map[uint64]uint64 {
	m := make(map[uint64]uint64, len(rows))
	for _, n := range rows {
		m[n.ID] = n.ParentID
	}
	return m
}

// buildNameMap 把 rows 转成 id→name 查询表(给 path 展示中文链路用)
func buildNameMap(rows []model.SysDepartment) map[uint64]string {
	m := make(map[uint64]string, len(rows))
	for _, n := range rows {
		m[n.ID] = n.Name
	}
	return m
}

// fillLeaderName 查 sys_user 拿 nickname,把 leader_id 映射成 leader_name 填到 rows
//   - 一次性查全 id 列表 + nickname in (...),避免 N+1
//   - leader_id=0 (未设) 保持 LeaderName 空,前端展示 '-'
//   - 2026-10-18 加,给前端 dept 页详情 '负责人' 字段用,不用前端二次查 user
func (s *roleService) fillLeaderName(ctx context.Context, rows []model.SysDepartment) error {
	if len(rows) == 0 {
		return nil
	}
	leaderIDs := make([]uint64, 0, len(rows))
	seen := make(map[uint64]bool, len(rows))
	for _, n := range rows {
		if n.LeaderID != 0 && !seen[n.LeaderID] {
			seen[n.LeaderID] = true
			leaderIDs = append(leaderIDs, n.LeaderID)
		}
	}
	if len(leaderIDs) == 0 {
		return nil
	}
	type userLite struct {
		ID       uint64
		Nickname string
	}
	var users []userLite
	if err := s.db.WithContext(ctx).
		Table("sys_user").
		Select("id, nickname").
		Where("id IN ?", leaderIDs).
		Scan(&users).Error; err != nil {
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	nameByID := make(map[uint64]string, len(users))
	for _, u := range users {
		nameByID[u.ID] = u.Nickname
	}
	for i := range rows {
		if rows[i].LeaderID != 0 {
			rows[i].LeaderName = nameByID[rows[i].LeaderID]
		}
	}
	return nil
}

// fillMemberCount 查 sys_user_department 表,给每行填 MemberCount
//   - 一次性 GROUP BY dept_id,避免 N+1
//   - 2026-10-18 加,给前端 dept 页 ViewDrawer '成员数' 字段用
//   - 没在 sys_user_department 关联的部门 MemberCount=0,前端展示 '0'
//   - 软删:本表没有 BaseModel 的 deleted_at 字段,直接 COUNT 即可
func (s *roleService) fillMemberCount(ctx context.Context, rows []model.SysDepartment) error {
	if len(rows) == 0 {
		return nil
	}
	deptIDs := make([]uint64, 0, len(rows))
	seen := make(map[uint64]bool, len(rows))
	for _, n := range rows {
		if !seen[n.ID] {
			seen[n.ID] = true
			deptIDs = append(deptIDs, n.ID)
		}
	}
	type countRow struct {
		DeptID uint64
		Cnt    int
	}
	var counts []countRow
	if err := s.db.WithContext(ctx).
		Table("sys_user_department").
		Select("dept_id, COUNT(*) as cnt").
		Where("dept_id IN ?", deptIDs).
		Group("dept_id").
		Scan(&counts).Error; err != nil {
		return errcode.New(errcode.Internal, "查询部门成员数失败").WithDetail(err.Error())
	}
	cntByID := make(map[uint64]int, len(counts))
	for _, c := range counts {
		cntByID[c.DeptID] = c.Cnt
	}
	for i := range rows {
		rows[i].MemberCount = cntByID[rows[i].ID]
	}
	return nil
}

// fillDeptPath 给 rows 里每行填 Path(根→当前 名称链,'/' 分隔,根部门 path 为空)
//   - maxDepth 防环:理论上 17 节点 ≤ 3 层,但若脏数据 parent 互相指会死循环
//   - 直接改 slice 里的指针指向的 struct,值拷贝(append/out)也无所谓
//   - 之前返 id 链 "root,mid,leaf," 用户看 ID 不知道哪个部门,改成中文名链路 "集团/数字交付中心/产品部"
func fillDeptPath(rows []model.SysDepartment, parentByID map[uint64]uint64, nameByID map[uint64]string) {
	for i := range rows {
		rows[i].Path = walkPath(rows[i].ID, parentByID, nameByID, 0)
	}
}

// walkPath 沿 parent 链走到根,把经过的 name 倒序拼成 "集团/数字交付中心/产品部"
//   - cur:当前节点 id
//   - depth:递归深度,>32 视为环直接返回,防脏数据死循环
//   - 根部门(parent_id=0)Path 返回空,前端展示 '-',符合 '无父级 = 无路径' 语义
//   - 中间任一节点找不到(脏数据导致),该段省略,尽量给用户看到最长已知链路
func walkPath(cur uint64, parentByID map[uint64]uint64, nameByID map[uint64]string, _ int) string {
	chain := []uint64{cur}
	pid, ok := parentByID[cur]
	for ok && pid != 0 {
		chain = append(chain, pid)
		pid, ok = parentByID[pid]
		// 防环:链长度 > 32 直接断
		if len(chain) > 32 {
			break
		}
	}
	// 根部门:链只有自己 → 路径为空
	if len(chain) == 1 {
		return ""
	}
	// 倒序拼装 name,用 '/' 分隔
	parts := make([]string, 0, len(chain))
	for i := len(chain) - 1; i >= 0; i-- {
		name, found := nameByID[chain[i]]
		if !found {
			// 中间节点脏数据丢失,跳过这段
			continue
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, "/")
}

func (s *roleService) GetDepartment(ctx context.Context, id uint64) (*model.SysDepartment, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var d model.SysDepartment
	if err := s.db.WithContext(ctx).First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "部门不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询部门失败").WithDetail(err.Error())
	}
	// 2026-10-18:补 Path(根→当前 id 链),详情页 '部门路径' 字段用
	//   这里只查单条,多走一次全表查 id→parent 映射,17 节点成本可接受
	if d.ParentID != 0 {
		var all []model.SysDepartment
		if err := s.db.WithContext(ctx).
			Select("id, parent_id, name").
			Find(&all).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询部门路径失败").WithDetail(err.Error())
		}
		d.Path = walkPath(d.ID, buildParentMap(all), buildNameMap(all), 0)
	}
	// 2026-10-18:补 leader_name(单条)
	if d.LeaderID != 0 {
		if err := s.fillLeaderName(ctx, []model.SysDepartment{d}); err != nil {
			return nil, err
		}
	}
	// 2026-10-18:补 member_count(单条)
	if err := s.fillMemberCount(ctx, []model.SysDepartment{d}); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *roleService) CreateDepartment(ctx context.Context, caller *Caller, dept *model.SysDepartment) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if err := MustNoEmpty("name", dept.Name); err != nil {
		return 0, err
	}
	if dept.Code != "" {
		var cnt int64
		if err := s.db.WithContext(ctx).Model(&model.SysDepartment{}).
			Where("code = ?", dept.Code).Count(&cnt).Error; err != nil {
			return 0, errcode.New(errcode.Internal, "校验部门失败").WithDetail(err.Error())
		}
		if cnt > 0 {
			return 0, errcode.New(errcode.Conflict, "部门 code 已存在")
		}
	}
	dept.ID = sonyflake.Next()
	dept.IsActive = true
	if err := s.db.WithContext(ctx).Create(dept).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "创建部门失败").WithDetail(err.Error())
	}
	return dept.ID, nil
}

// UpdateDepartment 更新部门
//
// ⚠️ **入参是 DTO(指针字段),不是 model.SysDepartment** —— 理由见
// DepartmentUpdateInput 的注释。核心区别只有一条:
// **nil = 没传(不动),非 nil = 传了(哪怕是 "0" 也要写进去)**。
func (s *roleService) UpdateDepartment(ctx context.Context, caller *Caller, id uint64, in *DepartmentUpdateInput) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	if in == nil {
		return errcode.New(errcode.BadRequest, "请求体不能为空")
	}
	var old model.SysDepartment
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "部门不存在")
		}
		return errcode.New(errcode.Internal, "查询部门失败").WithDetail(err.Error())
	}
	updates := map[string]any{}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return errcode.New(errcode.BadRequest, "部门名称不能为空")
		}
		updates["name"] = name
	}
	if in.Code != nil {
		code := strings.TrimSpace(*in.Code)
		if code != "" {
			// ⚠️ **code 上有唯一索引** —— 不先查就写,撞了会变成一条
			// "更新部门失败"(500),而用户看到的只是"保存失败"。
			// CreateDepartment 那边已经有同样的校验,这里补齐(更新的路径以前没有)。
			var cnt int64
			if err := s.db.WithContext(ctx).Model(&model.SysDepartment{}).
				Where("code = ? AND id <> ?", code, id).Count(&cnt).Error; err != nil {
				return errcode.New(errcode.Internal, "校验部门失败").WithDetail(err.Error())
			}
			if cnt > 0 {
				return errcode.New(errcode.Conflict, "部门 code 已存在")
			}
			updates["code"] = code
		}
	}
	if in.LeaderID != nil {
		// ⚠️ **"0" = 清空负责人** —— 以前这里是 `if dept.LeaderID > 0`,
		//   于是"清空负责人"这个操作永远不生效(改了没反应,刷新又回来了)。
		leaderID, err := parseDeptID(*in.LeaderID, "leader_id")
		if err != nil {
			return err
		}
		updates["leader_id"] = leaderID
	}
	if in.Sort != nil {
		updates["sort"] = *in.Sort
	}
	if in.ParentID != nil {
		// ⚠️ **"0" = 提到顶级** —— 同上,以前那个 `|| dept.ParentID == 0`
		//   是恒真式,parent_id 永远被写 ⇒ 只想改名字的 PATCH 会把部门
		//   **静默搬到根**。现在 nil(没传)才是不动。
		parentID, err := parseDeptID(*in.ParentID, "parent_id")
		if err != nil {
			return err
		}
		updates["parent_id"] = parentID
	}
	if in.IsActive != nil {
		updates["is_active"] = *in.IsActive
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(&old).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新部门失败").WithDetail(err.Error())
	}
	return nil
}

// parseDeptID 解析"可能是 0 的 ID 串"。
//
// ⚠️ **跟 parseParentID 的区别**:那个把空串当 0(父资源"顶级"就是空),
// 这个不行 —— 负责人/父部门字段的"没传"由 **nil 指针**表达,
// 空串在这儿是**脏数据**(前端发 "" 意味着"清空"还是"没传"?说不清 ⇒ 拒掉)。
func parseDeptID(s string, field string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errcode.New(errcode.InvalidParam, field+" 不能为空(要清空请发 \"0\")")
	}
	if s == "0" {
		return 0, nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, errcode.New(errcode.InvalidParam, field+" 必须是数字串").WithDetail(err.Error())
	}
	return v, nil
}

func (s *roleService) DeleteDepartment(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.SysDepartment{}).
		Where("parent_id = ?", id).Count(&cnt).Error; err != nil {
		return errcode.New(errcode.Internal, "校验子部门失败").WithDetail(err.Error())
	}
	if cnt > 0 {
		return errcode.New(errcode.BadRequest, "存在子部门,不可删除")
	}
	if err := s.db.WithContext(ctx).Model(&model.SysUserDepartment{}).
		Where("dept_id = ?", id).Count(&cnt).Error; err != nil {
		return errcode.New(errcode.Internal, "校验部门人员失败").WithDetail(err.Error())
	}
	if cnt > 0 {
		return errcode.New(errcode.Conflict, "部门下存在用户,不可删除")
	}
	if err := s.db.WithContext(ctx).Delete(&model.SysDepartment{}, id).Error; err != nil {
		return errcode.New(errcode.Internal, "删除部门失败").WithDetail(err.Error())
	}
	return nil
}

// ===== 字典 =====

// ListDict 查字典项
//
// 🔴 **M95:走缓存** —— 字典是"每个页面的下拉都要读"的数据,
// 不缓存就是每次都打库。缓存键是 `dict:items:<groupCode>`。
//
// ⚠️ **缓存不改变语义** —— 没有 L2(没配 Redis / cache.mode=off)时
// 退化成每次回源,返回值与开缓存时完全一致。
func (s *roleService) ListDict(ctx context.Context, dictType string) ([]model.SysDict, error) {
	return s.dictCache.Items(ctx, dictType, func(ctx context.Context) ([]model.SysDict, error) {
		var rows []model.SysDict
		tx := s.db.WithContext(ctx).Model(&model.SysDict{})
		if dictType != "" {
			// type 参数前端可能传 groupCode 或 type
			tx = tx.Where("group_code = ?", dictType)
		}
		// 2026-10-18:全表浏览支持(group_code='',dictType 透传给前端 ListDict 调用),
		//   排序按 group_code 优先,让同分组聚在一起便于扫视;分组内再按 sort
		if err := tx.Order("group_code ASC, sort ASC, id ASC").Find(&rows).Error; err != nil {
			return nil, errcode.New(errcode.Internal, "查询字典失败").WithDetail(err.Error())
		}
		return rows, nil
	})
}

// ListDictGroups 2026-10-18: 返回所有有数据的 group_code + 中文 label 列表
//
// 设计演进 (2026-10-18):
//   - v1: 返回 []string (groupCode), 前端 select 显示英文
//   - v2: 返回 []seed.DictGroupKV{Code, Label}, 前端 select 显示中文 label
// 中文 label 来自 seed.dictGroupLabels 单一来源(29 个 group 全部登记)
// 字典管理页 "业务类型" 下拉数据源
//
// 🔴 **M95:走缓存** —— 缓存键是 `dict:groups`。
// ⚠️ **与 `ListDict` 共用同一个失效前缀**(`dict:`)—— 字典项改了,
//   分组列表也一起作废,不用各写各的失效。
func (s *roleService) ListDictGroups(ctx context.Context) ([]seed.DictGroupKV, error) {
	return s.dictCache.Groups(ctx, func(ctx context.Context) ([]seed.DictGroupKV, error) {
		// 1) 先拿数据库里有数据的 group_code(去重)
		var dbGroups []string
		err := s.db.WithContext(ctx).
			Model(&model.SysDict{}).
			Where("is_active = ?", true).
			Distinct("group_code").
			Pluck("group_code", &dbGroups).Error
		if err != nil {
			return nil, errcode.New(errcode.Internal, "查询字典分组失败").WithDetail(err.Error())
		}
		dbSet := make(map[string]struct{}, len(dbGroups))
		for _, g := range dbGroups {
			if g != "" {
				dbSet[g] = struct{}{}
			}
		}
		// 2) 拿全部 label 表, 过滤出数据库里真有数据的
		all := seed.DictGroupLabels()
		out := make([]seed.DictGroupKV, 0, len(all))
		for _, kv := range all {
			if _, ok := dbSet[kv.Code]; ok {
				out = append(out, kv)
			}
		}
		return out, nil
	})
}

func (s *roleService) GetDict(ctx context.Context, id uint64) (*model.SysDict, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var d model.SysDict
	if err := s.db.WithContext(ctx).First(&d, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "字典项不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询字典失败").WithDetail(err.Error())
	}
	return &d, nil
}

// CreateDict 2026-10-18: 字典只读, 禁止新增
// 字典是代码 enum 的中央仓库, 任何新字典必须先在 seed.go mustDicts + seedAllDicts 登记
// 路径: cmd/migrate 自动跑 seed.Run, 无须运行时新增
// 即使 super_admin 也禁止(跟代码契约强绑定, 跟权限无关)
// 未来回退: 把这个函数体恢复成"原实现"即可, 路由和 service interface 都不用动
func (s *roleService) CreateDict(ctx context.Context, caller *Caller, d *model.SysDict) (uint64, error) {
	_ = caller
	_ = d
	return 0, errcode.New(errcode.Forbidden,
		"字典只读,禁止新增。新增字典请在 server/internal/seed/seed.go 中登记并跑 migrate (go run ./cmd/migrate -c configs/config.dev.yaml)")
}

func (s *roleService) UpdateDict(ctx context.Context, caller *Caller, id uint64, d *model.SysDict) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var old model.SysDict
	if err := s.db.WithContext(ctx).First(&old, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "字典项不存在")
		}
		return errcode.New(errcode.Internal, "查询字典失败").WithDetail(err.Error())
	}
	updates := map[string]any{}
	if d.ItemValue != "" {
		updates["item_value"] = d.ItemValue
	}
	if d.Label != "" {
		updates["label"] = d.Label
	}
	if d.Sort != 0 {
		updates["sort"] = d.Sort
	}
	if d.Description != "" {
		updates["description"] = d.Description
	}
	if d.IsActive != old.IsActive {
		updates["is_active"] = d.IsActive
	}
	if len(updates) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(&old).Updates(updates).Error; err != nil {
		return errcode.New(errcode.Internal, "更新字典失败").WithDetail(err.Error())
	}
	// 🔴 **顺序是死的:写库 → 失效缓存 → 广播事件**(M94 + M95)。
	//
	// 广播在写库成功之后(M94)—— 顺序反了会发出"改了但没改"的信号,
	// 前端据此重拉、拉到的还是旧值,下次真改的时候前端已经不信这个信号了。
	//
	// 失效在广播之前(M95)—— 前端收到事件就重拉,若此刻后端还吐着缓存里的
	// 旧字典,重拉回来的就是旧值。症状正是 M94 要修的"改了不生效",
	// 而且这次还多了一层"我明明让它刷新了"。
	//
	// 用的 group_code 取自**库里那一行**(`old`),不是请求体:
	// group_code 是代码契约的一部分,改它要走别的通道(改它等于换 enum)。
	//
	// ⚠️ **nil 判断在这里,不在调用方** —— 调用方只管发。
	s.invalidateDictCache(ctx)
	s.publishDictChanged(ctx, old.GroupCode)
	return nil
}

// dictCacheInvalidateTimeout 字典缓存失效的兜底 deadline。
//
// ⚠️ 失效走 Redis SCAN(前缀删),SCAN 是渐进式的,keyspace 大时会跑一阵。
//   没有 deadline 的话,一次"连得上但卡住"的 Redis 能把「保存字典」这条
//   请求一起拖住 —— 而这条请求在用户看来就是"点了保存没反应"。
//
// 2s 与 `dict_publisher.go` 的 `dictEventPublishTimeout` 同值:
// 同机房一次 SCAN+DEL 是毫秒级,2s 已经宽松到能覆盖一次 GC 停顿。
const dictCacheInvalidateTimeout = 2 * time.Second

// invalidateDictCache 作废全部字典缓存(写路径调用)。
//
// 🔴 **必须在写库之后、广播之前** —— 见 `UpdateDict` 里的顺序说明。
//
// ⚠️ **同步执行,不是 goroutine** —— 广播是异步的(goroutine 里投递),
//   失效若也是异步,两者谁先谁后就成了竞态:前端收到事件时缓存可能还没作废,
//   于是重拉回来的是旧值,又被写回缓存 —— 缓存从此一直是旧的,直到 TTL 到期。
//   同步失效把顺序钉死;代价是 PATCH 请求多花一次 Redis SCAN+DEL
//   (毫秒级,字典是低频管理操作,不值得为它引入异步的不确定性)。
//
// ⚠️ **剥掉取消链** —— 调用方的 ctx 可能已经死了(HTTP 请求超时 / 上游断开),
//   拿它删缓存会直接失败 → "改了没生效"。与 `dict_publisher.go` 同一套理由。
//
// ⚠️ **失败只记 warn,不影响主流程** —— 库里已经改成功了,这时候让请求失败
//   反而会让管理员以为"没改成",于是重复提交。失效失败的后果是"陈旧到 TTL
//   为止",比"保存失败"轻得多。
func (s *roleService) invalidateDictCache(ctx context.Context) {
	if s.dictCache == nil {
		return
	}
	safeCtx := context.WithoutCancel(ctx)
	inCtx, cancel := context.WithTimeout(safeCtx, dictCacheInvalidateTimeout)
	defer cancel()
	if err := s.dictCache.Invalidate(inCtx); err != nil {
		logger.L().Warn("dict: 字典缓存失效失败(期间可能读到旧字典)",
			zap.Error(err))
	}
}

// publishDictChanged 广播「字典组 X 被改了」。
//
// ⚠️ **publisher 为 nil 时静默跳过** —— 单测里不装 publisher 是常态。
// 但生产装配期**必须给真实现**(见 `NewRoleService` 的注释):
// nil 的症状是「字典改了没人知道」,而那**没有任何报错**。
func (s *roleService) publishDictChanged(ctx context.Context, groupCode string) {
	if s.dictPublisher == nil {
		return
	}
	s.dictPublisher.PublishDictChanged(ctx, groupCode)
}

// DeleteDict 2026-10-18: 字典只读, 禁止删除
// 字典是代码 enum 契约, 删除会导致前端 useDict() 拿不到 label
// 即使 super_admin 也禁止
// 如需下线某字典项, 改为 is_active=false(字典管理页"编辑"操作)
func (s *roleService) DeleteDict(ctx context.Context, caller *Caller, id uint64) error {
	_ = caller
	_ = id
	return errcode.New(errcode.Forbidden,
		"字典只读,禁止删除。如需下线某字典项,请改为 is_active=false(字典管理页'编辑'操作)")
}

// ===== helpers =====

// oneOf 判断 v 是否在 list 中
func oneOf(v string, list ...string) bool {
	for _, x := range list {
		if v == x {
			return true
		}
	}
	return false
}

// 静默 warning 抑制(为 linter;logger 在 impl 中按需使用)
var _ = logger.L
var _ = time.Time{}
var _ = zap.NewNop
