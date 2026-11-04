// AuthService 鉴权服务接口
// 文档 §8.1
package service

import (
	"context"

	"rapid-admin/internal/model"
)

// LoginRequest 登录请求
//
// JSON tag 强制 snake_case(2026-10-18 修复):
//   - Go encoding/json 默认走 case-insensitive 匹配,纯字母大小写 OK
//   - 但 snake_case 的下划线无法匹配 camelCase(下划线不是字母 case),例如
//     `captcha_id` 不会自动匹配 `CaptchaID`,导致 handler 拿到空串
//   - 教训:所有跨 JSON 边界的 service DTO 必须显式打 json tag
//   - LoginIP / UserAgent 不从 body 来(handler 注入),不暴露给前端
type LoginRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"` // 前端已 SHA-256
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
	// MfaCode 登录二次验证码(M150)。
	//
	// 开启了 MFA 的账号:第一次登录不带它 → 服务端自动发码并返
	// errcode.MfaRequired(20109);前端把它填上重新提交(**密码也一起重发**,
	// 挑战是无状态的,不引入第三种短期凭证)。
	MfaCode   string `json:"mfa_code"`
	LoginIP   string `json:"-"`
	UserAgent string `json:"-"`
}

// AuthService 鉴权服务
type AuthService interface {
	// Login 登录
	Login(ctx context.Context, req LoginRequest) (*AuthTokens, error)
	// Logout 登出(revoke 当前 jti + 写 sys_login_log.LogoutAt)
	Logout(ctx context.Context, caller *Caller) error
	// TouchLoginSession 记一次"这条会话刚刚有活动"(节流写 sys_login_log.LastActiveAt)
	//
	// ⚠️ **纯记账,不是判据** —— 写失败只影响"活跃设备列表"的显示精度,
	// 不影响任何放行判定(放行看的是吊销存储的三道闸:jti / sid / 作废线)。
	TouchLoginSession(ctx context.Context, caller *Caller) error
	// RefreshToken 刷新 access token
	RefreshToken(ctx context.Context, refreshToken string) (*AuthTokens, error)
	// ChangePassword 修改自己的密码(校验旧密码 + revoke 当前 jti)
	ChangePassword(ctx context.Context, caller *Caller, oldPwd, newPwd string) error
	// ResetPassword 重置密码
	//   - 深度防御:handler 已做 IDOR 拦截(target == caller.UserID 或 caller 是 super_admin)
	//   - 这里再校一次 + revoke 当前 jti
	ResetPassword(ctx context.Context, caller *Caller, targetUserID uint64, newPwd string) error
	// SendSms 发送短信验证码(scene: view_full_phone / reset_password / change_phone)
	SendSms(ctx context.Context, phone, scene string) error
	// VerifySms 校验短信验证码
	//   单次性: 校验通过立即标记 used_at, 不可再次使用 (与 captcha 一次性语义对齐)
	//   写入失败 / 并发双提交 都会被识别为 "已使用", 不允许复用
	VerifySms(ctx context.Context, phone, scene, code string) (bool, error)
	// SwitchIdentity 切换身份(返回新 token + **换会话**)
	//   smsCode 切换到高敏角色时必填, 其他角色可空
	//
	//   ⚠️ 2026-11-01(M148):切换会**换 sid 并吊销旧 sid** ——
	//   切换**之前**那张 refresh 立刻作废(它换发的是**旧身份**的 access)。
	//   顺带把 caller 改成切换后的身份(handler 的角色切换审计从那儿取 to_role)。
	//   见 auth_impl.go 的 rotateSession。
	SwitchIdentity(ctx context.Context, caller *Caller, identityID uint64, smsCode string) (*AuthTokens, error)
	// SendAccountSms 给**当前账号登记的手机号**发验证码(2026-10-28 M104)
	//
	//   **手机号由服务端从 sys_user 取**,前端不传、也拿不到明文:
	//   /auth/me 只返脱敏值(138****5678),拿它发不出短信(后端 validPhone 拒)。
	//
	//   存在的理由:有两个前端场景必须要短信,但**手上只有脱敏号** ——
	//     ① 切到高敏身份(scene=change_phone,码由 SwitchIdentity 消费)
	//     ② 个人中心改密码(scene=reset_password,码由 VerifyAccountSms 消费)
	//   以前这两个场景都走公开接口 /public/auth/sms/send + 前端自己传手机号,
	//   于是**两条都是死的**(脱敏号过不了 validPhone)。
	//
	//   scene 白名单见 accountSmsScenes —— login 不在其中(它是"手机号还没绑到
	//   这个账号"时用的,自助发给自己登记的号没有意义)。
	SendAccountSms(ctx context.Context, caller *Caller, scene string) error
	// VerifyAccountSms 校验并消费发给当前账号登记手机号的验证码
	//
	//   与公开的 VerifySms 的区别只有一个:**手机号由服务端取**,前端不传。
	//   校验规则完全一致(code + scene + phone 三者全等 + 未用过 + 未过期),
	//   见 consumeSmsCode —— 绑定手机号那条规则只有这一处实现。
	VerifyAccountSms(ctx context.Context, caller *Caller, scene, code string) (bool, error)
	// SendBindPhoneCode 给**新手机号**发验证码(2026-10-29 C5)
	//
	//   与 SendAccountSms 的方向正好相反:那里发给**账号登记的号**,
	//   这里发给**用户想换绑的新号** —— 所以手机号由前端传。
	//
	//   ⚠️ **为什么必须由前端传新号**:换绑要证明的是"用户占着**新**号",
	//     而服务端手上只有旧号。旧号上收到的验证码只能证明"用户还占着旧号",
	//     证明不了"用户占着新号" —— 那是换绑真正要证明的那件事。
	//
	//   scene 复用 change_phone(阿里云那张"换绑手机模板"就是为它准备的)。
	//   两个场景靠 phone 这一维天然隔离:
	//     · 切高敏身份 → 码发给【旧号】,消费时按旧号查
	//     · 换绑手机号 → 码发给【新号】,消费时按新号查
	//   ⇒ 同一条码在两边都用不了(见 consumeSmsCode 的 phone 条件)。
	SendBindPhoneCode(ctx context.Context, caller *Caller, phone string) error
	// BindPhone 换绑手机号(校验**新号**收到的验证码 + 双写 phone / phone_hash)
	//
	//   短信二次验证只发生在"新号"这一侧 —— 这是 SRS §2.2.3 的原话:
	//   「输入新手机号并完成新号验证」。旧号不再单独验证(它要失效了)。
	//
	//   ⚠️ **phone 与 phone_hash 必须一起写**(M100 审计定的硬约束):
	//     只写 phone 的话,"按手机号短信登录 / 短信重置密码"会直接失配 ——
	//     那两条流程是按 phone_hash 查人的(见 LoginBySms / ResetPasswordBySms)。
	BindPhone(ctx context.Context, caller *Caller, phone, code string) (*CurrentUserView, error)
	// GetCurrentUser 当前用户信息(含身份列表 + 资源树)
	//   ⚠️ M149:参数是 caller(不是 userID)—— 服务层需要"当前身份"才能算对
	//   资源树(以前按所有身份的并集算,切身份后界面完全不变)。
	//
	//   ⚠️ M149-b:**不再返 isSuper / isOps** —— 那两个标志由
	//   NewCurrentUserView 按当前身份的角色码一次性派生(唯一口径 platformFlags)。
	//   这里再返一遍就是同一个判定做了两遍,迟早漂移。
	GetCurrentUser(ctx context.Context, caller *Caller) (*model.SysUser, []model.SysUserIdentity, []model.SysResource, error)
	// AccessibleResources 当前 caller 可访问的资源(供前端动态菜单/按钮)
	//   ⚠️ M149:**按当前身份算**(caller.RoleCode = token 里当前身份的角色),
	//   不是"这个用户所有身份"的并集 —— 否则切身份后菜单/按钮完全不变。
	//   typeFilter="" 返全部 type (默认,向后兼容)
	//   typeFilter="menu" 仅返 type=menu(供 Sidebar,Phase 5 M2 新加)
	//   typeFilter="api" 仅返 type=api(供按钮级 <Permission perm="user:create">)
	//   typeFilter="button" 仅返 type=button(供 RowAction / 工具栏按钮)
	AccessibleResources(ctx context.Context, caller *Caller, typeFilter string) ([]model.SysResource, error)
	// UpdateProfile 更新个人资料(nickname / email / realName / avatar)
	//   phone 变更不在本接口范围(需 SMS 二次验证, 走单独端点)
	//
	//   ⚠️ **M113(2026-10-29):返回类型从 *model.SysUser 改成 *CurrentUserView**
	//
	//	旧实现返的是**裸 model.SysUser**,而前端把它当 UserInfo 存进了 store:
	//
	//		svc.UpdateProfile(...) → h.OK(c, u)  →  { id, username, nickname, ... }
	//
	//	而 model.SysUser 的 `phone` / `email` 是 `json:"-"`(**根本不返**),
	//	`identities` / `is_platform_super_admin` / `current_identity_id`
	//	**压根不存在** ⇒ 前端 `setUserInfo(updated)` 一执行,
	//	**身份列表、超管标记、当前身份、手机号全被洗成空**。
	//	(症状:改一次昵称,右上角的超管标签没了、"我的身份"页签空了。)
	//
	//	技术方案 §7.7.4.6 一直写的就是"Response Data: UserInfo" ——
	//	是**实现**偏离了契约,不是契约写错了。现在让实现回到契约上。
	UpdateProfile(ctx context.Context, caller *Caller, dto UpdateProfileDTO) (*CurrentUserView, error)

	// ===== 短信验证码扩展(公开) =====
	// LoginBySms 短信验证码登录(免密码, 校验通过后签发与 Login 同样的 token)
	//
	//   ⚠️ M150:mfa_enabled 的账号**不需要再加验** —— 验证码本来就发到
	//   登记手机号上,"持机"这半个因子已天然满足,MFA 的目的已经达成。
	LoginBySms(ctx context.Context, phone, smsCode, loginIP, userAgent string) (*AuthTokens, error)
	// ResetPasswordBySms 短信验证码重置密码(自助, 无需登录)
	ResetPasswordBySms(ctx context.Context, phone, smsCode, newPassword string) error

	// ===== 登录二次验证 MFA(M150)=====
	//
	// 因子是"绑定手机收码"。前提:账号已登记手机号(个人中心绑定);
	// 开着 MFA 但手机没了 = fail-closed,恢复走管理员 DisableUserMfa。
	//
	// SendMfaLoginCode 登录 MFA 步骤的**重发**(POST /public/auth/mfa/send)。
	//   密码必须先验对才发 —— 发码权 = "知道密码",与 Login 的自动发码同权。
	//   loginlock 照常计数,防拿这个端点撞密码。
	//   冷却由 SendSms 的 per phone+scene 限流兜底,不会变成短信轰炸器。
	SendMfaLoginCode(ctx context.Context, username, password, loginIP string) error
	// EnableMfa 开启登录二次验证(本人操作,smsCode = 发给登记手机号的 mfa 码)
	//   返回最新的 CurrentUserView —— 前端整份替换 userInfo(mfa_enabled 变了)。
	//   未绑手机号 → InvalidParam("账号未登记手机号");已开启 → StateInvalid。
	EnableMfa(ctx context.Context, caller *Caller, smsCode string) (*CurrentUserView, error)
	// DisableMfa 关闭登录二次验证(本人操作,同样要验证码 ——
	//   否则偷到会话的人第一件事就是把 MFA 关掉)
	DisableMfa(ctx context.Context, caller *Caller, smsCode string) (*CurrentUserView, error)

	// ===== 会话与设备管理(SRS §2.2.1 #6)=====
	// ListSessions 我自己的活跃会话 + 历史登录记录
	//   只看本人 —— 不接 userId 参数,没有可越权的入口
	ListSessions(ctx context.Context, caller *Caller, page, size int) (*SessionOverview, error)
	// RevokeSessions 强制下线指定会话(必须属于我自己)
	//   返回真正被吊销的条数(传进来的 sid 可能已失效,那个数要如实反映)
	RevokeSessions(ctx context.Context, caller *Caller, sessionIDs []string) (int, error)
}

// UpdateProfileDTO 个人资料 DTO
//   指针字段:区分"不改"(nil)与"改空串"两种语义
//   字段含义与前端 web/src/types/user.ts 的 UpdateProfileRequest 对齐
//
// ⚠️ **这里没有 Avatar 字段**(M124 删掉了)。
// 它以前在,允许前端直接传一个**任意 URL** 当头像 —— 而头像会渲染在
// **别人的浏览器里**(管理端用户列表 / 顶栏 / 身份切换器)⇒ 任意用户都能
// 往管理员的页面上塞一个外部图片,那是**跟踪像素**(读 IP / Referer,
// 还能借管理员的浏览器发请求)。
//
// 而且它本来就是**死代码**:前端从来没发过这个字段。
// ⇒ 现在头像是**上传**出来的,唯一入口 `SetAvatar`(见 user_avatar.go)。
type UpdateProfileDTO struct {
	Nickname *string `json:"nickname,omitempty"`
	Email    *string `json:"email,omitempty"`
	RealName *string `json:"real_name,omitempty"`
}
