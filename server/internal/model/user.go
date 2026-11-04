// 系统用户(后台员工 + 供应商账号)
// 文档 §5.4
package model

// SysUser 后台用户
type SysUser struct {
	BaseModel
	Username      string `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash  string `gorm:"size:255;not null" json:"-"` // bcrypt
	Nickname      string `gorm:"size:64;not null" json:"nickname"`
	RealName      string `gorm:"size:64" json:"real_name"`
	RealNameHash  string `gorm:"size:64;index" json:"-"`
	Phone         string `gorm:"size:512" json:"-"`        // AES-256-GCM(Base64)
	PhoneHash     string `gorm:"size:64;index" json:"-"`   // HMAC-SHA256 盲索引
	Email         string `gorm:"size:512" json:"-"`
	EmailHash     string `gorm:"size:64;index" json:"-"`
	IdCard        string `gorm:"size:1024" json:"-"`       // 加密身份证
	IdCardHash    string `gorm:"size:64;index" json:"-"`
	Avatar        string `gorm:"size:255" json:"avatar"`
	IsActive      bool   `gorm:"default:true;index" json:"is_active"`
	IsLeave       bool   `gorm:"default:false" json:"is_leave"`
	MustChangePwd bool   `gorm:"default:true" json:"must_change_pwd"`
	PwdExpireAt   *Time  `gorm:"index" json:"pwd_expire_at,omitempty"`
	LastLoginAt   *Time  `json:"last_login_at,omitempty"`
	LastLoginIP   string `gorm:"size:45" json:"last_login_ip,omitempty"`
	FailedCount   int    `gorm:"default:0" json:"-"`
	LockedUntil   *Time  `json:"locked_until,omitempty"`

	// MfaEnabled 登录二次验证(M150):开启后,密码登录要多验一条
	// 发到**登记手机号**上的短信验证码(scene=mfa)。
	//
	// ⚠️ **开着 MFA 但没绑手机号 = fail-closed**(登录被拒)——
	// 那是"二次验证"的本意:验证不了就不放行。恢复路径是管理员
	// POST /system/users/:id/mfa/disable(user:update),不是放行。
	//
	// ⚠️ 短信登录(LoginBySms)不受影响:验证码本来就发到登记手机号上,
	// "持机"这半个因子已经天然满足,不再加验(见 LoginBySms 的注释)。
	MfaEnabled bool `gorm:"default:false" json:"mfa_enabled"`

	// 文件系统模块(设计文档 §4.3)。两列都在这里,不另起一张配额表 ——
	// 配额**每个用户一行**,跟用户 1:1,拆表只会多一次 JOIN。
	//
	// StorageUsedBytes 已用字节数。
	//
	// ⚠️ **预扣 - 结算**(§4.5):上传 init 时按申报大小预扣,上传完成时按实际
	// 大小找零。扣减必须用 `UPDATE ... SET storage_used_bytes =
	// storage_used_bytes + ?` 这种**原子自增**,不能"读出来 +n 再写回去" ——
	// 后者在并发上传下必然丢更新。
	//
	// ⚠️ **它会漂移**:进程崩在"文件落盘成功、DB 记账失败"之间,或者预扣的会话
	// 过期没回滚。漂移不可怕,**没有定时对账才可怕**(§4.4 / 风险 R2)。
	StorageUsedBytes int64 `gorm:"default:0" json:"storage_used_bytes"`
	// StorageQuotaBytes 配额上限,**0 = 不限**
	StorageQuotaBytes int64 `gorm:"default:0" json:"storage_quota_bytes"`

	// 关联
	Identities []SysUserIdentity `gorm:"foreignKey:UserID" json:"identities,omitempty"`
}

// TableName
func (SysUser) TableName() string { return "sys_user" }

// QuotaExceeded 这个用户再写入 incoming 字节会不会超配额。
//
// ⚠️ **0 配额 = 不限**,不是"配额为零"(那会让所有人都传不了任何东西)。
// 这个 0 的语义必须只在这里判一次,别散落到各个上传入口 —— 三处各判一次,
// 迟早有一处写成 `if used+size > quota` 而忘了 `quota > 0`,于是不限额的账号
// 一上传就被拒,而且报错是"超出配额",极难定位。
func (u *SysUser) QuotaExceeded(used, incoming int64) bool {
	if u.StorageQuotaBytes <= 0 {
		return false // 不限
	}
	return used+incoming > u.StorageQuotaBytes
}

// QuotaRemaining 还能传多少字节。无限额时返回一个足够大的值而不是 -1,
// 让调用方少写一个分支。
//
// ⚠️ 返回值**可能为负**(已用量已经超了,比如配额被调小)。调用方不该拿它做
// 展示 —— 展示用 `QuotaExceeded` 的布尔就够了。
func (u *SysUser) QuotaRemaining(used int64) int64 {
	if u.StorageQuotaBytes <= 0 {
		return 1<<63 - 1
	}
	return u.StorageQuotaBytes - used
}

// SysUserIdentity 用户-身份(一个用户可拥有多个身份,如业主兼物业)
// 文档 §5.4
type SysUserIdentity struct {
	BaseModel
	UserID     uint64 `gorm:"index;not null" json:"user_id,string"`
	RoleCode   string `gorm:"size:64;not null;index" json:"role_code"`
	RoleName   string `gorm:"size:64" json:"role_name"`
	ProjectID  uint64 `gorm:"index" json:"project_id,string"` // 0 = 全平台
	DeptID     uint64 `gorm:"index" json:"dept_id,string"`
	ValidFrom  Time   `json:"valid_from"`
	ValidTo    Time   `json:"valid_to"`
	IsActive   bool   `gorm:"default:true;index" json:"is_active"`
	IsPrimary  bool   `gorm:"default:false;index:idx_user_primary" json:"is_primary"` // 复合索引 (user_id, is_primary) 加速"查主身份"查询;唯一性由 service 层事务保证(MySQL 无 partial unique index)
}

func (SysUserIdentity) TableName() string { return "sys_user_identity" }

// SysRole 角色
type SysRole struct {
	BaseModel
	Code        string `gorm:"size:64;uniqueIndex;not null" json:"code"`
	Name        string `gorm:"size:64;not null" json:"name"`
	Description string `gorm:"size:255" json:"description"`
	// Type 角色类型(2026-10-17 新增,文档 §6.4):
	//   - platform 平台级(跨项目,管后台):超管/平台运营/风控合规
	//   - business 业务级(项目内,做交付/物业):房源/资料/交付/物业经理/物业工作人员/供应商
	// 默认 business(最常见)
	Type      string `gorm:"size:16;default:business;index" json:"type"`
	Sort      int    `gorm:"default:999" json:"sort"`
	IsBuiltin bool   `gorm:"default:false" json:"is_builtin"`
	IsActive  bool   `gorm:"default:true;index" json:"is_active"`
}

func (SysRole) TableName() string { return "sys_role" }

// SysResource 资源(菜单/按钮/API)
// 文档 §5.4
type SysResource struct {
	BaseModel
	ParentID  uint64 `gorm:"index" json:"parent_id,string"`
	Type      string `gorm:"size:16;not null" json:"type"` // menu / button / api
	Code      string `gorm:"size:128;uniqueIndex;not null" json:"code"`
	Name      string `gorm:"size:64;not null" json:"name"`
	Path      string `gorm:"size:255" json:"path"`
	Method    string `gorm:"size:8" json:"method"`
	Icon      string `gorm:"size:64" json:"icon"`
	Sort      int    `gorm:"default:0" json:"sort"`
	IsActive  bool   `gorm:"default:true;index" json:"is_active"`
}

func (SysResource) TableName() string { return "sys_resource" }

// SysRoleResource 角色-资源
type SysRoleResource struct {
	BaseModel
	RoleID     uint64 `gorm:"uniqueIndex:uk_rr;not null" json:"role_id,string"`
	ResourceID uint64 `gorm:"uniqueIndex:uk_rr;not null" json:"resource_id,string"`
	ProjectID  uint64 `gorm:"uniqueIndex:uk_rr" json:"project_id,string"` // NULL=全部项目
}

func (SysRoleResource) TableName() string { return "sys_role_resource" }
