// UserService 用户管理
package service

import (
	"context"
	"io"
	"time"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/jsonid"
)

// IdentityItem 一条身份
//
// 用于 CreateUserRequest.Identities / UpdateUserRequest.Identities 字段;
// 与 sys_user_identity 表 1:1 投影,前端 IdentityEditorDrawer 一次提交这个结构。
//
// IsPrimary 由后端根据"主部门 + 主身份"规则在事务内统一设置(避免前端乱传);
// ValidFrom/ValidTo 用 RFC3339 字符串,空表示"立即生效 / 永久"。
//
// 2026-10-19 替代原 IdentityIDs []uint64(只能传 ID,不能配 role/project/dept 字段,
// service 硬编码 RoleCode="role_user_admin" 的 P0 bug 已修)。
type IdentityItem struct {
	RoleCode  string  `json:"role_code"`            // 必填,如 "role_owner_manager"
	RoleName  string  `json:"role_name,omitempty"`  // 快照(冗余展示用,后端不严格校验)
	ProjectID uint64  `json:"project_id,omitempty,string"` // 0 = 全平台
	DeptID    uint64  `json:"dept_id,omitempty,string"`    // 0 = 不绑部门
	ValidFrom string  `json:"valid_from,omitempty"` // RFC3339,空 = time.Now()
	ValidTo   string  `json:"valid_to,omitempty"`   // RFC3339,空 = 永久
	IsPrimary bool    `json:"is_primary,omitempty"` // 提示字段,真正写入由 ensureSinglePrimary 决定
}

// parseIdentityValidFrom 解析 RFC3339 时间字符串,空返 time.Now()
func parseIdentityValidFrom(s string) time.Time {
	if s == "" {
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now()
	}
	return t
}

// parseIdentityValidTo 解析 RFC3339,空返零值(表示永久)
func parseIdentityValidTo(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// CreateUserRequest
//
// 2026-10-19 升级(框架层多身份 + 多部门):
//   - Identities []IdentityItem 替代 IdentityIDs []uint64
//     (旧字段兼容保留,若传旧字段则按数量建 N 条"默认后台员工"占位 identity;
//      新代码不要用,前端 IdentityEditorDrawer 直接发新字段)
//   - DepartmentIDs []uint64 替代单数 DepartmentID
//   - PrimaryDepartmentID uint64 主部门(必须在 DepartmentIDs 内,否则返错)
type CreateUserRequest struct {
	Username          string        `json:"username"`
	Password          string        `json:"password"`
	Nickname          string        `json:"nickname"`
	Phone             string        `json:"phone"`
	Email             string        `json:"email"`
	DepartmentIDs     jsonid.IDList `json:"department_ids"`
	PrimaryDepartmentID uint64      `json:"primary_department_id,string"`
	Identities        []IdentityItem `json:"identities"`
	// Legacy: 旧前端兼容(2026-10-19 之前发的请求);若 Identities 为空且 IdentityIDs 非空,按数量建 N 条占位
	IdentityIDs       jsonid.IDList `json:"identity_ids,omitempty"`
}

// UpdateUserRequest
type UpdateUserRequest struct {
	Nickname *string `json:"nickname"`
	Phone *string `json:"phone"`
	Email *string `json:"email"`
	// 2026-10-19:多部门;DepartmentIDs 非 nil 表示"全量替换"(含清空,空数组=清空所有部门)
	DepartmentIDs     *jsonid.IDList `json:"department_ids"`
	PrimaryDepartmentID *uint64     `json:"primary_department_id,string"`
	// 2026-10-19:多身份;Identities 非 nil 表示"全量替换"(清空后重建)
	Identities        *[]IdentityItem `json:"identities"`
	// Legacy: 旧前端兼容
	IdentityIDs       *jsonid.IDList `json:"identity_ids,omitempty"`
	IsActive *bool `json:"is_active"`
}

// UserListQuery
type UserListQuery struct {
	model.PageRequest
	Keyword string `json:"keyword"`
	IsActive *bool `json:"is_active"`
	DepartmentID uint64 `json:"department_id,string"`
	RoleCode string `json:"role_code"`
	IdentityID uint64 `json:"identity_id,string"`
	// ProjectIDs 数据范围(由 handler 注入 caller.ProjectIDs)
	// 空 = 全平台;非空 = 仅返回关联了这些项目身份的用户
	ProjectIDs jsonid.IDList `json:"project_ids"`
}

// UserListItem 列表条目 DTO(snake_case + 派生 status,与前端 types/permission.SysUser 对齐)
//
// Phase 5 M5+ 修复(2026-10-17):
//   - 原 List 直接返回 []SysUser(模型),字段命名 camelCase + 无 status + phone/email 是密文 json:"-"
//   - 现投影成 DTO:snake_case 字段 + 从 isActive/isLeave/LockedUntil 派生 status
//     + phone/email 仅在有 caller 解密授权时填明文,否则脱敏
//   - 派生规则:
//     locked (LockedUntil 未过期)  > disabled (isActive=false)  > active
//     isLeave=true 视为 disabled(无法登录)
type UserListItem struct {
	ID                    uint64  `json:"id,string"`
	Username              string  `json:"username"`
	Nickname              string  `json:"nickname"`
	RealName              string  `json:"real_name,omitempty"`
	Phone                 string  `json:"phone,omitempty"`        // 仅 caller 有 user:read-sensitive 权限时填明文,否则空
	Email                 string  `json:"email,omitempty"`
	Avatar                string  `json:"avatar"`
	DepartmentID          uint64  `json:"department_id,string"`
	DepartmentName        string  `json:"department_name"`
	Status                string  `json:"status"`                  // active | disabled | locked
	LastLoginAt           *string `json:"last_login_at,omitempty"`
	LastLoginIP           string  `json:"last_login_ip,omitempty"`
	MfaEnabled            bool    `json:"mfa_enabled"`
	IsPlatformSuperAdmin  bool    `json:"is_platform_super_admin"`
	CreatedAt             string  `json:"created_at"`
	// Roles 这个用户**当前有效**的角色(去重后)
	//
	// 2026-10-30 M122:列表里原来**完全没有**角色信息 —— 管理员只能点进详情
	// 才看得到"这个人到底有哪些权限"。列表页扫一眼看不出权限,是用户管理页
	// 最该自明的信息之一。
	//
	// ⚠️ 语义边界(别当成"身份清单"):
	//   · 只取 **is_active = true** 的身份 —— 停用的身份不授予任何权限
	//     (authz 的 loadCaller 同样要求 is_active = true)
	//   · 同一个 role_code 在多个身份里出现只留一条 —— 这一列回答的是
	//     "这个人有什么权限",不是"这个人有几个身份"(身份清单在详情抽屉里)
	//   · **不返回** null,没有角色就是空数组
	Roles []UserRoleBrief `json:"roles"`
}

// UserRoleBrief 列表行上的角色摘要
//
// ⚠️ 与 UserDetail.Identities 的区别:那是**身份**(含项目/部门/有效期/主身份),
//   这只是**角色**(授权的最小单位)。两者不要混用。
type UserRoleBrief struct {
	RoleCode string `json:"role_code"`
	RoleName string `json:"role_name"`
}

// UserDepartment 用户所属部门(详情视图)
//
// 2026-10-25:GET /system/users/:id 之前**只 Preload 了 Identities**,
// 不返部门 → 前端编辑弹窗只能把"部门"置为只读
// (发了 department_ids 会把次要部门洗掉,因为后端是"清空重建"式全量替换)。
// 补上这个字段之后前端就能正常编辑部门了。查询见 user_impl.listUserDepartments。
type UserDepartment struct {
	ID        uint64 `json:"id,string"`
	Name      string `json:"name"`
	Code      string `json:"code,omitempty"`
	IsPrimary bool   `json:"is_primary"`
}

// UserDetail 用户详情视图
//
// ⚠️ 用**嵌入** model.SysUser,而不是把字段抄一遍:
//   嵌入后 JSON 形状与原来的 SysUser 完全一致(唯一多出来的是 departments),
//   model 以后加列这里自动跟上 —— 抄一份的话必然漂。
//
// 为什么不直接给 model.SysUser 加 Departments 字段:
//   SysUser 是 GORM 模型,加 many2many 关系会被 AutoMigrate 拿去管
//   sys_user_department 这张 join 表(而这张表是手写迁移管的,
//   多一个 is_primary / is_leader 列),两套管法会打架。
type UserDetail struct {
	model.SysUser
	Departments []UserDepartment `json:"departments"`
}

// DeriveUserStatus 从 is_active / is_leave / locked_until 派生 status
func DeriveUserStatus(isActive, isLeave bool, lockedUntil *model.Time) string {
	if lockedUntil != nil && !lockedUntil.IsZero() && lockedUntil.Time.After(time.Now()) {
		return "locked"
	}
	if !isActive || isLeave {
		return "disabled"
	}
	return "active"
}

// UserService 用户管理
type UserService interface {
	List(ctx context.Context, q UserListQuery) (*model.PageResponse, error)
	// Get 取用户详情(含 identities + departments)
	//
	// ⚠️ 2026-10-25 返回类型从 *model.SysUser 改成 *UserDetail:
	//   详情页要展示/编辑"所属部门",而 SysUser 里没有这个字段。
	//   UserDetail 嵌入 SysUser,JSON 形状不变(只多一个 departments)。
	Get(ctx context.Context, id uint64) (*UserDetail, error)
	Create(ctx context.Context, caller *Caller, req CreateUserRequest) (uint64, error)
	Update(ctx context.Context, caller *Caller, id uint64, req UpdateUserRequest) error
	Delete(ctx context.Context, caller *Caller, id uint64) error
	// BatchRemove 2026-10-19 M63a:批量删除用户(同 Delete 规则:禁删自己/超管)
	//   - ids 必须非空
	//   - 任一 ID 失败回滚整体事务(原子)
	//   - 审计: 写 1 条 sys_operation_log.action=batch-delete, description 列出 ID 列表
	BatchRemove(ctx context.Context, caller *Caller, ids []uint64) error
	Unlock(ctx context.Context, caller *Caller, id uint64) error
	// DisableUserMfa 管理员关闭某用户的登录二次验证(M150)。
	//
	//   存在的理由:用户开着 MFA 却**手机丢了/换号收不到码** → 密码登录
	//   fail-closed,本人自助关 MFA 又必须有验证码 —— 死锁。管理员关闭
	//   是唯一恢复路径(资源码 user:update,与解锁/启停用同档)。
	//   关闭后立即推作废线(invalidateUserSessions):安全设置被外人动过,
	//   所有会话重新登录一遍不亏。
	//   未开启时报 StateInvalid(幂等请求不静默成功,口径同 EnableMfa)。
	DisableUserMfa(ctx context.Context, caller *Caller, id uint64) error
	ToggleActive(ctx context.Context, caller *Caller, id uint64, active bool) error
	GrantIdentity(ctx context.Context, caller *Caller, userID uint64, identityID uint64) error
	RevokeIdentity(ctx context.Context, caller *Caller, userID uint64, identityID uint64) error
	// SetAvatar 设置**当前用户自己**的头像(M124)。
	//
	// ⚠️ **头像是唯一一条写通道** —— 早期版本 `UpdateProfileDTO.Avatar`
	// 允许前端直接传一个**任意 URL**,那条路已删(见 user_avatar.go 的说明)。
	// 现在头像只能**上传**,不能贴外链。
	//
	// 落盘走文件系统(M80):biz = "avatar"、folder_id = 0 ⇒ key 形如
	// "{root}/{user_id}/avatar/{folder_id}/{name}" —— 即"用户 id 相关文件夹"。
	// 存进 `sys_user.avatar` 的是**稳定直链**(不是签名 URL):
	// 签名 URL 会过期,而头像要一直能显示。
	SetAvatar(ctx context.Context, caller *Caller, req AvatarUploadReq) (*AvatarView, error)
}

// AvatarUploadReq 上传头像的入参。
//
// ⚠️ 与 `FileReceiveReq` 同形(不重复造一个 multipart 结构),
// 但**只接受图片** —— 校验在 service 层做,不在 handler 层。
type AvatarUploadReq struct {
	// FileName 原始文件名(扩展名参与 MIME 探测)
	FileName string
	// ContentType 浏览器上报的类型。**只作参考**,判定以服务端探测 + 魔数为准
	ContentType string
	// Size 字节数
	Size int64
	// Body 内容流
	Body io.Reader
}

// AvatarView 头像视图
type AvatarView struct {
	// URL 稳定直链(可直接塞进 <img src>)
	URL string `json:"url"`
	// FileID 文件 id(字符串形式,雪花 id 不能用 Number)
	FileID string `json:"file_id"`
	// FileName 原始文件名
	FileName string `json:"file_name"`
	// FileSize 字节数
	FileSize int64 `json:"file_size"`
}
