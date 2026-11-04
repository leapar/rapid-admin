// 鉴权相关的前端视图 DTO
//
// 放在 service 层而不是 handler 层的原因:login / refresh / switch-identity 三个
// 端点都要返同一套结构,而它们的实现分散在 service(签 token)和 handler(拼 user)。
// DTO 放 handler 就意味着"谁返就用谁的形状",refresh 会退化成直接吐
// *model.SysUser —— 那个结构里根本没有 identities 字段,前端续期之后身份列表
// 静默消失。放 service 层 + 具名 struct,让形状分叉从"约定问题"变成"类型问题"。
package service

import (
	"time"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/desensitize"
)

// IdentityView 单个身份的前端视图
// 与 web/src/types/user.ts 的 Identity 字段对齐
type IdentityView struct {
	ID          uint64   `json:"id,string"`
	RoleCode    string   `json:"role_code"`
	RoleName    string   `json:"role_name"`
	ProjectID   uint64   `json:"project_id,string"`
	ProjectName string   `json:"project_name"` // 后端暂无,空串兜底
	Permissions []string `json:"permissions"`  // 后端暂无,空数组兜底
	ValidFrom   string   `json:"valid_from"`
	ValidTo     string   `json:"valid_to"` // 后端暂无,空串兜底
	IsActive    bool     `json:"is_active"`
	IsPrimary   bool     `json:"is_primary"`
}

// CurrentUserView 当前用户的前端视图
// 与 web/src/types/user.ts 的 UserInfo 字段对齐(snake_case)
//
// ⚠️ **M149(2026-11-01)新增 `current_role_code`**
//
//	前端以前靠 `identities.find(i => i.id === current_identity_id)` 反推
//	"当前身份的角色" —— 那是**猜**,有两处会猜错:
//
//	① 身份被管理员改过(后端 `writeUserIdentitiesFromItems` 是**全量替换**,
//	   身份行的 ID 全变)⇒ 旧 session 的 `current_identity_id` 在列表里
//	   **找不到** ⇒ 前端 roleCodes 变空 ⇒ 角色门控(AuthButton 的 roleAnyOf)
//	   整个失效(按钮凭空消失);
//
//	② 身份行在库里被改成别的角色 ⇒ 前端显示**新**角色、后端按 token 里的
//	   **旧**角色判(执行层用的是 token claim)。
//
//	⇒ 直接下发权威值:`current_role_code` = token claims 里那个 role_code,
//	  **与 RBAC 中间件判权限用的是同一个来源** ⇒ 界面显示的 = 实际能做的。
//
// ⚠️ **M113(2026-10-29):email 之前是脱敏的(而且脱敏本身就是个 bug)**
//
//	旧实现:`Email: desensitize.Email(decryptPII(u.Email))`
//	⇒ "zhangsan@qq.com" 变成 "z******@qq.com"。
//
//	个人中心的编辑表单把这个值**回填进可编辑的输入框**,用户改个昵称点保存,
//	后端收到的 email 就是 "z******@qq.com" ⇒ **把用户的真实邮箱写成了掩码串**。
//	(症状:改一次资料,邮箱就废了。)
//
//	为什么这里不能脱敏:
//
//  1. 这是**用户自己的**邮箱,而且这个端点只对本人可见(不像手机号会被别人看)
//  2. 管理员的用户列表 `UserListItem.Email` **本来就是明文** —— 脱敏只发生在
//     "自己看自己"这一处,既不一致,又挡不住真正想看的人
//  3. 技术方案 §9.3.2 的展示矩阵是给"列表/详情给别人看"用的;
//     §9.3.4 也写明前端 `mask.*` 只是 UI 兜底,不是安全边界
//
//	⇒ 这里返真实邮箱。手机号仍然脱敏(手机号要发短信、要被管理员看,口径不同)。
type CurrentUserView struct {
	ID                   uint64         `json:"id,string"`
	Username             string         `json:"username"`
	Nickname             string         `json:"nickname"`
	RealName             string         `json:"real_name"` // M113 补:以前压根没这个字段
	Phone                string         `json:"phone"`     // 已脱敏 138****1234
	Email                string         `json:"email"`     // 自己的邮箱不脱敏(见上方)
	Avatar               string         `json:"avatar"`
	Identities           []IdentityView `json:"identities"`
	CurrentIdentityID    uint64         `json:"current_identity_id,string"`
	CurrentRoleCode      string         `json:"current_role_code"`
	IsPlatformSuperAdmin bool           `json:"is_platform_super_admin"`
	IsPlatformOps        bool           `json:"is_platform_ops"`
	// MfaEnabled 登录二次验证开关(M150)。
	// 个人中心的 MFA 卡片按它渲染"已开启/未开启"—— 前端不自己存这个状态,
	// 五个端点(login/refresh/switch/me/profile)都下发最新值,谁最后返回谁生效。
	MfaEnabled bool `json:"mfa_enabled"`
}

// NewCurrentUserView 把 model.SysUser + identities 组装成前端视图
//
// currentIdentityID 是"服务端认定当前生效的身份":
//   - Login / LoginBySms → primary.ID(已经选好了主身份)
//   - SwitchIdentity      → target.ID(刚切过去的那个)
//   - RefreshToken        → 从 refresh token 的 claims 里解出来的 IdentityID
//   - GetCurrentUser      → handler 从 caller.IdentityID 拿
//
// 四个入口都给真值,前端不需要(也不应该)再靠 identities[0] 猜 —— 猜错的表现
// 是"token 里已经是 A 身份,界面还高亮 B 身份",而且不会报错。
//
// ⚠️ **M149(2026-11-01):参数从 `isSuper, isOps bool` 收成 `currentRoleCode string`**
//
//	以前是两个 bool 进来、判定在**调用方**做(每个调用方各写一遍
//	`hasSuperRole(x), hasOpsRole(x)`)⇒ 两份真相,迟早漂移。
//	现在只收**当前身份的角色码**,两个标志在**这里**一次性派生
//	(`platformFlags` —— 全仓唯一的判定口径)。
//
//	currentRoleCode 必须是**服务端权威值**(= token claims 里那个 role_code):
//
//	  · Login / LoginBySms → primary.RoleCode(已经选好了主身份)
//	  · SwitchIdentity      → target.RoleCode(刚切过去的那个)
//	  · RefreshToken        → claims.RoleCode
//	  · GetCurrentUser      → caller.RoleCode
func NewCurrentUserView(u *model.SysUser, identities []model.SysUserIdentity,
	currentRoleCode string, currentIdentityID uint64) *CurrentUserView {

	isSuper, isOps := platformFlags(currentRoleCode)

	idtList := make([]IdentityView, 0, len(identities))
	for _, id := range identities {
		idtList = append(idtList, IdentityView{
			ID:          id.ID,
			RoleCode:    id.RoleCode,
			RoleName:    id.RoleName,
			ProjectID:   id.ProjectID,
			ProjectName: "", // 后端暂无 projectName 字段
			Permissions: []string{},
			ValidFrom:   formatIdentityTime(id.ValidFrom.Time),
			ValidTo:     "", // 后端暂无
			IsActive:    id.IsActive,
			IsPrimary:   id.IsPrimary,
		})
	}
	return &CurrentUserView{
		ID:                   u.ID,
		Username:             u.Username,
		Nickname:             u.Nickname,
		RealName:             u.RealName,
		Phone:                desensitize.Phone(decryptPII(u.Phone)),
		Email:                decryptPII(u.Email),
		Avatar:               u.Avatar,
		Identities:           idtList,
		CurrentIdentityID:    currentIdentityID,
		CurrentRoleCode:      currentRoleCode,
		IsPlatformSuperAdmin: isSuper,
		IsPlatformOps:        isOps,
		MfaEnabled:           u.MfaEnabled,
	}
}

func formatIdentityTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// decryptPII 解密 model 上按 AES-256-GCM 加密的字段
//
// ⚠️ **M104(2026-10-28)修:这里原来恒返空串。**
//
// 后果不是"少显示一个字段" —— 它把整条短信二次验证链路掐死了:
//   /auth/me 的 phone 恒为 "" → 前端 IdentitySwitcher 拿不到手机号
//   → SmsCodeGuard 点「发送验证码」只弹「请先完善手机号信息」
//   → **切到超管身份这个功能根本走不通**(M103 把超管补进高敏名单之后暴露:
//     之前超管不在名单里,压根不弹这个框,"能切"是因为压根没做二次验证)。
//
// 为什么原样返回就是**真值**:`sys_user.phone / email` **本期是明文落库** ——
//   · service/user_impl.go:471 `Phone: req.Phone`(创建用户)
//   · service/user_impl.go:541 `updates["phone"] = *req.Phone`(改手机号)
//   · service/auth_impl.go:1058 `updates["email"] = *dto.Email`(改邮箱)
// 三条写入路径全是明文,没有一条走加密。所以这一层"解密"今天没有东西可解。
//
// 原来的注释担心的是"假装解密成功,把密文当明文发出去" —— 那个担心在
// **数据真的加密之后**才成立;而在那之前,把真值藏起来只会有"接口看着通、
// 数据全是空"这种查不出来的洞。
//
// TODO(P1+): 接入 crypto.DecryptFor(ctx, crypto.TierPII, ct) 后,
//   本函数改成真解密(签名要加 ctx),届时明文兼容分支自然消失。
func decryptPII(ciphertext string) string {
	return ciphertext
}

// AuthTokens 登录 / 续期 / 切换身份的**统一**响应体
//
// ⚠️ Token 的 json tag 是 `token` 而不是 `access_token` —— 这是 login、
// switch-identity、技术方案 §7.7.3 一贯在用的名字。前端曾按 `access_token`
// 取值,导致自动续期拿到 undefined、发 `Bearer undefined`、把用户踢回登录页。
// 改这个 tag 会同时波及另外两处调用方,对齐方向永远是"前端改向前端"。
//
// ⚠️ 这三个端点共用**这一个**结构是刻意的。此前 Login / SwitchIdentity 在
// handler 里各自拼 map,RefreshToken 在 service 里直吐 *model.SysUser ——
// 三份代码各自演进,refresh 那份缺 identities,用户续期之后身份列表静默消失。
// 统一到具名 struct 之后,形状分叉从"约定问题"变成编译期问题。
type AuthTokens struct {
	Token            string           `json:"token"`
	RefreshToken     string           `json:"refresh_token"`
	ExpiresIn        int              `json:"expires_in"`         // access token 剩余秒数
	RefreshExpiresIn int              `json:"refresh_expires_in"` // refresh token 剩余秒数
	User             *CurrentUserView `json:"user"`
}
