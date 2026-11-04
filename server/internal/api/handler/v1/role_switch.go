// 身份切换 handler
// 文档 §9.4 — 双因素切换(高危角色需 SMS)
//
// 2026-10-19 M62:双写 biz_role_switch_audit(业务专用细表)+ sys_operation_log(通用兜底)
//
// 2026-10-28 M104:切高敏身份用的短信验证码改由
//   POST /rapid-admin/v1/auth/sms/account/send 代发(见 auth.go 的 SendAccountSms),
//   不再是本文件里的专用接口 —— 手机号一律由服务端从 sys_user 取,
//   两个自助短信场景(切身份 / 个人中心改密)共用同一套实现。
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/model"
	"rapid-admin/internal/service"
)

type RoleSwitchHandler struct{ *handler.BaseHandler }

func NewRoleSwitchHandler(r *service.Registry) *RoleSwitchHandler {
	return &RoleSwitchHandler{BaseHandler: handler.New(r)}
}

// Switch POST /rapid-admin/v1/auth/switch-identity
//   body: { identityId, smsCode, reason }
func (h *RoleSwitchHandler) Switch(ctx context.Context, c *app.RequestContext) {
	caller := h.RequireCaller(c)
	if caller == nil {
		return
	}
	var body struct {
		IdentityID uint64 `json:"identity_id,string"`
		SmsCode    string `json:"sms_code"`
		Reason     string `json:"reason"`
	}
	if err := h.BindJSON(c, &body); err != nil {
		h.Fail(c, err)
		return
	}

	fromRole := caller.RoleCode
	fromDomain := ""
	if len(caller.ProjectIDs) > 0 {
		fromDomain = fmtUint64(caller.ProjectIDs[0])
	}

	res, err := h.Services.Auth.SwitchIdentity(ctx, caller, body.IdentityID, body.SmsCode)
	if err != nil {
		// 失败也写一条 audit(失败原因)
		h.writeRoleSwitchAudit(ctx, caller, fromRole, "", fromDomain, "", c, body.Reason, false, err.Error())
		h.Fail(c, err)
		return
	}

	// ⚠️ **M148(2026-11-01):这行以前是错的,而且错得很安静。**
	//
	//   注释写着"SwitchIdentity 内部已改 caller.RoleCode",但**内部从来没改过**
	//   ⇒ toRole 恒等于 fromRole ⇒ biz_role_switch_audit 那张表永远记不出
	//   "角色变了" —— 而它存在的意义就是记这个(查"谁把谁提成了超管"时,
	//   看到的 from == to,等于没有记录)。
	//
	//   现在 service 层那一步会把 caller 改成切换后的身份(它才知道目标角色),
	//   这行注释才变成真的。
	//   ⚠️ fromRole 必须在调用 SwitchIdentity **之前**取(上面第 45 行)——
	//   否则改完 caller 之后取到的也是新角色,from/to 又一样。
	toRole := caller.RoleCode
	toDomain := ""
	if len(caller.ProjectIDs) > 0 {
		toDomain = fmtUint64(caller.ProjectIDs[0])
	}
	_ = res // 避免 unused 警告(res 主要给前端展示)
	h.writeRoleSwitchAudit(ctx, caller, fromRole, toRole, fromDomain, toDomain, c, body.Reason, true, "")

	h.OK(c, res)
}

// writeRoleSwitchAudit 2026-10-19 M62 双写 biz_role_switch_audit + sys_operation_log
func (h *RoleSwitchHandler) writeRoleSwitchAudit(
	ctx context.Context,
	caller *service.Caller,
	fromRole, toRole string,
	fromDomain, toDomain string,
	c *app.RequestContext,
	reason string,
	success bool,
	errMsg string,
) {
	if h.Services.Audit == nil {
		return
	}
	auditLog := &model.RoleSwitchAudit{
		BaseModel:  model.BaseModel{},
		UserID:     caller.UserID,
		FromRole:   fromRole,
		ToRole:     toRole,
		FromDomain: fromDomain,
		ToDomain:   toDomain,
		IP:         c.ClientIP(),
		UA:         string(c.UserAgent()),
		Reason:     reason,
	}
	if !success {
		// 失败时 to_role 用占位("FAIL" 标识)
		// ⚠️ 前缀用 model.RoleSwitchFailPrefix —— service 侧靠它判 status,
		//   两处各写字面量就可能对不上(对不上 = 失败被记成成功)。
		auditLog.ToRole = model.RoleSwitchFailPrefix + errMsg
	}
	_ = h.Services.Audit.RecordRoleSwitch(ctx, auditLog)
}

// fmtUint64 简单转 string(避免拉 strconv)
func fmtUint64(n uint64) string {
	if n == 0 {
		return ""
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
