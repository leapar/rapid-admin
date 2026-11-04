// UserService 真实实现 — 系统用户管理
// 文档 §6.1
//
// 设计要点:
//  1. 不直接存明文手机/邮箱到加密字段(Phone/Email):为快速跑通 500 错误,
//     本期保持明文,后续 phase 接入 crypto.Field().Encrypt
//     (与 web/src/api/user.ts UpdateUserRequest.nickname/phone/email 字段对齐)
//  2. 密码哈希: 与 authService.Login 一致,前端 pre-hash 后的 hex 直接 bcrypt
//  3. 软删:sys_user.DeletedAt 由 gorm.DeletedAt 维护,本服务不物理删除
//  4. RBAC: 本期只做 super_admin 兜底,具体粒度权限由 middleware 挂载后控制
//  5. 数据范围: caller.ProjectIDs 非空时仅返回对应项目身份的用户
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
	"rapid-admin/internal/pkg/auth"
	"rapid-admin/internal/pkg/desensitize"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/pkg/logger"
	"rapid-admin/internal/pkg/permnotify"
	"rapid-admin/internal/pkg/scope"
	"rapid-admin/internal/pkg/sonyflake"
)

// userService 真实实现
type userService struct {
	db *gorm.DB
	// files 文件系统(M124 头像上传用)。**可以是 nil** ——
	// 那时 SetAvatar 返"文件系统未初始化",其余方法不受影响。
	//
	// ⚠️ 注入的是**同一个** FileService 实例(main.go 里 fileSvc 复用),
	// 不是另造一个 —— 另造一个会让 key 规则和配额记账各走各的。
	files FileService
	// scope 数据范围(行级权限,M155)。**可以是 nil** ——
	// 那时列表不做任何行级过滤(与"没配规则"同义)。
	//
	// ⚠️ **注入的是同一个 ScopeService 实例**(main.go 里 scopeSvc 复用),
	// 不是另造一个 —— 另造一个会让缓存键各算各的,
	// "改了规则立刻失效"就退化成"最多 5 分钟后生效"。
	scope ScopeService
}

// NewUserService 构造
//
// ⚠️ **files 是构造期注入的依赖,不是 setter** —— 头像上传要走文件系统,
// 而文件系统的 key 规则 / 配额记账必须跟别处**用同一个实例**。
// 传 nil 表示"本实例不支持头像上传"。
//
// ⚠️ **scope 同理** —— 数据范围的规则缓存必须在所有使用者之间共享,
// 传 nil 表示"本实例不做行级过滤"。
func NewUserService(db *gorm.DB, files FileService, scopeSvc ScopeService) UserService {
	return &userService{db: db, files: files, scope: scopeSvc}
}

// userScopeColumns 用户表上的数据范围列
//
// ⚠️ **三个维度都不是表上的裸列** ——
// 用户的项目归属在 sys_user_identity,部门归属在 sys_user_department(多对多),
// 所以这里写的是**子查询模板**,不是一个列名。
//
// ⚠️ **每个模板恰好一个 `?`** —— 占位符收 id 列表,
// 见 internal/pkg/scope/apply.go 的 buildListExpr。
var userScopeColumns = scope.Columns{
	Project: "id IN (SELECT user_id FROM sys_user_identity WHERE project_id IN ? AND is_active = 1)",
	Dept:    "id IN (SELECT user_id FROM sys_user_department WHERE dept_id IN ?)",
	Self:    "id IN ?",
}

// ensureSinglePrimary 业务层硬保证"1 user ≤ 1 is_primary=true"
//
// 背景:sys_user_identity.is_primary 没有 DB 唯一约束(MySQL 无 partial unique index),
// 必须 service 层事务内做"clear 旧 primary → set 新 primary"。
//
// 用法:任何插入/更新 is_primary=true 的代码,都应在同一 tx 内先调本函数清场。
// 注意:清的是同一 user 的所有 primary,**不影响**其他 user。
func ensureSinglePrimary(tx *gorm.DB, userID uint64, targetIdentityID uint64) error {
	// 1. 同 user 全部 primary 关掉
	if err := tx.Model(&model.SysUserIdentity{}).
		Where("user_id = ? AND is_primary = ?", userID, true).
		Update("is_primary", false).Error; err != nil {
		return err
	}
	// 2. 把目标 identity 设为主
	if targetIdentityID > 0 {
		if err := tx.Model(&model.SysUserIdentity{}).
			Where("id = ? AND user_id = ?", targetIdentityID, userID).
			Update("is_primary", true).Error; err != nil {
			return err
		}
	}
	return nil
}

// writeUserDepartments 写用户-部门多对多(全量替换)
//
//   - 清空该 user 全部 sys_user_department 记录
//   - 按 departmentIDs 顺序逐条插入;主部门用 is_primary=true 标记
//   - 主部门必须是 DepartmentIDs 的一员,否则报 InvalidParam
//   - 空 DepartmentIDs 视为"清空"(不报错)
//
// 2026-10-19 替代原单数 DepartmentID 字段
func writeUserDepartments(tx *gorm.DB, userID uint64, departmentIDs []uint64, primaryDepartmentID uint64) error {
	// 硬删 —— 见 clearBindings:软删撞 uk_ud,第二次保存同一批部门必 500
	if err := clearBindings(tx, &model.SysUserDepartment{}, "user_id = ?", userID); err != nil {
		return err
	}
	if len(departmentIDs) == 0 {
		return nil
	}
	// 主部门必须包含在 DepartmentIDs 内
	if primaryDepartmentID > 0 {
		found := false
		for _, d := range departmentIDs {
			if d == primaryDepartmentID {
				found = true
				break
			}
		}
		if !found {
			return errcode.New(errcode.InvalidParam, "主部门必须在部门列表中")
		}
	}
	// 第一个默认做主部门(若未显式指定)
	effectivePrimary := primaryDepartmentID
	if effectivePrimary == 0 && len(departmentIDs) > 0 {
		effectivePrimary = departmentIDs[0]
	}
	for _, d := range departmentIDs {
		dep := &model.SysUserDepartment{
			BaseModel: model.BaseModel{ID: sonyflake.Next()},
			UserID:    userID,
			DeptID:    d,
			IsPrimary: d == effectivePrimary,
		}
		if err := tx.Create(dep).Error; err != nil {
			return err
		}
	}
	return nil
}

// writeUserIdentitiesFromItems 写用户-身份(全量替换)
//
//   - 清空该 user 全部 sys_user_identity 记录
//   - 按 items 数组逐条插入,真正读 RoleCode / RoleName / ProjectID / DeptID / ValidFrom / ValidTo
//   - 旧 IdentityIDs 非空时(仅 Create 路径可能)按数量建 N 条"role_user_admin"占位,后端不再硬编码隐含身份
//   - 多条 IsPrimary=true 时由 ensureSinglePrimary 软保证只留一条(取第一条)
//
// 2026-10-19 替代原硬编码 RoleCode="role_user_admin" 的 P0 bug
func writeUserIdentitiesFromItems(tx *gorm.DB, userID uint64, items []IdentityItem, legacyLegacyIDs []uint64) error {
	if err := clearBindings(tx, &model.SysUserIdentity{}, "user_id = ?", userID); err != nil {
		return err
	}
	// 旧路径 fallback(传了 legacyIDs 但 items 为空)
	if len(items) == 0 {
		for range legacyLegacyIDs {
			items = append(items, IdentityItem{RoleCode: "role_user_admin", RoleName: "后台员工"})
		}
	}
	if len(items) == 0 {
		return nil
	}
	// 第一条默认主身份(若显式 IsPrimary,保留;多条 IsPrimary=true 走 ensureSinglePrimary 软保证)
	var firstID uint64
	for idx, it := range items {
		newID := sonyflake.Next()
		if idx == 0 {
			firstID = newID
		}
		ident := &model.SysUserIdentity{
			BaseModel: model.BaseModel{ID: newID},
			UserID:    userID,
			RoleCode:  it.RoleCode,
			RoleName:  it.RoleName,
			ProjectID: it.ProjectID,
			DeptID:    it.DeptID,
			ValidFrom: model.Time{Time: parseIdentityValidFrom(it.ValidFrom)},
			ValidTo:   model.Time{Time: parseIdentityValidTo(it.ValidTo)},
			IsActive:  true,
			IsPrimary: it.IsPrimary, // 写入时尊重前端传的;循环外走 ensureSinglePrimary 兜底
		}
		if err := tx.Create(ident).Error; err != nil {
			return err
		}
	}
	// 业务层软约束:同 user 只能有 1 个 is_primary=true
	if firstID > 0 {
		if err := ensureSinglePrimary(tx, userID, firstID); err != nil {
			return err
		}
	}
	return nil
}

// derefUint64 安全解 *uint64 → uint64(nil 返 0)
func derefUint64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

// ===== List =====

// List 分页查询用户
//  - Keyword 模糊匹配 username/nickname/phone/real_name
//  - IsActive 精确过滤
//  - DepartmentID 精确过滤(通过 sys_user_department 关联)
//  - RoleCode 精确过滤(通过 sys_user_identity.role_code 关联)
//  - caller.ProjectIDs 非空时叠加 project_id 范围(通过 identity)
func (s *userService) List(ctx context.Context, q UserListQuery) (*model.PageResponse, error) {
	// 规整 page/size(避免越界,与 pkg/pagination 一致)
	page, size := q.Page, q.Size
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	tx := s.db.WithContext(ctx).Model(&model.SysUser{})

	// 1. 关键词
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		// 兼容 phone(明文)和 phone_hash(若 seed 已写)
		tx = tx.Where(
			"username LIKE ? OR nickname LIKE ? OR phone LIKE ? OR real_name LIKE ? OR phone_hash = ?",
			like, like, like, like, desensitize.PhoneHash(kw),
		)
	}
	// 2. 启用状态
	if q.IsActive != nil {
		tx = tx.Where("is_active = ?", *q.IsActive)
	}
	// 3. 部门(join sys_user_department)
	if q.DepartmentID > 0 {
		tx = tx.Where("id IN (?)",
			s.db.WithContext(ctx).Model(&model.SysUserDepartment{}).
				Select("user_id").Where("dept_id = ?", q.DepartmentID),
		)
	}
	// 4. 角色(join sys_user_identity)
	if q.RoleCode != "" {
		tx = tx.Where("id IN (?)",
			s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
				Select("user_id").Where("role_code = ? AND is_active = ?", q.RoleCode, true),
		)
	}
	// 5. 单身份 id
	if q.IdentityID > 0 {
		tx = tx.Where("id IN (?)",
			s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
				Select("user_id").Where("id = ?", q.IdentityID),
		)
	}
	// 6. 数据范围(通过 identity.project_id 收窄)
	if len(q.ProjectIDs) > 0 {
		tx = tx.Where("id IN (?)",
			s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
				Select("user_id").Where("project_id IN ? AND is_active = ?", q.ProjectIDs, true),
		)
	}
	// 7. 行级权限(角色上配的数据范围规则,M155)
	//
	// ⚠️ **与第 6 条不是一回事** ——
	// 第 6 条是"调用方自己传了 project_ids 就按它收窄"(查询条件),
	// 第 7 条是"这个角色被配了只能看本部门"(权限规则)。
	// 后者不依赖调用方传什么 —— **不传就不过滤,那正是要治的病**。
	//
	// ⚠️ **没配规则时是零影响** —— EffectiveScope 返回零值 Scope
	// ⇒ 不过滤 ⇒ 上线当天现有查询结果一个字都不变。
	tx, err := applyScope(ctx, s.scope, tx, userScopeColumns)
	if err != nil {
		return nil, err
	}

	// 8. 排序
	orderBy := "id DESC"
	if q.OrderBy != "" {
		col := map[string]string{
			"id":         "id",
			"createdAt":  "created_at",
			"updatedAt":  "updated_at",
			"lastLogin":  "last_login_at",
			"username":   "username",
			"nickname":   "nickname",
		}[q.OrderBy]
		if col == "" {
			col = "id"
		}
		dir := "DESC"
		if strings.EqualFold(q.OrderDir, "asc") {
			dir = "ASC"
		}
		orderBy = col + " " + dir
	}

	// 8. 计数
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户总数失败").WithDetail(err.Error())
	}

	// 9. 列表
	var rows []model.SysUser
	if err := tx.Order(orderBy).Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户列表失败").WithDetail(err.Error())
	}

	// 10. 投影 DTO(Phase 5 M5+ 修复:snake_case + 派生 status)
	//  - 前端 types/permission.SysUser 期望 snake_case + status 字符串
	//  - 原模型 SysUser 是 camelCase + 派生字段在客户端算
	//  - phone/email 暂不在列表里暴露明文(列表走脱敏,详情走 user:read-sensitive 单独调 GetUserSensitive)
	items := make([]UserListItem, 0, len(rows))

	// 10.1 主部门:一条 SQL 取本页所有用户的主部门(不是 N+1)
	//  ⚠️ 2026-10-25:以前压根没填 —— DTO 里 department_id / department_name
	//  字段是有的,但没人给它们赋值,前端"部门"列恒为空。
	userIDs := make([]uint64, 0, len(rows))
	for i := range rows {
		userIDs = append(userIDs, rows[i].ID)
	}
	primaryDepts, err := s.primaryDepartments(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	// 10.2 角色:一条 SQL 取本页所有用户的有效角色(不是 N+1)
	//  ⚠️ 2026-10-30 M122:以前**压根没有** —— 列表里看不到这个人有什么权限,
	//  只能点进详情才看得到(而详情还要单独发一次请求)。
	roles, err := s.userRoles(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	for i := range rows {
		r := &rows[i]
		item := UserListItem{
			ID:           r.ID,
			Username:     r.Username,
			Nickname:     r.Nickname,
			RealName:     r.RealName,
			Avatar:       r.Avatar,
			Status:       DeriveUserStatus(r.IsActive, r.IsLeave, r.LockedUntil),
			LastLoginIP:  r.LastLoginIP,
			MfaEnabled:   r.MfaEnabled,
			CreatedAt:    r.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if d, ok := primaryDepts[r.ID]; ok {
			item.DepartmentID = d.ID
			item.DepartmentName = d.Name
		}
		// ⚠️ 没有角色也给**空数组**而不是 null ——
		//   null 在 JSON 里是"字段缺失",前端区分不了"这个人没有角色"
		//   和"接口没返这个字段"(两者都会渲染成空白,但只有前者是真的)。
		item.Roles = roles[r.ID]
		if item.Roles == nil {
			item.Roles = []UserRoleBrief{}
		}
		if r.LastLoginAt != nil && !r.LastLoginAt.IsZero() {
			s := r.LastLoginAt.Format("2006-01-02 15:04:05")
			item.LastLoginAt = &s
		}
		items = append(items, item)
	}

	return &model.PageResponse{
		List:  items,
		Total: total,
		Page:  page,
		Size:  size,
	}, nil
}

// ===== Get =====

// Get 取单条用户(预加载 identities + departments)
func (s *userService) Get(ctx context.Context, id uint64) (*UserDetail, error) {
	if id == 0 {
		return nil, errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var u model.SysUser
	if err := s.db.WithContext(ctx).
		Preload("Identities", func(db *gorm.DB) *gorm.DB {
			return db.Order("is_primary DESC, id ASC")
		}).
		First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errcode.New(errcode.NotFound, "用户不存在")
		}
		return nil, errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	depts, err := s.listUserDepartments(ctx, id)
	if err != nil {
		return nil, err
	}
	return &UserDetail{SysUser: u, Departments: depts}, nil
}

// listUserDepartments 查某用户的全部部门(主部门排最前)
//
// ⚠️ 为什么走 JOIN 而不是 GORM 的 Preload("Departments"):
//   SysUserDepartment 这张 join 表**不是** GORM 自动管的 many2many
//   (它多两列 is_primary / is_leader,还有手写迁移),
//   给模型挂 many2many 会让 AutoMigrate 跟手写迁移打架。
//   这里只读,一条 JOIN 查完 —— 顺带把部门名带出来,不用再查第二张表。
func (s *userService) listUserDepartments(ctx context.Context, userID uint64) ([]UserDepartment, error) {
	var rows []struct {
		DeptID    uint64
		DeptName  string
		DeptCode  string
		IsPrimary bool
	}
	err := s.db.WithContext(ctx).
		Table("sys_user_department AS ud").
		Select("ud.dept_id AS dept_id, d.name AS dept_name, d.code AS dept_code, ud.is_primary AS is_primary").
		Joins("JOIN sys_department AS d ON d.id = ud.dept_id AND d.deleted_at IS NULL").
		Where("ud.user_id = ? AND ud.deleted_at IS NULL", userID).
		Order("ud.is_primary DESC, d.sort ASC, d.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户部门失败").WithDetail(err.Error())
	}
	out := make([]UserDepartment, 0, len(rows))
	for _, r := range rows {
		out = append(out, UserDepartment{
			ID:        r.DeptID,
			Name:      r.DeptName,
			Code:      r.DeptCode,
			IsPrimary: r.IsPrimary,
		})
	}
	return out, nil
}

// userRoles 批量取一批用户的**有效角色**(去重后)
//
// 一条 SQL 查完本页所有用户 —— 不是每个用户查一次(那是 N+1)。
//
// ⚠️ **只取 is_active = true 的身份**:
//   停用的身份不授予任何权限(authz 的 loadCaller 同样要求 is_active = true),
//   把停用身份的角色也列出来 = 让管理员以为这个人还有那个权限 ——
//   **看得见的错权限比看不见的更危险**。
//
// ⚠️ **同一个 role_code 只保留一条**:一个人可以在不同项目/部门持同一个角色,
//   这一列回答的是"这个人有什么权限",不是"这个人有几个身份"
//   (身份清单在详情抽屉里,那里才有项目/部门/有效期)。
//
// 排序:主身份的角色排最前,其余按 role_code 升序 —— 同一页里两个人的
// 角色顺序一致,扫起来才舒服。
func (s *userService) userRoles(ctx context.Context, userIDs []uint64) (map[uint64][]UserRoleBrief, error) {
	out := make(map[uint64][]UserRoleBrief, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		UserID    uint64
		RoleCode  string
		RoleName  string
		IsPrimary bool
	}
	err := s.db.WithContext(ctx).
		Model(&model.SysUserIdentity{}).
		Select("user_id, role_code, role_name, is_primary").
		Where("user_id IN ? AND is_active = ?", userIDs, true).
		Order("is_primary DESC, role_code ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户角色失败").WithDetail(err.Error())
	}
	seen := make(map[uint64]map[string]bool, len(userIDs))
	for _, r := range rows {
		if r.RoleCode == "" {
			continue
		}
		if seen[r.UserID] == nil {
			seen[r.UserID] = map[string]bool{}
		}
		if seen[r.UserID][r.RoleCode] {
			continue
		}
		seen[r.UserID][r.RoleCode] = true
		out[r.UserID] = append(out[r.UserID], UserRoleBrief{
			RoleCode: r.RoleCode,
			RoleName: r.RoleName,
		})
	}
	return out, nil
}

// primaryDepartments 批量取一批用户的**主部门**
//
// 一条 SQL 查完本页所有用户 —— 不是每个用户查一次(那是 N+1)。
func (s *userService) primaryDepartments(ctx context.Context, userIDs []uint64) (map[uint64]UserDepartment, error) {
	out := make(map[uint64]UserDepartment, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		UserID    uint64
		DeptID    uint64
		DeptName  string
		DeptCode  string
		IsPrimary bool
	}
	err := s.db.WithContext(ctx).
		Table("sys_user_department AS ud").
		Select("ud.user_id AS user_id, ud.dept_id AS dept_id, d.name AS dept_name, d.code AS dept_code, ud.is_primary AS is_primary").
		Joins("JOIN sys_department AS d ON d.id = ud.dept_id AND d.deleted_at IS NULL").
		Where("ud.user_id IN ? AND ud.is_primary = ? AND ud.deleted_at IS NULL", userIDs, true).
		Scan(&rows).Error
	if err != nil {
		return nil, errcode.New(errcode.Internal, "查询用户部门失败").WithDetail(err.Error())
	}
	for _, r := range rows {
		out[r.UserID] = UserDepartment{
			ID:        r.DeptID,
			Name:      r.DeptName,
			Code:      r.DeptCode,
			IsPrimary: true,
		}
	}
	return out, nil
}

// ===== Create =====

// Create 新增用户
//  - username 唯一(数据库 uniqueIndex 兜底)
//  - password 必填(frontend 已 pre-hash 为 64 字符 hex)
//  - identities 可选(创建后批量写 sys_user_identity)
//  - 部门(本期范围外,先忽略 CreateUserRequest.DepartmentID 字段;
//    后续 phase 接 sys_user_department 多对多)
func (s *userService) Create(ctx context.Context, caller *Caller, req CreateUserRequest) (uint64, error) {
	if caller == nil || caller.UserID == 0 {
		return 0, errcode.New(errcode.Unauthorized, "未登录")
	}
	if err := MustNoEmpty("username", req.Username); err != nil {
		return 0, err
	}
	if err := MustNoEmpty("password", req.Password); err != nil {
		return 0, err
	}
	if len(req.Password) != 64 {
		return 0, errcode.New(errcode.InvalidParam, "密码格式错误(需前端 pre-hash)")
	}

	// 1. username 唯一性预检
	var exists int64
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("username = ?", req.Username).Count(&exists).Error; err != nil {
		return 0, errcode.New(errcode.Internal, "校验用户名失败").WithDetail(err.Error())
	}
	if exists > 0 {
		return 0, errcode.New(errcode.Conflict, "用户名已存在")
	}

	// 2. 密码哈希
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return 0, errcode.New(errcode.Internal, "密码哈希失败").WithDetail(err.Error())
	}

	// 3. 手机号盲索引(若填了)
	phoneHash := ""
	if req.Phone != "" {
		phoneHash = desensitize.PhoneHash(req.Phone)
	}

	// 4. 构造 user
	userID := sonyflake.Next()
	now := time.Now()
	u := &model.SysUser{
		BaseModel:     model.BaseModel{ID: userID},
		Username:      req.Username,
		PasswordHash:  hash,
		Nickname:      firstNonEmpty(req.Nickname, req.Username),
		Phone:         req.Phone,
		PhoneHash:     phoneHash,
		Email:         req.Email,
		Avatar:        "",
		IsActive:      true,
		MustChangePwd: true,
		LastLoginAt:   nil,
	}
	// 注:BaseModel.CreatedAt/UpdatedAt 由 gorm 维护,这里赋值保险
	_ = now

	// 5. 同事务写 user + departments + identities
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(u).Error; err != nil {
			return err
		}
		// 5a. 写部门多对多(sys_user_department)
		if err := writeUserDepartments(tx, userID, req.DepartmentIDs.Uints(), req.PrimaryDepartmentID); err != nil {
			return err
		}
		// 5b. 写身份(多身份;Identities 非空用 Identities,空但有旧 IdentityIDs 则用占位,全空不写)
		if err := writeUserIdentitiesFromItems(tx, userID, req.Identities, req.IdentityIDs.Uints()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, errcode.New(errcode.Internal, "创建用户失败").WithDetail(err.Error())
	}
	return userID, nil
}

// ===== Update =====

// Update 更新用户
//   - 指针字段:nil = 不改;非 nil = 改
//   - identities 整体替换(传 nil = 不动;传 [] = 全量替换)
//   - phone/email 本期直接覆盖(后续 phase 走加密)
func (s *userService) Update(ctx context.Context, caller *Caller, id uint64, req UpdateUserRequest) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}

	// 1. 加载 user
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "用户不存在")
		}
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}

	updates := map[string]any{}

	// nickname
	if req.Nickname != nil {
		nick := strings.TrimSpace(*req.Nickname)
		if nick == "" {
			return errcode.New(errcode.InvalidParam, "昵称不能为空")
		}
		if len(nick) > 64 {
			return errcode.New(errcode.InvalidParam, "昵称不能超过 64 字符")
		}
		updates["nickname"] = nick
	}
	// phone(本期明文覆盖;后续 phase 改加密)
	if req.Phone != nil {
		updates["phone"] = *req.Phone
		updates["phone_hash"] = desensitize.PhoneHash(*req.Phone)
	}
	// email(本期明文覆盖)
	if req.Email != nil {
		updates["email"] = *req.Email
	}
	// is_active
	if req.IsActive != nil {
		// 不能停用自己
		if id == caller.UserID && !*req.IsActive {
			return errcode.New(errcode.InvalidParam, "不能停用自己")
		}
		// 不能停用 super_admin
		if !*req.IsActive {
			var count int64
			if err := s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
				Where("user_id = ? AND role_code = ? AND is_active = ?", id, model.RoleSuperAdmin, true).
				Count(&count).Error; err != nil {
				return errcode.New(errcode.Internal, "校验角色失败").WithDetail(err.Error())
			}
			if count > 0 {
				return errcode.New(errcode.Forbidden, "不能停用超级管理员")
			}
		}
		updates["is_active"] = *req.IsActive
	}
	// 2. 同事务:user 更新 + (可选)部门全量替换 + (可选)身份全量替换
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&u).Updates(updates).Error; err != nil {
				return err
			}
		}
		// 2a. 部门全量替换(DepartmentIDs 字段非 nil 即触发,含清空)
		if req.DepartmentIDs != nil {
			if err := writeUserDepartments(tx, id, req.DepartmentIDs.Uints(), derefUint64(req.PrimaryDepartmentID)); err != nil {
				return err
			}
		}
		// 2b. 身份全量替换(Identities / IdentityIDs 任一非 nil 即触发)
		//   新协议优先:Identities 非空用 Identities;否则 fallback 到旧 IdentityIDs 占位逻辑(兼容老前端)
		if req.Identities != nil || req.IdentityIDs != nil {
			var items []IdentityItem
			if req.Identities != nil && len(*req.Identities) > 0 {
				items = *req.Identities
			} else if req.IdentityIDs != nil {
				// 兼容旧路径:用 IdentityIDs 数量建 N 条占位
				for range *req.IdentityIDs {
					items = append(items, IdentityItem{RoleCode: "role_user_admin", RoleName: "后台员工"})
				}
			}
			if err := writeUserIdentitiesFromItems(tx, id, items, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errcode.New(errcode.Internal, "更新用户失败").WithDetail(err.Error())
	}
	// 身份被直接编辑 → 广播"权限失效":存量 token 里的 role 不会自己变,
	// 在线用户的菜单/按钮要靠这条广播触发 re-fetch 才会刷新
	// (同 role_impl.go 的 role_resource_changed 模式;仅身份替换这一处触发,不扩散)。
	if req.Identities != nil || req.IdentityIDs != nil {
		permnotify.PublishInvalidateAll(ctx, caller.UserID, "user_role_changed")
	}
	return nil
}

// ===== Delete =====

// Delete 软删除用户
//  - 不能删自己
//  - 不能删 super_admin
//  - 级联软删 identities(显式,不依赖外键)
func (s *userService) Delete(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	if id == caller.UserID {
		return errcode.New(errcode.InvalidParam, "不能删除自己")
	}
	// 校验不是 super_admin
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
		Where("user_id = ? AND role_code = ? AND is_active = ?", id, model.RoleSuperAdmin, true).
		Count(&count).Error; err != nil {
		return errcode.New(errcode.Internal, "校验角色失败").WithDetail(err.Error())
	}
	if count > 0 {
		return errcode.New(errcode.Forbidden, "不能删除超级管理员")
	}

	// 软删
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.SysUser{}, id).Error; err != nil {
			return err
		}
		// 用户本身是软删(要能找回),但身份是绑定行 —— 硬删。
		// 旧注释写的是"无 DeletedAt 字段",那是不成立的:SysUserIdentity
		// 内嵌的是 BaseModel,有 DeletedAt,所以原来的 Delete() 其实是软删。
		if err := clearBindings(tx, &model.SysUserIdentity{}, "user_id = ?", id); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return errcode.New(errcode.Internal, "删除用户失败").WithDetail(err.Error())
	}
	return nil
}

// ===== BatchRemove =====

// BatchRemove 2026-10-19 M63a:批量删除用户
//   - 规则同 Delete:不能删自己/不能删 super_admin
//   - 一次性预校验所有 ID(避免半成功半失败)
//   - 单事务原子删 user + identities
//   - 审计: 写 1 条 sys_operation_log(批量操作用 description 记录 ID 列表)
func (s *userService) BatchRemove(ctx context.Context, caller *Caller, ids []uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if len(ids) == 0 {
		return errcode.New(errcode.BadRequest, "ids 不能为空")
	}
	if len(ids) > 200 {
		return errcode.New(errcode.BadRequest, "单次最多删除 200 个")
	}
	// 禁删自己
	for _, id := range ids {
		if id == caller.UserID {
			return errcode.New(errcode.InvalidParam, "不能删除自己")
		}
	}
	// 去重(防前端误传重复 ID)
	uniq := make(map[uint64]struct{}, len(ids))
	deduped := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := uniq[id]; ok {
			continue
		}
		uniq[id] = struct{}{}
		deduped = append(deduped, id)
	}
	if len(deduped) == 0 {
		return errcode.New(errcode.BadRequest, "ids 全为 0 或重复")
	}
	// 校验不含 super_admin
	var superCount int64
	if err := s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
		Where("user_id IN ? AND role_code = ? AND is_active = ?", deduped, model.RoleSuperAdmin, true).
		Count(&superCount).Error; err != nil {
		return errcode.New(errcode.Internal, "校验角色失败").WithDetail(err.Error())
	}
	if superCount > 0 {
		return errcode.New(errcode.Forbidden, "不能删除超级管理员")
	}
	// 校验 ID 都存在(避免外键 FK 静默跳过)
	var existCount int64
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id IN ?", deduped).Count(&existCount).Error; err != nil {
		return errcode.New(errcode.Internal, "校验用户失败").WithDetail(err.Error())
	}
	if int(existCount) != len(deduped) {
		return errcode.New(errcode.NotFound, "部分用户不存在")
	}
	// 单事务原子删
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 绑定行硬删(见 clearBindings);user 本体下面才是软删
		if err := clearBindings(tx, &model.SysUserIdentity{}, "user_id IN ?", deduped); err != nil {
			return err
		}
		if err := clearBindings(tx, &model.SysUserDepartment{}, "user_id IN ?", deduped); err != nil {
			return err
		}
		// user 软删
		if err := tx.Delete(&model.SysUser{}, deduped).Error; err != nil {
			return err
		}
		// 审计: 批量操作写 1 条 sys_operation_log(RBAC 中间件也写 1 条 per request,
		// 这里额外写 1 条业务细表,带 ID 列表,便于审计页查询)
		idsStr := make([]string, 0, len(deduped))
		for _, id := range deduped {
			idsStr = append(idsStr, strconv.FormatUint(id, 10))
		}
		opLog := &model.SysOperationLog{
			BaseModel:   model.BaseModel{ID: sonyflake.Next()},
			UserID:      caller.UserID,
			Module:      "user",
			Action:      "batch-delete",
			Status:      1,
			Description: "count=" + strconv.FormatUint(uint64(len(deduped)), 10) + " ids=[" + strings.Join(idsStr, ",") + "]",
		}
		if err := tx.Create(opLog).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return errcode.New(errcode.Internal, "批量删除失败").WithDetail(err.Error())
	}
	return nil
}

// ===== Unlock =====

// Unlock 解锁账号
//  - 清 failed_count
//  - 清 locked_until
//  - 不改 must_change_pwd
func (s *userService) Unlock(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	// 校验存在
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "用户不存在")
		}
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	if err := s.db.WithContext(ctx).Model(&u).Updates(map[string]any{
		"failed_count": 0,
		"locked_until": nil,
	}).Error; err != nil {
		return errcode.New(errcode.Internal, "解锁失败").WithDetail(err.Error())
	}
	logger.L().Info("user unlocked", zap.Uint64("user_id", id), zap.Uint64("by", caller.UserID))
	return nil
}

// DisableUserMfa 管理员关闭某用户的登录二次验证(M150)
//
//   前面那条注释写的是"为什么要有这个端点"(手机丢失 = 自助关闭死锁)。
//   这里补一条"为什么关了要踢会话":安全设置被**外人**(管理员代操作)动过,
//   最坏情形是"管理员账号被盗、挨个关掉别人的 MFA 再慢慢来" ——
//   推作废线让目标用户全部重新登录,攻击窗口从 8h 缩到 0。
//   Unlock 不踢会话是因为解锁后**本人**还在登录态;这个动作动的就是本人。
func (s *userService) DisableUserMfa(ctx context.Context, caller *Caller, id uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "用户不存在")
		}
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	if !u.MfaEnabled {
		return errcode.New(errcode.StateInvalid, "该账号未开启二次验证")
	}
	if err := s.db.WithContext(ctx).Model(&model.SysUser{}).
		Where("id = ?", id).
		Update("mfa_enabled", false).Error; err != nil {
		return errcode.New(errcode.Internal, "关闭二次验证失败").WithDetail(err.Error())
	}
	// 推作废线:下一次请求起,该用户所有会话重新走完整登录(含 MFA 关闭后的新状态)
	invalidateUserSessions(ctx, id, "mfa-disabled-by-admin")
	logger.L().Info("user mfa disabled by admin",
		zap.Uint64("user_id", id), zap.Uint64("by", caller.UserID))
	return nil
}

// ===== ToggleActive =====

// ToggleActive 切换启用/停用
//  - active=true  启用(is_active=true, 清 must_change_pwd=false 保持原值)
//  - active=false 停用(is_active=false,级联停用所有 active identity)
//  - 不能停用自己/不能停用 super_admin
func (s *userService) ToggleActive(ctx context.Context, caller *Caller, id uint64, active bool) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if id == 0 {
		return errcode.New(errcode.BadRequest, "id 不能为空")
	}
	if id == caller.UserID && !active {
		return errcode.New(errcode.InvalidParam, "不能停用自己")
	}
	if !active {
		var count int64
		if err := s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
			Where("user_id = ? AND role_code = ? AND is_active = ?", id, model.RoleSuperAdmin, true).
			Count(&count).Error; err != nil {
			return errcode.New(errcode.Internal, "校验角色失败").WithDetail(err.Error())
		}
		if count > 0 {
			return errcode.New(errcode.Forbidden, "不能停用超级管理员")
		}
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SysUser{}).Where("id = ?", id).
			Update("is_active", active).Error; err != nil {
			return err
		}
		if !active {
			// 停用时:级联停用 identity
			if err := tx.Model(&model.SysUserIdentity{}).
				Where("user_id = ?", id).
				Update("is_active", false).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errcode.New(errcode.Internal, "切换启用状态失败").WithDetail(err.Error())
	}
	// 停用必须与改密码同强度:推作废线把存量 access token(8h)一次性清零。
	// 不能只在 authz loader 加 is_active 过滤 —— principal 加载失败时中间件会
	// 降级放行 JWT 里的 role,过滤等于没拦;唯一可靠的口径是令牌本身作废
	// (同 DisableUserMfa 的做法)。
	if !active {
		invalidateUserSessions(ctx, id, "user-disabled-by-admin")
		logger.L().Info("user disabled by admin, sessions invalidated",
			zap.Uint64("user_id", id), zap.Uint64("by", caller.UserID))
	}
	return nil
}

// ===== GrantIdentity =====

// GrantIdentity 给用户授予身份(新增一条 sys_user_identity)
func (s *userService) GrantIdentity(ctx context.Context, caller *Caller, userID, identityID uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if userID == 0 || identityID == 0 {
		return errcode.New(errcode.BadRequest, "userId / identityId 必填")
	}
	// 1. 校验 user 存在
	var u model.SysUser
	if err := s.db.WithContext(ctx).First(&u, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.New(errcode.NotFound, "用户不存在")
		}
		return errcode.New(errcode.Internal, "查询用户失败").WithDetail(err.Error())
	}
	// 2. 校验 identityId 实际是哪个 role/project(本期简化:直接用默认 role_code)
	//    真实做法:前端传完整 identity object(role_code, project_id, dept_id, valid_from/to),
	//             服务端组装后落库;此处先做单字段占位
	ident := &model.SysUserIdentity{
		BaseModel: model.BaseModel{ID: sonyflake.Next()},
		UserID:    userID,
		RoleCode:  "role_user_admin",
		RoleName:  "后台员工",
		IsActive:  true,
		IsPrimary: false,
		ValidFrom: model.Time{Time: time.Now()},
	}
	if err := s.db.WithContext(ctx).Create(ident).Error; err != nil {
		return errcode.New(errcode.Internal, "授予身份失败").WithDetail(err.Error())
	}
	return nil
}

// ===== RevokeIdentity =====

// RevokeIdentity 撤销用户身份(软删 sys_user_identity)
func (s *userService) RevokeIdentity(ctx context.Context, caller *Caller, userID, identityID uint64) error {
	if caller == nil || caller.UserID == 0 {
		return errcode.New(errcode.Unauthorized, "未登录")
	}
	if userID == 0 || identityID == 0 {
		return errcode.New(errcode.BadRequest, "userId / identityId 必填")
	}
	// 不能撤销自己的 primary
	if userID == caller.UserID {
		var cnt int64
		if err := s.db.WithContext(ctx).Model(&model.SysUserIdentity{}).
			Where("id = ? AND user_id = ? AND is_primary = ?", identityID, userID, true).
			Count(&cnt).Error; err != nil {
			return errcode.New(errcode.Internal, "校验身份失败").WithDetail(err.Error())
		}
		if cnt > 0 {
			return errcode.New(errcode.InvalidParam, "不能撤销自己的主身份")
		}
	}
	// 硬删。旧注释写的是"软删(本期:硬删,无 DeletedAt 字段)" —— 两半自相矛盾,
	// 而且后半句不成立:SysUserIdentity 内嵌 BaseModel,有 DeletedAt。
	res := s.db.WithContext(ctx).Unscoped().
		Where("id = ? AND user_id = ?", identityID, userID).
		Delete(&model.SysUserIdentity{})
	if res.Error != nil {
		return errcode.New(errcode.Internal, "撤销身份失败").WithDetail(res.Error.Error())
	}
	if res.RowsAffected == 0 {
		return errcode.New(errcode.NotFound, "身份不存在或不属于该用户")
	}
	return nil
}

// ===== helpers =====

// firstNonEmpty 返回第一个非空字符串
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
