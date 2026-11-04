// 鉴权 handler
// 文档 §7.3
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/errcode"
	"rapid-admin/internal/service"
)

// AuthHandler
type AuthHandler struct {
	*handler.BaseHandler
}

// NewAuthHandler
func NewAuthHandler(r *service.Registry) *AuthHandler {
	return &AuthHandler{BaseHandler: handler.New(r)}
}

// requireCaller 取当前 caller
func (h *AuthHandler) requireCaller(c *app.RequestContext) *service.Caller {
	return h.BaseHandler.RequireCaller(c)
}

// Login POST /rapid-admin/v1/public/auth/login
func (h *AuthHandler) Login(ctx context.Context, c *app.RequestContext) {
	var req service.LoginRequest
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	// 图形验证码校验(handler 层统一处理, service 不感知 captcha 机制)
	//
	// ⚠️ M150 例外:MFA 复验那次调用(mfa_code 非空)跳过图形码。
	//   图形码是一次性的,首次挑战已经消耗掉了;而复验请求本身要
	//   「正确密码 + 发到绑定手机的短信码」两重凭据才走得通,密码错在
	//   MFA 检查之前就失败、照常进 loginlock —— 图形码在这条路径上没有
	//   增量价值,强要第二次只会把用户卡死在 MFA 步骤(永远 CaptchaWrong)。
	//   绕过图形码的自动化撞库仍被 loginlock(锁定)+ /auth/mfa/send 的
	//   IP 限流兜住。mfa/send 端点同样是"无图形码但密码先验"的同款取舍。
	if req.MfaCode == "" {
		if !h.verifyCaptcha(ctx, c, config.CaptchaSceneLogin, req.CaptchaID, req.CaptchaCode) {
			return
		}
	}
	req.LoginIP = c.ClientIP()
	req.UserAgent = string(c.UserAgent())
	res, err := h.Services.Auth.Login(ctx, req)
	if err != nil {
		h.Fail(c, err)
		return
	}
	// 2026-10-21:响应体统一由 service.AuthTokens 承担(handler 不再拼 map)。
	// 此前 Login 拿 *model.SysUser 直返后又要二次调 GetCurrentUser 重新拼一份
	// snake_case 视图 —— 两次查询换同一个结果,而且 login/switch/refresh 三个
	// 端点各拼各的,形状迟早分叉(refresh 就分叉过,漏了 identities)。
	h.OK(c, res)
}

// verifyCaptcha 校验请求体里的 captchaId + captchaCode
// 不通过时直接写错误响应并返回 false; 通过返回 true, handler 继续往下走
//
// 全后端唯一开关点:4 个 public 写接口都走这里,service 层完全不感知 captcha 机制。
// scene 决定用哪个开关(captcha.login_enabled / captcha.sms_send_enabled):
//
//	CaptchaSceneLogin    密码登录 / 短信登录 / 短信重置密码 —— 挡自动化撞库
//	CaptchaSceneSmsSend  发短信                      —— 防短信刷量闸门
//
// 开关关闭时直接放行,连 id/code 是否为空都不再要求 —— 前端此时也不显示
// 验证码框,若仍要求必填会变成"前端不显示、后端报必填"的死结。
//
// 每次调用现读 config,配合 Load() 的 OnConfigChange 可热改 yaml 即时生效,
// 不必重启;代价是每个请求多一次指针解引用,可忽略。
func (h *AuthHandler) verifyCaptcha(ctx context.Context, c *app.RequestContext, scene config.CaptchaScene, id, code string) bool {
	if !config.CaptchaEnabled(scene) {
		return true
	}
	if id == "" || code == "" {
		h.FailCode(c, errcode.CaptchaRequired, "请先输入图形验证码")
		return false
	}
	if !h.Services.Captcha.Verify(ctx, id, code) {
		h.FailCode(c, errcode.CaptchaWrong, "图形验证码错误或已过期")
		return false
	}
	return true
}

// Logout POST /rapid-admin/v1/auth/logout
//   - revoke 当前 jti(走 auth.Blacklist,无 Redis 时降级 skip)
//   - 写 sys_login_log.LogoutAt
//   - 前端收到 200 后清本地 state + 跳 /login
func (h *AuthHandler) Logout(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	if err := h.Services.Auth.Logout(ctx, caller); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// RefreshToken POST /rapid-admin/v1/public/auth/refresh
func (h *AuthHandler) RefreshToken(ctx context.Context, c *app.RequestContext) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	res, err := h.Services.Auth.RefreshToken(ctx, body.RefreshToken)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// ChangePassword POST /rapid-admin/v1/auth/password
//   - 校验旧密码 + 强度校验 + revoke 当前 jti
//   - 成功后前端需重新登录(因 jti 已失效)
func (h *AuthHandler) ChangePassword(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Auth.ChangePassword(ctx, caller, body.OldPassword, body.NewPassword); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// ResetPassword POST /rapid-admin/v1/auth/password/reset
//
// Phase 1.6 IDOR 修复(2026-10-17):
//   - 旧实现:handler 从 body 取 userId 直接调 service,任何有 auth:reset-password 权限的人
//     可重置任意用户密码(典型 OWASP BOLA #1)
//   - 新实现:
//     1) body.UserID 必须 == caller.UserID(改自己密码) — 普通用户
//     2) 或 caller 是 super_admin(代重置) — 走审计
//     3) userId=0 视为「自己」,自动填 caller.UserID
//   - 注:AuthService.ResetPassword 当前是 stub(返回 NotImplemented),handler 鉴权
//     修完后,service 真实实现需保留 caller 检查作为深度防御
func (h *AuthHandler) ResetPassword(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		UserID      uint64 `json:"user_id,string"`
		NewPassword string `json:"new_password"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	// 0 视为「自己」,自动填 caller.UserID
	if body.UserID == 0 {
		body.UserID = caller.UserID
	}
	// IDOR 阻断:非超管只能改自己密码
	if body.UserID != caller.UserID && !caller.HasRole("role_super_admin") {
		h.FailCode(c, errcode.Forbidden, "只能重置自己的密码")
		return
	}
	if err := h.Services.Auth.ResetPassword(ctx, caller, body.UserID, body.NewPassword); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// GetCurrentUser GET /rapid-admin/v1/auth/me
func (h *AuthHandler) GetCurrentUser(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	user, ids, resources, err := h.Services.Auth.GetCurrentUser(ctx, caller)
	if err != nil {
		h.Fail(c, err)
		return
	}
	// 2026-10-21:走 service.NewCurrentUserView —— 与 Login / SwitchIdentity /
	//   RefreshToken 共用同一个 view struct,四个端点的 user 形状从此由类型保证,
	//   不再是"每个 handler 自己拼一个 map"。
	//   current_identity_id 从 caller 拿(Caller 由 token claims 解析而来,
	//   是服务端权威值,不依赖前端回传)。
	currID := uint64(0)
	if caller.IdentityID != 0 {
		currID = caller.IdentityID
	}
	h.OK(c, map[string]any{
		// ⚠️ M149-b:角色码给的是 **caller.RoleCode**(token claims 里那个),
		//   与 RBAC 中间件判权限**同一个来源** ⇒ 前端不用猜"我是哪个角色"。
		"user":      service.NewCurrentUserView(user, ids, caller.RoleCode, currID),
		"resources": resources,
	})
}

// ListSessions GET /rapid-admin/v1/auth/sessions
//
//	我自己的活跃会话 + 历史登录记录(SRS §2.2.1 #6)
//	Query: page / size(历史记录那部分分页;活跃会话不分页)
//
//	**不接 userId**:只能看自己的,接口层面就没有可越权的入口。
func (h *AuthHandler) ListSessions(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	page, size := h.GetPage(c)
	res, err := h.Services.Auth.ListSessions(ctx, caller, page, size)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// RevokeSessionsRequest POST /auth/sessions/revoke 的请求体
type RevokeSessionsRequest struct {
	// SessionIDs 要下线的会话 id
	//
	// 前端从 /auth/sessions 的返回里取。**空数组 = 什么都不做**(不是错误):
	// "下线其他所有设备"在只有本机时就会传空。
	SessionIDs []string `json:"session_ids"`
}

// RevokeSessions POST /rapid-admin/v1/auth/sessions/revoke
//
//	强制下线指定会话(必须属于我自己 —— 归属校验在 service 层)
//
//	返回 {"revoked": n}:n 是**真正被吊销**的条数。传进来的 sid 可能已经
//	过期或被并发吊掉,那个数要如实反映,否则前端会说"已下线 3 个"而实际只下线 1 个。
func (h *AuthHandler) RevokeSessions(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var req RevokeSessionsRequest
	if err := h.BindJSON(c, &req); err != nil {
		h.Fail(c, err)
		return
	}
	n, err := h.Services.Auth.RevokeSessions(ctx, caller, req.SessionIDs)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, map[string]any{"revoked": n})
}

// AccessibleResources GET /rapid-admin/v1/system/resource/accessible
//
//	返回当前 caller 可访问的资源(供前端动态菜单/按钮)
//	Query: type=menu|api (Phase 5 M2 新加), 空 = 全部 type
func (h *AuthHandler) AccessibleResources(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	typeFilter := string(c.QueryArgs().Peek("type"))
	res, err := h.Services.Auth.AccessibleResources(ctx, caller, typeFilter)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// SwitchIdentityRequest POST /auth/switch-identity 的请求体
//
// 2026-10-21 从匿名 struct 提成具名类型 —— 有两个原因,第二个更要命:
//
//  1. 一致性:登录/续期/切换三个端点的响应已统一成 service.AuthTokens,
//     请求体却还散在各 handler 的匿名 struct 里。
//  2. 可测:`identity_id,string` 这一行就是身份切换 404 的**唯一现场**。
//     匿名 struct 没法在测试里引用它 —— 测试只能自己抄一份,抄的那份和线上
//     那份迟早分叉,而分叉之后测试还是绿的。提成具名类型后,
//     TestSwitchIdentityRequest_BindsStringIDLosslessly 测的就是线上这份。
type SwitchIdentityRequest struct {
	// IdentityID 必须以字符串上 wire:sonyflake 是 19 位数,JS 的 2^53 装不下。
	//   之前这里是裸 uint64,前端 JSON.parse 后末 6 位被吃掉,回传就 404。
	IdentityID uint64 `json:"identity_id,string"`
	// SmsCode 切换高敏角色时的短信验证码
	SmsCode string `json:"sms_code"`
}

// SwitchIdentity POST /rapid-admin/v1/auth/switch-identity
//   - 切换到高敏角色时 body.smsCode 必填(由前端 SmsCodeGuard 弹出)
//   - 返回新 token + 新 identities,前端用新 token 替换旧 token
//   - 2026-10-18:响应里 user 字段用 snake_case DTO,与 Login /auth/me 一致
func (h *AuthHandler) SwitchIdentity(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body SwitchIdentityRequest
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	res, err := h.Services.Auth.SwitchIdentity(ctx, caller, body.IdentityID, body.SmsCode)
	if err != nil {
		h.Fail(c, err)
		return
	}
	// 2026-10-21:同 Login,直接返 service 层的 AuthTokens。
	//   current_identity_id = 新切的身份 id(service 传 target.ID),
	//   前端 profile 高亮 + IdentitySwitcher 选中项都用它,不再需要二次查询。
	h.OK(c, res)
}

// UpdateProfile PATCH /rapid-admin/v1/auth/profile
//   - 更新个人资料(nickname / email / realName / avatar)
//   - phone 变更需走单独的 /auth/phone 端点 + SMS 二次验证(本期范围外)
//   - 任何人只可改自己的
//
// ⚠️ **M113(2026-10-29):返回的是 CurrentUserView,不是裸 SysUser**
//
//	前端把这份响应**整份替换**进 store 的 userInfo,
//	所以少返一个字段 = 把那个字段从界面上洗掉。
//	(旧实现返裸 SysUser ⇒ phone/email 是 json:"-" 不返、identities 压根不存在
//	 ⇒ 改一次昵称,超管标签和身份列表全空。)
//	契约见技术方案 §7.7.4.6 —— 那边一直写的就是 "Response Data: UserInfo"。
func (h *AuthHandler) UpdateProfile(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var dto service.UpdateProfileDTO
	if err := h.BindJSON(c, &dto); err != nil {
		h.Fail(c, err)
		return
	}
	u, err := h.Services.Auth.UpdateProfile(ctx, caller, dto)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, u)
}

// SendSms POST /rapid-admin/v1/public/auth/sms/send
//
//	公开接口, 发短信前必传图形验证码, 防止短信接口被脚本刷量
func (h *AuthHandler) SendSms(ctx context.Context, c *app.RequestContext) {
	var body struct {
		Phone       string `json:"phone"`
		Scene       string `json:"scene"`
		CaptchaID   string `json:"captcha_id"`
		CaptchaCode string `json:"captcha_code"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if !h.verifyCaptcha(ctx, c, config.CaptchaSceneSmsSend, body.CaptchaID, body.CaptchaCode) {
		return
	}
	if err := h.Services.Auth.SendSms(ctx, body.Phone, body.Scene); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// VerifySms POST /rapid-admin/v1/public/auth/sms/verify
func (h *AuthHandler) VerifySms(ctx context.Context, c *app.RequestContext) {
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
		Scene string `json:"scene"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	res, err := h.Services.Auth.VerifySms(ctx, body.Phone, body.Scene, body.Code)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// ===== 自助短信验证码(2026-10-28 M104)=====
//
// 这两个端点存在的理由只有一个:**前端手上只有脱敏手机号**。
//
//	/auth/me 返的 phone 是 138****5678,而公开的 /public/auth/sms/send
//	要求 11 位纯数字(validPhone)—— 前端拿脱敏号去发,后端直接
//	「手机号格式错误」。于是两个必须短信的场景**从来没走通过**:
//	  · 切到高敏身份(IdentitySwitcher)
//	  · 个人中心改密码(/profile)
//
//	所以:手机号一律由服务端从 sys_user 取,前端不传。
//	校验侧同理 —— VerifyAccountSms 也用账号登记的号去比对,
//	于是验证码天然**绑死在账号自己的手机上**。

// SendAccountSms POST /rapid-admin/v1/auth/sms/account/send
//
//	body: { scene }
//
//	scene 必须在 service 层的 accountSmsScenes 白名单里
//	(change_phone / reset_password / view_full_phone)。
func (h *AuthHandler) SendAccountSms(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		Scene string `json:"scene"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Auth.SendAccountSms(ctx, caller, body.Scene); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// VerifyAccountSms POST /rapid-admin/v1/auth/sms/account/verify
//
//	body: { scene, code }
//
//	校验通过即消费(used_at 置位),不可复用 —— 与公开的 VerifySms 同一套规则。
func (h *AuthHandler) VerifyAccountSms(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		Scene string `json:"scene"`
		Code  string `json:"code"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	res, err := h.Services.Auth.VerifyAccountSms(ctx, caller, body.Scene, body.Code)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// ===== 换绑手机号(2026-10-29 C5)=====
//
//	这两个端点跟上面的"自助短信"是**反方向**的:
//
//	  /auth/sms/account/send → 手机号由服务端取(账号登记的号)
//	  /auth/phone/code       → 手机号由**前端传**(要换绑的新号)
//
//	为什么要反着来:换绑要证明的是"用户占着**新**号",
//	而服务端手上只有旧号 —— 用旧号验证只能证明"用户还占着旧号",
//	证明不了换绑真正要证明的那件事。
//
//	校验规则在 service 层(BindPhone),handler 只做取参 + 返响应。

// SendBindPhoneCode POST /rapid-admin/v1/auth/phone/code
//
//	body: { phone }  —— 要换绑的**新**手机号
//
//	短信模板复用 change_phone(阿里云那张"换绑手机模板")。
func (h *AuthHandler) SendBindPhoneCode(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		Phone string `json:"phone"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Auth.SendBindPhoneCode(ctx, caller, body.Phone); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// BindPhone PATCH /rapid-admin/v1/auth/phone
//
//	body: { phone, code }  —— 新手机号 + 发到**新号**上的验证码
//
//	返的是 CurrentUserView(不是裸 SysUser):前端拿它整份替换 store 的 userInfo,
//	少返一个字段 = 把那个字段从界面上洗掉(同 UpdateProfile 的教训)。
func (h *AuthHandler) BindPhone(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	u, err := h.Services.Auth.BindPhone(ctx, caller, body.Phone, body.Code)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, u)
}

// ===== 登录二次验证 MFA(M150)=====
//
//	路由与权限:
//	  POST /public/auth/mfa/send   —— 登录中途重发码,公开(密码先验,见下)
//	  POST /auth/mfa/enable        —— 本人开启(码由 EnableMfa 消费)
//	  POST /auth/mfa/disable       —— 本人关闭(同上;不验码 = 偷会话的人先关 MFA)
//
//	权限复用 auth:account-sms(所有角色都有的自服务码):MFA 开关和"给自己
//	发/验验证码"是同一家族的自助能力,新增 auth:mfa 码要走 seed + 矩阵对账,
//	为两条路由不值(同 UpdateProfile/Avatar 复用码的先例)。

// SendMfaLoginCode POST /rapid-admin/v1/public/auth/mfa/send
//
//	body: { username, password } —— 密码是前端 SHA-256 后的 hex
//
//	登录 MFA 步骤的"重发验证码"。密码先验对才发 —— 发码权 = "知道密码",
//	与 Login 首次挑战的自动发码同权。loginlock 照常计数。
func (h *AuthHandler) SendMfaLoginCode(ctx context.Context, c *app.RequestContext) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if err := h.Services.Auth.SendMfaLoginCode(ctx, body.Username, body.Password, c.ClientIP()); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}

// EnableMfa POST /rapid-admin/v1/auth/mfa/enable
//
//	body: { sms_code }  —— 发到登记手机号上的 scene=mfa 验证码
//
//	返 CurrentUserView(mfa_enabled=true 的最新视图),前端整份替换 userInfo。
func (h *AuthHandler) EnableMfa(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		SmsCode string `json:"sms_code"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	u, err := h.Services.Auth.EnableMfa(ctx, caller, body.SmsCode)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, u)
}

// DisableMfa POST /rapid-admin/v1/auth/mfa/disable
//
//	body: { sms_code }
//
//	返 CurrentUserView(mfa_enabled=false 的最新视图)。
func (h *AuthHandler) DisableMfa(ctx context.Context, c *app.RequestContext) {
	caller := h.requireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		SmsCode string `json:"sms_code"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	u, err := h.Services.Auth.DisableMfa(ctx, caller, body.SmsCode)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, u)
}

// LoginBySms POST /rapid-admin/v1/public/auth/login-sms
// 短信验证码登录(免密码), 校验通过后签发与 Login 同等的 access+refresh
func (h *AuthHandler) LoginBySms(ctx context.Context, c *app.RequestContext) {
	var body struct {
		Phone       string `json:"phone"`
		SmsCode     string `json:"sms_code"`
		CaptchaID   string `json:"captcha_id"`
		CaptchaCode string `json:"captcha_code"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if !h.verifyCaptcha(ctx, c, config.CaptchaSceneLogin, body.CaptchaID, body.CaptchaCode) {
		return
	}
	res, err := h.Services.Auth.LoginBySms(ctx, body.Phone, body.SmsCode, c.ClientIP(), string(c.UserAgent()))
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}

// ResetPasswordBySms POST /rapid-admin/v1/public/auth/reset-password-sms
// 短信验证码 + 新密码 自助重置(无需登录)
func (h *AuthHandler) ResetPasswordBySms(ctx context.Context, c *app.RequestContext) {
	var body struct {
		Phone       string `json:"phone"`
		SmsCode     string `json:"sms_code"`
		NewPassword string `json:"new_password"`
		CaptchaID   string `json:"captcha_id"`
		CaptchaCode string `json:"captcha_code"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}
	if !h.verifyCaptcha(ctx, c, config.CaptchaSceneLogin, body.CaptchaID, body.CaptchaCode) {
		return
	}
	if err := h.Services.Auth.ResetPasswordBySms(ctx, body.Phone, body.SmsCode, body.NewPassword); err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, nil)
}
